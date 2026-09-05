package modelcapabilityqualification

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

type BindingReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// ResolveDefinitionBindings reads current candidate selectors. A sealed
// Release uses its own immutable pins and never calls this authoring reader.
func ResolveDefinitionBindings(ctx context.Context, q BindingReader, d modelprofile.Definition, operation, protocol, protocolVersion string) ([]Binding, error) {
	p := d.Profile()
	rows, err := q.Query(ctx, `SELECT b.operation_id,b.capability_id,b.execution_profile_id,b.execution_profile_version,
b.protocol_profile_id,b.protocol_profile_version,b.probe_contract_id,b.probe_contract_version,
b.expected_provider,b.expected_upstream_model,b.public_path,COALESCE(b.profile_definition_digest,'')
FROM model_capability_current_probe_contracts c
JOIN model_capability_probe_contract_bindings b USING(operation_id,capability_id,execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version)
WHERE c.operation_id=$1 AND c.execution_profile_id=$2 AND c.execution_profile_version=$3
AND c.protocol_profile_id=$4 AND c.protocol_profile_version=$5 ORDER BY c.capability_id`, operation, p.ID, p.Version, protocol, protocolVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Binding, 0)
	for rows.Next() {
		var b Binding
		var digest string
		if err := rows.Scan(&b.OperationID, &b.CapabilityID, &b.ExecutionProfileID, &b.ExecutionProfileVersion,
			&b.ProtocolProfileID, &b.ProtocolProfileVersion, &b.ProbeContractID, &b.ProbeContractVersion,
			&b.ExpectedProvider, &b.ExpectedUpstreamModel, &b.PublicPath, &digest); err != nil {
			return nil, err
		}
		if digest != "" && digest != d.Digest() {
			return nil, fmt.Errorf("%w: current binding definition mismatch", ErrUnsupported)
		}
		if digest == "" {
			// DecodeDefinition has already proved semantic equivalence at any
			// compiled address. Only historical bindings may lack this column.
			if _, ok := modelprofile.Lookup(p.ID, p.Version); !ok {
				return nil, fmt.Errorf("%w: data binding has no definition pin", ErrUnsupported)
			}
		}
		bound, err := BindDefinition(b, d)
		if err != nil {
			return nil, err
		}
		result = append(result, bound)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
