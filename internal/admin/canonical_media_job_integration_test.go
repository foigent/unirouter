//go:build integration

package admin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/mediajob"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/modelauthz"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCanonicalMediaImagePublishesAndDispatchesOriginalProjectJobIntegration(t *testing.T) {
	for _, upstreamModel := range []string{modelprofile.OpenAIImageFlare, endpointprotocol.DashScopeQwenImage30, endpointprotocol.DashScopeQwenImage30Pro, endpointprotocol.MiniMaxH3Image, endpointprotocol.DashScopeWanVideo, endpointprotocol.MiniMaxH3Video, endpointprotocol.GLMCogVideo, endpointprotocol.GLMImage, endpointprotocol.DeepSeekOCR, endpointprotocol.GLMOCR, endpointprotocol.DeepSeekASR, endpointprotocol.DeepSeekTTS, endpointprotocol.MiniMaxASR, endpointprotocol.MiniMaxTTS, endpointprotocol.GLMASR, endpointprotocol.GLMTTS, endpointprotocol.OpenAITranscribe, endpointprotocol.MiMoASR, endpointprotocol.MiMoTTS,
		endpointprotocol.DashScopeASR, endpointprotocol.DashScopeTTS} {
		t.Run(upstreamModel, func(t *testing.T) {
			testCanonicalMediaImageJob(t, upstreamModel, false, false, nil)
		})
	}
	t.Run("wan probe expires without resubmitting", func(t *testing.T) {
		testCanonicalMediaImageJob(t, endpointprotocol.DashScopeWanVideo, true, false, nil)
	})
	t.Run("wan mismatched provider origin is not probeable", func(t *testing.T) {
		testCanonicalMediaImageJob(t, endpointprotocol.DashScopeWanVideo, false, true, nil)
	})
	for _, v := range []qwenTTSUsageFixture{
		{name: "explicit_zero", usage: `{"input_tokens":12,"output_tokens":0,"total_tokens":12}`, expected: []string{"12", "0"}},
		{name: "unknown_output", usage: `{"input_tokens":12,"total_tokens":12}`, expected: []string{"12"}},
		{name: "all_unknown", usage: `{}`, expected: []string{}},
	} {
		t.Run("qwen_tts_"+v.name, func(t *testing.T) {
			testCanonicalMediaImageJob(t, endpointprotocol.DashScopeTTS, false, false, &v)
		})
	}
}

type qwenTTSUsageFixture struct {
	name, usage string
	expected    []string
}

func testCanonicalMediaImageJob(t *testing.T, upstreamModel string, expireProbe, mismatchedOrigin bool, ttsUsage *qwenTTSUsageFixture) {
	t.Helper()
	upstreamProvider, origin, upstreamPath := "openai", "https://api.openai.com", "/v1/images/generations"
	bundlePath, executor := "capability-bundles/openai-image-flare-job.json", modelqualification.MediaImageExecutorContractV1
	contextLimit, side, unitCount := 32000, 1024, 4
	isQwen := endpointprotocol.IsDashScopeQwenImageModel(upstreamModel)
	isMiniMax := upstreamModel == endpointprotocol.MiniMaxH3Image
	isWan := upstreamModel == endpointprotocol.DashScopeWanVideo
	isH3Video := upstreamModel == endpointprotocol.MiniMaxH3Video
	isCog := upstreamModel == endpointprotocol.GLMCogVideo
	isGLMImage := upstreamModel == endpointprotocol.GLMImage
	isGLMOCR := upstreamModel == endpointprotocol.GLMOCR
	isOCR := isGLMOCR || upstreamModel == endpointprotocol.DeepSeekOCR
	isMiniMaxTTS := upstreamModel == endpointprotocol.MiniMaxTTS
	isASR := slices.Contains([]string{endpointprotocol.DeepSeekASR, endpointprotocol.MiniMaxASR, endpointprotocol.GLMASR, endpointprotocol.OpenAITranscribe, endpointprotocol.MiMoASR, endpointprotocol.DashScopeASR}, upstreamModel)
	isTTS := slices.Contains([]string{endpointprotocol.DeepSeekTTS, endpointprotocol.MiniMaxTTS, endpointprotocol.GLMTTS, endpointprotocol.MiMoTTS, endpointprotocol.DashScopeTTS}, upstreamModel)
	isSpeech := isASR || isTTS
	isVideo := isWan || isH3Video || isCog
	isDashScopeTTS := upstreamModel == endpointprotocol.DashScopeTTS
	hasRemoteSyncAsset := isGLMImage || isDashScopeTTS
	isRemote := isVideo || hasRemoteSyncAsset
	if isQwen {
		upstreamProvider, origin, upstreamPath = "dashscope", "https://workspace-test.cn-beijing.maas.aliyuncs.com", endpointprotocol.DashScopeQwenImageEndpoint
		bundlePath, executor = "capability-bundles/dashscope-beijing-qwen-image-jobs.json", modelqualification.MediaQwenImageExecutorContractV1
		contextLimit, side, unitCount = endpointprotocol.DashScopeQwenMaxPromptBytes, 1024, 2
	} else if isMiniMax {
		upstreamProvider, origin = "minimax", endpointprotocol.MiniMaxChinaImageOrigin
		bundlePath, executor = "capability-bundles/minimax-china-h3-image-job.json", modelqualification.MediaMiniMaxImageExecutorContractV1
		contextLimit, side, unitCount = endpointprotocol.MiniMaxImagePromptBytes, 1024, 5
	} else if isWan {
		upstreamProvider, origin, upstreamPath = "dashscope", "https://workspace-test.cn-beijing.maas.aliyuncs.com", endpointprotocol.DashScopeWanVideoEndpoint
		bundlePath, executor = "capability-bundles/dashscope-beijing-wan-video-job.json", modelqualification.MediaVideoExecutorContractV1
		contextLimit, unitCount = endpointprotocol.DashScopeWanMaxPromptBytes, 2
	} else if isH3Video {
		upstreamProvider, origin, upstreamPath = "minimax", endpointprotocol.MiniMaxChinaImageOrigin, endpointprotocol.MiniMaxH3VideoEndpoint
		bundlePath, executor = "capability-bundles/minimax-china-h3-video-job.json", modelqualification.MediaVideoExecutorContractV1
		contextLimit, unitCount = endpointprotocol.MiniMaxH3VideoPromptBytes, 3
	} else if isCog {
		upstreamProvider, origin, upstreamPath = "glm", endpointprotocol.GLMChinaMediaOrigin, endpointprotocol.GLMCogVideoEndpoint
		bundlePath, executor = "capability-bundles/glm-china-cogvideox-3-video-job.json", modelqualification.MediaVideoExecutorContractV1
		contextLimit, unitCount = endpointprotocol.GLMCogVideoPromptBytes, 0
	} else if isGLMImage {
		upstreamProvider, origin, upstreamPath = "glm", endpointprotocol.GLMChinaMediaOrigin, endpointprotocol.GLMImageEndpoint
		bundlePath, executor = "capability-bundles/glm-china-image-job.json", modelqualification.MediaGLMImageExecutorContractV1
		contextLimit, side, unitCount = endpointprotocol.GLMImagePromptBytes, 1280, 0
	} else if isOCR {
		upstreamProvider, origin, upstreamPath = "deepseek", endpointprotocol.DeepSeekOCROrigin, endpointprotocol.DeepSeekOCREndpoint
		bundlePath, executor = "capability-bundles/deepseek-ocr-job.json", modelqualification.MediaOCRExecutorContractV1
		contextLimit, unitCount = endpointprotocol.MaxOCRPNGBytes, 1
		if isGLMOCR {
			upstreamProvider, origin, upstreamPath = "glm", endpointprotocol.GLMChinaMediaOrigin, endpointprotocol.GLMOCREndpoint
			bundlePath, unitCount = "capability-bundles/glm-china-ocr-job.json", 2
		}
	} else if isASR {
		upstreamProvider, origin, upstreamPath = "deepseek", endpointprotocol.DeepSeekOCROrigin, endpointprotocol.DeepSeekASREndpoint
		bundlePath, executor = "capability-bundles/deepseek-asr-job.json", modelqualification.MediaSpeechExecutorContractV1
		contextLimit, unitCount = endpointprotocol.MaxASRWAVBytes, 1
		switch upstreamModel {
		case endpointprotocol.MiniMaxASR:
			upstreamProvider, origin, upstreamPath = "minimax", endpointprotocol.MiniMaxChinaImageOrigin, endpointprotocol.MiniMaxASREndpoint
			bundlePath = "capability-bundles/minimax-china-asr-job.json"
		case endpointprotocol.GLMASR:
			upstreamProvider, origin, upstreamPath = "glm", endpointprotocol.GLMChinaMediaOrigin, endpointprotocol.GLMASREndpoint
			bundlePath, unitCount = "capability-bundles/glm-china-asr-job.json", 0
		case endpointprotocol.OpenAITranscribe:
			upstreamProvider, origin, upstreamPath = "openai", "https://api.openai.com", endpointprotocol.OpenAITranscribeEndpoint
			bundlePath, unitCount = "capability-bundles/openai-transcribe-job.json", 4
		case endpointprotocol.MiMoASR:
			upstreamProvider, origin, upstreamPath = "mimo", endpointprotocol.MiMoSpeechOrigin, endpointprotocol.MiMoSpeechEndpoint
			bundlePath = "capability-bundles/mimo-china-asr-job.json"
		case endpointprotocol.DashScopeASR:
			upstreamProvider, origin, upstreamPath = "dashscope", "https://workspace-test.cn-beijing.maas.aliyuncs.com", endpointprotocol.DashScopeASREndpoint
			bundlePath, unitCount = "capability-bundles/dashscope-beijing-asr-job.json", 3
		}
	} else if isTTS {
		upstreamProvider, origin, upstreamPath = "deepseek", endpointprotocol.DeepSeekOCROrigin, endpointprotocol.DeepSeekTTSEndpoint
		bundlePath, executor = "capability-bundles/deepseek-tts-job.json", modelqualification.MediaSpeechExecutorContractV1
		contextLimit, unitCount = endpointprotocol.MediaTTSMaxTextBytes, 0
		switch upstreamModel {
		case endpointprotocol.GLMTTS:
			upstreamProvider, origin, upstreamPath = "glm", endpointprotocol.GLMChinaMediaOrigin, endpointprotocol.GLMTTSEndpoint
			bundlePath = "capability-bundles/glm-china-tts-job.json"
		case endpointprotocol.MiMoTTS:
			upstreamProvider, origin, upstreamPath = "mimo", endpointprotocol.MiMoSpeechOrigin, endpointprotocol.MiMoSpeechEndpoint
			bundlePath, unitCount = "capability-bundles/mimo-china-tts-job.json", 2
		case endpointprotocol.DashScopeTTS:
			upstreamProvider, origin, upstreamPath = "dashscope", "https://workspace-test.cn-beijing.maas.aliyuncs.com", endpointprotocol.DashScopeTTSEndpoint
			bundlePath, unitCount = "capability-bundles/dashscope-beijing-tts-job.json", 2
		case endpointprotocol.MiniMaxTTS:
			upstreamProvider, origin, upstreamPath = "minimax", endpointprotocol.MiniMaxChinaImageOrigin, endpointprotocol.MiniMaxTTSEndpoint
			bundlePath = "capability-bundles/minimax-china-tts-job.json"
			unitCount = 1
		}
	}
	ctx := t.Context()
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.CustomerStandardPricingHead)
	dir := testutil.MigrationsDir(t)
	ledger, err := schemaauthority.ExpectedLedger(dir)
	require.NoError(t, err)
	require.NoError(t, migrate.RunExisting(ctx, pool, dir))
	if upstreamModel == modelprofile.OpenAIImageFlare {
		checkCanonicalMediaRollback(t, pool, dir, "", "")
	}
	manifest := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
	manifestBody, err := manifest.CanonicalBytes()
	require.NoError(t, err)
	manifestSHA, err := manifest.Digest()
	require.NoError(t, err)
	artifact, instance := "sha256:"+strings.Repeat("a", 64), "uri_"+strings.Repeat("b", 32)
	_, err = pool.Exec(ctx, "INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes) VALUES($1,$2,$3)", artifact, manifestSHA, manifestBody)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at)
