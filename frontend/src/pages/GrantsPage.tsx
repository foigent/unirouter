import { useState, useEffect } from 'react';
import { Button, Form, Toast, SideSheet } from '@douyinfe/semi-ui';
import { IconPlus } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { formatDateTime } from '../utils/formatDate';
import { MoneyText, PagedTable, PageHeader, Surface } from '../ui';

export default function GrantsPage() {
  const [grants, setGrants] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);
  const [showDrawer, setShowDrawer] = useState(false);
  const [formApi, setFormApi] = useState<any>(null);

  const load = (p = page, size = pageSize) => {
    setLoading(true);
    api.getGrants(p, size)
      .then((d: any) => { setGrants(d.data || []); setTotal(d.total || 0); })
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(1); }, []);

  const handleCreate = async (v: any) => {
    try {
      await api.createGrant({
        amount_cents: Math.round(parseFloat(v.amount) * 100),
        reason: v.reason,
      });
      Toast.success('额度调整已记录');
      setShowDrawer(false);
      formApi?.reset();
      load(1);
    } catch { Toast.error('额度调整失败'); }
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '组织', dataIndex: 'org_name', render: (v: string) => v || '-' },
    { title: '金额（元）', dataIndex: 'amount_cents', render: (v: number) => <MoneyText cents={v} tone="income" /> },
    { title: '原因', dataIndex: 'reason' },
    { title: '请求ID', dataIndex: 'client_request_id', render: (v: string) => <span style={{ fontFamily: 'monospace', fontSize: 12 }}>{v}</span> },
    { title: '时间', dataIndex: 'created_at', render: (v: string) => formatDateTime(v) },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="Billing"
        title="额度调整"
        description="记录组织授信额度调整和业务原因，辅助财务追踪。"
        actions={<Button icon={<IconPlus />} type="primary" onClick={() => { formApi?.reset(); setShowDrawer(true); }}>新增调整</Button>}
      />

      <Surface>
        <PagedTable
          columns={columns}
          dataSource={grants}
          loading={loading}
          rowKey="id"
          page={page}
          pageSize={pageSize}
          total={total}
          onPageChange={(p: number) => { setPage(p); load(p, pageSize); }}
          onPageSizeChange={(s: number) => { setPageSize(s); setPage(1); load(1, s); }}
        />
      </Surface>

      <SideSheet
        title="新增额度调整"
        visible={showDrawer}
        onCancel={() => setShowDrawer(false)}
        width={480}
      >
        <Form getFormApi={(api: any) => setFormApi(api)} onSubmit={handleCreate}>
          <Form.Input field="amount" label="金额（元）" rules={[{ required: true, message: '必填' }]} placeholder="100.00" />
          <Form.Input field="reason" label="原因" placeholder="授信调整 / 商务补偿 / 活动额度" />
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 24 }}>
            <Button onClick={() => setShowDrawer(false)}>取消</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>确认调整</Button>
          </div>
        </Form>
      </SideSheet>
    </div>
  );
}
