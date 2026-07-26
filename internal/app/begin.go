package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/yesme/hctl/internal/config"
	"github.com/yesme/hctl/internal/derive"
)

func (a *App) commandBegin(ctx context.Context, load *loaded, options globalOptions, args []string) int {
	parsed, err := parseArgs(args, map[string]bool{"--adopt": true}, map[string]bool{"--draft-only": true})
	if err != nil {
		return a.fail(ExitUsage, err)
	}
	id, err := requireOne(parsed.Positionals, "usage: hctl begin <assignment> [--adopt <branch>|--draft-only]")
	if err != nil {
		return a.fail(ExitUsage, err)
	}
	if parsed.Bools["--draft-only"] && parsed.Values["--adopt"] != "" {
		return a.fail(ExitUsage, fmt.Errorf("--adopt and --draft-only are mutually exclusive"))
	}
	record, ok := assignmentByID(load.Snapshot.Assignments, id)
	if !ok {
		return a.fail(ExitGuard, fmt.Errorf("unknown assignment %q", id))
	}
	if load.SeatErr != nil {
		return a.fail(ExitGuard, load.SeatErr)
	}
	if load.Seat != record.Assignment.Author.Seat {
		return a.fail(ExitGuard, fmt.Errorf("assignment %s author is %s, current seat is %s", id, record.Assignment.Author.Seat, load.Seat))
	}
	author, ok := derive.FindByAssignmentKind(load.Result, id, "author")
	if !ok {
		return a.fail(ExitGuard, fmt.Errorf("author obligation for %s was not derived", id))
	}
	branch := strings.TrimPrefix(author.BranchRef, "refs/heads/")
	if !parsed.Bools["--draft-only"] && author.Claim == nil && parsed.Values["--adopt"] == "" {
		_, localExists, resolveErr := load.Repo.Resolve(ctx, author.BranchRef)
		if resolveErr != nil {
			return a.fail(ExitGuard, resolveErr)
		}
		if localExists || load.Snapshot.BranchTips[author.BranchRef] != "" {
			return a.fail(ExitGuard, fmt.Errorf(
				"branch %s already exists without an active claim; rerun with --adopt %s to make adoption explicit",
				author.BranchRef, branch,
			))
		}
	}
	claimOID := ""
	if !parsed.Bools["--draft-only"] {
		claimOID, _, err = a.acquireClaim(ctx, load, options, author, "", true)
		if err != nil {
			return a.fail(ExitGuard, err)
		}
	} else {
		fmt.Fprintf(a.Stderr,
			"WARNING DRAFT_ONLY: assignment=%s obligation=%s holder=%s; no claim was acquired and no remote side effect is permitted\n",
			id, author.Obligation.ID, author.Holder)
	}
	if err := a.prepareBranch(ctx, load, record, branch, parsed.Values["--adopt"]); err != nil {
		if claimOID != "" {
			return a.fail(ExitGuard, fmt.Errorf(
				"branch setup failed after claim acquisition; fencing_token=%s remains active; fix the branch issue and rerun begin: %w",
				claimOID, err,
			))
		}
		return a.fail(ExitGuard, err)
	}
	mode := "formal"
	if parsed.Bools["--draft-only"] {
		mode = "draft-only"
	}
	fmt.Fprintf(a.Stdout,
		"begin %s\nassignment=%s\nobligation=%s\nholder=%s\nclaim=%s\nbranch=refs/heads/%s\nnext=work, commit, push, and open the PR before gates claim\n",
		mode, id, author.Obligation.ID, author.Holder, valueOr(claimOID, "<none>"), branch)
	return ExitOK
}

func (a *App) prepareBranch(ctx context.Context, load *loaded, record config.AssignmentRecord, branch, adopt string) error {
	expectedRef := "refs/heads/" + branch
	expectedOID, expectedExists, err := load.Repo.Resolve(ctx, expectedRef)
	if err != nil {
		return err
	}
	if expectedExists {
		if adopt != "" {
			adoptOID, err := resolveBranchCandidate(ctx, load, adopt)
			if err != nil {
				return err
			}
			if adoptOID != expectedOID {
				return fmt.Errorf("expected branch %s already exists at %s, adopt target is %s", branch, expectedOID, adoptOID)
			}
		}
		return load.Repo.Switch(ctx, branch)
	}
	start := load.Snapshot.BranchTips[record.Assignment.BaseRef]
	if start == "" {
		return fmt.Errorf("base ref %s is unavailable", record.Assignment.BaseRef)
	}
	if adopt != "" {
		start, err = resolveBranchCandidate(ctx, load, adopt)
		if err != nil {
			return err
		}
	} else if published := load.Snapshot.BranchTips[expectedRef]; published != "" {
		// A seat claim is cross-machine state. A fresh clone resumes its own
		// canonical task branch from the observed remote tip.
		start = published
	}
	return load.Repo.CreateAndSwitch(ctx, branch, start)
}

func resolveBranchCandidate(ctx context.Context, load *loaded, branch string) (string, error) {
	short := branch
	if strings.HasPrefix(short, "refs/heads/") {
		short = strings.TrimPrefix(short, "refs/heads/")
	} else if strings.HasPrefix(short, "refs/") {
		return "", fmt.Errorf("adopt target %q is not a local branch", branch)
	}
	if _, err := load.Repo.Output(ctx, "check-ref-format", "--branch", short); err != nil {
		return "", fmt.Errorf("invalid adopt branch %q: %w", branch, err)
	}
	ref := "refs/heads/" + short
	oid, ok, err := load.Repo.Resolve(ctx, ref)
	if err != nil {
		return "", err
	}
	if ok {
		return oid, nil
	}
	if oid = load.Snapshot.BranchTips[ref]; oid != "" {
		return oid, nil
	}
	return "", fmt.Errorf("adopt branch %q does not exist locally or on the observed remote", branch)
}

func assignmentByID(records []config.AssignmentRecord, id string) (config.AssignmentRecord, bool) {
	for _, record := range records {
		if record.Assignment.ID == id {
			return record, true
		}
	}
	return config.AssignmentRecord{}, false
}
