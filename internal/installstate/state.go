// Package installstate reads the bounded fresh-install envelope. It never
// infers migration progress from that envelope: schema_migrations is authoritative.
package installstate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/strictjson"
)

const Schema = "unirouter.fresh-install.v1"
const Prefix = Schema + "\n"
const MaximumBytes = 1 << 20

var gitHex = regexp.MustCompile(`^([a-f0-9]{40}|[a-f0-9]{64})$`)
var hex64 = regexp.MustCompile(`^[a-f0-9]{64}$`)
var installID = regexp.MustCompile(`^ufi_[a-f0-9]{32}$`)

type Target struct {
	Environment      string `json:"environment"`
	SystemIdentifier string `json:"system_identifier"`
	Database         string `json:"database"`
	DatabaseOID      int64  `json:"database_oid"`
	Owner            string `json:"owner"`
	Role             string `json:"role"`
}

type Artifact struct {
	Version          string            `json:"version"`
	Commit           string            `json:"commit"`
	Tree             string            `json:"tree"`
	ImageDigest      string            `json:"image_digest"`
	ExecutableDigest string            `json:"executable_digest"`
	ManifestDigest   string            `json:"manifest_digest"`
	Ledger           []string          `json:"ledger"`
	Publications     []string          `json:"publications"`
	Files            map[string]string `json:"files"`
}

type Plan struct {
	SchemaVersion  string   `json:"schema_version"`
	InstallationID string   `json:"installation_id"`
	Target         Target   `json:"target"`
	Artifact       Artifact `json:"artifact"`
	FreshShape     string   `json:"fresh_shape"`
	Digest         string   `json:"digest"`
}

// References record which immutable Plan authorized a publication (including
// an interrupted ordinary prefix). They are not a second migration ledger.
type Reference struct {
	Publication string `json:"publication"`
	PlanDigest  string `json:"plan_digest"`
	FileSHA256  string `json:"file_sha256"`
	Supersedes  string `json:"supersedes,omitempty"`
	Committed   bool   `json:"committed"`
}

type Completion struct {
	LedgerSHA256    string `json:"ledger_sha256"`
	MigrationSHA256 string `json:"migration_sha256"`
	ManifestSHA256  string `json:"manifest_sha256"`
}

type Record struct {
	Plan       Plan        `json:"plan"`
	State      string      `json:"state"`
	References []Reference `json:"publication_plans"`
	Completion *Completion `json:"completion,omitempty"`
}

type Snapshot struct {
	HasLedger bool
	Ledger    []string
	Record    *Record
	Comment   string
}

type Reader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func SHA(wire []byte) string           { sum := sha256.Sum256(wire); return hex.EncodeToString(sum[:]) }
func LedgerSHA(ledger []string) string { return SHA([]byte(strings.Join(ledger, "\n"))) }

func (p Plan) CalculateDigest() string {
	p.Digest = ""
	wire, _ := json.Marshal(p)
	return SHA(wire)
}

