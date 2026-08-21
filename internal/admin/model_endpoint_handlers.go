package admin

import "net/http"

func (h *Handler) ActivateModelEndpoint(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
}

func (h *Handler) StartModelEndpointCanary(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
}

func (h *Handler) RecoverModelEndpoint(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
}
