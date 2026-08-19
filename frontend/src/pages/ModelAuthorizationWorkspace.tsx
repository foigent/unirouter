import { useCallback, useEffect, useMemo, useState } from 'react';
import { Banner, Button, Form, Popconfirm, Select, SideSheet, Space, Table, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState } from '../ui';
import { customerContextBlockerCopy, effectiveCustomerOrgID, resolveCustomerContext } from '../utils/customerContext';
import { parseModelList } from '../utils/modelList';
import { loadAllPages } from '../utils/loadAllPages';
import { projectDisplayName } from '../utils/projectDisplay';

interface AuthorizationContract {
  contract_version: string;
  environment: string;
  subjects: string[];
  effects: string[];
  endpoints: string[];
  precedence: string;
  authority: string;
  tool_or_mcp_authority: string;
}

interface ModelAuthorization {
  id: number;
  org_id: number;
  authorization_ref: string;
  subject_type: string;
  subject_ref: string;
  environment: string;
  effect: string;
  allowed_models: string[];
  allowed_endpoints: string[];
  not_before?: string;
  expires_at?: string;
  status: string;
  revision: number;
  reason: string;
  created_at: string;
  activated_at?: string;
  revoked_at?: string;
  updated_at: string;
}

const subjectLabels: Record<string, string> = { api_key: 'API Key', access_credential: '应用访问凭证', project: '项目', user: '人员', agent: 'Agent' };
const endpointLabels: Record<string, string> = {
  '/v1/chat/completions': 'Chat', '/v1/embeddings': 'Embeddings', '/v1/models': '模型目录', '/v1/media/jobs': '异步模型任务',
};

function splitSet(value: unknown) {
  return parseModelList(value);
}

function statusColor(status: string) {
  if (status === 'active') return 'green';
  if (status === 'revoked') return 'grey';
  return 'orange';
}

