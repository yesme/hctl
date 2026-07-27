package derive

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/yesme/hctl/internal/config"
	"github.com/yesme/hctl/internal/facts"
	"github.com/yesme/hctl/internal/gitx"
	"github.com/yesme/hctl/internal/protocol"
)

type ClaimState struct {
	OID               string             `json:"oid"`
	Actor             protocol.Actor     `json:"actor"`
	CreatedAt         time.Time          `json:"created_at"`
	Revision          *protocol.Revision `json:"revision,omitempty"`
	Tip               *string            `json:"tip,omitempty"`
	ReclaimOf         *string            `json:"reclaim_of,omitempty"`
	ReclaimReason     *string            `json:"reclaim_reason,omitempty"`
	Stale             bool               `json:"stale"`
	Overdue           bool               `json:"overdue"`
	Streak            int                `json:"reclaim_streak"`
	Escalated         bool               `json:"escalated"`
	LastProgress      string             `json:"last_progress,omitempty"`
	CoordinatorConfig *protocol.Source   `json:"coordinator_config,omitempty"`
}

type VerdictState struct {
	OID              string               `json:"oid"`
	Decision         string               `json:"decision"`
	Completeness     string               `json:"completeness"`
	IncompleteReason string               `json:"incomplete_reason,omitempty"`
	Scope            protocol.ReviewScope `json:"scope"`
	Report           protocol.Source      `json:"report"`
	PR               int                  `json:"pr"`
	Covers           bool                 `json:"covers"`
	Valid            bool                 `json:"valid"`
	Reason           string               `json:"reason,omitempty"`
}

type ObligationState struct {
	Obligation       protocol.Obligation     `json:"obligation"`
	Assignment       string                  `json:"assignment"`
	AssignmentPath   string                  `json:"assignment_path"`
	Kind             string                  `json:"kind"`
	GateID           string                  `json:"gate_id,omitempty"`
	GateMode         string                  `json:"gate_mode,omitempty"`
	Threshold        string                  `json:"threshold,omitempty"`
	Holder           string                  `json:"holder"`
	BranchRef        string                  `json:"branch_ref"`
	Revision         *protocol.Revision      `json:"revision,omitempty"`
	PR               int                     `json:"pr,omitempty"`
	TimeoutSeconds   int64                   `json:"timeout_seconds"`
	Claim            *ClaimState             `json:"claim,omitempty"`
	Verdict          *VerdictState           `json:"verdict,omitempty"`
	State            string                  `json:"state"`
	NextAction       string                  `json:"next_action"`
	Canceled         bool                    `json:"canceled"`
	Completed        bool                    `json:"completed"`
	Green            bool                    `json:"green"`
	MergedCommit     string                  `json:"merged_commit,omitempty"`
	AuditDebt        string                  `json:"audit_debt,omitempty"`
	AssignmentRecord config.AssignmentRecord `json:"-"`
	Gate             *config.Gate            `json:"-"`
}

type Result struct {
	ObservedAt  time.Time          `json:"observed_at"`
	Seat        string             `json:"seat,omitempty"`
	Complete    bool               `json:"complete"`
	Problems    []facts.Problem    `json:"problems"`
	FactTips    map[string]string  `json:"fact_tips"`
	Obligations []*ObligationState `json:"obligations"`
}

func (r *Result) Find(id string) (*ObligationState, bool) {
	for _, obligation := range r.Obligations {
		if obligation.Obligation.ID == id {
			return obligation, true
		}
	}
	return nil, false
}

func (r *Result) WriteGuard() error {
	for _, problem := range r.Problems {
		if problem.Severity == facts.SeverityError {
			return fmt.Errorf("%s: %s", problem.Code, problem.Detail)
		}
	}
	if !r.Complete {
		return fmt.Errorf("INCOMPLETE_FACTS: derive result is incomplete")
	}
	return nil
}

func (r *Result) Attention(seat string) []*ObligationState {
	var out []*ObligationState
	for _, obligation := range r.Obligations {
		if obligation.Holder != seat || obligation.Completed || obligation.Canceled {
			continue
		}
		switch obligation.State {
		case "claimable", "stale", "overdue", "claimed", "review_incomplete":
			out = append(out, obligation)
		}
	}
	return out
}

type Engine struct {
	Repo           *gitx.Repo
	Now            func() time.Time
	SkipMergeAudit bool
}