VALUES($1,$2,$3,$4,'serving','receiving',clock_timestamp()+interval '1 hour')`, instance, artifact, runtimegeneration.CompatibilityEpoch, schemaauthority.LatestSupportedMigration)
	require.NoError(t, err)
	authority := publication.AuthorityV2{Environment: "integration", ArtifactMigrationLedger: ledger,
		Publisher: publication.PublisherIdentityV2{Artifact: publication.ArtifactIdentity{
			Kind: "source", Commit: strings.Repeat("a", 40), Tree: strings.Repeat("b", 40), Version: "test", PublisherExecutableDigest: strings.Repeat("c", 64),
		}},
	}
	publish := func(req publication.PlanRequestV2) {
		t.Helper()
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		require.NoError(t, err)
		plan, planErr := publication.BuildPlanV2(ctx, tx, authority, req)
		require.NoError(t, tx.Rollback(ctx))
		require.NoError(t, planErr)
		result, err := publication.ApplyV2(ctx, pool, authority, req, plan)
		require.NoError(t, err)
		require.Equal(t, "committed", result.Status)
	}
	publish(publication.PlanRequestV2{PublicationID: "media-legacy-import", Kind: "legacy_import"})
	wire, err := os.ReadFile("../../" + bundlePath)
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	var definition modelprofile.Definition
	for _, raw := range bundle.Definitions {
		candidate, err := modelprofile.DecodeDefinition(raw)
		require.NoError(t, err)
		if slices.Contains(candidate.Profile().UpstreamModelIDs, upstreamModel) {
			definition = candidate
		}
	}
	require.NotEmpty(t, definition.Digest())
	authority.ApprovedBundleDigest = fmt.Sprintf("%x", sha256.Sum256(wire))
	authority.Publisher.Carrier = &publication.CarrierIdentityV2{Repository: "fixture/repository", Commit: strings.Repeat("f", 40), Tree: strings.Repeat("a", 40), Path: bundlePath, WorkflowRunID: 1, ArtifactID: 2}
	authority.ValidateBinding = func(publication.DefinitionBindingV2, modelprofile.Definition) error {
		return errors.New("media operation evidence is not a capability binding")
	}
	publish(publication.PlanRequestV2{PublicationID: "media-definition", Kind: "delta", BundleBytes: wire})

	var org, actor, operator, approver int64
	require.NoError(t, pool.QueryRow(ctx, "INSERT INTO orgs(name,status,org_type) VALUES('media-customer','active','customer') RETURNING id").Scan(&org))
	require.NoError(t, pool.QueryRow(ctx, "INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,'media-customer','fixture','org_admin','org_admin','active') RETURNING id", org).Scan(&actor))
	require.NoError(t, pool.QueryRow(ctx, "INSERT INTO orgs(name,status,org_type) VALUES('media-platform','active','platform') RETURNING id").Scan(&operator))
	require.NoError(t, pool.QueryRow(ctx, "INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,'media-platform','fixture','platform_admin','platform_admin','active') RETURNING id", operator).Scan(&approver))
	customer := canonicalApplicationAuthorizedRequest(org, actor)
	platform := canonicalOperationProbeAuthorizedRequest(operator, org, approver, auth.RolePlatformAdmin)
	const fixtureSecret = "synthetic-canonical-media-test-secret"
	enc := []byte(strings.Repeat("e", 32))
	h := NewHandlerWithAPIKeyHashSecret(pool, "media-fixture", fixtureSecret, enc, nil)
	const model = "fixture-image"
	const operation = string(modeloperation.IDMediaJobs)
	adapterConfig := json.RawMessage(`{"endpoint_path_mode":"canonical_v1"}`)
	if (isOCR || isSpeech) && upstreamProvider == "deepseek" {
		adapterConfig = json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)
	}
	app := createCanonicalApplicationForTest(t, h, customer, canonicalModelApplicationCreateRequest{
		Connection: canonicalConnectionInput{Name: "canonical-image", Provider: upstreamProvider, BaseURL: origin,
			AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: adapterConfig},
		PublicModel: canonicalPublicModelInput{ModelName: model},
		Deployment:  canonicalDeploymentInput{UpstreamModel: upstreamModel, ProfileDefinitionDigest: definition.Digest()},
		Credential:  canonicalCredentialInput{APIKey: "synthetic-canonical-image-key"},
		Operation:   canonicalOperationInput{OperationID: operation},
	}, http.StatusCreated).Application
	bindingID := app.Credential.Bindings[0].ID
	if mismatchedOrigin {
		_, err := pool.Exec(ctx, `UPDATE provider_connections SET base_url='https://wrong.example.invalid',config_revision=config_revision+1 WHERE id=$1`, app.Connection.ID)
		require.NoError(t, err)
	}
	projected := checkOperationProbeAvailability(t, h, customer, app.Deployment.ID, operation, !mismatchedOrigin, isVideo && !mismatchedOrigin)
	if mismatchedOrigin {
		return
	}
	if isVideo && !expireProbe {
		if dir := os.Getenv("CANONICAL_MEDIA_SOURCE_EVIDENCE_DIR"); dir != "" {
			dir = filepath.Join(dir, upstreamModel)
			require.NoError(t, os.MkdirAll(dir, 0700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "operation-probe-detail.json"), projected, 0600))
		}
	}
	tile := image.NewRGBA(image.Rect(0, 0, side, side))
	tile.Set(1, 0, color.RGBA{R: 40, G: 90, B: 240, A: 255})
	var artifactImage bytes.Buffer
	require.NoError(t, png.Encode(&artifactImage, tile))
	responseWire := fmt.Sprintf(`{"created":1,"data":[{"b64_json":%q}],"usage":{"input_tokens":50,"input_tokens_details":{"text_tokens":50,"image_tokens":0},"output_tokens":50,"total_tokens":100}}`, base64.StdEncoding.EncodeToString(artifactImage.Bytes()))
	if isQwen {
		responseWire = fmt.Sprintf(`{"created":1,"data":[{"b64_json":%q}],"usage":{"input_image_count":0,"output_image_count":1,"output_height":1024,"output_width":1024}}`, base64.StdEncoding.EncodeToString(artifactImage.Bytes()))
	} else if isMiniMax {
		responseWire = fmt.Sprintf(`{"created":1,"output_format":"png","data":[{"b64_json":%q,"size":"1024x1024"}],"usage":{"generated_images":1,"input_tokens":25,"output_tokens":50,"total_tokens":75,"input_tokens_details":{"text_tokens":25,"image_tokens":0},"output_tokens_details":{"image_tokens":50}}}`, base64.StdEncoding.EncodeToString(artifactImage.Bytes()))
	}
	artifactBytes, artifactMIME := artifactImage.Bytes(), "image/png"
	var ocrPNG, audioFixture []byte
	if isSpeech {
		file := "../endpointprotocol/testdata/deepseek-speech-synthetic.wav"
		if isMiniMaxTTS {
			file = "../endpointprotocol/testdata/minimax-speech-synthetic.wav"
		}
		if isDashScopeTTS {
			file = "../endpointprotocol/testdata/dashscope-speech-synthetic.wav"
		}
		audioFixture, err = os.ReadFile(file)
		require.NoError(t, err)
	}
	if isASR {
		artifactBytes, artifactMIME = []byte("2947"), "text/plain; charset=utf-8"
		responseWire = asrFixtureResult("2947", 2496, upstreamModel)
	}
	if isTTS {
		artifactBytes, artifactMIME = audioFixture, "audio/wav"
		responseWire = string(audioFixture)
		if isMiniMaxTTS {
			responseWire = fmt.Sprintf(`{"data":{"audio":%q,"status":2},"extra_info":{"usage_characters":19,"word_count":47,"audio_sample_rate":32000,"audio_format":"wav","audio_size":%d,"audio_channel":1},"base_resp":{"status_code":0}}`, hex.EncodeToString(audioFixture), len(audioFixture))
		}
		if upstreamModel == endpointprotocol.MiMoTTS {
			responseWire = fmt.Sprintf(`{"id":"mimo-speech-fixture","created":1,"model":"mimo-v2.5-tts","object":"chat.completion","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"","audio":{"data":%q}}}],"usage":{"prompt_tokens":213,"completion_tokens":97,"total_tokens":310}}`, base64.StdEncoding.EncodeToString(audioFixture))
		}
	}
	if isOCR {
		ocrPNG, err = os.ReadFile("../endpointprotocol/testdata/deepseek-ocr-synthetic.png")
		require.NoError(t, err)
		artifactBytes, artifactMIME = []byte("MAPLE 5826"), "text/plain; charset=utf-8"
		responseWire = ocrFixtureResult(string(artifactBytes), isGLMOCR)
	}
	providerTask, statusPrefix := "wan-fixture-original", "/api/v1/tasks/"
	providerAsset := "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/synthetic.mp4"
	providerAccepted := fmt.Sprintf(`{"request_id":"fixture","output":{"task_id":%q,"task_status":"PENDING"}}`, providerTask)
	// The original documented shape uses a five-second example. This is
	// a synthetic two-second 480P response and independently generated MP4.
	providerComplete := fmt.Sprintf(`{"request_id":"fixture","output":{"task_id":%q,"task_status":"SUCCEEDED","video_url":%q},"usage":{"video_count":1,"duration":2.000,"input_video_duration":0,"output_video_duration":2,"SR":480,"served_model":"wan3.0-video"}}`, providerTask, providerAsset)
	if isGLMImage {
		providerAsset = "https://sfile.chatglm.cn/synthetic.png"
		responseWire = fmt.Sprintf(`{"created":1,"data":[{"url":%q}]}`, providerAsset)
	}
	if isDashScopeTTS {
		providerAsset = "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/synthetic.wav?audience=fixture"
		replyAsset := strings.Replace(providerAsset, "https://", "http://", 1)
		usage := `{"input_tokens":12,"output_tokens":16,"total_tokens":28}`
		if ttsUsage != nil {
			usage, unitCount = ttsUsage.usage, len(ttsUsage.expected)
		}
		responseWire = fmt.Sprintf(`{"request_id":"dashscope-tts-fixture","output":{"finish_reason":"stop","audio":{"url":%q,"data":"","id":"audio-fixture","expires_at":2000000000}},"usage":%s}`, replyAsset, usage)
	}
	if isVideo {
		assetFixture := "dashscope-wan-synthetic-480p.mp4"
		if isH3Video {
			assetFixture = "minimax-h3-synthetic-768p.mp4"
			providerTask, statusPrefix = "h3-fixture-original", endpointprotocol.MiniMaxH3VideoStatusPrefix
			providerAsset = "https://cdn.hailuoai.com/synthetic.mp4"
			providerAccepted = fmt.Sprintf(`{"task_id":%q}`, providerTask)
			providerComplete = fmt.Sprintf(`{"task":{"id":%q,"model":"MiniMax-H3","status":"succeeded","content":{"url":%q},"resolution":"768P","duration":4,"modality":"video","task_type":"generation","usage":{"total_seconds":4,"input_seconds":0,"output_seconds":4,"input_image_count":0,"input_audio_seconds":0}}}`, providerTask, providerAsset)
		}
		if isCog {
			assetFixture = "glm-cogvideox3-synthetic-720p.mp4"
			providerTask, statusPrefix = "cog-fixture-original", endpointprotocol.GLMCogVideoStatusPrefix
			providerAsset = "https://sfile.chatglm.cn/synthetic.mp4"
			providerAccepted = fmt.Sprintf(`{"id":%q,"model":"cogvideox-3","task_status":"PROCESSING","request_id":"create-receipt"}`, providerTask)
			providerComplete = fmt.Sprintf(`{"model":"cogvideox-3","task_status":"SUCCESS","request_id":"query-receipt","video_result":[{"url":%q}]}`, providerAsset)
		}
		artifactBytes, err = os.ReadFile("../endpointprotocol/testdata/" + assetFixture)
		require.NoError(t, err)
		if isWan {
			require.Equal(t, "6302e61e4ea3a476dd2c7c33de6d646a9ff43dda8df29f6251684cf33355a8ef", fmt.Sprintf("%x", sha256.Sum256(artifactBytes)))
		}
		artifactMIME = "video/mp4"
	}
	probeCalls := 0
	var probeClock time.Duration
	h.asyncMediaProbeNow = func() time.Time { return time.Now().Add(probeClock) }
	interruptProbeRead := isVideo
	wrongSpeechAnswer := false
	h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(req *http.Request) (*http.Response, error) {
		if hasRemoteSyncAsset && req.Method == http.MethodGet {
			require.Equal(t, providerAsset, req.URL.String())
			require.Empty(t, req.Header.Get("Authorization"))
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(artifactBytes))}, nil
		}
		if isVideo && req.Method == http.MethodGet {
			if req.URL.String() == providerAsset {
				require.Empty(t, req.Header.Get("Authorization"))
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(artifactBytes)), Header: http.Header{"Content-Type": []string{"video/mp4"}}}, nil
			}
			require.Equal(t, origin+statusPrefix+providerTask, req.URL.String())
			require.Equal(t, "Bearer synthetic-canonical-image-key", req.Header.Get("Authorization"))
			if interruptProbeRead {
				interruptProbeRead = false
				return nil, context.Canceled
			}
			return nativeClientToolHTTPResponse(providerComplete), nil
		}
		probeCalls++
		require.Equal(t, origin+upstreamPath, req.URL.String())
		require.Equal(t, http.MethodPost, req.Method)
		require.Nil(t, req.GetBody)
		if isOCR {
			pngBytes, err := readOCRFixtureInput(req, isGLMOCR)
			require.NoError(t, err)
			code, err := recoverOCRGlyphFixture(pngBytes)
			require.NoError(t, err)
			return nativeClientToolHTTPResponse(ocrFixtureResult(code, isGLMOCR)), nil
		}
		if isASR {
			audio, err := readASRFixtureInput(req, upstreamModel)
			require.NoError(t, err)
			code, err := recoverASRFixtureDigits(audio)
			require.NoError(t, err)
			info, ok := endpointprotocol.ValidatePCM16WAV(audio)
			require.True(t, ok)
			if wrongSpeechAnswer {
				code = strings.Repeat("1", asrProbeDigitCount)
			}
			return nativeClientToolHTTPResponse(asrFixtureResult(code, info.DurationMS, upstreamModel)), nil
		}
		var request map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&request))
		if isTTS {
			require.True(t, validTTSFixtureRequest(request, "Hello from UniRouter.", upstreamModel))
			return nativeClientToolHTTPResponse(responseWire), nil
		}
		require.Equal(t, upstreamModel, request["model"])
		if isWan {
			require.Equal(t, "enable", req.Header.Get("X-DashScope-Async"))
			params, ok := request["parameters"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "480P", params["resolution"])
			require.EqualValues(t, 2, params["duration"])
			return nativeClientToolHTTPResponse(providerAccepted), nil
		}
		if isH3Video {
			require.Equal(t, "768P", request["resolution"])
			require.EqualValues(t, 4, request["duration"])
			require.Equal(t, false, request["generate_audio"])
			require.Equal(t, "task", request["response_format"])
			require.Len(t, request["content"], 1)
			return nativeClientToolHTTPResponse(providerAccepted), nil
		}
		if isCog {
			require.Equal(t, "1280x720", request["size"])
			require.EqualValues(t, 5, request["duration"])
			require.EqualValues(t, 30, request["fps"])
			require.Equal(t, false, request["with_audio"])
			require.Equal(t, "speed", request["quality"])
			return nativeClientToolHTTPResponse(providerAccepted), nil
		}
		if isGLMImage {
			require.Equal(t, "1280x1280", request["size"])
			require.NotContains(t, request, "output_format")
			return nativeClientToolHTTPResponse(responseWire), nil
		}
		require.EqualValues(t, 1, request["n"])
		if isQwen {
			require.Equal(t, "b64_json", request["response_format"])
			require.Equal(t, false, request["prompt_extend"])
			require.NotContains(t, request, "quality")
		} else if isMiniMax {
			require.Equal(t, "b64_json", request["response_format"])
			require.Equal(t, "png", request["output_format"])
			require.Equal(t, true, request["disable_prompt_enhanced"])
			require.NotContains(t, request, "quality")
		} else {
			require.Equal(t, "low", request["quality"])
		}
		return nativeClientToolHTTPResponse(responseWire), nil
	}))
	serveProbe := func() canonicalOperationProbeResponse {
		t.Helper()
		if !isVideo {
			return serveCanonicalOperationProbeForTest(t, h, customer, app.Deployment.ID, operation, bindingID, "", http.StatusOK)
		}
		n := probeCalls
		accepted := serveCanonicalOperationProbeForTest(t, h, customer, app.Deployment.ID, operation, bindingID, "", http.StatusAccepted)
		require.True(t, accepted.Pending)
		require.Equal(t, "provider_task_accepted", accepted.ReasonCode)
		require.False(t, accepted.Success)
		require.NotEmpty(t, accepted.ProbeRunRef)
		discoveryPath := fmt.Sprintf("/api/v1/model-deployments/%d/operations/%s/probes?credential_binding_id=%d", app.Deployment.ID, operation, bindingID)
		discovery := customer(http.MethodGet, discoveryPath, "")
		discovery.SetPathValue("deploymentID", strconv.FormatInt(app.Deployment.ID, 10))
		discovery.SetPathValue("operationID", operation)
		discoveryRecorder := httptest.NewRecorder()
		h.ReadCanonicalAsyncModelOperationProbe(discoveryRecorder, discovery)
		require.Equal(t, http.StatusAccepted, discoveryRecorder.Code, discoveryRecorder.Body.String())
		require.Contains(t, discoveryRecorder.Body.String(), accepted.ProbeRunRef)
		require.NotContains(t, discoveryRecorder.Body.String(), providerTask)
		require.Equal(t, n+1, probeCalls)
		read := func(status int) (canonicalOperationProbeResponse, *httptest.ResponseRecorder) {
			path := fmt.Sprintf("/api/v1/model-deployments/%d/operations/%s/probes/%s?credential_binding_id=%d", app.Deployment.ID, operation, accepted.ProbeRunRef, bindingID)
			req := customer(http.MethodGet, path, "")
			req.SetPathValue("deploymentID", strconv.FormatInt(app.Deployment.ID, 10))
			req.SetPathValue("operationID", operation)
			req.SetPathValue("probeRunRef", accepted.ProbeRunRef)
			rec := httptest.NewRecorder()
			h.ReadCanonicalAsyncModelOperationProbe(rec, req)
			require.Equal(t, status, rec.Code, rec.Body.String())
			var response canonicalOperationProbeResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			require.Equal(t, accepted.ProbeRunRef, response.ProbeRunRef)
			require.NotContains(t, rec.Body.String(), providerTask)
			require.NotContains(t, rec.Body.String(), providerAsset)
			return response, rec
		}
		replayed := serveCanonicalOperationProbeForTest(t, h, customer, app.Deployment.ID, operation, bindingID, "", http.StatusAccepted)
		require.Equal(t, accepted.ProbeRunRef, replayed.ProbeRunRef, "retrying after a missing response finds the retained reference")
		require.Equal(t, n+1, probeCalls, "duplicate POST must not submit or query the provider")
		if expireProbe {
			probeClock += endpointprotocol.MediaTaskReadWindow + time.Second
			terminal, _ := read(http.StatusOK)
			require.False(t, terminal.Success)
			require.True(t, terminal.Stale)
			require.False(t, terminal.Current)
			require.Zero(t, terminal.ProbeID)
			require.Equal(t, "provider_task_read_window_expired", terminal.ReasonCode)
			repeated := serveCanonicalOperationProbeForTest(t, h, customer, app.Deployment.ID, operation, bindingID, "", http.StatusOK)
			require.Equal(t, terminal, repeated)
			require.Equal(t, n+1, probeCalls)
			return terminal
		}
		waiting, waitResponse := read(http.StatusAccepted)
		require.Equal(t, "15", waitResponse.Header().Get("Retry-After"))
		require.Equal(t, "provider_task_not_due", waiting.ReasonCode)
		probeClock += 150 * time.Second
		if n > 0 {
			// An operator opening a fresh page twelve minutes after submission
			// can still continue the same accepted task after discovery.
			probeClock += 10 * time.Minute
		}
		if n == 0 {
			interrupted, _ := read(http.StatusAccepted)
			require.Equal(t, "provider_task_read_unavailable", interrupted.ReasonCode)
			probeClock += endpointprotocol.MediaTaskPollInterval + time.Second
		}
		final, _ := read(http.StatusOK)
		require.True(t, final.Success)
		if n == 0 {
			if dir := os.Getenv("CANONICAL_MEDIA_SOURCE_EVIDENCE_DIR"); dir != "" {
				dir = filepath.Join(dir, upstreamModel)
				require.NoError(t, os.MkdirAll(dir, 0700))
				wire, err := json.MarshalIndent(map[string]any{"start": accepted, "discover": json.RawMessage(discoveryRecorder.Body.Bytes()), "not_due": waiting, "terminal": final}, "", "  ")
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(dir, "operation-probe-wire.json"), wire, 0600))
			}
		}
		recovered, _ := read(http.StatusOK)
		require.Equal(t, final, recovered)
		require.Equal(t, n+1, probeCalls, "resume must issue no new paid POST")
		return final
	}
	expectedProbeCalls := 2
	if isASR {
		// Both protocols must reject a valid transcription envelope with a
		// fixed answer unrelated to the waveform.
		wrongSpeechAnswer = true
		rejected := serveProbe()
		require.False(t, rejected.Success)
		require.False(t, rejected.OperationAdvanced)
		wrongSpeechAnswer = false
		expectedProbeCalls++
	}
	probe := serveProbe()
	if expireProbe {
		return
	}
	require.True(t, probe.Success, "%+v", probe)
	var connectionRevision, deploymentRevision, operationRevision int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT connection.config_revision,deployment.config_revision,operation.revision
  FROM model_deployments deployment JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN model_deployment_operations operation ON operation.deployment_id=deployment.id
 WHERE deployment.id=$1 AND operation.operation_id=$2`, app.Deployment.ID, operation).Scan(&connectionRevision, &deploymentRevision, &operationRevision))
	payload, err := json.Marshal(modelOperationProductionApprovalRequest{
		ProbeID: probe.ProbeID, ExpectedConnectionRevision: connectionRevision, ExpectedDeploymentRevision: deploymentRevision,
		ExpectedOperationRevision: operationRevision, ExpectedExecutorContractVersion: executor,
		Reason: "exact one image fixture",
	})
	require.NoError(t, err)
	r := platform(http.MethodPost, "/api/v1/model-deployments/production-approvals", string(payload))
	r.SetPathValue("deploymentID", strconv.FormatInt(app.Deployment.ID, 10))
	r.SetPathValue("operationID", operation)
	approved := httptest.NewRecorder()
	h.ApproveCanonicalModelOperationProduction(approved, r)
	require.Equal(t, http.StatusCreated, approved.Code, approved.Body.String())
	current := serveProbe()
	require.True(t, current.Success && current.Current && !current.OperationAdvanced, "%+v", current)
	require.Equal(t, expectedProbeCalls, probeCalls)

	legacyRouter, err := gateway.NewRouterWithPolicy(pool, routingpolicy.Default())
	require.NoError(t, err)
	manager, err := runtimechange.NewManager(pool, legacyRouter, routingpolicy.Default())
	require.NoError(t, err)
	h.SetRuntimeChangeManager(manager)
	author := func(baseline runtimechange.DeploymentReleaseBaseline, key string) runtimechange.DeploymentRelease {
		t.Helper()
		receipt, _ := serveCanonicalDeploymentReleaseAuthoring(t, h, customer, canonicalDeploymentReleaseAuthoringRequest{
			PublicModelID: app.PublicModel.ID, Baseline: baseline,
			Selections:              []runtimechange.DeploymentReleaseSelection{{DeploymentID: app.Deployment.ID, OperationID: operation}},
			ProfileDefinitionDigest: definition.Digest(), PublicContextLimit: contextLimit, PublicOutputLimit: 1,
			Reason: "seal exact media operation fixture",
		}, key, http.StatusCreated)
		return receipt.Release
	}
	initial := author(runtimechange.DeploymentReleaseBaseline{Kind: runtimechange.DeploymentReleaseBaselineInitial}, "media-freeze")
	activated, _ := serveCanonicalLifecycleMutation(t, h, customer, initial.ReleaseRef, "activate-initial",
		fmt.Sprintf(`{"expected_release_version":%d,"reason":"media fixture"}`, initial.Version),
		"media-activate", h.ActivateInitialCanonicalDeploymentLifecycle, http.StatusOK)
	require.Equal(t, expectedProbeCalls, probeCalls, "no additional provider work during release creation or activation")

	var project, keyID, foreignProject, foreignKeyID int64
	projectKeys := make(map[int64]string, 2)
	seedProject := func(name string, projectID, apiKeyID *int64) {
		t.Helper()
		require.NoError(t, pool.QueryRow(ctx, "INSERT INTO projects(org_id,name,slug,status,budget_period) VALUES($1,$2,$2,'active','none') RETURNING id", org, name).Scan(projectID))
		require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO api_keys(org_id,project_id,name,key_hash,key_prefix,status,allowed_models)
VALUES($1,$2,$3,$4,$3,'active',$5) RETURNING id`, org, *projectID, name, auth.HashAPIKey(name, fixtureSecret), model).Scan(apiKeyID))
		projectKeys[*apiKeyID] = name
	}
	seedProject("image-project", &project, &keyID)
	seedProject("other-project", &foreignProject, &foreignKeyID)
	planner, err := modelrouting.NewPlanner(pool, routingpolicy.Default())
	require.NoError(t, err)
	coordinator, err := modelexecution.New(pool, billing.NewBilling(pool, nil), modelexecution.DefaultConfig())
	require.NoError(t, err)
	routes := &mediajob.CanonicalRoutes{Planner: planner, Coordinator: coordinator}
	evaluator, err := modelauthz.NewEvaluator(pool, "integration")
	require.NoError(t, err)
	store, err := mediajob.NewPostgresStore(pool, mediajob.NewExecutionAuthorizer(evaluator))
	require.NoError(t, err)
	store.SetCanonicalRoutes(routes)
	require.NoError(t, store.CheckWorkerAuthority(ctx))
	source, err := sourcefacts.New(pool, sourcefacts.Options{Enabled: true, ObservationsEnabled: true, SourceInstance: "canonical-media-test", CursorSecret: fixtureSecret, I01Enabled: true})
	require.NoError(t, err)
	store.SetSourceMaterializer(source)
	require.NoError(t, source.CheckReady(ctx))
	readSource := canonicalMediaSourceReader(t, h, source, pool, operator, approver, org, project)
	capabilities := readSource("capabilities", "", "")
	require.Contains(t, string(capabilities), sourcefacts.MediaChargeProfile)
	runtimeInitial := readSource("events", sourcefacts.RuntimeProfile, "")
	chargeInitial := readSource("events", sourcefacts.MediaChargeProfile, "")
	cipher, err := mediajob.NewApplicationCipher(enc)
	require.NoError(t, err)
	objects := &canonicalMediaObjectFixture{results: map[string]mediajob.ProviderResult{}}
	limits := canonicalImageLimitFixture{}
	service, err := mediajob.NewService(store, objects, cipher, mediajob.NewSubmissionAuthorizer(pool, evaluator), []byte(fixtureSecret), mediajob.ServiceOptions{RuntimeEnabled: true, AdmissionEnabled: true, CanonicalRoutes: routes, LimitPolicy: limits})
	require.NoError(t, err)
	handler, err := mediajob.NewHTTPHandler(service, pool)
	require.NoError(t, err)
	mux := http.NewServeMux()
	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	projectAuth := middleware.GatewayAuthWithAgentCapabilitiesAndClientIP(pool, fixtureSecret, nil, resolver)
	budget := payloadpolicy.RequestMiddlewareWithMax(payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits()), mediajob.MaxRequestBytes, middleware.GetOrgIDFromContext)
	mux.Handle("POST /v1/media/jobs", middleware.GatewayAuthAcrossBufferedBody(projectAuth, budget)(middleware.CaptureExecutionContext(source)(http.HandlerFunc(handler.Submit))))
	mux.Handle("GET /v1/media/jobs/{id}", projectAuth(http.HandlerFunc(handler.Get)))
	mux.Handle("GET /v1/media/jobs/{id}/content", projectAuth(http.HandlerFunc(handler.Content)))
	mux.Handle("GET /api/v1/media-jobs/{id}/content", middleware.AdminAuth(pool, "media-fixture")(middleware.CustomerContext(pool)(http.HandlerFunc(handler.AdminContent))))
	projectHandler := middleware.OperationIdentityWithSecret(fixtureSecret)(mux)
	call := func(method, path string, apiKeyID int64, intent, prompt string) *httptest.ResponseRecorder {
		body := ""
		if prompt != "" {
			if isVideo {
				body = fmt.Sprintf(`{"model":%q,"operation":"text_to_video","prompt":%q}`, model, prompt)
			} else if isASR {
				data, err := endpointprotocol.TranscriptionRequestBody(model, audioFixture)
				require.NoError(t, err)
				body = string(data)
			} else if isTTS {
				body = fmt.Sprintf(`{"model":%q,"operation":"text_to_speech","prompt":"Two nine four seven."}`, model)
			} else if isOCR {
				data, err := endpointprotocol.DeepSeekOCRProbeBody(model, ocrPNG)
				require.NoError(t, err)
				body = string(data)
			} else if isGLMImage {
				body = fmt.Sprintf(`{"model":%q,"prompt":%q,"n":1}`, model, prompt)
			} else {
				body = fmt.Sprintf(`{"model":%q,"prompt":%q,"n":1,"size":"1024x1024","output_format":"png"}`, model, prompt)
			}
		}
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", intent)
		r.Header.Set(middleware.ChatUpstreamAttemptsHeader, "1")
		r.Header.Set("Authorization", "Bearer "+projectKeys[apiKeyID])
		w := httptest.NewRecorder()
		projectHandler.ServeHTTP(w, r)
		return w
	}
	var wrongDimensions bytes.Buffer
	require.NoError(t, png.Encode(&wrongDimensions, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	wrongImageWire := strings.Replace(responseWire, base64.StdEncoding.EncodeToString(artifactImage.Bytes()),
		base64.StdEncoding.EncodeToString(wrongDimensions.Bytes()), 1)

	assetURL, err := url.Parse(providerAsset)
	require.NoError(t, err)
	var posts, statusReads, assetReads atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hasRemoteSyncAsset && r.Method == http.MethodGet {
			assetReads.Add(1)
			if r.URL.Path != assetURL.Path || r.URL.RawQuery != assetURL.RawQuery || r.Header.Get("Authorization") != "" {
				http.Error(w, "wrong image fixture download", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", artifactMIME)
			_, _ = w.Write(artifactBytes)
			return
		}
		if isVideo && r.Method == http.MethodGet {
			switch r.URL.Path {
			case statusPrefix + providerTask:
				statusReads.Add(1)
				if r.Header.Get("Authorization") != "Bearer synthetic-canonical-image-key" {
					http.Error(w, "wrong fixture status identity", http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(providerComplete))
			case "/synthetic.mp4":
				assetReads.Add(1)
				if r.Header.Get("Authorization") != "" {
					http.Error(w, "credential on asset fetch", http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "video/mp4")
				_, _ = w.Write(artifactBytes)
			default:
				http.NotFound(w, r)
			}
			return
		}
		posts.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != upstreamPath || r.Header.Get("Authorization") != "Bearer synthetic-canonical-image-key" {
			http.Error(w, "unexpected outbound fixture", http.StatusBadRequest)
			return
		}
		if isOCR {
			file, err := readOCRFixtureInput(r, isGLMOCR)
			if err != nil || !bytes.Equal(file, ocrPNG) {
				http.Error(w, "wrong OCR fixture input", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(responseWire))
			return
		}
		if isASR {
			audio, err := readASRFixtureInput(r, upstreamModel)
			if err != nil || !bytes.Equal(audio, audioFixture) {
				http.Error(w, "wrong WAV fixture", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(responseWire))
			return
		}
		if isTTS {
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || !validTTSFixtureRequest(body, "Two nine four seven.", upstreamModel) {
				http.Error(w, "wrong synthesis fixture request", http.StatusBadRequest)
				return
			}
			mime := "audio/wav"
			if isMiniMaxTTS || isDashScopeTTS || upstreamModel == endpointprotocol.MiMoTTS {
				mime = "application/json"
			}
			w.Header().Set("Content-Type", mime)
			_, _ = w.Write([]byte(responseWire))
			return
		}
		var request struct {
			Prompt, Model         string
			Size                  string
			N                     int
			ResponseFormat        string                  `json:"response_format"`
			PromptExtend          *bool                   `json:"prompt_extend"`
			DisablePromptEnhanced bool                    `json:"disable_prompt_enhanced"`
			Content               []struct{ Text string } `json:"content"`
			Input                 struct{ Prompt string } `json:"input"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.Model != upstreamModel {
			http.Error(w, "wrong media model", http.StatusBadRequest)
			return
		}
		if isQwen && (request.N != 1 || request.Size != "1024x1024" || request.ResponseFormat != "b64_json" ||
			request.PromptExtend == nil || *request.PromptExtend) {
			http.Error(w, "wrong image wire controls", http.StatusBadRequest)
			return
		}
		if isMiniMax && (request.N != 1 || request.Size != "1024x1024" || request.ResponseFormat != "b64_json" ||
			!request.DisablePromptEnhanced) {
			http.Error(w, "wrong MiniMax image wire controls", http.StatusBadRequest)
			return
		}
		if isWan {
			request.Prompt = request.Input.Prompt
		}
		if isH3Video && len(request.Content) == 1 {
			request.Prompt = request.Content[0].Text
		}
		if request.Prompt == "lost response" {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		wire := responseWire
		if isVideo {
			wire = providerAccepted
		}
		if request.Prompt == "wrong dimensions" {
			wire = wrongImageWire
		}
		_, _ = w.Write([]byte(wire))
	}))
	t.Cleanup(upstream.Close)
	transport := upstream.Client().Transport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig.ServerName = "example.com"
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != strings.TrimPrefix(origin, "https://")+":443" &&
			!(isRemote && addr == assetURL.Hostname()+":443") {
			return nil, errors.New("fixture refuses any non-image address")
		}
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
	}
	provider, err := mediajob.NewGPTImageProvider(legacyRouter, enc, mediajob.GPTImageProviderOptions{HTTPClient: &http.Client{Transport: transport}})
	require.NoError(t, err)
	provider.SetSourceObserver(source)
	var clockOffset time.Duration
	clock := func() time.Time { return time.Now().Add(clockOffset) }
	worker, err := mediajob.NewWorker(store, objects, provider, cipher, mediajob.WorkerOptions{Owner: "media-worker", LimitPolicy: limits, Now: clock})
	require.NoError(t, err)
	run := func() error {
		t.Helper()
		didWork, err := worker.RunOnce(ctx)
		require.True(t, didWork)
		return err
	}
	submit := func(intent, prompt string) string {
		t.Helper()
		result := call(http.MethodPost, modeloperation.PathMediaJobs, keyID, intent, prompt)
		require.Equal(t, http.StatusAccepted, result.Code, result.Body.String())
		require.Equal(t, "1", result.Header().Get(middleware.ChatUpstreamAttemptsHeader))
		require.NotContains(t, result.Body.String(), "canonical_route")
		return result.Header().Get("Location")
	}
	path := submit("success", "canonical test tile")
	{
		job, err := store.Get(ctx, mediajob.Scope{OrgID: org, ProjectID: project}, strings.TrimPrefix(path, "/v1/media/jobs/"))
		require.NoError(t, err)
		require.NotNil(t, job.CanonicalRoute)
		require.Equal(t, initial.ID, job.CanonicalRoute.RuntimeReleaseID)
		require.Equal(t, bindingID, job.CanonicalRoute.CredentialBindingID)
	}
	require.NoError(t, run())
	require.EqualValues(t, 1, posts.Load())
	if isVideo {
		require.Zero(t, statusReads.Load(), "must persist receipt before starting status reads")
		require.Zero(t, assetReads.Load())
		var accepted string
		var attempt int
		jobID := strings.TrimPrefix(path, modeloperation.PathMediaJobs+"/")
		require.NoError(t, pool.QueryRow(ctx, `SELECT provider_task_id,attempt FROM async_media_jobs WHERE job_id=$1`, jobID).Scan(&accepted, &attempt))
		require.Equal(t, providerTask, accepted)
		require.Equal(t, 1, attempt)
		clockOffset += 12 * time.Minute
		worker, err = mediajob.NewWorker(store, objects, provider, cipher, mediajob.WorkerOptions{Owner: "media-worker-restarted", LimitPolicy: limits, Now: clock})
		require.NoError(t, err)
		require.NoError(t, run())
		require.EqualValues(t, 1, posts.Load())
		require.EqualValues(t, 1, statusReads.Load())
		require.EqualValues(t, 1, assetReads.Load())
	}
	if hasRemoteSyncAsset {
		require.EqualValues(t, 1, assetReads.Load())
		require.Zero(t, statusReads.Load())
	}
	content := call(http.MethodGet, path+"/content", keyID, "", "")
	require.Equal(t, http.StatusOK, content.Code, content.Body.String())
	require.Equal(t, artifactBytes, content.Body.Bytes())
	require.Equal(t, artifactMIME, content.Header().Get("Content-Type"))
	require.Equal(t, "private, no-store", content.Header().Get("Cache-Control"))
	require.Equal(t, http.StatusUnauthorized, call(http.MethodGet, path+"/content", 0, "", "").Code)
	completed, err := store.Get(ctx, mediajob.Scope{OrgID: org, ProjectID: project}, strings.TrimPrefix(path, modeloperation.PathMediaJobs+"/"))
	require.NoError(t, err)
	require.NotNil(t, completed.Output)
	require.Equal(t, fmt.Sprintf("sha256:%x", sha256.Sum256(content.Body.Bytes())), completed.Output.ContentSHA256)
	require.NotNil(t, completed.CanonicalDispatch)
	require.Equal(t, completed.CanonicalRoute.RuntimeReleaseID, completed.CanonicalDispatch.RuntimeReleaseID)
	require.Equal(t, completed.CanonicalRoute.CredentialBindingID, completed.CanonicalDispatch.CredentialBindingID)
	checkCanonicalMediaSourcePages(t, readSource, completed, unitCount, capabilities, runtimeInitial, chargeInitial, ttsUsage)
	require.Equal(t, http.StatusNotFound, call(http.MethodGet, path+"/content", foreignKeyID, "", "").Code)
	var viewer int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status)
		VALUES($1,'synthetic-image-viewer','fixture','member','viewer','active') RETURNING id`, org).Scan(&viewer))
	_, err = pool.Exec(ctx, `INSERT INTO project_members(project_id,user_id,role) VALUES($1,$2,'project_viewer')`, project, viewer)
	require.NoError(t, err)
	viewerJWT := testutil.MustAdminSessionJWT(t, ctx, pool, viewer, org, auth.RoleViewer, "media-fixture")
	readAsViewer := func(projectID int64) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/media-jobs/"+completed.JobID+"/content?project_id="+stringInt64(projectID), nil)
		r.Header.Set("Authorization", "Bearer "+viewerJWT)
		w := httptest.NewRecorder()
		projectHandler.ServeHTTP(w, r)
		return w
	}
	if isRemote || isOCR || isSpeech {
		if dir := os.Getenv("CANONICAL_MEDIA_SOURCE_EVIDENCE_DIR"); dir != "" {
			name := upstreamModel
			if ttsUsage != nil {
				name += "_" + ttsUsage.name
			}
			dir = filepath.Join(dir, name)
			details := call(http.MethodGet, path, keyID, "", "")
			require.Equal(t, http.StatusOK, details.Code, details.Body.String())
			require.NoError(t, os.WriteFile(filepath.Join(dir, "project-job.json"), details.Body.Bytes(), 0600))
			summary, err := json.MarshalIndent(map[string]any{"job_id": completed.JobID, "request_id": completed.RequestID,
				"status": content.Code, "mime": content.Header().Get("Content-Type"), "size_bytes": len(content.Body.Bytes()),
				"sha256": completed.Output.ContentSHA256, "provider_post": posts.Load(), "provider_status_get": statusReads.Load(), "provider_asset_get": assetReads.Load()}, "", "  ")
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "project-content.json"), summary, 0600))
		}
	}
	authorized := readAsViewer(project)
	require.Equal(t, http.StatusOK, authorized.Code, authorized.Body.String())
	require.Equal(t, artifactBytes, authorized.Body.Bytes())
	require.Equal(t, http.StatusForbidden, readAsViewer(foreignProject).Code)
	var chargeFacts, tokenAttempts int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM async_media_charge_facts WHERE org_id=$1 AND project_id=$2 AND amount_micros IS NULL", org, project).Scan(&chargeFacts))
	require.Equal(t, 1, chargeFacts)
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM billing_provider_attempts WHERE org_id=$1", org).Scan(&tokenAttempts))
	require.Zero(t, tokenAttempts)
	if isOCR || isSpeech {
		replay := call(http.MethodPost, modeloperation.PathMediaJobs, keyID, "success", "same fixture")
		require.Equal(t, http.StatusOK, replay.Code)
		require.Equal(t, path, replay.Header().Get("Location"))
		require.EqualValues(t, 1, posts.Load())
		return
	}
	lost := submit("lost", "lost response")
	require.NoError(t, run())
	require.EqualValues(t, 2, posts.Load())
	lostState := call(http.MethodGet, lost, keyID, "", "")
	require.Contains(t, lostState.Body.String(), `"provider_outcome":"unknown"`)
	replayed := call(http.MethodPost, modeloperation.PathMediaJobs, keyID, "lost", "lost response")
	require.Equal(t, http.StatusOK, replayed.Code, replayed.Body.String())
	require.Equal(t, lost, replayed.Header().Get("Location"))
	require.EqualValues(t, 2, posts.Load())

	waiting := submit("wait-slot", "previous slot")
	active := mustAdminDeploymentRelease(t, manager, initial.ReleaseRef)
	second := author(runtimechange.DeploymentReleaseBaseline{Kind: runtimechange.DeploymentReleaseBaselineStable, ExpectedSlotVersion: activated.Slot.AfterVersion, StableReleaseRef: active.ReleaseRef, ExpectedStableReleaseVersion: active.Version}, "media-freeze-next")
	canary, _ := serveCanonicalLifecycleMutation(t, h, customer, second.ReleaseRef, "start-canary",
		fmt.Sprintf(`{"expected_release_version":%d,"expected_slot_version":%d,"canary_basis_points":500}`, second.Version, activated.Slot.AfterVersion),
		"media-start-next", h.StartCanonicalDeploymentCanary, http.StatusOK)
	child := mustAdminDeploymentRelease(t, manager, second.ReleaseRef)
	_, _ = serveCanonicalLifecycleMutation(t, h, customer, second.ReleaseRef, "promote",
		fmt.Sprintf(`{"expected_release_version":%d,"expected_slot_version":%d}`, child.Version, canary.Slot.AfterVersion),
		"media-promote-next", h.PromoteCanonicalDeploymentCanary, http.StatusOK)
	same := call(http.MethodPost, modeloperation.PathMediaJobs, keyID, "wait-slot", "previous slot")
	require.Equal(t, http.StatusOK, same.Code, same.Body.String())
	require.Equal(t, waiting, same.Header().Get("Location"))
	require.ErrorIs(t, run(), mediajob.ErrForbidden)
	require.EqualValues(t, 2, posts.Load())
	newSlot := submit("new-slot", "new slot works")
	require.NoError(t, run())
	if isVideo {
		clockOffset += endpointprotocol.MediaTaskPollInterval + time.Second
		require.NoError(t, run())
	}
	require.Equal(t, http.StatusOK, call(http.MethodGet, newSlot+"/content", keyID, "", "").Code)
	require.EqualValues(t, 3, posts.Load())

	wantFinalPosts := int32(3)
	if upstreamModel == modelprofile.OpenAIImageFlare {
		wrong := submit("wrong-dimensions", "wrong dimensions")
		require.NoError(t, run())
		wrongJob, err := store.Get(ctx, mediajob.Scope{OrgID: org, ProjectID: project}, strings.TrimPrefix(wrong, modeloperation.PathMediaJobs+"/"))
		require.NoError(t, err)
		require.Equal(t, mediajob.OutcomeUnknown, wrongJob.Lifecycle.Outcome)
		require.Empty(t, wrongJob.Output)
		require.Empty(t, wrongJob.ChargeFact)
		wantFinalPosts++
	}

	if isWan {
		startedPosts, statusBefore := posts.Load(), statusReads.Load()
		crashedPath := submit("crash-before-ack", "crash before accepted task is recorded")
		crashStore := &canonicalMediaLostAckStore{PostgresStore: store}
		crashed, err := mediajob.NewWorker(crashStore, objects, provider, cipher, mediajob.WorkerOptions{Owner: "fixture-before-ack", LimitPolicy: limits, Now: clock})
		require.NoError(t, err)
		worked, err := crashed.RunOnce(ctx)
		require.True(t, worked)
		require.ErrorIs(t, err, context.Canceled)
		crashedID := strings.TrimPrefix(crashedPath, modeloperation.PathMediaJobs+"/")
		var accepted *string
		require.NoError(t, pool.QueryRow(ctx, `SELECT provider_task_id FROM async_media_jobs WHERE job_id=$1`, crashedID).Scan(&accepted))
		require.Nil(t, accepted)
		require.Equal(t, startedPosts+1, posts.Load())
		clockOffset += 6 * time.Minute
		_, err = worker.RunOnce(ctx)
		require.NoError(t, err)
		job, err := store.Get(ctx, mediajob.Scope{OrgID: org, ProjectID: project}, crashedID)
		require.NoError(t, err)
		require.Equal(t, mediajob.OutcomeUnknown, job.Lifecycle.Outcome)
		require.Equal(t, "provider_outcome_unknown_after_lease_expiry", job.ErrorCode)
		require.Equal(t, 1, job.Attempt)
		require.Equal(t, statusBefore, statusReads.Load())
		require.Equal(t, startedPosts+1, posts.Load())
		expired := mediajob.Lease{JobID: job.JobID, Scope: job.Scope, Owner: "fixture-before-ack", Fence: job.LeaseFence, Attempt: 1}
		require.ErrorIs(t, store.ScheduleProviderStatus(ctx, expired, providerTask, clock()), mediajob.ErrLeaseLost)
		require.NoError(t, run(), "release the crashed job admission reservation before the next job")
		wantFinalPosts++
	}
	_ = submit("revoke", "revoked before dispatch")
	_, err = pool.Exec(ctx, "UPDATE api_keys SET status='disabled' WHERE id=$1", keyID)
	require.NoError(t, err)
	require.ErrorIs(t, run(), mediajob.ErrForbidden)
	require.Equal(t, wantFinalPosts, posts.Load())
	if upstreamModel == modelprofile.OpenAIImageFlare {
		checkCanonicalMediaRollback(t, pool, dir, instance, "rollback would discard retained canonical media authority")
	}
}

func validTTSFixtureRequest(body map[string]any, text, model string) bool {
	switch model {
	case endpointprotocol.MiMoTTS:
		messages, _ := body["messages"].([]any)
		if len(messages) != 1 {
			return false
		}
		message, _ := messages[0].(map[string]any)
		audio, _ := body["audio"].(map[string]any)
		return body["model"] == model && body["stream"] == false && message["role"] == "assistant" &&
			message["content"] == text && audio["voice"] == "mimo_default" && audio["format"] == "wav"
	case endpointprotocol.DashScopeTTS:
		input, _ := body["input"].(map[string]any)
		return body["model"] == model && input["text"] == text && input["voice"] == "longanhuan_v3.1" &&
			input["format"] == "wav" && input["enable_ssml"] == false && input["sample_rate"] == float64(24000)
	}
	if model == endpointprotocol.GLMTTS {
		return body["model"] == model && body["input"] == text && body["voice"] == "tongtong" &&
			body["response_format"] == "wav" && body["stream"] == false
	}
	if body["text"] != text {
		return false
	}
	if model != endpointprotocol.MiniMaxTTS {
		return body["format"] == "wav" && body["voice_id"] == "stella" && body["filter_markdown"] == false
	}
	voice, _ := body["voice_setting"].(map[string]any)
	audio, _ := body["audio_setting"].(map[string]any)
	return body["model"] == endpointprotocol.MiniMaxTTS && body["output_format"] == "hex" && body["stream"] == false &&
		voice["voice_id"] == "male-qn-qingse" && audio["format"] == "wav" &&
		audio["sample_rate"] == float64(32000) && audio["channel"] == float64(1)
}

func asrFixtureResult(text string, durationMS int64, model string) string {
	switch model {
	case endpointprotocol.MiniMaxASR:
		return fmt.Sprintf(`{"trace_id":"fixture-asr","text":%q,"duration":%d.%03d}`, text, durationMS/1000, durationMS%1000)
	case endpointprotocol.GLMASR:
		return fmt.Sprintf(`{"id":"fixture-asr","request_id":"fixture-asr","created":1,"model":"glm-asr-2512","text":%q}`, text)
	case endpointprotocol.DashScopeASR:
		return fmt.Sprintf(`{"request_id":"qwen-speech-fixture","output":{"text":%q},"usage":{"input_tokens":78,"output_tokens":16,"total_tokens":94,"duration":%d}}`, text, durationMS/1000)
	case endpointprotocol.MiMoASR:
		return fmt.Sprintf(`{"id":"mimo-asr-fixture","created":1,"model":"mimo-v2.5-asr","object":"chat.completion","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":%q,"audio":null}}],"usage":{"prompt_tokens":46,"completion_tokens":20,"total_tokens":66,"seconds":%d}}`, text, durationMS/1000)
	case endpointprotocol.OpenAITranscribe:
		return fmt.Sprintf(`{"text":%q,"usage":{"type":"tokens","input_tokens":14,"input_token_details":{"text_tokens":0,"audio_tokens":14},"output_tokens":45,"total_tokens":59}}`, text)
	default:
		return fmt.Sprintf(`{"request_id":"fixture-asr","text":%q,"duration_ms":%d}`, text, durationMS)
	}
}

func readASRFixtureInput(req *http.Request, model string) ([]byte, error) {
	if model == endpointprotocol.DashScopeASR || model == endpointprotocol.MiMoASR {
		type message struct {
			Role    string
			Content []struct {
				Type  string
				Audio struct{ Data string } `json:"input_audio"`
			}
		}
		var payload struct {
			Model    string
			Stream   *bool
			Messages []message
			Input    struct{ Messages []message }
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil || payload.Model != model {
			return nil, errors.New("invalid speech JSON wire")
		}
		messages := payload.Messages
		if model == endpointprotocol.DashScopeASR {
			if req.Header.Get("X-DashScope-SSE") != "disable" || payload.Stream != nil || len(messages) != 0 {
				return nil, errors.New("incorrect DashScope speech framing")
			}
			messages = payload.Input.Messages
		} else if payload.Stream == nil || *payload.Stream || len(payload.Input.Messages) != 0 {
			return nil, errors.New("incorrect MiMo speech framing")
		}
		if len(messages) != 1 || messages[0].Role != "user" || len(messages[0].Content) != 1 ||
			messages[0].Content[0].Type != "input_audio" {
			return nil, errors.New("incorrect speech content")
		}
		audio, _, err := endpointprotocol.DecodeASRInputWAV(messages[0].Content[0].Audio.Data)
		return audio, err
	}

	mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return nil, errors.New("expected multipart speech")
	}
	form := multipart.NewReader(req.Body, params["boundary"])
	var fields [][2]string
	switch model {
	case endpointprotocol.MiniMaxASR, endpointprotocol.OpenAITranscribe:
		fields = [][2]string{{"model", model}, {"response_format", "json"}, {"stream", "false"}}
	case endpointprotocol.GLMASR:
		fields = [][2]string{{"model", model}, {"stream", "false"}}
	}
	for _, field := range fields {
		part, err := form.NextPart()
		if err != nil || part.FormName() != field[0] || part.FileName() != "" {
			return nil, errors.New("wrong speech field")
		}
		value, err := io.ReadAll(part)
		if err != nil || string(value) != field[1] {
			return nil, errors.New("wrong speech field value")
		}
	}
	file, err := form.NextPart()
	if err != nil || file.FormName() != "file" || file.FileName() != "speech.wav" {
		return nil, errors.New("missing speech WAV")
	}
	raw, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	if _, err := form.NextPart(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected speech multipart fields")
	}
	return raw, nil
}

func recoverASRFixtureDigits(wave []byte) (string, error) {
	const header, chunk = 44, 8640
	if len(wave) != header+asrProbeDigitCount*chunk {
		return "", errors.New("incorrect audio fixture length")
	}
	var decoded strings.Builder
	for i := range asrProbeDigitCount {
		pcm := wave[header+i*chunk : header+(i+1)*chunk]
		var digitMatched bool
		for _, digit := range asrProbeDigits {
			part, err := asrDigitSamples.ReadFile("probeassets/digit-" + string(digit) + ".pcm")
			if err != nil {
				return "", err
			}
			if bytes.Equal(part, pcm) {
				decoded.WriteRune(digit)
				digitMatched = true
				break
			}
		}
		if !digitMatched {
			return "", errors.New("unknown speech fixture chunk")
		}
	}
	return decoded.String(), nil
}

func ocrFixtureResult(text string, glm bool) string {
	if glm {
		return fmt.Sprintf(`{"id":"fixture-ocr","created":1,"model":"GLM-OCR","md_results":%q,"data_info":{"num_pages":1},"usage":{"prompt_tokens":44,"completion_tokens":7,"total_tokens":51},"request_id":"fixture-ocr-request"}`, text)
	}
	return fmt.Sprintf(`{"id":"ocr-fixture","object":"ocr.result","created":1,"purpose":"text_recognition","page_count":1,"pages":[{"index":0,"text":%q,"finish_reason":"stop"}],"usage":{"input_tokens":15,"output_tokens":4,"total_tokens":19}}`, text)
}

func readOCRFixtureInput(req *http.Request, glm bool) ([]byte, error) {
	if glm {
		var wire struct {
			Model  string
			File   string
			Crop   *bool `json:"return_crop_images"`
			Layout *bool `json:"need_layout_visualization"`
		}
		if json.NewDecoder(req.Body).Decode(&wire) != nil || wire.Model != endpointprotocol.GLMOCR ||
			wire.Crop == nil || *wire.Crop || wire.Layout == nil || *wire.Layout {
			return nil, errors.New("invalid original GLM OCR envelope")
		}
		return base64.StdEncoding.Strict().DecodeString(wire.File)
	}
	mimeType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || mimeType != "multipart/form-data" {
		return nil, errors.New("expected multipart OCR")
	}
	form := multipart.NewReader(req.Body, params["boundary"])
	purpose, err := form.NextPart()
	if err != nil || purpose.FormName() != "purpose" {
		return nil, errors.New("missing OCR purpose")
	}
	purposeBody, err := io.ReadAll(purpose)
	if err != nil || string(purposeBody) != "text_recognition" {
		return nil, errors.New("wrong OCR purpose")
	}
	file, err := form.NextPart()
	if err != nil || file.FormName() != "file" || file.FileName() != "page.png" {
		return nil, errors.New("missing OCR page")
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	if _, err := form.NextPart(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected OCR multipart fields")
	}
	return data, nil
}

func recoverOCRGlyphFixture(input []byte) (string, error) {
	canvas, err := png.Decode(bytes.NewReader(input))
	if err != nil {
		return "", err
	}
	var code [8]byte
	for i := range code {
		var digit [7]uint8
		for y := range digit {
			for x := range 5 {
				sampleX, sampleY := ocrProbeInset+ocrProbeGlyphScale*(i*6+x)+5, ocrProbeInset+ocrProbeGlyphScale*y+5
				if color.GrayModel.Convert(canvas.At(sampleX, sampleY)).(color.Gray).Y == 0 {
					digit[y] |= 1 << (4 - x)
				}
			}
		}
		value := slices.Index(ocrProbeDigits[:], digit)
		if value == -1 {
			return "", errors.New("unreadable challenge glyph")
		}
		code[i] = byte('0' + value)
	}
	return string(code[:]), nil
}

func checkOperationProbeAvailability(t *testing.T, h *Handler, authorize func(string, string, string) *http.Request,
	deploymentID int64, operation string, probeAvailable, asynchronous bool,
) []byte {
	t.Helper()
	assertApplication := func(app canonicalModelApplication) {
		require.Equal(t, deploymentID, app.Deployment.ID)
		require.Len(t, app.Credential.Bindings, 1)
		require.True(t, app.Credential.Bindings[0].Eligible)
		require.Len(t, app.Operations, 1)
		require.Equal(t, operation, app.Operations[0].OperationID)
		require.Equal(t, probeAvailable, app.Operations[0].ProbeAvailable, "%+v", app.Operations[0])
		require.Equal(t, asynchronous, app.Operations[0].AsyncProbe)
	}
	list := httptest.NewRecorder()
	h.ListCanonicalModelApplications(list, authorize(http.MethodGet, "/api/v1/model-deployments", ""))
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	var projection canonicalModelApplicationProjection
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &projection))
	var found bool
	for _, app := range projection.Applications {
		if app.Deployment.ID == deploymentID {
			assertApplication(app)
			found = true
		}
	}
	require.True(t, found, "the deployment must be discoverable from the normal list")
	path := fmt.Sprintf("/api/v1/model-deployments/%d?operation=%s", deploymentID, operation)
	req := authorize(http.MethodGet, path, "")
	req.SetPathValue("deploymentID", strconv.FormatInt(deploymentID, 10))
	detail := httptest.NewRecorder()
	h.GetCanonicalModelApplication(detail, req)
	require.Equal(t, http.StatusOK, detail.Code, detail.Body.String())
	var application canonicalModelApplication
	require.NoError(t, json.Unmarshal(detail.Body.Bytes(), &application))
	assertApplication(application)
	require.NotContains(t, detail.Body.String(), "synthetic-canonical-image-key")
	return detail.Body.Bytes()
}

func checkCanonicalMediaRollback(t *testing.T, pool *pgxpool.Pool, dir, instance, wantError string) {
	t.Helper()
	ctx := t.Context()
	down, err := os.ReadFile(filepath.Join(dir, schemaauthority.CanonicalMediaJobHead+".down.sql"))
	require.NoError(t, err)
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	if instance != "" {
		_, err = tx.Exec(ctx, "UPDATE runtime_generations SET state='stopped',ingress_state='absent',revision=revision+1 WHERE instance_id=$1", instance)
		require.NoError(t, err)
	}
	asyncDown, err := os.ReadFile(filepath.Join(dir, schemaauthority.AsyncMediaProviderTaskHead+".down.sql"))
	require.NoError(t, err)
	_, err = tx.Exec(ctx, strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(asyncDown)), "BEGIN;"), "COMMIT;"))
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "DELETE FROM schema_migrations WHERE version=$1", schemaauthority.AsyncMediaProviderTaskHead)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(down)), "BEGIN;"), "COMMIT;"))
	if wantError != "" {
		require.ErrorContains(t, err, wantError)
		return
	}
	require.NoError(t, err)
	var bindings int
	require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM model_operation_execution_support_bindings WHERE operation_id=$1", modeloperation.IDMediaJobs).Scan(&bindings))
	require.Zero(t, bindings)
}

