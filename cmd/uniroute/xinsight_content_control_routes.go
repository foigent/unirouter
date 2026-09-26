package main

import "net/http"

type contentControlHandler interface {
	GetXInsightContentConnection(http.ResponseWriter, *http.Request)
	ContentControl(string) http.HandlerFunc
}

func registerXInsightContentControlRoutes(mux *http.ServeMux, h contentControlHandler, guards xInsightFeedControlRouteGuards) {
	const base = "/api/v1/data-exchange/xinsight/source-connections/{project_id}/content"
	mux.Handle("GET "+base, guards.safety(h.GetXInsightContentConnection))
	for _, action := range []string{"prepare", "rotate", "activate", "resume"} {
		mux.Handle("POST "+base+"/"+action, guards.expansion(h.ContentControl(action)))
	}
	for _, action := range []string{"pause", "cancel", "revoke"} {
		mux.Handle("POST "+base+"/"+action, guards.safety(h.ContentControl(action)))
	}
}
