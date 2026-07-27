package config

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/yesme/hctl/internal/gitx"
	"github.com/yesme/hctl/internal/protocol"
)

func DecodeSeats(raw []byte) (Seats, error) {
	var value Seats
	meta, err := toml.Decode(string(raw), &value)
	if err != nil {
		return Seats{}, fmt.Errorf("decode seats TOML: %w", err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return Seats{}, fmt.Errorf("unknown seats TOML keys: %v", undecoded)
	}
	for _, key := range []string{"schema_version", "kernel", "merge_coordinator", "enforcement", "seats"} {
		if !meta.IsDefined(key) {
			return Seats{}, fmt.Errorf("required seats TOML key %q is missing", key)
		}
	}
	for id := range value.Seats {
		for _, key := range []string{
			"harness", "model", "capabilities", "write_scope", "author_concurrency",
			"memo_branch_patterns", "features",
		} {
			if !meta.IsDefined("seats", id, key) {
				return Seats{}, fmt.Errorf("seat %q required key %q is missing", id, key)
			}
		}
	}
	if err := ValidateSeats(value); err != nil {
		return Seats{}, err
	}
	return value, nil
}

func DecodeAssignment(raw []byte) (Assignment, error) {
	var value Assignment
	meta, err := toml.Decode(string(raw), &value)
	if err != nil {
		return Assignment{}, fmt.Errorf("decode assignment TOML: %w", err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return Assignment{}, fmt.Errorf("unknown assignment TOML keys: %v", undecoded)
	}
	for _, key := range []string{"schema_version", "id", "kind", "needs", "base_ref", "author", "gates", "merge"} {
		if !meta.IsDefined(key) {
			return Assignment{}, fmt.Errorf("required assignment TOML key %q is missing", key)
		}
	}
	return value, nil
}

// LoadAt reads all P1 configuration from one immutable Git commit. Blob reads
// share one cat-file --batch process.
func LoadAt(ctx context.Context, repo *gitx.Repo, commit string) (SeatsRecord, []AssignmentRecord, error) {
	entries, err := repo.ListTree(ctx, commit, ".hctl")
	if err != nil {
		return SeatsRecord{}, nil, fmt.Errorf("list .hctl at %s: %w", commit, err)
	}
	byPath := make(map[string]gitx.TreeEntry, len(entries))
	for _, entry := range entries {
		byPath[entry.Path] = entry
	}
	seatsEntry, ok := byPath[SeatsPath]
	if !ok || seatsEntry.Type != "blob" {
		return SeatsRecord{}, nil, fmt.Errorf("%s is missing at %s", SeatsPath, commit)
	}
	batch, err := repo.NewBatch(ctx)
	if err != nil {
		return SeatsRecord{}, nil, err
	}
	defer batch.Close()
	seatsObject, err := batch.Read(seatsEntry.OID)
	if err != nil {
		return SeatsRecord{}, nil, err
	}
	if seatsObject.Type != "blob" {
		return SeatsRecord{}, nil, fmt.Errorf("%s is not a blob", SeatsPath)
	}
	seats, err := DecodeSeats(seatsObject.Data)
	if err != nil {
		return SeatsRecord{}, nil, fmt.Errorf("%s: %w", SeatsPath, err)
	}
	seatsRecord := SeatsRecord{
		Config: seats,
		Source: protocol.Source{Commit: commit, Path: SeatsPath, Blob: seatsEntry.OID},
	}
	var paths []string
	for path, entry := range byPath {
		if strings.HasPrefix(path, ".hctl/assignments/") && strings.HasSuffix(path, ".toml") && entry.Type == "blob" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	records := make([]AssignmentRecord, 0, len(paths))
	for _, path := range paths {
		entry := byPath[path]
		object, err := batch.Read(entry.OID)
		if err != nil {
			return SeatsRecord{}, nil, err
		}
		if object.Type != "blob" {
			return SeatsRecord{}, nil, fmt.Errorf("%s is not a blob", path)
		}
		assignment, err := DecodeAssignment(object.Data)
		if err != nil {
			return SeatsRecord{}, nil, fmt.Errorf("%s: %w", path, err)
		}
		records = append(records, AssignmentRecord{
			Assignment: assignment,
			Source:     protocol.Source{Commit: commit, Path: path, Blob: entry.OID},
		})
	}
	if err := ValidateAssignments(records, seats); err != nil {
		return SeatsRecord{}, nil, err
	}
	return seatsRecord, records, nil
}
