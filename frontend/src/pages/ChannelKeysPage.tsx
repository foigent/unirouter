import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Banner, Button, Modal, Form, Toast, Space, SideSheet, Typography } from '@douyinfe/semi-ui';
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
  updated_at?: string;
}

type ChannelOption = ChannelReadinessChannel & {
  label: string;
  value: number;
  base_status: string;
  disabled?: boolean;
};

function credentialMutationErrorMessage(error: unknown, action: '创建' | '保存') {
  const raw = error instanceof Error ? error.message : '';
  if (raw.toLowerCase().includes('channel must be staged or disabled')) {
    return `${action}失败：该模型连接仍在生效。请返回“模型连接”先执行紧急禁用，再新增或轮换供应商凭证。`;
  }
  return `${action}失败${raw ? `：${raw}` : ''}`;
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

export default function ChannelKeysPage({ embedded = false }: { embedded?: boolean } = {}) {
  const navigate = useNavigate();
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
  const [channels, setChannels] = useState<ChannelOption[]>([]);
  const [saveNotice, setSaveNotice] = useState('');
  const [submitError, setSubmitError] = useState('');
  const [submitting, setSubmitting] = useState(false);

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
        const list = (d.data || d.channels || []).map((c: any) => {
          const effectiveStatus = c.effective_status || c.status;
          return {
            id: c.id,
            name: c.name,
            label: `${c.name} · ${channelStatusLabel(effectiveStatus)}${c.status === 'staged' && effectiveStatus === 'active' ? '（运行时发布）' : ''}`,
            value: c.id,
            base_status: c.status,
            status: effectiveStatus,
            models: c.models || '',
            disabled: effectiveStatus === 'active',
          };
        });
        setChannels(list);
      })
      .catch(() => {});
  };

  useEffect(() => { load(1); loadChannels(); }, []);

  const closeDrawer = () => { setShowDrawer(false); setEditing(null); setSubmitError(''); };

  const handleSubmit = async (values: Record<string, any>) => {
    setSubmitError('');
    setSubmitting(true);
    try {
      if (editing) {
        const activeParent = editing.channel_status === 'active';
		const keyChanged = !activeParent && Boolean(String(values.api_key || '').trim());
        const payload: Record<string, any> = activeParent ? { status: values.status } : {
          channel_id: parseInt(values.channel_id),
          allowed_models: normalizeModelList(values.allowed_models),
          status: values.status,
        };
		if (keyChanged) payload.key = String(values.api_key).trim();
        await api.updateChannelKey(editing.id, payload);
		setSaveNotice(keyChanged
		  ? `${editing.channel_name} 的新供应商 API Key 已加密保存；为安全起见不会回显明文。`
		  : `${editing.channel_name} 的供应商凭证设置已保存。`);
		Toast.success('供应商凭证已保存');
      } else {
        await api.createChannelKey({
          channel_id: parseInt(values.channel_id),
          key: String(values.api_key || '').trim(),
          allowed_models: normalizeModelList(values.allowed_models),
        });
		setSaveNotice('新的供应商凭证已加密保存；为安全起见不会回显明文。');
		Toast.success('供应商凭证已创建');
      }
      closeDrawer();
      load();
    } catch (error: any) {
      const message = credentialMutationErrorMessage(error, editing ? '保存' : '创建');
		setSubmitError(message);
		Toast.error(message);
	} finally {
		setSubmitting(false);
    }
  };

  const handleDelete = async (id: number) => {
    Modal.confirm({
      title: '确认删除',
      content: '这里只删除供应商凭证，不会删除模型连接或历史发布记录。删除后该 Key 将无法使用，确定吗？',
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
	setSubmitError('');
    setEditing({ ...record, channel_status: recordChannel(record).status });
    setShowDrawer(true);
  };

  const openCreate = () => {
	setSubmitError('');
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
  const recordChannel = (record: ChannelKey): ChannelOption => channelByID.get(record.channel_id) || {
    id: record.channel_id,
    name: record.channel_name,
    status: record.channel_status,
	base_status: record.channel_status,
	label: record.channel_name,
	value: record.channel_id,
  };
  const enabledOnInactiveChannelCount = keys.filter(key => (
    key.status === 'active' && recordChannel(key).status !== 'active'
  )).length;
  const modelMismatchCount = keys.filter(key => {
    const channel = channelByID.get(key.channel_id);
    return key.status === 'active'
      && Boolean(channel?.models)
      && credentialModelCoverage(channel?.models, key.allowed_models).kind === 'none';
  }).length;

  const activeConnections = channels.filter(channel => channel.status === 'active');

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    {
      title: '所属通道', width: 150,
		render: (_: unknown, record: ChannelKey) => {
			const channel = recordChannel(record);
			const runtimePublished = channel.base_status === 'staged' && channel.status === 'active';
			return <div style={{ display: 'grid', gap: 4 }}>
				<span>{record.channel_name || '-'}</span>
				<StatusTag status={channel.status}>{runtimePublished ? '运行时生效（配置待发布）' : `通道${channelStatusLabel(channel.status)}`}</StatusTag>
			</div>;
		},
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
		const channel = recordChannel(record);
        const state = evaluateCredentialCombination(record, channel);
		const label = state === 'route_candidate' && channel.base_status === 'staged'
			? '已进入路由（发布生效）'
			: combinationLabel[state];
		return <StatusTag status={combinationStatus[state]}>{label}</StatusTag>;
      },
    },
    {
      title: '失败 / 冷却', width: 180,
      render: (_: unknown, record: ChannelKey) => isCredentialCoolingDown(record)
        ? <StatusTag status="warning">失败 {record.fail_count || 0} 次 · 冷却至 {formatDateTime(record.cooldown_until)}</StatusTag>
        : `失败 ${record.fail_count || 0} 次`,
    },
    { title: '最近使用', dataIndex: 'last_used_at', width: 170, render: (v: string | null) => v ? formatDateTime(v) : '尚未使用' },
	{ title: '最近更新', dataIndex: 'updated_at', width: 170, render: (v: string, record: ChannelKey) => formatDateTime(v || record.created_at) },
    {
      title: '操作', width: 120,
      render: (_: any, record: ChannelKey) => (
        <Space>
          <Button aria-label={`编辑供应商凭证 ${record.channel_name}`} icon={<IconEdit />} size="small" onClick={() => openEdit(record)} />
          <Button aria-label={`删除供应商凭证 ${record.channel_name}`} icon={<IconDelete />} size="small" type="danger" disabled={recordChannel(record).status === 'active'} onClick={() => handleDelete(record.id)} />
        </Space>
      ),
    },
  ];

  return (
    <div className={embedded ? 'ur-embedded-page' : 'ur-page'}>
      {!embedded && <PageHeader
        eyebrow="模型接入"
        title="供应商凭证"
		description="凭证状态、所属通道状态和模型覆盖是三个独立条件；凭证启用不代表通道已经生效。明文 Key 永不回显，保存结果请以成功提示和“最近更新”为准。"
        actions={<Space>
          <ModelConnectionBackButton />
          <Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增供应商凭证</Button>
        </Space>}
      />}

      {embedded && <div className="ur-embedded-heading">
        <div><Typography.Title heading={5} style={{ margin: 0 }}>供应商凭证</Typography.Title><Typography.Text type="tertiary">明文 Key 永不回显；状态以当前端点、模型覆盖和冷却条件共同计算。</Typography.Text></div>
        <Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增供应商凭证</Button>
      </div>}

	  {saveNotice && <Banner
		type="success"
		description={saveNotice}
		closeIcon={null}
		style={{ marginBottom: 16 }}
	  />}

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
        {!editing && activeConnections.length > 0 && <Banner
          type="warning"
          description={`${activeConnections.map(channel => channel.name).join('、')} 正在生效，不能直接新增或轮换凭证。请先到模型连接执行紧急禁用。`}
          closeIcon={null}
          style={{ marginBottom: 16 }}
        />}
        {editing?.channel_status === 'active' && <div className="ur-playground-error" style={{ marginBottom: 16 }}><strong>当前通道正在生效</strong><span>为防止绕过发布门禁，此处只允许停用现有凭证。新增或轮换凭证请先到“模型连接”执行紧急禁用。</span></div>}
		{submitError && <Banner type="danger" description={submitError} closeIcon={null} style={{ marginBottom: 16 }} />}
        <Form getFormApi={(api: any) => setFormApi(api)} onSubmit={handleSubmit}>
          <Form.Select field="channel_id" label="所属通道" rules={[{ required: true, message: '必填' }]}
            optionList={channels} disabled={editing?.channel_status === 'active'} />
			<PasswordField
				field="api_key"
				label={editing ? '新的供应商 API Key（可选）' : '供应商 API Key'}
				rules={editing ? [] : [{ required: true, message: '必填' }]}
				placeholder={editing ? '留空保留当前 Key' : 'sk-...'}
				extraText={editing ? '输入完整新 Key 后点击“保存修改”。保存成功后仍会置空，因为明文永不回显。' : undefined}
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
            {!editing && activeConnections.length > 0 && <Button onClick={() => navigate(`/channels?channel_id=${activeConnections[0].id}`)}>前往紧急禁用</Button>}
            <Button onClick={closeDrawer}>取消</Button>
			<Button type="primary" loading={submitting} disabled={submitting} onClick={() => formApi?.submitForm()}>{editing ? '保存修改' : '创建'}</Button>
          </div>
        </Form>
      </SideSheet>
    </div>
  );
}