func canonicalMediaSourceReader(t *testing.T, h *Handler, source *sourcefacts.Service, pool *pgxpool.Pool, operator, approver, org, project int64) func(string, string, string) []byte {
	t.Helper()
	h.SetSourceFactsController(source)
	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	f := sourceControlFixture{
		xInsightFeedAuthorizationFixture: xInsightFeedAuthorizationFixture{pool: pool, orgID: operator, projectID: project, actorID: approver},
		handler:                          h, source: source, reader: source.ObservationHandler(resolver),
	}
	profiles := []string{sourcefacts.CostProfile, sourcefacts.RuntimeProfile, sourcefacts.MediaChargeProfile}
	slices.Sort(profiles)
	reg, token := f.registration(t, "canonical-media-audit", profiles)
	reg.SourceInstance = source.SourceInstance()
	reg.OrgID = stringInt64(org)
	require.NoError(t, reg.Validate())
	key := f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": reg}, http.StatusOK)
	f.control(t, "activate", map[string]any{"credential_id": key["credential_id"]}, http.StatusOK)
	f.control(t, "resume", map[string]any{}, http.StatusOK)
	return func(path, profile, cursor string) []byte {
		t.Helper()
		query := url.Values{}
		if profile != "" {
			query.Set("profile", profile)
		}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		return f.read(t, token, "/api/v1/integrations/xinsight/source/v2/"+path+"?"+query.Encode(), "127.0.0.1:1234", http.StatusOK).Body.Bytes()
	}
}

