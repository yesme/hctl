package derive

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yesme/hctl/internal/config"
	"github.com/yesme/hctl/internal/facts"
	"github.com/yesme/hctl/internal/gitx"
	"github.com/yesme/hctl/internal/protocol"
)

func TestGateBaseAdvanceStalesClaim(t *testing.T) {
	t.Parallel()
	state, snapshot, repo := deriveFixture(t)
	claim := state.Claim
	if claim == nil || claim.Stale {
		t.Fatalf("fixture claim should be active: %+v", claim)
	}
	for ref, oid := range snapshot.BranchTips {
		if ref == "refs/heads/main" {
			snapshot.BranchTips[ref] = snapshot.BranchTips[state.BranchRef]
			_ = oid
		}
	}
	snapshot.MainCommit = snapshot.BranchTips["refs/heads/main"]
	result := Engine{Repo: repo, Now: func() time.Time { return time.Now() }, SkipMergeAudit: true}.Derive(context.Background(), snapshot, "grok")
	got, ok := result.Find(state.Obligation.ID)
	if !ok || got.Claim == nil || !got.Claim.Stale || got.State != "stale" {
		t.Fatalf("base-only movement did not stale gate claim: %+v", got)
	}
}

func TestQuorumAST(t *testing.T) {
	t.Parallel()
	node := config.QuorumNode{AtLeast: &config.AtLeastQuorum{
		Count: 2,
		Of: []config.QuorumNode{
			{Seat: "a"}, {Seat: "b"}, {Seat: "c"},
		},
	}}
	if !EvaluateQuorum(node, map[string]bool{"a": true, "c": true}) {
		t.Fatal("2-of-3 should be green")
	}
	if EvaluateQuorum(node, map[string]bool{"a": true}) {
		t.Fatal("1-of-3 should be red")
	}
}

func TestDeltaOnlyDoesNotCoverFullRequirement(t *testing.T) {
	t.Parallel()
	if scopeCovers(protocol.ReviewScope{
		Kind:  "composite",
		Parts: []protocol.ScopePart{{Kind: "delta", FromBlob: strings.Repeat("a", 40), ToBlob: strings.Repeat("b", 40)}},
	}) {
		t.Fatal("delta-only composite silently covered the full review surface")
	}
	if !scopeCovers(protocol.ReviewScope{
		Kind: "composite",
		Parts: []protocol.ScopePart{
			{Kind: "fix_verification", Findings: []string{"F1"}, TargetBlob: strings.Repeat("b", 40)},
			{Kind: "delta", FromBlob: strings.Repeat("a", 40), ToBlob: strings.Repeat("b", 40)},
		},
	}) {
		t.Fatal("fix verification + delta should form a complete composite")
	}
}

func TestCancelCurrentClaimThenFreshClaimResetsWindow(t *testing.T) {
	t.Parallel()
	state, snapshot, repo := deriveFixture(t)
	first := snapshot.Chains["grok"].Events[0]
	cancel := &protocol.Event{
		OID: strings.Repeat("c", 40), Type: "CANCEL", ChainSeat: "grok",
		Cancel: &protocol.Cancel{
			SchemaVersion: 1, Actor: protocol.Actor{Seat: "grok", Machine: "m"},
			CreatedAt: time.Now(), Target: protocol.CancelTarget{Kind: "claim", Claim: first.OID},
			Reason: "user reset",
		},
	}
	claim := *first.Claim
	claim.CreatedAt = claim.CreatedAt.Add(time.Second)
	claim.ReclaimOf = nil
	claim.ReclaimReason = nil
	fresh := &protocol.Event{
		OID: strings.Repeat("d", 40), Type: "CLAIM", ChainSeat: "grok", Claim: &claim,
	}
	snapshot.Chains["grok"].Events = append(snapshot.Chains["grok"].Events, cancel, fresh)
	if err := facts.PopulateIndexes(context.Background(), repo, snapshot); err != nil {
		t.Fatal(err)
	}
	result := Engine{Repo: repo, Now: time.Now, SkipMergeAudit: true}.Derive(context.Background(), snapshot, "grok")
	got, ok := result.Find(state.Obligation.ID)
	if !ok || got.Claim == nil || got.Claim.OID != fresh.OID || got.Claim.Streak != 0 || got.Claim.Escalated {
		t.Fatalf("CANCEL did not reset to a fresh claim window: %+v problems=%+v", got, result.Problems)
	}
	if !result.Complete {
		t.Fatalf("valid cancel/fresh-claim sequence was rejected: %+v", result.Problems)
	}
}

