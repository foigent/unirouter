package xinsightconnector

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	xControlV2HeadSHA256         = "75e79ceae6370c0c24ccb50e527140ea66b69ba0"
	xControlV2TreeSHA256         = "d5e94c5ca9972c21eb200e1f55093abb99041621"
	xControlV2FieldMatrixSHA256  = "e916790bfff23eb843eb0dc6aa912a997dd141273c1b8c1263c25b5521c86376"
	xControlV2FixtureIndexSHA256 = "bc1964e1ab76735e1e495dbfebf0549cad03cb0e53659d1b1e4d6e93a83eb6d9"
)

type controlV2FixtureIndex struct {
	SchemaVersion    string                  `json:"schema_version"`
	OperationSchema  string                  `json:"operation_schema"`
	ResultSchema     string                  `json:"result_schema"`
	Canonicalization string                  `json:"canonicalization"`
	Entries          []controlV2FixtureEntry `json:"entries"`
}

type controlV2FixtureEntry struct {
	Path            string `json:"path"`
	Expect          string `json:"expect"`
	ReasonCode      string `json:"reason_code"`
	CanonicalSHA256 string `json:"canonical_sha256"`
	RawSHA256       string `json:"raw_sha256"`
}

func TestControlV2FrozenXFixtures(t *testing.T) {
	root := filepath.Join("testdata", "control-v2")
	indexBytes := mustReadControlV2Fixture(t, filepath.Join(root, "index.json"))
	require.Equal(t, xControlV2FixtureIndexSHA256, rawSHA256(indexBytes))

	var index controlV2FixtureIndex
	require.NoError(t, json.Unmarshal(indexBytes, &index))
	require.Equal(t, "xinsight.connector-control-conformance-index.v1", index.SchemaVersion)
	require.Equal(t, ControlOperationV2Schema, index.OperationSchema)
	require.Equal(t, ControlResultV2Schema, index.ResultSchema)
	require.Equal(t, "RFC8259 compact JSON using the product typed field order", index.Canonicalization)
	require.Len(t, index.Entries, 11)

	prepareResultRaw := mustReadControlV2Fixture(t, filepath.Join(root, "valid/02-prepare-paused-source-result.json"))
	prepareResult, err := DecodeControlResultV2(prepareResultRaw)
	require.NoError(t, err)

	for _, fixture := range index.Entries {
		fixture := fixture
		t.Run(fixture.Path, func(t *testing.T) {
			require.False(t, filepath.IsAbs(fixture.Path))
			require.NotContains(t, fixture.Path, "..")
			raw := mustReadControlV2Fixture(t, filepath.Join(root, filepath.FromSlash(fixture.Path)))
			if fixture.CanonicalSHA256 != "" {
				require.Equal(t, fixture.CanonicalSHA256, hashBytes(bytes.TrimSuffix(raw, []byte{'\n'})))
			} else {
				require.Equal(t, fixture.RawSHA256, "sha256:"+rawSHA256(raw))
			}

			if fixture.Expect == "valid" {
				canonical, digest := decodeCanonicalControlV2Fixture(t, raw)
				require.Equal(t, fixture.CanonicalSHA256, digest)
				require.Equal(t, bytes.TrimSuffix(raw, []byte{'\n'}), canonical)
				return
			}

			var validationErr error
			if fixture.Path == "invalid/04-resume-wrong-predecessor.json" {
				operation, err := DecodeControlOperationV2(raw)
				require.NoError(t, err, "the operation is structurally valid; the durable predecessor is not")
				validationErr = ValidateControlOperationPredecessorV2(operation, &prepareResult)
			} else {
				_, validationErr = DecodeControlOperationV2(raw)
			}
			require.Error(t, validationErr)
			require.Equal(t, fixture.ReasonCode, ControlV2ErrorCode(validationErr))
		})
	}

	t.Logf("frozen X authority head=%s tree=%s field_matrix_sha256=%s", xControlV2HeadSHA256, xControlV2TreeSHA256, xControlV2FieldMatrixSHA256)
}

