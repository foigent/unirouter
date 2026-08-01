import { useCallback, useEffect, useMemo, useState } from 'react';
import { Banner, Button, Form, Popconfirm, Select, SideSheet, Space, Table, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { isPlatformAdminRole } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState } from '../ui';
import { loadAllPages } from '../utils/loadAllPages';

interface Organization { id: number; name: string }

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

const subjectLabels: Record<string, string> = { api_key: '访问凭证', project: '项目', user: '人员', agent: 'Agent' };
const endpointLabels: Record<string, string> = {
  '/v1/chat/completions': 'Chat', '/v1/embeddings': 'Embeddings', '/v1/models': '模型目录', '/v1/media/jobs': '模型任务',
};

function splitSet(value: unknown) {
  return String(value || '').split(/[,，\n\r]+/).map(item => item.trim()).filter(Boolean);
}

function statusColor(status: string) {
  if (status === 'active') return 'green';
  if (status === 'revoked') return 'grey';
  return 'orange';
}

export default function ModelAuthorizationWorkspace() {
  const user = useAuthStore(state => state.user);
  const platform = isPlatformAdminRole(user?.role);
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [orgID, setOrgID] = useState<number | undefined>(user?.org_id);
  const [contract, setContract] = useState<AuthorizationContract | null>(null);
  const [policies, setPolicies] = useState<ModelAuthorization[]>([]);
  const [selectedRef, setSelectedRef] = useState('');
  const [status, setStatus] = useState('');
  const [loading, setLoading] = useState(false);
  const [mutating, setMutating] = useState(false);
  const [error, setError] = useState('');
  const [sheetOpen, setSheetOpen] = useState(false);
  const [formApi, setFormApi] = useState<any>(null);

  const selected = policies.find(item => item.authorization_ref === selectedRef) || policies[0] || null;

  useEffect(() => {
    api.getModelAuthorizationContract().then(setContract).catch((loadError: any) => setError(loadError?.message || '授权合同不可用'));
  }, []);

  useEffect(() => {
    if (!platform) return;
    loadAllPages<Organization>((page, size) => api.getOrganizations(page, size))
      .then(items => {
        setOrganizations(items);
        setOrgID(current => current || items[0]?.id);
      })
      .catch((loadError: any) => setError(loadError?.message || '组织列表不可用'));
  }, [platform]);

  const load = useCallback(async () => {
    if (platform && !orgID) return;
    setLoading(true);
    setError('');
    try {
      const data = await api.getModelAuthorizations(platform ? orgID : undefined, status);
      const next = (data?.data || []) as ModelAuthorization[];
      setPolicies(next);
      setSelectedRef(current => next.some(item => item.authorization_ref === current) ? current : next[0]?.authorization_ref || '');
    } catch (loadError: any) {
      setPolicies([]);
      setError(loadError?.message || '无法加载模型调用授权。');
    } finally {
      setLoading(false);
    }
  }, [orgID, platform, status]);

  useEffect(() => { void load(); }, [load]);

  const stats = useMemo(() => ({
    active: policies.filter(item => item.status === 'active').length,
    draft: policies.filter(item => item.status === 'draft').length,
    deny: policies.filter(item => item.status === 'active' && item.effect === 'deny').length,
  }), [policies]);

  const create = async (values: Record<string, any>) => {
    if (platform && !orgID) {
      Toast.error('请先选择目标组织');
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
        authorization_ref: String(values.authorization_ref || '').trim().toLowerCase(),
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
    { title: '修订', dataIndex: 'revision', width: 80, render: (value: number) => `r${value}` },
  ];

  return (
    <>
      <Banner type="info" description="这是 UniRouter 的模型调用 PEP：显式拒绝优先，多个主体的允许范围取交集；它只能缩小现有访问凭证、项目、人员和 Agent 能力，不能授予 Tool 或 MCP 权限。" />
      <div className="ur-modelgov-summary">
        <div><Typography.Text type="tertiary">生效策略</Typography.Text><strong>{stats.active}</strong></div>
        <div><Typography.Text type="tertiary">待激活草稿</Typography.Text><strong>{stats.draft}</strong></div>
        <div><Typography.Text type="tertiary">显式拒绝</Typography.Text><strong>{stats.deny}</strong></div>
      </div>
      <div className="ur-context-explorer ur-governance-explorer ur-model-authorization-workspace">
        <main className="ur-explorer-panel">
          <div className="ur-panel-title">
            <div>
              <Typography.Title heading={5} style={{ margin: 0 }}>模型调用授权策略</Typography.Title>
              <Typography.Text className="ur-panel-hint">{contract?.contract_version || '正在加载合同'} · 当前环境 {contract?.environment || '-'}</Typography.Text>
            </div>
            <Space>
              {platform && <Select aria-label="授权目标组织" value={orgID} onChange={value => setOrgID(Number(value))} optionList={organizations.map(org => ({ label: `${org.name}（ID ${org.id}）`, value: org.id }))} style={{ width: 210 }} />}
              <Select aria-label="授权状态筛选" value={status} onChange={value => setStatus(String(value || ''))} showClear placeholder="全部状态" optionList={[{ label: '草稿', value: 'draft' }, { label: '生效', value: 'active' }, { label: '已撤销', value: 'revoked' }]} style={{ width: 120 }} />
              <Button icon={<IconRefresh />} loading={loading} onClick={() => void load()}>刷新</Button>
              <Button icon={<IconPlus />} type="primary" disabled={platform && !orgID} onClick={() => setSheetOpen(true)}>新增授权</Button>
            </Space>
          </div>
          {error ? <EmptyState title="授权策略加载失败" description={error} actions={<Button type="primary" onClick={() => void load()}>重试</Button>} minHeight={320} /> : (
            <div className="ur-table-shell"><Table rowKey="authorization_ref" columns={columns} dataSource={policies} loading={loading} pagination={false}
              empty={<EmptyState title="暂无授权策略" description="无策略时保持现有访问凭证、项目模型集和 Agent 能力规则；创建并激活后只会进一步收窄。" minHeight={300} />} /></div>
          )}
        </main>
        <aside className="ur-explorer-panel ur-explorer-detail" aria-label="模型调用授权检查器">
          <div className="ur-detail-header"><div><Typography.Text className="ur-panel-hint">Authorization Inspector</Typography.Text><Typography.Title heading={5} style={{ margin: 0 }}>适用范围与操作</Typography.Title></div><Tag color={statusColor(selected?.status || '')}>{selected?.status === 'active' ? '生效' : selected?.status === 'draft' ? '草稿' : selected ? '已撤销' : '未选择'}</Tag></div>
          {selected ? <>
            <dl className="ur-financial-truth-detail">
              <dt>策略标识</dt><dd className="ur-mono-break">{selected.authorization_ref}</dd>
              <dt>组织</dt><dd>#{selected.org_id}</dd>
              <dt>主体</dt><dd>{subjectLabels[selected.subject_type] || selected.subject_type} · {selected.subject_ref}</dd>
              <dt>效果</dt><dd>{selected.effect === 'deny' ? '显式拒绝（最高优先级）' : '允许范围（与其他主体求交集）'}</dd>
              <dt>环境</dt><dd>{selected.environment}</dd>
              <dt>模型</dt><dd>{selected.allowed_models.join('、')}</dd>
              <dt>端点</dt><dd>{selected.allowed_endpoints.map(item => endpointLabels[item] || item).join('、')}</dd>
              <dt>有效期</dt><dd>{selected.not_before || selected.expires_at ? `${selected.not_before || '立即'} → ${selected.expires_at || '持续'}` : '持续（直至撤销）'}</dd>
              <dt>修订</dt><dd>r{selected.revision}</dd>
              <dt>理由</dt><dd>{selected.reason}</dd>
              <dt>Tool/MCP 权限</dt><dd>无</dd>
              <dt>运行时变更</dt><dd>none（请求时只读判定）</dd>
            </dl>
            <div className="ur-governance-actions">
              {selected.status === 'draft' && <Popconfirm title="激活该收窄策略？" content="激活后匹配请求将立即执行拒绝优先与允许范围交集。" onConfirm={() => void transition('授权策略已激活', () => api.activateModelAuthorization(selected.authorization_ref, selected.revision, platform ? selected.org_id : undefined))}><Button type="primary" loading={mutating}>激活</Button></Popconfirm>}
              {selected.status === 'active' && <Popconfirm title="撤销该策略？" content="撤销只移除这一层收窄，不会扩大超过基础访问凭证、项目模型集或 Agent 能力。" onConfirm={() => void transition('授权策略已撤销', () => api.revokeModelAuthorization(selected.authorization_ref, selected.revision, platform ? selected.org_id : undefined))}><Button type="danger" loading={mutating}>撤销</Button></Popconfirm>}
            </div>
          </> : <EmptyState title="选择一条授权策略" description="右侧固定显示主体、环境、模型、端点、理由与生命周期操作。" minHeight={300} />}
        </aside>
      </div>

      <SideSheet title="新增模型调用授权" visible={sheetOpen} onCancel={() => setSheetOpen(false)} width={560}>
        <Banner type="warning" description="草稿不会影响请求；激活后只会收窄现有权限。主体 ID 必须属于当前组织，Agent 使用已约定的 Agent 标识。" style={{ marginBottom: 16 }} />
        <Form getFormApi={(next: any) => setFormApi(next)} onSubmit={create} initValues={{ environment: contract?.environment || '*', effect: 'allow', allowed_endpoints: contract?.endpoints || [] }}>
          <Form.Input field="authorization_ref" label="策略标识" placeholder="mca_project_company_chat" rules={[{ required: true, message: '请输入策略标识' }]} />
          <div className="ur-modelgov-form-grid">
            <Form.Select field="subject_type" label="主体类型" optionList={(contract?.subjects || ['api_key', 'project', 'user', 'agent']).map(value => ({ label: subjectLabels[value] || value, value }))} rules={[{ required: true, message: '请选择主体类型' }]} />
            <Form.Input field="subject_ref" label="主体 ID / Agent 标识" rules={[{ required: true, message: '请输入主体标识' }]} />
          </div>
          <div className="ur-modelgov-form-grid">
            <Form.Input field="environment" label="环境" placeholder="production 或 *" rules={[{ required: true, message: '请输入环境' }]} />
            <Form.Select field="effect" label="效果" optionList={[{ label: '允许范围（交集）', value: 'allow' }, { label: '显式拒绝', value: 'deny' }]} />
          </div>
          <Form.TextArea field="allowed_models" label="模型范围" placeholder={'company-chat\ngpt-4o'} rows={5} rules={[{ required: true, message: '请输入模型范围' }]} />
          <Form.Select field="allowed_endpoints" label="端点范围" multiple optionList={(contract?.endpoints || []).map(value => ({ label: endpointLabels[value] || value, value }))} rules={[{ required: true, message: '请选择端点范围' }]} />
          <Form.TextArea field="reason" label="业务理由" rows={4} rules={[{ required: true, message: '请输入业务理由' }]} />
          <div className="ur-form-actions"><Button onClick={() => setSheetOpen(false)}>取消</Button><Button type="primary" loading={mutating} onClick={() => formApi?.submitForm()}>创建草稿</Button></div>
        </Form>
      </SideSheet>
    </>
  );
}
