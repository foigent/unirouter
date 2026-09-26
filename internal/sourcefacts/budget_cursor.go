package sourcefacts

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

type budgetAccess struct {
	access
	dGeneration, catalogRevision int64
}

// D cursors deliberately do not use A/B stream epochs or connection generation.
// Credential status and global pause/revoke are checked before cursor decoding.
type budgetCursor struct {
	Version            string `json:"version"`
	Kind               string `json:"kind"`
	Instance           string `json:"instance"`
	Org                int64  `json:"org"`
	Project            int64  `json:"project"`
	Key                int64  `json:"key"`
	AccessGeneration   int64  `json:"access_generation"`
	CatalogRevision    int64  `json:"catalog_revision"`
	AfterRef           string `json:"after_ref,omitempty"`
	BudgetRef          string `json:"budget_ref,omitempty"`
	DefinitionRevision int64  `json:"definition_revision,omitempty"`
	BindingRevision    int64  `json:"binding_revision,omitempty"`
	AfterAlertID       int64  `json:"after_alert_id,omitempty"`
}

func (s *Service) encodeBudgetCursor(c budgetCursor) (string, error) {
	if c.Version != BudgetSnapshotVersion || !oneOf(c.Kind, "catalog", "alerts") || c.Instance != s.options.SourceInstance || c.Org <= 0 || c.Project <= 0 || c.Key <= 0 || c.AccessGeneration <= 0 || c.CatalogRevision <= 0 {
		return "", errors.New("invalid budget cursor scope")
	}
	if c.Kind == "catalog" {
		if !opaque(c.AfterRef, 256) || c.BudgetRef != "" || c.DefinitionRevision != 0 || c.BindingRevision != 0 || c.AfterAlertID != 0 {
			return "", errors.New("invalid budget catalog cursor")
		}
	} else if !opaque(c.BudgetRef, 256) || c.DefinitionRevision <= 0 || c.BindingRevision < 0 || c.AfterAlertID <= 0 || c.AfterRef != "" {
		return "", errors.New("invalid budget alert cursor")
	}
	wire, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(s.options.CursorSecret))
	mac.Write([]byte("unirouter.source-budget-cursor.v1\x00" + c.Kind + "\x00"))
	mac.Write(wire)
	token := base64.RawURLEncoding.EncodeToString(wire) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if len(token) > 4096 {
		return "", errors.New("budget cursor exceeds limit")
	}
	return token, nil
}

func (s *Service) decodeBudgetCursor(token, kind string, scope budgetAccess) (budgetCursor, error) {
	var c budgetCursor
	invalid := errors.New("invalid or stale budget cursor")
	if len(token) == 0 || len(token) > 4096 || !oneOf(kind, "catalog", "alerts") {
		return c, invalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return c, invalid
	}
	wire, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil {
		return c, invalid
	}
	signature, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return c, invalid
	}
	mac := hmac.New(sha256.New, []byte(s.options.CursorSecret))
	mac.Write([]byte("unirouter.source-budget-cursor.v1\x00" + kind + "\x00"))
	mac.Write(wire)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return c, invalid
	}
	if eventconnector.ValidateStrictJSON(wire, eventconnector.StrictJSONOptions{RejectNull: true}) != nil {
		return c, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil {
		return c, invalid
	}
	reencoded, e := s.encodeBudgetCursor(c)
	if e != nil || reencoded != token || c.Kind != kind || c.Org != scope.org || c.Project != scope.project || c.Key != scope.keyID || c.Instance != scope.instance || c.AccessGeneration != scope.dGeneration || c.CatalogRevision != scope.catalogRevision {
		return c, invalid
	}
	return c, nil
}
