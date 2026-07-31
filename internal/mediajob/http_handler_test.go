package mediajob

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteMediaErrorClassifiesBareSentinels(t *testing.T) {
	for name, testCase := range map[string]struct {
		err    error
		status int
		code   string
	}{
		"invalid":     {err: ErrInvalid, status: http.StatusBadRequest, code: "invalid_request"},
		"forbidden":   {err: ErrForbidden, status: http.StatusForbidden, code: "forbidden"},
		"not found":   {err: ErrNotFound, status: http.StatusNotFound, code: "job_not_found"},
		"conflict":    {err: ErrConflict, status: http.StatusConflict, code: "state_conflict"},
		"expired":     {err: ErrOutputExpired, status: http.StatusGone, code: "output_expired"},
		"unavailable": {err: ErrUnavailable, status: http.StatusServiceUnavailable, code: "service_unavailable"},
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeMediaError(response, testCase.err)
			if response.Code != testCase.status {
				t.Fatalf("status=%d want %d", response.Code, testCase.status)
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Error.Code != testCase.code {
				t.Fatalf("code=%q want %q", body.Error.Code, testCase.code)
			}
		})
	}
}
