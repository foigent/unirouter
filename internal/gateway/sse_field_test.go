package gateway

import "testing"

func TestParseSSEFieldLineTreatsOptionalSpaceEquivalently(t *testing.T) {
	for _, line := range []string{
		`data:{"kind":"placeholder"}`,
		`data: {"kind":"placeholder"}`,
	} {
		field, ok := parseSSEFieldLine(line)
		if !ok || field.name != "data" || field.value != `{"kind":"placeholder"}` {
			t.Fatalf("unexpected parsed data field for %q: ok=%t field=%+v", line, ok, field)
		}
	}
}

func TestParseSSEFieldLineUsesFirstColonAndRemovesAtMostOneSpace(t *testing.T) {
	tests := []struct {
		line  string
		name  string
		value string
		ok    bool
	}{
		{line: `data: {"value":"a:b"}`, name: "data", value: `{"value":"a:b"}`, ok: true},
		{line: `data:  kept`, name: "data", value: " kept", ok: true},
		{line: "data", name: "data", value: "", ok: true},
		{line: "event: message", name: "event", value: "message", ok: true},
		{line: "id: 7", name: "id", value: "7", ok: true},
		{line: "retry: 1000", name: "retry", value: "1000", ok: true},
		{line: "Data: value", name: "Data", value: "value", ok: true},
		{line: ": comment", ok: false},
		{line: "", ok: false},
	}
	for _, test := range tests {
		field, ok := parseSSEFieldLine(test.line)
		if ok != test.ok || field.name != test.name || field.value != test.value {
			t.Fatalf("parse %q: got ok=%t field=%+v", test.line, ok, field)
		}
	}
}
