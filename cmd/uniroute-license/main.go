package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/signing"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: uniroute-license <keygen|issue|verify>")
	}
	switch args[0] {
	case "keygen":
		return runKeygen(args[1:], stdout)
	case "issue":
		return runIssue(args[1:], stdout)
	case "verify":
		return runVerify(args[1:], stdout)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runKeygen(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	keyID := fs.String("key-id", "", "stable signing key identifier")
	privateOut := fs.String("private-out", "", "new private-key file")
	publicOut := fs.String("public-out", "", "new single-key JSON public-key ring file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !validCLIKeyID(*keyID) || *privateOut == "" || *publicOut == "" {
		return fmt.Errorf("--key-id, --private-out and --public-out are required")
	}
	if samePath(*privateOut, *publicOut) {
		return fmt.Errorf("private and public output paths must differ")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate Ed25519 key: %w", err)
	}
	encodedPrivate, _ := signing.EncodePrivateKey(privateKey)
	encodedPublic, _ := signing.EncodePublicKey(publicKey)
	encodedPublicRing, err := json.Marshal(map[string]string{*keyID: encodedPublic})
	if err != nil {
		return fmt.Errorf("encode public-key ring: %w", err)
	}
	if err := writeNewFile(*privateOut, []byte(encodedPrivate+"\n"), 0o600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}
	if err := writeNewFile(*publicOut, append(encodedPublicRing, '\n'), 0o644); err != nil {
		if removeErr := os.Remove(*privateOut); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("write public key: %v; remove newly created private key: %w", err, removeErr)
		}
		return fmt.Errorf("write public key: %w", err)
	}
	_, err = fmt.Fprintf(stdout, "key_id=%s\npublic_key_file=%s\nprivate_key_file=%s\n", *keyID, *publicOut, *privateOut)
	return err
}

func runIssue(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("issue", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	keyID := fs.String("key-id", "", "trusted signing key identifier")
	privateKeyPath := fs.String("private-key", "", "private-key file")
	payloadPath := fs.String("payload", "", "strict License payload JSON")
	outPath := fs.String("out", "", "new signed License document")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !validCLIKeyID(*keyID) || *privateKeyPath == "" || *payloadPath == "" || *outPath == "" {
		return fmt.Errorf("--key-id, --private-key, --payload and --out are required")
	}
	privateRaw, err := os.ReadFile(*privateKeyPath)
	if err != nil {
		return fmt.Errorf("read private key: %w", err)
	}
	privateKey, err := signing.ParsePrivateKey(string(privateRaw))
	if err != nil {
		return fmt.Errorf("parse private key: %w", err)
	}
	payloadRaw, err := os.ReadFile(*payloadPath)
	if err != nil {
		return fmt.Errorf("read payload: %w", err)
	}
	var payload licensing.Payload
	decoder := json.NewDecoder(bytes.NewReader(payloadRaw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return fmt.Errorf("parse payload: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return fmt.Errorf("parse payload: %w", err)
	}
	doc, err := licensing.SignCommercialPayload(payload, *keyID, privateKey)
	if err != nil {
		return fmt.Errorf("sign License: %w", err)
	}
	document, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	document = append(document, '\n')
	if err := writeNewFile(*outPath, document, 0o600); err != nil {
		return fmt.Errorf("write signed License: %w", err)
	}
	_, err = fmt.Fprintf(stdout, "license_id=%s\nkey_id=%s\nout=%s\n", payload.LicenseID, *keyID, *outPath)
	return err
}

func runVerify(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	publicKeysPath := fs.String("public-keys", "", "JSON public-key ring file")
	deploymentID := fs.String("deployment-id", "", "expected deployment identity")
	licensePath := fs.String("license", "", "signed License document")
	atText := fs.String("at", "", "optional RFC3339 verification time")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *publicKeysPath == "" || *deploymentID == "" || *licensePath == "" {
		return fmt.Errorf("--public-keys, --deployment-id and --license are required")
	}
	publicKeysRaw, err := os.ReadFile(*publicKeysPath)
	if err != nil {
		return fmt.Errorf("read public-key ring: %w", err)
	}
	ring, err := licensing.ParsePublicKeyRing(string(publicKeysRaw))
	if err != nil {
		return err
	}
	licenseRaw, err := os.ReadFile(*licensePath)
	if err != nil {
		return fmt.Errorf("read License: %w", err)
	}
	verifier := licensing.NewCommercialVerifier(ring, nil, 30, *deploymentID)
	if strings.TrimSpace(*atText) != "" {
		at, err := time.Parse(time.RFC3339, *atText)
		if err != nil {
			return fmt.Errorf("--at must be RFC3339: %w", err)
		}
		verifier.WithNow(func() time.Time { return at })
	}
	status := verifier.VerifyBytes(licenseRaw)
	if !status.Enforceable || !status.Valid {
		return fmt.Errorf("License verification failed: status=%s error=%s", status.Status, status.Error)
	}
	_, err = fmt.Fprintf(stdout, "status=%s\nlicense_id=%s\ndeployment_id=%s\nkey_id=%s\n", status.Status, status.License.LicenseID, status.License.DeploymentID, status.KeyID)
	return err
}

func writeNewFile(path string, contents []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	removePartial := func(cause error) error {
		_ = file.Close()
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("%v; remove partial output: %w", cause, removeErr)
		}
		return cause
	}
	if _, err := file.Write(contents); err != nil {
		return removePartial(err)
	}
	if err := file.Sync(); err != nil {
		return removePartial(err)
	}
	if err := file.Close(); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("close output: %v; remove partial output: %w", err, removeErr)
		}
		return err
	}
	return nil
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && leftAbs == rightAbs
}

func validCLIKeyID(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func requireEOF(decoder *json.Decoder) error {
	var extra interface{}
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("unexpected trailing JSON value")
}
