package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/yesme/hctl/internal/derive"
	"github.com/yesme/hctl/internal/facts"
	"github.com/yesme/hctl/internal/identity"
	"github.com/yesme/hctl/internal/protocol"
	"github.com/yesme/hctl/internal/store"
)

func (a *App) commandClaim(ctx context.Context, load *loaded, options globalOptions, args []string) int {
	parsed, err := parseArgs(args, map[string]bool{"--reclaim": true}, nil)
	if err != nil {
		return a.fail(ExitUsage, err)
	}
	id, err := requireOne(parsed.Positionals, "usage: hctl claim <obligation> [--reclaim <claim-oid>]")
	if err != nil {
		return a.fail(ExitUsage, err)
	}
	state, ok := load.Result.Find(id)
	if !ok {
		return a.fail(ExitGuard, fmt.Errorf("unknown obligation %q; run hctl status", id))
	}
	oid, reused, err := a.acquireClaim(ctx, load, options, state, parsed.Values["--reclaim"], true)
	if err != nil {
		return a.fail(ExitGuard, err)
	}
	action := "acquired"
	if reused {
		action = "already-active"
	}
	next := state.NextAction
	if !reused {
		next = nextAfterClaim(state)
	}
	fmt.Fprintf(a.Stdout,
		"claim %s\nobligation=%s\nholder=%s\nfencing_token=%s\nnext=%s\n",
		action, state.Obligation.ID, state.Holder, oid, next)
	return ExitOK
}

