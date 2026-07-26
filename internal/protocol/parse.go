package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/yesme/hctl/internal/jcs"
)

type UnsupportedError struct {
	Reason string
}

func (e *UnsupportedError) Error() string { return "UNSUPPORTED_FACTS: " + e.Reason }

type StructuralError struct {
	Reason string
}

func (e *StructuralError) Error() string { return "CORRUPT_CHAIN: " + e.Reason }

func structural(format string, args ...any) error {
	return &StructuralError{Reason: fmt.Sprintf(format, args...)}
}

func ParseEvent(raw []byte, chainSeat string) (*Event, error) {
	value, err := jcs.Parse(raw)
	if err != nil {
		return nil, structural("event is not valid I-JSON: %v", err)
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, structural("event root is not an object")
	}
	version, err := integer(obj["schema_version"])
	if err != nil {
		return nil, structural("schema_version: %v", err)
	}
	typeName, ok := obj["type"].(string)
	if !ok || typeName == "" {
		return nil, structural("type is missing or not a string")
	}
	actor, err := parseActor(obj["actor"])
	if err != nil {
		return nil, structural("actor: %v", err)
	}
	if actor.Seat != chainSeat {
		return nil, structural("actor seat %q does not match chain seat %q", actor.Seat, chainSeat)
	}
	createdAt, err := dateTime(obj["created_at"])
	if err != nil {
		return nil, structural("created_at: %v", err)
	}
	if version != SchemaVersion {
		return nil, &UnsupportedError{Reason: fmt.Sprintf("event schema_version %d is not supported", version)}
	}
	event := &Event{Type: typeName, ChainSeat: chainSeat, Raw: append([]byte(nil), raw...)}
	switch typeName {
	case "CLAIM":
		claim, err := parseClaim(obj, actor, createdAt)
		if err != nil {
			var unsupported *UnsupportedError
			if errors.As(err, &unsupported) {
				return nil, unsupported
			}
			return nil, structural("CLAIM: %v", err)
		}
		event.Claim = claim
	case "VERDICT":
		verdict, err := parseVerdict(obj, actor, createdAt)
		if err != nil {
			var unsupported *UnsupportedError
			if errors.As(err, &unsupported) {
				return nil, unsupported
			}
			return nil, structural("VERDICT: %v", err)
		}
		event.Verdict = verdict
	case "CANCEL":
		cancel, err := parseCancel(obj, actor, createdAt)
		if err != nil {
			var unsupported *UnsupportedError
			if errors.As(err, &unsupported) {
				return nil, unsupported
			}
			return nil, structural("CANCEL: %v", err)
		}
		event.Cancel = cancel
	default:
		return nil, &UnsupportedError{Reason: fmt.Sprintf("event type %q is not supported", typeName)}
	}
	return event, nil
}

