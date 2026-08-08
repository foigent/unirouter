package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/config"
)

func TestRunCLI_IgnoresServerMode(t *testing.T) {
	handled, err := runCLI(context.Background(), nil, &config.Config{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if handled {
		t.Fatal("expected no CLI handling without args")
	}
}

func TestRunCLI_AdminSetPasswordRejectsNonTerminalInputWithoutStdinFlag(t *testing.T) {
	handled, err := runCLIWithAdminPasswordInput(context.Background(),
		[]string{"admin", "set-password", "--username", "admin"},
		&config.Config{}, nonTerminalAdminPasswordInput("ignored"))
	if !handled {
		t.Fatal("expected admin CLI to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "standard input is not a terminal") || !strings.Contains(err.Error(), "--password-stdin") {
		t.Fatalf("expected actionable non-terminal password error, got %v", err)
	}
}

func TestRunCLI_AdminRejectsPasswordValueInArgumentsBeforeDatabaseAccess(t *testing.T) {
	const exposedPassword = "argv-secret-password-123"
	for _, args := range [][]string{
		{"admn", "--password=" + exposedPassword},
		{"admin", "--password=" + exposedPassword},
		{"admin", "set-password", "--username", "admin", "--password", exposedPassword},
		{"admin", "set-password", "--username", "admin", "--password=" + exposedPassword},
		{"admin", "create", "--username", "operator", "-password", exposedPassword},
		{"admin", "create", "--username", "operator", "-password=" + exposedPassword},
	} {
		handled, err := runCLIWithAdminPasswordInput(context.Background(), args, &config.Config{}, nonTerminalAdminPasswordInput("ignored"))
		if !handled {
			t.Fatal("expected admin CLI to be handled")
		}
		if err == nil || !strings.Contains(err.Error(), "--password is not supported") {
			t.Fatalf("expected argv password rejection before database access, got %v", err)
		}
		if strings.Contains(err.Error(), exposedPassword) {
			t.Fatalf("error must not echo the password value: %v", err)
		}
	}
}

func TestRunCLI_AdminInvalidFlagDoesNotEchoAttachedValue(t *testing.T) {
	const exposedValue = "attached-secret-value-123"
	handled, err := runCLIWithAdminPasswordInput(context.Background(), []string{
		"admin", "set-password", "--username", "admin", "--passwd=" + exposedValue,
	}, &config.Config{}, nonTerminalAdminPasswordInput("ignored"))
	if !handled {
		t.Fatal("expected admin CLI to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "invalid admin set-password arguments") {
		t.Fatalf("expected sanitized invalid-flag error, got %v", err)
	}
	if strings.Contains(err.Error(), exposedValue) {
		t.Fatalf("error must not echo the attached value: %v", err)
	}
}

func TestRunCLI_AdminUnknownCommandDoesNotEchoArgument(t *testing.T) {
	const exposedValue = "unexpected-secret-value-123"
	handled, err := runCLIWithAdminPasswordInput(context.Background(),
		[]string{"admin", exposedValue}, &config.Config{}, nonTerminalAdminPasswordInput("ignored"))
	if !handled {
		t.Fatal("expected admin CLI to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "supported: create, set-password") {
		t.Fatalf("expected generic unknown command error, got %v", err)
	}
	if strings.Contains(err.Error(), exposedValue) {
		t.Fatalf("error must not echo the unknown argv token: %v", err)
	}
}

func TestRunCLI_AdminRejectsUnexpectedPositionalArgumentWithoutEcho(t *testing.T) {
	const exposedPassword = "positional-secret-password-123"
	handled, err := runCLIWithAdminPasswordInput(context.Background(), []string{
		"admin", "set-password", "--username", "admin", exposedPassword,
	}, &config.Config{}, nonTerminalAdminPasswordInput("ignored"))
	if !handled {
		t.Fatal("expected admin CLI to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "unexpected positional argument") {
		t.Fatalf("expected positional argument rejection, got %v", err)
	}
	if strings.Contains(err.Error(), exposedPassword) {
		t.Fatalf("error must not echo the positional value: %v", err)
	}
}

func TestRunCLI_AdminRefusesDefaultPassword(t *testing.T) {
	handled, err := runCLIWithAdminPasswordInput(context.Background(),
		[]string{"admin", "set-password", "--username", "unicode-\u0130dentity", "--password-stdin"},
		&config.Config{}, nonTerminalAdminPasswordInput("admin123456\n"))
	if !handled {
		t.Fatal("expected admin CLI to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "public default admin password") {
		t.Fatalf("expected default password refusal, got %v", err)
	}
}

func TestRunCLI_AdminRefusesWeakPassword(t *testing.T) {
	handled, err := runCLIWithAdminPasswordInput(context.Background(),
		[]string{"admin", "set-password", "--username", "admin", "--password-stdin"},
		&config.Config{}, nonTerminalAdminPasswordInput("short-pass\n"))
	if !handled {
		t.Fatal("expected admin CLI to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "at least 12 characters") {
		t.Fatalf("expected password policy refusal, got %v", err)
	}
}

func TestRunCLI_AdminCreateRejectsInvalidUTF8PasswordBeforeDatabaseAccess(t *testing.T) {
	invalidPassword := append([]byte{0xff}, []byte(strings.Repeat("a", 11)+"\n")...)
	handled, err := runCLIWithAdminPasswordInput(context.Background(),
		[]string{"admin", "create", "--username", "operator", "--password-stdin"},
		&config.Config{}, nonTerminalAdminPasswordInput(string(invalidPassword)))
	if !handled {
		t.Fatal("expected admin CLI to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
		t.Fatalf("expected invalid UTF-8 rejection before database access, got %v", err)
	}
}

func TestAdminCreateRoleNormalizationRejectsProjectRoles(t *testing.T) {
	for _, test := range []struct {
		input                 string
		wantOrganizationRole  string
		wantCompatibilityRole string
	}{
		{input: auth.RoleLegacyAdmin, wantOrganizationRole: auth.RolePlatformAdmin, wantCompatibilityRole: auth.RoleLegacyAdmin},
		{input: auth.RolePlatformAdmin, wantOrganizationRole: auth.RolePlatformAdmin, wantCompatibilityRole: auth.RolePlatformAdmin},
		{input: auth.RoleOrgAdmin, wantOrganizationRole: auth.RoleOrgAdmin, wantCompatibilityRole: auth.RoleOrgAdmin},
		{input: auth.RoleBillingAdmin, wantOrganizationRole: auth.RoleBillingAdmin, wantCompatibilityRole: auth.RoleBillingAdmin},
		{input: auth.RoleOrgViewer, wantOrganizationRole: auth.RoleOrgViewer, wantCompatibilityRole: auth.RoleOrgViewer},
		{input: auth.RoleMember, wantOrganizationRole: auth.RoleMember, wantCompatibilityRole: auth.RoleMember},
	} {
		organizationRole, compatibilityRole, err := normalizeAdminCreateRoles(test.input)
		if err != nil {
			t.Fatalf("normalizeAdminCreateRoles(%q): %v", test.input, err)
		}
		if organizationRole != test.wantOrganizationRole || compatibilityRole != test.wantCompatibilityRole {
			t.Fatalf("normalizeAdminCreateRoles(%q) = %q/%q, want %q/%q", test.input, organizationRole, compatibilityRole, test.wantOrganizationRole, test.wantCompatibilityRole)
		}
	}

	for _, role := range []string{
		auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer,
		auth.ProjectRoleUser, auth.ProjectRoleViewer, auth.LegacyProjectRoleDeveloper,
		"unknown_role",
	} {
		if _, _, err := normalizeAdminCreateRoles(role); err == nil {
			t.Fatalf("project/unknown role %q must not be accepted by admin create", role)
		}
	}
}

func TestRunCLIAdminCreateRejectsProjectRoleBeforePasswordOrDatabase(t *testing.T) {
	input := nonTerminalAdminPasswordInput("must-not-be-read\n")
	input.stdin = errorReader{err: errors.New("password reader must not be reached")}
	handled, err := runCLIWithAdminPasswordInput(context.Background(), []string{
		"admin", "create", "--username", "operator", "--role", auth.RoleProjectAdmin, "--password-stdin",
	}, &config.Config{}, input)
	if !handled {
		t.Fatal("expected admin CLI to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "invalid --role") {
		t.Fatalf("expected project role rejection before password/database work, got %v", err)
	}
}

func TestReadAdminPasswordLinePreservesPasswordCharacters(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "leading and trailing spaces", input: "  exact password value  \n", want: "  exact password value  "},
		{name: "CRLF terminator", input: "password-with-crlf\r\n", want: "password-with-crlf"},
		{name: "LF terminator", input: "password-with-lf\n", want: "password-with-lf"},
		{name: "EOF without terminator", input: "password-at-eof", want: "password-at-eof"},
		{name: "maximum bytes", input: strings.Repeat("a", auth.MaxLocalPasswordBytes) + "\n", want: strings.Repeat("a", auth.MaxLocalPasswordBytes)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readAdminPasswordLine(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("readAdminPasswordLine() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("readAdminPasswordLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadAdminPasswordLineRejectsInvalidInput(t *testing.T) {
	readFailure := errors.New("read failed")
	for _, tt := range []struct {
		name  string
		input io.Reader
		want  string
	}{
		{name: "over maximum with newline", input: strings.NewReader(strings.Repeat("a", auth.MaxLocalPasswordBytes+1) + "\n"), want: "exceeds 72 bytes"},
		{name: "over maximum without newline", input: strings.NewReader(strings.Repeat("a", auth.MaxLocalPasswordBytes+1)), want: "exceeds 72 bytes"},
		{name: "second line", input: strings.NewReader("first-password\nsecond-password\n"), want: "exactly one line"},
		{name: "extra byte", input: strings.NewReader("first-password\nx"), want: "exactly one line"},
		{name: "reader error", input: errorReader{err: readFailure}, want: "read failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readAdminPasswordLine(tt.input)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("readAdminPasswordLine() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestRunCLI_AdminRejectsEmptyPasswordFromStdin(t *testing.T) {
	handled, err := runCLIWithAdminPasswordInput(context.Background(),
		[]string{"admin", "set-password", "--username", "admin", "--password-stdin"},
		&config.Config{}, nonTerminalAdminPasswordInput("\n"))
	if !handled {
		t.Fatal("expected admin CLI to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "at least 12 characters") {
		t.Fatalf("expected empty password policy rejection, got %v", err)
	}
}

func TestReadAdminPasswordRejectsStdinFlagOnTerminalWithoutEcho(t *testing.T) {
	hiddenReads := 0
	input := adminPasswordInput{
		stdin:        strings.NewReader("must-not-be-read\n"),
		promptWriter: io.Discard,
		stdinFD:      9,
		isTerminal:   func(fd int) bool { return fd == 9 },
		readHidden: func(int) ([]byte, error) {
			hiddenReads++
			return []byte("must-not-be-read"), nil
		},
	}
	_, err := readAdminPassword(input, true)
	if err == nil || !strings.Contains(err.Error(), "requires non-terminal standard input") {
		t.Fatalf("expected TTY --password-stdin rejection, got %v", err)
	}
	if hiddenReads != 0 {
		t.Fatalf("TTY --password-stdin must not read an echoed or hidden value, got %d reads", hiddenReads)
	}
}

func TestReadAdminPasswordUsesHiddenConfirmedPrompt(t *testing.T) {
	var prompts bytes.Buffer
	values := [][]byte{[]byte("hidden-password-123"), []byte("hidden-password-123")}
	reads := 0
	input := adminPasswordInput{
		promptWriter: &prompts,
		stdinFD:      11,
		isTerminal:   func(fd int) bool { return fd == 11 },
		readHidden: func(fd int) ([]byte, error) {
			if fd != 11 {
				t.Fatalf("readHidden fd = %d, want 11", fd)
			}
			value := append([]byte(nil), values[reads]...)
			reads++
			return value, nil
		},
	}
	got, err := readAdminPassword(input, false)
	if err != nil {
		t.Fatalf("readAdminPassword() error = %v", err)
	}
	if got != "hidden-password-123" {
		t.Fatalf("readAdminPassword() = %q", got)
	}
	if reads != 2 {
		t.Fatalf("hidden read count = %d, want 2", reads)
	}
	if prompts.String() != "New password: \nConfirm new password: \n" {
		t.Fatalf("unexpected hidden prompts %q", prompts.String())
	}
}

func TestReadAdminPasswordRejectsHiddenConfirmationMismatch(t *testing.T) {
	values := [][]byte{[]byte("hidden-password-123"), []byte("different-password-456")}
	reads := 0
	input := adminPasswordInput{
		promptWriter: io.Discard,
		stdinFD:      12,
		isTerminal:   func(int) bool { return true },
		readHidden: func(int) ([]byte, error) {
			value := append([]byte(nil), values[reads]...)
			reads++
			return value, nil
		},
	}
	_, err := readAdminPassword(input, false)
	if err == nil || !strings.Contains(err.Error(), "confirmation does not match") {
		t.Fatalf("expected hidden confirmation mismatch, got %v", err)
	}
}

func TestReadAdminPasswordRejectsHiddenReadFailureAndOversize(t *testing.T) {
	readFailure := errors.New("hidden read failed")
	for _, tt := range []struct {
		name       string
		readHidden func(int) ([]byte, error)
		want       string
	}{
		{
			name: "reader failure",
			readHidden: func(int) ([]byte, error) {
				return nil, readFailure
			},
			want: "hidden read failed",
		},
		{
			name: "over maximum",
			readHidden: func(int) ([]byte, error) {
				return []byte(strings.Repeat("a", auth.MaxLocalPasswordBytes+1)), nil
			},
			want: "exceeds 72 bytes",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := adminPasswordInput{
				promptWriter: io.Discard,
				stdinFD:      13,
				isTerminal:   func(int) bool { return true },
				readHidden:   tt.readHidden,
			}
			_, err := readAdminPassword(input, false)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("readAdminPassword() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestRunCLI_AdminRejectsWhitespaceOnlyUsernameBeforePasswordRead(t *testing.T) {
	reads := 0
	input := adminPasswordInput{
		stdin:        errorReader{err: errors.New("unexpected stdin read")},
		promptWriter: io.Discard,
		stdinFD:      -1,
		isTerminal:   func(int) bool { return false },
		readHidden: func(int) ([]byte, error) {
			reads++
			return nil, errors.New("unexpected read")
		},
	}
	handled, err := runCLIWithAdminPasswordInput(context.Background(), []string{
		"admin", "set-password", "--username", " \t ", "--password-stdin",
	}, &config.Config{}, input)
	if !handled {
		t.Fatal("expected admin CLI to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "local login ID is required") {
		t.Fatalf("expected whitespace-only username rejection, got %v", err)
	}
	if reads != 0 {
		t.Fatalf("password input must not be read for an invalid username, got %d hidden reads", reads)
	}
}

func nonTerminalAdminPasswordInput(value string) adminPasswordInput {
	return adminPasswordInput{
		stdin:        strings.NewReader(value),
		promptWriter: io.Discard,
		stdinFD:      -1,
		isTerminal:   func(int) bool { return false },
		readHidden: func(int) ([]byte, error) {
			return nil, errors.New("unexpected hidden password read")
		},
	}
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}
