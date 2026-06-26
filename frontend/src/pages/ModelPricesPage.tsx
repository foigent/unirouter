import { useState, useEffect } from 'react';
import { Typography, Button, Table, Modal, Form, Toast } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api } from '../api/client';

const { Title } = Typography;

export default function ModelPricesPage() {
  const [prices, setPrices] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [editPrice, setEditPrice] = useState<any>(null);
  const [createApi, setCreateApi] = useState<any>(null);
  const [editApi, setEditApi] = useState<any>(null);

  const load = (p = page) => {
    setLoading(true);
    api.getModelPriceList(p, 20)
      .then((d: any) => { setPrices(d.data || []); setTotal(d.total || 0); })
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(1); }, []);

  const handleCreate = async (v: any) => {
    try {
      await api.createModelPrice({
        model_name: v.model_name,
        input_price_cents: Math.round(parseFloat(v.input_price) * 100),
        output_price_cents: Math.round(parseFloat(v.output_price) * 100),
      });
      Toast.success('价格创建成功');
      setShowCreate(false);
      createApi?.reset();
      load(1);
    } catch { Toast.error('创建失败'); }
  };

  const handleUpdate = async (v: any) => {
    if (!editPrice) return;
    try {
      await api.updateModelPrice(editPrice.id, {
        model_name: v.model_name,
        input_price_cents: Math.round(parseFloat(v.input_price) * 100),
        output_price_cents: Math.round(parseFloat(v.output_price) * 100),
      });
      Toast.success('更新成功');
      setEditPrice(null);
      load();
    } catch { Toast.error('更新失败'); }
  };

  const handleDelete = (r: any) => {
    Modal.confirm({
      title: '确认删除',
      content: `确定要删除模型 ${r.model_name || r.model} 的价格吗？`,
      onOk: async () => {
        try {
          await api.deleteModelPrice(r.id);
          Toast.success('删除成功');
          load();
        } catch { Toast.error('删除失败'); }
      },
    });
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '模型', dataIndex: 'model_name' },
    { title: '输入价格（分/1M token）', dataIndex: 'input_price_cents' },
    { title: '输出价格（分/1M token）', dataIndex: 'output_price_cents' },
    {
      title: '操作',
      render: (_: any, r: any) => (
        <div style={{ display: 'flex', gap: 8 }}>
          <Button size="small" icon={<IconEdit />} onClick={() => setEditPrice(r)}>编辑</Button>
          <Button size="small" theme="solid" type="danger" icon={<IconDelete />} onClick={() => handleDelete(r)}>删除</Button>
        </div>
      ),
    },
  ];

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <Title heading={4} style={{ margin: 0 }}>模型价格管理</Title>
        <Button icon={<IconPlus />} theme="solid" onClick={() => setShowCreate(true)}>新增价格</Button>
      </div>
      <Table columns={columns} dataSource={prices} loading={loading} rowKey="id"
        pagination={{ currentPage: page, pageSize: 20, total, onPageChange: (p) => { setPage(p); load(p); } }} />

      <Modal title="新增模型价格" visible={showCreate}
        onCancel={() => { setShowCreate(false); createApi?.reset(); }}
        onOk={() => createApi?.submitForm()} okText="创建">
        <Form getFormApi={(a) => setCreateApi(a)} onSubmit={handleCreate}>
          <Form.Input field="model_name" label="模型名称" placeholder="gpt-4o" rules={[{ required: true }]} />
          <Form.Input field="input_price" label="输入价格（元/1M token）" placeholder="2.50" rules={[{ required: true }]} />
          <Form.Input field="output_price" label="输出价格（元/1M token）" placeholder="10.00" rules={[{ required: true }]} />
        </Form>
      </Modal>

      <Modal title="编辑模型价格" visible={!!editPrice} onCancel={() => setEditPrice(null)}
        onOk={() => editApi?.submitForm()} okText="保存">
        <Form getFormApi={(a) => setEditApi(a)} onSubmit={handleUpdate}
          initValues={editPrice ? {
            model_name: editPrice.model_name,
            input_price: (editPrice.input_price_cents / 100).toFixed(2),
            output_price: (editPrice.output_price_cents / 100).toFixed(2),
          } : {}}>
          <Form.Input field="model_name" label="模型名称" rules={[{ required: true }]} />
          <Form.Input field="input_price" label="输入价格（元/1M token）" rules={[{ required: true }]} />
          <Form.Input field="output_price" label="输出价格（元/1M token）" rules={[{ required: true }]} />
        </Form>
      </Modal>
    </div>
  );
}
