package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yesme/hctl/internal/config"
	"github.com/yesme/hctl/internal/gitx"
	"github.com/yesme/hctl/internal/protocol"
)

func DetectSeat(ctx context.Context, repo *gitx.Repo, seats config.Seats, explicit string) (string, error) {
	if explicit == "" {
		explicit = os.Getenv("HCTL_SEAT")
	}
	if explicit != "" {
		if _, ok := seats.Seats[explicit]; !ok {
			return "", fmt.Errorf("seat %q is not configured", explicit)
		}
		return explicit, nil
	}
	if branch, ok, err := repo.CurrentBranch(ctx); err != nil {
		return "", err
	} else if ok {
		full := "refs/heads/" + branch
		var matches []string
		for id, seat := range seats.Seats {
			if config.MatchesAny(full, seat.AuthorBranchPatterns) || config.MatchesAny(full, seat.MemoBranchPatterns) {
				matches = append(matches, id)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return "", fmt.Errorf("current branch %q matches multiple seats: %s", full, strings.Join(matches, ", "))
		}
	}
	base := filepath.Base(repo.Root)
	var matches []string
	for id := range seats.Seats {
		if strings.HasSuffix(base, "-"+id) || base == id {
			matches = append(matches, id)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return "", fmt.Errorf("cannot derive seat from branch or worktree; set HCTL_SEAT")
}

func Actor(repo *gitx.Repo, seat, explicitMachine, explicitSession string) (protocol.Actor, error) {
	machine := explicitMachine
	if machine == "" {
		machine = os.Getenv("HCTL_MACHINE")
	}
	if machine == "" {
		path := filepath.Join(repo.CommonDir, "hctl", "machine")
		raw, err := os.ReadFile(path)
		switch {
		case err == nil:
			machine = strings.TrimSpace(string(raw))
		case os.IsNotExist(err):
			generated, err := uuidV4()
			if err != nil {
				return protocol.Actor{}, err
			}
			if err := gitx.AtomicWriteFile(path, []byte(generated+"\n"), 0o600); err != nil {
				return protocol.Actor{}, fmt.Errorf("persist machine id: %w", err)
			}
			machine = generated
		default:
			return protocol.Actor{}, fmt.Errorf("read machine id: %w", err)
		}
	}
	session := explicitSession
	if session == "" {
		session = os.Getenv("HCTL_SESSION")
	}
	var sessionPointer *string
	if session != "" {
		sessionPointer = &session
	}
	actor := protocol.Actor{Seat: seat, Machine: machine, Session: sessionPointer}
	if err := protocol.ValidateActor(actor); err != nil {
		return protocol.Actor{}, err
	}
	return actor, nil
}

func uuidV4() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		hex.EncodeToString(data[0:4]),
		hex.EncodeToString(data[4:6]),
		hex.EncodeToString(data[6:8]),
		hex.EncodeToString(data[8:10]),
		hex.EncodeToString(data[10:16]),
	), nil
}
