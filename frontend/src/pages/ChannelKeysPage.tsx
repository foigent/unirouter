import { useEffect, useMemo, useRef, useState } from 'react';
import { Banner, Button, Modal, Form, Toast, Space, SideSheet } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState, PagedTable, PageHeader, PasswordField, StatusTag, Surface } from '../ui';
import { formatDateTime } from '../utils/formatDate';
import {
  channelStatusLabel,
  credentialModelCoverage,
  evaluateCredentialCombination,
  isCredentialCoolingDown,
  type ChannelReadinessChannel,
  type CredentialCombinationState,
} from '../utils/channelReadiness';
import { normalizeModelList } from '../utils/modelList';
import ModelConnectionBackButton from '../components/ModelConnectionBackButton';

interface ChannelKey {
  id: number;
  channel_id: number;
  channel_name: string;
  channel_status: string;
  key_prefix: string;
  allowed_models: string;
  status: string;
  fail_count: number;
  last_used_at?: string | null;
  cooldown_until?: string | null;
  created_at: string;
}

const combinationStatus: Record<CredentialCombinationState, string> = {
  credential_disabled: 'disabled',
  cooling_down: 'warning',
  model_mismatch: 'failed',
  route_candidate: 'success',
  awaiting_release: 'warning',
  channel_disabled: 'disabled',
  unknown_channel: 'warning',
};

