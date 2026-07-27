package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/yesme/hctl/internal/facts"
	"github.com/yesme/hctl/internal/gitx"
	"github.com/yesme/hctl/internal/protocol"
)

type RecoveryState string

const (
	RecoveryClean     RecoveryState = "clean"
	RecoveryDelivered RecoveryState = "delivered"
	RecoveryPublished RecoveryState = "published"
	RecoveryLost      RecoveryState = "lost"
)

type RecoveryResult struct {
	State       RecoveryState
	Publishing  string
	Remote      string
	RecoveryRef string
}

type LostPublicationError struct {
	Pending     string
	Remote      string
	RecoveryRef string
}

func (e *LostPublicationError) Error() string {
	return fmt.Sprintf(
		"PENDING_PUBLICATION_LOST: pending %s diverged from remote %s; preserved at %s; rerun after inspecting recovery",
		e.Pending, none(e.Remote), e.RecoveryRef,
	)
}

type PendingPublicationError struct {
	OID   string
	Cause error
}

func (e *PendingPublicationError) Error() string {
	return fmt.Sprintf("PENDING_PUBLICATION: remote outcome for event %s is not acquired; rerun to recover: %v", e.OID, e.Cause)
}

func (e *PendingPublicationError) Unwrap() error { return e.Cause }

type RecoveredPublicationError struct {
	Result RecoveryResult
}

func (e *RecoveredPublicationError) Error() string {
	return fmt.Sprintf(
		"PENDING_PUBLICATION_RECOVERED: publishing ref is now %s (%s); rerun from a fresh fact snapshot before appending",
		none(e.Result.Publishing), e.Result.State,
	)
}

type Publisher struct {
	Repo   *gitx.Repo
	Remote string
	Seat   string
}

// Recover resolves the publishing journal before any append. It never discards
// a losing pending commit: divergence is first anchored under refs/hctl/recovery.
func (p Publisher) Recover(ctx context.Context, advertisedRemoteTip string) (RecoveryResult, error) {
	if !protocol.ValidToken(p.Seat) {
		return RecoveryResult{}, fmt.Errorf("invalid publishing seat %q", p.Seat)
	}
	localRef := facts.CoopRef(p.Seat)
	trackingRef := facts.TrackingCoopRef(p.Remote, p.Seat)
	localTip, localExists, err := p.Repo.Resolve(ctx, localRef)
	if err != nil {
		return RecoveryResult{}, err
	}
	trackingTip, trackingExists, err := p.Repo.Resolve(ctx, trackingRef)
	if err != nil {
		return RecoveryResult{}, err
	}
	if advertisedRemoteTip != "" && (!trackingExists || trackingTip != advertisedRemoteTip) {
		return RecoveryResult{}, fmt.Errorf(
			"INCOMPLETE_FACTS: tracking tip %s does not equal advertised remote tip %s",
			none(trackingTip), advertisedRemoteTip,
		)
	}
	if advertisedRemoteTip == "" && trackingExists {
		return RecoveryResult{}, fmt.Errorf(
			"CORRUPT_CHAIN: remote chain disappeared while tracking retains %s",
			trackingTip,
		)
	}
	if !localExists {
		if trackingExists {
			if err := p.Repo.UpdateRef(ctx, localRef, trackingTip, "", "hctl: initialize publishing ref"); err != nil {
				return RecoveryResult{}, err
			}
			localTip = trackingTip
		}
		return RecoveryResult{State: RecoveryClean, Publishing: localTip, Remote: trackingTip}, nil
	}
	if localTip == trackingTip {
		return RecoveryResult{State: RecoveryClean, Publishing: localTip, Remote: trackingTip}, nil
	}
	// A pending journal must itself be a structurally valid chain before hctl
	// will attempt to publish it.
	chains, problems := facts.ReadChains(ctx, p.Repo, map[string]string{p.Seat: localTip})
	for _, problem := range problems {
		if problem.Severity == facts.SeverityError {
			return RecoveryResult{}, fmt.Errorf("%s: local pending chain is invalid: %s", problem.Code, problem.Detail)
		}
	}
	if chain := chains[p.Seat]; chain == nil || chain.Corrupt != nil {
		return RecoveryResult{}, errors.New("CORRUPT_CHAIN: local pending chain cannot be read")
	}

	if trackingTip != "" {
		delivered, err := p.Repo.IsAncestor(ctx, localTip, trackingTip)
		if err != nil {
			return RecoveryResult{}, err
		}
		if delivered {
			if err := p.Repo.UpdateRef(ctx, localRef, trackingTip, localTip, "hctl: pending publication delivered"); err != nil {
				return RecoveryResult{}, err
			}
			return RecoveryResult{State: RecoveryDelivered, Publishing: trackingTip, Remote: trackingTip}, nil
		}
	}
	pendingAhead := trackingTip == ""
	if trackingTip != "" {
		var err error
		pendingAhead, err = p.Repo.IsAncestor(ctx, trackingTip, localTip)
		if err != nil {
			return RecoveryResult{}, err
		}
	}
	if pendingAhead {
		if err := p.pushLease(ctx, trackingTip); err == nil {
			fetchErr := p.fetchTracking(ctx)
			if fetchErr != nil {
				// Push exit zero is the linearization point. The publication is
				// acquired even if refreshing the local observation fails.
				return RecoveryResult{State: RecoveryPublished, Publishing: localTip, Remote: localTip},
					fmt.Errorf("publication acquired but tracking refresh failed: %w", fetchErr)
			}
			return RecoveryResult{State: RecoveryPublished, Publishing: localTip, Remote: localTip}, nil
		} else {
			return RecoveryResult{}, &PendingPublicationError{OID: localTip, Cause: err}
		}
	}

	recoveryRef := "refs/hctl/recovery/" + p.Seat + "/" + localTip
	if existing, ok, err := p.Repo.Resolve(ctx, recoveryRef); err != nil {
		return RecoveryResult{}, err
	} else if ok {
		if existing != localTip {
			return RecoveryResult{}, fmt.Errorf("recovery ref collision at %s", recoveryRef)
		}
	} else if err := p.Repo.UpdateRef(ctx, recoveryRef, localTip, "", "hctl: preserve lost pending publication"); err != nil {
		return RecoveryResult{}, err
	}
	if trackingTip == "" {
		if err := p.Repo.DeleteRef(ctx, localRef, localTip, "hctl: align lost publication to absent remote"); err != nil {
			return RecoveryResult{}, err
		}
	} else if err := p.Repo.UpdateRef(ctx, localRef, trackingTip, localTip, "hctl: align lost publication to remote winner"); err != nil {
		return RecoveryResult{}, err
	}
	result := RecoveryResult{
		State: RecoveryLost, Publishing: trackingTip, Remote: trackingTip, RecoveryRef: recoveryRef,
	}
	return result, &LostPublicationError{Pending: localTip, Remote: trackingTip, RecoveryRef: recoveryRef}
}