func checkCanonicalMediaSourcePages(t *testing.T, read func(string, string, string) []byte, job mediajob.Job, unitCount int, capabilities, runtimeInitial, chargeInitial []byte, ttsUsage *qwenTTSUsageFixture) {
	t.Helper()
	type page struct {
		NextCursor string `json:"next_cursor"`
		Events     []struct {
			Payload json.RawMessage `json:"payload"`
		} `json:"events"`
	}
	decode := func(wire []byte) page {
		var p page
		require.NoError(t, json.Unmarshal(wire, &p))
		require.NotEmpty(t, p.NextCursor)
		return p
	}
	runtimePage := read("events", sourcefacts.RuntimeProfile, decode(runtimeInitial).NextCursor)
	chargePage := read("events", sourcefacts.MediaChargeProfile, decode(chargeInitial).NextCursor)
	var terminal sourcefacts.Observation
	for _, event := range decode(runtimePage).Events {
		d, err := sourcefacts.DecodeObservation(event.Payload)
		require.NoError(t, err)
		if d.Kind == "media_terminal" {
			require.Empty(t, terminal.RequestID)
			terminal = d
		}
	}
	charges := decode(chargePage).Events
	require.Len(t, charges, 1)
	charge, err := sourcefacts.DecodeObservation(charges[0].Payload)
	require.NoError(t, err)
	require.Equal(t, "media_charge", charge.Kind)
	require.Equal(t, job.RequestID, terminal.RequestID)
	require.Equal(t, terminal.RequestID, charge.RequestID)
	require.Equal(t, terminal.SourceInstance, charge.SourceInstance)
	require.Equal(t, terminal.OrgID, charge.OrgID)
	require.Equal(t, terminal.ProjectID, charge.ProjectID)
	require.Equal(t, terminal.SubjectID, charge.SubjectID)
	require.Equal(t, terminal.OccurredAt, charge.OccurredAt)
	require.NotNil(t, charge.MediaCharge)
	require.Equal(t, job.JobID, charge.MediaCharge.JobID)
	require.Equal(t, mediajob.ChargeAmountPricingPending, charge.MediaCharge.AmountState)
	require.Nil(t, charge.MediaCharge.CustomerCharge)
	require.Len(t, charge.MediaCharge.Units, unitCount)
	if job.Operation == mediajob.OperationTextToVideo && unitCount != 0 {
		require.Equal(t, endpointprotocol.UnitInputVideoDurationMS, charge.MediaCharge.Units[0].Class)
		require.Equal(t, "0", charge.MediaCharge.Units[0].Quantity)
		require.Equal(t, endpointprotocol.UnitOutputVideoDurationMS, charge.MediaCharge.Units[1].Class)
		if job.MappedModel == endpointprotocol.MiniMaxH3Video {
			require.Equal(t, "4000", charge.MediaCharge.Units[1].Quantity)
			require.Equal(t, endpointprotocol.UnitInputImages, charge.MediaCharge.Units[2].Class)
			require.Equal(t, "0", charge.MediaCharge.Units[2].Quantity)
		} else {
			require.Equal(t, "2000", charge.MediaCharge.Units[1].Quantity)
		}
		require.NotContains(t, string(chargePage), "synthetic.mp4")
		require.NotContains(t, string(runtimePage), "wan-fixture-original")
		require.NotContains(t, string(runtimePage), "h3-fixture-original")
		require.NotContains(t, string(runtimePage), "cog-fixture-original")
	} else if unitCount == 2 && job.Operation == mediajob.OperationTextToImage {
		require.Equal(t, endpointprotocol.UnitInputImages, charge.MediaCharge.Units[0].Class)
		require.Equal(t, "0", charge.MediaCharge.Units[0].Quantity)
		require.Equal(t, endpointprotocol.UnitOutputImages, charge.MediaCharge.Units[1].Class)
		require.Equal(t, "1", charge.MediaCharge.Units[1].Quantity)
	}
	switch job.ProfileID {
	case endpointprotocol.ProfileMiMoASR:
		require.Equal(t, endpointprotocol.UnitInputAudioDurationMS, charge.MediaCharge.Units[0].Class)
		require.Equal(t, "2000", charge.MediaCharge.Units[0].Quantity)
	case endpointprotocol.ProfileDashScopeTTS:
		expected := []string{"12", "16"}
		if ttsUsage != nil {
			expected = ttsUsage.expected
		}
		classes := []endpointprotocol.UnitClass{endpointprotocol.UnitInputTokens, endpointprotocol.UnitOutputTokens}
		for i, quantity := range expected {
			require.Equal(t, classes[i], charge.MediaCharge.Units[i].Class)
			require.Equal(t, quantity, charge.MediaCharge.Units[i].Quantity)
		}
	case endpointprotocol.ProfileGLMOCR:
		units := charge.MediaCharge.Units
		require.Equal(t, endpointprotocol.UnitInputTokens, units[0].Class)
		require.Equal(t, endpointprotocol.UnitOutputTokens, units[1].Class)
		require.Equal(t, "44", units[0].Quantity)
		require.Equal(t, "7", units[1].Quantity)
	case endpointprotocol.ProfileMiMoTTS, endpointprotocol.ProfileDashScopeASR:
		units := charge.MediaCharge.Units
		require.Equal(t, endpointprotocol.UnitInputTokens, units[0].Class)
		require.Equal(t, endpointprotocol.UnitOutputTokens, units[1].Class)
		switch job.ProfileID {
		case endpointprotocol.ProfileMiMoTTS:
			require.Equal(t, "213", units[0].Quantity)
			require.Equal(t, "97", units[1].Quantity)
		case endpointprotocol.ProfileDashScopeASR:
			require.Equal(t, "78", units[0].Quantity)
			require.Equal(t, "16", units[1].Quantity)
			require.Equal(t, endpointprotocol.UnitInputAudioDurationMS, units[2].Class)
			require.Equal(t, "2000", units[2].Quantity)
		}
	case endpointprotocol.ProfileDeepSeekASR, endpointprotocol.ProfileMiniMaxASR:
		require.Equal(t, endpointprotocol.UnitInputAudioDurationMS, charge.MediaCharge.Units[0].Class)
		require.Equal(t, "2496", charge.MediaCharge.Units[0].Quantity)
	case endpointprotocol.ProfileOpenAITranscribe:
		units := charge.MediaCharge.Units
		require.Equal(t, []endpointprotocol.UnitClass{endpointprotocol.UnitInputTokens, endpointprotocol.UnitInputTextTokens, endpointprotocol.UnitInputAudioTokens, endpointprotocol.UnitOutputTokens},
			[]endpointprotocol.UnitClass{units[0].Class, units[1].Class, units[2].Class, units[3].Class})
		require.Equal(t, []string{"14", "0", "14", "45"}, []string{units[0].Quantity, units[1].Quantity, units[2].Quantity, units[3].Quantity})
	}
	if job.ProfileID == endpointprotocol.ProfileMiniMaxTTS {
		require.Equal(t, endpointprotocol.UnitInputBillableChars, charge.MediaCharge.Units[0].Class)
		require.Equal(t, "19", charge.MediaCharge.Units[0].Quantity)
	}
	if job.ProfileID == endpointprotocol.ProfileDeepSeekOCR {
		require.Equal(t, endpointprotocol.UnitRecognizedPages, charge.MediaCharge.Units[0].Class)
		require.Equal(t, "1", charge.MediaCharge.Units[0].Quantity)
		require.NotContains(t, string(runtimePage), "MAPLE")
		require.NotContains(t, string(chargePage), "MAPLE")
	}
	if unitCount == 5 {
		require.Equal(t, endpointprotocol.UnitInputImageTokens, charge.MediaCharge.Units[2].Class)
		require.Equal(t, "0", charge.MediaCharge.Units[2].Quantity)
		require.Equal(t, endpointprotocol.UnitOutputImages, charge.MediaCharge.Units[4].Class)
		require.Equal(t, "1", charge.MediaCharge.Units[4].Quantity)
	}

	if directory := os.Getenv("CANONICAL_MEDIA_SOURCE_EVIDENCE_DIR"); directory != "" {
		name := job.MappedModel
		if ttsUsage != nil {
			name += "_" + ttsUsage.name
		}
		directory = filepath.Join(directory, name)
		require.NoError(t, os.MkdirAll(directory, 0700))
		for name, wire := range map[string][]byte{
			"capabilities.json":    capabilities,
			"runtime-initial.json": runtimeInitial, "runtime-page.json": runtimePage,
			"charge-initial.json": chargeInitial, "charge-page.json": chargePage,
		} {
			require.NoError(t, os.WriteFile(filepath.Join(directory, name), wire, 0600))
		}
	}
}

