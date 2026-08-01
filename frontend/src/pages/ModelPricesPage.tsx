import { useEffect, useRef, useState } from 'react';
import { Button, Modal, Form, Input, Toast, Space, SideSheet } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { isPlatformAdminRole } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, PagedTable, PageHeader, Surface } from '../ui';
import { canonicalExactDecimal, groupExactDecimal, padExactDecimal } from '../utils/exactDecimal';

export function parseCNYPerMillionPrice(value: unknown) {
	return canonicalExactDecimal(value, {
		decimalPlaces: 5,
		maxWholeDigits: 13,
		maxScaled: 922337203685477580n,
		errorMessage: 'invalid CNY-per-million price',
	});
}

export function parseCachePriceRatio(value: unknown) {
	return canonicalExactDecimal(value, {
		decimalPlaces: 6,
		maxWholeDigits: 3,
		maxScaled: 100000000n,
		errorMessage: 'invalid cache price ratio',
	});
}

function formatCNYPerMillionPrice(value: unknown) {
	try {
		return `¥${groupExactDecimal(parseCNYPerMillionPrice(value))}`;
	} catch {
		return '-';
	}
}

function formatCachePriceRatio(value: unknown) {
	try {
		return `${padExactDecimal(parseCachePriceRatio(value ?? '1'), 3)}×`;
	} catch {
		return '-';
	}
}

function mutationError(prefix: string, error: unknown) {
	return error instanceof Error && error.message ? `${prefix}：${error.message}` : prefix;
}

