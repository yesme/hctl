package derive

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yesme/hctl/internal/config"
	"github.com/yesme/hctl/internal/facts"
	"github.com/yesme/hctl/internal/gitx"
	"github.com/yesme/hctl/internal/protocol"
)

func TestMemoBaseEquivalentCarriesOriginalVerdictEvidence(t *testing.T) {
	result, gate := deriveCarryFixture(t, carryFixtureOptions{})
	if !result.Complete {
		t.Fatalf("carry fixture is incomplete: %+v", result.Problems)
	}
	if gate.Carried == nil || gate.Carried.Kind != memoBaseCarryKind {
		t.Fatalf("equivalent candidate did not carry: %+v", gate)
	}
	if gate.Carried.Verdict != strings.Repeat("b", 40) ||
		gate.Carried.Report.Path != "memory/review.md" ||
		gate.Claim == nil ||
		gate.Claim.Revision == nil ||
		gate.Carried.Revision != *gate.Claim.Revision {
		t.Fatalf("original evidence was not retained: %+v", gate.Carried)
	}
	if !gate.Green || gate.State != "satisfied" || gate.Verdict == nil ||
		!gate.Verdict.Valid || !gate.Verdict.Covers {
		t.Fatalf("carried green verdict did not satisfy the gate: %+v", gate)
	}
	if gate.Claim == nil || !gate.Claim.Stale {
		t.Fatalf("carry must not rewrite the old claim as exact-current: %+v", gate.Claim)
	}
	var merge *ObligationState
	for _, state := range result.Obligations {
		if state.Assignment == "demo" && state.Kind == "merge" {
			merge = state
			break
		}
	}
	if merge == nil || !merge.Green || merge.State != "claimable" {
		t.Fatalf("carried verdict did not reach merge quorum: %+v", merge)
	}
}

func TestMemoBaseEquivalentFailsClosed(t *testing.T) {
	tests := []struct {
		name           string
		options        carryFixtureOptions
		wantIncomplete bool
	}{
		{
			name: "whitespace-delta",
			options: carryFixtureOptions{
				currentContent: "    if ready:\n",
			},
		},
		{
			name: "commit-message",
			options: carryFixtureOptions{
				currentMessage: "candidate changed\n",
			},
		},
		{
			name: "file-mode",
			options: carryFixtureOptions{
				currentMode: 0o755,
			},
		},
		{
			name: "author-identity",
			options: carryFixtureOptions{
				currentAuthor: "different-author",
			},
		},
		{
			name: "non-linear-candidate",
			options: carryFixtureOptions{
				nonLinear: true,
			},
		},
		{
			name: "source-base-advance",
			options: carryFixtureOptions{
				basePath: "README.md",
			},
		},
		{
			name: "request-changes",
			options: carryFixtureOptions{
				decision: "REQUEST_CHANGES",
			},
		},
		{
			name: "incomplete",
			options: carryFixtureOptions{
				completeness: "INCOMPLETE",
			},
		},
		{
			name: "newer-verdict",
			options: carryFixtureOptions{
				newerDecision: "REQUEST_CHANGES",
			},
		},
		{
			name: "different-pr",
			options: carryFixtureOptions{
				currentPR: 2,
			},
			wantIncomplete: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, gate := deriveCarryFixture(t, test.options)
			if !result.Complete && !test.wantIncomplete {
				t.Fatalf("negative fixture is incomplete: %+v", result.Problems)
			}
			if gate.Carried != nil || gate.Green || gate.State == "satisfied" {
				t.Fatalf("unsafe carry accepted: %+v", gate)
			}
		})
	}
}

type carryFixtureOptions struct {
	currentContent string
	currentMessage string
	basePath       string
	decision       string
	completeness   string
	newerDecision  string
	currentPR      int
	currentMode    os.FileMode
	currentAuthor  string
	nonLinear      bool
}

