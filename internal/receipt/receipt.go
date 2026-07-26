package receipt

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/yesme/hctl/internal/gitx"
	"github.com/yesme/hctl/internal/protocol"
)

type Receipt struct {
	Version    int               `json:"version"`
	Assignment string            `json:"assignment"`
	Obligation string            `json:"obligation"`
	PR         int               `json:"pr"`
	Base       string            `json:"base"`
	Head       string            `json:"head"`
	MergeClaim string            `json:"merge_claim"`
	Method     string            `json:"method"`
	FactTips   map[string]string `json:"fact_tips"`
}

func (r Receipt) Body() (string, error) {
	if err := r.ValidateShape(); err != nil {
		return "", err
	}
	lines := []string{
		"Hctl-Version: " + strconv.Itoa(r.Version),
		"Hctl-Assignment: " + r.Assignment,
		"Hctl-Obligation: " + r.Obligation,
		"Hctl-PR: " + strconv.Itoa(r.PR),
		"Hctl-Base: " + r.Base,
		"Hctl-Head: " + r.Head,
		"Hctl-Merge-Claim: " + r.MergeClaim,
		"Hctl-Method: " + r.Method,
	}
	seats := make([]string, 0, len(r.FactTips))
	for seat := range r.FactTips {
		seats = append(seats, seat)
	}
	sort.Strings(seats)
	for _, seat := range seats {
		lines = append(lines, "Hctl-Fact-Tip: "+seat+"="+r.FactTips[seat])
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func Parse(message string) (Receipt, error) {
	r := Receipt{FactTips: map[string]string{}}
	single := map[string]string{}
	allowed := map[string]bool{
		"Hctl-Version": true, "Hctl-Assignment": true, "Hctl-Obligation": true,
		"Hctl-PR": true, "Hctl-Base": true, "Hctl-Head": true,
		"Hctl-Merge-Claim": true, "Hctl-Method": true,
	}
	for _, line := range strings.Split(message, "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !strings.HasPrefix(line, "Hctl-") {
			continue
		}
		if !ok || !strings.HasPrefix(key, "Hctl-") {
			return Receipt{}, fmt.Errorf("malformed receipt field %q", line)
		}
		if key == "Hctl-Fact-Tip" {
			seat, oid, ok := strings.Cut(value, "=")
			if !ok || !protocol.ValidToken(seat) || !protocol.ValidOID(oid) {
				return Receipt{}, fmt.Errorf("invalid Hctl-Fact-Tip %q", value)
			}
			if _, duplicate := r.FactTips[seat]; duplicate {
				return Receipt{}, fmt.Errorf("duplicate Hctl-Fact-Tip seat %q", seat)
			}
			r.FactTips[seat] = oid
			continue
		}
		if !allowed[key] {
			return Receipt{}, fmt.Errorf("unknown receipt field %s", key)
		}
		if _, duplicate := single[key]; duplicate {
			return Receipt{}, fmt.Errorf("duplicate receipt field %s", key)
		}
		single[key] = value
	}
	required := []string{
		"Hctl-Version", "Hctl-Assignment", "Hctl-Obligation", "Hctl-PR",
		"Hctl-Base", "Hctl-Head", "Hctl-Merge-Claim", "Hctl-Method",
	}
	for _, key := range required {
		if single[key] == "" {
			return Receipt{}, fmt.Errorf("missing receipt field %s", key)
		}
	}
	var err error
	r.Version, err = strconv.Atoi(single["Hctl-Version"])
	if err != nil {
		return Receipt{}, fmt.Errorf("invalid Hctl-Version")
	}
	r.PR, err = strconv.Atoi(single["Hctl-PR"])
	if err != nil {
		return Receipt{}, fmt.Errorf("invalid Hctl-PR")
	}
	r.Assignment = single["Hctl-Assignment"]
	r.Obligation = single["Hctl-Obligation"]
	r.Base = single["Hctl-Base"]
	r.Head = single["Hctl-Head"]
	r.MergeClaim = single["Hctl-Merge-Claim"]
	r.Method = single["Hctl-Method"]
	if err := r.ValidateShape(); err != nil {
		return Receipt{}, err
	}
	return r, nil
}

func (r Receipt) ValidateShape() error {
	if r.Version != 1 {
		return fmt.Errorf("Hctl-Version must be 1")
	}
	if !protocol.ValidToken(r.Assignment) {
		return fmt.Errorf("invalid Hctl-Assignment")
	}
	if !protocol.ValidObligationID(r.Obligation) {
		return fmt.Errorf("invalid Hctl-Obligation")
	}
	if r.PR < 1 {
		return fmt.Errorf("invalid Hctl-PR")
	}
	if !protocol.ValidOID(r.Base) || !protocol.ValidOID(r.Head) || !protocol.ValidOID(r.MergeClaim) {
		return fmt.Errorf("receipt base, head, and merge claim must be full Git OIDs")
	}
	if r.Method != "squash" {
		return fmt.Errorf("UNJUDGEABLE_MERGE: Hctl-Method must be squash")
	}
	for seat, oid := range r.FactTips {
		if !protocol.ValidToken(seat) || !protocol.ValidOID(oid) {
			return fmt.Errorf("invalid fact tip %q=%q", seat, oid)
		}
	}
	return nil
}

func VerifyCommit(ctx context.Context, repo *gitx.Repo, mergeCommit string, expected Receipt) error {
	batch, err := repo.NewBatch(ctx)
	if err != nil {
		return err
	}
	defer batch.Close()
	object, err := batch.Read(mergeCommit)
	if err != nil {
		return err
	}
	if object.Type != "commit" {
		return fmt.Errorf("merge result %s is not a commit", mergeCommit)
	}
	commit, err := gitx.ParseCommit(object.Data)
	if err != nil {
		return err
	}
	if len(commit.Parents) != 1 {
		return fmt.Errorf("UNJUDGEABLE_MERGE: squash result has %d parents", len(commit.Parents))
	}
	if commit.Parents[0] != expected.Base {
		return fmt.Errorf("merge parent %s does not equal Hctl-Base %s", commit.Parents[0], expected.Base)
	}
	actual, err := Parse(commit.Message)
	if err != nil {
		return fmt.Errorf("invalid merge receipt: %w", err)
	}
	if !equal(actual, expected) {
		return fmt.Errorf("merge receipt does not equal the checked pre-merge snapshot")
	}
	head, err := batch.Read(expected.Head)
	if err != nil {
		return err
	}
	if head.Type != "commit" {
		return fmt.Errorf("Hctl-Head is not a commit")
	}
	headCommit, err := gitx.ParseCommit(head.Data)
	if err != nil {
		return err
	}
	if commit.Tree != headCommit.Tree {
		return fmt.Errorf("squash result tree %s does not equal head tree %s", commit.Tree, headCommit.Tree)
	}
	return nil
}

func equal(a, b Receipt) bool {
	if a.Version != b.Version || a.Assignment != b.Assignment || a.Obligation != b.Obligation ||
		a.PR != b.PR || a.Base != b.Base || a.Head != b.Head || a.MergeClaim != b.MergeClaim ||
		a.Method != b.Method || len(a.FactTips) != len(b.FactTips) {
		return false
	}
	for seat, oid := range a.FactTips {
		if b.FactTips[seat] != oid {
			return false
		}
	}
	return true
}