const combinationLabel: Record<CredentialCombinationState, string> = {
  credential_disabled: '凭证停用',
  cooling_down: '凭证冷却中',
  model_mismatch: '阻断：模型无交集',
  route_candidate: '可进入路由候选',
  awaiting_release: '凭证就绪·待发布',
  channel_disabled: '凭证保留·通道禁用',
  unknown_channel: '通道状态待确认',
};

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
  const [channels, setChannels] = useState<Array<ChannelReadinessChannel & { label: string; value: number }>>([]);

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
        const list = (d.data || d.channels || []).map((c: any) => ({
          id: c.id,
          name: c.name,
          label: `${c.name} · ${channelStatusLabel(c.status)}`,
          value: c.id,
          status: c.status,
          models: c.models || '',
        }));
        setChannels(list);
      })
      .catch(() => {});
  };

  useEffect(() => { load(1); loadChannels(); }, []);

  const closeDrawer = () => { setShowDrawer(false); setEditing(null); };

  const handleSubmit = async (values: Record<string, any>) => {
    try {
      if (editing) {
        const activeParent = editing.channel_status === 'active';
        const payload: Record<string, any> = activeParent ? { status: values.status } : {
          channel_id: parseInt(values.channel_id),
          allowed_models: normalizeModelList(values.allowed_models),
          status: values.status,
        };
        if (!activeParent && String(values.api_key || '').trim()) payload.key = String(values.api_key).trim();
        await api.updateChannelKey(editing.id, payload);
        Toast.success('更新成功');
      } else {
        await api.createChannelKey({
          channel_id: parseInt(values.channel_id),
          key: String(values.api_key || '').trim(),
          allowed_models: normalizeModelList(values.allowed_models),
        });
        Toast.success('创建成功');
      }
      closeDrawer();
      load();
    } catch (error: any) {
			Toast.error(`${editing ? '更新' : '创建'}失败${error?.message ? `：${error.message}` : ''}`);
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

  const channelByID = useMemo(
    () => new Map(channels.map(channel => [channel.id, channel])),
    [channels],
  );
  const enabledOnInactiveChannelCount = keys.filter(key => (
    key.status === 'active' && key.channel_status !== 'active'
  )).length;
  const modelMismatchCount = keys.filter(key => {
    const channel = channelByID.get(key.channel_id);
    return key.status === 'active'
      && Boolean(channel?.models)
      && credentialModelCoverage(channel?.models, key.allowed_models).kind === 'none';
  }).length;

  const recordChannel = (record: ChannelKey): ChannelReadinessChannel => channelByID.get(record.channel_id) || {
    id: record.channel_id,
    name: record.channel_name,
    status: record.channel_status,
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    {
      title: '所属通道', width: 150,
      render: (_: unknown, record: ChannelKey) => (
        <div style={{ display: 'grid', gap: 4 }}>
          <span>{record.channel_name || '-'}</span>
          <StatusTag status={record.channel_status}>{`通道${channelStatusLabel(record.channel_status)}`}</StatusTag>
        </div>
      ),
    },
    { title: '凭证标识', dataIndex: 'key_prefix', render: (v: string) => <code>{v}</code> },
    {
      title: '凭证状态', dataIndex: 'status', width: 100,
      render: (v: string) => <StatusTag status={v}>{v === 'active' ? '凭证启用' : v === 'disabled' ? '凭证停用' : v}</StatusTag>,
    },
    {
      title: '模型覆盖', width: 210,
      render: (_: unknown, record: ChannelKey) => {
        const coverage = credentialModelCoverage(recordChannel(record).models, record.allowed_models);
        const label = coverage.kind === 'all'
          ? `全部通道模型（${coverage.matching.length}）`
          : coverage.kind === 'partial'
            ? `相交 ${coverage.matching.length}/${coverage.channelModels.length}`
            : coverage.kind === 'none'
              ? '模型范围无交集'
              : '通道模型未配置或待加载';
        return (
          <div style={{ display: 'grid', gap: 4 }}>
            <span>{record.allowed_models || '全部'}</span>
            <StatusTag status={coverage.kind === 'none' ? 'failed' : coverage.kind === 'channel_unconfigured' ? 'warning' : 'success'}>{label}</StatusTag>
          </div>
        );
      },
    },
    {
      title: '组合状态', width: 170,
      render: (_: unknown, record: ChannelKey) => {
        const state = evaluateCredentialCombination(record, recordChannel(record));
        return <StatusTag status={combinationStatus[state]}>{combinationLabel[state]}</StatusTag>;
      },
    },
    {
      title: '失败 / 冷却', width: 180,
      render: (_: unknown, record: ChannelKey) => isCredentialCoolingDown(record)
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
          <Button aria-label={`删除供应商凭证 ${record.channel_name}`} icon={<IconDelete />} size="small" type="danger" disabled={record.channel_status === 'active'} onClick={() => handleDelete(record.id)} />
        </Space>
      ),
    },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="模型接入"
        title="供应商凭证"
        description="凭证状态、所属通道状态和模型覆盖是三个独立条件；凭证启用不代表通道已经生效。凭证标识来自加密存储记录，并非供应商 Key 前缀。"
        actions={<Space>
          <ModelConnectionBackButton />
          <Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增供应商凭证</Button>
        </Space>}
      />

      <Banner
        type={(enabledOnInactiveChannelCount > 0 || modelMismatchCount > 0) ? 'warning' : 'info'}
        description={`只有“通道生效 + 凭证启用且未冷却 + 模型范围相交”才可进入路由候选。当前页 ${enabledOnInactiveChannelCount} 条启用凭证绑定待发布/禁用通道，${modelMismatchCount} 条凭证与通道模型范围无交集。`}
        closeIcon={null}
        style={{ marginBottom: 16 }}
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
        {editing?.channel_status === 'active' && <div className="ur-playground-error" style={{ marginBottom: 16 }}><strong>当前通道正在生效</strong><span>为防止绕过发布门禁，此处只允许紧急停用。轮换凭证请先把通道改为 staged。</span></div>}
        <Form getFormApi={(api: any) => setFormApi(api)} onSubmit={handleSubmit}>
          <Form.Select field="channel_id" label="所属通道" rules={[{ required: true, message: '必填' }]}
            optionList={editing ? channels : channels.filter(channel => channel.status !== 'active')} disabled={editing?.channel_status === 'active'} />
			<PasswordField
				field="api_key"
				label="供应商 API Key"
				rules={editing ? [] : [{ required: true, message: '必填' }]}
				placeholder={editing ? '留空表示不更换现有凭证' : 'sk-...'}
				extraText={editing ? '已有凭证不会回显；只有需要轮换时才输入新的完整凭证。' : undefined}
				autoComplete="new-password"
				visibilityResetKey={editing?.id || 'new'}
				disabled={editing?.channel_status === 'active'}
			/>
          <Form.Input
            field="allowed_models"
            label="允许模型（留空=全部）"
            placeholder="deepseek-chat,deepseek-reasoner"
            extraText="必须使用精确模型标识；可用中英文逗号、分号或换行分隔，保存时会去空格、去重并统一为英文逗号。"
				disabled={editing?.channel_status === 'active'}
          />
          {editing && <Form.Select
            field="status"
            label="状态"
            optionList={editing.channel_status === 'active' ? [{ label: '紧急停用', value: 'disabled' }] : [{ label: '启用（随 staged 通道发布）', value: 'active' }, { label: '停用', value: 'disabled' }]}
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