func TestControlV2ThreePhaseChainAndBindings(t *testing.T) {
	root := filepath.Join("testdata", "control-v2", "valid")
	prepareOperation := mustDecodeControlV2Operation(t, filepath.Join(root, "01-prepare-paused-source-operation.json"))
	prepareResult := mustDecodeControlV2Result(t, filepath.Join(root, "02-prepare-paused-source-result.json"))
	registerOperation := mustDecodeControlV2Operation(t, filepath.Join(root, "03-register-receiver-operation.json"))
	registerResult := mustDecodeControlV2Result(t, filepath.Join(root, "04-register-receiver-result.json"))
	resumeOperation := mustDecodeControlV2Operation(t, filepath.Join(root, "05-resume-source-operation.json"))
	resumeResult := mustDecodeControlV2Result(t, filepath.Join(root, "06-resume-source-result.json"))

	require.NoError(t, ValidateControlOperationPredecessorV2(prepareOperation, nil))
	require.NoError(t, ValidateControlResultForOperationV2(prepareOperation, prepareResult))
	require.NoError(t, ValidateControlOperationPredecessorV2(registerOperation, &prepareResult))
	require.NoError(t, ValidateControlResultForOperationV2(registerOperation, registerResult))
	require.NoError(t, ValidateControlOperationPredecessorV2(resumeOperation, &registerResult))
	require.NoError(t, ValidateControlResultForOperationV2(resumeOperation, resumeResult))

	registerRequest, ok := registerOperation.Request.(RegisterReceiverRequestV2)
	require.True(t, ok)
	require.Equal(t, int64(1), registerRequest.ConsumerEpoch)
	require.Equal(t, ControlFixedReceiverRoute, registerRequest.NetworkBinding.Route)
	require.Equal(t, "https", registerRequest.NetworkBinding.Transport)
	registrationBytes, err := json.Marshal(registerRequest.ReceiverRegistration)
	require.NoError(t, err)
	require.NotContains(t, string(registrationBytes), `"token":`)
	require.NotContains(t, string(registrationBytes), "bearer")
	require.Equal(t, "sha256:dd6a7e9ae56b499bbbebae83ff178749ebd402e428841a0167b3b499187b13db", hashBytes(registrationBytes))

	crossInstallation := registerOperation
	crossInstallation.InstallationID = "xci_ZZZZZZZZZZZZZZZZZZZZZZ"
	err = ValidateControlOperationPredecessorV2(crossInstallation, &prepareResult)
	require.Equal(t, "predecessor_phase_conflict", ControlV2ErrorCode(err))

	changedResult := registerResult
	changedPayload := changedResult.Result.(RegisterReceiverResultV2)
	changedPayload.ReceiverRegistrationSHA256 = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	changedResult.Result = changedPayload
	err = ValidateControlResultForOperationV2(registerOperation, changedResult)
	require.Equal(t, "result_binding_conflict", ControlV2ErrorCode(err))
}

func TestControlV2ExactCaseUnknownDuplicateAndSecretFieldsFailClosed(t *testing.T) {
	raw := mustReadControlV2Fixture(t, filepath.Join("testdata", "control-v2", "valid", "01-prepare-paused-source-operation.json"))
	original, err := DecodeControlOperationV2(raw)
	require.NoError(t, err)
	originalDigest, err := original.CanonicalSHA256()
	require.NoError(t, err)

	caseAliasRaw := bytes.Replace(raw, []byte("xci_AAAAAAAAAAAAAAAAAAAAAA"), []byte("xci_aAAAAAAAAAAAAAAAAAAAAA"), 1)
	caseAlias, err := DecodeControlOperationV2(caseAliasRaw)
	require.NoError(t, err)
	require.Equal(t, "xci_aAAAAAAAAAAAAAAAAAAAAA", caseAlias.InstallationID)
	caseAliasDigest, err := caseAlias.CanonicalSHA256()
	require.NoError(t, err)
	require.NotEqual(t, originalDigest, caseAliasDigest)

	unknown := bytes.Replace(raw, []byte(`"required_source_state":"paused"`), []byte(`"required_source_state":"paused","token":"secret"`), 1)
	_, err = DecodeControlOperationV2(unknown)
	require.Equal(t, "unknown_or_secret_field", ControlV2ErrorCode(err))

	duplicateNested := bytes.Replace(raw, []byte(`"feed_schema":"unirouter.completed-execution.v1"`), []byte(`"feed_schema":"unirouter.completed-execution.v1","feed_schema":"unirouter.completed-execution.v1"`), 1)
	_, err = DecodeControlOperationV2(duplicateNested)
	require.Equal(t, "duplicate_field", ControlV2ErrorCode(err))

	whitespaceIdentity := bytes.Replace(raw, []byte("project.second-integration"), []byte(" project.second-integration"), 1)
	_, err = DecodeControlOperationV2(whitespaceIdentity)
	require.Equal(t, "operation_invalid", ControlV2ErrorCode(err))

	wrongIdentityShape := bytes.Replace(raw, []byte("xci_AAAAAAAAAAAAAAAAAAAAAA"), []byte("xci_short"), 1)
	_, err = DecodeControlOperationV2(wrongIdentityShape)
	require.Equal(t, "operation_invalid", ControlV2ErrorCode(err))

	_, err = DecodeControlOperationV2(append(bytes.Clone(raw), []byte(`{"second":true}`)...))
	require.Equal(t, "json_invalid", ControlV2ErrorCode(err))
}

