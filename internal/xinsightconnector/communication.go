package xinsightconnector

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/db"
)

const CommunicationWindow = 30 * time.Second
const CommunicationLocalWindow = 40 * time.Second
const CommunicationMaxUnits = 8
const CommunicationMaxRequests = 24
const CommunicationDefaultCadenceMS = 60000

var ErrCommunicationPaused = errors.New("connector automatic communication is disabled or not due")
var ErrCommunicationBusy = errors.New("connector communication is still in flight")

type CommunicationState struct {
	OrgID            int64             `json:"-"`
	ProjectID        int64             `json:"project_id"`
	InstallationID   string            `json:"installation_id"`
	AutomaticEnabled bool              `json:"automatic_enabled"`
	CadenceMS        int               `json:"cadence_ms"`
	Revision         int64             `json:"revision"`
	Effective        string            `json:"effective"`
	PeerState        string            `json:"peer_state"`
	PeerObservedAt   *time.Time        `json:"peer_observed_at,omitempty"`
	LastAttemptAt    *time.Time        `json:"last_attempt_at,omitempty"`
	LastSuccessAt    *time.Time        `json:"last_success_at,omitempty"`
	LastCode         string            `json:"last_code"`
	LastRun          *CommunicationRun `json:"last_run,omitempty"`
}

type CommunicationRun struct {
	Key            string     `json:"idempotency_key"`
	InstallationID string     `json:"installation_id"`
	Revision       int64      `json:"revision"`
	State          string     `json:"state"`
	Units          int        `json:"units"`
	Requests       int        `json:"requests"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

type CommunicationStore struct {
	runtimeEnabled bool
	db             db.DBQuerier
	legacyCadence  int
	ownerInstance  string
	ownerArtifact  string
}

func NewCommunicationStore(database db.DBQuerier, legacyCadence int, instance, artifact string) *CommunicationStore {
	if legacyCadence < 250 {
		legacyCadence = 1000
	}
	return &CommunicationStore{db: database, legacyCadence: legacyCadence, ownerInstance: instance, ownerArtifact: artifact, runtimeEnabled: true}
}

// Policy defaults are created in the installation transaction by M127. Missing
// policy/binding/schema is an error, never permission to contact the peer.
func (s *CommunicationStore) Read(ctx context.Context, org, project int64) (CommunicationState, error) {
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return CommunicationState{}, e
	}
	defer tx.Rollback(ctx)
	state, e := s.ReadTx(ctx, tx, org, project)
	if e != nil {
		return state, e
	}
	return state, tx.Commit(ctx)
}

func (s *CommunicationStore) ReadTx(ctx context.Context, tx pgx.Tx, org, project int64) (CommunicationState, error) {
	var st CommunicationState
	var lease *string
	var deadline *time.Time
	var leaseKind *string
	var leaseRevision *int64
	e := tx.QueryRow(ctx, `SELECT p.org_id,p.project_id,i.installation_id,p.automatic_enabled,COALESCE(p.cadence_ms,$3),p.revision,
 p.lease_id,p.lease_kind,p.lease_revision,p.dispatch_deadline,p.last_attempt_at,p.last_success_at,p.last_code,p.peer_observed_at
 FROM xinsight_communication_policies p JOIN xinsight_connector_installations i ON i.org_id=p.org_id AND i.project_id=p.project_id
 WHERE p.org_id=$1 AND p.project_id=$2 AND i.observed_state<>'disconnected'`, org, project, s.legacyCadence).Scan(&st.OrgID, &st.ProjectID, &st.InstallationID, &st.AutomaticEnabled, &st.CadenceMS, &st.Revision, &lease, &leaseKind, &leaseRevision, &deadline, &st.LastAttemptAt, &st.LastSuccessAt, &st.LastCode, &st.PeerObservedAt)
	if e != nil {
		return st, e
	}
	st.PeerState = "unknown"
	if st.PeerObservedAt != nil {
		st.PeerState = "last_observed"
	}
	st.Effective = "paused"
	if st.AutomaticEnabled {
		st.Effective = "automatic"
	}
	if !s.runtimeEnabled {
		st.Effective = "runtime_disabled"
	}
	if lease != nil {
		st.Effective = "running"
		if leaseKind != nil && *leaseKind == "manual" {
			st.Effective = "manual_running"
		}
		if (!st.AutomaticEnabled && (leaseKind == nil || *leaseKind != "manual")) || (leaseRevision != nil && *leaseRevision != st.Revision) {
			st.Effective = "pausing"
		}
		if deadline == nil || !deadline.After(time.Now()) {
			st.Effective = "completion_unknown"
		}
	}
	var run CommunicationRun
	e = tx.QueryRow(ctx, `SELECT idempotency_key,installation_id,revision,state,units,requests,started_at,finished_at FROM xinsight_communication_runs WHERE org_id=$1 AND project_id=$2 ORDER BY started_at DESC,idempotency_key DESC LIMIT 1`, org, project).Scan(&run.Key, &run.InstallationID, &run.Revision, &run.State, &run.Units, &run.Requests, &run.StartedAt, &run.FinishedAt)
	if e == nil {
		st.LastRun = &run
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return st, e
	}
	return st, nil
}

func (s *CommunicationStore) UpdateTx(ctx context.Context, tx pgx.Tx, org, project, actor, revision int64, id string, enabled bool, cadence int) error {
	if actor <= 0 || revision < 1 || cadence < 250 || cadence > 3600000 {
		return ErrInvalid
	}
	var locked int64
	if e := tx.QueryRow(ctx, `SELECT revision FROM xinsight_communication_policies WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, org, project).Scan(&locked); e != nil {
		return e
	}
	if locked != revision {
		return ErrConflict
	}
	if e := s.reapStoppedTx(ctx, tx, org, project); e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, `UPDATE xinsight_communication_policies p SET automatic_enabled=$6,cadence_ms=$7,revision=revision+1,updated_by=$3,updated_at=clock_timestamp(),next_auto_at=clock_timestamp()
 WHERE org_id=$1 AND project_id=$2 AND revision=$4 AND EXISTS(SELECT 1 FROM xinsight_connector_installations i WHERE i.org_id=p.org_id AND i.project_id=p.project_id AND i.installation_id=$5 AND i.observed_state<>'disconnected')`, org, project, actor, revision, id, enabled, cadence)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