export default function ModelAuthorizationWorkspace() {
  const user = useAuthStore(state => state.user);
  const platform = isPlatformAdminRole(organizationRoleForUser(user));
  const customerContext = resolveCustomerContext(user);
  const orgID = effectiveCustomerOrgID(user);
  const customerContextReady = customerContext.state === 'ready' && Boolean(orgID);
  const [contract, setContract] = useState<AuthorizationContract | null>(null);
  const [policies, setPolicies] = useState<ModelAuthorization[]>([]);
  const [selectedRef, setSelectedRef] = useState('');
  const [status, setStatus] = useState('');
  const [loading, setLoading] = useState(true);
  const [mutating, setMutating] = useState(false);
  const [error, setError] = useState('');
  const [sheetOpen, setSheetOpen] = useState(false);
  const [formApi, setFormApi] = useState<any>(null);
  const [subjectType, setSubjectType] = useState('user');
  const [subjectOptions, setSubjectOptions] = useState<Record<string, Array<{ value: string; label: string }>>>({ user: [], project: [], api_key: [] });
  const [modelOptions, setModelOptions] = useState<string[]>([]);
  const [optionLoading, setOptionLoading] = useState(false);

  const selected = policies.find(item => item.authorization_ref === selectedRef) || policies[0] || null;

  useEffect(() => {
    if (!customerContextReady) return;
    api.getModelAuthorizationContract().then(setContract).catch((loadError: any) => setError(loadError?.message || '授权合同不可用'));
  }, [customerContextReady]);

  const load = useCallback(async () => {
    if (!customerContextReady || !orgID) return;
    setLoading(true);
    setError('');
    try {
      const data = await api.getModelAuthorizations(platform ? orgID : undefined, status);
      const next = ((data?.data || []) as ModelAuthorization[]).filter(item => item.org_id === orgID);
      setPolicies(next);
      setSelectedRef(current => next.some(item => item.authorization_ref === current) ? current : next[0]?.authorization_ref || '');
    } catch (loadError: any) {
      setPolicies([]);
      setError(loadError?.message || '无法加载高级模型限制。');
    } finally {
      setLoading(false);
    }
  }, [customerContextReady, orgID, platform, status]);

  useEffect(() => { void load(); }, [load]);

  useEffect(() => {
    if (!customerContextReady || !orgID) return;
    setOptionLoading(true);
    void Promise.all([
      loadAllPages<any>((page, size) => api.getUsers(page, size, 'local')),
      loadAllPages<any>((page, size) => api.getUsers(page, size, 'enterprise')),
      loadAllPages<any>((page, size) => api.getProjects(page, size, { status: 'active', org_id: orgID })),
      loadAllPages<any>((page, size) => api.getKeys(page, size)),
      api.getModelNames(),
    ]).then(([localUsers, enterpriseUsers, projects, keys, modelData]) => {
      const people = [...localUsers, ...enterpriseUsers]
        .filter(item => Number(item.org_id) === Number(orgID) && item.status === 'active')
        .map(item => ({ value: String(item.id), label: `${item.username}${item.auth_provider === 'oidc' ? ' · OIDC' : ' · 本地账号'}` }));
      setSubjectOptions({
        user: people,
        project: projects.filter(item => Number(item.org_id) === Number(orgID) && item.status === 'active')
          .map(item => ({ value: String(item.id), label: projectDisplayName(item) })),
        api_key: keys.filter(item => Number(item.org_id) === Number(orgID))
          .map(item => ({ value: String(item.id), label: `${item.name || `API Key #${item.id}`} · ${item.key_prefix || ''}***` })),
      });
      const source = Array.isArray(modelData?.data) ? modelData.data : modelData;
      setModelOptions((Array.isArray(source) ? source : []).map((item: any) => typeof item === 'string' ? item : item.model).filter(Boolean));
    }).catch((optionError: any) => {
      setError(optionError?.message || '无法加载人员、项目和模型选项');
    }).finally(() => setOptionLoading(false));
  }, [customerContextReady, orgID]);

  const stats = useMemo(() => ({
    active: policies.filter(item => item.status === 'active').length,
    draft: policies.filter(item => item.status === 'draft').length,
    deny: policies.filter(item => item.status === 'active' && item.effect === 'deny').length,
  }), [policies]);

  const create = async (values: Record<string, any>) => {
    if (!customerContextReady || !orgID) {
      Toast.error('无法确认唯一客户组织');
      return;
    }
    const models = splitSet(values.allowed_models);
    const endpoints = Array.isArray(values.allowed_endpoints) ? values.allowed_endpoints.map(String) : [];
    if (!models.length || !endpoints.length) {
      Toast.error('模型范围和端点范围均不能为空');
      return;
    }
    setMutating(true);
    try {
      const created = await api.createModelAuthorization({
        ...(platform ? { org_id: orgID } : {}),
        subject_type: String(values.subject_type || ''),
        subject_ref: String(values.subject_ref || '').trim(),
        environment: String(values.environment || '*').trim().toLowerCase(),
        effect: String(values.effect || 'allow'),
        allowed_models: models,
        allowed_endpoints: endpoints,
        reason: String(values.reason || '').trim(),
      }) as ModelAuthorization;
      Toast.success('授权草稿已创建；未激活前不影响运行时');
      setSheetOpen(false);
      formApi?.reset();
      await load();
      setSelectedRef(created.authorization_ref);
    } catch (createError: any) {
      Toast.error(`创建失败${createError?.message ? `：${createError.message}` : ''}`);
    } finally {
      setMutating(false);
    }
  };

  const transition = async (label: string, operation: () => Promise<any>) => {
    setMutating(true);
    try {
      const updated = await operation() as ModelAuthorization;
      Toast.success(label);
      await load();
      setSelectedRef(updated.authorization_ref);
    } catch (transitionError: any) {
      Toast.error(`${label}失败${transitionError?.message ? `：${transitionError.message}` : ''}`);
    } finally {
      setMutating(false);
    }
  };

  const columns = [
    {
      title: '授权策略', dataIndex: 'authorization_ref', width: 230,
      render: (value: string, record: ModelAuthorization) => (
        <button className={`ur-release-link ${selected?.authorization_ref === value ? 'is-active' : ''}`} onClick={() => setSelectedRef(value)}>
          <strong>{value}</strong><span>{subjectLabels[record.subject_type] || record.subject_type} · {record.subject_ref}</span>
        </button>
      ),
    },
    { title: '效果', dataIndex: 'effect', width: 90, render: (value: string) => <Tag color={value === 'deny' ? 'red' : 'blue'}>{value === 'deny' ? '拒绝' : '允许范围'}</Tag> },
    { title: '模型范围', dataIndex: 'allowed_models', render: (value: string[]) => value.join('、') },
    { title: '环境', dataIndex: 'environment', width: 110 },
    { title: '状态', dataIndex: 'status', width: 100, render: (value: string) => <Tag color={statusColor(value)}>{value === 'active' ? '生效' : value === 'draft' ? '草稿' : '已撤销'}</Tag> },
  ];

  if (!customerContextReady || !orgID) {
    const blocker = customerContextBlockerCopy(customerContext.state === 'ready' ? 'unavailable' : customerContext.state);
    return <EmptyState title={blocker.title} description={blocker.description} minHeight={320} />;
  }

  return (
    <>
      <Banner type="info" description="这是高级收窄策略，不是给用户开通模型的入口。正常授权请先在“人员与权限”把用户加入项目，再由用户创建个人项目凭证；工作负载则由项目管理员创建应用凭证。这里的显式拒绝与允许交集只能进一步缩小已有权限。" closeIcon={null} />
      <div className="ur-modelgov-summary">
        <div><Typography.Text type="tertiary">生效策略</Typography.Text><strong>{stats.active}</strong></div>
        <div><Typography.Text type="tertiary">待激活草稿</Typography.Text><strong>{stats.draft}</strong></div>
        <div><Typography.Text type="tertiary">显式拒绝</Typography.Text><strong>{stats.deny}</strong></div>
      </div>
      <div className="ur-context-explorer ur-governance-explorer ur-model-authorization-workspace">
        <main className="ur-explorer-panel">
          <div className="ur-panel-title">
            <div>
              <Typography.Title heading={5} style={{ margin: 0 }}>高级模型限制策略</Typography.Title>
              <Typography.Text className="ur-panel-hint">{contract?.contract_version || '正在加载合同'} · 当前环境 {contract?.environment || '-'}</Typography.Text>
            </div>
            <Space>
              {platform && <Typography.Text type="tertiary">客户范围：{customerContext.orgName || `组织 #${orgID}`}</Typography.Text>}
              <Select aria-labelledby="model-authorization-status-label" insetLabel="状态" insetLabelId="model-authorization-status-label" value={status} onChange={value => setStatus(String(value || ''))} showClear placeholder="全部" optionList={[{ label: '草稿', value: 'draft' }, { label: '生效', value: 'active' }, { label: '已撤销', value: 'revoked' }]} style={{ width: 140 }} />
              <Button icon={<IconRefresh />} loading={loading} onClick={() => void load()}>刷新</Button>
              <Button aria-label="新增限制策略" icon={<IconPlus />} type="primary" onClick={() => { setSubjectType('user'); setSheetOpen(true); }}>新增限制策略</Button>
            </Space>
          </div>
          {loading && policies.length === 0 ? <EmptyState title="正在读取高级模型限制" description="正在核对当前客户范围和生效策略，请稍候。" minHeight={320} /> : error ? <EmptyState title="高级模型限制加载失败" description={error} actions={<Button type="primary" onClick={() => void load()}>重试</Button>} minHeight={320} /> : (
            <div className="ur-table-shell"><Table rowKey="authorization_ref" columns={columns} dataSource={policies} loading={loading} pagination={false}
              empty={<EmptyState title="暂无授权策略" description="无策略时保持现有访问凭证、项目模型集和 Agent 能力规则；创建并激活后只会进一步收窄。" minHeight={300} />} /></div>
          )}
        </main>
        <aside className="ur-explorer-panel ur-explorer-detail" aria-label="高级模型限制检查器">
          <div className="ur-detail-header"><div><Typography.Text className="ur-panel-hint">限制策略检查器</Typography.Text><Typography.Title heading={5} style={{ margin: 0 }}>适用范围与操作</Typography.Title></div><Tag color={statusColor(selected?.status || '')}>{selected?.status === 'active' ? '生效' : selected?.status === 'draft' ? '草稿' : selected ? '已撤销' : '未选择'}</Tag></div>
          {loading && policies.length === 0 ? <EmptyState title="正在读取限制策略证据" description="加载完成后会显示适用主体、模型、端点和允许的下一步。" minHeight={300} /> : selected ? <>
            <dl className="ur-financial-truth-detail">
              <dt>策略标识</dt><dd className="ur-mono-break">{selected.authorization_ref}</dd>
              <dt>客户范围</dt><dd>{customerContext.orgName || `组织 #${selected.org_id}`}</dd>
              <dt>主体</dt><dd>{subjectLabels[selected.subject_type] || selected.subject_type} · {selected.subject_ref}</dd>
              <dt>效果</dt><dd>{selected.effect === 'deny' ? '显式拒绝（最高优先级）' : '允许范围（与其他主体求交集）'}</dd>
              <dt>环境</dt><dd>{selected.environment}</dd>
              <dt>模型</dt><dd>{selected.allowed_models.join('、')}</dd>
              <dt>端点</dt><dd>{selected.allowed_endpoints.map(item => endpointLabels[item] || item).join('、')}</dd>
              <dt>有效期</dt><dd>{selected.not_before || selected.expires_at ? `${selected.not_before || '立即'} → ${selected.expires_at || '持续'}` : '持续（直至撤销）'}</dd>
              <dt>理由</dt><dd>{selected.reason}</dd>
              <dt>Tool/MCP 权限</dt><dd>无</dd>
              <dt>运行时变更</dt><dd>none（请求时只读判定）</dd>
            </dl>
            <div className="ur-governance-actions">
              {selected.status === 'draft' && <Popconfirm title="激活该收窄策略？" content="激活后匹配请求将立即执行拒绝优先与允许范围交集。" onConfirm={() => void transition('授权策略已激活', () => api.activateModelAuthorization(selected.authorization_ref, selected.revision, platform ? orgID : undefined))}><Button type="primary" loading={mutating}>激活</Button></Popconfirm>}
              {selected.status === 'active' && <Popconfirm title="撤销该策略？" content="撤销只移除这一层收窄，不会扩大超过基础访问凭证、项目模型集或 Agent 能力。" onConfirm={() => void transition('授权策略已撤销', () => api.revokeModelAuthorization(selected.authorization_ref, selected.revision, platform ? orgID : undefined))}><Button type="danger" loading={mutating}>撤销</Button></Popconfirm>}
            </div>
          </> : <EmptyState title="选择一条授权策略" description="右侧固定显示主体、环境、模型、端点、理由与生命周期操作。" minHeight={300} />}
        </aside>
      </div>

      <SideSheet title="新增高级模型限制" visible={sheetOpen} onCancel={() => setSheetOpen(false)} width={560}>
        <Banner type="warning" description="这不是开通权限。先在“人员与权限”把用户加入项目；本表单只把该用户已有 API Key 的模型范围进一步缩小。策略标识由系统自动生成。" closeIcon={null} style={{ marginBottom: 16 }} />
        <Form getFormApi={(next: any) => setFormApi(next)} onSubmit={create} initValues={{ subject_type: 'user', environment: contract?.environment || '*', effect: 'allow', allowed_endpoints: ['/v1/chat/completions'], reason: '管理员配置的模型调用范围' }}>
          <Form.Select
            field="subject_type"
            label={<span id="model-authorization-subject-type-label">限制对象类型</span>}
            aria-labelledby="model-authorization-subject-type-label"
            optionList={(contract?.subjects || ['user', 'project', 'api_key', 'access_credential', 'agent']).map(value => ({ label: subjectLabels[value] || value, value }))}
            onChange={value => { setSubjectType(String(value || 'user')); formApi?.setValue('subject_ref', undefined); }}
            rules={[{ required: true, message: '请选择限制对象类型' }]}
            extraText="限制单个账号请选择“人员”；只有审计或自动化场景才需要其他类型。"
          />
          {['user', 'project', 'api_key'].includes(subjectType) ? <Form.Select
            field="subject_ref"
            label={<span id="model-authorization-subject-label">选择{subjectLabels[subjectType]}</span>}
            aria-labelledby="model-authorization-subject-label"
            filter
            loading={optionLoading}
            optionList={subjectOptions[subjectType] || []}
            rules={[{ required: true, message: `请选择${subjectLabels[subjectType]}` }]}
            emptyContent={`没有可选择的${subjectLabels[subjectType]}`}
          /> : <Form.Input field="subject_ref" label={`${subjectLabels[subjectType] || '主体'}标识（高级）`} rules={[{ required: true, message: '请输入已存在的主体标识' }]} extraText="只接受相应对象高级审计信息中的稳定标识。" />}
          <Form.Select
            field="allowed_models"
            label={<span id="model-authorization-models-label">只允许调用这些模型</span>}
            aria-labelledby="model-authorization-models-label"
            multiple filter loading={optionLoading}
            optionList={modelOptions.map(value => ({ label: value, value }))}
            rules={[{ required: true, message: '请选择至少一个模型' }]}
            extraText="这是与项目/API Key 原有范围的交集，不会凭空授予模型。"
          />
          <Form.TextArea field="reason" label="设置原因" rows={3} rules={[{ required: true, message: '请填写设置原因' }]} />
          <details className="ur-form-advanced">
            <summary>高级规则（拒绝策略、环境和接口范围）</summary>
            <div className="ur-modelgov-form-grid" style={{ marginTop: 12 }}>
              <Form.Input field="environment" label="环境" placeholder="production 或 *" rules={[{ required: true, message: '请输入环境' }]} />
              <Form.Select field="effect" label={<span id="model-authorization-effect-label">规则方式</span>} aria-labelledby="model-authorization-effect-label" optionList={[{ label: '只允许所选模型（推荐）', value: 'allow' }, { label: '拒绝所选模型', value: 'deny' }]} />
            </div>
            <Form.Select field="allowed_endpoints" label={<span id="model-authorization-endpoints-label">接口范围</span>} aria-labelledby="model-authorization-endpoints-label" multiple optionList={(contract?.endpoints || []).map(value => ({ label: endpointLabels[value] || value, value }))} rules={[{ required: true, message: '请选择接口范围' }]} />
          </details>
          <div className="ur-form-actions"><Button onClick={() => setSheetOpen(false)}>取消</Button><Button type="primary" loading={mutating} onClick={() => formApi?.submitForm()}>创建草稿</Button></div>
        </Form>
      </SideSheet>
    </>
  );
}
