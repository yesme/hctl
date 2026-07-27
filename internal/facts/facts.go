package facts

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yesme/hctl/internal/config"
	"github.com/yesme/hctl/internal/gitx"
	"github.com/yesme/hctl/internal/protocol"
)

type Severity string

const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type Problem struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	Seat     string   `json:"seat,omitempty"`
	Detail   string   `json:"detail"`
}

type Chain struct {
	Seat        string
	Tip         string
	Commits     []string
	Events      []*protocol.Event
	Unsupported []string
	Corrupt     error
}

type Snapshot struct {
	ObservedAt      time.Time
	Remote          string
	Advertised      map[string]string
	MainCommit      string
	Seats           config.SeatsRecord
	Assignments     []config.AssignmentRecord
	BranchTips      map[string]string
	PullTips        map[int]string
	FactTips        map[string]string
	Chains          map[string]*Chain
	MainHistory     []MainCommit
	Reachable       map[string]bool
	KnownCommits    map[string]bool
	MissingCommits  map[string]bool
	CommitParents   map[string][]string
	MatchingSources map[string]bool
	ValidSources    map[string]bool
	CommitTrees     map[string]string
	Problems        []Problem
}

type MainCommit struct {
	OID     string
	Tree    string
	Parents []string
	Message string
}

func (s *Snapshot) Complete() bool {
	for _, problem := range s.Problems {
		if problem.Severity == SeverityError {
			return false
		}
	}
	return s.MainCommit != "" && s.Seats.Config.SchemaVersion == 1
}

func (s *Snapshot) WriteGuard() error {
	var codes []string
	for _, problem := range s.Problems {
		if problem.Severity == SeverityError {
			codes = append(codes, problem.Code)
		}
	}
	if len(codes) > 0 {
		sort.Strings(codes)
		return fmt.Errorf("%s: writes are disabled until facts are repaired", strings.Join(unique(codes), ","))
	}
	if s.MainCommit == "" {
		return errors.New("INCOMPLETE_FACTS: current main is unavailable")
	}
	return nil
}

type Observer struct {
	Repo   *gitx.Repo
	Remote string
	Now    func() time.Time
}

var remoteNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (o Observer) Observe(ctx context.Context) *Snapshot {
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	snapshot := &Snapshot{
		ObservedAt:   now().UTC(),
		Remote:       o.Remote,
		Advertised:   map[string]string{},
		BranchTips:   map[string]string{},
		PullTips:     map[int]string{},
		FactTips:     map[string]string{},
		Chains:       map[string]*Chain{},
		Reachable:    map[string]bool{},
		ValidSources: map[string]bool{},
		CommitTrees:  map[string]string{},
	}
	if !remoteNamePattern.MatchString(o.Remote) {
		snapshot.Problems = append(snapshot.Problems, Problem{
			Code: "INCOMPLETE_FACTS", Severity: SeverityError,
			Detail: fmt.Sprintf("unsafe remote name %q", o.Remote),
		})
		return snapshot
	}
	advertised, err := lsRemote(ctx, o.Repo, o.Remote)
	if err != nil {
		snapshot.Problems = append(snapshot.Problems, Problem{
			Code: "INCOMPLETE_FACTS", Severity: SeverityError,
			Detail: fmt.Sprintf("cannot observe remote %q: %v", o.Remote, err),
		})
		return snapshot
	}
	snapshot.Advertised = advertised
	mainOnly := map[string]bool{"refs/heads/main": true}
	if err := fetchBranchPlane(ctx, o.Repo, o.Remote, advertised, mainOnly, mainOnly, false); err != nil {
		snapshot.Problems = append(snapshot.Problems, Problem{
			Code: "INCOMPLETE_FACTS", Severity: SeverityError,
			Detail: fmt.Sprintf("cannot fetch main: %v", err),
		})
		return snapshot
	}
	mainOID, exists := advertised["refs/heads/main"]
	if !exists {
		snapshot.Problems = append(snapshot.Problems, Problem{
			Code: "INCOMPLETE_FACTS", Severity: SeverityError,
			Detail: "remote does not advertise refs/heads/main",
		})
		return snapshot
	}
	snapshot.MainCommit = mainOID
	seats, assignments, err := config.LoadAt(ctx, o.Repo, mainOID)
	if err != nil {
		code := "INVALID_CONFIG"
		if strings.Contains(err.Error(), "AMBIGUOUS_ASSIGNMENT") {
			code = "AMBIGUOUS_ASSIGNMENT"
		}
		snapshot.Problems = append(snapshot.Problems, Problem{
			Code: code, Severity: SeverityError, Detail: err.Error(),
		})
		return snapshot
	}
	snapshot.Seats = seats
	snapshot.Assignments = assignments
	relevantHeads := map[string]bool{"refs/heads/main": true}
	forwardOnlyHeads := map[string]bool{"refs/heads/main": true}
	for _, record := range assignments {
		relevantHeads[record.Assignment.BaseRef] = true
		forwardOnlyHeads[record.Assignment.BaseRef] = true
	}
	advertisedRefs := make([]string, 0, len(advertised))
	for ref := range advertised {
		advertisedRefs = append(advertisedRefs, ref)
	}
	sort.Strings(advertisedRefs)
	for _, ref := range advertisedRefs {
		if !strings.HasPrefix(ref, "refs/heads/") {
			continue
		}
		for _, seatID := range sortedSeatIDs(seats.Config.Seats) {
			seat := seats.Config.Seats[seatID]
			if config.MatchesAny(ref, seat.AuthorBranchPatterns) ||
				config.MatchesAny(ref, seat.MemoBranchPatterns) {
				relevantHeads[ref] = true
				break
			}
		}
	}
	if err := fetchBranchPlane(ctx, o.Repo, o.Remote, advertised, relevantHeads, forwardOnlyHeads, true); err != nil {
		snapshot.Problems = append(snapshot.Problems, Problem{
			Code: "INCOMPLETE_FACTS", Severity: SeverityError,
			Detail: fmt.Sprintf("cannot fetch relevant branch/pull plane: %v", err),
		})
		return snapshot
	}
	for ref := range relevantHeads {
		if oid := advertised[ref]; oid != "" {
			snapshot.BranchTips[ref] = oid
		}
	}
	for ref, oid := range advertised {
		if strings.HasPrefix(ref, "refs/pull/") && strings.HasSuffix(ref, "/head") {
			middle := strings.TrimSuffix(strings.TrimPrefix(ref, "refs/pull/"), "/head")
			number, err := strconv.Atoi(middle)
			if err == nil && number > 0 {
				snapshot.PullTips[number] = oid
			}
		}
	}
	for _, record := range assignments {
		if snapshot.BranchTips[record.Assignment.BaseRef] == "" {
			snapshot.Problems = append(snapshot.Problems, Problem{
				Code: "INCOMPLETE_FACTS", Severity: SeverityError,
				Detail: fmt.Sprintf("assignment %q base ref %s is not advertised", record.Assignment.ID, record.Assignment.BaseRef),
			})
		}
	}

	for _, ref := range advertisedRefs {
		if !strings.HasPrefix(ref, "refs/coop/") {
			continue
		}
		seat := strings.TrimPrefix(ref, "refs/coop/")
		if _, known := seats.Config.Seats[seat]; !known {
			snapshot.Problems = append(snapshot.Problems, Problem{
				Code: "UNSUPPORTED_FACTS", Severity: SeverityError, Seat: seat,
				Detail: fmt.Sprintf("remote advertises a chain for unknown seat %q", seat),
			})
		}
	}

	for _, seat := range sortedSeatIDs(seats.Config.Seats) {
		remoteRef := CoopRef(seat)
		advertisedTip := advertised[remoteRef]
		trackingRef := TrackingCoopRef(o.Remote, seat)
		if err := syncTrackingRef(ctx, o.Repo, o.Remote, seat, remoteRef, trackingRef, advertisedTip); err != nil {
			snapshot.Problems = append(snapshot.Problems, Problem{
				Code: "CORRUPT_CHAIN", Severity: SeverityError, Seat: seat, Detail: err.Error(),
			})
			continue
		}
		if advertisedTip != "" {
			snapshot.FactTips[seat] = advertisedTip
		}
	}

	chains, problems := readChains(ctx, o.Repo, snapshot.FactTips)
	snapshot.Chains = chains
	snapshot.Problems = append(snapshot.Problems, problems...)
	history, err := readMainHistory(ctx, o.Repo, snapshot.MainCommit)
	if err != nil {
		snapshot.Problems = append(snapshot.Problems, Problem{
			Code: "INCOMPLETE_FACTS", Severity: SeverityError,
			Detail: fmt.Sprintf("cannot read main first-parent history: %v", err),
		})
	} else {
		snapshot.MainHistory = history
	}
	if err := PopulateIndexes(ctx, o.Repo, snapshot); err != nil {
		snapshot.Problems = append(snapshot.Problems, Problem{
			Code: "INCOMPLETE_FACTS", Severity: SeverityError,
			Detail: fmt.Sprintf("cannot build batch source/object indexes: %v", err),
		})
	} else if len(snapshot.MissingCommits) > 0 {
		if err := fetchMissingCommits(ctx, o.Repo, o.Remote, snapshot.MissingCommits); err != nil {
			snapshot.Problems = append(snapshot.Problems, Problem{
				Code: "INCOMPLETE_FACTS", Severity: SeverityError,
				Detail: fmt.Sprintf("referenced commit objects are unavailable: %v", err),
			})
		} else if err := PopulateIndexes(ctx, o.Repo, snapshot); err != nil {
			snapshot.Problems = append(snapshot.Problems, Problem{
				Code: "INCOMPLETE_FACTS", Severity: SeverityError,
				Detail: fmt.Sprintf("cannot rebuild source/object indexes after object fetch: %v", err),
			})
		}
	}
	for _, seat := range sortedSeatIDs(seats.Config.Seats) {
		publishing, publishingExists, err := o.Repo.Resolve(ctx, CoopRef(seat))
		if err != nil {
			snapshot.Problems = append(snapshot.Problems, Problem{
				Code: "INCOMPLETE_FACTS", Severity: SeverityError, Seat: seat,
				Detail: fmt.Sprintf("read publishing ref: %v", err),
			})
			continue
		}
		if publishingExists && publishing != snapshot.FactTips[seat] {
			snapshot.Problems = append(snapshot.Problems, Problem{
				Code: "PENDING_PUBLICATION", Severity: SeverityWarning, Seat: seat,
				Detail: fmt.Sprintf("publishing tip %s differs from observed remote tip %s", publishing, emptyAsNone(snapshot.FactTips[seat])),
			})
		}
	}
	return snapshot
}

