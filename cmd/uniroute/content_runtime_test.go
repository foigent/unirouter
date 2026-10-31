package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
)

func TestContentRuntimeStoreOpensOnlyExistingExactBinding(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	tokenRoot, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Chmod(root, 0700))
	require.NoError(t, os.Chmod(tokenRoot, 0700))
	tokenFile := filepath.Join(tokenRoot, "binding")
	cfg := &config.Config{SourceContentStorageRoot: root, SourceContentStorageIdentity: "synthetic-runtime-store", SourceContentStorageTokenFile: tokenFile}
	_, err = openContentRuntimeStore(cfg)
	require.Error(t, err)
	require.NoError(t, os.WriteFile(tokenFile, []byte(strings.Repeat("a", 64)), 0600))
	_, err = openContentRuntimeStore(cfg)
	require.Error(t, err)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, entries, "startup must not create a marker")
	binding, err := contentcapture.InitializeFileStore(root, cfg.SourceContentStorageIdentity)
	require.NoError(t, err)
	_, err = openContentRuntimeStore(cfg)
	require.Error(t, err, "wrong token must fail")
	require.NoError(t, os.WriteFile(tokenFile, []byte(binding.Token+"\n"), 0600))
	store, err := openContentRuntimeStore(cfg)
	require.NoError(t, err)
	require.NoError(t, store.CheckReady(t.Context()))
	require.NoError(t, store.Close())
	cfg.SourceContentStorageIdentity = "wrong"
	_, err = openContentRuntimeStore(cfg)
	require.Error(t, err)
	require.NoError(t, os.WriteFile(tokenFile, []byte(strings.Repeat("secret-", 100)), 0600))
	_, err = openContentRuntimeStore(cfg)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret-")
}

func TestContentRuntimeBoundsFollowStreamDurationAndPreserveGlobalStop(t *testing.T) {
	cfg := &config.Config{PayloadCaptureMode: contentcapture.ModeDisabled, SourceContentFullEnabled: true, StreamMaxDurationSecs: 86400, AsyncMediaProviderTimeoutSecs: 900}
	o := contentRuntimeOptions(cfg, "synthetic")
	require.Equal(t, contentcapture.ModeDisabled, o.Policy.DefaultMode)
	require.Greater(t, o.Engine.MaxLifetime, 24*time.Hour)
	require.Greater(t, o.LeaseTTL, o.Engine.MaxLifetime+o.Engine.FinalizeTimeout)
	require.Greater(t, o.Policy.Limits.MaxSavedBytes, int64(17<<20))
	require.Equal(t, o.Policy.Limits.MaxSavedBytes, o.Policy.Limits.MaxReadBytes)
	require.NoError(t, o.Policy.Limits.Validate())
}

func TestContentRuntimeDrainTimeoutRetainsFileRoot(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	binding, err := contentcapture.InitializeFileStore(root, "synthetic-drain")
	require.NoError(t, err)
	files, err := contentcapture.OpenFileStore(root, binding)
	require.NoError(t, err)
	// A nil service is already drained. An HTTP/media user still prevents closing.
	runtime := &contentRuntime{files: files}
	err = runtime.close(context.Background(), false)
	require.Error(t, err)
	require.NoError(t, files.CheckReady(t.Context()))
	require.NoError(t, runtime.close(context.Background(), true))
	require.Error(t, files.CheckReady(t.Context()))
}

func TestContentRuntimeMaintenanceCancelAndTimeoutRetainsRoot(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	binding, err := contentcapture.InitializeFileStore(root, "expiry-owner")
	require.NoError(t, err)
	files, err := contentcapture.OpenFileStore(root, binding)
	require.NoError(t, err)
	runtime := &contentRuntime{files: files}
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	background := runtimegeneration.NewBackgroundStartup()
	defer background.Stop()
	runtime.startExpiryOwner(background, func(ctx context.Context) { close(entered); <-ctx.Done(); close(cancelled); <-release })
	background.Admit()
	background.Start(nil)
	<-entered
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, runtime.close(ctx, true), context.DeadlineExceeded)
	<-cancelled
	require.NoError(t, files.CheckReady(t.Context()))
	close(release)
	require.NoError(t, runtime.close(t.Context(), true))
	require.Error(t, files.CheckReady(t.Context()))
}