func (e Engine) Derive(ctx context.Context, snapshot *facts.Snapshot, currentSeat string) *Result {
	now := time.Now
	if e.Now != nil {
		now = e.Now
	}
	result := &Result{
		ObservedAt: snapshot.ObservedAt,
		Seat:       currentSeat,
		Complete:   snapshot.Complete(),
		Problems:   append([]facts.Problem(nil), snapshot.Problems...),
		FactTips:   cloneMap(snapshot.FactTips),
	}
	if snapshot.Seats.Config.SchemaVersion != 1 {
		return result
	}
	byID := map[string]*ObligationState{}
	assignments := append([]config.AssignmentRecord(nil), snapshot.Assignments...)
	sort.Slice(assignments, func(i, j int) bool {
		return assignments[i].Assignment.ID < assignments[j].Assignment.ID
	})
	for _, record := range assignments {
		states, err := deriveStaticObligations(record, snapshot.Seats.Config)
		if err != nil {
			result.Problems = append(result.Problems, facts.Problem{
				Code: "INVALID_CONFIG", Severity: facts.SeverityError, Detail: err.Error(),
			})
			result.Complete = false
			continue
		}
		base := snapshot.BranchTips[record.Assignment.BaseRef]
		head := snapshot.BranchTips[record.BranchRef()]
		pr, ambiguous := pullForHead(snapshot.PullTips, head)
		if ambiguous {
			result.Problems = append(result.Problems, facts.Problem{
				Code: "AMBIGUOUS_PR", Severity: facts.SeverityError,
				Detail: fmt.Sprintf("assignment %q head %s belongs to multiple pull refs", record.Assignment.ID, head),
			})
		}
		for _, state := range states {
			state.BranchRef = record.BranchRef()
			if base != "" && head != "" {
				state.Revision = &protocol.Revision{Base: base, Head: head}
				state.PR = pr
			}
			result.Obligations = append(result.Obligations, state)
			byID[state.Obligation.ID] = state
		}
	}

	// A current obligation is owned by exactly one seat. A known event for it
	// on any other seat chain is a semantic integrity failure, not a competing
	// vote or claim.
	for _, chainSeat := range sortedChainSeats(snapshot.Chains) {
		chain := snapshot.Chains[chainSeat]
		for _, event := range chain.Events {
			var obligationID string
			switch {
			case event.Claim != nil:
				obligationID = event.Claim.Obligation.ID
			case event.Verdict != nil:
				obligationID = event.Verdict.Obligation.ID
			}
			state := byID[obligationID]
			historicalMergeClaim := event.Claim != nil &&
				event.Claim.Obligation.Preimage.Kind == "merge" &&
				event.Claim.RevisionAtClaim != nil &&
				event.Claim.RevisionAtClaim.Base != snapshot.MainCommit
			if state != nil && state.Holder != chainSeat && !historicalMergeClaim {
				addInvalid(result, chainSeat, fmt.Sprintf(
					"event %s for obligation %s is on %s chain; assigned holder is %s",
					event.OID, obligationID, chainSeat, state.Holder,
				))
			}
		}
	}

	canceledObligations := map[string]bool{}
	canceledClaims := map[string]bool{}
	var canceledAssignments []protocol.Source
	for _, chainSeat := range sortedChainSeats(snapshot.Chains) {
		chain := snapshot.Chains[chainSeat]
		for _, event := range chain.Events {
			if event.Cancel == nil {
				continue
			}
			switch event.Cancel.Target.Kind {
			case "obligation":
				canceledObligations[event.Cancel.Target.Obligation] = true
			case "claim":
				canceledClaims[event.Cancel.Target.Claim] = true
			case "assignment":
				if event.Cancel.Target.Assignment != nil {
					canceledAssignments = append(canceledAssignments, *event.Cancel.Target.Assignment)
				}
			}
		}
	}

	for _, state := range result.Obligations {
		if canceledObligations[state.Obligation.ID] ||
			e.assignmentCanceled(snapshot, state.AssignmentRecord, canceledAssignments) {
			state.Canceled = true
		}
		acceptedVerdicts := map[string]bool{}
		e.replayClaims(snapshot, result, state, canceledClaims, acceptedVerdicts)
		e.selectVerdict(snapshot, state, acceptedVerdicts)
	}
	e.markMovedAssignments(snapshot, result)
	if !e.SkipMergeAudit && snapshot.MainCommit != "" {
		e.applyMergeHistory(ctx, snapshot, result)
	}
	e.validateCapacities(result)
	e.evaluateStates(snapshot, result, now().UTC())
	result.Complete = result.Complete && noErrors(result.Problems)
	return result
}

