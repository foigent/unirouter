package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelqualification"
)

func TestModelOperationProductionApprovalIngressIsStrictAndBounded(t *testing.T) {
	valid := `{"probe_id":7,"expected_connection_revision":2,"expected_deployment_revision":3,"expected_operation_revision":4,"expected_executor_contract_version":"` + modelqualification.ExecutorContractVersion + `","reason":"reviewed exact live probe"}`
	for _, test := range []struct {
		name string
		body string
		ok   bool
	}{
		{name: "valid", body: valid, ok: true},
		{name: "unknown member", body: strings.TrimSuffix(valid, "}") + `,"production":true}`},
		{name: "duplicate", body: strings.Replace(valid, `"probe_id":7`, `"probe_id":7,"probe_id":8`, 1)},
		{name: "wrong executor", body: strings.Replace(valid, modelqualification.ExecutorContractVersion, "future.executor.v2", 1)},
		{name: "line break reason", body: strings.Replace(valid, "reviewed exact live probe", `reviewed\nprobe`, 1)},
		{name: "oversized", body: valid + strings.Repeat(" ", modelOperationProductionApprovalMaxRequestBytes)},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/approval", strings.NewReader(test.body))
			result, err := decodeModelOperationProductionApprovalRequest(httptest.NewRecorder(), request)
			if test.ok {
				require.NoError(t, err)
				require.Equal(t, int64(7), result.ProbeID)
				return
			}
			require.ErrorIs(t, err, errModelOperationProductionApprovalInvalid)
		})
	}
}

func TestModelOperationProductionApprovalReplayBindsActorAndCustomerOrganizations(t *testing.T) {
	existing := modelOperationProductionApprovalResponse{
		ProbeID: 7, CustomerOrgID: 41, DeploymentID: 51,
		OperationID: "openai.responses.v1", ApprovedBy: 61, ApprovedByOrgID: 71,
		SourceConnectionRevision: 2, SourceDeploymentRevision: 3,
		SourceOperationRevision: 4, ExecutorContractVersion: modelqualification.ExecutorContractVersion,
		Reason: "reviewed exact live probe", Qualified: true, QualificationState: "production_qualified",
	}
	request := modelOperationProductionApprovalRequest{
		ProbeID: 7, ExpectedConnectionRevision: 2, ExpectedDeploymentRevision: 3,
		ExpectedOperationRevision: 4, ExpectedExecutorContractVersion: modelqualification.ExecutorContractVersion,
		Reason: "reviewed exact live probe",
	}
	require.True(t, modelOperationProductionApprovalReplayMatches(
		existing, 61, 71, 41, 51, "openai.responses.v1", request,
	))
	require.False(t, modelOperationProductionApprovalReplayMatches(
		existing, 61, 41, 41, 51, "openai.responses.v1", request,
	), "forged actor organization must not replay another actor-org receipt")
	require.False(t, modelOperationProductionApprovalReplayMatches(
		existing, 61, 71, 71, 51, "openai.responses.v1", request,
	), "a receipt cannot move to another customer organization")
	existing.Qualified = false
	existing.QualificationState = "connection_revision_stale"
	require.True(t, modelOperationProductionApprovalReplayMatches(
		existing, 61, 71, 41, 51, "openai.responses.v1", request,
	), "exact historical receipt replay must expose current staleness instead of changing receipt identity")
}
