import type { ReactNode } from 'react';
import { useEffect, useState } from 'react';
import { Button, Table, Toast, Typography } from '@douyinfe/semi-ui';
import { IconAlertTriangle, IconBell, IconRefresh, IconServer } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { canReadRuntimeStatus } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, MetricTile, PageHeader, StatusTag, Surface } from '../ui';
import { deliveryStateLabel, eventStatusLabel, eventTypeLabel, internalStatusLabel, severityLabel } from '../utils/displayLabels';

const { Text } = Typography;

interface Incident {
  id: number;
  event_type: string;
  severity: string;
  title: string;
  message: string;
  resource: string;
  resource_id: string;
  status: string;
  delivery_state: string;
  created_at: string;
  updated_at: string;
}

interface InternalStatus {
  status: string;
  open_critical: number;
  open_warning: number;
  incidents: Incident[];
  maintenance_windows: unknown[];
}

interface RuntimeIntegration {
  status: string;
  url?: string;
  health_url?: string;
  checked_at?: string;
  latency_ms?: number;
  reason?: string;
  scrape_status?: string;
  version?: string;
}

interface RuntimeMetric {
  status: string;
  reason?: string;
  usage_ratio?: number;
  logical_cores?: number;
  sample_millis?: number;
  total_bytes?: number;
  used_bytes?: number;
  free_bytes?: number;
  mount_point?: string;
  interface_count?: number;
  active_interface_count?: number;
  rx_bytes?: number;
  tx_bytes?: number;
}

interface RuntimeStatus {
  status: string;
  generated_at: string;
  data_freshness_seconds: number;
  runtime_environment: {
    service: string;
    environment: string;
    deployment_mode: string;
    deployment_role: string;
    version?: string;
    commit?: string;
    go_version: string;
    num_cpu: number;
    goroutines: number;
    uptime_seconds: number;
  };
  capacity_health: {
    app_memory: { status: string; alloc_bytes: number; sys_bytes: number; heap_alloc_bytes: number; heap_sys_bytes: number };
    database: { status: string; latency_ms?: number; reason?: string };
    redis: { status: string; latency_ms?: number; reason?: string };
    traffic: {
      status: string;
      window_minutes: number;
      requests: number;
      error_requests: number;
      error_rate: number;
      total_tokens: number;
      cost_cents: number;
      p95_latency_ms: number;
      p99_latency_ms: number;
      reason?: string;
    };
    cpu: RuntimeMetric;
    disk: RuntimeMetric;
    network: RuntimeMetric;
  };
  observability_integrations: {
    prometheus: RuntimeIntegration;
    grafana: RuntimeIntegration;
  };
  effective_configuration?: {
    version: string;
    source: string;
    routing_policy: {
      version: string;
      source: string;
      max_retry_count: number;
      circuit_breaker_threshold: number;
      circuit_breaker_recover_seconds: number;
      circuit_breaker_half_open_percent: number;
      rate_limit_cooldown_seconds: number;
      auth_failure_cooldown_seconds: number;
      key_failure_threshold: number;
    };
    stream_transport: {
      version: string;
      source: string;
      max_sse_line_bytes: number;
      write_idle_timeout_seconds: number;
      heartbeat_seconds: number;
      max_duration_seconds: number;
    };
    agent_capability: {
      version: string;
      source: string;
      discovery_mode: string;
      authority_scope: string;
      token_ttl_seconds: number;
      refresh_base_seconds: number;
      refresh_jitter_seconds: number;
      root_authority_cache_seconds: number;
      active_revocation_slo_seconds: number;
      max_workflow_ttl_seconds: number;
      max_depth: number;
      max_workflow_agents: number;
      redis_outage_mode: string;
      normal_expiry_mode: string;
      refresh_concurrency: string;
    };
    budget_alerts: {
      version: string;
      scope: string;
      budget_source: string;
      period_source: string;
      evaluation_trigger: string;
      event_ledger: string;
      delivery_route_source: string;
      event_type: string;
      threshold_percent: number;
      legacy_global_settings_active: boolean;
      retired_global_setting_keys: string[];
    };
  };
  topology?: {
    scope: string;
    visibility: string;
    network_exposure: string;
    multi_instance_state: string;
    nodes: Array<{ id: string; kind: string; role: string; status: string }>;
    edges: Array<{ from: string; to: string; kind: string; status: string }>;
    limitations: string[];
  };
}

