import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import ReconciliationPage from '../ReconciliationPage';

const mockGetReconciliation = vi.fn();
const mockGetModelNames = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getReconciliation: (...args: any[]) => mockGetReconciliation(...args),
    getModelNames: (...args: any[]) => mockGetModelNames(...args),
  },
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
      cache_read_cost_cents: 0.003,
      cache_creation_cost_cents: 0.025,
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
    cache_read_cost_cents: 0.003,
    cache_creation_cost_cents: 0.025,
    authoritative_cache_records: 1,
    unreported_cache_records: 0,
  },
};

describe('ReconciliationPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetReconciliation.mockResolvedValue(RECON_RESPONSE);
    mockGetModelNames.mockResolvedValue({ data: ['company-chat'] });
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
    expect(screen.getAllByText('流式计数').length).toBeGreaterThan(0);
    expect(screen.getByText('缓存读取 Token')).toBeTruthy();
    expect(screen.getByText('缓存写入 Token')).toBeTruthy();
    expect(screen.getByText(/读 30.*写 10/)).toBeTruthy();
  });

  it('renders usage source filter', async () => {
    render(<ReconciliationPage />);

    expect(await screen.findByText('来源筛选')).toBeTruthy();
  });
});