func deriveStaticObligations(record config.AssignmentRecord, seats config.Seats) ([]*ObligationState, error) {
	a := record.Assignment
	author, err := protocol.NewStaticObligation(a.ID, record.Source.Blob, "author", a.Author.BranchSlug, nil, record.Source)
	if err != nil {
		return nil, err
	}
	states := []*ObligationState{{
		Obligation: author, Assignment: a.ID, AssignmentPath: record.Source.Path,
		Kind: "author", Holder: a.Author.Seat, TimeoutSeconds: a.Author.ClaimTimeoutSeconds,
		AssignmentRecord: record,
	}}
	for i := range a.Gates {
		gate := &a.Gates[i]
		leaves, err := config.ValidateQuorum(gate.Requirement, seats)
		if err != nil {
			return nil, err
		}
		sort.Strings(leaves)
		for _, seat := range leaves {
			obligation, err := protocol.NewStaticObligation(
				a.ID, record.Source.Blob, "gate", a.Author.BranchSlug,
				&protocol.GateAspect{GateID: gate.ID, GaterSeat: seat}, record.Source,
			)
			if err != nil {
				return nil, err
			}
			gateCopy := *gate
			states = append(states, &ObligationState{
				Obligation: obligation, Assignment: a.ID, AssignmentPath: record.Source.Path,
				Kind: "gate", GateID: gate.ID, GateMode: gate.Mode, Threshold: gate.Threshold,
				Holder: seat, TimeoutSeconds: gate.ClaimTimeoutSeconds,
				AssignmentRecord: record, Gate: &gateCopy,
			})
		}
	}
	merge, err := protocol.NewStaticObligation(a.ID, record.Source.Blob, "merge", a.Author.BranchSlug, nil, record.Source)
	if err != nil {
		return nil, err
	}
	states = append(states, &ObligationState{
		Obligation: merge, Assignment: a.ID, AssignmentPath: record.Source.Path,
		Kind: "merge", Holder: seats.MergeCoordinator, TimeoutSeconds: a.Author.ClaimTimeoutSeconds,
		AssignmentRecord: record,
	})
	return states, nil
}