// Append creates one single-file event commit, advances the publishing ref by
// local CAS, then publishes by remote force-with-lease. The returned OID is a
// fencing token only when err is nil.
func (p Publisher) Append(ctx context.Context, advertisedRemoteTip string, payload []byte, summary string) (string, error) {
	recovery, err := p.Recover(ctx, advertisedRemoteTip)
	if err != nil {
		return "", err
	}
	if recovery.State != RecoveryClean {
		return "", &RecoveredPublicationError{Result: recovery}
	}
	parent := recovery.Publishing
	blob, err := p.Repo.HashObject(ctx, appendNewline(payload))
	if err != nil {
		return "", err
	}
	tree, err := p.Repo.MakeSingleFileTree(ctx, "event.json", blob)
	if err != nil {
		return "", err
	}
	commit, err := p.Repo.CommitTree(ctx, tree, parent, summary)
	if err != nil {
		return "", err
	}
	if err := p.Repo.UpdateRef(ctx, facts.CoopRef(p.Seat), commit, parent, "hctl: append "+summary); err != nil {
		return "", fmt.Errorf("local CLAIM/event CAS lost: %w", err)
	}
	if err := p.pushLease(ctx, parent); err != nil {
		return "", &PendingPublicationError{OID: commit, Cause: err}
	}
	// A successful push is the acquisition point. Refresh tracking through fetch
	// (never update-ref) so observation and publishing remain separate.
	if err := p.fetchTracking(ctx); err != nil {
		return commit, nil
	}
	return commit, nil
}

func (p Publisher) pushLease(ctx context.Context, expected string) error {
	remoteRef := facts.CoopRef(p.Seat)
	lease := "--force-with-lease=" + remoteRef + ":" + expected
	refspec := remoteRef + ":" + remoteRef
	_, err := p.Repo.Output(ctx, "push", "--porcelain", lease, p.Remote, refspec)
	return err
}

func (p Publisher) fetchTracking(ctx context.Context) error {
	remoteRef := facts.CoopRef(p.Seat)
	trackingRef := facts.TrackingCoopRef(p.Remote, p.Seat)
	_, err := p.Repo.Output(ctx, "fetch", "--no-tags", p.Remote, remoteRef+":"+trackingRef)
	return err
}

func appendNewline(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\n' {
		return data
	}
	out := make([]byte, len(data)+1)
	copy(out, data)
	out[len(data)] = '\n'
	return out
}

func none(value string) string {
	if strings.TrimSpace(value) == "" {
		return "<none>"
	}
	return value
}
