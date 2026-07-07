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
      group_by: { key: 'overall', label: '总体' },
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
        key: '全部请求',
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

  it('loads usage analytics with the default overall group and visible semantics', async () => {
    render(<UsageAnalyticsPage />);

    await waitFor(() => {
      expect(mockGetUsageAnalytics).toHaveBeenCalledWith(expect.objectContaining({ group_by: 'overall' }));
    });

    expect(await screen.findByText('用量分析')).toBeTruthy();
    expect(screen.getByText('未设置筛选表示当前权限范围内全部数据。供应商是 provider family 或 adapter 维度；上游通道是一个具体配置实例，包含 endpoint、凭证、权重/优先级和可用模型。')).toBeTruthy();
    expect(screen.getByText('用量范围')).toBeTruthy();
    expect(screen.getByPlaceholderText('Owner 用户 ID')).toBeTruthy();
    expect(screen.getByText('当前筛选口径 / 总览已包含全部匹配数据')).toBeTruthy();
    expect(screen.getByText('当前分组：总览（不分组）。请求内容：默认不存储明文 / 响应内容：默认不存储明文')).toBeTruthy();
    expect(screen.getByText('全部请求')).toBeTruthy();
  });
});
