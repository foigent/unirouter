package xinsightconnector

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	connectorRedeemPath     = "/api/v1/connector-control/invitations/redeem"
	connectorOperationsPath = "/api/v1/connector-control/operations"
	connectorHTTPBodyLimit  = int64(64 * 1024)
)

// HTTPControlCounterpart is an installation-bound client for the three fixed
// X management coordinates. It has no caller-supplied path and never follows
// redirects. Invitation and control plaintext are obtained only from the
// encrypted journal immediately before a request.
type HTTPControlCounterpart struct {
	origin       string
	installation RuntimeInstallationDescriptor
	journal      *PostgresPhaseJournal
	client       *http.Client
}

func NewHTTPControlCounterpart(
	origin string,
	installation RuntimeInstallationDescriptor,
	journal *PostgresPhaseJournal,
	client *http.Client,
	requestTimeout time.Duration,
) (*HTTPControlCounterpart, error) {
	canonicalOrigin, err := validateConnectorControlOrigin(origin)
	if err != nil || journal == nil ||
		!validExactExternalIdentity(installation.InstallationID) ||
		!validExactDeploymentIdentity(installation.XDeploymentID) ||
		requestTimeout < 250*time.Millisecond || requestTimeout > 30*time.Second {
		return nil, ErrInvalid
	}
	if client == nil {
		client = &http.Client{}
	}
	copyClient := *client
	copyClient.Timeout = requestTimeout
	copyClient.Jar = nil
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &HTTPControlCounterpart{
		origin: canonicalOrigin, installation: installation,
		journal: journal, client: &copyClient,
	}, nil
}

// Redeem persists the request identity before sending and finalizes only a
// strictly validated result. It is safe to retry after send/response/process
// loss because PrepareRedeem reconstructs the same canonical bytes.
func (counterpart *HTTPControlCounterpart) Redeem(ctx context.Context) error {
	if counterpart == nil || counterpart.journal == nil || counterpart.client == nil {
		return ErrUnavailable
	}
	prepared, err := counterpart.journal.PrepareRedeem(ctx, counterpart.installation.InstallationID)
	if err != nil {
		return err
	}
	if prepared.InstallationID != counterpart.installation.InstallationID ||
		prepared.XDeploymentID != counterpart.installation.XDeploymentID ||
		(counterpart.installation.ControlProjectID != "" &&
			prepared.ControlProjectID != counterpart.installation.ControlProjectID) {
		return ErrConflict
	}
	response, err := counterpart.request(
		ctx, http.MethodPost, connectorRedeemPath, prepared.CanonicalRequest, "",
	)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return connectorHTTPStatusError("redeem", response.StatusCode)
	}
	if err := requireConnectorJSONContentType(response.Header); err != nil {
		return err
	}
	body, err := readConnectorHTTPBody(response.Body)
	if err != nil {
		return err
	}
	return counterpart.journal.FinalizeRedeemFromOrigin(ctx, prepared, body, counterpart.origin)
}

func (counterpart *HTTPControlCounterpart) Poll(
	ctx context.Context,
) ([]byte, bool, error) {
	if counterpart == nil || counterpart.journal == nil || counterpart.client == nil ||
		!validControlText(counterpart.installation.ControlProjectID, 128) {
		return nil, false, ErrUnavailable
	}
	bearer, err := counterpart.journal.controlToken(ctx, counterpart.installation.InstallationID)
	if err != nil {
		return nil, false, err
	}
	query := url.Values{}
	query.Set("installation_id", counterpart.installation.InstallationID)
	response, err := counterpart.request(
		ctx, http.MethodGet, connectorOperationsPath+"?"+query.Encode(), nil, bearer,
	)
	if err != nil {
		return nil, false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return nil, false, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, false, connectorHTTPStatusError("poll", response.StatusCode)
	}
	if err := requireConnectorJSONContentType(response.Header); err != nil {
		return nil, false, err
	}
	body, err := readConnectorHTTPBody(response.Body)
	if err != nil {
		return nil, false, err
	}
	version, err := ControlSchemaVersion(body)
	if err != nil {
		return nil, false, err
	}
	if version == ControlOperationV3Schema {
		operation, err := DecodeControlOperationV3(body)
		if err != nil {
			return nil, false, err
		}
		canonical, err := operation.CanonicalJSON()
		if err != nil || operation.InstallationID != counterpart.installation.InstallationID || operation.ProjectID != counterpart.installation.ControlProjectID {
			return nil, false, ErrConflict
		}
		return canonical, true, nil
	}
	if version != ControlOperationV2Schema {
		return nil, false, ErrInvalid
	}
	operation, err := DecodeControlOperationV2(body)
	if err != nil {
		return nil, false, err
	}
	canonical, err := operation.CanonicalJSON()
	if err != nil ||
		operation.InstallationID != counterpart.installation.InstallationID ||
		operation.ProjectID != counterpart.installation.ControlProjectID {
		return nil, false, ErrConflict
	}
	// X writes HTTP JSON with json.Encoder, so transport framing and struct
	// field order are not product authority. The Agent consumes only the exact
	// canonical v2 projection derived from the strictly decoded operation.
	return canonical, true, nil
}

