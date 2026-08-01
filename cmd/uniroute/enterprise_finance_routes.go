package main

import (
	"net/http"

	"github.com/uniroute/unirouter/internal/admin"
)

type enterpriseFinanceRouteGuards struct {
	read  func(http.HandlerFunc) http.Handler
	write func(http.HandlerFunc) http.Handler
}

func registerEnterpriseFinanceRoutes(mux *http.ServeMux, handler *admin.Handler, guards enterpriseFinanceRouteGuards) {
	mux.Handle("GET /api/v1/enterprise-finance/fiscal-periods", guards.read(handler.ListEnterpriseFiscalPeriods))
	mux.Handle("POST /api/v1/enterprise-finance/fiscal-periods", guards.write(handler.CreateEnterpriseFiscalPeriod))
	mux.Handle("POST /api/v1/enterprise-finance/fiscal-periods/{period_ref}/transition", guards.write(handler.TransitionEnterpriseFiscalPeriod))
	mux.Handle("GET /api/v1/enterprise-finance/cost-centers", guards.read(handler.ListEnterpriseCostCenters))
	mux.Handle("POST /api/v1/enterprise-finance/cost-centers", guards.write(handler.CreateEnterpriseCostCenterVersion))
	mux.Handle("GET /api/v1/enterprise-finance/budgets", guards.read(handler.ListEnterpriseBudgets))
	mux.Handle("GET /api/v1/enterprise-finance/budget-positions", guards.read(handler.ListEnterpriseBudgetPositions))
	mux.Handle("POST /api/v1/enterprise-finance/budgets", guards.write(handler.CreateEnterpriseBudgetDraft))
	mux.Handle("POST /api/v1/enterprise-finance/budgets/{budget_ref}/approve", guards.write(handler.ApproveEnterpriseBudget))
	mux.Handle("GET /api/v1/enterprise-finance/rate-cards", guards.read(handler.ListEnterpriseRateCards))
	mux.Handle("POST /api/v1/enterprise-finance/rate-cards", guards.write(handler.CreateEnterpriseRateCardDraft))
	mux.Handle("POST /api/v1/enterprise-finance/rate-cards/{rate_card_ref}/approve", guards.write(handler.ApproveEnterpriseRateCard))
	mux.Handle("POST /api/v1/enterprise-finance/rate-cards/{rate_card_ref}/activate", guards.write(handler.ActivateEnterpriseRateCard))
	mux.Handle("GET /api/v1/enterprise-finance/pricing-snapshots/{request_ref}", guards.read(handler.GetEnterprisePricingSnapshot))
	mux.Handle("GET /api/v1/enterprise-finance/allocation-runs", guards.read(handler.ListEnterpriseAllocationRuns))
	mux.Handle("GET /api/v1/enterprise-finance/allocation-runs/{allocation_run_ref}", guards.read(handler.GetEnterpriseAllocationRun))
	mux.Handle("POST /api/v1/enterprise-finance/allocation-runs", guards.write(handler.CreateEnterpriseAllocationRun))
	mux.Handle("GET /api/v1/enterprise-finance/allocation-summary", guards.read(handler.GetEnterpriseAllocationSummary))
	mux.Handle("GET /api/v1/enterprise-finance/adjustment-journals", guards.read(handler.ListEnterpriseAdjustmentJournals))
	mux.Handle("POST /api/v1/enterprise-finance/adjustment-journals", guards.write(handler.CreateEnterpriseAdjustmentJournal))
	mux.Handle("POST /api/v1/enterprise-finance/adjustment-journals/{journal_ref}/approve", guards.write(handler.ApproveEnterpriseAdjustmentJournal))
}
