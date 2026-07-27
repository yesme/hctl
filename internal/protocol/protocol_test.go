package protocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const (
	sha1A = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sha1B = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	sha1C = "cccccccccccccccccccccccccccccccccccccccc"
)

func testObligation(t *testing.T, kind string, aspect *GateAspect) Obligation {
	t.Helper()
	o, err := NewStaticObligation(
		"p1-kernel", sha1A, kind, "p1-kernel", aspect,
		Source{Commit: sha1B, Path: ".hctl/assignments/p1-kernel.toml", Blob: sha1A},
	)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestObligationIdentityIgnoresWireOrdering(t *testing.T) {
	t.Parallel()
	o := testObligation(t, "gate", &GateAspect{GateID: "adversarial", GaterSeat: "grok"})
	rawA, err := json.Marshal(o.Preimage)
	if err != nil {
		t.Fatal(err)
	}
	rawB := []byte(`{"target":"p1-kernel","kind":"gate","assignment":{"blob":"` + sha1A + `","id":"p1-kernel"},"aspect":{"gater_seat":"grok","gate_id":"adversarial"}}`)
	var p ObligationPreimage
	if err := json.Unmarshal(rawB, &p); err != nil {
		t.Fatal(err)
	}
	idA, err := ObligationID(o.Preimage)
	if err != nil {
		t.Fatal(err)
	}
	idB, err := ObligationID(p)
	if err != nil {
		t.Fatal(err)
	}
	if idA != idB {
		t.Fatalf("identity changed with ordering:\n%s\n%s\nwire=%s", idA, idB, rawA)
	}
}

func TestParseClaimAndRejectIdentityMismatch(t *testing.T) {
	t.Parallel()
	o := testObligation(t, "author", nil)
	claim := Claim{
		SchemaVersion:     1,
		Actor:             Actor{Seat: "codex", Machine: "machine", Session: nil},
		CreatedAt:         time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC),
		Obligation:        o,
		TipAtClaimPresent: true,
	}
	raw, err := MarshalClaim(claim)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseEvent(raw, "codex"); err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(raw), o.ID, "sha256:"+strings.Repeat("0", 64), 1)
	if _, err := ParseEvent([]byte(broken), "codex"); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("expected identity mismatch, got %v", err)
	}
}

func TestUnknownEventIsUnsupportedNotCorrupt(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
	  "schema_version": 1,
	  "type": "FUTURE",
	  "actor": {"seat":"codex","machine":"m","session":null},
	  "created_at":"2026-07-27T00:00:00Z",
	  "payload": {}
	}`)
	_, err := ParseEvent(raw, "codex")
	var unsupported *UnsupportedError
	if !strings.Contains(err.Error(), "UNSUPPORTED_FACTS") || !asUnsupported(err, &unsupported) {
		t.Fatalf("expected unsupported error, got %v", err)
	}
}

func asUnsupported(err error, target **UnsupportedError) bool {
	for err != nil {
		if value, ok := err.(*UnsupportedError); ok {
			*target = value
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			break
		}
		err = u.Unwrap()
	}
	return false
}

func TestDuplicateKeyIsCorrupt(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"schema_version":1,"schema_version":1,"type":"CLAIM","actor":{"seat":"codex","machine":"m","session":null},"created_at":"2026-07-27T00:00:00Z"}`)
	_, err := ParseEvent(raw, "codex")
	if err == nil || !strings.Contains(err.Error(), "CORRUPT_CHAIN") {
		t.Fatalf("expected corrupt error, got %v", err)
	}
}

func TestEventTimeMustBeUTC(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
	  "schema_version": 1,
	  "type": "FUTURE",
	  "actor": {"seat":"codex","machine":"m","session":null},
	  "created_at":"2026-07-27T08:00:00+08:00"
	}`)
	if _, err := ParseEvent(raw, "codex"); err == nil || !strings.Contains(err.Error(), "UTC") {
		t.Fatalf("expected non-UTC timestamp rejection, got %v", err)
	}
}

func TestMemoPathIsLexicallyContained(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"memory/review.md", "memory/codex-2026-07-27.md"} {
		if !ValidMemoPath(value) {
			t.Errorf("valid memo path rejected: %q", value)
		}
	}
	for _, value := range []string{"memory/", "memory/../.hctl/seats.toml", "/memory/review.md", "memory/a\nb"} {
		if ValidMemoPath(value) {
			t.Errorf("unsafe memo path accepted: %q", value)
		}
	}
}
