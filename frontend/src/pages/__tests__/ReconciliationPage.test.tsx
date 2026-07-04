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
  });

  it('renders usage source filter', async () => {
    render(<ReconciliationPage />);

    expect(await screen.findByText('来源筛选')).toBeTruthy();
  });
});
