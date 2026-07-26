package app

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/yesme/hctl/internal/facts"
	"github.com/yesme/hctl/internal/identity"
	"github.com/yesme/hctl/internal/protocol"
	"github.com/yesme/hctl/internal/store"
)

func (a *App) commandVerdict(ctx context.Context, load *loaded, options globalOptions, args []string) int {
	parsed, err := parseArgs(args, map[string]bool{
		"--decision": true, "--report": true, "--completeness": true,
		"--scope": true, "--incomplete-reason": true, "--pr": true,
	}, nil)
	if err != nil {
		return a.fail(ExitUsage, err)
	}
	id, err := requireOne(parsed.Positionals,
		"usage: hctl verdict <gate-obligation> --decision <decision> --report <path|commit:path> --completeness <value> [options]")
	if err != nil {
		return a.fail(ExitUsage, err)
	}
	state, ok := load.Result.Find(id)
	if !ok || state.Kind != "gate" {
		return a.fail(ExitGuard, fmt.Errorf("%q is not a current gate obligation", id))
	}
	if load.SeatErr != nil {
		return a.fail(ExitGuard, load.SeatErr)
	}
	if err := load.Result.WriteGuard(); err != nil {
		return a.fail(ExitGuard, err)
	}
	if load.Seat != state.Holder {
		return a.fail(ExitGuard, fmt.Errorf("gate holder is %s, current seat is %s", state.Holder, load.Seat))
	}
	if state.Canceled || state.Completed {
		return a.fail(ExitGuard, fmt.Errorf("gate obligation is %s", state.State))
	}
	if state.Claim == nil || state.Claim.Stale || state.Claim.Escalated {
		return a.fail(ExitGuard, fmt.Errorf("verdict requires a current non-stale active claim; state=%s", state.State))
	}
	if state.Revision == nil {
		return a.fail(ExitGuard, fmt.Errorf("gate has no exact revision"))
	}
	decision := parsed.Values["--decision"]
	if decision != "APPROVE" && decision != "REQUEST_CHANGES" {
		return a.fail(ExitUsage, fmt.Errorf("--decision must be APPROVE or REQUEST_CHANGES"))
	}
	completeness := parsed.Values["--completeness"]
	if completeness != "COMPLETE" && completeness != "INCOMPLETE" {
		return a.fail(ExitUsage, fmt.Errorf("--completeness must be COMPLETE or INCOMPLETE"))
	}
	incompleteReason := parsed.Values["--incomplete-reason"]
	if completeness == "INCOMPLETE" && incompleteReason == "" {
		return a.fail(ExitUsage, fmt.Errorf("INCOMPLETE requires --incomplete-reason naming uncovered work"))
	}
	reportValue := parsed.Values["--report"]
	if reportValue == "" {
		return a.fail(ExitUsage, fmt.Errorf("--report is required"))
	}
	report, err := resolveReport(ctx, load, state.Revision.Base, reportValue)
	if err != nil {
		return a.fail(ExitGuard, err)
	}
	scope, err := parseScopeFlag(parsed.Values["--scope"])
	if err != nil {
		return a.fail(ExitUsage, err)
	}
	pr := state.PR
	if value := parsed.Values["--pr"]; value != "" {
		pr, err = parsePositiveInt(value, "--pr")
		if err != nil {
			return a.fail(ExitUsage, err)
		}
	}
	if pr < 1 || load.Snapshot.PullTips[pr] != state.Revision.Head {
		return a.fail(ExitGuard, fmt.Errorf("PR %d does not identify exact head %s", pr, state.Revision.Head))
	}
	publisher := store.Publisher{Repo: load.Repo, Remote: load.Snapshot.Remote, Seat: load.Seat}
	if err := ensurePublishingClean(
		ctx, publisher, load.Snapshot.Advertised[facts.CoopRef(load.Seat)],
	); err != nil {
		return a.fail(ExitGuard, err)
	}
	if state.Verdict != nil && state.Verdict.Valid &&
		state.Verdict.Decision == decision &&
		state.Verdict.Completeness == completeness &&
		state.Verdict.IncompleteReason == incompleteReason &&
		state.Verdict.Report == report &&
		state.Verdict.PR == pr &&
		reflect.DeepEqual(state.Verdict.Scope, scope) {
		fmt.Fprintf(a.Stdout,
			"verdict already-current\nobligation=%s\nholder=%s\nverdict_oid=%s\nclaim=%s\nrevision={base:%s,head:%s}\ndecision=%s completeness=%s scope=%s\nnext=hctl status\n",
			state.Obligation.ID, state.Holder, state.Verdict.OID, state.Claim.OID,
			state.Revision.Base, state.Revision.Head, decision, completeness, scope.Kind)
		return ExitOK
	}
	actor, err := identity.Actor(load.Repo, load.Seat, options.Machine, options.Session)
	if err != nil {
		return a.fail(ExitGuard, err)
	}
	verdict := protocol.Verdict{
		SchemaVersion:    1,
		Actor:            actor,
		CreatedAt:        a.Now().UTC(),
		Obligation:       state.Obligation,
		Claim:            state.Claim.OID,
		PR:               pr,
		Revision:         *state.Revision,
		Decision:         decision,
		Report:           report,
		Scope:            scope,
		Completeness:     completeness,
		IncompleteReason: incompleteReason,
	}
	payload, err := protocol.MarshalVerdict(verdict)
	if err != nil {
		return a.fail(ExitGuard, err)
	}
	oid, err := publisher.Append(
		ctx, load.Snapshot.Advertised[facts.CoopRef(load.Seat)], payload,
		fmt.Sprintf("VERDICT %s %s", decision, shortID(state.Obligation.ID)),
	)
	if err != nil {
		return a.fail(ExitGuard, err)
	}
	fmt.Fprintf(a.Stdout,
		"verdict published\nobligation=%s\nholder=%s\nverdict_oid=%s\nclaim=%s\nrevision={base:%s,head:%s}\ndecision=%s completeness=%s scope=%s\nnext=hctl status\n",
		state.Obligation.ID, state.Holder, oid, state.Claim.OID,
		state.Revision.Base, state.Revision.Head, decision, completeness, scope.Kind)
	return ExitOK
}

