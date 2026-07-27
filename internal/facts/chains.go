package facts

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/yesme/hctl/internal/gitx"
	"github.com/yesme/hctl/internal/protocol"
)

func readChains(ctx context.Context, repo *gitx.Repo, tips map[string]string) (map[string]*Chain, []Problem) {
	chains := make(map[string]*Chain, len(tips))
	if len(tips) == 0 {
		return chains, nil
	}
	format, err := repo.ObjectFormat(ctx)
	if err != nil {
		return chains, []Problem{{Code: "INCOMPLETE_FACTS", Severity: SeverityError, Detail: err.Error()}}
	}
	oidBytes := 20
	if format == "sha256" {
		oidBytes = 32
	}
	batch, err := repo.NewBatch(ctx)
	if err != nil {
		return chains, []Problem{{Code: "INCOMPLETE_FACTS", Severity: SeverityError, Detail: err.Error()}}
	}
	defer batch.Close()

	seats := make([]string, 0, len(tips))
	for seat := range tips {
		seats = append(seats, seat)
	}
	sort.Strings(seats)
	var problems []Problem
	for _, seat := range seats {
		chain := &Chain{Seat: seat, Tip: tips[seat]}
		chains[seat] = chain
		type chainItem struct {
			event       *protocol.Event
			unsupported string
		}
		var reverse []chainItem
		var reverseCommits []string
		seen := map[string]bool{}
		oid := chain.Tip
		for oid != "" {
			if seen[oid] {
				chain.Corrupt = fmt.Errorf("commit cycle at %s", oid)
				break
			}
			seen[oid] = true
			reverseCommits = append(reverseCommits, oid)
			object, err := batch.Read(oid)
			if err != nil {
				chain.Corrupt = err
				break
			}
			if object.Type != "commit" {
				chain.Corrupt = fmt.Errorf("%s is %s, not a commit", oid, object.Type)
				break
			}
			commit, err := gitx.ParseCommit(object.Data)
			if err != nil {
				chain.Corrupt = err
				break
			}
			if len(commit.Parents) > 1 {
				chain.Corrupt = fmt.Errorf("event commit %s has %d parents", oid, len(commit.Parents))
				break
			}
			treeObject, err := batch.Read(commit.Tree)
			if err != nil {
				chain.Corrupt = err
				break
			}
			if treeObject.Type != "tree" {
				chain.Corrupt = fmt.Errorf("%s tree header names a %s", oid, treeObject.Type)
				break
			}
			blobOID, err := gitx.ParseSingleFileTree(treeObject.Data, oidBytes, "event.json")
			if err != nil {
				chain.Corrupt = fmt.Errorf("event commit %s: %w", oid, err)
				break
			}
			blobObject, err := batch.Read(blobOID)
			if err != nil {
				chain.Corrupt = err
				break
			}
			if blobObject.Type != "blob" {
				chain.Corrupt = fmt.Errorf("event.json at %s is %s", oid, blobObject.Type)
				break
			}
			event, err := protocol.ParseEvent(blobObject.Data, seat)
			if err != nil {
				var unsupported *protocol.UnsupportedError
				if errors.As(err, &unsupported) {
					detail := fmt.Sprintf("%s: %s", oid, unsupported.Reason)
					chain.Unsupported = append(chain.Unsupported, detail)
					reverse = append(reverse, chainItem{unsupported: detail})
				} else {
					chain.Corrupt = fmt.Errorf("%s: %w", oid, err)
					break
				}
			} else {
				event.OID = oid
				event.CommitterUnix = commit.CommitterUnix
				if len(commit.Parents) == 1 {
					event.Parent = commit.Parents[0]
				}
				reverse = append(reverse, chainItem{event: event})
				if commit.CommitterUnix != 0 {
					delta := eventTimeDelta(event, commit.CommitterUnix)
					if delta > 5*time.Minute {
						problems = append(problems, Problem{
							Code: "TIMESTAMP_DRIFT", Severity: SeverityWarning, Seat: seat,
							Detail: fmt.Sprintf("event %s created_at differs from commit time by %s", oid, delta.Round(time.Second)),
						})
					}
				}
			}
			if len(commit.Parents) == 0 {
				oid = ""
			} else {
				oid = commit.Parents[0]
			}
		}
		if chain.Corrupt != nil {
			chain.Events = nil
			problems = append(problems, Problem{
				Code: "CORRUPT_CHAIN", Severity: SeverityError, Seat: seat,
				Detail: chain.Corrupt.Error(),
			})
			continue
		}
		for i := len(reverse) - 1; i >= 0; i-- {
			if reverse[i].unsupported != "" {
				// An old reader may safely expose the known prefix, but it must
				// not derive through a future control fact.
				break
			}
			chain.Events = append(chain.Events, reverse[i].event)
		}
		for i := len(reverseCommits) - 1; i >= 0; i-- {
			chain.Commits = append(chain.Commits, reverseCommits[i])
		}
		if len(chain.Unsupported) > 0 {
			problems = append(problems, Problem{
				Code: "UNSUPPORTED_FACTS", Severity: SeverityError, Seat: seat,
				Detail: fmt.Sprintf("%d unsupported event(s): %s", len(chain.Unsupported), chain.Unsupported[0]),
			})
		}
	}
	return chains, problems
}

// ReadChains exposes the batch chain reader to recovery and receipt replay.
// The returned map is all-or-quarantine per seat.
func ReadChains(ctx context.Context, repo *gitx.Repo, tips map[string]string) (map[string]*Chain, []Problem) {
	return readChains(ctx, repo, tips)
}

func eventTimeDelta(event *protocol.Event, commitUnix int64) time.Duration {
	var eventTime time.Time
	switch event.Type {
	case "CLAIM":
		eventTime = event.Claim.CreatedAt
	case "VERDICT":
		eventTime = event.Verdict.CreatedAt
	case "CANCEL":
		eventTime = event.Cancel.CreatedAt
	}
	delta := eventTime.Sub(time.Unix(commitUnix, 0))
	if delta < 0 {
		delta = -delta
	}
	return delta
}
