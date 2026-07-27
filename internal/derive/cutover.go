package derive

import (
	"context"
	"fmt"

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

	treeActive := map[string]bool{}
	activeAt := make([]bool, len(history))
	for i, entry := range history {
		activeAt[i] = enforcementActive(batch, entry, treeActive)
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

// enforcementActive reports whether the seats file at the commit proves
// enforcement=active. Identical trees share one verdict; any unreadable state
// proves nothing and is not-active.
func enforcementActive(batch *gitx.Batch, entry facts.MainCommit, cache map[string]bool) bool {
	if active, seen := cache[entry.Tree]; seen {
		return active
	}
	active := false
	object, err := batch.Read(entry.OID + ":.hctl/seats.toml")
	if err == nil && object.Type == "blob" {
		var loose struct {
			Enforcement string `toml:"enforcement"`
		}
		if _, err := toml.Decode(string(object.Data), &loose); err == nil {
			active = loose.Enforcement == "active"
		}
	}
	cache[entry.Tree] = active
	return active
}
