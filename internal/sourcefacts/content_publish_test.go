package sourcefacts

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/contentcapture"
)

func contentPublishFixture(body []byte) (contentcapture.Reservation, contentcapture.StoredReceipt, ContentManifest, contentPublicationRow) {
	m := syntheticContentManifest(body)
	m.Capture.PolicyRef = "content-policy:1:2"
	retention, quota, maxSaved := int64(86400), int64(1024), int64(128)
	c := contentcapture.PolicyConfig{Mode: contentcapture.ModeFullContent, SupportedProtocols: []string{m.Protocol}, ObservationPoints: []string{m.ObservationPoint}, RetentionSeconds: &retention, QuotaBytes: &quota, MaxSavedBytes: &maxSaved, AllowedPurposes: []string{"human_review"}, OnLimit: "record_capture_gap"}
	effective, _ := time.Parse(time.RFC3339Nano, m.Capture.PolicyEffectiveAt)
	expires, _ := time.Parse(time.RFC3339Nano, m.ExpiresAt)
	r := contentcapture.Reservation{OrgID: 1, ProjectID: 2, ContentRef: m.ContentRef, SourceFactID: m.SourceFactID, Revision: 1, RecoveryEpoch: 1, Effective: contentcapture.PolicyVersion{PolicyVersion: "1", Config: c}, EffectiveAt: m.Capture.PolicyEffectiveAt, Binding: contentcapture.StoreBinding{Identity: "synthetic-store", Token: strings.Repeat("1", 64)}, ObjectName: strings.Repeat("2", 64), ReservedBytes: 128, LeaseOwner: "synthetic-worker", LeaseExpiresAt: expires.Add(-time.Hour), ExpiresAt: expires}
	receipt := contentcapture.StoredReceipt{Binding: r.Binding, ObjectName: r.ObjectName, SavedBytes: int64(len(body)), SHA256: m.Representation.Digest}
	wire, _ := json.Marshal(c)
	row := contentPublicationRow{policyVersion: 1, policyConfig: wire, policyEffectiveAt: effective, epoch: 1, binding: r.Binding, name: r.ObjectName, state: "reserved", reserved: 128, expires: expires, leaseOwner: r.LeaseOwner, leaseExpires: r.LeaseExpiresAt}
	return r, receipt, m, row
}

func TestContentPublishOriginalPersistentBinding(t *testing.T) {
	r, receipt, m, row := contentPublishFixture([]byte("body"))
	if err := validateContentPublication(r, receipt, m); err != nil {
		t.Fatal(err)
	}
	if err := row.validate(r, receipt); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*contentPublicationRow){
		"policy_version": func(p *contentPublicationRow) { p.policyVersion++ },
		"policy_config":  func(p *contentPublicationRow) { p.policyConfig = []byte(`{"mode":"disabled"}`) },
		"valid_different_config": func(p *contentPublicationRow) {
			c := r.Effective.Config
			limit := int64(127)
			c.MaxSavedBytes = &limit
			p.policyConfig, _ = json.Marshal(c)
		},
		"policy_time":     func(p *contentPublicationRow) { p.policyEffectiveAt = p.policyEffectiveAt.Add(time.Microsecond) },
		"epoch":           func(p *contentPublicationRow) { p.epoch++ },
		"marker":          func(p *contentPublicationRow) { p.binding.Token = strings.Repeat("3", 64) },
		"name":            func(p *contentPublicationRow) { p.name = strings.Repeat("4", 64) },
		"amount":          func(p *contentPublicationRow) { p.reserved-- },
		"owner":           func(p *contentPublicationRow) { p.leaseOwner = "other" },
		"lease":           func(p *contentPublicationRow) { p.leaseExpires = p.leaseExpires.Add(time.Microsecond) },
		"expires":         func(p *contentPublicationRow) { p.expires = p.expires.Add(time.Microsecond) },
		"orphan":          func(p *contentPublicationRow) { p.state = "orphan" },
		"partially_ready": func(p *contentPublicationRow) { p.saved = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := row
			mutate(&bad)
			if !errors.Is(bad.validate(r, receipt), ErrContentPublishBinding) {
				t.Fatal("accepted changed durable binding")
			}
		})
	}
	row.state = "ready"
	row.saved = receipt.SavedBytes
	row.digest = &receipt.SHA256
	if err := row.validate(r, receipt); err != nil {
		t.Fatal(err)
	}
	row.saved++
	if !errors.Is(row.validate(r, receipt), ErrContentPublishBinding) {
		t.Fatal("accepted changed ready bytes")
	}
}

