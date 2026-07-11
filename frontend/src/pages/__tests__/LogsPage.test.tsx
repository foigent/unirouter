import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import LogsPage from '../LogsPage';

const mockGetLogs = vi.fn();
const mockGetModelNames = vi.fn();
const mockGetChannels = vi.fn();
const mockGetLogTrace = vi.fn();
vi.mock('../../api/client', () => ({
  api: {
    getLogs: (...args: any[]) => mockGetLogs(...args),
    getModelNames: (...args: any[]) => mockGetModelNames(...args),
    getChannels: (...args: any[]) => mockGetChannels(...args),
    getLogTrace: (...args: any[]) => mockGetLogTrace(...args),
  },
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
      completion_tokens: 200,
      total_tokens: 700,
      cost_cents: 15,
      is_stream: true,
      ip_address: '1.2.3.4',
      requested_model: 'gpt-4o',
      upstream_status_code: 200,
      upstream_error: '',
      usage_source: 'api',
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
      cost_cents: 2,
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

const MOCK_CHANNELS = {
  data: [
    { provider: 'openai' },
    { provider: 'anthropic' },
  ],
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
    mockGetLogs.mockResolvedValue(MOCK_LOGS_RESPONSE);
    mockGetModelNames.mockResolvedValue(MOCK_MODEL_NAMES);
    mockGetChannels.mockResolvedValue(MOCK_CHANNELS);
    mockGetLogTrace.mockResolvedValue(MOCK_TRACE_RESPONSE);
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

  it('renders log data in DOM after fetch', async () => {
    const { container } = renderLogs();
    await waitFor(() => {
      const html = container.textContent || '';
      expect(html).toContain('req-001-aaa');
      expect(html).toContain('req-002-bbb');
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
      expect(mockGetChannels).toHaveBeenCalled();
    });
    const labels = Array.from(container.querySelectorAll('.ur-filter-label')).map(el => el.textContent || '');
    expect(labels.indexOf('渠道 / 供应商')).toBeGreaterThanOrEqual(0);
    expect(labels.indexOf('模型')).toBeGreaterThanOrEqual(0);
    expect(labels.indexOf('渠道 / 供应商')).toBeLessThan(labels.indexOf('模型'));
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
    for (const placeholder of ['Trace ID（精确匹配）', '上游 Request ID（精确匹配）', 'Evidence Ref（精确匹配）', 'Source Event Hash（精确匹配）']) {
      expect(container.querySelector(`input[placeholder="${placeholder}"]`)).toBeTruthy();
    }
  });

  it('renders request id copy buttons', async () => {
    const { container } = renderLogs();
    await waitFor(() => {
      expect(mockGetLogs).toHaveBeenCalled();
    });
    expect(container.querySelectorAll('button[aria-label="复制 Request ID"]').length).toBeGreaterThan(0);
  });

  it('loads request trace events when opening log detail', async () => {
    const { container } = renderLogs();
    await waitFor(() => {
      expect(container.textContent || '').toContain('req-001-aaa');
    });

    const requestID = await screen.findByText(/req-001-aaa/);
    const row = requestID.closest('tr');
    expect(row).toBeTruthy();
    fireEvent.click(row as HTMLElement);

    await waitFor(() => {
      expect(mockGetLogTrace).toHaveBeenCalledWith(1);
    });
    await waitFor(() => {
      expect(container.textContent || '').toContain('gateway.request.accepted');
    });
  });

  it('renders PRD5 structural trace evidence fields in detail view', async () => {
    const { container } = renderLogs();
    await waitFor(() => {
      expect(container.textContent || '').toContain('req-001-aaa');
    });

    const requestID = await screen.findByText(/req-001-aaa/);
    const row = requestID.closest('tr');
    expect(row).toBeTruthy();
    fireEvent.click(row as HTMLElement);

    await waitFor(() => {
      const html = container.textContent || '';
      expect(html).toContain('证据质量');
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
    await waitFor(() => expect(container.textContent || '').toContain('req-001-aaa'));
    const requestID = await screen.findByText(/req-001-aaa/);
    fireEvent.click(requestID.closest('tr') as HTMLElement);

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
  });

  it('handles API error gracefully', async () => {
    mockGetLogs.mockRejectedValue(new Error('Network error'));
    renderLogs();
    // Should not crash
    await waitFor(() => {
      expect(mockGetLogs).toHaveBeenCalled();
    });
  });

  it('loads model names and channels for filter options on mount', async () => {
    renderLogs();
    await waitFor(() => {
      expect(mockGetModelNames).toHaveBeenCalled();
      expect(mockGetChannels).toHaveBeenCalledWith(1, 100);
    });
  });
});
