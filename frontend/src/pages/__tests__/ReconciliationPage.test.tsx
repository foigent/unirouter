import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import ReconciliationPage from '../ReconciliationPage';

const mockGetReconciliation = vi.fn();
const mockGetModelNames = vi.fn();
const mockGetOrganizations = vi.fn();
const mockPreviewProviderCOGSOffline = vi.fn();
const mockGetPendingProviderCosts = vi.fn();
const mockReconcileProviderCost = vi.fn();
const mockGetEnterpriseFiscalPeriods = vi.fn();
const mockGetEnterpriseAllocationSummary = vi.fn();
const mockGetEnterpriseAllocationRuns = vi.fn();
const mockGetEnterpriseAdjustmentJournals = vi.fn();
const auth = vi.hoisted(() => ({ user: null as any }));

vi.mock('../../api/client', () => ({
  api: {
    getReconciliation: (...args: any[]) => mockGetReconciliation(...args),
    getModelNames: (...args: any[]) => mockGetModelNames(...args),
    getOrganizations: (...args: any[]) => mockGetOrganizations(...args),
    previewProviderCOGSOffline: (...args: any[]) => mockPreviewProviderCOGSOffline(...args),
  },
}));

vi.mock('../../api/providerCosts', () => ({
  providerCostApi: {
    listPending: (...args: any[]) => mockGetPendingProviderCosts(...args),
    reconcile: (...args: any[]) => mockReconcileProviderCost(...args),
  },
}));

vi.mock('../../api/enterpriseFinance', () => ({
  enterpriseFinanceApi: {
    getFiscalPeriods: (...args: any[]) => mockGetEnterpriseFiscalPeriods(...args),
    getAllocationSummary: (...args: any[]) => mockGetEnterpriseAllocationSummary(...args),
    getAllocationRuns: (...args: any[]) => mockGetEnterpriseAllocationRuns(...args),
    getAdjustmentJournals: (...args: any[]) => mockGetEnterpriseAdjustmentJournals(...args),
  },
}));

vi.mock('../../store/auth', () => ({
  useAuthStore: (selector: any) => selector({ user: auth.user }),
}));

const RECON_RESPONSE = {
  data: [
    {
      id: 1,
      request_id: 'req-recon-001',
      model: 'company-chat',
      estimated_tokens: 90,
      actual_tokens: 100,
      delta: 10,
      usage_source: 'counter_v2',
      variance_ratio: 0.1111,
      source_confidence: 0.65,
      uncached_prompt_tokens: 50,
      cache_read_tokens: 30,
      cache_creation_tokens: 10,
      cache_usage_schema: 'anthropic.prompt_cache.v1',
      cache_usage_status: 'cache_read_write_reported',
      pricing_version: 'pv2_test',
      cache_read_cost_micros: '30',
      cache_creation_cost_micros: '250',
      status: 'reconciled',
      created_at: '2026-07-02T10:00:00Z',
    },
  ],
  total: 1,
  summary: {
    estimated_tokens: 80,
    upstream_tokens: 20,
    preflight_estimated_tokens: 90,
    actual_tokens: 100,
    variance_tokens: 10,
    variance_ratio: 0.1111,
    average_source_confidence: 0.65,
    estimated_ratio: 0.8,
    estimated_ratio_alert_threshold: 0.2,
    estimated_usage_alert: true,
    estimated_ratio_scope: 'filtered_query',
    cache_read_tokens: 30,
    cache_creation_tokens: 10,
    cache_read_cost_micros: '30',
    cache_creation_cost_micros: '250',
    authoritative_cache_records: 1,
    unreported_cache_records: 0,
  },
};

