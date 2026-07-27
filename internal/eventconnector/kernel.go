package eventconnector

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	EventEnvelopeSpecVersion = "foigent.event-envelope.v1"
	IdempotencyDomain        = "foigent.event-idempotency.v1"
	maxSafeJSONInteger       = int64(9_007_199_254_740_991)
)

var (
	sha256ReferencePattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	commitPattern          = regexp.MustCompile(`^[0-9a-f]{40}$`)
	semverPattern          = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	productPattern         = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	connectorIDPattern     = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_-]*)+$`)
	eventTypePattern       = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_]*)+$`)
	eventSchemaRefPattern  = regexp.MustCompile(`^foigent://events/[a-z0-9][a-z0-9/_-]*/v[1-9][0-9]*$`)
	positiveDecimalPattern = regexp.MustCompile(`^[1-9][0-9]*$`)
	versionPattern         = regexp.MustCompile(`^[1-9][0-9]*$`)
	fieldGroupNamePattern  = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)
	subjectTypePattern     = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	int64DecimalPattern    = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
	microsecondTimePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z$`)
)

var prohibitedDeliveryKeys = map[string]struct{}{
	"source_cursor":         {},
	"next_cursor":           {},
	"cursor":                {},
	"high_watermark":        {},
	"generated_at":          {},
	"has_more":              {},
	"page":                  {},
	"checkpoint":            {},
	"acknowledged_sequence": {},
	"ingested_at":           {},
	"ingestion":             {},
	"retry":                 {},
}

type ProtocolString struct {
	value string
}

func NewProtocolString(name, value string) (ProtocolString, *ConnectorError) {
	if value == "" || !utf8.ValidString(value) {
		return ProtocolString{}, contractError(
			"invalid_protocol_string",
			"validate",
			"validate_protocol_string",
			name+" must be nonempty valid UTF-8",
			nil,
		)
	}
	if !norm.NFC.IsNormalString(value) {
		return ProtocolString{}, contractError(
			"protocol_string_not_nfc",
			"validate",
			"validate_protocol_string",
			name+" must already be NFC",
			nil,
		)
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return ProtocolString{}, contractError(
				"invalid_protocol_string",
				"validate",
				"validate_protocol_string",
				name+" must not contain control characters",
				nil,
			)
		}
	}
	return ProtocolString{value: value}, nil
}

func (value ProtocolString) String() string {
	return value.value
}

type OpaqueString struct {
	value string
}

func NewOpaqueString(
	name string,
	value string,
	minimumBytes int,
	maximumBytes int,
	rejectControl bool,
) (OpaqueString, *ConnectorError) {
	if !utf8.ValidString(value) || len(value) < minimumBytes || len(value) > maximumBytes {
		return OpaqueString{}, contractError(
			"invalid_opaque_string",
			"validate",
			"validate_opaque_string",
			fmt.Sprintf("%s must contain %d..%d exact UTF-8 bytes", name, minimumBytes, maximumBytes),
			nil,
		)
	}
	if rejectControl {
		for _, char := range value {
			if unicode.IsControl(char) {
				return OpaqueString{}, contractError(
					"invalid_opaque_string",
					"validate",
					"validate_opaque_string",
					name+" must not contain control characters",
					nil,
				)
			}
		}
	}
	return OpaqueString{value: value}, nil
}

func (value OpaqueString) String() string {
	return value.value
}

type SchemaBinding struct {
	SchemaID string `json:"schema_id"`
	SHA256   string `json:"sha256"`
}

type EventSchemaBinding struct {
	EventType     string `json:"event_type"`
	SchemaRef     string `json:"schema_ref"`
	SchemaVersion string `json:"schema_version"`
	SHA256        string `json:"sha256"`
}

type DescriptorBinding struct {
	ProtocolVersion      string
	ProtocolCommit       string
	ReleaseDigest        string
	ManifestDigest       string
	EnvelopeSpecVersion  string
	EnvelopeSchemaDigest string
	SourceProduct        string
	ConnectorID          string
	ConnectorVersion     string
	SourceSchema         SchemaBinding
	EmittedEvents        []EventSchemaBinding
}

type Descriptor struct {
	binding DescriptorBinding
	key     string
	events  map[string]EventSchemaBinding
}

func NewDescriptor(binding DescriptorBinding) (Descriptor, *ConnectorError) {
	protocolVersion, err := NewProtocolString("protocol_version", binding.ProtocolVersion)
	if err != nil {
		return Descriptor{}, err
	}
	if !semverPattern.MatchString(protocolVersion.String()) {
		return Descriptor{}, contractError("invalid_protocol_version", "configure", "bind_descriptor", "Protocol version is invalid", nil)
	}
	if !commitPattern.MatchString(binding.ProtocolCommit) {
		return Descriptor{}, contractError("invalid_protocol_commit", "configure", "bind_descriptor", "Protocol commit is invalid", nil)
	}
	for name, digest := range map[string]string{
		"release_digest":         binding.ReleaseDigest,
		"manifest_digest":        binding.ManifestDigest,
		"envelope_schema_digest": binding.EnvelopeSchemaDigest,
		"source_schema_digest":   binding.SourceSchema.SHA256,
	} {
		if !sha256ReferencePattern.MatchString(digest) {
			return Descriptor{}, contractError("invalid_digest", "configure", "bind_descriptor", name+" is invalid", nil)
		}
	}
	envelopeSpec, err := NewProtocolString("envelope_spec_version", binding.EnvelopeSpecVersion)
	if err != nil {
		return Descriptor{}, err
	}
	if envelopeSpec.String() != EventEnvelopeSpecVersion {
		return Descriptor{}, contractError("unsupported_envelope_spec", "configure", "bind_descriptor", "Envelope spec version is unsupported", nil)
	}
	product, err := NewProtocolString("source_product", binding.SourceProduct)
	if err != nil {
		return Descriptor{}, err
	}
	if !productPattern.MatchString(product.String()) || len(product.String()) > 64 {
		return Descriptor{}, contractError("invalid_source_product", "configure", "bind_descriptor", "Source product is invalid", nil)
	}
	connectorID, err := NewProtocolString("connector_id", binding.ConnectorID)
	if err != nil {
		return Descriptor{}, err
	}
	if !connectorIDPattern.MatchString(connectorID.String()) || len(connectorID.String()) > 160 {
		return Descriptor{}, contractError("invalid_connector_id", "configure", "bind_descriptor", "Connector ID is invalid", nil)
	}
	connectorVersion, err := NewProtocolString("connector_version", binding.ConnectorVersion)
	if err != nil {
		return Descriptor{}, err
	}
	if !semverPattern.MatchString(connectorVersion.String()) {
		return Descriptor{}, contractError("invalid_connector_version", "configure", "bind_descriptor", "Connector version is invalid", nil)
	}
	sourceSchemaID, err := NewProtocolString("source_schema.schema_id", binding.SourceSchema.SchemaID)
	if err != nil {
		return Descriptor{}, err
	}
	if len(sourceSchemaID.String()) > 200 {
		return Descriptor{}, contractError("invalid_source_schema", "configure", "bind_descriptor", "Source schema ID is too long", nil)
	}
	if len(binding.EmittedEvents) == 0 {
		return Descriptor{}, contractError("missing_event_schema", "configure", "bind_descriptor", "At least one emitted Event schema is required", nil)
	}
	events := make(map[string]EventSchemaBinding, len(binding.EmittedEvents))
	for _, candidate := range binding.EmittedEvents {
		eventType, eventErr := NewProtocolString("emitted_event.event_type", candidate.EventType)
		if eventErr != nil {
			return Descriptor{}, eventErr
		}
		schemaRef, schemaErr := NewProtocolString("emitted_event.schema_ref", candidate.SchemaRef)
		if schemaErr != nil {
			return Descriptor{}, schemaErr
		}
		schemaVersion, versionErr := NewProtocolString("emitted_event.schema_version", candidate.SchemaVersion)
		if versionErr != nil {
			return Descriptor{}, versionErr
		}
		if !eventTypePattern.MatchString(eventType.String()) ||
			len(eventType.String()) > 128 ||
			!eventSchemaRefPattern.MatchString(schemaRef.String()) ||
			len(schemaRef.String()) > 200 ||
			!versionPattern.MatchString(schemaVersion.String()) ||
			len(schemaVersion.String()) > 20 ||
			!sha256ReferencePattern.MatchString(candidate.SHA256) {
			return Descriptor{}, contractError("invalid_event_schema", "configure", "bind_descriptor", "Emitted Event schema binding is invalid", nil)
		}
		if _, duplicate := events[eventType.String()]; duplicate {
			return Descriptor{}, contractError("duplicate_event_schema", "configure", "bind_descriptor", "Emitted Event type is duplicated", nil)
		}
		events[eventType.String()] = candidate
	}
	clonedBinding := binding
	clonedBinding.EmittedEvents = append([]EventSchemaBinding(nil), binding.EmittedEvents...)
	sortedEvents := append([]EventSchemaBinding(nil), binding.EmittedEvents...)
	sort.Slice(sortedEvents, func(left, right int) bool {
		return sortedEvents[left].EventType < sortedEvents[right].EventType
	})
	keyParts := []string{
		binding.ProtocolVersion,
		binding.ProtocolCommit,
		binding.ReleaseDigest,
		binding.ManifestDigest,
		binding.EnvelopeSpecVersion,
		binding.EnvelopeSchemaDigest,
		binding.SourceProduct,
		binding.ConnectorID,
		binding.ConnectorVersion,
		binding.SourceSchema.SchemaID,
		binding.SourceSchema.SHA256,
	}
	for _, event := range sortedEvents {
		keyParts = append(keyParts, event.EventType, event.SchemaRef, event.SchemaVersion, event.SHA256)
	}
	keyInput := strings.Join(keyParts, "\x00")
	return Descriptor{
		binding: clonedBinding,
		key:     SHA256Reference([]byte(keyInput)),
		events:  events,
	}, nil
}

func (descriptor Descriptor) Binding() DescriptorBinding {
	cloned := descriptor.binding
	cloned.EmittedEvents = append([]EventSchemaBinding(nil), descriptor.binding.EmittedEvents...)
	return cloned
}

func (descriptor Descriptor) ConnectorID() string {
	return descriptor.binding.ConnectorID
}

func (descriptor Descriptor) ConnectorVersion() string {
	return descriptor.binding.ConnectorVersion
}

func (descriptor Descriptor) SourceProduct() string {
	return descriptor.binding.SourceProduct
}

type StrictJSONOptions struct {
	RejectNull bool
}

type SourceArtifact struct {
	descriptorKey    string
	schemaID         string
	schemaDigest     string
	revision         string
	producerInstance string
	sourceBytes      []byte
	sourceHash       string
}

func NewSourceArtifact(
	descriptor Descriptor,
	producerInstance string,
	revision string,
	sourceBytes []byte,
	options StrictJSONOptions,
) (SourceArtifact, *ConnectorError) {
	if descriptor.key == "" {
		return SourceArtifact{}, contractError("descriptor_missing", "configure", "bind_source_artifact", "Pinned descriptor is missing", nil)
	}
	instance, err := NewOpaqueString("producer.instance", producerInstance, 1, 128, false)
	if err != nil {
		return SourceArtifact{}, err
	}
	sourceRevision, err := NewOpaqueString("source_revision", revision, 1, 64, true)
	if err != nil {
		return SourceArtifact{}, err
	}
	if strictErr := ValidateStrictJSON(sourceBytes, options); strictErr != nil {
		return SourceArtifact{}, strictErr
	}
	cloned := append([]byte(nil), sourceBytes...)
	return SourceArtifact{
		descriptorKey:    descriptor.key,
		schemaID:         descriptor.binding.SourceSchema.SchemaID,
		schemaDigest:     descriptor.binding.SourceSchema.SHA256,
		revision:         sourceRevision.String(),
		producerInstance: instance.String(),
		sourceBytes:      cloned,
		sourceHash:       HashSourceBytes(cloned),
	}, nil
}

func (artifact SourceArtifact) Bytes() []byte {
	return append([]byte(nil), artifact.sourceBytes...)
}

func (artifact SourceArtifact) SourceHash() string {
	return artifact.sourceHash
}

func (artifact SourceArtifact) Revision() string {
	return artifact.revision
}

func (artifact SourceArtifact) ProducerInstance() string {
	return artifact.producerInstance
}

type EventScope struct {
	TenantRef  *OpaqueString
	OrgRef     string
	ProjectRef string
}

type EventSubject struct {
	Type ProtocolString
	Ref  OpaqueString
}

type EventDraft struct {
	EventID        OpaqueString
	EventType      ProtocolString
	SchemaRef      ProtocolString
	SchemaVersion  ProtocolString
	Scope          EventScope
	Subject        EventSubject
	Actor          map[string]any
	Time           map[string]any
	Correlation    map[string]any
	SourceRevision OpaqueString
	DataPolicy     map[string]any
	Provenance     map[string]any
	Payload        map[string]any
}

type SourceAdapter interface {
	Descriptor() Descriptor
	Project(SourceArtifact) (EventDraft, *ConnectorError)
}

type IdentityComponents struct {
	ProducerProduct  string
	ProducerInstance string
	TenantRef        *string
	OrgRef           string
	ProjectRef       string
	EventType        string
	EventID          string
}

func BuildIdentityInput(identity IdentityComponents) ([]byte, *ConnectorError) {
	product, err := NewProtocolString("producer.product", identity.ProducerProduct)
	if err != nil {
		return nil, err
	}
	eventType, err := NewProtocolString("event_type", identity.EventType)
	if err != nil {
		return nil, err
	}
	if !productPattern.MatchString(product.String()) ||
		len(product.String()) > 64 ||
		!eventTypePattern.MatchString(eventType.String()) ||
		len(eventType.String()) > 128 {
		return nil, contractError("invalid_identity_component", "validate", "build_idempotency", "Protocol identity component is invalid", nil)
	}
	if _, err := NewOpaqueString("producer.instance", identity.ProducerInstance, 1, 128, false); err != nil {
		return nil, err
	}
	if _, err := NewOpaqueString("event_id", identity.EventID, 1, 128, true); err != nil {
		return nil, err
	}
	if !isCanonicalPositiveDecimal(identity.OrgRef) || !isCanonicalPositiveDecimal(identity.ProjectRef) {
		return nil, contractError("invalid_scope", "validate", "build_idempotency", "Organization and project references must be canonical positive decimals", nil)
	}
	if identity.TenantRef != nil {
		if _, err := NewOpaqueString("scope.tenant_ref", *identity.TenantRef, 1, 128, true); err != nil {
			return nil, err
		}
	}
	input := make([]byte, 0, len(IdempotencyDomain)+128)
	input = append(input, IdempotencyDomain...)
	for _, value := range []string{identity.ProducerProduct, identity.ProducerInstance} {
		input, err = appendLPUTF8(input, value)
		if err != nil {
			return nil, err
		}
	}
	if identity.TenantRef == nil {
		input = append(input, 0, 0, 0, 0)
	} else {
		input, err = appendLPUTF8(input, *identity.TenantRef)
		if err != nil {
			return nil, err
		}
	}
	for _, value := range []string{identity.OrgRef, identity.ProjectRef, identity.EventType, identity.EventID} {
		input, err = appendLPUTF8(input, value)
		if err != nil {
			return nil, err
		}
	}
	return input, nil
}

func appendLPUTF8(destination []byte, value string) ([]byte, *ConnectorError) {
	if !utf8.ValidString(value) {
		return nil, contractError("invalid_utf8", "validate", "build_idempotency", "Idempotency component is not valid UTF-8", nil)
	}
	if uint64(len(value)) > math.MaxUint32 {
		return nil, contractError("identity_component_too_large", "validate", "build_idempotency", "Idempotency component exceeds uint32 bytes", nil)
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	destination = append(destination, length[:]...)
	destination = append(destination, value...)
	return destination, nil
}

type ImmutableEvent struct {
	hashInput        map[string]any
	hashInputBytes   []byte
	envelope         map[string]any
	envelopeBytes    []byte
	envelopeHash     string
	idempotencyKey   string
	sourceHash       string
	eventID          string
	eventType        string
	connectorID      string
	connectorVersion string
	producerInstance string
}

func (event ImmutableEvent) HashInput() map[string]any {
	return cloneJSONObject(event.hashInput)
}

func (event ImmutableEvent) HashInputBytes() []byte {
	return append([]byte(nil), event.hashInputBytes...)
}

func (event ImmutableEvent) Envelope() map[string]any {
	return cloneJSONObject(event.envelope)
}

func (event ImmutableEvent) EnvelopeBytes() []byte {
	return append([]byte(nil), event.envelopeBytes...)
}

func (event ImmutableEvent) MarshalJSON() ([]byte, error) {
	if len(event.envelopeBytes) == 0 {
		return nil, errors.New("immutable Event is not finalized")
	}
	return event.EnvelopeBytes(), nil
}

func (event ImmutableEvent) EnvelopeHash() string {
	return event.envelopeHash
}

func (event ImmutableEvent) IdempotencyKey() string {
	return event.idempotencyKey
}

func (event ImmutableEvent) SourceHash() string {
	return event.sourceHash
}

func (event ImmutableEvent) EventID() string {
	return event.eventID
}

func (event ImmutableEvent) EventType() string {
	return event.eventType
}

func (event ImmutableEvent) ConnectorID() string {
	return event.connectorID
}

func (event ImmutableEvent) ConnectorVersion() string {
	return event.connectorVersion
}

func (event ImmutableEvent) ProducerInstance() string {
	return event.producerInstance
}

func ProjectAndFinalize(adapter SourceAdapter, artifact SourceArtifact) (ImmutableEvent, *ConnectorError) {
	if adapter == nil {
		return ImmutableEvent{}, contractError("adapter_missing", "normalize", "project_source", "Source Adapter is missing", nil)
	}
	descriptor := adapter.Descriptor()
	if descriptor.key == "" || artifact.descriptorKey != descriptor.key {
		return ImmutableEvent{}, contractError("descriptor_binding_mismatch", "normalize", "project_source", "Source Artifact descriptor binding does not match the Adapter", nil)
	}
	draft, err := adapter.Project(artifact)
	if err != nil {
		return ImmutableEvent{}, err
	}
	return Finalize(descriptor, artifact, draft)
}

func Finalize(
	descriptor Descriptor,
	artifact SourceArtifact,
	draft EventDraft,
) (ImmutableEvent, *ConnectorError) {
	if descriptor.key == "" || artifact.descriptorKey != descriptor.key {
		return ImmutableEvent{}, contractError("descriptor_binding_mismatch", "normalize", "finalize_event", "Source Artifact descriptor binding does not match", nil)
	}
	eventBinding, supported := descriptor.events[draft.EventType.String()]
	if !supported ||
		eventBinding.SchemaRef != draft.SchemaRef.String() ||
		eventBinding.SchemaVersion != draft.SchemaVersion.String() {
		return ImmutableEvent{}, contractError("event_schema_not_pinned", "normalize", "finalize_event", "Event schema is not allowed by the pinned descriptor", nil)
	}
	if draft.SourceRevision.String() != artifact.revision {
		return ImmutableEvent{}, contractError("source_revision_mismatch", "normalize", "finalize_event", "Event source revision differs from the immutable Source Artifact", nil)
	}
	if !isCanonicalPositiveDecimal(draft.Scope.OrgRef) || !isCanonicalPositiveDecimal(draft.Scope.ProjectRef) {
		return ImmutableEvent{}, contractError("invalid_scope", "normalize", "finalize_event", "Event scope is invalid", nil)
	}
	if draft.EventID.String() == "" || draft.Subject.Type.String() == "" || draft.Subject.Ref.String() == "" {
		return ImmutableEvent{}, contractError("invalid_event_header", "normalize", "finalize_event", "Event header is incomplete", nil)
	}
	if _, eventIDErr := NewOpaqueString("event_id", draft.EventID.String(), 1, 128, true); eventIDErr != nil {
		return ImmutableEvent{}, eventIDErr
	}
	if _, subjectRefErr := NewOpaqueString("subject.ref", draft.Subject.Ref.String(), 1, 128, true); subjectRefErr != nil {
		return ImmutableEvent{}, subjectRefErr
	}
	if !subjectTypePattern.MatchString(draft.Subject.Type.String()) || len(draft.Subject.Type.String()) > 64 {
		return ImmutableEvent{}, contractError("invalid_subject", "normalize", "finalize_event", "Event subject.type is invalid", nil)
	}
	if sectionErr := validateCommonSections(draft); sectionErr != nil {
		return ImmutableEvent{}, sectionErr
	}
	var tenantRef *string
	if draft.Scope.TenantRef != nil {
		value := draft.Scope.TenantRef.String()
		tenantRef = &value
	}
	identityInput, identityErr := BuildIdentityInput(IdentityComponents{
		ProducerProduct:  descriptor.binding.SourceProduct,
		ProducerInstance: artifact.producerInstance,
		TenantRef:        tenantRef,
		OrgRef:           draft.Scope.OrgRef,
		ProjectRef:       draft.Scope.ProjectRef,
		EventType:        draft.EventType.String(),
		EventID:          draft.EventID.String(),
	})
	if identityErr != nil {
		return ImmutableEvent{}, identityErr
	}
	idempotencyKey := SHA256Reference(identityInput)
	scope := map[string]any{
		"org_ref":     draft.Scope.OrgRef,
		"project_ref": draft.Scope.ProjectRef,
	}
	if tenantRef != nil {
		scope["tenant_ref"] = *tenantRef
	}
	hashInput := map[string]any{
		"spec_version":   EventEnvelopeSpecVersion,
		"event_id":       draft.EventID.String(),
		"event_type":     draft.EventType.String(),
		"schema_ref":     draft.SchemaRef.String(),
		"schema_version": draft.SchemaVersion.String(),
		"producer": map[string]any{
			"product":           descriptor.binding.SourceProduct,
			"instance":          artifact.producerInstance,
			"connector_id":      descriptor.binding.ConnectorID,
			"connector_version": descriptor.binding.ConnectorVersion,
		},
		"scope": scope,
		"subject": map[string]any{
			"type": draft.Subject.Type.String(),
			"ref":  draft.Subject.Ref.String(),
		},
		"actor":       cloneJSONObject(draft.Actor),
		"time":        cloneJSONObject(draft.Time),
		"correlation": cloneJSONObject(draft.Correlation),
		"identity": map[string]any{
			"idempotency_key": idempotencyKey,
			"source_revision": draft.SourceRevision.String(),
		},
		"integrity": map[string]any{
			"source_hash": artifact.sourceHash,
		},
		"data_policy": cloneJSONObject(draft.DataPolicy),
		"provenance":  cloneJSONObject(draft.Provenance),
		"payload":     cloneJSONObject(draft.Payload),
	}
	if path, found := findProhibitedDeliveryKey(hashInput, ""); found {
		return ImmutableEvent{}, contractError(
			"delivery_state_in_event",
			"normalize",
			"finalize_event",
			"Delivery state is not allowed in Event content at "+path,
			nil,
		)
	}
	hashInputBytes, canonicalErr := CanonicalJSON(hashInput)
	if canonicalErr != nil {
		return ImmutableEvent{}, canonicalErr
	}
	envelopeHash := HashCanonicalEnvelopeBytes(hashInputBytes)
	envelope := cloneJSONObject(hashInput)
	envelope["integrity"].(map[string]any)["envelope_hash"] = envelopeHash
	envelopeBytes, canonicalErr := CanonicalJSON(envelope)
	if canonicalErr != nil {
		return ImmutableEvent{}, canonicalErr
	}
	return ImmutableEvent{
		hashInput:        cloneJSONObject(hashInput),
		hashInputBytes:   append([]byte(nil), hashInputBytes...),
		envelope:         cloneJSONObject(envelope),
		envelopeBytes:    append([]byte(nil), envelopeBytes...),
		envelopeHash:     envelopeHash,
		idempotencyKey:   idempotencyKey,
		sourceHash:       artifact.sourceHash,
		eventID:          draft.EventID.String(),
		eventType:        draft.EventType.String(),
		connectorID:      descriptor.binding.ConnectorID,
		connectorVersion: descriptor.binding.ConnectorVersion,
		producerInstance: artifact.producerInstance,
	}, nil
}

func validateCommonSections(draft EventDraft) *ConnectorError {
	if draft.Actor == nil || draft.Time == nil || draft.Correlation == nil ||
		draft.DataPolicy == nil || draft.Provenance == nil || draft.Payload == nil {
		return contractError("missing_event_section", "normalize", "finalize_event", "Event section is missing", nil)
	}
	actorType, ok := draft.Actor["type"].(string)
	if !ok {
		return contractError("invalid_actor", "normalize", "finalize_event", "Event actor.type is invalid", nil)
	}
	actor, err := NewProtocolString("actor.type", actorType)
	if err != nil {
		return err
	}
	switch actor.String() {
	case "system", "service", "user", "unknown":
	default:
		return contractError("invalid_actor", "normalize", "finalize_event", "Event actor.type is unsupported", nil)
	}
	for key, value := range draft.Actor {
		switch key {
		case "type":
		case "ref", "on_behalf_of":
			text, ok := value.(string)
			if !ok {
				return contractError("invalid_actor", "normalize", "finalize_event", "Event actor reference is invalid", nil)
			}
			if _, opaqueErr := NewOpaqueString("actor."+key, text, 1, 128, true); opaqueErr != nil {
				return opaqueErr
			}
		default:
			return contractError("invalid_actor", "normalize", "finalize_event", "Event actor contains an unsupported field", nil)
		}
	}
	if len(draft.Time) == 0 {
		return contractError("invalid_event_time", "normalize", "finalize_event", "Event time requires at least one instant", nil)
	}
	for key, value := range draft.Time {
		switch key {
		case "occurred_at", "observed_at", "terminal_at":
		default:
			return contractError("invalid_event_time", "normalize", "finalize_event", "Event time contains an unsupported field", nil)
		}
		text, ok := value.(string)
		if !ok || ValidateCanonicalMicrosecondTime(text) != nil {
			return contractError("invalid_event_time", "normalize", "finalize_event", "Event time is not canonical UTC microsecond time", nil)
		}
	}
	for key, value := range draft.Correlation {
		switch key {
		case "request_ref", "trace_ref", "span_ref", "causation_event_id":
		default:
			return contractError("invalid_correlation", "normalize", "finalize_event", "Event correlation contains an unsupported field", nil)
		}
		text, ok := value.(string)
		if !ok {
			return contractError("invalid_correlation", "normalize", "finalize_event", "Event correlation reference is invalid", nil)
		}
		if _, opaqueErr := NewOpaqueString("correlation."+key, text, 1, 128, true); opaqueErr != nil {
			return opaqueErr
		}
	}
	captureMode, ok := draft.DataPolicy["payload_capture_mode"].(string)
	if !ok {
		return contractError("invalid_data_policy", "normalize", "finalize_event", "Event payload_capture_mode is missing", nil)
	}
	if _, protocolErr := NewProtocolString("data_policy.payload_capture_mode", captureMode); protocolErr != nil {
		return protocolErr
	}
	switch captureMode {
	case "none", "metadata_only", "redacted", "full":
	default:
		return contractError("invalid_data_policy", "normalize", "finalize_event", "Event payload_capture_mode is unsupported", nil)
	}
	if _, ok := draft.DataPolicy["raw_payload_present"].(bool); !ok {
		return contractError("invalid_data_policy", "normalize", "finalize_event", "Event raw_payload_present is missing", nil)
	}
	for key, value := range draft.DataPolicy {
		switch key {
		case "payload_capture_mode", "raw_payload_present":
		case "redaction_profile":
			text, ok := value.(string)
			if !ok {
				return contractError("invalid_data_policy", "normalize", "finalize_event", "Event redaction_profile is invalid", nil)
			}
			if _, opaqueErr := NewOpaqueString("data_policy.redaction_profile", text, 1, 128, true); opaqueErr != nil {
				return opaqueErr
			}
		default:
			return contractError("invalid_data_policy", "normalize", "finalize_event", "Event data_policy contains an unsupported field", nil)
		}
	}
	fidelity, ok := draft.Provenance["adapter_fidelity"].(string)
	if !ok {
		return contractError("invalid_provenance", "normalize", "finalize_event", "Event adapter_fidelity is missing", nil)
	}
	if _, protocolErr := NewProtocolString("provenance.adapter_fidelity", fidelity); protocolErr != nil {
		return protocolErr
	}
	switch fidelity {
	case "F0", "F1", "F2", "F3":
	default:
		return contractError("invalid_provenance", "normalize", "finalize_event", "Event adapter_fidelity is unsupported", nil)
	}
	fieldGroups, ok := draft.Provenance["field_groups"].(map[string]any)
	if !ok || len(fieldGroups) == 0 {
		return contractError("invalid_provenance", "normalize", "finalize_event", "Event field_groups are missing", nil)
	}
	for name, rawValue := range fieldGroups {
		value, ok := rawValue.(string)
		if !ok || !fieldGroupNamePattern.MatchString(name) {
			return contractError("invalid_provenance", "normalize", "finalize_event", "Event field group is invalid", nil)
		}
		if _, protocolErr := NewProtocolString("provenance.field_groups", value); protocolErr != nil {
			return protocolErr
		}
		switch value {
		case "direct", "derived", "declared", "suppressed", "gap", "not_applicable", "unknown":
		default:
			return contractError("invalid_provenance", "normalize", "finalize_event", "Event field provenance is unsupported", nil)
		}
	}
	return nil
}

func ValidateCanonicalMicrosecondTime(value string) error {
	if !microsecondTimePattern.MatchString(value) {
		return errors.New("time is not canonical UTC microsecond text")
	}
	parsed, err := time.Parse("2006-01-02T15:04:05.000000Z", value)
	if err != nil || parsed.UTC().Format("2006-01-02T15:04:05.000000Z") != value {
		return errors.New("time is not a valid canonical UTC microsecond instant")
	}
	return nil
}

func FormatMicrosecondTime(value time.Time) (string, *ConnectorError) {
	utc := value.UTC()
	if utc.Nanosecond()%1_000 != 0 {
		return "", contractError(
			"timestamp_precision_loss",
			"normalize",
			"normalize_timestamp",
			"Timestamp cannot be represented losslessly at microsecond precision",
			nil,
		)
	}
	return utc.Format("2006-01-02T15:04:05.000000Z"), nil
}

func NormalizeSourceTime(value string) (string, *ConnectorError) {
	if !utf8.ValidString(value) {
		return "", contractError("invalid_timestamp", "normalize", "normalize_timestamp", "Timestamp is not valid UTF-8", nil)
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", contractError("invalid_timestamp", "normalize", "normalize_timestamp", "Timestamp is not a valid zoned instant", err)
	}
	return FormatMicrosecondTime(parsed)
}

func ValidateInt64Decimal(value string) error {
	if !int64DecimalPattern.MatchString(value) {
		return errors.New("value is not a canonical nonnegative decimal")
	}
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		return errors.New("value is outside the int64 range")
	}
	return nil
}

func isCanonicalPositiveDecimal(value string) bool {
	return len(value) <= 20 && positiveDecimalPattern.MatchString(value)
}

func SHA256Reference(input []byte) string {
	digest := sha256.Sum256(input)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func HashSourceBytes(input []byte) string {
	return SHA256Reference(input)
}

func HashCanonicalEnvelopeBytes(input []byte) string {
	return SHA256Reference(input)
}

func ValidateStrictJSON(payload []byte, options StrictJSONOptions) *ConnectorError {
	if len(payload) == 0 {
		return contractError("empty_source", "decode", "decode_source_json", "Source JSON is empty", nil)
	}
	if !utf8.Valid(payload) {
		return contractError("invalid_utf8", "decode", "decode_source_json", "Source bytes must be valid UTF-8", nil)
	}
	if err := rejectInvalidUnicodeEscapes(payload); err != nil {
		return contractError("invalid_unicode_escape", "decode", "decode_source_json", "Source JSON contains an invalid Unicode escape", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := walkStrictJSONValue(decoder, options); err != nil {
		code := "invalid_source_json"
		if errors.Is(err, errDuplicateJSONKey) {
			code = "duplicate_json_key"
		}
		return contractError(code, "decode", "decode_source_json", "Source JSON violates strict parsing rules", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("trailing JSON content")
		}
		return contractError("invalid_source_json", "decode", "decode_source_json", "Source JSON contains trailing content", err)
	}
	return nil
}

var errDuplicateJSONKey = errors.New("duplicate JSON key")

func walkStrictJSONValue(decoder *json.Decoder, options StrictJSONOptions) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		if options.RejectNull {
			return errors.New("null is not allowed")
		}
		return nil
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("%w %q", errDuplicateJSONKey, key)
			}
			seen[key] = struct{}{}
			if err := walkStrictJSONValue(decoder, options); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("object is not closed")
		}
	case '[':
		for decoder.More() {
			if err := walkStrictJSONValue(decoder, options); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("array is not closed")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}

func rejectInvalidUnicodeEscapes(payload []byte) error {
	inString := false
	for index := 0; index < len(payload); index++ {
		switch {
		case !inString:
			if payload[index] == '"' {
				inString = true
			}
		case payload[index] == '"':
			inString = false
		case payload[index] == '\\':
			if index+1 >= len(payload) {
				return errors.New("unterminated escape")
			}
			if payload[index+1] != 'u' {
				index++
				continue
			}
			first, err := decodeHex16(payload[index+2:])
			if err != nil {
				return err
			}
			switch {
			case first >= 0xd800 && first <= 0xdbff:
				if index+12 > len(payload) || payload[index+6] != '\\' || payload[index+7] != 'u' {
					return errors.New("unpaired high surrogate")
				}
				second, err := decodeHex16(payload[index+8:])
				if err != nil {
					return err
				}
				if second < 0xdc00 || second > 0xdfff {
					return errors.New("high surrogate is not followed by a low surrogate")
				}
				index += 11
			case first >= 0xdc00 && first <= 0xdfff:
				return errors.New("unpaired low surrogate")
			default:
				index += 5
			}
		}
	}
	return nil
}

func decodeHex16(input []byte) (uint16, error) {
	if len(input) < 4 {
		return 0, errors.New("short Unicode escape")
	}
	var decoded [2]byte
	if _, err := hex.Decode(decoded[:], input[:4]); err != nil {
		return 0, err
	}
	return uint16(decoded[0])<<8 | uint16(decoded[1]), nil
}

func CanonicalJSON(value any) ([]byte, *ConnectorError) {
	var output bytes.Buffer
	if err := appendCanonicalJSON(&output, value); err != nil {
		return nil, contractError("canonicalization_failed", "normalize", "canonicalize_event", "Event cannot be canonicalized", err)
	}
	return output.Bytes(), nil
}

func appendCanonicalJSON(output *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case bool:
		if typed {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
	case string:
		if !utf8.ValidString(typed) {
			return errors.New("canonical string is not valid UTF-8")
		}
		appendCanonicalString(output, typed)
	case int:
		if int64(typed) < -maxSafeJSONInteger || int64(typed) > maxSafeJSONInteger {
			return errors.New("integer exceeds the safe-integer profile")
		}
		output.WriteString(strconv.Itoa(typed))
	case int64:
		if typed < -maxSafeJSONInteger || typed > maxSafeJSONInteger {
			return errors.New("integer exceeds the safe-integer profile")
		}
		output.WriteString(strconv.FormatInt(typed, 10))
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			if !utf8.ValidString(key) {
				return errors.New("canonical object key is not valid UTF-8")
			}
			keys = append(keys, key)
		}
		sort.Slice(keys, func(left, right int) bool {
			return compareUTF16(keys[left], keys[right]) < 0
		})
		output.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				output.WriteByte(',')
			}
			appendCanonicalString(output, key)
			output.WriteByte(':')
			if err := appendCanonicalJSON(output, typed[key]); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	case []any:
		output.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := appendCanonicalJSON(output, item); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	default:
		return fmt.Errorf("unsupported canonical JSON type %T", value)
	}
	return nil
}

func appendCanonicalString(output *bytes.Buffer, value string) {
	output.WriteByte('"')
	for _, character := range value {
		switch character {
		case '"', '\\':
			output.WriteByte('\\')
			output.WriteRune(character)
		case '\b':
			output.WriteString(`\b`)
		case '\t':
			output.WriteString(`\t`)
		case '\n':
			output.WriteString(`\n`)
		case '\f':
			output.WriteString(`\f`)
		case '\r':
			output.WriteString(`\r`)
		default:
			if character < 0x20 {
				fmt.Fprintf(output, `\u%04x`, character)
			} else {
				output.WriteRune(character)
			}
		}
	}
	output.WriteByte('"')
}

func compareUTF16(left, right string) int {
	leftUnits := utf16.Encode([]rune(left))
	rightUnits := utf16.Encode([]rune(right))
	limit := len(leftUnits)
	if len(rightUnits) < limit {
		limit = len(rightUnits)
	}
	for index := 0; index < limit; index++ {
		switch {
		case leftUnits[index] < rightUnits[index]:
			return -1
		case leftUnits[index] > rightUnits[index]:
			return 1
		}
	}
	switch {
	case len(leftUnits) < len(rightUnits):
		return -1
	case len(leftUnits) > len(rightUnits):
		return 1
	default:
		return 0
	}
}

func cloneJSONObject(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = cloneJSONValue(value)
	}
	return cloned
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneJSONObject(typed)
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneJSONValue(item)
		}
		return cloned
	default:
		return typed
	}
}

func findProhibitedDeliveryKey(value any, prefix string) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(left, right int) bool {
			return compareUTF16(keys[left], keys[right]) < 0
		})
		for _, key := range keys {
			child := typed[key]
			path := prefix + "/" + key
			if _, prohibited := prohibitedDeliveryKeys[key]; prohibited {
				return path, true
			}
			if nestedPath, found := findProhibitedDeliveryKey(child, path); found {
				return nestedPath, true
			}
		}
	case []any:
		for index, item := range typed {
			path := fmt.Sprintf("%s/%d", prefix, index)
			if nestedPath, found := findProhibitedDeliveryKey(item, path); found {
				return nestedPath, true
			}
		}
	}
	return "", false
}
