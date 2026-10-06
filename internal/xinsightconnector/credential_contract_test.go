package xinsightconnector

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSharedCredentialLifecycleWireFixtures(t *testing.T) {
	for _, name := range []string{"rotate", "recover", "confirm", "prepared", "prepared-microseconds", "committed"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "credential_lifecycle", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var canonical []byte
			switch name {
			case "rotate", "recover":
				r, err := DecodeCredentialRequest(raw)
				if err != nil {
					t.Fatal(err)
				}
				canonical, err = r.CanonicalJSON()
				if err != nil {
					t.Fatal(err)
				}
			case "confirm":
				var c CredentialConfirm
				if err := decodeStrictControlV2(raw, &c); err != nil {
					t.Fatal(err)
				}
				canonical, err = c.CanonicalJSON()
				if err != nil {
					t.Fatal(err)
				}
			default:
				r, err := DecodeCredentialReceipt(raw)
				if err != nil {
					t.Fatal(err)
				}
				canonical, err = r.CanonicalJSON()
				if err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(raw, canonical) {
				t.Fatalf("shared fixture %s changed canonical bytes", name)
			}
		})
	}
}
