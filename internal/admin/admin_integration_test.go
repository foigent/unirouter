//go:build integration

package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

// --- Login handler tests ---

func TestLogin_Success_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	// Create a test user with bcrypt-hashed password
	hash, err := bcrypt.GenerateFromPassword([]byte("testpassword123"), bcrypt.DefaultCost)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(),
		`INSERT INTO users (org_id, username, password_hash, role, status) VALUES ($1, 'testadmin', $2, 'admin', 'active')`,
		orgID, string(hash),
	)
	require.NoError(t, err)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"username":"testadmin","password":"testpassword123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotEmpty(t, resp["token"], "should return JWT token")
	require.Equal(t, "testadmin", resp["username"])
	require.Equal(t, "admin", resp["role"])
}

func TestLogin_WrongPassword_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	hash, err := bcrypt.GenerateFromPassword([]byte("testpassword123"), bcrypt.DefaultCost)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(),
		`INSERT INTO users (org_id, username, password_hash, role, status) VALUES ($1, 'testadmin', $2, 'admin', 'active')`,
		orgID, string(hash),
	)
	require.NoError(t, err)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"username":"testadmin","password":"wrongpassword"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLogin_DisabledUser_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	hash, err := bcrypt.GenerateFromPassword([]byte("testpassword123"), bcrypt.DefaultCost)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(),
		`INSERT INTO users (org_id, username, password_hash, role, status) VALUES ($1, 'disableduser', $2, 'admin', 'disabled')`,
		orgID, string(hash),
	)
	require.NoError(t, err)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"username":"disableduser","password":"testpassword123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestLogin_EmptyCredentials_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"username":"","password":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestLogin_WrongMethod_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil)
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestLogin_UserNotFound_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"username":"nonexistent","password":"whatever"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLogin_InvalidJSON_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// --- CreateAPIKey handler tests ---

