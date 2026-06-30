import { formatDateTime } from '../utils/formatDate';
import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { Typography, Row, Col, Card, Table, Spin, Tag, Tooltip, Button, Badge, Progress } from '@douyinfe/semi-ui';
import { IconHistogram, IconPercentage, IconClock, IconTick, IconRefresh } from '@douyinfe/semi-icons';
import {
  AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip as RTooltip,
  ResponsiveContainer, BarChart, Bar, Cell,
} from 'recharts';
import { api } from '../api/client';

const { Title, Text } = Typography;

/* ── Types ── */

interface DashboardStats {
  total_requests: number;
  today_cost_cents: number;
  avg_latency_ms: number;
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
type TrendMetric = 'requests' | 'cost_cents';

/* ── Helpers ── */

function getRangeParams(range: TimeRange, customStart?: string, customEnd?: string): { startTime?: string; endTime?: string } {
  const now = new Date();
  const fmt = (d: Date) => d.toISOString();
  switch (range) {
    case '24h':
      return { startTime: fmt(new Date(now.getTime() - 24 * 86400000)), endTime: fmt(now) };
    case '7d':
      return { startTime: fmt(new Date(now.getTime() - 7 * 86400000)), endTime: fmt(now) };
    case '30d':
      return { startTime: fmt(new Date(now.getTime() - 30 * 86400000)), endTime: fmt(now) };
    case 'custom':
      return { startTime: customStart, endTime: customEnd };
  }
}

function formatCost(cents: number): string {
  if (cents >= 10000) return `¥${(cents / 100).toFixed(0)}`;
  return `¥${(cents / 100).toFixed(2)}`;
}

function formatNumber(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toFixed(0);
}

function DeltaTag({ value, suffix = '', inverse = false }: { value: number; suffix?: string; inverse?: boolean }) {
  const isPositive = value > 0;
  const color = inverse
    ? (isPositive ? 'red' : 'green')
    : (isPositive ? 'green' : 'red');
  if (Math.abs(value) < 0.001) return <Text type="tertiary" size="small">—</Text>;
  const sign = isPositive ? '+' : '';
  return (
    <Tag size="small" color={color} style={{ marginLeft: 8, fontWeight: 600 }}>
      {sign}{typeof value === 'number' && Math.abs(value) < 100 ? value.toFixed(2) : formatNumber(value)}{suffix}
    </Tag>
  );
}

/* ── Bar colors per model ── */
const MODEL_COLORS = ['#0077FA', '#05944F', '#F5A623', '#7B61FF', '#DD2C00', '#00B2D9', '#E8598D', '#8B5CF6', '#10B981', '#F59E0B'];

/* ── Channel Health Card ── */

function ChannelHealthCard({ ch }: { ch: NonNullable<DashboardStats['channel_health']>[number] }) {
  const total = ch.success_count + ch.fail_count;
  const rate = total > 0 ? (ch.success_count / total * 100) : 0;
  const statusColor = ch.status === 'healthy' ? '#05944F' : ch.status === 'degraded' ? '#F5A623' : '#DD2C00';
  return (
    <Card
      style={{ borderRadius: 10, border: '1px solid var(--semi-color-border)' }}
      bodyStyle={{ padding: 16 }}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
        <Text strong style={{ fontSize: 14 }}>{ch.name}</Text>
        <Badge dot style={{ backgroundColor: statusColor }} />
      </div>
      <Text type="tertiary" size="small" style={{ display: 'block', marginBottom: 8 }}>{ch.provider}</Text>
      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
        <div>
          <Text type="tertiary" size="small">成功率</Text>
          <div style={{ fontSize: 16, fontWeight: 600, color: statusColor }}>{rate.toFixed(1)}%</div>
        </div>
        <div>
          <Text type="tertiary" size="small">请求</Text>
          <div style={{ fontSize: 16, fontWeight: 600 }}>{formatNumber(total)}</div>
        </div>
        <div>
          <Text type="tertiary" size="small">状态</Text>
          <Tag size="small" color={ch.enabled ? (ch.status === 'healthy' ? 'green' : ch.status === 'degraded' ? 'orange' : 'red') : 'grey'}>
            {ch.enabled ? ch.status : '禁用'}
          </Tag>
        </div>
      </div>
    </Card>
  );
}

/* ── Custom Tooltip ── */

function TrendTooltip({ active, payload, label }: any) {
  if (!active || !payload?.length) return null;
  return (
    <div style={{ background: '#fff', border: '1px solid #e5e5e5', borderRadius: 8, padding: '10px 14px', boxShadow: '0 2px 8px rgba(0,0,0,0.08)' }}>
      <Text size="small" style={{ fontWeight: 600 }}>{label}</Text>
      {payload.map((p: any, i: number) => (
        <div key={i} style={{ marginTop: 4, fontSize: 13 }}>
          <span style={{ color: p.color, marginRight: 6 }}>●</span>
          {p.name === 'requests' ? '请求数' : '费用'}: <strong>{p.name === 'cost_cents' ? formatCost(p.value) : formatNumber(p.value)}</strong>
        </div>
      ))}
    </div>
  );
}

/* ── Page Component ── */

export default function DashboardPage() {
  const navigate = useNavigate();
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [range, setRange] = useState<TimeRange>('24h');
  const [lastUpdated, setLastUpdated] = useState<Date>(new Date());
  const [trendMetric, setTrendMetric] = useState<TrendMetric>('requests');

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

  // Trend chart data
  const trendData = (stats?.daily_trend || []).map(d => ({
    date: d.date.slice(5), // MM-DD
    requests: d.requests,
    cost_cents: d.cost_cents / 100, // convert to ¥
  }));

  // Model distribution data sorted desc
  const modelData = (stats?.model_distribution || [])
    .sort((a, b) => b.requests - a.requests)
    .slice(0, 10);

  // Recent logs columns
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
      render: (v: number) => {
        const color = v >= 200 && v < 300 ? 'green' : v === 429 ? 'orange' : v >= 500 ? 'red' : 'orange';
        return <Tag size="small" color={color}>{v}</Tag>;
      },
    },
    {
      title: '延迟',
      dataIndex: 'latency_ms',
      key: 'latency_ms',
      render: (v: number) => (
        <Text style={{ color: v > 2000 ? '#DD2C00' : v > 1000 ? '#F5A623' : 'inherit' }}>
          {v}ms
        </Text>
      ),
    },
    {
      title: '费用',
      dataIndex: 'cost_cents',
      key: 'cost_cents',
      render: (v: number) => formatCost(v),
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

  return (
    <Spin spinning={loading} size="large">
      <div style={{ padding: '0 4px' }}>
        {/* ── Header ── */}
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 24 }}>
          <Title heading={3} style={{ margin: 0 }}>Dashboard</Title>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
            <div style={{ display: 'flex', gap: 4, background: 'var(--semi-color-fill-0)', borderRadius: 8, padding: 3 }}>
              {timeRanges.map(t => (
                <Button
                  key={t.value}
                  size="small"
                  theme={range === t.value ? 'solid' : 'borderless'}
                  type={range === t.value ? 'primary' : 'tertiary'}
                  onClick={() => setRange(t.value)}
                  style={{ borderRadius: 6 }}
                >
                  {t.label}
                </Button>
              ))}
            </div>
            <Tooltip content="刷新">
              <Button icon={<IconRefresh />} size="small" onClick={fetchData} />
            </Tooltip>
            <Text type="tertiary" size="small">
              更新于 {lastUpdated.toLocaleTimeString()}
            </Text>
          </div>
        </div>

