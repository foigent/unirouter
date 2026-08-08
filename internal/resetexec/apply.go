package resetexec

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/uniroute/unirouter/internal/resetplan"
)

var ErrPlanDrift = errors.New("RESET_PLAN_DRIFT_DETECTED")

const advisoryLockName = "unirouter.second-integration-reset.v2"

type Result struct {
	ScopeSHA256      string `json:"scope_sha256"`
	DeletedRows      int64  `json:"deleted_rows"`
	RedisKeysDeleted int64  `json:"redis_keys_deleted"`
	RetainedAdminID  int64  `json:"retained_admin_id"`
	RetainedOrgID    int64  `json:"retained_org_id"`
	EmptyBaseline    bool   `json:"empty_baseline"`
}

func Preview(
	ctx context.Context,
	pool *pgxpool.Pool,
	redisClient *redis.Client,
	target resetplan.TargetIdentity,
	currentSourceInstance string,
	nextSourceInstance string,
	now time.Time,
) (resetplan.Plan, error) {
	if pool == nil {
		return resetplan.Plan{}, fmt.Errorf("reset preview requires PostgreSQL")
	}
	redisObservation, err := ObserveRedis(ctx, redisClient)
	if err != nil {
		return resetplan.Plan{}, err
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return resetplan.Plan{}, fmt.Errorf("begin reset preview snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	snapshot, err := Inspect(ctx, tx, redisObservation)
	if err != nil {
		return resetplan.Plan{}, err
	}
	plan, err := resetplan.Build(snapshot, target, currentSourceInstance, nextSourceInstance, now)
	if err != nil {
		return resetplan.Plan{}, err
	}
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return resetplan.Plan{}, fmt.Errorf("close reset preview snapshot: %w", err)
	}
	return plan, nil
}

// Apply executes a reviewed plan only after a read-only no-drift preflight.
// Redis product cache state is removed before PostgreSQL is touched. A later
// PostgreSQL failure can therefore leave caches empty, but cannot leave a
// partially committed relational reset.
func Apply(
	ctx context.Context,
	pool *pgxpool.Pool,
	redisClient *redis.Client,
	plan resetplan.Plan,
	effectiveTarget resetplan.TargetIdentity,
	effectiveSourceInstance string,
) (Result, error) {
	result := Result{ScopeSHA256: plan.ScopeSHA256}
	if pool == nil || redisClient == nil {
		return result, fmt.Errorf("reset apply requires PostgreSQL and Redis")
	}
	if err := resetplan.Validate(plan); err != nil {
		return result, err
	}
	if effectiveTarget != plan.Target {
		return result, fmt.Errorf("%w: target runtime identity changed", ErrPlanDrift)
	}
	if strings.TrimSpace(effectiveSourceInstance) != plan.NextSourceInstance {
		return result, fmt.Errorf("%w: effective Source Instance must equal reviewed next_source_instance", ErrPlanDrift)
	}

	redisObservation, err := ObserveRedis(ctx, redisClient)
	if err != nil {
		return result, err
	}
	if err := compareCurrentScope(ctx, pool, redisObservation, plan); err != nil {
		return result, err
	}

	result.RedisKeysDeleted, err = PurgeRedis(ctx, redisClient)
	if err != nil {
		return result, err
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite})
	if err != nil {
		return result, fmt.Errorf("begin reset transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := prepareLockedTransaction(ctx, tx, plan); err != nil {
		return result, err
	}

	lockedRedis := resetplan.RedisObservation{BoundToScope: false}
	lockedSnapshot, err := Inspect(ctx, tx, lockedRedis)
	if err != nil {
		return result, err
	}
	lockedPlan, err := resetplan.Build(
		lockedSnapshot,
		plan.Target,
		plan.CurrentSourceInstance,
		plan.NextSourceInstance,
		time.Now(),
	)
	if err != nil {
		return result, fmt.Errorf("%w: rebuild locked plan: %v", ErrPlanDrift, err)
	}
	if lockedPlan.ScopeSHA256 != plan.ScopeSHA256 {
		return result, fmt.Errorf("%w: locked scope %s does not match reviewed scope %s", ErrPlanDrift, lockedPlan.ScopeSHA256, plan.ScopeSHA256)
	}

	if err := deferReviewedConstraints(ctx, tx, plan.DeferredFKConstraints); err != nil {
		return result, err
	}
	if err := setReviewedTriggers(ctx, tx, plan.GuardedDeleteTriggers, false); err != nil {
		return result, err
	}
	deletedRows, err := executeDeleteOrder(ctx, tx, plan)
	if err != nil {
		return result, err
	}
	result.DeletedRows = deletedRows
	if err := normalizeRetainedIdentity(ctx, tx, plan.SelectedAdmin); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
		return result, fmt.Errorf("validate deferred reset constraints: %w", err)
	}
	if err := setReviewedTriggers(ctx, tx, plan.GuardedDeleteTriggers, true); err != nil {
		return result, err
	}

	postSnapshot, err := Inspect(ctx, tx, lockedRedis)
	if err != nil {
		return result, fmt.Errorf("inspect post-reset baseline: %w", err)
	}
	if err := verifyPostconditions(plan, postSnapshot); err != nil {
		return result, err
	}
	if err := verifyRedisPostcondition(ctx, redisClient); err != nil {
		return result, err
	}
	if err := tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("commit reset transaction: %w", err)
	}

	result.RetainedAdminID = plan.SelectedAdmin.UserID
	result.RetainedOrgID = plan.SelectedAdmin.OrganizationID
	if err := verifyRedisPostcondition(ctx, redisClient); err != nil {
		return result, fmt.Errorf("PostgreSQL reset committed but final Redis empty-baseline readback failed: %w", err)
	}
	result.EmptyBaseline = true
	return result, nil
}

