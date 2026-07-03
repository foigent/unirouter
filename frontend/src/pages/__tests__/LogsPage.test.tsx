import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import LogsPage from '../LogsPage';

const mockGetLogs = vi.fn();
const mockGetModelNames = vi.fn();
const mockGetChannels = vi.fn();
vi.mock('../../api/client', () => ({
  api: {
    getLogs: (...args: any[]) => mockGetLogs(...args),
    getModelNames: (...args: any[]) => mockGetModelNames(...args),
    getChannels: (...args: any[]) => mockGetChannels(...args),
  },
}));

const MOCK_LOGS_RESPONSE = {
  data: [
    {
      id: 1,
      request_id: 'req-001-aaa',
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

  it('renders request id copy buttons', async () => {
    const { container } = renderLogs();
    await waitFor(() => {
      expect(mockGetLogs).toHaveBeenCalled();
    });
    expect(container.querySelectorAll('button[aria-label="复制 Request ID"]').length).toBeGreaterThan(0);
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
