import { useState, useEffect } from 'react';
import { Typography, Button, Table, Modal, Form, Toast, Tag } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api } from '../api/client';

const { Title } = Typography;

export default function OrgModelPricesPage() {
  const [prices, setPrices] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [editItem, setEditItem] = useState<any>(null);
  const [createApi, setCreateApi] = useState<any>(null);
  const [editApi, setEditApi] = useState<any>(null);

  const load = () => {
    setLoading(true);
    api.getOrgModelPrices(1)
      .then((d: any) => { setPrices(d.items || []); })
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, []);

  const handleCreate = async (v: any) => {
    try {
      await api.createOrgModelPrice({
        org_id: 1,
        model_name: v.model_name,
        multiplier: parseFloat(v.multiplier),
      });
      Toast.success('倍率创建成功');
      setShowCreate(false);
      createApi?.reset();
      load();
    } catch { Toast.error('创建失败'); }
  };

  const handleUpdate = async (v: any) => {
    if (!editItem) return;
    try {
      await api.updateOrgModelPrice(editItem.id, {
        multiplier: parseFloat(v.multiplier),
      });
      Toast.success('更新成功');
      setEditItem(null);
      load();
    } catch { Toast.error('更新失败'); }
  };

  const handleDelete = (r: any) => {
    Modal.confirm({
      title: '确认删除',
      content: `确定要删除模型 ${r.model_name} 的倍率配置吗？`,
      onOk: async () => {
        try {
          await api.deleteOrgModelPrice(r.id);
          Toast.success('删除成功');
          load();
        } catch { Toast.error('删除失败'); }
      },
    });
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '模型名称', dataIndex: 'model_name' },
    {
      title: '倍率',
      dataIndex: 'multiplier',
      render: (v: number) => (
        <Tag color={v === 1 ? 'green' : v > 1 ? 'orange' : 'blue'}>{v}x</Tag>
      ),
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      render: (v: string) => v ? new Date(v).toLocaleString('zh-CN') : '-',
    },
    {
      title: '操作',
      render: (_: any, r: any) => (
        <div style={{ display: 'flex', gap: 8 }}>
          <Button size="small" icon={<IconEdit />} onClick={() => setEditItem(r)}>编辑</Button>
          <Button size="small" theme="solid" type="danger" icon={<IconDelete />} onClick={() => handleDelete(r)}>删除</Button>
        </div>
      ),
    },
  ];

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <Title heading={4} style={{ margin: 0 }}>组织模型倍率</Title>
        <Button icon={<IconPlus />} theme="solid" onClick={() => setShowCreate(true)}>新增倍率</Button>
      </div>
      <Table columns={columns} dataSource={prices} loading={loading} rowKey="id" pagination={false} />

      <Modal title="新增模型倍率" visible={showCreate}
        onCancel={() => { setShowCreate(false); createApi?.reset(); }}
        onOk={() => createApi?.submitForm()} okText="创建">
        <Form getFormApi={(a) => setCreateApi(a)} onSubmit={handleCreate}>
          <Form.Input field="model_name" label="模型名称" placeholder="gpt-4o" rules={[{ required: true }]} />
          <Form.Input field="multiplier" label="倍率" placeholder="1.0" rules={[{ required: true }]} />
        </Form>
      </Modal>

      <Modal title="编辑模型倍率" visible={!!editItem} onCancel={() => setEditItem(null)}
        onOk={() => editApi?.submitForm()} okText="保存">
        <Form getFormApi={(a) => setEditApi(a)} onSubmit={handleUpdate}
          initValues={editItem ? { multiplier: String(editItem.multiplier) } : {}}>
          <Form.Input field="model_name" label="模型名称" disabled initValue={editItem?.model_name} />
          <Form.Input field="multiplier" label="倍率" rules={[{ required: true }]} />
        </Form>
      </Modal>
    </div>
  );
}
