package ipallowlist

import "testing"

func TestNormalizeIsCanonicalAndRejectsAmbiguity(t *testing.T) {
	got, err := Normalize(" 2001:db8::1,192.0.2.9,192.0.2.0/24,192.0.2.9 ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "192.0.2.0/24,192.0.2.9,2001:db8::1" {
		t.Fatalf("normalized allowlist = %q", got)
	}
	for _, invalid := range []string{
		"not-an-ip", "192.0.2.1,", "0.0.0.0/0", "::/0", "::ffff:192.0.2.1",
	} {
		if _, err := Normalize(invalid); err == nil {
			t.Fatalf("invalid allowlist %q accepted", invalid)
		}
	}
}

func TestMatchesCanonicalAndLegacyEntries(t *testing.T) {
	if !Matches("192.0.2.9", "192.0.2.0/24") || !Matches("2001:db8::1", "2001:db8::1") {
		t.Fatal("valid IP allowlist did not match")
	}
	if Matches("198.51.100.1", "invalid,192.0.2.0/24") {
		t.Fatal("invalid legacy entry expanded access")
	}
	if !Matches("198.51.100.1", "") {
		t.Fatal("empty allowlist must remain unrestricted")
	}
}
