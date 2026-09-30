package admin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/eventconnector"
)

const (
	policyManagePurpose = "xinsight.policy.manage"
	sourceEnrollPurpose = "xinsight.source.enroll"
)

var machineOperationIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type machineCredentialRegistration struct {
	InstallationID string `json:"installation_id"`
	Purpose        string `json:"purpose"`
	KeyPrefix      string `json:"key_prefix"`
	VerifierSHA256 string `json:"verifier_sha256"`
	OperationID    string `json:"operation_id"`
	AllowedIP      string `json:"allowed_ip"`
}

func exactMachineCredentialFields(wire []byte) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(wire, &fields) != nil || fields == nil {
		return false
	}
	allowed := map[string]bool{"installation_id": true, "purpose": true, "key_prefix": true, "verifier_sha256": true,
		"operation_id": true, "allowed_ip": true}
	for key := range fields {
		if !allowed[key] {
			return false
		}
	}
	return true
}

func decodeMachineCredentialRegistration(w http.ResponseWriter, r *http.Request) (machineCredentialRegistration, bool) {
	var input machineCredentialRegistration
	wire, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<10))
	if err == nil {
		if strictErr := eventconnector.ValidateStrictJSON(wire, eventconnector.StrictJSONOptions{RejectNull: true}); strictErr != nil {
			err = strictErr
		}
	}
	if err == nil && !exactMachineCredentialFields(wire) {
		err = errors.New("unknown field")
	}
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(wire))
		dec.DisallowUnknownFields()
		err = dec.Decode(&input)
		if err == nil {
			err = dec.Decode(new(any))
			if errors.Is(err, io.EOF) {
				err = nil
			} else if err == nil {
				err = errors.New("trailing JSON")
			}
		}
	}
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": "Invalid credential registration JSON"})
		return input, false
	}
	return input, true
}

