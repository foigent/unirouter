import { useCallback, useEffect, useMemo, useState } from 'react';
import { Banner, Button, Form, Popconfirm, SideSheet, Space, Table, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState } from '../ui';
import { loadAllPages } from '../utils/loadAllPages';
import { parseModelList } from '../utils/modelList';

interface Channel {
  id: number;
  name: string;
  provider: string;
  models: string;
  status: string;
  priority: number;
  routing_weight: number;
  routing_epoch: number;
}

interface ChannelOverride {
  channel_id: number;
  expected_routing_epoch: number;
  admission: string;
  priority?: number;
  weight?: number;
}

interface RuntimeRelease {
  id: number;
  release_ref: string;
  model_name: string;
  candidate_hash: string;
  baseline_hash: string;
  candidate: { contract_version: string; model_name: string; overrides: ChannelOverride[] };
  provider_pack_pins: Array<{ pack_id: string; pack_version: string; manifest_sha256: string }>;
  simulation_report: {
    report_version: string;
    overall_risk: string;
    summary?: { unchanged: number; compatible: number; review_required: number; breaking: number };
    route_changes?: Array<{ field: string; baseline: string; candidate: string; risk: string; reason: string }>;
  };
  overall_risk: string;
  stage: string;
  version: number;
  canary_basis_points: number;
  reason: string;
  created_at: string;
  updated_at: string;
}

const stageLabels: Record<string, string> = {
  draft: '草稿', validated: '已验证', shadow: '待灰度（零流量）', canary: '灰度', active: '已生效', rolled_back: '已回滚', rejected: '已拒绝',
};

const riskLabels: Record<string, string> = {
  unchanged: '无变化', compatible: '兼容', review_required: '需复核', breaking: '阻断',
};

function stageColor(stage: string) {
  if (stage === 'active') return 'green';
  if (stage === 'canary') return 'blue';
  if (stage === 'rolled_back' || stage === 'rejected') return 'grey';
  return 'orange';
}

function riskColor(risk: string) {
  if (risk === 'breaking') return 'red';
  if (risk === 'review_required') return 'orange';
  return risk === 'compatible' ? 'blue' : 'green';
}

