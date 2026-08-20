package gateway

import (
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

// requireModelExecutionDispatchWired is the gateway's first P3 execution
// fence. It runs only after the planner has produced its immutable plan, but
// before provider material is decrypted or built and before cache, Freeze,
// attempt-ledger, coordinator, or provider work can begin. Coordinator keeps
// the same rule as a second, independent last-dispatch fence.
func requireModelExecutionDispatchWired(operation modeloperation.ID, plan modelrouting.Plan) error {
	if modelqualification.IsQualificationRequired(string(operation)) ||
		modelqualification.IsQualificationRequired(plan.ModelOperationID) {
		return modelexecution.ErrDispatchNotWired
	}
	for _, route := range plan.Routes {
		if route.PublicEvidence != nil {
			return modelexecution.ErrDispatchNotWired
		}
	}
	return nil
}
