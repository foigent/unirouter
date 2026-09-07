package admin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/middleware"
)

const (
	canonicalModelDeploymentCreateScope = "model_deployment.create.v1"
)

var (
	errCanonicalApplicationIdempotencyRequired = errors.New("canonical model application idempotency key is required")
	errCanonicalApplicationIdempotencyConflict = errors.New("canonical model application idempotency key conflicts with another request")
)

type canonicalModelApplicationIdempotency struct {
	CustomerOrgID      int64
	ActorUserID        int64
	ActorOrgID         int64
	KeyHash            string
	RequestFingerprint string
	ServerOperationID  string
}

type canonicalModelApplicationReceipt struct {
	ID                 int64
	RequestFingerprint string
	ServerOperationID  string
	DeploymentID       int64
	ModelOperationID   string
	Status             string
	OriginalHTTPStatus int
	ReusedConnection   bool
	ReusedPublicModel  bool
	ReusedDeployment   bool
	ReusedCredential   bool
	ReusedOperation    bool
	ExpiresAt          time.Time
}

type canonicalCreateCommitError struct{ cause error }

func (e *canonicalCreateCommitError) Error() string {
	return "commit canonical application create: " + e.cause.Error()
}
func (e *canonicalCreateCommitError) Unwrap() error { return e.cause }

func (h *Handler) canonicalCreateIdempotency(
	ctx context.Context,
	orgID int64,
	request canonicalModelApplicationCreateRequest,
) (canonicalModelApplicationIdempotency, canonicalModelApplicationCreateRequest, error) {
	identity := middleware.GetRequestIdentityFromContext(ctx)
	if !identity.IdempotencyKeyPresent || identity.IdempotencyKeyHash == "" {
		return canonicalModelApplicationIdempotency{}, request, errCanonicalApplicationIdempotencyRequired
	}
	actorUserID := middleware.GetUserIDFromContext(ctx)
	actorOrgID := middleware.GetOrgIDFromContext(ctx)
	if orgID <= 0 || actorUserID <= 0 || actorOrgID <= 0 ||
		len(identity.IdempotencyKeyHash) != 64 || strings.TrimSpace(identity.OperationID) == "" ||
		strings.TrimSpace(h.apiKeyHashSecret) == "" {
		return canonicalModelApplicationIdempotency{}, request, fmt.Errorf("canonical model application idempotency identity is unavailable")
	}

	normalized, err := normalizeCanonicalApplicationCreate(request)
	if err != nil {
		return canonicalModelApplicationIdempotency{}, request, err
	}
	fingerprint, err := canonicalModelApplicationFingerprint(normalized, h.apiKeyHashSecret)
	if err != nil {
		return canonicalModelApplicationIdempotency{}, request, err
	}
	return canonicalModelApplicationIdempotency{
		CustomerOrgID: orgID, ActorUserID: actorUserID, ActorOrgID: actorOrgID,
		KeyHash: identity.IdempotencyKeyHash, RequestFingerprint: fingerprint,
		ServerOperationID: identity.OperationID,
	}, normalized, nil
}

func canonicalModelApplicationFingerprint(request canonicalModelApplicationCreateRequest, secret string) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", errors.New("canonical model application fingerprint secret is unavailable")
	}
	// The upstream API key influences the mutation identity, but neither it nor
	// its reusable digest is persisted. Only the outer request HMAC reaches the
	// receipt table.
	if request.Credential.APIKey != "" {
		request.Credential.APIKey = canonicalApplicationHMAC(
			secret, "unirouter.model-deployment-create.provider-secret.v1", []byte(request.Credential.APIKey),
		)
	}
	// Collapse only values that the create contract itself resolves to fixed
	// effective defaults. Optional assertions on an existing connection/model
	// remain distinguishable because omitting an assertion is not equivalent to
	// supplying one.
	dataFirst := request.PublicModel.ID == 0 && request.Deployment.ProfileDefinitionDigest != ""
	if request.PublicModel.ID == 0 {
		if request.PublicModel.ProviderFamily == "" {
			request.PublicModel.ProviderFamily = request.Connection.Provider
		}
		if request.PublicModel.ContextLength == nil && !dataFirst {
			request.PublicModel.ContextLength = canonicalApplicationIntPointer(0)
		}
		if request.PublicModel.OutputLimit == nil && !dataFirst {
			request.PublicModel.OutputLimit = canonicalApplicationIntPointer(0)
		}
		if request.PublicModel.ApprovedCatalog == nil {
			request.PublicModel.ApprovedCatalog = canonicalApplicationBoolPointer(false)
		}
	}
	if request.Deployment.Priority == nil {
		request.Deployment.Priority = canonicalApplicationIntPointer(100)
	}
	if request.Deployment.RoutingWeight == nil {
		request.Deployment.RoutingWeight = canonicalApplicationIntPointer(100)
	}
	if request.Deployment.MaxContext == nil && !dataFirst {
		request.Deployment.MaxContext = canonicalApplicationIntPointer(0)
	}
	if request.Deployment.DefaultMaxOutput == nil && !dataFirst {
		request.Deployment.DefaultMaxOutput = canonicalApplicationIntPointer(0)
	}
	if request.Deployment.DegradationThreshold == nil {
		request.Deployment.DegradationThreshold = canonicalApplicationIntPointer(10)
	}
	wire, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("marshal normalized canonical application fingerprint: %w", err)
	}
	return canonicalApplicationHMAC(
		secret, "unirouter.model-deployment-create.request.v1", wire,
	), nil
}

