package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yesme/hctl/internal/protocol"
)

const SeatsPath = ".hctl/seats.toml"

var (
	baseRefPattern = regexp.MustCompile(`^refs/heads/[a-z0-9._/-]+$`)
	branchPattern  = regexp.MustCompile(`^refs/heads/[a-z0-9._/-]*(\*)?[a-z0-9._/-]*$`)
)

type Seats struct {
	SchemaVersion    int             `toml:"schema_version"`
	Kernel           string          `toml:"kernel"`
	MergeCoordinator string          `toml:"merge_coordinator"`
	Enforcement      string          `toml:"enforcement"`
	Seats            map[string]Seat `toml:"seats"`
}

type Seat struct {
	Harness              string   `toml:"harness"`
	Model                string   `toml:"model"`
	Capabilities         []string `toml:"capabilities"`
	WriteScope           string   `toml:"write_scope"`
	AuthorConcurrency    int      `toml:"author_concurrency"`
	AuthorBranchPatterns []string `toml:"author_branch_patterns"`
	MemoBranchPatterns   []string `toml:"memo_branch_patterns"`
	TokenClass           string   `toml:"token_class"`
	Features             Features `toml:"features"`
}

type Features struct {
	SessionResume    string `toml:"session_resume"`
	ParallelSessions string `toml:"parallel_sessions"`
	Subagents        string `toml:"subagents"`
	Skill            string `toml:"skill"`
	DoorFile         string `toml:"door_file"`
}

type Assignment struct {
	SchemaVersion int      `toml:"schema_version"`
	ID            string   `toml:"id"`
	Kind          string   `toml:"kind"`
	Needs         []string `toml:"needs"`
	BaseRef       string   `toml:"base_ref"`
	Author        Author   `toml:"author"`
	Gates         []Gate   `toml:"gates"`
	Merge         Merge    `toml:"merge"`
}

type Author struct {
	Seat                string `toml:"seat"`
	BranchSlug          string `toml:"branch_slug"`
	ClaimTimeoutSeconds int64  `toml:"claim_timeout_seconds"`
}

type Gate struct {
	ID                  string      `toml:"id"`
	Mode                string      `toml:"mode"`
	Requirement         QuorumNode  `toml:"requirement"`
	Threshold           string      `toml:"threshold"`
	OnTimeout           TimeoutRule `toml:"on_timeout"`
	ClaimTimeoutSeconds int64       `toml:"claim_timeout_seconds"`
}

type TimeoutRule struct {
	Action string `toml:"action"`
	Deputy string `toml:"deputy"`
}

type QuorumNode struct {
	Seat    string         `toml:"seat"`
	AllOf   []QuorumNode   `toml:"all_of"`
	AnyOf   []QuorumNode   `toml:"any_of"`
	AtLeast *AtLeastQuorum `toml:"at_least"`
}

type AtLeastQuorum struct {
	Count int          `toml:"count"`
	Of    []QuorumNode `toml:"of"`
}

type Merge struct {
	Method string `toml:"method"`
}

type SeatsRecord struct {
	Config Seats
	Source protocol.Source
}

type AssignmentRecord struct {
	Assignment Assignment
	Source     protocol.Source
}

func (a AssignmentRecord) BranchRef() string {
	return "refs/heads/work/" + a.Assignment.Author.Seat + "/" + a.Assignment.Author.BranchSlug
}

func (a AssignmentRecord) SourceKey() string {
	return a.Source.Commit + ":" + a.Source.Path + ":" + a.Source.Blob
}

