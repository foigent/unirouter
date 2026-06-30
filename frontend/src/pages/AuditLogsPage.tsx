import { formatDateTime } from '../utils/formatDate';
import { useState, useEffect } from 'react';
import { Table, Tag } from '@douyinfe/semi-ui';
import { api } from '../api/client';
import { PageHeader, Surface } from '../ui';

type TagColor = 'amber' | 'blue' | 'cyan' | 'green' | 'grey' | 'indigo' | 'light-blue' | 'light-green' | 'lime' | 'orange' | 'pink' | 'purple' | 'red' | 'teal' | 'violet' | 'yellow' | 'white';
const ACTION_COLORS: Record<string, TagColor> = {
  create: 'green', update: 'blue', delete: 'red',
  login: 'purple', topup: 'cyan', grant: 'green',
  budget_alert: 'orange', freeze: 'yellow', settle: 'blue',
  change_password: 'orange',
};

function actionColor(action: string): TagColor {
  const suffix = action?.split('.')?.pop() || '';
  return ACTION_COLORS[action] || ACTION_COLORS[suffix] || 'grey';
}

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
    { title: '操作者', dataIndex: 'user_id', render: (v: number) => v ? `User #${v}` : '-' },
    {
      title: '动作', dataIndex: 'action',
      render: (v: string) => <Tag color={actionColor(v)}>{v}</Tag>,
    },
    { title: '目标类型', dataIndex: 'resource' },
    { title: '目标ID', dataIndex: 'resource_id', render: (v: string) => v || '-' },
    {
      title: '详情', dataIndex: 'detail',
      render: (v: any) => v ? <span style={{ fontFamily: 'monospace', fontSize: 12 }}>{JSON.stringify(v)}</span> : '-',
    },
    { title: '时间', dataIndex: 'created_at', render: (v: string) => formatDateTime(v) },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="Access"
        title="审计日志"
        description="查看登录、变更、充值、赠送和结算等关键后台动作。"
      />
      <Surface>
        <div className="ur-table-shell">
          <Table columns={columns} dataSource={logs} loading={loading} rowKey="id"
            pagination={{ currentPage: page, pageSize, pageSizeOpts: [20, 50, 100], showSizeChanger: true, total, onPageChange: (p) => { setPage(p); load(p); }, onPageSizeChange: (s) => { setPageSize(s); setPage(1); load(1); } }} />
        </div>
      </Surface>
    </div>
  );
}
