package modelprofile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/uniroute/unirouter/internal/strictjson"
	"github.com/uniroute/unirouter/internal/tokenprofile"
)

const DefinitionSchemaV1 = "unirouter.profile-definition.v1"
const DefinitionRuntimeEpoch = "unirouter.runtime-compatibility.v1"
const MaximumDefinitionBytes = 64 << 10

var definitionIdentity = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,199}$`)
var definitionProfileID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,99}$`)
var definitionVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// Definition retains the canonical bytes, not a pointer to mutable catalog
// state. Accessors return copies. Authority to publish or activate it belongs
// to Publication and Release, never to this data decoder.
type Definition struct {
	profile     Profile
	lineage     string
	predecessor string
	canonical   []byte
	digest      string
}

type definitionDocument struct {
	SchemaVersion       string          `json:"schema_version"`
	MinimumRuntimeEpoch string          `json:"minimum_runtime_compatibility_epoch"`
	ProductLineageID    string          `json:"product_lineage_id"`
	PredecessorDigest   string          `json:"predecessor_digest,omitempty"`
	Profile             json.RawMessage `json:"profile"`
}

func (d Definition) Profile() Profile          { return clone(d.profile) }
func (d Definition) ProductLineageID() string  { return d.lineage }
func (d Definition) PredecessorDigest() string { return d.predecessor }
func (d Definition) Digest() string            { return d.digest }
func (d Definition) CanonicalBytes() []byte    { return bytes.Clone(d.canonical) }

// OperationProtocolIdentity comes from validated immutable data. The legacy
// Profile-address registry remains solely the explicit compiled fallback.
func (d Definition) OperationProtocolIdentity(operation string) (OperationProtocolIdentity, error) {
	if d.digest == "" || operation != OperationOpenAIChat || d.profile.APISurface != APISurfaceOpenAIChat {
		return OperationProtocolIdentity{}, fmt.Errorf("unsupported data Profile operation")
	}
	return OperationProtocolIdentity{OperationID: operation, ProtocolProfileID: d.profile.ProtocolProfile, ProtocolProfileVersion: ChatOperationProtocolBindingVersionV1}, nil
}

// NewDefinition is authoring only. Current attachment metadata is deliberately
// removed; publication selectors and Release pins have their own owners.
func NewDefinition(profile Profile, lineage, predecessor string) (Definition, error) {
	profile.Current = false
	wire, err := json.Marshal(struct {
		Profile
		Current *bool `json:"current,omitempty"`
	}{Profile: clone(profile)})
	if err != nil {
		return Definition{}, err
	}
	document, err := json.Marshal(definitionDocument{SchemaVersion: DefinitionSchemaV1, MinimumRuntimeEpoch: DefinitionRuntimeEpoch,
		ProductLineageID: lineage, PredecessorDigest: predecessor, Profile: wire})
	if err != nil {
		return Definition{}, err
	}
	return DecodeDefinition(document)
}

// DecodeDefinition rejects ambiguous JSON, unknown fields and unavailable
// implementation references. It never guesses behavior from a model name.
// Schema v1 authoring covers typed Chat; existing native protocol Profiles
// continue through their established immutable path.
func DecodeDefinition(wire []byte) (Definition, error) {
	if len(wire) == 0 || len(wire) > MaximumDefinitionBytes {
		return Definition{}, fmt.Errorf("profile definition size is invalid")
	}
	if err := strictjson.Validate(wire); err != nil {
		return Definition{}, err
	}
	if err := exactDefinitionKeys(wire, reflect.TypeOf(definitionDocument{})); err != nil {
		return Definition{}, err
	}
	var doc definitionDocument
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return Definition{}, err
	}
	if doc.SchemaVersion != DefinitionSchemaV1 || doc.MinimumRuntimeEpoch != DefinitionRuntimeEpoch || !definitionIdentity.MatchString(doc.ProductLineageID) {
		return Definition{}, fmt.Errorf("profile definition schema or lineage is invalid")
	}
	if doc.PredecessorDigest != "" && !IsDefinitionDigest(doc.PredecessorDigest) {
		return Definition{}, fmt.Errorf("profile predecessor digest is invalid")
	}
	if err := exactDefinitionKeys(doc.Profile, reflect.TypeOf(Profile{})); err != nil {
		return Definition{}, err
	}
	var data struct {
		Profile
		Current json.RawMessage `json:"current"`
	}
	decoder = json.NewDecoder(bytes.NewReader(doc.Profile))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return Definition{}, err
	}
	if len(data.Current) != 0 {
		return Definition{}, fmt.Errorf("current attachment state is not profile definition data")
	}
	if err := validateDefinitionProfile(data.Profile); err != nil {
		return Definition{}, err
	}
	// Re-encode typed data so omitted defaults have one representation. Array
	// order is retained: this is also the historical component contract order.
	profile := clone(data.Profile)
	profile.Current = false
	canonicalProfile, err := json.Marshal(struct {
		Profile
		Current *bool `json:"current,omitempty"`
	}{Profile: profile})
	if err != nil {
		return Definition{}, err
	}
	doc.Profile = canonicalProfile
	canonical, err := json.Marshal(doc)
	if err != nil {
		return Definition{}, err
	}
	canonical, err = strictjson.Canonicalize(canonical)
	if err != nil {
		return Definition{}, err
	}
	sum := sha256.Sum256(canonical)
	return Definition{profile, doc.ProductLineageID, doc.PredecessorDigest, canonical, hex.EncodeToString(sum[:])}, nil
}

