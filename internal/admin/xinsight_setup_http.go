package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

type xInsightSetupRuntime struct {
	peer   config.XInsightConnectorStaticPeer
	client *http.Client
	status func(context.Context, string, string) ([]byte, error)
}

// SetXInsightSetupPeer receives only deployment-owned static configuration.
// It has no dynamic registration and never accepts an origin from a browser.
func (h *Handler) SetXInsightSetupPeer(peer *config.XInsightConnectorStaticPeer, client *http.Client, status func(context.Context, string, string) ([]byte, error)) error {
	if peer == nil {
		h.xInsightSetup = nil
		return nil
	}
	if err := peer.Validate(); err != nil {
		return err
	}
	if client == nil {
		client = &http.Client{}
	}
	c := *client
	c.Jar = nil
	c.Timeout = 10 * time.Second
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	h.xInsightSetup = &xInsightSetupRuntime{peer: *peer, client: &c, status: status}
	return nil
}

func decodeXInsightSetupJSON(w http.ResponseWriter, r *http.Request, out any) error {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32768))
	if err != nil {
		return xinsightconnector.ErrInvalid
	}
	return decodeXInsightSetupWire(raw, out)
}
func decodeXInsightSetupWire(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > 32768 || eventconnector.ValidateStrictJSON(raw, eventconnector.StrictJSONOptions{}) != nil {
		return xinsightconnector.ErrInvalid
	}
	if !exactSetupFields(raw, reflect.TypeOf(out).Elem()) {
		return xinsightconnector.ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || !errors.Is(d.Decode(new(any)), io.EOF) {
		return xinsightconnector.ErrInvalid
	}
	return nil
}

// encoding/json otherwise accepts case-insensitive aliases. Closed setup DTOs
// preserve exact field names at every nested object without weakening the
// original contracts. Nullable pointer fields are explicitly allowed.
func exactSetupFields(raw []byte, t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		if bytes.Equal(raw, []byte("null")) {
			return true
		}
		return exactSetupFields(raw, t.Elem())
	}
	if t == reflect.TypeOf(time.Time{}) {
		return true
	}
	switch t.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return false
		}
		allowed := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				allowed[name] = f.Type
			}
		}
		for name, value := range fields {
			ft, ok := allowed[name]
			if !ok || !exactSetupFields(value, ft) {
				return false
			}
		}
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return true
		}
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) != nil {
			return false
		}
		for _, value := range values {
			if !exactSetupFields(value, t.Elem()) {
				return false
			}
		}
	default:
		if bytes.Equal(raw, []byte("null")) {
			return false
		}
	}
	return true
}
func setupRequirement(org, project int64) committingActorRequirement {
	return committingActorRequirement{TargetOrgID: org, AllowedOrganizationRoles: []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin}, Project: &committingActorProjectRequirement{ProjectID: project, AllowedRoles: []string{projectMemberAdmin}, MustBeActive: true}}
}
func (h *Handler) GetXInsightSetupPeer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	org, ok := parsePositivePathID(w, r, "org_id", "Invalid organization")
	if !ok {
		return
	}
	project, e := strconv.ParseInt(r.URL.Query().Get("project_id"), 10, 64)
	if e != nil || project <= 0 {
		writeJSON(w, 400, map[string]any{"error": "project_id required"})
		return
	}
	tx, e := h.db.Begin(r.Context())
	if e != nil {
		writeMachineDomainError(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	if _, e = h.reauthorizeCommittingActorTx(r.Context(), tx, setupRequirement(org, project)); e != nil {
		writeJSON(w, 403, map[string]any{"error": "setup_denied"})
		return
	}
	view := xInsightSetupPeerView{SchemaVersion: "unirouter.xinsight.setup-peer.v1", OrgID: strconv.FormatInt(org, 10), AnchorProjectID: strconv.FormatInt(project, 10)}
	if h.sourceFactsController != nil {
		view.SourceInstance = h.sourceFactsController.SourceInstance()
	}
	if h.xInsightSetup != nil {
		p := h.xInsightSetup.peer
		view.XIOrigin = p.XInsightOrigin
		view.XIDeploymentID = p.XDeploymentID
		view.XISetupURL = p.XInsightOrigin + "/connectors/unirouter/setup"
		view.TrustReady = true
	}
	if e = tx.Commit(r.Context()); e != nil {
		writeMachineDomainError(w, e)
		return
	}
	writeJSON(w, 200, view)
}

func (h *Handler) resolveXInsightSetupConsent(ctx context.Context, inv xinsightconnector.FrozenInvitationV1) (xInsightSetupXIConsent, error) {
	var zero xInsightSetupXIConsent
	if h.xInsightSetup == nil {
		return zero, xinsightconnector.ErrUnavailable
	}
	token, e := xinsightconnector.ValidateSetupInvitation(inv, h.xInsightSetup.peer.XInsightOrigin, time.Now().UTC())
	if e != nil {
		return zero, e
	}
	resolved, e := h.resolveXInsightSetupInvitation(ctx, token)
	if e != nil {
		return zero, e
	}
	if xinsightconnector.PublicCanonicalSHA256(inv) != xinsightconnector.PublicCanonicalSHA256(resolved.Invitation) {
		return zero, xinsightconnector.ErrConflict
	}
	return resolved.Consent, nil
}
func (h *Handler) resolveXInsightSetupInvitation(ctx context.Context, token string) (xInsightSetupResolved, error) {
	var out xInsightSetupResolved
	if h.xInsightSetup == nil {
		return out, xinsightconnector.ErrUnavailable
	}
	if !regexp.MustCompile(`^xiiv1_[A-Za-z0-9_-]{43}$`).MatchString(token) {
		return out, xinsightconnector.ErrInvalid
	}
	p := h.xInsightSetup.peer
	raw, _ := json.Marshal(struct {
		Token string `json:"invitation_token"`
	}{token})
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, p.XInsightOrigin+"/api/v1/connector-setups/resolve", bytes.NewReader(raw))
	if e != nil {
		return out, e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, e := h.xInsightSetup.client.Do(req)
	if e != nil {
		return out, xinsightconnector.ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return out, xinsightconnector.ErrConflict
	}
	raw, e = io.ReadAll(io.LimitReader(res.Body, 32769))
	if e != nil || decodeXInsightSetupWire(raw, &out) != nil {
		return out, xinsightconnector.ErrInvalid
	}
	inv := out.Invitation
	consent := out.Consent
	actual, e := xinsightconnector.ValidateSetupInvitation(inv, p.XInsightOrigin, time.Now().UTC())
	if e != nil || actual != token || inv.DeploymentID != p.XDeploymentID {
		return out, xinsightconnector.ErrConflict
	}
	install, e := url.Parse(inv.InstallURL)
	if e != nil || install.Scheme+"://"+install.Host != p.UROrigin {
		return out, xinsightconnector.ErrConflict
	}
	expires, e := time.Parse(time.RFC3339Nano, inv.ExpiresAt)
	sha := consent.ConsentSHA256
	consent.ConsentSHA256 = ""
	if e != nil || out.Consent.SchemaVersion != "xinsight.connector-setup-consent-view.v1" || out.Consent.InstallationID != inv.InstallationID || out.Consent.DeploymentID != p.XDeploymentID || out.Consent.XIOrigin != p.XInsightOrigin || out.Consent.UROrigin != p.UROrigin || !out.Consent.ExpiresAt.Equal(expires) || out.Consent.ConsentRevision != 1 || sha != xinsightconnector.PublicCanonicalSHA256(consent) || out.Consent.ExpectedCustodyRevision < 0 || !validSetupCustody(out.Consent.Custody) {
		return out, xinsightconnector.ErrConflict
	}
	return out, nil
}

func validSetupCustody(c xInsightSetupCustody) bool {
	return (c.Mode == "disabled" || c.Mode == "full_content") && c.RetentionSeconds >= 1 && c.RetentionSeconds <= 31536000 && c.ProjectQuotaBytes >= 1 && c.ProjectQuotaBytes <= 1<<40 && c.MaxItemBytes >= 0 && c.MaxItemBytes <= 32<<20
}

func (h *Handler) setupPreviewTx(ctx context.Context, tx pgx.Tx, org, project int64, actor committingActorState, inv xinsightconnector.FrozenInvitationV1, xi xInsightSetupXIConsent) (xInsightSetupPreview, error) {
	out := xInsightSetupPreview{SchemaVersion: "unirouter.xinsight.setup-preview.v1", Invitation: inv, InstallationID: inv.InstallationID, XIConsent: xi, AvailableProfiles: []string{}, DefaultProfiles: []string{}, ExistingProfiles: []string{}, ReceiverMode: xinsightconnector.ReceiverModeNewInstall, AllowedCoverage: []string{"exact"}, AllowedPolicyScopes: []string{"project"}, AllowedPolicyAccess: []xInsightSetupPolicyAccess{}, Issues: []xInsightSetupIssue{}}
	if h.sourceFactsController == nil || !h.sourceFactsController.Enabled() {
		return out, xinsightconnector.ErrUnavailable
	}
	out.AvailableProfiles = sourcefacts.EnabledProfiles(h.sourceFactsController)
	out.DefaultProfiles = sourcefacts.DefaultEnrollmentProfiles(out.AvailableProfiles)
	if c, ok := h.sourceFactsController.(interface{ ContentEnabled() bool }); ok {
		out.ContentAvailable = c.ContentEnabled()
	}
	if auth.RoleIn(actor.OrganizationRole, auth.RolePlatformAdmin, auth.RoleOrgAdmin) {
		out.AllowedCoverage = append(out.AllowedCoverage, "all_current_future")
		out.AllowedPolicyScopes = append(out.AllowedPolicyScopes, "enterprise")
		for _, kind := range []string{"enterprise_default", "project_policy", "user_exception"} {
			out.AllowedPolicyAccess = append(out.AllowedPolicyAccess, xInsightSetupPolicyAccess{TargetKind: kind, Coverage: "exact", Read: true, Write: true})
			if kind != "enterprise_default" {
				out.AllowedPolicyAccess = append(out.AllowedPolicyAccess, xInsightSetupPolicyAccess{TargetKind: kind, Coverage: "all_current_future", Read: true, Write: true})
			}
		}
	} else {
		out.Issues = append(out.Issues, xInsightSetupIssue{"machine_registration_requires_org_admin", true})
	}
	controller, ok := h.contentPolicyController.(scopedContentPolicyController)
	if !ok || h.contentPolicyReadiness == nil {
		return out, xinsightconnector.ErrUnavailable
	}
	app, e := h.contentPolicyReadiness(ctx)
	if e != nil {
		return out, e
	}
	out.URPolicy, e = controller.ScopedManagementTx(ctx, tx, org, project, 0, app.StorageStatus)
	if e != nil {
		return out, e
	}
	// Original feed/key identity and progress are included in the preview CAS.
	var status string
	var active, staged int
	e = tx.QueryRow(ctx, `SELECT COALESCE((SELECT status FROM completed_execution_feed_connections WHERE org_id=$1 AND project_id=$2),'not_configured'),count(*) FILTER(WHERE status='feed_active'),count(*) FILTER(WHERE status='feed_staged') FROM api_keys WHERE org_id=$1 AND project_id=$2 AND purpose='xinsight.feed.read'`, org, project).Scan(&status, &active, &staged)
	if e != nil {
		return out, e
	}
	if status == "active" && active == 1 && staged == 0 {
		out.ReceiverMode = xinsightconnector.ReceiverModeAdoptExisting
		var keyID int64
		var prefix, scheme, operation, ip string
		e = tx.QueryRow(ctx, `SELECT id,key_prefix,verifier_scheme,registration_operation_id,allowed_ips FROM api_keys WHERE org_id=$1 AND project_id=$2 AND purpose='xinsight.feed.read' AND status='feed_active' FOR SHARE`, org, project).Scan(&keyID, &prefix, &scheme, &operation, &ip)
		if e != nil {
			return out, e
		}
		b := struct {
			KeyID                                 int64
			Prefix, Scheme, Operation, IP, Source string
		}{keyID, prefix, scheme, operation, ip, h.sourceFactsController.SourceInstance()}
		sha := xinsightconnector.PublicCanonicalSHA256(b)
		out.ExistingBindingSHA256 = &sha
		if scheme != auth.ReceiverFeedVerifierScheme || ip != h.xInsightSetup.peer.AllowedIP {
			out.Issues = append(out.Issues, xInsightSetupIssue{"existing_receiver_incompatible", true})
		}
	} else if active != 0 || staged != 0 {
		out.Issues = append(out.Issues, xInsightSetupIssue{"existing_receiver_conflict", true})
	}
	// Original source identities are reviewed before a new grant exists. The
	// aggregate existing-binding digest binds their exact non-secret snapshots.
	states, sourceErr := h.setupExistingSourceStatesTx(ctx, tx, org, project, inv)
	if sourceErr != nil {
		out.Issues = append(out.Issues, xInsightSetupIssue{"existing_source_requires_review", true})
	} else if len(states) > 0 {
		sha := xinsightconnector.PublicCanonicalSHA256(struct {
			Feed    *string                 `json:"feed"`
			Sources []bindExistingCandidate `json:"sources"`
		}{out.ExistingBindingSHA256, states})
		out.ExistingBindingSHA256 = &sha
		out.ExistingProfiles = append([]string{}, states[0].ConnectionProfiles...)
		out.DefaultProfiles = append([]string{}, states[0].ConnectionProfiles...)
	}
	// A previously enrolled scope must be reviewed, never duplicated silently.
	hasEnrollment := false
	rows, e := tx.Query(ctx, `SELECT profiles FROM xinsight_source_enrollments WHERE org_id=$1 AND state='active' AND (exact_project_id=$2 OR coverage='all_current_future') ORDER BY id`, org, project)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var profiles []string
		if e = rows.Scan(&profiles); e != nil {
			break
		}
		hasEnrollment = true
		out.ExistingProfiles = append(out.ExistingProfiles, profiles...)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return out, e
	}
	if hasEnrollment {
		out.Issues = append(out.Issues, xInsightSetupIssue{"existing_enrollment_requires_review", true})
	}
	out.PreviewSHA256 = xinsightconnector.PublicCanonicalSHA256(out)
	return out, nil
}
func (h *Handler) PreviewXInsightSetup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	org, ok := parsePositivePathID(w, r, "org_id", "Invalid organization")
	if !ok {
		return
	}
	var in struct {
		ProjectID       string `json:"project_id"`
		InvitationToken string `json:"invitation_token"`
	}
	if decodeXInsightSetupJSON(w, r, &in) != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid_setup_preview"})
		return
	}
	project, e := strconv.ParseInt(in.ProjectID, 10, 64)
	if e != nil || project <= 0 {
		writeJSON(w, 400, map[string]any{"error": "invalid_project"})
		return
	}
	// Authenticate before contacting the one fixed peer; no locks span HTTP.
	tx, e := h.db.Begin(r.Context())
	if e != nil {
		writeMachineDomainError(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	_, e = h.reauthorizeCommittingActorTx(r.Context(), tx, setupRequirement(org, project))
	if e != nil {
		writeJSON(w, 403, map[string]any{"error": "setup_denied"})
		return
	}
	if tx.Commit(r.Context()) != nil {
		writeJSON(w, 503, map[string]any{"error": "setup_unavailable"})
		return
	}
	resolved, e := h.resolveXInsightSetupInvitation(r.Context(), in.InvitationToken)
	if e != nil {
		writeSetupError(w, e)
		return
	}
	tx, e = h.db.Begin(r.Context())
	if e != nil {
		writeMachineDomainError(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	actor, e := h.reauthorizeCommittingActorTx(r.Context(), tx, setupRequirement(org, project))
	if e != nil {
		writeJSON(w, 403, map[string]any{"error": "setup_denied"})
		return
	}
	out, e := h.setupPreviewTx(r.Context(), tx, org, project, actor, resolved.Invitation, resolved.Consent)
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		writeSetupError(w, e)
		return
	}
	writeJSON(w, 200, out)
}
func writeSetupError(w http.ResponseWriter, e error) {
	code := 503
	message := "setup_unavailable"
	if errors.Is(e, xinsightconnector.ErrInvalid) {
		code = 400
		message = "invalid_setup"
	}
	if errors.Is(e, xinsightconnector.ErrConflict) || errors.Is(e, contentcapture.ErrPolicyVersionConflict) {
		code = 409
		message = "setup_changed"
	}
	writeJSON(w, code, map[string]any{"error": message})
}

// Existing connection readback uses the same original candidate reader as the
// supported bind-existing API. It carries no token and creates no grant.
func (h *Handler) setupExistingSourceStatesTx(ctx context.Context, tx pgx.Tx, org, project int64, inv xinsightconnector.FrozenInvitationV1) ([]bindExistingCandidate, error) {
	out := []bindExistingCandidate{}
	var exists bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution_source_connections WHERE org_id=$1 AND project_id=$2)`, org, project).Scan(&exists); e != nil {
		return out, e
	}
	if !exists {
		return out, nil
	}
	kinds := []string{"facts"}
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution_source_content_scopes WHERE org_id=$1 AND project_id=$2)`, org, project).Scan(&exists); e != nil {
		return out, e
	}
	if exists {
		kinds = append(kinds, "content")
	}
	for _, kind := range kinds {
		c, e := readBindExistingCandidateTx(ctx, tx, bindExistingCandidate{SchemaVersion: "unirouter.xinsight.bind-existing-candidate.v1", InstallationID: inv.InstallationID, XDeploymentID: inv.DeploymentID, SourceInstance: h.sourceFactsController.SourceInstance(), OrgID: strconv.FormatInt(org, 10), ProjectID: strconv.FormatInt(project, 10), Kind: kind})
		if e != nil {
			return out, e
		}
		if len(c.Keys) != 1 || c.Keys[0].Status != "feed_active" || c.Keys[0].PolicyLabels != "" || c.Keys[0].VerifierScheme != auth.ReceiverFeedVerifierScheme || c.Keys[0].AllowedIP != h.xInsightSetup.peer.AllowedIP || !h.sourceFactsController.ProfilesEnabled(c.ConnectionProfiles) {
			return out, xinsightconnector.ErrConflict
		}
		out = append(out, c)
	}
	return out, nil
}