func TestCreateAPIKey_Success_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := fmt.Sprintf(`{"org_id":%d,"name":"test-key","rpm_limit":100,"qps_limit":20}`, orgID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/keys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateAPIKey(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

	var resp map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotEmpty(t, resp["key"], "should return raw key")
	require.NotEmpty(t, resp["key_prefix"], "should return key prefix")
	require.NotZero(t, resp["id"])

	// Verify key_prefix starts with expected pattern (ur-XXXX-...)
	keyPrefix := resp["key_prefix"].(string)
	require.Contains(t, keyPrefix, "ur-")

	// Verify key stored in DB
	var keyHash, dbPrefix string
	err = pool.QueryRow(t.Context(),
		`SELECT key_hash, key_prefix FROM api_keys WHERE id = $1`,
		int64(resp["id"].(float64)),
	).Scan(&keyHash, &dbPrefix)
	require.NoError(t, err)
	require.NotEmpty(t, keyHash)
	require.NotEqual(t, resp["key"], keyHash, "stored hash should differ from raw key")
}

func TestCreateAPIKey_NoName_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"org_id":1,"name":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/keys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateAPIKey(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateAPIKey_DefaultLimits_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	// RPM/QPS not specified — should use defaults (60/10)
	body := fmt.Sprintf(`{"org_id":%d,"name":"default-limits-key"}`, orgID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/keys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateAPIKey(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	keyID := int64(resp["id"].(float64))

	// Verify default limits in DB
	var rpm, qps int
	err := pool.QueryRow(t.Context(),
		`SELECT rpm_limit, qps_limit FROM api_keys WHERE id = $1`, keyID,
	).Scan(&rpm, &qps)
	require.NoError(t, err)
	require.Equal(t, 60, rpm, "default rpm_limit should be 60")
	require.Equal(t, 10, qps, "default qps_limit should be 10")
}

func TestCreateAPIKey_OrgDefault_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	// Need an org to exist; org_id=0 is hardcoded to 1 in CreateAPIKey.
	// Create a synthetic org with id=1 via direct insert (bypassing auto-increment).
	_, err := pool.Exec(t.Context(),
		`INSERT INTO orgs (id, name, status, org_type) VALUES (1, 'default-org', 'active', 'standard') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	// Reset sequence if needed
	pool.Exec(t.Context(), `SELECT setval('orgs_id_seq', GREATEST(1, (SELECT COALESCE(MAX(id),0) FROM orgs)))`)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	// org_id=0 should default to 1
	body := `{"org_id":0,"name":"default-org-key"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/keys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateAPIKey(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
}

// --- CreateChannel handler tests ---

func TestCreateChannel_Success_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"name":"test-channel","provider":"openai","base_url":"https://api.openai.com","models":"gpt-4,gpt-3.5"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/channels", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateChannel(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

	var resp map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotZero(t, resp["id"])

	// Verify channel in DB
	var name, status string
	err = pool.QueryRow(t.Context(),
		`SELECT name, status FROM channels WHERE id = $1`,
		int64(resp["id"].(float64)),
	).Scan(&name, &status)
	require.NoError(t, err)
	require.Equal(t, "test-channel", name)
	require.Equal(t, "active", status)
}

func TestCreateChannel_MissingFields_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"name":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/channels", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateChannel(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateChannel_DefaultPriority_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	// priority = 0 should default to 100
	body := `{"name":"default-pri-channel","provider":"openai","base_url":"https://api.openai.com","models":"gpt-4","priority":0}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/channels", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateChannel(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)

	// Verify priority in DB is 100
	var priority int
	err := pool.QueryRow(t.Context(),
		`SELECT priority FROM channels WHERE id = $1`,
		int64(resp["id"].(float64)),
	).Scan(&priority)
	require.NoError(t, err)
	require.Equal(t, 100, priority)
}

func TestCreateChannel_InvalidJSON_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/channels", bytes.NewBufferString("bad json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateChannel(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCleanupOldData_ReleasesStaleFrozen_Integration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 900)

	_, err := pool.Exec(t.Context(),
		`UPDATE wallets SET frozen = 100 WHERE org_id = $1`,
		orgID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(),
		`INSERT INTO freeze_idempotency (request_id, org_id, frozen_amount, status, created_at, updated_at)
		 VALUES ('stale-settle-cleanup', $1, 100, 'frozen', NOW() - INTERVAL '25 hours', NOW() - INTERVAL '25 hours')`,
		orgID,
	)
	require.NoError(t, err)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)
	require.NoError(t, h.CleanupOldData(t.Context(), 30))

	var balance, frozen int
	err = pool.QueryRow(t.Context(), `SELECT balance, frozen FROM wallets WHERE org_id = $1`, orgID).Scan(&balance, &frozen)
	require.NoError(t, err)
	require.Equal(t, 1000, balance)
	require.Equal(t, 0, frozen)

	var status string
	err = pool.QueryRow(t.Context(), `SELECT status FROM freeze_idempotency WHERE request_id = 'stale-settle-cleanup'`).Scan(&status)
	require.NoError(t, err)
	require.Equal(t, "cancelled", status)
}

// --- TopUp handler tests ---

func TestTopUp_Success_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 1000)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"amount_cents":500,"description":"Test top-up"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/recharge", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.TopUp(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	// Verify balance increased
	var balance int
	err := pool.QueryRow(t.Context(), `SELECT balance FROM wallets WHERE org_id = $1`, orgID).Scan(&balance)
	require.NoError(t, err)
	require.Equal(t, 1500, balance, "balance should be 1000 + 500")
}

func TestTopUp_NegativeAmount_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 1000)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"amount_cents":-100}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/recharge", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.TopUp(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Verify balance unchanged
	var balance int
	err := pool.QueryRow(t.Context(), `SELECT balance FROM wallets WHERE org_id = $1`, orgID).Scan(&balance)
	require.NoError(t, err)
	require.Equal(t, 1000, balance, "balance should not change")
}

func TestTopUp_ZeroAmount_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 1000)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"amount_cents":0}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/recharge", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.TopUp(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTopUp_Idempotency_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 1000)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"amount_cents":500,"client_request_id":"idem-test-123"}`

	// First request
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/recharge", bytes.NewBufferString(body))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	h.TopUp(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)

	// Second request with same id — should return 409 Conflict
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/recharge", bytes.NewBufferString(body))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	h.TopUp(rec2, req2)
	require.Equal(t, http.StatusConflict, rec2.Code)

	// Verify balance only increased once
	var balance int
	err := pool.QueryRow(t.Context(), `SELECT balance FROM wallets WHERE org_id = $1`, orgID).Scan(&balance)
	require.NoError(t, err)
	require.Equal(t, 1500, balance, "balance should be 1000 + 500, not 1000 + 1000")
}

// --- CreateGrant handler tests ---

func TestCreateGrant_Success_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 1000)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"org_id":0,"amount_cents":500,"reason":"Test grant"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/grants", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateGrant(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

	var resp map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotZero(t, resp["id"])

	// Verify wallet balance increased
	var balance int
	err = pool.QueryRow(t.Context(), `SELECT balance FROM wallets WHERE org_id = $1`, orgID).Scan(&balance)
	require.NoError(t, err)
	require.Equal(t, 1500, balance, "balance should be 1000 + 500")
}

func TestCreateGrant_Idempotency_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 1000)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"org_id":0,"amount_cents":500,"reason":"Test grant","client_request_id":"grant-idem-1"}`

	// First request
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/grants", bytes.NewBufferString(body))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	h.CreateGrant(rec1, req1)
	require.Equal(t, http.StatusCreated, rec1.Code)

	// Second request with same client_request_id — should return existing id
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/grants", bytes.NewBufferString(body))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	h.CreateGrant(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code, "body: %s", rec2.Body.String())

	var resp map[string]interface{}
	json.Unmarshal(rec2.Body.Bytes(), &resp)
	require.Equal(t, true, resp["duplicate"])

	// Verify balance only increased once
	var balance int
	err := pool.QueryRow(t.Context(), `SELECT balance FROM wallets WHERE org_id = $1`, orgID).Scan(&balance)
	require.NoError(t, err)
	require.Equal(t, 1500, balance, "balance should be 1000 + 500, not 2000")
}

func TestCreateGrant_ZeroAmount_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 1000)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"amount_cents":0,"reason":"Zero grant"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/grants", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateGrant(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Verify wallet unchanged
	var balance int
	err := pool.QueryRow(t.Context(), `SELECT balance FROM wallets WHERE org_id = $1`, orgID).Scan(&balance)
	require.NoError(t, err)
	require.Equal(t, 1000, balance)
}

func TestCreateGrant_InvalidJSON_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/grants", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateGrant(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// --- GetWallet handler tests ---

func TestGetWallet_Success_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 5000)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet", nil)
	rec := httptest.NewRecorder()

	h.GetWallet(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, float64(5000), resp["balance_cents"])
}

func TestGetWallet_NotFound_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	// No wallet created

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet", nil)
	rec := httptest.NewRecorder()

	h.GetWallet(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

// --- BalanceAdjust handler tests ---

func TestBalanceAdjust_Success_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 1000)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"target_balance":500,"description":"Admin adjustment"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/adjust", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.BalanceAdjust(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, true, resp["success"])
	require.Equal(t, float64(1000), resp["previous"])
	require.Equal(t, float64(500), resp["new_balance"])
	require.Equal(t, float64(-500), resp["adjust_amount"])

	// Verify balance in DB
	var balance int
	err = pool.QueryRow(t.Context(), `SELECT balance FROM wallets WHERE org_id = $1`, orgID).Scan(&balance)
	require.NoError(t, err)
	require.Equal(t, 500, balance)
}

func TestBalanceAdjust_NegativeTarget_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 1000)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"target_balance":-100}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/adjust", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.BalanceAdjust(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Verify balance unchanged
	var balance int
	err := pool.QueryRow(t.Context(), `SELECT balance FROM wallets WHERE org_id = $1`, orgID).Scan(&balance)
	require.NoError(t, err)
	require.Equal(t, 1000, balance)
}

func TestBalanceAdjust_NoWallet_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	// No wallet created

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"target_balance":500}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/adjust", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.BalanceAdjust(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestBalanceAdjust_WrongMethod_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 1000)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet/adjust", nil)
	rec := httptest.NewRecorder()

	h.BalanceAdjust(rec, req)

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestBalanceAdjust_InvalidJSON_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 1000)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallet/adjust", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.BalanceAdjust(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// --- Phase B: Additional handler integration tests ---

// --- User management ---

func TestCreateUser_Success_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := fmt.Sprintf(`{"org_id":%d,"username":"newuser","password":"pass123","role":"org_admin"}`, orgID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateUser(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
	var resp map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotZero(t, resp["id"])

	// Verify in DB
	var username, role string
	err = pool.QueryRow(t.Context(),
		`SELECT username, role FROM users WHERE id = $1`, resp["id"]).Scan(&username, &role)
	require.NoError(t, err)
	require.Equal(t, "newuser", username)
	require.Equal(t, "org_admin", role)
}

func TestCreateUser_DuplicateUsername_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := fmt.Sprintf(`{"org_id":%d,"username":"dupuser","password":"pass123"}`, orgID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.CreateUser(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)

	// Duplicate
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewBufferString(body))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	h.CreateUser(rec2, req2)
	require.Equal(t, http.StatusConflict, rec2.Code)
}

func TestCreateUser_MissingFields_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	_ = testutil.NewTestOrg(t, pool)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"org_id":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateUser(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateUser_DefaultRole_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := fmt.Sprintf(`{"org_id":%d,"username":"defaultrole","password":"pass123"}`, orgID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateUser(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
	var resp map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	var role string
	err = pool.QueryRow(t.Context(), `SELECT role FROM users WHERE id = $1`, resp["id"]).Scan(&role)
	require.NoError(t, err)
	require.Equal(t, "org_viewer", role, "default role should be org_viewer")
}

func TestDeleteUser_Success_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	// Create user directly in DB
	var userID int64
	err := pool.QueryRow(t.Context(),
		`INSERT INTO users (org_id, username, password_hash, role) VALUES ($1, 'todelete', 'hash', 'org_viewer') RETURNING id`,
		orgID).Scan(&userID)
	require.NoError(t, err)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", userID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", userID))
	rec := httptest.NewRecorder()

	h.DeleteUser(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	// Verify deleted
	var count int
	pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM users WHERE id = $1`, userID).Scan(&count)
	require.Equal(t, 0, count)
}

