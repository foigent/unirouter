package strictjson

import (
	"errors"
	"strings"
	"testing"
)

func TestCanonicalizeRejectsDuplicateMembersAtEveryLevel(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "root", body: `{"model":"one","model":"two"}`},
		{name: "message", body: `{"messages":[{"role":"user","content":"one","content":"two"}]}`},
		{name: "content part", body: `{"messages":[{"content":[{"type":"text","text":"one","text":"two"}]}]}`},
		{name: "tool", body: `{"tools":[{"function":{"name":"one","name":"two"}}]}`},
		{name: "escaped equivalent", body: `{"model":"one","\u006dodel":"two"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Canonicalize([]byte(test.body)); !errors.Is(err, ErrDuplicateKey) {
				t.Fatalf("error=%v, want ErrDuplicateKey", err)
			}
		})
	}
}

func TestCanonicalizePreservesLargeNumberPrecisionAndNormalizesObjects(t *testing.T) {
	canonical, err := Canonicalize([]byte(` { "z": 900719925474099312345678901234567890, "a": { "n": -1.2300e+45 } } `))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":{"n":-1.2300e+45},"z":900719925474099312345678901234567890}`
	if string(canonical) != want {
		t.Fatalf("canonical=%s, want %s", canonical, want)
	}
}

func TestCanonicalizeRejectsIncompleteAndTrailingDocuments(t *testing.T) {
	for _, body := range []string{"", `{`, `{"model":"one"} {"model":"two"}`, `{"model":"one"} private-secret`} {
		if canonical, err := Canonicalize([]byte(body)); err == nil {
			t.Fatalf("body=%q canonical=%s, want error", body, canonical)
		}
	}
}

func TestCanonicalizeLeavesUnknownFieldPolicyToCaller(t *testing.T) {
	canonical, err := Canonicalize([]byte(`{"model":"gpt","future_extension":{"opaque":[true,null,1]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(canonical), `"future_extension":{"opaque":[true,null,1]}`) {
		t.Fatalf("unknown extension was not retained in canonical form: %s", canonical)
	}
}
