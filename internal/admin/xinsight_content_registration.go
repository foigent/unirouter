package admin

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

// Only the C transaction coordinator may call this after original create
// authorization, parent/scope locks and nextContentControlState validation.
// It deliberately does not mutate active keys or the legacy connection state.
func (h *Handler) insertContentStagedTx(ctx context.Context, tx pgx.Tx, org, project int64, action string, request contentControlRequest, state contentControlState) (contentControlKey, error) {
	var key contentControlKey
	if request.Registration == nil || sourcefacts.ValidateContentRegistration(*request.Registration) != nil || request.OperationID != request.Registration.OperationID {
		return key, errXInsightFeedInvalidScope
	}
	if h.sourceFactsController == nil || request.Registration.OrgID != strconv.FormatInt(org, 10) || request.Registration.ProjectID != strconv.FormatInt(project, 10) || request.Registration.SourceInstance != h.sourceFactsController.SourceInstance() {
		return key, errXInsightFeedInvalidScope
	}
	if _, err := nextContentControlState(state, action, request); err != nil {
		return key, err
	}
	if action != "prepare" && action != "rotate" {
		return key, errXInsightFeedInvalidScope
	}
	var floor sourcefacts.ContentBoundary
	if action == "prepare" {
		var instance string
		var after int64
		var at time.Time
		if err := tx.QueryRow(ctx, `SELECT source_instance,retained_after_sequence,retained_after_time FROM execution_content_streams WHERE org_id=$1 AND project_id=$2 FOR SHARE`, org, project).Scan(&instance, &after, &at); err != nil {
			return key, err
		}
		if instance != h.sourceFactsController.SourceInstance() {
			return key, errXInsightFeedConflict
		}
		floor = sourcefacts.ContentBoundary{Time: executioncontext.Time(at), AfterSequence: strconv.FormatInt(after, 10)}
	}
	key = contentRegistrationBinding(action, state, floor, request.AllowedIP)
	if !exactHostNetworkBinding(key.ips) || key.CoverageFrom.Validate() != nil {
		return key, errXInsightFeedInvalidScope
	}
	digest, err := sourcefacts.HashContentRegistration(*request.Registration)
	if err != nil {
		return key, err
	}
	key.RegistrationHash = digest
	verifier, err := auth.DecodeReceiverFeedVerifier(request.Registration.VerifierSHA256)
	if err != nil {
		return key, err
	}
	keyHash := auth.HashReceiverFeedVerifier(verifier, h.apiKeyHashSecret)
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,key_type,name,key_hash,key_prefix,status,rpm_limit,qps_limit,budget_limit,allowed_ips,allowed_models,policy_labels,purpose,verifier_scheme,registration_operation_id,source_lifecycle_scope)
	 VALUES($1,$2,'service','X-Insight content',$3,$4,'feed_staged',60,1,0,$5,'','','xinsight.source.read',$6,$7,'content') RETURNING id`, org, project, keyHash, request.Registration.KeyPrefix, key.ips, auth.ReceiverFeedVerifierScheme, request.OperationID).Scan(&id)
	if err != nil {
		return key, err
	}
	key.CredentialID = strconv.FormatInt(id, 10)
	if _, err = tx.Exec(ctx, `INSERT INTO execution_source_credentials(api_key_id,org_id,project_id,registration_sha256,profiles,source_lifecycle_scope) VALUES($1,$2,$3,$4,$5,'content')`, id, org, project, digest, request.Registration.Profiles); err != nil {
		return key, err
	}
	after, _ := contentControlNumber(key.CoverageFrom.AfterSequence, true)
	at, _ := time.Parse(time.RFC3339Nano, key.CoverageFrom.Time)
	_, err = tx.Exec(ctx, `INSERT INTO execution_source_content_bindings(api_key_id,org_id,project_id,coverage_after_sequence,coverage_time,allowed_purposes) VALUES($1,$2,$3,$4,$5,$6)`, id, org, project, after, at, key.AllowedPurposes)
	return key, err
}
