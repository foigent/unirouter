package modelcapabilitypublication

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

// CarrierIdentityV2 identifies the independently verified CI artifact. The
// download/provenance owner supplies it after checking the configured repository,
// exact successful workflow/head and artifact bytes; JSON alone is no approval.
type CarrierIdentityV2 struct {
	Repository    string `json:"repository"`
	Commit        string `json:"commit"`
	Tree          string `json:"tree"`
	Path          string `json:"path"`
	WorkflowRunID int64  `json:"workflow_run_id"`
	ArtifactID    int64  `json:"artifact_id"`
}
type PublisherIdentityV2 struct {
	Artifact ArtifactIdentity   `json:"artifact"`
	Carrier  *CarrierIdentityV2 `json:"carrier"`
}
type AuthorityV2 struct {
	Environment             string
	ArtifactMigrationLedger []string
	Publisher               PublisherIdentityV2
	ApprovedBundleDigest    string
	ValidateBinding         BindingImplementationValidator
}
type PlanRequestV2 struct {
	PublicationID            string
	Kind                     string
	CompensatesPublicationID string
	BundleBytes              []byte
}

// PlanV2 retains the current predecessor and exact delta, never the historical
// migration inventory, complete bindings or NoOp entries. Digest() is external
// to canonical bytes to avoid a self-referential hash.
type PlanV2 struct {
	SchemaVersion            string                `json:"schema_version"`
	ProjectionSchemaVersion  string                `json:"projection_schema_version"`
	MaterializerContract     string                `json:"materializer_contract_version"`
	PublicationID            string                `json:"publication_id"`
	Kind                     string                `json:"kind"`
	CompensatesPublicationID string                `json:"compensates_publication_id"`
	LegacyThrough            string                `json:"legacy_through"`
	LegacyProjectionDigest   string                `json:"legacy_projection_digest"`
	ExpectedPredecessor      string                `json:"expected_predecessor"`
	ExpectedProjectionDigest string                `json:"expected_projection_digest"`
	ResultProjectionDigest   string                `json:"result_projection_digest"`
	BundleDigest             string                `json:"bundle_digest"`
	Target                   TargetIdentity        `json:"target_identity"`
	Publisher                PublisherIdentityV2   `json:"publisher_identity"`
	DefinitionDigests        []string              `json:"definition_digests"`
	BindingDelta             []DefinitionBindingV2 `json:"binding_delta"`
	SelectorDelta            []SelectorTransition  `json:"selector_delta"`
}

func (p PlanV2) CanonicalBytes() ([]byte, error) { return canonicalV2(p) }
func (p PlanV2) Digest() (string, error) {
	wire, err := p.CanonicalBytes()
	if err != nil {
		return "", err
	}
	return sha256Hex(wire), nil
}
func DecodePlanV2(wire []byte, digest string) (PlanV2, error) {
	var p PlanV2
	if len(wire) == 0 || len(wire) > MaximumBundleBytes || !modelprofile.IsDefinitionDigest(digest) || sha256Hex(wire) != digest {
		return p, fmt.Errorf("Plan content digest mismatch")
	}
	if err := decodeStrict(wire, &p); err != nil {
		return p, err
	}
	canonical, err := p.CanonicalBytes()
	if err != nil || !bytes.Equal(wire, canonical) {
		return p, fmt.Errorf("Plan bytes must be canonical")
	}
	if p.SchemaVersion != PlanSchemaV2 || p.ProjectionSchemaVersion != ProjectionSchemaV2 || p.MaterializerContract != MaterializerContractV2 {
		return p, fmt.Errorf("unsupported Plan contract")
	}
	return p, nil
}

