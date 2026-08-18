import { useState, useEffect } from 'react';
import { Button, Modal, Form, Toast, Space, SideSheet, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, PagedTable, PageHeader, Surface } from '../ui';
import { customerContextBlockerCopy, effectiveCustomerOrgID, resolveCustomerContext } from '../utils/customerContext';
import { canonicalExactDecimal } from '../utils/exactDecimal';

export function parseOrgModelPriceMultiplier(value: unknown) {
  return canonicalExactDecimal(value, {
    decimalPlaces: 3,
    maxWholeDigits: 2,
    minScaled: 1n,
    maxScaled: 99999n,
    errorMessage: 'invalid organization price multiplier',
  });
}

function mutationError(prefix: string, error: unknown) {
  return error instanceof Error && error.message ? `${prefix}：${error.message}` : prefix;
}

export default function OrgModelPricesPage({ embedded = false }: { embedded?: boolean }) {
  const user = useAuthStore((s) => s.user);
  const canMutate = isPlatformAdminRole(organizationRoleForUser(user));
  const customerContext = resolveCustomerContext(user);
  const selectedOrgID = canMutate ? effectiveCustomerOrgID(user) : undefined;
  const customerContextReady = !canMutate || customerContext.state === 'ready';
  const [prices, setPrices] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);
  const [showDrawer, setShowDrawer] = useState(false);
  const [editPrice, setEditPrice] = useState<any>(null);
  const [formApi, setFormApi] = useState<any>(null);
  const [modelNames, setModelNames] = useState<string[]>([]);

  const load = (p = page, size = pageSize, orgID = selectedOrgID) => {
    setLoading(true);
    api.getOrgModelPrices(p, size, orgID)
      .then((d: any) => { setPrices(d.data || d.items || []); setTotal(d.total || 0); })
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    if (!customerContextReady) return;
    load(1, pageSize, selectedOrgID);
    api.getModelNames().then((d: any) => setModelNames(d.data || [])).catch(() => {});
  }, [customerContextReady, selectedOrgID]);

  const closeDrawer = () => { setShowDrawer(false); setEditPrice(null); };

  const handleCreate = async (v: any) => {
    const orgID = Number(selectedOrgID);
    if (!Number.isSafeInteger(orgID) || orgID <= 0) {
      Toast.error('无法确认唯一客户组织，未创建倍率');
      return;
    }
    try {
      await api.createOrgModelPrice({
        org_id: orgID,
        model_name: v.model_name,
		multiplier: parseOrgModelPriceMultiplier(v.multiplier),
      });
      Toast.success('创建成功');
      closeDrawer();
      load(1, pageSize, orgID);
    } catch (error) { Toast.error(mutationError('创建失败', error)); }
  };

  const handleUpdate = async (v: any) => {
    if (!editPrice) return;
    try {
      await api.updateOrgModelPrice(editPrice.id, {
		multiplier: parseOrgModelPriceMultiplier(v.multiplier),
      });
      Toast.success('更新成功');
      closeDrawer();
      load();
    } catch (error) { Toast.error(mutationError('更新失败', error)); }
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
  };

  const openEdit = (r: any) => {
    setEditPrice(r);
    setShowDrawer(true);
    setTimeout(() => {
      formApi?.setValues({
        model_name: r.model_name,
		multiplier: String(r.multiplier),
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
	{ title: '计费倍率', dataIndex: 'multiplier', render: (v: unknown) => {
		try { return `${parseOrgModelPriceMultiplier(v)}×`; } catch { return '-'; }
	} },
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

  if (!customerContextReady) {
    const blocker = customerContextBlockerCopy(customerContext.state as Exclude<typeof customerContext.state, 'ready'>);
    return (
      <div className={embedded ? undefined : 'ur-page'}>
        {!embedded && <PageHeader eyebrow="成本与财务" title={blocker.title} description="客户倍率必须绑定唯一客户组织。" />}
        <Surface><EmptyState title={blocker.title} description={blocker.description} minHeight={240} /></Surface>
      </div>
    );
  }

  return (
    <div className={embedded ? undefined : 'ur-page'}>
      {!embedded && <PageHeader
        eyebrow="成本与财务"
        title="组织计费倍率"
        description={canMutate
          ? '按组织设置基础模型价格的乘数；例如 1.2× 表示按基础价格的 1.2 倍计费。'
          : '只读查看本组织适用的计费倍率；倍率由平台管理员统一维护。'}
        actions={canMutate ? <Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增倍率</Button> : undefined}
      />}

      <Surface
        title={embedded ? '组织倍率' : undefined}
        description={embedded ? (canMutate
          ? '为目标组织设置基础价格乘数；未配置时使用 1.0×。'
          : '只读查看本组织适用的价格乘数。') : undefined}
        actions={embedded && canMutate ? <Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增倍率</Button> : undefined}
      >
        {canMutate && <Typography.Text type="tertiary">客户范围：{customerContext.orgName || `组织 #${selectedOrgID}`}</Typography.Text>}
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
          <Typography.Paragraph type="tertiary">客户范围：{customerContext.orgName || `组织 #${selectedOrgID}`}</Typography.Paragraph>
          <Form.Select field="model_name" aria-label="组织计价模型" label="模型名称" rules={[{ required: true, message: '请选择模型' }]} filter optionList={modelNames.map(m => ({ label: m, value: m }))} placeholder="选择或搜索模型" disabled={!!editPrice} />
		  <Form.Input field="multiplier" label="计费倍率" rules={[{ required: true, message: '必填' }]} placeholder="例如 1.2" />
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 24 }}>
            <Button onClick={closeDrawer}>取消</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>{editPrice ? '更新' : '创建'}</Button>
          </div>
        </Form>
      </SideSheet>}
    </div>
  );
}
