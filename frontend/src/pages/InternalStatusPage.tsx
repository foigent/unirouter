import '../i18n/monitoring';
import { translate as t, useI18n } from '../i18n';
import type { MessageKey } from '../i18n/catalog';
import { formatDateTime } from '../utils/formatDate';
import type { ReactNode } from 'react';
import { useEffect, useState } from 'react';
import { Button, Table, Toast, Typography } from '@douyinfe/semi-ui';
import { IconAlertTriangle, IconBell, IconRefresh, IconRoute, IconServer } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { authzContextForUser, canReadRuntimeStatus, hasLicensedFeature, licenseRestrictionMessage } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, MetricTile, PageHeader, StatusTag, Surface } from '../ui';
import { customerContextBlockerCopy, resolveCustomerContext } from '../utils/customerContext';

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
type LoadFailure = { key: MessageKey; detail?: string } | null;
class MissingRuntimeResponse extends Error {
  constructor(readonly key: MessageKey) { super(key); }
}
function failureText(failure: LoadFailure) {
  return failure ? t(failure.key) + (failure.detail ? `: ${failure.detail}` : '') : '';
}

function runtimeStatusLabel(status?: string) {
  switch (status) {
    case 'operational':
      return t("正常");
    case 'warning':
      return t("预警");
    case 'degraded':
      return t("降级");
    default:
      return status || '-';
  }
}

function notificationAttentionLabel(level?: string) {
  switch (level) {
    case 'critical':
      return t("严重");
    case 'warning':
      return t("预警");
    case 'none':
      return t("无待处理");
    default:
      return level || '-';
  }
}

function componentStatusLabel(status?: string) {
  switch (status) {
    case 'connected':
    case 'available':
      return t("可用");
    case 'configured':
      return t("已配置");
    case 'not_configured':
      return t("未配置");
    case 'stale':
      return t("陈旧");
    case 'misconfigured':
      return t("配置异常");
    case 'unavailable':
      return t("不可用");
    case 'unknown':
      return t("未知");
    case 'operational':
      return t("正常");
    case 'redacted':
      return t("已隐藏敏感细节");
    case 'not_observed':
      return t("未观测");
    default:
      return status || '-';
  }
}

function topologyKindLabel(kind?: string) {
  if (kind === 'service') return t("服务");
  if (kind === 'dependency') return t("依赖");
  return kind || '-';
}

function topologyRoleLabel(role?: string) {
  if (role === 'stable') return t("稳定实例");
  if (role === 'canary') return t("灰度实例");
  if (role === 'postgresql') return t("PostgreSQL 数据库");
  if (role === 'redis') return t("Redis 缓存");
  return role || '-';
}

function topologyRelationLabel(kind?: string) {
  if (kind === 'uses' || kind === 'runtime_dependency') return t("运行时依赖");
  return kind || '-';
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
    integration.scrape_status
      ? (integration.scrape_status === 'up' ? t("抓取正常") : t('抓取状态：{state}', { state: integration.scrape_status }))
      : '',
    integration.version ? `v${integration.version}` : '',
    integration.latency_ms !== undefined ? t('{count} 毫秒', { count: integration.latency_ms }) : '',
    integration.reason === 'auth_required' ? t("需要认证") : integration.reason || '',
  ].filter(Boolean);
  if (!integration.url) return details.join(' / ') || '-';
  return (
    <span>
      <a href={integration.url} target="_blank" rel="noreferrer">{integration.url}</a>
      {details.length > 0 ? ` / ${details.join(' / ')}` : ''}
    </span>
  );
}

function dependencyProbeMeta(dependency?: { latency_ms?: number; reason?: string }): ReactNode {
  if (!dependency) return '-';
  if (dependency.latency_ms !== undefined) {
    return (
      <span>
        {dependency.latency_ms}  {t("毫秒")}
        <br />

        {t("本次健康探测往返耗时（状态快照最多缓存 5 秒）")}
      </span>
    );
  }
  return dependency.reason || '-';
}

function formatRuntimeTimestamp(value?: string) {
  if (!value) return t("未知时间");
  const timestamp = new Date(value);
  if (Number.isNaN(timestamp.getTime())) return value;
  return formatDateTime(value);
}

