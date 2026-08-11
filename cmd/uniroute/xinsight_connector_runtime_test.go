package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

func TestXInsightConnectorRuntimeDisabledIsInert(t *testing.T) {
	supervisor, err := newConfiguredXInsightConnectorRuntimeSupervisor(
		&config.Config{XInsightConnectorAgentEnabled: false},
		xInsightConnectorRuntimeDependencies{},
		nil,
	)
	require.NoError(t, err)
	done := startXInsightConnectorRuntime(context.Background(), supervisor)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("disabled Connector runtime did not remain inert")
	}
}

func TestXInsightConnectorRuntimeRedeemsBeforePollingOnNextCycle(t *testing.T) {
	descriptor := xinsightconnector.RuntimeInstallationDescriptor{
		InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA",
		XDeploymentID:  "xid_BBBBBBBBBBBBBBBBBBBBBB",
	}
	pending := true
	var events []string
	source := xInsightConnectorRuntimeTestSource{
		redeem: func(
			_ context.Context,
			limit int,
		) ([]xinsightconnector.RuntimeInstallationDescriptor, error) {
			require.Equal(t, xInsightConnectorRuntimeDiscoveryLimit, limit)
			events = append(events, "discover_redeem")
			if pending {
				return []xinsightconnector.RuntimeInstallationDescriptor{descriptor}, nil
			}
			return nil, nil
		},
		runnable: func(
			context.Context,
			int,
		) ([]xinsightconnector.RuntimeInstallationDescriptor, error) {
			events = append(events, "discover_runnable")
			if pending {
				return nil, nil
			}
			return []xinsightconnector.RuntimeInstallationDescriptor{descriptor}, nil
		},
	}
	counterpart := &xInsightConnectorRuntimeCounterpart{}
	supervisor, err := newXInsightConnectorRuntimeSupervisor(
		true,
		time.Second,
		source,
		func(
			_ context.Context,
			got xinsightconnector.RuntimeInstallationDescriptor,
		) (xInsightConnectorRuntimeRedeemer, error) {
			require.Equal(t, descriptor, got)
			events = append(events, "redeemer")
			return xInsightConnectorRuntimeRedeemerFunc(func(context.Context) error {
				events = append(events, "redeem")
				pending = false
				return nil
			}), nil
		},
		func(
			context.Context,
			xinsightconnector.RuntimeInstallationDescriptor,
		) (xinsightconnector.ControlCounterpart, error) {
			events = append(events, "counterpart")
			return counterpart, nil
		},
		func(string, xinsightconnector.ControlCounterpart) (xInsightConnectorRuntimeAgent, error) {
			return xInsightConnectorRuntimeAgentFunc(func(context.Context) (bool, error) {
				events = append(events, "poll_ack")
				return false, nil
			}), nil
		},
		nil,
	)
	require.NoError(t, err)
	require.NoError(t, supervisor.runCycle(context.Background()))
	require.Equal(t, []string{"discover_runnable", "discover_redeem", "redeemer", "redeem"}, events,
		"redeem must not cross its durable promotion boundary into Poll/Ack in one cycle")

	require.NoError(t, supervisor.runCycle(context.Background()))
	require.Equal(t, []string{
		"discover_runnable", "discover_redeem", "redeemer", "redeem",
		"discover_runnable", "discover_redeem", "counterpart", "poll_ack",
	}, events)
}