func (e Engine) replayClaims(
	snapshot *facts.Snapshot,
	result *Result,
	state *ObligationState,
	canceledClaims map[string]bool,
	acceptedVerdicts map[string]bool,
) {
	chain := snapshot.Chains[state.Holder]
	if chain == nil {
		return
	}
	var active *ClaimState
	streak := 0
	escalated := false
	for _, event := range chain.Events {
		if event.Cancel != nil && event.Cancel.Target.Kind == "claim" {
			if active != nil && event.Cancel.Target.Claim == active.OID {
				active = nil
				streak = 0
				escalated = false
			}
			continue
		}
		if event.Verdict != nil && event.Verdict.Obligation.ID == state.Obligation.ID {
			v := event.Verdict
			if snapshot.MissingCommits[v.Report.Commit] {
				addIncomplete(result, state.Holder, fmt.Sprintf("verdict %s report commit is unavailable", event.OID))
				continue
			}
			reason := ""
			switch {
			case state.Kind != "gate":
				reason = "verdict targets a non-gate obligation"
			case !sameObligation(v.Obligation, state.Obligation):
				reason = "obligation wire differs from derived obligation"
			case !e.obligationSourceValid(snapshot, v.Obligation, state.AssignmentRecord):
				reason = "assignment source is not a reachable matching blob"
			case v.Actor.Seat != state.Holder:
				reason = "actor is not the gate holder"
			case active == nil:
				reason = "there is no active claim"
			case v.Claim != active.OID:
				reason = "claim is not the active fencing token at this chain position"
			case active.Escalated:
				reason = "claim is escalated and frozen"
			case active.Revision == nil || v.Revision != *active.Revision:
				reason = "revision does not match the active claim"
			case snapshot.PullTips[v.PR] != v.Revision.Head:
				reason = "PR does not identify the exact verdict head"
			case !e.reportReachableAt(snapshot, v.Report, v.Revision.Base):
				reason = "report source is not reachable from revision base or blob does not match"
			}
			if reason != "" {
				addInvalid(result, state.Holder, fmt.Sprintf("verdict %s: %s", event.OID, reason))
				continue
			}
			acceptedVerdicts[event.OID] = true
			if v.Completeness == "COMPLETE" {
				// A completed attempt starts a fresh damping window. It cannot
				// thaw escalation because an escalated verdict was rejected
				// above; only a completion accepted before escalation resets.
				streak = 0
				escalated = false
				active.Streak = 0
				active.Escalated = false
			}
			continue
		}
		if event.Claim == nil || event.Claim.Obligation.ID != state.Obligation.ID {
			continue
		}
		claim := event.Claim
		if !sameObligation(claim.Obligation, state.Obligation) {
			addInvalid(result, state.Holder, fmt.Sprintf("claim %s obligation wire differs from derived obligation", event.OID))
			continue
		}
		if !e.obligationSourceValid(snapshot, claim.Obligation, state.AssignmentRecord) {
			addInvalid(result, state.Holder, fmt.Sprintf("claim %s assignment source is not a reachable matching blob", event.OID))
			continue
		}
		if claim.Actor.Seat != state.Holder {
			addInvalid(result, state.Holder, fmt.Sprintf("claim %s actor is not obligation holder", event.OID))
			continue
		}
		if state.Kind == "merge" &&
			claim.RevisionAtClaim != nil &&
			claim.RevisionAtClaim.Base == snapshot.MainCommit &&
			(claim.CoordinatorConfig == nil || *claim.CoordinatorConfig != snapshot.Seats.Source) {
			addInvalid(result, state.Holder, fmt.Sprintf("claim %s coordinator_config does not match current main", event.OID))
			continue
		}
		next := &ClaimState{
			OID: event.OID, Actor: claim.Actor, CreatedAt: claim.CreatedAt,
			Revision: claim.RevisionAtClaim, Tip: claim.TipAtClaim,
			ReclaimOf: claim.ReclaimOf, ReclaimReason: claim.ReclaimReason,
			CoordinatorConfig: claim.CoordinatorConfig,
		}
		switch state.Kind {
		case "author":
			if next.Tip != nil {
				if snapshot.MissingCommits[*next.Tip] {
					addIncomplete(result, state.Holder, fmt.Sprintf("claim %s tip_at_claim object is unavailable", event.OID))
					continue
				}
				if !snapshot.CommitKnown(*next.Tip) {
					addInvalid(result, state.Holder, fmt.Sprintf("claim %s tip_at_claim is not a commit", event.OID))
					continue
				}
			}
		case "gate", "merge":
			baseTip := snapshot.BranchTips[state.AssignmentRecord.Assignment.BaseRef]
			if next.Revision != nil &&
				(snapshot.MissingCommits[next.Revision.Base] || snapshot.MissingCommits[next.Revision.Head]) {
				addIncomplete(result, state.Holder, fmt.Sprintf("claim %s revision object is unavailable", event.OID))
				continue
			}
			if next.Revision == nil ||
				!snapshot.CommitKnown(next.Revision.Base) ||
				!snapshot.CommitKnown(next.Revision.Head) ||
				!snapshot.IsAncestor(next.Revision.Base, baseTip) {
				addInvalid(result, state.Holder, fmt.Sprintf("claim %s revision anchors are unavailable or base is not on the assignment base branch", event.OID))
				continue
			}
		}
		if active == nil {
			if claim.ReclaimOf != nil {
				addInvalid(result, state.Holder, fmt.Sprintf("claim %s reclaims without a current claim", event.OID))
				continue
			}
			streak = 0
			escalated = false
		} else {
			if claim.ReclaimOf == nil || *claim.ReclaimOf != active.OID {
				addInvalid(result, state.Holder, fmt.Sprintf("claim %s does not fence active claim %s", event.OID, active.OID))
				continue
			}
			if escalated {
				addInvalid(result, state.Holder, fmt.Sprintf("claim %s attempts to reclaim an escalated obligation", event.OID))
				continue
			}
			if claim.ReclaimReason != nil && *claim.ReclaimReason == "overdue" &&
				claim.CreatedAt.Before(active.CreatedAt.Add(time.Duration(state.TimeoutSeconds)*time.Second)) {
				addInvalid(result, state.Holder, fmt.Sprintf("claim %s asserts overdue before the timeout boundary", event.OID))
				continue
			}
			class, progress, err := e.classifyProgress(snapshot, state.Kind, active, next)
			if err != nil {
				addInvalid(result, state.Holder, fmt.Sprintf("claim %s progress cannot be classified: %v", event.OID, err))
				continue
			}
			next.LastProgress = class
			if progress {
				streak = 0
			} else {
				streak++
				if streak >= 2 {
					escalated = true
				}
			}
		}
		next.Streak = streak
		next.Escalated = escalated
		active = next
	}
	// A CANCEL on another seat chain has no cross-ref total order. It can
	// safely clear only the claim that is still current at the final snapshot;
	// canceling an already-fenced ancestor never invalidates its descendants.
	if active != nil && canceledClaims[active.OID] {
		active = nil
		streak = 0
		escalated = false
	}
	if active == nil {
		return
	}
	switch state.Kind {
	case "gate", "merge":
		active.Stale = state.Revision == nil || active.Revision == nil || *active.Revision != *state.Revision
	case "author":
		active.Stale = false
	}
	state.Claim = active
}

