import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import CostOptimizationPage from '../CostOptimizationPage';

const mockSummary = vi.fn();
const mockTrend = vi.fn();
const mockBreakdown = vi.fn();
const mockOpportunities = vi.fn();
const mockOrganizations = vi.fn();
const auth = vi.hoisted(() => ({ user: { role: 'org_admin', org_id: 7 } as any }));

vi.mock('../../api/client', () => ({
  api: {
    getCostOptimizationSummary: (...args: any[]) => mockSummary(...args),
    getCostOptimizationTrend: (...args: any[]) => mockTrend(...args),
    getCostOptimizationBreakdown: (...args: any[]) => mockBreakdown(...args),
    getCostOptimizationOpportunities: (...args: any[]) => mockOpportunities(...args),
    getOrganizations: (...args: any[]) => mockOrganizations(...args),
  },
}));

vi.mock('../../store/auth', () => ({
  useAuthStore: (selector: any) => selector({ user: auth.user }),
}));

describe('CostOptimizationPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    auth.user = { role: 'org_admin', org_id: 7 };
    mockOrganizations.mockResolvedValue({ data: [{ id: 7, name: '总部' }], total: 1 });
    mockSummary.mockResolvedValue({
      summary: {
        metric_version: 'unirouter.cost-optimization.metrics.v1',
        currency: 'CNY',
        amount_scale: 1_000_000,
        amount_authority: 'request_time_frozen_customer_pricing',
        candidate_requests: 10,
        eligible_requests: 8,
        cache_used_requests: 6,
        cache_read_requests: 5,
        cache_write_requests: 4,
        prompt_tokens: 1000,
        cache_read_tokens: 400,
        cache_creation_tokens: 200,
        ordinary_input_baseline_cost_micros: '1000000',
        actual_cost_micros: '700000',
        net_savings_micros: '300000',
        cache_read_savings_micros: '350000',
        cache_creation_premium_micros: '50000',
        rounding_adjustment_micros: '0',
        evidence_coverage_ratio: 0.8,
        cache_read_token_ratio: 0.4,
        read_to_creation_token_ratio: 2,
        response_cache_runtime: 'not_connected_to_gateway',
        compression_runtime: 'default_off_shadow_not_started',
        supplier_cogs_authority: 'not_claimed_use_provider_reconciliation',
      },
    });
    mockTrend.mockResolvedValue({ data: [{
      day: '2026-08-04',
      candidate_requests: 10,
      eligible_requests: 8,
      cache_read_tokens: 400,
      cache_creation_tokens: 200,
      net_savings_micros: '300000',
      cache_read_savings_micros: '350000',
      cache_creation_premium_micros: '50000',
      evidence_coverage_ratio: 0.8,
    }] });
    mockBreakdown.mockResolvedValue({ data: [{
      key: 'gpt-4o',
      candidate_requests: 10,
      eligible_requests: 8,
      cache_used_requests: 6,
      prompt_tokens: 1000,
      cache_read_tokens: 400,
      cache_creation_tokens: 200,
      ordinary_input_baseline_cost_micros: '1000000',
      actual_cost_micros: '700000',
      net_savings_micros: '300000',
      cache_read_savings_micros: '350000',
      cache_creation_premium_micros: '50000',
      rounding_adjustment_micros: '0',
      evidence_coverage_ratio: 0.8,
      cache_read_token_ratio: 0.4,
      read_to_creation_token_ratio: 2,
    }] });
    mockOpportunities.mockResolvedValue({ data: [{
      kind: 'evidence_gap',
      severity: 'warning',
      group_key: 'gpt-4o',
      group_label: '模型',
      title: '补齐 Prompt Cache 计量证据',
      rationale: '存在证据缺口。',
      next_action: '检查请求日志。',
      evidence: { gap_requests: 2 },
    }] });
  });

  it('renders the read-only savings truth and a persistent auto-selected inspector', async () => {
    render(<MemoryRouter><CostOptimizationPage /></MemoryRouter>);

    await waitFor(() => expect(mockSummary).toHaveBeenCalled());
    expect(mockTrend).toHaveBeenCalled();
    expect(mockBreakdown).toHaveBeenCalledWith(expect.objectContaining({ group_by: 'model' }));
    expect(mockOpportunities).toHaveBeenCalledWith(expect.objectContaining({ group_by: 'model' }));

    expect(await screen.findByRole('heading', { name: '成本优化' })).toBeTruthy();
    expect(screen.getByText(/不等于供应商 COGS/)).toBeTruthy();
    expect(screen.getByText('Provider Prompt Cache')).toBeTruthy();
    expect(screen.getByText('Exact Response Cache')).toBeTruthy();
    expect(screen.getByText('Compression')).toBeTruthy();
    expect(screen.getByText('Gateway 未接入')).toBeTruthy();
    expect(screen.getByText('默认关闭 / Shadow 未启动')).toBeTruthy();
    expect(screen.getAllByText('¥0.300000').length).toBeGreaterThanOrEqual(2);

    const inspector = screen.getByLabelText('成本优化证据检查器');
    expect(within(inspector).getByRole('heading', { name: 'gpt-4o' })).toBeTruthy();
    expect(within(inspector).getByText('已自动选中')).toBeTruthy();
    expect(within(inspector).getByText(/供应商 COGS、未接入 Gateway/)).toBeTruthy();
    expect(within(inspector).getByRole('button', { name: /查看请求证据/ })).toBeTruthy();
  });

  it('keeps organization admins on server-enforced tenant scope', async () => {
    render(<MemoryRouter><CostOptimizationPage /></MemoryRouter>);
    await waitFor(() => expect(mockSummary).toHaveBeenCalled());
    const filters = mockSummary.mock.calls[mockSummary.mock.calls.length - 1]?.[0];
    expect(filters).not.toHaveProperty('org_id');
    expect(screen.queryByLabelText('目标组织')).toBeNull();
    expect(screen.getByLabelText('拆解维度')).toBeTruthy();
    expect(mockOrganizations).not.toHaveBeenCalled();
  });

  it('gives platform administrators named organization and grouping controls', async () => {
    auth.user = { role: 'platform_admin', org_id: 7 };
    render(<MemoryRouter><CostOptimizationPage /></MemoryRouter>);

    expect(await screen.findByLabelText('目标组织')).toBeTruthy();
    expect(screen.getByLabelText('拆解维度')).toBeTruthy();
    await waitFor(() => expect(mockOrganizations).toHaveBeenCalled());
  });
});
