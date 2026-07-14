package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"testing"
)

func TestPrivateKeyRoundTrip(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encodedPrivate, err := EncodePrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParsePrivateKey(encodedPrivate)
	if err != nil {
		t.Fatal(err)
	}
	if string(parsed) != string(privateKey) {
		t.Fatal("private key round trip mismatch")
	}
	if _, err := EncodePublicKey(publicKey); err != nil {
		t.Fatal(err)
	}
}

func TestParsePublicKeyRing(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodePublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]string{"upgrade-2026-a": encoded})
	if err != nil {
		t.Fatal(err)
	}
	ring, err := ParsePublicKeyRing(string(raw), "UPGRADE_PUBLIC_KEYS")
	if err != nil {
		t.Fatal(err)
	}
	if got := ring["upgrade-2026-a"]; !publicKey.Equal(got) {
		t.Fatal("parsed public key does not match")
	}
	if _, err := ParsePublicKeyRing(`{"bad key":"value"}`, "UPGRADE_PUBLIC_KEYS"); err == nil {
		t.Fatal("expected invalid key id rejection")
	}
	tooMany := make(map[string]string, 33)
	for index := 0; index < 33; index++ {
		tooMany[fmt.Sprintf("key-%02d", index)] = encoded
	}
	tooManyJSON, err := json.Marshal(tooMany)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePublicKeyRing(string(tooManyJSON), "UPGRADE_PUBLIC_KEYS"); err == nil {
		t.Fatal("expected oversized key ring rejection")
	}
}
