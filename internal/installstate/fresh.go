package installstate

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// FreshShape must run inside the caller's consistent transaction. No DDL,
// ledger creation, business seed or database reservation occurs here.
func FreshShape(ctx context.Context, tx pgx.Tx) (string, error) {
	s, err := Read(ctx, tx)
	if err != nil {
		return "", err
	}
	if len(s.Ledger) != 0 || s.Comment != "" {
		return "", fmt.Errorf("fresh claim refuses existing ledger or COMMENT")
	}
	if s.HasLedger {
		var exact bool
		err = tx.QueryRow(ctx, `SELECT c.relkind='r' AND c.relpersistence='p' AND NOT c.relrowsecurity AND NOT c.relforcerowsecurity
 AND NOT EXISTS(SELECT 1 FROM pg_inherits WHERE inhrelid=c.oid OR inhparent=c.oid)
 AND (SELECT count(*) FROM pg_attribute WHERE attrelid=c.oid AND attnum>0)=2
 AND (SELECT count(*) FROM pg_attribute WHERE attrelid=c.oid AND NOT attisdropped AND attnotnull AND
 ((attnum=1 AND attname='version' AND format_type(atttypid,atttypmod)='character varying(64)') OR
  (attnum=2 AND attname='applied_at' AND format_type(atttypid,atttypmod)='timestamp with time zone')))=2
 AND (SELECT count(*) FROM pg_attrdef WHERE adrelid=c.oid)=1
 AND EXISTS(SELECT 1 FROM pg_attrdef WHERE adrelid=c.oid AND adnum=2 AND pg_get_expr(adbin,adrelid)='now()')
 AND (SELECT count(*) FROM pg_constraint WHERE conrelid=c.oid)=1
 AND EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid=c.oid AND contype='p' AND conkey=ARRAY[1]::smallint[])
 AND (SELECT count(*) FROM pg_index WHERE indrelid=c.oid)=1
 AND NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=c.oid)
 AND NOT EXISTS(SELECT 1 FROM pg_rewrite WHERE ev_class=c.oid)
 AND pg_has_role(current_user,c.relowner,'USAGE')
 FROM pg_class c WHERE c.oid='public.schema_migrations'::regclass`).Scan(&exact)
		if err != nil {
			return "", err
		}
		if !exact {
			return "", fmt.Errorf("existing empty schema_migrations is not the original migration-owner table")
		}
	}
	var objects int64
	err = tx.QueryRow(ctx, `WITH allowed AS (SELECT to_regclass('public.schema_migrations') AS ledger),
 userns AS (SELECT oid FROM pg_namespace WHERE nspname !~ '^pg_' AND nspname<>'information_schema')
 SELECT
 (SELECT count(*) FROM pg_namespace WHERE nspname !~ '^pg_' AND nspname NOT IN ('public','information_schema'))+
 (SELECT count(*) FROM pg_class c WHERE c.relnamespace IN (SELECT oid FROM userns)
   AND c.oid IS DISTINCT FROM (SELECT ledger FROM allowed)
   AND NOT EXISTS(SELECT 1 FROM pg_index i WHERE i.indexrelid=c.oid AND i.indrelid=(SELECT ledger FROM allowed) AND i.indisprimary))+
 (SELECT count(*) FROM pg_proc WHERE pronamespace IN (SELECT oid FROM userns))+
 (SELECT count(*) FROM pg_type t WHERE t.typnamespace IN (SELECT oid FROM userns)
   AND t.typrelid IS DISTINCT FROM (SELECT ledger FROM allowed)
   AND NOT EXISTS(SELECT 1 FROM pg_class c WHERE c.oid=(SELECT ledger FROM allowed) AND t.typelem=c.reltype))+
 (SELECT count(*) FROM pg_operator WHERE oprnamespace IN (SELECT oid FROM userns))+
 (SELECT count(*) FROM pg_opclass WHERE opcnamespace IN (SELECT oid FROM userns))+
 (SELECT count(*) FROM pg_opfamily WHERE opfnamespace IN (SELECT oid FROM userns))+
 (SELECT count(*) FROM pg_collation WHERE collnamespace IN (SELECT oid FROM userns))+
 (SELECT count(*) FROM pg_conversion WHERE connamespace IN (SELECT oid FROM userns))+
 (SELECT count(*) FROM pg_ts_config WHERE cfgnamespace IN (SELECT oid FROM userns))+
 (SELECT count(*) FROM pg_ts_dict WHERE dictnamespace IN (SELECT oid FROM userns))+
 (SELECT count(*) FROM pg_extension WHERE extname<>'plpgsql')+
 (SELECT count(*) FROM pg_foreign_server)+(SELECT count(*) FROM pg_foreign_data_wrapper)+
 (SELECT count(*) FROM pg_event_trigger)+(SELECT count(*) FROM pg_publication)+
 (SELECT count(*) FROM pg_largeobject_metadata)`).Scan(&objects)
	if err != nil {
		return "", err
	}
	if objects != 0 {
		return "", fmt.Errorf("fresh claim refuses nonempty or mixed database objects")
	}
	var canCreate bool
	if err = tx.QueryRow(ctx, `SELECT has_schema_privilege(current_user,'public','CREATE')`).Scan(&canCreate); err != nil {
		return "", err
	}
	if !canCreate {
		return "", fmt.Errorf("fresh install requires the existing schema owner's privileges")
	}
	if s.HasLedger {
		return "empty_original_ledger", nil
	}
	return "empty_database", nil
}