func TestXInsightConnectorRuntimeProcessesBoundedControlInstallationsSequentially(t *testing.T) {
	installations := []xinsightconnector.RuntimeInstallationDescriptor{{
		InstallationID:   "xci_AAAAAAAAAAAAAAAAAAAAAA",
		XDeploymentID:    "xid_BBBBBBBBBBBBBBBBBBBBBB",
		ControlProjectID: "project.second-integration",
	}, {
		InstallationID:   "xci_CCCCCCCCCCCCCCCCCCCCCC",
		XDeploymentID:    "xid_DDDDDDDDDDDDDDDDDDDDDD",
		ControlProjectID: "project.other-integration",
	}}
	var events []string
	var active, maximum atomic.Int32
	source := xInsightConnectorRuntimeTestSource{runnable: func(
		_ context.Context,
		limit int,
	) ([]xinsightconnector.RuntimeInstallationDescriptor, error) {
		require.Equal(t, xInsightConnectorRuntimeDiscoveryLimit, limit)
		events = append(events, "discover")
		return installations, nil
	}}
	counterpart := &xInsightConnectorRuntimeCounterpart{}
	supervisor, err := newXInsightConnectorRuntimeSupervisor(
		true,
		time.Second,
		source,
		xInsightConnectorRuntimeNoopRedeemerFactory,
		func(
			_ context.Context,
			got xinsightconnector.RuntimeInstallationDescriptor,
		) (xinsightconnector.ControlCounterpart, error) {
			events = append(events, "counterpart:"+got.InstallationID)
			return counterpart, nil
		},
		func(
			installationID string,
			got xinsightconnector.ControlCounterpart,
		) (xInsightConnectorRuntimeAgent, error) {
			require.Same(t, counterpart, got)
			events = append(events, "agent:"+installationID)
			return xInsightConnectorRuntimeAgentFunc(func(context.Context) (bool, error) {
				current := active.Add(1)
				defer active.Add(-1)
				if current > maximum.Load() {
					maximum.Store(current)
				}
				events = append(events, "run_once:"+installationID)
				return false, nil
			}), nil
		},
		nil,
	)
	require.NoError(t, err)
	require.NoError(t, supervisor.runCycle(context.Background()))
	require.Equal(t, []string{
		"discover",
		"counterpart:xci_AAAAAAAAAAAAAAAAAAAAAA",
		"agent:xci_AAAAAAAAAAAAAAAAAAAAAA",
		"run_once:xci_AAAAAAAAAAAAAAAAAAAAAA",
		"counterpart:xci_CCCCCCCCCCCCCCCCCCCCCC",
		"agent:xci_CCCCCCCCCCCCCCCCCCCCCC",
		"run_once:xci_CCCCCCCCCCCCCCCCCCCCCC",
	}, events)
	require.Equal(t, int32(1), maximum.Load())
}

func TestXInsightConnectorRuntimeRejectsUnboundedDiscovery(t *testing.T) {
	source := xInsightConnectorRuntimeTestSource{runnable: func(
		context.Context,
		int,
	) ([]xinsightconnector.RuntimeInstallationDescriptor, error) {
		installations := make([]xinsightconnector.RuntimeInstallationDescriptor, xInsightConnectorRuntimeDiscoveryLimit+1)
		return installations, nil
	}}
	var factoryCalls atomic.Int32
	supervisor, err := newXInsightConnectorRuntimeSupervisor(
		true,
		time.Second,
		source,
		xInsightConnectorRuntimeNoopRedeemerFactory,
		func(
			context.Context,
			xinsightconnector.RuntimeInstallationDescriptor,
		) (xinsightconnector.ControlCounterpart, error) {
			factoryCalls.Add(1)
			return &xInsightConnectorRuntimeCounterpart{}, nil
		},
		func(string, xinsightconnector.ControlCounterpart) (xInsightConnectorRuntimeAgent, error) {
			return xInsightConnectorRuntimeAgentFunc(func(context.Context) (bool, error) {
				return false, nil
			}), nil
		},
		nil,
	)
	require.NoError(t, err)
	err = supervisor.runCycle(context.Background())
	require.ErrorContains(t, err, "exceeded bounded limit")
	require.Zero(t, factoryCalls.Load())
}