// An abandoned slot is released only by its worker or the existing stopped /
// ingress-absent generation proof. A deadline alone never proves quiescence.
func (s *CommunicationStore) reapStoppedTx(ctx context.Context, tx pgx.Tx, org, project int64) error {
	_, e := tx.Exec(ctx, `WITH retired AS (
 UPDATE xinsight_communication_policies p SET lease_id=NULL,lease_kind=NULL,lease_installation=NULL,lease_revision=NULL,dispatch_deadline=NULL,owner_instance=NULL,owner_artifact=NULL,last_code='interrupted'
 WHERE org_id=$1 AND project_id=$2 AND lease_id IS NOT NULL AND EXISTS(SELECT 1 FROM runtime_generations g WHERE g.instance_id=p.owner_instance AND g.artifact_digest=p.owner_artifact AND g.state='stopped' AND g.ingress_state='absent') RETURNING org_id,project_id)
 UPDATE xinsight_communication_runs r SET state='interrupted',finished_at=clock_timestamp() FROM retired t WHERE r.org_id=t.org_id AND r.project_id=t.project_id AND r.state='running'`, org, project)
	return e
}

type CommunicationPermit struct {
	store            *CommunicationStore
	org, project     int64
	id, installation string
	revision         int64
	deadline         time.Time
	parent           context.Context
	mu               sync.Mutex
	requests, units  int
	continuations    map[string]bool
}

type communicationPermitKey struct{}

func WithCommunicationPermit(ctx context.Context, p *CommunicationPermit) context.Context {
	return context.WithValue(ctx, communicationPermitKey{}, p)
}

// CheckCommunicationRequest is called at the single HTTP dispatch seam. The
// phase's local receipt/journal writes deliberately do not use this network gate.
func CheckCommunicationRequest(ctx context.Context, method, path string) error {
	p, _ := ctx.Value(communicationPermitKey{}).(*CommunicationPermit)
	if p == nil {
		return nil
	} // Explicit existing setup/recovery calls keep their own bounded consent.
	return p.beforeRequest(ctx, method, path)
}