func (p Plan) Validate() error {
	if p.SchemaVersion != Schema || !installID.MatchString(p.InstallationID) || p.Digest != p.CalculateDigest() {
		return fmt.Errorf("fresh-install Plan identity or digest is invalid")
	}
	if p.Target.Database == "" || p.Target.DatabaseOID <= 0 || p.Target.Owner == "" || p.Target.Role == "" || p.Target.SystemIdentifier == "" || p.Target.Environment == "" || p.Target.Environment == "unspecified" {
		return fmt.Errorf("fresh-install requires an exact database and environment identity")
	}
	a := p.Artifact
	if a.Version == "" || !gitHex.MatchString(a.Commit) || !gitHex.MatchString(a.Tree) || !strings.HasPrefix(a.ImageDigest, "sha256:") || !hex64.MatchString(strings.TrimPrefix(a.ImageDigest, "sha256:")) || !strings.HasPrefix(a.ExecutableDigest, "sha256:") || !hex64.MatchString(strings.TrimPrefix(a.ExecutableDigest, "sha256:")) || !hex64.MatchString(a.ManifestDigest) {
		return fmt.Errorf("fresh-install requires exact build, image and support identities")
	}
	if len(a.Ledger) == 0 || (a.Ledger[len(a.Ledger)-1] != schemaauthority.BudgetSnapshotExpandHead && a.Ledger[len(a.Ledger)-1] != schemaauthority.ContentCaptureExpandHead && a.Ledger[len(a.Ledger)-1] != schemaauthority.ContentReclaimExpandHead && a.Ledger[len(a.Ledger)-1] != schemaauthority.PreDispatchExpandHead && (a.Ledger[len(a.Ledger)-1] != schemaauthority.ScopedContentExpandHead && a.Ledger[len(a.Ledger)-1] != schemaauthority.XInsightPolicyExpandHead)) || schemaauthority.VerifyRuntimeLedger(a.Ledger, a.Ledger) != nil {
		return fmt.Errorf("fresh-install requires an exact supported M118, M119, M120 or M121 artifact")
	}
	for _, v := range a.Ledger {
		if !hex64.MatchString(a.Files[v+".up.sql"]) {
			return fmt.Errorf("missing migration identity: %s", v)
		}
	}
	for name, digest := range a.Files {
		if strings.ContainsAny(name, "/\\") || !hex64.MatchString(digest) {
			return fmt.Errorf("invalid artifact file identity")
		}
	}
	if len(a.Publications) == 0 {
		return fmt.Errorf("missing publication inventory")
	}
	for i, id := range a.Publications {
		if !slices.Contains(a.Ledger, id) || (i > 0 && a.Publications[i-1] >= id) {
			return fmt.Errorf("invalid publication inventory")
		}
	}
	if p.FreshShape != "empty_database" && p.FreshShape != "empty_original_ledger" {
		return fmt.Errorf("unknown fresh database shape")
	}
	return nil
}

func (r Record) Validate() error {
	if err := r.Plan.Validate(); err != nil {
		return err
	}
	if r.State != "active" && r.State != "completed" {
		return fmt.Errorf("invalid install state")
	}
	if (r.State == "completed") != (r.Completion != nil) {
		return fmt.Errorf("inconsistent completion proof")
	}
	seen := make(map[string]Reference)
	latest := make(map[string]Reference)
	for _, ref := range r.References {
		if !slices.Contains(r.Plan.Artifact.Publications, ref.Publication) || !hex64.MatchString(ref.PlanDigest) || !hex64.MatchString(ref.FileSHA256) {
			return fmt.Errorf("invalid publication reference")
		}
		if _, ok := seen[ref.PlanDigest]; ok {
			return fmt.Errorf("duplicate publication Plan reference")
		}
		prior, exists := latest[ref.Publication]
		if (exists && (prior.Committed || ref.Supersedes != prior.PlanDigest)) || (!exists && ref.Supersedes != "") {
			return fmt.Errorf("invalid publication supersession")
		}
		seen[ref.PlanDigest], latest[ref.Publication] = ref, ref
	}
	if c := r.Completion; c != nil {
		for _, id := range r.Plan.Artifact.Publications {
			if !latest[id].Committed {
				return fmt.Errorf("completion lacks a committed publication Plan")
			}
		}
		if c.LedgerSHA256 != LedgerSHA(r.Plan.Artifact.Ledger) || c.MigrationSHA256 != "sha256:"+r.Plan.Artifact.Files[r.Plan.Artifact.Ledger[len(r.Plan.Artifact.Ledger)-1]+".up.sql"] || c.ManifestSHA256 != r.Plan.Artifact.ManifestDigest {
			return fmt.Errorf("invalid historical completion binding")
		}
	}
	return nil
}

func DecodePlan(wire []byte) (Plan, error) {
	var p Plan
	if err := decode(wire, &p); err != nil {
		return p, err
	}
	return p, p.Validate()
}

