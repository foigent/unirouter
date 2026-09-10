package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

type sourceControlRequest struct {
	AllowedIP    string                    `json:"allowed_ip,omitempty"`
	Registration *sourcefacts.Registration `json:"registration,omitempty"`
	Profiles     []string                  `json:"profiles,omitempty"`
	CredentialID string                    `json:"credential_id,omitempty"`
}

// encoding/json accepts case-insensitive aliases even with DisallowUnknownFields.
// Registration contracts use exact field names, including inside the bundle.
func exactSourceControlFields(wire []byte) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(wire, &object) != nil || object == nil {
		return false
	}
	for key := range object {
		if !slices.Contains([]string{"allowed_ip", "registration", "profiles", "credential_id"}, key) {
			return false
		}
	}
	if value, ok := object["registration"]; ok {
		var registration map[string]json.RawMessage
		if json.Unmarshal(value, &registration) != nil || registration == nil {
			return false
		}
		for key := range registration {
			if !slices.Contains([]string{"schema_version", "token_format", "verifier_scheme", "key_prefix", "verifier_sha256", "operation_id", "source_instance", "org_id", "project_id", "profiles"}, key) {
				return false
			}
		}
	}
	return true
}

func (h *Handler) sourceControlReady(w http.ResponseWriter, requireCapture bool) bool {
	w.Header().Set("Cache-Control", "no-store")
	if h.sourceFactsController == nil || requireCapture && !h.sourceFactsController.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Source facts capture is not enabled"})
		return false
	}
	return true
}

