import { formatDateTime } from '../utils/formatDate';
import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { Typography, Table, Spin, Tag, Tooltip, Button, Progress, Checkbox, Select } from '@douyinfe/semi-ui';
import {
  IconActivity,
  IconAlertTriangle,
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
  ResponsiveContainer, BarChart, Bar, Cell,
} from 'recharts';
import { api } from '../api/client';
import { EmptyState, LatencyText, MetricTile, MoneyText, PageHeader, StatusTag, Surface, formatMoney, microsToChartYuan, parseMicros } from '../ui';
import { formatCompactNumber } from '../utils/formatNumber';
import { useAuthStore } from '../store/auth';
import { isBillingRole, isOrgViewerRole, isPlatformAdminRole, isRouteAllowedForRole } from '../navigation';
import { loadAllPages } from '../utils/loadAllPages';

const { Text } = Typography;

interface DashboardStats {
  total_requests: number;
	today_cost_micros: string;
	currency: 'CNY';
	amount_scale: 1000000;
  avg_latency_ms: number;
  p95_latency_ms?: number;
  p99_latency_ms?: number;
  success_rate: number;
  recent_logs: any[];
	model_prices?: { model_name: string; input_price_cny_per_million: string; output_price_cny_per_million: string }[];
	wallet_balance_micros?: string;
	wallet_granted_micros?: string;
	wallet_frozen_micros?: string;
	wallet_threshold_micros?: string;
	daily_trend?: { date: string; requests: number; cost_micros: string }[];
  model_distribution?: { model: string; requests: number }[];
  channel_health?: { id: number; name: string; provider: string; enabled: boolean; status: string; success_count: number; fail_count: number }[];
	delta_vs_yesterday?: { cost_micros: string; latency_ms: number; success_rate: number };
}

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

const MODEL_COLORS = ['#1f6feb', '#0f8f5f', '#b76e00', '#6f4bd8', '#d92d20', '#0e7490', '#be185d', '#475467'];

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

function channelHealthView(ch: NonNullable<DashboardStats['channel_health']>[number]) {
  if (!ch.enabled) return { tone: 'disabled', label: '禁用', icon: <IconMinusCircle /> };
  if (ch.status === 'error') return { tone: 'error', label: '异常', icon: <IconAlertTriangle /> };
  if (ch.status === 'warning' || ch.status === 'degraded') return { tone: 'warning', label: '告警', icon: <IconAlertTriangle /> };
  return { tone: 'healthy', label: '健康', icon: <IconTickCircle /> };
}

