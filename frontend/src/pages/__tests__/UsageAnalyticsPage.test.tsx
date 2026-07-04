import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import UsageAnalyticsPage from '../UsageAnalyticsPage';

const mockGetUsageAnalytics = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getUsageAnalytics: (...args: any[]) => mockGetUsageAnalytics(...args),
  },
}));

describe('UsageAnalyticsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetUsageAnalytics.mockResolvedValue({
      group_by: { key: 'project', label: '项目' },
      summary: {
        total_requests: 12,
        success_requests: 10,
        failed_requests: 2,
        total_tokens: 1200,
        cost_cents: 32,
        avg_latency_ms: 210,
        estimated_tokens: 200,
        upstream_tokens: 1000,
        estimated_ratio: 0.2,
        csv_export_scope: 'filtered_query',
        prompt_retention: 'not_stored_by_default',
        response_retention: 'not_stored_by_default',
      },
      data: [{
        key: '7',
        total_requests: 12,
        success_requests: 10,
        failed_requests: 2,
        total_tokens: 1200,
        cost_cents: 32,
        avg_latency_ms: 210,
        estimated_ratio: 0.2,
      }],
    });
  });

  it('loads usage analytics with the default project group', async () => {
    render(<UsageAnalyticsPage />);

    await waitFor(() => {
      expect(mockGetUsageAnalytics).toHaveBeenCalledWith(expect.objectContaining({ group_by: 'project' }));
    });

    expect(await screen.findByText('用量分析')).toBeTruthy();
    expect(screen.getByText('当前筛选口径')).toBeTruthy();
    expect(screen.getByText('请求内容：默认不存储明文 / 响应内容：默认不存储明文')).toBeTruthy();
  });
});
