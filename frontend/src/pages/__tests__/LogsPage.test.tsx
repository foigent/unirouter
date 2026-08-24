import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from '../../test/MemoryRouter';
import LogsPage from '../LogsPage';

const mockGetLogs = vi.fn();
const mockGetModelNames = vi.fn();
const mockGetLogTrace = vi.fn();
const mockGetOrganizations = vi.fn();
const auth = vi.hoisted(() => ({ user: null as any }));
vi.mock('../../api/client', () => ({
  api: {
    getLogs: (...args: any[]) => mockGetLogs(...args),
    getModelNames: (...args: any[]) => mockGetModelNames(...args),
    getLogTrace: (...args: any[]) => mockGetLogTrace(...args),
    getOrganizations: (...args: any[]) => mockGetOrganizations(...args),
  },
}));

vi.mock('../../store/auth', () => ({
  useAuthStore: (selector: any) => selector({ user: auth.user }),
}));

const MOCK_LOGS_RESPONSE = {
  data: [
    {
      id: 1,
      request_id: 'req-001-aaa',
      client_request_id: 'client-001',
      workflow_id: 'workflow-001',
      agent_id: 'agent-001',
      attempt_id: 'att-final-001',
      model: 'gpt-4o',
      provider: 'openai',
      channel: 'OpenAI Main',
      status_code: 200,
      latency_ms: 320,
      ttft_ms: 150,
      prompt_tokens: 500,
      uncached_prompt_tokens: 350,
      cache_read_tokens: 100,
      cache_creation_tokens: 50,
      completion_tokens: 200,
      total_tokens: 700,
      cost_micros: '150000',
      is_stream: true,
      ip_address: '1.2.3.4',
      requested_model: 'gpt-4o',
      upstream_status_code: 200,
      upstream_error: '',
      usage_source: 'api',
      cache_usage_schema: 'anthropic.prompt_cache.v1',
      cache_usage_status: 'cache_read_write_reported',
      pricing_version: 'pv2_test',
      cache_read_ratio: 0.1,
      cache_creation_ratio: 1.25,
      effective_input_price_micros_per_million: '50000000',
      effective_cache_read_price_micros_per_million: '5000000',
      effective_cache_creation_price_micros_per_million: '62500000',
      effective_output_price_micros_per_million: '150000000',
      cache_read_cost_micros: '500',
      cache_creation_cost_micros: '3125',
      created_at: '2026-06-29T10:00:00Z',
    },
    {
      id: 2,
      request_id: 'req-002-bbb',
      model: 'claude-3.5-sonnet',
      provider: 'anthropic',
      channel: 'Anthropic Direct',
      status_code: 500,
      latency_ms: 2500,
      ttft_ms: 0,
      prompt_tokens: 100,
      completion_tokens: 0,
      total_tokens: 100,
      cost_micros: '20000',
      is_stream: false,
      ip_address: '5.6.7.8',
      requested_model: 'claude-3.5-sonnet',
      upstream_status_code: 500,
      upstream_error: 'Internal server error from upstream',
      usage_source: 'api',
      created_at: '2026-06-29T09:30:00Z',
    },
  ],
  total: 2,
};

const MOCK_MODEL_NAMES = {
  data: ['gpt-4o', 'claude-3.5-sonnet'],
};

