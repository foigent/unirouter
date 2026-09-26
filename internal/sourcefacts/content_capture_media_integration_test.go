//go:build integration

package sourcefacts

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/mediajob"
	"github.com/uniroute/unirouter/internal/models"
)

type contentMediaRouter struct {
	mediajob.ImageRouter
	route gateway.RouteResult
}

func (r contentMediaRouter) RouteAllWithKey(context.Context, string, string) ([]gateway.RouteResult, error) {
	return []gateway.RouteResult{r.route}, nil
}
func (r contentMediaRouter) MarkChannelSuccess(context.Context, int64) error { return nil }
func (r contentMediaRouter) MarkKeySuccess(context.Context, int64) error     { return nil }

func TestContentMediaOriginalCommittedAttemptWithoutBPG(t *testing.T) {
	p := setupCaptureServicePG(t, contentcapture.ModeFullContent, 65536, endpointprotocol.ProfileOpenAIImageGPT)
	ctx := t.Context()
	source := p.service.source
	require.False(t, source.ObservationsEnabled())
	org, project := p.identity.OrgID, p.identity.ProjectID
	var key int64
	require.NoError(t, p.pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,name,key_hash,key_prefix,status,allowed_models)VALUES($1,$2,'synthetic-content-media',repeat('d',64),'ur-content-media','active','company-image') RETURNING id`, org, project).Scan(&key))
	store, err := mediajob.NewPostgresStore(p.pool, mediajob.ExecutionModelAuthorizerFunc(func(context.Context, db.DBQuerier, mediajob.ExecutionAuthorization) error { return nil }))
	require.NoError(t, err)
	store.SetSourceMaterializer(source)
	now := time.Now().UTC().Truncate(time.Microsecond)
	scope := mediajob.Scope{OrgID: org, ProjectID: project}
	job, reused, err := store.CreateOrGet(ctx, mediajob.CreateRecord{AllowCreate: true, ExpectedLimitSubject: limitpolicy.Subject{OrgID: org, ProjectID: project, APIKeyID: key, Model: "company-image", BaseRPSLimit: 10, BaseRPMLimit: 60}, Job: mediajob.Job{ContractVersion: mediajob.ContractVersion, JobID: "mediajob_" + strings.Repeat("a", 32), Scope: scope, APIKeyID: key, RequestID: "synthetic-content-media", Operation: mediajob.OperationTextToImage, ProfileID: endpointprotocol.ProfileOpenAIImageGPT, RequestedModel: "company-image", RoutingModel: "company-image", RequestSHA256: "sha256:" + strings.Repeat("a", 64), IdempotencyHash: "hmac-sha256:" + strings.Repeat("a", 64), RequestCiphertext: "synthetic-ciphertext", Lifecycle: mediajob.Lifecycle{Admission: mediajob.AdmissionAccepted, Queue: mediajob.QueueQueued, Lease: mediajob.LeaseUnleased, Execution: mediajob.ExecutionPending, Outcome: mediajob.OutcomePending, Cancellation: mediajob.CancellationNotRequested, Object: mediajob.ObjectPending, Settlement: mediajob.SettlementNotEligible}, MaxAttempts: 3, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}})
	require.NoError(t, err)
	require.False(t, reused)
	claimed, err := store.Claim(ctx, "synthetic-content-worker", now.Add(time.Second), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	lease := mediajob.Lease{JobID: job.JobID, Scope: scope, Owner: claimed.LeaseOwner, Fence: claimed.LeaseFence, Job: *claimed}
	subject, err := store.ExecutionLimitSubject(ctx, lease)
	require.NoError(t, err)
	attached, _, err := store.AttachExecutionReservation(ctx, lease, subject, &limitpolicy.DurableReservation{OperationID: "synthetic-content-operation"}, "synthetic-content-operation", now.Add(time.Hour), now.Add(2*time.Second))
	require.NoError(t, err)
	job = *claimed
	job.Attempt = attached.ProviderAttempt
	route := mediajob.SourceMediaRoute{Sequence: 1, Total: 1, ChannelID: 4, ChannelKeyID: 5, ProtocolProfile: endpointprotocol.ProfileOpenAIImageGPT}
	for _, kind := range []string{"fence", "attempt", "request", "scope", "owner"} {
		wrong := job
		switch kind {
		case "fence":
			wrong.LeaseFence++
		case "attempt":
			wrong.Attempt++
		case "request":
			wrong.RequestID = "wrong"
		case "scope":
			wrong.Scope.ProjectID++
		case "owner":
			wrong.LeaseOwner = "wrong"
		}
		request, _ := http.NewRequestWithContext(ctx, "POST", "https://synthetic.invalid", strings.NewReader("body"))
		wrapped, observe := p.service.ObserveMediaProvider(ctx, wrong, route, request)
		require.Same(t, request, wrapped)
		observe(nil, io.ErrClosedPipe)
		require.Empty(t, p.manifests(t))
	}
	image := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'E', 'N', 'D'}
	responseBody := `{"created":1,"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(image) + `"}],"usage":{"input_tokens":3,"input_tokens_details":{"text_tokens":3,"image_tokens":0},"output_tokens":5,"total_tokens":8}}`
	var requestBody []byte
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var readErr error
		requestBody, readErr = io.ReadAll(r.Body)
		require.NoError(t, readErr)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responseBody))
	}))
	defer server.Close()
	enc := []byte(strings.Repeat("e", 32))
	encrypted, err := auth.EncryptKey([]byte("synthetic-provider"), enc)
	require.NoError(t, err)
	provider, err := mediajob.NewGPTImageProvider(contentMediaRouter{route: gateway.RouteResult{Channel: models.Channel{ID: 4, BaseURL: server.URL, AdapterType: adapters.TypeOpenAICompatible}, ChannelKey: models.ChannelKey{ID: 5, KeyEnc: encrypted}, MappedModel: "gpt-image-1"}}, enc, mediajob.GPTImageProviderOptions{HTTPClient: server.Client()})
	require.NoError(t, err)
	provider.SetContentObserver(p.service)
	require.False(t, provider.SourceObservationsEnabled())
	result, err := provider.Generate(ctx, job, mediajob.NormalizedRequest{Model: "company-image", Prompt: "synthetic image prompt", N: 1, OutputFormat: "png"})
	require.NoError(t, err)
	require.Equal(t, image, result.Bytes)
	require.Equal(t, "completed", result.Outcome.ProviderState)
	closeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	require.NoError(t, p.service.Close(closeCtx))
	manifests := p.manifests(t)
	require.Len(t, manifests, 4)
	for _, m := range manifests {
		if m.Revision != "2" {
			continue
		}
		require.Equal(t, source.mediaRouteID(job, route), m.AttemptID)
		require.Equal(t, endpointprotocol.ProfileOpenAIImageGPT, m.Protocol)
		require.Equal(t, "complete", m.Capture.Completeness)
		var receipt contentcapture.StoredReceipt
		require.NoError(t, p.pool.QueryRow(ctx, `SELECT storage_identity,storage_binding_token,relative_path,saved_bytes,digest FROM execution_content_objects WHERE content_ref=$1 AND state='ready'`, m.ContentRef).Scan(&receipt.Binding.Identity, &receipt.Binding.Token, &receipt.ObjectName, &receipt.SavedBytes, &receipt.SHA256))
		f, err := p.service.files.OpenVerified(ctx, receipt, contentcapture.StoreLimits{MaxSavedBytes: 65536, MaxReadBytes: 65536, MaxBufferBytes: 32768})
		require.NoError(t, err)
		body, err := io.ReadAll(f)
		require.NoError(t, err)
		require.NoError(t, f.Close())
		if m.ObservationPoint == "provider_request" {
			require.Equal(t, requestBody, body)
		} else {
			require.Equal(t, responseBody, string(body))
		}
	}
	var count int
	require.NoError(t, p.pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts WHERE profile=ANY($1)`, ObservationProfiles()).Scan(&count))
	require.Zero(t, count, "C must not silently enable B")
	live, err := store.Get(ctx, scope, job.JobID)
	require.NoError(t, err)
	require.Equal(t, job.Attempt, live.Attempt)
	require.Equal(t, mediajob.ExecutionRunning, live.Lifecycle.Execution, "capture cannot manufacture a media financial terminal")
}
