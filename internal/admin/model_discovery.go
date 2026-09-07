package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/strictjson"
)

const modelDiscoveryMaxBytes = 2 << 20

type discoveredModel struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Provider          string   `json:"provider"`
	InferenceProvider string   `json:"inference_provider"`
	Capabilities      []string `json:"documented_capabilities"`
	Features          []string `json:"documented_features"`
}

type modelDiscoveryResult struct {
	ContractVersion    string            `json:"contract_version"`
	ConnectionID       int64             `json:"connection_id"`
	ConnectionRevision int64             `json:"connection_revision"`
	BindingID          int64             `json:"credential_binding_id"`
	BindingRevision    int64             `json:"credential_revision"`
	ObservedAt         time.Time         `json:"observed_at"`
	Source             string            `json:"source"`
	Page               int               `json:"page"`
	PageSize           int               `json:"page_size"`
	Total              int               `json:"total"`
	Models             []discoveredModel `json:"models"`
}

type modelDiscoverySnapshot struct {
	ConnectionID, ConnectionRevision, BindingRevision int64
	Provider, BaseURL, AdapterType, ConnectionState   string
	DeploymentState, BindingState, Secret             string
	Eligible                                          bool
}

// DiscoverCanonicalModels reads supplier metadata with a currently owned binding.
// It does not run a Probe or modify any execution, pricing or publication state.
func (h *Handler) DiscoverCanonicalModels(w http.ResponseWriter, r *http.Request) {
	org, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	deployment, err := canonicalDeploymentID(r)
	q := r.URL.Query()
	binding, be := strconv.ParseInt(q.Get("credential_binding_id"), 10, 64)
	connectionRev, ce := strconv.ParseInt(q.Get("connection_revision"), 10, 64)
	bindingRev, bre := strconv.ParseInt(q.Get("credential_revision"), 10, 64)
	page, pe := strconv.Atoi(q.Get("page"))
	query := q.Get("query")
	if err != nil || be != nil || ce != nil || bre != nil || pe != nil || binding <= 0 || connectionRev <= 0 || bindingRev <= 0 || page < 1 || page > 1000 || len(query) > 200 || !utf8.ValidString(query) || strings.ContainsAny(query, "\x00\r\n") {
		writeModelDiscoveryError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	snapshot, err := h.loadModelDiscoverySnapshot(ctx, org, deployment, binding)
	if errors.Is(err, pgx.ErrNoRows) {
		writeModelDiscoveryError(w, http.StatusNotFound, "target_not_found")
		return
	}
	if err != nil {
		writeModelDiscoveryError(w, http.StatusServiceUnavailable, "target_unavailable")
		return
	}
	if !snapshot.eligible(connectionRev, bindingRev) {
		writeModelDiscoveryError(w, http.StatusConflict, "target_changed_or_disabled")
		return
	}
	target, err := modelDiscoveryURL(snapshot, page, query)
	if err != nil {
		writeModelDiscoveryError(w, http.StatusUnprocessableEntity, "workspace_discovery_unsupported")
		return
	}
	secret, err := auth.DecryptKey(snapshot.Secret, h.encKey)
	if err != nil || strings.TrimSpace(secret) == "" {
		writeModelDiscoveryError(w, http.StatusConflict, "credential_unavailable")
		return
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		writeModelDiscoveryError(w, http.StatusBadRequest, "invalid_endpoint")
		return
	}
	request.Header.Set("Authorization", "Bearer "+secret)
	request.Header.Set("Accept", "application/json")
	response, err := h.canonicalOperationProbeDoer().Do(request)
	if err != nil {
		writeModelDiscoveryError(w, http.StatusBadGateway, "upstream_unavailable")
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		code := "upstream_rejected"
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			code = "upstream_access_denied"
		}
		writeModelDiscoveryError(w, http.StatusBadGateway, code)
		return
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, modelDiscoveryMaxBytes+1))
	if err != nil || len(body) > modelDiscoveryMaxBytes {
		writeModelDiscoveryError(w, http.StatusBadGateway, "response_limit")
		return
	}
	result, err := decodeModelDiscovery(body, page)
	if err != nil {
		writeModelDiscoveryError(w, http.StatusBadGateway, "response_invalid")
		return
	}
	current, err := h.loadModelDiscoverySnapshot(ctx, org, deployment, binding)
	if err != nil || current != snapshot || !current.eligible(connectionRev, bindingRev) {
		writeModelDiscoveryError(w, http.StatusConflict, "target_changed_or_disabled")
		return
	}
	result.ConnectionID, result.ConnectionRevision = snapshot.ConnectionID, connectionRev
	result.BindingID, result.BindingRevision = binding, bindingRev
	result.ObservedAt = time.Now().UTC()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) loadModelDiscoverySnapshot(ctx context.Context, org, deployment, binding int64) (modelDiscoverySnapshot, error) {
	var s modelDiscoverySnapshot
	err := h.db.QueryRow(ctx, `SELECT c.id,c.config_revision,b.state_revision,c.provider,c.base_url,c.adapter_type,c.admin_state,d.admin_state,b.status,b.key_enc,
 (b.cooldown_until IS NULL OR b.cooldown_until<=statement_timestamp())
 FROM model_deployments d JOIN model_registry m ON m.id=d.public_model_id AND m.org_id=d.customer_org_id
 JOIN provider_connections c ON c.id=d.connection_id
 JOIN credential_binding_deployments link ON link.deployment_id=d.id AND link.connection_id=c.id AND link.binding_id=$3
 JOIN provider_credential_bindings b ON b.id=link.binding_id AND b.connection_id=c.id
 WHERE d.customer_org_id=$1 AND d.id=$2 AND d.retired_at IS NULL AND c.retired_at IS NULL
 AND link.created_at<=statement_timestamp()`, org, deployment, binding).Scan(
		&s.ConnectionID, &s.ConnectionRevision, &s.BindingRevision, &s.Provider, &s.BaseURL, &s.AdapterType,
		&s.ConnectionState, &s.DeploymentState, &s.BindingState, &s.Secret, &s.Eligible)
	return s, err
}