export default function RuntimeChangeControlWorkspace() {
  const [releases, setReleases] = useState<RuntimeRelease[]>([]);
  const [channels, setChannels] = useState<Channel[]>([]);
  const [selectedRef, setSelectedRef] = useState('');
  const [loading, setLoading] = useState(false);
  const [transitioning, setTransitioning] = useState(false);
  const [loadError, setLoadError] = useState('');
  const [sheetOpen, setSheetOpen] = useState(false);
  const [formApi, setFormApi] = useState<any>(null);
  const [draftChannelID, setDraftChannelID] = useState<number>();

  const selected = releases.find(item => item.release_ref === selectedRef) || releases[0] || null;

  const load = useCallback(async () => {
    setLoading(true);
    setLoadError('');
    try {
      const [releaseData, channelData] = await Promise.all([
        api.getRuntimeChanges(),
        loadAllPages<Channel>((page, size) => api.getChannels(page, size)),
      ]);
      const next = (releaseData?.data || []) as RuntimeRelease[];
      setReleases(next);
      setChannels(channelData as Channel[]);
      setSelectedRef(current => next.some(item => item.release_ref === current) ? current : next[0]?.release_ref || '');
    } catch (error: any) {
      setLoadError(error?.message || '无法加载运行时发布状态。');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  const stats = useMemo(() => ({
    active: releases.filter(item => item.stage === 'active').length,
    canary: releases.filter(item => item.stage === 'canary').length,
    pending: releases.filter(item => ['draft', 'validated', 'shadow'].includes(item.stage)).length,
  }), [releases]);

  const releaseChannels = useMemo(
    () => channels.filter(channel => ['staged', 'active'].includes(channel.status) && parseModelList(channel.models).length > 0),
    [channels],
  );
  const excludedChannelCount = channels.length - releaseChannels.length;
  const draftChannel = releaseChannels.find(channel => channel.id === draftChannelID);
  const draftModelOptions = draftChannel ? parseModelList(draftChannel.models) : [];

  const createRelease = async (values: Record<string, any>) => {
    const channel = releaseChannels.find(item => item.id === Number(values.channel_id));
    if (!channel) {
      Toast.error('请选择当前可发布的通道快照');
      return;
    }
    const modelName = String(values.model_name || '').trim();
    if (!parseModelList(channel.models).includes(modelName)) {
      Toast.error('目标模型必须来自所选通道当前声明的模型列表');
      return;
    }
    const priority = values.priority === undefined || values.priority === null || values.priority === '' ? undefined : Number(values.priority);
    const weight = values.weight === undefined || values.weight === null || values.weight === '' ? undefined : Number(values.weight);
    const admission = String(values.admission || 'inherit');
    if (admission === 'inherit' && priority === undefined && weight === undefined) {
      Toast.error('至少选择准入变化，或填写优先级/权重');
      return;
    }
    setTransitioning(true);
    try {
      const created = await api.createRuntimeChange({
        model_name: modelName,
        reason: String(values.reason || '').trim(),
        overrides: [{
          channel_id: channel.id,
          expected_routing_epoch: channel.routing_epoch,
          admission,
          ...(priority !== undefined ? { priority } : {}),
          ...(weight !== undefined ? { weight } : {}),
        }],
      }) as RuntimeRelease;
      Toast.success('发布草稿已创建并生成确定性模拟证据');
      setSheetOpen(false);
      setDraftChannelID(undefined);
      formApi?.reset();
      await load();
      setSelectedRef(created.release_ref);
    } catch (error: any) {
      Toast.error(`创建失败${error?.message ? `：${error.message}` : ''}`);
    } finally {
      setTransitioning(false);
    }
  };

  const transition = async (label: string, operation: () => Promise<any>) => {
    setTransitioning(true);
    try {
      const updated = await operation() as RuntimeRelease;
      Toast.success(label);
      await load();
      setSelectedRef(updated.release_ref);
    } catch (error: any) {
      Toast.error(`${label}失败${error?.message ? `：${error.message}` : ''}`);
    } finally {
      setTransitioning(false);
    }
  };

  const columns = [
    {
      title: '发布候选', dataIndex: 'release_ref', width: 260,
      render: (value: string, record: RuntimeRelease) => (
        <button className={`ur-release-link ${selected?.release_ref === value ? 'is-active' : ''}`} onClick={() => setSelectedRef(value)}>
          <strong>{record.model_name}</strong><span>{record.reason}</span>
        </button>
      ),
    },
    { title: '阶段', dataIndex: 'stage', width: 150, render: (value: string) => <Tag color={stageColor(value)}>{stageLabels[value] || value}</Tag> },
    { title: '风险', dataIndex: 'overall_risk', width: 110, render: (value: string) => <Tag color={riskColor(value)}>{riskLabels[value] || value}</Tag> },
    { title: '版本', dataIndex: 'version', width: 80, render: (value: number) => `r${value}` },
    { title: '灰度', dataIndex: 'canary_basis_points', width: 90, render: (value: number) => value ? `${(value / 100).toFixed(0)}%` : '-' },
    { title: '更新时间', dataIndex: 'updated_at', render: (value: string) => value ? new Date(value).toLocaleString() : '-' },
  ];

  const selectedChannel = (id: number) => channels.find(item => item.id === id);

  return (
    <>
      <Banner
        type="info"
        description="在线发布只覆盖通道准入、优先级和权重。Adapter 必须具备离线协议夹具，但夹具不代表供应商账号、网络或真实接口已经验证；请先在上游检测完成真实连通检查。紧急禁用始终高于任何发布。"
      />
      <div className="ur-modelgov-summary">
        <div><Typography.Text type="tertiary">当前生效</Typography.Text><strong>{stats.active}</strong></div>
        <div><Typography.Text type="tertiary">灰度中</Typography.Text><strong>{stats.canary}</strong></div>
        <div><Typography.Text type="tertiary">待推进</Typography.Text><strong>{stats.pending}</strong></div>
      </div>
      <div className="ur-context-explorer ur-governance-explorer ur-runtime-release-workspace">
        <main className="ur-explorer-panel">
          <div className="ur-panel-title">
            <div>
              <Typography.Title heading={5} style={{ margin: 0 }}>确定性发布队列</Typography.Title>
              <Typography.Text className="ur-panel-hint">草稿 → 验证 → 待灰度（零流量状态）→ 灰度（最高 50%）→ 生效；只有进入灰度后才会获得真实流量。</Typography.Text>
            </div>
            <Space>
              <Button icon={<IconRefresh />} onClick={() => void load()} loading={loading}>刷新</Button>
              <Button icon={<IconPlus />} type="primary" onClick={() => { formApi?.reset(); setDraftChannelID(undefined); setSheetOpen(true); }}>新建发布</Button>
            </Space>
          </div>
          {loadError ? (
            <EmptyState title="发布状态加载失败" description={loadError} actions={<Button type="primary" onClick={() => void load()}>重试</Button>} minHeight={320} />
          ) : (
            <div className="ur-table-shell">
              <Table rowKey="release_ref" columns={columns} dataSource={releases} loading={loading} pagination={false}
                empty={<EmptyState title="暂无运行时发布" description="新 Provider 通道先保持 staged，再在这里生成模拟证据并灰度上线。" minHeight={300} />} />
            </div>
          )}
        </main>
        <aside className="ur-explorer-panel ur-explorer-detail" aria-label="运行时发布检查器">
          <div className="ur-detail-header">
            <div><Typography.Text className="ur-panel-hint">Release Inspector</Typography.Text><Typography.Title heading={5} style={{ margin: 0 }}>证据与推进</Typography.Title></div>
            <Tag color={stageColor(selected?.stage || '')}>{selected ? stageLabels[selected.stage] || selected.stage : '未选择'}</Tag>
          </div>
          {selected ? (
            <>
              <dl className="ur-financial-truth-detail">
                <dt>模型</dt><dd>{selected.model_name}</dd>
                <dt>版本 / 阶段</dt><dd>r{selected.version} / {stageLabels[selected.stage] || selected.stage}</dd>
                <dt>模拟风险</dt><dd>{riskLabels[selected.overall_risk] || selected.overall_risk}</dd>
                <dt>灰度比例</dt><dd>{selected.canary_basis_points ? `${selected.canary_basis_points / 100}%` : '未启用'}</dd>
                <dt>理由</dt><dd>{selected.reason}</dd>
                <dt>运行时范围</dt><dd>route assignment only</dd>
              </dl>
              <details>
                <summary>高级标识与证据哈希</summary>
                <dl className="ur-financial-truth-detail">
                  <dt>自动发布标识</dt><dd className="ur-mono-break">{selected.release_ref}</dd>
                  <dt>候选 Hash</dt><dd className="ur-mono-break">{selected.candidate_hash}</dd>
                  <dt>基线 Hash</dt><dd className="ur-mono-break">{selected.baseline_hash}</dd>
                  <dt>离线夹具目录快照</dt><dd>{selected.provider_pack_pins.length} 个固定身份</dd>
                </dl>
              </details>
              {selected.stage === 'shadow' && <Banner type="warning" description="这是待灰度的零流量状态：当前阶段不会发送真实请求，也没有生成供应商连通性、质量或流量验证证据。" />}
              <Typography.Title heading={6}>通道覆盖</Typography.Title>
              <div className="ur-release-overrides">
                {selected.candidate.overrides.map(item => {
                  const channel = selectedChannel(item.channel_id);
                  return <div key={item.channel_id}>
                    <strong>{channel?.name || `Channel #${item.channel_id}`}</strong>
                    <span>epoch {item.expected_routing_epoch} · {item.admission} · P{item.priority ?? channel?.priority ?? '-'} · W{item.weight ?? channel?.routing_weight ?? '-'}</span>
                  </div>;
                })}
              </div>
              <div className="ur-governance-actions">
                {selected.stage === 'draft' && <Button type="primary" loading={transitioning} onClick={() => void transition('验证完成', () => api.validateRuntimeChange(selected.release_ref, selected.version))}>验证候选</Button>}
                {selected.stage === 'validated' && <Button type="primary" loading={transitioning} onClick={() => void transition('已标记为待灰度（零流量）', () => api.shadowRuntimeChange(selected.release_ref, selected.version))}>标记为待灰度（零流量）</Button>}
                {selected.stage === 'shadow' && <Popconfirm title="启动 5% 确定性灰度？" content="同一服务端请求标识会稳定落在同一发布槽。" onConfirm={() => void transition('5% 灰度已启动', () => api.canaryRuntimeChange(selected.release_ref, selected.version, 500))}><Button type="primary" loading={transitioning}>启动 5% 灰度</Button></Popconfirm>}
                {selected.stage === 'canary' && <Popconfirm title="将灰度候选提升为稳定版本？" content="提升后全部请求采用该受控路由覆盖。" onConfirm={() => void transition('发布已提升为稳定版本', () => api.promoteRuntimeChange(selected.release_ref, selected.version))}><Button type="primary" loading={transitioning}>提升为稳定</Button></Popconfirm>}
                {['canary', 'active'].includes(selected.stage) && <Popconfirm title="确认回滚该发布？" content="将恢复父稳定版本；紧急禁用状态不会被覆盖。" onConfirm={() => void transition('发布已回滚', () => api.rollbackRuntimeChange(selected.release_ref, selected.version))}><Button type="danger" loading={transitioning}>回滚</Button></Popconfirm>}
              </div>
            </>
          ) : <EmptyState title="选择一条发布" description="右侧固定显示风险、Hash、Provider Pack、覆盖内容和允许的下一步。" minHeight={300} />}
        </aside>
      </div>

      <SideSheet title="新建运行时发布" visible={sheetOpen} onCancel={() => { setSheetOpen(false); setDraftChannelID(undefined); }} width={560}>
        <Banner type="warning" description="新通道必须保持 staged。发布不会修改通道原始记录，只会创建可审计、可回滚的运行时覆盖。" style={{ marginBottom: 16 }} />
        <Form getFormApi={(next: any) => setFormApi(next)} onSubmit={createRelease} initValues={{ admission: 'allow' }}>
          <Typography.Paragraph type="tertiary">发布标识由服务器安全生成；创建后仅在右侧“高级标识与证据哈希”中显示。</Typography.Paragraph>
          <Form.Select
            field="channel_id"
            label="通道快照"
            optionList={releaseChannels.map(channel => ({ label: `${channel.name} · ${channel.provider} · ${channel.status} · epoch ${channel.routing_epoch}`, value: channel.id }))}
            onChange={(value) => {
              const channelID = Number(value);
              const channel = releaseChannels.find(item => item.id === channelID);
              const models = channel ? parseModelList(channel.models) : [];
              setDraftChannelID(channelID);
              formApi?.setValue('model_name', models.length === 1 ? models[0] : undefined);
            }}
            extraText={`仅显示 staged/active 且已声明模型的通道${excludedChannelCount > 0 ? `；已排除 ${excludedChannelCount} 个禁用或无模型通道` : ''}。凭证和网络就绪度请以上游检测结果为准。`}
            rules={[{ required: true, message: '请选择通道' }]}
          />
          <Form.Select
            field="model_name"
            label="目标模型"
            disabled={!draftChannel}
            placeholder={draftChannel ? '选择该通道声明的模型' : '请先选择通道'}
            optionList={draftModelOptions.map(model => ({ label: model, value: model }))}
            rules={[{ required: true, message: '请选择目标模型' }]}
          />
          <Form.Select field="admission" label="准入覆盖" optionList={[
            { label: '允许（仅 staged/active，绝不复活 disabled）', value: 'allow' },
            { label: '拒绝', value: 'deny' },
            { label: '继承基础状态', value: 'inherit' },
          ]} />
          <div className="ur-modelgov-form-grid">
            <Form.InputNumber field="priority" label="优先级（可选）" min={1} max={10000} style={{ width: '100%' }} />
            <Form.InputNumber field="weight" label="权重（可选）" min={1} max={1000} style={{ width: '100%' }} />
          </div>
          <Form.TextArea field="reason" label="业务理由" rows={4} rules={[{ required: true, message: '请输入发布理由' }]} />
          <div className="ur-form-actions"><Button onClick={() => { setSheetOpen(false); setDraftChannelID(undefined); }}>取消</Button><Button type="primary" loading={transitioning} onClick={() => formApi?.submitForm()}>生成草稿与模拟证据</Button></div>
        </Form>
      </SideSheet>
    </>
  );
}
