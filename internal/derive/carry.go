package derive

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/yesme/hctl/internal/facts"
	"github.com/yesme/hctl/internal/gitx"
	"github.com/yesme/hctl/internal/protocol"
)

const (
	memoBaseCarryKind        = "memo-base-equivalent"
	candidateFingerprintV1   = "hctl-candidate-fingerprint-v1\x00"
	candidateFingerprintDone = "end"
)

// CarriedVerdict retains the evidence that was accepted for an older exact
// revision. Carry is derived state only: it never rewrites the original
// VERDICT event or pretends that the old claim targeted the new revision.
type CarriedVerdict struct {
	Kind     string            `json:"kind"`
	Verdict  string            `json:"verdict"`
	Claim    string            `json:"claim"`
	Report   protocol.Source   `json:"report"`
	Revision protocol.Revision `json:"revision"`
}

type candidateSurface struct {
	fingerprint string
	paths       map[string]bool
	ok          bool
}

type exactDelta struct {
	path string
	old  gitx.TreeEntry
	next gitx.TreeEntry
}

type carryEvaluator struct {
	ctx        context.Context
	repo       *gitx.Repo
	snapshot   *facts.Snapshot
	candidates map[protocol.Revision]candidateSurface
	trees      map[string]map[string]gitx.TreeEntry
	deltas     map[[2]string][]exactDelta
}

func newCarryEvaluator(ctx context.Context, repo *gitx.Repo, snapshot *facts.Snapshot) *carryEvaluator {
	return &carryEvaluator{
		ctx:        ctx,
		repo:       repo,
		snapshot:   snapshot,
		candidates: map[protocol.Revision]candidateSurface{},
		trees:      map[string]map[string]gitx.TreeEntry{},
		deltas:     map[[2]string][]exactDelta{},
	}
}

// memoBaseEquivalent proves the intentionally narrow v1 carry surface. Every
// false or unreadable predicate fails closed and leaves the gate needing an
// exact-revision verdict.
func (c *carryEvaluator) memoBaseEquivalent(old, current protocol.Revision) bool {
	if c == nil || c.repo == nil || c.snapshot == nil {
		return false
	}
	if !c.snapshot.CommitKnown(old.Base) ||
		!c.snapshot.CommitKnown(old.Head) ||
		!c.snapshot.CommitKnown(current.Base) ||
		!c.snapshot.CommitKnown(current.Head) ||
		!c.snapshot.IsAncestor(old.Base, current.Base) {
		return false
	}
	oldCandidate := c.candidate(old)
	currentCandidate := c.candidate(current)
	if !oldCandidate.ok || !currentCandidate.ok ||
		oldCandidate.fingerprint != currentCandidate.fingerprint {
		return false
	}
	baseDelta, ok := c.delta(old.Base, current.Base)
	if !ok {
		return false
	}
	for _, change := range baseDelta {
		if !protocol.ValidMemoPath(change.path) || oldCandidate.paths[change.path] {
			return false
		}
	}
	return true
}

func (c *carryEvaluator) candidate(revision protocol.Revision) candidateSurface {
	if cached, exists := c.candidates[revision]; exists {
		return cached
	}
	surface := c.buildCandidate(revision)
	c.candidates[revision] = surface
	return surface
}

func (c *carryEvaluator) buildCandidate(revision protocol.Revision) candidateSurface {
	if revision.Base == "" || revision.Head == "" {
		return candidateSurface{}
	}
	batch, err := c.repo.NewBatch(c.ctx)
	if err != nil {
		return candidateSurface{}
	}
	defer batch.Close()

	type candidateCommit struct {
		oid    string
		parent string
		commit gitx.Commit
	}
	var reverse []candidateCommit
	seen := map[string]bool{}
	for oid := revision.Head; oid != revision.Base; {
		if seen[oid] {
			return candidateSurface{}
		}
		seen[oid] = true
		object, err := batch.Read(oid)
		if err != nil || object.Type != "commit" {
			return candidateSurface{}
		}
		commit, err := gitx.ParseCommit(object.Data)
		if err != nil || len(commit.Parents) != 1 {
			// Merge candidates are deliberately outside the v1 carry surface.
			return candidateSurface{}
		}
		reverse = append(reverse, candidateCommit{
			oid: oid, parent: commit.Parents[0], commit: commit,
		})
		oid = commit.Parents[0]
	}
	for left, right := 0, len(reverse)-1; left < right; left, right = left+1, right-1 {
		reverse[left], reverse[right] = reverse[right], reverse[left]
	}

	hash := sha256.New()
	writeFingerprintField(hash, "domain", []byte(candidateFingerprintV1))
	writeFingerprintUint(hash, "commits", uint64(len(reverse)))
	paths := map[string]bool{}
	for _, item := range reverse {
		author, ok := authorIdentity(item.commit.Author)
		if !ok {
			return candidateSurface{}
		}
		delta, ok := c.delta(item.parent, item.oid)
		if !ok {
			return candidateSurface{}
		}
		writeFingerprintField(hash, "commit", nil)
		writeFingerprintField(hash, "author", []byte(author))
		writeFingerprintField(hash, "message", []byte(item.commit.Message))
		writeFingerprintUint(hash, "changes", uint64(len(delta)))
		for _, change := range delta {
			paths[change.path] = true
			writeFingerprintField(hash, "path", []byte(change.path))
			writeTreeSide(hash, "old", change.old)
			writeTreeSide(hash, "new", change.next)
		}
	}
	writeFingerprintField(hash, candidateFingerprintDone, nil)
	return candidateSurface{
		fingerprint: hex.EncodeToString(hash.Sum(nil)),
		paths:       paths,
		ok:          true,
	}
}

