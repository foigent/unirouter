package sourcefacts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCurrentRuntimeCursorRejectsCrossScopeAndEventTokens(t *testing.T) {
	s, err := New(nil, Options{SourceInstance: "synthetic-cursor", CursorSecret: strings.Repeat("c", 32)})
	require.NoError(t, err)
	scope := s.currentCursorScope(access{org: 1, project: 2, keyID: 3, generation: 4}, "request-a")
	cursor := scope
	cursor.AfterRequest = "request-a"
	cursor.AfterFamily = "stream"
	cursor.AfterSubject = "attempt-a"
	token, err := s.encodeCurrentCursor(cursor)
	require.NoError(t, err)
	decoded, err := s.decodeCurrentCursor(token, scope)
	require.NoError(t, err)
	require.Equal(t, cursor, decoded)
	for _, change := range []func(*currentRuntimeCursor){
		func(v *currentRuntimeCursor) { v.Source = "other" }, func(v *currentRuntimeCursor) { v.Org = "2" }, func(v *currentRuntimeCursor) { v.Project = "3" }, func(v *currentRuntimeCursor) { v.Key = "4" }, func(v *currentRuntimeCursor) { v.Generation = "5" }, func(v *currentRuntimeCursor) { v.RequestFilter = "" }, func(v *currentRuntimeCursor) { v.Version = "unirouter.source-observation-cursor.v1" },
	} {
		altered := scope
		change(&altered)
		_, err = s.decodeCurrentCursor(token, altered)
		require.Error(t, err)
	}
	for _, bad := range []string{"", token[:len(token)-5], strings.Repeat("x", 4097), "source-event-cursor"} {
		_, err = s.decodeCurrentCursor(bad, scope)
		require.Error(t, err)
	}
	s.options.CursorSecret = strings.Repeat("d", 32)
	_, err = s.decodeCurrentCursor(token, scope)
	require.Error(t, err)
}