        {/* ── KPI Row ── */}
        <Row gutter={16} style={{ marginBottom: 24 }}>
          {/* Card 1: Total Requests */}
          <Col span={6}>
            <Card style={{ borderRadius: 12, border: '1px solid var(--semi-color-border)' }} bodyStyle={{ padding: 20 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12 }}>
                <div style={{ width: 40, height: 40, borderRadius: 10, background: '#0077FA14', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                  <IconHistogram style={{ color: '#0077FA', fontSize: 20 }} />
                </div>
                <Text type="tertiary">总请求数</Text>
              </div>
              <div style={{ fontSize: 28, fontWeight: 700, color: '#0077FA' }}>
                {formatNumber(stats?.total_requests || 0)}
              </div>
              {delta && (
                <div style={{ marginTop: 6 }}>
                  <Text type="tertiary" size="small">当前累计</Text>
                </div>
              )}
            </Card>
          </Col>

          {/* Card 2: Success Rate */}
          <Col span={6}>
            <Card style={{ borderRadius: 12, border: '1px solid var(--semi-color-border)' }} bodyStyle={{ padding: 20 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12 }}>
                <div style={{ width: 40, height: 40, borderRadius: 10, background: '#05944F14', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                  <IconPercentage style={{ color: '#05944F', fontSize: 20 }} />
                </div>
                <Text type="tertiary">成功率</Text>
              </div>
              <div style={{ fontSize: 28, fontWeight: 700, color: (stats?.success_rate || 0) >= 0.99 ? '#05944F' : '#DD2C00' }}>
                {((stats?.success_rate || 0) * 100).toFixed(2)}%
              </div>
              {delta && (
                <div style={{ marginTop: 6 }}>
                  <Text type="tertiary" size="small">vs 昨日</Text>
                  <DeltaTag value={delta.success_rate * 100} suffix="%" />
                </div>
              )}
            </Card>
          </Col>

          {/* Card 3: Avg Latency */}
          <Col span={6}>
            <Card style={{ borderRadius: 12, border: '1px solid var(--semi-color-border)' }} bodyStyle={{ padding: 20 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12 }}>
                <div style={{ width: 40, height: 40, borderRadius: 10, background: '#F5A62314', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                  <IconClock style={{ color: '#F5A623', fontSize: 20 }} />
                </div>
                <Text type="tertiary">平均延迟</Text>
              </div>
              <div style={{ fontSize: 28, fontWeight: 700, color: (stats?.avg_latency_ms || 0) > 2000 ? '#DD2C00' : (stats?.avg_latency_ms || 0) > 500 ? '#F5A623' : '#05944F' }}>
                {(stats?.avg_latency_ms || 0).toFixed(0)}ms
              </div>
              {delta && (
                <div style={{ marginTop: 6 }}>
                  <Text type="tertiary" size="small">vs 昨日</Text>
                  <DeltaTag value={delta.latency_ms} suffix="ms" inverse />
                </div>
              )}
            </Card>
          </Col>

          {/* Card 4: Today's Cost */}
          <Col span={6}>
            <Card style={{ borderRadius: 12, border: '1px solid var(--semi-color-border)' }} bodyStyle={{ padding: 20 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12 }}>
                <div style={{ width: 40, height: 40, borderRadius: 10, background: '#7B61FF14', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                  <IconTick style={{ color: '#7B61FF', fontSize: 20 }} />
                </div>
                <Text type="tertiary">今日花费</Text>
              </div>
              <div style={{ fontSize: 28, fontWeight: 700, color: '#7B61FF' }}>
                {formatCost(stats?.today_cost_cents || 0)}
              </div>
              {delta && (
                <div style={{ marginTop: 6 }}>
                  <Text type="tertiary" size="small">vs 昨日</Text>
                  <DeltaTag value={delta.cost_cents / 100} suffix="¥" inverse />
                </div>
              )}
              {stats?.wallet_threshold && stats.wallet_threshold > 0 && (
                <div style={{ marginTop: 12 }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4 }}>
                    <Text type="tertiary" size="small">预算进度</Text>
                    <Text type="tertiary" size="small">{formatCost(stats.today_cost_cents)} / {formatCost(stats.wallet_threshold)}</Text>
                  </div>
                  <Progress
                    percent={Math.min(100, (stats.today_cost_cents / stats.wallet_threshold) * 100)}
                    showInfo={false}
                    stroke={stats.today_cost_cents / stats.wallet_threshold > 0.8 ? '#DD2C00' : '#0077FA'}
                    size="small"
                  />
                </div>
              )}
            </Card>
          </Col>
        </Row>

        {/* ── AI Activity Trend (Area Chart) ── */}
        <Card style={{ borderRadius: 12, marginBottom: 24, border: '1px solid var(--semi-color-border)' }} bodyStyle={{ padding: 20 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
            <Title heading={5} style={{ margin: 0 }}>AI 活动趋势</Title>
            <div style={{ display: 'flex', gap: 4, background: 'var(--semi-color-fill-0)', borderRadius: 6, padding: 2 }}>
              <Button
                size="small"
                theme={trendMetric === 'requests' ? 'solid' : 'borderless'}
                type={trendMetric === 'requests' ? 'primary' : 'tertiary'}
                onClick={() => setTrendMetric('requests')}
                style={{ borderRadius: 5 }}
              >
                请求数
              </Button>
              <Button
                size="small"
                theme={trendMetric === 'cost_cents' ? 'solid' : 'borderless'}
                type={trendMetric === 'cost_cents' ? 'primary' : 'tertiary'}
                onClick={() => setTrendMetric('cost_cents')}
                style={{ borderRadius: 5 }}
              >
                费用
              </Button>
            </div>
          </div>
          {trendData.length > 0 ? (
            <ResponsiveContainer width="100%" height={280}>
              <AreaChart data={trendData} margin={{ top: 10, right: 20, left: 0, bottom: 0 }}>
                <defs>
                  <linearGradient id="colorRequests" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#0077FA" stopOpacity={0.2} />
                    <stop offset="95%" stopColor="#0077FA" stopOpacity={0} />
                  </linearGradient>
                  <linearGradient id="colorCost" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#7B61FF" stopOpacity={0.2} />
                    <stop offset="95%" stopColor="#7B61FF" stopOpacity={0} />
                  </linearGradient>
                  <linearGradient id="colorLatency" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#F5A623" stopOpacity={0.2} />
                    <stop offset="95%" stopColor="#F5A623" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="var(--semi-color-border)" />
                <XAxis dataKey="date" tick={{ fontSize: 12 }} stroke="var(--semi-color-text-2)" />
                <YAxis tick={{ fontSize: 12 }} stroke="var(--semi-color-text-2)" />
                <RTooltip content={<TrendTooltip />} />
                <Area
                  type="monotone"
                  dataKey={trendMetric}
                  stroke={trendMetric === 'requests' ? '#0077FA' : '#7B61FF'}
                  strokeWidth={2}
                  fill={`url(#color${trendMetric === 'requests' ? 'Requests' : 'Cost'})`}
                  dot={false}
                  activeDot={{ r: 5, strokeWidth: 2 }}
                />
              </AreaChart>
            </ResponsiveContainer>
          ) : (
            <div style={{ height: 280, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
              <Text type="tertiary">暂无趋势数据</Text>
            </div>
          )}
        </Card>

        {/* ── Model Distribution (Horizontal Bar) + Channel Health (Cards Grid) ── */}
        <Row gutter={16} style={{ marginBottom: 24 }}>
          <Col span={16}>
            <Card style={{ borderRadius: 12, height: '100%', border: '1px solid var(--semi-color-border)' }} bodyStyle={{ padding: 20 }}>
              <Title heading={5} style={{ margin: '0 0 16px 0' }}>模型活动分布</Title>
              {modelData.length > 0 ? (
                <ResponsiveContainer width="100%" height={Math.max(220, modelData.length * 36 + 20)}>
                  <BarChart data={modelData} layout="vertical" margin={{ top: 0, right: 20, left: 0, bottom: 0 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="var(--semi-color-border)" horizontal={false} />
                    <XAxis type="number" tick={{ fontSize: 12 }} stroke="var(--semi-color-text-2)" />
                    <YAxis type="category" dataKey="model" width={140} tick={{ fontSize: 12 }} stroke="var(--semi-color-text-2)" />
                    <RTooltip formatter={(v: number) => {
                    const total = (stats?.model_distribution || []).reduce((s, m) => s + m.requests, 0);
                    const pct = total > 0 ? (v / total * 100).toFixed(1) : '0';
                    return [`${formatNumber(v)} (${pct}%)`, '请求数'];
                  }} />
                    <Bar dataKey="requests" radius={[0, 4, 4, 0]} maxBarSize={24}>
                      {modelData.map((_, i) => (
                        <Cell key={i} fill={MODEL_COLORS[i % MODEL_COLORS.length]} />
                      ))}
                    </Bar>
                  </BarChart>
                </ResponsiveContainer>
              ) : (
                <div style={{ height: 200, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                  <Text type="tertiary">暂无模型数据</Text>
                </div>
              )}
            </Card>
          </Col>

          <Col span={8}>
            <Card style={{ borderRadius: 12, height: '100%', border: '1px solid var(--semi-color-border)' }} bodyStyle={{ padding: 20 }}>
              <Title heading={5} style={{ margin: '0 0 16px 0' }}>渠道健康状态</Title>
              {(stats?.channel_health || []).length > 0 ? (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 10, maxHeight: 360, overflowY: 'auto' }}>
                  {(stats?.channel_health || []).map(ch => (
                    <ChannelHealthCard key={ch.id} ch={ch} />
                  ))}
                </div>
              ) : (
                <div style={{ height: 200, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                  <Text type="tertiary">暂无渠道数据</Text>
                </div>
              )}
            </Card>
          </Col>
        </Row>

        {/* ── Recent Requests ── */}
        <Card style={{ borderRadius: 12, border: '1px solid var(--semi-color-border)' }} bodyStyle={{ padding: 20 }}>
          <Title heading={5} style={{ margin: '0 0 16px 0' }}>最近请求</Title>
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
        </Card>
      </div>
    </Spin>
  );
}