func parseClaim(obj map[string]any, actor Actor, createdAt time.Time) (*Claim, error) {
	allowed := []string{
		"schema_version", "type", "actor", "created_at", "obligation",
		"revision_at_claim", "tip_at_claim", "coordinator_config",
		"reclaim_of", "reclaim_reason",
	}
	if err := exactObject(obj, allowed,
		"schema_version", "type", "actor", "created_at", "obligation", "reclaim_of", "reclaim_reason"); err != nil {
		return nil, err
	}
	obligation, err := parseObligation(obj["obligation"])
	if err != nil {
		return nil, err
	}
	c := &Claim{
		SchemaVersion: SchemaVersion,
		Actor:         actor,
		CreatedAt:     createdAt,
		Obligation:    obligation,
	}
	if raw, present := obj["revision_at_claim"]; present {
		revision, err := parseRevision(raw)
		if err != nil {
			return nil, fmt.Errorf("revision_at_claim: %w", err)
		}
		c.RevisionAtClaim = &revision
	}
	if raw, present := obj["tip_at_claim"]; present {
		c.TipAtClaimPresent = true
		if raw != nil {
			tip, ok := raw.(string)
			if !ok || !ValidOID(tip) {
				return nil, errors.New("tip_at_claim must be null or a full Git OID")
			}
			c.TipAtClaim = &tip
		}
	}
	if raw, present := obj["coordinator_config"]; present {
		source, err := parseSource(raw)
		if err != nil {
			return nil, fmt.Errorf("coordinator_config: %w", err)
		}
		c.CoordinatorConfig = &source
	}
	c.ReclaimOf, err = nullableOID(obj["reclaim_of"])
	if err != nil {
		return nil, fmt.Errorf("reclaim_of: %w", err)
	}
	if raw := obj["reclaim_reason"]; raw != nil {
		reason, ok := raw.(string)
		if !ok || (reason != "overdue" && reason != "operator") {
			return nil, errors.New("reclaim_reason must be null, overdue, or operator")
		}
		c.ReclaimReason = &reason
	}
	if (c.ReclaimOf == nil) != (c.ReclaimReason == nil) {
		return nil, errors.New("reclaim_of and reclaim_reason must both be null or both be set")
	}
	switch obligation.Preimage.Kind {
	case "author":
		if !c.TipAtClaimPresent || c.RevisionAtClaim != nil || c.CoordinatorConfig != nil {
			return nil, errors.New("author claim requires only tip_at_claim")
		}
	case "gate":
		if c.RevisionAtClaim == nil || c.TipAtClaimPresent || c.CoordinatorConfig != nil {
			return nil, errors.New("gate claim requires only revision_at_claim")
		}
	case "merge":
		if c.RevisionAtClaim == nil || c.CoordinatorConfig == nil || c.TipAtClaimPresent {
			return nil, errors.New("merge claim requires revision_at_claim and coordinator_config")
		}
	}
	return c, nil
}

func parseVerdict(obj map[string]any, actor Actor, createdAt time.Time) (*Verdict, error) {
	allowed := []string{
		"schema_version", "type", "actor", "created_at", "obligation", "claim",
		"pr", "revision", "decision", "report", "scope", "completeness", "incomplete_reason",
	}
	if err := exactObject(obj, allowed,
		"schema_version", "type", "actor", "created_at", "obligation", "claim",
		"pr", "revision", "decision", "report", "scope", "completeness"); err != nil {
		return nil, err
	}
	obligation, err := parseObligation(obj["obligation"])
	if err != nil {
		return nil, err
	}
	if obligation.Preimage.Kind != "gate" {
		return nil, errors.New("verdict obligation kind must be gate")
	}
	claim, ok := obj["claim"].(string)
	if !ok || !ValidOID(claim) {
		return nil, errors.New("claim must be a full Git OID")
	}
	pr64, err := integer(obj["pr"])
	if err != nil || pr64 < 1 {
		return nil, errors.New("pr must be a positive integer")
	}
	revision, err := parseRevision(obj["revision"])
	if err != nil {
		return nil, fmt.Errorf("revision: %w", err)
	}
	decision, ok := obj["decision"].(string)
	if !ok || (decision != "APPROVE" && decision != "REQUEST_CHANGES") {
		return nil, errors.New("decision must be APPROVE or REQUEST_CHANGES")
	}
	report, err := parseSource(obj["report"])
	if err != nil {
		return nil, fmt.Errorf("report: %w", err)
	}
	scope, err := parseScope(obj["scope"])
	if err != nil {
		return nil, fmt.Errorf("scope: %w", err)
	}
	completeness, ok := obj["completeness"].(string)
	if !ok || (completeness != "COMPLETE" && completeness != "INCOMPLETE") {
		return nil, errors.New("completeness must be COMPLETE or INCOMPLETE")
	}
	incompleteReason := ""
	if raw, present := obj["incomplete_reason"]; present {
		var ok bool
		incompleteReason, ok = raw.(string)
		if !ok {
			return nil, errors.New("incomplete_reason must be a string")
		}
	}
	if completeness == "INCOMPLETE" && incompleteReason == "" {
		return nil, errors.New("INCOMPLETE verdict requires incomplete_reason")
	}
	return &Verdict{
		SchemaVersion:    SchemaVersion,
		Actor:            actor,
		CreatedAt:        createdAt,
		Obligation:       obligation,
		Claim:            claim,
		PR:               int(pr64),
		Revision:         revision,
		Decision:         decision,
		Report:           report,
		Scope:            scope,
		Completeness:     completeness,
		IncompleteReason: incompleteReason,
	}, nil
}