func SourceKey(source protocol.Source) string {
	return source.Commit + "\x00" + source.Path + "\x00" + source.Blob
}

func (s *Snapshot) SourceValid(source protocol.Source) bool {
	return s.ValidSources[SourceKey(source)]
}

func (s *Snapshot) SourceMatches(source protocol.Source) bool {
	return s.MatchingSources[SourceKey(source)]
}

func (s *Snapshot) CommitKnown(oid string) bool {
	return oid != "" && s.KnownCommits[oid]
}

// IsAncestor answers from the object graph captured by PopulateIndexes. Event
// replay must not spawn one merge-base process per CLAIM.
func (s *Snapshot) IsAncestor(older, newer string) bool {
	if !s.CommitKnown(older) || !s.CommitKnown(newer) {
		return false
	}
	if older == newer {
		return true
	}
	seen := map[string]bool{}
	stack := []string{newer}
	for len(stack) > 0 {
		last := len(stack) - 1
		oid := stack[last]
		stack = stack[:last]
		if seen[oid] {
			continue
		}
		seen[oid] = true
		for _, parent := range s.CommitParents[oid] {
			if parent == older {
				return true
			}
			stack = append(stack, parent)
		}
	}
	return false
}

func CoopRef(seat string) string {
	return "refs/coop/" + seat
}

func TrackingCoopRef(remote, seat string) string {
	return "refs/hctl/remotes/" + remote + "/coop/" + seat
}

func TrackingHeadRef(remote, branch string) string {
	return "refs/hctl/remotes/" + remote + "/heads/" + strings.TrimPrefix(branch, "refs/heads/")
}

func TrackingPullRef(remote string, number int) string {
	return fmt.Sprintf("refs/hctl/remotes/%s/pull/%d/head", remote, number)
}

func lsRemote(ctx context.Context, repo *gitx.Repo, remote string) (map[string]string, error) {
	out, err := repo.Output(ctx, "ls-remote", "--refs", remote)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		oid, ref, ok := strings.Cut(line, "\t")
		if !ok || !protocol.ValidOID(oid) {
			return nil, fmt.Errorf("malformed ls-remote line %q", line)
		}
		if strings.HasPrefix(ref, "refs/heads/") ||
			(strings.HasPrefix(ref, "refs/pull/") && strings.HasSuffix(ref, "/head")) ||
			strings.HasPrefix(ref, "refs/coop/") {
			result[ref] = oid
		}
	}
	return result, nil
}

