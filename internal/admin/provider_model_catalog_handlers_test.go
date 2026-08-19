package admin

import (
	"strings"
	"testing"
)

func TestDecodeProviderModelCatalogReturnsSortedExactUniqueIDs(t *testing.T) {
	items, err := decodeProviderModelCatalog([]byte(`{
		"data":[
			{"id":"kimi-k3","owned_by":" moonshot ","context_length":262144},
			{"id":"kimi-k2.7-code","context_length":131072},
			{"id":"kimi-k3","context_length":1},
			{"id":"Kimi K2.7 Code"},
			{"id":"bad\nmodel"}
		]
	}`))
	if err != nil {
		t.Fatalf("decode provider catalog: %v", err)
	}
	if len(items) != 2 || items[0].ID != "kimi-k2.7-code" || items[1].ID != "kimi-k3" {
		t.Fatalf("unexpected provider catalog: %#v", items)
	}
	if items[1].OwnedBy != "moonshot" || items[1].ContextLength != 262144 {
		t.Fatalf("provider metadata was not normalized: %#v", items[1])
	}
}

func TestDecodeProviderModelCatalogRejectsMissingDataEnvelope(t *testing.T) {
	for _, body := range []string{`{}`, `{"data":null}`, `{"data":{}}`, `not-json`} {
		if _, err := decodeProviderModelCatalog([]byte(body)); err == nil {
			t.Fatalf("catalog body %s was accepted", body)
		}
	}
}

func TestProviderCatalogErrorReturnsOnlyNormalizedProviderMessage(t *testing.T) {
	message := providerCatalogError([]byte(`{"error":{"message":" permission denied "}}`))
	if !strings.Contains(message, "permission denied") {
		t.Fatalf("unexpected provider error: %q", message)
	}
	if message := providerCatalogError([]byte(`<html>sensitive proxy page</html>`)); strings.Contains(message, "sensitive") {
		t.Fatalf("unrecognized upstream body leaked into the error: %q", message)
	}
}
