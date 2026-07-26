package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/yesme/hctl/internal/derive"
	"github.com/yesme/hctl/internal/receipt"
)

func (a *App) commandMerge(ctx context.Context, load *loaded, options globalOptions, args []string) int {
	parsed, err := parseArgs(args, nil, map[string]bool{"--check": true})
	if err != nil {
		return a.fail(ExitUsage, err)
	}
	value, err := requireOne(parsed.Positionals, "usage: hctl merge <pr> [--check]")
	if err != nil {
		return a.fail(ExitUsage, err)
	}
	pr, err := parsePositiveInt(value, "pr")
	if err != nil {
		return a.fail(ExitUsage, err)
	}
	if load.SeatErr != nil {
		return a.fail(ExitGuard, load.SeatErr)
	}
	if err := load.Result.WriteGuard(); err != nil {
		return a.fail(ExitGuard, err)
	}
	if load.Seat != load.Snapshot.Seats.Config.MergeCoordinator {
		return a.fail(ExitGuard, fmt.Errorf("merge coordinator is %s, current seat is %s", load.Snapshot.Seats.Config.MergeCoordinator, load.Seat))
	}
	state, err := mergeStateForPR(load.Result, pr)
	if err != nil {
		return a.fail(ExitGuard, err)
	}
	if state.Completed {
		fmt.Fprintf(a.Stdout,
			"merge already-completed\nassignment=%s\nobligation=%s\nholder=%s\nmerge_commit=%s\nbase=%s\nhead=%s\nnext=hctl status\n",
			state.Assignment, state.Obligation.ID, state.Holder, state.MergedCommit,
			state.Revision.Base, state.Revision.Head)
		return ExitOK
	}
	if state.Canceled || state.AuditDebt != "" {
		return a.fail(ExitGuard, fmt.Errorf("merge obligation is %s", state.State))
	}
	if state.Claim == nil || state.Claim.Stale || state.Claim.Escalated {
		return a.fail(ExitGuard, fmt.Errorf("merge requires a current active merge claim; obligation=%s state=%s", state.Obligation.ID, state.State))
	}
	if state.Claim.Actor.Seat != load.Seat {
		return a.fail(ExitGuard, fmt.Errorf("merge claim holder %s is not current coordinator %s", state.Claim.Actor.Seat, load.Seat))
	}
	if state.Revision == nil || state.Revision.Base != load.Snapshot.MainCommit {
		return a.fail(ExitGuard, fmt.Errorf("strict base mismatch: claim=%v current-main=%s", state.Revision, load.Snapshot.MainCommit))
	}
	if load.Snapshot.PullTips[pr] != state.Revision.Head {
		return a.fail(ExitGuard, fmt.Errorf("PR %d head is not exact Hctl-Head %s", pr, state.Revision.Head))
	}
	if !state.Green {
		return a.fail(ExitGuard, fmt.Errorf("required quorum is red: %s", derive.RequiredGateSummary(load.Result, state.Assignment)))
	}
	if state.Claim.CoordinatorConfig == nil || *state.Claim.CoordinatorConfig != load.Snapshot.Seats.Source {
		return a.fail(ExitGuard, fmt.Errorf(
			"coordinator config moved: claim=%v current=%v; re-claim under current config",
			state.Claim.CoordinatorConfig, load.Snapshot.Seats.Source,
		))
	}
	if err := checkEnforcedHCTLChange(ctx, load, state); err != nil {
		return a.fail(ExitGuard, err)
	}
	expected := receipt.Receipt{
		Version: 1, Assignment: state.Assignment, Obligation: state.Obligation.ID,
		PR: pr, Base: state.Revision.Base, Head: state.Revision.Head,
		MergeClaim: state.Claim.OID, Method: "squash",
		FactTips: cloneTips(load.Result.FactTips),
	}
	body, err := expected.Body()
	if err != nil {
		return a.fail(ExitGuard, err)
	}
	if parsed.Bools["--check"] {
		fmt.Fprintf(a.Stdout,
			"merge check passed\nassignment=%s\nobligation=%s\nholder=%s\nrevision={base:%s,head:%s}\nrequired_gates=%s\nreceipt:\n%s",
			state.Assignment, state.Obligation.ID, state.Holder,
			state.Revision.Base, state.Revision.Head,
			derive.RequiredGateSummary(load.Result, state.Assignment), body)
		return ExitOK
	}
	if err := runGHMerge(ctx, load, pr, state.Revision.Head, body); err != nil {
		return a.fail(ExitGuard, err)
	}
	after := a.load(ctx, load.Repo, options)
	if after.Snapshot.MainCommit == state.Revision.Base {
		return a.fail(ExitGuard, fmt.Errorf("merge command returned success but main did not advance"))
	}
	mergeCommit, err := findVerifiedMergeCommit(ctx, load, after, expected)
	if err != nil {
		return a.fail(ExitGuard, fmt.Errorf("MERGE_INTEGRITY_INCIDENT: %w", err))
	}
	closed, ok := derive.FindByAssignmentKind(after.Result, state.Assignment, "merge")
	if !ok || !closed.Completed || closed.MergedCommit != mergeCommit {
		detail := "merge obligation did not close under receipt replay"
		if ok && closed.AuditDebt != "" {
			detail = closed.AuditDebt
		}
		return a.fail(ExitGuard, fmt.Errorf("MERGE_INTEGRITY_INCIDENT: %s", detail))
	}
	fmt.Fprintf(a.Stdout,
		"merge verified\nassignment=%s\nobligation=%s\nholder=%s\nmerge_commit=%s\nbase=%s\nhead=%s\nclaim=%s\nnext=hctl status\n",
		state.Assignment, state.Obligation.ID, state.Holder,
		mergeCommit, state.Revision.Base, state.Revision.Head, state.Claim.OID)
	return ExitOK
}

