import { useCallback, useEffect, useMemo, useState } from 'react';
import { Banner, Button, Form, Input, SideSheet, Space, Table, Toast, Typography } from '@douyinfe/semi-ui';
import { IconEdit, IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState, StatusTag } from '../ui';
import { loadAllPages } from '../utils/loadAllPages';

interface InvocationOptimizationPolicy {
  id: number;
  contract_version: string;
  org_id: number;
  project_id: number;
  policy_ref: string;
  display_name: string;
  model_selector: string;
  region: string;
  residency: string;
  response_cache_target: 'disabled' | 'canary';
  response_cache_canary_basis_points: number;
  response_cache_ttl_seconds: number;
  response_cache_persistence_allowed: boolean;
  response_cache_require_request_opt_in: boolean;
  compression_target: 'disabled' | 'canary';
  compression_canary_basis_points: number;
  compression_require_request_opt_in: boolean;
  compression_minimum_input_tokens: number;
  compression_minimum_saved_tokens: number;
  compression_maximum_reduction_basis_points: number;
  minimum_shadow_samples: number;
  status: 'active' | 'disabled';
  stage: 'draft' | 'validated' | 'shadow' | 'canary' | 'rolled_back';
  revision: number;
  updated_at: string;
}

interface OptimizationContract {
  contract_version: string;
  response_cache_runtime_ready: boolean;
  maximum_canary_basis_points: number;
  request_opt_in_always_required: boolean;
  raw_content_in_receipts: boolean;
  x_insight_in_request_path: boolean;
}

interface ProjectOption {
  id: number;
  name: string;
}

type TransitionAction = 'validate' | 'shadow' | 'canary' | 'rollback';

const stageLabels: Record<string, string> = {
  draft: '草稿', validated: '已验证', shadow: '影子评估', canary: '受控灰度', rolled_back: '已回滚',
};

const transitionCopy: Record<TransitionAction, { label: string; title: string; description: string }> = {
  validate: { label: '验证策略', title: '验证策略合同', description: '只冻结并校验参数，不改变请求执行。' },
  shadow: { label: '启动 Shadow', title: '进入影子评估', description: '只记录资格、质量与节省估算，不读取缓存、不修改 Prompt。' },
  canary: { label: '启动 Canary', title: '启动受控灰度', description: '仅命中确定性分桶且显式 opt-in 的请求可能被优化；比例上限 10%。' },
  rollback: { label: '立即回滚', title: '回滚优化策略', description: '停止后续优化分配；既有终态证据保持不可变。' },
};

function percentLabel(basisPoints: number) {
  return basisPoints > 0 ? `${(basisPoints / 100).toFixed(basisPoints % 100 ? 2 : 0)}%` : '关闭';
}

function policyFormValues(policy?: InvocationOptimizationPolicy) {
  return policy ? {
    ...policy,
    project_id: policy.project_id || undefined,
  } : {
    model_selector: '*', region: 'global', residency: 'customer_policy', status: 'active',
    response_cache_target: 'disabled', response_cache_canary_basis_points: 100,
    response_cache_ttl_seconds: 300, response_cache_persistence_allowed: false,
    compression_target: 'disabled', compression_canary_basis_points: 100,
    compression_minimum_input_tokens: 128, compression_minimum_saved_tokens: 8,
    compression_maximum_reduction_basis_points: 2500, minimum_shadow_samples: 100,
  };
}

