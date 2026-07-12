import { useState, useEffect } from 'react';
import { Button, Modal, Form, Toast, Space, SideSheet } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { isPlatformAdminRole } from '../navigation';
import { useAuthStore } from '../store/auth';
import { MoneyText, PagedTable, PageHeader, Surface } from '../ui';

export function yuanPerKToCents(value: unknown) {
  const price = Number.parseFloat(String(value));
  if (!Number.isFinite(price) || price < 0) {
    throw new Error('invalid price');
  }
  return Number((price * 100).toFixed(6));
}

export default function ModelPricesPage() {
  const role = useAuthStore((s) => s.user?.role);
  const canMutate = isPlatformAdminRole(role);
  const [prices, setPrices] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);
  const [showDrawer, setShowDrawer] = useState(false);
  const [editPrice, setEditPrice] = useState<any>(null);
  const [formApi, setFormApi] = useState<any>(null);
  const [modelNames, setModelNames] = useState<string[]>([]);

  const load = (p = page, size = pageSize) => {
    setLoading(true);
    api.getModelPriceList(p, size)
      .then((d: any) => { setPrices(d.data || []); setTotal(d.total || 0); })
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(1); api.getModelNames().then((d: any) => setModelNames(d.data || [])).catch(() => {}); }, []);

  const closeDrawer = () => { setShowDrawer(false); setEditPrice(null); };

  const handleCreate = async (v: any) => {
    try {
      await api.createModelPrice({
        model_name: v.model_name,
        input_price_cents: yuanPerKToCents(v.input_price),
        output_price_cents: yuanPerKToCents(v.output_price),
        cache_read_ratio: Number(v.cache_read_ratio ?? 1),
        cache_creation_ratio: Number(v.cache_creation_ratio ?? 1),
      });
      Toast.success('价格创建成功');
      closeDrawer();
      load(1);
    } catch { Toast.error('创建失败'); }
  };

  const handleUpdate = async (v: any) => {
    if (!editPrice) return;
    try {
      await api.updateModelPrice(editPrice.id, {
        model_name: v.model_name,
        input_price_cents: yuanPerKToCents(v.input_price),
        output_price_cents: yuanPerKToCents(v.output_price),
        cache_read_ratio: Number(v.cache_read_ratio ?? 1),
        cache_creation_ratio: Number(v.cache_creation_ratio ?? 1),
      });
      Toast.success('更新成功');
      closeDrawer();
      load();
    } catch { Toast.error('更新失败'); }
  };

  const handleDelete = (id: number) => {
    Modal.confirm({
      title: '确认删除',
      content: '确定要删除这个价格配置吗？',
      onOk: async () => {
        try {
          await api.deleteModelPrice(id);
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
        model_name: r.model_name,
        input_price: (r.input_price_cents / 100).toString(),
        output_price: (r.output_price_cents / 100).toString(),
        cache_read_ratio: Number(r.cache_read_ratio ?? 1),
        cache_creation_ratio: Number(r.cache_creation_ratio ?? 1),
      });
    }, 100);
  };

  const columns: any[] = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '模型名称', dataIndex: 'model_name' },
    { title: '输入价格（元/千token）', dataIndex: 'input_price_cents', render: (v: number) => <MoneyText cents={v} precision={4} /> },
    { title: '输出价格（元/千token）', dataIndex: 'output_price_cents', render: (v: number) => <MoneyText cents={v} precision={4} /> },
    { title: '缓存读取倍率', dataIndex: 'cache_read_ratio', width: 120, render: (v: number) => `${Number(v ?? 1).toFixed(3)}×` },
    { title: '缓存写入倍率', dataIndex: 'cache_creation_ratio', width: 120, render: (v: number) => `${Number(v ?? 1).toFixed(3)}×` },
    { title: '更新时间', dataIndex: 'updated_at', render: (v: string) => v || '-' },
  ];
  if (canMutate) columns.push({
      title: '操作', width: 120,
      render: (_: any, record: any) => (
        <Space>
          <Button icon={<IconEdit />} size="small" onClick={() => openEdit(record)} />
          <Button icon={<IconDelete />} size="small" type="danger" onClick={() => handleDelete(record.id)} />
        </Space>
      ),
    });

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="Billing"
        title="模型定价"
        description={canMutate
          ? '维护基础输入/输出价格和供应商 Prompt Cache 倍率；只影响变更后的请求。'
          : '只读查看平台统一维护的基础模型价格与供应商 Prompt Cache 倍率。'}
        actions={canMutate ? <Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增价格</Button> : undefined}
      />

      <Surface>
        <PagedTable
          columns={columns}
          dataSource={prices}
          loading={loading}
          rowKey="id"
          page={page}
          pageSize={pageSize}
          total={total}
          onPageChange={(p: number) => { setPage(p); load(p, pageSize); }}
          onPageSizeChange={(s: number) => { setPageSize(s); setPage(1); load(1, s); }}
        />
      </Surface>

      {canMutate && <SideSheet
        title={editPrice ? '编辑价格' : '新增价格'}
        visible={showDrawer}
        onCancel={closeDrawer}
        width={480}
      >
        <Form getFormApi={(api: any) => setFormApi(api)} onSubmit={editPrice ? handleUpdate : handleCreate}>
          <Form.Select field="model_name" label="模型名称" rules={[{ required: true, message: '请选择模型' }]} filter optionList={modelNames.map(m => ({ label: m, value: m }))} placeholder="选择或搜索模型" />
          <Form.Input field="input_price" label="输入价格（元/千token）" rules={[{ required: true, message: '必填' }]} placeholder="0.002" />
          <Form.Input field="output_price" label="输出价格（元/千token）" rules={[{ required: true, message: '必填' }]} placeholder="0.001" />
          <Form.InputNumber field="cache_read_ratio" label="缓存读取倍率" initValue={1} min={0} max={100} precision={6} rules={[{ required: true, message: '必填' }]} placeholder="0.1" style={{ width: '100%' }} />
          <Form.InputNumber field="cache_creation_ratio" label="缓存写入倍率" initValue={1} min={0} max={100} precision={6} rules={[{ required: true, message: '必填' }]} placeholder="1.25" style={{ width: '100%' }} />
          <div style={{ marginTop: 8, color: 'var(--semi-color-text-2)', fontSize: 12, lineHeight: 1.7 }}>
            倍率以普通输入价格为基准：0.1 表示按 10% 计费，1.25 表示按 125% 计费。默认 1 不打折也不加价；仅供应商明确回传的 Prompt Cache Token 使用倍率，不是响应缓存。
          </div>
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 24 }}>
            <Button onClick={closeDrawer}>取消</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>{editPrice ? '更新' : '创建'}</Button>
          </div>
        </Form>
      </SideSheet>}
    </div>
  );
}
