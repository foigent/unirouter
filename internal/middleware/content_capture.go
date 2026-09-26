package middleware

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
)

// CaptureContent belongs inside repeated live auth and license validation, but
// before strict JSON/guardrail rewriting. Its failures never change execution.
func CaptureContent(starter contentcapture.Starter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if starter == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			operation, ok := modeloperation.LookupRequest(r.Method, r.URL.Path)
			key := GetAPIKeyFromContext(r.Context())
			body, retained := payloadpolicy.BodyFromContext(r.Context())
			if !ok || !operation.Paid || key == nil || key.ProjectID == nil || *key.ProjectID <= 0 || !retained {
				next.ServeHTTP(w, r)
				return
			}
			id := contentcapture.Observation{OrgID: key.OrgID, ProjectID: *key.ProjectID, RequestID: GetRequestIDFromContext(r.Context()), Protocol: string(operation.ID), MediaType: r.Header.Get("Content-Type")}
			contentcapture.ObserveBytes(r.Context(), starter, id, body)
			wrapped, finish := contentcapture.ObserveResponse(r.Context(), starter, id, w)
			completed := false
			defer func() {
				reason := "none"
				if !completed {
					reason = "stream_interrupted"
				}
				if r.Context().Err() != nil {
					reason = "request_cancelled"
				}
				finish(reason)
			}()
			next.ServeHTTP(wrapped, r)
			completed = true
		})
	}
}
