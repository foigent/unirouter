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
        cpu: { status: 'available', usage_ratio: 0.25, logical_cores: 8, sample_millis: 50 },
        disk: { status: 'available', usage_ratio: 0.7, total_bytes: 10737418240, used_bytes: 7516192768, free_bytes: 3221225472, mount_point: '/' },
        network: { status: 'available', interface_count: 3, active_interface_count: 2, rx_bytes: 1048576, tx_bytes: 2097152 },
      },
      observability_integrations: {
        prometheus: { status: 'connected', url: 'http://prometheus:9090', latency_ms: 3, scrape_status: 'up' },
        grafana: { status: 'misconfigured', url: 'http://grafana:3000', reason: 'auth_required' },
      },
      effective_configuration: {
        version: 'effective-config.v1',
        source: 'startup_environment_and_schema_contract',
        routing_policy: {
          version: 'routing-config.v1',
          source: 'startup_environment',
          max_retry_count: 4,
          circuit_breaker_threshold: 7,
          circuit_breaker_recover_seconds: 90,
          circuit_breaker_half_open_percent: 25,
          rate_limit_cooldown_seconds: 45,
          auth_failure_cooldown_seconds: 1800,
          key_failure_threshold: 6,
        },
        stream_transport: {
          version: 'stream-transport.v1',
          source: 'startup_environment',
          max_sse_line_bytes: 2097152,
          write_idle_timeout_seconds: 30,
          heartbeat_seconds: 15,
          max_duration_seconds: 1800,
        },
        agent_capability: {
          version: 'unirouter.agent-capability.v1',
          source: 'compiled_security_contract',
          discovery_mode: 'explicit_supervisor_exchange_and_parent_delegation',
          authority_scope: 'model_gateway_only',
          token_ttl_seconds: 300,
          refresh_base_seconds: 180,
          refresh_jitter_seconds: 15,
          root_authority_cache_seconds: 15,
          active_revocation_slo_seconds: 15,
          max_workflow_ttl_seconds: 86400,
          max_depth: 16,
          max_workflow_agents: 10000,
          redis_outage_mode: 'fail_closed',
          normal_expiry_mode: 'deny_new_admission_keep_admitted_work',
          refresh_concurrency: 'server_convergent_and_client_singleflight',
        },
        budget_alerts: {
          version: 'project-budget-alert.v1',
          scope: 'project',
          budget_source: 'projects.budget_cents',
          period_source: 'projects.budget_period',
          evaluation_trigger: 'post_settlement',
          event_ledger: 'notification_events',
          delivery_route_source: 'notification_webhooks',
          event_type: 'project_budget.threshold',
          threshold_percent: 100,
          legacy_global_settings_active: false,
          retired_global_setting_keys: ['budget_alert_webhook_url', 'budget_alert_threshold_cents', 'budget_cents'],
        },
      },
      topology: {
        scope: 'current_process',
        visibility: 'redacted',
        network_exposure: 'not_observed',
        multi_instance_state: 'not_configured',
        nodes: [
          { id: 'service:uniroute', kind: 'service', role: 'canary', status: 'warning' },
          { id: 'dependency:database', kind: 'dependency', role: 'postgresql', status: 'connected' },
          { id: 'dependency:redis', kind: 'dependency', role: 'redis', status: 'unavailable' },
        ],
        edges: [],
        limitations: ['network_exposure_not_observed'],
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
    expect(screen.getByText('CPU')).toBeTruthy();
    expect(screen.getByText('25.0%')).toBeTruthy();
    expect(screen.getByText(/8 cores \/ sample 50ms/)).toBeTruthy();
    expect(screen.getByText('磁盘')).toBeTruthy();
    expect(screen.getByText('70.0%')).toBeTruthy();
    expect(screen.getByText(/7.0 GB \/ 10.0 GB used @ \//)).toBeTruthy();
    expect(screen.getByText('网络')).toBeTruthy();
    expect(screen.getByText('2/3')).toBeTruthy();
    expect(screen.getByText(/active 2\/3 \/ rx 1.0 MB \/ tx 2.0 MB/)).toBeTruthy();
    expect(screen.getByText('有效路由策略')).toBeTruthy();
    expect(screen.getByText('Provider 重试')).toBeTruthy();
    expect(screen.getByText('4 次')).toBeTruthy();
    expect(screen.getByText(/恢复等待 90 秒/)).toBeTruthy();
    expect(screen.getByText('25%')).toBeTruthy();
    expect(screen.getByText('429 密钥冷却')).toBeTruthy();
    expect(screen.getByText('鉴权失败冷却')).toBeTruthy();
    expect(screen.getByText(/startup_environment/)).toBeTruthy();
    expect(screen.getByText('有效流式传输策略')).toBeTruthy();
    expect(screen.getByText('单帧安全上限')).toBeTruthy();
    expect(screen.getByText('2.0 MB')).toBeTruthy();
    expect(screen.getByText('写入空闲超时')).toBeTruthy();
    expect(screen.getByText('连接心跳')).toBeTruthy();
    expect(screen.getByText('最长流时长')).toBeTruthy();
    expect(screen.getByText('30 分钟')).toBeTruthy();
    expect(screen.getByText('Agent 能力合同')).toBeTruthy();
    expect(screen.getByText('能力令牌')).toBeTruthy();
    expect(screen.getByText('子 Agent 发现')).toBeTruthy();
    expect(screen.getByText('显式登记')).toBeTruthy();
    expect(screen.getByText('撤销收敛')).toBeTruthy();
    expect(screen.getByText('≤ 15 秒')).toBeTruthy();
    expect(screen.getByText('仅模型网关')).toBeTruthy();
    expect(screen.getByText('有效预算告警合同')).toBeTruthy();
    expect(screen.getByText('项目级')).toBeTruthy();
    expect(screen.getByText('100%')).toBeTruthy();
    expect(screen.getByText('租户隔离')).toBeTruthy();
    expect(screen.getByText('已停用')).toBeTruthy();
    expect(screen.getByText('受限拓扑')).toBeTruthy();
    expect(screen.getByText('service:uniroute')).toBeTruthy();
    expect(screen.getByText('网络暴露: not_observed')).toBeTruthy();
    expect(screen.getByText('Provider channel failure')).toBeTruthy();
    expect(screen.getByText('上游通道故障')).toBeTruthy();
    expect(screen.getByText('Open 预警事件')).toBeTruthy();
    expect(screen.getByText('severity = warning 且 status = open')).toBeTruthy();
    expect(screen.getByText('未启用')).toBeTruthy();
    expect(screen.getByText(/事件状态：未关闭 \/ 已确认 \/ 已解决/)).toBeTruthy();
    expect(screen.getByText(/投递状态只表示 webhook 投递结果/)).toBeTruthy();
  });
});
