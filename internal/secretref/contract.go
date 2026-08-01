// Package secretref defines the backend-neutral boundary for versioned
// supplier credentials.
//
// The package deliberately separates safe lifecycle metadata from secret
// material. References, bindings, snapshots and audit receipts are safe to
// serialize. Secret material is only exposed to a short, non-reentrant
// WithMaterial callback and is never included in a returned value or error.
package secretref

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	// MaxMaterialBytes bounds a single fixture secret. Production backends may
	// impose a smaller limit, but must never accept an empty value.
	MaxMaterialBytes = 64 * 1024
)

var (
	ErrInvalidReference   = errors.New("secretref: invalid reference")
	ErrInvalidVersion     = errors.New("secretref: invalid version")
	ErrInvalidMaterial    = errors.New("secretref: invalid material")
	ErrVersionExists      = errors.New("secretref: version already exists")
	ErrVersionNotFound    = errors.New("secretref: version not found")
	ErrRevisionConflict   = errors.New("secretref: lifecycle revision conflict")
	ErrInvalidTransition  = errors.New("secretref: invalid lifecycle transition")
	ErrNoActiveVersion    = errors.New("secretref: no active version")
	ErrStaleBinding       = errors.New("secretref: stale version binding")
	ErrBackendUnavailable = errors.New("secretref: backend unavailable")
	ErrBackendNotFound    = errors.New("secretref: backend not registered")
	ErrBackendConflict    = errors.New("secretref: backend registration conflict")
	ErrConsumerRejected   = errors.New("secretref: material consumer rejected")
)

var (
	backendPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	pathPattern    = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._/-]{0,190}[A-Za-z0-9])?$`)
	versionPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,126}[A-Za-z0-9])?$`)
)

// Reference identifies one logical secret without selecting a mutable
// "latest" value. Backend is an implementation name, Scope is the
// operator-defined ownership path, and Name is the logical credential name.
//
// Values are intentionally restricted to printable ASCII so references are
// unambiguous in audit records and cannot contain control characters.
type Reference struct {
	Backend string `json:"backend"`
	Scope   string `json:"scope"`
	Name    string `json:"name"`
}

func (r Reference) Validate() error {
	if !validBackend(r.Backend) ||
		!validPath(r.Scope) ||
		!validPath(r.Name) ||
		strings.Contains(r.Name, "/") {
		return ErrInvalidReference
	}
	return nil
}

func validBackend(value string) bool {
	return len(value) <= 63 && backendPattern.MatchString(value)
}

func validPath(value string) bool {
	if !pathPattern.MatchString(value) ||
		strings.Contains(value, "//") ||
		strings.Contains(value, "/./") ||
		strings.Contains(value, "/../") ||
		strings.HasPrefix(value, "./") ||
		strings.HasPrefix(value, "../") ||
		strings.HasSuffix(value, "/.") ||
		strings.HasSuffix(value, "/..") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// Canonical is a stable metadata-only representation. Reference validation
// guarantees that none of its components require escaping.
func (r Reference) Canonical() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return fmt.Sprintf("secretref://%s/%s/%s", r.Backend, r.Scope, r.Name), nil
}

// VersionState describes lifecycle state only. Secret bytes remain immutable
// for the lifetime of a version.
type VersionState string

const (
	VersionStaged  VersionState = "staged"
	VersionActive  VersionState = "active"
	VersionRetired VersionState = "retired"
	VersionRevoked VersionState = "revoked"
)

// Binding is the exact credential version and active generation pinned by a
// worker. A backend must reject it after a later activation or rollback.
type Binding struct {
	Reference  Reference `json:"reference"`
	Version    string    `json:"version"`
	Generation uint64    `json:"generation"`
}

func (b Binding) Validate() error {
	if err := b.Reference.Validate(); err != nil {
		return err
	}
	if !versionPattern.MatchString(b.Version) {
		return ErrInvalidVersion
	}
	if b.Generation == 0 {
		return ErrStaleBinding
	}
	return nil
}

