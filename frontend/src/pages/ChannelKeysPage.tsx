import { useEffect, useRef, useState } from 'react';
import { Button, Modal, Form, Toast, Space, SideSheet } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState, PagedTable, PageHeader, PasswordField, StatusTag, Surface } from '../ui';
import { formatDateTime } from '../utils/formatDate';

interface ChannelKey {
  id: number;
  channel_id: number;
  channel_name: string;
  key_prefix: string;
  allowed_models: string;
  status: string;
  fail_count: number;
  last_used_at?: string | null;
  cooldown_until?: string | null;
  created_at: string;
}

function normalizeAllowedModels(value: unknown) {
  return String(value || '')
    .split(/[,，\n]/)
    .map(model => model.trim())
    .filter(Boolean)
    .filter((model, index, models) => models.indexOf(model) === index)
    .join(',');
}

export default function ChannelKeysPage() {
  const [keys, setKeys] = useState<ChannelKey[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const requestVersion = useRef(0);
  const [showDrawer, setShowDrawer] = useState(false);
  const [editing, setEditing] = useState<ChannelKey | null>(null);
  const [formApi, setFormApi] = useState<any>(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [total, setTotal] = useState(0);
  const [channels, setChannels] = useState<any[]>([]);

  const load = (p = page, size = pageSize) => {
    const version = ++requestVersion.current;
    setLoading(true);
    setLoadError('');
    setKeys([]);
    setTotal(0);
    api.getChannelKeys(p, size)
      .then((d: any) => {
        if (version !== requestVersion.current) return;
        setKeys(d.data || []);
        setTotal(d.total || 0);
      })
      .catch(() => {
        if (version === requestVersion.current) setLoadError('无法加载供应商凭证列表。');
      })
      .finally(() => {
        if (version === requestVersion.current) setLoading(false);
      });
  };

  const loadChannels = () => {
    api.getChannels(1, 100)
      .then((d: any) => {
        const list = (d.data || d.channels || []).map((c: any) => ({ label: c.name, value: c.id }));
        setChannels(list);
      })
      .catch(() => {});
  };

  useEffect(() => { load(1); loadChannels(); }, []);

  const closeDrawer = () => { setShowDrawer(false); setEditing(null); };

  const handleSubmit = async (values: Record<string, any>) => {
    try {
      if (editing) {
        const payload: Record<string, any> = {
          channel_id: parseInt(values.channel_id),
          allowed_models: normalizeAllowedModels(values.allowed_models),
          status: values.status,
        };
        if (String(values.api_key || '').trim()) payload.key = String(values.api_key).trim();
        await api.updateChannelKey(editing.id, payload);
        Toast.success('更新成功');
      } else {
        await api.createChannelKey({
          channel_id: parseInt(values.channel_id),
          key: String(values.api_key || '').trim(),
          allowed_models: normalizeAllowedModels(values.allowed_models),
        });
        Toast.success('创建成功');
      }
      closeDrawer();
      load();
    } catch {
      Toast.error(editing ? '更新失败' : '创建失败');
    }
  };

  const handleDelete = async (id: number) => {
    Modal.confirm({
      title: '确认删除',
      content: '删除后该密钥将无法使用，确定吗？',
      onOk: async () => {
        try {
          await api.deleteChannelKey(id);
          Toast.success('已删除');
          load();
        } catch { Toast.error('删除失败'); }
      },
    });
  };

  const openEdit = (record: ChannelKey) => {
    setEditing(record);
    setShowDrawer(true);
  };

  const openCreate = () => {
    setEditing(null);
    setShowDrawer(true);
  };

  useEffect(() => {
    if (!showDrawer || !formApi) return;
    formApi.reset();
    if (editing) {
      formApi.setValues({
        channel_id: editing.channel_id,
        api_key: '',
        allowed_models: editing.allowed_models || '',
        status: editing.status,
      });
    }
  }, [editing, formApi, showDrawer]);

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '通道', dataIndex: 'channel_name', render: (v: string) => v || '-' },
    { title: '凭证标识', dataIndex: 'key_prefix', render: (v: string) => <code>{v}</code> },
    { title: '允许模型', dataIndex: 'allowed_models', render: (v: string) => v || '全部' },
    {
      title: '状态', dataIndex: 'status', width: 80,
      render: (v: string) => <StatusTag status={v}>{v === 'active' ? '启用' : v === 'disabled' ? '停用' : v}</StatusTag>,
    },
    {
      title: '失败 / 冷却', width: 180,
      render: (_: unknown, record: ChannelKey) => record.cooldown_until
        ? <StatusTag status="warning">失败 {record.fail_count || 0} 次 · 冷却至 {formatDateTime(record.cooldown_until)}</StatusTag>
        : `失败 ${record.fail_count || 0} 次`,
    },
    { title: '最近使用', dataIndex: 'last_used_at', width: 170, render: (v: string | null) => v ? formatDateTime(v) : '尚未使用' },
    { title: '创建时间', dataIndex: 'created_at', width: 170, render: (v: string) => formatDateTime(v) },
    {
      title: '操作', width: 120,
      render: (_: any, record: ChannelKey) => (
        <Space>
          <Button aria-label={`编辑供应商凭证 ${record.channel_name}`} icon={<IconEdit />} size="small" onClick={() => openEdit(record)} />
          <Button aria-label={`删除供应商凭证 ${record.channel_name}`} icon={<IconDelete />} size="small" type="danger" onClick={() => handleDelete(record.id)} />
        </Space>
      ),
    },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="模型接入"
        title="供应商凭证"
        description="管理上游通道使用的供应商访问凭证、精确模型白名单和运行状态。凭证标识来自加密存储记录，只用于区分记录，并非供应商 Key 前缀。"
        actions={<Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增供应商凭证</Button>}
      />

      <Surface>
        <PagedTable
          columns={columns}
          dataSource={keys}
          loading={loading}
          rowKey="id"
          empty={loadError ? (
            <EmptyState title="供应商凭证加载失败" description={loadError} actions={<Button type="primary" onClick={() => load()}>重新加载供应商凭证</Button>} minHeight={220} />
          ) : <EmptyState title="暂无供应商凭证" minHeight={220} />}
          page={page}
          pageSize={pageSize}
          total={total}
          pageSizeOpts={[20, 50, 100]}
          showSizeChanger
          onPageChange={(p: number) => { setPage(p); load(p, pageSize); }}
          onPageSizeChange={(s: number) => { setPageSize(s); setPage(1); load(1, s); }}
        />
      </Surface>

      <SideSheet
        title={editing ? '编辑供应商凭证' : '新增供应商凭证'}
        visible={showDrawer}
        onCancel={closeDrawer}
        width={480}
      >
        <Form getFormApi={(api: any) => setFormApi(api)} onSubmit={handleSubmit}>
          <Form.Select field="channel_id" label="所属通道" rules={[{ required: true, message: '必填' }]}
            optionList={channels} />
			<PasswordField
				field="api_key"
				label="供应商 API Key"
				rules={editing ? [] : [{ required: true, message: '必填' }]}
				placeholder={editing ? '留空表示不更换现有凭证' : 'sk-...'}
				extraText={editing ? '已有凭证不会回显；只有需要轮换时才输入新的完整凭证。' : undefined}
				autoComplete="new-password"
				visibilityResetKey={editing?.id || 'new'}
			/>
          <Form.Input
            field="allowed_models"
            label="允许模型（逗号分隔，留空=全部）"
            placeholder="deepseek-chat,deepseek-reasoner"
            extraText="必须使用模型定价/模型治理中的精确模型标识；中文逗号会在保存时规范为英文逗号。"
          />
          {editing && <Form.Select
            field="status"
            label="状态"
            optionList={[{ label: '启用', value: 'active' }, { label: '停用', value: 'disabled' }]}
            rules={[{ required: true, message: '必填' }]}
          />}
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 24 }}>
            <Button onClick={closeDrawer}>取消</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>{editing ? '更新' : '创建'}</Button>
          </div>
        </Form>
      </SideSheet>
    </div>
  );
}
