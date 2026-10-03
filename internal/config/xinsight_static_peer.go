package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/netip"
)

// XInsightConnectorStaticPeer is installation-tool-owned trust, not browser
// input. It admits future invitations from one already trusted deployment.
type XInsightConnectorStaticPeer struct {
	XInsightOrigin       string `json:"xinsight_origin"`
	XDeploymentID        string `json:"x_deployment_id"`
	UROrigin             string `json:"ur_origin"`
	AllowedIP            string `json:"allowed_ip"`
	NetworkBindingSHA256 string `json:"network_binding_sha256"`
}

// XInsightManagedFeedNetworkDigest consumes the original XI hostctl authority;
// field order and names match xinsight.managed-feed-network-binding.v1.
func XInsightManagedFeedNetworkDigest(urOrigin string) (string, error) {
	if err := validateXInsightConnectorControlOrigin(urOrigin); err != nil {
		return "", err
	}
	authority := struct {
		SchemaVersion string `json:"schema_version"`
		TrustedOrigin string `json:"trusted_origin"`
		Transport     string `json:"transport"`
		Route         string `json:"route"`
	}{"xinsight.managed-feed-network-binding.v1", urOrigin, "https", "/api/v1/integrations/xinsight/completed-executions"}
	wire, err := json.Marshal(authority)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(wire)), nil
}

func (peer XInsightConnectorStaticPeer) Validate() error {
	if err := validateXInsightConnectorControlOrigin(peer.XInsightOrigin); err != nil {
		return fmt.Errorf("invalid static peer origin: %w", err)
	}
	if !xInsightConnectorDeploymentPattern.MatchString(peer.XDeploymentID) {
		return fmt.Errorf("invalid static peer deployment identity")
	}
	ip, err := netip.ParsePrefix(peer.AllowedIP)
	if err != nil || ip.String() != peer.AllowedIP || ip.Bits() != ip.Addr().BitLen() ||
		ip.Addr().Is4In6() || ip.Addr().IsUnspecified() || ip.Addr().IsMulticast() {
		return fmt.Errorf("static peer requires one exact receiver IPv4 /32 or IPv6 /128")
	}
	digest, err := XInsightManagedFeedNetworkDigest(peer.UROrigin)
	if err != nil || digest != peer.NetworkBindingSHA256 {
		return fmt.Errorf("static peer network digest differs from the canonical UR origin binding")
	}
	return nil
}

func parseXInsightConnectorStaticPeer(raw string) (*XInsightConnectorStaticPeer, error) {
	if raw == "" {
		return nil, nil
	}
	var peer XInsightConnectorStaticPeer
	if err := json.Unmarshal([]byte(raw), &peer); err != nil {
		return nil, fmt.Errorf("invalid XINSIGHT_CONNECTOR_STATIC_PEER_JSON")
	}
	canonical, err := json.Marshal(peer)
	if err != nil || string(canonical) != raw {
		return nil, fmt.Errorf("XINSIGHT_CONNECTOR_STATIC_PEER_JSON must be the exact installation-tool-generated object")
	}
	if err := peer.Validate(); err != nil {
		return nil, err
	}
	return &peer, nil
}
