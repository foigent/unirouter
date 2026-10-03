package xinsightconnector

import (
	"encoding/json"
	"net/netip"
	"regexp"
	"strconv"
)

const SourceDescriptorSchemaV1 = "unirouter.xinsight.connector-source-descriptor.v1"

// SourceDescriptor is an identity readback after the original v2 lifecycle.
// It does not attest machine activation, grants, adoption or feed consumption.
type SourceDescriptor struct {
	SchemaVersion         string          `json:"schema_version"`
	SourceBinding         SourceBinding   `json:"source_binding"`
	SourceBindingSHA256   string          `json:"source_binding_sha256"`
	ReceiverNetwork       ReceiverNetwork `json:"receiver_network"`
	ReceiverNetworkSHA256 string          `json:"receiver_network_sha256"`
	ResumeOperationID     string          `json:"resume_operation_id"`
	ResumeResultSHA256    string          `json:"resume_result_sha256"`
}

type ReceiverNetwork struct {
	FeedKeyID               string `json:"feed_key_id"`
	KeyPrefix               string `json:"key_prefix"`
	RegistrationOperationID string `json:"registration_operation_id"`
	VerifierScheme          string `json:"verifier_scheme"`
	AllowedIP               string `json:"allowed_ip"`
}

func ValidateSourceDescriptor(d SourceDescriptor) error {
	b, n := d.SourceBinding, d.ReceiverNetwork
	id, e := strconv.ParseInt(n.FeedKeyID, 10, 64)
	p, pe := netip.ParsePrefix(n.AllowedIP)
	if d.SchemaVersion != SourceDescriptorSchemaV1 || b.SchemaVersion != SourceBindingSchemaVersion ||
		!validExactExternalIdentity(b.InstallationID) || !validExactDeploymentIdentity(b.XDeploymentID) ||
		b.OrgID <= 0 || b.ProjectID <= 0 || !validSourceInstance(b.SourceInstance) ||
		b.EventScope != EventScopeModelCompleted || b.FeedSchema != CompletedFeedSchemaVersion ||
		!hashPattern.MatchString(d.SourceBindingSHA256) || PublicCanonicalSHA256(b) != d.SourceBindingSHA256 ||
		e != nil || id <= 0 || strconv.FormatInt(id, 10) != n.FeedKeyID ||
		!regexp.MustCompile(`^ur-xf1-[a-f0-9]{13}$`).MatchString(n.KeyPrefix) ||
		!controlFeedOperationID.MatchString(n.RegistrationOperationID) || n.VerifierScheme != "receiver_sha256_v1" ||
		pe != nil || p.String() != n.AllowedIP || p.Bits() != p.Addr().BitLen() || p.Addr().Zone() != "" ||
		!hashPattern.MatchString(d.ReceiverNetworkSHA256) || PublicCanonicalSHA256(n) != d.ReceiverNetworkSHA256 ||
		!remoteOperationID.MatchString(d.ResumeOperationID) || !hashPattern.MatchString(d.ResumeResultSHA256) {
		return ErrInvalid
	}
	return nil
}

func DecodeSourceDescriptor(raw []byte) (SourceDescriptor, error) {
	var d SourceDescriptor
	if len(raw) > 16<<10 || decodeClosedV3(raw, &d) != nil || ValidateSourceDescriptor(d) != nil {
		return d, ErrInvalid
	}
	return d, nil
}

func (d SourceDescriptor) CanonicalJSON() ([]byte, error) {
	if e := ValidateSourceDescriptor(d); e != nil {
		return nil, e
	}
	return json.Marshal(d)
}
