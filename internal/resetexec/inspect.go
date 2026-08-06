package resetexec

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/resetplan"
	"golang.org/x/crypto/bcrypt"
)

type queryer interface {
	Query(context.Context, string, ...interface{}) (pgx.Rows, error)
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

func Inspect(ctx context.Context, q queryer, redis resetplan.RedisObservation) (resetplan.Snapshot, error) {
	tables, err := inspectTables(ctx, q)
	if err != nil {
		return resetplan.Snapshot{}, err
	}
	admin, err := inspectSelectedAdmin(ctx, q)
	if err != nil {
		return resetplan.Snapshot{}, err
	}
	if err := inspectProtectedActorReferences(ctx, q, admin.UserID); err != nil {
		return resetplan.Snapshot{}, err
	}
	protected, err := inspectProtectedState(ctx, q)
	if err != nil {
		return resetplan.Snapshot{}, err
	}
	settingResets, err := inspectRequiredSettingResets(ctx, q)
	if err != nil {
		return resetplan.Snapshot{}, err
	}
	migrations, err := inspectMigrations(ctx, q)
	if err != nil {
		return resetplan.Snapshot{}, err
	}
	sequences, err := inspectSequences(ctx, q)
	if err != nil {
		return resetplan.Snapshot{}, err
	}
	foreignKeys, err := inspectForeignKeys(ctx, q)
	if err != nil {
		return resetplan.Snapshot{}, err
	}
	triggers, err := inspectTriggers(ctx, q)
	if err != nil {
		return resetplan.Snapshot{}, err
	}
	return resetplan.Snapshot{
		Tables: tables, Admin: admin, Protected: protected, SettingResets: settingResets,
		MigrationVersions: migrations, Sequences: sequences,
		ForeignKeys: foreignKeys, Triggers: triggers, Redis: redis,
	}, nil
}

func inspectRequiredSettingResets(ctx context.Context, q queryer) ([]resetplan.SettingReset, error) {
	targetDigest := digestBytes([]byte(`[]`))
	resets := make([]resetplan.SettingReset, 0, len(resetplan.PlatformBaselineSettingKeys()))
	for _, key := range resetplan.PlatformBaselineSettingKeys() {
		var valueJSON, rowJSON string
		if err := q.QueryRow(ctx, `
			SELECT value::text, to_jsonb(s)::text
			  FROM settings s
			 WHERE key = $1`, key).Scan(&valueJSON, &rowJSON); err != nil {
			return nil, fmt.Errorf("inspect required baseline setting %s: %w", key, err)
		}
		resets = append(resets, resetplan.SettingReset{
			Key: key, CurrentRowSHA256: digestBytes([]byte(rowJSON)),
			TargetValueSHA256: targetDigest, AlreadyAtTarget: valueJSON == `[]`,
		})
	}
	return resets, nil
}

func inspectTables(ctx context.Context, q queryer) ([]resetplan.TableInventory, error) {
	rows, err := q.Query(ctx, `
		SELECT tablename
		  FROM pg_catalog.pg_tables
		 WHERE schemaname = 'public'
		 ORDER BY tablename`)
	if err != nil {
		return nil, fmt.Errorf("inventory public tables: %w", err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read public table inventory: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read public table inventory: %w", err)
	}
	rows.Close()

	inventory := make([]resetplan.TableInventory, 0, len(names))
	for _, name := range names {
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s", pgx.Identifier{"public", name}.Sanitize())
		var count int64
		if err := q.QueryRow(ctx, query).Scan(&count); err != nil {
			return nil, fmt.Errorf("count table %s: %w", name, err)
		}
		inventory = append(inventory, resetplan.TableInventory{Name: name, RowCount: count})
	}
	return inventory, nil
}

func inspectSelectedAdmin(ctx context.Context, q queryer) (resetplan.AdminSelection, error) {
	rows, err := q.Query(ctx, `
		SELECT u.id, u.org_id, u.password_hash, u.organization_role, u.role, u.status,
		       COALESCE(NULLIF(u.auth_provider, ''), 'local'), u.auth_epoch,
		       o.status, o.parent_id, o.org_type, o.settings = '{}'::jsonb,
		       u.email IS NOT NULL,
		       u.oidc_issuer IS NOT NULL OR u.oidc_subject IS NOT NULL,
		       to_jsonb(u)::text, to_jsonb(o)::text
		  FROM users u
		  JOIN orgs o ON o.id = u.org_id
		 WHERE COALESCE(NULLIF(u.auth_provider, ''), 'local') = 'local'
		   AND public.normalize_local_login_id(u.username) = $1
		 ORDER BY u.id`, resetplan.PreservedLogin)
	if err != nil {
		return resetplan.AdminSelection{}, fmt.Errorf("select preserved admin: %w", err)
	}
	type candidate struct {
		selection    resetplan.AdminSelection
		passwordHash string
		parentID     sql.NullInt64
		userJSON     string
		orgJSON      string
	}
	var candidates []candidate
	for rows.Next() {
		var item candidate
		item.selection.NormalizedLogin = resetplan.PreservedLogin
		if err := rows.Scan(
			&item.selection.UserID, &item.selection.OrganizationID,
			&item.passwordHash, &item.selection.OrganizationRole, &item.selection.Role, &item.selection.Status,
			&item.selection.AuthProvider, &item.selection.AuthEpoch,
			&item.selection.OrganizationStatus, &item.parentID,
			&item.selection.OrganizationType, &item.selection.OrganizationEmpty,
			&item.selection.EmailPresent, &item.selection.OIDCIdentityPresent,
			&item.userJSON, &item.orgJSON,
		); err != nil {
			rows.Close()
			return resetplan.AdminSelection{}, fmt.Errorf("scan preserved admin: %w", err)
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return resetplan.AdminSelection{}, fmt.Errorf("read preserved admin: %w", err)
	}
	rows.Close()
	if len(candidates) != 1 {
		return resetplan.AdminSelection{}, fmt.Errorf("selected-admin preflight failed: normalized login %q matched %d local accounts", resetplan.PreservedLogin, len(candidates))
	}
	selected := candidates[0]
	if selected.parentID.Valid {
		return resetplan.AdminSelection{}, fmt.Errorf("selected-admin preflight failed: preserved organization must be a root shell")
	}
	if _, err := bcrypt.Cost([]byte(selected.passwordHash)); err != nil {
		return resetplan.AdminSelection{}, fmt.Errorf("selected-admin preflight failed: password verifier is not a supported bcrypt value")
	}
	selected.selection.PasswordVerifierValid = true
	selected.selection.PasswordSHA256 = digestBytes([]byte(selected.passwordHash))
	selected.selection.UserRowSHA256 = digestBytes([]byte(selected.userJSON))
	selected.selection.OrganizationRowSHA256 = digestBytes([]byte(selected.orgJSON))
	return selected.selection, nil
}

func inspectProtectedActorReferences(ctx context.Context, q queryer, selectedUserID int64) error {
	checks := []struct {
		table string
		query string
	}{
		{
			table: "deployment_profiles",
			query: `SELECT COUNT(*) FROM deployment_profiles
			         WHERE created_by IS DISTINCT FROM $1 OR updated_by IS DISTINCT FROM $1`,
		},
		{
			table: "gateway_endpoints",
			query: `SELECT COUNT(*) FROM gateway_endpoints
			         WHERE created_by IS DISTINCT FROM $1 OR updated_by IS DISTINCT FROM $1`,
		},
	}
	for _, check := range checks {
		var count int64
		if err := q.QueryRow(ctx, check.query, selectedUserID).Scan(&count); err != nil {
			return fmt.Errorf("inspect protected actor references in %s: %w", check.table, err)
		}
		if count != 0 {
			return fmt.Errorf("selected-admin preflight failed: protected table %s has %d actor reference(s) to users that would be removed", check.table, count)
		}
	}
	return nil
}

func inspectProtectedState(ctx context.Context, q queryer) (resetplan.ProtectedState, error) {
	protectedNames := []string{
		"commercial_license_entitlements", "deployment_profiles", "gateway_endpoints",
		"micro_ledger_runtime_state", "release_history", "release_state",
	}
	protected := resetplan.ProtectedState{SettingKeys: resetplan.ProtectedSettingKeys()}
	for _, name := range protectedNames {
		digest, err := tableContentDigest(ctx, q, name, "", nil)
		if err != nil {
			return resetplan.ProtectedState{}, err
		}
		protected.Tables = append(protected.Tables, digest)
	}
	settings, err := tableContentDigest(
		ctx, q, "settings", "WHERE key = ANY($1::text[])", []interface{}{protected.SettingKeys},
	)
	if err != nil {
		return resetplan.ProtectedState{}, err
	}
	settings.Name = "settings:protected_keys"
	protected.Settings = settings

	rows, err := q.Query(ctx, `SELECT key FROM settings WHERE key = ANY($1::text[]) ORDER BY key`, protected.SettingKeys)
	if err != nil {
		return resetplan.ProtectedState{}, fmt.Errorf("list protected settings: %w", err)
	}
	var present []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return resetplan.ProtectedState{}, fmt.Errorf("scan protected setting: %w", err)
		}
		present = append(present, key)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return resetplan.ProtectedState{}, fmt.Errorf("read protected settings: %w", err)
	}
	rows.Close()
	if !slices.Contains(present, "ledger.amount_scale") || !slices.Contains(present, "ledger.version") {
		return resetplan.ProtectedState{}, fmt.Errorf("protected settings preflight failed: ledger unit contract is incomplete")
	}
	return protected, nil
}

func tableContentDigest(ctx context.Context, q queryer, table, where string, args []interface{}) (resetplan.ContentDigest, error) {
	identifier := pgx.Identifier{"public", table}.Sanitize()
	query := fmt.Sprintf(`
		SELECT COUNT(*)::bigint,
		       COALESCE(jsonb_agg(row_value ORDER BY row_value::text), '[]'::jsonb)::text
		  FROM (SELECT to_jsonb(t) AS row_value FROM %s AS t %s) AS rows`, identifier, where)
	var count int64
	var canonicalRows string
	if err := q.QueryRow(ctx, query, args...).Scan(&count, &canonicalRows); err != nil {
		return resetplan.ContentDigest{}, fmt.Errorf("digest protected table %s: %w", table, err)
	}
	return resetplan.ContentDigest{Name: table, RowCount: count, SHA256: digestBytes([]byte(canonicalRows))}, nil
}

func inspectMigrations(ctx context.Context, q queryer) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("inspect schema migrations: %w", err)
	}
	var versions []string
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan schema migration: %w", err)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read schema migrations: %w", err)
	}
	rows.Close()
	return versions, nil
}

