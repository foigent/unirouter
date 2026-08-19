package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
)

const providerModelCatalogMaxBytes = 1 << 20

type providerModelCatalogItem struct {
	ID            string `json:"id"`
	OwnedBy       string `json:"owned_by,omitempty"`
	ContextLength int64  `json:"context_length,omitempty"`
}

// ListChannelProviderModels asks the already configured provider credential
// for its live /v1/models catalog. It never infers model IDs from marketing
// names and never returns the credential or the upstream response body.
func (h *Handler) ListChannelProviderModels(w http.ResponseWriter, r *http.Request) {
	channelID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || channelID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid channel ID"})
		return
	}
	var baseURL, provider, adapterType string
	var adapterConfig json.RawMessage
	if err := h.db.QueryRow(r.Context(), `
SELECT base_url,provider,COALESCE(adapter_type,'openai_compatible'),COALESCE(adapter_config,'{}'::jsonb)
  FROM channels WHERE id=$1`, channelID).Scan(&baseURL, &provider, &adapterType, &adapterConfig); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Channel not found"})
		} else {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Channel store unavailable"})
		}
		return
	}
	adapterType = adapters.NormalizeType(adapterType)
	if adapterType == adapters.TypeAzureOpenAI {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "Azure OpenAI exposes deployment names rather than a portable provider model catalog",
			"code":  "provider_catalog_unsupported",
		})
		return
	}
	if adapterType != adapters.TypeOpenAICompatible && adapterType != adapters.TypeAnthropicNative {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Provider model catalog is not supported for this adapter", "code": "provider_catalog_unsupported"})
		return
	}

	var keyEnc string
	if err := h.db.QueryRow(r.Context(), `
SELECT key_enc FROM channel_keys
 WHERE channel_id=$1 AND status='active' AND (cooldown_until IS NULL OR cooldown_until<=NOW())
 ORDER BY id LIMIT 1`, channelID).Scan(&keyEnc); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "No active provider credential is available", "code": "no_eligible_key"})
		} else {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Provider credential store unavailable"})
		}
		return
	}
	apiKey, err := auth.DecryptKey(keyEnc, h.encKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to decrypt provider credential"})
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, adapters.UpstreamEndpoint(baseURL, "/v1/models"), nil)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid provider Base URL"})
		return
	}
	if adapterType == adapters.TypeAnthropicNative {
		var cfg struct {
			Version string `json:"anthropic_version"`
		}
		_ = json.Unmarshal(adapterConfig, &cfg)
		if strings.TrimSpace(cfg.Version) == "" {
			cfg.Version = "2023-06-01"
		}
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", cfg.Version)
	} else {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			// Never forward a provider credential across an upstream redirect.
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		h.recordAuditFromRequest(r, "channel.provider_models.list", "channel", strconv.FormatInt(channelID, 10), map[string]interface{}{"after": map[string]interface{}{"success": false, "provider": provider, "error": auditErrorSummary(err.Error())}})
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": "Provider model catalog request failed", "detail": auditErrorSummary(err.Error())})
		return
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, providerModelCatalogMaxBytes+1))
	if readErr != nil || len(body) > providerModelCatalogMaxBytes {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": "Provider model catalog response is unreadable or too large"})
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := providerCatalogError(body)
		h.recordAuditFromRequest(r, "channel.provider_models.list", "channel", strconv.FormatInt(channelID, 10), map[string]interface{}{"after": map[string]interface{}{"success": false, "provider": provider, "status_code": resp.StatusCode, "error": auditErrorSummary(message)}})
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": "Provider model catalog rejected the request", "status_code": resp.StatusCode, "detail": message})
		return
	}
	items, err := decodeProviderModelCatalog(body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": "Provider returned an unrecognized model catalog"})
		return
	}
	h.recordAuditFromRequest(r, "channel.provider_models.list", "channel", strconv.FormatInt(channelID, 10), map[string]interface{}{"after": map[string]interface{}{"success": true, "provider": provider, "model_count": len(items)}})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"authority": "live_provider_api", "provider": provider, "channel_id": channelID,
		"endpoint": req.URL.String(), "fetched_at": time.Now().UTC(), "data": items, "total": len(items),
	})
}

func decodeProviderModelCatalog(body []byte) ([]providerModelCatalogItem, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil, errors.New("missing provider model data")
	}
	var data []struct {
		ID            string `json:"id"`
		OwnedBy       string `json:"owned_by"`
		ContextLength int64  `json:"context_length"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(data))
	items := make([]providerModelCatalogItem, 0, len(data))
	for _, raw := range data {
		id := strings.TrimSpace(raw.ID)
		if id == "" || len(id) > 200 || strings.ContainsAny(id, "\x00\r\n") || strings.IndexFunc(id, unicode.IsSpace) >= 0 {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		ownedBy := strings.TrimSpace(raw.OwnedBy)
		if len(ownedBy) > 200 || strings.ContainsAny(ownedBy, "\x00\r\n") {
			ownedBy = ""
		}
		contextLength := raw.ContextLength
		if contextLength < 0 {
			contextLength = 0
		}
		items = append(items, providerModelCatalogItem{ID: id, OwnedBy: ownedBy, ContextLength: contextLength})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func providerCatalogError(body []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		if message := strings.TrimSpace(envelope.Error.Message); message != "" {
			return auditErrorSummary(message)
		}
		if code := strings.TrimSpace(envelope.Error.Code); code != "" {
			return auditErrorSummary(code)
		}
	}
	return fmt.Sprintf("upstream returned HTTP response without a recognized error message")
}
