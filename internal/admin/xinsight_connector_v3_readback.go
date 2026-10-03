package admin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

type deliveredMachineCandidate struct {
	ControlOperationID   string                                `json:"control_operation_id"`
	ControlRequestSHA256 string                                `json:"control_request_sha256"`
	ControlResultSHA256  string                                `json:"control_result_sha256"`
	Purpose              string                                `json:"purpose"`
	Registration         xinsightconnector.MachineRegistration `json:"registration"`
	RegistrationSHA256   string                                `json:"registration_sha256"`
	CustodyReceiptSHA256 string                                `json:"custody_receipt_sha256"`
	Delivery             string                                `json:"delivery"`
	Activation           string                                `json:"activation"`
	ObservedAt           string                                `json:"observed_at"`
}
type receiverAdoptionSummary struct {
	BindOperationID         string `json:"bind_operation_id"`
	CandidateSHA256         string `json:"candidate_sha256"`
	AdoptionBindingSHA256   string `json:"adoption_binding_sha256"`
	FeedKeyID               string `json:"feed_key_id"`
	Status                  string `json:"status"`
	ConfirmOperationID      string `json:"confirm_operation_id,omitempty"`
	ConfirmResultSHA256     string `json:"confirm_result_sha256,omitempty"`
	XIAdoptionReceiptSHA256 string `json:"xi_adoption_receipt_sha256,omitempty"`
	FeedReadReceiptSHA256   string `json:"feed_read_receipt_sha256,omitempty"`
}

func (h *Handler) readConnectorV3PublicDetails(ctx context.Context, i *xInsightConnectorInstallationSummary, org, project int64) error {
	i.DeliveredMachineCandidates = []deliveredMachineCandidate{}
	role := actorOrganizationRole(ctx)
	if role != auth.RoleOrgAdmin && role != auth.RolePlatformAdmin {
		return nil
	}
	candidates, err := readConnectorV3MachineCandidates(ctx, h.db, i, org, project)
	if err != nil {
		return err
	}
	i.DeliveredMachineCandidates = candidates

	var adoptionSchema bool
	if h.db.QueryRow(ctx, `SELECT to_regclass('public.xinsight_receiver_adoptions') IS NOT NULL`).Scan(&adoptionSchema) != nil {
		return xinsightconnector.ErrUnavailable
	}
	if !adoptionSchema {
		return nil
	}
	var a receiverAdoptionSummary
	var key int64
	var confirm, confirmSHA, xi, read sql.NullString
	var bindRequest, bindResult []byte
	var bindReqSHA, bindResSHA string
	err = h.db.QueryRow(ctx, `SELECT a.bind_operation_id,a.candidate_sha256,a.adoption_binding_sha256,a.feed_key_id,a.status,a.confirm_operation_id,a.confirm_result_sha256,a.xi_adoption_receipt_sha256,a.feed_read_receipt_sha256,b.request_canonical,b.result_canonical,b.request_sha256,b.result_sha256 FROM xinsight_receiver_adoptions a JOIN xinsight_connector_phase_operations b ON b.operation_id=a.bind_operation_id AND b.installation_id=a.installation_id WHERE a.installation_id=$1 AND a.org_id=$2 AND a.project_id=$3`, i.InstallationID, org, project).Scan(&a.BindOperationID, &a.CandidateSHA256, &a.AdoptionBindingSHA256, &key, &a.Status, &confirm, &confirmSHA, &xi, &read, &bindRequest, &bindResult, &bindReqSHA, &bindResSHA)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	op, e := xinsightconnector.DecodeControlOperationV3(bindRequest)
	if e != nil {
		return e
	}
	r, e := xinsightconnector.DecodeControlResultV3(bindResult)
	if e != nil || xinsightconnector.ValidateControlResultForOperationV3(op, r) != nil || op.Phase != xinsightconnector.ControlPhaseBindExisting || r.Status != "completed" || bindReqSHA != xinsightconnector.PublicCanonicalSHA256(op) || bindResSHA != xinsightconnector.PublicCanonicalSHA256(r) || bindReqSHA != publicV3BytesSHA(bindRequest) || bindResSHA != publicV3BytesSHA(bindResult) {
		return xinsightconnector.ErrConflict
	}
	b := r.Result.(xinsightconnector.BindExistingReceiverResult)
	a.FeedKeyID = strconv.FormatInt(key, 10)
	if b.AdoptionBinding.OrgID != org || b.AdoptionBinding.ProjectID != project || b.AdoptionBinding.InstallationID != i.InstallationID || b.CandidateSHA256 != a.CandidateSHA256 || b.AdoptionBindingSHA256 != a.AdoptionBindingSHA256 || b.AdoptionBinding.FeedKeyID != a.FeedKeyID {
		return xinsightconnector.ErrConflict
	}
	if a.Status == "confirmed" {
		a.ConfirmOperationID = confirm.String
		a.ConfirmResultSHA256 = confirmSHA.String
		a.XIAdoptionReceiptSHA256 = xi.String
		a.FeedReadReceiptSHA256 = read.String
		var req, res []byte
		var reqSHA, resSHA string
		err = h.db.QueryRow(ctx, `SELECT request_canonical,result_canonical,request_sha256,result_sha256 FROM xinsight_connector_phase_operations WHERE operation_id=$1 AND installation_id=$2 AND org_id=$3 AND project_id=$4`, a.ConfirmOperationID, i.InstallationID, org, project).Scan(&req, &res, &reqSHA, &resSHA)
		if err != nil {
			return err
		}
		co, e := xinsightconnector.DecodeControlOperationV3(req)
		if e != nil {
			return e
		}
		cr, e := xinsightconnector.DecodeControlResultV3(res)
		if e != nil || xinsightconnector.ValidateControlResultForOperationV3(co, cr) != nil || co.Phase != xinsightconnector.ControlPhaseConfirmExisting || cr.Status != "completed" || reqSHA != xinsightconnector.PublicCanonicalSHA256(co) || resSHA != xinsightconnector.PublicCanonicalSHA256(cr) || reqSHA != publicV3BytesSHA(req) || resSHA != publicV3BytesSHA(res) || resSHA != a.ConfirmResultSHA256 || co.PredecessorOperationID != a.BindOperationID || co.PredecessorResultSHA256 != bindResSHA {
			return xinsightconnector.ErrConflict
		}
		cb := cr.Result.(xinsightconnector.ConfirmExistingReceiverResult)
		if cb.AdoptionBindingSHA256 != a.AdoptionBindingSHA256 || cb.XIAdoptionReceiptSHA256 != a.XIAdoptionReceiptSHA256 || cb.FeedReadReceiptSHA256 != a.FeedReadReceiptSHA256 {
			return xinsightconnector.ErrConflict
		}
	}
	i.ReceiverAdoption = &a
	return nil
}

