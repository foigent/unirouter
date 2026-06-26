import { formatDateTime } from '../utils/formatDate';
import { useState, useEffect } from 'react';
import { Typography, Row, Col, Card, Table, Spin } from '@douyinfe/semi-ui';
import { IconHistogram, IconPercentage, IconClock, IconTick } from '@douyinfe/semi-icons';
import { api } from '../api/client';

const { Title, Text } = Typography;

interface DashboardStats {
  total_requests: number;
  today_cost_cents: number;
  avg_latency_ms: number;
  success_rate: number;
  recent_logs: any[];
  model_prices: { model: string; input_price_cents: number; output_price_cents: number }[];
  wallet_balance?: number;
  wallet_granted?: number;
  wallet_frozen?: number;
  wallet_threshold?: number;
  daily_trend?: { date: string; requests: number; cost_cents: number }[];
}

export default function DashboardPage() {
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.getDashboardStats()
      .then((data) => setStats(data))
      .catch(console.error)
      .finally(() => setLoading(false));
  }, []);

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
    },
    {
      label: '平均延迟',
      value: stats ? `${stats.avg_latency_ms}ms` : '—',
      icon: <IconClock />,
      color: '#F5A623',
    },
    {
      label: '成功率',
      value: stats ? `${(stats.success_rate * 100).toFixed(1)}%` : '—',
      icon: <IconTick />,
      color: '#7B61FF',
    },
  ];

  // Budget usage warning
  const totalBudget = (stats?.wallet_balance ?? 0) + (stats?.wallet_granted ?? 0);
  const frozen = stats?.wallet_frozen ?? 0;
  const usedBudget = frozen + (stats?.wallet_threshold ?? 0);
  const budgetUsagePct = totalBudget > 0 ? (usedBudget / totalBudget) * 100 : 0;
  const showBudgetWarning = totalBudget > 0 && budgetUsagePct >= 50;

  const priceColumns = [
    { title: '模型名称', dataIndex: 'model', key: 'model' },
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
                <Text type="secondary" size="small">{card.label}</Text>
                <div style={{ color: card.color, fontSize: 20 }}>{card.icon}</div>
              </div>
              <Title heading={3} style={{ margin: 0, color: card.color }}>{card.value}</Title>
            </Card>
          </Col>
        ))}
      </Row>

      <Row gutter={16}>
        <Col span={12}>
          <Card style={{ marginBottom: 16 }}>
            <Title heading={5}>模型价格</Title>
            <Table
              dataSource={stats?.model_prices || []}
              columns={priceColumns}
              rowKey="model"
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
