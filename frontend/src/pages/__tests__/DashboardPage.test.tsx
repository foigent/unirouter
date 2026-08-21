import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import DashboardPage from '../DashboardPage';
import { useAuthStore } from '../../store/auth';
import type { LicenseAccess } from '../../navigation';

const PARTIAL_LICENSE: LicenseAccess = {
  status: 'active',
  enforceable: true,
  features: {
    gateway: true,
    agent_capabilities: false,
    oidc: false,
    guardrails: true,
    notifications: true,
    model_governance: true,
    data_exchange: false,
  },
};

const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return { ...actual, useNavigate: () => mockNavigate };
});

const mockGetDashboardStats = vi.fn();
const mockGetOrganizations = vi.fn();
const mockGetInternalStatus = vi.fn();
vi.mock('../../api/client', () => ({
  api: {
    getOrganizations: (...args: any[]) => mockGetOrganizations(...args),
    getInternalStatus: (...args: any[]) => mockGetInternalStatus(...args),
  },
}));
vi.mock('../../api/dashboard', () => ({
  dashboardApi: {
    getStats: (...args: unknown[]) => mockGetDashboardStats(...args),
  },
}));

const MOCK_STATS = {
  total_requests: 12345,
  today_cost_micros: '56780000',
  currency: 'CNY',
  amount_scale: 1_000_000,
  avg_latency_ms: 320,
  p95_latency_ms: 780,
  p99_latency_ms: 1290,
  success_rate: 0.987,
  wallet_balance_micros: '1000000000',
  wallet_granted_micros: '200000000',
  wallet_frozen_micros: '50000000',
  wallet_threshold_micros: '10000000',
  recent_logs: [
    {
      request_id: 'req-abc-123',
      model: 'gpt-4o',
      routing_model: 'gpt-4o',
      route_deployment_id: 1,
      route_model_operation_id: 'openai.chat_completions.v1',
      route_identity: 'canonical_deployment_operation',
      provider: 'openai',
      channel: 'OpenAI Main',
      status_code: 200,
      latency_ms: 450,
      total_tokens: 1500,
      cost_micros: '300000',
      created_at: '2026-06-29T10:00:00Z',
      is_stream: false,
    },
  ],
  model_prices: [
    { model_name: 'gpt-4o', input_price_cny_per_million: '1000', output_price_cny_per_million: '2000' },
  ],
  daily_trend: [
    { date: '2026-06-28', requests: 5000, cost_micros: '20000000' },
    { date: '2026-06-29', requests: 7345, cost_micros: '36780000' },
  ],
  model_distribution: [
    { model: 'gpt-4o', requests: 8000, success_count: 7980, fail_count: 20, success_rate: 0.9975, p95_latency_ms: 620 },
    { model: 'claude-3.5-sonnet', requests: 4345, success_count: 4200, fail_count: 145, success_rate: 0.9666, p95_latency_ms: 980 },
  ],
  channel_health: [
    { id: 1, deployment_id: 1, model_operation_id: 'openai.chat_completions.v1', name: 'OpenAI Main', provider: 'openai', base_url: 'https://api.openai.com/v1', public_model: 'gpt-4o', public_models: ['gpt-4o'], enabled: true, available: true, credential_ready: true, status: 'healthy', success_count: 10000, fail_count: 50 },
    { id: 2, deployment_id: 2, model_operation_id: 'openai.chat_completions.v1', name: 'Azure Backup', provider: 'azure', public_model: 'gpt-4o', enabled: true, available: true, credential_ready: true, status: 'warning', success_count: 2000, fail_count: 300 },
    { id: 3, deployment_id: 3, model_operation_id: 'openai.chat_completions.v1', name: 'Disabled CH', provider: 'custom', public_model: 'gpt-4o', enabled: false, available: false, credential_ready: false, status: 'disabled', success_count: 0, fail_count: 0 },
  ],
  delta_vs_yesterday: { cost_micros: '5000000', latency_ms: -20, success_rate: 0.005 },
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
    mockGetOrganizations.mockResolvedValue({
      data: [{ id: 1, name: '北区组织' }, { id: 2, name: '南区组织' }],
      total: 2,
    });
    mockGetInternalStatus.mockResolvedValue({
      summary_kind: 'notification_events',
      attention_level: 'warning',
      open_critical: 1,
      open_warning: 2,
    });
    useAuthStore.setState({
      token: 'token',
      isAuthenticated: true,
      profileLoading: false,
      profileError: null,
      user: {
        id: 1, username: 'admin', role: 'platform_admin', org_id: 99, org_name: '技术组织',
        customer_context: { state: 'ready', org_id: 1, org_name: '北区组织' },
        identity_scope: 'deployment_operator',
      },
    });
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
    expect(html).toContain('概览');
    expect(html).toContain('客户运营总览');
    expect(html).toContain('数据范围：北区组织');
    expect(html).toContain('统一观察当前客户的请求量、成功率、延迟、成本和模型部署健康状态。');
    expect(html).not.toContain('Gateway overview');
    expect(html).not.toContain('AI Gateway 控制台');
  });

  it('summarizes verifiable risks and contextual actions without duplicating the navigation taxonomy', async () => {
    renderDashboard();

    expect(await screen.findByText('运行关注与待处理动作')).toBeTruthy();
    expect(mockGetInternalStatus).toHaveBeenCalledWith({ org_id: 1 });
    expect(screen.getByText('运行信号')).toBeTruthy();
    expect(screen.getByText('需要关注')).toBeTruthy();
    expect(screen.getByText(/异常部署操作 1 · 失败请求 350/)).toBeTruthy();
    expect(screen.getByText('预警 · 3')).toBeTruthy();
    expect(screen.getByText('严重 1 · 预警 2')).toBeTruthy();
    expect(screen.getByText('结算风险')).toBeTruthy();
    expect(screen.getByRole('button', { name: '查看运行状态' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '处理通知' })).toBeTruthy();
    expect(screen.queryByText('任务入口')).toBeNull();
  });

  it('does not turn missing traffic or a failed notification summary into a healthy result', async () => {
    mockGetDashboardStats.mockResolvedValue({
      ...MOCK_STATS,
      total_requests: 0,
      success_rate: 0,
      channel_health: [],
      recent_logs: [],
    });
    mockGetInternalStatus.mockRejectedValue(new Error('unavailable'));

    renderDashboard();

    expect(await screen.findByText('暂无可判定样本')).toBeTruthy();
    expect(await screen.findByText('读取失败')).toBeTruthy();
    expect(screen.getByText('不以缺失或读取失败冒充零事件')).toBeTruthy();
    expect(screen.queryByText('当前窗口正常')).toBeNull();
  });

  it('uses the canonical customer as the explicit dashboard scope without an organization selector', async () => {
    renderDashboard();

    await waitFor(() => expect(mockGetDashboardStats).toHaveBeenCalledWith(expect.any(String), expect.any(String), 1));
    expect(mockGetOrganizations).not.toHaveBeenCalled();
    expect(screen.queryByText('目标组织')).toBeNull();
    expect(screen.queryByRole('combobox', { name: '目标组织' })).toBeNull();
    expect(await screen.findByText(/数据范围：北区组织/)).toBeTruthy();
  });

  it('keeps an empty recent-request table stable in the canonical customer scope', async () => {
    mockGetDashboardStats.mockResolvedValue({ ...MOCK_STATS, recent_logs: [] });
    renderDashboard();

    expect(await screen.findByText('暂无请求记录')).toBeTruthy();
    await waitFor(() => expect(mockGetDashboardStats).toHaveBeenCalledWith(expect.any(String), expect.any(String), 1));
    expect(await screen.findByText(/数据范围：北区组织/)).toBeTruthy();
    expect(screen.getByText('暂无请求记录')).toBeTruthy();
  });

  it('blocks dashboard reads when the deployment has no unique customer context', () => {
    useAuthStore.setState({
      user: {
        id: 1, username: 'admin', role: 'platform_admin', org_id: 99,
        customer_context: { state: 'ambiguous' }, identity_scope: 'deployment_operator',
      },
    });

    renderDashboard();

    expect(screen.getByRole('heading', { name: '客户组织上下文不唯一' })).toBeTruthy();
    expect(mockGetDashboardStats).not.toHaveBeenCalled();
    expect(mockGetInternalStatus).not.toHaveBeenCalled();
  });

  it('does not duplicate the sidebar task taxonomy inside the dashboard', async () => {
    useAuthStore.setState({
      user: { id: 2, username: 'billing', role: 'billing_admin', org_id: 1, org_name: '北区组织' },
    });

    renderDashboard();

    expect(await screen.findByText('财务与用量总览')).toBeTruthy();
    expect(screen.queryByText('任务入口')).toBeNull();
    expect(screen.queryByRole('button', { name: '预算与成本中心' })).toBeNull();
    expect(screen.queryByRole('button', { name: '分摊与对账' })).toBeNull();
    expect(screen.queryByRole('button', { name: '上游通道' })).toBeNull();
    expect(screen.queryByRole('button', { name: '用户管理' })).toBeNull();
    expect(screen.getByText('结算可用额度')).toBeTruthy();
    expect(screen.queryByText('当前可路由部署操作')).toBeNull();
    expect(screen.queryByText('模型部署可用状态')).toBeNull();
    expect(screen.queryByText('运行关注与待处理动作')).toBeNull();
    expect(mockGetInternalStatus).not.toHaveBeenCalled();
  });

  it('does not reproduce licensed navigation controls as dashboard task cards', async () => {
    useAuthStore.setState({
      user: {
        id: 2,
        username: 'org-admin',
        role: 'org_admin',
        org_id: 1,
        org_name: '北区组织',
        license_access: PARTIAL_LICENSE,
      },
    });

    renderDashboard();

    expect(await screen.findByText('组织运营总览')).toBeTruthy();
    expect(screen.queryByRole('button', { name: '模型治理（授权未包含）' })).toBeNull();
    expect(screen.queryByRole('button', { name: '内容安全（授权未包含）' })).toBeNull();
    expect(screen.queryByRole('button', { name: /X-Insight 数据交换/ })).toBeNull();
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

  it('uses the wallet threshold as a low-balance threshold rather than a spend budget', async () => {
    const { container } = renderDashboard();
    await waitFor(() => {
      const html = container.textContent || '';
      expect(html).toContain('结算可用额度');
      expect(html).toContain('¥1200.00');
      expect(html).toContain('余额正常');
      expect(html).toContain('阈值 ¥10.00');
      expect(html).not.toContain('预算进度');
    });
  });

  it('marks the low-balance threshold as triggered when available balance falls below it', async () => {
    mockGetDashboardStats.mockResolvedValue({
      ...MOCK_STATS,
      wallet_balance_micros: '1000000',
      wallet_granted_micros: '0',
      wallet_threshold_micros: '10000000',
    });

    renderDashboard();

    expect(await screen.findByText('已触发低余额')).toBeTruthy();
  });

  it('labels selected-window cost and hides the fixed 24h comparison for longer ranges', async () => {
    renderDashboard();

    expect(await screen.findByText('当前窗口费用')).toBeTruthy();
    expect(screen.getAllByText('较前 24h').length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole('button', { name: '7 天' }));

    await waitFor(() => expect(mockGetDashboardStats).toHaveBeenCalledTimes(2));
    expect(screen.queryByText('较前 24h')).toBeNull();
    expect(screen.getByText('按所选时间窗口累计')).toBeTruthy();
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
    renderDashboard();
    const openAIConnection = await screen.findByRole('button', { name: '查看模型部署 OpenAI Main' });
    expect(openAIConnection).toBeTruthy();
    expect(screen.getByText('Azure Backup')).toBeTruthy();
    expect(screen.getByText('模型：gpt-4o · api.openai.com')).toBeTruthy();
    expect(screen.getByText('当前可路由部署操作')).toBeTruthy();
    expect(screen.getByText('2/3')).toBeTruthy();
    fireEvent.click(openAIConnection);
    expect(mockNavigate).toHaveBeenCalledWith('/model-endpoints?deployment_id=1&operation_id=openai.chat_completions.v1');
  });

  it('shows no sample instead of a zero success rate for a healthy idle connection', async () => {
    mockGetDashboardStats.mockResolvedValue({
      ...MOCK_STATS,
      channel_health: [{
        id: 4,
        deployment_id: 4,
        model_operation_id: 'openai.chat_completions.v1',
        name: 'Idle healthy endpoint',
        provider: 'deepseek',
        public_model: 'deepseek-chat',
        enabled: true,
        available: true,
        credential_ready: true,
        status: 'healthy',
        success_count: 0,
        fail_count: 0,
      }],
    });

    renderDashboard();

    const card = await screen.findByRole('button', { name: '查看模型部署 Idle healthy endpoint' });
    expect(card.textContent).toContain('暂无样本');
    expect(card.textContent).not.toContain('0.0%');
    expect(card.textContent).toContain('健康');
  });

  it('shows the immutable canonical deployment operation on each recent request', async () => {
    renderDashboard();

    expect(await screen.findByText('部署 #1 · openai.chat_completions.v1')).toBeTruthy();
    expect(screen.queryByText('未选择路由')).toBeNull();
  });

  it('labels route_channel_id as legacy history instead of current deployment identity', async () => {
    mockGetDashboardStats.mockResolvedValue({
      ...MOCK_STATS,
      recent_logs: [{
        ...MOCK_STATS.recent_logs[0],
        route_deployment_id: undefined,
        route_model_operation_id: undefined,
        route_identity: 'legacy_history',
        route_channel_id: 77,
        channel: 'Archived channel',
      }],
    });

    renderDashboard();

    expect(await screen.findByText('历史连接：Archived channel')).toBeTruthy();
    expect(screen.queryByText(/部署 #77/)).toBeNull();
  });

  it('separates current route readiness from historical window results', async () => {
    mockGetDashboardStats.mockResolvedValue({
      ...MOCK_STATS,
      channel_health: [{
        id: 9, deployment_id: 9, model_operation_id: 'openai.chat_completions.v1',
        name: 'Kimi', provider: 'kimi', public_model: 'kimi-k3', enabled: true, available: false,
        credential_ready: false, runtime_published: true, status: 'no_credential',
        success_count: 1, fail_count: 0,
      }],
    });

    renderDashboard();

    expect(await screen.findByText('缺少凭证')).toBeTruthy();
    expect(screen.getByText('0/1')).toBeTruthy();
    expect(screen.getByText('100.0%')).toBeTruthy();
    expect(screen.getByText('按 canonical deployment + operation 判定')).toBeTruthy();
  });

  it('explains stale and unavailable route projections instead of showing healthy', async () => {
    mockGetDashboardStats.mockResolvedValue({
      ...MOCK_STATS,
      channel_health: [
        {
          id: 9, deployment_id: 9, model_operation_id: 'openai.chat_completions.v1',
          name: 'Kimi stale', provider: 'kimi', public_model: 'kimi-k3', enabled: false, available: false,
          credential_ready: true, runtime_published: false, status: 'stale_release',
          success_count: 1, fail_count: 0,
        },
        {
          id: 10, deployment_id: 10, model_operation_id: 'openai.chat_completions.v1',
          name: 'DeepSeek unknown', provider: 'deepseek', public_model: 'deepseek-chat', enabled: true, available: false,
          credential_ready: true, runtime_published: true, status: 'projection_unavailable',
          success_count: 3, fail_count: 0,
        },
      ],
    });

    renderDashboard();

    expect(await screen.findByText('发布记录已过期')).toBeTruthy();
    expect(screen.getByText('路由状态不可判定')).toBeTruthy();
    expect(screen.getByText('0/2')).toBeTruthy();
  });

  it('labels channel provider without repeating identical channel names', async () => {
    mockGetDashboardStats.mockResolvedValue({
      ...MOCK_STATS,
      channel_health: [
        { id: 1, deployment_id: 1, model_operation_id: 'openai.chat_completions.v1', name: 'deepseek', provider: 'deepseek', public_model: 'deepseek-chat', enabled: true, available: true, status: 'healthy', success_count: 10, fail_count: 0 },
        { id: 2, deployment_id: 2, model_operation_id: 'openai.chat_completions.v1', name: 'Mimo Token Plan', provider: 'openai', public_model: 'mimo-chat', enabled: true, available: true, status: 'healthy', success_count: 5, fail_count: 0 },
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

  it('shows an explicit retry state instead of rendering failed data as zeros', async () => {
    mockGetDashboardStats.mockRejectedValue(new Error('Network error'));
    renderDashboard();

    expect(await screen.findByText('总览数据加载失败')).toBeTruthy();
    expect(screen.getByRole('button', { name: '重新加载总览' })).toBeTruthy();
    expect(screen.queryByText('总请求数')).toBeNull();
  });

  it('does not offer a dead log-detail interaction to organization viewers', async () => {
    useAuthStore.setState({
      user: { id: 3, username: 'reader', role: 'org_viewer', org_id: 1, org_name: '北区组织' },
    });

    renderDashboard();

    expect(await screen.findByText('组织只读总览')).toBeTruthy();
    expect(screen.getByText(/当前只读角色不提供日志明细入口/)).toBeTruthy();
    expect(screen.queryByText('结算可用额度')).toBeNull();
    expect(screen.queryByText('当前可路由部署操作')).toBeNull();
    expect(screen.queryByText('模型部署可用状态')).toBeNull();
    fireEvent.click(screen.getByText(/req-abc-123/));
    expect(mockNavigate).not.toHaveBeenCalled();
  });
});
