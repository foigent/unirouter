package admin

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

type contentEnabledControllerFixture struct{}

func (contentEnabledControllerFixture) Enabled() bool                 { return true }
func (contentEnabledControllerFixture) ContentEnabled() bool          { return true }
func (contentEnabledControllerFixture) SourceInstance() string        { return "synthetic-u1" }
func (contentEnabledControllerFixture) ProfilesEnabled([]string) bool { return true }

func TestContentCoordinatorRejectsBeforeSQL(t *testing.T) {
	h := &Handler{sourceFactsController: contentEnabledControllerFixture{}}
	registration := sourcefacts.Registration{SchemaVersion: sourcefacts.RegistrationVersion, TokenFormat: auth.ReceiverFeedTokenFormat, VerifierScheme: auth.ReceiverFeedVerifierScheme, KeyPrefix: "ur-xf1-0000000000001", VerifierSHA256: strings.Repeat("1", 64), OperationID: strings.Repeat("2", 32), SourceInstance: "synthetic-u1", OrgID: "1", ProjectID: "2", Profiles: []string{sourcefacts.ContentProfile}}
	request := contentControlRequest{OperationID: registration.OperationID, ExpectedStateVersion: "0", Registration: &registration, AllowedIP: "127.0.0.1/32"}
	for _, name := range []string{"no_actor", "wrong_org", "wrong_project", "wrong_source", "wrong_operation", "wrong_action"} {
		t.Run(name, func(t *testing.T) {
			copyRegistration := registration
			copyRequest := request
			copyRequest.Registration = &copyRegistration
			action := "prepare"
			want := errXInsightFeedInvalidScope
			switch name {
			case "no_actor":
				want = errAPIKeyAuthorizationChanged
			case "wrong_org":
				copyRegistration.OrgID = "9"
			case "wrong_project":
				copyRegistration.ProjectID = "9"
			case "wrong_source":
				copyRegistration.SourceInstance = "other-source"
			case "wrong_operation":
				copyRegistration.OperationID = strings.Repeat("3", 32)
			case "wrong_action":
				action = "invalid"
			}
			// Nil tx fails on any SQL access. The enabled, otherwise valid
			// no_actor case must reach and return the original actor rejection.
			_, err := h.contentControlTx(context.Background(), nil, 1, 2, action, copyRequest, httptest.NewRequest("POST", "/synthetic", nil))
			if !errors.Is(err, want) {
				t.Fatalf("got %v, want %v", err, want)
			}
		})
	}
}
