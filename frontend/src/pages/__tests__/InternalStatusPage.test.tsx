import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import InternalStatusPage from '../InternalStatusPage';

const mockGetInternalStatus = vi.fn();
const mockGetRuntimeStatus = vi.fn();
const auth = vi.hoisted(() => ({ user: { role: 'platform_admin' } as any }));

vi.mock('../../api/client', () => ({
  api: {
    getInternalStatus: (...args: any[]) => mockGetInternalStatus(...args),
    getRuntimeStatus: (...args: any[]) => mockGetRuntimeStatus(...args),
  },
}));

vi.mock('../../store/auth', () => ({
  useAuthStore: (selector: any) => selector({ user: auth.user }),
}));

describe('InternalStatusPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    auth.user = {
      role: 'platform_admin',
      org_id: 1,
      customer_context: { state: 'ready', org_id: 7, org_name: '总部' },
    };
    window.history.replaceState({}, '', '/status');
    mockGetInternalStatus.mockResolvedValue({
      summary_kind: 'notification_events',
      attention_level: 'warning',
      open_critical: 0,
      open_warning: 1,
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
        version: '1.1.2-prd8-poc',
        commit: '7b8d6015c8a40499e333d2021d70c155dc49bf27',
        go_version: 'go1.24',
        num_cpu: 8,
        goroutines: 12,
        uptime_seconds: 120,
      },
      capacity_health: {
        app_memory: { status: 'available', alloc_bytes: 1048576, sys_bytes: 2097152, heap_alloc_bytes: 1048576, heap_sys_bytes: 2097152 },
        database: { status: 'connected', latency_ms: 2 },
        redis: { status: 'unavailable', reason: 'redis_client_not_configured' },
        traffic: { status: 'available', window_minutes: 5, requests: 40, error_requests: 1, error_rate: 0.025, total_tokens: 1200, cost_micros: '880000', p95_latency_ms: 240, p99_latency_ms: 310 },
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
          version: 'routing-config.v2',
          selection_version: 'routing-policy.v1',
          source: 'startup_environment_and_channel_state',
          max_retry_count: 4,
          circuit_breaker_threshold: 7,
          circuit_breaker_recover_seconds: 90,
          circuit_breaker_half_open_percent: 25,
          rate_limit_cooldown_seconds: 45,
          auth_failure_cooldown_seconds: 1800,
          key_failure_threshold: 6,
          snapshot_refresh_seconds: 1,
          snapshot_stale_seconds: 30,
          half_open_probe_lease_seconds: 15,
          max_candidate_count: 32,
          max_snapshot_count: 1024,
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
          runtime_budget_mode: 'reserve_estimate_on_every_ancestor_settle_actual',
          runtime_rate_mode: 'exact_redis_sliding_window_on_every_ancestor',
          runtime_concurrency_mode: 'expiring_redis_lease_on_every_ancestor',
          runtime_recovery_mode: 'conservative_ttl_until_p6_f20_durable_terminal_hook',
          max_invocation_lease_seconds: 86400,
          finalization_grace_seconds: 300,
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
        edges: [
          { from: 'service:uniroute', to: 'dependency:database', kind: 'uses', status: 'connected' },
        ],
        limitations: ['network_exposure_not_observed'],
      },
    });
  });

  it('renders runtime health separately from notification attention and dependency edges', async () => {
    render(<InternalStatusPage />);

    await waitFor(() => {
      expect(mockGetInternalStatus).toHaveBeenCalledWith({ org_id: 7 });
      expect(mockGetRuntimeStatus).toHaveBeenCalledWith(7);
    });

    expect(await screen.findByRole('heading', { name: '运行状态', level: 3 })).toBeTruthy();
    expect(screen.getByText('运行时观测')).toBeTruthy();
    expect(screen.getByText('数据库')).toBeTruthy();
    expect(screen.getByText(/本次健康探测往返耗时（状态快照最多缓存 5 秒）/)).toBeTruthy();
    expect(screen.getByText(/数据生成于 .*当前约 0 秒前/)).toBeTruthy();
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
    expect(screen.getByText('请求保护策略')).toBeTruthy();
    expect(screen.getByText('Provider 重试')).toBeTruthy();
    expect(screen.getByText('4 次')).toBeTruthy();
    expect(screen.getByText(/恢复等待 90 秒/)).toBeTruthy();
    expect(screen.getByText('25%')).toBeTruthy();
    expect(screen.getByText('429 密钥冷却')).toBeTruthy();
    expect(screen.getByText('鉴权失败冷却')).toBeTruthy();
    expect(screen.queryByText(/startup_environment/)).toBeNull();
    expect(screen.getByText('路由选择版本')).toBeTruthy();
    expect(screen.getByText('routing-policy.v1')).toBeTruthy();
    expect(screen.getByText('路由快照')).toBeTruthy();
    expect(screen.getByText(/数据库异常最多沿用 30 秒/)).toBeTruthy();
    expect(screen.getByText('半开探针租约')).toBeTruthy();
    expect(screen.getByText('路由内存边界')).toBeTruthy();
    expect(screen.getByText(/每实例最多 1024 个模型快照/)).toBeTruthy();
    expect(screen.getByText('流式连接保护')).toBeTruthy();
    expect(screen.getByText('单帧安全上限')).toBeTruthy();
    expect(screen.getByText('2.0 MB')).toBeTruthy();
    expect(screen.getByText('写入空闲超时')).toBeTruthy();
    expect(screen.getByText('连接心跳')).toBeTruthy();
    expect(screen.getByText('最长流时长')).toBeTruthy();
    expect(screen.getByText('30 分钟')).toBeTruthy();
    expect(screen.getByText('Agent 使用边界')).toBeTruthy();
    expect(screen.getByText('能力令牌')).toBeTruthy();
    expect(screen.getByText('子 Agent 发现')).toBeTruthy();
    expect(screen.getByText('显式登记')).toBeTruthy();
    expect(screen.getByText('撤销收敛')).toBeTruthy();
    expect(screen.getByText('≤ 15 秒')).toBeTruthy();
    expect(screen.getByText('仅模型网关')).toBeTruthy();
    expect(screen.getByText('祖先预算')).toBeTruthy();
    expect(screen.getByText('共享执行')).toBeTruthy();
    expect(screen.getByText('子树速率 / 并发')).toBeTruthy();
    expect(screen.getByText('Redis 共享')).toBeTruthy();
    expect(screen.getByText('预算告警规则')).toBeTruthy();
    expect(screen.getByText('项目级')).toBeTruthy();
    expect(screen.getByText('100%')).toBeTruthy();
    expect(screen.getByText('租户隔离')).toBeTruthy();
    expect(screen.getByText('已停用')).toBeTruthy();
    expect(screen.getByText('受限拓扑')).toBeTruthy();
    expect(screen.getAllByText('service:uniroute').length).toBeGreaterThan(1);
    expect(screen.getByText('uses')).toBeTruthy();
    expect(screen.getByText('网络暴露: not_observed')).toBeTruthy();
    expect(screen.getByText('通知关注级别')).toBeTruthy();
    expect(screen.getByText('未确认预警事件')).toBeTruthy();
    expect(screen.getAllByText('运行状态').length).toBeGreaterThan(1);
    expect(screen.getByText(/1\.1\.2-prd8-poc · 7b8d6015c8a4/)).toBeTruthy();
    expect(screen.getByRole('link', { name: '打开通知中心' }).getAttribute('href')).toBe('/notifications');
    const advanced = screen.getByText('高级运行参数（排障时展开）').closest('details');
    expect(advanced?.hasAttribute('open')).toBe(false);
    expect(screen.queryByText('Provider channel failure')).toBeNull();
  });

  it('binds platform reads to the canonical customer instead of the technical JWT organization', async () => {
    auth.user = {
      role: 'platform_admin',
      org_id: 1,
      customer_context: { state: 'ready', org_id: 7, org_name: '总部' },
    };
    render(<InternalStatusPage />);

    await waitFor(() => {
      expect(mockGetInternalStatus).toHaveBeenCalledWith({ org_id: 7 });
      expect(mockGetRuntimeStatus).toHaveBeenCalledWith(7);
    });
    expect(screen.queryByText('目标组织')).toBeNull();
  });

  it('passes project context to the notification summary and link', async () => {
    window.history.replaceState({}, '', '/status?project_id=9');
    render(<InternalStatusPage />);

    await waitFor(() => expect(mockGetInternalStatus).toHaveBeenCalledWith({ org_id: 7, project_id: 9 }));
    expect((await screen.findByRole('link', { name: '打开通知中心' })).getAttribute('href')).toBe('/notifications?project_id=9');
  });

  it('keeps runtime health available without calling the notification summary when the License omits notifications', async () => {
    auth.user = {
      role: 'org_admin',
      license_access: {
        status: 'active',
        enforceable: true,
        features: { notifications: false },
      },
    };
    render(<InternalStatusPage />);

    await waitFor(() => expect(mockGetRuntimeStatus).toHaveBeenCalled());
    expect(mockGetInternalStatus).not.toHaveBeenCalled();
    expect(screen.getByText('通知摘要已锁定')).toBeTruthy();
    expect(screen.getAllByText(/未包含「通知中心」/).length).toBeGreaterThan(0);
    expect(screen.getByText('运行时观测')).toBeTruthy();
    expect(screen.queryByRole('link', { name: '打开通知中心' })).toBeNull();
  });

  it('keeps deployment health visible but does not query customer notifications before initialization', async () => {
    auth.user = {
      role: 'platform_admin',
      org_id: 1,
      customer_context: { state: 'not_initialized' },
    };
    const defaultRuntime = await mockGetRuntimeStatus();
    mockGetRuntimeStatus.mockClear();
    mockGetRuntimeStatus.mockResolvedValueOnce({
      ...defaultRuntime,
      capacity_health: {
        ...defaultRuntime.capacity_health,
        traffic: {
          ...defaultRuntime.capacity_health.traffic,
          status: 'not_applicable',
          reason: 'customer_context_not_ready',
        },
      },
    });

    render(<InternalStatusPage />);

    await waitFor(() => expect(mockGetRuntimeStatus).toHaveBeenCalledWith());
    expect(mockGetInternalStatus).not.toHaveBeenCalled();
    expect(screen.getByText('未找到部署组织')).toBeTruthy();
    expect(screen.getByText('运行时观测')).toBeTruthy();
    expect(screen.getByText('客户组织尚未就绪，未查询客户流量')).toBeTruthy();
  });

  it('replaces old status with explicit errors instead of healthy-looking defaults', async () => {
    render(<InternalStatusPage />);

    expect(await screen.findByText('未确认预警事件')).toBeTruthy();
    await screen.findByText('Prometheus');

    mockGetInternalStatus.mockRejectedValueOnce(new Error('notification unavailable'));
    mockGetRuntimeStatus.mockRejectedValueOnce(new Error('runtime unavailable'));
    fireEvent.click(screen.getByRole('button', { name: /刷新/ }));

    expect(await screen.findByText('通知摘要加载失败')).toBeTruthy();
    expect(await screen.findByText('运行时状态加载失败')).toBeTruthy();
    expect(screen.getAllByText(/notification unavailable/).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/runtime unavailable/).length).toBeGreaterThan(0);
    expect(screen.queryByText('无待处理')).toBeNull();
    expect(screen.queryByText('未确认预警事件')).toBeNull();
    expect(screen.queryByText('Prometheus')).toBeNull();
  });
});
