package derive

import (
	"context"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/yesme/hctl/internal/facts"
	"github.com/yesme/hctl/internal/gitx"
)

const (
	acknowledgedBootstrapCode = "ACKNOWLEDGED_BOOTSTRAP_HISTORY"
	invalidCutoverCode        = "INVALID_CUTOVER"
)

// bootstrapCutover is the unique bootstrap→active transition on main's
// first-parent history (codex-27k §3.3). The cutover commit and its
// first-parent ancestors form the acknowledged bootstrap boundary: merge audit
// debt inside it is surfaced as a warning instead of a WriteGuard error. This
// is a bounded historical boundary, not repair — no receipt is fabricated and
// the obligations stay open for P2 repair.
type bootstrapCutover struct {
	OID          string
	acknowledged map[string]bool
}

func (c *bootstrapCutover) covers(oid string) bool {
	return c != nil && oid != "" && c.acknowledged[oid]
}

// detectCutover classifies every main first-parent commit as active or
// not-active and requires the shape active^k · not-active^m (tip first).
//
//   - k == 0            → still bootstrap era: no cutover, no problem
//   - k > 0 && m == 0   → active since genesis: no transition edge exists, so
//     nothing is acknowledged (a fresh adopter has no bootstrap debt to bound)
//   - k > 0 && m > 0    → cutover = the deepest active commit (index k-1)
//   - any active commit below not-active history (rollback active→bootstrap,
//     double flip, rewritten history) → INVALID_CUTOVER fail-closed error
//
// Enforcement is read per commit from `.hctl/seats.toml` with a loose decoder:
// historical blobs may predate the current strict schema and only the
// enforcement literal matters here. A missing or undecodable seats file proves
// nothing, so it counts as not-active — misclassifying a genuinely active
// commit this way can only tighten the shape check into a loud
// INVALID_CUTOVER, never silently widen the acknowledged set.
func (e Engine) detectCutover(ctx context.Context, snapshot *facts.Snapshot) (*bootstrapCutover, *facts.Problem) {
	history := snapshot.MainHistory
	if e.Repo == nil || len(history) == 0 {
		return nil, nil
	}
	batch, err := e.Repo.NewBatch(ctx)
	if err != nil {
		return nil, &facts.Problem{
			Code: invalidCutoverCode, Severity: facts.SeverityError,
			Detail: fmt.Sprintf("cannot read enforcement history: %v", err),
		}
	}
	defer batch.Close()

	treeState := map[string]enforcementState{}
	activeAt := make([]bool, len(history))
	for i, entry := range history {
		state := e.enforcementStateAt(ctx, batch, entry, treeState)
		if state == enforcementUnknown {
			return nil, &facts.Problem{
				Code: invalidCutoverCode, Severity: facts.SeverityError,
				Detail: fmt.Sprintf(
					"enforcement history is undecidable at commit %s: seats file exists but cannot be read or decoded; refusing to derive a cutover boundary",
					entry.OID,
				),
			}
		}
		activeAt[i] = state == enforcementActive
	}
	k := 0
	for k < len(activeAt) && activeAt[k] {
		k++
	}
	for i := k; i < len(activeAt); i++ {
		if activeAt[i] {
			return nil, &facts.Problem{
				Code: invalidCutoverCode, Severity: facts.SeverityError,
				Detail: fmt.Sprintf(
					"enforcement must flip bootstrap→active exactly once on main first-parent history: active commit %s appears below non-active commit %s",
					history[i].OID, history[k].OID,
				),
			}
		}
	}
	if k == 0 || k == len(activeAt) {
		return nil, nil
	}
	acknowledged := make(map[string]bool, len(history)-k+1)
	for i := k - 1; i < len(history); i++ {
		acknowledged[history[i].OID] = true
	}
	return &bootstrapCutover{OID: history[k-1].OID, acknowledged: acknowledged}, nil
}

// enforcementState is a three-valued read of `.hctl/seats.toml` at a commit
// (codex-pr72#P1-01): only a decodable seats file proves active or non-active,
// and only a provably absent path counts as non-active (pre-.hctl genesis).
// Everything else — unreadable object, wrong object type, undecodable TOML —
// is unknown and must fail closed: an unknown can never be collapsed into a
// boundary, because misreading one active commit as non-active would move the
// cutover toward tip and widen the acknowledged set, or hide an older flip.
type enforcementState int

const (
	enforcementNonActive enforcementState = iota
	enforcementActive
	enforcementUnknown
)

func (e Engine) enforcementStateAt(ctx context.Context, batch *gitx.Batch, entry facts.MainCommit, cache map[string]enforcementState) enforcementState {
	if state, seen := cache[entry.Tree]; seen {
		return state
	}
	state := enforcementUnknown
	object, err := batch.Read(entry.OID + ":.hctl/seats.toml")
	switch {
	case err == nil && object.Type == "blob":
		var loose struct {
			Enforcement string `toml:"enforcement"`
		}
		if _, decodeErr := toml.Decode(string(object.Data), &loose); decodeErr == nil {
			if loose.Enforcement == "active" {
				state = enforcementActive
			} else {
				state = enforcementNonActive
			}
		}
	case err == nil:
		// resolved to a non-blob object: undecidable
	default:
		// Distinguish "path does not exist at this commit" (defined non-active)
		// from "path exists but the object store cannot produce it" (unknown).
		listing, listErr := e.Repo.Output(ctx, "ls-tree", entry.OID, "--", ".hctl/seats.toml")
		if listErr == nil && strings.TrimSpace(listing) == "" {
			state = enforcementNonActive
		}
	}
	cache[entry.Tree] = state
	return state
}
