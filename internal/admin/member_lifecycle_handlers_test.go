package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/memberlifecycle"
)

func TestMemberLifecycleOfflineContractIsNoStoreAndVendorNeutral(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/member-lifecycle/offline-contract", nil)
	recorder := httptest.NewRecorder()
	(&Handler{}).MemberLifecycleOfflineContract(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control=%q", recorder.Header().Get("Cache-Control"))
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		memberlifecycle.ContractVersion,
		`"directory_vendor_selected":false`,
		`"idp_network_access":false`,
		`"runtime_mutation":"none"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in %s", expected, body)
		}
	}
	for _, forbidden := range []string{`"okta"`, `"entra"`, `"scim_vendor"`, `"credential_value"`, `"access_token"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("contract leaked or selected %q: %s", forbidden, body)
		}
	}
}

func TestPreviewMemberLifecycleReturnsNoMutationReport(t *testing.T) {
	events, err := memberlifecycle.SampleEventsBytes()
	if err != nil {
		t.Fatal(err)
	}
	var samples []json.RawMessage
	if err := json.Unmarshal(events, &samples); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/member-lifecycle/offline-preview", strings.NewReader(string(samples[2])))
	recorder := httptest.NewRecorder()
	(&Handler{}).PreviewMemberLifecycle(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var report memberlifecycle.Report
	if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.RuntimeMutation != memberlifecycle.RuntimeMutationNone ||
		len(report.EffectiveMemberships) != 0 ||
		len(report.EffectiveCapabilities) != 0 ||
		!report.Controls.DenyAdminSessions {
		t.Fatalf("unexpected report=%#v", report)
	}
}

func TestPreviewMemberLifecycleRejectsInvalidAndOversizedInputs(t *testing.T) {
	for name, test := range map[string]struct {
		body     string
		expected int
	}{
		"invalid":   {body: `{}`, expected: http.StatusBadRequest},
		"oversized": {body: strings.Repeat("x", memberlifecycle.MaxInputBytes+1), expected: http.StatusRequestEntityTooLarge},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/member-lifecycle/offline-preview", strings.NewReader(test.body))
			recorder := httptest.NewRecorder()
			(&Handler{}).PreviewMemberLifecycle(recorder, request)
			if recorder.Code != test.expected {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
