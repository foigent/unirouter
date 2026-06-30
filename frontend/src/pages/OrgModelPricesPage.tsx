import { useState, useEffect } from 'react';
import { Typography, Button, Table, Modal, Form, Toast, Space, SideSheet } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api } from '../api/client';

const { Title } = Typography;

export default function OrgModelPricesPage() {
  const [prices, setPrices] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);
  const [showDrawer, setShowDrawer] = useState(false);
  const [editPrice, setEditPrice] = useState<any>(null);
  const [formApi, setFormApi] = useState<any>(null);
  const [orgs, setOrgs] = useState<any[]>([]);
  const [modelNames, setModelNames] = useState<string[]>([]);

  const load = (p = page) => {
    setLoading(true);
    api.getOrgModelPrices(1, p, 20)
      .then((d: any) => { setPrices(d.data || []); setTotal(d.total || 0); })
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  const loadOrgs = () => {
    api.getUsers(1, 100)
      .then((d: any) => {
        const orgMap = new Map();
        (d.data || []).forEach((u: any) => {
          if (u.org_id && u.org_name && !orgMap.has(u.org_id)) {
            orgMap.set(u.org_id, { label: u.org_name, value: u.org_id });
          }
        });
        setOrgs(Array.from(orgMap.values()));
      })
      .catch(() => {});
  };

  useEffect(() => { load(1); loadOrgs(); api.getModelNames().then((d: any) => setModelNames(d.data || [])).catch(() => {}); }, []);

  const closeDrawer = () => { setShowDrawer(false); setEditPrice(null); };

  const handleCreate = async (v: any) => {
    try {
      await api.createOrgModelPrice({
        org_id: parseInt(v.org_id),
        model_name: v.model_name,
        input_price_cents: Math.round(parseFloat(v.input_price) * 100),
        output_price_cents: Math.round(parseFloat(v.output_price) * 100),
      });
      Toast.success('创建成功');
      closeDrawer();
      load(1);
    } catch { Toast.error('创建失败'); }
  };

  const handleUpdate = async (v: any) => {
    if (!editPrice) return;
    try {
      await api.updateOrgModelPrice(editPrice.id, {
        model_name: v.model_name,
        input_price_cents: Math.round(parseFloat(v.input_price) * 100),
        output_price_cents: Math.round(parseFloat(v.output_price) * 100),
      });
      Toast.success('更新成功');
      closeDrawer();
      load();
    } catch { Toast.error('更新失败'); }
  };

  const handleDelete = (id: number) => {
    Modal.confirm({
      title: '确认删除',
      content: '确定要删除这个价格覆盖吗？',
      onOk: async () => {
        try {
          await api.deleteOrgModelPrice(id);
          Toast.success('已删除');
          load();
        } catch { Toast.error('删除失败'); }
      },
    });
  };

  const openCreate = () => {
    setEditPrice(null);
    setShowDrawer(true);
    formApi?.reset();
  };

  const openEdit = (r: any) => {
    setEditPrice(r);
    setShowDrawer(true);
    setTimeout(() => {
      formApi?.setValues({
        org_id: r.org_id?.toString(),
        model_name: r.model_name,
        input_price: (r.input_price_cents / 100).toString(),
        output_price: (r.output_price_cents / 100).toString(),
      });
    }, 100);
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '组织', dataIndex: 'org_name', render: (v: string) => v || '-' },
    { title: '模型', dataIndex: 'model_name' },
    { title: '输入价格（元/千token）', dataIndex: 'input_price_cents', render: (v: number) => `¥${(v / 100).toFixed(4)}` },
    { title: '输出价格（元/千token）', dataIndex: 'output_price_cents', render: (v: number) => `¥${(v / 100).toFixed(4)}` },
    { title: '更新时间', dataIndex: 'updated_at', render: (v: string) => v || '-' },
    {
      title: '操作', width: 120,
      render: (_: any, record: any) => (
        <Space>
          <Button icon={<IconEdit />} size="small" onClick={() => openEdit(record)} />
          <Button icon={<IconDelete />} size="small" type="danger" onClick={() => handleDelete(record.id)} />
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <Title heading={4} style={{ margin: 0 }}>组织模型价格</Title>
        <Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增价格覆盖</Button>
      </div>

      <Table
        columns={columns}
        dataSource={prices}
        loading={loading}
        rowKey="id"
        pagination={{
          currentPage: page, pageSize, total,
          onPageChange: (p: number) => { setPage(p); load(p); },
          onPageSizeChange: (s: number) => { setPageSize(s); setPage(1); load(1); },
        }}
      />

      <SideSheet
        title={editPrice ? '编辑价格覆盖' : '新增价格覆盖'}
        visible={showDrawer}
        onCancel={closeDrawer}
        width={480}
      >
        <Form getFormApi={(api: any) => setFormApi(api)} onSubmit={editPrice ? handleUpdate : handleCreate}>
          <Form.Select field="org_id" label="组织" rules={[{ required: true, message: '必填' }]}
            optionList={orgs} disabled={!!editPrice} />
          <Form.Select field="model_name" label="模型名称" rules={[{ required: true, message: '请选择模型' }]} filter optionList={modelNames.map(m => ({ label: m, value: m }))} placeholder="选择或搜索模型" />
          <Form.Input field="input_price" label="输入价格（元/千token）" rules={[{ required: true, message: '必填' }]} placeholder="0.01" />
          <Form.Input field="output_price" label="输出价格（元/千token）" rules={[{ required: true, message: '必填' }]} placeholder="0.03" />
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 24 }}>
            <Button onClick={closeDrawer}>取消</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>{editPrice ? '更新' : '创建'}</Button>
          </div>
        </Form>
      </SideSheet>
    </div>
  );
}
