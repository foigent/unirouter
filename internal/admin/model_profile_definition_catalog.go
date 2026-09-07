package admin

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

type profileDefinitionCatalogEntry struct {
	modelprofile.Profile
	DefinitionDigest string `json:"profile_definition_digest"`
	Lineage          string `json:"product_lineage_id"`
}

// Publication is the only writer. Discovery exposes exact candidate revisions,
// never a current selector or a claim of qualification/runtime compatibility.
func (h *Handler) listPublishedProfileDefinitions(w http.ResponseWriter, r *http.Request) {
	if _, err := resolveActorOrganization(r.Context(), 0); err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	page, size := 1, 100
	for key, out := range map[string]*int{"page": &page, "page_size": &size} {
		if r.URL.Query().Has(key) {
			value, err := strconv.Atoi(r.URL.Query().Get(key))
			if err != nil || value < 1 || (key == "page_size" && value > 200) || value > 1000000 {
				writeCanonicalApplicationError(w, errCanonicalApplicationInvalid)
				return
			}
			*out = value
		}
	}
	provider, upstream := r.URL.Query().Get("provider"), r.URL.Query().Get("upstream_model")
	if len(provider) > 50 || len(upstream) > 200 || strings.ContainsAny(provider+upstream, "\x00\r\n") {
		writeCanonicalApplicationError(w, errCanonicalApplicationInvalid)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeCanonicalApplicationError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		writeCanonicalApplicationError(w, err)
		return
	}
	expanded, err := schemaauthority.HasCapabilityExpand(r.Context(), tx)
	if err != nil || !expanded {
		writeCanonicalApplicationError(w, fmt.Errorf("published Profile catalogue requires capability expand: %v", err))
		return
	}
	const filter = ` FROM model_profile_definitions WHERE ($1='' OR convert_from(canonical_bytes,'UTF8')::jsonb#>>'{profile,provider}'=$1)
AND ($2='' OR (convert_from(canonical_bytes,'UTF8')::jsonb#>'{profile,upstream_model_ids}') ? $2)`
	var total int
	if err = tx.QueryRow(r.Context(), `SELECT count(*)`+filter, provider, upstream).Scan(&total); err != nil {
		writeCanonicalApplicationError(w, err)
		return
	}
	rows, err := tx.Query(r.Context(), `SELECT definition_digest`+filter+` ORDER BY profile_id,profile_version,definition_digest LIMIT $3 OFFSET $4`, provider, upstream, size, (page-1)*size)
	if err != nil {
		writeCanonicalApplicationError(w, err)
		return
	}
	digests := []string{}
	for rows.Next() {
		var digest string
		if err = rows.Scan(&digest); err != nil {
			break
		}
		digests = append(digests, digest)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		writeCanonicalApplicationError(w, err)
		return
	}
	result := []profileDefinitionCatalogEntry{}
	for _, digest := range digests {
		d, err := modelprofile.ReadDefinition(r.Context(), tx, digest)
		if err != nil {
			writeCanonicalApplicationError(w, err)
			return
		}
		result = append(result, profileDefinitionCatalogEntry{d.Profile(), d.Digest(), d.ProductLineageID()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"contract_version": "unirouter.model-execution-profiles.v1", "data": result, "page": page, "page_size": size, "total": total})
}

// The existing Profile catalogue exposes published revisions for an exact
// customer-owned stable product. It has no upload, mutation or activation path.
func (h *Handler) listProductProfileDefinitions(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("public_model_id"), 10, 64)
	if err != nil || id <= 0 {
		writeCanonicalApplicationError(w, errCanonicalApplicationInvalid)
		return
	}
	org, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeCanonicalApplicationError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		writeCanonicalApplicationError(w, err)
		return
	}
	var profileID, version, lineage string
	err = tx.QueryRow(r.Context(), `SELECT execution_profile_id,execution_profile_version,COALESCE(to_jsonb(model_registry)->>'product_lineage_id','') FROM model_registry WHERE id=$1 AND org_id=$2`, id, org).Scan(&profileID, &version, &lineage)
	if err == pgx.ErrNoRows {
		writeCanonicalApplicationError(w, errCanonicalApplicationNotFound)
		return
	}
	if err != nil {
		writeCanonicalApplicationError(w, err)
		return
	}
	legacy, err := modelprofile.ResolveAuthoringBootstrap(r.Context(), tx, profileID, version, lineage)
	if err != nil {
		writeCanonicalApplicationError(w, fmt.Errorf("stable product bootstrap Profile is unavailable"))
		return
	}
	result := []profileDefinitionCatalogEntry{}
	expanded, err := schemaauthority.HasCapabilityExpand(r.Context(), tx)
	if err != nil {
		writeCanonicalApplicationError(w, err)
		return
	}
	if expanded {
		rows, err := tx.Query(r.Context(), `SELECT definition_digest FROM model_profile_definitions WHERE $1='' OR product_lineage_id=$1 ORDER BY profile_id,profile_version,definition_digest`, lineage)
		if err != nil {
			writeCanonicalApplicationError(w, err)
			return
		}
		digests := []string{}
		for rows.Next() {
			var d string
			if err = rows.Scan(&d); err != nil {
				break
			}
			digests = append(digests, d)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			writeCanonicalApplicationError(w, err)
			return
		}
		for _, digest := range digests {
			d, err := modelprofile.ReadDefinition(r.Context(), tx, digest)
			if err != nil {
				writeCanonicalApplicationError(w, err)
				return
			}
			if modelprofile.SameStableProductSurface(legacy, d.Profile()) {
				result = append(result, profileDefinitionCatalogEntry{d.Profile(), d.Digest(), d.ProductLineageID()})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"contract_version": "unirouter.model-execution-profiles.v1", "data": result})
}