func parseCancel(obj map[string]any, actor Actor, createdAt time.Time) (*Cancel, error) {
	if err := exactObject(obj,
		[]string{"schema_version", "type", "actor", "created_at", "target", "reason", "authority"},
		"schema_version", "type", "actor", "created_at", "target", "reason", "authority"); err != nil {
		return nil, err
	}
	reason, ok := obj["reason"].(string)
	if !ok || reason == "" {
		return nil, errors.New("reason must be a non-empty string")
	}
	authority, ok := obj["authority"].(map[string]any)
	if !ok || exactObject(authority, []string{"kind"}, "kind") != nil || authority["kind"] != "user" {
		return nil, errors.New("authority must be exactly {kind:user}")
	}
	targetObj, ok := obj["target"].(map[string]any)
	if !ok {
		return nil, errors.New("target must be an object")
	}
	kind, ok := targetObj["kind"].(string)
	if !ok {
		return nil, errors.New("target.kind must be a string")
	}
	target := CancelTarget{Kind: kind}
	switch kind {
	case "assignment":
		if err := exactObject(targetObj, []string{"kind", "assignment"}, "kind", "assignment"); err != nil {
			return nil, err
		}
		source, err := parseSource(targetObj["assignment"])
		if err != nil {
			return nil, err
		}
		target.Assignment = &source
	case "obligation":
		if err := exactObject(targetObj, []string{"kind", "obligation"}, "kind", "obligation"); err != nil {
			return nil, err
		}
		id, ok := targetObj["obligation"].(string)
		if !ok || !ValidObligationID(id) {
			return nil, errors.New("target obligation is invalid")
		}
		target.Obligation = id
	case "claim":
		if err := exactObject(targetObj, []string{"kind", "claim"}, "kind", "claim"); err != nil {
			return nil, err
		}
		oid, ok := targetObj["claim"].(string)
		if !ok || !ValidOID(oid) {
			return nil, errors.New("target claim is invalid")
		}
		target.Claim = oid
	default:
		return nil, fmt.Errorf("invalid cancel target kind %q", kind)
	}
	return &Cancel{
		SchemaVersion: SchemaVersion,
		Actor:         actor,
		CreatedAt:     createdAt,
		Target:        target,
		Reason:        reason,
	}, nil
}

func parseActor(raw any) (Actor, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return Actor{}, errors.New("must be an object")
	}
	if err := exactObject(obj, []string{"seat", "machine", "session"}, "seat", "machine", "session"); err != nil {
		return Actor{}, err
	}
	seat, ok := obj["seat"].(string)
	if !ok {
		return Actor{}, errors.New("seat must be a string")
	}
	machine, ok := obj["machine"].(string)
	if !ok {
		return Actor{}, errors.New("machine must be a string")
	}
	var session *string
	if obj["session"] != nil {
		value, ok := obj["session"].(string)
		if !ok {
			return Actor{}, errors.New("session must be a string or null")
		}
		session = &value
	}
	actor := Actor{Seat: seat, Machine: machine, Session: session}
	return actor, ValidateActor(actor)
}