func (s modelDiscoverySnapshot) eligible(connectionRev, bindingRev int64) bool {
	return s.ConnectionRevision == connectionRev && s.BindingRevision == bindingRev && s.ConnectionState == "enabled" &&
		s.DeploymentState == "enabled" && s.BindingState == "active" && s.Eligible && s.Secret != ""
}

func modelDiscoveryURL(s modelDiscoverySnapshot, page int, query string) (string, error) {
	class, err := modelprofile.ClassifyEndpoint(s.Provider, s.BaseURL)
	if err != nil || s.Provider != "dashscope" || s.AdapterType != "openai_compatible" || class != modelprofile.EndpointClassDashScopeBeijingWorkspacePayGo {
		return "", errors.New("unsupported discovery endpoint")
	}
	u, err := url.Parse(s.BaseURL)
	if err != nil || u.Scheme != "https" || u.Port() != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("invalid discovery endpoint")
	}
	u.Path, u.RawPath = "/api/v1/models", ""
	q := url.Values{"page_no": {strconv.Itoa(page)}, "page_size": {"50"}, "capabilities": {"TG"}}
	if query != "" {
		q.Set("name", query)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func decodeModelDiscovery(body []byte, page int) (modelDiscoveryResult, error) {
	result := modelDiscoveryResult{ContractVersion: "unirouter.model-discovery.v1", Source: "provider_catalog", Page: page, PageSize: 50, Models: []discoveredModel{}}
	if strictjson.Validate(body) != nil {
		return result, errors.New("ambiguous discovery data")
	}
	var wire struct {
		Success bool `json:"success"`
		Output  struct {
			Total    int `json:"total"`
			Page     int `json:"page_no"`
			PageSize int `json:"page_size"`
			Models   []struct {
				Model, Name, Provider string
				InferenceProvider     string   `json:"inference_provider"`
				Capabilities          []string `json:"capabilities"`
				Features              []string `json:"features"`
			} `json:"models"`
		} `json:"output"`
	}
	if json.Unmarshal(body, &wire) != nil || !wire.Success || wire.Output.Total < len(wire.Output.Models) || wire.Output.Page != page || wire.Output.PageSize != 50 || wire.Output.Models == nil || len(wire.Output.Models) > 50 {
		return result, errors.New("invalid discovery envelope")
	}
	seen := map[string]bool{}
	for _, m := range wire.Output.Models {
		if m.Model == "" || len(m.Model) > 200 || strings.TrimSpace(m.Model) != m.Model || strings.ContainsAny(m.Model, "\x00\r\n") || seen[m.Model] || len(m.Name) > 500 || len(m.Provider) > 200 || len(m.InferenceProvider) > 200 || len(m.Capabilities) > 32 || len(m.Features) > 64 {
			return result, errors.New("invalid discovery model")
		}
		for _, values := range [][]string{m.Capabilities, m.Features} {
			for _, value := range values {
				if len(value) > 200 || strings.ContainsAny(value, "\x00\r\n") {
					return result, errors.New("invalid discovery label")
				}
			}
		}
		if m.Capabilities == nil {
			m.Capabilities = []string{}
		}
		if m.Features == nil {
			m.Features = []string{}
		}
		seen[m.Model] = true
		result.Models = append(result.Models, discoveredModel{m.Model, m.Name, m.Provider, m.InferenceProvider, m.Capabilities, m.Features})
	}
	result.Total = wire.Output.Total
	return result, nil
}

func writeModelDiscoveryError(w http.ResponseWriter, status int, reason string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]string{"code": "model_discovery_" + reason, "error": "Model discovery did not complete; configuration and qualification are unchanged."})
}