export default function ModelPricesPage({ embedded = false }: { embedded?: boolean }) {
  const role = useAuthStore((s) => s.user?.role);
  const canMutate = isPlatformAdminRole(role);
  const [prices, setPrices] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const requestVersion = useRef(0);
  const [showDrawer, setShowDrawer] = useState(false);
  const [editPrice, setEditPrice] = useState<any>(null);
  const [formApi, setFormApi] = useState<any>(null);

  const load = (p = page, size = pageSize) => {
    const version = ++requestVersion.current;
    setLoading(true);
    setLoadError('');
    setPrices([]);
    setTotal(0);
    api.getModelPriceList(p, size)
      .then((d: any) => {
        if (version !== requestVersion.current) return;
        setPrices(d.data || []);
        setTotal(d.total || 0);
      })
      .catch(() => {
        if (version === requestVersion.current) setLoadError('无法加载模型价格列表。');
      })
      .finally(() => {
        if (version === requestVersion.current) setLoading(false);
      });
  };

  useEffect(() => { load(1); }, []);

  const closeDrawer = () => { setShowDrawer(false); setEditPrice(null); };

  const handleCreate = async (v: any) => {
    try {
      await api.createModelPrice({
        model_name: String(v.model_name || '').trim(),
        input_price_cny_per_million_tokens: parseCNYPerMillionPrice(v.input_price),
        output_price_cny_per_million_tokens: parseCNYPerMillionPrice(v.output_price),
		cache_read_ratio: parseCachePriceRatio(v.cache_read_ratio ?? '1'),
		cache_creation_ratio: parseCachePriceRatio(v.cache_creation_ratio ?? '1'),
      });
      Toast.success('价格创建成功');
      closeDrawer();
      load(1);
    } catch (error) { Toast.error(mutationError('创建失败', error)); }
  };

  const handleUpdate = async (v: any) => {
    if (!editPrice) return;
    try {
      await api.updateModelPrice(editPrice.id, {
        input_price_cny_per_million_tokens: parseCNYPerMillionPrice(v.input_price),
        output_price_cny_per_million_tokens: parseCNYPerMillionPrice(v.output_price),
		cache_read_ratio: parseCachePriceRatio(v.cache_read_ratio ?? '1'),
		cache_creation_ratio: parseCachePriceRatio(v.cache_creation_ratio ?? '1'),
      });
      Toast.success('更新成功');
      closeDrawer();
      load();
    } catch (error) { Toast.error(mutationError('更新失败', error)); }
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
  };

  const columns: any[] = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '模型名称', dataIndex: 'model_name' },
    { title: '输入价格（CNY/百万 tokens）', dataIndex: 'input_price_cny_per_million_tokens', render: formatCNYPerMillionPrice },
    { title: '输出价格（CNY/百万 tokens）', dataIndex: 'output_price_cny_per_million_tokens', render: formatCNYPerMillionPrice },
	{ title: '缓存读取倍率', dataIndex: 'cache_read_ratio', width: 120, render: formatCachePriceRatio },
	{ title: '缓存写入倍率', dataIndex: 'cache_creation_ratio', width: 120, render: formatCachePriceRatio },
    { title: '更新时间', dataIndex: 'updated_at', render: (v: string) => v || '-' },
  ];
  if (canMutate) columns.push({
      title: '操作', width: 120,
      render: (_: any, record: any) => (
        <Space>
          <Button aria-label="编辑价格" icon={<IconEdit />} size="small" onClick={() => openEdit(record)} />
          <Button aria-label="删除价格" icon={<IconDelete />} size="small" type="danger" onClick={() => handleDelete(record.id)} />
        </Space>
      ),
    });

  return (
    <div className={embedded ? undefined : 'ur-page'}>
      {!embedded && <PageHeader
        eyebrow="成本与财务"
        title="模型定价"
        description={canMutate
          ? '按 CNY/百万 tokens 维护明确的基础价格和供应商 Prompt Cache 倍率；未配置价格的模型拒绝计费与路由。'
          : '只读查看平台统一维护的 CNY/百万 tokens 基础价格与供应商 Prompt Cache 倍率。'}
        actions={canMutate ? <Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增价格</Button> : undefined}
      />}

      <Surface
        title={embedded ? '基础价格' : undefined}
        description={embedded ? (canMutate
          ? '维护平台统一的 CNY/百万 tokens 基础价格与 Prompt Cache 倍率；未配置价格的模型拒绝计费与路由。'
          : '只读查看平台统一维护的基础价格与 Prompt Cache 倍率。') : undefined}
        actions={embedded && canMutate ? <Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增价格</Button> : undefined}
      >
        <PagedTable
          columns={columns}
          dataSource={prices}
          loading={loading}
          rowKey="id"
          empty={loadError ? (
            <EmptyState title="模型价格加载失败" description={loadError} actions={<Button type="primary" onClick={() => load()}>重新加载模型价格</Button>} minHeight={220} />
          ) : <EmptyState title="暂无模型价格" minHeight={220} />}
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
        <Form
          key={editPrice ? `edit-${editPrice.id}` : 'create'}
          getFormApi={(api: any) => setFormApi(api)}
          onSubmit={editPrice ? handleUpdate : handleCreate}
          initValues={editPrice ? {
            input_price: String(editPrice.input_price_cny_per_million_tokens),
            output_price: String(editPrice.output_price_cny_per_million_tokens),
            cache_read_ratio: String(editPrice.cache_read_ratio ?? '1'),
            cache_creation_ratio: String(editPrice.cache_creation_ratio ?? '1'),
          } : { cache_read_ratio: '1', cache_creation_ratio: '1' }}
        >
          {editPrice ? <div style={{ marginBottom: 12 }}>
            <div style={{ marginBottom: 4, color: 'var(--semi-color-text-0)', fontSize: 14 }}>模型名称</div>
            <Input aria-label="模型名称" disabled value={String(editPrice.model_name || '')} />
            <div style={{ marginTop: 4, color: 'var(--semi-color-text-2)', fontSize: 12, lineHeight: 1.7 }}>
              模型名称是计费、组织倍率、路由和模型治理共同使用的稳定标识，创建后不可修改。如需更名，请新建价格记录并单独迁移相关引用。
            </div>
          </div> : <Form.Input field="model_name" label="模型名称" rules={[{ required: true, message: '请输入模型名称' }]} placeholder="例如 gpt-4o" />}
          <Form.Input field="input_price" label="输入价格（CNY/百万 tokens）" rules={[{ required: true, message: '必填' }]} placeholder="例如 18.00000" />
          <Form.Input field="output_price" label="输出价格（CNY/百万 tokens）" rules={[{ required: true, message: '必填' }]} placeholder="例如 72.00000" />
		  <Form.Input field="cache_read_ratio" label="缓存读取倍率" rules={[{ required: true, message: '必填' }]} placeholder="0.1" />
		  <Form.Input field="cache_creation_ratio" label="缓存写入倍率" rules={[{ required: true, message: '必填' }]} placeholder="1.25" />
          <div style={{ marginTop: 8, color: 'var(--semi-color-text-2)', fontSize: 12, lineHeight: 1.7 }}>
            价格统一使用人民币/百万 tokens，最多 5 位小数，不接受单位缩写、尾随字符或科学计数法。倍率以普通输入价格为基准：0.1 表示按 10% 计费，1.25 表示按 125% 计费；仅供应商明确回传的 Prompt Cache Token 使用倍率，不是响应缓存。
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
