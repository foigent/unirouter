import type { ReactNode } from 'react';
import { useEffect, useState } from 'react';
import { Button, Select, Table, Toast, Typography } from '@douyinfe/semi-ui';
import { IconAlertTriangle, IconBell, IconRefresh, IconRoute, IconServer } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { canReadRuntimeStatus, hasLicensedFeature, isPlatformAdminRole, licenseRestrictionMessage } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, MetricTile, PageHeader, StatusTag, Surface } from '../ui';
import { loadAllPages } from '../utils/loadAllPages';

const { Text } = Typography;

interface InternalStatus {
  summary_kind: 'notification_events';
  attention_level: string;
  open_critical: number;
  open_warning: number;
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
      cost_micros: string;
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
      selection_version?: string;
      source: string;
      max_retry_count: number;
      circuit_breaker_threshold: number;
      circuit_breaker_recover_seconds: number;
      circuit_breaker_half_open_percent: number;
      rate_limit_cooldown_seconds: number;
      auth_failure_cooldown_seconds: number;
      key_failure_threshold: number;
      snapshot_refresh_seconds?: number;
      snapshot_stale_seconds?: number;
      half_open_probe_lease_seconds?: number;
      max_candidate_count?: number;
      max_snapshot_count?: number;
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
      runtime_budget_mode: string;
      runtime_rate_mode: string;
      runtime_concurrency_mode: string;
      runtime_recovery_mode: string;
      max_invocation_lease_seconds: number;
      finalization_grace_seconds: number;
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
type LoadState = 'loading' | 'ready' | 'error';

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

function notificationAttentionLabel(level?: string) {
  switch (level) {
    case 'critical':
      return '严重';
    case 'warning':
      return '预警';
    case 'none':
      return '无待处理';
    default:
      return level || '-';
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

function projectIDFromLocation() {
  const raw = new URLSearchParams(window.location.search).get('project_id');
  if (!raw || !/^\d+$/.test(raw)) return undefined;
  const value = Number(raw);
  return Number.isSafeInteger(value) && value > 0 ? value : undefined;
}

export default function InternalStatusPage() {
  const user = useAuthStore((state) => state.user);
  const platform = isPlatformAdminRole(user?.role);
  const [selectedOrgID, setSelectedOrgID] = useState<number | undefined>(platform ? user?.org_id : undefined);
  const [organizations, setOrganizations] = useState<Array<{ id: number; name: string }>>([]);
  const [data, setData] = useState<InternalStatus | null>(null);
  const [runtimeData, setRuntimeData] = useState<RuntimeStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [notificationLoadState, setNotificationLoadState] = useState<LoadState>('loading');
  const [notificationLoadError, setNotificationLoadError] = useState('');
  const [runtimeLoadState, setRuntimeLoadState] = useState<LoadState>('loading');
  const [runtimeLoadError, setRuntimeLoadError] = useState('');
  const role = user?.role;
  const canViewRuntime = canReadRuntimeStatus(role);
  const notificationsLicensed = hasLicensedFeature(user?.license_access, 'notifications');
  const notificationsRestriction = user?.license_access
    ? licenseRestrictionMessage(user.license_access, 'notifications')
    : '当前商业授权信息尚未加载。';
  const targetOrgID = platform ? selectedOrgID : undefined;
  const prometheusIntegration = runtimeData?.observability_integrations.prometheus;
  const grafanaIntegration = runtimeData?.observability_integrations.grafana;
  const routingPolicy = runtimeData?.effective_configuration?.routing_policy;
  const streamTransport = runtimeData?.effective_configuration?.stream_transport;
  const agentCapability = runtimeData?.effective_configuration?.agent_capability;
  const budgetAlerts = runtimeData?.effective_configuration?.budget_alerts;
  const projectID = projectIDFromLocation();
  const notificationCenterHref = projectID ? `/notifications?project_id=${projectID}` : '/notifications';

  const load = () => {
    setLoading(true);
    const requests: Promise<void>[] = [];
    if (notificationsLicensed) {
      setData(null);
      setNotificationLoadState('loading');
      setNotificationLoadError('');
      requests.push(api.getInternalStatus(targetOrgID || projectID ? {
        ...(targetOrgID ? { org_id: targetOrgID } : {}),
        ...(projectID ? { project_id: projectID } : {}),
      } : undefined)
        .then((result: any) => {
          if (!result) throw new Error('服务未返回通知摘要');
          setData(result);
          setNotificationLoadState('ready');
        })
        .catch((err: any) => {
          const message = `通知摘要加载失败${err?.message ? `：${err.message}` : ''}`;
          setNotificationLoadError(message);
          setNotificationLoadState('error');
          Toast.error(message);
        }));
    } else {
      setData(null);
      setNotificationLoadState('ready');
      setNotificationLoadError('');
    }
    if (canViewRuntime) {
      setRuntimeData(null);
      setRuntimeLoadState('loading');
      setRuntimeLoadError('');
      requests.push(
        (targetOrgID ? api.getRuntimeStatus(targetOrgID) : api.getRuntimeStatus())
          .then((result: any) => {
            if (!result) throw new Error('服务未返回运行时状态');
            setRuntimeData(result);
            setRuntimeLoadState('ready');
          })
          .catch((err: any) => {
            const message = `运行时状态加载失败${err?.message ? `：${err.message}` : ''}`;
            setRuntimeLoadError(message);
            setRuntimeLoadState('error');
            Toast.error(message);
          }),
      );
    } else {
      setRuntimeData(null);
      setRuntimeLoadState('ready');
      setRuntimeLoadError('');
    }
    Promise.allSettled(requests).finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, [canViewRuntime, notificationsLicensed, projectID, targetOrgID]);

  useEffect(() => {
    if (!platform) return;
    if (user?.org_id) setSelectedOrgID(current => current || user.org_id);
    loadAllPages<{ id: number; name: string }>((page, size) => api.getOrganizations(page, size))
      .then(setOrganizations)
      .catch((err: any) => Toast.error(`组织列表加载失败${err?.message ? `：${err.message}` : ''}`));
  }, [platform, user?.org_id]);

  const changeOrganization = (value: unknown) => {
    const orgID = Number(value);
    if (!Number.isSafeInteger(orgID) || orgID <= 0) return;
    if (projectID) {
      const params = new URLSearchParams(window.location.search);
      params.delete('project_id');
      const query = params.toString();
      window.history.replaceState(null, '', `${window.location.pathname}${query ? `?${query}` : ''}`);
    }
    setSelectedOrgID(orgID);
    setData(null);
    setRuntimeData(null);
  };

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="监控与用量"
        title="运行状态"
        description="服务健康以运行时观测为准；未确认通知仅提示需要人工关注的业务事件。"
        actions={(
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            {platform && (
              <>
                <Text type="tertiary">目标组织</Text>
                <Select
                  aria-label="目标组织"
                  placeholder="选择目标组织"
                  value={selectedOrgID}
                  optionList={organizations.map(org => ({ label: `${org.name}（ID ${org.id}）`, value: org.id }))}
                  onChange={changeOrganization}
                  disabled={loading}
                  style={{ width: 220 }}
                />
              </>
            )}
            <Button icon={<IconRefresh />} onClick={load} loading={loading}>刷新</Button>
          </div>
        )}
      />

			{notificationsLicensed ? notificationLoadState === 'loading' ? (
				<Surface title="通知关注摘要">
					<EmptyState title="正在加载通知摘要..." minHeight={180} />
				</Surface>
			) : notificationLoadState === 'error' ? (
				<Surface title="通知关注摘要">
					<EmptyState
						title="通知摘要加载失败"
						description={notificationLoadError}
						actions={<Button icon={<IconRefresh />} onClick={load}>重试</Button>}
						minHeight={180}
					/>
				</Surface>
			) : data ? <div className="ur-kpi-grid">
				<MetricTile
					label="通知关注级别"
					value={<StatusTag status={data.attention_level}>{notificationAttentionLabel(data.attention_level)}</StatusTag>}
					icon={<IconBell />}
					tone={data.attention_level === 'critical' ? 'red' : data.attention_level === 'warning' ? 'amber' : 'green'}
					meta="仅基于未确认通知，不代表服务健康"
				/>
				<MetricTile label="未确认严重事件" value={data.open_critical} icon={<IconAlertTriangle />} tone={data.open_critical > 0 ? 'red' : 'slate'} meta="status = open 的严重通知" />
				<MetricTile label="未确认预警事件" value={data.open_warning} icon={<IconBell />} tone={data.open_warning > 0 ? 'amber' : 'slate'} meta="status = open 的预警通知" />
				<MetricTile label="事件处理" value={<a href={notificationCenterHref}>打开通知中心</a>} icon={<IconBell />} tone="blue" meta="查看详情、确认或解决事件" />
			</div> : null : (
        <Surface title="通知关注摘要" description="运行状态保持可用；通知事件摘要需要当前商业授权包含通知中心。">
          <EmptyState
            title="通知摘要已锁定"
            description={notificationsRestriction}
            minHeight={180}
          />
        </Surface>
      )}

			{canViewRuntime && runtimeLoadState === 'loading' && (
				<Surface title="运行时观测">
					<EmptyState title="正在加载运行时状态..." minHeight={220} />
				</Surface>
			)}

			{canViewRuntime && runtimeLoadState === 'error' && (
				<Surface title="运行时观测">
					<EmptyState
						title="运行时状态加载失败"
						description={runtimeLoadError}
						actions={<Button icon={<IconRefresh />} onClick={load}>重试</Button>}
						minHeight={220}
					/>
				</Surface>
			)}

			{canViewRuntime && runtimeLoadState === 'ready' && runtimeData && (
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

      {canViewRuntime && (routingPolicy || streamTransport || agentCapability || budgetAlerts) && (
        <details style={{ marginTop: 16 }}>
          <summary style={{ cursor: 'pointer', fontWeight: 600, marginBottom: 12 }}>高级运行参数（排障时展开）</summary>
      {routingPolicy && (
        <Surface title="请求保护策略" description="当前实例处理上游失败、限流和熔断时采用的保护参数。">
          <div className="ur-kpi-grid">
            <MetricTile
              label="Provider 重试"
              value={`${routingPolicy.max_retry_count} 次`}
              icon={<IconRefresh />}
              tone="blue"
              meta="同一次请求中的追加尝试次数"
            />
            <MetricTile
              label="默认熔断阈值"
              value={`${routingPolicy.circuit_breaker_threshold} 次`}
              icon={<IconAlertTriangle />}
              tone="amber"
              meta={`各通道可单独设置 / 恢复等待 ${routingPolicy.circuit_breaker_recover_seconds} 秒`}
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
              meta="连续失败后暂停使用该密钥"
            />
            <MetricTile
              label="路由选择版本"
              value={routingPolicy.selection_version || '-'}
              icon={<IconRoute />}
              tone="blue"
              meta="优先级后按同级静态权重"
            />
            <MetricTile
              label="路由快照"
              value={`${routingPolicy.snapshot_refresh_seconds ?? '-'} 秒刷新`}
              icon={<IconRefresh />}
              tone="blue"
              meta={`数据库异常最多沿用 ${routingPolicy.snapshot_stale_seconds ?? '-'} 秒`}
            />
            <MetricTile
              label="半开探针租约"
              value={`${routingPolicy.half_open_probe_lease_seconds ?? '-'} 秒`}
              icon={<IconServer />}
              tone="amber"
              meta="活动请求会续租；崩溃后到期释放"
            />
            <MetricTile
              label="路由内存边界"
              value={`${routingPolicy.max_candidate_count ?? '-'} 个候选`}
              icon={<IconServer />}
              tone="slate"
              meta={`每实例最多 ${routingPolicy.max_snapshot_count ?? '-'} 个模型快照`}
            />
          </div>
        </Surface>
      )}

      {streamTransport && (
        <Surface title="流式连接保护" description="长回答和大工具结果传输时使用的帧大小、心跳与连接时限。">
          <div className="ur-kpi-grid">
            <MetricTile
              label="单帧安全上限"
              value={formatBytes(streamTransport.max_sse_line_bytes)}
              icon={<IconServer />}
              tone="blue"
              meta="超过上限会安全终止"
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

      {agentCapability && (
        <Surface title="Agent 使用边界" description="子 Agent 必须显式登记；授权只覆盖模型网关，不包含工具、账号、数据库或导出权限。">
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
              meta={`最大委派深度 ${agentCapability.max_depth}`}
            />
            <MetricTile
              label="祖先预算"
              value="共享执行"
              icon={<IconServer />}
              tone="green"
              meta="每层预留成本与 Token，结算后写入实际用量"
            />
            <MetricTile
              label="子树速率 / 并发"
              value="Redis 共享"
              icon={<IconRefresh />}
              tone="blue"
              meta={`每层精确窗口 + 到期租约 / 终态缓冲 ${agentCapability.finalization_grace_seconds} 秒`}
            />
          </div>
        </Surface>
      )}

      {budgetAlerts && (
        <Surface title="预算告警规则" description="按当前租户的项目预算触发，处理记录可在通知中心查看。">
          <div className="ur-kpi-grid">
            <MetricTile
              label="告警范围"
              value={budgetAlerts.scope === 'project' ? '项目级' : budgetAlerts.scope}
              icon={<IconServer />}
              tone="blue"
              meta="使用项目预算与预算周期"
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
              meta="通知中心记录并按 Webhook 路由投递"
            />
            <MetricTile
              label="旧全局配置"
              value={budgetAlerts.legacy_global_settings_active ? '仍在生效' : '已停用'}
              icon={<IconServer />}
              tone={budgetAlerts.legacy_global_settings_active ? 'red' : 'slate'}
              meta="仅项目级规则生效"
            />
          </div>
        </Surface>
      )}
        </details>
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
          <div className="ur-table-shell" style={{ marginTop: 12 }}>
            <Table
              rowKey={(row?: { from: string; to: string; kind: string }) => row ? `${row.from}-${row.to}-${row.kind}` : ''}
              size="small"
              pagination={false}
              dataSource={runtimeData.topology.edges}
              columns={[
                { title: '来源', dataIndex: 'from', render: (value: string) => <Text code>{value}</Text> },
                { title: '依赖关系', dataIndex: 'kind', width: 160 },
                { title: '目标', dataIndex: 'to', render: (value: string) => <Text code>{value}</Text> },
                { title: '状态', dataIndex: 'status', width: 140, render: (value: string) => <StatusTag status={value}>{componentStatusLabel(value)}</StatusTag> },
              ]}
              empty={<Text type="tertiary">当前运行时未返回依赖边。</Text>}
            />
          </div>
        </Surface>
      )}
    </div>
  );
}

function TagStatus({ label, value }: { label: string; value: string }) {
  return <StatusTag status={value}>{label}: {componentStatusLabel(value)}</StatusTag>;
}
