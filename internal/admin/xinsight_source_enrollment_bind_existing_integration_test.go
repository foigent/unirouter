//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

func waitForBindExistingLocks(t *testing.T, f sourceControlFixture, minimum int) {
	t.Helper()
	require.Eventually(t, func() bool {
		var waiting int
		err := f.pool.QueryRow(t.Context(), `SELECT count(*) FROM pg_stat_activity
 WHERE datname=current_database() AND wait_event_type='Lock' AND pid<>pg_backend_pid()`).Scan(&waiting)
		return err == nil && waiting >= minimum
	}, 3*time.Second, 5*time.Millisecond)
}

func TestXInsightBindExistingFactsAndContentIntegration(t *testing.T) {
	for _, profiles := range [][]string{{sourcefacts.ContextProfile}, sourceEnrollmentEightProfiles()} {
		t.Run(strconv.Itoa(len(profiles))+"_profiles", func(t *testing.T) {
			testXInsightBindExistingProfiles(t, profiles)
		})
	}
}

func testXInsightBindExistingProfiles(t *testing.T, profiles []string) {
	t.Helper()
	f := newContentControlIntegrationFixture(t)
	enableEnrollmentObservations(t, &f)
	ctx := context.Background()
	legacy, factsToken := f.registration(t, "bind-existing-facts", profiles)
	key := f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": legacy}, 200)
	f.control(t, "activate", map[string]any{"credential_id": key["credential_id"]}, 200)
	f.control(t, "resume", map[string]any{}, 200)
	resolverBefore, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	readOriginal := func(profile, cursor string) *httptest.ResponseRecorder {
		t.Helper()
		f.reader = f.source.Handler(resolverBefore)
		target := sourceReadPath + "?profile=" + profile
		if !sourcefacts.ValidProfile(profile) {
			f.reader = f.source.ObservationHandler(resolverBefore)
			target = "/api/v1/integrations/xinsight/source/v2/events?profile=" + profile
		}
		if cursor != "" {
			target += "&cursor=" + url.QueryEscape(cursor)
		}
		return f.read(t, factsToken, target, "127.0.0.1:3000", 200)
	}
	cursors := map[string]string{}
	for _, profile := range profiles {
		cursors[profile] = sourcePage(t, readOriginal(profile, "")).NextCursor
	}
	f.capture(t, "original-before-adoption")
	f.reader = f.source.Handler(resolverBefore)
	readIdentity := func() string {
		t.Helper()
		var identity string
		require.NoError(t, f.pool.QueryRow(ctx, `SELECT jsonb_build_object('key_id',ak.id,'hash',ak.key_hash,'prefix',ak.key_prefix,'registration',ak.registration_operation_id,'ips',ak.allowed_ips,'state',ak.status,'credential_profiles',sc.profiles,'connection_profiles',c.profiles,'generation',c.scope_generation)::text FROM api_keys ak JOIN execution_source_credentials sc ON sc.api_key_id=ak.id JOIN execution_source_connections c ON c.org_id=sc.org_id AND c.project_id=sc.project_id WHERE ak.id=$1`, key["credential_id"]).Scan(&identity))
		return identity
	}
	originalIdentity := readIdentity()
	prepareContent, contentToken := contentIntegrationPrepare(t, f, "bind-existing-content")
	stagedContent := contentIntegrationHTTP(t, f, "prepare", prepareContent, 200)
	activeContent := contentIntegrationHTTP(t, f, "activate", contentIntegrationTarget("bind-existing-activate", "1", stagedContent), 200)
	const installation = "xci_eeeeeeeeeeeeeeeeeeeeee"
	_, err = f.pool.Exec(ctx, `INSERT INTO xinsight_connector_installations
 (installation_id,org_id,project_id,x_deployment_id,invitation_sha256,event_scope,source_instance,source_binding_sha256,
 desired_state,observed_state,consented_by) VALUES($1,$2,$3,'xdep_bind_existing',$4,'model.request.completed',$5,$6,'active','connected',$7)`,
		installation, f.orgID, f.projectID, "sha256:"+strings.Repeat("a", 64), "synthetic-source-control", "sha256:"+strings.Repeat("b", 64), f.actorID)
	require.NoError(t, err)
	grant := sourceEnrollmentInput{InstallationID: installation, Coverage: "exact", ProjectID: stringInt64(f.projectID),
		Profiles: profiles, IncludeContent: true}
	r := f.request(t, "bind-existing-grant", grant)
	r.SetPathValue("org_id", stringInt64(f.orgID))
	w := httptest.NewRecorder()
	f.handler.CreateXInsightSourceEnrollment(w, r)
	require.Equal(t, 201, w.Code, w.Body.String())
	var grantBody map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &grantBody))
	grantID := grantBody["enrollment_id"].(string)
	var otherProject int64
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,status,budget_period)
 VALUES($1,'Other bind project','other-bind-project','active','none') RETURNING id`, f.orgID).Scan(&otherProject))
	other := f.request(t, "bind-existing-other-project", map[string]any{})
	other.Method = "GET"
	other.SetPathValue("org_id", stringInt64(f.orgID))
	other.SetPathValue("project_id", stringInt64(otherProject))
	other.SetPathValue("enrollment_id", grantID)
	otherResponse := httptest.NewRecorder()
	f.handler.GetXInsightBindExistingCandidates(otherResponse, other)
	require.Equal(t, 409, otherResponse.Code, otherResponse.Body.String(), "exact enrollment cannot move to another project")
	request := func(method, action string, body any) *httptest.ResponseRecorder {
		t.Helper()
		req := f.request(t, action, body)
		req.Method = method
		req.SetPathValue("org_id", stringInt64(f.orgID))
		req.SetPathValue("project_id", stringInt64(f.projectID))
		req.SetPathValue("enrollment_id", grantID)
		response := httptest.NewRecorder()
		if method == "GET" {
			f.handler.GetXInsightBindExistingCandidates(response, req)
		} else {
			f.handler.BindExistingXInsightSourceEnrollment(response, req)
		}
		return response
	}
	if len(profiles) == 8 {
		mismatchGrant := grant
		mismatchGrant.Profiles = profiles[:len(profiles)-1]
		req := f.request(t, "bind-existing-mismatched-profile-grant", mismatchGrant)
		req.SetPathValue("org_id", stringInt64(f.orgID))
		response := httptest.NewRecorder()
		f.handler.CreateXInsightSourceEnrollment(response, req)
		require.Equal(t, 201, response.Code, response.Body.String())
		var mismatchBody map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &mismatchBody))
		correctGrantID := grantID
		grantID = mismatchBody["enrollment_id"].(string)
		preview := request("GET", "bind-existing-mismatched-preview", map[string]any{})
		require.Equal(t, 200, preview.Code, preview.Body.String())
		var mismatchCandidates struct {
			Data []bindExistingCandidate `json:"data"`
		}
		require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &mismatchCandidates))
		for _, candidate := range mismatchCandidates.Data {
			if candidate.Kind == "facts" {
				require.Equal(t, 409, request("POST", "bind-existing-mismatched-profile-reject", bindExistingInput{
					SchemaVersion: "unirouter.xinsight.bind-existing.v1", Kind: "facts", CredentialID: candidate.Keys[0].CredentialID,
					ExpectedStateSHA256: candidate.StateSHA256, OperationID: contentIntegrationOperation("bind-existing-mismatched-profile-reject"),
				}).Code)
			}
		}
		require.Equal(t, originalIdentity, readIdentity())
		grantID = correctGrantID
	}
	_, err = f.pool.Exec(ctx, `UPDATE xinsight_connector_installations SET source_instance='other-source' WHERE installation_id=$1`, installation)
	require.NoError(t, err)
	require.Equal(t, 409, request("GET", "bind-existing-installation-drift", map[string]any{}).Code)
	_, err = f.pool.Exec(ctx, `UPDATE xinsight_connector_installations SET source_instance=$2 WHERE installation_id=$1`, installation, "synthetic-source-control")
	require.NoError(t, err)
	preview := request("GET", "bind-existing-preview", map[string]any{})
	require.Equal(t, 200, preview.Code, preview.Body.String())
	var candidates struct {
		Data []bindExistingCandidate `json:"data"`
	}
	require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &candidates))
	require.Len(t, candidates.Data, 2)
	byKind := map[string]bindExistingCandidate{}
	for _, candidate := range candidates.Data {
		byKind[candidate.Kind] = candidate
		require.Len(t, candidate.Keys, 1)
		require.Equal(t, "feed_active", candidate.Keys[0].Status)
	}
	require.Equal(t, legacy.OperationID, byKind["facts"].Keys[0].RegistrationID)
	// An in-flight C rotation is not silently adopted, even with an otherwise
	// valid enrollment. Refresh the state digest only after cancelling it.
	nextRegistration, _ := f.registration(t, "bind-existing-next-content", []string{sourcefacts.ContentProfile})
	rotate := contentIntegrationTarget("bind-existing-unused", "2", activeContent)
	rotate.OperationID, rotate.Registration = nextRegistration.OperationID, &nextRegistration
	staged := contentIntegrationHTTP(t, f, "rotate", rotate, 200)
	preview = request("GET", "bind-existing-staged-preview", map[string]any{})
	require.Equal(t, 200, preview.Code, preview.Body.String())
	require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &candidates))
	for _, candidate := range candidates.Data {
		if candidate.Kind != "content" {
			continue
		}
		require.Len(t, candidate.Keys, 2)
		var oldKey string
		for _, key := range candidate.Keys {
			if key.Status == "feed_active" {
				oldKey = key.CredentialID
			}
		}
		require.Equal(t, 409, request("POST", "bind-existing-staged-reject", bindExistingInput{
			SchemaVersion: "unirouter.xinsight.bind-existing.v1", Kind: "content", CredentialID: oldKey,
			ExpectedStateSHA256: candidate.StateSHA256, OperationID: contentIntegrationOperation("bind-existing-staged-reject"),
		}).Code)
	}
	afterCancel := contentIntegrationHTTP(t, f, "cancel", contentIntegrationTarget("bind-existing-cancel", "3", staged), 200)
	preview = request("GET", "bind-existing-refreshed-preview", map[string]any{})
	require.Equal(t, 200, preview.Code, preview.Body.String())
	require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &candidates))
	for _, candidate := range candidates.Data {
		byKind[candidate.Kind] = candidate
	}
	// A real content rotation must win the shared actor/connection lock queue
	// before bind-existing reads the old key. The bind then rejects its stale
	// prestate without creating a binding or changing the old key's label.
	racingRegistration, _ := f.registration(t, "bind-existing-concurrent-rotate", []string{sourcefacts.ContentProfile})
	rotateInput := contentIntegrationTarget("bind-existing-concurrent-rotate", afterCancel.StateVersion, activeContent)
	rotateInput.OperationID, rotateInput.Registration = racingRegistration.OperationID, &racingRegistration
	rotateRequest := f.request(t, "bind-existing-concurrent-rotate", rotateInput)
	rotateRequest.URL.Path = "/api/v1/data-exchange/xinsight/source-connections/" + stringInt64(f.projectID) + "/content/rotate"
	bindInput := bindExistingInput{SchemaVersion: "unirouter.xinsight.bind-existing.v1", Kind: "content",
		CredentialID: byKind["content"].Keys[0].CredentialID, ExpectedStateSHA256: byKind["content"].StateSHA256,
		OperationID: contentIntegrationOperation("bind-existing-concurrent-rotate-reject")}
	bindRequest := f.request(t, "bind-existing-concurrent-rotate-bind", bindInput)
	bindRequest.SetPathValue("org_id", stringInt64(f.orgID))
	bindRequest.SetPathValue("project_id", stringInt64(f.projectID))
	bindRequest.SetPathValue("enrollment_id", grantID)
	barrier, err := f.pool.Begin(ctx)
	require.NoError(t, err)
	defer barrier.Rollback(ctx)
	_, err = barrier.Exec(ctx, `SELECT 1 FROM execution_source_connections WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, f.orgID, f.projectID)
	require.NoError(t, err)
	rotateDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		f.handler.ContentControl("rotate")(response, rotateRequest)
		rotateDone <- response
	}()
	waitForBindExistingLocks(t, f, 1)
	bindDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		f.handler.BindExistingXInsightSourceEnrollment(response, bindRequest)
		bindDone <- response
	}()
	waitForBindExistingLocks(t, f, 2)
	require.NoError(t, barrier.Commit(ctx))
	var rotated contentControlResult
	select {
	case response := <-rotateDone:
		require.Equal(t, 200, response.Code, response.Body.String())
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &rotated))
	case <-time.After(6 * time.Second):
		t.Fatal("content rotation did not finish after connection lock release")
	}
	select {
	case response := <-bindDone:
		require.Equal(t, 409, response.Code, response.Body.String())
	case <-time.After(6 * time.Second):
		t.Fatal("bind did not finish after content rotation")
	}
	var raceBindings int
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM xinsight_source_enrollment_bindings WHERE enrollment_id=$1`, strings.TrimPrefix(grantID, "xise_")).Scan(&raceBindings))
	require.Zero(t, raceBindings)
	afterRaceCancel := contentIntegrationHTTP(t, f, "cancel", contentIntegrationTarget("bind-existing-race-cancel", rotated.StateVersion, rotated), 200)
	preview = request("GET", "bind-existing-after-race-preview", map[string]any{})
	require.Equal(t, 200, preview.Code, preview.Body.String())
	require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &candidates))
	for _, candidate := range candidates.Data {
		byKind[candidate.Kind] = candidate
	}
	// The human revoke wins the enrollment row queue before a pending C bind.
	// The bind must observe the committed revoke and leave the manual key alone.
	revokeRequest := f.request(t, "bind-existing-concurrent-revoke", map[string]any{"expected_revision": "1"})
	revokeRequest.SetPathValue("org_id", stringInt64(f.orgID))
	revokeRequest.SetPathValue("enrollment_id", grantID)
	revokeBindInput := bindExistingInput{SchemaVersion: "unirouter.xinsight.bind-existing.v1", Kind: "content",
		CredentialID: byKind["content"].Keys[0].CredentialID, ExpectedStateSHA256: byKind["content"].StateSHA256,
		OperationID: contentIntegrationOperation("bind-existing-concurrent-revoke-reject")}
	revokeBindRequest := f.request(t, "bind-existing-concurrent-revoke-bind", revokeBindInput)
	revokeBindRequest.SetPathValue("org_id", stringInt64(f.orgID))
	revokeBindRequest.SetPathValue("project_id", stringInt64(f.projectID))
	revokeBindRequest.SetPathValue("enrollment_id", grantID)
	revokeBarrier, err := f.pool.Begin(ctx)
	require.NoError(t, err)
	defer revokeBarrier.Rollback(ctx)
	_, err = revokeBarrier.Exec(ctx, `SELECT 1 FROM xinsight_source_enrollments WHERE id=$1 FOR UPDATE`, strings.TrimPrefix(grantID, "xise_"))
	require.NoError(t, err)
	revokeDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		f.handler.RevokeXInsightSourceEnrollment(response, revokeRequest)
		revokeDone <- response
	}()
	waitForBindExistingLocks(t, f, 1)
	revokeBindDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		f.handler.BindExistingXInsightSourceEnrollment(response, revokeBindRequest)
		revokeBindDone <- response
	}()
	waitForBindExistingLocks(t, f, 2)
	require.NoError(t, revokeBarrier.Commit(ctx))
	select {
	case response := <-revokeDone:
		require.Equal(t, 200, response.Code, response.Body.String())
	case <-time.After(6 * time.Second):
		t.Fatal("enrollment revoke did not finish after lock release")
	}
	select {
	case response := <-revokeBindDone:
		require.Equal(t, 409, response.Code, response.Body.String())
	case <-time.After(6 * time.Second):
		t.Fatal("bind did not finish after enrollment revoke")
	}
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM xinsight_source_enrollment_bindings WHERE enrollment_id=$1`, strings.TrimPrefix(grantID, "xise_")).Scan(&raceBindings))
	require.Zero(t, raceBindings)
	var unboundLabel string
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT policy_labels FROM api_keys WHERE id=$1`, revokeBindInput.CredentialID).Scan(&unboundLabel))
	require.Empty(t, unboundLabel)
	newGrant := f.request(t, "bind-existing-grant-after-revoke", grant)
	newGrant.SetPathValue("org_id", stringInt64(f.orgID))
	newGrantResponse := httptest.NewRecorder()
	f.handler.CreateXInsightSourceEnrollment(newGrantResponse, newGrant)
	require.Equal(t, 201, newGrantResponse.Code, newGrantResponse.Body.String())
	require.NoError(t, json.Unmarshal(newGrantResponse.Body.Bytes(), &grantBody))
	grantID = grantBody["enrollment_id"].(string)
	preview = request("GET", "bind-existing-new-grant-preview", map[string]any{})
	require.Equal(t, 200, preview.Code, preview.Body.String())
	require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &candidates))
	for _, candidate := range candidates.Data {
		byKind[candidate.Kind] = candidate
	}
	for _, kind := range []string{"facts", "content"} {
		candidate := byKind[kind]
		input := bindExistingInput{SchemaVersion: "unirouter.xinsight.bind-existing.v1", Kind: kind,
			CredentialID: candidate.Keys[0].CredentialID, ExpectedStateSHA256: candidate.StateSHA256,
			OperationID: contentIntegrationOperation("bind-existing-" + kind)}
		stale := input
		stale.ExpectedStateSHA256 = "sha256:" + strings.Repeat("0", 64)
		require.Equal(t, 409, request("POST", "bind-existing-stale", stale).Code)
		var label string
		require.NoError(t, f.pool.QueryRow(ctx, `SELECT policy_labels FROM api_keys WHERE id=$1`, input.CredentialID).Scan(&label))
		require.Empty(t, label)
		bound := request("POST", "bind-existing-commit", input)
		require.Equal(t, 200, bound.Code, bound.Body.String())
		require.Equal(t, 200, request("POST", "bind-existing-replay", input).Code)
		changed := input
		changed.ExpectedStateSHA256 = stale.ExpectedStateSHA256
		require.Equal(t, 409, request("POST", "bind-existing-body-change", changed).Code)
		require.NoError(t, f.pool.QueryRow(ctx, `SELECT policy_labels FROM api_keys WHERE id=$1`, input.CredentialID).Scan(&label))
		require.Equal(t, sourcefacts.AutoEnrollmentPolicyLabel, label)
	}
	require.Equal(t, originalIdentity, readIdentity(), "binding must preserve the original key, registration, IP, profiles and scope generation")
	for _, profile := range profiles {
		readOriginal(profile, cursors[profile])
	}
	f.reader = f.source.Handler(resolverBefore)
	f.read(t, factsToken, sourceReadPath+"/capabilities", "127.0.0.1:3000", 200)
	var contentBinding int
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM xinsight_source_enrollment_bindings WHERE enrollment_id=$1 AND state='active'`,
		strings.TrimPrefix(grantID, "xise_")).Scan(&contentBinding))
	require.Equal(t, 2, contentBinding)
	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	contentAuthority, err := f.source.NewContentBodyAuthority(resolver)
	require.NoError(t, err)
	contentRequest := httptest.NewRequest("GET", "/api/v1/integrations/xinsight/source-content", nil)
	contentRequest.Header.Set("Authorization", "Bearer "+contentToken)
	contentRequest.RemoteAddr = "127.0.0.1:3000"
	_, failure := contentAuthority.Authenticate(ctx, contentRequest)
	require.Nil(t, failure)
	// After bind wins, the original content control path can still rotate the
	// credential. Activation revokes the bound old key and its old token.
	postBindRegistration, _ := f.registration(t, "bind-existing-post-bind-rotate", []string{sourcefacts.ContentProfile})
	postBindRotate := contentIntegrationTarget("bind-existing-post-bind-rotate", afterRaceCancel.StateVersion, activeContent)
	postBindRotate.OperationID, postBindRotate.Registration = postBindRegistration.OperationID, &postBindRegistration
	postBindStaged := contentIntegrationHTTP(t, f, "rotate", postBindRotate, 200)
	contentIntegrationHTTP(t, f, "activate", contentIntegrationTarget("bind-existing-post-bind-activate", postBindStaged.StateVersion, postBindStaged), 200)
	_, failure = contentAuthority.Authenticate(ctx, contentRequest)
	require.NotNil(t, failure)
	require.Equal(t, 401, failure.Status)
	r = f.request(t, "bind-existing-revoke", map[string]any{"expected_revision": "1"})
	r.SetPathValue("org_id", stringInt64(f.orgID))
	r.SetPathValue("enrollment_id", grantID)
	w = httptest.NewRecorder()
	f.handler.RevokeXInsightSourceEnrollment(w, r)
	require.Equal(t, 200, w.Code, w.Body.String())
	f.read(t, factsToken, sourceReadPath+"/capabilities", "127.0.0.1:3000", 401)
	_, failure = contentAuthority.Authenticate(ctx, contentRequest)
	require.NotNil(t, failure)
	require.Equal(t, 401, failure.Status)
}