func resolveReport(ctx context.Context, load *loaded, base, value string) (protocol.Source, error) {
	commit, path := base, value
	if prefix, suffix, ok := strings.Cut(value, ":"); ok && protocol.ValidOID(prefix) {
		commit, path = prefix, suffix
	}
	if path == "" {
		return protocol.Source{}, fmt.Errorf("report path is empty")
	}
	if !protocol.ValidMemoPath(path) {
		return protocol.Source{}, fmt.Errorf("report path %q is outside the memo lane memory/", path)
	}
	blob, _, err := load.Repo.BlobAt(ctx, commit, path)
	if err != nil {
		return protocol.Source{}, err
	}
	reachable, err := load.Repo.IsAncestor(ctx, commit, base)
	if err != nil {
		return protocol.Source{}, err
	}
	if !reachable {
		return protocol.Source{}, fmt.Errorf("report commit %s is not reachable from base %s; merge memo first", commit, base)
	}
	return protocol.Source{Commit: commit, Path: path, Blob: blob}, nil
}

func parseScopeFlag(value string) (protocol.ReviewScope, error) {
	if value == "" || value == "full" {
		return protocol.ReviewScope{Kind: "full"}, nil
	}
	raw := []byte(value)
	if strings.HasPrefix(value, "@") {
		var err error
		raw, err = os.ReadFile(strings.TrimPrefix(value, "@"))
		if err != nil {
			return protocol.ReviewScope{}, err
		}
	}
	scope, err := protocol.ParseReviewScope(raw)
	if err != nil {
		return protocol.ReviewScope{}, fmt.Errorf("invalid --scope: %w", err)
	}
	return scope, nil
}
