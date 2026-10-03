package xinsightconnector

import (
	"bytes"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestControlV3GoldenFixtures(t *testing.T) {
	root := filepath.Join("testdata", "control-v3")
	raw, e := os.ReadFile(filepath.Join(root, "index.json"))
	require.NoError(t, e)
	require.Equal(t, "sha256:f01b7af3aa0768220d7530a9a3c38fda4b041cd49ba1cfdf8c686c90d7174286", hashBytes(raw))
	var index controlV2FixtureIndex
	require.NoError(t, json.Unmarshal(raw, &index))
	for _, entry := range index.Entries {
		t.Run(entry.Path, func(t *testing.T) {
			wire, e := os.ReadFile(filepath.Join(root, entry.Path))
			require.NoError(t, e)
			var canonical []byte
			if strings.Contains(entry.Path, "operation") || strings.Contains(entry.Path, "purpose-prefix") {
				o, err := DecodeControlOperationV3(wire)
				if entry.Expect == "invalid" {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				canonical, e = o.CanonicalJSON()
			} else {
				r, err := DecodeControlResultV3(wire)
				if entry.Expect == "invalid" {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				canonical, e = r.CanonicalJSON()
			}
			require.NoError(t, e)
			require.Equal(t, wire, canonical)
			require.Equal(t, entry.CanonicalSHA256, hashBytes(canonical))
		})
	}
	for _, pair := range [][2]string{{"01-bind-existing-operation", "02-bind-existing-result"}, {"03-confirm-existing-operation", "04-confirm-existing-result"}, {"05-policy-pair-operation", "06-policy-pair-result"}, {"07-source-enroll-pair-operation", "08-source-enroll-pair-result"}} {
		o := loadV3Operation(t, pair[0])
		wire, e := os.ReadFile(filepath.Join(root, "valid", pair[1]+".json"))
		require.NoError(t, e)
		r, e := DecodeControlResultV3(wire)
		require.NoError(t, e)
		require.NoError(t, ValidateControlResultForOperationV3(o, r))
	}
}
func loadV3Operation(t *testing.T, name string) ControlOperationV3 {
	t.Helper()
	b, e := os.ReadFile(filepath.Join("testdata", "control-v3", "valid", name+".json"))
	require.NoError(t, e)
	o, e := DecodeControlOperationV3(b)
	require.NoError(t, e)
	return o
}
func TestControlV3StrictFieldsAndEligibility(t *testing.T) {
	raw, e := os.ReadFile("testdata/control-v3/valid/05-policy-pair-operation.json")
	require.NoError(t, e)
	for name, wire := range map[string][]byte{
		"null":            bytes.Replace(raw, []byte(`"purpose":"xinsight.policy.manage"`), []byte(`"purpose":null`), 1),
		"secret":          bytes.Replace(raw, []byte(`"custody_state":"prepared"`), []byte(`"custody_state":"prepared","token":"secret"`), 1),
		"duplicate":       bytes.Replace(raw, []byte(`"desired_state":"active"`), []byte(`"desired_state":"active","desired_state":"active"`), 1),
		"case":            bytes.Replace(raw, []byte(`"allowed_ip"`), []byte(`"Allowed_IP"`), 1),
		"missing":         bytes.Replace(raw, []byte(`,"allowed_ip":"192.0.2.10/32"`), nil, 1),
		"unknown-version": bytes.Replace(raw, []byte(ControlOperationV3Schema), []byte("xinsight.connector-control-operation.v99"), 1),
		"invalid-utf8":    bytes.Replace(raw, []byte("synthetic-source"), []byte{255}, 1),
	} {
		t.Run(name, func(t *testing.T) { _, e := DecodeControlOperationV3(wire); require.Error(t, e) })
	}
	o := loadV3Operation(t, "03-confirm-existing-operation")
	p, e := os.ReadFile("testdata/control-v3/valid/02-bind-existing-result.json")
	require.NoError(t, e)
	require.NoError(t, ValidateControlPredecessorV3(o, p))
	o.PredecessorResultSHA256 = "sha256:" + strings.Repeat("a", 64)
	require.Error(t, ValidateControlPredecessorV3(o, p))
	c := loadV3Operation(t, "03-confirm-existing-operation")
	q := c.Request.(ConfirmExistingReceiverRequest)
	q.FeedReadReceipt.PagesCommitted = q.XIAdoptionReceipt.PagesCommittedBefore
	q.FeedReadReceiptSHA256 = PublicCanonicalSHA256(q.FeedReadReceipt)
	c.Request = q
	require.Error(t, ValidateControlOperationV3(c), "old success cannot confirm")
	c = loadV3Operation(t, "03-confirm-existing-operation")
	q = c.Request.(ConfirmExistingReceiverRequest)
	q.XIAdoptionReceipt.PagesCommittedBefore = 0
	q.XIAdoptionReceiptSHA256 = PublicCanonicalSHA256(q.XIAdoptionReceipt)
	c.Request = q
	b, e := c.CanonicalJSON()
	require.NoError(t, e)
	b = bytes.Replace(b, []byte(`,"pages_committed_before":0`), nil, 1)
	_, e = DecodeControlOperationV3(b)
	require.Error(t, e, "zero-valued required counters cannot be omitted")
}

func TestControlV3ConfirmDoesNotOrderAcrossHostClocks(t *testing.T) {
	o := loadV3Operation(t, "03-confirm-existing-operation")
	wire, e := os.ReadFile("testdata/control-v3/valid/04-confirm-existing-result.json")
	require.NoError(t, e)
	r, e := DecodeControlResultV3(wire)
	require.NoError(t, e)
	// UR observed_at may precede XI completed_at. Each host's own temporal
	// predicates remain enforced; the predecessor/digests bind the two sides.
	r.ObservedAt = "2026-10-02T23:59:00Z"
	require.NoError(t, ValidateControlResultForOperationV3(o, r))
	q := o.Request.(ConfirmExistingReceiverRequest)
	q.FeedReadReceipt.CompletedAt = q.XIAdoptionReceipt.CommittedAt
	q.FeedReadReceiptSHA256 = PublicCanonicalSHA256(q.FeedReadReceipt)
	o.Request = q
	require.Error(t, ValidateControlOperationV3(o))
}