func deriveCarryFixture(
	t *testing.T,
	options carryFixtureOptions,
) (*Result, *ObligationState) {
	t.Helper()
	ctx := context.Background()
	repo := initDeriveRepo(t)
	base := mustResolve(t, repo, "HEAD")
	assignmentBlob, _, err := repo.BlobAt(ctx, base, ".hctl/assignments/demo.toml")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Output(ctx, "switch", "--detach", "--quiet", base); err != nil {
		t.Fatal(err)
	}
	writeAndCommit(t, repo, "candidate.py", "if ready:\n", "candidate\n", 0o644)
	oldHead := mustResolve(t, repo, "HEAD")

	if _, err := repo.Output(ctx, "switch", "--detach", "--quiet", base); err != nil {
		t.Fatal(err)
	}
	basePath := options.basePath
	if basePath == "" {
		basePath = "memory/round.md"
	}
	writeAndCommit(t, repo, basePath, "gate memo\n", "advance base\n", 0o644)
	currentBase := mustResolve(t, repo, "HEAD")

	currentContent := options.currentContent
	currentMessage := options.currentMessage
	if currentContent == "" {
		currentContent = "if ready:\n"
	}
	if currentMessage == "" {
		currentMessage = "candidate\n"
	}
	if currentContent == "if ready:\n" &&
		currentMessage == "candidate\n" &&
		options.currentMode == 0 &&
		options.currentAuthor == "" {
		if _, err := repo.Output(ctx, "cherry-pick", "--quiet", oldHead); err != nil {
			t.Fatal(err)
		}
	} else {
		if options.currentAuthor != "" {
			if _, err := repo.Output(ctx, "config", "user.name", options.currentAuthor); err != nil {
				t.Fatal(err)
			}
		}
		mode := options.currentMode
		if mode == 0 {
			mode = 0o644
		}
		writeAndCommit(t, repo, "candidate.py", currentContent, currentMessage, mode)
	}
	currentHead := mustResolve(t, repo, "HEAD")
	if options.nonLinear {
		baseTree, err := repo.Output(ctx, "rev-parse", currentBase+"^{tree}")
		if err != nil {
			t.Fatal(err)
		}
		side, err := repo.Output(
			ctx, "commit-tree", strings.TrimSpace(baseTree),
			"-p", currentBase, "-m", "side parent",
		)
		if err != nil {
			t.Fatal(err)
		}
		currentTree, err := repo.Output(ctx, "rev-parse", currentHead+"^{tree}")
		if err != nil {
			t.Fatal(err)
		}
		merged, err := repo.Output(
			ctx, "commit-tree", strings.TrimSpace(currentTree),
			"-p", currentHead, "-p", strings.TrimSpace(side), "-m", "merge candidate",
		)
		if err != nil {
			t.Fatal(err)
		}
		currentHead = strings.TrimSpace(merged)
	}

	source := protocol.Source{
		Commit: base, Path: ".hctl/assignments/demo.toml", Blob: assignmentBlob,
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
		SchemaVersion: 1, Kernel: "hctl@bootstrap", Enforcement: "bootstrap",
		MergeCoordinator: "claude",
		Seats:            map[string]config.Seat{"codex": {}, "grok": {}, "claude": {}},
	}
	states, err := deriveStaticObligations(assignment, seats)
	if err != nil {
		t.Fatal(err)
	}
	var gate *ObligationState
	for _, state := range states {
		if state.Kind == "gate" {
			gate = state
			break
		}
	}
	if gate == nil {
		t.Fatal("gate obligation is missing")
	}

	claimOID := strings.Repeat("a", 40)
	oldRevision := protocol.Revision{Base: base, Head: oldHead}
	createdAt := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	claim := &protocol.Event{
		OID: claimOID, Type: "CLAIM", ChainSeat: "grok",
		Claim: &protocol.Claim{
			SchemaVersion: 1, Actor: protocol.Actor{Seat: "grok", Machine: "test"},
			CreatedAt: createdAt, Obligation: gate.Obligation, RevisionAtClaim: &oldRevision,
		},
	}
	reportBlob, _, err := repo.BlobAt(ctx, base, "memory/review.md")
	if err != nil {
		t.Fatal(err)
	}
	decision := options.decision
	if decision == "" {
		decision = "APPROVE"
	}
	completeness := options.completeness
	if completeness == "" {
		completeness = "COMPLETE"
	}
	verdict := &protocol.Event{
		OID: strings.Repeat("b", 40), Type: "VERDICT", ChainSeat: "grok",
		Verdict: &protocol.Verdict{
			SchemaVersion: 1, Actor: protocol.Actor{Seat: "grok", Machine: "test"},
			CreatedAt: createdAt.Add(time.Minute), Obligation: gate.Obligation,
			Claim: claimOID, PR: 1, Revision: oldRevision, Decision: decision,
			Report: protocol.Source{Commit: base, Path: "memory/review.md", Blob: reportBlob},
			Scope:  protocol.ReviewScope{Kind: "full"}, Completeness: completeness,
			IncompleteReason: incompleteReason(completeness),
		},
	}
	events := []*protocol.Event{claim, verdict}
	if options.newerDecision != "" {
		newer := *verdict.Verdict
		newer.CreatedAt = newer.CreatedAt.Add(time.Minute)
		newer.Decision = options.newerDecision
		events = append(events, &protocol.Event{
			OID: strings.Repeat("c", 40), Type: "VERDICT", ChainSeat: "grok", Verdict: &newer,
		})
	}
	currentPR := options.currentPR
	if currentPR == 0 {
		currentPR = 1
	}
	snapshot := &facts.Snapshot{
		ObservedAt: time.Now(), MainCommit: currentBase,
		Seats:       config.SeatsRecord{Config: seats, Source: source},
		Assignments: []config.AssignmentRecord{assignment},
		BranchTips: map[string]string{
			"refs/heads/main": currentBase, assignment.BranchRef(): currentHead,
		},
		PullTips: map[int]string{currentPR: currentHead},
		FactTips: map[string]string{"grok": events[len(events)-1].OID},
		Chains: map[string]*facts.Chain{
			"grok": {Seat: "grok", Tip: events[len(events)-1].OID, Events: events},
		},
	}
	if err := facts.PopulateIndexes(ctx, repo, snapshot); err != nil {
		t.Fatal(err)
	}
	result := Engine{Repo: repo, Now: time.Now, SkipMergeAudit: true}.Derive(ctx, snapshot, "grok")
	derived, ok := result.Find(gate.Obligation.ID)
	if !ok {
		t.Fatal("gate was not derived")
	}
	return result, derived
}

func writeAndCommit(
	t *testing.T,
	repo *gitx.Repo,
	path, contents, message string,
	mode os.FileMode,
) {
	t.Helper()
	fullPath := filepath.Join(repo.Root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Output(context.Background(), "add", "--", path); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Output(context.Background(), "commit", "--quiet", "-m", message); err != nil {
		t.Fatal(err)
	}
}

func mustResolve(t *testing.T, repo *gitx.Repo, ref string) string {
	t.Helper()
	oid, ok, err := repo.Resolve(context.Background(), ref)
	if err != nil || !ok {
		t.Fatalf("resolve %s: oid=%s ok=%t err=%v", ref, oid, ok, err)
	}
	return oid
}

func incompleteReason(completeness string) string {
	if completeness == "INCOMPLETE" {
		return "not all paths reviewed"
	}
	return ""
}