func verifyRedisPostcondition(ctx context.Context, client *redis.Client) error {
	observation, err := ObserveRedis(ctx, client)
	if err != nil {
		return fmt.Errorf("verify reset Redis empty baseline: %w", err)
	}
	for _, prefix := range observation.Prefixes {
		if prefix.KeyCount != 0 {
			return fmt.Errorf("verify reset Redis empty baseline: prefix %q contains %d key(s)", prefix.Prefix, prefix.KeyCount)
		}
	}
	return nil
}

func normalizeRetainedIdentity(ctx context.Context, tx pgx.Tx, selected resetplan.AdminSelection) error {
	updated, err := tx.Exec(ctx, `
		UPDATE users
		   SET email = NULL,
		       auth_provider = 'local',
		       oidc_issuer = NULL,
		       oidc_subject = NULL,
		       status = 'active',
		       auth_epoch = auth_epoch + 1,
		       updated_at = transaction_timestamp()
		 WHERE id = $1 AND org_id = $2`, selected.UserID, selected.OrganizationID)
	if err != nil {
		return fmt.Errorf("normalize retained administrator: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return fmt.Errorf("%w: retained administrator normalization affected %d rows, expected 1", ErrPlanDrift, updated.RowsAffected())
	}
	updated, err = tx.Exec(ctx, `
		UPDATE orgs
		   SET settings = '{}'::jsonb,
		       status = 'active',
		       parent_id = NULL,
		       updated_at = transaction_timestamp()
		 WHERE id = $1`, selected.OrganizationID)
	if err != nil {
		return fmt.Errorf("normalize retained administrator organization: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return fmt.Errorf("%w: retained administrator organization normalization affected %d rows, expected 1", ErrPlanDrift, updated.RowsAffected())
	}
	return nil
}

func compareCurrentScope(
	ctx context.Context,
	pool *pgxpool.Pool,
	redisObservation resetplan.RedisObservation,
	plan resetplan.Plan,
) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("begin reset no-drift preflight: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	snapshot, err := Inspect(ctx, tx, redisObservation)
	if err != nil {
		return err
	}
	current, err := resetplan.Build(snapshot, plan.Target, plan.CurrentSourceInstance, plan.NextSourceInstance, time.Now())
	if err != nil {
		return fmt.Errorf("%w: rebuild preflight plan: %v", ErrPlanDrift, err)
	}
	if current.ScopeSHA256 != plan.ScopeSHA256 {
		return fmt.Errorf("%w: current scope %s does not match reviewed scope %s", ErrPlanDrift, current.ScopeSHA256, plan.ScopeSHA256)
	}
	return nil
}

func prepareLockedTransaction(ctx context.Context, tx pgx.Tx, plan resetplan.Plan) error {
	for _, statement := range []string{
		"SET LOCAL lock_timeout = '10s'",
		"SET LOCAL statement_timeout = '5min'",
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("configure reset transaction: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", advisoryLockName); err != nil {
		return fmt.Errorf("acquire reset advisory lock: %w", err)
	}
	tables := make([]string, 0, len(plan.Tables))
	for _, table := range plan.Tables {
		tables = append(tables, pgx.Identifier{"public", table.Name}.Sanitize())
	}
	slices.Sort(tables)
	if len(tables) == 0 {
		return fmt.Errorf("reset plan contains no tables")
	}
	if _, err := tx.Exec(ctx, "LOCK TABLE "+strings.Join(tables, ", ")+" IN ACCESS EXCLUSIVE MODE"); err != nil {
		return fmt.Errorf("lock reset scope: %w", err)
	}
	return nil
}

func deferReviewedConstraints(ctx context.Context, tx pgx.Tx, names []string) error {
	if !slices.Equal(names, resetplan.DeferredCycleConstraints()) {
		return fmt.Errorf("%w: deferred constraint set changed", ErrPlanDrift)
	}
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, pgx.Identifier{name}.Sanitize())
	}
	if _, err := tx.Exec(ctx, "SET CONSTRAINTS "+strings.Join(quoted, ", ")+" DEFERRED"); err != nil {
		return fmt.Errorf("defer reviewed reset constraints: %w", err)
	}
	return nil
}

func setReviewedTriggers(ctx context.Context, tx pgx.Tx, refs []resetplan.TriggerRef, enabled bool) error {
	if !reflect.DeepEqual(refs, resetplan.RequiredResetDeleteTriggers()) {
		return fmt.Errorf("%w: trigger set changed", ErrPlanDrift)
	}
	action := "DISABLE"
	if enabled {
		action = "ENABLE"
	}
	for _, ref := range refs {
		statement := fmt.Sprintf(
			"ALTER TABLE %s %s TRIGGER %s",
			pgx.Identifier{"public", ref.Table}.Sanitize(),
			action,
			pgx.Identifier{ref.Name}.Sanitize(),
		)
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("%s reviewed trigger %s.%s: %w", strings.ToLower(action), ref.Table, ref.Name, err)
		}
	}
	return nil
}

