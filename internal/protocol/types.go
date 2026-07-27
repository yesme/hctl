package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/yesme/hctl/internal/jcs"
)

const (
	SchemaVersion    = 1
	ObligationDomain = "hctl-obligation-v1\x00"
)

var (
	tokenPattern      = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	oidPattern        = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
	obligationPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type Source struct {
	Commit string `json:"commit"`
	Path   string `json:"path"`
	Blob   string `json:"blob"`
}

type Revision struct {
	Base string `json:"base"`
	Head string `json:"head"`
}

type Actor struct {
	Seat    string  `json:"seat"`
	Machine string  `json:"machine"`
	Session *string `json:"session"`
}

type AssignmentIdentity struct {
	ID          string `json:"id,omitempty"`
	Blob        string `json:"blob,omitempty"`
	AssignEvent string `json:"assign_event,omitempty"`
}

func (a AssignmentIdentity) IsStatic() bool {
	return a.ID != "" && a.Blob != "" && a.AssignEvent == ""
}

type GateAspect struct {
	GateID    string `json:"gate_id"`
	GaterSeat string `json:"gater_seat"`
}

type ObligationPreimage struct {
	Assignment AssignmentIdentity `json:"assignment"`
	Kind       string             `json:"kind"`
	Target     string             `json:"target"`
	Aspect     *GateAspect        `json:"aspect"`
}

type Obligation struct {
	Preimage ObligationPreimage `json:"preimage"`
	ID       string             `json:"id"`
	Source   *Source            `json:"source,omitempty"`
}

func NewStaticObligation(assignmentID, assignmentBlob, kind, target string, aspect *GateAspect, source Source) (Obligation, error) {
	preimage := ObligationPreimage{
		Assignment: AssignmentIdentity{ID: assignmentID, Blob: assignmentBlob},
		Kind:       kind,
		Target:     target,
		Aspect:     aspect,
	}
	id, err := ObligationID(preimage)
	if err != nil {
		return Obligation{}, err
	}
	return Obligation{Preimage: preimage, ID: id, Source: &source}, nil
}

func ObligationID(preimage ObligationPreimage) (string, error) {
	if err := ValidatePreimage(preimage); err != nil {
		return "", err
	}
	canonical, err := jcs.Marshal(preimage)
	if err != nil {
		return "", fmt.Errorf("canonicalize obligation preimage: %w", err)
	}
	sum := sha256.Sum256(append([]byte(ObligationDomain), canonical...))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func ValidatePreimage(p ObligationPreimage) error {
	if !p.Assignment.IsStatic() {
		if p.Assignment.AssignEvent != "" && p.Assignment.ID == "" && p.Assignment.Blob == "" {
			if !ValidOID(p.Assignment.AssignEvent) {
				return fmt.Errorf("invalid dynamic assignment event OID")
			}
		} else {
			return fmt.Errorf("assignment identity must be exactly static or dynamic")
		}
	} else {
		if !ValidToken(p.Assignment.ID) {
			return fmt.Errorf("invalid assignment id %q", p.Assignment.ID)
		}
		if !ValidOID(p.Assignment.Blob) {
			return fmt.Errorf("invalid assignment blob %q", p.Assignment.Blob)
		}
	}
	switch p.Kind {
	case "author", "merge":
		if p.Aspect != nil {
			return fmt.Errorf("%s obligation aspect must be null", p.Kind)
		}
	case "gate":
		if p.Aspect == nil {
			return fmt.Errorf("gate obligation requires an aspect")
		}
		if !ValidToken(p.Aspect.GateID) || !ValidToken(p.Aspect.GaterSeat) {
			return fmt.Errorf("gate aspect contains an invalid identity token")
		}
	default:
		return fmt.Errorf("invalid obligation kind %q", p.Kind)
	}
	if !ValidToken(p.Target) {
		return fmt.Errorf("invalid obligation target %q", p.Target)
	}
	return nil
}

func ValidateObligation(o Obligation) error {
	if err := ValidatePreimage(o.Preimage); err != nil {
		return err
	}
	want, err := ObligationID(o.Preimage)
	if err != nil {
		return err
	}
	if o.ID != want {
		return fmt.Errorf("obligation id mismatch: got %q, recomputed %q", o.ID, want)
	}
	if o.Preimage.Assignment.IsStatic() {
		if o.Source == nil {
			return fmt.Errorf("static obligation requires source")
		}
		if err := ValidateSource(*o.Source); err != nil {
			return fmt.Errorf("invalid obligation source: %w", err)
		}
	}
	return nil
}

func ValidateSource(s Source) error {
	if !ValidOID(s.Commit) || !ValidOID(s.Blob) {
		return fmt.Errorf("source commit and blob must be full Git OIDs")
	}
	if s.Path == "" {
		return fmt.Errorf("source path is empty")
	}
	if strings.ContainsRune(s.Path, '\x00') {
		return fmt.Errorf("source path contains NUL")
	}
	return nil
}

func ValidMemoPath(value string) bool {
	if !strings.HasPrefix(value, "memory/") ||
		value == "memory/" ||
		strings.ContainsAny(value, "\x00\r\n") {
		return false
	}
	return path.Clean(value) == value
}

func ValidateRevision(r Revision) error {
	if !ValidOID(r.Base) || !ValidOID(r.Head) {
		return fmt.Errorf("revision base and head must be full Git OIDs")
	}
	return nil
}

func ValidateActor(a Actor) error {
	if !ValidToken(a.Seat) {
		return fmt.Errorf("invalid actor seat %q", a.Seat)
	}
	if a.Machine == "" {
		return fmt.Errorf("actor machine is empty")
	}
	return nil
}

func ValidToken(s string) bool        { return tokenPattern.MatchString(s) }
func ValidOID(s string) bool          { return oidPattern.MatchString(s) }
func ValidObligationID(s string) bool { return obligationPattern.MatchString(s) }

type Claim struct {
	SchemaVersion     int
	Actor             Actor
	CreatedAt         time.Time
	Obligation        Obligation
	RevisionAtClaim   *Revision
	TipAtClaim        *string
	TipAtClaimPresent bool
	CoordinatorConfig *Source
	ReclaimOf         *string
	ReclaimReason     *string
}

type ScopePart struct {
	Kind       string   `json:"kind"`
	Findings   []string `json:"findings,omitempty"`
	TargetBlob string   `json:"target_blob,omitempty"`
	FromBlob   string   `json:"from_blob,omitempty"`
	ToBlob     string   `json:"to_blob,omitempty"`
}

type ReviewScope struct {
	Kind  string      `json:"kind"`
	Parts []ScopePart `json:"parts,omitempty"`
}

type Verdict struct {
	SchemaVersion    int
	Actor            Actor
	CreatedAt        time.Time
	Obligation       Obligation
	Claim            string
	PR               int
	Revision         Revision
	Decision         string
	Report           Source
	Scope            ReviewScope
	Completeness     string
	IncompleteReason string
}

type CancelTarget struct {
	Kind       string
	Assignment *Source
	Obligation string
	Claim      string
}

type Cancel struct {
	SchemaVersion int
	Actor         Actor
	CreatedAt     time.Time
	Target        CancelTarget
	Reason        string
}

type Event struct {
	OID           string
	Parent        string
	ChainSeat     string
	CommitterUnix int64
	Type          string
	Claim         *Claim
	Verdict       *Verdict
	Cancel        *Cancel
	Raw           []byte
}

func MarshalClaim(c Claim) ([]byte, error) {
	obj := map[string]any{
		"schema_version": c.SchemaVersion,
		"type":           "CLAIM",
		"actor":          c.Actor,
		"created_at":     c.CreatedAt.UTC().Format(time.RFC3339),
		"obligation":     c.Obligation,
		"reclaim_of":     c.ReclaimOf,
		"reclaim_reason": c.ReclaimReason,
	}
	switch c.Obligation.Preimage.Kind {
	case "author":
		if !c.TipAtClaimPresent {
			return nil, fmt.Errorf("author claim requires tip_at_claim (which may be null)")
		}
		obj["tip_at_claim"] = c.TipAtClaim
	case "gate":
		if c.RevisionAtClaim == nil {
			return nil, fmt.Errorf("gate claim requires revision_at_claim")
		}
		obj["revision_at_claim"] = c.RevisionAtClaim
	case "merge":
		if c.RevisionAtClaim == nil || c.CoordinatorConfig == nil {
			return nil, fmt.Errorf("merge claim requires revision_at_claim and coordinator_config")
		}
		obj["revision_at_claim"] = c.RevisionAtClaim
		obj["coordinator_config"] = c.CoordinatorConfig
	default:
		return nil, fmt.Errorf("invalid claim obligation kind")
	}
	return json.MarshalIndent(obj, "", "  ")
}

func MarshalVerdict(v Verdict) ([]byte, error) {
	obj := map[string]any{
		"schema_version": v.SchemaVersion,
		"type":           "VERDICT",
		"actor":          v.Actor,
		"created_at":     v.CreatedAt.UTC().Format(time.RFC3339),
		"obligation":     v.Obligation,
		"claim":          v.Claim,
		"pr":             v.PR,
		"revision":       v.Revision,
		"decision":       v.Decision,
		"report":         v.Report,
		"scope":          scopeWire(v.Scope),
		"completeness":   v.Completeness,
	}
	if v.IncompleteReason != "" {
		obj["incomplete_reason"] = v.IncompleteReason
	}
	return json.MarshalIndent(obj, "", "  ")
}

func MarshalCancel(c Cancel) ([]byte, error) {
	var target any
	switch c.Target.Kind {
	case "assignment":
		target = map[string]any{"kind": "assignment", "assignment": c.Target.Assignment}
	case "obligation":
		target = map[string]any{"kind": "obligation", "obligation": c.Target.Obligation}
	case "claim":
		target = map[string]any{"kind": "claim", "claim": c.Target.Claim}
	default:
		return nil, fmt.Errorf("invalid cancel target kind %q", c.Target.Kind)
	}
	obj := map[string]any{
		"schema_version": c.SchemaVersion,
		"type":           "CANCEL",
		"actor":          c.Actor,
		"created_at":     c.CreatedAt.UTC().Format(time.RFC3339),
		"target":         target,
		"reason":         c.Reason,
		"authority":      map[string]any{"kind": "user"},
	}
	return json.MarshalIndent(obj, "", "  ")
}

func scopeWire(scope ReviewScope) any {
	if scope.Kind == "full" {
		return map[string]any{"kind": "full"}
	}
	parts := make([]any, 0, len(scope.Parts))
	for _, part := range scope.Parts {
		switch part.Kind {
		case "fix_verification":
			parts = append(parts, map[string]any{
				"kind":        part.Kind,
				"findings":    part.Findings,
				"target_blob": part.TargetBlob,
			})
		case "delta":
			parts = append(parts, map[string]any{
				"kind":      part.Kind,
				"from_blob": part.FromBlob,
				"to_blob":   part.ToBlob,
			})
		}
	}
	return map[string]any{"kind": "composite", "parts": parts}
}