func TestXInsightConnectorRuntimeContinuesAfterOneInstallationFailure(t *testing.T) {
	installations := []xinsightconnector.RuntimeInstallationDescriptor{
		{InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA"},
		{InstallationID: "xci_BBBBBBBBBBBBBBBBBBBBBB"},
	}
	source := xInsightConnectorRuntimeTestSource{runnable: func(
		context.Context,
		int,
	) ([]xinsightconnector.RuntimeInstallationDescriptor, error) {
		return installations, nil
	}}
	var processed []string
	supervisor, err := newXInsightConnectorRuntimeSupervisor(
		true,
		time.Second,
		source,
		xInsightConnectorRuntimeNoopRedeemerFactory,
		func(
			_ context.Context,
			installation xinsightconnector.RuntimeInstallationDescriptor,
		) (xinsightconnector.ControlCounterpart, error) {
			if installation.InstallationID == installations[0].InstallationID {
				return nil, errors.New("synthetic first-installation outage")
			}
			return &xInsightConnectorRuntimeCounterpart{}, nil
		},
		func(
			installationID string,
			_ xinsightconnector.ControlCounterpart,
		) (xInsightConnectorRuntimeAgent, error) {
			return xInsightConnectorRuntimeAgentFunc(func(context.Context) (bool, error) {
				processed = append(processed, installationID)
				return false, nil
			}), nil
		},
		nil,
	)
	require.NoError(t, err)
	err = supervisor.runCycle(context.Background())
	require.ErrorContains(t, err, "1 of 2")
	require.Equal(t, []string{installations[1].InstallationID}, processed)
}

func TestXInsightConnectorRuntimePendingFailureDoesNotStarveActiveInstallation(t *testing.T) {
	pending := xinsightconnector.RuntimeInstallationDescriptor{
		InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA",
	}
	runnable := xinsightconnector.RuntimeInstallationDescriptor{
		InstallationID: "xci_BBBBBBBBBBBBBBBBBBBBBB",
	}
	source := xInsightConnectorRuntimeTestSource{
		redeem: func(
			context.Context,
			int,
		) ([]xinsightconnector.RuntimeInstallationDescriptor, error) {
			return []xinsightconnector.RuntimeInstallationDescriptor{pending}, nil
		},
		runnable: func(
			context.Context,
			int,
		) ([]xinsightconnector.RuntimeInstallationDescriptor, error) {
			return []xinsightconnector.RuntimeInstallationDescriptor{runnable}, nil
		},
	}
	var processed []string
	supervisor, err := newXInsightConnectorRuntimeSupervisor(
		true,
		time.Second,
		source,
		func(
			context.Context,
			xinsightconnector.RuntimeInstallationDescriptor,
		) (xInsightConnectorRuntimeRedeemer, error) {
			return xInsightConnectorRuntimeRedeemerFunc(func(context.Context) error {
				return errors.New("synthetic expired invitation")
			}), nil
		},
		func(
			context.Context,
			xinsightconnector.RuntimeInstallationDescriptor,
		) (xinsightconnector.ControlCounterpart, error) {
			return &xInsightConnectorRuntimeCounterpart{}, nil
		},
		func(
			installationID string,
			_ xinsightconnector.ControlCounterpart,
		) (xInsightConnectorRuntimeAgent, error) {
			return xInsightConnectorRuntimeAgentFunc(func(context.Context) (bool, error) {
				processed = append(processed, installationID)
				return false, nil
			}), nil
		},
		nil,
	)
	require.NoError(t, err)
	err = supervisor.runCycle(context.Background())
	require.ErrorContains(t, err, "1 of 1 pending")
	require.Equal(t, []string{runnable.InstallationID}, processed)
}

func TestXInsightConnectorRuntimeGracefullyStopsOneActiveCycle(t *testing.T) {
	entered := make(chan struct{})
	var enteredOnce sync.Once
	var active atomic.Int32
	var maximum atomic.Int32
	source := xInsightConnectorRuntimeTestSource{runnable: func(
		context.Context,
		int,
	) ([]xinsightconnector.RuntimeInstallationDescriptor, error) {
		return []xinsightconnector.RuntimeInstallationDescriptor{{
			InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA",
		}}, nil
	}}
	counterpart := &xInsightConnectorRuntimeCounterpart{}
	supervisor, err := newXInsightConnectorRuntimeSupervisor(
		true,
		time.Millisecond,
		source,
		xInsightConnectorRuntimeNoopRedeemerFactory,
		func(
			context.Context,
			xinsightconnector.RuntimeInstallationDescriptor,
		) (xinsightconnector.ControlCounterpart, error) {
			return counterpart, nil
		},
		func(string, xinsightconnector.ControlCounterpart) (xInsightConnectorRuntimeAgent, error) {
			return xInsightConnectorRuntimeAgentFunc(func(ctx context.Context) (bool, error) {
				current := active.Add(1)
				defer active.Add(-1)
				for {
					previous := maximum.Load()
					if current <= previous || maximum.CompareAndSwap(previous, current) {
						break
					}
				}
				enteredOnce.Do(func() { close(entered) })
				<-ctx.Done()
				return true, ctx.Err()
			}), nil
		},
		nil,
	)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := startXInsightConnectorRuntime(ctx, supervisor)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("Connector runtime did not enter its bounded cycle")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Connector runtime did not stop after app context cancellation")
	}
	require.Zero(t, active.Load())
	require.Equal(t, int32(1), maximum.Load())
}

