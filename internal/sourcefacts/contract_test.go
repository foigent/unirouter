package sourcefacts

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/eventconnector/pullv1"
	"github.com/uniroute/unirouter/internal/executioncontext"
)

func TestIndependentFrozenPagesMatchActualSourceProjection(t *testing.T) {
	wire, err := os.ReadFile("../../contracts/source-facts/v1/golden-pages.json")
	require.NoError(t, err)
	var fixture struct {
		Valid []struct {
			Name string
			Page struct {
				BatchVersion     string            `json:"batch_version"`
				ConnectorID      string            `json:"connector_id"`
				ConnectorVersion string            `json:"connector_version"`
				ProducerInstance string            `json:"producer_instance"`
				GeneratedAt      string            `json:"generated_at"`
				Events           []json.RawMessage `json:"events"`
				NextCursor       string            `json:"next_cursor"`
				HighWatermark    string            `json:"high_watermark"`
				HasMore          bool              `json:"has_more"`
			}
		}
	}
	require.NoError(t, json.Unmarshal(wire, &fixture))
	require.Len(t, fixture.Valid, 14)
	for _, v := range fixture.Valid {
		t.Run(v.Name, func(t *testing.T) {
			p := v.Page
			batch := pullv1.EventBatch{BatchVersion: p.BatchVersion, ConnectorID: p.ConnectorID, ConnectorVersion: p.ConnectorVersion, ProducerInstance: p.ProducerInstance, GeneratedAt: p.GeneratedAt, NextCursor: p.NextCursor, HighWatermark: p.HighWatermark, HasMore: p.HasMore, Events: []eventconnector.ImmutableEvent{}}
			for _, raw := range p.Events {
				var envelope map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(raw, &envelope))
				var document Document
				require.NoError(t, json.Unmarshal(envelope["payload"], &document))
				canonical, err := Encode(document)
				require.NoError(t, err)
				event, err := Event(document, canonical)
				require.NoError(t, err)
				var expected map[string]any
				decoder := json.NewDecoder(bytes.NewReader(raw))
				decoder.UseNumber()
				require.NoError(t, decoder.Decode(&expected))
				expectedBytes, ce := eventconnector.CanonicalJSON(expected)
				require.Nil(t, ce)
				require.Equal(t, string(expectedBytes), string(event.EnvelopeBytes()), "independent Python identity and canonical hashes must match actual Go projection")
				batch.Events = append(batch.Events, event)
			}
			require.Nil(t, batch.Validate())
		})
	}
}

func TestIndependentFrozenSourceVectors(t *testing.T) {
	wire, err := os.ReadFile("../../contracts/source-facts/v1/golden-source-facts.json")
	require.NoError(t, err)
	var vectors struct {
		Valid []struct {
			Name         string
			Document     Document
			Canonical    string
			SourceSHA256 string `json:"source_sha256"`
		}
		Invalid []struct{ Name, Wire string }
	}
	require.NoError(t, json.Unmarshal(wire, &vectors))
	require.Len(t, vectors.Valid, 8)
	require.Len(t, vectors.Invalid, 9)
	for _, v := range vectors.Valid {
		t.Run(v.Name, func(t *testing.T) {
			encoded, err := Encode(v.Document)
			require.NoError(t, err)
			require.Equal(t, v.Canonical, string(encoded))
			require.Equal(t, v.SourceSHA256, fmt.Sprintf("sha256:%x", sha256.Sum256(encoded)))
			d, err := Decode([]byte(v.Canonical))
			require.NoError(t, err)
			require.Equal(t, v.Document, d)
			_, err = Event(d, encoded)
			require.NoError(t, err)
		})
	}
	for _, v := range vectors.Invalid {
		t.Run(v.Name, func(t *testing.T) { _, err := Decode([]byte(v.Wire)); require.Error(t, err) })
	}
}