func canonicalApplicationIntPointer(value int) *int    { return &value }
func canonicalApplicationBoolPointer(value bool) *bool { return &value }

func canonicalApplicationHMAC(secret, domain string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(domain))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(payload)
	return "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil))
}

func claimCanonicalModelApplicationReceipt(
	ctx context.Context,
	tx pgx.Tx,
	claim canonicalModelApplicationIdempotency,
) (bool, canonicalModelApplicationReceipt, error) {
	// An expired receipt is no longer part of the replay contract. Its trigger
	// permits this exact retention deletion and rejects premature deletion.
	if _, err := tx.Exec(ctx, `
DELETE FROM model_deployment_create_receipts
 WHERE customer_org_id=$1 AND actor_user_id=$2 AND operation_scope=$3
   AND idempotency_key_hash=$4 AND expires_at <= clock_timestamp()`,
		claim.CustomerOrgID, claim.ActorUserID, canonicalModelDeploymentCreateScope, claim.KeyHash,
	); err != nil {
		return false, canonicalModelApplicationReceipt{}, fmt.Errorf("expire canonical application receipt: %w", err)
	}

	var receipt canonicalModelApplicationReceipt
	err := tx.QueryRow(ctx, `
INSERT INTO model_deployment_create_receipts (
  customer_org_id,actor_user_id,actor_org_id,operation_scope,
  idempotency_key_hash,request_fingerprint,server_operation_id,
  status,created_at,expires_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,'in_progress',clock_timestamp(),
        clock_timestamp()+$8*INTERVAL '1 second',clock_timestamp())
ON CONFLICT (customer_org_id,actor_user_id,operation_scope,idempotency_key_hash)
DO NOTHING
RETURNING id`,
		claim.CustomerOrgID, claim.ActorUserID, claim.ActorOrgID,
		canonicalModelDeploymentCreateScope, claim.KeyHash, claim.RequestFingerprint,
		claim.ServerOperationID, int64(middleware.DefaultIdempotencyTTL/time.Second),
	).Scan(&receipt.ID)
	if err == nil {
		return true, receipt, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, canonicalModelApplicationReceipt{}, fmt.Errorf("claim canonical application receipt: %w", err)
	}
	receipt, err = readCanonicalModelApplicationReceipt(ctx, tx, claim)
	if err != nil {
		return false, canonicalModelApplicationReceipt{}, fmt.Errorf("read canonical application receipt winner: %w", err)
	}
	return false, receipt, nil
}

func readCanonicalModelApplicationReceipt(
	ctx context.Context,
	store interface {
		QueryRow(context.Context, string, ...interface{}) pgx.Row
	},
	claim canonicalModelApplicationIdempotency,
) (canonicalModelApplicationReceipt, error) {
	row := store.QueryRow(ctx, `
SELECT id,request_fingerprint,server_operation_id,deployment_id,
       model_operation_id,status,original_http_status,
       reused_connection,reused_public_model,reused_deployment,
       reused_credential,reused_operation,expires_at
 FROM model_deployment_create_receipts
 WHERE customer_org_id=$1 AND actor_user_id=$2 AND operation_scope=$3
   AND idempotency_key_hash=$4 AND expires_at > clock_timestamp()`,
		claim.CustomerOrgID, claim.ActorUserID, canonicalModelDeploymentCreateScope, claim.KeyHash,
	)
	var receipt canonicalModelApplicationReceipt
	var deploymentID sql.NullInt64
	var operationID sql.NullString
	var statusCode sql.NullInt16
	var reusedConnection, reusedPublicModel, reusedDeployment, reusedCredential, reusedOperation sql.NullBool
	if err := row.Scan(
		&receipt.ID, &receipt.RequestFingerprint, &receipt.ServerOperationID, &deploymentID,
		&operationID, &receipt.Status, &statusCode,
		&reusedConnection, &reusedPublicModel, &reusedDeployment,
		&reusedCredential, &reusedOperation, &receipt.ExpiresAt,
	); err != nil {
		return canonicalModelApplicationReceipt{}, err
	}
	receipt.DeploymentID = deploymentID.Int64
	receipt.ModelOperationID = operationID.String
	receipt.OriginalHTTPStatus = int(statusCode.Int16)
	receipt.ReusedConnection = reusedConnection.Bool
	receipt.ReusedPublicModel = reusedPublicModel.Bool
	receipt.ReusedDeployment = reusedDeployment.Bool
	receipt.ReusedCredential = reusedCredential.Bool
	receipt.ReusedOperation = reusedOperation.Bool
	return receipt, nil
}

