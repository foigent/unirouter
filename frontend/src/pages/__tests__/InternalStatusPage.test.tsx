import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import InternalStatusPage from '../InternalStatusPage';

const mockGetInternalStatus = vi.fn();
const mockGetRuntimeStatus = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getInternalStatus: (...args: any[]) => mockGetInternalStatus(...args),
    getRuntimeStatus: (...args: any[]) => mockGetRuntimeStatus(...args),
  },
}));

vi.mock('../../store/auth', () => ({
  useAuthStore: (selector: any) => selector({ user: { role: 'platform_admin' } }),
}));

describe('InternalStatusPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetInternalStatus.mockResolvedValue({
      status: 'warning',
      open_critical: 0,
      open_warning: 1,
      maintenance_windows: [],
      incidents: [{
        id: 1,
        event_type: 'provider.channel.failure',
        severity: 'warning',
        title: 'Provider channel failure',
        message: 'primary channel failed',
        resource: 'channel',
        resource_id: '3',
        status: 'open',
        delivery_state: 'failed',
        created_at: '2026-07-04T10:00:00Z',
        updated_at: '2026-07-04T10:00:00Z',
      }],
    });
    mockGetRuntimeStatus.mockResolvedValue({
      status: 'warning',
      generated_at: '2026-07-07T10:00:00Z',
      data_freshness_seconds: 0,
      runtime_environment: {
        service: 'uniroute',
        environment: 'staging',
        deployment_mode: 'docker-compose',
        deployment_role: 'canary',
        go_version: 'go1.24',
        num_cpu: 8,
        goroutines: 12,
        uptime_seconds: 120,
      },
      capacity_health: {
        app_memory: { status: 'available', alloc_bytes: 1048576, sys_bytes: 2097152, heap_alloc_bytes: 1048576, heap_sys_bytes: 2097152 },
        database: { status: 'connected', latency_ms: 2 },
        redis: { status: 'unavailable', reason: 'redis_client_not_configured' },
        traffic: { status: 'available', window_minutes: 5, requests: 40, error_requests: 1, error_rate: 0.025, total_tokens: 1200, cost_cents: 88, p95_latency_ms: 240, p99_latency_ms: 310 },
        cpu: { status: 'unavailable', reason: 'telemetry_source_not_configured' },
        disk: { status: 'unavailable', reason: 'telemetry_source_not_configured' },
        network: { status: 'unavailable', reason: 'telemetry_source_not_configured' },
      },
      observability_integrations: {
        prometheus: { status: 'connected', url: 'http://prometheus:9090', latency_ms: 3, scrape_status: 'up' },
        grafana: { status: 'misconfigured', url: 'http://grafana:3000', reason: 'auth_required' },
      },
    });
  });

  it('renders internal status and incident history', async () => {
    render(<InternalStatusPage />);

    await waitFor(() => {
      expect(mockGetInternalStatus).toHaveBeenCalled();
      expect(mockGetRuntimeStatus).toHaveBeenCalled();
    });

    expect(await screen.findByText('内部状态')).toBeTruthy();
    expect(screen.getByText('运行时观测')).toBeTruthy();
    expect(screen.getByText('数据库')).toBeTruthy();
    expect(screen.getByText('Prometheus')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'http://prometheus:9090' })).toBeTruthy();
    expect(screen.getByText(/scrape up \/ 3 ms/)).toBeTruthy();
    expect(screen.getByRole('link', { name: 'http://grafana:3000' })).toBeTruthy();
    expect(screen.getByText(/auth_required/)).toBeTruthy();
    expect(screen.getByText(/CPU 不可用 \/ 磁盘 不可用 \/ 网络 不可用/)).toBeTruthy();
    expect(screen.getByText('Provider channel failure')).toBeTruthy();
    expect(screen.getByText('上游通道故障')).toBeTruthy();
    expect(screen.getByText('Open 预警事件')).toBeTruthy();
    expect(screen.getByText('severity = warning 且 status = open')).toBeTruthy();
    expect(screen.getByText('未启用')).toBeTruthy();
    expect(screen.getByText(/事件状态：未关闭 \/ 已确认 \/ 已解决/)).toBeTruthy();
    expect(screen.getByText(/投递状态只表示 webhook 投递结果/)).toBeTruthy();
  });
});