func publicV3BytesSHA(raw []byte) string {
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

type connectorCandidateReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func readConnectorV3MachineCandidates(ctx context.Context, reader connectorCandidateReader, i *xInsightConnectorInstallationSummary, org, project int64) ([]deliveredMachineCandidate, error) {
	candidates := []deliveredMachineCandidate{}

	rows, err := reader.Query(ctx, `SELECT request_sha256,request_canonical,result_sha256,result_canonical FROM xinsight_connector_phase_operations WHERE installation_id=$1 AND org_id=$2 AND project_id=$3 AND control_fence=$4 AND result_canonical IS NOT NULL ORDER BY convert_from(request_canonical,'UTF8')::jsonb#>>'{request,registration,purpose}',operation_id`, i.InstallationID, org, project, i.ControlFence)
	if err != nil {
		return nil, err
	}
	// Only typed, validated public projection escapes the journal. Never return
	// encrypted bindings, arbitrary journal JSON, or control/receiver secrets.
	for rows.Next() {
		var requestSHA, resultSHA string
		var request, result []byte
		if err = rows.Scan(&requestSHA, &request, &resultSHA, &result); err != nil {
			rows.Close()
			return nil, err
		}
		version, e := xinsightconnector.ControlSchemaVersion(request)
		if e != nil {
			rows.Close()
			return nil, e
		}
		if version == xinsightconnector.ControlOperationV2Schema {
			continue
		}
		if version != xinsightconnector.ControlOperationV3Schema {
			rows.Close()
			return nil, xinsightconnector.ErrInvalid
		}
		op, e := xinsightconnector.DecodeControlOperationV3(request)
		if e != nil {
			rows.Close()
			return nil, e
		}
		r, e := xinsightconnector.DecodeControlResultV3(result)
		if e != nil {
			rows.Close()
			return nil, e
		}
		reqCanonical, e := op.CanonicalJSON()
		if e != nil {
			rows.Close()
			return nil, e
		}
		resCanonical, e := r.CanonicalJSON()
		if e != nil {
			rows.Close()
			return nil, e
		}
		if !bytes.Equal(request, reqCanonical) || !bytes.Equal(result, resCanonical) || requestSHA != xinsightconnector.PublicCanonicalSHA256(op) || resultSHA != xinsightconnector.PublicCanonicalSHA256(r) || op.InstallationID != i.InstallationID || xinsightconnector.ValidateControlResultForOperationV3(op, r) != nil {
			rows.Close()
			return nil, xinsightconnector.ErrConflict
		}
		if op.Kind != xinsightconnector.ControlKindPairMachine || r.Status != "completed" {
			continue
		}
		q := op.Request.(xinsightconnector.DeliverMachineRequest)
		body := r.Result.(xinsightconnector.DeliverMachineResult)
		if q.SourceInstance != i.SourceInstance || q.SourceBindingSHA256 != i.SourceBindingSHA256 {
			rows.Close()
			return nil, xinsightconnector.ErrConflict
		}
		candidates = append(candidates, deliveredMachineCandidate{ControlOperationID: op.OperationID, ControlRequestSHA256: requestSHA, ControlResultSHA256: resultSHA, Purpose: q.Registration.Purpose, Registration: q.Registration, RegistrationSHA256: q.RegistrationSHA256, CustodyReceiptSHA256: q.CustodyReceiptSHA256, Delivery: body.Readiness, Activation: body.Activation, ObservedAt: r.ObservedAt})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return candidates, nil
}