// ReadSetupStatus is a separate bounded, read-only projection using the
// original installation control bearer. It does not extend Poll/ACK wire.
func (counterpart *HTTPControlCounterpart) ReadSetupStatus(ctx context.Context) ([]byte, error) {
	tx, err := counterpart.journal.db.Begin(ctx)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer tx.Rollback(ctx)
	origin, err := ReadCompletedRedeemOriginTx(ctx, tx, counterpart.installation.InstallationID, counterpart.installation.XDeploymentID)
	if err != nil || origin != counterpart.origin {
		return nil, ErrConflict
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, ErrUnavailable
	}
	bearer, err := counterpart.journal.controlToken(ctx, counterpart.installation.InstallationID)
	if err != nil {
		return nil, err
	}
	response, err := counterpart.request(ctx, http.MethodGet, "/api/v1/connector-control/installations/"+url.PathEscape(counterpart.installation.InstallationID)+"/setup", nil, bearer)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, connectorHTTPStatusError("setup status", response.StatusCode)
	}
	if requireConnectorJSONContentType(response.Header) != nil {
		return nil, ErrInvalid
	}
	return readConnectorHTTPBody(response.Body)
}

func (counterpart *HTTPControlCounterpart) Acknowledge(
	ctx context.Context,
	canonicalResult []byte,
) error {
	if counterpart == nil || counterpart.journal == nil || counterpart.client == nil {
		return ErrUnavailable
	}
	version, err := ControlSchemaVersion(canonicalResult)
	if err != nil {
		return err
	}
	if version == ControlResultV3Schema {
		result, err := DecodeControlResultV3(canonicalResult)
		if err != nil {
			return err
		}
		canonical, err := result.CanonicalJSON()
		if err != nil || !bytes.Equal(canonical, canonicalResult) || result.InstallationID != counterpart.installation.InstallationID {
			return ErrConflict
		}
		return counterpart.acknowledgeV3(ctx, result.OperationID, canonicalResult)
	}
	if version != ControlResultV2Schema {
		return ErrInvalid
	}
	result, err := DecodeControlResultV2(canonicalResult)
	if err != nil {
		return err
	}
	canonical, err := result.CanonicalJSON()
	if err != nil || !bytes.Equal(canonical, canonicalResult) ||
		result.InstallationID != counterpart.installation.InstallationID {
		return ErrConflict
	}
	bearer, err := counterpart.journal.controlToken(ctx, counterpart.installation.InstallationID)
	if err != nil {
		return err
	}
	response, err := counterpart.request(
		ctx, http.MethodPost, connectorOperationsPath+"/"+url.PathEscape(result.OperationID),
		canonicalResult, bearer,
	)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return connectorHTTPStatusError("acknowledge", response.StatusCode)
	}
	_, err = readConnectorHTTPBody(response.Body)
	return err
}

func (counterpart *HTTPControlCounterpart) request(
	ctx context.Context,
	method, path string,
	body []byte,
	bearer string,
) (*http.Response, error) {
	request, err := http.NewRequestWithContext(
		ctx, method, counterpart.origin+path, bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: build Connector control request", ErrUnavailable)
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	if path == connectorRedeemPath &&
		(request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "") {
		return nil, ErrConflict
	}
	response, err := counterpart.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: Connector control request failed", ErrUnavailable)
	}
	return response, nil
}

func validateConnectorControlOrigin(raw string) (string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return "", ErrInvalid
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" ||
		parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return "", ErrInvalid
	}
	return "https://" + parsed.Host, nil
}

func requireConnectorJSONContentType(header http.Header) error {
	contentType := header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		return fmt.Errorf("%w: Connector control response content type is invalid", ErrUnavailable)
	}
	return nil
}

func readConnectorHTTPBody(body io.Reader) ([]byte, error) {
	if body == nil {
		return nil, ErrUnavailable
	}
	bounded, err := io.ReadAll(io.LimitReader(body, connectorHTTPBodyLimit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read Connector control response", ErrUnavailable)
	}
	if int64(len(bounded)) > connectorHTTPBodyLimit {
		return nil, fmt.Errorf("%w: Connector control response exceeds bound", ErrUnavailable)
	}
	return bounded, nil
}

func connectorHTTPStatusError(action string, status int) error {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusConflict, http.StatusGone, http.StatusServiceUnavailable:
		return fmt.Errorf("%w: Connector control %s returned status %d", ErrUnavailable, action, status)
	default:
		return fmt.Errorf("%w: Connector control %s returned unexpected status %d", ErrUnavailable, action, status)
	}
}

func (counterpart *HTTPControlCounterpart) acknowledgeV3(ctx context.Context, operationID string, canonical []byte) error {
	bearer, err := counterpart.journal.controlToken(ctx, counterpart.installation.InstallationID)
	if err != nil {
		return err
	}
	response, err := counterpart.request(ctx, http.MethodPost, connectorOperationsPath+"/"+url.PathEscape(operationID), canonical, bearer)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return connectorHTTPStatusError("acknowledge", response.StatusCode)
	}
	_, err = readConnectorHTTPBody(response.Body)
	return err
}
