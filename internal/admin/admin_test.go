package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewHandler(t *testing.T) {
	h := NewHandler(nil, "test-secret", []byte("0123456789abcdef01234567"), nil)
	if h == nil {
		t.Fatal("NewHandler should not return nil")
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]interface{}{
		"data": []interface{}{},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if _, ok := resp["data"]; !ok {
		t.Fatal("expected 'data' key in response")
	}
}

func TestWriteJSON_VariousStatuses(t *testing.T) {
	statuses := []int{200, 201, 400, 401, 404, 500}
	for _, status := range statuses {
		rec := httptest.NewRecorder()
		writeJSON(rec, status, map[string]string{"msg": "test"})
		if rec.Code != status {
			t.Fatalf("status %d: got %d", status, rec.Code)
		}
	}
}

func TestWriteJSON_NilData(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, 200, nil)
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// nil encodes to "null\n"
	body := rec.Body.String()
	if body != "null\n" {
		t.Fatalf("expected 'null\\n', got %q", body)
	}
}

func TestWriteJSON_ComplexData(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, 200, map[string]interface{}{
		"data": []map[string]interface{}{
			{"id": 1, "name": "test"},
			{"id": 2, "name": "test2"},
		},
		"total": 2,
	})
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	total := resp["total"].(float64)
	if total != 2 {
		t.Fatalf("expected total=2, got %v", total)
	}
}

func TestHandler_Fields(t *testing.T) {
	encKey := []byte("0123456789abcdef01234567")
	h := &Handler{
		db:        nil,
		jwtSecret: "my-secret",
		encKey:    encKey,
	}
	if h.jwtSecret != "my-secret" {
		t.Fatal("jwtSecret mismatch")
	}
	if len(h.encKey) != 24 {
		t.Fatalf("encKey length mismatch: %d", len(h.encKey))
	}
}

func TestListChannels_NilDB(t *testing.T) {
	// Verify that calling with nil DB panics (expected behavior - guards are in middleware)
	h := NewHandler(nil, "s", []byte("0123456789abcdef01234567"), nil)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic with nil DB")
		}
	}()
	req := httptest.NewRequest("GET", "/admin/channels", nil)
	rec := httptest.NewRecorder()
	h.ListChannels(rec, req)
}

func TestJoinStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		sep      string
		expected string
	}{
		{"empty", []string{}, ", ", ""},
		{"single", []string{"a"}, ", ", "a"},
		{"two", []string{"a", "b"}, ", ", "a, b"},
		{"three", []string{"a", "b", "c"}, ", ", "a, b, c"},
		{"no_separator", []string{"a", "b"}, "", "ab"},
		{"different_sep", []string{"x", "y", "z"}, " | ", "x | y | z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinStrings(tt.input, tt.sep)
			if got != tt.expected {
				t.Errorf("joinStrings(%v, %q) = %q, want %q", tt.input, tt.sep, got, tt.expected)
			}
		})
	}
}

func TestWriteJSON_StatusCodePriority(t *testing.T) {
	// Verify that writeJSON writes the status code BEFORE the body
	rec := httptest.NewRecorder()
	writeJSON(rec, 201, map[string]string{"created": "yes"})
	if rec.Code != 201 {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
}
