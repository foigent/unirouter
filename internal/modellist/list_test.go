package modellist

import (
	"reflect"
	"testing"
)

func TestParseNormalizeAndContains(t *testing.T) {
	input := " kimi-k3;kimi-k2.7-code；kimi-k2.6\n kimi-k3 "
	want := []string{"kimi-k3", "kimi-k2.7-code", "kimi-k2.6"}
	if got := Parse(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse() = %#v, want %#v", got, want)
	}
	if got := Normalize(input); got != "kimi-k3,kimi-k2.7-code,kimi-k2.6" {
		t.Fatalf("Normalize() = %q", got)
	}
	if !Contains(input, "kimi-k2.7-code") || Contains(input, "kimi-k2") {
		t.Fatal("Contains() did not enforce exact membership")
	}
}