func ValidateSeats(c Seats) error {
	if c.SchemaVersion != 1 {
		return fmt.Errorf("seats schema_version must be 1")
	}
	if c.Kernel == "" {
		return fmt.Errorf("kernel pin is empty")
	}
	if !protocol.ValidToken(c.MergeCoordinator) {
		return fmt.Errorf("invalid merge_coordinator %q", c.MergeCoordinator)
	}
	if c.Enforcement != "bootstrap" && c.Enforcement != "active" {
		return fmt.Errorf("enforcement must be bootstrap or active")
	}
	if len(c.Seats) == 0 {
		return fmt.Errorf("at least one seat is required")
	}
	seatIDs := sortedSeatIDs(c.Seats)
	for _, id := range seatIDs {
		seat := c.Seats[id]
		if !protocol.ValidToken(id) {
			return fmt.Errorf("invalid seat id %q", id)
		}
		if err := validateSeat(id, seat); err != nil {
			return err
		}
	}
	coordinator, ok := c.Seats[c.MergeCoordinator]
	if !ok {
		return fmt.Errorf("merge_coordinator %q is not a configured seat", c.MergeCoordinator)
	}
	if coordinator.WriteScope != "canonical" || coordinator.AuthorConcurrency != 1 {
		return fmt.Errorf("merge_coordinator %q must have canonical write scope", c.MergeCoordinator)
	}
	type namedPattern struct {
		seat string
		lane string
		text string
	}
	var patterns []namedPattern
	for _, id := range seatIDs {
		seat := c.Seats[id]
		for _, pattern := range seat.AuthorBranchPatterns {
			patterns = append(patterns, namedPattern{id, "author", pattern})
		}
		for _, pattern := range seat.MemoBranchPatterns {
			patterns = append(patterns, namedPattern{id, "memo", pattern})
		}
	}
	for i := range patterns {
		for j := i + 1; j < len(patterns); j++ {
			if patternsIntersect(patterns[i].text, patterns[j].text) {
				return fmt.Errorf(
					"branch patterns overlap: %s/%s %q and %s/%s %q",
					patterns[i].seat, patterns[i].lane, patterns[i].text,
					patterns[j].seat, patterns[j].lane, patterns[j].text,
				)
			}
		}
	}
	return nil
}

func validateSeat(id string, seat Seat) error {
	if seat.Harness == "" || seat.Model == "" {
		return fmt.Errorf("seat %q harness and model are required", id)
	}
	if duplicate(seat.Capabilities) {
		return fmt.Errorf("seat %q has duplicate capabilities", id)
	}
	switch seat.WriteScope {
	case "canonical":
		if seat.AuthorConcurrency != 1 || len(seat.AuthorBranchPatterns) == 0 {
			return fmt.Errorf("canonical seat %q requires author_concurrency=1 and author patterns", id)
		}
	case "memo-only":
		if seat.AuthorConcurrency != 0 || len(seat.AuthorBranchPatterns) != 0 {
			return fmt.Errorf("memo-only seat %q requires author_concurrency=0 and no author patterns", id)
		}
	default:
		return fmt.Errorf("seat %q has invalid write_scope %q", id, seat.WriteScope)
	}
	if len(seat.MemoBranchPatterns) == 0 {
		return fmt.Errorf("seat %q requires at least one memo branch pattern", id)
	}
	for _, pattern := range append(append([]string(nil), seat.AuthorBranchPatterns...), seat.MemoBranchPatterns...) {
		if !validBranchPattern(pattern) {
			return fmt.Errorf("seat %q has invalid branch pattern %q", id, pattern)
		}
	}
	if duplicate(seat.AuthorBranchPatterns) || duplicate(seat.MemoBranchPatterns) {
		return fmt.Errorf("seat %q has duplicate branch patterns", id)
	}
	if seat.TokenClass != "" && seat.TokenClass != "full" && seat.TokenClass != "limited" {
		return fmt.Errorf("seat %q has invalid token_class %q", id, seat.TokenClass)
	}
	for name, value := range map[string]string{
		"session_resume":    seat.Features.SessionResume,
		"parallel_sessions": seat.Features.ParallelSessions,
		"subagents":         seat.Features.Subagents,
	} {
		if value != "yes" && value != "no" && value != "limited" && value != "unknown" {
			return fmt.Errorf("seat %q feature %s has invalid value %q", id, name, value)
		}
	}
	switch seat.Features.Skill {
	case "native", "adapter", "unsupported", "unknown":
	default:
		return fmt.Errorf("seat %q has invalid skill feature %q", id, seat.Features.Skill)
	}
	if seat.Features.DoorFile == "" {
		return fmt.Errorf("seat %q door_file is empty", id)
	}
	return nil
}

