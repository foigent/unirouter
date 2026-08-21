// Package deploymentprofile defines product-local desired-state records for
// UniRouter installations and Gateway endpoints. It deliberately excludes TLS
// private keys and never applies or restarts runtime infrastructure.
package deploymentprofile

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

const ContractVersion = "unirouter.deployment-profile.v1"

const (
	KindECSDevelopment   = "ecs_development"
	KindOnPremCompose    = "onprem_compose"
	KindOnPremKubernetes = "onprem_kubernetes"

	VisibilityPublic   = "public"
	VisibilityInternal = "internal"

	TLSExternalTermination = "external_termination"
	TLSMountedCertificate  = "mounted_certificate"
	TLSPrivateCA           = "private_ca"
	TLSDisabled            = "disabled"

	EndpointOpenAIChat        = "endpoint.openai-chat.v1"
	EndpointOpenAIResponses   = "endpoint.openai-responses.contract.v1"
	EndpointAnthropicMessages = "endpoint.anthropic-messages.v1"

	EvidenceRepositoryRuntimeVerified = "repository_runtime_verified"
)

var (
	ErrInvalid = errors.New("deploymentprofile: invalid input")
	refPattern = regexp.MustCompile(`^(dep|gwe)_[a-z0-9][a-z0-9._-]{2,79}$`)
)

type ProfileDraft struct {
	ProfileRef   string `json:"profile_ref"`
	DisplayName  string `json:"display_name"`
	Kind         string `json:"kind"`
	Environment  string `json:"environment"`
	IngressMode  string `json:"ingress_mode"`
	DNSMode      string `json:"dns_mode"`
	OutboundMode string `json:"outbound_mode"`
	Notes        string `json:"notes"`
	Status       string `json:"status"`
}

type EndpointDraft struct {
	EndpointRef     string `json:"endpoint_ref"`
	DisplayName     string `json:"display_name"`
	BaseURL         string `json:"base_url"`
	Visibility      string `json:"visibility"`
	TLSMode         string `json:"tls_mode"`
	CertificateRef  string `json:"certificate_ref"`
	EndpointProfile string `json:"endpoint_profile"`
	Status          string `json:"status"`
}

type EndpointProfile struct {
	ProfileID     string   `json:"profile_id"`
	DisplayName   string   `json:"display_name"`
	Paths         []string `json:"paths"`
	RuntimeState  string   `json:"runtime_state"`
	EvidenceLevel string   `json:"evidence_level"`
	Streaming     bool     `json:"streaming"`
}

func EndpointProfiles() []EndpointProfile {
	return []EndpointProfile{
		{ProfileID: EndpointOpenAIChat, DisplayName: "OpenAI Chat Completions", Paths: []string{"/v1/chat/completions", "/v1/models"}, RuntimeState: "runtime_supported", EvidenceLevel: "verified", Streaming: true},
		{ProfileID: EndpointOpenAIResponses, DisplayName: "OpenAI Responses (strict non-stream text)", Paths: []string{"/v1/responses"}, RuntimeState: "runtime_supported", EvidenceLevel: EvidenceRepositoryRuntimeVerified, Streaming: false},
		{ProfileID: EndpointAnthropicMessages, DisplayName: "Anthropic Messages (strict non-stream text)", Paths: []string{"/v1/messages"}, RuntimeState: "runtime_supported", EvidenceLevel: EvidenceRepositoryRuntimeVerified, Streaming: false},
	}
}

