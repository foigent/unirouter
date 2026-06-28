import { formatDateTime } from '../utils/formatDate';
import { useState, useEffect } from 'react';
import { Typography, Row, Col, Card, Table, Spin, DatePicker, Tag, Tooltip } from '@douyinfe/semi-ui';
import { IconHistogram, IconPercentage, IconClock, IconTick } from '@douyinfe/semi-icons';
import { api } from '../api/client';

const { Title, Text } = Typography;

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

type TimeRange = 'today' | '7d' | '30d' | 'custom';

function getRangeParams(range: TimeRange, customStart?: string, customEnd?: string): { startTime?: string; endTime?: string } {
  const now = new Date();
  const fmt = (d: Date) => d.toISOString();
  switch (range) {
    case 'today': {
      const start = new Date(now.getFullYear(), now.getMonth(), now.getDate());
      return { startTime: fmt(start), endTime: fmt(now) };
    }
    case '7d':
      return { startTime: fmt(new Date(now.getTime() - 7 * 86400000)), endTime: fmt(now) };
    case '30d':
      return { startTime: fmt(new Date(now.getTime() - 30 * 86400000)), endTime: fmt(now) };
    case 'custom':
      return { startTime: customStart, endTime: customEnd };
  }
}

function DeltaBadge({ value, suffix, invert }: { value: number; suffix: string; invert?: boolean }) {
  const improved = invert ? value < 0 : value > 0;
  const degraded = invert ? value > 0 : value < 0;
  if (Math.abs(value) < 0.001) return null;
  const color = improved ? '#05944F' : degraded ? '#F53F3F' : '#666';
  const arrow = value > 0 ? '↑' : '↓';
  const display = suffix === '%' ? `${(Math.abs(value) * 100).toFixed(1)}%` : `${Math.abs(Math.round(value))}${suffix}`;
  return (
    <Tooltip content={`较昨日 ${value > 0 ? '+' : ''}${display}`}>
      <span style={{ fontSize: 12, color, marginLeft: 8, fontWeight: 500 }}>
        {arrow}{display}
      </span>
    </Tooltip>
  );
}

const PIE_COLORS = ['#0077FA', '#05944F', '#F5A623', '#7B61FF', '#F53F3F', '#36CFC9', '#FF85C0'];

function ModelDistributionPie({ data }: { data: { model: string; requests: number }[] }) {
  if (!data || data.length === 0) return <Text type="secondary">暂无数据</Text>;
  const total = data.reduce((s, d) => s + d.requests, 0);
  if (total === 0) return <Text type="secondary">暂无数据</Text>;

  let cumPercent = 0;
  const gradientStops: string[] = [];
  data.forEach((d, i) => {
    const pct = (d.requests / total) * 100;
    gradientStops.push(`${PIE_COLORS[i % PIE_COLORS.length]} ${cumPercent}% ${cumPercent + pct}%`);
    cumPercent += pct;
  });

  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 24 }}>
      <div
        style={{
          width: 120, height: 120, borderRadius: '50%', flexShrink: 0,
          background: `conic-gradient(${gradientStops.join(', ')})`,
        }}
      />
      <div style={{ flex: 1 }}>
        {data.map((d, i) => (
          <div key={d.model} style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
            <div style={{ width: 10, height: 10, borderRadius: 2, background: PIE_COLORS[i % PIE_COLORS.length], flexShrink: 0 }} />
            <Text size="small" style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{d.model}</Text>
            <Text size="small" type="secondary">{((d.requests / total) * 100).toFixed(1)}%</Text>
          </div>
        ))}
      </div>
    </div>
  );
}

function ChannelHealthPanel({ data }: { data: { id: number; name: string; provider: string; status: string; success_count: number; fail_count: number }[] }) {
  if (!data || data.length === 0) return <Text type="secondary">暂无渠道</Text>;
  const statusMap: Record<string, { color: string; label: string }> = {
    healthy: { color: 'green', label: '正常' },
    warning: { color: 'orange', label: '告警' },
    error: { color: 'red', label: '故障' },
    disabled: { color: 'grey', label: '已禁用' },
  };
  const columns = [
    { title: '渠道', dataIndex: 'name', key: 'name', width: 120 },
    { title: '供应商', dataIndex: 'provider', key: 'provider', width: 100 },
    {
      title: '状态', dataIndex: 'status', key: 'status', width: 80,
      render: (v: string) => {
        const s = statusMap[v] || { color: 'grey', label: v };
        return <Tag color={s.color as any}>{s.label}</Tag>;
      },
    },
    {
      title: '成功率', key: 'rate', width: 80,
      render: (_: any, r: any) => {
        const total = r.success_count + r.fail_count;
        if (total === 0) return '—';
        return `${((r.success_count / total) * 100).toFixed(1)}%`;
      },
    },
  ];
  return <Table dataSource={data} columns={columns} rowKey="id" size="small" pagination={false} />;
}