func (e Engine) classifyProgress(snapshot *facts.Snapshot, kind string, old, next *ClaimState) (string, bool, error) {
	if kind == "author" {
		if old.Tip == nil && next.Tip != nil {
			return "forward", true, nil
		}
		if old.Tip == nil || next.Tip == nil {
			return "no-progress", false, nil
		}
		if *old.Tip == *next.Tip {
			return "no-progress", false, nil
		}
		if !snapshot.CommitKnown(*old.Tip) || !snapshot.CommitKnown(*next.Tip) {
			return "", false, fmt.Errorf("author progress anchor is unavailable")
		}
		if snapshot.IsAncestor(*old.Tip, *next.Tip) {
			return "forward", true, nil
		}
		return "rewrite-or-rollback", false, nil
	}
	if old.Revision == nil || next.Revision == nil {
		return "no-progress", false, nil
	}
	if old.Revision.Head == next.Revision.Head && old.Revision.Base != next.Revision.Base {
		return "environment-reset", true, nil
	}
	if *old.Revision == *next.Revision {
		return "no-progress", false, nil
	}
	baseForward := old.Revision.Base == next.Revision.Base
	if !baseForward {
		if !snapshot.CommitKnown(old.Revision.Base) || !snapshot.CommitKnown(next.Revision.Base) {
			return "", false, fmt.Errorf("base progress anchor is unavailable")
		}
		baseForward = snapshot.IsAncestor(old.Revision.Base, next.Revision.Base)
	}
	headForward := old.Revision.Head == next.Revision.Head
	if !headForward {
		if !snapshot.CommitKnown(old.Revision.Head) || !snapshot.CommitKnown(next.Revision.Head) {
			return "", false, fmt.Errorf("head progress anchor is unavailable")
		}
		headForward = snapshot.IsAncestor(old.Revision.Head, next.Revision.Head)
	}
	if baseForward && headForward {
		return "forward", true, nil
	}
	return "rewrite-or-rollback", false, nil
}

func (e Engine) selectVerdict(
	snapshot *facts.Snapshot,
	state *ObligationState,
	acceptedVerdicts map[string]bool,
) {
	if state.Kind != "gate" || state.Revision == nil {
		return
	}
	chain := snapshot.Chains[state.Holder]
	if chain == nil {
		return
	}
	var latest *protocol.Event
	for _, event := range chain.Events {
		if event.Verdict == nil || event.Verdict.Obligation.ID != state.Obligation.ID {
			continue
		}
		if event.Verdict.Revision != *state.Revision {
			continue
		}
		latest = event
	}
	if latest == nil {
		return
	}
	v := latest.Verdict
	verdict := &VerdictState{
		OID: latest.OID, Decision: v.Decision, Completeness: v.Completeness,
		IncompleteReason: v.IncompleteReason,
		Scope:            v.Scope, Report: v.Report, PR: v.PR,
	}
	state.Verdict = verdict
	switch {
	case !acceptedVerdicts[latest.OID]:
		verdict.Reason = "verdict was not accepted by the chain state machine"
	case state.Claim == nil:
		verdict.Reason = "claim is no longer active"
	case v.Claim != state.Claim.OID:
		verdict.Reason = "claim has been fenced by a newer claim"
	case state.Claim.Escalated:
		verdict.Reason = "claim is escalated and frozen"
	case state.Claim.Stale:
		verdict.Reason = "claim is stale"
	case state.PR != 0 && v.PR != state.PR:
		verdict.Reason = "PR does not match the exact head"
	default:
		verdict.Valid = true
		verdict.Covers = scopeCovers(v.Scope)
	}
}