func validateCanonicalModelApplicationReplay(
	receipt canonicalModelApplicationReceipt,
	claim canonicalModelApplicationIdempotency,
) error {
	if receipt.RequestFingerprint != claim.RequestFingerprint {
		return errCanonicalApplicationIdempotencyConflict
	}
	if receipt.Status != "completed" || receipt.DeploymentID <= 0 ||
		receipt.ModelOperationID == "" || receipt.OriginalHTTPStatus != 201 {
		return fmt.Errorf("canonical model application receipt is not replayable")
	}
	return nil
}

func completeCanonicalModelApplicationReceipt(
	ctx context.Context,
	tx pgx.Tx,
	receiptID int64,
	result canonicalModelApplicationCreateResult,
) error {
	tag, err := tx.Exec(ctx, `
UPDATE model_deployment_create_receipts
   SET status='completed',deployment_id=$2,model_operation_id=$3,
       original_http_status=201,
       reused_connection=$4,reused_public_model=$5,reused_deployment=$6,
       reused_credential=$7,reused_operation=$8,
       completed_at=statement_timestamp(),updated_at=statement_timestamp()
 WHERE id=$1 AND status='in_progress'`,
		receiptID, result.Application.Deployment.ID,
		result.Application.Operations[0].OperationID,
		result.Reused.Connection, result.Reused.PublicModel, result.Reused.Deployment,
		result.Reused.Credential, result.Reused.Operation,
	)
	if err != nil {
		return fmt.Errorf("complete canonical application receipt: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("complete canonical application receipt: claim ownership lost")
	}
	return nil
}

func canonicalResultFromReceipt(
	application canonicalModelApplication,
	receipt canonicalModelApplicationReceipt,
) canonicalModelApplicationCreateResult {
	result := canonicalModelApplicationCreateResult{
		Application: application, IdempotencyReplay: true,
		OriginalHTTPStatus:  receipt.OriginalHTTPStatus,
		OriginalOperationID: receipt.ServerOperationID,
	}
	result.Reused.Connection = receipt.ReusedConnection
	result.Reused.PublicModel = receipt.ReusedPublicModel
	result.Reused.Deployment = receipt.ReusedDeployment
	result.Reused.Credential = receipt.ReusedCredential
	result.Reused.Operation = receipt.ReusedOperation
	return result
}

func (h *Handler) readCanonicalModelApplicationReplay(
	ctx context.Context,
	store canonicalApplicationReadStore,
	orgID int64,
	readback canonicalApplicationSnapshotLoader,
	receipt canonicalModelApplicationReceipt,
) (canonicalModelApplicationCreateResult, error) {
	if readback == nil {
		return canonicalModelApplicationCreateResult{}, errors.New("read canonical application replay: snapshot loader is unavailable")
	}
	var generatedAt time.Time
	if err := store.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&generatedAt); err != nil {
		return canonicalModelApplicationCreateResult{}, fmt.Errorf("timestamp canonical application replay: %w", err)
	}
	projection, err := readback(
		ctx, store, orgID, receipt.DeploymentID, receipt.ModelOperationID, generatedAt,
	)
	if err != nil {
		return canonicalModelApplicationCreateResult{}, fmt.Errorf("read canonical application replay: %w", err)
	}
	if len(projection.Applications) != 1 || len(projection.Applications[0].Operations) != 1 {
		return canonicalModelApplicationCreateResult{}, fmt.Errorf("read canonical application replay: %w", errCanonicalApplicationNotFound)
	}
	return canonicalResultFromReceipt(projection.Applications[0], receipt), nil
}

func (h *Handler) recoverCanonicalModelApplicationCommit(
	ctx context.Context,
	claim canonicalModelApplicationIdempotency,
) (canonicalModelApplicationCreateResult, error) {
	receipt, err := readCanonicalModelApplicationReceipt(ctx, h.db, claim)
	if err != nil {
		return canonicalModelApplicationCreateResult{}, fmt.Errorf("recover canonical application commit receipt: %w", err)
	}
	if err := validateCanonicalModelApplicationReplay(receipt, claim); err != nil {
		return canonicalModelApplicationCreateResult{}, err
	}
	projection, err := h.loadCanonicalModelApplications(
		ctx, claim.CustomerOrgID, receipt.DeploymentID, receipt.ModelOperationID,
	)
	if err != nil {
		return canonicalModelApplicationCreateResult{}, fmt.Errorf("recover canonical application commit projection: %w", err)
	}
	if len(projection.Applications) != 1 || len(projection.Applications[0].Operations) != 1 {
		return canonicalModelApplicationCreateResult{}, errCanonicalApplicationNotFound
	}
	result := canonicalResultFromReceipt(projection.Applications[0], receipt)
	// A commit acknowledgement can be lost for either the original mutation or
	// a later read-only replay transaction. The persisted server operation ID
	// distinguishes them without guessing: only recovery of the operation that
	// actually created the receipt remains a first-write response (201 plus one
	// snapshot invalidation). A different operation ID is a genuine replay.
	result.IdempotencyReplay = receipt.ServerOperationID != claim.ServerOperationID
	return result, nil
}

func isCanonicalCreateTransactionRetryable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01")
}
