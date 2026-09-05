package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

// A bridge starts on the exact legacy schema without inventing lease rows.
// After the explicit expand, it verifies that committed authority and joins
// the lease protocol. Actual ingress membership remains controller-owned.
func startRuntimeGeneration(ctx context.Context, pool *pgxpool.Pool, runtime *httpRuntime, artifactLedger []string, initialLedger []string) (func(), <-chan struct{}, error) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	head := initialLedger[len(initialLedger)-1]
	manifest := runtimegeneration.CurrentManifest([]string{schemaauthority.LegacyCapabilityHead, schemaauthority.CapabilityExpandHead})
	refresh := func() error {
		leaseCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		if head == schemaauthority.LegacyCapabilityHead {
			ledger, err := readCompatibleRuntimeSchema(leaseCtx, pool, artifactLedger)
			if err != nil {
				return err
			}
			if ledger[len(ledger)-1] == head {
				runtime.mu.Lock()
				runtime.authorityUnavailable = false
				runtime.mu.Unlock()
				return nil
			}
			verified, err := verifyRuntimeAuthority(leaseCtx, pool, artifactLedger)
			if err != nil {
				return err
			}
			head = verified[len(verified)-1]
			wire, _ := json.Marshal(verified)
			runtime.mu.Lock()
			runtime.schemaDigest = fmt.Sprintf("%x", sha256.Sum256(wire))
			runtime.leaseRequired = true
			runtime.mu.Unlock()
		}
		runtime.mu.Lock()
		identity := runtimegeneration.Identity{Instance: runtime.instance, Artifact: runtime.artifact}
		draining := runtime.draining
		runtime.leaseRequired = true
		runtime.mu.Unlock()
		until, _, err := runtimegeneration.Renew(leaseCtx, pool, identity, manifest, artifactLedger, draining)
		runtime.mu.Lock()
		defer runtime.mu.Unlock()
		if err != nil {
			return err
		}
		runtime.leaseUntil = until
		runtime.authorityUnavailable = false
		return nil
	}
	if err := refresh(); err != nil {
		cancel()
		close(done)
		return nil, done, err
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(runtimegeneration.LeaseRefresh)
		defer ticker.Stop()
		failed := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := refresh()
				if err != nil {
					runtime.mu.Lock()
					// A transient renewal error cannot extend the previous confirmed
					// lease. It expires on its conservative local deadline. Explicit
					// incompatibility and the pre-lease legacy phase fence immediately.
					if head == schemaauthority.LegacyCapabilityHead || errors.Is(err, runtimegeneration.ErrIncompatible) {
						runtime.authorityUnavailable = true
					}
					runtime.mu.Unlock()
					if !failed {
						log.Printf("Runtime authority renewal unavailable; admission requires the last confirmed compatible lease: %v", err)
					}
					failed = true
				} else {
					if failed {
						log.Print("Runtime authority renewal recovered")
					}
					failed = false
				}
			}
		}
	}()
	return cancel, done, nil
}