func executeDeleteOrder(ctx context.Context, tx pgx.Tx, plan resetplan.Plan) (int64, error) {
	var total int64
	protectedKeys := resetplan.ProtectedSettingKeys()
	for _, step := range plan.DeleteOrder {
		table := pgx.Identifier{"public", step.Table}.Sanitize()
		var statement string
		var args []any
		switch step.Action {
		case resetplan.ActionDeleteAll:
			statement = "DELETE FROM " + table
		case resetplan.ActionDeleteExceptSelectedAdmin:
			statement = "DELETE FROM " + table + " WHERE id <> $1"
			args = []any{plan.SelectedAdmin.UserID}
		case resetplan.ActionDeleteExceptSelectedOrg:
			statement = "DELETE FROM " + table + " WHERE id <> $1"
			args = []any{plan.SelectedAdmin.OrganizationID}
		case resetplan.ActionResetSettingsBaseline:
			retainedKeys := append(protectedKeys, resetplan.PlatformBaselineSettingKeys()...)
			statement = "DELETE FROM " + table + " WHERE NOT (key = ANY($1::text[]))"
			args = []any{retainedKeys}
		default:
			return total, fmt.Errorf("%w: unsupported delete action for %s", ErrPlanDrift, step.Table)
		}
		tag, err := tx.Exec(ctx, statement, args...)
		if err != nil {
			return total, fmt.Errorf("delete reset table %s: %w", step.Table, err)
		}
		if tag.RowsAffected() != step.ExpectedDeletedRows {
			return total, fmt.Errorf(
				"%w: table %s deleted %d rows, expected %d",
				ErrPlanDrift, step.Table, tag.RowsAffected(), step.ExpectedDeletedRows,
			)
		}
		if step.Action == resetplan.ActionResetSettingsBaseline {
			updated, updateErr := tx.Exec(ctx, `
				UPDATE settings
				   SET value = '[]'::jsonb, updated_at = transaction_timestamp()
				 WHERE key = 'sensitive_words'`)
			if updateErr != nil {
				return total, fmt.Errorf("reset required platform setting sensitive_words: %w", updateErr)
			}
			if updated.RowsAffected() != 1 {
				return total, fmt.Errorf("%w: required platform setting sensitive_words updated %d rows, expected 1", ErrPlanDrift, updated.RowsAffected())
			}
		}
		total += tag.RowsAffected()
	}
	return total, nil
}

