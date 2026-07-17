package auth

import (
	"errors"
	"testing"
)

func TestNormalizeLocalUsername(t *testing.T) {
	username, err := NormalizeLocalUsername(" \t\u00a0CasePreserved\u3000 ")
	if err != nil {
		t.Fatalf("normalize local username: %v", err)
	}
	if username != "CasePreserved" {
		t.Fatalf("expected display casing without outer whitespace, got %q", username)
	}

	for _, value := range []string{"", " \t\n", "\u00a0\u3000"} {
		if _, err := NormalizeLocalUsername(value); !errors.Is(err, ErrLocalLoginIDRequired) {
			t.Fatalf("expected required error for %q, got %v", value, err)
		}
	}

	for _, whitespace := range []rune{
		'\u0009', '\u000A', '\u000B', '\u000C', '\u000D', '\u0020',
		'\u0085', '\u00A0', '\u1680', '\u2000', '\u2001', '\u2002',
		'\u2003', '\u2004', '\u2005', '\u2006', '\u2007', '\u2008',
		'\u2009', '\u200A', '\u2028', '\u2029', '\u202F', '\u205F',
		'\u3000',
	} {
		username, err := NormalizeLocalUsername(string(whitespace) + "Display" + string(whitespace))
		if err != nil || username != "Display" {
			t.Fatalf("expected U+%04X to trim, got %q err=%v", whitespace, username, err)
		}
	}
}