func TestXInsightConnectorRuntimeKeepsControlFailureOutOfGlobalLifecycle(t *testing.T) {
	controlFailure := errors.New("synthetic control-plane outage xib_must_not_log")
	logs := make(chan string, 1)
	source := xInsightConnectorRuntimeTestSource{runnable: func(
		context.Context,
		int,
	) ([]xinsightconnector.RuntimeInstallationDescriptor, error) {
		return nil, controlFailure
	}}
	supervisor, err := newXInsightConnectorRuntimeSupervisor(
		true,
		time.Millisecond,
		source,
		xInsightConnectorRuntimeNoopRedeemerFactory,
		func(
			context.Context,
			xinsightconnector.RuntimeInstallationDescriptor,
		) (xinsightconnector.ControlCounterpart, error) {
			return nil, errors.New("must not build counterpart")
		},
		func(string, xinsightconnector.ControlCounterpart) (xInsightConnectorRuntimeAgent, error) {
			return nil, errors.New("must not build agent")
		},
		func(format string, args ...any) {
			select {
			case logs <- format:
			default:
			}
		},
	)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := startXInsightConnectorRuntime(ctx, supervisor)
	var message string
	select {
	case message = <-logs:
	case <-time.After(time.Second):
		t.Fatal("Connector runtime did not report its local control health")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Connector runtime did not stop after health test cancellation")
	}
	require.NotContains(t, message, controlFailure.Error())
	require.NotContains(t, message, "xib_must_not_log")
}

type xInsightConnectorRuntimeTestSource struct {
	redeem   func(context.Context, int) ([]xinsightconnector.RuntimeInstallationDescriptor, error)
	runnable func(context.Context, int) ([]xinsightconnector.RuntimeInstallationDescriptor, error)
}

func (source xInsightConnectorRuntimeTestSource) ListRedeemPendingInstallations(
	ctx context.Context,
	limit int,
) ([]xinsightconnector.RuntimeInstallationDescriptor, error) {
	if source.redeem == nil {
		return nil, nil
	}
	return source.redeem(ctx, limit)
}

func (source xInsightConnectorRuntimeTestSource) ListRunnableInstallations(
	ctx context.Context,
	limit int,
) ([]xinsightconnector.RuntimeInstallationDescriptor, error) {
	return source.runnable(ctx, limit)
}

type xInsightConnectorRuntimeRedeemerFunc func(context.Context) error

func (fn xInsightConnectorRuntimeRedeemerFunc) Redeem(ctx context.Context) error {
	return fn(ctx)
}

var xInsightConnectorRuntimeNoopRedeemerFactory = func(
	context.Context,
	xinsightconnector.RuntimeInstallationDescriptor,
) (xInsightConnectorRuntimeRedeemer, error) {
	return xInsightConnectorRuntimeRedeemerFunc(func(context.Context) error { return nil }), nil
}

type xInsightConnectorRuntimeCounterpart struct{}

func (*xInsightConnectorRuntimeCounterpart) Poll(context.Context) ([]byte, bool, error) {
	return nil, false, nil
}

func (*xInsightConnectorRuntimeCounterpart) Acknowledge(context.Context, []byte) error {
	return nil
}

type xInsightConnectorRuntimeAgentFunc func(context.Context) (bool, error)

func (fn xInsightConnectorRuntimeAgentFunc) RunOnce(ctx context.Context) (bool, error) {
	return fn(ctx)
}