// encoding/json accepts case-insensitive aliases even with
// DisallowUnknownFields. Definitions use only their schema's exact member
// names, so differently cased duplicate semantic fields cannot be normalized
// into a valid signed object. These two schema objects have no embedded fields.
func exactDefinitionKeys(wire []byte, schema reflect.Type) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(wire, &fields); err != nil || fields == nil {
		return fmt.Errorf("profile definition requires a JSON object")
	}
	allowed := make(map[string]bool, schema.NumField())
	for i := 0; i < schema.NumField(); i++ {
		name, _, _ := strings.Cut(schema.Field(i).Tag.Get("json"), ",")
		allowed[name] = true
	}
	for name := range fields {
		if !allowed[name] {
			return fmt.Errorf("unknown profile definition member %q", name)
		}
	}
	return nil
}

func IsDefinitionDigest(digest string) bool {
	decoded, err := hex.DecodeString(digest)
	return err == nil && len(decoded) == sha256.Size && digest == strings.ToLower(digest)
}

// VerifyDefinition proves the stored content-addressed value before use. A
// failed lookup or hash check must never fall back to a current revision.
func VerifyDefinition(wire []byte, expectedDigest string) (Definition, error) {
	if !IsDefinitionDigest(expectedDigest) {
		return Definition{}, fmt.Errorf("expected profile digest is invalid")
	}
	d, err := DecodeDefinition(wire)
	if err != nil {
		return Definition{}, err
	}
	if d.Digest() != expectedDigest || !bytes.Equal(wire, d.canonical) {
		return Definition{}, fmt.Errorf("profile definition bytes do not match exact canonical digest")
	}
	return d, nil
}

func validateDefinitionProfile(p Profile) error {
	// An existing immutable address cannot acquire different semantics through
	// the data carrier. A genuine change requires a new revision, even when the
	// compiled catalog happens to contain the old address on this binary.
	if legacy, ok := Lookup(p.ID, p.Version); ok {
		legacy.Current = false
		if !reflect.DeepEqual(legacy, p) {
			return fmt.Errorf("data definition reinterprets a compiled Profile address")
		}
	}
	if !definitionProfileID.MatchString(p.ID) || !definitionVersion.MatchString(p.Version) || len(p.Version) > 40 ||
		p.ID == UnverifiedProfileID || p.LegacyProtocolAuthority || p.Current ||
		p.APISurface != APISurfaceOpenAIChat || p.ProtocolProfileVersion != "" || p.OperationPolicyProfile != "" {
		return fmt.Errorf("profile definition requires a typed Chat execution identity")
	}
	for _, value := range []string{p.Provider, p.Region, p.Label} {
		if value == "" || value != strings.TrimSpace(value) || strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("invalid profile identity text")
		}
	}
	for _, list := range [][]string{p.Operations, p.EndpointClasses, p.UpstreamModelIDs, p.Capabilities, p.ProviderCapabilities, p.AlwaysRequiredCapabilities, p.Aliases, p.EvidenceRefs} {
		seen := map[string]bool{}
		for _, value := range list {
			if value == "" || value != strings.TrimSpace(value) || strings.ContainsAny(value, "\x00\r\n") || seen[value] {
				return fmt.Errorf("invalid or duplicate profile collection member")
			}
			seen[value] = true
		}
	}
	if len(p.EndpointClasses) == 0 || len(p.UpstreamModelIDs) == 0 || p.ProviderContextWindow <= 0 ||
		p.ProviderMaxOutput < 0 || p.DefaultContextWindow <= 0 || p.DefaultOutputLimit <= 0 {
		return fmt.Errorf("profile definition requires finite positive execution limits and product endpoints")
	}
	if err := ValidateEffectiveLimits(p, p.DefaultContextWindow, p.DefaultOutputLimit); err != nil {
		return err
	}
	for _, endpoint := range p.EndpointClasses {
		if _, ok := endpointPathModes[endpoint]; !ok {
			return fmt.Errorf("unknown endpoint implementation %q", endpoint)
		}
	}
	if _, ok := ResolveChatProtocolContract(p.ProtocolProfile); !ok {
		return fmt.Errorf("unknown Chat implementation")
	}
	if _, ok := ResolveReasoningContract(p.ReasoningProfile); !ok {
		return fmt.Errorf("unknown reasoning implementation")
	}
	if _, ok := ResolveCacheUsageContract(p.CacheUsageProfile); !ok {
		return fmt.Errorf("unknown usage implementation")
	}
	if !tokenprofile.Supports(p.TokenizerProfileID, p.TokenizerProfileVersion) &&
		(p.TokenizerProfileID != TokenizerByteUpperID || p.TokenizerProfileVersion != TokenizerByteUpperVersion) {
		return fmt.Errorf("unknown tokenizer implementation")
	}
	for _, capability := range p.Capabilities {
		switch capability {
		case CapabilityStreaming, CapabilityPartialMode, CapabilityDynamicTools, CapabilityBuiltinWebSearch,
			CapabilityVision, CapabilityAudio, CapabilityVideo, CapabilityFiles, CapabilityTools, CapabilityFunctionTools,
			CapabilityToolChoiceAuto, CapabilityToolChoiceNamed, CapabilityToolChoiceReq, CapabilityParallelTools,
			CapabilityToolStream, CapabilityReasoning, CapabilityReasoningBudget, CapabilityReasoningEffort,
			CapabilityJSONObject, CapabilityJSONSchema, CapabilityStrictJSONSchema, CapabilityJSONOutput,
			CapabilityPromptCache, CapabilityExplicitCache, CapabilityCacheReadUsage, CapabilityCacheCreateUsage,
			CapabilityBuiltinSearchReq, CapabilityServiceTier:
		default:
			return fmt.Errorf("unknown executable capability %q", capability)
		}
	}
	return validateExecutableComponents(p, false)
}
