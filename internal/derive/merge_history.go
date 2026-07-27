package derive

import (
	"context"
	"fmt"
	"strings"

	"github.com/yesme/hctl/internal/config"
	"github.com/yesme/hctl/internal/facts"
	"github.com/yesme/hctl/internal/protocol"
	"github.com/yesme/hctl/internal/receipt"
)

func (e Engine) applyMergeHistory(ctx context.Context, snapshot *facts.Snapshot, result *Result) {
	statesByAssignment := map[string][]*ObligationState{}
	for _, state := range result.Obligations {
		statesByAssignment[state.Assignment] = append(statesByAssignment[state.Assignment], state)
	}
	cutover, cutoverProblem := e.detectCutover(ctx, snapshot)
	if cutoverProblem != nil {
		result.Problems = append(result.Problems, *cutoverProblem)
	}
	for _, record := range snapshot.Assignments {
		states := statesByAssignment[record.Assignment.ID]
		if len(states) == 0 {
			continue
		}
		var mergeState *ObligationState
		for _, state := range states {
			if state.Kind == "merge" {
				mergeState = state
				break
			}
		}
		if mergeState == nil {
			continue
		}

		// A receipt is the durable completion fact. The work branch may already
		// have been deleted (D-08), so replay receipts before consulting any
		// live branch ref.
		var receiptCommit *facts.MainCommit
		var parsedReceipt receipt.Receipt
		var malformedReceipt error
		for i := range snapshot.MainHistory {
			entry := &snapshot.MainHistory[i]
			if !strings.Contains(entry.Message, "Hctl-") {
				continue
			}
			parsed, err := receipt.Parse(entry.Message)
			if err != nil {
				if receiptMessageNamesAssignment(entry.Message, record.Assignment.ID) {
					receiptCommit = entry
					malformedReceipt = err
					break
				}
				continue
			}
			if parsed.Assignment != record.Assignment.ID ||
				parsed.Obligation != mergeState.Obligation.ID {
				continue
			}
			receiptCommit = entry
			parsedReceipt = parsed
			break
		}
		if receiptCommit != nil {
			for _, state := range states {
				state.MergedCommit = receiptCommit.OID
			}
			if malformedReceipt != nil {
				e.markMergeDebt(result, states, cutover, receiptCommit.OID, "INVALID_RECEIPT",
					fmt.Sprintf("assignment %q merge commit %s: %v", record.Assignment.ID, receiptCommit.OID, malformedReceipt))
				continue
			}
			revision := structRevision(parsedReceipt.Base, parsedReceipt.Head)
			for _, state := range states {
				state.Revision = &revision
				state.PR = parsedReceipt.PR
			}
			if err := e.validateReceiptReplay(ctx, snapshot, record, states, *receiptCommit, parsedReceipt); err != nil {
				e.markMergeDebt(result, states, cutover, receiptCommit.OID, "INVALID_RECEIPT",
					fmt.Sprintf("assignment %q merge commit %s: %v", record.Assignment.ID, receiptCommit.OID, err))
				continue
			}
			for _, state := range states {
				state.Completed = true
			}
			continue
		}

		head := snapshot.BranchTips[record.BranchRef()]
		if head == "" {
			continue
		}
		headTree := snapshot.CommitTrees[head]
		if headTree == "" {
			result.Problems = append(result.Problems, facts.Problem{
				Code: "INCOMPLETE_FACTS", Severity: facts.SeverityError,
				Detail: fmt.Sprintf("read assignment %q head tree: object is unavailable", record.Assignment.ID),
			})
			continue
		}
		var merged *facts.MainCommit
		for i := range snapshot.MainHistory {
			entry := &snapshot.MainHistory[i]
			if entry.Tree != headTree {
				continue
			}
			if len(entry.Parents) == 0 {
				continue
			}
			merged = entry
			break
		}
		if merged == nil {
			// A non-squash merge can retain the head commit without ever
			// producing an exact squash tree on the first-parent line.
			if snapshot.Reachable[head] {
				e.markMergeDebt(result, states, cutover, "", "UNJUDGEABLE_MERGE",
					fmt.Sprintf("assignment %q head %s is reachable from main without a judgeable squash receipt", record.Assignment.ID, head))
			}
			continue
		}
		for _, state := range states {
			state.MergedCommit = merged.OID
			revision := structRevision(merged.Parents[0], head)
			state.Revision = &revision
			if pr, ambiguous := pullForHead(snapshot.PullTips, head); !ambiguous {
				state.PR = pr
			}
		}
		if len(merged.Parents) != 1 {
			e.markMergeDebt(result, states, cutover, merged.OID, "UNJUDGEABLE_MERGE",
				fmt.Sprintf("assignment %q integration commit %s has %d parents", record.Assignment.ID, merged.OID, len(merged.Parents)))
			continue
		}
		parsed, err := receipt.Parse(merged.Message)
		if err != nil {
			code := "UNRECORDED_MERGE"
			if strings.Contains(merged.Message, "Hctl-") {
				code = "INVALID_RECEIPT"
			}
			e.markMergeDebt(result, states, cutover, merged.OID, code,
				fmt.Sprintf("assignment %q merge commit %s: %v", record.Assignment.ID, merged.OID, err))
			continue
		}
		if err := e.validateReceiptReplay(ctx, snapshot, record, states, *merged, parsed); err != nil {
			e.markMergeDebt(result, states, cutover, merged.OID, "INVALID_RECEIPT",
				fmt.Sprintf("assignment %q merge commit %s: %v", record.Assignment.ID, merged.OID, err))
			continue
		}
		for _, state := range states {
			state.Completed = true
		}
	}
}