func findVerifiedMergeCommit(
	ctx context.Context,
	before, after *loaded,
	expected receipt.Receipt,
) (string, error) {
	var lastErr error
	for _, entry := range after.Snapshot.MainHistory {
		if entry.OID == expected.Base {
			break
		}
		if !strings.Contains(entry.Message, "Hctl-") {
			continue
		}
		if err := receipt.VerifyCommit(ctx, before.Repo, entry.OID, expected); err == nil {
			return entry.OID, nil
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("no matching receipt commit found after Hctl-Base %s", expected.Base)
}

func mergeStateForPR(result *derive.Result, pr int) (*derive.ObligationState, error) {
	var found *derive.ObligationState
	for _, state := range result.Obligations {
		if state.Kind != "merge" || state.PR != pr {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("PR %d maps to multiple merge obligations", pr)
		}
		found = state
	}
	if found == nil {
		return nil, fmt.Errorf("no merge obligation maps to PR %d; run hctl status", pr)
	}
	return found, nil
}

func checkEnforcedHCTLChange(ctx context.Context, load *loaded, state *derive.ObligationState) error {
	if load.Snapshot.Seats.Config.Enforcement != "active" {
		return nil
	}
	out, err := load.Repo.Output(ctx, "diff", "--name-only", "-z", state.Revision.Base, state.Revision.Head, "--", ".hctl")
	if err != nil {
		return err
	}
	if out == "" {
		return nil
	}
	for _, gate := range state.AssignmentRecord.Assignment.Gates {
		if gate.Mode == "required" {
			return nil
		}
	}
	return fmt.Errorf("active enforcement: .hctl/** change has no required non-author gate")
}

func runGHMerge(ctx context.Context, load *loaded, pr int, head, body string) error {
	path, err := exec.LookPath("gh")
	if err != nil {
		return fmt.Errorf("gh is required for merge: %w", err)
	}
	file, err := os.CreateTemp("", "hctl-receipt-*.txt")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.WriteString(body); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, path,
		"pr", "merge", strconv.Itoa(pr),
		"--squash", "--match-head-commit", head, "--body-file", name,
	)
	cmd.Dir = load.Repo.Root
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1")
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh pr merge failed: %s: %w", strings.TrimSpace(output.String()), err)
	}
	return nil
}

func cloneTips(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for seat, oid := range input {
		output[seat] = oid
	}
	return output
}
