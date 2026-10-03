package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestXInsightStaticPeerRejectsIdentityNetworkAndDigestDrift(t *testing.T) {
	digest, err := XInsightManagedFeedNetworkDigest("https://unirouter.internal.example")
	require.NoError(t, err)
	require.Equal(t, "sha256:eae65fba466700918992bcb11c555b6a00ab9f83b2919144e69223f65abb1208", digest)
	peer := XInsightConnectorStaticPeer{XInsightOrigin: "https://xinsight.internal.example",
		XDeploymentID: "xid_ZZZZZZZZZZZZZZZZZZZZZZ", UROrigin: "https://unirouter.internal.example",
		AllowedIP: "203.0.113.7/32", NetworkBindingSHA256: digest}
	require.NoError(t, peer.Validate())
	wire, err := json.Marshal(peer)
	require.NoError(t, err)
	decoded, err := parseXInsightConnectorStaticPeer(string(wire))
	require.NoError(t, err)
	require.Equal(t, peer, *decoded)
	for _, raw := range []string{"null", "[]", " " + string(wire),
		strings.Replace(string(wire), `"allowed_ip"`, `"Allowed_IP"`, 1),
		strings.Replace(string(wire), `"allowed_ip":"203.0.113.7/32"`, `"allowed_ip":"203.0.113.7/32","allowed_ip":"203.0.113.7/32"`, 1)} {
		_, err := parseXInsightConnectorStaticPeer(raw)
		require.Error(t, err)
	}
	for _, mutate := range []func(*XInsightConnectorStaticPeer){
		func(p *XInsightConnectorStaticPeer) { p.XInsightOrigin = "http://xinsight.internal.example" },
		func(p *XInsightConnectorStaticPeer) { p.XDeploymentID = "other" },
		func(p *XInsightConnectorStaticPeer) { p.UROrigin += "/" },
		func(p *XInsightConnectorStaticPeer) { p.NetworkBindingSHA256 = "sha256:" + strings.Repeat("a", 64) },
		func(p *XInsightConnectorStaticPeer) { p.AllowedIP = "203.0.113.0/24" },
		func(p *XInsightConnectorStaticPeer) { p.AllowedIP = "0.0.0.0/32" },
		func(p *XInsightConnectorStaticPeer) { p.AllowedIP = "224.0.0.1/32" },
		func(p *XInsightConnectorStaticPeer) { p.AllowedIP = "::ffff:203.0.113.7/128" },
	} {
		copy := peer
		mutate(&copy)
		require.Error(t, copy.Validate())
	}
	t.Setenv("XINSIGHT_CONNECTOR_STATIC_PEER_JSON", string(wire))
	t.Setenv("XINSIGHT_CONNECTOR_CONTROL_ORIGIN", peer.XInsightOrigin)
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("API_KEY_HASH_SECRET", "api-key-secret")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, peer, *cfg.XInsightConnectorStaticPeer)
	cfg.XInsightConnectorControlOrigin = "https://other.example"
	require.Error(t, cfg.validateXInsightConnector())
}