type canonicalMediaLostAckStore struct{ *mediajob.PostgresStore }

func (*canonicalMediaLostAckStore) ScheduleProviderStatus(context.Context, mediajob.Lease, string, time.Time) error {
	return context.Canceled
}

type canonicalImageLimitFixture struct{}

func (canonicalImageLimitFixture) Reserve(_ context.Context, r limitpolicy.AdmissionRequest) (*limitpolicy.Reservation, error) {
	return &limitpolicy.Reservation{OperationID: r.OperationID}, nil
}
func (canonicalImageLimitFixture) Finalize(context.Context, *limitpolicy.Reservation, int64) error {
	return nil
}
func (canonicalImageLimitFixture) Cancel(context.Context, *limitpolicy.Reservation) error { return nil }
func (canonicalImageLimitFixture) CancelBeforeDispatch(context.Context, *limitpolicy.Reservation) error {
	return nil
}
func (canonicalImageLimitFixture) TerminalStateTTL(ttl time.Duration) time.Duration {
	return ttl + time.Minute
}

type canonicalMediaObjectFixture struct {
	results map[string]mediajob.ProviderResult
}

func (f *canonicalMediaObjectFixture) Persist(_ context.Context, j mediajob.Job, r mediajob.ProviderResult) (mediajob.PersistedResult, error) {
	f.results[j.JobID] = r
	return mediajob.PersistedResult{
		ObjectKey: "fixture/content/" + j.JobID, ReceiptKey: "fixture/receipt/" + j.JobID, ContentSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(r.Bytes)),
		MediaType: r.MediaType, SizeBytes: int64(len(r.Bytes)), MappedModel: r.MappedModel, Outcome: r.Outcome, PersistedAt: time.Now().UTC(),
	}, nil
}
func (*canonicalMediaObjectFixture) Recover(context.Context, mediajob.Job) (*mediajob.PersistedResult, error) {
	return nil, nil
}
func (f *canonicalMediaObjectFixture) Read(_ context.Context, j mediajob.Job) (mediajob.OutputContent, error) {
	r, ok := f.results[j.JobID]
	if !ok {
		return mediajob.OutputContent{}, mediajob.ErrNotFound
	}
	return mediajob.OutputContent{MediaType: r.MediaType, Bytes: r.Bytes}, nil
}
func (f *canonicalMediaObjectFixture) Delete(_ context.Context, j mediajob.Job) error {
	delete(f.results, j.JobID)
	return nil
}
