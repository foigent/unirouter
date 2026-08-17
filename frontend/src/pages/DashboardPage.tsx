import { formatDateTime } from '../utils/formatDate';
import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { Typography, Table, Spin, Tag, Tooltip, Button, Progress, Checkbox } from '@douyinfe/semi-ui';
import {
  IconActivity,
  IconAlertTriangle,
  IconBell,
  IconClock,
  IconCoinMoney,
  IconHistogram,
  IconMinusCircle,
  IconPercentage,
  IconPulse,
  IconRefresh,
  IconServer,
  IconTickCircle,
} from '@douyinfe/semi-icons';
import {
  AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip as RTooltip,
  ResponsiveContainer,
} from 'recharts';
import { api } from '../api/client';
import { dashboardApi, type DashboardStats } from '../api/dashboard';
import { EmptyState, LatencyText, MetricTile, MoneyText, PageHeader, StatusTag, Surface, formatMoney, microsToChartYuan, parseMicros } from '../ui';
import { formatCompactNumber } from '../utils/formatNumber';
import { useAuthStore } from '../store/auth';
import { authzContextForUser, hasLicensedFeature, isBillingRole, isOrgViewerRole, isPlatformAdminRole, isRouteAllowedForAuthz } from '../navigation';
import { customerContextBlockerCopy, effectiveCustomerOrgID, resolveCustomerContext } from '../utils/customerContext';

const { Text } = Typography;

interface DashboardAttention {
  summary_kind: 'notification_events';
  attention_level: string;
  open_critical: number;
  open_warning: number;
}

type AttentionLoadState = 'idle' | 'loading' | 'ready' | 'error' | 'locked';

// Semi Table compares dataSource references during componentDidUpdate. Keep the
// empty state stable so unrelated dashboard state updates cannot trigger an
// update loop while there are no recent requests.
const EMPTY_RECENT_LOGS: DashboardStats['recent_logs'] = [];

type TimeRange = '24h' | '7d' | '30d' | 'custom';
type TrendMetric = 'requests' | 'cost_yuan';

const TREND_METRIC_META: Record<TrendMetric, { label: string; color: string; yAxisId: TrendMetric }> = {
  requests: { label: '请求数', color: '#1f6feb', yAxisId: 'requests' },
  cost_yuan: { label: '费用', color: '#6f4bd8', yAxisId: 'cost_yuan' },
};

function getRangeParams(range: TimeRange, customStart?: string, customEnd?: string): { startTime?: string; endTime?: string } {
  const now = new Date();
  const fmt = (d: Date) => d.toISOString();
  switch (range) {
    case '24h':
      return { startTime: fmt(new Date(now.getTime() - 86400000)), endTime: fmt(now) };
    case '7d':
      return { startTime: fmt(new Date(now.getTime() - 7 * 86400000)), endTime: fmt(now) };
    case '30d':
      return { startTime: fmt(new Date(now.getTime() - 30 * 86400000)), endTime: fmt(now) };
    case 'custom':
      return { startTime: customStart, endTime: customEnd };
  }
}

