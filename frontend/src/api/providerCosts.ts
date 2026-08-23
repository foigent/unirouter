import { appendParams, request } from './transport';

export type PendingProviderCost = {
  org_id: number;
  request_id: string;
  attempt_id: string;
  api_key_id: number;
  provider: string;
  channel: string;
  state: string;
  outcome_certainty: string;
  billing_mode: string;
  calculated_cost_micros?: string;
  charged_cost_micros?: string;
  currency: string;
  amount_scale: number;
  upstream_request_id?: string;
  started_at: string;
  updated_at: string;
};

export type PendingProviderCostList = {
  data: PendingProviderCost[];
  org_id: number;
  limit: number;
};

export type ProviderCostReconciliationMutation = {
  org_id: number;
  request_id: string;
  attempt_id: string;
  action: 'refund';
  reason: string;
  evidence_ref: string;
};

export type ProviderCostReconciliationResult = {
  org_id: number;
  request_id: string;
  attempt_id: string;
  api_key_id: number;
  action: string;
  terminal_transaction_id: number;
  terminal_observation_receipt_id?: number;
  customer_charged_micros: string;
  customer_refund_micros: string;
  currency: string;
  amount_scale: number;
  billing_mode: string;
  state: string;
};

export const providerCostApi = {
  listPending: (orgId: number) => request<PendingProviderCostList>(
    appendParams('/provider-cost/pending', { org_id: orgId }),
  ),
  reconcile: (mutation: ProviderCostReconciliationMutation) => request<ProviderCostReconciliationResult>(
    '/provider-cost/reconciliations',
    { method: 'POST', body: JSON.stringify(mutation) },
  ),
};
