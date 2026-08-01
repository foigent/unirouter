package admin

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeStrictJSONBody(t *testing.T) {
	type body struct {
		Version int `json:"version"`
	}

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid with trailing whitespace", input: "{\"version\":1}\n\t", wantErr: false},
		{name: "unknown field", input: "{\"version\":1,\"unexpected\":true}", wantErr: true},
		{name: "second json value", input: "{\"version\":1} {\"version\":2}", wantErr: true},
		{name: "malformed", input: "{\"version\":", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/runtime-changes", strings.NewReader(test.input))
			var decoded body
			err := decodeStrictJSONBody(request, &decoded)
			if (err != nil) != test.wantErr {
				t.Fatalf("decodeStrictJSONBody() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && decoded.Version != 1 {
				t.Fatalf("decoded version = %d, want 1", decoded.Version)
			}
		})
	}
}

func TestDecodeStrictJSONBodyRejectsOversizedWhitespaceSuffix(t *testing.T) {
	const maxBodyBytes = 2 << 20
	request := httptest.NewRequest("POST", "/runtime-changes", strings.NewReader("{\"version\":1}"+strings.Repeat(" ", maxBodyBytes)))
	var decoded struct {
		Version int `json:"version"`
	}
	if err := decodeStrictJSONBody(request, &decoded); err == nil || err.Error() != "JSON body too large" {
		t.Fatalf("expected explicit oversized-body rejection, got %v", err)
	}
}
