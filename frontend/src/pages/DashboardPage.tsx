import { formatDateTime } from '../utils/formatDate';
import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { Typography, Table, Spin, Tag, Tooltip, Button, Progress, Checkbox } from '@douyinfe/semi-ui';
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
import { EmptyState, LatencyText, MetricTile, MoneyText, PageHeader, StatusTag, Surface, formatMoney } from '../ui';

const { Text } = Typography;

interface DashboardStats {
  total_requests: number;
  today_cost_cents: number;
  avg_latency_ms: number;
  p95_latency_ms?: number;
  p99_latency_ms?: number;
  success_rate: number;
  recent_logs: any[];
  model_prices: { model_name: string; input_price_cents: number; output_price_cents: number }[];
  wallet_balance?: number;
  wallet_granted?: number;
  wallet_frozen?: number;
  wallet_threshold?: number;
  daily_trend?: { date: string; requests: number; cost_cents: number }[];
  model_distribution?: { model: string; requests: number }[];
  channel_health?: { id: number; name: string; provider: string; enabled: boolean; status: string; success_count: number; fail_count: number }[];
  delta_vs_yesterday?: { cost_cents: number; latency_ms: number; success_rate: number };
}

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

function formatNumber(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toFixed(0);
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
  const displayValue = Math.abs(value) < 100 ? value.toFixed(2) : formatNumber(value);
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
  if (provider === 'openai' && !channelName.toLowerCase().includes('openai')) return 'OpenAI-compatible';
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
        {providerLabel && <Text className="ur-channel-provider" title={ch.provider}>Provider: {providerLabel}</Text>}
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
            <Text className="ur-channel-stat-value">{formatNumber(total)}</Text>
          </div>
          <div className="ur-channel-stat">
            <Text className="ur-channel-stat-label">失败</Text>
            <Text className="ur-channel-stat-value">{formatNumber(ch.fail_count)}</Text>
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
          {TREND_METRIC_META[p.dataKey as TrendMetric]?.label || p.name}: <strong>{p.dataKey === 'cost_yuan' ? formatYuan(Number(p.value)) : formatNumber(Number(p.value))}</strong>
        </div>
      ))}
    </div>
  );
}

