package licensestudio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/licensing"
)

const (
	testToken = "studio-test-token"
	testHost  = "127.0.0.1:43123"
)

type fakeDialogs struct {
	directory string
	openFiles []string
	saveFiles []string
	err       error
}

func (f *fakeDialogs) ChooseDirectory(context.Context, string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.directory, nil
}

func (f *fakeDialogs) ChooseOpenFile(context.Context, string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if len(f.openFiles) == 0 {
		return "", errors.New("no fake open file configured")
	}
	path := f.openFiles[0]
	f.openFiles = f.openFiles[1:]
	return path, nil
}

func (f *fakeDialogs) ChooseSaveFile(context.Context, string, string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if len(f.saveFiles) == 0 {
		return "", errors.New("no fake save file configured")
	}
	path := f.saveFiles[0]
	f.saveFiles = f.saveFiles[1:]
	return path, nil
}

func TestServerEndToEndKeygenIssueVerify(t *testing.T) {
	directory := t.TempDir()
	dialogs := &fakeDialogs{directory: directory}
	handler := newTestServer(t, dialogs)

	keygen := callAPI(t, handler, "keygen", map[string]string{"purpose": "license", "key_id": "license-test-a"})
	if keygen.Code != http.StatusCreated {
		t.Fatalf("keygen status=%d body=%s", keygen.Code, keygen.Body.String())
	}
	privatePath := filepath.Join(directory, "license-test-a.private")
	publicPath := filepath.Join(directory, "license-test-a.public-keys.json")
	licensePath := filepath.Join(directory, "customer.license.json")
	dialogs.openFiles = []string{privatePath}
	dialogs.saveFiles = []string{licensePath}
	payload := licensing.Payload{
		LicenseID: "lic-http", Customer: "HTTP Customer", DeploymentID: "http-deployment", Tier: "enterprise",
		IssuedAt: "2026-07-15T00:00:00Z", NotBefore: "2026-07-15T00:00:00Z", ExpiresAt: "2027-07-15T23:59:59Z",
		Features: []string{licensing.FeatureAll}, Limits: map[string]int64{licensing.LimitActiveUsers: 20},
	}
	issued := callAPI(t, handler, "issue", issueRequest{KeyID: "license-test-a", Payload: payload})
	if issued.Code != http.StatusCreated {
		t.Fatalf("issue status=%d body=%s", issued.Code, issued.Body.String())
	}

	dialogs.openFiles = []string{publicPath, licensePath}
	verified := callAPI(t, handler, "verify", verifyRequest{DeploymentID: "http-deployment"})
	if verified.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", verified.Code, verified.Body.String())
	}
	var status licensing.Status
	if err := json.Unmarshal(verified.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Valid || !status.Enforceable || !status.SignatureValid || !status.DeploymentMatch {
		t.Fatalf("unexpected verification status: %+v", status)
	}
}

func TestServerProtectsLocalSessionAndAssets(t *testing.T) {
	handler := newTestServer(t, &fakeDialogs{directory: t.TempDir()})
	page := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://"+testHost+"/"+testToken+"/", nil)
	request.Host = testHost
	request.RemoteAddr = "127.0.0.1:55555"
	handler.ServeHTTP(page, request)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "UniRouter License Studio") {
		t.Fatalf("page status=%d body=%s", page.Code, page.Body.String())
	}
	if !strings.Contains(page.Header().Get("Content-Security-Policy"), "connect-src 'self'") || page.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("missing security headers: %v", page.Header())
	}

	wrongOrigin := httptest.NewRecorder()
	apiRequest := newAPIRequest(t, "keygen", map[string]string{"purpose": "license", "key_id": "license-test-a"})
	apiRequest.Header.Set("Origin", "https://attacker.invalid")
	handler.ServeHTTP(wrongOrigin, apiRequest)
	if wrongOrigin.Code != http.StatusForbidden {
		t.Fatalf("wrong-origin status=%d body=%s", wrongOrigin.Code, wrongOrigin.Body.String())
	}

	remote := httptest.NewRecorder()
	remoteRequest := newAPIRequest(t, "keygen", map[string]string{"purpose": "license", "key_id": "license-test-a"})
	remoteRequest.RemoteAddr = "192.0.2.44:55555"
	handler.ServeHTTP(remote, remoteRequest)
	if remote.Code != http.StatusNotFound {
		t.Fatalf("non-loopback status=%d body=%s", remote.Code, remote.Body.String())
	}
}

func TestServerReportsDialogCancellationWithoutWriting(t *testing.T) {
	directory := t.TempDir()
	handler := newTestServer(t, &fakeDialogs{directory: directory, err: ErrDialogCancelled})
	response := callAPI(t, handler, "keygen", map[string]string{"purpose": "license", "key_id": "license-test-a"})
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "cancelled") {
		t.Fatalf("cancel status=%d body=%s", response.Code, response.Body.String())
	}
	entries, err := filepath.Glob(filepath.Join(directory, "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("dialog cancellation wrote files: %v", entries)
	}
}

func newTestServer(t *testing.T, dialogs FileDialogs) http.Handler {
	t.Helper()
	handler, err := NewServer(ServerOptions{
		Dialogs: dialogs, SessionToken: testToken, AllowedHost: testHost,
		Now: func() time.Time { return time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func callAPI(t *testing.T, handler http.Handler, endpoint string, payload interface{}) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, newAPIRequest(t, endpoint, payload))
	return response
}

func newAPIRequest(t *testing.T, endpoint string, payload interface{}) *http.Request {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://"+testHost+"/"+testToken+"/api/"+endpoint, bytes.NewReader(raw))
	request.Host = testHost
	request.RemoteAddr = "127.0.0.1:55555"
	request.Header.Set("Origin", "http://"+testHost)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-UniRouter-Studio-Token", testToken)
	return request
}