func (p *CommunicationPermit) beforeRequest(ctx context.Context, method, path string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.parent.Err() != nil || !time.Now().Before(p.deadline) || p.requests >= CommunicationMaxRequests {
		return ErrCommunicationPaused
	}
	continuation := p.continuations[method+" "+path]
	var valid bool
	if e := p.store.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM xinsight_communication_policies WHERE org_id=$1 AND project_id=$2 AND lease_id=$3 AND lease_installation=$4 AND dispatch_deadline>clock_timestamp() AND (revision=$5 OR $6))`, p.org, p.project, p.id, p.installation, p.revision, continuation).Scan(&valid); e != nil {
		return e
	}
	if !valid {
		return ErrCommunicationPaused
	}
	p.requests++
	delete(p.continuations, method+" "+path)
	return nil
}

// ClaimTx serializes with policy mutations. No row lock survives commit or HTTP.
// Manual idempotency is checked before lease/revision admission, including after
// an HTTP response loss. A different binding is never accepted as a replay.
func (s *CommunicationStore) ClaimTx(ctx context.Context, tx pgx.Tx, org, project, actor, revision int64, id, key, kind string) (*CommunicationPermit, *CommunicationRun, error) {
	if kind != "automatic" && kind != "manual" {
		return nil, nil, ErrInvalid
	}
	if kind == "manual" && (actor <= 0 || len(key) < 1 || len(key) > 128 || !validControlText(key, 128)) {
		return nil, nil, ErrInvalid
	}
	var locked int64
	if e := tx.QueryRow(ctx, `SELECT revision FROM xinsight_communication_policies WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, org, project).Scan(&locked); e != nil {
		return nil, nil, e
	}
	if e := s.reapStoppedTx(ctx, tx, org, project); e != nil {
		return nil, nil, e
	}
	if kind == "manual" {
		var r CommunicationRun
		e := tx.QueryRow(ctx, `SELECT idempotency_key,installation_id,revision,state,units,requests,started_at,finished_at FROM xinsight_communication_runs WHERE org_id=$1 AND project_id=$2 AND idempotency_key=$3`, org, project, key).Scan(&r.Key, &r.InstallationID, &r.Revision, &r.State, &r.Units, &r.Requests, &r.StartedAt, &r.FinishedAt)
		if e == nil {
			if r.InstallationID != id || r.Revision != revision {
				return nil, nil, ErrConflict
			}
			return nil, &r, nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return nil, nil, e
		}
	}
	if !s.runtimeEnabled {
		return nil, nil, ErrUnavailable
	}
	var automatic, due, ownerReady bool
	var active *string
	e := tx.QueryRow(ctx, `SELECT automatic_enabled,next_auto_at<=clock_timestamp(),lease_id,
 EXISTS(SELECT 1 FROM runtime_generations g WHERE g.instance_id=$3 AND g.artifact_digest=$4 AND g.state='serving' AND g.lease_expires_at>clock_timestamp())
 FROM xinsight_communication_policies WHERE org_id=$1 AND project_id=$2`, org, project, s.ownerInstance, s.ownerArtifact).Scan(&automatic, &due, &active, &ownerReady)
	if e != nil {
		return nil, nil, e
	}
	if kind == "manual" && automatic {
		return nil, nil, ErrConflict
	}
	if active != nil {
		return nil, nil, ErrCommunicationBusy
	}
	if kind != "manual" && (!automatic || !due) {
		return nil, nil, ErrCommunicationPaused
	}
	if kind == "manual" && revision != locked {
		return nil, nil, ErrConflict
	}
	if !ownerReady {
		return nil, nil, ErrUnavailable
	}
	var deployment string
	e = tx.QueryRow(ctx, `SELECT x_deployment_id FROM xinsight_connector_installations WHERE org_id=$1 AND project_id=$2 AND installation_id=$3 AND desired_state='active' AND observed_state<>'disconnected'`, org, project, id).Scan(&deployment)
	if e != nil {
		return nil, nil, ErrConflict
	}
	token, e := randomLeaseToken()
	if e != nil {
		return nil, nil, e
	}
	var deadline time.Time
	e = tx.QueryRow(ctx, `UPDATE xinsight_communication_policies SET lease_id=$3,lease_kind=$4,lease_installation=$5,lease_revision=revision,owner_instance=$6,owner_artifact=$7,
 dispatch_deadline=clock_timestamp()+interval '30 seconds',last_attempt_at=clock_timestamp() WHERE org_id=$1 AND project_id=$2 RETURNING dispatch_deadline`, org, project, token, kind, id, s.ownerInstance, s.ownerArtifact).Scan(&deadline)
	if e != nil {
		return nil, nil, e
	}
	p := &CommunicationPermit{store: s, org: org, project: project, id: token, installation: id, revision: locked, deadline: deadline, parent: ctx}
	if kind == "manual" {
		_, e = tx.Exec(ctx, `INSERT INTO xinsight_communication_runs(org_id,project_id,idempotency_key,installation_id,deployment_id,revision,requested_by,lease_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, org, project, key, id, deployment, locked, actor, token)
	}
	return p, nil, e
}

func (s *CommunicationStore) ClaimAutomatic(ctx context.Context, id string, kind string) (*CommunicationPermit, error) {
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	var org, project int64
	if e = tx.QueryRow(ctx, `SELECT org_id,project_id FROM xinsight_connector_installations WHERE installation_id=$1 AND observed_state<>'disconnected'`, id).Scan(&org, &project); e != nil {
		return nil, e
	}
	p, _, e := s.ClaimTx(ctx, tx, org, project, 0, 0, id, "", kind)
	if e != nil {
		return nil, e
	}
	return p, tx.Commit(ctx)
}

// NextUnit refuses new work after a policy revision change. An already admitted
// unit may still finish its truthful local journal and bounded ACK/confirmation.
func (p *CommunicationPermit) NextUnit(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.units >= CommunicationMaxUnits || p.requests >= CommunicationMaxRequests || p.parent.Err() != nil || !time.Now().Before(p.deadline) {
		return ErrCommunicationPaused
	}
	var valid bool
	e := p.store.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM xinsight_communication_policies WHERE org_id=$1 AND project_id=$2 AND lease_id=$3 AND revision=$4)`, p.org, p.project, p.id, p.revision).Scan(&valid)
	if e != nil {
		return e
	}
	if !valid {
		return ErrCommunicationPaused
	}
	p.units++
	return nil
}

