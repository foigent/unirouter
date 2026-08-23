import { appendParams, request } from './transport';

export type ExactAmount = { value_minor: string; currency: string; scale: number };
export type EnterpriseScope = { kind: string; ref: string };

export type RateComponent = {
  meter: string;
  unit_quantity: string;
  amount: ExactAmount;
};
export type DailyPriceBand = {
  band_ref: string;
  start_minute_utc: number;
  end_minute_utc: number;
  components: RateComponent[];
};

export type FiscalPeriod = {
  org_id: number;
  period_ref: string;
  start_at: string;
  end_at: string;
  state: string;
  version: number;
  created_by?: string;
  state_changed_by: string;
  created_at?: string;
  updated_at?: string;
};

export type CostCenter = {
  org_id?: number;
  cost_center_ref: string;
  version: number;
  name: string;
  legal_entity_ref: string;
  owner_unit_ref?: string;
  source_ref: string;
  source_revision: string;
  valid_from: string;
  valid_to?: string;
  created_by?: string;
  created_at?: string;
};

export type BudgetRecord = {
  plan: {
    budget_ref: string;
    scope: EnterpriseScope;
    fiscal_period_ref: string;
    amount: ExactAmount;
    version: number;
    state: string;
    approved_by?: string;
  };
  created_by: string;
  runtime_effect: string;
};

export type BudgetPosition = {
  budget_ref: string;
  budget_version: number;
  actual: { status: string; amount?: ExactAmount };
  committed: { status: string; amount?: ExactAmount };
  forecast: { status: string; amount?: ExactAmount };
  variance_minor?: string;
  coverage_state: string;
  allocation_run_ref?: string;
};

export type RateSourceEvidence = {
  evidence_ref: string;
  source_kind: string;
  source_url: string;
  source_revision: string;
  fetched_at: string;
  payload_sha256: string;
  transform_version: string;
  currency?: string;
  unit_contract?: string;
  last_success_at: string;
  stale_after: string;
};

export type RateCardRecord = {
  id: number;
  card: {
    rate_card_ref: string;
    provider_ref: string;
    model_ref: string;
    layer: string;
    version: number;
    state: string;
    effective_from: string;
    effective_to?: string;
    source_evidence_ref?: string;
    created_by: string;
    approved_by?: string;
    components: RateComponent[];
    daily_price_bands?: DailyPriceBand[];
  };
  source_evidence?: RateSourceEvidence;
};

export type RequestPricingAuthority = {
  authority: string;
  selection_key: string;
  contract_priority: string[];
  state: string;
  missing_behavior: string;
  provider_ambiguity: string;
  active_card_count: number;
  draft_card_count: number;
  source_evidence_count: number;
  stale_source_count: number;
  last_source_success_at?: string;
  official_sync_mode: string;
  active_on_import: boolean;
  legacy_compatibility: string;
  public_source_failure: string;
  current_mode: 'legacy' | 'rate_card';
  strict_active: boolean;
};

export type LegacyRateBootstrapResult = {
  created_draft_count: number;
  existing_draft_count: number;
  skipped_model_count: number;
  skipped_models?: string[];
  state: string;
  approval_required: boolean;
  activation_required: boolean;
  legacy_runtime_fallback: boolean;
};

export type RateCardPreflightResult = {
  ready: boolean;
  checked_at: string;
  switch_instruction: string;
  models: Array<{
    model_ref: string;
    state: 'ready' | 'not_ready';
    rate_card_ref?: string;
    rate_card_version?: number;
    layer?: string;
    provider_ref?: string;
    pricing_version?: string;
    error?: string;
  }>;
};

export type PricingSnapshotResult = {
  result: string;
  snapshot: {
    request_ref: string;
    org_id: number;
    project_ref: string;
    priced_at: string;
    snapshot_hash: string;
  };
  binding_count: number;
  created_at: string;
};

export type AllocationRun = {
  id: number;
  request: {
    allocation_run_ref: string;
    source_revision: string;
    rule_set_ref: string;
    rule_set_revision: string;
    supersedes_run_ref?: string;
  };
  input_hash: string;
  source_record_count: number;
  direct_count: number;
  rule_based_count: number;
  unallocated_count: number;
  conflict_count: number;
  source_total: ExactAmount;
  allocated_total: ExactAmount;
  created_at: string;
};

export type ReconciliationSummary = {
  fiscal_period_ref: string;
  period_state: string;
  period_version: number;
  source_record_count: number;
  source_total: ExactAmount;
  latest_run?: AllocationRun;
  missing_record_count: number;
  coverage_state: string;
  close_ready: boolean;
  ledger_mutation: string;
  customer_debt_action: string;
  gl_export_state: string;
};

export type JournalRecord = {
  journal: {
    journal_ref: string;
    version: number;
    request_ref: string;
    entry_type: string;
    reversal_of_journal_ref?: string;
    target: EnterpriseScope;
    amount: ExactAmount;
    state: string;
    source_allocation_run_ref: string;
    created_by: string;
    approved_by?: string;
    gl_export_state: string;
  };
  created_at: string;
};

type ListResponse<T> = { data: T[]; total: number; org_id?: number };

type RateCardDraftInput = {
  org_id?: number;
  provider_ref: string;
  model_ref: string;
  layer: string;
  effective_from: string;
  effective_to: string | null;
  source_evidence_ref?: string;
  components: RateComponent[];
  daily_price_bands: DailyPriceBand[];
};

type RateCardTransitionInput = {
  org_id?: number;
  expected_version: number;
  supersedes_rate_card_ref?: string;
  expected_superseded_version?: number;
};

