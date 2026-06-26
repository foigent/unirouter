import { formatDateTime } from '../utils/formatDate';
import { useState, useEffect } from 'react';
import { Typography, Button, Table, Modal, Form, Toast } from '@douyinfe/semi-ui';
import { IconPlus } from '@douyinfe/semi-icons';
import { api } from '../api/client';

const { Title } = Typography;

export default function GrantsPage() {
  const [grants, setGrants] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [formApi, setFormApi] = useState<any>(null);

  const load = (p = page) => {
    setLoading(true);
    api.getGrants(p, 20)
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
      Toast.success('赠送成功');
      setShowCreate(false);
      formApi?.reset();
      load(1);
    } catch { Toast.error('赠送失败'); }
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '组织', dataIndex: 'org_name', render: (v: string) => v || '-' },
    { title: '金额（元）', dataIndex: 'amount_cents', render: (v: number) => `¥${(v / 100).toFixed(2)}` },
    { title: '原因', dataIndex: 'reason' },
    { title: '请求ID', dataIndex: 'client_request_id', render: (v: string) => <span style={{ fontFamily: 'monospace', fontSize: 12 }}>{v}</span> },
    { title: '时间', dataIndex: 'created_at', render: (v: string) => formatDateTime(v) },
  ];

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <Title heading={4} style={{ margin: 0 }}>赠送管理</Title>
        <Button icon={<IconPlus />} theme="solid" onClick={() => setShowCreate(true)}>新增赠送</Button>
      </div>
      <Table columns={columns} dataSource={grants} loading={loading} rowKey="id"
        pagination={{ currentPage: page, pageSize: 20, total, onPageChange: (p) => { setPage(p); load(p); } }} />

      <Modal title="新增赠送" visible={showCreate}
        onCancel={() => { setShowCreate(false); formApi?.reset(); }}
        onOk={() => formApi?.submitForm()} okText="确认赠送">
        <Form getFormApi={(a) => setFormApi(a)} onSubmit={handleCreate}>
          <Form.Input field="amount" label="金额（元）" placeholder="100.00" rules={[{ required: true }]} />
          <Form.Input field="reason" label="原因" placeholder="试用赠送" rules={[{ required: true }]} />
        </Form>
      </Modal>
    </div>
  );
}