func (p *CommunicationPermit) Finish(ctx context.Context, code string, successful bool) error {
	if code != "completed" && code != "empty" && code != "partial" && code != "failed" && code != "interrupted" {
		return ErrInvalid
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	tx, e := p.store.db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var owned int
	if e = tx.QueryRow(ctx, `SELECT 1 FROM xinsight_communication_policies WHERE org_id=$1 AND project_id=$2 AND lease_id=$3 FOR UPDATE`, p.org, p.project, p.id).Scan(&owned); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE xinsight_communication_runs SET state=$4,units=$5,requests=$6,finished_at=clock_timestamp() WHERE org_id=$1 AND project_id=$2 AND lease_id=$3 AND state='running'`, p.org, p.project, p.id, code, p.units, p.requests)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE xinsight_communication_policies SET lease_id=NULL,lease_kind=NULL,lease_installation=NULL,lease_revision=NULL,dispatch_deadline=NULL,owner_instance=NULL,owner_artifact=NULL,
 next_auto_at=clock_timestamp()+COALESCE(cadence_ms,$5)*interval '1 millisecond',last_code=$4,last_success_at=CASE WHEN $6 THEN clock_timestamp() ELSE last_success_at END
 WHERE org_id=$1 AND project_id=$2 AND lease_id=$3`, p.org, p.project, p.id, code, p.store.legacyCadence, successful)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// RuntimeUnit selects only the exact current binding, with credential work
// ahead of ordinary control. It never returns bearer bytes to the caller.
func (s *CommunicationStore) RuntimeUnit(ctx context.Context, id string) (RuntimeInstallationDescriptor, string, error) {
	var d RuntimeInstallationDescriptor
	var pending, runnable, credential bool
	e := s.db.QueryRow(ctx, `SELECT i.installation_id,i.x_deployment_id,COALESCE(b.control_project_id,''),
 b.invitation_token_enc IS NOT NULL AND b.redeem_result_sha256 IS NULL,
 b.invitation_token_enc IS NULL AND b.control_token_enc IS NOT NULL AND b.redeem_result_sha256 IS NOT NULL AND b.control_token_expires_at>clock_timestamp(),
 i.setup_request_id IS NOT NULL AND b.invitation_token_enc IS NULL AND b.redeemed_origin IS NOT NULL AND b.redeem_result_sha256 IS NOT NULL AND
 ((b.control_token_expires_at>clock_timestamp() AND b.control_token_expires_at<=clock_timestamp()+interval '6 hours') OR EXISTS(SELECT 1 FROM xinsight_connector_credential_operations o WHERE o.installation_id=i.installation_id AND o.state IN ('pending','committed')))
 FROM xinsight_connector_installations i JOIN xinsight_connector_control_bindings b USING(installation_id,org_id,project_id)
 WHERE i.installation_id=$1 AND i.desired_state='active' AND i.observed_state<>'disconnected'`, id).Scan(&d.InstallationID, &d.XDeploymentID, &d.ControlProjectID, &pending, &runnable, &credential)
	if e != nil {
		return d, "", e
	}
	if credential {
		return d, "credential", nil
	}
	if pending {
		return d, "redeem", nil
	}
	if runnable {
		return d, "control", nil
	}
	return d, "", nil
}

func (s *CommunicationStore) SavePeerObservation(ctx context.Context, id string, raw []byte) error {
	if len(raw) == 0 || len(raw) > 32768 {
		return ErrInvalid
	}
	tag, e := s.db.Exec(ctx, `UPDATE xinsight_communication_policies p SET peer_status=$2,peer_observed_at=clock_timestamp() FROM xinsight_connector_installations i WHERE i.installation_id=$1 AND i.org_id=p.org_id AND i.project_id=p.project_id AND i.observed_state<>'disconnected'`, id, raw)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}
func (s *CommunicationStore) PeerObservation(ctx context.Context, id string) ([]byte, error) {
	var raw []byte
	e := s.db.QueryRow(ctx, `SELECT p.peer_status FROM xinsight_communication_policies p JOIN xinsight_connector_installations i USING(org_id,project_id) WHERE i.installation_id=$1 AND i.observed_state<>'disconnected'`, id).Scan(&raw)
	if e != nil {
		return nil, e
	}
	if len(raw) == 0 {
		return nil, ErrUnavailable
	}
	return raw, nil
}

// Only a validated remote result grants its exact follow-up coordinate. A
// superseded permit never starts a new Poll, rotate, recover or status probe.
func allowCommunicationContinuation(ctx context.Context, method, path string) {
	p, _ := ctx.Value(communicationPermitKey{}).(*CommunicationPermit)
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.continuations == nil {
		p.continuations = map[string]bool{}
	}
	p.continuations[method+" "+path] = true
}

func (s *CommunicationStore) AutomaticInstallationIDs(ctx context.Context, limit int) ([]string, error) {
	if limit < 1 || limit > maxRuntimeInstallationBatch {
		return nil, ErrInvalid
	}
	rows, e := s.db.Query(ctx, `SELECT i.installation_id FROM xinsight_communication_policies p JOIN xinsight_connector_installations i USING(org_id,project_id)
 WHERE p.automatic_enabled AND p.next_auto_at<=clock_timestamp() AND i.desired_state='active' AND i.observed_state<>'disconnected'
 AND (p.lease_id IS NULL OR EXISTS(SELECT 1 FROM runtime_generations g WHERE g.instance_id=p.owner_instance AND g.artifact_digest=p.owner_artifact AND g.state='stopped' AND g.ingress_state='absent'))
 ORDER BY p.next_auto_at,i.installation_id COLLATE "C" LIMIT $1`, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetRuntimeEnabled is startup wiring only; it never mutates global configuration.
func (s *CommunicationStore) SetRuntimeEnabled(enabled bool) { s.runtimeEnabled = enabled }

func (s *CommunicationStore) InitializationPending(ctx context.Context, id string) (bool, error) {
	var pending bool
	e := s.db.QueryRow(ctx, `SELECT observed_state<>'connected' OR (setup_request_id IS NOT NULL AND setup_state NOT IN ('waiting_data','ready')) FROM xinsight_connector_installations WHERE installation_id=$1 AND observed_state<>'disconnected'`, id).Scan(&pending)
	return pending, e
}