func (e Engine) reportReachableAt(snapshot *facts.Snapshot, report protocol.Source, base string) bool {
	return protocol.ValidMemoPath(report.Path) &&
		snapshot.SourceMatches(report) &&
		snapshot.IsAncestor(report.Commit, base)
}

func (e Engine) evaluateStates(snapshot *facts.Snapshot, result *Result, now time.Time) {
	byAssignment := map[string][]*ObligationState{}
	for _, state := range result.Obligations {
		byAssignment[state.Assignment] = append(byAssignment[state.Assignment], state)
		if state.Claim != nil {
			state.Claim.Overdue = !state.Claim.Stale &&
				now.After(state.Claim.CreatedAt.Add(time.Duration(state.TimeoutSeconds)*time.Second))
		}
	}
	for assignment, states := range byAssignment {
		votesByGate := map[string]map[string]bool{}
		var record config.AssignmentRecord
		for _, state := range states {
			record = state.AssignmentRecord
			if state.Kind != "gate" {
				continue
			}
			if votesByGate[state.GateID] == nil {
				votesByGate[state.GateID] = map[string]bool{}
			}
			votesByGate[state.GateID][state.Holder] =
				state.Verdict != nil && state.Verdict.Valid && state.Verdict.Covers &&
					state.Verdict.Decision == "APPROVE" && state.Verdict.Completeness == "COMPLETE"
			state.Green = votesByGate[state.GateID][state.Holder]
		}
		requiredGreen := true
		for _, gate := range record.Assignment.Gates {
			if gate.Mode != "required" {
				continue
			}
			if !EvaluateQuorum(gate.Requirement, votesByGate[gate.ID]) {
				requiredGreen = false
			}
		}
		for _, state := range states {
			if state.Kind == "merge" {
				state.Green = requiredGreen
			}
		}
		_ = assignment
	}

	activeMerge := 0
	for _, state := range result.Obligations {
		if state.Kind == "merge" && state.Claim != nil && !state.Claim.Stale &&
			!state.Canceled && !state.Completed && state.AuditDebt == "" {
			activeMerge++
		}
	}
	for _, state := range result.Obligations {
		switch {
		case state.Completed:
			state.State = "completed"
			state.NextAction = "none"
		case state.Canceled:
			state.State = "canceled"
			state.NextAction = "user must issue a new assignment revision to revive this obligation"
		case state.AuditDebt != "":
			state.State = strings.ToLower(state.AuditDebt)
			state.NextAction = "escalate merge audit debt; P1 never synthesizes a receipt or guesses missing facts"
		case state.Kind == "gate" && state.Green:
			state.State = "satisfied"
			state.NextAction = "wait for required quorum and merge; publish a newer verdict only for a changed decision"
		case state.Claim == nil && state.Kind != "author" && state.Revision == nil:
			state.State = "waiting_head"
			state.NextAction = fmt.Sprintf("wait for %s to be published", state.BranchRef)
		case state.Claim == nil && state.Kind != "author" && state.PR == 0:
			state.State = "waiting_pr"
			state.NextAction = "author must open a PR for the exact head"
		case state.Claim != nil && state.Claim.Escalated:
			state.State = "escalated"
			state.NextAction = fmt.Sprintf("user CANCEL claim %s, CANCEL obligation, or issue a new assignment revision", state.Claim.OID)
		case state.Claim != nil && state.Claim.Stale:
			state.State = "stale"
			if state.Kind == "merge" && !state.Green {
				state.NextAction = fmt.Sprintf(
					"required gates must re-green, then hctl claim %s --reclaim %s",
					state.Obligation.ID, state.Claim.OID,
				)
			} else {
				state.NextAction = fmt.Sprintf("hctl claim %s --reclaim %s", state.Obligation.ID, state.Claim.OID)
			}
		case state.Kind == "gate" && state.Verdict != nil && state.Verdict.Valid &&
			(state.Verdict.Completeness != "COMPLETE" || !state.Verdict.Covers):
			state.State = "review_incomplete"
			state.NextAction = "finish the uncovered review surface and publish a newer exact-revision verdict"
		case state.Kind == "gate" && state.Verdict != nil && state.Verdict.Valid &&
			state.Verdict.Decision == "REQUEST_CHANGES":
			state.State = "changes_requested"
			state.NextAction = "wait for the author to publish a new revision, or publish a newer verdict if the decision changes"
		case state.Kind == "merge" && !state.Green:
			state.State = "blocked_quorum"
			state.NextAction = "wait for all required gate quorum expressions to be green"
		case state.Claim != nil && state.Claim.Overdue:
			state.State = "overdue"
			state.NextAction = fmt.Sprintf("holder may continue, or explicitly run hctl claim %s --reclaim %s", state.Obligation.ID, state.Claim.OID)
		case state.Claim != nil:
			state.State = "claimed"
			switch state.Kind {
			case "author":
				state.NextAction = fmt.Sprintf("holder %s works on %s", state.Holder, state.BranchRef)
			case "gate":
				if state.Verdict != nil && state.Verdict.Valid {
					state.NextAction = "publish a newer exact-revision verdict only if the decision changes"
				} else {
					state.NextAction = fmt.Sprintf("review threshold %s, merge report memo, then hctl verdict %s", state.Threshold, state.Obligation.ID)
				}
			case "merge":
				state.NextAction = fmt.Sprintf("hctl merge %d --check, then hctl merge %d", state.PR, state.PR)
			}
		case hasUnmetNeeds(state, result):
			state.State = "blocked_needs"
			state.NextAction = "wait for prerequisite assignments to complete"
		case state.Kind == "merge" && activeMerge > 0:
			state.State = "blocked_capacity"
			state.NextAction = "wait for the repo-wide merge slot to be released"
		default:
			state.State = "claimable"
			state.NextAction = fmt.Sprintf("hctl claim %s", state.Obligation.ID)
		}
	}
}

