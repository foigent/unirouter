package terminalhook

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/outbox"
)

const (
	HookAgentAncestor = "agent_ancestor"
	HookLimitPolicy   = "limit_policy"

	TerminalSettled   = "settled"
	TerminalCancelled = "cancelled"
)

type terminalEventPayload struct {
	OperationID string `json:"operation_id"`
	OrgID       int64  `json:"org_id"`
	HookType    string `json:"hook_type"`
	Terminal    string `json:"terminal"`
}

type Executor func(context.Context, json.RawMessage, string, int64, int64) error

type Service struct {
	db     db.DBQuerier
	outbox *outbox.Service

	mu        sync.RWMutex
	executors map[string]Executor
}

func New(database db.DBQuerier, outboxService *outbox.Service) (*Service, error) {
	if database == nil || outboxService == nil {
		return nil, fmt.Errorf("terminal hook database and outbox are required")
	}
	return &Service{db: database, outbox: outboxService, executors: make(map[string]Executor)}, nil
}

func (s *Service) RegisterExecutor(hookType string, executor Executor) error {
	if s == nil || executor == nil || !validHookType(hookType) {
		return fmt.Errorf("valid terminal hook type and executor are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.executors[hookType]; exists {
		return fmt.Errorf("terminal hook executor already registered for %s", hookType)
	}
	s.executors[hookType] = executor
	return nil
}

// Register persists a terminal-only server reservation. If financial recovery
// already reached a terminal state, registration reconciles it in the same
// transaction so startup/worker ordering cannot lose the hook.
func (s *Service) Register(ctx context.Context, hookType, operationID string, orgID int64, reservation any, expiresAt time.Time) error {
	if s == nil || s.db == nil || s.outbox == nil {
		return fmt.Errorf("terminal hook service is unavailable")
	}
	if !validHookType(hookType) || strings.TrimSpace(operationID) == "" || len(operationID) > 64 || orgID <= 0 {
		return fmt.Errorf("terminal hook identity is invalid")
	}
	if expiresAt.IsZero() {
		return fmt.Errorf("terminal hook expiry is required")
	}
	reservationJSON, err := json.Marshal(reservation)
	if err != nil || !json.Valid(reservationJSON) || len(reservationJSON) > 65536 || len(reservationJSON) == 0 || reservationJSON[0] != '{' {
		return fmt.Errorf("terminal hook reservation is invalid")
	}
	digest := sha256.Sum256(reservationJSON)
	hash := hex.EncodeToString(digest[:])

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin terminal hook registration: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`INSERT INTO runtime_terminal_hooks (
			operation_id, org_id, hook_type, reservation, reservation_hash,
			expires_at, created_at, updated_at
		 ) VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		 ON CONFLICT (operation_id, hook_type) DO NOTHING`,
		operationID, orgID, hookType, string(reservationJSON), hash, expiresAt.UTC(),
	); err != nil {
		return fmt.Errorf("insert terminal hook: %w", err)
	}
	var existingOrgID int64
	var existingHash string
	if err := tx.QueryRow(ctx,
		`SELECT org_id, reservation_hash FROM runtime_terminal_hooks
		 WHERE operation_id = $1 AND hook_type = $2 FOR UPDATE`, operationID, hookType,
	).Scan(&existingOrgID, &existingHash); err != nil {
		return fmt.Errorf("read terminal hook: %w", err)
	}
	if existingOrgID != orgID || existingHash != hash {
		return fmt.Errorf("terminal hook reservation conflict")
	}
	if err := reconcileFinancialStateTx(ctx, tx, s.outbox, operationID, orgID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Reconcile reads the authoritative financial terminal state and schedules
// every matching runtime hook. It is safe to call after either Settle or
// Cancel, including an ambiguous recovery boundary.
func (s *Service) Reconcile(ctx context.Context, operationID string, orgID int64) error {
	if s == nil || s.db == nil || s.outbox == nil {
		return fmt.Errorf("terminal hook service is unavailable")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := reconcileFinancialStateTx(ctx, tx, s.outbox, operationID, orgID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func reconcileFinancialStateTx(ctx context.Context, tx pgx.Tx, outboxService *outbox.Service, operationID string, orgID int64) error {
	var status string
	var promptTokens, completionTokens, calculatedCost int64
	err := tx.QueryRow(ctx,
		`SELECT status, prompt_tokens, completion_tokens, calculated_cost
		 FROM freeze_idempotency
		 WHERE request_id = $1 AND org_id = $2`, operationID, orgID,
	).Scan(&status, &promptTokens, &completionTokens, &calculatedCost)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("terminal hook financial reservation is missing")
	}
	if err != nil {
		return err
	}
	switch status {
	case "settled":
		return MarkFromFinancialTx(ctx, tx, outboxService, operationID, orgID, TerminalSettled, promptTokens+completionTokens, calculatedCost)
	case "cancelled":
		return MarkFromFinancialTx(ctx, tx, outboxService, operationID, orgID, TerminalCancelled, 0, 0)
	case "frozen", "settlement_pending":
		return nil
	default:
		return fmt.Errorf("unknown financial reservation state %s", status)
	}
}

// MarkFromFinancialTx is called from the minimal financial journal worker so
// a recovered settlement cannot bypass Agent/policy terminal cleanup.
func MarkFromFinancialTx(ctx context.Context, tx pgx.Tx, outboxService *outbox.Service, operationID string, orgID int64, terminal string, actualTokens, actualCostCents int64) error {
	if tx == nil || outboxService == nil || strings.TrimSpace(operationID) == "" || orgID <= 0 || !validTerminal(terminal) || actualTokens < 0 || actualCostCents < 0 {
		return fmt.Errorf("runtime terminal mark is invalid")
	}
	rows, err := tx.Query(ctx,
		`UPDATE runtime_terminal_hooks
		 SET terminal_kind = $3,
		     actual_tokens = $4,
		     actual_cost_cents = $5,
		     updated_at = NOW()
		 WHERE operation_id = $1 AND org_id = $2
		   AND completed_at IS NULL
		   AND terminal_kind IN ('', $3)
		 RETURNING hook_type`,
		operationID, orgID, terminal, actualTokens, actualCostCents,
	)
	if err != nil {
		return fmt.Errorf("mark runtime terminal hooks: %w", err)
	}
	hookTypes := make([]string, 0, 2)
	for rows.Next() {
		var hookType string
		if err := rows.Scan(&hookType); err != nil {
			rows.Close()
			return err
		}
		hookTypes = append(hookTypes, hookType)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, hookType := range hookTypes {
		eventKey := fmt.Sprintf("runtime-terminal:%s:%s:%d:%s", terminal, hookType, orgID, operationID)
		payload := terminalEventPayload{
			OperationID: operationID,
			OrgID:       orgID,
			HookType:    hookType,
			Terminal:    terminal,
		}
		event, err := outbox.NewEvent(
			outbox.EventRuntimeTerminal,
			eventKey,
			outbox.PriorityBilling,
			orgID,
			operationID,
			payload,
			time.Now().UTC(),
		)
		if err != nil {
			return err
		}
		if _, _, err := outbox.EnqueueTx(ctx, tx, event); err != nil {
			return fmt.Errorf("enqueue runtime terminal hook: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE runtime_terminal_hooks
			 SET terminal_event_key = $3, updated_at = NOW()
			 WHERE operation_id = $1 AND hook_type = $2`, operationID, hookType, eventKey,
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ProcessEvent(ctx context.Context, event outbox.Event) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("terminal hook service is unavailable")
	}
	var payload terminalEventPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode terminal hook event: %w", err)
	}
	if !validHookType(payload.HookType) || !validTerminal(payload.Terminal) || payload.OperationID == "" || payload.OrgID <= 0 {
		return fmt.Errorf("terminal hook event identity is invalid")
	}

	var reservation json.RawMessage
	var terminal string
	var actualTokens, actualCost int64
	var expiresAt time.Time
	var completedAt sql.NullTime
	err := s.db.QueryRow(ctx,
		`SELECT reservation, terminal_kind, actual_tokens, actual_cost_cents,
		        expires_at, completed_at
		 FROM runtime_terminal_hooks
		 WHERE operation_id = $1 AND org_id = $2 AND hook_type = $3`,
		payload.OperationID, payload.OrgID, payload.HookType,
	).Scan(&reservation, &terminal, &actualTokens, &actualCost, &expiresAt, &completedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if completedAt.Valid {
		return nil
	}
	if terminal != payload.Terminal {
		return fmt.Errorf("terminal hook event conflicts with durable state")
	}
	if !time.Now().UTC().Before(expiresAt.UTC()) {
		_, err := s.db.Exec(ctx,
			`UPDATE runtime_terminal_hooks
			 SET completed_at = NOW(), terminal_outcome = 'expired_conservative_ttl',
			     last_error = '', updated_at = NOW()
			 WHERE operation_id = $1 AND hook_type = $2 AND completed_at IS NULL`,
			payload.OperationID, payload.HookType,
		)
		return err
	}

	s.mu.RLock()
	executor := s.executors[payload.HookType]
	s.mu.RUnlock()
	if executor == nil {
		return fmt.Errorf("no terminal hook executor registered for %s", payload.HookType)
	}
	if err := executor(ctx, reservation, terminal, actualTokens, actualCost); err != nil {
		message := strings.TrimSpace(err.Error())
		if len(message) > 512 {
			message = message[:512]
		}
		_, _ = s.db.Exec(ctx,
			`UPDATE runtime_terminal_hooks SET last_error = $3, updated_at = NOW()
			 WHERE operation_id = $1 AND hook_type = $2 AND completed_at IS NULL`,
			payload.OperationID, payload.HookType, message,
		)
		return err
	}
	outcome := "finalized"
	if terminal == TerminalCancelled {
		outcome = "cancelled"
	}
	_, err = s.db.Exec(ctx,
		`UPDATE runtime_terminal_hooks
		 SET completed_at = NOW(), terminal_outcome = $3, last_error = '', updated_at = NOW()
		 WHERE operation_id = $1 AND hook_type = $2 AND completed_at IS NULL`,
		payload.OperationID, payload.HookType, outcome,
	)
	return err
}

func validHookType(value string) bool {
	return value == HookAgentAncestor || value == HookLimitPolicy
}

func validTerminal(value string) bool {
	return value == TerminalSettled || value == TerminalCancelled
}