func decode(wire []byte, value any) error {
	if len(wire) == 0 || len(wire) > MaximumBytes {
		return fmt.Errorf("install document size is invalid")
	}
	if err := strictjson.Validate(wire); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(wire))
	d.DisallowUnknownFields()
	return d.Decode(value)
}

func (r Record) CommentBytes() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	wire, err := json.Marshal(r)
	if len(wire)+len(Prefix) > MaximumBytes {
		return nil, fmt.Errorf("install envelope exceeds bounded size")
	}
	return append([]byte(Prefix), wire...), err
}

func ParseComment(comment string) (*Record, error) {
	if !strings.HasPrefix(comment, "unirouter.fresh-install.") {
		return nil, nil
	}
	if !strings.HasPrefix(comment, Prefix) {
		return nil, fmt.Errorf("unsupported installation envelope")
	}
	var r Record
	if err := decode([]byte(strings.TrimPrefix(comment, Prefix)), &r); err != nil {
		return nil, err
	}
	wire, err := r.CommentBytes()
	if err != nil {
		return nil, err
	}
	if string(wire) != comment {
		return nil, fmt.Errorf("noncanonical install envelope")
	}
	return &r, nil
}

// Metadata and ledger are read by ONE statement. The catalog existence probe
// cannot grant admission; an absent table is always refused by ordinary writers.
func Read(ctx context.Context, q Reader) (Snapshot, error) {
	var s Snapshot
	if err := q.QueryRow(ctx, `SELECT to_regclass('public.schema_migrations') IS NOT NULL`).Scan(&s.HasLedger); err != nil {
		return s, err
	}
	if !s.HasLedger {
		return s, nil
	}
	var ledger string
	err := q.QueryRow(ctx, `SELECT COALESCE(obj_description('public.schema_migrations'::regclass,'pg_class'),''),
 COALESCE((SELECT jsonb_agg(version ORDER BY version) FROM public.schema_migrations),'[]'::jsonb)::text`).Scan(&s.Comment, &ledger)
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal([]byte(ledger), &s.Ledger); err != nil {
		return s, err
	}
	s.Record, err = ParseComment(s.Comment)
	return s, err
}

// This gate does not grant domain authority. Original schema, permission,
// publication and economic checks still apply after the non-fresh admission.
func RequireOrdinary(ctx context.Context, q Reader) (Snapshot, error) {
	s, err := Read(ctx, q)
	if err != nil {
		return s, err
	}
	if s.Record != nil && s.Record.State == "active" {
		return s, fmt.Errorf("fresh installation is incomplete; resume the original install Plan")
	}
	if len(s.Ledger) == 0 {
		return s, fmt.Errorf("empty database requires the explicit fresh-install owner")
	}
	if s.Record != nil {
		historical := s.Record.Plan.Artifact.Ledger
		if len(s.Ledger) < len(historical) || !slices.Equal(s.Ledger[:len(historical)], historical) {
			return s, fmt.Errorf("completed installation has no matching committed ledger")
		}
	}
	return s, nil
}

func ReadTarget(ctx context.Context, q Reader, environment string) (Target, error) {
	t := Target{Environment: environment}
	err := q.QueryRow(ctx, `SELECT current_database(),oid::bigint,pg_get_userbyid(datdba),current_user,
 (SELECT system_identifier::text FROM pg_control_system()) FROM pg_database WHERE datname=current_database()`).Scan(&t.Database, &t.DatabaseOID, &t.Owner, &t.Role, &t.SystemIdentifier)
	return t, err
}

// The caller must hold the migration owner's physical connection/transaction.
func Write(ctx context.Context, tx pgx.Tx, r Record) error {
	wire, err := r.CommentBytes()
	if err != nil {
		return err
	}
	// PostgreSQL quotes the literal, including when standard_conforming_strings
	// differs. The qualified identifier is fixed, never supplied by a Plan.
	var statement string
	if err = tx.QueryRow(ctx, `SELECT format('COMMENT ON TABLE public.schema_migrations IS %L',$1::text)`, string(wire)).Scan(&statement); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, statement)
	return err
}
