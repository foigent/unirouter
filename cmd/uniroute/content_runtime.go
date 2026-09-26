package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

type contentRuntime struct {
	service           *sourcefacts.ContentCaptureService
	files             *contentcapture.FileStore
	options           sourcefacts.ContentCaptureServiceConfig
	maintenanceCancel context.CancelFunc
	maintenanceDone   <-chan struct{}
}

func (c *contentRuntime) captureClient(next http.Handler) http.Handler {
	if c == nil {
		return next
	}
	return middleware.CaptureContent(c.service)(next)
}

func contentRuntimeOptions(cfg *config.Config, owner string) sourcefacts.ContentCaptureServiceConfig {
	lifetime := max(cfg.StreamPolicyConfig().MaxDuration(), time.Duration(cfg.AsyncMediaProviderTimeoutSecs)*time.Second, 5*time.Minute) + time.Minute
	return sourcefacts.ContentCaptureServiceConfig{
		Policy: contentcapture.PolicyStoreOptions{SourceInstance: cfg.CompletedExecutionFeedSourceInstance, DefaultMode: cfg.PayloadCaptureMode, FullContentEnabled: cfg.SourceContentFullEnabled,
			SupportedProtocols: []string{string(modeloperation.IDChatCompletions), string(modeloperation.IDEmbeddings), string(modeloperation.IDResponses), string(modeloperation.IDMessages), string(modeloperation.IDMediaJobs), endpointprotocol.ProfileOpenAIImageGPT},
			ObservationPoints:  []string{"client_request", "provider_request", "provider_response", "client_response"},
			Limits:             contentcapture.PolicyLimits{MaxSavedBytes: 32 << 20, MaxReadBytes: 32 << 20, MaxBufferBytes: 32 << 10, MaxManifestBytes: 64 << 10, MaxManifestItems: 100}},
		Engine:     contentcapture.EngineOptions{Queue: contentcapture.QueueLimits{MaxItems: 2048, MaxBytes: 64 << 20, MaxProjectBytes: 32 << 20, MaxChunkBytes: 32 << 10}, MaxActive: 128, MaxProjectActive: 32, PrepareTimeout: 250 * time.Millisecond, FinalizeTimeout: 5 * time.Second, MaxLifetime: lifetime},
		LeaseOwner: owner, LeaseTTL: lifetime + time.Minute, StartTimeout: 250 * time.Millisecond, HealthTimeout: 250 * time.Millisecond,
	}
}

func openContentRuntimeStore(cfg *config.Config) (*contentcapture.FileStore, error) {
	if cfg.SourceContentStorageRoot == "" {
		return nil, nil
	}
	// Never initialize or replace a marker here, and never include token bytes
	// in errors. CLI and main share the private, anchored token reader.
	return contentcapture.OpenBoundFileStore(cfg.SourceContentStorageRoot, cfg.SourceContentStorageIdentity, cfg.SourceContentStorageTokenFile)
}

func newContentRuntime(cfg *config.Config, source *sourcefacts.Service) (*contentRuntime, error) {
	if !cfg.SourceContentEnabled {
		return nil, nil
	}
	files, err := openContentRuntimeStore(cfg)
	if err != nil {
		return nil, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		_ = files.Close()
		return nil, err
	}
	options := contentRuntimeOptions(cfg, "content-"+hex.EncodeToString(nonce[:]))
	service, err := sourcefacts.NewContentCaptureService(source, files, options)
	if err != nil {
		_ = files.Close()
		return nil, err
	}
	return &contentRuntime{service: service, files: files, options: options}, nil
}

// startMaintenance is called once by the runtime owner, after preflight.
func (c *contentRuntime) startMaintenance(source *sourcefacts.Service) {
	if c == nil || c.files == nil {
		return
	}
	c.startExpiryOwner(func(ctx context.Context) {
		result, err := source.ReclaimExpiredContent(ctx, c.files, 1000)
		if err != nil && ctx.Err() == nil {
			log.Printf("Content expiry incomplete: examined=%d reclaimed=%d failed=%d", result.Examined, result.Reclaimed, result.Failed)
		}
	})
}

func (c *contentRuntime) startExpiryOwner(reclaim func(context.Context)) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	c.maintenanceCancel = cancel
	c.maintenanceDone = done
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			batch, cancelBatch := context.WithTimeout(ctx, 30*time.Second)
			reclaim(batch)
			cancelBatch()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (c *contentRuntime) registerReads(mux *http.ServeMux, source *sourcefacts.Service, resolver middleware.ClientIPResolver) error {
	if c == nil {
		return nil
	}
	authority, err := source.NewContentBodyAuthority(resolver)
	if err != nil {
		return err
	}
	l := c.options.Policy.Limits
	modes := []string{contentcapture.ModeDisabled, contentcapture.ModeMetadataOnly}
	if c.options.Policy.FullContentEnabled {
		modes = append(modes, contentcapture.ModeFullContent)
	}
	reads, err := sourcefacts.NewContentReadHandlers(sourcefacts.ContentReadHandlerConfig{Authority: authority, Store: c.files, SupportedProtocols: c.options.Policy.SupportedProtocols, ObservationPoints: c.options.Policy.ObservationPoints, CaptureModes: modes,
		Limits: sourcefacts.ContentLimits{MaxSavedBytes: l.MaxSavedBytes, MaxReadBytes: l.MaxReadBytes, MaxBufferBytes: l.MaxBufferBytes, MaxManifestBytes: l.MaxManifestBytes, MaxManifestItems: int(l.MaxManifestItems)}, MaxPageBytes: sourcefacts.MaxPageBytes, MaxVerifyBytesPerPage: 64 << 20, MaxConcurrentRequests: 8, RequestTimeout: 30 * time.Second, WriteTimeout: 5 * time.Second})
	if err != nil {
		return err
	}
	body, err := sourcefacts.NewContentBodyHandler(sourcefacts.ContentBodyHandlerConfig{Authority: authority, Store: c.files, MaxReadBytes: l.MaxReadBytes, MaxBufferBytes: l.MaxBufferBytes, MaxConcurrentReads: 8, RequestTimeout: 2 * time.Minute, WriteTimeout: 5 * time.Second})
	if err != nil {
		return err
	}
	mux.Handle("GET "+sourcefacts.ContentCapabilitiesPath, reads.Capabilities)
	mux.Handle("GET "+sourcefacts.ContentManifestsPath, reads.Manifests)
	mux.Handle("GET "+sourcefacts.ContentEndpointTemplate, body)
	return nil
}

func (c *contentRuntime) close(ctx context.Context, usersDrained bool) error {
	if c == nil {
		return nil
	}
	if c.maintenanceCancel != nil {
		c.maintenanceCancel()
	}
	if c.maintenanceDone != nil {
		select {
		case <-c.maintenanceDone:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := c.service.Close(ctx); err != nil {
		return err
	}
	// Readers may still own verified file handles/root operations after a drain
	// timeout. Retain the root until process exit, even if Engine was idle.
	if !usersDrained {
		return errors.New("content storage retained: HTTP or media users did not drain")
	}
	return c.files.Close()
}