func machineCredentialPrefixValid(purpose, prefix string) bool {
	var expected string
	switch purpose {
	case policyManagePurpose:
		expected = auth.XInsightPolicyTokenPrefix
	case sourceEnrollPurpose:
		expected = auth.XInsightEnrollmentTokenPrefix
	default:
		return false
	}
	if len(prefix) != 20 || !strings.HasPrefix(prefix, expected) {
		return false
	}
	for _, c := range prefix[len(expected):] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (h *Handler) RegisterXInsightMachineCredential(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	orgID, ok := parsePositivePathID(w, r, "org_id", "Invalid organization ID")
	if !ok {
		return
	}
	input, ok := decodeMachineCredentialRegistration(w, r)
	if !ok {
		return
	}
	verifier, err := auth.DecodeReceiverFeedVerifier(input.VerifierSHA256)
	if err != nil || !machineCredentialPrefixValid(input.Purpose, input.KeyPrefix) ||
		!machineOperationIDPattern.MatchString(input.OperationID) || !exactHostNetworkBinding(input.AllowedIP) || input.InstallationID == "" {
		writeJSON(w, 400, map[string]any{"error": "Invalid credential registration"})
		return
	}
	if h.apiKeyHashSecret == "" {
		writeJSON(w, 503, map[string]any{"error": "Credential verifier unavailable"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Database unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	_, err = h.reauthorizeCommittingActorTx(r.Context(), tx, committingActorRequirement{TargetOrgID: orgID,
		OrganizationForUpdate: true, AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin}})
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "Credential registration denied"})
		return
	}
	var anchor int64
	var deployment, instance, binding, desired, observed string
	err = tx.QueryRow(r.Context(), `SELECT project_id,x_deployment_id,source_instance,source_binding_sha256,desired_state,observed_state
 FROM xinsight_connector_installations WHERE installation_id=$1 AND org_id=$2 FOR UPDATE`, input.InstallationID, orgID).Scan(&anchor, &deployment, &instance, &binding, &desired, &observed)
	if err != nil || desired != "active" || observed == "disconnected" || h.sourceFactsController == nil ||
		instance != h.sourceFactsController.SourceInstance() || binding == "" {
		writeJSON(w, 409, map[string]any{"error": "Installation binding unavailable"})
		return
	}
	hash := auth.HashReceiverFeedVerifier(verifier, h.apiKeyHashSecret)
	canonical, _ := json.Marshal(input)
	digest := sha256.Sum256(canonical)
	registrationSHA := "sha256:" + hex.EncodeToString(digest[:])
	var priorID int64
	var priorSHA, priorInstallation, priorStatus string
	err = tx.QueryRow(r.Context(), `SELECT ak.id,COALESCE(mc.registration_sha256,''),COALESCE(mc.installation_id,''),ak.status
 FROM api_keys ak LEFT JOIN xinsight_machine_credentials mc ON mc.api_key_id=ak.id
 WHERE ak.org_id=$1 AND ak.project_id=$2 AND ak.purpose=$3 AND ak.registration_operation_id=$4 FOR UPDATE OF ak`,
		orgID, anchor, input.Purpose, input.OperationID).Scan(&priorID, &priorSHA, &priorInstallation, &priorStatus)
	if err == nil {
		if priorSHA != registrationSHA || priorInstallation != input.InstallationID {
			writeJSON(w, 409, map[string]any{"error": "registration_operation_changed"})
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeJSON(w, 503, map[string]any{"error": "Credential read failed"})
			return
		}
		writeJSON(w, 200, map[string]any{"key_id": strconv.FormatInt(priorID, 10), "key_prefix": input.KeyPrefix, "status": priorStatus, "purpose": input.Purpose})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 503, map[string]any{"error": "Credential read failed"})
		return
	}
	var keyID int64
	err = tx.QueryRow(r.Context(), `INSERT INTO api_keys(org_id,project_id,key_type,name,key_hash,key_prefix,status,
 rpm_limit,qps_limit,budget_limit,allowed_ips,allowed_models,policy_labels,purpose,verifier_scheme,registration_operation_id)
 VALUES($1,$2,'service','X-Insight machine scope',$3,$4,'feed_staged',60,1,0,$5,'','',$6,$7,$8) RETURNING id`,
		orgID, anchor, hash, input.KeyPrefix, input.AllowedIP, input.Purpose, auth.ReceiverFeedVerifierScheme, input.OperationID).Scan(&keyID)
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": "Credential registration conflict"})
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO xinsight_machine_credentials
 (api_key_id,installation_id,org_id,anchor_project_id,x_deployment_id,source_instance,purpose,registration_sha256)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, keyID, input.InstallationID, orgID, anchor, deployment, instance, input.Purpose, registrationSHA)
	if err == nil {
		err = insertAuditEntry(r.Context(), tx, "data_exchange.xinsight.machine_credential.register", "api_key", strconv.FormatInt(keyID, 10),
			map[string]any{"installation_id": input.InstallationID, "purpose": input.Purpose, "key_prefix": input.KeyPrefix}, requestAuditMeta(r, orgID, anchor))
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Credential registration failed"})
		return
	}
	writeJSON(w, 201, map[string]any{"key_id": strconv.FormatInt(keyID, 10), "key_prefix": input.KeyPrefix, "status": "feed_staged", "purpose": input.Purpose})
}

func (h *Handler) ListXInsightMachineCredentials(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	orgID, ok := parsePositivePathID(w, r, "org_id", "Invalid organization ID")
	if !ok {
		return
	}
	installation := r.URL.Query().Get("installation_id")
	if installation == "" {
		writeJSON(w, 400, map[string]any{"error": "installation_id required"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Database unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	_, err = h.reauthorizeCommittingActorTx(r.Context(), tx, committingActorRequirement{TargetOrgID: orgID,
		AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin}})
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "Credential list denied"})
		return
	}
	rows, err := tx.Query(r.Context(), `SELECT ak.id,ak.key_prefix,ak.status,ak.purpose,ak.registration_operation_id
 FROM xinsight_machine_credentials mc JOIN api_keys ak ON ak.id=mc.api_key_id
 WHERE mc.org_id=$1 AND mc.installation_id=$2 ORDER BY ak.id`, orgID, installation)
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Credential list failed"})
		return
	}
	data := []map[string]any{}
	for rows.Next() {
		var id int64
		var prefix, status, purpose, operation string
		if err = rows.Scan(&id, &prefix, &status, &purpose, &operation); err != nil {
			break
		}
		data = append(data, map[string]any{"key_id": strconv.FormatInt(id, 10), "key_prefix": prefix, "status": status,
			"purpose": purpose, "operation_id": operation})
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Credential list failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"data": data})
}

