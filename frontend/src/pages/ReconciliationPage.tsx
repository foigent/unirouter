import { useState, useEffect } from 'react';
import { Button, Table, Select, Toast, Tag } from '@douyinfe/semi-ui';
import { IconAlertTriangle, IconCoinMoney, IconRefresh, IconServer } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { MetricTile, PageHeader, StatusTag, Surface } from '../ui';
import { usageSourceLabel, usageSourceOptions } from '../utils/displayLabels';

interface ReconRecord {
  id: number;
  request_id: string;
  model: string;
  estimated_tokens: number;
  actual_tokens: number;
  delta: number;
  usage_source?: string;
  variance_tokens?: number;
  variance_ratio?: number;
  source_confidence?: number;
  status: string;
  created_at: string;
}

interface ReconSummary {
  estimated_tokens: number;
  upstream_tokens: number;
  preflight_estimated_tokens: number;
  actual_tokens: number;
  variance_tokens: number;
  variance_ratio: number;
  average_source_confidence: number;
  estimated_ratio: number;
  estimated_ratio_alert_threshold: number;
  estimated_usage_alert: boolean;
  estimated_ratio_scope: string;
}

function formatPercent(value?: number) {
  return `${((value || 0) * 100).toFixed(1)}%`;
}

export default function ReconciliationPage() {
  const [records, setRecords] = useState<ReconRecord[]>([]);
  const [summary, setSummary] = useState<ReconSummary | null>(null);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);
  const [modelFilter, setModelFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [usageSourceFilter, setUsageSourceFilter] = useState('');
  const [modelNames, setModelNames] = useState<string[]>([]);

  const load = (p = page) => {
    setLoading(true);
    api.getReconciliation(p, pageSize, {
      model: modelFilter,
      status: statusFilter,
      usage_source: usageSourceFilter,
    })
      .then((res: any) => {
        setRecords(res.data || []);
        setSummary(res.summary || null);
        setTotal(res.total || 0);
      })
      .catch(() => Toast.error({ content: '加载失败' }))
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, [page, pageSize, modelFilter, statusFilter, usageSourceFilter]);
  useEffect(() => { api.getModelNames().then((d: any) => setModelNames(d.data || [])).catch(() => {}); }, []);

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 80 },
    { title: 'Request ID', dataIndex: 'request_id', key: 'request_id', ellipsis: true },
    { title: '模型', dataIndex: 'model', key: 'model', width: 150 },
    { title: '估算 Token', dataIndex: 'estimated_tokens', key: 'estimated_tokens', width: 120 },
    { title: '实际 Token', dataIndex: 'actual_tokens', key: 'actual_tokens', width: 120 },
    {
      title: '来源',
      dataIndex: 'usage_source',
      key: 'usage_source',
      width: 132,
      render: (v: string) => v ? <StatusTag status={v}>{usageSourceLabel(v)}</StatusTag> : '-',
    },
    {
      title: '差值',
      dataIndex: 'delta',
      key: 'delta',
      width: 100,
      render: (v: number) => (
        <Tag color={v > 0 ? 'red' : v < 0 ? 'green' : 'grey'}>
          {v > 0 ? '+' : ''}{v}
        </Tag>
      ),
    },
    {
      title: '偏差率',
      dataIndex: 'variance_ratio',
      key: 'variance_ratio',
      width: 100,
      render: (v: number) => <Tag color={(v || 0) > 0 ? 'red' : (v || 0) < 0 ? 'green' : 'grey'}>{formatPercent(v)}</Tag>,
    },
    {
      title: '置信度',
      dataIndex: 'source_confidence',
      key: 'source_confidence',
      width: 100,
      render: (v: number) => formatPercent(v),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => (
        <StatusTag status={v === 'reconciled' ? 'success' : v === 'pending' ? 'pending' : 'failed'}>
          {v === 'reconciled' ? '正常' : v === 'error' ? '错误' : v}
        </StatusTag>
      ),
    },
    { title: '时间', dataIndex: 'created_at', key: 'created_at', width: 180 },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="Billing"
        title="用量对账"
        description="对比估算用量和上游回传用量，辅助发现计量偏差。"
      />
      <Surface title="筛选条件">
        <div style={{ display: 'flex', gap: 16, alignItems: 'center', flexWrap: 'wrap' }}>
          <Select
            placeholder="模型筛选"
            style={{ width: 200 }}
            value={modelFilter || undefined}
            onChange={(v) => { setModelFilter((v as string) || ''); setPage(1); }}
            optionList={[
              { label: '全部模型', value: '' },
              ...modelNames.map(m => ({ label: m, value: m })),
            ]}
          />
          <Select
            placeholder="状态筛选"
            style={{ width: 150 }}
            value={statusFilter || undefined}
            onChange={(v) => { setStatusFilter((v as string) || ''); setPage(1); }}
            optionList={[
              { label: '全部', value: '' },
              { label: '正常', value: 'reconciled' },
              { label: '错误', value: 'error' },
            ]}
          />
          <Select
            placeholder="来源筛选"
            style={{ width: 190 }}
            value={usageSourceFilter || undefined}
            onChange={(v) => { setUsageSourceFilter((v as string) || ''); setPage(1); }}
            optionList={usageSourceOptions}
          />
          <Button icon={<IconRefresh />} onClick={() => load()}>刷新</Button>
        </div>
      </Surface>
      <div className="ur-kpi-grid">
        <MetricTile
          label="估算占比"
          value={formatPercent(summary?.estimated_ratio)}
          icon={<IconAlertTriangle />}
          tone={summary?.estimated_usage_alert ? 'red' : 'green'}
          meta={(
            <span>
              阈值 {formatPercent(summary?.estimated_ratio_alert_threshold)}
              {' '}
              <StatusTag status={summary?.estimated_usage_alert ? 'warning' : 'success'}>
                {summary?.estimated_usage_alert ? '已触发' : '正常'}
              </StatusTag>
            </span>
          )}
        />
        <MetricTile
          label="估算来源 Token"
          value={(summary?.estimated_tokens || 0).toLocaleString()}
          icon={<IconServer />}
          tone="amber"
          meta={`口径 ${summary?.estimated_ratio_scope === 'filtered_query' ? '当前筛选口径' : summary?.estimated_ratio_scope || '当前筛选口径'}`}
        />
        <MetricTile
          label="上游 Token"
          value={(summary?.upstream_tokens || 0).toLocaleString()}
          icon={<IconCoinMoney />}
          tone="blue"
          meta={`实际合计 ${(summary?.actual_tokens || 0).toLocaleString()}`}
        />
        <MetricTile
          label="全量偏差"
          value={`${summary?.variance_tokens && summary.variance_tokens > 0 ? '+' : ''}${(summary?.variance_tokens || 0).toLocaleString()}`}
          icon={<IconAlertTriangle />}
          tone={(summary?.variance_tokens || 0) === 0 ? 'slate' : 'violet'}
          meta={`偏差率 ${formatPercent(summary?.variance_ratio)} / 置信度 ${formatPercent(summary?.average_source_confidence)}`}
        />
      </div>
      <Surface>
        <div className="ur-table-shell">
          <Table
            columns={columns}
            dataSource={records}
            loading={loading}
            rowKey="id"
            pagination={{
              currentPage: page,
              pageSize,
              pageSizeOpts: [20, 50, 100],
              showSizeChanger: true,
              total,
              onPageChange: (p: number) => setPage(p),
              onPageSizeChange: (s: number) => { setPageSize(s); setPage(1); },
            }}
          />
        </div>
      </Surface>
    </div>
  );
}
