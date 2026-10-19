package main

import (
	"context"
	"errors"
	"time"

	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

// The original supervisor owns automatic work. One connection gets one unit
// per persisted cadence; explicit Sync once re-discovers after each finite unit.
func (s *xInsightConnectorRuntimeSupervisor) communicationUnit(ctx context.Context, d xinsightconnector.RuntimeInstallationDescriptor, kind string) (bool, error) {
	if s.setupAllowed != nil {
		if e := s.setupAllowed(ctx, d.InstallationID); e != nil {
			return false, e
		}
	}
	switch kind {
	case "credential":
		c, e := s.counterpartFactory(ctx, d)
		if e != nil {
			return false, e
		}
		r, ok := c.(xInsightCredentialRunner)
		if !ok {
			return false, xinsightconnector.ErrUnavailable
		}
		return true, r.RunCredential(ctx)
	case "redeem":
		r, e := s.redeemerFactory(ctx, d)
		if e != nil {
			return false, e
		}
		return true, r.Redeem(ctx)
	default:
		c, e := s.counterpartFactory(ctx, d)
		if e != nil {
			return false, e
		}
		a, e := s.agentFactory(d.InstallationID, c)
		if e != nil {
			return false, e
		}
		worked, err := a.RunOnce(ctx)
		if err != nil {
			return worked, err
		}
		if s.observePeer != nil {
			if err = s.observePeer(ctx, d.InstallationID, d.XDeploymentID); err != nil {
				return worked, err
			}
		}
		return worked, nil
	}
}

func (s *xInsightConnectorRuntimeSupervisor) runCommunicationCycle(ctx context.Context) error {
	ids, e := s.communication.AutomaticInstallationIDs(ctx, xInsightConnectorRuntimeDiscoveryLimit)
	if e == nil {
		for _, id := range ids {
			d, kind, discoveryErr := s.communication.RuntimeUnit(ctx, id)
			if discoveryErr != nil {
				e = errors.Join(e, discoveryErr)
				continue
			}
			if kind == "" {
				continue
			}
			p, claimErr := s.communication.ClaimAutomatic(ctx, d.InstallationID, "automatic")
			if errors.Is(claimErr, xinsightconnector.ErrCommunicationPaused) || errors.Is(claimErr, xinsightconnector.ErrCommunicationBusy) {
				continue
			}
			if claimErr != nil {
				e = errors.Join(e, claimErr)
				continue
			}
			code, success, unitErr := s.runCommunicationPermit(ctx, p, d, kind, false)
			finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			finishErr := p.Finish(finishCtx, code, success)
			cancel()
			e = errors.Join(e, unitErr, finishErr)
		}
	}
	if s.setupAdvance != nil {
		e = errors.Join(e, s.setupAdvance(ctx, xInsightConnectorRuntimeDiscoveryLimit))
	}
	return e
}

func (s *xInsightConnectorRuntimeSupervisor) runCommunicationPermit(parent context.Context, p *xinsightconnector.CommunicationPermit, initial xinsightconnector.RuntimeInstallationDescriptor, kind string, burst bool) (string, bool, error) {
	// Network admission is separately bounded by the permit. A received remote
	// result must still be recorded after a pause/cancel/deadline; local journal
	// completion gets a finite drain window, and never invents remote success.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), xinsightconnector.CommunicationLocalWindow)
	defer cancel()
	ctx = xinsightconnector.WithCommunicationPermit(ctx, p)
	success := false
	for n := 0; n < xinsightconnector.CommunicationMaxUnits; n++ {
		if e := p.NextUnit(ctx); e != nil {
			return "partial", success, e
		}
		worked, e := s.communicationUnit(ctx, initial, kind)
		if e != nil {
			if errors.Is(e, xinsightconnector.ErrCommunicationPaused) {
				return "partial", success, e
			}
			return "failed", success, e
		}
		success = true
		if burst && s.advanceOne != nil {
			if e = s.advanceOne(ctx, initial.InstallationID); e != nil {
				return "partial", success, e
			}
		}
		if !worked {
			pending, e := s.communication.InitializationPending(ctx, initial.InstallationID)
			if e != nil {
				return "partial", success, e
			}
			if !pending && s.setupPending != nil {
				pending, e = s.setupPending(ctx, initial.InstallationID)
			}
			if e != nil {
				return "partial", success, e
			}
			if pending {
				return "partial", true, nil
			}
			return "empty", true, nil
		}
		if !burst {
			return "completed", true, nil
		}

		d, k, e := s.communication.RuntimeUnit(ctx, initial.InstallationID)
		if e != nil {
			return "partial", success, e
		}
		if k == "" {
			return "completed", true, nil
		}
		initial, kind = d, k
	}
	return "partial", success, nil
}

func (s *xInsightConnectorRuntimeSupervisor) syncCommunicationOnce(ctx context.Context, p *xinsightconnector.CommunicationPermit, id string) (string, bool, error) {
	if s == nil || !s.enabled {
		return "failed", false, xinsightconnector.ErrUnavailable
	}
	d, kind, e := s.communication.RuntimeUnit(ctx, id)
	if e != nil {
		return "failed", false, e
	}
	if kind == "" {
		return "empty", false, nil
	}
	return s.runCommunicationPermit(ctx, p, d, kind, true)
}
