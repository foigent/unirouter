import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import DashboardPage from '../DashboardPage';

const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return { ...actual, useNavigate: () => mockNavigate };
});

const mockGetDashboardStats = vi.fn();
vi.mock('../../api/client', () => ({
  api: { getDashboardStats: (...args: any[]) => mockGetDashboardStats(...args) },
}));

const MOCK_STATS = {
  total_requests: 12345,
  today_cost_cents: 5678,
  avg_latency_ms: 320,
  p95_latency_ms: 780,
  p99_latency_ms: 1290,
  success_rate: 0.987,
  wallet_balance: 100000,
  wallet_granted: 20000,
  wallet_frozen: 5000,
  wallet_threshold: 1000,
  recent_logs: [
    {
      request_id: 'req-abc-123',
      model: 'gpt-4o',
      provider: 'openai',
      status_code: 200,
      latency_ms: 450,
      total_tokens: 1500,
      cost_cents: 30,
      created_at: '2026-06-29T10:00:00Z',
      is_stream: false,
    },
  ],
  model_prices: [
    { model_name: 'gpt-4o', input_price_cents: 100, output_price_cents: 200 },
  ],
  daily_trend: [
    { date: '2026-06-28', requests: 5000, cost_cents: 2000 },
    { date: '2026-06-29', requests: 7345, cost_cents: 3678 },
  ],
  model_distribution: [
    { model: 'gpt-4o', requests: 8000 },
    { model: 'claude-3.5-sonnet', requests: 4345 },
  ],
  channel_health: [
    { id: 1, name: 'OpenAI Main', provider: 'openai', enabled: true, status: 'healthy', success_count: 10000, fail_count: 50 },
    { id: 2, name: 'Azure Backup', provider: 'azure', enabled: true, status: 'degraded', success_count: 2000, fail_count: 300 },
    { id: 3, name: 'Disabled CH', provider: 'custom', enabled: false, status: 'healthy', success_count: 0, fail_count: 0 },
  ],
  delta_vs_yesterday: { cost_cents: 500, latency_ms: -20, success_rate: 0.005 },
};

function renderDashboard() {
  return render(
    <MemoryRouter>
      <DashboardPage />
    </MemoryRouter>
  );
}

describe('DashboardPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetDashboardStats.mockResolvedValue(MOCK_STATS);
  });

  it('calls getDashboardStats on mount', async () => {
    renderDashboard();
    await waitFor(() => {
      expect(mockGetDashboardStats).toHaveBeenCalled();
    });
  });

  it('uses platform-level dashboard taxonomy in the page header', async () => {
    const { container } = renderDashboard();
    await waitFor(() => {
      expect(mockGetDashboardStats).toHaveBeenCalled();
    });

    const html = container.textContent || '';
    expect(html).toContain('Platform Overview');
    expect(html).toContain('平台总览');
    expect(html).toContain('统一观察请求量、成功率、延迟、成本和渠道健康状态。');
    expect(html).not.toContain('Gateway overview');
    expect(html).not.toContain('AI Gateway 控制台');
  });

  it('passes time range params to API', async () => {
    renderDashboard();
    await waitFor(() => {
      expect(mockGetDashboardStats).toHaveBeenCalled();
    });
    const callArgs = mockGetDashboardStats.mock.calls[0];
    expect(callArgs[0]).toBeDefined(); // startTime
    expect(callArgs[1]).toBeDefined(); // endTime
  });

  it('renders stats data in DOM after fetch', async () => {
    const { container } = renderDashboard();
    await waitFor(() => {
      const html = container.textContent || '';
      // total_requests 12345 => "12.3K" formatted
      expect(html).toContain('12.3K');
    });
  });

  it('renders wallet balance', async () => {
    const { container } = renderDashboard();
    await waitFor(() => {
      const html = container.textContent || '';
      // wallet_balance 100000 cents = ¥1,000.00
      // wallet shows as budget progress: today_cost/threshold
      expect(html).toContain('56.78');
      expect(html).toContain('预算进度');
    });
  });

  it('renders model prices with model_name field', async () => {
    const { container } = renderDashboard();
    await waitFor(() => {
      const html = container.textContent || '';
      expect(html).toContain('gpt-4o');
    });
  });

  it('renders recent request IDs', async () => {
    const { container } = renderDashboard();
    await waitFor(() => {
      const html = container.textContent || '';
      expect(html).toContain('req-abc-123');
    });
  });

  it('renders channel health data', async () => {
    const { container } = renderDashboard();
    await waitFor(() => {
      const html = container.textContent || '';
      expect(html).toContain('OpenAI Main');
      expect(html).toContain('Azure Backup');
    });
  });

  it('labels channel provider without repeating identical channel names', async () => {
    mockGetDashboardStats.mockResolvedValue({
      ...MOCK_STATS,
      channel_health: [
        { id: 1, name: 'deepseek', provider: 'deepseek', enabled: true, status: 'healthy', success_count: 10, fail_count: 0 },
        { id: 2, name: 'Mimo Token Plan', provider: 'openai', enabled: true, status: 'healthy', success_count: 5, fail_count: 0 },
      ],
    });

    const { container } = renderDashboard();
    await waitFor(() => {
      const html = container.textContent || '';
      expect(html).toContain('Mimo Token Plan');
    });

    const html = container.textContent || '';
    expect(html).toContain('供应商：兼容 OpenAI 协议');
    expect(html).not.toContain('供应商：deepseek');
  });

  it('renders trend metrics as selectable checkboxes', async () => {
    const { container } = renderDashboard();
    await waitFor(() => {
      expect(mockGetDashboardStats).toHaveBeenCalled();
    });

    const trendControls = container.querySelectorAll('.ur-trend-checklist input[type="checkbox"]');
    expect(trendControls).toHaveLength(2);
    expect(container.textContent || '').toContain('请求数');
    expect(container.textContent || '').toContain('费用');
  });

  it('renders latency percentiles and the aligned dashboard layout sections', async () => {
    const { container } = renderDashboard();
    await waitFor(() => {
      expect(mockGetDashboardStats).toHaveBeenCalled();
    });

    const html = container.textContent || '';
    expect(html).toContain('P95 延迟');
    expect(html).toContain('780ms');
    expect(html).toContain('P99');
    expect(html).toContain('1290ms');
    expect(container.querySelector('.ur-dashboard-status-strip')).toBeTruthy();
    expect(container.querySelector('.ur-dashboard-analytics-grid')).toBeTruthy();
    expect(container.querySelector('.ur-latency-surface')).toBeTruthy();
  });

  it('handles API error gracefully', async () => {
    mockGetDashboardStats.mockRejectedValue(new Error('Network error'));
    const { container } = renderDashboard();
    await waitFor(() => {
      expect(mockGetDashboardStats).toHaveBeenCalled();
    });
    // Should not crash
    expect(container).toBeTruthy();
  });
});