describe('ReconciliationPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    auth.user = { id: 4, username: 'billing', role: 'billing_admin', org_id: 7 };
    mockGetReconciliation.mockResolvedValue(RECON_RESPONSE);
    mockGetModelNames.mockResolvedValue({ data: ['company-chat'] });
    mockGetOrganizations.mockResolvedValue({ data: [{ id: 7, name: '总部' }], total: 1 });
    mockGetPendingProviderCosts.mockResolvedValue({ data: [], org_id: 7 });
    mockReconcileProviderCost.mockResolvedValue({
      request_id: 'req-provider-hold',
      attempt_id: 'attempt-provider-hold',
      action: 'refund',
      terminal_observation_receipt_id: 812,
      customer_charged_micros: '0',
      customer_refund_micros: '0',
      currency: 'CNY',
      amount_scale: 1000000,
      billing_mode: 'cost_observation',
      state: 'reconciled',
    });
    mockGetEnterpriseFiscalPeriods.mockResolvedValue({
      data: [{ period_ref: 'FY26-P08', state: 'reconciled', version: 3 }],
    });
    mockGetEnterpriseAllocationSummary.mockResolvedValue({
      fiscal_period_ref: 'FY26-P08', period_state: 'reconciled', period_version: 3,
      source_record_count: 2,
      source_total: { value_minor: '3000000', currency: 'CNY', scale: 1000000 },
      missing_record_count: 0, coverage_state: 'complete', close_ready: true,
      ledger_mutation: 'none', customer_debt_action: 'none', gl_export_state: 'not_configured',
      latest_run: { unallocated_count: 0, conflict_count: 0 },
    });
    mockGetEnterpriseAllocationRuns.mockResolvedValue({ data: [], total: 0 });
    mockGetEnterpriseAdjustmentJournals.mockResolvedValue({ data: [], total: 0 });
    mockPreviewProviderCOGSOffline.mockResolvedValue({
      report_version: 'unirouter.provider-cogs-report.v1',
      contract_version: 'unirouter.provider-cogs-offline.v1',
      import_id: 'import-test',
      import_payload_hash: `sha256:${'1'.repeat(64)}`,
      import_semantic_hash: `sha256:${'2'.repeat(64)}`,
      source_snapshot_hash: `sha256:${'3'.repeat(64)}`,
      provider: 'openai',
      source_instance: `sha256:${'4'.repeat(64)}`,
      currency: 'USD',
      amount_scale: '1000000',
      replay_status: 'accepted',
      customer_ledger_mutation: 'none',
      matches: [{
        status: 'exact_request_matched',
        evidence_level: 'exact_request_usage',
        line_id: 'supplier-line-1',
        request_id: 'req-financial-truth',
        attempt_id: 'attempt-1',
        upstream_request_id: 'upstream-1',
        project_ref: `sha256:${'5'.repeat(64)}`,
        provider_cost: { value_minor: '120', currency: 'USD', scale: '1000000', authority: 'supplier_reported' },
        provider_estimate: { value_minor: '100', currency: 'USD', scale: '1000000', authority: 'unirouter_estimated' },
        provider_variance_minor: '20',
        customer_revenue: { value_minor: '5000', currency: 'CNY', scale: '1000000', authority: 'customer_ledger_snapshot' },
        customer_debt_action: 'none',
      }],
      summary: {
        pages: 1,
        unique_supplier_lines: 1,
        duplicate_noop_lines: 0,
        source_attempts: 1,
        exact_request_matches: 1,
        supplier_only: 0,
        aggregate_unattributed: 0,
        ambiguous: 0,
        source_only: 0,
        supplier_evidence_pending: 0,
        supplier_total_minor: '120',
        matched_supplier_minor: '120',
        unattributed_minor: '0',
      },
    });
  });

  it('loads reconciliation through the API client and renders full-query summary', async () => {
    render(<ReconciliationPage />);

    await waitFor(() => {
      expect(mockGetReconciliation).toHaveBeenCalledWith(1, 20, {
        model: '',
        status: '',
        usage_source: '',
      });
    });

    expect(await screen.findByText('req-recon-001')).toBeTruthy();
    expect(screen.getByText('80.0%')).toBeTruthy();
    expect(screen.getByText(/当前筛选口径/)).toBeTruthy();
    expect(screen.getAllByText('流式估算（v2）').length).toBeGreaterThan(0);
    expect(screen.getByText('缓存读取 Token')).toBeTruthy();
    expect(screen.getByText('缓存写入 Token')).toBeTruthy();
    expect(screen.getByText(/读 30.*写 10/)).toBeTruthy();
    expect(screen.getByText(/读取费用 ¥0\.000030/)).toBeTruthy();
    expect(screen.getByText(/写入费用 ¥0\.000250/)).toBeTruthy();
    expect(await screen.findByText('企业成本分摊与关账')).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '财务期间' })).toBeTruthy();
    expect(screen.getByText('关账就绪')).toBeTruthy();
    expect(mockGetEnterpriseAllocationSummary).toHaveBeenCalledWith('FY26-P08', undefined);
  });

  it('scopes platform reconciliation to the canonical customer, never the technical organization', async () => {
    auth.user = {
      role: 'platform_admin',
      org_id: 99,
      customer_context: { state: 'ready', org_id: 7, org_name: '总部' },
    };
    render(<ReconciliationPage />);

    expect(await screen.findByText('客户范围：总部')).toBeTruthy();
    expect(screen.queryByText('目标组织')).toBeNull();
    await waitFor(() => expect(mockGetReconciliation).toHaveBeenCalledWith(1, 20, expect.objectContaining({ org_id: 7 })));
    expect(mockGetReconciliation).not.toHaveBeenCalledWith(1, 20, expect.objectContaining({ org_id: 99 }));
    expect(mockGetOrganizations).not.toHaveBeenCalled();
  });

  it('lets a platform admin release a cost_observation hold without supplying an amount', async () => {
    auth.user = {
      role: 'platform_admin',
      org_id: 99,
      customer_context: { state: 'ready', org_id: 7, org_name: '总部' },
    };
    mockGetPendingProviderCosts.mockResolvedValue({
      org_id: 7,
      data: [{
        org_id: 7,
        request_id: 'req-provider-hold',
        attempt_id: 'attempt-provider-hold',
        api_key_id: 44,
        provider: 'deepseek',
        channel: 'deepseek-direct',
        state: 'output_withheld',
        outcome_certainty: 'known_usage',
        billing_mode: 'cost_observation',
        calculated_cost_micros: '0',
        currency: 'CNY',
        amount_scale: 1000000,
        started_at: '2026-08-24T00:00:00Z',
        updated_at: '2026-08-24T00:01:00Z',
      }],
    });
    render(<ReconciliationPage />);

    expect(await screen.findByText('req-provider-hold')).toBeTruthy();
    expect(mockGetPendingProviderCosts).toHaveBeenCalledWith(7);
    fireEvent.click(screen.getByRole('button', { name: '零收费协调' }));
    expect(await screen.findByText('确认零收费供应商成本协调')).toBeTruthy();
    expect((screen.getByLabelText('协调原因') as HTMLTextAreaElement).value).toContain('cost_observation');
    fireEvent.change(screen.getByLabelText('证据引用'), { target: { value: 'call_log:347' } });
    fireEvent.click(screen.getByRole('button', { name: '解除冻结并生成收据' }));

    await waitFor(() => expect(mockReconcileProviderCost).toHaveBeenCalledWith({
      org_id: 7,
      request_id: 'req-provider-hold',
      attempt_id: 'attempt-provider-hold',
      action: 'refund',
      reason: expect.stringContaining('cost_observation'),
      evidence_ref: 'call_log:347',
    }));
    expect(await screen.findByText(/观察收据 #812/)).toBeTruthy();
    expect(screen.getByText(/客户收费 0，退款 0/)).toBeTruthy();
  });

  it('fails closed before customer-domain reads when the canonical customer is ambiguous', async () => {
    auth.user = {
      role: 'platform_admin',
      org_id: 99,
      customer_context: { state: 'ambiguous' },
    };
    render(<ReconciliationPage />);

    expect(await screen.findByRole('heading', { name: '客户组织上下文不唯一' })).toBeTruthy();
    expect(mockGetReconciliation).not.toHaveBeenCalled();
    expect(mockGetModelNames).not.toHaveBeenCalled();
    expect(mockGetEnterpriseAllocationSummary).not.toHaveBeenCalled();
    expect(mockGetOrganizations).not.toHaveBeenCalled();
  });

  it('renders usage source filter', async () => {
    render(<ReconciliationPage />);

    expect(await screen.findByText('来源筛选')).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '模型' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '状态' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '计量来源' })).toBeTruthy();
  });

  it('previews supplier evidence and auto-opens the persistent financial inspector', async () => {
    render(<ReconciliationPage />);

    expect(screen.getByLabelText('供应商成本证据检查器')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('供应商成本离线证据 JSON'), {
      target: { value: '{"contract_version":"unirouter.provider-cogs-offline.v1"}' },
    });
    fireEvent.click(screen.getByText('严格校验并预览'));

    await waitFor(() => expect(mockPreviewProviderCOGSOffline).toHaveBeenCalledWith(
      '{"contract_version":"unirouter.provider-cogs-offline.v1"}',
    ));
    expect(await screen.findByText('req-financial-truth')).toBeTruthy();
    expect(screen.getByText(/req-financial-truth.*attempt-1/)).toBeTruthy();
    expect(screen.getByText('客户账：none')).toBeTruthy();
    expect(screen.getAllByText('请求级精确匹配').length).toBeGreaterThan(0);
    expect(screen.getByText('5000 / 1000000 CNY')).toBeTruthy();
  });
});
