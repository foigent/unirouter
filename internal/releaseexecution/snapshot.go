// Package releaseexecution defines the immutable interpretation sealed into
// the existing Model Release. It owns no activation, pricing or second writer.
package releaseexecution

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/strictjson"
)

const SnapshotSchema = "unirouter.release-execution.v1"
const DecisionContract = "unirouter.capability-decision.v1"
const MaximumSnapshotBytes = 4 << 20

// A pin references retained immutable facts; it does not copy observations
// into the Release or freeze mutable permission, credential or budget state.
type CapabilityPin struct {
	DeploymentID         int64  `json:"deployment_id"`
	OperationID          string `json:"operation_id"`
	CapabilityID         string `json:"capability_id"`
	ApprovalID           int64  `json:"approval_id"`
	ProbeID              int64  `json:"probe_id"`
	ProbeContractID      string `json:"probe_contract_id"`
	ProbeContractVersion string `json:"probe_contract_version"`
	BindingDigest        string `json:"binding_digest"`
	EvidenceDigest       string `json:"evidence_digest"`
	SelectorDigest       string `json:"selector_digest"`
	DecisionContract     string `json:"decision_contract"`
}

type Snapshot struct {
	SchemaVersion           string                                 `json:"schema_version"`
	PublicModelID           int64                                  `json:"public_model_id"`
	ProductLineageID        string                                 `json:"product_lineage_id"`
	ProfileDefinitionDigest string                                 `json:"profile_definition_digest"`
	ExecutionProfileID      string                                 `json:"execution_profile_id"`
	ExecutionProfileVersion string                                 `json:"execution_profile_version"`
	PublicContextLimit      int                                    `json:"public_context_limit"`
	PublicOutputLimit       int                                    `json:"public_output_limit"`
	RouteItemsHash          string                                 `json:"route_items_hash"`
	CapabilityPins          []CapabilityPin                        `json:"capability_pins"`
	Requirements            []modelprofile.ImplementationReference `json:"requirements"`
}

func (s *Snapshot) Clone() *Snapshot {
	if s == nil {
		return nil
	}
	copy := *s
	copy.CapabilityPins = slices.Clone(s.CapabilityPins)
	copy.Requirements = slices.Clone(s.Requirements)
	return &copy
}

func Requirements(definition modelprofile.Definition, pins []CapabilityPin) []modelprofile.ImplementationReference {
	refs := append(definition.Requirements(), modelprofile.ImplementationReference{Kind: "runtime_release", ID: SnapshotSchema}, modelprofile.ImplementationReference{Kind: "capability_decision", ID: DecisionContract})
	for _, pin := range pins {
		refs = append(refs, modelprofile.ImplementationReference{Kind: "capability_probe", ID: pin.ProbeContractID + "@" + pin.ProbeContractVersion})
	}
	modelprofile.SortImplementationReferences(refs)
	return slices.Compact(refs)
}

func (s Snapshot) ValidateDefinition(d modelprofile.Definition) error {
	p := d.Profile()
	if d.Digest() == "" || s.ProfileDefinitionDigest != d.Digest() || s.ProductLineageID != d.ProductLineageID() ||
		s.ExecutionProfileID != p.ID || s.ExecutionProfileVersion != p.Version {
		return fmt.Errorf("Release and immutable Profile definition differ")
	}
	if err := modelprofile.ValidateEffectiveLimits(p, s.PublicContextLimit, s.PublicOutputLimit); err != nil {
		return err
	}
	if !slices.Equal(s.Requirements, Requirements(d, s.CapabilityPins)) {
		return fmt.Errorf("Release requirements differ from exact execution components")
	}
	return nil
}

func (s Snapshot) CanonicalBytes() ([]byte, error) {
	if s.SchemaVersion != SnapshotSchema || s.PublicModelID <= 0 || s.ProductLineageID == "" ||
		!modelprofile.IsDefinitionDigest(s.ProfileDefinitionDigest) || s.ExecutionProfileID == "" || s.ExecutionProfileVersion == "" ||
		s.PublicContextLimit <= 0 || s.PublicOutputLimit <= 0 || !validRouteHash(s.RouteItemsHash) || len(s.Requirements) == 0 {
		return nil, fmt.Errorf("incomplete Release execution snapshot")
	}
	seen := map[string]bool{}
	for _, pin := range s.CapabilityPins {
		key := fmt.Sprintf("%d/%s/%s", pin.DeploymentID, pin.OperationID, pin.CapabilityID)
		if pin.DeploymentID <= 0 || pin.OperationID != modelprofile.OperationOpenAIChat || pin.CapabilityID == "" || pin.ApprovalID <= 0 || pin.ProbeID <= 0 ||
			pin.ProbeContractID == "" || pin.ProbeContractVersion == "" || pin.DecisionContract != DecisionContract ||
			!modelprofile.IsDefinitionDigest(pin.BindingDigest) || !modelprofile.IsDefinitionDigest(pin.EvidenceDigest) ||
			!modelprofile.IsDefinitionDigest(pin.SelectorDigest) || seen[key] {
			return nil, fmt.Errorf("invalid or duplicate Release capability decision")
		}
		seen[key] = true
	}
	wire, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	if len(wire) > MaximumSnapshotBytes {
		return nil, fmt.Errorf("Release snapshot exceeds limit")
	}
	return strictjson.Canonicalize(wire)
}

func validRouteHash(s string) bool {
	return len(s) == 71 && s[:7] == "sha256:" && modelprofile.IsDefinitionDigest(s[7:])
}

func (s Snapshot) Digest() (string, error) {
	wire, err := s.CanonicalBytes()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(wire)
	return hex.EncodeToString(sum[:]), nil
}

func VerifySnapshot(wire []byte, digest string) (Snapshot, error) {
	if len(wire) == 0 || len(wire) > MaximumSnapshotBytes || !modelprofile.IsDefinitionDigest(digest) {
		return Snapshot{}, fmt.Errorf("invalid Release snapshot content address")
	}
	if err := strictjson.Validate(wire); err != nil {
		return Snapshot{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	var s Snapshot
	if err := decoder.Decode(&s); err != nil {
		return Snapshot{}, err
	}
	canonical, err := s.CanonicalBytes()
	if err != nil {
		return Snapshot{}, err
	}
	sum := sha256.Sum256(canonical)
	if !bytes.Equal(wire, canonical) || hex.EncodeToString(sum[:]) != digest {
		return Snapshot{}, fmt.Errorf("Release snapshot canonical bytes or digest mismatch")
	}
	return s, nil
}
