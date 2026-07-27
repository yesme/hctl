package app

import (
	"context"
	"fmt"
	"time"

	"github.com/yesme/hctl/internal/derive"
	"github.com/yesme/hctl/internal/gitx"
)

func (a *App) commandWait(ctx context.Context, repo *gitx.Repo, options globalOptions, args []string) int {
	parsed, err := parseArgs(args,
		map[string]bool{"--timeout": true, "--interval": true},
		map[string]bool{"--json": true},
	)
	if err != nil || len(parsed.Positionals) != 0 {
		if err == nil {
			err = fmt.Errorf("usage: hctl wait [--timeout <duration>] [--interval <duration>] [--json]")
		}
		return a.fail(ExitUsage, err)
	}
	timeout := 5 * time.Minute
	interval := 2 * time.Second
	if value := parsed.Values["--timeout"]; value != "" {
		timeout, err = time.ParseDuration(value)
		if err != nil || timeout <= 0 {
			return a.fail(ExitUsage, fmt.Errorf("--timeout must be a positive Go duration"))
		}
	}
	if value := parsed.Values["--interval"]; value != "" {
		interval, err = time.ParseDuration(value)
		if err != nil || interval < 250*time.Millisecond {
			return a.fail(ExitUsage, fmt.Errorf("--interval must be at least 250ms"))
		}
	}
	initial := a.load(ctx, repo, options)
	if initial.SeatErr != nil {
		return a.fail(ExitGuard, initial.SeatErr)
	}
	if err := initial.Result.WriteGuard(); err != nil {
		return a.fail(ExitGuard, err)
	}
	if attention := initial.Result.Attention(initial.Seat); len(attention) > 0 {
		return a.printAttention(initial, attention, parsed.Bools["--json"])
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return a.fail(ExitGuard, ctx.Err())
		case <-timer.C:
			if parsed.Bools["--json"] {
				_ = writeJSON(a.Stdout, map[string]any{
					"schema_version": 1,
					"event":          "WAIT_TIMEOUT",
					"timeout":        timeout.String(),
					"seat":           initial.Seat,
					"obligations":    []any{},
				})
			} else {
				fmt.Fprintf(a.Stdout, "WAIT_TIMEOUT seat=%s timeout=%s attention=none\n", initial.Seat, timeout)
			}
			return ExitWaitTimeout
		case <-ticker.C:
			current := a.load(ctx, repo, options)
			if current.SeatErr != nil {
				return a.fail(ExitGuard, current.SeatErr)
			}
			if err := current.Result.WriteGuard(); err != nil {
				return a.fail(ExitGuard, err)
			}
			if attention := current.Result.Attention(current.Seat); len(attention) > 0 {
				return a.printAttention(current, attention, parsed.Bools["--json"])
			}
		}
	}
}

func (a *App) printAttention(load *loaded, attention []*derive.ObligationState, jsonOutput bool) int {
	if jsonOutput {
		if err := writeJSON(a.Stdout, map[string]any{
			"schema_version": 1,
			"event":          "ATTENTION",
			"seat":           load.Seat,
			"obligations":    attention,
		}); err != nil {
			return a.fail(ExitGuard, err)
		}
		return ExitOK
	}
	fmt.Fprintf(a.Stdout, "ATTENTION seat=%s count=%d\n", load.Seat, len(attention))
	for _, state := range attention {
		fmt.Fprintf(a.Stdout, "obligation=%s holder=%s state=%s next=%s\n",
			state.Obligation.ID, state.Holder, state.State, state.NextAction)
	}
	return ExitOK
}