func TestContentPublishManifestMustMatchAllocation(t *testing.T) {
	r, receipt, m, _ := contentPublishFixture(nil)
	for name, mutate := range map[string]func(*ContentManifest){
		"policy_ref":     func(m *ContentManifest) { m.Capture.PolicyRef = "other" },
		"version":        func(m *ContentManifest) { m.Capture.PolicyVersion = "2" },
		"effective_time": func(m *ContentManifest) { m.Capture.PolicyEffectiveAt = "2026-09-15T00:00:01.000000Z" },
		"mode":           func(m *ContentManifest) { m.Capture.Mode = "metadata_only" },
		"protocol":       func(m *ContentManifest) { m.Protocol = "unknown" },
		"point":          func(m *ContentManifest) { m.ObservationPoint = "provider_response" },
		"fact":           func(m *ContentManifest) { m.SourceFactID = "uf1_" + strings.Repeat("b", 64) },
		"ref":            func(m *ContentManifest) { m.ContentRef = "uc1_" + strings.Repeat("b", 64) },
		"expiry":         func(m *ContentManifest) { m.ExpiresAt = "2026-09-18T00:00:00.000000Z" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := m
			mutate(&bad)
			if !errors.Is(validateContentPublication(r, receipt, bad), ErrContentPublishBinding) {
				t.Fatal("accepted changed manifest")
			}
		})
	}
	changed := receipt
	changed.SavedBytes++
	if validateContentPublication(r, changed, m) == nil {
		t.Fatal("accepted changed receipt")
	}
}

func TestContentPublishAccountingBoundedAndZero(t *testing.T) {
	for _, v := range []struct{ reserved, stored, allocation, saved, wantReserved, wantStored int64 }{{128, 10, 128, 0, 0, 10}, {256, 10, 128, 20, 128, 30}, {math.MaxInt64 - 10, 10, 128, 128, math.MaxInt64 - 138, 138}} {
		r, s, err := contentPublishedAccounting(v.reserved, v.stored, v.allocation, v.saved)
		if err != nil || r != v.wantReserved || s != v.wantStored {
			t.Fatalf("accounting %v: %d %d %v", v, r, s, err)
		}
	}
	for _, v := range [][4]int64{{-1, 0, 1, 0}, {1, -1, 1, 0}, {1, 0, 0, 0}, {1, 0, 2, 0}, {1, 0, 1, -1}, {1, 0, 1, 2}, {math.MaxInt64, 1, 1, 1}} {
		if _, _, err := contentPublishedAccounting(v[0], v[1], v[2], v[3]); !errors.Is(err, ErrContentPublishAccounting) {
			t.Fatalf("accepted invalid accounting %v", v)
		}
	}
}

func TestContentPublishRejectsFinancialTransactionBeforeFileIO(t *testing.T) {
	r, receipt, m, _ := contentPublishFixture(nil)
	s := &Service{db: syntheticFinancialTransaction{}, options: Options{Enabled: true, ContentEnabled: true, SourceInstance: m.SourceInstance}}
	_, err := s.PublishReservedContent(t.Context(), nil, r, receipt, m, contentcapture.StoreLimits{})
	if !errors.Is(err, ErrContentJournalTransaction) {
		t.Fatal(err)
	}
	s.options.ContentEnabled = false
	_, err = s.PublishReservedContent(t.Context(), nil, r, receipt, m, contentcapture.StoreLimits{})
	if !errors.Is(err, ErrContentJournalDisabled) {
		t.Fatal(err)
	}
}
