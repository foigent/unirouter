import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import UsageAnalyticsPage from '../UsageAnalyticsPage';

const mockGetUsageAnalytics = vi.fn();
const mockGetOrganizations = vi.fn();
const auth = vi.hoisted(() => ({ user: null as any }));

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => { resolve = next; });
  return { promise, resolve };
}

vi.mock('../../api/client', () => ({
  api: {
    getUsageAnalytics: (...args: any[]) => mockGetUsageAnalytics(...args),
    getOrganizations: (...args: any[]) => mockGetOrganizations(...args),
  },
}));

vi.mock('../../store/auth', () => ({
  useAuthStore: (selector: any) => selector({ user: auth.user }),
}));

describe('UsageAnalyticsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    auth.user = null;
    mockGetOrganizations.mockResolvedValue({ data: [{ id: 7, name: '总部' }], total: 1 });
    mockGetUsageAnalytics.mockResolvedValue({
      group_by: { key: 'overall', label: '总体' },
      summary: {
        total_requests: 12,
        success_requests: 10,
        failed_requests: 2,
        total_tokens: 1200,
        cost_micros: '320000',
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
        label: '全部请求',
        total_requests: 12,
        success_requests: 10,
        failed_requests: 2,
        total_tokens: 1200,
        cost_micros: '320000',
        avg_latency_ms: 210,
        estimated_ratio: 0.2,
      }],
    });
  });

  it('renders resolved entity labels instead of bare owner ids', async () => {
    mockGetUsageAnalytics.mockResolvedValueOnce({
      summary: { total_requests: 1, total_tokens: 96, cost_micros: '0', estimated_ratio: 0 },
      data: [{
        key: '34',
        label: 'uiaccept（用户 #34）',
        total_requests: 1,
        success_requests: 1,
        failed_requests: 0,
        total_tokens: 96,
        cost_micros: '0',
        avg_latency_ms: 1200,
        estimated_ratio: 0,
      }],
    });

    render(<UsageAnalyticsPage />);

    expect(await screen.findByText('uiaccept（用户 #34）')).toBeTruthy();
    expect(screen.queryByText('34')).toBeNull();
  });

  it('loads usage analytics with the default overall group and visible semantics', async () => {
    render(<UsageAnalyticsPage />);

    await waitFor(() => {
      expect(mockGetUsageAnalytics).toHaveBeenCalledWith(expect.objectContaining({ group_by: 'overall' }));
    });

    expect(await screen.findByText('用量分析')).toBeTruthy();
    expect(screen.getByText('未设置筛选表示当前权限范围内全部数据。供应商是 provider family 或 adapter 维度；上游通道是一个具体配置实例，包含 endpoint、凭证、权重/优先级和可用模型。')).toBeTruthy();
    expect(screen.getByText('用量范围')).toBeTruthy();
    expect(screen.getByPlaceholderText('所有者用户 ID')).toBeTruthy();
    expect(screen.getByText('当前筛选口径 / 总览已包含全部匹配数据')).toBeTruthy();
    expect(screen.getByText('当前分组：总览（不分组）。请求内容：默认不存储明文 / 响应内容：默认不存储明文')).toBeTruthy();
    expect(screen.getByText('全部请求')).toBeTruthy();
    expect(screen.getAllByText('¥0.320000').length).toBeGreaterThanOrEqual(2);
    expect(screen.getByRole('combobox', { name: '分组' })).toBeTruthy();
    expect(screen.getByRole('textbox', { name: '模型筛选' })).toBeTruthy();
    expect(screen.getByLabelText('用量开始时间')).toBeTruthy();
  });

  it('adds the canonical customer to platform analytics requests without a selector', async () => {
    auth.user = {
      role: 'platform_admin',
      org_id: 99,
      customer_context: { state: 'ready', org_id: 7, org_name: '总部' },
      identity_scope: 'deployment_operator',
    };
    render(<UsageAnalyticsPage />);

    expect(await screen.findByText('客户范围：总部')).toBeTruthy();
    expect(screen.queryByLabelText('目标组织')).toBeNull();
    await waitFor(() => expect(mockGetUsageAnalytics).toHaveBeenCalledWith(expect.objectContaining({ org_id: 7 })));
    expect(mockGetOrganizations).not.toHaveBeenCalled();
  });

  it('does not query usage when a platform customer context is ambiguous', () => {
    auth.user = { role: 'platform_admin', org_id: 99, customer_context: { state: 'ambiguous' } };
    render(<UsageAnalyticsPage />);

    expect(screen.getAllByText('客户组织上下文不唯一').length).toBeGreaterThanOrEqual(1);
    expect(mockGetUsageAnalytics).not.toHaveBeenCalled();
  });

  it('keeps organization-scoped roles on their server-enforced JWT scope', async () => {
    auth.user = { role: 'org_admin', org_id: 7 };
    render(<UsageAnalyticsPage />);

    await waitFor(() => expect(mockGetUsageAnalytics).toHaveBeenCalled());
    const latestFilters = mockGetUsageAnalytics.mock.calls[mockGetUsageAnalytics.mock.calls.length - 1]?.[0];
    expect(latestFilters).not.toHaveProperty('org_id');
    expect(screen.queryByText('目标组织')).toBeNull();
    expect(mockGetOrganizations).not.toHaveBeenCalled();
  });

  it('discards stale filtered responses, clears old data, and exposes retry on failure', async () => {
    const oldRequest = deferred<any>();
    const currentRequest = deferred<any>();
    mockGetUsageAnalytics
      .mockReturnValueOnce(oldRequest.promise)
      .mockReturnValueOnce(currentRequest.promise);

    render(<UsageAnalyticsPage />);
    await waitFor(() => expect(mockGetUsageAnalytics).toHaveBeenCalledTimes(1));

    fireEvent.change(screen.getByPlaceholderText('模型'), { target: { value: 'fresh-model' } });
    await waitFor(() => expect(mockGetUsageAnalytics).toHaveBeenCalledTimes(2));
    expect(screen.queryByText('全部请求')).toBeNull();

    await act(async () => currentRequest.resolve({
      summary: { total_requests: 1, total_tokens: 2, cost_micros: '3', estimated_ratio: 0 },
      data: [{ key: 'fresh-model', total_requests: 1, success_requests: 1, failed_requests: 0, total_tokens: 2, cost_micros: '3', avg_latency_ms: 4, estimated_ratio: 0 }],
    }));
    expect(await screen.findByText('fresh-model')).toBeTruthy();

    await act(async () => oldRequest.resolve({
      summary: { total_requests: 99 },
      data: [{ key: 'stale-model', total_requests: 99 }],
    }));
    await waitFor(() => expect(screen.queryByText('stale-model')).toBeNull());

    mockGetUsageAnalytics.mockRejectedValueOnce(new Error('unavailable'));
    const callCount = mockGetUsageAnalytics.mock.calls.length;
    fireEvent.click(screen.getByRole('button', { name: /刷新/ }));
    await waitFor(() => expect(mockGetUsageAnalytics.mock.calls.length).toBe(callCount + 1));
    expect(await screen.findByRole('heading', { name: '用量分析加载失败' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '重新加载用量分析' })).toBeTruthy();
  });
});