export const enterpriseFinanceApi = {
  getFiscalPeriods: (orgId?: number) =>
    request<ListResponse<FiscalPeriod>>(appendParams('/enterprise-finance/fiscal-periods', { org_id: orgId })),
  createFiscalPeriod: (data: { org_id?: number; period_ref: string; start_at: string; end_at: string }) =>
    request<FiscalPeriod>('/enterprise-finance/fiscal-periods', { method: 'POST', body: JSON.stringify(data) }),
  transitionFiscalPeriod: (periodRef: string, data: { org_id?: number; to_state: string; expected_version: number; reason: string }) =>
    request<FiscalPeriod>(`/enterprise-finance/fiscal-periods/${encodeURIComponent(periodRef)}/transition`, { method: 'POST', body: JSON.stringify(data) }),
  getCostCenters: (orgId?: number, effectiveAt?: string) =>
    request<ListResponse<CostCenter>>(appendParams('/enterprise-finance/cost-centers', { org_id: orgId, effective_at: effectiveAt })),
  createCostCenterVersion: (data: {
    org_id?: number; cost_center_ref: string; version: number; name: string; legal_entity_ref: string;
    owner_unit_ref?: string; source_ref: string; source_revision: string; valid_from: string; valid_to: string | null;
  }) => request<CostCenter>('/enterprise-finance/cost-centers', { method: 'POST', body: JSON.stringify(data) }),
  getBudgets: (orgId?: number) =>
    request<ListResponse<BudgetRecord>>(appendParams('/enterprise-finance/budgets', { org_id: orgId })),
  getBudgetPositions: (orgId?: number) =>
    request<ListResponse<BudgetPosition>>(appendParams('/enterprise-finance/budget-positions', { org_id: orgId })),
  createBudgetDraft: (data: { org_id?: number; budget_ref?: string; scope: EnterpriseScope; fiscal_period_ref: string; amount: ExactAmount }) =>
    request<BudgetRecord>('/enterprise-finance/budgets', { method: 'POST', body: JSON.stringify(data) }),
  approveBudget: (budgetRef: string, data: { org_id?: number; expected_version: number }) =>
    request<BudgetRecord>(`/enterprise-finance/budgets/${encodeURIComponent(budgetRef)}/approve`, { method: 'POST', body: JSON.stringify(data) }),
  getRateCards: (orgId?: number) =>
    request<ListResponse<RateCardRecord> & { request_pricing_authority: RequestPricingAuthority }>(appendParams('/enterprise-finance/rate-cards', { org_id: orgId })),
  createRateCardDraft: (data: RateCardDraftInput) =>
    request<RateCardRecord>('/enterprise-finance/rate-cards', { method: 'POST', body: JSON.stringify(data) }),
  importOfficialRateCardDraft: (data: Omit<RateCardDraftInput, 'layer' | 'source_evidence_ref'> & {
    source_url: string; source_revision: string; fetched_at: string; payload_sha256: string;
    transform_version: string; stale_after: string;
  }) => request<{ record: RateCardRecord; import_state: 'draft'; automatic_activation: false; network_fetch_performed: false }>(
    '/enterprise-finance/rate-cards/import-official-draft', { method: 'POST', body: JSON.stringify(data) },
  ),
  bootstrapLegacyRateCardDrafts: (data: { org_id?: number }) =>
    request<LegacyRateBootstrapResult>('/enterprise-finance/rate-cards/bootstrap-legacy-drafts', { method: 'POST', body: JSON.stringify(data) }),
  preflightRateCardAuthority: (data: { org_id?: number; models: Array<{ model_ref: string }> }) =>
    request<RateCardPreflightResult>('/enterprise-finance/rate-cards/preflight', { method: 'POST', body: JSON.stringify(data) }),
  approveRateCard: (rateCardRef: string, data: RateCardTransitionInput) =>
    request<RateCardRecord>(`/enterprise-finance/rate-cards/${encodeURIComponent(rateCardRef)}/approve`, { method: 'POST', body: JSON.stringify(data) }),
  activateRateCard: (rateCardRef: string, data: RateCardTransitionInput) =>
    request<RateCardRecord>(`/enterprise-finance/rate-cards/${encodeURIComponent(rateCardRef)}/activate`, { method: 'POST', body: JSON.stringify(data) }),
  getPricingSnapshot: (requestRef: string, orgId?: number) =>
    request<PricingSnapshotResult>(appendParams(`/enterprise-finance/pricing-snapshots/${encodeURIComponent(requestRef)}`, { org_id: orgId })),
  getAllocationRuns: (periodRef: string, orgId?: number) =>
    request<ListResponse<AllocationRun>>(appendParams('/enterprise-finance/allocation-runs', { period_ref: periodRef, org_id: orgId })),
  getAllocationSummary: (periodRef: string, orgId?: number) =>
    request<ReconciliationSummary>(appendParams('/enterprise-finance/allocation-summary', { period_ref: periodRef, org_id: orgId })),
  getAdjustmentJournals: (periodRef: string, orgId?: number) =>
    request<ListResponse<JournalRecord>>(appendParams('/enterprise-finance/adjustment-journals', { period_ref: periodRef, org_id: orgId })),
  createAdjustmentJournal: (data: {
    org_id?: number; journal_ref: string; fiscal_period_ref: string; request_ref: string; entry_type: string;
    reversal_of_journal_ref: string; target: EnterpriseScope; amount: ExactAmount; reason: string; source_allocation_run_ref: string;
  }) => request<JournalRecord>('/enterprise-finance/adjustment-journals', { method: 'POST', body: JSON.stringify(data) }),
  approveAdjustmentJournal: (journalRef: string, data: { org_id?: number; expected_version: number }) =>
    request<JournalRecord>(`/enterprise-finance/adjustment-journals/${encodeURIComponent(journalRef)}/approve`, { method: 'POST', body: JSON.stringify(data) }),
};
