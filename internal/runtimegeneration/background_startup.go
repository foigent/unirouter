package runtimegeneration

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/installstate"
)

// BackgroundStartAllowed observes existing native authority without joining
// ingress or changing it. A completed whole-fleet withdrawal holds a new
// process's business writers until admission completes. No head is the normal
// fresh-install path; an unknown/malformed head must never become that path.
// This is startup ordering, not Gateway or settlement execution authority.
func BackgroundStartAllowed(ctx context.Context, pool *pgxpool.Pool, identity Identity, artifactLedger []string) (bool, error) {
	if err := identity.Validate(); err != nil {
		return false, err
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err := installstate.RequireOrdinary(ctx, tx); err != nil {
		return false, err
	}
	_, head, err := readLeaseLedger(ctx, tx, artifactLedger)
	if err != nil {
		return false, err
	}
	var live bool
	var wire []byte
	var digest string
	if err := tx.QueryRow(ctx, `SELECT g.artifact_digest=$2 AND g.compatibility_epoch=$3
AND g.schema_head=$4 AND g.state='serving' AND g.lease_expires_at>clock_timestamp(),m.canonical_bytes,m.manifest_digest
FROM runtime_generations g JOIN runtime_support_manifests m USING(artifact_digest)
WHERE g.instance_id=$1`, identity.Instance, identity.Artifact, CompatibilityEpoch, head).Scan(&live, &wire, &digest); err != nil {
		return false, err
	}
	if !live {
		return false, nil
	}
	manifest, err := VerifyManifest(wire, digest)
	if err != nil {
		return false, err
	}
	if err := requireLeaseManifest(manifest, head); err != nil {
		return false, err
	}
	rows, err := tx.Query(ctx, `SELECT c.change_id FROM runtime_ingress_changes c
WHERE NOT EXISTS(SELECT 1 FROM runtime_ingress_changes n WHERE n.predecessor_id=c.change_id) LIMIT 2`)
	if err != nil {
		return false, err
	}
	var heads []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return false, err
		}
		heads = append(heads, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	switch len(heads) {
	case 0:
		// A corrupt cycle has records but no head; it is not a fresh install.
		var empty bool
		if err := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM runtime_ingress_changes)`).Scan(&empty); err != nil {
			return false, err
		}
		return empty, nil
	case 1:
		plan, status, err := ReadIngress(ctx, tx, heads[0])
		if err != nil {
			return false, err
		}
		switch status {
		case "applied":
			return len(plan.After) > 0, nil
		case "pending", "ambiguous":
			return false, nil
		default:
			return false, fmt.Errorf("unknown native ingress state")
		}
	default:
		return false, fmt.Errorf("native ingress has multiple heads")
	}
}

// BackgroundStartup owns only one process's initial business-worker launch.
// The existing lease refresher calls Admit after a successful native read.
// Workers retain their original contexts and drain order after launch: a later
// withdrawal must not cancel accepted work's financial/content terminalization.
type BackgroundStartup struct {
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	admitted   bool
	configured bool
	preparing  bool
	finished   bool
	prepare    func(context.Context) error
	workers    []backgroundWorker
	done       chan struct{}
	err        error
}

type backgroundWorker struct {
	ctx  context.Context
	run  func(context.Context)
	done chan struct{}
}

func NewBackgroundStartup() *BackgroundStartup {
	ctx, cancel := context.WithCancel(context.Background())
	return &BackgroundStartup{ctx: ctx, cancel: cancel, done: make(chan struct{})}
}

// Go collects a worker before Start. Its completion handle also closes when a
// held startup is canceled or baseline preparation fails, without running it.
func (s *BackgroundStartup) Go(ctx context.Context, run func(context.Context)) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.configured || s.ctx.Err() != nil {
		panic("background workers must be collected before startup")
	}
	w := backgroundWorker{ctx: ctx, run: run, done: make(chan struct{})}
	s.workers = append(s.workers, w)
	return w.done
}

func (s *BackgroundStartup) Admitted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.admitted && s.ctx.Err() == nil
}

func (s *BackgroundStartup) Admit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() == nil {
		s.admitted = true
		s.startLocked()
	}
}

// Start seals the production worker collection. Deferred baseline work must
// finish successfully before any worker is launched. Stop and group launch
// serialize on the same lock, so canceled preparation cannot partially launch.
func (s *BackgroundStartup) Start(prepare func(context.Context) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.configured {
		panic("background startup configured twice")
	}
	s.configured, s.prepare = true, prepare
	s.startLocked()
}

func (s *BackgroundStartup) startLocked() {
	if !s.configured || !s.admitted || s.preparing || s.finished || s.ctx.Err() != nil {
		return
	}
	s.preparing = true
	go func() {
		var err error
		if s.prepare != nil {
			err = s.prepare(s.ctx)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.ctx.Err() != nil {
			err = s.ctx.Err()
		}
		for _, w := range s.workers {
			if err != nil {
				close(w.done)
				continue
			}
			go func(w backgroundWorker) {
				defer close(w.done)
				if w.ctx.Err() == nil {
					w.run(w.ctx)
				}
			}(w)
		}
		s.err, s.finished = err, true
		close(s.done)
	}()
}

// Stop prevents a held launch and cancels only deferred baseline preparation.
// It deliberately leaves already launched workers to their existing owners.
func (s *BackgroundStartup) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel()
	if !s.preparing && !s.finished {
		for _, w := range s.workers {
			close(w.done)
		}
		s.err, s.finished = s.ctx.Err(), true
		close(s.done)
	}
}

func (s *BackgroundStartup) Done() <-chan struct{} { return s.done }

// Ready opens new ordinary HTTP only after baseline success and group launch.
// Once started, withdrawal/Stop does not revoke accepted work's dependencies.
func (s *BackgroundStartup) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finished && s.err == nil
}

func (s *BackgroundStartup) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