type MetricTone = 'blue' | 'green' | 'amber' | 'violet' | 'red' | 'slate';

function runtimeStatusLabel(status?: string) {
  switch (status) {
    case 'operational':
      return '正常';
    case 'warning':
      return '预警';
    case 'degraded':
      return '降级';
    default:
      return status || '-';
  }
}

function componentStatusLabel(status?: string) {
  switch (status) {
    case 'connected':
    case 'available':
      return '可用';
    case 'configured':
      return '已配置';
    case 'not_configured':
      return '未配置';
    case 'stale':
      return '陈旧';
    case 'misconfigured':
      return '配置异常';
    case 'unavailable':
      return '不可用';
    case 'unknown':
      return '未知';
    default:
      return status || '-';
  }
}

function integrationTone(status?: string): MetricTone {
  switch (status) {
    case 'connected':
    case 'configured':
      return 'green';
    case 'stale':
    case 'misconfigured':
    case 'unknown':
      return 'amber';
    case 'unavailable':
      return 'red';
    default:
      return 'slate';
  }
}

function metricTone(metric?: RuntimeMetric): MetricTone {
  if (!metric) return 'slate';
  if (metric.status === 'unavailable') return 'red';
  if (metric.usage_ratio !== undefined && metric.usage_ratio >= 0.9) return 'red';
  if (metric.usage_ratio !== undefined && metric.usage_ratio >= 0.75) return 'amber';
  if (metric.status === 'available' || metric.status === 'connected') return 'green';
  return integrationTone(metric.status);
}

function integrationMeta(integration?: RuntimeIntegration): ReactNode {
  if (!integration) return '-';
  const details = [
    integration.scrape_status ? `scrape ${integration.scrape_status}` : '',
    integration.version ? `v${integration.version}` : '',
    integration.latency_ms ? `${integration.latency_ms} ms` : '',
    integration.reason || '',
  ].filter(Boolean);
  if (!integration.url) return details.join(' / ') || '-';
  return (
    <span>
      <a href={integration.url} target="_blank" rel="noreferrer">{integration.url}</a>
      {details.length > 0 ? ` / ${details.join(' / ')}` : ''}
    </span>
  );
}