export default function InvocationOptimizationWorkspace({ orgId }: { orgId?: number }) {
  const [policies, setPolicies] = useState<InvocationOptimizationPolicy[]>([]);
  const [contract, setContract] = useState<OptimizationContract | null>(null);
  const [projects, setProjects] = useState<ProjectOption[]>([]);
  const [selectedRef, setSelectedRef] = useState('');
  const [loading, setLoading] = useState(false);
  const [mutating, setMutating] = useState(false);
  const [loadError, setLoadError] = useState('');
  const [editing, setEditing] = useState<InvocationOptimizationPolicy | null | undefined>(undefined);
  const [formApi, setFormApi] = useState<any>(null);
  const [transition, setTransition] = useState<TransitionAction | null>(null);
  const [transitionReason, setTransitionReason] = useState('');

  const selected = policies.find(item => item.policy_ref === selectedRef) || policies[0] || null;

  const load = useCallback(async () => {
    setLoading(true);
    setLoadError('');
    try {
      const [policyResponse, contractResponse, projectItems] = await Promise.all([
        api.getInvocationOptimizationPolicies(orgId),
        api.getInvocationOptimizationContract(),
        loadAllPages<ProjectOption>((page, size) => api.getProjects(page, size, {
          status: 'active', ...(orgId ? { org_id: orgId } : {}),
        })),
      ]);
      const next = (policyResponse?.data || []) as InvocationOptimizationPolicy[];
      setPolicies(next);
      setContract(contractResponse as OptimizationContract);
      setProjects(projectItems);
      setSelectedRef(current => next.some(item => item.policy_ref === current) ? current : next[0]?.policy_ref || '');
    } catch (error: any) {
      setPolicies([]);
      setContract(null);
      setProjects([]);
      setSelectedRef('');
      setLoadError(error?.message || '无法加载调用优化策略。');
    } finally {
      setLoading(false);
    }
  }, [orgId]);

  useEffect(() => { void load(); }, [load]);

  useEffect(() => {
    if (editing === undefined || !formApi) return;
    setTimeout(() => formApi.setValues(policyFormValues(editing || undefined)), 0);
  }, [editing, formApi]);

  const stats = useMemo(() => ({
    shadow: policies.filter(item => item.stage === 'shadow').length,
    canary: policies.filter(item => item.stage === 'canary').length,
    inactive: policies.filter(item => item.stage === 'rolled_back' || item.status === 'disabled').length,
  }), [policies]);

  const savePolicy = async (values: Record<string, any>) => {
    const responseTarget = String(values.response_cache_target || 'disabled') as 'disabled' | 'canary';
    const compressionTarget = String(values.compression_target || 'disabled') as 'disabled' | 'canary';
    const responseCanaryBPS = Number(values.response_cache_canary_basis_points || 0);
    const compressionCanaryBPS = Number(values.compression_canary_basis_points || 0);
    if ((responseTarget === 'canary' && (responseCanaryBPS < 1 || responseCanaryBPS > 1000)) ||
        (compressionTarget === 'canary' && (compressionCanaryBPS < 1 || compressionCanaryBPS > 1000))) {
      Toast.error('准备 Shadow / Canary 的驱动必须明确填写 1–1000 基点（0.01%–10%）');
      return;
    }
    if (responseTarget === 'canary' && !values.response_cache_persistence_allowed) {
      Toast.error('Exact Cache 必须由管理员单独确认允许加密持久化，不能随 Canary 目标自动开启');
      return;
    }
    const payload = {
      org_id: orgId || 0,
      project_id: Number(values.project_id || 0),
      policy_ref: String(values.policy_ref || editing?.policy_ref || '').trim().toLowerCase(),
      display_name: String(values.display_name || '').trim(),
      model_selector: String(values.model_selector || '*').trim(),
      region: String(values.region || 'global').trim(),
      residency: String(values.residency || 'customer_policy').trim(),
      response_cache_target: responseTarget,
      response_cache_canary_basis_points: responseTarget === 'canary' ? responseCanaryBPS : 0,
      response_cache_ttl_seconds: Number(values.response_cache_ttl_seconds || 300),
      response_cache_persistence_allowed: Boolean(values.response_cache_persistence_allowed),
      response_cache_require_request_opt_in: true,
      compression_target: compressionTarget,
      compression_canary_basis_points: compressionTarget === 'canary' ? compressionCanaryBPS : 0,
      compression_require_request_opt_in: true,
      compression_minimum_input_tokens: Number(values.compression_minimum_input_tokens || 128),
      compression_minimum_saved_tokens: Number(values.compression_minimum_saved_tokens || 8),
      compression_maximum_reduction_basis_points: Number(values.compression_maximum_reduction_basis_points || 2500),
      minimum_shadow_samples: Number(values.minimum_shadow_samples || 100),
      status: String(values.status || 'active'),
    };
    setMutating(true);
    try {
      const result = editing
        ? await api.updateInvocationOptimizationPolicy(editing.policy_ref, { ...payload, expected_revision: editing.revision })
        : await api.createInvocationOptimizationPolicy(payload);
      Toast.success(editing ? '策略草稿已更新' : '策略草稿已创建');
      setEditing(undefined);
      formApi?.reset();
      await load();
      setSelectedRef(result?.policy_ref || payload.policy_ref);
    } catch (error: any) {
      Toast.error(`保存失败${error?.message ? `：${error.message}` : ''}`);
    } finally {
      setMutating(false);
    }
  };

  const executeTransition = async () => {
    if (!selected || !transition) return;
    const reason = transitionReason.trim();
    if (!reason) {
      Toast.error('请输入本次推进或回滚的审计理由');
      return;
    }
    setMutating(true);
    try {
      const result = await api.transitionInvocationOptimizationPolicy(
        selected.policy_ref, transition, selected.revision, reason,
      );
      Toast.success(`${transitionCopy[transition].label}完成`);
      setTransition(null);
      setTransitionReason('');
      await load();
      setSelectedRef(result?.policy_ref || selected.policy_ref);
    } catch (error: any) {
      Toast.error(`${transitionCopy[transition].label}失败${error?.message ? `：${error.message}` : ''}`);
    } finally {
      setMutating(false);
    }
  };

  const hasSelectedDriver = Boolean(selected && (
    selected.response_cache_target === 'canary' || selected.compression_target === 'canary'
  ));
  const lifecycleActive = selected?.status === 'active' && hasSelectedDriver;
  const nextActions = selected ? [
    ...(selected.stage === 'draft' && lifecycleActive ? ['validate' as const] : []),
    ...(selected.stage === 'validated' && lifecycleActive ? ['shadow' as const] : []),
    ...(selected.stage === 'shadow' && lifecycleActive ? ['canary' as const, 'rollback' as const] : []),
    ...(selected.stage === 'canary' ? ['rollback' as const] : []),
  ] : [];

  const columns = [
    {
      title: '策略', dataIndex: 'policy_ref',
      render: (value: string, record: InvocationOptimizationPolicy) => (
        <button className={`ur-release-link ${selected?.policy_ref === value ? 'is-active' : ''}`} onClick={() => setSelectedRef(value)}>
          <strong>{record.display_name}</strong><span>{value} · {record.model_selector}</span>
        </button>
      ),
    },
    { title: '阶段', dataIndex: 'stage', width: 112, render: (value: string) => <StatusTag status={value}>{stageLabels[value] || value}</StatusTag> },
    { title: 'Exact Cache', key: 'cache', width: 120, render: (_: unknown, record: InvocationOptimizationPolicy) => percentLabel(record.response_cache_canary_basis_points) },
    { title: 'Compression', key: 'compression', width: 120, render: (_: unknown, record: InvocationOptimizationPolicy) => percentLabel(record.compression_canary_basis_points) },
    { title: '版本', dataIndex: 'revision', width: 72, render: (value: number) => `r${value}` },
  ];

  return (
    <>
      <Banner
        type={contract?.response_cache_runtime_ready ? 'info' : 'warning'}
        description={contract?.response_cache_runtime_ready
          ? 'Exact Cache 密钥与 Redis 运行时已就绪，但策略仍默认关闭。Shadow 不读写缓存；Canary 还要求足量零失败证据与请求显式 opt-in。'
          : 'Exact Cache 密钥运行时未配置，因此不能进入 Cache Canary；Compression Shadow 仍可独立评估。所有策略默认关闭，X-Insight 不在请求关键路径。'}
      />
      <div className="ur-modelgov-summary ur-optimization-summary">
        <div><Typography.Text type="tertiary">Shadow 中</Typography.Text><strong>{stats.shadow}</strong></div>
        <div><Typography.Text type="tertiary">Canary 中</Typography.Text><strong>{stats.canary}</strong></div>
        <div><Typography.Text type="tertiary">已停用 / 回滚</Typography.Text><strong>{stats.inactive}</strong></div>
        <div><Typography.Text type="tertiary">Exact Cache 运行时</Typography.Text><strong className="ur-optimization-runtime">{contract?.response_cache_runtime_ready ? '就绪' : '未配置'}</strong></div>
      </div>
      <div className="ur-context-explorer ur-governance-explorer ur-invocation-optimization-workspace">
        <main className="ur-explorer-panel">
          <div className="ur-panel-title">
            <div>
              <Typography.Title heading={5} style={{ margin: 0 }}>调用优化策略</Typography.Title>
              <Typography.Text className="ur-panel-hint">草稿 → 验证 → Shadow → 证据门禁 → Canary；无直接全量启用入口。</Typography.Text>
            </div>
            <Space>
              <Button icon={<IconRefresh />} onClick={() => void load()} loading={loading}>刷新</Button>
              <Button icon={<IconPlus />} type="primary" onClick={() => setEditing(null)}>新建策略</Button>
            </Space>
          </div>
          {loadError ? (
            <EmptyState title="调用优化策略加载失败" description={loadError} actions={<Button type="primary" onClick={() => void load()}>重试</Button>} minHeight={320} />
          ) : (
            <div className="ur-table-shell">
              <Table rowKey="policy_ref" columns={columns} dataSource={policies} loading={loading} pagination={false}
                empty={<EmptyState title="暂无调用优化策略" description="默认状态不会缓存响应或修改 Prompt。先创建草稿，再进入只读 Shadow 收集证据。" minHeight={300} />} />
            </div>
          )}
        </main>
        <aside className="ur-explorer-panel ur-explorer-detail" aria-label="调用优化策略检查器">
          <div className="ur-detail-header">
            <div><Typography.Text className="ur-panel-hint">Policy Inspector</Typography.Text><Typography.Title heading={5} style={{ margin: 0 }}>证据门禁与执行边界</Typography.Title></div>
            <StatusTag status={selected?.stage || 'disabled'}>{selected ? stageLabels[selected.stage] : '未选择'}</StatusTag>
          </div>
          {selected ? (
            <>
              <dl className="ur-financial-truth-detail">
                <dt>策略 / 版本</dt><dd className="ur-mono-break">{selected.policy_ref} / r{selected.revision}</dd>
                <dt>模型范围</dt><dd>{selected.model_selector}</dd>
                <dt>组织 / 项目</dt><dd>{selected.org_id} / {selected.project_id || '全部项目'}</dd>
                <dt>区域 / 驻留</dt><dd>{selected.region} / {selected.residency}</dd>
                <dt>Exact Cache</dt><dd>{selected.response_cache_target === 'canary' ? `${percentLabel(selected.response_cache_canary_basis_points)} · TTL ${selected.response_cache_ttl_seconds}s` : '关闭'}</dd>
                <dt>Compression</dt><dd>{selected.compression_target === 'canary' ? `${percentLabel(selected.compression_canary_basis_points)} · ≥${selected.compression_minimum_saved_tokens} tokens` : '关闭'}</dd>
                <dt>Shadow 门槛</dt><dd>≥ {selected.minimum_shadow_samples} 样本、eligible &gt; 0、失败 = 0</dd>
                <dt>请求许可</dt><dd>两个驱动均强制显式 opt-in</dd>
                <dt>内容保留</dt><dd>策略证据不保存 Prompt / Response 明文</dd>
              </dl>
              <div className="ur-detail-card">
                <strong className="ur-detail-card-title">长期执行边界</strong>
                <Typography.Text>Exact Cache 只接受精确身份与终态来源证明；Compression V1 只做可逆空白规整。路由、IAM、计费、Guardrail 和最终执行权限仍由 UniRouter 核心拥有。</Typography.Text>
              </div>
              <div className="ur-governance-actions">
                {['draft', 'rolled_back'].includes(selected.stage) && <Button icon={<IconEdit />} onClick={() => setEditing(selected)}>编辑草稿</Button>}
                {nextActions.map(action => (
                  <Button key={action} type={action === 'canary' ? 'warning' : action === 'rollback' ? 'danger' : 'primary'}
                    disabled={action === 'canary' && selected.response_cache_target === 'canary' && !contract?.response_cache_runtime_ready}
                    onClick={() => { setTransition(action); setTransitionReason(''); }}>
                    {transitionCopy[action].label}
                  </Button>
                ))}
              </div>
              {selected.stage === 'draft' && !lifecycleActive && (
                <Typography.Text type="tertiary">先将策略设为“可评估”，并至少选择一个准备 Shadow / Canary 的驱动，才能验证合同。</Typography.Text>
              )}
            </>
          ) : <EmptyState title="选择一条策略" description="右侧固定显示范围、比例、证据门槛、数据边界和唯一允许的下一步。" minHeight={320} />}
        </aside>
      </div>

      <SideSheet title={editing ? '编辑调用优化策略' : '新建调用优化策略'} visible={editing !== undefined} onCancel={() => setEditing(undefined)} width={620}>
        <Banner type="info" description="创建或编辑只保存草稿，不会启动 Shadow 或 Canary。两个驱动均没有绕过请求 opt-in 的开关。" style={{ marginBottom: 16 }} />
        <Form getFormApi={(next: any) => setFormApi(next)} onSubmit={savePolicy} initValues={policyFormValues(editing || undefined)}>
          <div className="ur-modelgov-form-grid">
            <Form.Input field="policy_ref" label="策略标识" placeholder="opt_company_chat" disabled={Boolean(editing)} rules={[{ required: true, message: '请输入策略标识' }]} />
            <Form.Input field="display_name" label="显示名称" placeholder="Company Chat 优化" rules={[{ required: true, message: '请输入显示名称' }]} />
            <Form.Select field="project_id" label="项目范围（留空为组织级）" showClear optionList={projects.map(item => ({ value: item.id, label: `${item.name}（ID ${item.id}）` }))} style={{ width: '100%' }} />
            <Form.Input field="model_selector" label="模型范围" placeholder="* 或 company-chat" rules={[{ required: true }]} />
            <Form.Input field="region" label="区域" rules={[{ required: true }]} />
            <Form.Input field="residency" label="数据驻留策略" rules={[{ required: true }]} />
            <Form.Select field="response_cache_target" label="Exact Response Cache" optionList={[{ value: 'disabled', label: '关闭' }, { value: 'canary', label: '准备 Shadow / Canary' }]} style={{ width: '100%' }} />
            <Form.InputNumber field="response_cache_canary_basis_points" label="Cache Canary 基点（100 = 1%）" min={0} max={1000} style={{ width: '100%' }} />
            <Form.InputNumber field="response_cache_ttl_seconds" label="Cache TTL（60–86400 秒）" min={60} max={86400} style={{ width: '100%' }} />
            <Form.Checkbox field="response_cache_persistence_allowed" label="Exact Cache 数据许可">
              允许在该范围内以独立密钥加密持久化符合条件的响应（仍需请求显式 opt-in）
            </Form.Checkbox>
            <Form.Select field="compression_target" label="Prompt Compression" optionList={[{ value: 'disabled', label: '关闭' }, { value: 'canary', label: '准备 Shadow / Canary' }]} style={{ width: '100%' }} />
            <Form.InputNumber field="compression_canary_basis_points" label="Compression Canary 基点" min={0} max={1000} style={{ width: '100%' }} />
            <Form.InputNumber field="compression_minimum_input_tokens" label="最小输入 Token" min={1} max={1000000} style={{ width: '100%' }} />
            <Form.InputNumber field="compression_minimum_saved_tokens" label="最小预计节省 Token" min={1} max={1000000} style={{ width: '100%' }} />
            <Form.InputNumber field="compression_maximum_reduction_basis_points" label="最大压缩比例基点" min={1} max={5000} style={{ width: '100%' }} />
            <Form.InputNumber field="minimum_shadow_samples" label="Shadow 最小样本" min={10} max={1000000} style={{ width: '100%' }} />
            <Form.Select field="status" label="策略状态" optionList={[{ value: 'active', label: '可评估' }, { value: 'disabled', label: '停用' }]} style={{ width: '100%' }} />
          </div>
          <div className="ur-form-actions"><Button onClick={() => setEditing(undefined)}>取消</Button><Button htmlType="submit" type="primary" loading={mutating}>保存草稿</Button></div>
        </Form>
      </SideSheet>

      <SideSheet title={transition ? transitionCopy[transition].title : ''} visible={Boolean(transition)} onCancel={() => setTransition(null)} width={480}>
        {transition && <>
          <Banner type={transition === 'canary' || transition === 'rollback' ? 'warning' : 'info'} description={transitionCopy[transition].description} style={{ marginBottom: 16 }} />
          <Typography.Paragraph>当前策略：<Typography.Text code>{selected?.policy_ref}</Typography.Text> · r{selected?.revision}</Typography.Paragraph>
          <Input value={transitionReason} onChange={setTransitionReason} placeholder="填写可审计理由（必填）" maxLength={500} showClear />
          {transition === 'canary' && <div className="ur-detail-card" style={{ marginTop: 16 }}><strong className="ur-detail-card-title">服务端将再次 fail-closed 校验</strong><Typography.Text>Shadow 样本、eligible 数量、质量失败、Cache 密钥运行时与乐观锁版本必须全部满足；UI 不能绕过。</Typography.Text></div>}
          <div className="ur-form-actions"><Button onClick={() => setTransition(null)}>取消</Button><Button type={transition === 'rollback' ? 'danger' : 'primary'} loading={mutating} onClick={() => void executeTransition()}>{transitionCopy[transition].label}</Button></div>
        </>}
      </SideSheet>
    </>
  );
}