func parseObligation(raw any) (Obligation, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return Obligation{}, errors.New("obligation must be an object")
	}
	if err := exactObject(obj, []string{"preimage", "id", "source"}, "preimage", "id"); err != nil {
		return Obligation{}, err
	}
	preimageObj, ok := obj["preimage"].(map[string]any)
	if !ok {
		return Obligation{}, errors.New("obligation.preimage must be an object")
	}
	if err := exactObject(preimageObj, []string{"assignment", "kind", "target", "aspect"}, "assignment", "kind", "target", "aspect"); err != nil {
		return Obligation{}, err
	}
	assignmentObj, ok := preimageObj["assignment"].(map[string]any)
	if !ok {
		return Obligation{}, errors.New("preimage.assignment must be an object")
	}
	var assignment AssignmentIdentity
	if _, static := assignmentObj["id"]; static {
		if err := exactObject(assignmentObj, []string{"id", "blob"}, "id", "blob"); err != nil {
			return Obligation{}, err
		}
		assignment.ID, _ = assignmentObj["id"].(string)
		assignment.Blob, _ = assignmentObj["blob"].(string)
	} else {
		if err := exactObject(assignmentObj, []string{"assign_event"}, "assign_event"); err != nil {
			return Obligation{}, err
		}
		assignment.AssignEvent, _ = assignmentObj["assign_event"].(string)
	}
	kind, _ := preimageObj["kind"].(string)
	target, _ := preimageObj["target"].(string)
	var aspect *GateAspect
	if preimageObj["aspect"] != nil {
		aspectObj, ok := preimageObj["aspect"].(map[string]any)
		if !ok {
			return Obligation{}, errors.New("preimage.aspect must be null or an object")
		}
		if err := exactObject(aspectObj, []string{"gate_id", "gater_seat"}, "gate_id", "gater_seat"); err != nil {
			return Obligation{}, err
		}
		gateID, _ := aspectObj["gate_id"].(string)
		gaterSeat, _ := aspectObj["gater_seat"].(string)
		aspect = &GateAspect{GateID: gateID, GaterSeat: gaterSeat}
	}
	id, _ := obj["id"].(string)
	obligation := Obligation{
		Preimage: ObligationPreimage{
			Assignment: assignment,
			Kind:       kind,
			Target:     target,
			Aspect:     aspect,
		},
		ID: id,
	}
	if rawSource, present := obj["source"]; present {
		source, err := parseSource(rawSource)
		if err != nil {
			return Obligation{}, err
		}
		obligation.Source = &source
	}
	if assignment.AssignEvent != "" {
		return Obligation{}, &UnsupportedError{Reason: "dynamic assignment obligations are reserved for P2"}
	}
	return obligation, ValidateObligation(obligation)
}

func parseSource(raw any) (Source, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return Source{}, errors.New("source must be an object")
	}
	if err := exactObject(obj, []string{"commit", "path", "blob"}, "commit", "path", "blob"); err != nil {
		return Source{}, err
	}
	source := Source{}
	source.Commit, _ = obj["commit"].(string)
	source.Path, _ = obj["path"].(string)
	source.Blob, _ = obj["blob"].(string)
	return source, ValidateSource(source)
}

func parseRevision(raw any) (Revision, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return Revision{}, errors.New("revision must be an object")
	}
	if err := exactObject(obj, []string{"base", "head"}, "base", "head"); err != nil {
		return Revision{}, err
	}
	revision := Revision{}
	revision.Base, _ = obj["base"].(string)
	revision.Head, _ = obj["head"].(string)
	return revision, ValidateRevision(revision)
}

