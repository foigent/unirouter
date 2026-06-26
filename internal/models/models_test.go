package models

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"
)

func TestOrganization_JSONRoundtrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	org := Organization{
		ID:        1,
		Name:      "Test Org",
		Status:    "active",
		Settings:  json.RawMessage(`{"key":"value"}`),
		OrgType:   "customer",
		CreatedAt: now,
		UpdatedAt: now,
	}
	data, err := json.Marshal(org)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded Organization
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Name != org.Name || decoded.Status != org.Status || decoded.OrgType != org.OrgType {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", org, decoded)
	}
}

func TestUser_PasswordHashHidden(t *testing.T) {
	u := User{ID: 1, Username: "admin", PasswordHash: "secret-hash"}
	data, _ := json.Marshal(u)
	if contains(data, "secret-hash") {
		t.Fatal("PasswordHash should be hidden in JSON (json:\"-\")")
	}
	if !contains(data, "admin") {
		t.Fatal("Username should be present in JSON")
	}
}

func TestChannelKey_KeyEncHidden(t *testing.T) {
	k := ChannelKey{ID: 1, KeyEnc: "encrypted-secret"}
	data, _ := json.Marshal(k)
	if contains(data, "encrypted-secret") {
		t.Fatal("KeyEnc should be hidden in JSON")
	}
}

func TestApiKey_KeyHashHidden(t *testing.T) {
	k := ApiKey{ID: 1, Name: "test-key", KeyHash: "hash-value"}
	data, _ := json.Marshal(k)
	if contains(data, "hash-value") {
		t.Fatal("KeyHash should be hidden in JSON")
	}
	if !contains(data, "test-key") {
		t.Fatal("Name should be present in JSON")
	}
}

func TestWallet_BalanceFields(t *testing.T) {
	w := Wallet{OrgID: 1, Balance: 1000, GrantedBalance: 200, Frozen: 50, TotalSpent: 300}
	data, _ := json.Marshal(w)
	var decoded Wallet
	json.Unmarshal(data, &decoded)
	if decoded.Balance != 1000 || decoded.Frozen != 50 || decoded.TotalSpent != 300 {
		t.Fatalf("wallet roundtrip mismatch: %+v", decoded)
	}
}

func TestCallLog_NullableFields(t *testing.T) {
	statusCode := 500
	cl := CallLog{
		ID:                 1,
		RequestID:          "req-123",
		Model:              "gpt-4o",
		UpstreamStatusCode: &statusCode,
		UpstreamError:      sql.NullString{String: "rate limited", Valid: true},
		IsStream:           true,
	}
	data, _ := json.Marshal(cl)
	if !contains(data, "rate limited") {
		t.Fatal("upstream error should be present")
	}
	if !contains(data, "true") {
		t.Fatal("is_stream should be true")
	}
}

func TestSetting_JSONValue(t *testing.T) {
	s := Setting{
		Key:         "site_name",
		Value:       json.RawMessage(`"UniRoute"`),
		Description: "Site display name",
	}
	data, _ := json.Marshal(s)
	var decoded Setting
	json.Unmarshal(data, &decoded)
	if decoded.Key != "site_name" {
		t.Fatalf("key mismatch: %s", decoded.Key)
	}
}

func TestTransaction_Types(t *testing.T) {
	types := []string{"freeze", "settle", "cancel", "recharge", "grant", "deduction"}
	for _, typ := range types {
		tx := Transaction{Type: typ, Status: "completed", Amount: 100}
		data, _ := json.Marshal(tx)
		if !contains(data, typ) {
			t.Fatalf("type %q should be in JSON", typ)
		}
	}
}

func TestGrant_NullableExpiresAt(t *testing.T) {
	g := Grant{ID: 1, ExpiresAt: nil}
	data, _ := json.Marshal(g)
	if !contains(data, "null") {
		t.Fatal("nil ExpiresAt should marshal as null")
	}
}

func contains(data []byte, s string) bool {
	return len(data) > 0 && len(s) > 0 && jsonContains(data, s)
}

func jsonContains(data []byte, s string) bool {
	str := string(data)
	for i := 0; i <= len(str)-len(s); i++ {
		if str[i:i+len(s)] == s {
			return true
		}
	}
	return false
}