function formatTrendLabel(value: string): string {
  if (!value) return '';
  const d = new Date(value);
  if (!Number.isNaN(d.getTime()) && value.includes('T')) {
    return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:00`;
  }
  return value.length >= 10 ? value.slice(5, 10) : value;
}

function formatYuan(n: number): string {
  if (n >= 1_000_000) return `¥${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `¥${(n / 1_000).toFixed(1)}K`;
  if (n >= 10) return `¥${n.toFixed(0)}`;
  return `¥${n.toFixed(2)}`;
}

function DeltaTag({ value, suffix = '', inverse = false }: { value: number; suffix?: string; inverse?: boolean }) {
  const isPositive = value > 0;
  const color = inverse
    ? (isPositive ? 'red' : 'green')
    : (isPositive ? 'green' : 'red');
  if (Math.abs(value) < 0.001) return <Text type="tertiary" size="small">—</Text>;
  const sign = isPositive ? '+' : '';
  const displayValue = Math.abs(value) < 100 ? value.toFixed(2) : formatCompactNumber(value);
  return (
    <Tag size="small" color={color} style={{ marginLeft: 8, fontWeight: 600 }}>
      {sign}{displayValue}{suffix}
    </Tag>
  );
}

function latencyTone(value: number): 'green' | 'amber' | 'red' {
  if (value > 2000) return 'red';
  if (value > 500) return 'amber';
  return 'green';
}

function providerDisplayName(provider: string, channelName: string): string | null {
  if (!provider) return null;
  if (provider.toLowerCase() === channelName.toLowerCase()) return null;
  if (provider === 'openai' && !channelName.toLowerCase().includes('openai')) return '兼容 OpenAI 协议';
  return provider;
}

function endpointHost(baseURL?: string): string {
  if (!baseURL) return '';
  try {
    return new URL(baseURL).host;
  } catch {
    return baseURL;
  }
}

function channelHealthView(ch: NonNullable<DashboardStats['channel_health']>[number]) {
  if (ch.status === 'disabled') return { tone: 'disabled', label: '禁用', icon: <IconMinusCircle /> };
  if (ch.status === 'stale_release') return { tone: 'error', label: '发布记录已过期', icon: <IconAlertTriangle /> };
  if (ch.status === 'projection_unavailable') return { tone: 'error', label: '路由状态不可判定', icon: <IconAlertTriangle /> };
  if (ch.status === 'awaiting_release') return { tone: 'disabled', label: '待启用', icon: <IconMinusCircle /> };
  if (ch.status === 'standby') return { tone: 'disabled', label: '未承载当前路由', icon: <IconMinusCircle /> };
  if (ch.status === 'no_credential') return { tone: 'warning', label: '缺少凭证', icon: <IconAlertTriangle /> };
  if (ch.status === 'circuit_unavailable') return { tone: 'error', label: '熔断不可用', icon: <IconAlertTriangle /> };
  if (!ch.enabled) return { tone: 'disabled', label: '待启用', icon: <IconMinusCircle /> };
  if (ch.status === 'error') return { tone: 'error', label: '异常', icon: <IconAlertTriangle /> };
  if (ch.status === 'warning' || ch.status === 'degraded') return { tone: 'warning', label: '告警', icon: <IconAlertTriangle /> };
  return { tone: 'healthy', label: '健康', icon: <IconTickCircle /> };
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

function ChannelHealthCard({ ch, onOpen }: { ch: NonNullable<DashboardStats['channel_health']>[number]; onOpen: () => void }) {
  const total = ch.success_count + ch.fail_count;
  const rate = total > 0 ? (ch.success_count / total * 100) : 0;
  const providerLabel = providerDisplayName(ch.provider, ch.name);
  const health = channelHealthView(ch);
  const models = ch.public_models || [];
  const host = endpointHost(ch.base_url);

  return (
    <button type="button" className="ur-channel-card" onClick={onOpen} aria-label={`查看模型连接 ${ch.name}`}>
      <div style={{ minWidth: 0 }}>
        <Text className="ur-channel-name" title={ch.name}>{ch.name}</Text>
        {providerLabel && <Text className="ur-channel-provider" title={ch.provider}>供应商：{providerLabel}</Text>}
        {(models.length > 0 || host) && (
          <Text className="ur-channel-provider" title={[models.join('、'), host].filter(Boolean).join(' · ')}>
            {[models.length > 0 ? `模型：${models.join('、')}` : '', host].filter(Boolean).join(' · ')}
          </Text>
        )}
        <Progress
          percent={rate}
          showInfo={false}
          size="small"
          className="ur-channel-progress"
          stroke={rate >= 99 ? '#13a671' : rate >= 90 ? '#bd7a13' : '#d94c48'}
        />
        <div className="ur-channel-meta">
          <div className="ur-channel-stat">
            <Text className="ur-channel-stat-label">窗口成功率</Text>
            <Text className="ur-channel-stat-value">{rate.toFixed(1)}%</Text>
          </div>
          <div className="ur-channel-stat">
            <Text className="ur-channel-stat-label">窗口请求</Text>
            <Text className="ur-channel-stat-value">{formatCompactNumber(total)}</Text>
          </div>
          <div className="ur-channel-stat">
            <Text className="ur-channel-stat-label">窗口失败</Text>
            <Text className="ur-channel-stat-value">{formatCompactNumber(ch.fail_count)}</Text>
          </div>
        </div>
      </div>
      <div className={`ur-channel-state ur-channel-state-${health.tone}`}>
        {health.icon}
        <span>{health.label}</span>
      </div>
    </button>
  );
}

function LatencyPercentilePanel({ avg, p95, p99 }: { avg: number; p95: number; p99: number }) {
  const rows = [
    { label: 'Avg', value: avg },
    { label: 'P95', value: p95 },
    { label: 'P99', value: p99 },
  ];

  return (
    <div className="ur-latency-stack">
      {rows.map(row => {
        const tone = latencyTone(row.value);
        return (
          <div key={row.label} className={`ur-latency-row ur-latency-row-${tone}`}>
            <div className="ur-latency-row-head">
              <Text>{row.label}</Text>
              <strong>{row.value.toFixed(0)}ms</strong>
            </div>
            <Progress
              percent={Math.min(100, row.value / 2500 * 100)}
              showInfo={false}
              size="small"
              stroke={tone === 'red' ? '#d94c48' : tone === 'amber' ? '#bd7a13' : '#13a671'}
            />
          </div>
        );
      })}
    </div>
  );
}

function TrendTooltip({ active, payload, label }: any) {
  if (!active || !payload?.length) return null;
  return (
    <div className="ur-chart-tooltip">
      <Text size="small" style={{ fontWeight: 600 }}>{label}</Text>
      {payload.map((p: any) => (
        <div key={p.dataKey} style={{ marginTop: 4, fontSize: 13 }}>
          <span style={{ color: p.color, marginRight: 6 }}>●</span>
          {TREND_METRIC_META[p.dataKey as TrendMetric]?.label || p.name}: <strong>{p.dataKey === 'cost_yuan' ? formatYuan(Number(p.value)) : formatCompactNumber(Number(p.value))}</strong>
        </div>
      ))}
    </div>
  );
}

export default function DashboardPage() {
  const navigate = useNavigate();
  const user = useAuthStore((state) => state.user);
  const authz = authzContextForUser(user);
  const organizationRole = authz.organizationRole;
  const customerContext = resolveCustomerContext(user);
  const selectedOrgID = effectiveCustomerOrgID(user);
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [range, setRange] = useState<TimeRange>('24h');
  const [lastUpdated, setLastUpdated] = useState<Date>(new Date());
  const [visibleTrendMetrics, setVisibleTrendMetrics] = useState<TrendMetric[]>(['requests', 'cost_yuan']);
  const platform = isPlatformAdminRole(organizationRole);
  const [attention, setAttention] = useState<DashboardAttention | null>(null);
  const [attentionLoadState, setAttentionLoadState] = useState<AttentionLoadState>('idle');
  const requestVersion = useRef(0);
  const attentionRequestVersion = useRef(0);
  const notificationsLicensed = hasLicensedFeature(user?.license_access, 'notifications');

  const fetchData = useCallback(async () => {
    const version = ++requestVersion.current;
    if (customerContext.state !== 'ready' || !selectedOrgID) {
      setStats(null);
      setLoading(false);
      setLoadError('');
      return;
    }
    setLoading(true);
    setLoadError('');
    try {
      const params = getRangeParams(range);
      const data = await dashboardApi.getStats(params.startTime, params.endTime, platform ? selectedOrgID : undefined);
      if (version !== requestVersion.current) return;
      setStats(data);
      setLastUpdated(new Date());
    } catch (e) {
      if (version !== requestVersion.current) return;
      console.error('Failed to load dashboard stats', e);
      setStats(null);
      setLoadError('无法读取当前权限范围的总览数据。请检查网络或服务状态后重试。');
    } finally {
      if (version === requestVersion.current) setLoading(false);
    }
  }, [customerContext.state, platform, range, selectedOrgID]);

  const fetchAttention = useCallback(async () => {
    const version = ++attentionRequestVersion.current;
    if (customerContext.state !== 'ready' || !selectedOrgID) {
      setAttention(null);
      setAttentionLoadState('idle');
      return;
    }
    if (!platform) {
      setAttention(null);
      setAttentionLoadState('idle');
      return;
    }
    if (!notificationsLicensed) {
      setAttention(null);
      setAttentionLoadState('locked');
      return;
    }
    setAttention(null);
    setAttentionLoadState('loading');
    try {
      const data = await api.getInternalStatus(selectedOrgID ? { org_id: selectedOrgID } : undefined);
      if (!data) throw new Error('服务未返回通知摘要');
      if (version !== attentionRequestVersion.current) return;
      setAttention(data);
      setAttentionLoadState('ready');
    } catch {
      if (version !== attentionRequestVersion.current) return;
      setAttention(null);
      setAttentionLoadState('error');
    }
  }, [customerContext.state, notificationsLicensed, platform, selectedOrgID]);

  const refreshDashboard = useCallback(() => {
    void fetchData();
    void fetchAttention();
  }, [fetchAttention, fetchData]);

  useEffect(() => { refreshDashboard(); }, [refreshDashboard]);

  const comparableDelta = range === '24h' ? stats?.delta_vs_yesterday : undefined;
  const trendData = (stats?.daily_trend || []).map(d => ({
    date: formatTrendLabel(d.date),
    requests: d.requests,
		cost_yuan: microsToChartYuan(d.cost_micros),
  }));
  const modelData = [...(stats?.model_distribution || [])]
    .sort((a, b) => b.requests - a.requests);
	const walletThreshold = stats?.wallet_threshold_micros || '0';
	const walletAvailable = parseMicros(stats?.wallet_balance_micros || '0') + parseMicros(stats?.wallet_granted_micros || '0');
  const hasWalletThreshold = parseMicros(walletThreshold) > 0n;
  const lowBalance = hasWalletThreshold && walletAvailable <= parseMicros(walletThreshold);
  const scopeLabel = customerContext.orgName || (selectedOrgID ? `组织 #${selectedOrgID}` : '未确认客户组织');
  const avgLatency = stats?.avg_latency_ms || 0;
  const p95Latency = stats?.p95_latency_ms ?? avgLatency;
  const p99Latency = stats?.p99_latency_ms ?? p95Latency;
  const healthyChannels = (stats?.channel_health || []).filter(ch => ch.available ?? channelHealthView(ch).tone === 'healthy').length;
  const channelTotal = (stats?.channel_health || []).length;
  const enabledUnhealthyChannels = (stats?.channel_health || []).filter(ch => {
    const tone = channelHealthView(ch).tone;
    return ch.enabled && (tone === 'warning' || tone === 'error');
  }).length;
  const recentChannelFailures = (stats?.channel_health || []).reduce((total, ch) => total + ch.fail_count, 0);
  const hasWindowSample = (stats?.total_requests || 0) > 0 || (stats?.channel_health || []).some(ch => ch.success_count + ch.fail_count > 0);
  const windowNeedsAttention = hasWindowSample && (enabledUnhealthyChannels > 0 || (stats?.success_rate || 0) < 0.99);
  const openNotificationCount = (attention?.open_critical || 0) + (attention?.open_warning || 0);
  const canOpenNotifications = notificationsLicensed && isRouteAllowedForAuthz('/notifications', authz);
  const canReadWallet = platform || isBillingRole(organizationRole);
  const canReadChannelHealth = platform;
  const canOpenRecentRequests = isRouteAllowedForAuthz('/logs', authz);
  const dashboardTitle = isPlatformAdminRole(organizationRole)
    ? '客户运营总览'
    : isBillingRole(organizationRole)
      ? '财务与用量总览'
      : isOrgViewerRole(organizationRole)
        ? '组织只读总览'
        : '组织运营总览';
  const dashboardDescription = isPlatformAdminRole(organizationRole)
    ? '统一观察当前客户的请求量、成功率、延迟、成本和通道健康状态。'
    : isBillingRole(organizationRole)
      ? '统一观察用量、费用、余额和近期请求状态。'
      : '统一观察当前组织的请求量、成功率、延迟和费用。';
  const recentCols = useMemo(() => [
    {
      title: 'Request ID',
      dataIndex: 'request_id',
      key: 'request_id',
      render: (v: string) => (
        <Tooltip content={v}>
          <Text code style={{ maxWidth: 120 }} ellipsis={{ showTooltip: false }}>{v.slice(0, 12)}…</Text>
        </Tooltip>
      ),
    },
    {
      title: '模型',
      dataIndex: 'model',
      key: 'model',
      render: (v: string) => <Tag size="small">{v}</Tag>,
    },
    {
      title: '端点',
      key: 'endpoint',
      render: (_: unknown, row: NonNullable<DashboardStats['recent_logs']>[number]) => row.route_channel_id
        ? <Tooltip content={[row.provider, row.channel].filter(Boolean).join(' · ')}><Text>{row.channel || `端点 #${row.route_channel_id}`}</Text></Tooltip>
        : <Text type="tertiary">未选择路由</Text>,
    },
    {
      title: '状态码',
      dataIndex: 'status_code',
      key: 'status_code',
      render: (v: number) => <StatusTag status={v}>{v}</StatusTag>,
    },
    {
      title: '延迟',
      dataIndex: 'latency_ms',
      key: 'latency_ms',
      render: (v: number) => <LatencyText value={v} />,
    },
    {
      title: '费用',
		dataIndex: 'cost_micros',
		key: 'cost_micros',
		render: (v: string) => <MoneyText micros={v || '0'} precision={6} tone="cost" />,
    },
    {
      title: '时间',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (v: string) => formatDateTime(v),
    },
  ], []);

  const timeRanges: { label: string; value: TimeRange }[] = [
    { label: '24h', value: '24h' },
    { label: '7 天', value: '7d' },
    { label: '30 天', value: '30d' },
  ];

  const toggleTrendMetric = (metric: TrendMetric) => {
    setVisibleTrendMetrics(prev => {
      if (prev.includes(metric)) {
        return prev.length === 1 ? prev : prev.filter(item => item !== metric);
      }
      return [...prev, metric];
    });
  };

  if (customerContext.state !== 'ready') {
    const blocker = customerContextBlockerCopy(customerContext.state);
    return (
      <div className="ur-page ur-page-wide">
        <PageHeader eyebrow="概览" title="客户运营总览" description="本页只展示当前部署唯一客户组织的运行与用量数据。" />
        <Surface>
          <EmptyState title={blocker.title} description={blocker.description} minHeight={280} />
        </Surface>
      </div>
    );
  }

  const dashboardHeader = (
    <PageHeader
      eyebrow="概览"
      title={dashboardTitle}
      description={`数据范围：${scopeLabel}。${dashboardDescription}`}
      actions={(
        <>
          <div className="ur-toolbar time-range-selector">
            {timeRanges.map(t => (
              <Button
                key={t.value}
                size="small"
                theme={range === t.value ? 'solid' : 'borderless'}
                type={range === t.value ? 'primary' : 'tertiary'}
                onClick={() => setRange(t.value)}
              >
                {t.label}
              </Button>
            ))}
          </div>
          <Tooltip content="刷新">
            <Button icon={<IconRefresh />} size="small" onClick={refreshDashboard} className="ur-icon-button" aria-label="刷新总览" />
          </Tooltip>
        </>
      )}
    />
  );

  if (!loading && loadError) {
    return (
      <div className="ur-page">
        <section className="ur-dashboard-hero">{dashboardHeader}</section>
        <Surface>
          <EmptyState
            title="总览数据加载失败"
            description={loadError}
            actions={<Button type="primary" icon={<IconRefresh />} onClick={refreshDashboard} aria-label="重新加载总览">重新加载</Button>}
          />
        </Surface>
      </div>
    );
  }

  return (
    <Spin spinning={loading} size="large">
      <div className="ur-page">
        <section className="ur-dashboard-hero">
          {dashboardHeader}
          <div className="ur-dashboard-status-strip">
            <div className="ur-dashboard-status-card">
              <IconPulse />
              <div>
                <Text>更新时间</Text>
                <strong>{lastUpdated.toLocaleTimeString()}</strong>
              </div>
            </div>
            {canReadChannelHealth && (
              <div className="ur-dashboard-status-card">
                <IconServer />
                <div>
                  <Text>当前可路由连接</Text>
                  <strong>{healthyChannels}/{channelTotal || 0}</strong>
                  <Text type="tertiary" size="small">按模型端点与网关当前路由判定</Text>
                </div>
              </div>
            )}
            {canReadWallet && (
              <div className="ur-dashboard-status-card">
                <IconCoinMoney />
                <div>
                  <Text>结算可用额度</Text>
				  <strong>{formatMoney(walletAvailable)}</strong>
                  <div style={{ marginTop: 4 }}>
                    {hasWalletThreshold ? (
                      <><StatusTag status={lowBalance ? 'warning' : 'available'}>{lowBalance ? '已触发低余额' : '余额正常'}</StatusTag><Text type="tertiary" size="small" style={{ marginLeft: 6 }}>阈值 {formatMoney(walletThreshold)}</Text></>
                    ) : <Text type="tertiary" size="small">低余额阈值未配置</Text>}
                  </div>
                </div>
              </div>
            )}
          </div>
        </section>

        <div className="ur-kpi-grid">
          <MetricTile
            label="总请求数"
            value={formatCompactNumber(stats?.total_requests || 0)}
            tone="blue"
            icon={<IconHistogram />}
            meta={<Text type="tertiary" size="small">当前窗口累计</Text>}
          />
          <MetricTile
            label="全模型成功率"
            value={`${((stats?.success_rate || 0) * 100).toFixed(2)}%`}
            tone={(stats?.success_rate || 0) >= 0.99 ? 'green' : 'red'}
            icon={<IconPercentage />}
            meta={comparableDelta && <><Text type="tertiary" size="small">较前 24h</Text><DeltaTag value={comparableDelta.success_rate * 100} suffix=" 个百分点" /></>}
          />
          <MetricTile
            label="全模型 P95 延迟"
            value={`${p95Latency.toFixed(0)}ms`}
            tone={latencyTone(p95Latency)}
            icon={<IconClock />}
            meta={(
              <div className="ur-kpi-meta-stack">
                <Text type="tertiary" size="small">Avg {avgLatency.toFixed(0)}ms · P99 {p99Latency.toFixed(0)}ms</Text>
                {comparableDelta && <div><Text type="tertiary" size="small">较前 24h</Text><DeltaTag value={comparableDelta.latency_ms} suffix="ms" inverse /></div>}
              </div>
            )}
          />
          <MetricTile
            label="当前窗口费用"
			value={<MoneyText micros={stats?.today_cost_micros || '0'} precision={6} strong tone="cost" />}
            tone="violet"
            icon={<IconCoinMoney />}
            meta={comparableDelta ? (
              <><Text type="tertiary" size="small">较前 24h</Text><DeltaTag value={microsToChartYuan(comparableDelta.cost_micros)} suffix=" 元" inverse /></>
            ) : <Text type="tertiary" size="small">按所选时间窗口累计</Text>}
          />
        </div>

        {platform && (
          <Surface
            title="运行关注与待处理动作"
            description="只汇总当前窗口信号和未确认通知，不把统计相关性写成根因；深度归因仍进入 X-Insight 调查工作区。"
            actions={(
              <div className="ur-toolbar">
                <Button size="small" onClick={() => navigate('/status')}>查看运行状态</Button>
                {canOpenNotifications && <Button size="small" type="primary" onClick={() => navigate('/notifications')}>处理通知</Button>}
              </div>
            )}
          >
            <div className="ur-dashboard-attention-grid">
              <div className="ur-dashboard-status-card">
                <IconPulse />
                <div>
                  <Text>运行信号</Text>
                  <strong>
                    <StatusTag status={!hasWindowSample ? 'pending' : windowNeedsAttention ? 'warning' : 'available'}>
                      {!hasWindowSample ? '暂无可判定样本' : windowNeedsAttention ? '需要关注' : '当前窗口正常'}
                    </StatusTag>
                  </strong>
                  <Text>{hasWindowSample ? `异常通道 ${enabledUnhealthyChannels} · 失败请求 ${formatCompactNumber(recentChannelFailures)}` : '等待真实请求后评估成功率与通道窗口状态'}</Text>
                </div>
              </div>
              <div className="ur-dashboard-status-card">
                <IconBell />
                <div>
                  <Text>未确认通知</Text>
                  <strong>
                    {attentionLoadState === 'ready' && attention ? (
                      <StatusTag status={attention.attention_level}>{notificationAttentionLabel(attention.attention_level)} · {openNotificationCount}</StatusTag>
                    ) : attentionLoadState === 'loading' ? (
                      <StatusTag status="pending">读取中</StatusTag>
                    ) : attentionLoadState === 'locked' ? (
                      <StatusTag status="disabled">商业授权不可用</StatusTag>
                    ) : (
                      <StatusTag status="error">读取失败</StatusTag>
                    )}
                  </strong>
                  <Text>{attentionLoadState === 'ready' && attention ? `严重 ${attention.open_critical} · 预警 ${attention.open_warning}` : '不以缺失或读取失败冒充零事件'}</Text>
                </div>
              </div>
              <div className="ur-dashboard-status-card">
                <IconCoinMoney />
                <div>
                  <Text>结算风险</Text>
                  <strong>
                    <StatusTag status={!hasWalletThreshold ? 'pending' : lowBalance ? 'warning' : 'available'}>
                      {!hasWalletThreshold ? '阈值未配置' : lowBalance ? '低余额待处理' : '余额正常'}
                    </StatusTag>
                  </strong>
                  <Text>{hasWalletThreshold ? `可用 ${formatMoney(walletAvailable)} · 阈值 ${formatMoney(walletThreshold)}` : '配置企业低余额阈值后才进行风险判断'}</Text>
                </div>
              </div>
            </div>
          </Surface>
        )}

        <div className="ur-dashboard-analytics-grid">
          <Surface
            title="AI 活动趋势"
            description="按当前时间窗口展示请求量与费用走势，帮助对照流量和成本变化。"
            className="ur-primary-chart"
            actions={(
              <div className="ur-toolbar ur-trend-checklist">
                {(Object.keys(TREND_METRIC_META) as TrendMetric[]).map(metric => (
                  <Checkbox
                    key={metric}
                    checked={visibleTrendMetrics.includes(metric)}
                    onChange={() => toggleTrendMetric(metric)}
                    aria-label={`显示${TREND_METRIC_META[metric].label}`}
                  >
                    <span className="ur-trend-check-label" style={{ color: TREND_METRIC_META[metric].color }}>
                      {TREND_METRIC_META[metric].label}
                    </span>
                  </Checkbox>
                ))}
              </div>
            )}
          >
            {trendData.length > 0 ? (
              <ResponsiveContainer width="100%" height={280}>
                <AreaChart data={trendData} margin={{ top: 10, right: 12, left: 0, bottom: 0 }}>
                  <defs>
                    <linearGradient id="colorRequests" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#1f6feb" stopOpacity={0.18} />
                      <stop offset="95%" stopColor="#1f6feb" stopOpacity={0} />
                    </linearGradient>
                    <linearGradient id="colorCost" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#6f4bd8" stopOpacity={0.18} />
                      <stop offset="95%" stopColor="#6f4bd8" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" stroke="var(--ur-border-soft)" />
                  <XAxis dataKey="date" tick={{ fontSize: 12 }} stroke="var(--ur-text-muted)" />
                  <YAxis
                    yAxisId="requests"
                    tick={{ fontSize: 12 }}
                    stroke="var(--ur-text-muted)"
                    tickFormatter={formatCompactNumber}
                    allowDecimals={false}
                  />
                  <YAxis
                    yAxisId="cost_yuan"
                    orientation="right"
                    tick={{ fontSize: 12 }}
                    stroke="var(--ur-text-muted)"
                    tickFormatter={formatYuan}
                    width={64}
                  />
                  <RTooltip content={<TrendTooltip />} />
                  {visibleTrendMetrics.includes('requests') && (
                    <Area
                      yAxisId="requests"
                      type="monotone"
                      dataKey="requests"
                      name="请求数"
                      stroke={TREND_METRIC_META.requests.color}
                      strokeWidth={2}
                      fill="url(#colorRequests)"
                      dot={false}
                      isAnimationActive={false}
                      activeDot={{ r: 5, strokeWidth: 2 }}
                    />
                  )}
                  {visibleTrendMetrics.includes('cost_yuan') && (
                    <Area
                      yAxisId="cost_yuan"
                      type="monotone"
                      dataKey="cost_yuan"
                      name="费用"
                      stroke={TREND_METRIC_META.cost_yuan.color}
                      strokeWidth={2}
                      fill="url(#colorCost)"
                      dot={false}
                      isAnimationActive={false}
                      activeDot={{ r: 5, strokeWidth: 2 }}
                    />
                  )}
                </AreaChart>
              </ResponsiveContainer>
            ) : (
              <EmptyState title="暂无趋势数据" minHeight={280} />
            )}
          </Surface>

          <Surface title="全模型延迟分位" description="当前窗口全部模型合并后的平均与尾部延迟；逐模型数据见下方。" className="ur-latency-surface">
            <div className="ur-latency-heading">
              <IconActivity />
              <div>
                <Text>P95 延迟</Text>
                <strong>{p95Latency.toFixed(0)}ms</strong>
              </div>
            </div>
            <LatencyPercentilePanel avg={avgLatency} p95={p95Latency} p99={p99Latency} />
          </Surface>
        </div>

        <div className={canReadChannelHealth ? 'ur-split-grid' : undefined}>
          <Surface title="模型运行状态" description="逐公共模型展示当前窗口请求、成功率和 P95；模型身份直接来自网关已解析的请求记录，不按名称猜测合并。">
            {modelData.length > 0 ? (
              <Table
                size="small"
                pagination={false}
                rowKey="model"
                dataSource={modelData}
                scroll={{ y: 300 }}
                columns={[
                  { title: '模型', dataIndex: 'model', render: (value: string) => <Tag size="small">{value}</Tag> },
                  { title: '请求', dataIndex: 'requests', render: (value: number) => formatCompactNumber(value) },
                  {
                    title: '窗口状态',
                    render: (_: unknown, row: NonNullable<DashboardStats['model_distribution']>[number]) => {
                      const rate = row.success_rate ?? (row.requests > 0 ? (row.success_count || 0) / row.requests : 0);
                      const status = rate >= 0.99 ? 'available' : rate >= 0.9 ? 'warning' : 'error';
                      return <StatusTag status={status}>{(rate * 100).toFixed(1)}%</StatusTag>;
                    },
                  },
                  {
                    title: 'P95 延迟',
                    dataIndex: 'p95_latency_ms',
                    render: (value?: number) => value === undefined ? '—' : <LatencyText value={value} />,
                  },
                ]}
              />
            ) : (
              <EmptyState title="暂无模型数据" />
            )}
          </Surface>

          {canReadChannelHealth && (
            <Surface title="连接可用状态" description="当前是否可路由与窗口成功率分开显示；点击连接可进入处理。">
              {(stats?.channel_health || []).length > 0 ? (
                <div className="ur-channel-list">
                  {(stats?.channel_health || []).map(ch => (
                    <ChannelHealthCard key={ch.id} ch={ch} onOpen={() => navigate(`/channels?channel_id=${ch.id}`)} />
                  ))}
                </div>
              ) : (
                <EmptyState title="暂无通道数据" />
              )}
            </Surface>
          )}
        </div>

        <Surface
          title="最近请求"
          description={canOpenRecentRequests
            ? '点击任意请求可跳转到日志探索器并带入 Request ID。'
            : '展示当前权限范围内的最近请求摘要；当前只读角色不提供日志明细入口。'}
        >
          <div className="ur-table-shell">
            <Table
              dataSource={stats?.recent_logs ?? EMPTY_RECENT_LOGS}
              columns={recentCols}
              rowKey="request_id"
              size="small"
              pagination={false}
			  empty="暂无请求记录"
			  onRow={canOpenRecentRequests ? (record) => ({
				onClick: () => {
				  if (record) navigate(`/logs?request_id=${record.request_id}`);
				},
				style: { cursor: 'pointer' },
			  }) : undefined}
            />
          </div>
        </Surface>
      </div>
    </Spin>
  );
}