func ValidateAssignments(records []AssignmentRecord, seats Seats) error {
	byID := make(map[string]AssignmentRecord, len(records))
	for _, record := range records {
		a := record.Assignment
		if _, exists := byID[a.ID]; exists {
			return fmt.Errorf("AMBIGUOUS_ASSIGNMENT: duplicate logical assignment id %q", a.ID)
		}
		if err := ValidateAssignment(a, seats); err != nil {
			return fmt.Errorf("%s: %w", record.Source.Path, err)
		}
		byID[a.ID] = record
	}
	for _, record := range records {
		for _, need := range record.Assignment.Needs {
			if _, exists := byID[need]; !exists {
				return fmt.Errorf("assignment %q needs unknown assignment %q", record.Assignment.ID, need)
			}
		}
	}
	state := make(map[string]uint8, len(records))
	var visit func(string) error
	visit = func(id string) error {
		switch state[id] {
		case 1:
			return fmt.Errorf("assignment dependency cycle contains %q", id)
		case 2:
			return nil
		}
		state[id] = 1
		for _, need := range byID[id].Assignment.Needs {
			if err := visit(need); err != nil {
				return err
			}
		}
		state[id] = 2
		return nil
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func ValidateAssignment(a Assignment, seats Seats) error {
	if a.SchemaVersion != 1 {
		return fmt.Errorf("schema_version must be 1")
	}
	if !protocol.ValidToken(a.ID) || !protocol.ValidToken(a.Author.Seat) || !protocol.ValidToken(a.Author.BranchSlug) {
		return fmt.Errorf("id, author.seat, and branch_slug must use the identity token grammar")
	}
	if a.Kind != "change" {
		return fmt.Errorf("kind must be change in P1")
	}
	if duplicate(a.Needs) {
		return fmt.Errorf("needs contains duplicates")
	}
	for _, need := range a.Needs {
		if !protocol.ValidToken(need) {
			return fmt.Errorf("invalid prerequisite id %q", need)
		}
	}
	if !baseRefPattern.MatchString(a.BaseRef) ||
		strings.Contains(a.BaseRef, "..") ||
		strings.Contains(a.BaseRef, "//") ||
		strings.HasSuffix(a.BaseRef, ".") ||
		strings.HasSuffix(a.BaseRef, "/") {
		return fmt.Errorf("invalid base_ref %q", a.BaseRef)
	}
	for _, component := range strings.Split(a.BaseRef, "/") {
		if strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return fmt.Errorf("invalid base_ref %q", a.BaseRef)
		}
	}
	authorSeat, ok := seats.Seats[a.Author.Seat]
	if !ok {
		return fmt.Errorf("unknown author seat %q", a.Author.Seat)
	}
	if authorSeat.WriteScope != "canonical" || authorSeat.AuthorConcurrency != 1 {
		return fmt.Errorf("author seat %q cannot author canonical work", a.Author.Seat)
	}
	if a.Author.ClaimTimeoutSeconds < 60 {
		return fmt.Errorf("author claim timeout must be at least 60 seconds")
	}
	branch := "refs/heads/work/" + a.Author.Seat + "/" + a.Author.BranchSlug
	if !MatchesAny(branch, authorSeat.AuthorBranchPatterns) {
		return fmt.Errorf("assignment branch %q is outside author seat patterns", branch)
	}
	if a.Merge.Method != "squash" {
		return fmt.Errorf("merge method must be squash")
	}
	gateIDs := map[string]bool{}
	for i, gate := range a.Gates {
		if !protocol.ValidToken(gate.ID) || gateIDs[gate.ID] {
			return fmt.Errorf("gate %d has invalid or duplicate id %q", i, gate.ID)
		}
		gateIDs[gate.ID] = true
		if gate.Mode != "required" && gate.Mode != "advisory" && gate.Mode != "observe" {
			return fmt.Errorf("gate %q has invalid mode %q", gate.ID, gate.Mode)
		}
		if gate.Threshold != "P0" && gate.Threshold != "P1" && gate.Threshold != "all" {
			return fmt.Errorf("gate %q has invalid threshold %q", gate.ID, gate.Threshold)
		}
		if gate.ClaimTimeoutSeconds < 60 {
			return fmt.Errorf("gate %q claim timeout must be at least 60 seconds", gate.ID)
		}
		switch gate.OnTimeout.Action {
		case "escalate":
			if gate.OnTimeout.Deputy != "" {
				return fmt.Errorf("gate %q escalate timeout cannot name a deputy", gate.ID)
			}
		case "deputy":
			if !protocol.ValidToken(gate.OnTimeout.Deputy) {
				return fmt.Errorf("gate %q deputy is invalid", gate.ID)
			}
			if _, ok := seats.Seats[gate.OnTimeout.Deputy]; !ok {
				return fmt.Errorf("gate %q deputy seat is unknown", gate.ID)
			}
		case "proceed":
			if gate.Mode == "required" {
				return fmt.Errorf("required gate %q cannot proceed on timeout", gate.ID)
			}
			if gate.OnTimeout.Deputy != "" {
				return fmt.Errorf("gate %q proceed timeout cannot name a deputy", gate.ID)
			}
		default:
			return fmt.Errorf("gate %q has invalid timeout action %q", gate.ID, gate.OnTimeout.Action)
		}
		leaves, err := ValidateQuorum(gate.Requirement, seats)
		if err != nil {
			return fmt.Errorf("gate %q requirement: %w", gate.ID, err)
		}
		seen := map[string]bool{}
		for _, leaf := range leaves {
			if leaf == a.Author.Seat {
				return fmt.Errorf("gate %q gater %q is also the author", gate.ID, leaf)
			}
			if seen[leaf] {
				return fmt.Errorf("gate %q repeats leaf seat %q", gate.ID, leaf)
			}
			seen[leaf] = true
		}
	}
	return nil
}

func ValidateQuorum(node QuorumNode, seats Seats) ([]string, error) {
	variants := 0
	if node.Seat != "" {
		variants++
	}
	if node.AllOf != nil {
		variants++
	}
	if node.AnyOf != nil {
		variants++
	}
	if node.AtLeast != nil {
		variants++
	}
	if variants != 1 {
		return nil, fmt.Errorf("quorum node must contain exactly one variant")
	}
	if node.Seat != "" {
		if !protocol.ValidToken(node.Seat) {
			return nil, fmt.Errorf("invalid seat token %q", node.Seat)
		}
		if _, ok := seats.Seats[node.Seat]; !ok {
			return nil, fmt.Errorf("unknown seat %q", node.Seat)
		}
		return []string{node.Seat}, nil
	}
	children := node.AllOf
	if node.AnyOf != nil {
		children = node.AnyOf
	}
	if node.AtLeast != nil {
		children = node.AtLeast.Of
		if node.AtLeast.Count < 1 || node.AtLeast.Count > len(children) {
			return nil, fmt.Errorf("at_least count must be between 1 and len(of)")
		}
	}
	if len(children) == 0 {
		return nil, fmt.Errorf("quorum collection must not be empty")
	}
	var leaves []string
	for _, child := range children {
		childLeaves, err := ValidateQuorum(child, seats)
		if err != nil {
			return nil, err
		}
		leaves = append(leaves, childLeaves...)
	}
	return leaves, nil
}

func MatchesAny(ref string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchPattern(pattern, ref) {
			return true
		}
	}
	return false
}