func (e Engine) validateCapacities(result *Result) {
	activeMerge := 0
	activeAuthors := map[string]int{}
	for _, state := range result.Obligations {
		if state.Claim == nil || state.Claim.Stale || state.Canceled || state.Completed {
			continue
		}
		switch state.Kind {
		case "merge":
			if state.AuditDebt != "" {
				continue
			}
			activeMerge++
		case "author":
			activeAuthors[state.Holder]++
		}
	}
	if activeMerge > 1 {
		addInvalid(result, "", fmt.Sprintf("merge capacity=1 violated by %d active merge claims", activeMerge))
	}
	for seat, count := range activeAuthors {
		if count > 1 {
			addInvalid(result, seat, fmt.Sprintf("author capacity=1 violated by %d active author claims", count))
		}
	}
}

func hasUnmetNeeds(state *ObligationState, result *Result) bool {
	if len(state.AssignmentRecord.Assignment.Needs) == 0 {
		return false
	}
	for _, need := range state.AssignmentRecord.Assignment.Needs {
		completed := false
		for _, candidate := range result.Obligations {
			if candidate.Assignment == need && candidate.Kind == "merge" && candidate.Completed {
				completed = true
				break
			}
		}
		if !completed {
			return true
		}
	}
	return false
}

func (e Engine) markMovedAssignments(snapshot *facts.Snapshot, result *Result) {
	current := map[string]string{}
	for _, record := range snapshot.Assignments {
		current[record.Assignment.ID] = record.Source.Blob
	}
	reported := map[string]bool{}
	for _, chainSeat := range sortedChainSeats(snapshot.Chains) {
		chain := snapshot.Chains[chainSeat]
		for _, event := range chain.Events {
			var obligation *protocol.Obligation
			switch {
			case event.Claim != nil:
				obligation = &event.Claim.Obligation
			case event.Verdict != nil:
				obligation = &event.Verdict.Obligation
			}
			if obligation == nil || !obligation.Preimage.Assignment.IsStatic() {
				continue
			}
			id := obligation.Preimage.Assignment.ID
			if blob, exists := current[id]; exists && blob != obligation.Preimage.Assignment.Blob && !reported[id] {
				result.Problems = append(result.Problems, facts.Problem{
					Code: "ASSIGNMENT_REVISION_MOVED", Severity: facts.SeverityWarning,
					Detail: fmt.Sprintf("assignment_revision moved for %q: event blob %s, current blob %s", id, obligation.Preimage.Assignment.Blob, blob),
				})
				reported[id] = true
			}
		}
	}
}

