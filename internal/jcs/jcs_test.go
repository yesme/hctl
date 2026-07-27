package jcs

import (
	"strings"
	"testing"
)

func TestCanonicalizeEquivalentObjects(t *testing.T) {
	t.Parallel()
	inputs := []string{
		`{"kind":"gate","aspect":{"gater_seat":"grok","gate_id":"adversarial"},"target":"p1-kernel"}`,
		"{\n  \"target\":\"p1-kernel\", \"aspect\":{\"gate_id\":\"adversarial\",\"gater_seat\":\"grok\"},\"kind\":\"gate\"}",
		`{"target":"p1-kernel","kind":"g\u0061te","aspect":{"gate_id":"adversarial","gater_seat":"grok"}}`,
	}
	var want string
	for i, input := range inputs {
		got, err := Canonicalize([]byte(input))
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if i == 0 {
			want = string(got)
		} else if string(got) != want {
			t.Fatalf("case %d:\n got %s\nwant %s", i, got, want)
		}
	}
}

func TestRejectDuplicateAndLoneSurrogate(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		`{"a":1,"a":2}`,
		`{"a":"\ud800"}`,
		`{"a":"\udc00"}`,
		`{"a":"\ud800\u0061"}`,
	} {
		if _, err := Canonicalize([]byte(input)); err == nil {
			t.Errorf("expected rejection for %s", input)
		}
	}
}

func TestUTF16PropertyOrder(t *testing.T) {
	t.Parallel()
	// U+10000 sorts before U+E000 by UTF-16 code units, unlike code point order.
	got, err := Canonicalize([]byte(`{"\ue000":1,"\ud800\udc00":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), `{"𐀀":2,`) {
		t.Fatalf("unexpected UTF-16 ordering: %s", got)
	}
}

func TestECMAScriptNumberThresholds(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		`1e-6`: `0.000001`,
		`1e-7`: `1e-7`,
		`1e20`: `100000000000000000000`,
		`1e21`: `1e+21`,
		`-0.0`: `0`,
	}
	for input, want := range cases {
		got, err := Canonicalize([]byte(input))
		if err != nil {
			t.Fatalf("%s: %v", input, err)
		}
		if string(got) != want {
			t.Errorf("%s: got %s want %s", input, got, want)
		}
	}
}
