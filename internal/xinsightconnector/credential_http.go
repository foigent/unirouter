package xinsightconnector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

func credentialBasePath(id string) string {
	return "/api/v1/connector-control/installations/" + url.PathEscape(id) + "/credentials"
}

func (counterpart *HTTPControlCounterpart) RunCredential(ctx context.Context) error {
	if counterpart == nil || counterpart.journal == nil || counterpart.client == nil {
		return ErrUnavailable
	}
	id := counterpart.installation.InstallationID
	prepared, err := counterpart.journal.PrepareRotate(ctx, id)
	if err != nil {
		return err
	}
	if prepared.request.XInsightOrigin != counterpart.origin || prepared.request.DeploymentID != counterpart.installation.XDeploymentID ||
		prepared.request.ProjectID != counterpart.installation.ControlProjectID {
		return ErrConflict
	}
	return counterpart.completeCredential(ctx, prepared)
}

func (counterpart *HTTPControlCounterpart) ResumeCredential(ctx context.Context) error {
	prepared, err := counterpart.journal.PendingCredential(ctx, counterpart.installation.InstallationID)
	if err != nil {
		return err
	}
	if prepared.request.XInsightOrigin != counterpart.origin || prepared.request.DeploymentID != counterpart.installation.XDeploymentID {
		return ErrConflict
	}
	return counterpart.completeCredential(ctx, prepared)
}

func (counterpart *HTTPControlCounterpart) completeCredential(ctx context.Context, op preparedCredentialOperation) error {
	id := op.request.InstallationID
	if err := counterpart.journal.ValidateCredentialOrigin(ctx, id, counterpart.origin); err != nil {
		return err
	}
	if op.state == "committed" && op.receipt != nil {
		return counterpart.journal.PromoteCredential(ctx, id, op.request.OperationID)
	}
	base := credentialBasePath(id)
	queryPath := base + "/operations/" + url.PathEscape(op.request.OperationID)
	response, err := counterpart.request(ctx, http.MethodGet, queryPath, nil, op.replacement)
	if err != nil {
		return err
	}
	// An as-yet-unregistered replacement is rejected by XI's service-auth
	// middleware with 401 before the query handler can return 404. Neither
	// status proves absence or success; resend only the same durable request.
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusUnauthorized {
		response.Body.Close()
		if op.request.Kind == "rotate" {
			status, err := counterpart.journal.CredentialStatus(ctx, id)
			if err != nil || !status.CurrentExpiresAt.After(counterpart.journal.now().UTC()) {
				return ErrConflict
			}
		}
		path := base + "/rotate"
		bearer := ""
		if op.request.Kind == "recover" {
			path = base + "/recover"
		} else {
			bearer, err = counterpart.journal.controlToken(ctx, id)
			if err != nil || hashBytes([]byte(bearer)) != op.request.PreviousTokenSHA256 {
				return ErrConflict
			}
		}
		response, err = counterpart.request(ctx, http.MethodPost, path, op.canonical, bearer)
		if err != nil {
			return err
		}
	}
	receipt, err := readCredentialResponse(response)
	if err != nil {
		return err
	}
	if err := counterpart.journal.RecordCredentialReceipt(ctx, op, receipt); err != nil {
		return err
	}
	if receipt.State == "committed" {
		return counterpart.journal.PromoteCredential(ctx, id, op.request.OperationID)
	}
	confirm := CredentialConfirm{SchemaVersion: CredentialRequestSchema, OperationID: op.request.OperationID, RequestSHA256: op.requestSHA256}
	body, err := confirm.CanonicalJSON()
	if err != nil {
		return err
	}
	response, err = counterpart.request(ctx, http.MethodPost, queryPath+"/confirm", body, op.replacement)
	if err != nil {
		return err
	}
	receipt, err = readCredentialResponse(response)
	if err != nil || receipt.State != "committed" {
		return ErrConflict
	}
	if err = counterpart.journal.RecordCredentialReceipt(ctx, op, receipt); err != nil {
		return err
	}
	return counterpart.journal.PromoteCredential(ctx, id, op.request.OperationID)
}

func readCredentialResponse(response *http.Response) (CredentialReceipt, error) {
	if response == nil {
		return CredentialReceipt{}, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return CredentialReceipt{}, connectorHTTPStatusError("credential lifecycle", response.StatusCode)
	}
	if requireConnectorJSONContentType(response.Header) != nil {
		return CredentialReceipt{}, ErrInvalid
	}
	raw, err := readConnectorHTTPBody(response.Body)
	if err != nil {
		return CredentialReceipt{}, err
	}
	return DecodeCredentialReceipt(raw)
}

// ResolveCredentialRecovery uses the original fixed XI origin and no ambient
// browser credentials. The returned binding is non-secret; the ticket remains
// only in the caller's protected request and in the encrypted pending journal.
func (counterpart *HTTPControlCounterpart) ResolveCredentialRecovery(ctx context.Context, ticket string, out any) error {
	if counterpart == nil || !credentialTicketPattern.MatchString(ticket) {
		return ErrInvalid
	}
	body, _ := json.Marshal(struct {
		Ticket string `json:"ticket"`
	}{ticket})
	response, err := counterpart.request(ctx, http.MethodPost, "/api/v1/connector-control/credential-recoveries/resolve", body, "")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return connectorHTTPStatusError("recovery resolve", response.StatusCode)
	}
	if requireConnectorJSONContentType(response.Header) != nil {
		return ErrInvalid
	}
	raw, err := readConnectorHTTPBody(response.Body)
	if err != nil {
		return err
	}
	if rejectDuplicateControlV2Fields(raw) != nil || decodeStrictControlV2(raw, out) != nil {
		return ErrInvalid
	}
	return nil
}