func TestCompleteVerdictResetsReclaimStreak(t *testing.T) {
	t.Parallel()
	state, snapshot, repo := deriveFixture(t)
	first := snapshot.Chains["grok"].Events[0]
	secondClaim := *first.Claim
	secondClaim.CreatedAt = secondClaim.CreatedAt.Add(time.Second)
	secondClaimOID := strings.Repeat("c", 40)
	secondClaim.ReclaimOf = &first.OID
	operator := "operator"
	secondClaim.ReclaimReason = &operator
	second := &protocol.Event{
		OID: secondClaimOID, Type: "CLAIM", ChainSeat: "grok", Claim: &secondClaim,
	}
	reportBlob, _, err := repo.BlobAt(context.Background(), snapshot.MainCommit, "memory/review.md")
	if err != nil {
		t.Fatal(err)
	}
	verdict := &protocol.Event{
		OID: strings.Repeat("d", 40), Type: "VERDICT", ChainSeat: "grok",
		Verdict: &protocol.Verdict{
			SchemaVersion: 1, Actor: protocol.Actor{Seat: "grok", Machine: "m"},
			CreatedAt: secondClaim.CreatedAt.Add(time.Second), Obligation: state.Obligation,
			Claim: secondClaimOID, PR: 1, Revision: *secondClaim.RevisionAtClaim,
			Decision: "REQUEST_CHANGES",
			Report: protocol.Source{
				Commit: snapshot.MainCommit, Path: "memory/review.md", Blob: reportBlob,
			},
			Scope: protocol.ReviewScope{Kind: "full"}, Completeness: "COMPLETE",
		},
	}
	thirdClaim := secondClaim
	thirdClaim.CreatedAt = secondClaim.CreatedAt.Add(2 * time.Second)
	thirdClaimOID := strings.Repeat("f", 40)
	thirdClaim.ReclaimOf = &secondClaimOID
	third := &protocol.Event{
		OID: thirdClaimOID, Type: "CLAIM", ChainSeat: "grok", Claim: &thirdClaim,
	}
	snapshot.Chains["grok"].Events = append(snapshot.Chains["grok"].Events, second, verdict, third)
	if err := facts.PopulateIndexes(context.Background(), repo, snapshot); err != nil {
		t.Fatal(err)
	}
	result := Engine{Repo: repo, Now: time.Now, SkipMergeAudit: true}.Derive(context.Background(), snapshot, "grok")
	got, ok := result.Find(state.Obligation.ID)
	if !ok || got.Claim == nil || got.Claim.OID != thirdClaimOID ||
		got.Claim.Streak != 1 || got.Claim.Escalated {
		t.Fatalf("completion did not open a new damping window: %+v problems=%+v", got, result.Problems)
	}
	if !result.Complete {
		t.Fatalf("valid completion/reclaim sequence was rejected: %+v", result.Problems)
	}
}

func deriveFixture(t *testing.T) (*ObligationState, *facts.Snapshot, *gitx.Repo) {
	t.Helper()
	repo := initDeriveRepo(t)
	base, ok, err := repo.Resolve(context.Background(), "HEAD")
	if err != nil || !ok {
		t.Fatalf("resolve fixture base: %s %v %v", base, ok, err)
	}
	blob, _, err := repo.BlobAt(context.Background(), base, ".hctl/assignments/demo.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Output(context.Background(), "commit", "--allow-empty", "-q", "-m", "head"); err != nil {
		t.Fatal(err)
	}
	head, ok, err := repo.Resolve(context.Background(), "HEAD")
	if err != nil || !ok {
		t.Fatalf("resolve fixture head: %s %v %v", head, ok, err)
	}
	source := protocol.Source{
		Commit: base, Path: ".hctl/assignments/demo.toml", Blob: blob,
	}
	assignment := config.AssignmentRecord{
		Assignment: config.Assignment{
			SchemaVersion: 1, ID: "demo", Kind: "change", BaseRef: "refs/heads/main",
			Author: config.Author{Seat: "codex", BranchSlug: "demo", ClaimTimeoutSeconds: 3600},
			Gates: []config.Gate{{
				ID: "review", Mode: "required", Threshold: "P1", ClaimTimeoutSeconds: 3600,
				Requirement: config.QuorumNode{Seat: "grok"},
				OnTimeout:   config.TimeoutRule{Action: "escalate"},
			}},
			Merge: config.Merge{Method: "squash"},
		},
		Source: source,
	}
	seats := config.Seats{
		SchemaVersion: 1, Kernel: "hctl@bootstrap", Enforcement: "bootstrap", MergeCoordinator: "claude",
		Seats: map[string]config.Seat{"codex": {}, "grok": {}, "claude": {}},
	}
	states, err := deriveStaticObligations(assignment, seats)
	if err != nil {
		t.Fatal(err)
	}
	var gate *ObligationState
	for _, candidate := range states {
		if candidate.Kind == "gate" {
			gate = candidate
		}
	}
	claim := &protocol.Claim{
		SchemaVersion: 1, Actor: protocol.Actor{Seat: "grok", Machine: "m"},
		CreatedAt: time.Now(), Obligation: gate.Obligation,
		RevisionAtClaim: &protocol.Revision{Base: base, Head: head},
	}
	event := &protocol.Event{OID: strings.Repeat("e", 40), Type: "CLAIM", ChainSeat: "grok", Claim: claim}
	snapshot := &facts.Snapshot{
		ObservedAt: time.Now(), MainCommit: base,
		Seats:       config.SeatsRecord{Config: seats, Source: source},
		Assignments: []config.AssignmentRecord{assignment},
		BranchTips:  map[string]string{"refs/heads/main": base, assignment.BranchRef(): head},
		PullTips:    map[int]string{1: head}, FactTips: map[string]string{"grok": event.OID},
		Chains: map[string]*facts.Chain{"grok": {Seat: "grok", Tip: event.OID, Events: []*protocol.Event{event}}},
	}
	if err := facts.PopulateIndexes(context.Background(), repo, snapshot); err != nil {
		t.Fatal(err)
	}
	result := Engine{Repo: repo, Now: time.Now, SkipMergeAudit: true}.Derive(context.Background(), snapshot, "grok")
	got, ok := result.Find(gate.Obligation.ID)
	if !ok {
		t.Fatal("gate not derived")
	}
	return got, snapshot, repo
}