func inspectSequences(ctx context.Context, q queryer) ([]resetplan.SequenceState, error) {
	rows, err := q.Query(ctx, `
		SELECT DISTINCT sequence.relname, owned.relname, attribute.attname,
		       metadata.seqincrement, metadata.seqcycle
		  FROM pg_catalog.pg_class sequence
		  JOIN pg_catalog.pg_namespace namespace ON namespace.oid = sequence.relnamespace
		  JOIN pg_catalog.pg_sequence metadata ON metadata.seqrelid = sequence.oid
		  LEFT JOIN pg_catalog.pg_depend dependency
		    ON dependency.classid = 'pg_class'::regclass
		   AND dependency.objid = sequence.oid
		   AND dependency.deptype IN ('a', 'i')
		  LEFT JOIN pg_catalog.pg_class owned ON owned.oid = dependency.refobjid
		  LEFT JOIN pg_catalog.pg_attribute attribute
		    ON attribute.attrelid = dependency.refobjid
		   AND attribute.attnum = dependency.refobjsubid
		 WHERE namespace.nspname = 'public' AND sequence.relkind = 'S'
		 ORDER BY sequence.relname`)
	if err != nil {
		return nil, fmt.Errorf("inspect sequences: %w", err)
	}
	var sequences []resetplan.SequenceState
	for rows.Next() {
		var state resetplan.SequenceState
		if err := rows.Scan(
			&state.Name, &state.OwnedTable, &state.OwnedColumn,
			&state.IncrementBy, &state.Cycle,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan sequence ownership: %w", err)
		}
		sequences = append(sequences, state)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read sequence names: %w", err)
	}
	rows.Close()

	for index := range sequences {
		state := &sequences[index]
		query := fmt.Sprintf("SELECT last_value, is_called FROM %s", pgx.Identifier{"public", state.Name}.Sanitize())
		if err := q.QueryRow(ctx, query).Scan(&state.LastValue, &state.IsCalled); err != nil {
			return nil, fmt.Errorf("inspect sequence %s: %w", state.Name, err)
		}
		state.NextValue = state.LastValue
		if state.IsCalled {
			if state.IncrementBy <= 0 || state.LastValue > int64(^uint64(0)>>1)-state.IncrementBy {
				return nil, fmt.Errorf("inspect sequence %s: unsupported increment or exhausted range", state.Name)
			}
			state.NextValue += state.IncrementBy
		}
		maxQuery := fmt.Sprintf(
			"SELECT COALESCE(MAX(%s), 0)::bigint FROM %s",
			pgx.Identifier{state.OwnedColumn}.Sanitize(),
			pgx.Identifier{"public", state.OwnedTable}.Sanitize(),
		)
		if err := q.QueryRow(ctx, maxQuery).Scan(&state.MaxObservedValue); err != nil {
			return nil, fmt.Errorf("inspect sequence owner %s.%s: %w", state.OwnedTable, state.OwnedColumn, err)
		}
	}
	return sequences, nil
}

