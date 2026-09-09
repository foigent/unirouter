// Package sourcefacts owns the activation-forward UR work-fact profiles.
package sourcefacts

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	assets "github.com/uniroute/unirouter/contracts/source-facts/v1"
	"io"
	"slices"
	"strconv"

	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/executioncontext"
)

const (
	Version          = assets.SchemaVersion
	ContextProfile   = "unirouter.execution-context.v1"
	IdentityProfile  = "unirouter.execution-identity.v1"
	CostProfile      = "unirouter.cost-basis.v1"
	Purpose          = "xinsight.source.read"
	ConnectorID      = "unirouter.workfacts"
	ConnectorVersion = "1.0.0"
	MaxDocumentBytes = 64 << 10
	MaxPageBytes     = 8 << 20
)

func Profiles() []string               { return []string{ContextProfile, IdentityProfile, CostProfile} }
func ValidProfile(profile string) bool { return slices.Contains(Profiles(), profile) }

type EconomicReference struct {
	Source               string `json:"source"`
	SourceInstance       string `json:"source_instance"`
	OrgID                string `json:"org_id"`
	ProjectID            string `json:"project_id"`
	RequestID            string `json:"request_id"`
	Revision             string `json:"revision"`
	SnapshotSHA256       string `json:"snapshot_sha256,omitempty"`
	PublicationStatus    string `json:"publication_status"`
	CalculatedAmountPath string `json:"calculated_amount_path,omitempty"`
	ChargedAmountPath    string `json:"charged_amount_path,omitempty"`
	ChargeFactRef        string `json:"charge_fact_ref,omitempty"`
}

type CostDocument struct {
	MediaAmountState   string               `json:"media_amount_state,omitempty"`
	Model              string               `json:"model"`
	TerminalStatus     string               `json:"terminal_status"`
	Reference          EconomicReference    `json:"economic_reference"`
	Basis              *billing.SourceBasis `json:"basis,omitempty"`
	BasisStatus        string               `json:"basis_status"`
	JournalBillingMode string               `json:"journal_billing_mode,omitempty"`
	JournalModeSource  string               `json:"journal_mode_source,omitempty"`
}

type Document struct {
	SchemaVersion    string                     `json:"schema_version"`
	SourceInstance   string                     `json:"source_instance"`
	OrgID            string                     `json:"org_id"`
	ProjectID        string                     `json:"project_id"`
	RequestID        string                     `json:"request_id"`
	Profile          string                     `json:"profile"`
	Stage            string                     `json:"stage"`
	SourceFactID     string                     `json:"source_fact_id"`
	Revision         string                     `json:"revision"`
	EventID          string                     `json:"event_id"`
	PreviousRevision string                     `json:"previous_revision,omitempty"`
	PreviousHash     string                     `json:"previous_hash,omitempty"`
	CorrectionReason string                     `json:"correction_reason,omitempty"`
	OccurredAt       string                     `json:"occurred_at"`
	RecordedAt       string                     `json:"recorded_at"`
	CaptureStatus    string                     `json:"capture_status"`
	Core             *executioncontext.Core     `json:"core,omitempty"`
	Identity         *executioncontext.Identity `json:"identity,omitempty"`
	Cost             *CostDocument              `json:"cost,omitempty"`
}

func sourceID(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(part)))
		h.Write(n[:])
		h.Write([]byte(part))
	}
	return "uf1_" + hex.EncodeToString(h.Sum(nil))
}

func (d Document) factID() string {
	return sourceID(Version, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, d.RequestID, d.Stage)
}
func (d Document) eventID() string { return sourceID(Version+".revision", d.factID(), d.Revision) }

func positiveID(value string) bool {
	n, e := strconv.ParseInt(value, 10, 64)
	return e == nil && n > 0 && strconv.FormatInt(n, 10) == value
}

