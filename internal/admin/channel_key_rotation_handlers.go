package admin

import "net/http"

func (h *Handler) RotateChannelKey(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
}
