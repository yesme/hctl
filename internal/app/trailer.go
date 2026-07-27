package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/yesme/hctl/internal/gitx"
	"github.com/yesme/hctl/internal/identity"
)

func (a *App) commandTrailer(ctx context.Context, options globalOptions, args []string) int {
	if len(args) != 0 {
		return a.fail(ExitUsage, fmt.Errorf("usage: hctl trailer"))
	}
	model := strings.TrimSpace(os.Getenv("HCTL_MODEL_DISPLAY"))
	effort := strings.TrimSpace(os.Getenv("HCTL_REASONING_EFFORT"))
	if model == "" || effort == "" {
		return a.fail(ExitGuard, fmt.Errorf(
			"runtime attribution unavailable; set HCTL_MODEL_DISPLAY and HCTL_REASONING_EFFORT (D-23 fails closed; session adapters are P2)",
		))
	}
	if strings.ContainsAny(model+effort, "\r\n<>") {
		return a.fail(ExitGuard, fmt.Errorf("runtime attribution contains unsafe trailer characters"))
	}

	repo, err := gitx.Open(ctx, options.RepoDir)
	if err != nil {
		return a.fail(ExitGuard, err)
	}
	// Offline-local only: no Observer / ls-remote (codex-pr65#P1-04).
	attr, err := a.loadAttributionLocal(ctx, repo, options)
	if err != nil {
		return a.fail(ExitGuard, err)
	}
	local, err := identity.LoadLocal(repo)
	if err != nil {
		return a.fail(ExitGuard, err)
	}
	email, source, err := ResolveCoauthorEmail(attr.Seat, attr.Seats, local)
	if err != nil {
		return a.fail(ExitGuard, err)
	}
	fmt.Fprintf(a.Stdout, "Co-authored-by: %s (%s effort) <%s>\n", model, effort, email)
	fmt.Fprintf(a.Stderr, "hctl trailer: email_source=%s seat=%s\n", source, attr.Seat)
	return ExitOK
}