func (e Engine) validateReceiptReplay(
	ctx context.Context,
	current *facts.Snapshot,
	currentRecord config.AssignmentRecord,
	states []*ObligationState,
	merged facts.MainCommit,
	r receipt.Receipt,
) error {
	var mergeState *ObligationState
	for _, state := range states {
		if state.Kind == "merge" {
			mergeState = state
			break
		}
	}
	if mergeState == nil {
		return fmt.Errorf("merge obligation is missing")
	}
	if r.Assignment != currentRecord.Assignment.ID ||
		r.Obligation != mergeState.Obligation.ID ||
		r.Base != merged.Parents[0] ||
		r.Head == "" ||
		r.MergeClaim == "" {
		return fmt.Errorf("receipt identity fields do not match the assignment and integration commit")
	}
	if current.PullTips[r.PR] != r.Head {
		return fmt.Errorf("receipt PR %d does not map to head %s", r.PR, r.Head)
	}
	if err := receipt.VerifyCommit(ctx, e.Repo, merged.OID, r); err != nil {
		return err
	}
	baseSeats, baseAssignments, err := config.LoadAt(ctx, e.Repo, r.Base)
	if err != nil {
		return fmt.Errorf("load config at Hctl-Base: %w", err)
	}
	var baseRecord config.AssignmentRecord
	found := false
	for _, candidate := range baseAssignments {
		if candidate.Assignment.ID == currentRecord.Assignment.ID &&
			candidate.Source.Blob == currentRecord.Source.Blob {
			baseRecord = candidate
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("frozen assignment revision is not present at Hctl-Base")
	}
	for seat, tip := range r.FactTips {
		if _, exists := baseSeats.Config.Seats[seat]; !exists {
			return fmt.Errorf("receipt fact tip names unknown base-config seat %q", seat)
		}
		currentTip := current.FactTips[seat]
		if currentTip == "" {
			return fmt.Errorf("receipt fact tip %s=%s is absent from the current chain set", seat, tip)
		}
		if !chainContains(current.Chains[seat], tip) {
			return fmt.Errorf("receipt fact tip %s=%s is not a prefix of current facts", seat, tip)
		}
	}
	chains, problems := facts.ReadChains(ctx, e.Repo, r.FactTips)
	for _, problem := range problems {
		if problem.Severity == facts.SeverityError {
			return fmt.Errorf("%s while replaying receipt: %s", problem.Code, problem.Detail)
		}
	}
	temp := &facts.Snapshot{
		ObservedAt:  current.ObservedAt,
		Remote:      current.Remote,
		MainCommit:  r.Base,
		Seats:       baseSeats,
		Assignments: []config.AssignmentRecord{baseRecord},
		BranchTips: map[string]string{
			baseRecord.Assignment.BaseRef: r.Base,
			baseRecord.BranchRef():        r.Head,
		},
		PullTips: map[int]string{r.PR: r.Head},
		FactTips: cloneMap(r.FactTips),
		Chains:   chains,
	}
	if err := facts.PopulateIndexes(ctx, e.Repo, temp); err != nil {
		return fmt.Errorf("index receipt fact snapshot: %w", err)
	}
	replayed := (Engine{Repo: e.Repo, Now: e.Now, SkipMergeAudit: true}).Derive(ctx, temp, "")
	if !replayed.Complete {
		return fmt.Errorf("receipt fact snapshot is not complete")
	}
	replayedMerge, ok := FindByAssignmentKind(replayed, baseRecord.Assignment.ID, "merge")
	if !ok || replayedMerge.Claim == nil {
		return fmt.Errorf("receipt fact tips do not contain an active merge claim")
	}
	if replayedMerge.Canceled || replayedMerge.Completed {
		return fmt.Errorf("receipt merge obligation is canceled or already completed")
	}
	if replayedMerge.Claim.OID != r.MergeClaim {
		return fmt.Errorf("receipt merge claim %s is not active (active=%s)", r.MergeClaim, replayedMerge.Claim.OID)
	}
	if replayedMerge.Claim.CoordinatorConfig == nil ||
		*replayedMerge.Claim.CoordinatorConfig != baseSeats.Source ||
		replayedMerge.Claim.Actor.Seat != baseSeats.Config.MergeCoordinator {
		return fmt.Errorf("merge claim coordinator_config/actor does not match Hctl-Base")
	}
	if replayedMerge.Revision == nil ||
		replayedMerge.Revision.Base != r.Base ||
		replayedMerge.Revision.Head != r.Head ||
		!replayedMerge.Green {
		return fmt.Errorf("receipt replay does not prove exact-revision required quorum green")
	}
	return nil
}

func receiptMessageNamesAssignment(message, assignment string) bool {
	want := "Hctl-Assignment: " + assignment
	for _, line := range strings.Split(message, "\n") {
		if line == want {
			return true
		}
	}
	return false
}

// markMergeDebt records merge audit debt. Debt whose integration commit lies
// inside the acknowledged bootstrap boundary (the cutover commit or its
// first-parent ancestors) is surfaced as ACKNOWLEDGED_BOOTSTRAP_HISTORY at
// warning severity: bounded history must not permanently trip WriteGuard, but
// it is not repair — the obligation stays open and no receipt is fabricated.
// Debt after the cutover (or debt that cannot be placed on the first-parent
// line at all, mergedOID=="") keeps full error severity.
func (e Engine) markMergeDebt(result *Result, states []*ObligationState, cutover *bootstrapCutover, mergedOID, code, detail string) {
	if cutover.covers(mergedOID) {
		for _, state := range states {
			state.AuditDebt = acknowledgedBootstrapCode
		}
		result.Problems = append(result.Problems, facts.Problem{
			Code: acknowledgedBootstrapCode, Severity: facts.SeverityWarning,
			Detail: fmt.Sprintf("bootstrap history before cutover %s (%s): %s", cutover.OID, code, detail),
		})
		return
	}
	for _, state := range states {
		state.AuditDebt = code
	}
	result.Problems = append(result.Problems, facts.Problem{
		Code: code, Severity: facts.SeverityError, Detail: detail,
	})
}

func chainContains(chain *facts.Chain, oid string) bool {
	if chain == nil {
		return false
	}
	for _, commit := range chain.Commits {
		if commit == oid {
			return true
		}
	}
	return false
}

func structRevision(base, head string) protocol.Revision {
	return protocol.Revision{Base: base, Head: head}
}