function formatBytes(value?: number) {
  if (value === undefined || value === null || !Number.isFinite(value)) return '-';
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
    metric.logical_cores ? t('{count} 个逻辑核心', { count: metric.logical_cores }) : '',
    metric.sample_millis ? t('采样 {count} 毫秒', { count: metric.sample_millis }) : '',
    metric.reason || '',
  ].filter(Boolean);
  return details.join(' / ') || '-';
}

function diskMeta(metric?: RuntimeMetric) {
  if (!metric) return '-';
  if (metric.total_bytes) {
    return t('已用 {used} / 总计 {total}', { used: formatBytes(metric.used_bytes), total: formatBytes(metric.total_bytes) }) + (metric.mount_point ? t(' · 挂载点 {path}', { path: metric.mount_point }) : '');
  }
  return metric.reason || '-';
}

function networkMeta(metric?: RuntimeMetric) {
  if (!metric) return '-';
  const details = [
    metric.interface_count !== undefined ? t('活动网卡 {active}/{total}', { active: metric.active_interface_count ?? '—', total: metric.interface_count }) : '',
    metric.rx_bytes !== undefined ? t('接收 {bytes}', { bytes: formatBytes(metric.rx_bytes) }) : '',
    metric.tx_bytes !== undefined ? t('发送 {bytes}', { bytes: formatBytes(metric.tx_bytes) }) : '',
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
  useI18n();
  const user = useAuthStore((state) => state.user);
  const authz = authzContextForUser(user);
  const customerContext = resolveCustomerContext(user);
  const [data, setData] = useState<InternalStatus | null>(null);
  const [runtimeData, setRuntimeData] = useState<RuntimeStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [notificationLoadState, setNotificationLoadState] = useState<LoadState>('loading');
  const [notificationLoadError, setNotificationLoadError] = useState<LoadFailure>(null);
  const [runtimeLoadState, setRuntimeLoadState] = useState<LoadState>('loading');
  const [runtimeLoadError, setRuntimeLoadError] = useState<LoadFailure>(null);
  const canViewRuntime = canReadRuntimeStatus(authz);
  const notificationsLicensed = hasLicensedFeature(user?.license_access, 'notifications');
  const notificationsRestriction = user?.license_access
    ? licenseRestrictionMessage(user.license_access, 'notifications')
    : t("当前商业授权信息尚未加载。");
  const targetOrgID = customerContext.state === 'ready' ? customerContext.orgID : undefined;
  const notificationBlocker = customerContext.state === 'ready'
    ? null
    : customerContextBlockerCopy(customerContext.state);
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
    if (notificationsLicensed && customerContext.state === 'ready') {
      setData(null);
      setNotificationLoadState('loading');
      setNotificationLoadError(null);
      requests.push(api.getInternalStatus(targetOrgID || projectID ? {
        ...(targetOrgID ? { org_id: targetOrgID } : {}),
        ...(projectID ? { project_id: projectID } : {}),
      } : undefined)
        .then((result: any) => {
          if (!result) throw new MissingRuntimeResponse('服务未返回通知摘要');
          setData(result);
          setNotificationLoadState('ready');
        })
        .catch((err: any) => {
          const failure: LoadFailure = err instanceof MissingRuntimeResponse ? { key: err.key } : { key: '通知摘要加载失败', detail: err?.message };
          setNotificationLoadError(failure);
          setNotificationLoadState('error');
          Toast.error(failureText(failure));
        }));
    } else {
      setData(null);
      setNotificationLoadState('ready');
      setNotificationLoadError(null);
    }
    if (canViewRuntime) {
      setRuntimeData(null);
      setRuntimeLoadState('loading');
      setRuntimeLoadError(null);
      requests.push(
        (targetOrgID ? api.getRuntimeStatus(targetOrgID) : api.getRuntimeStatus())
          .then((result: any) => {
            if (!result) throw new MissingRuntimeResponse('服务未返回运行时状态');
            setRuntimeData(result);
            setRuntimeLoadState('ready');
          })
          .catch((err: any) => {
            const failure: LoadFailure = err instanceof MissingRuntimeResponse ? { key: err.key } : { key: '运行时状态加载失败', detail: err?.message };
            setRuntimeLoadError(failure);
            setRuntimeLoadState('error');
            Toast.error(failureText(failure));
          }),
      );
    } else {
      setRuntimeData(null);
      setRuntimeLoadState('ready');
      setRuntimeLoadError(null);
    }
    Promise.allSettled(requests).finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, [canViewRuntime, customerContext.state, notificationsLicensed, projectID, targetOrgID]);

  return (
    <div className="ur-page ur-page-wide ur-runtime-workspace">
      <PageHeader
        eyebrow={t("监控与用量")}
        title={t("运行状态")}
        description={t("服务健康以运行时观测为准；未确认通知仅提示需要人工关注的业务事件。")}
        actions={(
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <Button icon={<IconRefresh />} onClick={load} loading={loading}>{t("刷新")}</Button>
          </div>
        )}
      />


			{canViewRuntime && runtimeLoadState === 'loading' && (
				<Surface title={t("运行时观测")}>
					<EmptyState title={t("正在加载运行时状态...")} minHeight={220} />
				</Surface>
			)}

			{canViewRuntime && runtimeLoadState === 'error' && (
				<Surface title={t("运行时观测")}>
					<EmptyState
						title={t("运行时状态加载失败")}
						description={failureText(runtimeLoadError)}
						actions={<Button icon={<IconRefresh />} onClick={load}>{t("重试")}</Button>}
						minHeight={220}
					/>
				</Surface>
			)}

			{canViewRuntime && runtimeLoadState === 'ready' && runtimeData && (
				<Surface
          title={t("运行时观测")}
          description={t('应用运行时、依赖健康、近 5 分钟流量和外部监控健康。数据生成于 {time}，当前约 {seconds} 秒前。', { time: formatRuntimeTimestamp(runtimeData.generated_at), seconds: Math.max(0, runtimeData.data_freshness_seconds) })}
        >
          <div className="ur-kpi-grid">
            <MetricTile
              label={t("运行状态")}
              value={<StatusTag status={runtimeData?.status || 'pending'}>{runtimeStatusLabel(runtimeData?.status)}</StatusTag>}
              icon={<IconServer />}
              tone={runtimeData?.status === 'degraded' ? 'red' : runtimeData?.status === 'warning' ? 'amber' : 'green'}
              meta={(
                <span>
                  {runtimeData?.runtime_environment.environment || '-'} / {runtimeData?.runtime_environment.deployment_role || '-'}
                  <br />
                  {runtimeData?.runtime_environment.version || t("版本未声明")} · {runtimeData?.runtime_environment.commit ? runtimeData.runtime_environment.commit.slice(0, 12) : t("commit 未声明")}
                </span>
              )}
            />
            <MetricTile
              label={t("数据库")}
              value={<StatusTag status={runtimeData?.capacity_health.database.status || 'pending'}>{componentStatusLabel(runtimeData?.capacity_health.database.status)}</StatusTag>}
              icon={<IconServer />}
              tone={runtimeData?.capacity_health.database.status === 'connected' ? 'green' : 'red'}
              meta={dependencyProbeMeta(runtimeData?.capacity_health.database)}
            />
            <MetricTile
              label="Redis"
              value={<StatusTag status={runtimeData?.capacity_health.redis.status || 'pending'}>{componentStatusLabel(runtimeData?.capacity_health.redis.status)}</StatusTag>}
              icon={<IconServer />}
              tone={runtimeData?.capacity_health.redis.status === 'connected' ? 'green' : 'amber'}
              meta={dependencyProbeMeta(runtimeData?.capacity_health.redis)}
            />
            <MetricTile
              label={t("近 5 分钟请求")}
              value={runtimeData?.capacity_health.traffic.status === 'available' ? runtimeData.capacity_health.traffic.requests : '-'}
              icon={<IconBell />}
              tone={(runtimeData?.capacity_health.traffic.error_rate || 0) >= 0.05 ? 'amber' : 'blue'}
              meta={runtimeData?.capacity_health.traffic.status === 'not_applicable'
                ? t("客户组织尚未就绪，未查询客户流量")
                : t('错误率 {rate} / tokens {tokens}', { rate: formatPercent(runtimeData?.capacity_health.traffic.error_rate), tokens: runtimeData?.capacity_health.traffic.total_tokens ?? '-' })}
            />
          </div>
          <div className="ur-kpi-grid" style={{ marginTop: 16 }}>
            <MetricTile
              label={t("应用内存")}
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
              label={t("磁盘")}
              value={metricValue(runtimeData?.capacity_health.disk)}
              icon={<IconServer />}
              tone={metricTone(runtimeData?.capacity_health.disk)}
              meta={diskMeta(runtimeData?.capacity_health.disk)}
            />
            <MetricTile
              label={t("网络")}
              value={runtimeData?.capacity_health.network.active_interface_count !== undefined
                ? `${runtimeData.capacity_health.network.active_interface_count}/${runtimeData.capacity_health.network.interface_count ?? '—'}`
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

			{notificationsLicensed ? notificationBlocker ? (
				<Surface title={t("通知关注摘要")} description={t("部署运行时观测仍可用；客户通知保持关闭，直到客户范围可验证。")}>
					<EmptyState
						title={notificationBlocker.title}
						description={notificationBlocker.description}
						minHeight={180}
					/>
				</Surface>
			) : notificationLoadState === 'loading' ? (
				<Surface title={t("通知关注摘要")}>
					<EmptyState title={t("正在加载通知摘要...")} minHeight={180} />
				</Surface>
			) : notificationLoadState === 'error' ? (
				<Surface title={t("通知关注摘要")}>
					<EmptyState
						title={t("通知摘要加载失败")}
						description={failureText(notificationLoadError)}
						actions={<Button icon={<IconRefresh />} onClick={load}>{t("重试")}</Button>}
						minHeight={180}
					/>
				</Surface>
			) : data ? <div className="ur-kpi-grid">
				<MetricTile
					label={t("通知关注级别")}
					value={<StatusTag status={data.attention_level}>{notificationAttentionLabel(data.attention_level)}</StatusTag>}
					icon={<IconBell />}
					tone={data.attention_level === 'critical' ? 'red' : data.attention_level === 'warning' ? 'amber' : 'green'}
					meta={t("仅基于未确认通知，不代表服务健康")}
				/>
				<MetricTile label={t("未确认严重事件")} value={data.open_critical} icon={<IconAlertTriangle />} tone={data.open_critical > 0 ? 'red' : 'slate'} meta={t("处理状态为未关闭的严重通知")} />
				<MetricTile label={t("未确认预警事件")} value={data.open_warning} icon={<IconBell />} tone={data.open_warning > 0 ? 'amber' : 'slate'} meta={t("处理状态为未关闭的预警通知")} />
				<MetricTile label={t("事件处理")} value={<a href={notificationCenterHref}>{t("打开通知中心")}</a>} icon={<IconBell />} tone="blue" meta={t("查看详情、确认或解决事件")} />
			</div> : null : (
        <Surface title={t("通知关注摘要")} description={t("运行状态保持可用；通知事件摘要需要当前商业授权包含通知中心。")}>
          <EmptyState
            title={t("通知摘要已锁定")}
            description={notificationsRestriction}
            minHeight={180}
          />
        </Surface>
      )}


      {canViewRuntime && (routingPolicy || streamTransport || agentCapability || budgetAlerts) && (
        <details style={{ marginTop: 16 }}>
          <summary style={{ cursor: 'pointer', fontWeight: 600, marginBottom: 12 }}>{t("高级运行参数（排障时展开）")}</summary>
      {routingPolicy && (
        <Surface title={t("请求保护策略")} description={t("当前实例处理上游失败、限流和熔断时采用的保护参数。")}>
          <div className="ur-kpi-grid">
            <MetricTile
              label={t("供应商重试")}
              value={t('{count} 次', { count: routingPolicy.max_retry_count })}
              icon={<IconRefresh />}
              tone="blue"
              meta={t("同一次请求中的追加尝试次数")}
            />
            <MetricTile
              label={t("默认熔断阈值")}
              value={t('{count} 次', { count: routingPolicy.circuit_breaker_threshold })}
              icon={<IconAlertTriangle />}
              tone="amber"
              meta={t('各通道可单独设置 / 恢复等待 {seconds} 秒', { seconds: routingPolicy.circuit_breaker_recover_seconds })}
            />
            <MetricTile
              label={t("半开探测流量")}
              value={`${routingPolicy.circuit_breaker_half_open_percent}%`}
              icon={<IconServer />}
              tone="blue"
              meta={t("恢复等待结束后的候选流量比例")}
            />
            <MetricTile
              label={t("429 密钥冷却")}
              value={t('{count} 秒', { count: routingPolicy.rate_limit_cooldown_seconds })}
              icon={<IconBell />}
              tone="amber"
              meta={t("供应商限流后的暂停时间")}
            />
            <MetricTile
              label={t("鉴权失败冷却")}
              value={t('{count} 秒', { count: routingPolicy.auth_failure_cooldown_seconds })}
              icon={<IconAlertTriangle />}
              tone="red"
              meta={t("供应商返回 401/403 后的暂停时间")}
            />
            <MetricTile
              label={t("密钥失效阈值")}
              value={t('{count} 次', { count: routingPolicy.key_failure_threshold })}
              icon={<IconServer />}
              tone="slate"
              meta={t("连续失败后暂停使用该密钥")}
            />
            <MetricTile
              label={t("路由选择版本")}
              value={routingPolicy.selection_version || '-'}
              icon={<IconRoute />}
              tone="blue"
              meta={t("优先级后按同级静态权重")}
            />
            <MetricTile
              label={t("路由快照")}
              value={t('{seconds} 秒刷新', { seconds: routingPolicy.snapshot_refresh_seconds ?? '-' })}
              icon={<IconRefresh />}
              tone="blue"
              meta={t('数据库异常最多沿用 {seconds} 秒', { seconds: routingPolicy.snapshot_stale_seconds ?? '-' })}
            />
            <MetricTile
              label={t("半开探针租约")}
              value={t('{count} 秒', { count: routingPolicy.half_open_probe_lease_seconds ?? '-' })}
              icon={<IconServer />}
              tone="amber"
              meta={t("活动请求会续租；崩溃后到期释放")}
            />
            <MetricTile
              label={t("路由内存边界")}
              value={t('{count} 个候选', { count: routingPolicy.max_candidate_count ?? '-' })}
              icon={<IconServer />}
              tone="slate"
              meta={t('每实例最多 {count} 个模型快照', { count: routingPolicy.max_snapshot_count ?? '-' })}
            />
          </div>
        </Surface>
      )}

      {streamTransport && (
        <Surface title={t("流式连接保护")} description={t("长回答和大工具结果传输时使用的帧大小、心跳与连接时限。")}>
          <div className="ur-kpi-grid">
            <MetricTile
              label={t("单帧安全上限")}
              value={formatBytes(streamTransport.max_sse_line_bytes)}
              icon={<IconServer />}
              tone="blue"
              meta={t("超过上限会安全终止")}
            />
            <MetricTile
              label={t("写入空闲超时")}
              value={t('{count} 秒', { count: streamTransport.write_idle_timeout_seconds })}
              icon={<IconAlertTriangle />}
              tone="amber"
              meta={t("每次成功写入都会重新计时")}
            />
            <MetricTile
              label={t("连接心跳")}
              value={t('{count} 秒', { count: streamTransport.heartbeat_seconds })}
              icon={<IconBell />}
              tone="green"
              meta={t("长时间无模型内容时保持连接可观测")}
            />
            <MetricTile
              label={t("最长流时长")}
              value={t('{count} 分钟', { count: Math.round(streamTransport.max_duration_seconds / 60) })}
              icon={<IconRefresh />}
              tone="slate"
              meta={t("本实例启动时生效")}
            />
          </div>
        </Surface>
      )}

      {agentCapability && (
        <Surface title={t("Agent 使用边界")} description={t("子 Agent 必须显式登记；授权只覆盖模型网关，不包含工具、账号、数据库或导出权限。")}>
          <div className="ur-kpi-grid">
            <MetricTile
              label={t("能力令牌")}
              value={t('{count} 分钟', { count: Math.round(agentCapability.token_ttl_seconds / 60) })}
              icon={<IconServer />}
              tone="violet"
              meta={t('约 {minutes} 分钟刷新，抖动 ±{seconds} 秒', { minutes: Math.round(agentCapability.refresh_base_seconds / 60), seconds: agentCapability.refresh_jitter_seconds })}
            />
            <MetricTile
              label={t("子 Agent 发现")}
              value={t("显式登记")}
              icon={<IconServer />}
              tone="blue"
              meta={t("Supervisor 交换 + 父能力委派")}
            />
            <MetricTile
              label={t("撤销收敛")}
              value={`≤ ${t('{count} 秒', { count: agentCapability.active_revocation_slo_seconds })}`}
              icon={<IconAlertTriangle />}
              tone="green"
              meta={t('Redis 故障：{mode}', { mode: agentCapability.redis_outage_mode === 'fail_closed' ? t('拒绝新准入') : agentCapability.redis_outage_mode })}
            />
            <MetricTile
              label={t("边界")}
              value={t("仅模型网关")}
              icon={<IconServer />}
              tone="slate"
              meta={t('最大委派深度 {count}', { count: agentCapability.max_depth })}
            />
            <MetricTile
              label={t("祖先预算")}
              value={t("共享执行")}
              icon={<IconServer />}
              tone="green"
              meta={t("每层预留成本与 Token，结算后写入实际用量")}
            />
            <MetricTile
              label={t("子树速率 / 并发")}
              value={t("Redis 共享")}
              icon={<IconRefresh />}
              tone="blue"
              meta={t('每层精确窗口 + 到期租约 / 终态缓冲 {seconds} 秒', { seconds: agentCapability.finalization_grace_seconds })}
            />
          </div>
        </Surface>
      )}

      {budgetAlerts && (
        <Surface title={t("预算告警规则")} description={t("按当前租户的项目预算触发，处理记录可在通知中心查看。")}>
          <div className="ur-kpi-grid">
            <MetricTile
              label={t("告警范围")}
              value={budgetAlerts.scope === 'project' ? t("项目级") : budgetAlerts.scope}
              icon={<IconServer />}
              tone="blue"
              meta={t("使用项目预算与预算周期")}
            />
            <MetricTile
              label={t("触发阈值")}
              value={`${budgetAlerts.threshold_percent}%`}
              icon={<IconAlertTriangle />}
              tone="amber"
              meta={t("项目当期已结算用量达到预算")}
            />
            <MetricTile
              label={t("事件与投递")}
              value={t("租户隔离")}
              icon={<IconBell />}
              tone="green"
              meta={t("通知中心记录并按 Webhook 路由投递")}
            />
            <MetricTile
              label={t("旧全局配置")}
              value={budgetAlerts.legacy_global_settings_active ? t("仍在生效") : t("已停用")}
              icon={<IconServer />}
              tone={budgetAlerts.legacy_global_settings_active ? 'red' : 'slate'}
              meta={t("仅项目级规则生效")}
            />
          </div>
        </Surface>
      )}
        </details>
      )}

      {canViewRuntime && runtimeData?.topology && (
        <Surface title={t("受限拓扑")} description={t("当前进程与逻辑依赖关系；地址、主机标识和网络暴露状态不在应用内推断。")}>
          <div className="ur-chip-row" style={{ marginBottom: 12 }}>
            <TagStatus label={t("可见性")} value={runtimeData.topology.visibility} />
            <TagStatus label={t("网络暴露")} value={runtimeData.topology.network_exposure} />
            <TagStatus label={t("多实例")} value={runtimeData.topology.multi_instance_state} />
          </div>
          <div className="ur-table-shell">
            <Table
              rowKey="id"
              size="small"
              pagination={false}
              dataSource={runtimeData.topology.nodes}
              columns={[
                { title: t("逻辑节点"), dataIndex: 'id', render: (value: string) => <Text code>{value}</Text> },
                { title: t("类型"), dataIndex: 'kind', width: 140, render: (value: string) => topologyKindLabel(value) },
                { title: t("角色"), dataIndex: 'role', width: 180, render: (value: string) => topologyRoleLabel(value) },
                { title: t("状态"), dataIndex: 'status', width: 140, render: (value: string) => <StatusTag status={value}>{componentStatusLabel(value)}</StatusTag> },
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
                { title: t("来源"), dataIndex: 'from', render: (value: string) => <Text code>{value}</Text> },
                { title: t("依赖关系"), dataIndex: 'kind', width: 160, render: (value: string) => topologyRelationLabel(value) },
                { title: t("目标"), dataIndex: 'to', render: (value: string) => <Text code>{value}</Text> },
                { title: t("状态"), dataIndex: 'status', width: 140, render: (value: string) => <StatusTag status={value}>{componentStatusLabel(value)}</StatusTag> },
              ]}
              empty={<Text type="tertiary">{t("当前运行时未返回依赖边。")}</Text>}
            />
          </div>
        </Surface>
      )}
    </div>
  );
}

function TagStatus({ label, value }: { label: string; value: string }) {
  return <StatusTag status={value}>{label}：{componentStatusLabel(value)}</StatusTag>;
}
