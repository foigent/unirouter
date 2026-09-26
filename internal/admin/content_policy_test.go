package admin

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/uniroute/unirouter/internal/contentcapture"
)

type uncalledContentPolicy struct{ contentPolicyController }

func TestContentPolicyRejectsNoActorBeforeReadinessAndSQL(t *testing.T) {
	for _, method := range []string{"GET", "PUT"} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			h := &Handler{contentPolicyController: &uncalledContentPolicy{}, contentPolicyReadiness: func(context.Context) (contentcapture.PolicyApplication, error) {
				calls++
				return contentcapture.PolicyApplication{}, nil
			}}
			r := httptest.NewRequest(method, "/api/v1/projects/2/payload-policy", nil)
			r.SetPathValue("id", "2")
			w := httptest.NewRecorder()
			h.projectPayloadPolicy(w, r, method == "PUT")
			if w.Code != 403 || calls != 0 {
				t.Fatalf("status=%d readiness_calls=%d", w.Code, calls)
			}
		})
	}
}