func fetchBranchPlane(
	ctx context.Context,
	repo *gitx.Repo,
	remote string,
	advertised map[string]string,
	relevantHeads map[string]bool,
	forwardOnlyHeads map[string]bool,
	includePulls bool,
) error {
	var refspecs []string
	for ref := range advertised {
		switch {
		case strings.HasPrefix(ref, "refs/heads/") && relevantHeads[ref]:
			if !forwardOnlyHeads[ref] {
				refspecs = append(refspecs, "+"+ref+":"+TrackingHeadRef(remote, ref))
			} else {
				// Integration bases are forward-only. No '+' makes rollback loud.
				refspecs = append(refspecs, ref+":"+TrackingHeadRef(remote, ref))
			}
		case includePulls && strings.HasPrefix(ref, "refs/pull/") && strings.HasSuffix(ref, "/head"):
			middle := strings.TrimSuffix(strings.TrimPrefix(ref, "refs/pull/"), "/head")
			number, err := strconv.Atoi(middle)
			if err == nil && number > 0 {
				refspecs = append(refspecs, "+"+ref+":"+TrackingPullRef(remote, number))
			}
		}
	}
	if len(refspecs) == 0 {
		return nil
	}
	sort.Strings(refspecs)
	args := append([]string{"fetch", "--no-tags", remote}, refspecs...)
	_, err := repo.Output(ctx, args...)
	return err
}

func fetchMissingCommits(
	ctx context.Context,
	repo *gitx.Repo,
	remote string,
	missing map[string]bool,
) error {
	oids := make([]string, 0, len(missing))
	for oid := range missing {
		if protocol.ValidOID(oid) {
			oids = append(oids, oid)
		}
	}
	if len(oids) == 0 {
		return nil
	}
	sort.Strings(oids)
	args := append([]string{"fetch", "--no-tags", remote}, oids...)
	_, err := repo.Output(ctx, args...)
	return err
}

func syncTrackingRef(ctx context.Context, repo *gitx.Repo, remote, seat, remoteRef, trackingRef, advertisedTip string) error {
	trackingTip, trackingExists, err := repo.Resolve(ctx, trackingRef)
	if err != nil {
		return err
	}
	if advertisedTip == "" {
		if trackingExists {
			if err := preserveTrackingRecovery(ctx, repo, seat, trackingTip); err != nil {
				return fmt.Errorf("preserve disappeared chain tip: %w", err)
			}
			return fmt.Errorf("remote chain %s disappeared; retained last tip %s", remoteRef, trackingTip)
		}
		return nil
	}
	if trackingExists && trackingTip == advertisedTip {
		return nil
	}
	// Fetching by OID populates the object database without moving the durable
	// tracking ref. This lets us classify rollback/divergence before following it.
	if _, exists, err := repo.Resolve(ctx, advertisedTip); err != nil {
		return err
	} else if !exists {
		if _, err := repo.Output(ctx, "fetch", "--no-tags", remote, advertisedTip); err != nil {
			return fmt.Errorf("fetch advertised chain object %s: %w", advertisedTip, err)
		}
	}
	if trackingExists {
		forward, err := repo.IsAncestor(ctx, trackingTip, advertisedTip)
		if err != nil {
			return err
		}
		if !forward {
			backward, backErr := repo.IsAncestor(ctx, advertisedTip, trackingTip)
			if backErr != nil {
				return backErr
			}
			if backward {
				if err := preserveTrackingRecovery(ctx, repo, seat, trackingTip); err != nil {
					return fmt.Errorf("preserve rolled-back chain tip: %w", err)
				}
				return fmt.Errorf("remote chain %s rolled back from %s to %s", remoteRef, trackingTip, advertisedTip)
			}
			if err := preserveTrackingRecovery(ctx, repo, seat, trackingTip); err != nil {
				return fmt.Errorf("preserve diverged chain tip: %w", err)
			}
			return fmt.Errorf("remote chain %s diverged: retained %s, advertised %s", remoteRef, trackingTip, advertisedTip)
		}
	}
	_, err = repo.Output(ctx, "fetch", "--no-tags", remote, remoteRef+":"+trackingRef)
	if err != nil {
		return fmt.Errorf("advance tracking ref %s: %w", trackingRef, err)
	}
	return nil
}

func preserveTrackingRecovery(ctx context.Context, repo *gitx.Repo, seat, oid string) error {
	ref := "refs/hctl/recovery/" + seat + "/" + oid
	if existing, ok, err := repo.Resolve(ctx, ref); err != nil {
		return err
	} else if ok {
		if existing != oid {
			return fmt.Errorf("recovery ref collision at %s", ref)
		}
		return nil
	}
	return repo.UpdateRef(ctx, ref, oid, "", "hctl: preserve last verified remote chain tip")
}

func unique(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := values[:0]
	var previous string
	for i, value := range values {
		if i == 0 || value != previous {
			out = append(out, value)
			previous = value
		}
	}
	return out
}

func emptyAsNone(value string) string {
	if value == "" {
		return "<none>"
	}
	return value
}

func sortedSeatIDs(seats map[string]config.Seat) []string {
	ids := make([]string, 0, len(seats))
	for id := range seats {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
