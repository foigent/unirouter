package auth

import (
	"strings"
	"testing"
)

func TestValidateLocalPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		valid    bool
		wantErr  string
	}{
		{name: "minimum", password: "correct-horse", valid: true},
		{name: "too short", password: "short-pass", valid: false, wantErr: "at least 12 characters"},
		{name: "bcrypt boundary", password: strings.Repeat("a", MaxLocalPasswordBytes), valid: true},
		{name: "over bcrypt boundary", password: strings.Repeat("a", MaxLocalPasswordBytes+1), valid: false, wantErr: "at most 72 bytes"},
		{name: "unicode characters not bytes", password: "十一字符密码需要足够长", valid: false, wantErr: "at least 12 characters"},
		{name: "twelve supplementary code points", password: strings.Repeat("😀", MinLocalPasswordCharacters), valid: true},
		{name: "exactly 72 multibyte bytes", password: strings.Repeat("界", 24), valid: true},
		{name: "73 multibyte bytes", password: strings.Repeat("界", 23) + "😀", valid: false, wantErr: "at most 72 bytes"},
		{name: "invalid UTF-8 cannot become an unrecoverable credential", password: string([]byte{0xff}) + strings.Repeat("a", 11), valid: false, wantErr: "valid UTF-8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateLocalPassword(tt.password)
			if (err == nil) != tt.valid {
				t.Fatalf("ValidateLocalPassword() error = %v, valid=%v", err, tt.valid)
			}
			if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateLocalPassword() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}
