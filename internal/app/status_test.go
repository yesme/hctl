package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yesme/hctl/internal/derive"
	"github.com/yesme/hctl/internal/facts"
	"github.com/yesme/hctl/internal/protocol"
)

func TestStatusShowsCarriedVerdictEvidence(t *testing.T) {
	base := strings.Repeat("a", 40)
	head := strings.Repeat("b", 40)
	verdict := strings.Repeat("c", 40)
	reportBlob := strings.Repeat("d", 40)
	state := &derive.ObligationState{
		Obligation: protocol.Obligation{ID: "sha256:" + strings.Repeat("e", 64)},
		Assignment: "demo", Kind: "gate", Holder: "grok",
		GateID: "review", GateMode: "required", Threshold: "P1",
		State: "satisfied", NextAction: "wait for merge",
		Carried: &derive.CarriedVerdict{
			Kind: memoBaseCarryKindForTest, Verdict: verdict, Claim: strings.Repeat("f", 40),
			Report:   protocol.Source{Commit: base, Path: "memory/review.md", Blob: reportBlob},
			Revision: protocol.Revision{Base: base, Head: head},
		},
	}
	load := &loaded{
		Snapshot: &facts.Snapshot{Remote: "origin", MainCommit: strings.Repeat("1", 40)},
		Result: &derive.Result{
			Complete: true, FactTips: map[string]string{}, Obligations: []*derive.ObligationState{state},
		},
		Seat: "codex",
	}

	var stdout, stderr bytes.Buffer
	instance := &App{Stdout: &stdout, Stderr: &stderr}
	if code := instance.commandStatus(load, nil); code != ExitOK {
		t.Fatalf("human status failed: code=%d stderr=%s", code, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "carried: memo-base-equivalent") ||
		!strings.Contains(output, "original verdict="+verdict) ||
		!strings.Contains(output, "revision={base:"+base+",head:"+head+"}") ||
		!strings.Contains(output, "path:memory/review.md") {
		t.Fatalf("human status omitted carry evidence:\n%s", output)
	}

	stdout.Reset()
	if code := instance.commandStatus(load, []string{"--json"}); code != ExitOK {
		t.Fatalf("JSON status failed: code=%d stderr=%s", code, stderr.String())
	}
	var document struct {
		Obligations []struct {
			Carried *derive.CarriedVerdict `json:"carried"`
		} `json:"obligations"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Obligations) != 1 ||
		document.Obligations[0].Carried == nil ||
		document.Obligations[0].Carried.Verdict != verdict ||
		document.Obligations[0].Carried.Revision != (protocol.Revision{Base: base, Head: head}) {
		t.Fatalf("JSON status omitted original carry evidence: %+v", document)
	}
}

const memoBaseCarryKindForTest = "memo-base-equivalent"
