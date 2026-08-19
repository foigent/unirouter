package admin

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/modelgov"
	"github.com/uniroute/unirouter/internal/modellist"
)

var (
	errInvalidPublicModelID = errors.New("invalid public model id")
	publicModelIDPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,159}$`)
)

type modelIdentityStore interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

func validatePublicModelIdentities(ctx context.Context, store modelIdentityStore, orgID int64, models string) error {
	for _, model := range modellist.Parse(models) {
		if !publicModelIDPattern.MatchString(model) {
			return errInvalidPublicModelID
		}
		if _, err := modelgov.ActivatedIdentity(ctx, store, orgID, model); err != nil {
			return err
		}
	}
	return nil
}

func writePublicModelIdentityError(w http.ResponseWriter, requested string, err error) {
	switch {
	case errors.Is(err, errInvalidPublicModelID):
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":           "Invalid public model ID",
			"code":            "invalid_public_model_id",
			"message":         "公共模型 ID 只能使用小写字母、数字、点、短横线或下划线；展示名称请在模型目录单独维护。",
			"requested_model": requested,
		})
	case errors.Is(err, modelgov.ErrModelIdentityConflict):
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":           "Public model identity already exists",
			"code":            "model_identity_conflict",
			"message":         "检测到同一模型的现有公共 ID，请复用该 ID 并把不同供应商配置为它的另一个端点。",
			"requested_model": requested,
			"canonical_model": modelgov.ExistingIdentity(err),
			"next_step":       "reuse_canonical_model",
		})
	case errors.Is(err, modelgov.ErrModelRetired):
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":           "Public model is retired",
			"code":            "model_retired",
			"message":         "该公共模型已退役。请先在高级运营中明确恢复其生命周期，或使用另一个公共模型 ID。",
			"requested_model": requested,
			"next_step":       "review_model_lifecycle",
		})
	default:
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error":   "Public model identity unavailable",
			"code":    "model_identity_unavailable",
			"message": "暂时无法核对公共模型身份；未创建端点或发布记录。",
		})
	}
}