func inspectForeignKeys(ctx context.Context, q queryer) ([]resetplan.ForeignKey, error) {
	rows, err := q.Query(ctx, `
		SELECT con.conname, child.relname, parent.relname,
		       pg_get_constraintdef(con.oid),
		       CASE con.confdeltype
		         WHEN 'a' THEN 'NO ACTION'
		         WHEN 'r' THEN 'RESTRICT'
		         WHEN 'c' THEN 'CASCADE'
		         WHEN 'n' THEN 'SET NULL'
		         WHEN 'd' THEN 'SET DEFAULT'
		         ELSE 'UNKNOWN'
		       END,
		       con.condeferrable, con.condeferred
		  FROM pg_catalog.pg_constraint con
		  JOIN pg_catalog.pg_class child ON child.oid = con.conrelid
		  JOIN pg_catalog.pg_class parent ON parent.oid = con.confrelid
		 WHERE con.contype = 'f'
		   AND con.connamespace = 'public'::regnamespace
		 ORDER BY child.relname, parent.relname, con.conname`)
	if err != nil {
		return nil, fmt.Errorf("inspect foreign keys: %w", err)
	}
	var foreignKeys []resetplan.ForeignKey
	for rows.Next() {
		var fk resetplan.ForeignKey
		if err := rows.Scan(
			&fk.Name, &fk.ChildTable, &fk.ParentTable, &fk.Definition,
			&fk.DeleteAction, &fk.Deferrable, &fk.InitiallyDeferred,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan foreign key: %w", err)
		}
		foreignKeys = append(foreignKeys, fk)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read foreign keys: %w", err)
	}
	rows.Close()
	return foreignKeys, nil
}