func testSnapshot() executioncontext.Snapshot {
	return executioncontext.Snapshot{SchemaVersion: executioncontext.Version,
		Core:     executioncontext.Core{RequestID: "source-test", OrgID: "1", ProjectID: "2", APIKeyID: "9007199254740993", KeyType: "personal", AuthKind: "api_key", AuthEpoch: "0", Operation: "chat.completions", Protocol: "openai.v1", CapturedAt: "2026-09-09T00:00:00.000000Z"},
		Identity: executioncontext.Identity{OwnerUserID: "9007199254740994", ActualHuman: "unknown", AuthorityRead: "live_key"}}
}

func TestCanonicalSourceProfileAndRevisionIdentity(t *testing.T) {
	s := &Service{options: Options{SourceInstance: "synthetic-ur"}}
	snapshot := testSnapshot()
	d := s.contexts(1, 2, "source-test", "authenticated", snapshot.Core.CapturedAt, snapshot.Core.CapturedAt, &snapshot)[0]
	wire, e := Encode(d)
	require.NoError(t, e)
	decoded, e := Decode(wire)
	require.NoError(t, e)
	require.Equal(t, d, decoded)
	require.Contains(t, string(wire), `"api_key_id":"9007199254740993"`)
	require.NotContains(t, string(wire), "owner_user_id")
	event, e := Event(d, wire)
	require.NoError(t, e)
	first, e := json.Marshal(event)
	require.NoError(t, e)
	again, e := Event(d, wire)
	require.NoError(t, e)
	second, e := json.Marshal(again)
	require.NoError(t, e)
	require.Equal(t, first, second)
	d.PreviousRevision = "1"
	d.PreviousHash = "sha256:" + strings.Repeat("a", 64)
	d.CorrectionReason = "late_observation"
	d.Revision = "2"
	d.EventID = d.eventID()
	require.Equal(t, decoded.SourceFactID, d.SourceFactID)
	require.NotEqual(t, decoded.EventID, d.EventID)
	_, e = Encode(d)
	require.NoError(t, e)
}

func TestStrictSourceRejectsUnknownDuplicateAndCrossProfileFields(t *testing.T) {
	s := &Service{options: Options{SourceInstance: "synthetic-ur"}}
	snap := testSnapshot()
	d := s.contexts(1, 2, "source-test", "authenticated", snap.Core.CapturedAt, snap.Core.CapturedAt, &snap)[0]
	wire, e := Encode(d)
	require.NoError(t, e)
	for _, suffix := range []string{`,"prompt":"private"}`, `,"profile":"unirouter.execution-context.v1"}`, `,"identity":null}`} {
		_, e = Decode(append(append([]byte{}, wire[:len(wire)-1]...), []byte(suffix)...))
		require.Error(t, e)
	}
	d.Identity = &snap.Identity
	_, e = Encode(d)
	require.Error(t, e)
	d.Identity = nil
	d.Core.OrgID = "3"
	_, e = Encode(d)
	require.Error(t, e)
}

func TestCursorIsBoundToProfileScopeProducerAndEpoch(t *testing.T) {
	s := &Service{options: Options{SourceInstance: "synthetic-ur", CursorSecret: strings.Repeat("x", 32)}}
	scope := cursorScope{1, 2, 3, 4, ContextProfile}
	wire, e := s.encodeCursor(scope, 9007199254740993)
	require.NoError(t, e)
	require.Len(t, wire, 140)
	position, e := s.decodeCursor(wire, scope)
	require.NoError(t, e)
	require.EqualValues(t, 9007199254740993, position)
	for _, other := range []cursorScope{{2, 2, 3, 4, ContextProfile}, {1, 3, 3, 4, ContextProfile}, {1, 2, 4, 4, ContextProfile}, {1, 2, 3, 5, ContextProfile}, {1, 2, 3, 4, IdentityProfile}} {
		_, e = s.decodeCursor(wire, other)
		require.Error(t, e)
	}
	s.options.SourceInstance = "different"
	_, e = s.decodeCursor(wire, scope)
	require.Error(t, e)
	_, e = s.decodeCursor(wire+"=", scope)
	require.Error(t, e)
}
