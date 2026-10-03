package xinsightconnector

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func descriptorGolden(t *testing.T) ([]byte, SourceDescriptor) {
	t.Helper()
	raw, e := os.ReadFile("testdata/source-descriptor-v1/valid.json")
	require.NoError(t, e)
	require.Equal(t, "b75f590075c0098de400f0980bb8313027eb65632acf7e8a62364af6f7a1b264", fmt.Sprintf("%x", sha256.Sum256(raw)))
	d, e := DecodeSourceDescriptor(raw)
	require.NoError(t, e)
	return raw, d
}

func TestSourceDescriptorFrozenCanonicalAndOriginalV2Identity(t *testing.T) {
	raw, d := descriptorGolden(t)
	canonical, e := d.CanonicalJSON()
	require.NoError(t, e)
	require.Equal(t, bytes.TrimSpace(raw), canonical)
	require.Equal(t, "sha256:76829dc4f68cb0c41295a7d7c6f8d6d255b5fb2cf34f33e7d09fa6caa13befe5", d.SourceBindingSHA256)
	require.Equal(t, "sha256:28997a24bee1bb4c0ef59c0cbde34eee0966382325419df626eacd283d28f3a6", d.ReceiverNetworkSHA256)
	old, e := os.ReadFile("testdata/control-v2/valid/06-resume-source-result.json")
	require.NoError(t, e)
	r, e := DecodeControlResultV2(old)
	require.NoError(t, e)
	require.Equal(t, r.OperationID, d.ResumeOperationID)
	require.Equal(t, r.Result.(ResumeSourceResultV2).SourceInstance, d.SourceBinding.SourceInstance)
	require.Equal(t, PublicCanonicalSHA256(r), d.ResumeResultSHA256)
}

func TestSourceDescriptorStrictClosedDecoder(t *testing.T) {
	raw, _ := descriptorGolden(t)
	text := string(bytes.TrimSpace(raw))
	cases := map[string]string{
		"missing":        strings.Replace(text, `"schema_version":"unirouter.xinsight.connector-source-descriptor.v1",`, "", 1),
		"null":           strings.Replace(text, `"receiver_network":{`, `"receiver_network":null,"discarded":{`, 1),
		"null leaf":      strings.Replace(text, `"org_id":2`, `"org_id":null`, 1),
		"alias":          strings.Replace(text, `"org_id":2`, `"Org_ID":2`, 1),
		"duplicate":      strings.Replace(text, `"org_id":2`, `"org_id":2,"org_id":2`, 1),
		"extra":          strings.Replace(text, `"org_id":2`, `"org_id":2,"permission":true`, 1),
		"token":          strings.Replace(text, `"org_id":2`, `"org_id":2,"token":"synthetic"`, 1),
		"string integer": strings.Replace(text, `"org_id":2`, `"org_id":"2"`, 1),
		"float":          strings.Replace(text, `"org_id":2`, `"org_id":2.0`, 1),
		"exponent":       strings.Replace(text, `"org_id":2`, `"org_id":2e0`, 1),
		"overflow":       strings.Replace(text, `"org_id":2`, `"org_id":9223372036854775808`, 1),
		"binding digest": strings.Replace(text, "sha256:76829dc4", "sha256:00000000", 1),
		"network digest": strings.Replace(text, "sha256:28997a24", "sha256:00000000", 1),
		"trailing":       text + `{}`,
		"invalid UTF8":   strings.Replace(text, "ecs.second-integration.v2", string([]byte{0xff}), 1),
		"too large":      text + strings.Repeat(" ", 16<<10),
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			_, e := DecodeSourceDescriptor([]byte(bad))
			require.Error(t, e)
		})
	}
	_, d := descriptorGolden(t)
	for _, bad := range []string{"1050/32", "192.0.2.10/24", "192.0.2.10", " 192.0.2.10/32", "2001:0db8::1/128"} {
		d.ReceiverNetwork.AllowedIP = bad
		d.ReceiverNetworkSHA256 = PublicCanonicalSHA256(d.ReceiverNetwork)
		require.Error(t, ValidateSourceDescriptor(d), bad)
	}
}

func TestDescriptorIdentityReadbackDoesNotAssertMachineNetworkEligibility(t *testing.T) {
	_, d := descriptorGolden(t)
	o := loadV3Operation(t, "05-policy-pair-operation")
	for _, ip := range []string{"::ffff:192.0.2.10/128", "0.0.0.0/32", "::/128", "224.0.0.1/32", "ff02::1/128"} {
		t.Run(ip, func(t *testing.T) {
			d.ReceiverNetwork.AllowedIP = ip
			d.ReceiverNetworkSHA256 = PublicCanonicalSHA256(d.ReceiverNetwork)
			require.NoError(t, ValidateSourceDescriptor(d), "r1 identity shape preserves exact canonical host CIDR")
			q := o.Request.(DeliverMachineRequest)
			q.Registration.AllowedIP = ip
			q.RegistrationSHA256 = PublicCanonicalSHA256(q.Registration)
			q.CustodyReceipt.RegistrationSHA256 = q.RegistrationSHA256
			q.CustodyReceiptSHA256 = PublicCanonicalSHA256(q.CustodyReceipt)
			o.Request = q
			require.Error(t, ValidateControlOperationV3(o), "same frozen XI machine eligibility exclusions")
			bind := loadV3Operation(t, "01-bind-existing-operation")
			b := bind.Request.(BindExistingReceiverRequest)
			b.Candidate.AllowedIP = ip
			b.CandidateSHA256 = PublicCanonicalSHA256(b.Candidate)
			bind.Request = b
			require.Error(t, ValidateControlOperationV3(bind))
		})
	}
}