const MOCK_TRACE_RESPONSE = {
  request_id: 'req-001-aaa',
  events: [
    {
      id: 1,
      event_type: 'gateway.request.accepted',
      stage: 'gateway',
      severity: 'info',
      model: 'gpt-4o',
      requested_model: 'gpt-4o',
      provider: '',
      channel: '',
      status_code: null,
      latency_ms: 2,
      metadata: { estimated_tokens: 700, stream: true },
      created_at: '2026-06-29T10:00:01Z',
      trace_id: 'req-001-aaa',
      span_id: 'span-001-gateway',
      parent_span_id: null,
      schema_version: 'unirouter.evidence.v1',
      source_component: 'gateway',
      capture_mode: 'native',
      capture_gap: null,
      evidence_ref: 'unirouter:request_trace_events:1',
      upstream_request_id: 'upstream-001',
      decision_kind: 'admission',
      decision_result: 'accepted',
      decision_reason: 'request accepted',
      actor_type: 'api_key',
      actor_ref: 'key_123',
      on_behalf_of: null,
      session_id: 'session-001',
      client_request_id: 'client-001',
      workflow_id: 'workflow-001',
      agent_id: 'agent-001',
      payload_capture_mode: 'metadata_only',
      payload_ref: null,
      source_event_hash: 'hash-001',
    },
    {
      id: 2, event_type: 'routing.route_selected', stage: 'routing', severity: 'info', model: 'gpt-4o', requested_model: 'gpt-4o',
      provider: 'openai', channel: 'primary', status_code: null, latency_ms: 3, attempt_id: 'att-primary', metadata: { mapped_model: 'gpt-4o' }, created_at: '2026-06-29T10:00:02Z',
    },
    {
      id: 3, event_type: 'provider.request_failed', stage: 'provider', severity: 'warning', model: 'gpt-4o', requested_model: 'gpt-4o',
      provider: 'openai', channel: 'primary', status_code: null, latency_ms: 100, attempt_id: 'att-primary', metadata: {}, created_at: '2026-06-29T10:00:03Z',
    },
    {
      id: 4, event_type: 'routing.route_selected', stage: 'routing', severity: 'info', model: 'gpt-4o', requested_model: 'gpt-4o',
      provider: 'azure', channel: 'secondary', status_code: null, latency_ms: 104, attempt_id: 'att-secondary', metadata: { mapped_model: 'gpt-4o' }, created_at: '2026-06-29T10:00:04Z',
    },
    {
      id: 5, event_type: 'provider.response_accepted', stage: 'provider', severity: 'info', model: 'gpt-4o', requested_model: 'gpt-4o',
      provider: 'azure', channel: 'secondary', status_code: 200, latency_ms: 220, attempt_id: 'att-secondary', metadata: {}, created_at: '2026-06-29T10:00:05Z',
    },
    {
      id: 6, event_type: 'billing.settle.succeeded', stage: 'billing', severity: 'info', model: 'gpt-4o', requested_model: 'gpt-4o',
      provider: 'azure', channel: 'secondary', status_code: 200, latency_ms: 240, attempt_id: 'att-secondary',
      metadata: {
        billing_status: 'settled',
        usage_source: 'upstream',
        usage_confidence: 1,
        actual_cost_micros: '150000',
        charged_cost_micros: '150000',
        money_schema: 'micro-cny.v1',
        pricing_version: 'pv2_test',
      },
      capture_gap: 'redacted',
      decision_kind: 'billing',
      decision_result: 'allowed',
      decision_reason: 'billing.settled',
      created_at: '2026-06-29T10:00:06Z',
    },
  ],
};

function renderLogs() {
  return render(
    <MemoryRouter>
      <LogsPage />
    </MemoryRouter>
  );
}

