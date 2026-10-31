package upstreamrelease

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
)

const NamespaceMigration = "000129_upstream_observation_restore_namespace"
const LegacyValidatorBodySHA256 = "f2ffc71b42a5c0c9e92c0e06f495345ac8d76550284eb82b208727ccc8a70356"
const LegacyTextHelperSHA256 = "a46a33ebafc84191586d578546880f7bdaabcd46820c65ae0254c3530c7c4d95"
const NamespaceConfig = "search_path=pg_catalog, public"

type NamespaceShape struct {
	BodySHA256 string   `json:"body_sha256"`
	Config     []string `json:"config"`
}

// ReadNamespace rejects unknown definitions before either normal admission or
// the bounded compatibility correction. The body/predicate is never rewritten.
func ReadNamespace(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (NamespaceShape, error) {
	var result NamespaceShape
	var valid bool
	err := q.QueryRow(ctx, `SELECT encode(sha256(convert_to(p.prosrc,'UTF8')),'hex'),p.proconfig,
p.provolatile='i' AND NOT p.prosecdef AND NOT p.proisstrict AND NOT p.proleakproof
AND p.proparallel='u' AND p.prorettype='boolean'::regtype AND l.lanname='plpgsql'
AND p.prokind='f' AND NOT p.proretset AND p.provariadic=0 AND p.pronargdefaults=0 AND p.proargnames=ARRAY['v']::text[]
AND EXISTS(SELECT 1 FROM pg_proc h JOIN pg_language hl ON hl.oid=h.prolang
 WHERE h.oid='public.valid_upstream_release_text(text,integer,boolean)'::regprocedure
 AND encode(sha256(convert_to(h.prosrc,'UTF8')),'hex')=$1
 AND h.provolatile='i' AND NOT h.prosecdef AND NOT h.proisstrict AND NOT h.proleakproof
 AND h.proparallel='u' AND h.prorettype='boolean'::regtype AND hl.lanname='sql' AND h.proconfig IS NULL
 AND h.prokind='f' AND NOT h.proretset AND h.provariadic=0 AND h.pronargdefaults=0 AND h.proargnames=ARRAY['v','n','required']::text[])
FROM pg_proc p JOIN pg_language l ON l.oid=p.prolang
WHERE p.oid='public.valid_upstream_release_observation(jsonb)'::regprocedure`, LegacyTextHelperSHA256).Scan(&result.BodySHA256, &result.Config, &valid)
	if err != nil {
		return result, err
	}
	if !valid || result.BodySHA256 != LegacyValidatorBodySHA256 ||
		!(result.Config == nil || (len(result.Config) == 1 && result.Config[0] == NamespaceConfig)) {
		return result, fmt.Errorf("unknown upstream observation validator definition or configuration")
	}
	return result, nil
}

func CheckNamespace(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) error {
	shape, err := ReadNamespace(ctx, q)
	if err != nil {
		return err
	}
	if len(shape.Config) != 1 || shape.Config[0] != NamespaceConfig {
		return fmt.Errorf("upstream observation validator namespace is not corrected")
	}
	return nil
}

// NamespaceCorrection returns the same canonical fragment executed by M129.
// The strict command check is an input validator, not another implementation.
func NamespaceCorrection(dir string) (string, error) {
	wire, err := os.ReadFile(filepath.Join(dir, NamespaceMigration+".up.sql"))
	if err != nil {
		return "", err
	}
	begin, end := "-- unirouter:observation-namespace-correction-begin\n", "-- unirouter:observation-namespace-correction-end"
	parts := strings.Split(string(wire), begin)
	if len(parts) != 2 {
		return "", fmt.Errorf("missing exact namespace correction")
	}
	fragments := strings.Split(parts[1], end)
	if len(fragments) != 2 {
		return "", fmt.Errorf("ambiguous namespace correction")
	}
	sql := strings.TrimSpace(fragments[0])
	if sql != "ALTER FUNCTION public.valid_upstream_release_observation(jsonb) SET search_path TO pg_catalog, public;" {
		return "", fmt.Errorf("unknown namespace correction command")
	}
	return sql, nil
}
