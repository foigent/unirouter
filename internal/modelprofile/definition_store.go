package modelprofile

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// DefinitionReader intentionally exposes no write path. The caller may pass
// its existing repeatable-read transaction so Release and Profile reads share
// the same snapshot. Publication is the only definition writer.
type DefinitionReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func ReadDefinition(ctx context.Context, q DefinitionReader, digest string) (Definition, error) {
	if q == nil || !IsDefinitionDigest(digest) {
		return Definition{}, fmt.Errorf("exact definition reader and digest are required")
	}
	var wire []byte
	var id, version, lineage, schema, predecessor string
	if err := q.QueryRow(ctx, `SELECT profile_id,profile_version,product_lineage_id,schema_version,canonical_bytes,COALESCE(predecessor_digest,'')
FROM model_profile_definitions WHERE definition_digest=$1`, digest).Scan(&id, &version, &lineage, &schema, &wire, &predecessor); err != nil {
		return Definition{}, fmt.Errorf("read exact profile definition: %w", err)
	}
	d, err := VerifyDefinition(wire, digest)
	if err != nil {
		return Definition{}, err
	}
	if d.profile.ID != id || d.profile.Version != version || d.lineage != lineage || d.predecessor != predecessor || schema != DefinitionSchemaV1 {
		return Definition{}, fmt.Errorf("profile definition indexed identity differs from canonical bytes")
	}
	return d, nil
}

// ResolvePinned keeps legacy absence explicit. Once a Release names a digest,
// a missing, corrupt or incompatible definition cannot fall back to Lookup.
func ResolvePinned(ctx context.Context, q DefinitionReader, id, version, digest string) (Profile, error) {
	if digest == "" {
		p, ok := Lookup(id, version)
		if !ok {
			return Profile{}, fmt.Errorf("legacy profile identity is unknown")
		}
		return p, nil
	}
	d, err := ReadDefinition(ctx, q, digest)
	if err != nil {
		return Profile{}, err
	}
	if d.profile.ID != id || d.profile.Version != version {
		return Profile{}, fmt.Errorf("Release profile address differs from its exact definition")
	}
	return d.Profile(), nil
}
