package admin

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestNormalizeModelMappingKeepsPublicAndUpstreamIdentitySeparate(t *testing.T) {
	encoded, err := normalizeModelMapping(json.RawMessage(`{
		" company-fast ": "deepseek/deepseek-v4-flash",
		"company-reasoning": "accounts/provider/models/deepseek-r1"
	}`), "company-fast; company-reasoning")
	if err != nil {
		t.Fatalf("normalize mapping: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("decode mapping: %v", err)
	}
	if got["company-fast"] != "deepseek/deepseek-v4-flash" || got["company-reasoning"] != "accounts/provider/models/deepseek-r1" {
		t.Fatalf("unexpected mapping: %#v", got)
	}
}

func TestNormalizeModelMappingRejectsAmbiguousOrUnconfiguredIdentity(t *testing.T) {
	cases := []json.RawMessage{
		json.RawMessage(`{"unknown":"supplier-model"}`),
		json.RawMessage(`{"company-fast":"supplier;other"}`),
		json.RawMessage(`{"company-fast":"supplier-a"," company-fast ":"supplier-b"}`),
		json.RawMessage(`[]`),
	}
	for _, input := range cases {
		if _, err := normalizeModelMapping(input, "company-fast"); !errors.Is(err, errInvalidModelMapping) {
			t.Fatalf("input %s error = %v, want errInvalidModelMapping", input, err)
		}
	}
}

func TestNormalizeModelMappingDefaultsToEmptyObject(t *testing.T) {
	for _, input := range []json.RawMessage{nil, json.RawMessage(`null`)} {
		encoded, err := normalizeModelMapping(input, "company-fast")
		if err != nil || string(encoded) != `{}` {
			t.Fatalf("input %s encoded=%s err=%v", input, encoded, err)
		}
	}
}