func (d Document) Validate() error {
	if d.SchemaVersion != Version || !ValidProfile(d.Profile) || !positiveID(d.OrgID) || !positiveID(d.ProjectID) || !positiveID(d.Revision) {
		return errors.New("invalid source fact contract or scope")
	}
	if _, e := eventconnector.NewOpaqueString("source_instance", d.SourceInstance, 1, 128, true); e != nil {
		return e
	}
	if _, e := eventconnector.NewOpaqueString("request_id", d.RequestID, 1, 64, true); e != nil {
		return e
	}
	if !slices.Contains([]string{"authenticated", "admission", "dispatch", "terminal"}, d.Stage) ||
		d.SourceFactID != d.factID() || d.EventID != d.eventID() {
		return errors.New("invalid source fact identity")
	}
	if eventconnector.ValidateCanonicalMicrosecondTime(d.OccurredAt) != nil || eventconnector.ValidateCanonicalMicrosecondTime(d.RecordedAt) != nil {
		return errors.New("invalid source fact time")
	}
	if d.Revision == "1" {
		if d.PreviousHash != "" || d.PreviousRevision != "" || d.CorrectionReason != "" {
			return errors.New("initial fact has predecessor")
		}
	} else {
		previous, e := strconv.ParseInt(d.PreviousRevision, 10, 64)
		revision, _ := strconv.ParseInt(d.Revision, 10, 64)
		if e != nil || !positiveID(d.PreviousRevision) || previous != revision-1 || !validHash(d.PreviousHash) || !sourceText(d.CorrectionReason, 128, true) {
			return errors.New("invalid source revision predecessor")
		}
	}
	if d.CaptureStatus != "available" && d.CaptureStatus != "not_captured" {
		return errors.New("invalid capture status")
	}
	switch d.Profile {
	case ContextProfile:
		if d.Identity != nil || d.Cost != nil || (d.Core == nil) != (d.CaptureStatus == "not_captured") {
			return errors.New("context profile field boundary")
		}
		if d.Core != nil && (d.Core.RequestID != d.RequestID || d.Core.OrgID != d.OrgID || d.Core.ProjectID != d.ProjectID) {
			return errors.New("context scope mismatch")
		}
		if d.Core != nil {
			if e := d.Core.Validate(); e != nil {
				return e
			}
		}
	case IdentityProfile:
		if d.Core != nil || d.Cost != nil || (d.Identity == nil) != (d.CaptureStatus == "not_captured") {
			return errors.New("identity profile field boundary")
		}
		if d.Identity != nil {
			if e := d.Identity.Validate(); e != nil {
				return e
			}
		}
	case CostProfile:
		if d.Core != nil || d.Identity != nil || d.Cost == nil || d.Stage != "terminal" {
			return errors.New("cost profile field boundary")
		}
		if e := d.Cost.Validate(); e != nil {
			return e
		}
		ref := d.Cost.Reference
		if ref.SourceInstance != d.SourceInstance || ref.OrgID != d.OrgID || ref.ProjectID != d.ProjectID || ref.RequestID != d.RequestID {
			return errors.New("economic reference scope mismatch")
		}
		if ref.Source == "completed_execution" {
			if ref.Revision != "1" || !validHash(ref.SnapshotSHA256) || !slices.Contains([]string{"published_by_i01", "not_published_by_i01"}, ref.PublicationStatus) || ref.CalculatedAmountPath != "money.calculated_cost_micros" || ref.ChargedAmountPath != "money.charged_cost_micros" {
				return errors.New("invalid I01 reference")
			}
		} else if ref.Source != "async_media_charge_fact" || ref.Revision != "1" || ref.PublicationStatus != "not_applicable" || ref.SnapshotSHA256 != "" || ref.CalculatedAmountPath != "" || ref.ChargedAmountPath != "" || !sourceText(ref.ChargeFactRef, 96, false) {
			return errors.New("invalid independent economic source")
		}
	}
	return nil
}

func validHash(value string) bool {
	if len(value) != 71 || value[:7] != "sha256:" {
		return false
	}
	b, e := hex.DecodeString(value[7:])
	return e == nil && len(b) == 32 && value[7:] == hex.EncodeToString(b)
}

func Encode(d Document) ([]byte, error) {
	if e := d.Validate(); e != nil {
		return nil, e
	}
	wire, e := json.Marshal(d)
	if e != nil {
		return nil, e
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.UseNumber()
	if e = decoder.Decode(&value); e != nil {
		return nil, e
	}
	encoded, ce := eventconnector.CanonicalJSON(value)
	if ce != nil {
		return nil, ce
	}
	if len(encoded) > MaxDocumentBytes {
		return nil, errors.New("source fact exceeds byte limit")
	}
	return encoded, nil
}

func Decode(wire []byte) (Document, error) {
	if len(wire) > MaxDocumentBytes {
		return Document{}, errors.New("source fact exceeds byte limit")
	}
	if e := eventconnector.ValidateStrictJSON(wire, eventconnector.StrictJSONOptions{RejectNull: true}); e != nil {
		return Document{}, e
	}
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	var d Document
	if e := decoder.Decode(&d); e != nil {
		return d, e
	}
	if e := decoder.Decode(&struct{}{}); !errors.Is(e, io.EOF) {
		return d, errors.New("trailing source data")
	}
	if e := d.Validate(); e != nil {
		return d, e
	}
	canonical, e := Encode(d)
	if e != nil {
		return d, e
	}
	if !bytes.Equal(canonical, wire) {
		return d, fmt.Errorf("source fact is not canonical")
	}
	return d, nil
}