function formatBytes(value?: number) {
  if (!value) return '-';
  const units = ['B', 'KB', 'MB', 'GB'];
  let amount = value;
  let unit = 0;
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024;
    unit += 1;
  }
  return `${amount.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

function formatPercent(value?: number) {
  if (value === undefined || value === null) return '-';
  return `${(value * 100).toFixed(2)}%`;
}

function formatMetricPercent(value?: number) {
  if (value === undefined || value === null) return '-';
  return `${(value * 100).toFixed(1)}%`;
}

function metricValue(metric?: RuntimeMetric): ReactNode {
  if (!metric) return '-';
  if (metric.usage_ratio !== undefined) return formatMetricPercent(metric.usage_ratio);
  return <StatusTag status={metric.status || 'pending'}>{componentStatusLabel(metric.status)}</StatusTag>;
}

function cpuMeta(metric?: RuntimeMetric) {
  if (!metric) return '-';
  const details = [
    metric.logical_cores ? `${metric.logical_cores} cores` : '',
    metric.sample_millis ? `sample ${metric.sample_millis}ms` : '',
    metric.reason || '',
  ].filter(Boolean);
  return details.join(' / ') || '-';
}

function diskMeta(metric?: RuntimeMetric) {
  if (!metric) return '-';
  if (metric.total_bytes) {
    return `${formatBytes(metric.used_bytes)} / ${formatBytes(metric.total_bytes)} used${metric.mount_point ? ` @ ${metric.mount_point}` : ''}`;
  }
  return metric.reason || '-';
}

function networkMeta(metric?: RuntimeMetric) {
  if (!metric) return '-';
  const details = [
    metric.interface_count !== undefined ? `active ${metric.active_interface_count ?? 0}/${metric.interface_count}` : '',
    metric.rx_bytes ? `rx ${formatBytes(metric.rx_bytes)}` : '',
    metric.tx_bytes ? `tx ${formatBytes(metric.tx_bytes)}` : '',
    metric.reason || '',
  ].filter(Boolean);
  return details.join(' / ') || '-';
}

export default function InternalStatusPage() {
  const [data, setData] = useState<InternalStatus | null>(null);
  const [runtimeData, setRuntimeData] = useState<RuntimeStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const role = useAuthStore((s) => s.user?.role);
  const canViewRuntime = canReadRuntimeStatus(role);
  const prometheusIntegration = runtimeData?.observability_integrations.prometheus;
  const grafanaIntegration = runtimeData?.observability_integrations.grafana;
  const routingPolicy = runtimeData?.effective_configuration?.routing_policy;
  const streamTransport = runtimeData?.effective_configuration?.stream_transport;
  const agentCapability = runtimeData?.effective_configuration?.agent_capability;
  const budgetAlerts = runtimeData?.effective_configuration?.budget_alerts;

  const load = () => {
    setLoading(true);
    const requests: Promise<void>[] = [
      api.getInternalStatus()
        .then((result: any) => setData(result))
        .catch((err: any) => {
          Toast.error(`内部状态加载失败${err?.message ? `：${err.message}` : ''}`);
        }),
    ];
    if (canViewRuntime) {
      requests.push(
        api.getRuntimeStatus()
          .then((result: any) => setRuntimeData(result))
          .catch((err: any) => {
            Toast.error(`运行时状态加载失败${err?.message ? `：${err.message}` : ''}`);
          }),
      );
    } else {
      setRuntimeData(null);
    }
    Promise.allSettled(requests).finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, [canViewRuntime]);

  const columns = [
    { title: '事件', dataIndex: 'title', key: 'title', render: (value: string, row: Incident) => (
      <div>
        <Text strong>{value}</Text>
        <div><Text type="tertiary" size="small">{row.message}</Text></div>
      </div>
    ) },
    { title: '类型', dataIndex: 'event_type', key: 'event_type', render: (value: string) => <Text>{eventTypeLabel(value)}</Text> },
    { title: '级别', dataIndex: 'severity', key: 'severity', render: (value: string) => <StatusTag status={value}>{severityLabel(value)}</StatusTag> },
    { title: '事件状态', dataIndex: 'status', key: 'status', render: (value: string) => <StatusTag status={value}>{eventStatusLabel(value)}</StatusTag> },
    { title: '投递', dataIndex: 'delivery_state', key: 'delivery_state', render: (value: string) => <StatusTag status={value}>{deliveryStateLabel(value)}</StatusTag> },
    { title: '资源', key: 'resource', render: (_: unknown, row: Incident) => row.resource ? `${row.resource}:${row.resource_id || '-'}` : '-' },
    { title: '时间', dataIndex: 'created_at', key: 'created_at', render: (value: string) => value ? new Date(value).toLocaleString() : '-' },
  ];

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="系统治理"
        title="内部状态"
        description="事件状态保持 notification_events 聚合语义；运行时观测独立展示应用、依赖和外部监控状态。"
        actions={<Button icon={<IconRefresh />} onClick={load} loading={loading}>刷新</Button>}
      />

      <div className="ur-kpi-grid">
        <MetricTile label="当前状态" value={<StatusTag status={data?.status || 'operational'}>{internalStatusLabel(data?.status || 'operational')}</StatusTag>} icon={<IconServer />} tone={data?.status === 'degraded' ? 'red' : data?.status === 'warning' ? 'amber' : 'green'} meta="基于通知事件聚合" />
        <MetricTile label="Open 严重事件" value={data?.open_critical || 0} icon={<IconAlertTriangle />} tone={(data?.open_critical || 0) > 0 ? 'red' : 'slate'} meta="severity = critical 且 status = open" />
        <MetricTile label="Open 预警事件" value={data?.open_warning || 0} icon={<IconBell />} tone={(data?.open_warning || 0) > 0 ? 'amber' : 'slate'} meta="severity = warning 且 status = open" />
        <MetricTile label="维护窗口" value="未启用" icon={<IconServer />} tone="slate" meta="当前接口保留空数组，调度能力进入 PRD4" />
      </div>

      {canViewRuntime && (
        <Surface title="运行时观测" description="应用运行时、依赖健康、近 5 分钟流量和外部监控健康。">
          <div className="ur-kpi-grid">
            <MetricTile
              label="运行状态"
              value={<StatusTag status={runtimeData?.status || 'pending'}>{runtimeStatusLabel(runtimeData?.status)}</StatusTag>}
              icon={<IconServer />}
              tone={runtimeData?.status === 'degraded' ? 'red' : runtimeData?.status === 'warning' ? 'amber' : 'green'}
              meta={`${runtimeData?.runtime_environment.environment || '-'} / ${runtimeData?.runtime_environment.deployment_role || '-'}`}
            />
            <MetricTile
              label="数据库"
              value={<StatusTag status={runtimeData?.capacity_health.database.status || 'pending'}>{componentStatusLabel(runtimeData?.capacity_health.database.status)}</StatusTag>}
              icon={<IconServer />}
              tone={runtimeData?.capacity_health.database.status === 'connected' ? 'green' : 'red'}
              meta={runtimeData?.capacity_health.database.latency_ms ? `${runtimeData.capacity_health.database.latency_ms} ms` : runtimeData?.capacity_health.database.reason || '-'}
            />
            <MetricTile
              label="Redis"
              value={<StatusTag status={runtimeData?.capacity_health.redis.status || 'pending'}>{componentStatusLabel(runtimeData?.capacity_health.redis.status)}</StatusTag>}
              icon={<IconServer />}
              tone={runtimeData?.capacity_health.redis.status === 'connected' ? 'green' : 'amber'}
              meta={runtimeData?.capacity_health.redis.latency_ms ? `${runtimeData.capacity_health.redis.latency_ms} ms` : runtimeData?.capacity_health.redis.reason || '-'}
            />
            <MetricTile
              label="近 5 分钟请求"
              value={runtimeData?.capacity_health.traffic.requests ?? '-'}
              icon={<IconBell />}
              tone={(runtimeData?.capacity_health.traffic.error_rate || 0) >= 0.05 ? 'amber' : 'blue'}
              meta={`错误率 ${formatPercent(runtimeData?.capacity_health.traffic.error_rate)} / tokens ${runtimeData?.capacity_health.traffic.total_tokens ?? '-'}`}
            />
          </div>
          <div className="ur-kpi-grid" style={{ marginTop: 16 }}>
            <MetricTile
              label="应用内存"
              value={formatBytes(runtimeData?.capacity_health.app_memory.alloc_bytes)}
              icon={<IconServer />}
              tone={runtimeData?.capacity_health.app_memory.status === 'available' ? 'green' : 'slate'}
              meta={`heap ${formatBytes(runtimeData?.capacity_health.app_memory.heap_alloc_bytes)} / goroutines ${runtimeData?.runtime_environment.goroutines ?? '-'}`}
            />
            <MetricTile
              label="CPU"
              value={metricValue(runtimeData?.capacity_health.cpu)}
              icon={<IconAlertTriangle />}
              tone={metricTone(runtimeData?.capacity_health.cpu)}
              meta={cpuMeta(runtimeData?.capacity_health.cpu)}
            />
            <MetricTile
              label="磁盘"
              value={metricValue(runtimeData?.capacity_health.disk)}
              icon={<IconServer />}
              tone={metricTone(runtimeData?.capacity_health.disk)}
              meta={diskMeta(runtimeData?.capacity_health.disk)}
            />
            <MetricTile
              label="网络"
              value={runtimeData?.capacity_health.network.active_interface_count !== undefined
                ? `${runtimeData.capacity_health.network.active_interface_count}/${runtimeData.capacity_health.network.interface_count ?? 0}`
                : metricValue(runtimeData?.capacity_health.network)}
              icon={<IconServer />}
              tone={metricTone(runtimeData?.capacity_health.network)}
              meta={networkMeta(runtimeData?.capacity_health.network)}
            />
            <MetricTile
              label="Prometheus"
              value={<StatusTag status={prometheusIntegration?.status || 'pending'}>{componentStatusLabel(prometheusIntegration?.status)}</StatusTag>}
              icon={<IconServer />}
              tone={integrationTone(prometheusIntegration?.status)}
              meta={integrationMeta(prometheusIntegration)}
            />
            <MetricTile
              label="Grafana"
              value={<StatusTag status={grafanaIntegration?.status || 'pending'}>{componentStatusLabel(grafanaIntegration?.status)}</StatusTag>}
              icon={<IconServer />}
              tone={integrationTone(grafanaIntegration?.status)}
              meta={integrationMeta(grafanaIntegration)}
            />
          </div>
        </Surface>
      )}

      {canViewRuntime && routingPolicy && (
        <Surface title="有效路由策略" description="当前实例启动时实际采用的重试、熔断、半开探测与密钥冷却参数；版本和值同时写入路由轨迹。">
          <div className="ur-kpi-grid">
            <MetricTile
              label="Provider 重试"
              value={`${routingPolicy.max_retry_count} 次`}
              icon={<IconRefresh />}
              tone="blue"
              meta={`首次调用之外 / ${routingPolicy.version}`}
            />
            <MetricTile
              label="渠道熔断阈值"
              value={`${routingPolicy.circuit_breaker_threshold} 次`}
              icon={<IconAlertTriangle />}
              tone="amber"
              meta={`恢复等待 ${routingPolicy.circuit_breaker_recover_seconds} 秒`}
            />
            <MetricTile
              label="半开探测流量"
              value={`${routingPolicy.circuit_breaker_half_open_percent}%`}
              icon={<IconServer />}
              tone="blue"
              meta="恢复等待结束后的候选流量比例"
            />
            <MetricTile
              label="429 密钥冷却"
              value={`${routingPolicy.rate_limit_cooldown_seconds} 秒`}
              icon={<IconBell />}
              tone="amber"
              meta="Provider 限流后的暂停时间"
            />
            <MetricTile
              label="鉴权失败冷却"
              value={`${routingPolicy.auth_failure_cooldown_seconds} 秒`}
              icon={<IconAlertTriangle />}
              tone="red"
              meta="Provider 401/403 后的暂停时间"
            />
            <MetricTile
              label="密钥失效阈值"
              value={`${routingPolicy.key_failure_threshold} 次`}
              icon={<IconServer />}
              tone="slate"
              meta={routingPolicy.source}
            />
          </div>
        </Surface>
      )}

      {canViewRuntime && streamTransport && (
        <Surface title="有效流式传输策略" description="当前实例实际采用的流式帧大小、心跳与连接时限。它保护长回答和大工具结果的传输；动态追加计费冻结属于后续独立能力。">
          <div className="ur-kpi-grid">
            <MetricTile
              label="单帧安全上限"
              value={formatBytes(streamTransport.max_sse_line_bytes)}
              icon={<IconServer />}
              tone="blue"
              meta={`超过上限会安全终止 / ${streamTransport.version}`}
            />
            <MetricTile
              label="写入空闲超时"
              value={`${streamTransport.write_idle_timeout_seconds} 秒`}
              icon={<IconAlertTriangle />}
              tone="amber"
              meta="每次成功写入都会重新计时"
            />
            <MetricTile
              label="连接心跳"
              value={`${streamTransport.heartbeat_seconds} 秒`}
              icon={<IconBell />}
              tone="green"
              meta="长时间无模型内容时保持连接可观测"
            />
            <MetricTile
              label="最长流时长"
              value={`${Math.round(streamTransport.max_duration_seconds / 60)} 分钟`}
              icon={<IconRefresh />}
              tone="slate"
              meta="本实例启动时生效"
            />
          </div>
        </Surface>
      )}

      {canViewRuntime && agentCapability && (
        <Surface title="Agent 能力合同" description="子 Agent 必须显式登记；该能力只覆盖 UniRouter 模型网关，不代表 Tool、IAM、数据库或导出权限。">
          <div className="ur-kpi-grid">
            <MetricTile
              label="能力令牌"
              value={`${Math.round(agentCapability.token_ttl_seconds / 60)} 分钟`}
              icon={<IconServer />}
              tone="violet"
              meta={`约 ${Math.round(agentCapability.refresh_base_seconds / 60)} 分钟刷新，抖动 ±${agentCapability.refresh_jitter_seconds} 秒`}
            />
            <MetricTile
              label="子 Agent 发现"
              value="显式登记"
              icon={<IconServer />}
              tone="blue"
              meta="Supervisor 交换 + 父能力委派"
            />
            <MetricTile
              label="撤销收敛"
              value={`≤ ${agentCapability.active_revocation_slo_seconds} 秒`}
              icon={<IconAlertTriangle />}
              tone="green"
              meta={`Redis 故障：${agentCapability.redis_outage_mode === 'fail_closed' ? '拒绝新准入' : agentCapability.redis_outage_mode}`}
            />
            <MetricTile
              label="边界"
              value="仅模型网关"
              icon={<IconServer />}
              tone="slate"
              meta={`${agentCapability.version} / 深度 ≤ ${agentCapability.max_depth}`}
            />
          </div>
        </Surface>
      )}

      {canViewRuntime && budgetAlerts && (
        <Surface title="有效预算告警合同" description="预算告警只按当前租户的项目统计；项目预算变更写管理审计，触发结果与投递写通知事件账本。旧的全局第一钱包配置已停用。">
          <div className="ur-kpi-grid">
            <MetricTile
              label="告警范围"
              value={budgetAlerts.scope === 'project' ? '项目级' : budgetAlerts.scope}
              icon={<IconServer />}
              tone="blue"
              meta={`${budgetAlerts.budget_source} / ${budgetAlerts.period_source}`}
            />
            <MetricTile
              label="触发阈值"
              value={`${budgetAlerts.threshold_percent}%`}
              icon={<IconAlertTriangle />}
              tone="amber"
              meta="项目当期已结算用量达到预算"
            />
            <MetricTile
              label="事件与投递"
              value="租户隔离"
              icon={<IconBell />}
              tone="green"
              meta={`${budgetAlerts.event_ledger} / ${budgetAlerts.delivery_route_source}`}
            />
            <MetricTile
              label="旧全局配置"
              value={budgetAlerts.legacy_global_settings_active ? '仍在生效' : '已停用'}
              icon={<IconServer />}
              tone={budgetAlerts.legacy_global_settings_active ? 'red' : 'slate'}
              meta={`${budgetAlerts.version} / ${budgetAlerts.evaluation_trigger}`}
            />
          </div>
        </Surface>
      )}

      {canViewRuntime && runtimeData?.topology && (
        <Surface title="受限拓扑" description="当前进程与逻辑依赖关系；地址、主机标识和网络暴露状态不在应用内推断。">
          <div className="ur-chip-row" style={{ marginBottom: 12 }}>
            <TagStatus label="可见性" value={runtimeData.topology.visibility} />
            <TagStatus label="网络暴露" value={runtimeData.topology.network_exposure} />
            <TagStatus label="多实例" value={runtimeData.topology.multi_instance_state} />
          </div>
          <div className="ur-table-shell">
            <Table
              rowKey="id"
              size="small"
              pagination={false}
              dataSource={runtimeData.topology.nodes}
              columns={[
                { title: '逻辑节点', dataIndex: 'id', render: (value: string) => <Text code>{value}</Text> },
                { title: '类型', dataIndex: 'kind', width: 140 },
                { title: '角色', dataIndex: 'role', width: 180 },
                { title: '状态', dataIndex: 'status', width: 140, render: (value: string) => <StatusTag status={value}>{componentStatusLabel(value)}</StatusTag> },
              ]}
            />
          </div>
        </Surface>
      )}

      <Surface title="通知事件" description="最近 20 条 notification_events 按 open 优先、时间倒序展示。事件状态：未关闭 / 已确认 / 已解决；投递状态只表示 webhook 投递结果。">
        <div className="ur-table-shell">
          <Table
            columns={columns}
            dataSource={data?.incidents || []}
            rowKey="id"
            loading={loading}
            pagination={false}
            size="small"
            empty={<EmptyState title="暂无事件" description="没有 open 或近期通知事件。" minHeight={180} />}
          />
        </div>
      </Surface>
    </div>
  );
}

function TagStatus({ label, value }: { label: string; value: string }) {
  return <StatusTag status={value}>{label}: {componentStatusLabel(value)}</StatusTag>;
}