describe('LogsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    window.history.replaceState(null, '', '/logs');
    auth.user = null;
    mockGetLogs.mockResolvedValue(MOCK_LOGS_RESPONSE);
    mockGetModelNames.mockResolvedValue(MOCK_MODEL_NAMES);
    mockGetLogTrace.mockResolvedValue(MOCK_TRACE_RESPONSE);
    mockGetOrganizations.mockResolvedValue({ data: [{ id: 7, name: '总部' }], total: 1 });
  });

  it('fetches logs with default page params', async () => {
    renderLogs();
    await waitFor(() => {
      expect(mockGetLogs).toHaveBeenCalled();
    });
    const [page, pageSize] = mockGetLogs.mock.calls[0];
    expect(page).toBe(1);
    expect(pageSize).toBe(50);
  });

  it('uses the canonical customer as the explicit platform request scope', async () => {
    auth.user = {
      role: 'platform_admin',
      org_id: 99,
      customer_context: { state: 'ready', org_id: 7, org_name: '总部' },
      identity_scope: 'deployment_operator',
    };
    renderLogs();

    expect(await screen.findByText('客户范围：总部')).toBeTruthy();
    expect(screen.queryByLabelText('目标组织')).toBeNull();
    await waitFor(() => expect(mockGetLogs).toHaveBeenCalledWith(1, 50, expect.objectContaining({ org_id: 7 })));
    expect(mockGetOrganizations).not.toHaveBeenCalled();
  });

  it('does not query execution records when platform customer context is unavailable', () => {
    auth.user = { role: 'platform_admin', org_id: 99, customer_context: { state: 'unavailable' } };
    renderLogs();

    expect(screen.getAllByText('无法确认客户组织上下文').length).toBeGreaterThanOrEqual(1);
    expect(mockGetLogs).not.toHaveBeenCalled();
  });

  it('renders log data in DOM after fetch', async () => {
    const { container } = renderLogs();
    await waitFor(() => {
      const html = container.textContent || '';
      expect(html).toContain('req-001-aaa');
      expect(html).toContain('req-002-bbb');
      expect(html).toContain('执行决策凭证');
    });
  });

  it('displays model info in rendered output', async () => {
    const { container } = renderLogs();
    await waitFor(() => {
      const html = container.textContent || '';
      expect(html).toContain('gpt-4o');
    });
  });

  it('renders absolute timestamps instead of relative time text', async () => {
    const { container } = renderLogs();
    await waitFor(() => {
      const html = container.textContent || '';
      expect(html).toContain('2026-06-29');
      expect(html).not.toContain('分钟前');
    });
  });

  it('places provider filter before model filter', async () => {
    const { container } = renderLogs();
    await waitFor(() => {
      expect(mockGetModelNames).toHaveBeenCalled();
    });
    const labels = Array.from(container.querySelectorAll('.ur-filter-label')).map(el => el.textContent || '');
    expect(labels.indexOf('供应商')).toBeGreaterThanOrEqual(0);
    expect(labels.indexOf('模型')).toBeGreaterThanOrEqual(0);
    expect(labels.indexOf('供应商')).toBeLessThan(labels.indexOf('模型'));
    expect(screen.getByText('选择供应商')).toBeTruthy();
  });

  it('builds provider and model dropdowns from actual requests when the price catalog is empty', async () => {
    mockGetModelNames.mockResolvedValueOnce({ data: [] });
    renderLogs();

    await waitFor(() => expect(mockGetLogs).toHaveBeenCalled());
    fireEvent.click(screen.getByText('选择供应商'));
    await waitFor(() => expect(screen.getAllByText('openai').length).toBeGreaterThan(0));
    expect(screen.getAllByText('anthropic').length).toBeGreaterThan(0);

    fireEvent.click(screen.getByText('模型名称'));
    await waitFor(() => expect(screen.getAllByText('gpt-4o').length).toBeGreaterThan(0));
    expect(screen.getAllByText('claude-3.5-sonnet').length).toBeGreaterThan(0);
  });

  it('renders usage source filter for metering drilldown', async () => {
    const { container } = renderLogs();
    await waitFor(() => {
      expect(mockGetLogs).toHaveBeenCalled();
    });
    const labels = Array.from(container.querySelectorAll('.ur-filter-label')).map(el => el.textContent || '');
    expect(labels).toContain('计量来源');
  });

  it('renders exact structural evidence filters', async () => {
    const { container } = renderLogs();
    await waitFor(() => {
      expect(mockGetLogs).toHaveBeenCalled();
    });
    const labels = Array.from(container.querySelectorAll('.ur-filter-label')).map(el => el.textContent || '');
    expect(labels).toContain('证据标识');
    for (const placeholder of ['Trace ID（精确匹配）', '上游 Request ID（精确匹配）', '证据引用（精确匹配）', '来源事件哈希（精确匹配）']) {
      expect(container.querySelector(`input[placeholder="${placeholder}"]`)).toBeTruthy();
    }
    expect(screen.getByRole('button', { name: '刷新执行请求' })).toBeTruthy();
    expect(screen.getByRole('textbox', { name: '搜索执行请求' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '供应商' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '模型' })).toBeTruthy();
    expect(screen.getByRole('textbox', { name: 'Trace ID 精确匹配' })).toBeTruthy();
  });

  it('renders request id copy buttons', async () => {
    const { container } = renderLogs();
    await waitFor(() => {
      expect(mockGetLogs).toHaveBeenCalled();
    });
    expect(container.querySelectorAll('button[aria-label="复制 Request ID"]').length).toBeGreaterThan(0);
  });

  it('keeps the receipt inspector visible and automatically loads the first request trace', async () => {
    const { container } = renderLogs();
    await waitFor(() => {
      expect(mockGetLogTrace).toHaveBeenCalledWith(1);
    });
    await waitFor(() => {
      const html = container.textContent || '';
      expect(html).toContain('执行决策凭证');
      expect(html).toContain('request accepted');
      expect(html).toContain('api_key:key_123');
      expect(window.location.search).toContain('log_id=1');
    });
    expect(container.querySelector('.ur-explorer-detail')).toBeTruthy();
    expect(container.querySelector('.semi-icon-close')).toBeFalsy();
  });

  it('shows authoritative provider prompt-cache usage separately from response caching', async () => {
    const { container } = renderLogs();
    await waitFor(() => expect(mockGetLogTrace).toHaveBeenCalledWith(1));
    fireEvent.click(screen.getByRole('button', { name: '成本与终态' }));

    await waitFor(() => {
      const html = container.textContent || '';
      expect(html).toContain('财务终态');
      expect(html).toContain('settled');
      expect(html).toContain('实际成本¥0.150000');
      expect(html).toContain('实际扣费¥0.150000');
      expect(html).toContain('micro-cny.v1');
      expect(html).toContain('当前脱敏 Trace 合同不暴露');
      expect(html).toContain('供应商 Prompt Cache');
      expect(html).toContain('已回传缓存读/写');
      expect(html).toContain('普通输入350');
      expect(html).toContain('缓存读取100');
      expect(html).toContain('缓存写入50');
      expect(html).toContain('读取倍率0.100×');
      expect(html).toContain('写入倍率1.250×');
      expect(html).toContain('读取费用¥0.000500');
      expect(html).toContain('写入费用¥0.003125');
      expect(html).toContain('未回传”不会推测命中');
    });
  });

  it('does not infer financial finality from an HTTP success or log cost', async () => {
    mockGetLogTrace.mockResolvedValue({
      ...MOCK_TRACE_RESPONSE,
      events: MOCK_TRACE_RESPONSE.events.filter(event => !event.event_type.startsWith('billing.')),
    });
    const { container } = renderLogs();
    await waitFor(() => expect(mockGetLogTrace).toHaveBeenCalledWith(1));
    fireEvent.click(screen.getByRole('button', { name: '成本与终态' }));

    await waitFor(() => {
      const html = container.textContent || '';
      expect(html).toContain('终态not captured');
      expect(html).toContain('HTTP 成功和日志费用不会被当作财务终态');
      expect(html).not.toContain('终态settled');
    });
  });

  it('preserves the exact cancelled no-billable-output terminal status', async () => {
    mockGetLogTrace.mockResolvedValue({
      ...MOCK_TRACE_RESPONSE,
      events: [
        ...MOCK_TRACE_RESPONSE.events.filter(event => !event.event_type.startsWith('billing.')),
        {
          id: 7,
          event_type: 'billing.cancelled.no_billable_output',
          stage: 'billing',
          severity: 'info',
          model: 'gpt-4o',
          requested_model: 'gpt-4o',
          provider: 'azure',
          channel: 'secondary',
          status_code: 200,
          latency_ms: 240,
          metadata: { billing_status: 'cancelled_no_billable_output' },
          created_at: '2026-06-29T10:00:06Z',
        },
      ],
    });
    const { container } = renderLogs();
    await waitFor(() => expect(mockGetLogTrace).toHaveBeenCalledWith(1));
    fireEvent.click(screen.getByRole('button', { name: '成本与终态' }));

    await waitFor(() => {
      const html = container.textContent || '';
      expect(html).toContain('终态cancelled_no_billable_output');
      expect(html).toContain('billing.cancelled.no_billable_output');
      expect(html).not.toContain('终态settled');
    });
  });

  it('does not present evidence as missing while the trace is still loading', async () => {
    mockGetLogTrace.mockReturnValue(new Promise(() => {}));
    const { container } = renderLogs();

    await waitFor(() => expect(mockGetLogTrace).toHaveBeenCalledWith(1));
    const html = container.textContent || '';
    expect(html).toContain('执行证据加载中');
    expect(html).toContain('加载完成前不作缺失或终态判断');
    expect(html).not.toContain('not captured');
    expect(html).not.toContain('执行证据不可用');
  });

  it('distinguishes unavailable trace evidence from successfully observed missing evidence', async () => {
    mockGetLogTrace
      .mockRejectedValueOnce(new Error('trace unavailable'))
      .mockResolvedValueOnce(MOCK_TRACE_RESPONSE);
    const { container } = renderLogs();

    await waitFor(() => {
      const html = container.textContent || '';
      expect(html).toContain('执行证据不可用');
      expect(html).toContain('不可用');
      expect(html).not.toContain('not captured');
    });

    fireEvent.click(screen.getByRole('button', { name: '重试证据' }));
    await waitFor(() => expect(mockGetLogTrace).toHaveBeenCalledTimes(2));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: '执行决策' })).toBeTruthy();
      expect(container.textContent || '').toContain('request accepted');
    });
  });

  it('renders PRD5 structural trace evidence fields in detail view', async () => {
    const { container } = renderLogs();
    await waitFor(() => expect(mockGetLogTrace).toHaveBeenCalledWith(1));
    fireEvent.click(screen.getByRole('button', { name: '来源与缺口' }));

    await waitFor(() => {
      const html = container.textContent || '';
      expect(html).toContain('来源证据摘要');
      expect(html).toContain('capture: native');
      expect(html).toContain('payload: metadata_only');
      expect(html).toContain('decision: admission / accepted / request accepted');
      expect(html).toContain('actor: api_key:key_123');
      expect(html).toContain('upstream: upstream-001');
      expect(html).toContain('client: client-001');
      expect(html).toContain('workflow: workflow-001');
      expect(html).toContain('agent: agent-001');
    });
  });

  it('summarizes route attempts and failover without changing routing semantics', async () => {
    const { container } = renderLogs();
    await waitFor(() => expect(mockGetLogTrace).toHaveBeenCalledWith(1));
    fireEvent.click(screen.getByRole('button', { name: '路由尝试' }));

    await waitFor(() => {
      const html = container.textContent || '';
      expect(html).toContain('路由与故障切换');
      expect(html).toContain('路由尝试2 次');
      expect(html).toContain('故障切换1 次');
      expect(html).toContain('openai / primary');
      expect(html).toContain('azure / secondary');
      expect(html).toContain('att-primary');
      expect(html).toContain('att-secondary');
      expect(html).toContain('上游接受');
    });
  });

  it('renders search input with correct placeholder', async () => {
    renderLogs();
    await waitFor(() => {
      const input = document.querySelector('input[placeholder*="Request"]') ||
                    document.querySelector('input[placeholder*="request"]') ||
                    document.querySelector('input[placeholder*="模型"]');
      expect(input).toBeTruthy();
    });
  });

  it('handles empty log response', async () => {
    mockGetLogs.mockResolvedValue({ data: [], total: 0 });
    const { container } = renderLogs();
    await waitFor(() => {
      expect(mockGetLogs).toHaveBeenCalled();
    });
    const html = container.textContent || '';
    expect(html).not.toContain('req-001-aaa');
    expect(html).toContain('暂无可检查的执行');
    expect(container.querySelector('.ur-explorer-detail')).toBeTruthy();
  });

  it('switches the persistent inspector with keyboard navigation', async () => {
    const { container } = renderLogs();
    await waitFor(() => expect(mockGetLogTrace).toHaveBeenCalledWith(1));

    fireEvent.keyDown(window, { key: 'ArrowDown' });

    await waitFor(() => expect(mockGetLogTrace).toHaveBeenCalledWith(2));
    const currentRequest = container.querySelector('.ur-request-id-full');
    expect(currentRequest?.textContent || '').toContain('req-002-bbb');
    expect(window.location.search).toContain('log_id=2');
  });

  it('does not hijack arrow keys from filter controls', async () => {
    renderLogs();
    await waitFor(() => expect(mockGetLogTrace).toHaveBeenCalledWith(1));

    const filterControl = screen.getAllByRole('combobox')[0];
    fireEvent.keyDown(filterControl, { key: 'ArrowDown' });

    expect(mockGetLogTrace).not.toHaveBeenCalledWith(2);
    expect(window.location.search).toContain('log_id=1');
  });

  it('restores a selected request from the log_id URL deep link', async () => {
    window.history.replaceState(null, '', '/logs?log_id=2');
    const { container } = renderLogs();

    await waitFor(() => expect(mockGetLogTrace).toHaveBeenCalledWith(2));
    const currentRequest = container.querySelector('.ur-request-id-full');
    expect(currentRequest?.textContent || '').toContain('req-002-bbb');
    expect(window.location.search).toContain('log_id=2');
  });

  it('does not preserve stale receipts when the filtered log list is unavailable', async () => {
    mockGetLogs.mockRejectedValueOnce(new Error('Network error'));
    const { container } = renderLogs();

    await waitFor(() => expect(container.textContent || '').toContain('请求列表不可用'));
    expect(container.textContent || '').toContain('不会保留旧凭证');
    expect(container.textContent || '').not.toContain('req-001-aaa');

    fireEvent.click(screen.getByRole('button', { name: '重试列表' }));
    await waitFor(() => expect(mockGetLogs).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(container.textContent || '').toContain('req-001-aaa'));
  });

  it('loads model names without requesting platform-only channel data', async () => {
    renderLogs();
    await waitFor(() => {
      expect(mockGetModelNames).toHaveBeenCalled();
    });
  });
});