export default function DashboardPage() {
  const navigate = useNavigate();
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [range, setRange] = useState<TimeRange>('24h');
  const [lastUpdated, setLastUpdated] = useState<Date>(new Date());
  const [visibleTrendMetrics, setVisibleTrendMetrics] = useState<TrendMetric[]>(['requests', 'cost_yuan']);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const params = getRangeParams(range);
      const data = await api.getDashboardStats(params.startTime, params.endTime);
      setStats(data);
      setLastUpdated(new Date());
    } catch (e) {
      console.error('Failed to load dashboard stats', e);
    } finally {
      setLoading(false);
    }
  }, [range]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const delta = stats?.delta_vs_yesterday;
  const trendData = (stats?.daily_trend || []).map(d => ({
    date: formatTrendLabel(d.date),
    requests: d.requests,
    cost_yuan: d.cost_cents / 100,
  }));
  const modelData = [...(stats?.model_distribution || [])]
    .sort((a, b) => b.requests - a.requests)
    .slice(0, 6);
  const budgetThreshold = stats?.wallet_threshold || 0;
  const budgetPercent = budgetThreshold > 0 ? Math.min(100, (stats?.today_cost_cents || 0) / budgetThreshold * 100) : 0;
  const avgLatency = stats?.avg_latency_ms || 0;
  const p95Latency = stats?.p95_latency_ms ?? avgLatency;
  const p99Latency = stats?.p99_latency_ms ?? p95Latency;
  const healthyChannels = (stats?.channel_health || []).filter(ch => channelHealthView(ch).tone === 'healthy').length;
  const channelTotal = (stats?.channel_health || []).length;

  const recentCols = [
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
      dataIndex: 'cost_cents',
      key: 'cost_cents',
      render: (v: number) => <MoneyText cents={v} precision={4} tone="cost" />,
    },
    {
      title: '时间',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (v: string) => formatDateTime(v),
    },
  ];

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

  return (
    <Spin spinning={loading} size="large">
      <div className="ur-page">
        <section className="ur-dashboard-hero">
          <PageHeader
            eyebrow="Gateway overview"
            title="AI Gateway 控制台"
            description="全局观察请求量、成功率、延迟、成本和渠道健康状态。"
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
                  <Button icon={<IconRefresh />} size="small" onClick={fetchData} className="ur-icon-button" />
                </Tooltip>
              </>
            )}
          />
          <div className="ur-dashboard-status-strip">
            <div className="ur-dashboard-status-card">
              <IconPulse />
              <div>
                <Text>更新时间</Text>
                <strong>{lastUpdated.toLocaleTimeString()}</strong>
              </div>
            </div>
            <div className="ur-dashboard-status-card">
              <IconServer />
              <div>
                <Text>健康渠道</Text>
                <strong>{healthyChannels}/{channelTotal || 0}</strong>
              </div>
            </div>
            <div className="ur-dashboard-status-card">
              <IconCoinMoney />
              <div>
                <Text>钱包余额</Text>
                <strong>{formatMoney((stats?.wallet_balance || 0) + (stats?.wallet_granted || 0))}</strong>
              </div>
            </div>
          </div>
        </section>

        <div className="ur-kpi-grid">
          <MetricTile
            label="总请求数"
            value={formatNumber(stats?.total_requests || 0)}
            tone="blue"
            icon={<IconHistogram />}
            meta={<Text type="tertiary" size="small">当前窗口累计</Text>}
          />
          <MetricTile
            label="成功率"
            value={`${((stats?.success_rate || 0) * 100).toFixed(2)}%`}
            tone={(stats?.success_rate || 0) >= 0.99 ? 'green' : 'red'}
            icon={<IconPercentage />}
            meta={delta && <><Text type="tertiary" size="small">vs 昨日</Text><DeltaTag value={delta.success_rate * 100} suffix="%" /></>}
          />
          <MetricTile
            label="P95 延迟"
            value={`${p95Latency.toFixed(0)}ms`}
            tone={latencyTone(p95Latency)}
            icon={<IconClock />}
            meta={(
              <div className="ur-kpi-meta-stack">
                <Text type="tertiary" size="small">Avg {avgLatency.toFixed(0)}ms · P99 {p99Latency.toFixed(0)}ms</Text>
                {delta && <div><Text type="tertiary" size="small">vs 昨日</Text><DeltaTag value={delta.latency_ms} suffix="ms" inverse /></div>}
              </div>
            )}
          />
          <MetricTile
            label="今日花费"
            value={<MoneyText cents={stats?.today_cost_cents || 0} strong tone="cost" />}
            tone="violet"
            icon={<IconCoinMoney />}
            meta={budgetThreshold > 0 ? (
              <div>
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10, marginBottom: 6 }}>
                  <Text type="tertiary" size="small">预算进度</Text>
                  <Text type="tertiary" size="small">{formatMoney(stats?.today_cost_cents || 0)} / {formatMoney(budgetThreshold)}</Text>
                </div>
                <Progress
                  percent={budgetPercent}
                  showInfo={false}
                  stroke={budgetPercent > 80 ? '#d92d20' : '#1f6feb'}
                  size="small"
                />
                {delta && <div style={{ marginTop: 7 }}><Text type="tertiary" size="small">vs 昨日</Text><DeltaTag value={delta.cost_cents / 100} suffix="¥" inverse /></div>}
              </div>
            ) : delta && <><Text type="tertiary" size="small">vs 昨日</Text><DeltaTag value={delta.cost_cents / 100} suffix="¥" inverse /></>}
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
                    tickFormatter={formatNumber}
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

        <div className="ur-split-grid">
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
                    return [`${formatNumber(value)} (${pct}%)`, '请求数'];
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

          <Surface title="渠道健康状态" description="聚合当前窗口内各渠道成功、失败和可用状态。">
            {(stats?.channel_health || []).length > 0 ? (
              <div className="ur-channel-list">
                {(stats?.channel_health || []).map(ch => (
                  <ChannelHealthCard key={ch.id} ch={ch} />
                ))}
              </div>
            ) : (
              <EmptyState title="暂无渠道数据" />
            )}
          </Surface>
        </div>

        <Surface title="最近请求" description="点击任意请求可跳转到日志探索器并带入 Request ID。">
          <div className="ur-table-shell">
            <Table
              dataSource={stats?.recent_logs || []}
              columns={recentCols}
              rowKey="request_id"
              size="small"
              pagination={false}
              empty="暂无请求记录"
              onRow={(record) => ({
                onClick: () => navigate(`/logs?request_id=${record.request_id}`),
                style: { cursor: 'pointer' },
              })}
            />
          </div>
        </Surface>
      </div>
    </Spin>
  );
}
