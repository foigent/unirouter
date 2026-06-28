import { formatDateTime } from '../utils/formatDate';
import { useState, useEffect } from 'react';
import { Typography, Table, Tag } from '@douyinfe/semi-ui';
import { api } from '../api/client';

const { Title } = Typography;

type TagColor = 'amber' | 'blue' | 'cyan' | 'green' | 'grey' | 'indigo' | 'light-blue' | 'light-green' | 'lime' | 'orange' | 'pink' | 'purple' | 'red' | 'teal' | 'violet' | 'yellow' | 'white';
const ACTION_COLORS: Record<string, TagColor> = {
  create: 'green', update: 'blue', delete: 'red',
  login: 'purple', topup: 'cyan', grant: 'green',
  budget_alert: 'orange', freeze: 'yellow', settle: 'blue',
};

export default function AuditLogsPage() {
  const [logs, setLogs] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);

  const load = (p = page) => {
    setLoading(true);
    api.getAuditLogs(p, 20)
      .then((d: any) => { setLogs(d.data || []); setTotal(d.total || 0); })
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(1); }, []);

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '操作者', dataIndex: 'actor' },
    {
      title: '动作', dataIndex: 'action',
      render: (v: string) => <Tag color={ACTION_COLORS[v] || 'grey'}>{v}</Tag>,
    },
    { title: '目标类型', dataIndex: 'target_type' },
    { title: '目标ID', dataIndex: 'target_id', render: (v: number) => v || '-' },
    {
      title: '详情', dataIndex: 'detail',
      render: (v: any) => v ? <span style={{ fontFamily: 'monospace', fontSize: 12 }}>{JSON.stringify(v)}</span> : '-',
    },
    { title: '时间', dataIndex: 'created_at', render: (v: string) => formatDateTime(v) },
  ];

  return (
    <div>
      <Title heading={4} style={{ marginBottom: 16 }}>审计日志</Title>
      <Table columns={columns} dataSource={logs} loading={loading} rowKey="id"
        pagination={{ currentPage: page, pageSize, pageSizeOpts: [20, 50, 100], showSizeChanger: true, total, onPageChange: (p) => { setPage(p); load(p); }, onPageSizeChange: (s) => { setPageSize(s); setPage(1); load(1); } }} />
    </div>
  );
}