func NormalizeProfile(input ProfileDraft) (ProfileDraft, error) {
	input.ProfileRef = strings.ToLower(strings.TrimSpace(input.ProfileRef))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	input.Environment = strings.ToLower(strings.TrimSpace(input.Environment))
	input.IngressMode = strings.ToLower(strings.TrimSpace(input.IngressMode))
	input.DNSMode = strings.ToLower(strings.TrimSpace(input.DNSMode))
	input.OutboundMode = strings.ToLower(strings.TrimSpace(input.OutboundMode))
	input.Notes = strings.TrimSpace(input.Notes)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = "active"
	}
	if !refPattern.MatchString(input.ProfileRef) || !strings.HasPrefix(input.ProfileRef, "dep_") || len(input.ProfileRef) > 64 {
		return ProfileDraft{}, fmt.Errorf("%w: profile_ref", ErrInvalid)
	}
	if input.DisplayName == "" || len(input.DisplayName) > 160 || len(input.Notes) > 2000 {
		return ProfileDraft{}, fmt.Errorf("%w: metadata", ErrInvalid)
	}
	if !slices.Contains([]string{KindECSDevelopment, KindOnPremCompose, KindOnPremKubernetes}, input.Kind) {
		return ProfileDraft{}, fmt.Errorf("%w: kind", ErrInvalid)
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`).MatchString(input.Environment) {
		return ProfileDraft{}, fmt.Errorf("%w: environment", ErrInvalid)
	}
	if !slices.Contains([]string{"external_load_balancer", "bundled_reverse_proxy", "cluster_ingress"}, input.IngressMode) ||
		!slices.Contains([]string{"public_dns", "private_dns", "ip_address", "customer_managed"}, input.DNSMode) ||
		!slices.Contains([]string{"direct", "proxy", "restricted", "air_gapped"}, input.OutboundMode) ||
		!slices.Contains([]string{"active", "disabled"}, input.Status) {
		return ProfileDraft{}, fmt.Errorf("%w: deployment mode", ErrInvalid)
	}
	return input, nil
}

func NormalizeEndpoint(input EndpointDraft) (EndpointDraft, error) {
	input.EndpointRef = strings.ToLower(strings.TrimSpace(input.EndpointRef))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	input.Visibility = strings.ToLower(strings.TrimSpace(input.Visibility))
	input.TLSMode = strings.ToLower(strings.TrimSpace(input.TLSMode))
	input.CertificateRef = strings.TrimSpace(input.CertificateRef)
	input.EndpointProfile = strings.ToLower(strings.TrimSpace(input.EndpointProfile))
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = "active"
	}
	if !refPattern.MatchString(input.EndpointRef) || !strings.HasPrefix(input.EndpointRef, "gwe_") || len(input.EndpointRef) > 64 {
		return EndpointDraft{}, fmt.Errorf("%w: endpoint_ref", ErrInvalid)
	}
	if input.DisplayName == "" || len(input.DisplayName) > 160 || len(input.CertificateRef) > 240 || strings.Contains(input.CertificateRef, "-----BEGIN") {
		return EndpointDraft{}, fmt.Errorf("%w: endpoint metadata", ErrInvalid)
	}
	parsed, err := url.ParseRequestURI(input.BaseURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return EndpointDraft{}, fmt.Errorf("%w: base_url", ErrInvalid)
	}
	if !slices.Contains([]string{"http", "https"}, parsed.Scheme) || !slices.Contains([]string{VisibilityPublic, VisibilityInternal}, input.Visibility) {
		return EndpointDraft{}, fmt.Errorf("%w: endpoint visibility", ErrInvalid)
	}
	if !slices.Contains([]string{TLSExternalTermination, TLSMountedCertificate, TLSPrivateCA, TLSDisabled}, input.TLSMode) {
		return EndpointDraft{}, fmt.Errorf("%w: tls_mode", ErrInvalid)
	}
	if input.Visibility == VisibilityPublic && parsed.Scheme != "https" {
		return EndpointDraft{}, fmt.Errorf("%w: public endpoint requires HTTPS", ErrInvalid)
	}
	if (parsed.Scheme == "https") == (input.TLSMode == TLSDisabled) {
		return EndpointDraft{}, fmt.Errorf("%w: scheme/tls mismatch", ErrInvalid)
	}
	foundProfile := false
	for _, profile := range EndpointProfiles() {
		if profile.ProfileID == input.EndpointProfile {
			foundProfile = true
			break
		}
	}
	if !foundProfile || !slices.Contains([]string{"active", "disabled"}, input.Status) {
		return EndpointDraft{}, fmt.Errorf("%w: endpoint profile", ErrInvalid)
	}
	return input, nil
}
