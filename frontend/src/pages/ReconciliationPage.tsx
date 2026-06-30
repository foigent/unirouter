import { useState, useEffect } from 'react';
import { Button, Table, Select, Toast, Tag } from '@douyinfe/semi-ui';
import { api } from '../api/client';
import { PageHeader, StatusTag, Surface } from '../ui';

interface ReconRecord {
  id: number;
  request_id: string;
  model: string;
  estimated_tokens: number;
  actual_tokens: number;
  delta: number;
  status: string;
  created_at: string;
}

export default function ReconciliationPage() {
  const [records, setRecords] = useState<ReconRecord[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);
  const [modelFilter, setModelFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [modelNames, setModelNames] = useState<string[]>([]);

  const load = (p = page) => {
    setLoading(true);
    const params = new URLSearchParams({ page: String(p), page_size: '20' });
    if (modelFilter) params.set('model', modelFilter);
    if (statusFilter) params.set('status', statusFilter);
    api.request(`/reconciliation?${params.toString()}`)
      .then((res: any) => {
        setRecords(res.data || []);
        setTotal(res.total || 0);
      })
      .catch(() => Toast.error({ content: '加载失败' }))
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, [page, modelFilter, statusFilter]);
  useEffect(() => { api.getModelNames().then((d: any) => setModelNames(d.data || [])).catch(() => {}); }, []);

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 80 },
    { title: 'Request ID', dataIndex: 'request_id', key: 'request_id', ellipsis: true },
    { title: '模型', dataIndex: 'model', key: 'model', width: 150 },
    { title: '估算 Token', dataIndex: 'estimated_tokens', key: 'estimated_tokens', width: 120 },
    { title: '实际 Token', dataIndex: 'actual_tokens', key: 'actual_tokens', width: 120 },
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
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => (
        <StatusTag status={v === 'reconciled' ? 'success' : v === 'pending' ? 'pending' : 'failed'}>
          {v === 'reconciled' ? '已调账' : v === 'pending' ? '待处理' : v}
        </StatusTag>
      ),
    },
    { title: '时间', dataIndex: 'created_at', key: 'created_at', width: 180 },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="Billing"
        title="Token 对账"
        description="对比估算 Token 和实际 Token，辅助发现计费偏差。"
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
              { label: '待处理', value: 'pending' },
              { label: '已调账', value: 'reconciled' },
            ]}
          />
          <Button onClick={() => load()}>刷新</Button>
        </div>
      </Surface>
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
              onChange: (p: number) => setPage(p),
              onPageSizeChange: (s: number) => { setPageSize(s); setPage(1); },
            }}
          />
        </div>
      </Surface>
    </div>
  );
}