export default function DashboardPage() {
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [timeRange, setTimeRange] = useState<TimeRange>('today');
  const [customRange, setCustomRange] = useState<[Date, Date] | null>(null);

  const fetchStats = (range: TimeRange, start?: string, end?: string) => {
    setLoading(true);
    const params = getRangeParams(range, start, end);
    api.getDashboardStats(params.startTime, params.endTime)
      .then((data) => setStats(data))
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    fetchStats(timeRange, customRange?.[0]?.toISOString(), customRange?.[1]?.toISOString());
  }, [timeRange, customRange]);

  const statCards = [
    {
      label: '总请求数',
      value: stats?.total_requests?.toLocaleString() ?? '—',
      icon: <IconHistogram />,
      color: '#0077FA',
    },
    {
      label: '今日花费',
      value: stats ? `¥${(stats.today_cost_cents / 100).toFixed(2)}` : '—',
      icon: <IconPercentage />,
      color: '#05944F',
      delta: stats?.delta_vs_yesterday ? <DeltaBadge value={stats.delta_vs_yesterday.cost_cents / 100} suffix="元" /> : null,
      budgetBar: stats ? (() => {
        const total = (stats.wallet_balance ?? 0) + (stats.wallet_granted ?? 0);
        const used = stats.wallet_frozen ?? 0;
        const pct = total > 0 ? (used / total) * 100 : 0;
        if (total <= 0) return null;
        const barColor = pct >= 80 ? '#F53F3F' : pct >= 50 ? '#F5A623' : '#05944F';
        return (
          <div style={{ marginTop: 8 }}>
            <div style={{ height: 4, background: '#e8e8e8', borderRadius: 2, overflow: 'hidden' }}>
              <div style={{ height: '100%', width: `${Math.min(pct, 100)}%`, background: barColor, borderRadius: 2, transition: 'width 0.3s' }} />
            </div>
            <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 2 }}>
              <Text size="small" type="secondary">预算使用 {pct.toFixed(0)}%</Text>
              <Text size="small" type="secondary">余额 ¥{((stats.wallet_balance ?? 0) / 100).toFixed(0)}</Text>
            </div>
          </div>
        );
      })() : null,
    },
    {
      label: '平均延迟',
      value: stats ? `${stats.avg_latency_ms}ms` : '—',
      icon: <IconClock />,
      color: '#F5A623',
      delta: stats?.delta_vs_yesterday ? <DeltaBadge value={stats.delta_vs_yesterday.latency_ms} suffix="ms" invert /> : null,
    },
    {
      label: '成功率',
      value: stats ? `${(stats.success_rate * 100).toFixed(1)}%` : '—',
      icon: <IconTick />,
      color: '#7B61FF',
      delta: stats?.delta_vs_yesterday ? <DeltaBadge value={stats.delta_vs_yesterday.success_rate} suffix="%" /> : null,
    },
  ];

  // Budget usage warning
  const totalBudget = (stats?.wallet_balance ?? 0) + (stats?.wallet_granted ?? 0);
  const frozen = stats?.wallet_frozen ?? 0;
  const usedBudget = frozen + (stats?.wallet_threshold ?? 0);
  const budgetUsagePct = totalBudget > 0 ? (usedBudget / totalBudget) * 100 : 0;
  const showBudgetWarning = totalBudget > 0 && budgetUsagePct >= 50;

  const priceColumns = [
    { title: '模型名称', dataIndex: 'model_name', key: 'model_name' },
    {
      title: '输入价格 (元/千token)',
      dataIndex: 'input_price_cents',
      key: 'input',
      render: (v: number) => `¥${(v / 100).toFixed(4)}`,
    },
    {
      title: '输出价格 (元/千token)',
      dataIndex: 'output_price_cents',
      key: 'output',
      render: (v: number) => `¥${(v / 100).toFixed(4)}`,
    },
  ];

  const logColumns = [
    { title: '请求ID', dataIndex: 'request_id', key: 'rid', ellipsis: true, width: 200 },
    { title: '模型', dataIndex: 'model', key: 'model' },
    { title: '状态码', dataIndex: 'status_code', key: 'sc', width: 80 },
    {
      title: '耗时',
      dataIndex: 'latency_ms',
      key: 'latency',
      width: 80,
      render: (v: number) => `${v}ms`,
    },
    { title: '时间', dataIndex: 'created_at', key: 'time', render: (v: string) => v ? formatDateTime(v) : '' },
  ];

  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: 100 }}>
        <Spin size="large" />
      </div>
    );
  }

  return (
    <div>
      <Title heading={4} style={{ marginBottom: 24 }}>仪表盘</Title>

      <div style={{ marginBottom: 16, display: 'flex', gap: 8, alignItems: 'center' }}>
        {(['today', '7d', '30d'] as TimeRange[]).map((r) => (
          <button
            key={r}
            onClick={() => { setTimeRange(r); setCustomRange(null); }}
            style={{
              padding: '6px 16px',
              borderRadius: 6,
              border: timeRange === r && !customRange ? '2px solid #0077FA' : '1px solid #e0e0e0',
              background: timeRange === r && !customRange ? '#f0f7ff' : '#fff',
              color: timeRange === r && !customRange ? '#0077FA' : '#333',
              cursor: 'pointer',
              fontWeight: timeRange === r && !customRange ? 600 : 400,
              fontSize: 14,
            }}
          >
            {r === 'today' ? '今天' : r === '7d' ? '近7天' : '近30天'}
          </button>
        ))}
        <DatePicker
          type="dateRange"
          value={customRange ?? undefined}
          onChange={(v) => {
            const arr = v as Date[] | undefined;
            if (arr && arr.length === 2) {
              setCustomRange([arr[0], arr[1]]);
              setTimeRange('custom');
            }
          }}
          placeholder={['开始日期', '结束日期']}
          style={{ marginLeft: 8 }}
        />
      </div>
      {showBudgetWarning && (
        <div style={{
          padding: '12px 16px',
          marginBottom: 24,
          background: budgetUsagePct >= 80 ? '#fff1f0' : '#fffbe6',
          border: budgetUsagePct >= 80 ? '1px solid #ffa39e' : '1px solid #ffe58f',
          borderRadius: 8,
          display: 'flex',
          alignItems: 'center',
          gap: 8,
        }}>
          <span style={{ fontSize: 18 }}>{budgetUsagePct >= 80 ? '🔴' : '🟡'}</span>
          <Text style={{ color: budgetUsagePct >= 80 ? '#cf1322' : '#d48806' }}>
            {budgetUsagePct >= 80
              ? `预算使用率已达 ${budgetUsagePct.toFixed(1)}%，建议尽快充值`
              : `预算使用率已达 ${budgetUsagePct.toFixed(1)}%，请关注余额`}
          </Text>
          <Text type="secondary" size="small" style={{ marginLeft: 'auto' }}>
            余额: ¥{((stats?.wallet_balance ?? 0) / 100).toFixed(2)} | 冻结: ¥{(frozen / 100).toFixed(2)}
          </Text>
        </div>
      )}

      <Row gutter={16} style={{ marginBottom: 24 }}>
        {statCards.map((card) => (
          <Col span={6} key={card.label}>
            <Card className="stat-card">
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
                <div style={{ display: 'flex', alignItems: 'center' }}>
                  <Text type="secondary" size="small">{card.label}</Text>
                  {(card as any).delta}
                </div>
                <div style={{ color: card.color, fontSize: 20 }}>{card.icon}</div>
              </div>
              <Title heading={3} style={{ margin: 0, color: card.color }}>{card.value}</Title>
              {(card as any).budgetBar}
            </Card>
          </Col>
        ))}
      </Row>

      <Row gutter={16} style={{ marginBottom: 24 }}>
        <Col span={8}>
          <Card style={{ height: '100%' }}>
            <Title heading={5}>模型请求分布</Title>
            <ModelDistributionPie data={stats?.model_distribution || []} />
          </Card>
        </Col>
        <Col span={16}>
          <Card style={{ height: '100%' }}>
            <Title heading={5}>渠道健康状态</Title>
            <ChannelHealthPanel data={stats?.channel_health || []} />
          </Card>
        </Col>
      </Row>

      <Row gutter={16}>
        <Col span={12}>
          <Card style={{ marginBottom: 16 }}>
            <Title heading={5}>模型价格</Title>
            <Table
              dataSource={stats?.model_prices || []}
              columns={priceColumns}
              rowKey="model_name"
              size="small"
              pagination={false}
            />
          </Card>
        </Col>
        <Col span={12}>
          <Card>
            <Title heading={5}>最近调用</Title>
            <Table
              dataSource={stats?.recent_logs || []}
              columns={logColumns}
              rowKey="request_id"
              size="small"
              pagination={false}
            />
          </Card>
        </Col>
      </Row>
    </div>
  );
}