func TestDeleteUser_NotFound_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	_ = testutil.NewTestOrg(t, pool)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/99999", nil)
	req.SetPathValue("id", "99999")
	rec := httptest.NewRecorder()

	h.DeleteUser(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDeleteUser_InvalidID_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	_ = testutil.NewTestOrg(t, pool)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/abc", nil)
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	h.DeleteUser(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUpdateUser_RoleChange_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	var userID int64
	err := pool.QueryRow(t.Context(),
		`INSERT INTO users (org_id, username, password_hash, role) VALUES ($1, 'roletest', 'hash', 'org_viewer') RETURNING id`,
		orgID).Scan(&userID)
	require.NoError(t, err)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"role":"org_admin"}`
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/users/%d", userID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", fmt.Sprintf("%d", userID))
	rec := httptest.NewRecorder()

	h.UpdateUser(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var role string
	pool.QueryRow(t.Context(), `SELECT role FROM users WHERE id = $1`, userID).Scan(&role)
	require.Equal(t, "org_admin", role)
}

func TestUpdateUser_StatusChange_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	var userID int64
	err := pool.QueryRow(t.Context(),
		`INSERT INTO users (org_id, username, password_hash, role, status) VALUES ($1, 'statustest', 'hash', 'org_viewer', 'active') RETURNING id`,
		orgID).Scan(&userID)
	require.NoError(t, err)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"status":"disabled"}`
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/users/%d", userID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", fmt.Sprintf("%d", userID))
	rec := httptest.NewRecorder()

	h.UpdateUser(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var status string
	pool.QueryRow(t.Context(), `SELECT status FROM users WHERE id = $1`, userID).Scan(&status)
	require.Equal(t, "disabled", status)
}