// VersionMetadata is safe for API, audit and UI use. It never contains a
// ciphertext, plaintext, digest of low-entropy secret material or backend
// readback handle.
type VersionMetadata struct {
	Version   string       `json:"version"`
	State     VersionState `json:"state"`
	StagedAt  time.Time    `json:"staged_at"`
	Activated *time.Time   `json:"activated_at,omitempty"`
	Retired   *time.Time   `json:"retired_at,omitempty"`
	Revoked   *time.Time   `json:"revoked_at,omitempty"`
}

type Snapshot struct {
	Reference  Reference         `json:"reference"`
	Revision   uint64            `json:"revision"`
	Generation uint64            `json:"generation"`
	Active     *Binding          `json:"active,omitempty"`
	Versions   []VersionMetadata `json:"versions"`
}

type Action string

const (
	ActionStage    Action = "stage"
	ActionActivate Action = "activate"
	ActionRollback Action = "rollback"
	ActionRevoke   Action = "revoke"
)

// AuditReceipt is metadata-only evidence of one successful lifecycle
// transition. Revision covers every mutation; Generation changes only when
// the active version changes.
type AuditReceipt struct {
	Action          Action    `json:"action"`
	Reference       Reference `json:"reference"`
	Version         string    `json:"version"`
	PreviousVersion string    `json:"previous_version,omitempty"`
	Revision        uint64    `json:"revision"`
	Generation      uint64    `json:"generation"`
	OccurredAt      time.Time `json:"occurred_at"`
}

// Backend is the complete Phase-7 B4 contract.
//
// expectedRevision is a compare-and-swap guard for lifecycle writers. Stage
// creates immutable bytes for a new version. Activate promotes a staged or
// retired version. Rollback promotes a retired version and records a distinct
// action. Revoke is forbidden for the active version.
//
// WithMaterial must atomically validate the exact Binding immediately before
// invoking consume. consume must be short, non-reentrant and perform no
// network I/O; it should only inject the bytes into an already bounded provider
// request. Backends must discard consumer error text and return
// ErrConsumerRejected so secret-derived errors cannot escape.
type Backend interface {
	Name() string
	Stage(ctx context.Context, ref Reference, version string, expectedRevision uint64, material []byte) (AuditReceipt, error)
	Activate(ctx context.Context, ref Reference, version string, expectedRevision uint64) (Binding, AuditReceipt, error)
	Rollback(ctx context.Context, ref Reference, version string, expectedRevision uint64) (Binding, AuditReceipt, error)
	Revoke(ctx context.Context, ref Reference, version string, expectedRevision uint64) (AuditReceipt, error)
	Pin(ctx context.Context, ref Reference) (Binding, error)
	WithMaterial(ctx context.Context, binding Binding, consume func([]byte) error) error
	Snapshot(ctx context.Context, ref Reference) (Snapshot, error)
	Audit(ctx context.Context, ref Reference) ([]AuditReceipt, error)
}

// Registry is an immutable, fixed allowlist of secret backends. It is not a
// dynamic plugin loader: production assembly selects concrete implementations
// during process construction.
type Registry struct {
	backends map[string]Backend
}

func NewRegistry(backends ...Backend) (*Registry, error) {
	registry := &Registry{backends: make(map[string]Backend, len(backends))}
	for _, backend := range backends {
		if backend == nil || !validBackend(backend.Name()) {
			return nil, ErrBackendNotFound
		}
		if _, exists := registry.backends[backend.Name()]; exists {
			return nil, ErrBackendConflict
		}
		registry.backends[backend.Name()] = backend
	}
	return registry, nil
}

func (r *Registry) BackendFor(ref Reference) (Backend, error) {
	if r == nil {
		return nil, ErrBackendNotFound
	}
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	backend := r.backends[ref.Backend]
	if backend == nil {
		return nil, ErrBackendNotFound
	}
	return backend, nil
}

func validateVersion(version string) error {
	if !versionPattern.MatchString(version) {
		return ErrInvalidVersion
	}
	return nil
}

func validateMaterial(material []byte) error {
	if len(material) == 0 || len(material) > MaxMaterialBytes {
		return ErrInvalidMaterial
	}
	return nil
}
