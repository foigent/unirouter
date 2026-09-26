package main

import "net/http"

type sourceControlHandler interface {
	GetXInsightSourceConnection(http.ResponseWriter, *http.Request)
	SourceControl(string) http.HandlerFunc
}

func registerXInsightSourceControlRoutes(mux *http.ServeMux, h sourceControlHandler, guards xInsightFeedControlRouteGuards) {
	mux.Handle("GET /api/v1/data-exchange/xinsight/source-connections/{project_id}", guards.safety(h.GetXInsightSourceConnection))
	mux.Handle("POST /api/v1/data-exchange/xinsight/source-connections/{project_id}/prepare", guards.expansion(h.SourceControl("prepare")))
	mux.Handle("POST /api/v1/data-exchange/xinsight/source-connections/{project_id}/rotate", guards.expansion(h.SourceControl("rotate")))
	mux.Handle("POST /api/v1/data-exchange/xinsight/source-connections/{project_id}/activate", guards.expansion(h.SourceControl("activate")))
	mux.Handle("POST /api/v1/data-exchange/xinsight/source-connections/{project_id}/resume", guards.expansion(h.SourceControl("resume")))
	mux.Handle("POST /api/v1/data-exchange/xinsight/source-connections/{project_id}/profiles", guards.expansion(h.SourceControl("profiles")))
	mux.Handle("POST /api/v1/data-exchange/xinsight/source-connections/{project_id}/pause", guards.safety(h.SourceControl("pause")))
	mux.Handle("POST /api/v1/data-exchange/xinsight/source-connections/{project_id}/cancel", guards.safety(h.SourceControl("cancel")))
	mux.Handle("POST /api/v1/data-exchange/xinsight/source-connections/{project_id}/revoke", guards.safety(h.SourceControl("revoke")))
}
