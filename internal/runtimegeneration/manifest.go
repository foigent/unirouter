// Package runtimegeneration owns binary support and ingress membership. It
// never selects a model Release or grants Provider dispatch authority.
package runtimegeneration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	budgetassets "github.com/uniroute/unirouter/contracts/source-budgets/v1"
	predispatchassets "github.com/uniroute/unirouter/contracts/source-facts/pre-dispatch-v2"
	sourceassets "github.com/uniroute/unirouter/contracts/source-facts/v1"
	observationassets "github.com/uniroute/unirouter/contracts/source-facts/v2"
	"slices"
	"strings"

	"github.com/uniroute/unirouter/internal/metering"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/releaseexecution"
	"github.com/uniroute/unirouter/internal/strictjson"
)

const ManifestSchemaV1 = "unirouter.runtime-support.v1"
const CompatibilityEpoch = modelprofile.DefinitionRuntimeEpoch
const MaximumManifestBytes = 1 << 20
const SourceContentLifecycleContract = "unirouter.source-content-lifecycle.v1"

func ContentLifecycleRequirements() []modelprofile.ImplementationReference {
	return []modelprofile.ImplementationReference{{Kind: "source_content", ID: SourceContentLifecycleContract}}
}

type Manifest struct {
	SchemaVersion      string                                 `json:"schema_version"`
	CompatibilityEpoch string                                 `json:"compatibility_epoch"`
	SchemaHeads        []string                               `json:"schema_heads"`
	Implementations    []modelprofile.ImplementationReference `json:"implementations"`
}

// CurrentManifest is assembled by the binary from its actual schema and
// component registries. Artifact association is separately immutable in DB;
// equal manifests across different artifacts are permitted.
func CurrentManifest(schemaHeads []string) Manifest {
	implementations := modelprofile.Implementations()
	implementations = append(implementations, modelcapabilityqualification.ProbeImplementations()...)
	implementations = append(implementations,
		modelprofile.ImplementationReference{Kind: "source_facts", ID: sourceassets.SchemaVersion},
		modelprofile.ImplementationReference{Kind: "source_content", ID: SourceContentLifecycleContract},
		modelprofile.ImplementationReference{Kind: "source_budgets", ID: budgetassets.SchemaVersion},
		modelprofile.ImplementationReference{Kind: "financial_journal", ID: budgetassets.WriterContract},
		modelprofile.ImplementationReference{Kind: "source_observations", ID: observationassets.SchemaVersion},
		modelprofile.ImplementationReference{Kind: "source_observations", ID: predispatchassets.SchemaVersion},
		modelprofile.ImplementationReference{Kind: "source_dispatch", ID: observationassets.DispatchOutboxContract},
		modelprofile.ImplementationReference{Kind: "stream_counter", ID: metering.GatewayCounterEstimateVersionV2},
		modelprofile.ImplementationReference{Kind: "stream_counter", ID: metering.GatewayCounterEstimateVersionV3},
		modelprofile.ImplementationReference{Kind: "capability_projection", ID: publication.ProjectionSchemaV2},
		modelprofile.ImplementationReference{Kind: "capability_materializer", ID: publication.MaterializerContractV2},
		modelprofile.ImplementationReference{Kind: "runtime_release", ID: releaseexecution.SnapshotSchema},
		modelprofile.ImplementationReference{Kind: "capability_decision", ID: releaseexecution.DecisionContract})
	modelprofile.SortImplementationReferences(implementations)
	return Manifest{ManifestSchemaV1, CompatibilityEpoch, slices.Clone(schemaHeads), implementations}
}

func (m Manifest) CanonicalBytes() ([]byte, error) {
	if m.SchemaVersion != ManifestSchemaV1 || m.CompatibilityEpoch != CompatibilityEpoch ||
		len(m.SchemaHeads) == 0 || len(m.Implementations) == 0 {
		return nil, fmt.Errorf("runtime support manifest identity is invalid")
	}
	m.SchemaHeads = slices.Clone(m.SchemaHeads)
	slices.Sort(m.SchemaHeads)
	for i, head := range m.SchemaHeads {
		if head == "" || head != strings.TrimSpace(head) || (i > 0 && head == m.SchemaHeads[i-1]) {
			return nil, fmt.Errorf("runtime schema support is invalid or duplicated")
		}
	}
	m.Implementations = slices.Clone(m.Implementations)
	modelprofile.SortImplementationReferences(m.Implementations)
	for i, ref := range m.Implementations {
		if ref.Kind == "" || ref.ID == "" || ref.Kind != strings.TrimSpace(ref.Kind) ||
			ref.ID != strings.TrimSpace(ref.ID) || strings.ContainsAny(ref.Kind+ref.ID, "\x00\r\n") ||
			(i > 0 && ref == m.Implementations[i-1]) {
			return nil, fmt.Errorf("runtime implementation support is invalid or duplicated")
		}
	}
	wire, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	if len(wire) > MaximumManifestBytes {
		return nil, fmt.Errorf("runtime support manifest is too large")
	}
	return strictjson.Canonicalize(wire)
}

func (m Manifest) Digest() (string, error) {
	wire, err := m.CanonicalBytes()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(wire)
	return hex.EncodeToString(sum[:]), nil
}

func VerifyManifest(wire []byte, digest string) (Manifest, error) {
	if len(wire) == 0 || len(wire) > MaximumManifestBytes || !modelprofile.IsDefinitionDigest(digest) {
		return Manifest{}, fmt.Errorf("runtime support content address is invalid")
	}
	if err := strictjson.Validate(wire); err != nil {
		return Manifest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, err
	}
	canonical, err := manifest.CanonicalBytes()
	if err != nil {
		return Manifest{}, err
	}
	sum := sha256.Sum256(canonical)
	if hex.EncodeToString(sum[:]) != digest || !bytes.Equal(wire, canonical) {
		return Manifest{}, fmt.Errorf("runtime support canonical bytes or digest mismatch")
	}
	return manifest, nil
}

// Require compares set membership, not whole-manifest equality. Candidate
// definitions may require fewer implementations than the binary supplies.
// Publication validation must also validate the complete typed composition.
func (m Manifest) Require(schemaHead string, refs []modelprofile.ImplementationReference) error {
	if _, err := m.CanonicalBytes(); err != nil {
		return err
	}
	if !slices.Contains(m.SchemaHeads, schemaHead) {
		return fmt.Errorf("unsupported exact schema head %q", schemaHead)
	}
	if len(refs) == 0 {
		return fmt.Errorf("execution requirements are empty")
	}
	support := make(map[modelprofile.ImplementationReference]bool, len(m.Implementations))
	for _, ref := range m.Implementations {
		support[ref] = true
	}
	for _, ref := range refs {
		if !support[ref] {
			return fmt.Errorf("unsupported implementation %s/%s", ref.Kind, ref.ID)
		}
	}
	return nil
}
