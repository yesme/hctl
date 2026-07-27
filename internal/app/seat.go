package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/yesme/hctl/internal/config"
	"github.com/yesme/hctl/internal/gitx"
	"github.com/yesme/hctl/internal/identity"
)

func (a *App) commandSeat(ctx context.Context, options globalOptions, args []string) int {
	if len(args) == 0 {
		return a.fail(ExitUsage, fmt.Errorf("usage: hctl seat init [--email <addr>] [--machine-alias <name>]"))
	}
	switch args[0] {
	case "init":
		return a.commandSeatInit(ctx, options, args[1:])
	default:
		return a.fail(ExitUsage, fmt.Errorf("unknown seat subcommand %q; use hctl seat init", args[0]))
	}
}

func (a *App) commandSeatInit(ctx context.Context, options globalOptions, args []string) int {
	emailFlag := ""
	aliasFlag := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--email":
			if i+1 >= len(args) {
				return a.fail(ExitUsage, fmt.Errorf("--email requires a value"))
			}
			emailFlag = args[i+1]
			i++
		case "--machine-alias":
			if i+1 >= len(args) {
				return a.fail(ExitUsage, fmt.Errorf("--machine-alias requires a value"))
			}
			aliasFlag = args[i+1]
			i++
		default:
			return a.fail(ExitUsage, fmt.Errorf("unknown seat init option %q", args[i]))
		}
	}

	repo, err := gitx.Open(ctx, options.RepoDir)
	if err != nil {
		return a.fail(ExitGuard, err)
	}
	// Attribution helpers stay offline-local (no ref-plane Observer / ls-remote).
	attr, err := a.loadAttributionLocal(ctx, repo, options)
	if err != nil {
		return a.fail(ExitGuard, err)
	}
	seat := attr.Seat
	seatCfg, ok := attr.Seats.Seats[seat]
	if !ok {
		return a.fail(ExitGuard, fmt.Errorf("seat %q is not configured", seat))
	}

	existing, err := identity.LoadLocal(repo)
	if err != nil {
		return a.fail(ExitGuard, err)
	}
	email := strings.TrimSpace(emailFlag)
	// Never inherit another seat's persisted email (even if a path bug shared files).
	if email == "" && existing.Seat == seat {
		email = strings.TrimSpace(existing.CoauthorEmail)
	}
	if email == "" {
		email = strings.TrimSpace(seatCfg.CoauthorEmail)
	}
	if email == "" {
		return a.fail(ExitGuard, fmt.Errorf(
			"no coauthor_email for seat %q: pass --email or set seats.toml coauthor_email",
			seat,
		))
	}
	if err := identity.ValidateCoauthorEmail(email); err != nil {
		return a.fail(ExitGuard, err)
	}
	if err := identity.RejectEmailOwnedByOtherSeat(seat, email, attr.Seats); err != nil {
		return a.fail(ExitGuard, err)
	}
	alias := strings.TrimSpace(aliasFlag)
	if alias == "" && existing.Seat == seat {
		alias = existing.MachineAlias
	}

	local := identity.LocalConfig{
		Seat:          seat,
		CoauthorEmail: email,
		MachineAlias:  alias,
	}
	if err := identity.SaveLocal(repo, local); err != nil {
		return a.fail(ExitGuard, err)
	}
	fmt.Fprintf(a.Stdout,
		"seat init ok\nseat=%s\ncoauthor_email=%s\nmachine_alias=%s\npath=%s\nnext=export HCTL_MODEL_DISPLAY and HCTL_REASONING_EFFORT for hctl trailer\n",
		local.Seat, local.CoauthorEmail, valueOr(local.MachineAlias, "<none>"), identity.LocalConfigPath(repo),
	)
	return ExitOK
}

// attributionLocal is the offline seat+seats view used by seat init and trailer.
// It never touches the remote ref plane.
type attributionLocal struct {
	Seat  string
	Seats config.Seats
}

func (a *App) loadAttributionLocal(ctx context.Context, repo *gitx.Repo, options globalOptions) (*attributionLocal, error) {
	head, err := repo.Output(ctx, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("resolve local HEAD for attribution: %w", err)
	}
	head = strings.TrimSpace(head)
	seatsRecord, _, err := config.LoadAt(ctx, repo, head)
	if err != nil {
		return nil, fmt.Errorf("load local seats for attribution: %w", err)
	}
	if seatsRecord.Config.SchemaVersion != 1 {
		return nil, fmt.Errorf("seats configuration is unavailable")
	}
	seat, err := identity.DetectSeat(ctx, repo, seatsRecord.Config, options.Seat)
	if err != nil {
		return nil, err
	}
	return &attributionLocal{Seat: seat, Seats: seatsRecord.Config}, nil
}

// ResolveCoauthorEmail returns env override, then local.toml, then seats.toml.
// Local seat mismatch is checked before env precedence (cannot bypass via env).
// Seat↔known-bot ownership is enforced on every resolved email (P1-03 shared guard).
func ResolveCoauthorEmail(seat string, seats config.Seats, local identity.LocalConfig) (string, string, error) {
	if local.Seat != "" && local.Seat != seat {
		return "", "", fmt.Errorf(
			"%w: local attribution seat %q does not match current seat %q; re-run hctl seat init",
			identity.ErrLocalSeatMismatch, local.Seat, seat,
		)
	}
	if seat == "" {
		return "", "", fmt.Errorf("seat is unknown; pass --seat or run hctl seat init")
	}
	if _, ok := seats.Seats[seat]; !ok {
		return "", "", fmt.Errorf("seat %q is not configured", seat)
	}

	var email, source string
	if env := strings.TrimSpace(os.Getenv("HCTL_COAUTHOR_EMAIL")); env != "" {
		if err := identity.ValidateCoauthorEmail(env); err != nil {
			return "", "", fmt.Errorf("HCTL_COAUTHOR_EMAIL: %w", err)
		}
		email, source = env, "env"
	} else if local.CoauthorEmail != "" {
		if err := identity.ValidateCoauthorEmail(local.CoauthorEmail); err != nil {
			return "", "", fmt.Errorf("local coauthor_email: %w", err)
		}
		email, source = local.CoauthorEmail, "local"
	} else {
		cfg := seats.Seats[seat]
		if strings.TrimSpace(cfg.CoauthorEmail) == "" {
			return "", "", fmt.Errorf("seat %q has no coauthor_email in seats.toml", seat)
		}
		if err := identity.ValidateCoauthorEmail(cfg.CoauthorEmail); err != nil {
			return "", "", fmt.Errorf("seats.toml coauthor_email: %w", err)
		}
		email, source = cfg.CoauthorEmail, "seats"
	}
	if err := identity.RejectEmailOwnedByOtherSeat(seat, email, seats); err != nil {
		return "", "", err
	}
	return email, source, nil
}
