package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

type xInsightFeedControlHandler interface {
	GetXInsightFeedConnection(http.ResponseWriter, *http.Request)
	InstallXInsightConnector(http.ResponseWriter, *http.Request)
	PauseXInsightFeedConnection(http.ResponseWriter, *http.Request)
	ResumeXInsightFeedConnection(http.ResponseWriter, *http.Request)
	CreateXInsightFeedCredential(http.ResponseWriter, *http.Request)
	ActivateInitialXInsightFeedCredential(http.ResponseWriter, *http.Request)
	RevokeXInsightFeedCredential(http.ResponseWriter, *http.Request)
	PrepareXInsightFeedRotation(http.ResponseWriter, *http.Request)
	ActivateXInsightFeedRotation(http.ResponseWriter, *http.Request)
	CancelXInsightFeedRotation(http.ResponseWriter, *http.Request)
}

type xInsightFeedControlRouteGuards struct {
	safety    func(http.HandlerFunc) http.Handler
	expansion func(http.HandlerFunc) http.Handler
}

func registerXInsightFeedControlRoutes(
	mux *http.ServeMux,
	handler xInsightFeedControlHandler,
	guards xInsightFeedControlRouteGuards,
) {
	if controls, ok := handler.(interface {
		GetXInsightCommunication(http.ResponseWriter, *http.Request)
		UpdateXInsightCommunication(http.ResponseWriter, *http.Request)
		SyncXInsightCommunicationOnce(http.ResponseWriter, *http.Request)
	}); ok {
		mux.Handle("GET /api/v1/data-exchange/xinsight/connections/{project_id}/communication", guards.safety(controls.GetXInsightCommunication))
		mux.Handle("PUT /api/v1/data-exchange/xinsight/connections/{project_id}/communication", guards.safety(controls.UpdateXInsightCommunication))
		mux.Handle("POST /api/v1/data-exchange/xinsight/connections/{project_id}/communication/sync-once", guards.expansion(controls.SyncXInsightCommunicationOnce))
	}
	mux.Handle("GET /api/v1/data-exchange/xinsight/connections/{project_id}", guards.safety(handler.GetXInsightFeedConnection))
	mux.Handle("POST /api/v1/data-exchange/xinsight/connections/{project_id}/install", guards.expansion(handler.InstallXInsightConnector))
	mux.Handle("POST /api/v1/data-exchange/xinsight/connections/{project_id}/pause", guards.safety(handler.PauseXInsightFeedConnection))
	mux.Handle("POST /api/v1/data-exchange/xinsight/connections/{project_id}/resume", guards.expansion(handler.ResumeXInsightFeedConnection))
	mux.Handle("POST /api/v1/data-exchange/xinsight/connections/{project_id}/credentials", guards.expansion(handler.CreateXInsightFeedCredential))
	mux.Handle("POST /api/v1/data-exchange/xinsight/connections/{project_id}/credentials/activate", guards.expansion(handler.ActivateInitialXInsightFeedCredential))
	mux.Handle("POST /api/v1/data-exchange/xinsight/connections/{project_id}/credentials/{credential_id}/revoke", guards.safety(handler.RevokeXInsightFeedCredential))
	mux.Handle("POST /api/v1/data-exchange/xinsight/connections/{project_id}/rotation/prepare", guards.expansion(handler.PrepareXInsightFeedRotation))
	mux.Handle("POST /api/v1/data-exchange/xinsight/connections/{project_id}/rotation/activate", guards.expansion(handler.ActivateXInsightFeedRotation))
	mux.Handle("POST /api/v1/data-exchange/xinsight/connections/{project_id}/rotation/cancel", guards.safety(handler.CancelXInsightFeedRotation))
}

var _ xInsightFeedControlHandler = (*admin.Handler)(nil)