func EvaluateQuorum(node config.QuorumNode, votes map[string]bool) bool {
	switch {
	case node.Seat != "":
		return votes[node.Seat]
	case node.AllOf != nil:
		for _, child := range node.AllOf {
			if !EvaluateQuorum(child, votes) {
				return false
			}
		}
		return len(node.AllOf) > 0
	case node.AnyOf != nil:
		for _, child := range node.AnyOf {
			if EvaluateQuorum(child, votes) {
				return true
			}
		}
		return false
	case node.AtLeast != nil:
		count := 0
		for _, child := range node.AtLeast.Of {
			if EvaluateQuorum(child, votes) {
				count++
			}
		}
		return count >= node.AtLeast.Count
	default:
		return false
	}
}

func scopeCovers(scope protocol.ReviewScope) bool {
	if scope.Kind == "full" {
		return true
	}
	hasFix, hasDelta := false, false
	for _, part := range scope.Parts {
		hasFix = hasFix || part.Kind == "fix_verification"
		hasDelta = hasDelta || part.Kind == "delta"
	}
	return hasFix && hasDelta
}

func sameObligation(a, b protocol.Obligation) bool {
	if a.ID != b.ID ||
		a.Preimage.Assignment != b.Preimage.Assignment ||
		a.Preimage.Kind != b.Preimage.Kind ||
		a.Preimage.Target != b.Preimage.Target {
		return false
	}
	if a.Preimage.Aspect == nil || b.Preimage.Aspect == nil {
		if a.Preimage.Aspect != nil || b.Preimage.Aspect != nil {
			return false
		}
	} else if *a.Preimage.Aspect != *b.Preimage.Aspect {
		return false
	}
	return true
}

func (e Engine) obligationSourceValid(
	snapshot *facts.Snapshot,
	obligation protocol.Obligation,
	record config.AssignmentRecord,
) bool {
	if obligation.Source == nil ||
		obligation.Source.Path != record.Source.Path ||
		obligation.Source.Blob != record.Source.Blob {
		return false
	}
	return snapshot.SourceValid(*obligation.Source)
}

func pullForHead(pulls map[int]string, head string) (int, bool) {
	if head == "" {
		return 0, false
	}
	found := 0
	for number, tip := range pulls {
		if tip != head {
			continue
		}
		if found != 0 {
			return 0, true
		}
		found = number
	}
	return found, false
}

func (e Engine) assignmentCanceled(
	snapshot *facts.Snapshot,
	record config.AssignmentRecord,
	cancels []protocol.Source,
) bool {
	for _, source := range cancels {
		if source.Path != record.Source.Path || source.Blob != record.Source.Blob {
			continue
		}
		if snapshot.SourceValid(source) {
			return true
		}
	}
	return false
}

func addInvalid(result *Result, seat, detail string) {
	result.Problems = append(result.Problems, facts.Problem{
		Code: "CORRUPT_CHAIN", Severity: facts.SeverityError, Seat: seat,
		Detail: "known event violates the protocol state machine: " + detail,
	})
}

func addIncomplete(result *Result, seat, detail string) {
	result.Problems = append(result.Problems, facts.Problem{
		Code: "INCOMPLETE_FACTS", Severity: facts.SeverityError, Seat: seat,
		Detail: detail,
	})
}

func noErrors(problems []facts.Problem) bool {
	for _, problem := range problems {
		if problem.Severity == facts.SeverityError {
			return false
		}
	}
	return true
}

func cloneMap(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func sortedChainSeats(chains map[string]*facts.Chain) []string {
	seats := make([]string, 0, len(chains))
	for seat := range chains {
		seats = append(seats, seat)
	}
	sort.Strings(seats)
	return seats
}

func FindByAssignmentKind(result *Result, assignment, kind string) (*ObligationState, bool) {
	for _, state := range result.Obligations {
		if state.Assignment == assignment && state.Kind == kind {
			return state, true
		}
	}
	return nil, false
}

func RequiredGateSummary(result *Result, assignment string) string {
	var parts []string
	for _, state := range result.Obligations {
		if state.Assignment == assignment && state.Kind == "gate" && state.GateMode == "required" {
			value := "red"
			if state.Green {
				value = "green"
			}
			parts = append(parts, state.GateID+"/"+state.Holder+"="+value)
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