func (h *Handler) changeXInsightMachineCredential(w http.ResponseWriter, r *http.Request, activate bool) {
	w.Header().Set("Cache-Control", "no-store")
	orgID, ok := parsePositivePathID(w, r, "org_id", "Invalid organization ID")
	if !ok {
		return
	}
	keyID, ok := positiveIDOrZero(r.PathValue("key_id"))
	if !ok || keyID == 0 {
		writeJSON(w, 400, map[string]any{"error": "Invalid key ID"})
		return
	}
	var anchor int64
	var installation, purpose string
	err := h.db.QueryRow(r.Context(), `SELECT installation_id,purpose,anchor_project_id FROM xinsight_machine_credentials WHERE api_key_id=$1 AND org_id=$2`, keyID, orgID).Scan(&installation, &purpose, &anchor)
	if err != nil {
		writeJSON(w, 404, map[string]any{"error": "Credential not found"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Database unavailable"})
		return
	}
	defer tx.Rollback(r.Context())
	_, err = h.reauthorizeCommittingActorTx(r.Context(), tx, committingActorRequirement{TargetOrgID: orgID,
		OrganizationForUpdate: true, AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin}})
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "Credential change denied"})
		return
	}
	var desired, observed string
	err = tx.QueryRow(r.Context(), `SELECT desired_state,observed_state FROM xinsight_connector_installations
 WHERE installation_id=$1 AND org_id=$2 AND project_id=$3 FOR UPDATE`, installation, orgID, anchor).Scan(&desired, &observed)
	if err != nil || activate && (desired != "active" || observed == "disconnected") {
		writeJSON(w, 409, map[string]any{"error": "Installation unavailable"})
		return
	}
	var status string
	rows, err := tx.Query(r.Context(), `SELECT id,status FROM api_keys WHERE org_id=$1 AND project_id=$2 AND purpose=$3
 AND (id=$4 OR status='feed_active') ORDER BY id FOR UPDATE`, orgID, anchor, purpose, keyID)
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Credential lock failed"})
		return
	}
	for rows.Next() {
		var id int64
		var current string
		if err = rows.Scan(&id, &current); err != nil {
			break
		}
		if id == keyID {
			status = current
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil || status == "" || activate && status != "feed_staged" || !activate && status == "feed_revoked" {
		writeJSON(w, 409, map[string]any{"error": "Credential state conflict"})
		return
	}
	if activate {
		// Exact purpose rotation replaces the previous active credential atomically.
		_, err = tx.Exec(r.Context(), `UPDATE api_keys SET status='feed_revoked',updated_at=NOW()
 WHERE org_id=$1 AND project_id=$2 AND purpose=$3 AND status='feed_active'`, orgID, anchor, purpose)
	}
	if err == nil {
		next := "feed_revoked"
		if activate {
			next = "feed_active"
		}
		_, err = tx.Exec(r.Context(), `UPDATE api_keys SET status=$2,updated_at=NOW() WHERE id=$1`, keyID, next)
	}
	if err == nil {
		action := "data_exchange.xinsight.machine_credential.revoke"
		if activate {
			action = "data_exchange.xinsight.machine_credential.activate"
		}
		err = insertAuditEntry(r.Context(), tx, action, "api_key", strconv.FormatInt(keyID, 10),
			map[string]any{"installation_id": installation, "purpose": purpose}, requestAuditMeta(r, orgID, anchor))
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Credential change failed"})
		return
	}
	next := "feed_revoked"
	if activate {
		next = "feed_active"
	}
	writeJSON(w, 200, map[string]any{"key_id": strconv.FormatInt(keyID, 10), "status": next})
}

func (h *Handler) ActivateXInsightMachineCredential(w http.ResponseWriter, r *http.Request) {
	h.changeXInsightMachineCredential(w, r, true)
}
func (h *Handler) RevokeXInsightMachineCredential(w http.ResponseWriter, r *http.Request) {
	h.changeXInsightMachineCredential(w, r, false)
}