function ChannelHealthCard({ ch }: { ch: NonNullable<DashboardStats['channel_health']>[number] }) {
  const total = ch.success_count + ch.fail_count;
  const rate = total > 0 ? (ch.success_count / total * 100) : 0;
  const providerLabel = providerDisplayName(ch.provider, ch.name);
  const health = channelHealthView(ch);

  return (
    <div className="ur-channel-card">
      <div style={{ minWidth: 0 }}>
        <Text className="ur-channel-name" title={ch.name}>{ch.name}</Text>
        {providerLabel && <Text className="ur-channel-provider" title={ch.provider}>供应商：{providerLabel}</Text>}
        <Progress
          percent={rate}
          showInfo={false}
          size="small"
          className="ur-channel-progress"
          stroke={rate >= 99 ? '#13a671' : rate >= 90 ? '#bd7a13' : '#d94c48'}
        />
        <div className="ur-channel-meta">
          <div className="ur-channel-stat">
            <Text className="ur-channel-stat-label">成功率</Text>
            <Text className="ur-channel-stat-value">{rate.toFixed(1)}%</Text>
          </div>
          <div className="ur-channel-stat">
            <Text className="ur-channel-stat-label">请求</Text>
            <Text className="ur-channel-stat-value">{formatCompactNumber(total)}</Text>
          </div>
          <div className="ur-channel-stat">
            <Text className="ur-channel-stat-label">失败</Text>
            <Text className="ur-channel-stat-value">{formatCompactNumber(ch.fail_count)}</Text>
          </div>
        </div>
      </div>
      <div className={`ur-channel-state ur-channel-state-${health.tone}`}>
        {health.icon}
        <span>{health.label}</span>
      </div>
    </div>
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
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [range, setRange] = useState<TimeRange>('24h');
  const [lastUpdated, setLastUpdated] = useState<Date>(new Date());
  const [visibleTrendMetrics, setVisibleTrendMetrics] = useState<TrendMetric[]>(['requests', 'cost_yuan']);
  const platform = isPlatformAdminRole(user?.role);
  const [selectedOrgID, setSelectedOrgID] = useState<number | undefined>(platform ? user?.org_id : undefined);
  const [organizations, setOrganizations] = useState<Array<{ id: number; name: string }>>([]);
  const [organizationsError, setOrganizationsError] = useState('');
  const requestVersion = useRef(0);

  const loadOrganizations = useCallback(async () => {
    if (!platform) return;
    setOrganizationsError('');
    try {
      const items = await loadAllPages<{ id: number; name: string }>((page, size) => api.getOrganizations(page, size));
      setOrganizations(items);
    } catch {
      setOrganizationsError('组织列表加载失败；当前总览仍使用登录账号所属组织。');
    }
  }, [platform]);

  useEffect(() => { void loadOrganizations(); }, [loadOrganizations]);

  const fetchData = useCallback(async () => {
    const version = ++requestVersion.current;
    setLoading(true);
    setLoadError('');
    try {
      const params = getRangeParams(range);
      const data = await api.getDashboardStats(params.startTime, params.endTime, platform ? selectedOrgID : undefined);
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
  }, [platform, range, selectedOrgID]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const comparableDelta = range === '24h' ? stats?.delta_vs_yesterday : undefined;
  const trendData = (stats?.daily_trend || []).map(d => ({
    date: formatTrendLabel(d.date),
    requests: d.requests,
		cost_yuan: microsToChartYuan(d.cost_micros),
  }));
  const modelData = [...(stats?.model_distribution || [])]
    .sort((a, b) => b.requests - a.requests)
    .slice(0, 6);
	const walletThreshold = stats?.wallet_threshold_micros || '0';
	const walletAvailable = parseMicros(stats?.wallet_balance_micros || '0') + parseMicros(stats?.wallet_granted_micros || '0');
  const hasWalletThreshold = parseMicros(walletThreshold) > 0n;
  const lowBalance = hasWalletThreshold && walletAvailable <= parseMicros(walletThreshold);
  const selectedOrganization = organizations.find(org => org.id === selectedOrgID);
  const scopeLabel = platform
    ? (selectedOrganization?.name || (selectedOrgID === user?.org_id ? user?.org_name : '') || (selectedOrgID ? `组织 #${selectedOrgID}` : '当前权限范围'))
    : (user?.org_name || (user?.org_id ? `组织 #${user.org_id}` : '当前权限范围'));
  const avgLatency = stats?.avg_latency_ms || 0;
  const p95Latency = stats?.p95_latency_ms ?? avgLatency;
  const p99Latency = stats?.p99_latency_ms ?? p95Latency;
  const healthyChannels = (stats?.channel_health || []).filter(ch => channelHealthView(ch).tone === 'healthy').length;
  const channelTotal = (stats?.channel_health || []).length;
  const canReadWallet = platform || isBillingRole(user?.role);
  const canReadChannelHealth = platform;
  const canOpenRecentRequests = isRouteAllowedForRole('/logs', user?.role);
  const dashboardTitle = isPlatformAdminRole(user?.role)
    ? '平台总览'
    : isBillingRole(user?.role)
      ? '财务与用量总览'
      : isOrgViewerRole(user?.role)
        ? '组织只读总览'
        : '组织运营总览';
  const dashboardDescription = isPlatformAdminRole(user?.role)
    ? '统一观察请求量、成功率、延迟、成本和通道健康状态。'
    : isBillingRole(user?.role)
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

  const dashboardHeader = (
    <PageHeader
      eyebrow="概览"
      title={dashboardTitle}
      description={`数据范围：${scopeLabel}。${dashboardDescription}`}
      actions={(
        <>
          {platform && (
            <>
              <Text id="dashboard-target-organization-label" type="tertiary">目标组织</Text>
              <Select
                aria-labelledby="dashboard-target-organization-label"
                value={selectedOrgID}
                optionList={organizations.map(org => ({ label: `${org.name}（ID ${org.id}）`, value: org.id }))}
                onChange={(value) => {
                  const orgID = Number(value);
                  if (!Number.isInteger(orgID) || orgID <= 0 || orgID === selectedOrgID) return;
                  setStats(null);
                  setLoadError('');
                  setSelectedOrgID(orgID);
                }}
                disabled={loading}
                filter
                style={{ width: 220 }}
              />
              {organizationsError && (
                <Tooltip content={organizationsError}>
                  <Button size="small" onClick={() => void loadOrganizations()}>重试组织列表</Button>
                </Tooltip>
              )}
            </>
          )}
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
            <Button icon={<IconRefresh />} size="small" onClick={fetchData} className="ur-icon-button" aria-label="刷新总览" />
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
            actions={<Button type="primary" icon={<IconRefresh />} onClick={fetchData} aria-label="重新加载总览">重新加载</Button>}
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
                  <Text>健康通道</Text>
                  <strong>{healthyChannels}/{channelTotal || 0}</strong>
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
            label="成功率"
            value={`${((stats?.success_rate || 0) * 100).toFixed(2)}%`}
            tone={(stats?.success_rate || 0) >= 0.99 ? 'green' : 'red'}
            icon={<IconPercentage />}
            meta={comparableDelta && <><Text type="tertiary" size="small">较前 24h</Text><DeltaTag value={comparableDelta.success_rate * 100} suffix=" 个百分点" /></>}
          />
          <MetricTile
            label="P95 延迟"
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

          <Surface title="延迟分位" description="当前窗口的平均与尾部延迟。" className="ur-latency-surface">
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
          <Surface title="模型活动分布" description="按请求数展示 Top 模型，优先帮助定位成本和流量集中点。">
            {modelData.length > 0 ? (
              <ResponsiveContainer width="100%" height={260}>
                <BarChart data={modelData} layout="vertical" margin={{ top: 0, right: 20, left: 0, bottom: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke="var(--ur-border-soft)" horizontal={false} />
                  <XAxis type="number" tick={{ fontSize: 12 }} stroke="var(--ur-text-muted)" />
                  <YAxis type="category" dataKey="model" width={132} tick={{ fontSize: 12 }} stroke="var(--ur-text-muted)" />
                  <RTooltip formatter={(v: any) => {
                    const value = Number(v);
                    const total = (stats?.model_distribution || []).reduce((s, m) => s + m.requests, 0);
                    const pct = total > 0 ? (value / total * 100).toFixed(1) : '0';
                    return [`${formatCompactNumber(value)} (${pct}%)`, '请求数'];
                  }} />
                  <Bar dataKey="requests" radius={[0, 4, 4, 0]} maxBarSize={24} isAnimationActive={false}>
                    {modelData.map((_, i) => (
                      <Cell key={i} fill={MODEL_COLORS[i % MODEL_COLORS.length]} />
                    ))}
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            ) : (
              <EmptyState title="暂无模型数据" />
            )}
          </Surface>

          {canReadChannelHealth && (
            <Surface title="通道健康状态" description="聚合当前窗口内各通道成功、失败和可用状态。">
              {(stats?.channel_health || []).length > 0 ? (
                <div className="ur-channel-list">
                  {(stats?.channel_health || []).map(ch => (
                    <ChannelHealthCard key={ch.id} ch={ch} />
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
                onClick: () => navigate(`/logs?request_id=${record.request_id}`),
                style: { cursor: 'pointer' },
              }) : undefined}
            />
          </div>
        </Surface>
      </div>
    </Spin>
  );
}