func (c *carryEvaluator) delta(old, next string) ([]exactDelta, bool) {
	key := [2]string{old, next}
	if cached, exists := c.deltas[key]; exists {
		return cached, true
	}
	oldTree, ok := c.tree(old)
	if !ok {
		return nil, false
	}
	nextTree, ok := c.tree(next)
	if !ok {
		return nil, false
	}
	paths := make([]string, 0, len(oldTree)+len(nextTree))
	seen := map[string]bool{}
	for path := range oldTree {
		seen[path] = true
		paths = append(paths, path)
	}
	for path := range nextTree {
		if !seen[path] {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	delta := make([]exactDelta, 0, len(paths))
	for _, path := range paths {
		oldEntry := oldTree[path]
		nextEntry := nextTree[path]
		if sameTreeEntry(oldEntry, nextEntry) {
			continue
		}
		delta = append(delta, exactDelta{path: path, old: oldEntry, next: nextEntry})
	}
	c.deltas[key] = delta
	return delta, true
}

func (c *carryEvaluator) tree(commit string) (map[string]gitx.TreeEntry, bool) {
	if cached, exists := c.trees[commit]; exists {
		return cached, true
	}
	entries, err := c.repo.ListTree(c.ctx, commit, "")
	if err != nil {
		return nil, false
	}
	tree := make(map[string]gitx.TreeEntry, len(entries))
	for _, entry := range entries {
		if _, duplicate := tree[entry.Path]; duplicate {
			return nil, false
		}
		tree[entry.Path] = entry
	}
	c.trees[commit] = tree
	return tree, true
}

func sameTreeEntry(left, right gitx.TreeEntry) bool {
	return left.Mode == right.Mode && left.Type == right.Type && left.OID == right.OID
}

// authorIdentity strips only the author timestamp and timezone. Name and email
// bytes remain exact; author date and all committer metadata are intentionally
// excluded because a rebase may rewrite them without changing authorship.
func authorIdentity(authorHeader string) (string, bool) {
	timezoneAt := strings.LastIndexByte(authorHeader, ' ')
	if timezoneAt <= 0 {
		return "", false
	}
	timestampAt := strings.LastIndexByte(authorHeader[:timezoneAt], ' ')
	if timestampAt <= 0 {
		return "", false
	}
	timestamp := authorHeader[timestampAt+1 : timezoneAt]
	timezone := authorHeader[timezoneAt+1:]
	if _, err := strconv.ParseInt(timestamp, 10, 64); err != nil ||
		len(timezone) != 5 ||
		(timezone[0] != '+' && timezone[0] != '-') {
		return "", false
	}
	for _, digit := range timezone[1:] {
		if digit < '0' || digit > '9' {
			return "", false
		}
	}
	identity := authorHeader[:timestampAt]
	return identity, identity != ""
}

func writeTreeSide(writer io.Writer, label string, entry gitx.TreeEntry) {
	present := entry.Mode != "" || entry.Type != "" || entry.OID != ""
	if !present {
		writeFingerprintField(writer, label+"-present", []byte{0})
		return
	}
	writeFingerprintField(writer, label+"-present", []byte{1})
	writeFingerprintField(writer, label+"-mode", []byte(entry.Mode))
	writeFingerprintField(writer, label+"-type", []byte(entry.Type))
	writeFingerprintField(writer, label+"-oid", []byte(entry.OID))
}

func writeFingerprintField(writer io.Writer, label string, value []byte) {
	writeRawUint(writer, uint64(len(label)))
	_, _ = io.WriteString(writer, label)
	writeRawUint(writer, uint64(len(value)))
	_, _ = writer.Write(value)
}

func writeFingerprintUint(writer io.Writer, label string, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	writeFingerprintField(writer, label, encoded[:])
}

func writeRawUint(writer io.Writer, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = writer.Write(encoded[:])
}