func validateAuthorityV2(a AuthorityV2, hasBundle bool) error {
	w := a.Publisher.Artifact
	if a.Environment == "" || a.Environment == "unspecified" || !gitObjectPattern.MatchString(w.Commit) || !gitObjectPattern.MatchString(w.Tree) ||
		validateText("writer version", w.Version) != nil || !sha256Pattern.MatchString(w.PublisherExecutableDigest) ||
		(w.Kind != ArtifactKindSource && w.Kind != ArtifactKindImage) ||
		(w.Kind == ArtifactKindImage && !sha256Pattern.MatchString(w.ImageDigest)) || (w.Kind == ArtifactKindSource && w.ImageDigest != "") {
		return fmt.Errorf("exact publisher artifact and trusted environment are required")
	}
	if hasBundle {
		c := a.Publisher.Carrier
		if !modelprofile.IsDefinitionDigest(a.ApprovedBundleDigest) || c == nil || c.Repository == "" || !gitObjectPattern.MatchString(c.Commit) || !gitObjectPattern.MatchString(c.Tree) ||
			c.Path == "" || strings.Contains(c.Path, "..") || strings.HasPrefix(c.Path, "/") || c.WorkflowRunID <= 0 || c.ArtifactID <= 0 {
			return fmt.Errorf("verified non-binary CI carrier and independent Bundle digest are required")
		}
	} else if a.ApprovedBundleDigest != "" || a.Publisher.Carrier != nil {
		return fmt.Errorf("legacy import must not carry candidate data")
	}
	return nil
}