func validBranchPattern(pattern string) bool {
	if !branchPattern.MatchString(pattern) || strings.Count(pattern, "*") > 1 {
		return false
	}
	if strings.Contains(pattern, "..") ||
		strings.Contains(pattern, "@{") ||
		strings.Contains(pattern, "//") ||
		strings.HasSuffix(pattern, ".") ||
		strings.HasSuffix(pattern, "/") {
		return false
	}
	for _, component := range strings.Split(pattern, "/") {
		if strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}

func matchPattern(pattern, ref string) bool {
	prefix, suffix, hasStar := strings.Cut(pattern, "*")
	if !hasStar {
		return pattern == ref
	}
	return strings.HasPrefix(ref, prefix) && strings.HasSuffix(ref, suffix) && len(ref) >= len(prefix)+len(suffix)
}

func patternsIntersect(a, b string) bool {
	ap, as, astar := strings.Cut(a, "*")
	bp, bs, bstar := strings.Cut(b, "*")
	if !astar {
		return matchPattern(b, a)
	}
	if !bstar {
		return matchPattern(a, b)
	}
	if !(strings.HasPrefix(ap, bp) || strings.HasPrefix(bp, ap)) {
		return false
	}
	if !(strings.HasSuffix(as, bs) || strings.HasSuffix(bs, as)) {
		return false
	}
	longPrefix := ap
	if len(bp) > len(longPrefix) {
		longPrefix = bp
	}
	longSuffix := as
	if len(bs) > len(longSuffix) {
		longSuffix = bs
	}
	candidate := longPrefix + longSuffix
	return matchPattern(a, candidate) && matchPattern(b, candidate)
}

func duplicate(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}

func AssignmentPaths(records []AssignmentRecord) []string {
	paths := make([]string, 0, len(records))
	for _, record := range records {
		paths = append(paths, filepath.ToSlash(record.Source.Path))
	}
	sort.Strings(paths)
	return paths
}

func sortedSeatIDs(seats map[string]Seat) []string {
	ids := make([]string, 0, len(seats))
	for id := range seats {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
