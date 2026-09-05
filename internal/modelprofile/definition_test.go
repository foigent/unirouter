package modelprofile

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDataDefinitionPreservesExistingSeriesAndDoesNotPublishItself(t *testing.T) {
	ids := []string{"deepseek.v4-flash.api", "deepseek.v4-pro.api", "kimi.k2.6.api", "kimi.k2.7-code.api", "kimi.k3.api",
		"minimax.m3.payg", "minimax.m2.7.payg", "minimax.m2.7-highspeed.payg", "glm.5.api", "glm.5-turbo.api", "glm.5.1.api", "glm.5.2.api", "glm.5.3.api", "glm.5v-turbo.api"}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			manifest, ok := currentProfileManifests[id]
			require.True(t, ok)
			original, ok := Lookup(id, manifest.Version)
			require.True(t, ok)
			before, err := NewDefinition(original, id, "")
			require.NoError(t, err)
			readback, err := VerifyDefinition(before.CanonicalBytes(), before.Digest())
			require.NoError(t, err)
			original.Current = false
			require.Equal(t, original, readback.Profile())
			require.NotContains(t, string(readback.CanonicalBytes()), `"current"`)
			next := original
			next.Version = "99.0.0"
			d, err := NewDefinition(next, id, before.Digest())
			require.NoError(t, err)
			require.NotEqual(t, before.Digest(), d.Digest())
			require.Equal(t, before.Digest(), d.PredecessorDigest())
			_, compiled := Lookup(next.ID, next.Version)
			require.False(t, compiled, "data decoding must not mutate the legacy registry")
			profileCopy := d.Profile()
			profileCopy.UpstreamModelIDs[0] = "mutated"
			bytesCopy := d.CanonicalBytes()
			bytesCopy[0] = '!'
			require.Equal(t, next, d.Profile())
			_, err = VerifyDefinition(d.CanonicalBytes(), d.Digest())
			require.NoError(t, err)
		})
	}
}

func TestDataDefinitionFailsClosedAtItsTrustBoundary(t *testing.T) {
	p, ok := Lookup("glm.5.3.api", "1.9.0")
	require.True(t, ok)
	d, err := NewDefinition(p, "glm/glm-5.3", "")
	require.NoError(t, err)
	for name, mutate := range map[string]func(Profile) Profile{
		"unknown parser":     func(p Profile) Profile { p.ProtocolProfile = "not-installed"; return p },
		"unknown reasoning":  func(p Profile) Profile { p.ReasoningProfile = "not-installed"; return p },
		"unknown usage":      func(p Profile) Profile { p.CacheUsageProfile = "not-installed"; return p },
		"unknown tokenizer":  func(p Profile) Profile { p.TokenizerProfileVersion = "99.0.0"; return p },
		"unknown capability": func(p Profile) Profile { p.Capabilities = append(p.Capabilities, "agent_ready"); return p },
		"unknown endpoint":   func(p Profile) Profile { p.EndpointClasses = []string{"unknown_endpoint"}; return p },
		"legacy escape":      func(p Profile) Profile { p.LegacyProtocolAuthority = true; return p },
		"limits":             func(p Profile) Profile { p.DefaultOutputLimit = p.ProviderContextWindow + 1; return p },
		"duplicate upstream": func(p Profile) Profile {
			p.UpstreamModelIDs = append(p.UpstreamModelIDs, p.UpstreamModelIDs[0])
			return p
		},
		"malformed version": func(p Profile) Profile { p.Version = "latest"; return p },
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewDefinition(mutate(clone(p)), "glm/glm-5.3", "")
			require.Error(t, err)
		})
	}
	for name, wire := range map[string][]byte{
		"unknown envelope":      []byte(strings.Replace(string(d.CanonicalBytes()), `"schema_version":`, `"extra":1,"schema_version":`, 1)),
		"duplicate nested key":  []byte(strings.Replace(string(d.CanonicalBytes()), `"provider":"glm"`, `"provider":"glm","provider":"other"`, 1)),
		"current even false":    []byte(strings.Replace(string(d.CanonicalBytes()), `"profile":{`, `"profile":{"current":false,`, 1)),
		"unknown profile field": []byte(strings.Replace(string(d.CanonicalBytes()), `"profile":{`, `"profile":{"code":"eval",`, 1)),
		"future schema":         []byte(strings.Replace(string(d.CanonicalBytes()), DefinitionSchemaV1, "unirouter.profile-definition.v999", 1)),
		"oversize":              make([]byte, MaximumDefinitionBytes+1),
		"case alias":            []byte(strings.Replace(string(d.CanonicalBytes()), `"provider":"glm"`, `"Provider":"glm"`, 1)),
		"semantic duplicate":    []byte(strings.Replace(string(d.CanonicalBytes()), `"provider":"glm"`, `"provider":"glm","PROVIDER":"other"`, 1)),
		"envelope case alias":   []byte(strings.Replace(string(d.CanonicalBytes()), `"schema_version":`, `"SCHEMA_VERSION":`, 1)),
		"null profile":          []byte(`{"schema_version":"` + DefinitionSchemaV1 + `","product_lineage_id":"glm/glm-5.3","profile":null}`),
	} {
		t.Run(name, func(t *testing.T) { _, err := DecodeDefinition(wire); require.Error(t, err) })
	}
	changed := strings.Replace(string(d.CanonicalBytes()), `"default_output_limit":65536`, `"default_output_limit":32768`, 1)
	require.NotEqual(t, changed, string(d.CanonicalBytes()))
	_, err = VerifyDefinition([]byte(changed), d.Digest())
	require.Error(t, err)
	var pretty json.RawMessage = d.CanonicalBytes()
	indented, err := json.MarshalIndent(pretty, "", "  ")
	require.NoError(t, err)
	decoded, err := DecodeDefinition(indented)
	require.NoError(t, err)
	require.Equal(t, d.Digest(), decoded.Digest())
	_, err = VerifyDefinition(indented, d.Digest())
	require.Error(t, err, "stored bytes must themselves be canonical")
}