func parseScope(raw any) (ReviewScope, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return ReviewScope{}, errors.New("must be an object")
	}
	kind, ok := obj["kind"].(string)
	if !ok {
		return ReviewScope{}, errors.New("kind must be a string")
	}
	switch kind {
	case "full":
		if err := exactObject(obj, []string{"kind"}, "kind"); err != nil {
			return ReviewScope{}, err
		}
		return ReviewScope{Kind: "full"}, nil
	case "composite":
		if err := exactObject(obj, []string{"kind", "parts"}, "kind", "parts"); err != nil {
			return ReviewScope{}, err
		}
		rawParts, ok := obj["parts"].([]any)
		if !ok || len(rawParts) == 0 {
			return ReviewScope{}, errors.New("composite parts must be a non-empty array")
		}
		scope := ReviewScope{Kind: "composite"}
		for _, rawPart := range rawParts {
			partObj, ok := rawPart.(map[string]any)
			if !ok {
				return ReviewScope{}, errors.New("scope part must be an object")
			}
			partKind, _ := partObj["kind"].(string)
			switch partKind {
			case "fix_verification":
				if err := exactObject(partObj, []string{"kind", "findings", "target_blob"}, "kind", "findings", "target_blob"); err != nil {
					return ReviewScope{}, err
				}
				rawFindings, ok := partObj["findings"].([]any)
				if !ok || len(rawFindings) == 0 {
					return ReviewScope{}, errors.New("fix_verification findings must be non-empty")
				}
				seen := map[string]bool{}
				part := ScopePart{Kind: partKind}
				for _, rawFinding := range rawFindings {
					finding, ok := rawFinding.(string)
					if !ok || seen[finding] {
						return ReviewScope{}, errors.New("findings must be unique strings")
					}
					seen[finding] = true
					part.Findings = append(part.Findings, finding)
				}
				part.TargetBlob, _ = partObj["target_blob"].(string)
				if !ValidOID(part.TargetBlob) {
					return ReviewScope{}, errors.New("invalid target_blob")
				}
				scope.Parts = append(scope.Parts, part)
			case "delta":
				if err := exactObject(partObj, []string{"kind", "from_blob", "to_blob"}, "kind", "from_blob", "to_blob"); err != nil {
					return ReviewScope{}, err
				}
				part := ScopePart{Kind: partKind}
				part.FromBlob, _ = partObj["from_blob"].(string)
				part.ToBlob, _ = partObj["to_blob"].(string)
				if !ValidOID(part.FromBlob) || !ValidOID(part.ToBlob) {
					return ReviewScope{}, errors.New("invalid delta blob OID")
				}
				scope.Parts = append(scope.Parts, part)
			default:
				return ReviewScope{}, fmt.Errorf("invalid scope part kind %q", partKind)
			}
		}
		return scope, nil
	default:
		return ReviewScope{}, fmt.Errorf("invalid scope kind %q", kind)
	}
}

// ParseReviewScope applies the same strict I-JSON and schema checks used for a
// VERDICT payload to a standalone scope value supplied by the CLI.
func ParseReviewScope(raw []byte) (ReviewScope, error) {
	value, err := jcs.Parse(raw)
	if err != nil {
		return ReviewScope{}, err
	}
	return parseScope(value)
}

func exactObject(obj map[string]any, allowed []string, required ...string) error {
	allow := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		allow[key] = true
	}
	for key := range obj {
		if !allow[key] {
			return fmt.Errorf("unknown property %q", key)
		}
	}
	for _, key := range required {
		if _, present := obj[key]; !present {
			return fmt.Errorf("required property %q is missing", key)
		}
	}
	return nil
}

func integer(raw any) (int64, error) {
	number, ok := raw.(json.Number)
	if !ok {
		return 0, errors.New("must be an integer")
	}
	value, err := strconv.ParseInt(string(number), 10, 64)
	if err != nil {
		return 0, errors.New("must be an integer")
	}
	return value, nil
}

func dateTime(raw any) (time.Time, error) {
	value, ok := raw.(string)
	if !ok {
		return time.Time{}, errors.New("must be a string")
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, errors.New("must be an RFC3339 date-time")
	}
	if _, offset := parsed.Zone(); offset != 0 {
		return time.Time{}, errors.New("must be an RFC3339 UTC date-time")
	}
	return parsed, nil
}

func nullableOID(raw any) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	value, ok := raw.(string)
	if !ok || !ValidOID(value) {
		return nil, errors.New("must be null or a full Git OID")
	}
	return &value, nil
}