// BuildPlanV2 is read-only in an existing repeatable-read transaction. Apply
// repeats it under the shared authority lock and compares exact Plan bytes.
func BuildPlanV2(ctx context.Context, tx pgx.Tx, a AuthorityV2, r PlanRequestV2) (PlanV2, error) {
	var p PlanV2
	if !publicationIdentityV2(r.PublicationID) || (r.Kind != "legacy_import" && r.Kind != "delta" && r.Kind != "compensation") {
		return p, fmt.Errorf("publication identity/kind is invalid")
	}
	hasBundle := r.Kind != "legacy_import"
	if err := validateAuthorityV2(a, hasBundle); err != nil {
		return p, err
	}
	ledger, err := readLedger(ctx, tx)
	if err != nil {
		return p, err
	}
	if err := schemaauthority.VerifyRuntimeLedger(a.ArtifactMigrationLedger, ledger); err != nil {
		return p, err
	}
	if ledger[len(ledger)-1] != schemaauthority.CapabilityExpandHead {
		return p, fmt.Errorf("explicit additive expand must precede capability authoring")
	}
	target, err := readTargetIdentity(ctx, tx, a.Environment)
	if err != nil {
		return p, err
	}
	head, err := ReadPublicationHeadV2(ctx, tx)
	if err != nil {
		return p, err
	}
	before, err := ReadStaticProjectionV2(ctx, tx)
	if err != nil {
		return p, err
	}
	beforeDigest, err := before.Digest()
	if err != nil {
		return p, err
	}
	p = PlanV2{SchemaVersion: PlanSchemaV2, ProjectionSchemaVersion: ProjectionSchemaV2, MaterializerContract: MaterializerContractV2,
		PublicationID: r.PublicationID, Kind: r.Kind, CompensatesPublicationID: r.CompensatesPublicationID,
		ExpectedPredecessor: head.PublicationID, ExpectedProjectionDigest: beforeDigest, ResultProjectionDigest: beforeDigest,
		Target: target, Publisher: a.Publisher, DefinitionDigests: []string{}, BindingDelta: []DefinitionBindingV2{}, SelectorDelta: []SelectorTransition{}}
	if !hasBundle {
		if head.PublicationID != "" || len(r.BundleBytes) != 0 || r.CompensatesPublicationID != "" || len(before.Definitions) != 0 {
			return p, fmt.Errorf("%w: legacy import is not an empty initial authoring transition", ErrPlanConflict)
		}
		legacyLedger := ledger[:len(ledger)-1]
		folded, err := FoldedProjectionForLedger(legacyLedger)
		if err != nil {
			return p, err
		}
		if err := VerifyStaticProjection(ctx, tx, folded); err != nil {
			return p, err
		}
		// CanonicalBaseline validates the special M100 export object only.
		// Import retains the fully folded actual legacy projection at this
		// Target's applied head, after VerifyStaticProjection proved equality.
		legacyWire, err := canonicalV2(normalizeBaseline(folded))
		if err != nil {
			return p, err
		}
		p.LegacyThrough = legacyLedger[len(legacyLedger)-1]
		p.LegacyProjectionDigest = sha256Hex(legacyWire)
		return p, nil
	}
	if head.PublicationID == "" || head.ProjectionDigest != beforeDigest {
		return p, fmt.Errorf("%w: actual static projection differs from imported publication head", ErrPlanConflict)
	}
	// A copied ledger from a different Target must not authorize new publications.
	var importedTarget []byte
	if err := tx.QueryRow(ctx, `SELECT target_identity::text FROM capability_publications WHERE kind='legacy_import'`).Scan(&importedTarget); err != nil {
		return p, err
	}
	var oldTarget TargetIdentity
	if err := decodeStrict(importedTarget, &oldTarget); err != nil || oldTarget != target {
		return p, fmt.Errorf("capability ledger Target identity mismatch")
	}
	if r.Kind == "compensation" {
		var found bool
		if !publicationIdentityV2(r.CompensatesPublicationID) {
			return p, fmt.Errorf("compensation requires an exact retained publication")
		}
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM capability_publications WHERE publication_id=$1 AND kind<>'legacy_import')`, r.CompensatesPublicationID).Scan(&found); err != nil {
			return p, err
		}
		if !found {
			return p, fmt.Errorf("compensated publication is absent")
		}
	} else if r.CompensatesPublicationID != "" {
		return p, fmt.Errorf("delta cannot impersonate compensation")
	}
	bundle, err := VerifyBundleV2(r.BundleBytes, a.ApprovedBundleDigest)
	if err != nil {
		return p, err
	}
	p.BundleDigest = a.ApprovedBundleDigest
	for _, wire := range bundle.Definitions {
		d, _ := modelprofile.DecodeDefinition(wire)
		p.DefinitionDigests = append(p.DefinitionDigests, d.Digest())
	}
	slices.Sort(p.DefinitionDigests)
	p.BindingDelta = slices.Clone(bundle.BindingDelta)
	p.SelectorDelta = slices.Clone(bundle.SelectorDelta)
	for _, selector := range bundle.SelectorDelta {
		// Compiled Profile addresses remain historical authoring authority.
		// Freeze their selectors permanently; an upgrade first creates a data
		// revision. This also excludes races with in-progress legacy sealing.
		if _, compiled := modelprofile.Lookup(selector.ExecutionProfileID, selector.ExecutionProfileVersion); compiled {
			return p, fmt.Errorf("%w: legacy Profile selectors remain frozen; publish a data revision", ErrPlanConflict)
		}
		var legacy bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runtime_change_releases WHERE execution_profile_id=$1 AND execution_profile_version=$2 AND route_items_sealed_at IS NOT NULL AND execution_snapshot_digest IS NULL)`, selector.ExecutionProfileID, selector.ExecutionProfileVersion).Scan(&legacy); err != nil {
			return p, err
		}
		if legacy {
			return p, fmt.Errorf("%w: selector mutation would reinterpret a sealed legacy Release", ErrPlanConflict)
		}
	}
	after, err := projectDeltaV2(before, bundle, r.PublicationID, a.ValidateBinding)
	if err != nil {
		return p, err
	}
	p.ResultProjectionDigest, err = after.Digest()
	return p, err
}

func publicationIdentityV2(id string) bool {
	if len(id) == 0 || len(id) > 200 {
		return false
	}
	for i, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || i > 0 && strings.ContainsRune("._/-", c)) {
			return false
		}
	}
	return true
}