func (a *App) acquireClaim(
	ctx context.Context,
	load *loaded,
	options globalOptions,
	state *derive.ObligationState,
	reclaimOID string,
	allowExisting bool,
) (string, bool, error) {
	if load.SeatErr != nil {
		return "", false, load.SeatErr
	}
	if err := load.Result.WriteGuard(); err != nil {
		return "", false, err
	}
	if load.Seat != state.Holder {
		return "", false, fmt.Errorf(
			"obligation %s holder is %s, current seat is %s",
			state.Obligation.ID, state.Holder, load.Seat,
		)
	}
	if state.Canceled || state.Completed || state.State == "escalated" {
		return "", false, fmt.Errorf("obligation %s is %s: %s", state.Obligation.ID, state.State, state.NextAction)
	}
	publisher := store.Publisher{Repo: load.Repo, Remote: load.Snapshot.Remote, Seat: load.Seat}
	if err := ensurePublishingClean(
		ctx, publisher, load.Snapshot.Advertised[facts.CoopRef(load.Seat)],
	); err != nil {
		return "", false, err
	}
	if state.Claim != nil && reclaimOID == "" {
		if allowExisting {
			return state.Claim.OID, true, nil
		}
		return "", false, fmt.Errorf("active claim is %s; use --reclaim explicitly", state.Claim.OID)
	}
	if state.Claim == nil && reclaimOID != "" {
		return "", false, fmt.Errorf("--reclaim supplied but obligation has no current claim")
	}
	if state.Claim != nil {
		if reclaimOID != state.Claim.OID {
			return "", false, fmt.Errorf("--reclaim must equal active claim %s", state.Claim.OID)
		}
		if !protocol.ValidOID(reclaimOID) {
			return "", false, fmt.Errorf("invalid reclaim OID")
		}
	}
	if state.Claim == nil {
		switch state.State {
		case "blocked_needs", "waiting_head", "waiting_pr", "blocked_quorum", "blocked_capacity", "satisfied":
			return "", false, fmt.Errorf("obligation %s is %s: %s", state.Obligation.ID, state.State, state.NextAction)
		}
	}
	if state.Kind == "author" && state.Claim == nil {
		for _, other := range load.Result.Obligations {
			if other == state || other.Kind != "author" || other.Holder != state.Holder || other.Claim == nil {
				continue
			}
			if !other.Claim.Stale && !other.Canceled && !other.Completed {
				return "", false, fmt.Errorf(
					"author capacity=1: %s already holds %s on assignment %s",
					state.Holder, other.Claim.OID, other.Assignment,
				)
			}
		}
	}
	if state.Kind == "merge" {
		if load.Snapshot.Seats.Config.MergeCoordinator != load.Seat {
			return "", false, fmt.Errorf("only merge coordinator %s may claim merge obligations", load.Snapshot.Seats.Config.MergeCoordinator)
		}
		for _, other := range load.Result.Obligations {
			if other == state || other.Kind != "merge" || other.Claim == nil {
				continue
			}
			if !other.Claim.Stale && !other.Canceled && !other.Completed {
				return "", false, fmt.Errorf("merge capacity=1 is held by claim %s", other.Claim.OID)
			}
		}
	}
	actor, err := identity.Actor(load.Repo, load.Seat, options.Machine, options.Session)
	if err != nil {
		return "", false, err
	}
	claim := protocol.Claim{
		SchemaVersion: 1,
		Actor:         actor,
		CreatedAt:     a.Now().UTC().Truncate(0),
		Obligation:    state.Obligation,
	}
	switch state.Kind {
	case "author":
		tip := load.Snapshot.BranchTips[state.BranchRef]
		if tip == "" {
			if localTip, ok, resolveErr := load.Repo.Resolve(ctx, state.BranchRef); resolveErr != nil {
				return "", false, resolveErr
			} else if ok {
				tip = localTip
			}
		}
		claim.TipAtClaimPresent = true
		if tip != "" {
			claim.TipAtClaim = &tip
		}
	case "gate":
		if state.Revision == nil {
			return "", false, fmt.Errorf("gate has no exact revision")
		}
		revision := *state.Revision
		claim.RevisionAtClaim = &revision
	case "merge":
		if state.Revision == nil || !state.Green {
			return "", false, fmt.Errorf("merge claim requires exact revision and green required quorum")
		}
		revision := *state.Revision
		claim.RevisionAtClaim = &revision
		source := load.Snapshot.Seats.Source
		claim.CoordinatorConfig = &source
	default:
		return "", false, fmt.Errorf("unsupported obligation kind %q", state.Kind)
	}
	if state.Claim != nil {
		claim.ReclaimOf = &reclaimOID
		reason := "operator"
		if state.Claim.Overdue {
			reason = "overdue"
		}
		claim.ReclaimReason = &reason
	}
	payload, err := protocol.MarshalClaim(claim)
	if err != nil {
		return "", false, err
	}
	summary := fmt.Sprintf("CLAIM %s %s", state.Kind, shortID(state.Obligation.ID))
	oid, err := publisher.Append(ctx, load.Snapshot.Advertised[facts.CoopRef(load.Seat)], payload, summary)
	if err != nil {
		return "", false, err
	}
	return oid, false, nil
}

func ensurePublishingClean(
	ctx context.Context,
	publisher store.Publisher,
	advertisedRemoteTip string,
) error {
	recovery, err := publisher.Recover(ctx, advertisedRemoteTip)
	if err != nil {
		return err
	}
	if recovery.State != store.RecoveryClean {
		return &store.RecoveredPublicationError{Result: recovery}
	}
	return nil
}

func shortID(id string) string {
	value := strings.TrimPrefix(id, "sha256:")
	if len(value) > 12 {
		value = value[:12]
	}
	return value
}

func nextAfterClaim(state *derive.ObligationState) string {
	switch state.Kind {
	case "author":
		return fmt.Sprintf("work on %s, then push and open the PR", state.BranchRef)
	case "gate":
		return fmt.Sprintf(
			"review threshold %s, merge the report memo, then run hctl verdict %s",
			state.Threshold, state.Obligation.ID,
		)
	case "merge":
		return fmt.Sprintf("run hctl merge %d --check, then hctl merge %d", state.PR, state.PR)
	default:
		return "run hctl status"
	}
}
