import { useEffect, useState } from 'react';
import { Button, Input, Select, Table, Toast, Typography } from '@douyinfe/semi-ui';
import { IconCoinMoney, IconDownload, IconRefresh, IconSearch, IconServer } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { MetricTile, MoneyText, PageHeader, StatusTag, Surface } from '../ui';

const { Text } = Typography;

interface AnalyticsRow {
  key: string;
  total_requests: number;
  success_requests: number;
  failed_requests: number;
  total_tokens: number;
  cost_cents: number;
  avg_latency_ms: number;
  estimated_ratio: number;
}

interface AnalyticsSummary {
  total_requests: number;
  success_requests: number;
  failed_requests: number;
  total_tokens: number;
  cost_cents: number;
  avg_latency_ms: number;
  estimated_tokens: number;
  upstream_tokens: number;
  estimated_ratio: number;
  csv_export_scope: string;
  prompt_retention: string;
  response_retention: string;
}

const groupOptions = [
  { label: 'Project', value: 'project' },
  { label: 'API Key', value: 'key' },
  { label: 'Model', value: 'model' },
  { label: '别名', value: 'alias' },
  { label: '供应商', value: 'provider' },
  { label: '渠道', value: 'channel' },
  { label: 'Status', value: 'status' },
  { label: 'Usage source', value: 'usage_source' },
];

const usageSourceOptions = [
  { label: '全部来源', value: '' },
  { label: 'upstream', value: 'upstream' },
  { label: 'counter_v2', value: 'counter_v2' },
  { label: 'adapter_estimated', value: 'adapter_estimated' },
  { label: 'estimated', value: 'estimated' },
  { label: 'no_route', value: 'no_route' },
  { label: 'upstream_failed', value: 'upstream_failed' },
];

function percent(value?: number) {
  return `${((value || 0) * 100).toFixed(1)}%`;
}

function compact(value?: number) {
  const n = Number(value || 0);
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toLocaleString();
}

export default function UsageAnalyticsPage() {
  const [rows, setRows] = useState<AnalyticsRow[]>([]);
  const [summary, setSummary] = useState<AnalyticsSummary | null>(null);
  const [groupBy, setGroupBy] = useState('project');
  const [status, setStatus] = useState('');
  const [usageSource, setUsageSource] = useState('');
  const [model, setModel] = useState('');
  const [provider, setProvider] = useState('');
  const [loading, setLoading] = useState(true);

  const filters = {
    group_by: groupBy,
    status,
    usage_source: usageSource,
    model,
    provider,
  };

  const load = () => {
    setLoading(true);
    api.getUsageAnalytics(filters)
      .then((data: any) => {
        setRows(data?.data || []);
        setSummary(data?.summary || null);
      })
      .catch((err: any) => Toast.error(`用量分析加载失败${err?.message ? `：${err.message}` : ''}`))
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, [groupBy, status, usageSource, model, provider]);

  const downloadCSV = async () => {
    try {
      const csv = await api.getUsageAnalytics({ ...filters, format: 'csv' }) as string;
      const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = 'uniroute-usage-analytics.csv';
      link.click();
      URL.revokeObjectURL(url);
    } catch (err: any) {
      Toast.error(`CSV 导出失败${err?.message ? `：${err.message}` : ''}`);
    }
  };

  const columns = [
    { title: groupOptions.find(option => option.value === groupBy)?.label || 'Group', dataIndex: 'key', key: 'key', render: (value: string) => <Text code>{value}</Text> },
    { title: '请求数', dataIndex: 'total_requests', key: 'total_requests', render: (value: number) => compact(value) },
    { title: '成功 / 失败', key: 'status_mix', render: (_: unknown, row: AnalyticsRow) => `${row.success_requests || 0} / ${row.failed_requests || 0}` },
    { title: 'Tokens', dataIndex: 'total_tokens', key: 'total_tokens', render: (value: number) => compact(value) },
    { title: '费用', dataIndex: 'cost_cents', key: 'cost_cents', render: (value: number) => <MoneyText cents={value || 0} precision={4} tone="cost" /> },
    { title: '平均延迟', dataIndex: 'avg_latency_ms', key: 'avg_latency_ms', render: (value: number) => `${Number(value || 0).toFixed(0)}ms` },
    { title: '估算占比', dataIndex: 'estimated_ratio', key: 'estimated_ratio', render: (value: number) => <StatusTag status={(value || 0) > 0.2 ? 'warning' : 'ok'}>{percent(value)}</StatusTag> },
  ];

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="Analytics"
        title="用量分析"
        description="按项目、Key、模型、别名、供应商、渠道、状态和 usage_source 钻取调用、Token、费用与估算占比。"
        actions={<Button icon={<IconDownload />} onClick={downloadCSV}>导出 CSV</Button>}
      />

      <Surface title="筛选与分组">
        <div className="ur-inline-controls">
          <Select value={groupBy} optionList={groupOptions} onChange={(value) => setGroupBy(String(value || 'project'))} style={{ width: 180 }} />
          <Select
            value={status || undefined}
            placeholder="状态"
            optionList={[
              { label: '全部状态', value: '' },
              { label: 'success', value: 'success' },
              { label: 'error', value: 'error' },
            ]}
            onChange={(value) => setStatus(String(value || ''))}
            style={{ width: 150 }}
          />
          <Select
            value={usageSource || undefined}
            placeholder="usage_source"
            optionList={usageSourceOptions}
            onChange={(value) => setUsageSource(String(value || ''))}
            style={{ width: 190 }}
          />
          <Input prefix={<IconSearch />} value={model} onChange={setModel} placeholder="模型" showClear style={{ width: 180 }} />
          <Input prefix={<IconServer />} value={provider} onChange={setProvider} placeholder="供应商" showClear style={{ width: 180 }} />
          <Button icon={<IconRefresh />} onClick={load} loading={loading}>刷新</Button>
        </div>
      </Surface>

      <div className="ur-kpi-grid">
        <MetricTile label="请求数" value={compact(summary?.total_requests)} icon={<IconSearch />} tone="blue" meta={`${summary?.success_requests || 0} success / ${summary?.failed_requests || 0} failed`} />
        <MetricTile label="Tokens" value={compact(summary?.total_tokens)} icon={<IconServer />} tone="violet" meta={`估算来源 ${compact(summary?.estimated_tokens)} / 上游 ${compact(summary?.upstream_tokens)}`} />
        <MetricTile label="费用" value={<MoneyText cents={summary?.cost_cents || 0} precision={4} strong tone="cost" />} icon={<IconCoinMoney />} tone="green" meta={`平均延迟 ${(summary?.avg_latency_ms || 0).toFixed(0)}ms`} />
        <MetricTile label="估算占比" value={percent(summary?.estimated_ratio)} icon={<IconSearch />} tone={(summary?.estimated_ratio || 0) > 0.2 ? 'amber' : 'slate'} meta={summary?.csv_export_scope || 'filtered_query'} />
      </div>

      <Surface title="钻取结果" description={`${summary?.prompt_retention || 'not_stored_by_default'} / ${summary?.response_retention || 'not_stored_by_default'}`}>
        <div className="ur-table-shell">
          <Table
            columns={columns}
            dataSource={rows}
            rowKey="key"
            loading={loading}
            pagination={false}
            size="small"
          />
        </div>
      </Surface>
    </div>
  );
}
