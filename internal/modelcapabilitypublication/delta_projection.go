package modelcapabilitypublication

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/strictjson"
)

// BindingImplementationValidator is supplied by the installed binary's typed
// Probe dispatcher. Bundle bytes cannot supply this authority. Publication
// cannot import that dispatcher, because its legacy reader already depends on
// this package; passing the executable validator keeps one implementation owner.
type BindingImplementationValidator func(DefinitionBindingV2, modelprofile.Definition) error

func projectDeltaV2(before StaticProjectionV2, bundle BundleV2, publicationID string, validate BindingImplementationValidator) (StaticProjectionV2, error) {
	after := StaticProjectionV2{ProjectionSchemaV2, slices.Clone(before.Definitions), slices.Clone(before.Bindings), slices.Clone(before.Selectors)}
	definitions := map[string]modelprofile.Definition{}
	addresses := map[string]string{}
	for _, wire := range before.Definitions {
		var row struct {
			Digest string `json:"definition_digest"`
			Bytes  string `json:"canonical_bytes"`
		}
		if err := json.Unmarshal(wire, &row); err != nil {
			return after, err
		}
		canonical, err := hex.DecodeString(row.Bytes)
		if err != nil {
			return after, err
		}
		d, err := modelprofile.VerifyDefinition(canonical, row.Digest)
		if err != nil {
			return after, err
		}
		definitions[d.Digest()] = d
		p := d.Profile()
		addresses[p.ID+"@"+p.Version] = d.Digest()
	}
	for _, wire := range bundle.Definitions {
		d, err := modelprofile.DecodeDefinition(wire)
		if err != nil {
			return after, err
		}
		p := d.Profile()
		address := p.ID + "@" + p.Version
		if _, exists := addresses[address]; exists {
			return after, fmt.Errorf("%w: definition address already exists; omit no-op data", ErrPlanConflict)
		}
		definitions[d.Digest()] = d
		addresses[address] = d.Digest()
		row, err := canonicalV2(map[string]any{"definition_digest": d.Digest(), "profile_id": p.ID, "profile_version": p.Version,
			"product_lineage_id": d.ProductLineageID(), "predecessor_digest": nullableV2(d.PredecessorDigest()),
			"schema_version": modelprofile.DefinitionSchemaV1, "canonical_bytes": hex.EncodeToString(d.CanonicalBytes()), "publication_id": publicationID})
		if err != nil {
			return after, err
		}
		after.Definitions = append(after.Definitions, row)
	}
	for _, wire := range bundle.Definitions {
		d, _ := modelprofile.DecodeDefinition(wire)
		if d.PredecessorDigest() != "" {
			predecessor, found := definitions[d.PredecessorDigest()]
			if !found || predecessor.ProductLineageID() != d.ProductLineageID() || predecessor.Profile().Version == d.Profile().Version {
				return after, fmt.Errorf("definition predecessor is absent or belongs to another product/revision")
			}
		}
	}
	bindings := map[string]bool{}
	for _, wire := range before.Bindings {
		var s Selector
		if err := json.Unmarshal(wire, &s); err != nil {
			return after, err
		}
		bindings[dataBindingKey(s)] = true
	}
	for _, b := range bundle.BindingDelta {
		key := dataBindingKey(b.Selector)
		if bindings[key] {
			return after, fmt.Errorf("%w: binding exists; no rewrite/no-op", ErrPlanConflict)
		}
		d, found := definitions[b.ProfileDefinitionDigest]
		if !found || validate == nil {
			return after, fmt.Errorf("binding definition or installed validator unavailable")
		}
		if err := validate(b, d); err != nil {
			return after, err
		}
		row, err := canonicalV2(map[string]any{"operation_id": b.OperationID, "capability_id": b.CapabilityID,
			"execution_profile_id": b.ExecutionProfileID, "execution_profile_version": b.ExecutionProfileVersion,
			"protocol_profile_id": b.ProtocolProfileID, "protocol_profile_version": b.ProtocolProfileVersion,
			"probe_contract_id": b.ProbeContractID, "probe_contract_version": b.ProbeContractVersion,
			"expected_provider": b.ExpectedProvider, "expected_upstream_model": b.ExpectedUpstreamModel, "public_path": b.PublicPath,
			"introduced_migration": nil, "introduced_publication_id": publicationID, "profile_definition_digest": b.ProfileDefinitionDigest})
		if err != nil {
			return after, err
		}
		after.Bindings = append(after.Bindings, row)
		bindings[key] = true
	}
	selectors := map[string]int{}
	for i, wire := range before.Selectors {
		var s Selector
		if err := json.Unmarshal(wire, &s); err != nil {
			return after, err
		}
		selectors[dataSelectorKey(s)] = i
	}
	for _, transition := range bundle.SelectorDelta {
		final := selectorFinalV2(transition)
		key := dataSelectorKey(final)
		index, found := selectors[key]
		if found != (transition.ExpectedPredecessor != nil) {
			return after, fmt.Errorf("%w: selector presence changed", ErrPlanConflict)
		}
		if found {
			var old Selector
			if err := json.Unmarshal(after.Selectors[index], &old); err != nil {
				return after, err
			}
			if old.ProbeContractID != transition.ExpectedPredecessor.ProbeContractID || old.ProbeContractVersion != transition.ExpectedPredecessor.ProbeContractVersion {
				return after, fmt.Errorf("%w: selector predecessor changed", ErrPlanConflict)
			}
		}
		if !bindings[dataBindingKey(final)] {
			return after, fmt.Errorf("selector has no exact immutable binding")
		}
		row, err := canonicalV2(final)
		if err != nil {
			return after, err
		}
		if found {
			after.Selectors[index] = row
		} else {
			selectors[key] = len(after.Selectors)
			after.Selectors = append(after.Selectors, row)
		}
	}
	return after, nil
}

func canonicalV2(value any) ([]byte, error) {
	wire, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return strictjson.Canonicalize(wire)
}
func nullableV2(value string) any {
	if value == "" {
		return nil
	}
	return value
}
