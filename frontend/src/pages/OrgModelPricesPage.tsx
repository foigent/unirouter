import { useState, useEffect } from 'react';
import { Button, Modal, Form, Toast, Space, SideSheet, Input } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { isPlatformAdminRole } from '../navigation';
import { useAuthStore } from '../store/auth';
import { PagedTable, PageHeader, Surface } from '../ui';

export default function OrgModelPricesPage() {
  const user = useAuthStore((s) => s.user);
  const canMutate = isPlatformAdminRole(user?.role);
  const [prices, setPrices] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);
  const [showDrawer, setShowDrawer] = useState(false);
  const [editPrice, setEditPrice] = useState<any>(null);
  const [formApi, setFormApi] = useState<any>(null);
  const [modelNames, setModelNames] = useState<string[]>([]);
  const [selectedOrgID, setSelectedOrgID] = useState<number | undefined>();
  const [orgFilter, setOrgFilter] = useState('');

  const load = (p = page, size = pageSize, orgID = selectedOrgID) => {
    setLoading(true);
    api.getOrgModelPrices(p, size, orgID)
      .then((d: any) => { setPrices(d.data || d.items || []); setTotal(d.total || 0); })
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(1); api.getModelNames().then((d: any) => setModelNames(d.data || [])).catch(() => {}); }, []);

  const closeDrawer = () => { setShowDrawer(false); setEditPrice(null); };

  const handleCreate = async (v: any) => {
    const orgID = Number(v.org_id);
    try {
      await api.createOrgModelPrice({
        org_id: orgID,
        model_name: v.model_name,
        multiplier: Number(v.multiplier),
      });
      Toast.success('创建成功');
      closeDrawer();
      setSelectedOrgID(orgID);
      setOrgFilter(String(orgID));
      load(1, pageSize, orgID);
    } catch { Toast.error('创建失败'); }
  };

  const handleUpdate = async (v: any) => {
    if (!editPrice) return;
    try {
      await api.updateOrgModelPrice(editPrice.id, {
        multiplier: Number(v.multiplier),
      });
      Toast.success('更新成功');
      closeDrawer();
      load();
    } catch { Toast.error('更新失败'); }
  };

  const handleDelete = (id: number) => {
    Modal.confirm({
      title: '确认删除',
      content: '删除后，该组织会恢复使用 1.0 倍基础价格。确定继续吗？',
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
    setTimeout(() => formApi?.setValue('org_id', selectedOrgID || user?.org_id), 0);
  };

  const openEdit = (r: any) => {
    setEditPrice(r);
    setShowDrawer(true);
    setTimeout(() => {
      formApi?.setValues({
        org_id: r.org_id?.toString(),
        model_name: r.model_name,
        multiplier: r.multiplier,
      });
    }, 100);
  };

  const columns: any[] = [
    { title: '策略 ID', dataIndex: 'id', width: 90 },
    {
      title: '组织', dataIndex: 'org_name',
      render: (v: string, record: any) => v ? `${v}（ID ${record.org_id}）` : `组织 ID ${record.org_id}`,
    },
    { title: '模型', dataIndex: 'model_name' },
    { title: '计费倍率', dataIndex: 'multiplier', render: (v: number) => `${Number(v).toFixed(3).replace(/0+$/, '').replace(/\.$/, '')}×` },
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

  const applyOrgFilter = () => {
    const parsed = Number.parseInt(orgFilter, 10);
    if (!Number.isInteger(parsed) || parsed <= 0) {
      Toast.warning('请输入有效的组织 ID');
      return;
    }
    setSelectedOrgID(parsed);
    setPage(1);
    load(1, pageSize, parsed);
  };

  const resetOrgFilter = () => {
    setOrgFilter('');
    setSelectedOrgID(undefined);
    setPage(1);
    load(1, pageSize, undefined);
  };

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="Billing"
        title="组织计费倍率"
        description={canMutate
          ? '按组织设置基础模型价格的乘数；例如 1.2× 表示按基础价格的 1.2 倍计费。'
          : '只读查看本组织适用的计费倍率；倍率由平台管理员统一维护。'}
        actions={canMutate ? <Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增倍率</Button> : undefined}
      />

      <Surface>
        {canMutate && <Space style={{ marginBottom: 16 }}>
          <Input
            aria-label="目标组织 ID"
            placeholder="输入组织 ID"
            value={orgFilter}
            onChange={(value) => setOrgFilter(value)}
            style={{ width: 180 }}
          />
          <Button onClick={applyOrgFilter}>查看目标组织</Button>
          <Button onClick={resetOrgFilter}>返回我的组织</Button>
        </Space>}
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
        title={editPrice ? '编辑组织倍率' : '新增组织倍率'}
        visible={showDrawer}
        onCancel={closeDrawer}
        width={480}
      >
        <Form getFormApi={(api: any) => setFormApi(api)} onSubmit={editPrice ? handleUpdate : handleCreate}>
          <Form.InputNumber field="org_id" label="组织 ID" min={1} rules={[{ required: true, message: '必填' }]}
            disabled={!!editPrice} style={{ width: '100%' }} />
          <Form.Select field="model_name" label="模型名称" rules={[{ required: true, message: '请选择模型' }]} filter optionList={modelNames.map(m => ({ label: m, value: m }))} placeholder="选择或搜索模型" disabled={!!editPrice} />
          <Form.InputNumber field="multiplier" label="计费倍率" min={0.001} max={99.999} precision={3} rules={[{ required: true, message: '必填' }]} placeholder="例如 1.2" style={{ width: '100%' }} />
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 24 }}>
            <Button onClick={closeDrawer}>取消</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>{editPrice ? '更新' : '创建'}</Button>
          </div>
        </Form>
      </SideSheet>}
    </div>
  );
}
