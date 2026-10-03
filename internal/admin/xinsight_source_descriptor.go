package admin

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

type sourceDescriptorError string

func (e sourceDescriptorError) Error() string { return string(e) }

const (
	descriptorNotFound sourceDescriptorError = "not_found"
	descriptorNotReady sourceDescriptorError = "not_ready"
	descriptorConflict sourceDescriptorError = "identity_conflict"
)

// XInsightSourceDescriptorHandler reuses the original feed bearer gate.
// The resolver is deployment-owned and performs no network I/O or mutation.
func (h *Handler) XInsightSourceDescriptorHandler(resolver XInsightReceiverBindingResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		id := r.PathValue("installation_id")
		var body []byte
		var e error
		if r.Body != nil {
			body, e = io.ReadAll(io.LimitReader(r.Body, 1))
		}
		if !regexp.MustCompile(`^xci_[A-Za-z0-9_-]{22}$`).MatchString(id) || r.URL.RawQuery != "" || r.URL.ForceQuery || e != nil || len(body) != 0 {
			writeJSON(w, 400, map[string]string{"error": "connector_source_descriptor.invalid_request"})
			return
		}
		d, e := h.readXInsightSourceDescriptor(r.Context(), id, resolver)
		if e != nil {
			status, code := 503, "unavailable"
			var typed sourceDescriptorError
			if errors.As(e, &typed) {
				code = string(typed)
				status = 409
				if typed == descriptorNotFound {
					status = 404
				}
			}
			writeJSON(w, status, map[string]string{"error": "connector_source_descriptor." + code})
			return
		}
		canonical, e := d.CanonicalJSON()
		if e != nil {
			writeJSON(w, 503, map[string]string{"error": "connector_source_descriptor.unavailable"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(canonical)
	})
}

type descriptorPhase struct {
	operation xinsightconnector.ControlOperationV2
	result    xinsightconnector.ControlResultV2
	sha       string
}

func readDescriptorPhase(ctx context.Context, tx pgx.Tx, id, installation, project string, org, urProject, fence int64) (descriptorPhase, error) {
	var p descriptorPhase
	var request, result []byte
	var requestSHA string
	e := tx.QueryRow(ctx, `SELECT request_canonical,request_sha256,result_canonical,result_sha256
		FROM xinsight_connector_phase_operations WHERE operation_id=$1 AND installation_id=$2 AND org_id=$3 AND project_id=$4 AND control_fence=$5`, id, installation, org, urProject, fence).Scan(&request, &requestSHA, &result, &p.sha)
	if errors.Is(e, pgx.ErrNoRows) {
		return p, descriptorConflict
	}
	if e != nil {
		return p, e
	}
	p.operation, e = xinsightconnector.DecodeControlOperationV2(request)
	if e != nil {
		return p, xinsightconnector.ErrUnavailable
	}
	p.result, e = xinsightconnector.DecodeControlResultV2(result)
	if e != nil || xinsightconnector.ValidateControlResultForOperationV2(p.operation, p.result) != nil {
		return p, xinsightconnector.ErrUnavailable
	}
	req, e := p.operation.CanonicalJSON()
	if e != nil {
		return p, e
	}
	res, e := p.result.CanonicalJSON()
	if e != nil || !bytes.Equal(req, request) || !bytes.Equal(res, result) || requestSHA != xinsightconnector.PublicCanonicalSHA256(p.operation) || p.sha != xinsightconnector.PublicCanonicalSHA256(p.result) {
		return p, xinsightconnector.ErrUnavailable
	}
	if p.operation.OperationID != id || p.operation.InstallationID != installation || p.operation.ProjectID != project || p.result.Status != "completed" {
		return p, descriptorConflict
	}
	return p, nil
}

func (h *Handler) readXInsightSourceDescriptor(ctx context.Context, id string, resolver XInsightReceiverBindingResolver) (xinsightconnector.SourceDescriptor, error) {
	var d xinsightconnector.SourceDescriptor
	if h == nil || h.db == nil || h.completedFeedController == nil || !h.completedFeedController.Enabled() {
		return d, xinsightconnector.ErrUnavailable
	}
	if resolver == nil {
		return d, descriptorNotReady
	}
	keyID, org, project := middleware.GetKeyIDFromContext(ctx), middleware.GetOrgIDFromContext(ctx), middleware.GetProjectIDFromContext(ctx)
	if keyID <= 0 || org <= 0 || project <= 0 {
		return d, descriptorNotFound
	}
	tx, e := h.db.Begin(ctx)
	if e != nil {
		return d, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY`); e != nil {
		return d, e
	}
	b := xinsightconnector.SourceBinding{SchemaVersion: xinsightconnector.SourceBindingSchemaVersion, InstallationID: id, FeedSchema: xinsightconnector.CompletedFeedSchemaVersion}
	var mode, desired, observed string
	var fence int64
	e = tx.QueryRow(ctx, `SELECT x_deployment_id,org_id,project_id,source_instance,event_scope,source_binding_sha256,
		COALESCE(to_jsonb(i)->>'receiver_mode','new_install'),desired_state,observed_state,control_fence
		FROM xinsight_connector_installations i WHERE installation_id=$1 AND org_id=$2 AND project_id=$3`, id, org, project).Scan(&b.XDeploymentID, &b.OrgID, &b.ProjectID, &b.SourceInstance, &b.EventScope, &d.SourceBindingSHA256, &mode, &desired, &observed, &fence)
	if errors.Is(e, pgx.ErrNoRows) {
		return d, descriptorNotFound
	}
	if e != nil {
		return d, e
	}
	if mode != xinsightconnector.ReceiverModeNewInstall || desired != "active" || observed != "connected" || fence <= 0 {
		return d, descriptorNotReady
	}
	if b.SourceInstance != h.completedFeedController.SourceInstance() || d.SourceBindingSHA256 != xinsightconnector.PublicCanonicalSHA256(b) {
		return d, descriptorConflict
	}
	var controlProject string
	var redeemed bool
	e = tx.QueryRow(ctx, `SELECT COALESCE(control_project_id,''),
		invitation_token_enc IS NULL AND COALESCE(control_token_enc,'')<>'' AND control_token_sha256 ~ '^sha256:[a-f0-9]{64}$'
		AND control_token_expires_at>CURRENT_TIMESTAMP AND redeem_request_sha256 ~ '^sha256:[a-f0-9]{64}$' AND redeem_result_sha256 ~ '^sha256:[a-f0-9]{64}$'
		FROM xinsight_connector_control_bindings WHERE installation_id=$1`, id).Scan(&controlProject, &redeemed)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && (!redeemed || controlProject == "") {
		return d, descriptorNotReady
	}
	if e != nil {
		return d, e
	}
	rows, e := tx.Query(ctx, `SELECT operation_id FROM xinsight_connector_phase_operations
		WHERE installation_id=$1 AND org_id=$2 AND project_id=$3 AND control_fence=$4 AND result_canonical IS NOT NULL
		AND convert_from(request_canonical,'UTF8')::jsonb->>'schema_version'='xinsight.connector-control-operation.v2'
		AND convert_from(request_canonical,'UTF8')::jsonb->>'phase'='resume_source'`, id, org, project, fence)
	if e != nil {
		return d, e
	}
	var ids []string
	for rows.Next() {
		var operation string
		if e = rows.Scan(&operation); e != nil {
			rows.Close()
			return d, e
		}
		ids = append(ids, operation)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return d, e
	}
	if len(ids) == 0 {
		var historical bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM xinsight_connector_phase_operations WHERE installation_id=$1 AND org_id=$2 AND project_id=$3 AND control_fence<>$4 AND result_canonical IS NOT NULL AND convert_from(request_canonical,'UTF8')::jsonb->>'phase'='resume_source')`, id, org, project, fence).Scan(&historical)
		if e != nil {
			return d, e
		}
		if historical {
			return d, descriptorConflict
		}
		return d, descriptorNotReady
	}
	if len(ids) != 1 {
		return d, descriptorConflict
	}
	resume, e := readDescriptorPhase(ctx, tx, ids[0], id, controlProject, org, project, fence)
	if e != nil {
		return d, e
	}
	if resume.operation.Phase != xinsightconnector.ControlPhaseResumeSource {
		return d, descriptorConflict
	}
	register, e := readDescriptorPhase(ctx, tx, resume.operation.PredecessorOperationID, id, controlProject, org, project, fence)
	if e != nil {
		return d, e
	}
	if register.operation.Phase != xinsightconnector.ControlPhaseRegisterReceiver || register.sha != resume.operation.PredecessorResultSHA256 {
		return d, descriptorConflict
	}
	prepare, e := readDescriptorPhase(ctx, tx, register.operation.PredecessorOperationID, id, controlProject, org, project, fence)
	if e != nil {
		return d, e
	}
	if prepare.operation.Phase != xinsightconnector.ControlPhasePreparePaused || prepare.sha != register.operation.PredecessorResultSHA256 {
		return d, descriptorConflict
	}
	if xinsightconnector.ValidateControlOperationPredecessorV2(prepare.operation, nil) != nil ||
		xinsightconnector.ValidateControlOperationPredecessorV2(register.operation, &prepare.result) != nil ||
		xinsightconnector.ValidateControlOperationPredecessorV2(resume.operation, &register.result) != nil {
		return d, descriptorConflict
	}
	rq := register.operation.Request.(xinsightconnector.RegisterReceiverRequestV2)
	sq := resume.operation.Request.(xinsightconnector.ResumeSourceRequestV2)
	rr := register.result.Result.(xinsightconnector.RegisterReceiverResultV2)
	sr := resume.result.Result.(xinsightconnector.ResumeSourceResultV2)
	pr := prepare.result.Result.(xinsightconnector.PreparePausedSourceResultV2)
	if rq.SourceInstance != b.SourceInstance || sq.SourceInstance != b.SourceInstance || pr.SourceInstance != b.SourceInstance || sr.SourceInstance != b.SourceInstance || sq.ReceiverRegistrationSHA256 != xinsightconnector.PublicCanonicalSHA256(rq.ReceiverRegistration) || sq.NetworkBindingSHA256 != rq.NetworkBinding.BindingSHA256 || rr.NetworkBindingSHA256 != rq.NetworkBinding.BindingSHA256 {
		return d, descriptorConflict
	}
	var rootMatches bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM xinsight_connector_operations WHERE installation_id=$1 AND org_id=$2 AND project_id=$3 AND control_fence=$4 AND action='connect' AND state='succeeded' AND result_code='connector_activation_complete' AND result_sha256=$5)`, id, org, project, fence, resume.sha).Scan(&rootMatches)
	if e != nil {
		return d, e
	}
	if !rootMatches {
		return d, descriptorConflict
	}
	var state, hash, prefix, keyStatus, scheme, operation, ip string
	var active, staged int
	e = tx.QueryRow(ctx, `SELECT c.status,
		(SELECT count(*) FROM api_keys WHERE org_id=$1 AND project_id=$2 AND purpose='xinsight.feed.read' AND status='feed_active'),
		(SELECT count(*) FROM api_keys WHERE org_id=$1 AND project_id=$2 AND purpose='xinsight.feed.read' AND status='feed_staged'),
		k.key_hash,k.key_prefix,k.status,k.verifier_scheme,k.registration_operation_id,k.allowed_ips
		FROM completed_execution_feed_connections c JOIN api_keys k ON k.id=$3 AND k.org_id=c.org_id AND k.project_id=c.project_id
		JOIN orgs o ON o.id=c.org_id AND o.status='active' JOIN projects p ON p.id=c.project_id AND p.org_id=c.org_id AND p.status='active'
		WHERE c.org_id=$1 AND c.project_id=$2 AND k.purpose='xinsight.feed.read' AND k.key_type='service' AND COALESCE(k.allowed_models,'')=''`, org, project, keyID).Scan(&state, &active, &staged, &hash, &prefix, &keyStatus, &scheme, &operation, &ip)
	if errors.Is(e, pgx.ErrNoRows) {
		return d, descriptorConflict
	}
	if e != nil {
		return d, e
	}
	if state != "active" || keyStatus != "feed_active" || active != 1 || staged != 0 {
		return d, descriptorNotReady
	}
	trusted, e := resolveXInsightReceiverBindingTx(ctx, tx, resolver, id, b.XDeploymentID, rq.NetworkBinding.BindingSHA256)
	if e != nil {
		return d, descriptorNotReady
	}
	verifier, e := auth.DecodeReceiverFeedVerifier(rq.ReceiverRegistration.VerifierSHA256)
	if e != nil {
		return d, xinsightconnector.ErrUnavailable
	}
	if trusted.BindingSHA256 != rq.NetworkBinding.BindingSHA256 || !exactHostNetworkBinding(trusted.AllowedIP) || ip != trusted.AllowedIP || prefix != rq.ReceiverRegistration.KeyPrefix || operation != rq.ReceiverRegistration.OperationID || scheme != rq.ReceiverRegistration.VerifierScheme || subtle.ConstantTimeCompare([]byte(hash), []byte(auth.HashReceiverFeedVerifier(verifier, h.apiKeyHashSecret))) != 1 {
		return d, descriptorConflict
	}
	d.SchemaVersion, d.SourceBinding = xinsightconnector.SourceDescriptorSchemaV1, b
	d.ReceiverNetwork = xinsightconnector.ReceiverNetwork{FeedKeyID: strconv.FormatInt(keyID, 10), KeyPrefix: prefix, RegistrationOperationID: operation, VerifierScheme: scheme, AllowedIP: ip}
	d.ReceiverNetworkSHA256 = xinsightconnector.PublicCanonicalSHA256(d.ReceiverNetwork)
	d.ResumeOperationID, d.ResumeResultSHA256 = resume.operation.OperationID, resume.sha
	if e = xinsightconnector.ValidateSourceDescriptor(d); e != nil {
		return d, xinsightconnector.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return d, e
	}
	return d, nil
}
