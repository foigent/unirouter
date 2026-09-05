package modelcapabilitypublication

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/uniroute/unirouter/internal/strictjson"
)

const ProjectionSchemaV2 = "unirouter.capability-projection.v2"

// StaticProjectionV2 contains only publisher-owned definitions, immutable
// binding origins and current candidate selectors. Timestamps, leases, probes,
// approvals, deployment state, usage and prices deliberately have no place.
// Each row retains canonical source content, not a cached projection summary.
type StaticProjectionV2 struct {
	SchemaVersion string            `json:"schema_version"`
	Definitions   []json.RawMessage `json:"definitions"`
	Bindings      []json.RawMessage `json:"bindings"`
	Selectors     []json.RawMessage `json:"selectors"`
}

func ReadStaticProjectionV2(ctx context.Context, q Querier) (StaticProjectionV2, error) {
	p := StaticProjectionV2{SchemaVersion: ProjectionSchemaV2}
	queries := []struct {
		sql  string
		into *[]json.RawMessage
	}{
		{`SELECT jsonb_build_object('definition_digest',definition_digest,
            'profile_id',profile_id,'profile_version',profile_version,
            'product_lineage_id',product_lineage_id,'predecessor_digest',predecessor_digest,
            'schema_version',schema_version,'canonical_bytes',encode(canonical_bytes,'hex'),
            'publication_id',publication_id)::text FROM model_profile_definitions`, &p.Definitions},
		{`SELECT jsonb_build_object('operation_id',operation_id,'capability_id',capability_id,
            'execution_profile_id',execution_profile_id,'execution_profile_version',execution_profile_version,
            'protocol_profile_id',protocol_profile_id,'protocol_profile_version',protocol_profile_version,
            'probe_contract_id',probe_contract_id,'probe_contract_version',probe_contract_version,
            'expected_provider',expected_provider,'expected_upstream_model',expected_upstream_model,
            'public_path',public_path,'introduced_migration',introduced_migration,
            'introduced_publication_id',introduced_publication_id,
            'profile_definition_digest',profile_definition_digest)::text FROM model_capability_probe_contract_bindings`, &p.Bindings},
		{`SELECT jsonb_build_object('operation_id',operation_id,'capability_id',capability_id,
            'execution_profile_id',execution_profile_id,'execution_profile_version',execution_profile_version,
            'protocol_profile_id',protocol_profile_id,'protocol_profile_version',protocol_profile_version,
            'probe_contract_id',probe_contract_id,'probe_contract_version',probe_contract_version)::text
            FROM model_capability_current_probe_contracts`, &p.Selectors},
	}
	for _, query := range queries {
		rows, err := q.Query(ctx, query.sql)
		if err != nil {
			return StaticProjectionV2{}, err
		}
		*query.into = make([]json.RawMessage, 0)
		for rows.Next() {
			var text string
			if err := rows.Scan(&text); err != nil {
				rows.Close()
				return StaticProjectionV2{}, err
			}
			canonical, err := strictjson.Canonicalize([]byte(text))
			if err != nil {
				rows.Close()
				return StaticProjectionV2{}, err
			}
			*query.into = append(*query.into, canonical)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return StaticProjectionV2{}, err
		}
	}
	return p, nil
}

func (p StaticProjectionV2) CanonicalBytes() ([]byte, error) {
	if p.SchemaVersion != ProjectionSchemaV2 {
		return nil, fmt.Errorf("unsupported static projection schema")
	}
	for _, collection := range []*[]json.RawMessage{&p.Definitions, &p.Bindings, &p.Selectors} {
		result := make([]json.RawMessage, 0, len(*collection))
		for _, row := range *collection {
			canonical, err := strictjson.Canonicalize(row)
			if err != nil {
				return nil, err
			}
			result = append(result, canonical)
		}
		// Sort in Go to avoid Target collation changing the publication digest.
		slices.SortFunc(result, func(a, b json.RawMessage) int { return bytes.Compare(a, b) })
		for i := 1; i < len(result); i++ {
			if bytes.Equal(result[i-1], result[i]) {
				return nil, fmt.Errorf("duplicate static projection row")
			}
		}
		*collection = result
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return strictjson.Canonicalize(wire)
}

func (p StaticProjectionV2) Digest() (string, error) {
	wire, err := p.CanonicalBytes()
	if err != nil {
		return "", err
	}
	return sha256Hex(wire), nil
}
