//go:build integration

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

func TestSourceObservationsRequireExplicitGrantsAndIndependentCursorsIntegration(t *testing.T) {
	empty := map[string]any{}
	f := newSourceControlFixture(t)
	available := func() []string {
		r := f.request(t, "", empty)
		r.Method = http.MethodGet
		w := httptest.NewRecorder()
		f.handler.GetXInsightSourceConnection(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		var body struct {
			Profiles []string `json:"available_profiles"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		require.True(t, slices.IsSorted(body.Profiles))
		return body.Profiles
	}
	require.ElementsMatch(t, sourcefacts.Profiles(), available())
	breg, _ := f.registration(t, "disabled-b", []string{sourcefacts.RuntimeProfile})
	f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": breg}, 503)
	areg, oldToken := f.registration(t, "original-a", []string{sourcefacts.ContextProfile})
	key := f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": areg}, 200)
	f.control(t, "activate", map[string]any{"credential_id": key["credential_id"]}, 200)
	f.control(t, "resume", empty, 200)
	var err error
	f.source, err = sourcefacts.New(f.pool, sourcefacts.Options{Enabled: true, ObservationsEnabled: true, SourceInstance: "synthetic-source-control", CursorSecret: syntheticSourceControlSecret})
	require.NoError(t, err)
	require.NoError(t, f.source.CheckReady(t.Context()))
	f.handler.SetSourceFactsController(f.source)
	require.ElementsMatch(t, append(sourcefacts.Profiles(), sourcefacts.ObservationProfiles()...), available())
	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	aReader, bReader := f.source.Handler(resolver), f.source.ObservationHandler(resolver)
	bTarget := "/api/v1/integrations/xinsight/source/v2/events?profile=" + sourcefacts.RuntimeProfile
	f.reader = bReader
	f.read(t, oldToken, bTarget, "127.0.0.1:2000", 403)
	runtimeReader := f.source.RuntimeCurrentHandler(resolver)
	runtimeTarget := "/api/v1/integrations/xinsight/source/v2/runtime/current"
	f.reader = runtimeReader
	f.read(t, oldToken, runtimeTarget, "127.0.0.1:2000", 403)
	f.reader = bReader
	var grants []string
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT profiles FROM execution_source_connections WHERE project_id=$1`, f.projectID).Scan(&grants))
	require.Equal(t, []string{sourcefacts.ContextProfile}, grants)
	f.control(t, "pause", empty, 200)
	profiles := []string{sourcefacts.ContextProfile, sourcefacts.RuntimeProfile}
	f.control(t, "profiles", map[string]any{"profiles": profiles}, 200)
	reg, token := f.registration(t, "explicit-a-b", profiles)
	key = f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": reg}, 200)
	f.control(t, "activate", map[string]any{"credential_id": key["credential_id"]}, 200)
	f.control(t, "resume", empty, 200)
	f.read(t, oldToken, bTarget, "127.0.0.1:2000", 401)
	f.reader = aReader
	aTarget := sourceReadPath + "?profile=" + sourcefacts.ContextProfile
	aBoot := sourcePage(t, f.read(t, token, aTarget, "127.0.0.1:2000", 200))
	f.reader = bReader
	bBoot := sourcePage(t, f.read(t, token, bTarget, "127.0.0.1:2000", 200))
	require.Empty(t, bBoot.Events)
	require.NotEqual(t, aBoot.NextCursor, bBoot.NextCursor)
	f.read(t, token, bTarget+"&cursor="+url.QueryEscape(aBoot.NextCursor), "127.0.0.1:2000", 400)
	f.reader = aReader
	f.read(t, token, aTarget+"&cursor="+url.QueryEscape(bBoot.NextCursor), "127.0.0.1:2000", 400)
	f.capture(t, "b-observed-one")
	f.capture(t, "b-observed-two")
	f.reader = bReader
	target := bTarget + "&limit=1&cursor=" + url.QueryEscape(bBoot.NextCursor)
	page := sourcePage(t, f.read(t, token, target, "127.0.0.1:2000", 200))
	require.Len(t, page.Events, 1)
	require.True(t, page.HasMore)
	replay := sourcePage(t, f.read(t, token, target, "127.0.0.1:2000", 200))
	require.Equal(t, page.Events, replay.Events)
	require.Equal(t, page.NextCursor, replay.NextCursor)
	var event map[string]any
	require.NoError(t, json.Unmarshal(page.Events[0], &event))
	payload, e := eventconnector.CanonicalJSON(event["payload"])
	require.Nil(t, e)
	d, err := sourcefacts.DecodeObservation(payload)
	require.NoError(t, err)
	require.Equal(t, "b-observed-one", d.RequestID)
	require.Equal(t, "not_observed", d.Runtime.StreamMode)
	require.Equal(t, "authenticated", d.Runtime.Admission)
	require.NotContains(t, string(payload), "owner_user_id")
	require.Equal(t, eventconnector.HashSourceBytes(payload), event["integrity"].(map[string]any)["source_hash"])
	f.reader = runtimeReader
	current := f.read(t, token, runtimeTarget, "127.0.0.1:2000", 200)
	require.Equal(t, "no-store", current.Header().Get("Cache-Control"))
	var runtimePage map[string]any
	require.NoError(t, json.Unmarshal(current.Body.Bytes(), &runtimePage))
	hash := runtimePage["source_sha256"]
	delete(runtimePage, "source_sha256")
	canonical, ce := eventconnector.CanonicalJSON(runtimePage)
	require.Nil(t, ce)
	require.Equal(t, hash, eventconnector.HashSourceBytes(canonical))
	require.Equal(t, []any{}, runtimePage["entries"])
	f.read(t, token, runtimeTarget+"?org_id=999", "127.0.0.1:2000", 400)
	f.read(t, token, runtimeTarget+"?limit=51", "127.0.0.1:2000", 400)
	f.read(t, token, runtimeTarget+"?cursor="+url.QueryEscape(bBoot.NextCursor), "127.0.0.1:2000", 400)
	f.read(t, token, runtimeTarget, "192.0.2.1:2000", 401)
	f.control(t, "pause", empty, 200)
	f.read(t, token, runtimeTarget, "127.0.0.1:2000", 503)
	f.reader = bReader
	f.read(t, token, bTarget, "127.0.0.1:2000", 503)
	f.control(t, "revoke", empty, 200)
	f.read(t, token, bTarget, "127.0.0.1:2000", 401)
	f.reader = runtimeReader
	f.read(t, token, runtimeTarget, "127.0.0.1:2000", 401)
}