func verifyPostconditions(plan resetplan.Plan, snapshot resetplan.Snapshot) error {
	post, err := resetplan.Build(
		snapshot,
		plan.Target,
		plan.CurrentSourceInstance,
		plan.NextSourceInstance,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("post-reset contract readback failed: %w", err)
	}
	selected := post.SelectedAdmin
	if selected.UserID != plan.SelectedAdmin.UserID ||
		selected.OrganizationID != plan.SelectedAdmin.OrganizationID ||
		selected.NormalizedLogin != plan.SelectedAdmin.NormalizedLogin ||
		selected.OrganizationRole != plan.SelectedAdmin.OrganizationRole ||
		selected.Role != plan.SelectedAdmin.Role || selected.Status != "active" ||
		selected.AuthProvider != "local" || selected.AuthEpoch != plan.SelectedAdmin.AuthEpoch+1 ||
		selected.PasswordSHA256 != plan.SelectedAdmin.PasswordSHA256 ||
		selected.OrganizationStatus != "active" ||
		selected.OrganizationType != plan.SelectedAdmin.OrganizationType ||
		selected.EmailPresent || selected.OIDCIdentityPresent || !selected.OrganizationEmpty {
		return fmt.Errorf("post-reset selected administrator minimum identity contract failed")
	}
	if !reflect.DeepEqual(post.Protected, plan.Protected) {
		return fmt.Errorf("post-reset protected platform state changed")
	}
	if !reflect.DeepEqual(post.Schema, plan.Schema) {
		return fmt.Errorf("post-reset schema, trigger, FK, or sequence invariant changed")
	}
	if err := verifySequencePostconditions(plan.Sequences, snapshot.Sequences); err != nil {
		return err
	}
	for _, reset := range post.SettingResets {
		if !reset.AlreadyAtTarget {
			return fmt.Errorf("post-reset required platform setting %s is not at its safe baseline", reset.Key)
		}
	}
	counts := make(map[string]int64, len(post.Tables))
	for _, table := range post.Tables {
		counts[table.Name] = table.RowCount
	}
	for _, expected := range plan.Tables {
		want := expected.ExpectedRetainedRows
		if expected.Action == resetplan.ActionPreserveAll {
			want = expected.RowCount
		}
		if counts[expected.Name] != want {
			return fmt.Errorf("post-reset table %s has %d rows, expected %d", expected.Name, counts[expected.Name], want)
		}
	}
	return nil
}

func verifySequencePostconditions(before, after []resetplan.SequenceState) error {
	if len(before) != len(after) {
		return fmt.Errorf("post-reset sequence inventory changed")
	}
	afterByName := make(map[string]resetplan.SequenceState, len(after))
	for _, sequence := range after {
		afterByName[sequence.Name] = sequence
	}
	for _, expected := range before {
		actual, ok := afterByName[expected.Name]
		if !ok || actual.OwnedTable != expected.OwnedTable || actual.OwnedColumn != expected.OwnedColumn ||
			actual.IncrementBy != expected.IncrementBy || actual.Cycle != expected.Cycle ||
			actual.LastValue != expected.LastValue || actual.IsCalled != expected.IsCalled ||
			actual.NextValue != expected.NextValue || actual.MaxObservedValue > expected.MaxObservedValue ||
			actual.NextValue <= actual.MaxObservedValue {
			return fmt.Errorf("post-reset sequence non-reuse contract failed for %s", expected.Name)
		}
	}
	return nil
}
