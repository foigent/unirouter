package xinsightconnector

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHTTPControlCounterpartAcceptsOnlyOneFixedHTTPSOrigin(t *testing.T) {
	descriptor := RuntimeInstallationDescriptor{
		InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA",
		XDeploymentID:  "xid_BBBBBBBBBBBBBBBBBBBBBB",
	}
	for _, origin := range []string{
		"http://control.example.test",
		"https://user@control.example.test",
		"https://control.example.test/path",
		"https://control.example.test?next=other",
		"https://control.example.test/#fragment",
		" https://control.example.test",
	} {
		t.Run(origin, func(t *testing.T) {
			_, err := NewHTTPControlCounterpart(
				origin, descriptor, &PostgresPhaseJournal{}, &http.Client{}, time.Second,
			)
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
	counterpart, err := NewHTTPControlCounterpart(
		"https://control.example.test", descriptor,
		&PostgresPhaseJournal{}, &http.Client{}, time.Second,
	)
	require.NoError(t, err)
	require.Equal(t, "https://control.example.test", counterpart.origin)
}

func TestHTTPControlCounterpartBoundsAndTypesResponses(t *testing.T) {
	require.NoError(t, requireConnectorJSONContentType(http.Header{
		"Content-Type": []string{"application/json; charset=utf-8"},
	}))
	require.Error(t, requireConnectorJSONContentType(http.Header{
		"Content-Type": []string{"text/plain"},
	}))
	_, err := readConnectorHTTPBody(strings.NewReader(strings.Repeat("x", int(connectorHTTPBodyLimit)+1)))
	require.Error(t, err)
}