func (h *Handler) GetXInsightSourceConnection(w http.ResponseWriter, r *http.Request) {
	if !h.sourceControlReady(w, false) {
		return
	}
	project, e := xInsightFeedProjectID(r)
	if e != nil {
		writeXInsightFeedControlError(w, e)
		return
	}
	org, e := h.xInsightFeedExistingProjectOrg(r.Context(), project)
	if e != nil {
		writeXInsightFeedControlError(w, e)
		return
	}
	if !h.canWriteAPIKey(r.Context(), org, project) {
		writeJSON(w, 403, map[string]any{"error": "Forbidden"})
		return
	}
	var instance, status string
	var generation int64
	var profiles []string
	availableProfiles := []string{}
	for _, profile := range append(append(sourcefacts.Profiles(), sourcefacts.ObservationProfiles()...), sourcefacts.BudgetProfiles()...) {
		if h.sourceFactsController.ProfilesEnabled([]string{profile}) {
			availableProfiles = append(availableProfiles, profile)
		}
	}
	slices.Sort(availableProfiles)
	e = h.db.QueryRow(r.Context(), `SELECT source_instance,status,scope_generation,profiles FROM execution_source_connections WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&instance, &status, &generation, &profiles)
	if errors.Is(e, pgx.ErrNoRows) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, map[string]any{"status": "not_configured", "source_instance": h.sourceFactsController.SourceInstance(), "capture_enabled": h.sourceFactsController.Enabled(), "available_profiles": availableProfiles})
		return
	}
	if e != nil {
		writeXInsightFeedControlError(w, e)
		return
	}
	rows, e := h.db.Query(r.Context(), `SELECT ak.id,ak.key_prefix,ak.status,ak.registration_operation_id,sc.profiles FROM api_keys ak JOIN execution_source_credentials sc ON sc.api_key_id=ak.id WHERE ak.org_id=$1 AND ak.project_id=$2 AND ak.purpose=$3 AND ak.status IN ('feed_active','feed_staged') ORDER BY ak.id`, org, project, sourcefacts.Purpose)
	if e != nil {
		writeXInsightFeedControlError(w, e)
		return
	}
	defer rows.Close()
	keys := []map[string]any{}
	for rows.Next() {
		var id int64
		var prefix, state, operation string
		var allowed []string
		if e = rows.Scan(&id, &prefix, &state, &operation, &allowed); e != nil {
			break
		}
		keys = append(keys, map[string]any{"id": strconv.FormatInt(id, 10), "key_prefix": prefix, "status": state, "operation_id": operation, "profiles": allowed})
	}
	if e == nil {
		e = rows.Err()
	}
	if e != nil {
		writeXInsightFeedControlError(w, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"schema_version": "unirouter.source-connection.v1", "source_instance": instance, "org_id": strconv.FormatInt(org, 10), "project_id": strconv.FormatInt(project, 10), "status": status, "scope_generation": strconv.FormatInt(generation, 10), "profiles": profiles, "credentials": keys, "bootstrap": "forward_only", "timeline_complete": false, "capture_enabled": h.sourceFactsController.Enabled(), "available_profiles": availableProfiles})
}

// Local product implementation of accepted plan section 3.1. Merely compiling
// or registering these handlers does not create credentials. Runtime invocation
// requires explicit capture enablement and existing live admin reauthorization.
func (h *Handler) SourceControl(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		safety := action == "pause" || action == "cancel" || action == "revoke"
		if !h.sourceControlReady(w, !safety) {
			return
		}
		if !slices.Contains([]string{"prepare", "rotate", "activate", "cancel", "pause", "resume", "revoke", "profiles"}, action) {
			writeJSON(w, 404, map[string]any{"error": "Unknown source control action"})
			return
		}
		var request sourceControlRequest
		wire, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
		if e == nil {
			if strictErr := eventconnector.ValidateStrictJSON(wire, eventconnector.StrictJSONOptions{RejectNull: true}); strictErr != nil {
				e = strictErr
			}
		}
		if e == nil {
			decoder := json.NewDecoder(bytes.NewReader(wire))
			decoder.DisallowUnknownFields()
			e = decoder.Decode(&request)
		}
		if e != nil || !exactSourceControlFields(wire) {
			writeJSON(w, 400, map[string]any{"error": "Invalid source control JSON"})
			return
		}
		register := action == "prepare" || action == "rotate"
		targetCredential := action == "activate" || action == "cancel"
		credentialID, idErr := strconv.ParseInt(request.CredentialID, 10, 64)
		if targetCredential && (idErr != nil || credentialID <= 0 || strconv.FormatInt(credentialID, 10) != request.CredentialID) || !targetCredential && request.CredentialID != "" {
			writeJSON(w, 400, map[string]any{"error": "Invalid target credential"})
			return
		}
		if register {
			if request.Registration == nil || request.Registration.Validate() != nil || request.Profiles != nil || action == "prepare" && !exactHostNetworkBinding(request.AllowedIP) || action == "rotate" && request.AllowedIP != "" {
				writeJSON(w, 400, map[string]any{"error": "Invalid source registration or network binding"})
				return
			}
		} else if request.Registration != nil || request.AllowedIP != "" || action == "profiles" && !sourcefacts.ValidateProfiles(request.Profiles) || action != "profiles" && request.Profiles != nil {
			writeJSON(w, 400, map[string]any{"error": "Unexpected source control fields"})
			return
		}
		if register && !h.sourceFactsController.ProfilesEnabled(request.Registration.Profiles) || action == "profiles" && !h.sourceFactsController.ProfilesEnabled(request.Profiles) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Requested source profile capture is disabled"})
			return
		}
		project, e := xInsightFeedProjectID(r)
		if e != nil {
			writeXInsightFeedControlError(w, e)
			return
		}
		org, e := h.xInsightFeedExistingProjectOrg(r.Context(), project)
		if e != nil {
			writeXInsightFeedControlError(w, e)
			return
		}
		if register && (request.Registration.SourceInstance != h.sourceFactsController.SourceInstance() || request.Registration.OrgID != strconv.FormatInt(org, 10) || request.Registration.ProjectID != strconv.FormatInt(project, 10)) {
			writeJSON(w, 400, map[string]any{"error": "Source registration scope differs"})
			return
		}
		tx, e := h.db.Begin(r.Context())
		if e != nil {
			writeXInsightFeedControlError(w, e)
			return
		}
		defer tx.Rollback(r.Context())
		operation := apiKeyMutationUpdate
		if register {
			operation = apiKeyMutationCreate
		}
		activeRequired := action != "pause" && action != "revoke" && action != "cancel"
		if e = h.reauthorizeXInsightFeedControlTx(r.Context(), tx, org, project, operation, activeRequired); e != nil {
			writeXInsightFeedControlError(w, e)
			return
		}
		if action == "prepare" {
			_, e = tx.Exec(r.Context(), `INSERT INTO execution_source_connections(org_id,project_id,source_instance,profiles) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, org, project, h.sourceFactsController.SourceInstance(), request.Registration.Profiles)
			if e != nil {
				writeXInsightFeedControlError(w, e)
				return
			}
		}
		var status, instance string
		var profiles []string
		var generation int64
		e = tx.QueryRow(r.Context(), `SELECT status,source_instance,profiles,scope_generation FROM execution_source_connections WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, org, project).Scan(&status, &instance, &profiles, &generation)
		if e != nil {
			writeXInsightFeedControlError(w, e)
			return
		}
		if status == "revoked" || !safety && instance != h.sourceFactsController.SourceInstance() {
			writeXInsightFeedControlError(w, errXInsightFeedConflict)
			return
		}
		var active, staged int
		e = tx.QueryRow(r.Context(), `SELECT count(*) FILTER(WHERE status='feed_active'),count(*) FILTER(WHERE status='feed_staged') FROM api_keys WHERE org_id=$1 AND project_id=$2 AND purpose=$3`, org, project, sourcefacts.Purpose).Scan(&active, &staged)
		if e != nil {
			writeXInsightFeedControlError(w, e)
			return
		}
		result := map[string]any{"status": status, "scope_generation": strconv.FormatInt(generation, 10)}
		if targetCredential {
			var currentID int64
			e = tx.QueryRow(r.Context(), `SELECT id FROM api_keys WHERE org_id=$1 AND project_id=$2 AND purpose=$3 AND status='feed_staged'`, org, project, sourcefacts.Purpose).Scan(&currentID)
			if errors.Is(e, pgx.ErrNoRows) || e == nil && currentID != credentialID {
				e = errXInsightFeedConflict
			}
			if e != nil {
				writeXInsightFeedControlError(w, e)
				return
			}
			result["credential_id"] = request.CredentialID
		}
		if register {
			if !slices.Equal(profiles, request.Registration.Profiles) {
				writeXInsightFeedControlError(w, errXInsightFeedConflict)
				return
			}
			digest, _ := request.Registration.Hash()
			var keyID int64
			var previousHash, previousStatus, previousIP string
			e = tx.QueryRow(r.Context(), `SELECT ak.id,sc.registration_sha256,ak.status,ak.allowed_ips FROM api_keys ak JOIN execution_source_credentials sc ON sc.api_key_id=ak.id WHERE ak.org_id=$1 AND ak.project_id=$2 AND ak.purpose=$3 AND ak.registration_operation_id=$4`, org, project, sourcefacts.Purpose, request.Registration.OperationID).Scan(&keyID, &previousHash, &previousStatus, &previousIP)
			if e == nil {
				if previousHash != digest || previousStatus == "feed_revoked" || action == "prepare" && previousIP != request.AllowedIP {
					writeXInsightFeedControlError(w, errXInsightFeedConflict)
					return
				}
				if e = tx.Commit(r.Context()); e != nil {
					writeXInsightFeedControlError(w, e)
					return
				}
				writeJSON(w, 200, map[string]any{"credential_id": strconv.FormatInt(keyID, 10), "credential_status": previousStatus, "replayed": true})
				return
			}
			if !errors.Is(e, pgx.ErrNoRows) {
				writeXInsightFeedControlError(w, e)
				return
			}
			if status != "paused" || staged != 0 || action == "prepare" && active != 0 || action == "rotate" && active != 1 {
				writeXInsightFeedControlError(w, errXInsightFeedConflict)
				return
			}
			allowedIP := request.AllowedIP
			if action == "rotate" {
				e = tx.QueryRow(r.Context(), `SELECT allowed_ips FROM api_keys WHERE org_id=$1 AND project_id=$2 AND purpose=$3 AND status='feed_active' FOR UPDATE`, org, project, sourcefacts.Purpose).Scan(&allowedIP)
				if e != nil {
					writeXInsightFeedControlError(w, e)
					return
				}
			}
			if !exactHostNetworkBinding(allowedIP) {
				writeXInsightFeedControlError(w, errXInsightFeedInvalidScope)
				return
			}
			verifier, _ := auth.DecodeReceiverFeedVerifier(request.Registration.VerifierSHA256)
			keyHash := auth.HashReceiverFeedVerifier(verifier, h.apiKeyHashSecret)
			e = tx.QueryRow(r.Context(), `INSERT INTO api_keys(org_id,project_id,key_type,name,key_hash,key_prefix,status,rpm_limit,qps_limit,budget_limit,allowed_ips,allowed_models,policy_labels,purpose,verifier_scheme,registration_operation_id) VALUES($1,$2,'service','X-Insight source',$3,$4,'feed_staged',60,1,0,$5,'','',$6,$7,$8) RETURNING id`, org, project, keyHash, request.Registration.KeyPrefix, allowedIP, sourcefacts.Purpose, auth.ReceiverFeedVerifierScheme, request.Registration.OperationID).Scan(&keyID)
			if e == nil {
				_, e = tx.Exec(r.Context(), `INSERT INTO execution_source_credentials(api_key_id,org_id,project_id,registration_sha256,profiles) VALUES($1,$2,$3,$4,$5)`, keyID, org, project, digest, profiles)
			}
			result["credential_id"] = strconv.FormatInt(keyID, 10)
			result["credential_status"] = "feed_staged"
		} else {
			switch action {
			case "activate":
				if status != "paused" || staged != 1 {
					e = errXInsightFeedConflict
					break
				}
				_, e = tx.Exec(r.Context(), `UPDATE api_keys SET status='feed_revoked',updated_at=clock_timestamp() WHERE org_id=$1 AND project_id=$2 AND purpose=$3 AND status='feed_active'`, org, project, sourcefacts.Purpose)
				if e == nil {
					_, e = tx.Exec(r.Context(), `UPDATE api_keys SET status='feed_active',updated_at=clock_timestamp() WHERE org_id=$1 AND project_id=$2 AND purpose=$3 AND status='feed_staged'`, org, project, sourcefacts.Purpose)
				}
			case "cancel":
				if status != "paused" || staged != 1 {
					e = errXInsightFeedConflict
					break
				}
				_, e = tx.Exec(r.Context(), `UPDATE api_keys SET status='feed_revoked',updated_at=clock_timestamp() WHERE org_id=$1 AND project_id=$2 AND purpose=$3 AND status='feed_staged'`, org, project, sourcefacts.Purpose)
			case "resume":
				if active != 1 || staged != 0 {
					e = errXInsightFeedConflict
					break
				}
				status = "active"
			case "pause":
				status = "paused"
			case "revoke", "profiles":
				if action == "profiles" && sourcefacts.OnlyBudgetProfilesChanged(profiles, request.Profiles) {
					profiles = request.Profiles
					break // D trigger advances only D; never rewrite existing credential registrations.
				}
				if action == "profiles" && status != "paused" {
					e = errXInsightFeedConflict
					break
				}
				if action == "profiles" && slices.Equal(profiles, request.Profiles) {
					break
				}
				_, e = tx.Exec(r.Context(), `UPDATE api_keys SET status='feed_revoked',updated_at=clock_timestamp() WHERE org_id=$1 AND project_id=$2 AND purpose=$3 AND status IN ('feed_active','feed_staged')`, org, project, sourcefacts.Purpose)
				generation++
				if action == "revoke" {
					status = "revoked"
				} else {
					profiles = request.Profiles
				}
			}
			if e == nil {
				_, e = tx.Exec(r.Context(), `UPDATE execution_source_connections SET status=$3,profiles=$4,scope_generation=$5,updated_at=clock_timestamp() WHERE org_id=$1 AND project_id=$2`, org, project, status, profiles, generation)
			}
			result["status"] = status
			result["scope_generation"] = strconv.FormatInt(generation, 10)
		}
		if e != nil {
			var pgErr *pgconn.PgError
			if errors.As(e, &pgErr) && pgErr.Code == "23505" {
				e = errXInsightFeedConflict
			}
			writeXInsightFeedControlError(w, e)
			return
		}
		if e = insertAuditEntry(r.Context(), tx, "data_exchange.xinsight.source."+action, "xinsight_source_connection", strconv.FormatInt(project, 10), result, requestAuditMeta(r, org, project)); e != nil {
			writeXInsightFeedControlError(w, e)
			return
		}
		if e = tx.Commit(r.Context()); e != nil {
			writeXInsightFeedControlError(w, e)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, result)
	}
}
