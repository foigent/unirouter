import { beforeEach, describe, expect, it, vi } from 'vitest';
import { dashboardApi } from '../dashboard';

const mockFetch = vi.fn();
vi.stubGlobal('fetch', mockFetch);

describe('dashboardApi', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    localStorage.clear();
  });

  it('binds the requested observation window to the canonical customer scope', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      status: 200,
      text: () => Promise.resolve(JSON.stringify({
        total_requests: 0,
        today_cost_micros: '0',
        currency: 'CNY',
        amount_scale: 1_000_000,
        avg_latency_ms: 0,
        p95_latency_ms: 0,
        p99_latency_ms: 0,
        success_rate: 0,
        recent_logs: [],
      })),
    });

    const result = await dashboardApi.getStats(
      '2026-07-15T00:00:00Z',
      '2026-07-16T00:00:00Z',
      22,
    );

    expect(result.total_requests).toBe(0);
    expect(mockFetch).toHaveBeenCalledWith(
      '/api/v1/dashboard/stats?start_time=2026-07-15T00%3A00%3A00Z&end_time=2026-07-16T00%3A00%3A00Z&org_id=22',
      expect.objectContaining({ headers: expect.any(Object) }),
    );
  });
});
