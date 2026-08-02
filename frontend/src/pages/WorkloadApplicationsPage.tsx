import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import {
  Banner,
  Button,
  Form,
  Modal,
  Select,
  SideSheet,
  Table,
  Tag,
  Toast,
  Typography,
} from '@douyinfe/semi-ui';
import { IconArchive, IconKeyStroked, IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { useAuthStore } from '../store/auth';
import { isPlatformAdminRole } from '../navigation';
import { EmptyState, MetricTile, PageHeader, Surface } from '../ui';
import { formatDateTime } from '../utils/formatDate';
import { loadAllPages } from '../utils/loadAllPages';

const { Text, Title, Paragraph } = Typography;

interface Organization { id: number; name: string }
interface Project { id: number; org_id: number; name: string; status?: string }
interface UserRecord { id: number; org_id: number; username: string; status?: string }

interface ClientProfile {
  profile_id: string;
  display_name: string;
  client_family: string;
  api_style: string;
  supported_endpoints: string[];
  runtime_state: string;
  evidence_level: string;
  recommended_for_poc: boolean;
  configuration_note: string;
}

interface WorkloadContract {
  contract_version: string;
  readiness_version: string;
  client_profiles: ClientProfile[];
  identity_authority: string;
  tool_or_mcp_authority: string;
  legacy_credentials: string;
  runtime_mutation: string;
}

interface WorkloadApplication {
  id: number;
  org_id: number;
  project_id: number;
  project_name: string;
  project_status: string;
  application_ref: string;
  display_name: string;
  kind: string;
  environment: string;
  identity_mode: string;
  client_profile: string;
  owner_team: string;
  description: string;
  status: string;
  revision: number;
  credential_count: number;
  active_credential_count: number;
  created_at: string;
  updated_at: string;
}

interface Credential {
  id: number;
  name: string;
  key_prefix: string;
  status: string;
  key_type: string;
  allowed_models: string;
  rpm_limit: number;
  rps_limit?: number;
  qps_limit?: number;
  created_at: string;
}

interface LegacyCredential extends Credential {
  org_id: number;
  project_id: number;
  project_name?: string;
  workload_application_ref?: string;
  purpose?: string;
}

interface ReadinessAxis {
  key: string;
  state: string;
  reason_code: string;
  description: string;
  fix_route?: string;
  evidence: string[];
}

interface ReadinessResult {
  contract_version: string;
  overall: string;
  endpoint?: string;
  model?: string;
  axes: ReadinessAxis[];
  runtime_mutation: string;
}

const kindOptions = [
  { value: 'service', label: '服务应用' },
  { value: 'agent', label: 'Agent' },
  { value: 'workflow', label: '工作流' },
  { value: 'interactive_client', label: '交互式客户端' },
  { value: 'integration_adapter', label: '隔离集成适配器' },
];

const kindLabels = Object.fromEntries(kindOptions.map(option => [option.value, option.label]));
const identityLabels: Record<string, string> = {
  service_credential: '服务身份',
  personal_development: '个人开发身份',
};

const readinessLabels: Record<string, string> = {
  application: '应用状态',
  project: '项目状态',
  client_profile: '客户端协议',
  application_credential: '调用凭证',
  model_authorization: '模型调用授权',
  limit_policy: '共享限额',
  model_governance: '模型治理',
  upstream_route: '上游可用路由',
};

function statusColor(status?: string) {
  if (status === 'active' || status === 'ready') return 'green';
  if (status === 'disabled' || status === 'blocked' || status === 'archived') return 'red';
  if (status === 'needs_input' || status === 'context_required') return 'orange';
  return 'grey';
}

function statusText(status?: string) {
  const labels: Record<string, string> = {
    active: '启用', disabled: '停用', archived: '已归档', ready: '可调用',
    blocked: '阻断', needs_input: '待选择上下文', context_required: '待补充',
    not_applicable: '不适用', verified: '已验证', protocol_compatible: '协议兼容',
    unsupported: '未支持', runtime_supported: '运行时支持', contract_only: '仅合同',
    adapter_required: '需要隔离适配器',
  };
  return labels[status || ''] || status || '未知';
}

function generatedQuickstart(profile: ClientProfile | undefined, endpoint: string, model: string, configuredBaseURL: string) {
  const gatewayOrigin = (configuredBaseURL || 'https://<configure-a-gateway-endpoint>').replace(/\/+$/, '');
  const apiBaseURL = `${gatewayOrigin}/v1`;
  const selectedModel = model || '<approved-model-alias>';
  const path = endpoint || '/v1/chat/completions';
  if (!profile) return '# 先选择客户端 Profile';
  if (profile.runtime_state === 'adapter_required') {
    return '# 此 Profile 需要独立部署适配器；不能在 UniRouter 网关进程内直接启用。';
  }
  if (profile.api_style === 'openai_responses') {
    return '# /v1/responses 当前仅冻结 Endpoint Profile，未声明运行时支持。';
  }
  if (profile.client_family === 'openclaw') {
    return JSON.stringify({
      provider: 'unirouter', baseUrl: apiBaseURL, apiKey: '${UNIROUTER_API_KEY}',
      api: 'openai-completions', models: [{ id: selectedModel, name: selectedModel }],
    }, null, 2);
  }
  if (profile.client_family === 'tencent_workbuddy') {
    return `接口 URL: ${gatewayOrigin}${path}\nAPI Key: \${UNIROUTER_API_KEY}\n模型: ${selectedModel}`;
  }
  return `import os\nfrom openai import OpenAI\n\nclient = OpenAI(\n    api_key=os.environ["UNIROUTER_API_KEY"],\n    base_url="${apiBaseURL}",\n)\nresponse = client.chat.completions.create(\n    model="${selectedModel}",\n    messages=[{"role": "user", "content": "ping"}],\n)`;
}

export default function WorkloadApplicationsPage() {
  const currentUser = useAuthStore(state => state.user);
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const platformAdmin = isPlatformAdminRole(currentUser?.role);
  const requestVersion = useRef(0);
  const [contract, setContract] = useState<WorkloadContract | null>(null);
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [users, setUsers] = useState<UserRecord[]>([]);
  const [applications, setApplications] = useState<WorkloadApplication[]>([]);
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [legacyCredentials, setLegacyCredentials] = useState<LegacyCredential[]>([]);
  const [models, setModels] = useState<string[]>([]);
  const [gatewayEndpoint, setGatewayEndpoint] = useState<{ base_url: string; observed_status?: string } | null>(null);
  const [selectedOrgID, setSelectedOrgID] = useState<number | undefined>(currentUser?.org_id || undefined);
  const [selectedProjectID, setSelectedProjectID] = useState<number | undefined>();
  const [selected, setSelected] = useState<WorkloadApplication | null>(null);
  const [selectedModel, setSelectedModel] = useState('');
  const [selectedEndpoint, setSelectedEndpoint] = useState('');
  const [readiness, setReadiness] = useState<ReadinessResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [readinessLoading, setReadinessLoading] = useState(false);
  const [applicationDrawer, setApplicationDrawer] = useState(false);
  const [credentialDrawer, setCredentialDrawer] = useState(false);
  const [editing, setEditing] = useState<WorkloadApplication | null>(null);
  const [applicationForm, setApplicationForm] = useState<any>(null);
  const [applicationOrgID, setApplicationOrgID] = useState<number | undefined>(currentUser?.org_id || undefined);
  const [credentialForm, setCredentialForm] = useState<any>(null);
  const [createdSecret, setCreatedSecret] = useState<{ key: string; name: string } | null>(null);

  const profiles = contract?.client_profiles || [];
  const selectedProfile = profiles.find(profile => profile.profile_id === selected?.client_profile);
  const scopedProjects = projects.filter(project => !selectedOrgID || project.org_id === selectedOrgID);
  const scopedUsers = users.filter(user => !selected?.org_id || user.org_id === selected.org_id);
  const showLegacyFirst = searchParams.get('section') === 'credentials';

  const loadReferenceData = async () => {
    const fallbackOrgs = currentUser?.org_id
      ? [{ id: currentUser.org_id, name: currentUser.org_name || `组织 #${currentUser.org_id}` }]
      : [];
    const [contractResult, orgResult, userResult, endpointResult] = await Promise.all([
      api.getWorkloadApplicationContract(),
      loadAllPages<Organization>((page, size) => api.getOrganizations(page, size)).catch(() => fallbackOrgs),
      loadAllPages<UserRecord>((page, size) => api.getUsers(page, size)).catch(() => []),
      api.getGatewayEndpoints().catch(() => ({ data: [] })),
    ]);
    const effectiveOrgs = orgResult.length ? orgResult : fallbackOrgs;
    const projectResult = platformAdmin
      ? (await Promise.all(effectiveOrgs.map(org => loadAllPages<Project>((page, size) => api.getProjects(page, size, { status: 'active', org_id: org.id })).catch(() => [])))).flat()
      : await loadAllPages<Project>((page, size) => api.getProjects(page, size, { status: 'active' })).catch(() => []);
    setContract(contractResult);
    setOrganizations(effectiveOrgs);
    setProjects(projectResult);
    setUsers(userResult);
    const compatibleEndpoints = (endpointResult?.data || []).filter((item: any) =>
      item.status === 'active' && item.endpoint_profile === 'endpoint.openai-chat.v1',
    );
    const recommendedEndpoint = compatibleEndpoints.find((item: any) => item.observed_status === 'ready') || compatibleEndpoints[0];
    setGatewayEndpoint(recommendedEndpoint || null);
    if (!selectedOrgID) setSelectedOrgID(currentUser?.org_id || effectiveOrgs[0]?.id);
  };

  const loadApplications = async () => {
    const version = ++requestVersion.current;
    setLoading(true);
    setLoadError('');
    try {
      const [applicationResult, keyItems] = await Promise.all([
        api.getWorkloadApplications({
          org_id: platformAdmin ? selectedOrgID : undefined,
          project_id: selectedProjectID,
        }),
        loadAllPages<LegacyCredential>((page, size) => api.getKeys(page, size, selectedProjectID ? { project_id: selectedProjectID } : undefined)),
      ]);
      if (version !== requestVersion.current) return;
      const items = (applicationResult?.data || []) as WorkloadApplication[];
      setApplications(items);
      setLegacyCredentials(keyItems.filter(key =>
        (!selectedOrgID || key.org_id === selectedOrgID) && !key.workload_application_ref && !key.purpose,
      ));
      setSelected(current => items.find(item => item.application_ref === current?.application_ref) || items[0] || null);
    } catch (error: any) {
      if (version === requestVersion.current) setLoadError(error?.message || '无法加载应用接入工作区。');
    } finally {
      if (version === requestVersion.current) setLoading(false);
    }
  };

  useEffect(() => {
    void loadReferenceData().catch((error: any) => setLoadError(error?.message || '无法读取应用接入合同。'));
  }, []);

  useEffect(() => { void loadApplications(); }, [selectedOrgID, selectedProjectID]);

  useEffect(() => {
    if (!selected) {
      setCredentials([]);
      setModels([]);
      setReadiness(null);
      return;
    }
    setSelectedModel('');
    const profile = profiles.find(item => item.profile_id === selected.client_profile);
    setSelectedEndpoint(profile?.supported_endpoints?.[0] || '');
    void Promise.all([
      api.getWorkloadApplicationCredentials(selected.application_ref, platformAdmin ? selected.org_id : undefined),
      loadAllPages<any>((page, size) => api.getModelCatalog(page, size, { project_id: selected.project_id, ...(platformAdmin ? { org_id: selected.org_id } : {}) })),
    ]).then(([credentialResult, catalogResult]) => {
      setCredentials(credentialResult?.data || []);
      const modelItems = Array.isArray(catalogResult) ? catalogResult : [];
      setModels(Array.from(new Set(modelItems.map((item: any) => item.model || item.alias || item.name).filter(Boolean))));
    }).catch(() => {
      setCredentials([]);
      setModels([]);
    });
  }, [selected?.application_ref, selected?.revision, profiles.length]);

  useEffect(() => {
    if (!selected) return;
    setReadinessLoading(true);
    api.getWorkloadApplicationReadiness(selected.application_ref, {
      org_id: platformAdmin ? selected.org_id : undefined,
      model: selectedModel || undefined,
      endpoint: selectedEndpoint || undefined,
    }).then(setReadiness).catch((error: any) => {
      setReadiness({ contract_version: 'unavailable', overall: 'blocked', axes: [{
        key: 'readiness', state: 'blocked', reason_code: 'readiness_unavailable',
        description: error?.message || '无法读取就绪度。', evidence: [],
      }], runtime_mutation: 'none' });
    }).finally(() => setReadinessLoading(false));
  }, [selected?.application_ref, selected?.revision, selectedModel, selectedEndpoint]);

  const openCreate = () => {
    setEditing(null);
    setApplicationDrawer(true);
    setTimeout(() => applicationForm?.reset(), 0);
  };

  const openEdit = () => {
    if (!selected) return;
    setEditing(selected);
    setApplicationDrawer(true);
  };

  useEffect(() => {
    if (!applicationDrawer || !applicationForm) return;
    applicationForm.reset();
    if (editing) {
      applicationForm.setValues({
        display_name: editing.display_name,
        environment: editing.environment,
        client_profile: editing.client_profile,
        owner_team: editing.owner_team,
        description: editing.description,
        status: editing.status,
      });
      return;
    }
    const orgID = selectedOrgID || currentUser?.org_id || organizations[0]?.id;
    setApplicationOrgID(orgID);
    applicationForm.setValues({
      org_id: orgID,
      project_id: projects.find(project => project.org_id === orgID)?.id,
      kind: 'agent',
      identity_mode: 'service_credential',
      environment: 'poc',
      client_profile: 'client.openai-sdk.chat.v1',
      status: 'active',
    });
  }, [applicationDrawer, applicationForm, editing, organizations, projects, selectedOrgID]);

  const submitApplication = async (values: Record<string, any>) => {
    try {
      if (editing) {
        await api.updateWorkloadApplication(editing.application_ref, {
          org_id: platformAdmin ? editing.org_id : 0,
          expected_revision: editing.revision,
          display_name: values.display_name,
          environment: values.environment,
          client_profile: values.client_profile,
          owner_team: values.owner_team || '',
          description: values.description || '',
          status: values.status,
        });
        Toast.success('应用已更新');
      } else {
        await api.createWorkloadApplication({
          org_id: platformAdmin ? Number(values.org_id) : Number(currentUser?.org_id || 0),
          project_id: Number(values.project_id),
          application_ref: values.application_ref,
          display_name: values.display_name,
          kind: values.kind,
          environment: values.environment,
          identity_mode: values.identity_mode,
          client_profile: values.client_profile,
          owner_team: values.owner_team || '',
          description: values.description || '',
          status: values.status,
        });
        Toast.success('应用已创建');
      }
      setApplicationDrawer(false);
      setEditing(null);
      await loadApplications();
    } catch (error: any) {
      Toast.error(error?.message || '保存应用失败');
    }
  };

  const submitCredential = async (values: Record<string, any>) => {
    if (!selected) return;
    const keyType = selected.identity_mode === 'personal_development' ? 'personal' : 'service';
    try {
      const result = await api.createKey({
        org_id: selected.org_id,
        project_id: selected.project_id,
        workload_application_ref: selected.application_ref,
        key_type: keyType,
        owner_user_id: keyType === 'personal' ? Number(values.owner_user_id) : 0,
        name: values.name,
        allowed_ips: values.allowed_ips || '',
        allowed_models: Array.isArray(values.allowed_models) ? values.allowed_models.join(',') : '',
        rpm_limit: Number(values.rpm_limit || 60),
        rps_limit: Number(values.rps_limit || 10),
        policy_labels: String(values.policy_labels || '').split(',').map(value => value.trim()).filter(Boolean),
      });
      if (result?.key) setCreatedSecret({ key: result.key, name: values.name });
      setCredentialDrawer(false);
      Toast.success('应用凭证已创建并原子绑定');
      await loadApplications();
    } catch (error: any) {
      Toast.error(error?.message || '创建凭证失败');
    }
  };

  const archiveSelected = () => {
    if (!selected) return;
    Modal.confirm({
      title: `归档 ${selected.display_name}？`,
      content: '归档会原子停用该应用仍启用的调用凭证，但不会删除请求凭证、账本、审计或历史证据。',
      onOk: async () => {
        await api.archiveWorkloadApplication(selected.application_ref, {
          org_id: platformAdmin ? selected.org_id : undefined,
          expected_revision: selected.revision,
        });
        Toast.success('应用已归档，历史执行证据保持不变');
        await loadApplications();
      },
    });
  };

  const columns = useMemo(() => [
    {
      title: '应用 / Agent', dataIndex: 'display_name',
      render: (_: string, record: WorkloadApplication) => (
        <div className="ur-table-primary"><strong>{record.display_name}</strong><code>{record.application_ref}</code></div>
      ),
    },
    { title: '类型', dataIndex: 'kind', width: 130, render: (value: string) => kindLabels[value] || value },
    { title: '项目', dataIndex: 'project_name', width: 150 },
    { title: '环境', dataIndex: 'environment', width: 90 },
    {
      title: '凭证', width: 92,
      render: (_: unknown, record: WorkloadApplication) => `${record.active_credential_count}/${record.credential_count}`,
    },
    { title: '状态', dataIndex: 'status', width: 90, render: (value: string) => <Tag color={statusColor(value)}>{statusText(value)}</Tag> },
    { title: '更新时间', dataIndex: 'updated_at', width: 170, render: formatDateTime },
  ], []);

  const credentialColumns = [
    { title: '名称', dataIndex: 'name' },
    { title: '身份', dataIndex: 'key_type', width: 100, render: (value: string) => value === 'personal' ? '个人' : '服务' },
    { title: '前缀', dataIndex: 'key_prefix', render: (value: string) => <code>{value}***</code> },
    { title: '模型范围', dataIndex: 'allowed_models', render: (value: string) => value || '继承项目可用范围' },
    { title: '限速', render: (_: unknown, record: Credential) => `RPM ${record.rpm_limit} / RPS ${record.rps_limit ?? record.qps_limit ?? '—'}` },
    { title: '状态', dataIndex: 'status', width: 90, render: (value: string) => <Tag color={statusColor(value)}>{statusText(value)}</Tag> },
  ];

  const legacyColumns = [
    { title: '名称', dataIndex: 'name' },
    { title: '项目', dataIndex: 'project_name' },
    { title: '前缀', dataIndex: 'key_prefix', render: (value: string) => <code>{value}***</code> },
    { title: '状态', dataIndex: 'status', render: (value: string) => <Tag color={statusColor(value)}>{statusText(value)}</Tag> },
    { title: '兼容身份', render: () => <Tag color="orange">legacy_unbound</Tag> },
  ];

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="访问与安全"
        title="应用与 Agent 接入"
        description="先登记调用方，再把凭证、客户端协议、模型授权与共享限额组合成可解释的有效权限；不授予 Tool 或 MCP 权限。"
        actions={<Button icon={<IconPlus />} type="primary" onClick={openCreate}>登记应用 / Agent</Button>}
      />

      <Banner
        type="info"
        description="现有未绑定凭证继续兼容，不自动迁移或改变请求行为；新凭证应绑定应用身份。就绪度是只读组合判断，不会修改路由或运行时。"
        style={{ marginBottom: 14 }}
      />

      <div className="ur-context-explorer ur-workload-explorer">
        <main className="ur-explorer-panel">
          <div className="ur-workload-filterbar">
            {platformAdmin && (
              <Select
                aria-label="筛选组织"
                value={selectedOrgID}
                optionList={organizations.map(org => ({ value: org.id, label: org.name }))}
                onChange={value => { setSelectedOrgID(Number(value)); setSelectedProjectID(undefined); }}
                style={{ width: 220 }}
              />
            )}
            <Select
              aria-label="筛选项目"
              placeholder="全部项目"
              value={selectedProjectID}
              showClear
              optionList={scopedProjects.map(project => ({ value: project.id, label: project.name }))}
              onChange={value => setSelectedProjectID(value ? Number(value) : undefined)}
              style={{ width: 220 }}
            />
            <Button icon={<IconRefresh />} onClick={() => void loadApplications()}>刷新</Button>
          </div>

          <div className="ur-kpi-grid ur-workload-kpis">
            <MetricTile label="登记调用方" value={applications.length} tone="blue" />
            <MetricTile label="启用" value={applications.filter(item => item.status === 'active').length} tone="green" />
            <MetricTile label="活跃应用凭证" value={applications.reduce((sum, item) => sum + item.active_credential_count, 0)} tone="violet" />
            <MetricTile label="旧未绑定凭证" value={legacyCredentials.length} tone={legacyCredentials.length ? 'amber' : 'slate'} />
          </div>

          <Surface
            title="调用方目录"
            description="单击一行，右侧会立即显示客户端能力、有效权限与修复入口。"
          >
            <div className="ur-table-shell">
              <Table
                rowKey="application_ref"
                columns={columns}
                dataSource={applications}
                loading={loading}
                pagination={false}
                scroll={{ x: 900 }}
                empty={loadError
                  ? <EmptyState title="应用接入加载失败" description={loadError} actions={<Button onClick={() => void loadApplications()}>重新加载</Button>} minHeight={220} />
                  : <EmptyState title="尚未登记应用或 Agent" description="登记后再创建绑定凭证，避免继续累积匿名共享密钥。" minHeight={220} />}
                onRow={(record?: WorkloadApplication) => ({
                  onClick: () => record && setSelected(record),
                  'aria-selected': record?.application_ref === selected?.application_ref,
                })}
              />
            </div>
          </Surface>

          {selected && (
            <Surface
              title={`${selected.display_name} · 已绑定凭证`}
              description="凭证只证明调用身份；最终可调用模型仍由项目、授权、限额、模型治理和路由共同决定。"
              actions={selected.status !== 'archived' ? <Button icon={<IconKeyStroked />} onClick={() => setCredentialDrawer(true)}>创建绑定凭证</Button> : undefined}
            >
              <Table rowKey="id" columns={credentialColumns} dataSource={credentials} pagination={false} empty={<EmptyState title="暂无绑定凭证" minHeight={150} />} />
            </Surface>
          )}

          {(showLegacyFirst || legacyCredentials.length > 0) && (
            <Surface
              title="旧凭证兼容区"
              description="这些凭证保持 legacy_unbound，不自动绑定应用；可在后续人工核对身份后绑定或逐步轮换。"
              actions={<Button onClick={() => navigate('/keys')}>打开旧凭证维护页</Button>}
            >
              <Table rowKey="id" columns={legacyColumns} dataSource={legacyCredentials} pagination={false} empty={<EmptyState title="没有旧未绑定凭证" minHeight={130} />} />
            </Surface>
          )}
        </main>

        <aside className="ur-explorer-panel ur-explorer-detail" aria-label="应用接入检查器">
          {selected ? (
            <>
              <div className="ur-detail-header">
                <div><Text className="ur-panel-hint">Effective Access Inspector</Text><Title heading={5} style={{ margin: 0 }}>{selected.display_name}</Title></div>
                <Tag color={statusColor(readiness?.overall || selected.status)}>{statusText(readiness?.overall || selected.status)}</Tag>
              </div>
              <dl className="ur-financial-truth-detail">
                <dt>应用身份</dt><dd className="ur-mono-break">{selected.application_ref}</dd>
                <dt>项目 / 环境</dt><dd>{selected.project_name} / {selected.environment}</dd>
                <dt>身份模式</dt><dd>{identityLabels[selected.identity_mode] || selected.identity_mode}</dd>
                <dt>负责人</dt><dd>{selected.owner_team || '未填写'}</dd>
                <dt>客户端</dt><dd>{selectedProfile?.display_name || selected.client_profile}</dd>
                <dt>兼容证据</dt><dd><Tag color={statusColor(selectedProfile?.evidence_level)}>{statusText(selectedProfile?.evidence_level)}</Tag></dd>
                <dt>工具 / MCP 权限</dt><dd>无</dd>
                <dt>归档影响</dt><dd>停用绑定凭证，不删除历史证据</dd>
              </dl>

              <Title heading={6}>调用上下文</Title>
              <div className="ur-workload-context-selectors">
                <Select aria-label="检查端点" value={selectedEndpoint} optionList={(selectedProfile?.supported_endpoints || []).map(value => ({ value, label: value }))} onChange={value => setSelectedEndpoint(String(value || ''))} style={{ width: '100%' }} />
                <Select aria-label="检查模型" placeholder="选择模型后验证完整链路" value={selectedModel || undefined} showClear filter optionList={models.map(value => ({ value, label: value }))} onChange={value => setSelectedModel(String(value || ''))} style={{ width: '100%' }} />
              </div>

              <Title heading={6}>有效权限链</Title>
              <div className="ur-readiness-axis-list" aria-busy={readinessLoading}>
                {(readiness?.axes || []).map(axis => (
                  <div className="ur-readiness-axis" key={axis.key}>
                    <div><strong>{readinessLabels[axis.key] || axis.key}</strong><Tag color={statusColor(axis.state)}>{statusText(axis.state)}</Tag></div>
                    <Text>{axis.description}</Text>
                    <Text type="tertiary" size="small">{axis.reason_code}</Text>
                    {axis.fix_route && <Button size="small" theme="borderless" onClick={() => navigate(axis.fix_route!)}>前往修复</Button>}
                  </div>
                ))}
              </div>

              <Title heading={6}>接入示例</Title>
              <Paragraph type={gatewayEndpoint?.observed_status === 'ready' ? 'success' : 'warning'}>
                {gatewayEndpoint
                  ? `Gateway Endpoint：${gatewayEndpoint.base_url} · ${gatewayEndpoint.observed_status === 'ready' ? '已独立验证' : '尚未完成独立验证'}`
                  : '尚未配置 Gateway Endpoint；示例保持不可直接调用的占位地址。'}
                {' '}Base URL 不会从浏览器当前地址推断。
              </Paragraph>
              <pre className="ur-code-block">{generatedQuickstart(selectedProfile, selectedEndpoint, selectedModel, gatewayEndpoint?.base_url || '')}</pre>
              <div className="ur-governance-actions">
                <Button onClick={openEdit} disabled={selected.status === 'archived'}>编辑应用</Button>
                <Button icon={<IconKeyStroked />} onClick={() => setCredentialDrawer(true)} disabled={selected.status !== 'active'}>创建凭证</Button>
                <Button icon={<IconArchive />} type="danger" onClick={archiveSelected} disabled={selected.status === 'archived'}>归档</Button>
              </div>
            </>
          ) : (
            <EmptyState title="选择或登记调用方" description="右侧固定展示身份、客户端兼容性、有效权限链和接入示例，无需额外点击展开。" minHeight={320} />
          )}
        </aside>
      </div>

      <SideSheet title={editing ? '编辑应用 / Agent' : '登记应用 / Agent'} visible={applicationDrawer} onCancel={() => { setApplicationDrawer(false); setEditing(null); }} width={520}>
        <Form getFormApi={setApplicationForm} onSubmit={submitApplication}>
          {!editing && platformAdmin && <Form.Select field="org_id" label="所属组织" optionList={organizations.map(org => ({ value: org.id, label: org.name }))} rules={[{ required: true, message: '请选择组织' }]} onChange={(value: any) => { const orgID = Number(value); setApplicationOrgID(orgID); applicationForm?.setValue('project_id', projects.find(project => project.org_id === orgID)?.id); }} style={{ width: '100%' }} />}
          {!editing && <Form.Select field="project_id" label="所属项目" optionList={projects.filter(project => !applicationOrgID || project.org_id === applicationOrgID).map(project => ({ value: project.id, label: project.name }))} rules={[{ required: true, message: '请选择项目' }]} style={{ width: '100%' }} />}
          {!editing && <Form.Input field="application_ref" label="稳定应用标识" placeholder="app_customer_service_agent" rules={[{ required: true, message: '请输入 app_ 开头的稳定标识' }]} />}
          <Form.Input field="display_name" label="显示名称" rules={[{ required: true, message: '请输入显示名称' }]} />
          {!editing && <Form.Select field="kind" label="调用方类型" optionList={kindOptions} rules={[{ required: true }]} style={{ width: '100%' }} />}
          {!editing && <Form.Select field="identity_mode" label="身份模式" optionList={[{ value: 'service_credential', label: '服务身份' }, { value: 'personal_development', label: '个人开发身份' }]} rules={[{ required: true }]} style={{ width: '100%' }} />}
          <Form.Input field="environment" label="环境标识" placeholder="poc / prod / dev" rules={[{ required: true }]} />
          <Form.Select field="client_profile" label="客户端 Profile" optionList={profiles.map(profile => ({ value: profile.profile_id, label: `${profile.display_name} · ${statusText(profile.evidence_level)}` }))} rules={[{ required: true }]} filter style={{ width: '100%' }} />
          <Form.Input field="owner_team" label="负责团队" placeholder="AI Platform / 客服项目组" />
          <Form.TextArea field="description" label="用途说明" maxCount={1000} />
          <Form.Select field="status" label="状态" optionList={[{ value: 'active', label: '启用' }, { value: 'disabled', label: '停用' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <div className="ur-form-actions"><Button onClick={() => setApplicationDrawer(false)}>取消</Button><Button type="primary" onClick={() => applicationForm?.submitForm()}>保存</Button></div>
        </Form>
      </SideSheet>

      <SideSheet title={`为 ${selected?.display_name || ''} 创建调用凭证`} visible={credentialDrawer} onCancel={() => setCredentialDrawer(false)} width={500}>
        <Banner type="warning" description="完整密钥只显示一次。创建会在同一事务中校验应用身份并完成绑定。" style={{ marginBottom: 16 }} />
        <Form getFormApi={setCredentialForm} onSubmit={submitCredential} initValues={{ rpm_limit: 60, rps_limit: 10 }}>
          <Form.Input field="name" label="凭证名称" placeholder={`${selected?.environment || 'poc'}-${selected?.kind || 'agent'}`} rules={[{ required: true }]} />
          {selected?.identity_mode === 'personal_development' && <Form.Select field="owner_user_id" label="个人所有者" optionList={scopedUsers.map(user => ({ value: user.id, label: user.username }))} rules={[{ required: true, message: '个人开发身份必须选择所有者' }]} style={{ width: '100%' }} />}
          <Form.Select field="allowed_models" label="允许模型（留空继承项目范围）" multiple filter optionList={models.map(value => ({ value, label: value }))} style={{ width: '100%' }} />
          <Form.Input field="allowed_ips" label="允许来源 IP / CIDR" placeholder="留空表示不在此层收窄" />
          <Form.InputNumber field="rpm_limit" label="RPM" min={1} style={{ width: '100%' }} />
          <Form.InputNumber field="rps_limit" label="RPS" min={1} style={{ width: '100%' }} />
          <Form.Input field="policy_labels" label="共享限额标签" placeholder="poc, customer-facing" />
          <div className="ur-form-actions"><Button onClick={() => setCredentialDrawer(false)}>取消</Button><Button type="primary" onClick={() => credentialForm?.submitForm()}>创建并绑定</Button></div>
        </Form>
      </SideSheet>

      <Modal
        title="请立即保存完整 API 密钥"
        visible={!!createdSecret}
        onCancel={() => setCreatedSecret(null)}
        footer={<Button type="primary" onClick={() => setCreatedSecret(null)}>我已安全保存</Button>}
      >
        <Text type="warning">关闭后系统只保留哈希，无法再次查看或找回。</Text>
        <div className="ur-secret-reveal"><strong>{createdSecret?.name}</strong><code>{createdSecret?.key}</code></div>
      </Modal>
    </div>
  );
}
