import { useState, useEffect } from 'react';
import { Table, Card, Select, Toast, Tag, Typography } from '@douyinfe/semi-ui';
import { api } from '../api/client';

const { Title } = Typography;

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
  const [loading, setLoading] = useState(true);
  const [modelFilter, setModelFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState('');

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
        <Tag color={v === 'reconciled' ? 'green' : v === 'pending' ? 'orange' : 'red'}>
          {v === 'reconciled' ? '已调账' : v === 'pending' ? '待处理' : v}
        </Tag>
      ),
    },
    { title: '时间', dataIndex: 'created_at', key: 'created_at', width: 180 },
  ];

  return (
    <div>
      <Title heading={4} style={{ marginBottom: 16 }}>Token 对账</Title>
      <Card style={{ marginBottom: 16 }}>
        <div style={{ display: 'flex', gap: 16, alignItems: 'center', flexWrap: 'wrap' }}>
          <Select
            placeholder="模型筛选"
            style={{ width: 200 }}
            value={modelFilter || undefined}
            onChange={(v) => { setModelFilter((v as string) || ''); setPage(1); }}
            optionList={[
              { label: '全部模型', value: '' },
              { label: 'gpt-4', value: 'gpt-4' },
              { label: 'gpt-3.5-turbo', value: 'gpt-3.5-turbo' },
              { label: 'claude-3-opus', value: 'claude-3-opus' },
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
          <button className="semi-button semi-button-secondary" onClick={() => load()}>刷新</button>
        </div>
      </Card>
      <Card>
        <Table
          columns={columns}
          dataSource={records}
          loading={loading}
          rowKey="id"
          pagination={{
            currentPage: page,
            pageSize: 20,
            total,
            onChange: (p: number) => setPage(p),
          }}
        />
      </Card>
    </div>
  );
}