func TestControlV2FailedTerminalHasNoResultPayload(t *testing.T) {
	raw := []byte(`{"schema_version":"xinsight.connector-control-result.v2","operation_id":"xco_BBBBBBBBBBBBBBBBBBBBBB","installation_id":"xci_AAAAAAAAAAAAAAAAAAAAAA","phase":"prepare_paused_source","status":"failed","reason_code":"source_unavailable","observed_at":"2030-01-01T00:01:00Z"}`)
	result, err := DecodeControlResultV2(raw)
	require.NoError(t, err)
	require.Nil(t, result.Result)
	canonical, err := result.CanonicalJSON()
	require.NoError(t, err)
	require.Equal(t, raw, canonical)

	withResult := bytes.Replace(raw, []byte(`"reason_code":"source_unavailable"`), []byte(`"result":{},"reason_code":"source_unavailable"`), 1)
	_, err = DecodeControlResultV2(withResult)
	require.Equal(t, "result_invalid", ControlV2ErrorCode(err))

	nonMachineReason := bytes.Replace(raw, []byte("source_unavailable"), []byte("raw upstream timeout"), 1)
	_, err = DecodeControlResultV2(nonMachineReason)
	require.Equal(t, "result_invalid", ControlV2ErrorCode(err))
}

func decodeCanonicalControlV2Fixture(t *testing.T, raw []byte) ([]byte, string) {
	t.Helper()
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
	}
	require.NoError(t, json.Unmarshal(raw, &envelope))
	if envelope.SchemaVersion == ControlOperationV2Schema {
		operation, err := DecodeControlOperationV2(raw)
		require.NoError(t, err)
		canonical, err := operation.CanonicalJSON()
		require.NoError(t, err)
		digest, err := operation.CanonicalSHA256()
		require.NoError(t, err)
		return canonical, digest
	}
	result, err := DecodeControlResultV2(raw)
	require.NoError(t, err)
	canonical, err := result.CanonicalJSON()
	require.NoError(t, err)
	digest, err := result.CanonicalSHA256()
	require.NoError(t, err)
	return canonical, digest
}

func mustDecodeControlV2Operation(t *testing.T, path string) ControlOperationV2 {
	t.Helper()
	operation, err := DecodeControlOperationV2(mustReadControlV2Fixture(t, path))
	require.NoError(t, err)
	return operation
}

func mustDecodeControlV2Result(t *testing.T, path string) ControlResultV2 {
	t.Helper()
	result, err := DecodeControlResultV2(mustReadControlV2Fixture(t, path))
	require.NoError(t, err)
	return result
}

func mustReadControlV2Fixture(t *testing.T, path string) []byte {
	t.Helper()
	value, err := os.ReadFile(path)
	require.NoError(t, err)
	return value
}

func rawSHA256(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func TestControlV2FixtureContainsNoReceiverBearer(t *testing.T) {
	root := filepath.Join("testdata", "control-v2")
	for _, relative := range []string{
		"valid/01-prepare-paused-source-operation.json",
		"valid/02-prepare-paused-source-result.json",
		"valid/03-register-receiver-operation.json",
		"valid/04-register-receiver-result.json",
		"valid/05-resume-source-operation.json",
		"valid/06-resume-source-result.json",
	} {
		raw := mustReadControlV2Fixture(t, filepath.Join(root, relative))
		require.NotContains(t, strings.ToLower(string(raw)), `"token"`)
		require.NotContains(t, strings.ToLower(string(raw)), "bearer")
	}
}