func inspectTriggers(ctx context.Context, q queryer) ([]resetplan.TriggerState, error) {
	rows, err := q.Query(ctx, `
		SELECT c.relname, t.tgname, pg_get_triggerdef(t.oid), t.tgenabled::text
		  FROM pg_catalog.pg_trigger t
		  JOIN pg_catalog.pg_class c ON c.oid = t.tgrelid
		  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND NOT t.tgisinternal
		 ORDER BY c.relname, t.tgname`)
	if err != nil {
		return nil, fmt.Errorf("inspect triggers: %w", err)
	}
	var triggers []resetplan.TriggerState
	for rows.Next() {
		var trigger resetplan.TriggerState
		if err := rows.Scan(&trigger.Table, &trigger.Name, &trigger.Definition, &trigger.Enabled); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan trigger: %w", err)
		}
		triggers = append(triggers, trigger)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read triggers: %w", err)
	}
	rows.Close()
	return triggers, nil
}

func digestBytes(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func equalContentDigests(left, right []resetplan.ContentDigest) bool {
	left = slices.Clone(left)
	right = slices.Clone(right)
	slices.SortFunc(left, func(a, b resetplan.ContentDigest) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(right, func(a, b resetplan.ContentDigest) int { return strings.Compare(a.Name, b.Name) })
	return slices.Equal(left, right)
}