func TestUpdateUser_NotFound_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	_ = testutil.NewTestOrg(t, pool)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"role":"admin"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/99999", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "99999")
	rec := httptest.NewRecorder()

	h.UpdateUser(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestListUsers_Success_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	// Create 3 users
	for i, role := range []string{"org_admin", "org_viewer", "org_viewer"} {
		pool.Exec(t.Context(),
			`INSERT INTO users (org_id, username, password_hash, role) VALUES ($1, $2, 'hash', $3)`,
			orgID, fmt.Sprintf("user%d", i), role)
	}

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	rec := httptest.NewRecorder()

	h.ListUsers(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	data := resp["data"].([]interface{})
	require.GreaterOrEqual(t, len(data), 3)
}

// --- API Key management ---

func TestUpdateAPIKey_Success_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	// Create key first
	var keyID int64
	err := pool.QueryRow(t.Context(),
		`INSERT INTO api_keys (org_id, name, key_hash, key_prefix, status, rpm_limit, qps_limit, allowed_ips, allowed_models, created_at, updated_at)
		 VALUES ($1, 'test-key', 'hash123', 'pk_test', 'active', 60, 10, '', '', NOW(), NOW()) RETURNING id`,
		orgID).Scan(&keyID)
	require.NoError(t, err)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"name":"updated-key","rpm_limit":120}`
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/keys/%d", keyID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", fmt.Sprintf("%d", keyID))
	rec := httptest.NewRecorder()

	h.UpdateAPIKey(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var name string
	var rpmLimit int
	pool.QueryRow(t.Context(), `SELECT name, rpm_limit FROM api_keys WHERE id = $1`, keyID).Scan(&name, &rpmLimit)
	require.Equal(t, "updated-key", name)
	require.Equal(t, 120, rpmLimit)
}

func TestUpdateAPIKey_NotFound_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	_ = testutil.NewTestOrg(t, pool)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{"name":"updated"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/keys/99999", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "99999")
	rec := httptest.NewRecorder()

	h.UpdateAPIKey(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUpdateAPIKey_NoFields_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	var keyID int64
	pool.QueryRow(t.Context(),
		`INSERT INTO api_keys (org_id, name, key_hash, key_prefix, status, rpm_limit, qps_limit, allowed_ips, allowed_models, created_at, updated_at)
		 VALUES ($1, 'test-key', 'hash123', 'pk_test', 'active', 60, 10, '', '', NOW(), NOW()) RETURNING id`,
		orgID).Scan(&keyID)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	body := `{}`
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/keys/%d", keyID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", fmt.Sprintf("%d", keyID))
	rec := httptest.NewRecorder()

	h.UpdateAPIKey(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDisableAPIKey_Success_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	var keyID int64
	pool.QueryRow(t.Context(),
		`INSERT INTO api_keys (org_id, name, key_hash, key_prefix, status, rpm_limit, qps_limit, allowed_ips, allowed_models, created_at, updated_at)
		 VALUES ($1, 'test-key', 'hash123', 'pk_test', 'active', 60, 10, '', '', NOW(), NOW()) RETURNING id`,
		orgID).Scan(&keyID)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/keys/%d/disable", keyID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", keyID))
	rec := httptest.NewRecorder()

	h.DisableAPIKey(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var status string
	pool.QueryRow(t.Context(), `SELECT status FROM api_keys WHERE id = $1`, keyID).Scan(&status)
	require.Equal(t, "disabled", status)
}

func TestDisableAPIKey_NotFound_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	_ = testutil.NewTestOrg(t, pool)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/keys/99999/disable", nil)
	req.SetPathValue("id", "99999")
	rec := httptest.NewRecorder()

	h.DisableAPIKey(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestListAPIKeys_Success_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	// Create a key via handler
	body := fmt.Sprintf(`{"org_id":%d,"name":"list-test-key"}`, orgID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/keys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.CreateAPIKey(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "create key: %s", rec.Body.String())

	// List
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/keys", nil)
	rec2 := httptest.NewRecorder()
	h.ListAPIKeys(rec2, req2)

	require.Equal(t, http.StatusOK, rec2.Code, "body: %s", rec2.Body.String())
	var resp map[string]interface{}
	err := json.Unmarshal(rec2.Body.Bytes(), &resp)
	require.NoError(t, err)
	data := resp["data"].([]interface{})
	require.GreaterOrEqual(t, len(data), 1, "should have at least 1 key")
}
