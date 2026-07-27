package identity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/yesme/hctl/internal/config"
	"github.com/yesme/hctl/internal/gitx"
)

// Hard attribution policy violations — doctor must report error under any
// enforcement (bootstrap warning is only for missing chain, not conflicts).
var (
	ErrEmailOwnedByOtherSeat = errors.New("coauthor email owned by another seat")
	ErrLocalSeatMismatch     = errors.New("local attribution seat mismatch")
)

// LocalRelPath is seat attribution state under the per-worktree git dir
// (equivalent to `git rev-parse --git-path hctl/local.toml`). It is never
// shared across linked worktrees and never part of main history.
const LocalRelPath = "hctl/local.toml"

// LocalConfig is per-worktree seat attribution state written by `hctl seat init`.
type LocalConfig struct {
	Seat          string `toml:"seat"`
	CoauthorEmail string `toml:"coauthor_email"`
	MachineAlias  string `toml:"machine_alias"`
}

func LocalConfigPath(repo *gitx.Repo) string {
	return filepath.Join(repo.GitDir, LocalRelPath)
}

func LoadLocal(repo *gitx.Repo) (LocalConfig, error) {
	path := LocalConfigPath(repo)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return LocalConfig{}, nil
		}
		return LocalConfig{}, fmt.Errorf("read local attribution: %w", err)
	}
	var value LocalConfig
	meta, err := toml.Decode(string(raw), &value)
	if err != nil {
		return LocalConfig{}, fmt.Errorf("decode local attribution: %w", err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return LocalConfig{}, fmt.Errorf("unknown local attribution keys: %v", undecoded)
	}
	return value, nil
}

func SaveLocal(repo *gitx.Repo, value LocalConfig) error {
	if err := validateLocal(value); err != nil {
		return err
	}
	path := LocalConfigPath(repo)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create local attribution dir: %w", err)
	}
	var builder strings.Builder
	if err := toml.NewEncoder(&builder).Encode(value); err != nil {
		return fmt.Errorf("encode local attribution: %w", err)
	}
	return gitx.AtomicWriteFile(path, []byte(builder.String()), 0o600)
}

func validateLocal(value LocalConfig) error {
	if value.Seat == "" {
		return fmt.Errorf("local seat is empty")
	}
	if value.CoauthorEmail != "" {
		if err := ValidateCoauthorEmail(value.CoauthorEmail); err != nil {
			return err
		}
	}
	if strings.ContainsAny(value.MachineAlias, "\r\n") {
		return fmt.Errorf("machine_alias contains a newline")
	}
	return nil
}

// ValidateCoauthorEmail is the shared fail-closed shape for bot emails.
func ValidateCoauthorEmail(email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return fmt.Errorf("coauthor_email is empty")
	}
	if strings.ContainsAny(email, "\r\n<>") {
		return fmt.Errorf("coauthor_email contains unsafe characters")
	}
	if !strings.Contains(email, "@") {
		return fmt.Errorf("coauthor_email must contain @")
	}
	return nil
}

// RejectEmailOwnedByOtherSeat is the single ownership guard for seat↔known-bot.
// Custom emails that are not configured for any other seat remain allowed.
// Own-seat table email is always allowed.
func RejectEmailOwnedByOtherSeat(seat, email string, seats config.Seats) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil
	}
	for id, cfg := range seats.Seats {
		if id == seat {
			continue
		}
		other := strings.TrimSpace(cfg.CoauthorEmail)
		if other != "" && strings.EqualFold(other, email) {
			return fmt.Errorf(
				"%w: coauthor_email %q is configured for seat %q, not current seat %q",
				ErrEmailOwnedByOtherSeat, email, id, seat,
			)
		}
	}
	return nil
}

// IsHardAttributionConflict reports policy violations that must never soft-warn.
func IsHardAttributionConflict(err error) bool {
	return errors.Is(err, ErrEmailOwnedByOtherSeat) || errors.Is(err, ErrLocalSeatMismatch)
}
