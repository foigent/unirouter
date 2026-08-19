import { useEffect, useMemo, useState } from 'react';
import { Banner, Button, Form, SideSheet, Table, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState, MetricTile, PageHeader, Surface } from '../ui';
import { formatDateTime } from '../utils/formatDate';

const { Text, Title } = Typography;

interface DeploymentProfile {
  id: number;
  profile_ref: string;
  display_name: string;
  kind: string;
  environment: string;
  ingress_mode: string;
  dns_mode: string;
  outbound_mode: string;
  notes: string;
  status: string;
  desired_revision: number;
  updated_at: string;
}

interface GatewayEndpoint {
  id: number;
  deployment_profile_id: number;
  deployment_profile_ref: string;
  deployment_name: string;
  endpoint_ref: string;
  display_name: string;
  base_url: string;
  visibility: string;
  tls_mode: string;
  certificate_ref: string;
  endpoint_profile: string;
  status: string;
  desired_revision: number;
  observed_status: string;
  observed_base_url: string;
  observed_evidence: Record<string, unknown>;
  observed_at?: string;
  updated_at: string;
}

interface EndpointProfile {
  profile_id: string;
  display_name: string;
  paths: string[];
  runtime_state: string;
  evidence_level: string;
  streaming: boolean;
}

interface DeploymentContract {
  contract_version: string;
  endpoint_profiles: EndpointProfile[];
  tls_private_key_storage: string;
  apply_authority: string;
  runtime_mutation: string;
}

interface ObservedRuntimeStatus {
  status: string;
  generated_at: string;
  runtime_environment?: {
    service?: string;
    environment?: string;
    deployment_mode?: string;
    deployment_role?: string;
    version?: string;
    commit?: string;
    uptime_seconds?: number;
  };
  capacity_health?: {
    database?: { status?: string; latency_ms?: number; reason?: string };
    redis?: { status?: string; latency_ms?: number; reason?: string };
  };
}

const labels: Record<string, string> = {
  operational: '正常', warning: '需关注',
  connected: '可用', local: '本地环境', stable: '稳定实例', 'docker-compose': 'Docker Compose', docker_compose: 'Docker Compose',
  active: '启用', disabled: '停用', ecs_development: 'ECS 开发联调', onprem_compose: '本地 Compose',
  onprem_kubernetes: '本地 Kubernetes', public: '公网', internal: '内网', external_load_balancer: '外部负载均衡',
  bundled_reverse_proxy: '内置反向代理', cluster_ingress: '集群 Ingress', public_dns: '公网 DNS',
  private_dns: '内网 DNS', ip_address: 'IP 地址', customer_managed: '客户管理', direct: '直接出站',
  proxy: '出站代理', restricted: '受限出站', air_gapped: '离线隔离', external_termination: '外部 TLS 终止',
  mounted_certificate: '挂载证书', private_ca: '私有 CA', disabled_tls: '未启用 TLS', unverified: '未独立验证',
  ready: '已验证可用', degraded: '降级', unreachable: '不可达', runtime_supported: '运行时支持',
  contract_only: '仅合同', verified: '已验证', unsupported: '未支持',
};

function label(value?: string) {
  if (value === 'disabled' || value === 'active') return labels[value];
  if (value === 'disabled_tls') return labels.disabled_tls;
  return labels[value || ''] || value || '—';
}

function stateColor(value?: string) {
  if (value === 'active' || value === 'ready' || value === 'runtime_supported' || value === 'verified') return 'green';
  if (value === 'disabled' || value === 'unreachable' || value === 'unsupported') return 'red';
  return 'orange';
}

export default function DeploymentNetworkPage() {
  const [contract, setContract] = useState<DeploymentContract | null>(null);
  const [profiles, setProfiles] = useState<DeploymentProfile[]>([]);
  const [endpoints, setEndpoints] = useState<GatewayEndpoint[]>([]);
  const [selectedProfile, setSelectedProfile] = useState<DeploymentProfile | null>(null);
  const [selectedEndpoint, setSelectedEndpoint] = useState<GatewayEndpoint | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [runtime, setRuntime] = useState<ObservedRuntimeStatus | null>(null);
  const [runtimeError, setRuntimeError] = useState('');
  const [profileDrawer, setProfileDrawer] = useState(false);
  const [endpointDrawer, setEndpointDrawer] = useState(false);
  const [editingProfile, setEditingProfile] = useState<DeploymentProfile | null>(null);
  const [editingEndpoint, setEditingEndpoint] = useState<GatewayEndpoint | null>(null);
  const [profileForm, setProfileForm] = useState<any>(null);
  const [endpointForm, setEndpointForm] = useState<any>(null);

  const load = async () => {
    setLoading(true);
    setLoadError('');
    setRuntimeError('');
    const [registryResult, runtimeResult] = await Promise.allSettled([
      Promise.all([api.getDeploymentNetworkContract(), api.getDeploymentProfiles(), api.getGatewayEndpoints()]),
      api.getRuntimeStatus(),
    ]);
    try {
      if (registryResult.status === 'rejected') throw registryResult.reason;
      const [contractResult, profileResult, endpointResult] = registryResult.value;
      const profileItems = profileResult?.data || [];
      const endpointItems = endpointResult?.data || [];
      setContract(contractResult);
      setProfiles(profileItems);
      setEndpoints(endpointItems);
      setSelectedProfile(current => profileItems.find((item: DeploymentProfile) => item.profile_ref === current?.profile_ref) || profileItems[0] || null);
      setSelectedEndpoint(current => endpointItems.find((item: GatewayEndpoint) => item.endpoint_ref === current?.endpoint_ref) || endpointItems[0] || null);
    } catch (error: any) {
      setLoadError(error?.message || '无法加载附加部署与入口登记。');
    }
    if (runtimeResult.status === 'fulfilled') {
      setRuntime(runtimeResult.value as ObservedRuntimeStatus);
    } else {
      setRuntime(null);
      setRuntimeError(runtimeResult.reason?.message || '无法读取当前运行实例');
    }
    setLoading(false);
  };

  useEffect(() => { void load(); }, []);

  const openProfile = (profile?: DeploymentProfile) => {
    setEditingProfile(profile || null);
    setProfileDrawer(true);
  };

  const openEndpoint = (endpoint?: GatewayEndpoint) => {
    setEditingEndpoint(endpoint || null);
    setEndpointDrawer(true);
  };

  useEffect(() => {
    if (!profileDrawer || !profileForm) return;
    profileForm.reset();
    profileForm.setValues(editingProfile ? {
      display_name: editingProfile.display_name, kind: editingProfile.kind,
      environment: editingProfile.environment, ingress_mode: editingProfile.ingress_mode,
      dns_mode: editingProfile.dns_mode, outbound_mode: editingProfile.outbound_mode,
      notes: editingProfile.notes, status: editingProfile.status,
    } : {
      kind: 'ecs_development', environment: 'poc', ingress_mode: 'bundled_reverse_proxy',
      dns_mode: 'ip_address', outbound_mode: 'direct', status: 'active',
    });
  }, [profileDrawer, profileForm, editingProfile]);

  useEffect(() => {
    if (!endpointDrawer || !endpointForm) return;
    endpointForm.reset();
    endpointForm.setValues(editingEndpoint ? {
      display_name: editingEndpoint.display_name, base_url: editingEndpoint.base_url,
      visibility: editingEndpoint.visibility, tls_mode: editingEndpoint.tls_mode,
      certificate_ref: editingEndpoint.certificate_ref, endpoint_profile: editingEndpoint.endpoint_profile,
      status: editingEndpoint.status,
    } : {
      deployment_profile_ref: selectedProfile?.profile_ref || profiles[0]?.profile_ref,
      visibility: 'public', tls_mode: 'external_termination',
      endpoint_profile: 'endpoint.openai-chat.v1', status: 'active',
    });
  }, [endpointDrawer, endpointForm, editingEndpoint, profiles, selectedProfile]);

  const submitProfile = async (values: Record<string, any>) => {
    try {
      if (editingProfile) {
        await api.updateDeploymentProfile(editingProfile.profile_ref, { ...values, expected_revision: editingProfile.desired_revision });
        Toast.success('附加部署登记已更新；未修改运行时');
      } else {
        await api.createDeploymentProfile(values);
        Toast.success('附加部署登记已创建');
      }
      setProfileDrawer(false);
      await load();
    } catch (error: any) { Toast.error(error?.message || '保存附加部署登记失败'); }
  };

  const submitEndpoint = async (values: Record<string, any>) => {
    try {
      if (editingEndpoint) {
        await api.updateGatewayEndpoint(editingEndpoint.endpoint_ref, { ...values, expected_revision: editingEndpoint.desired_revision });
        Toast.success('网关入口期望状态已更新；未修改 Ingress');
      } else {
        await api.createGatewayEndpoint(values);
        Toast.success('网关入口已登记，当前仍需独立验证');
      }
      setEndpointDrawer(false);
      await load();
    } catch (error: any) { Toast.error(error?.message || '保存网关入口失败'); }
  };

  const profileColumns = useMemo(() => [
    { title: '部署', render: (_: unknown, row: DeploymentProfile) => <div className="ur-table-primary"><strong>{row.display_name}</strong><code>{row.profile_ref}</code></div> },
    { title: '形态', dataIndex: 'kind', render: label },
    { title: '环境', dataIndex: 'environment' },
    { title: '入口', dataIndex: 'ingress_mode', render: label },
    { title: '出站', dataIndex: 'outbound_mode', render: label },
    { title: '状态', dataIndex: 'status', render: (value: string) => <Tag color={stateColor(value)}>{label(value)}</Tag> },
  ], []);

  const endpointColumns = useMemo(() => [
    { title: '网关入口', render: (_: unknown, row: GatewayEndpoint) => <div className="ur-table-primary"><strong>{row.display_name}</strong><code>{row.endpoint_ref}</code></div> },
    { title: '基础地址（Base URL）', dataIndex: 'base_url', render: (value: string) => <code className="ur-mono-break">{value}</code> },
    { title: '可见性', dataIndex: 'visibility', render: label },
    { title: 'TLS', dataIndex: 'tls_mode', render: (value: string) => label(value === 'disabled' ? 'disabled_tls' : value) },
    { title: '期望', dataIndex: 'status', render: (value: string) => <Tag color={stateColor(value)}>{label(value)}</Tag> },
    { title: '观测', dataIndex: 'observed_status', render: (value: string) => <Tag color={stateColor(value)}>{label(value)}</Tag> },
  ], []);

  const endpointProfile = contract?.endpoint_profiles.find(item => item.profile_id === selectedEndpoint?.endpoint_profile);

  return (
    <div className="ur-page ur-page-wide ur-deployment-page">
      <PageHeader
        eyebrow="系统与集成"
        title="部署与网络"
        description="先查看 UniRouter 实际正在运行的实例；如需记录额外的客户部署或访问入口，再维护附加登记。登记不会执行部署。"
        actions={<><Button onClick={() => openProfile()} icon={<IconPlus />}>登记附加部署</Button><Button type="primary" onClick={() => openEndpoint()} icon={<IconPlus />} disabled={!profiles.length}>登记附加入口</Button></>}
      />
      <Banner
        type="info"
        description="上方“当前运行实例”来自服务自身的只读观测；下方登记只保存非秘密的说明与期望状态，不会创建服务器、修改 DNS、重启 Ingress 或保存 TLS 私钥。"
        closeIcon={null}
        style={{ marginBottom: 16 }}
      />
      <Surface title="当前运行实例（只读观测）" description="这里显示当前 UniRouter 进程实际报告的环境和依赖健康，不依赖下面的手工登记。">
        {loading && !runtime ? <EmptyState title="正在加载当前运行实例..." description="正在读取服务自身报告的运行版本与依赖健康。" minHeight={150} /> : runtime ? <div className="ur-kpi-grid ur-workload-kpis">
          <MetricTile label="服务状态" value={label(runtime.status)} tone={runtime.status === 'operational' || runtime.status === 'ready' ? 'green' : 'amber'} />
          <MetricTile label="运行版本" value={runtime.runtime_environment?.version || '未声明'} meta={runtime.runtime_environment?.commit ? `代码提交 ${runtime.runtime_environment.commit.slice(0, 12)}` : '未声明代码提交'} />
          <MetricTile label="部署形态" value={label(runtime.runtime_environment?.deployment_mode)} meta={`${label(runtime.runtime_environment?.environment) || '未知环境'} · ${label(runtime.runtime_environment?.deployment_role) || '未知角色'}`} />
          <MetricTile label="依赖健康" value={`数据库 ${label(runtime.capacity_health?.database?.status)} / Redis ${label(runtime.capacity_health?.redis?.status)}`} meta={`本次探测：数据库 ${runtime.capacity_health?.database?.latency_ms ?? '-'} 毫秒 · Redis ${runtime.capacity_health?.redis?.latency_ms ?? '-'} 毫秒`} />
        </div> : <EmptyState title="当前运行实例观测不可用" description={runtimeError || '服务未返回运行状态。'} minHeight={150} />}
        {runtime?.generated_at && <Text type="tertiary">观测时间：{formatDateTime(runtime.generated_at)}。延迟是这一次健康探测的往返耗时，不是持续固定值。</Text>}
      </Surface>
      <div className="ur-context-explorer ur-deployment-explorer">
        <main className="ur-explorer-panel">
          <div className="ur-kpi-grid ur-workload-kpis">
            <MetricTile label="已登记附加部署" value={profiles.length} tone="blue" />
            <MetricTile label="已登记附加入口" value={endpoints.length} tone="violet" />
            <MetricTile label="HTTPS 期望入口" value={endpoints.filter(item => item.base_url.startsWith('https://') && item.status === 'active').length} tone="green" />
            <MetricTile label="独立验证通过" value={endpoints.filter(item => item.observed_status === 'ready').length} tone={endpoints.some(item => item.observed_status === 'ready') ? 'green' : 'amber'} />
          </div>
          {loadError && <Banner type="danger" description={loadError} style={{ marginBottom: 14 }} />}
          <Surface title="附加部署登记" description="只在需要记录另一个 ECS、客户 Compose 或 Kubernetes 安装时使用；0 条不表示当前服务没有运行。" actions={<Button icon={<IconRefresh />} onClick={() => void load()}>刷新</Button>}>
            <Table rowKey="profile_ref" columns={profileColumns} dataSource={profiles} loading={loading} pagination={false} scroll={{ x: 760 }} empty={<EmptyState title="没有附加部署登记" description="当前实例仍以上方只读观测为准。" minHeight={150} />} onRow={(row?: DeploymentProfile) => ({ onClick: () => row && setSelectedProfile(row), 'aria-selected': row?.profile_ref === selectedProfile?.profile_ref })} />
          </Surface>
          <Surface title="附加客户端入口登记" description="公开入口必须使用 HTTPS；登记描述预期访问方式，不会自动配置负载均衡、证书或 DNS。">
            <Table rowKey="endpoint_ref" columns={endpointColumns} dataSource={endpoints} loading={loading} pagination={false} scroll={{ x: 820 }} empty={<EmptyState title="没有附加入口登记" minHeight={160} />} onRow={(row?: GatewayEndpoint) => ({ onClick: () => row && setSelectedEndpoint(row), 'aria-selected': row?.endpoint_ref === selectedEndpoint?.endpoint_ref })} />
          </Surface>
        </main>
        <aside className="ur-explorer-panel ur-explorer-detail" aria-label="部署与网络检查器">
          {selectedEndpoint ? <>
            <div className="ur-detail-header"><div><Text className="ur-panel-hint">网关入口检查器</Text><Title heading={5} style={{ margin: 0 }}>{selectedEndpoint.display_name}</Title></div><Tag color={stateColor(selectedEndpoint.observed_status)}>{label(selectedEndpoint.observed_status)}</Tag></div>
            <dl className="ur-financial-truth-detail">
              <dt>部署</dt><dd>{selectedEndpoint.deployment_name}</dd>
              <dt>基础地址（Base URL）</dt><dd className="ur-mono-break">{selectedEndpoint.base_url}</dd>
              <dt>可见性</dt><dd>{label(selectedEndpoint.visibility)}</dd>
              <dt>TLS 模式</dt><dd>{label(selectedEndpoint.tls_mode === 'disabled' ? 'disabled_tls' : selectedEndpoint.tls_mode)}</dd>
              <dt>证书引用</dt><dd className="ur-mono-break">{selectedEndpoint.certificate_ref || '未填写（不存私钥）'}</dd>
              <dt>接口能力配置</dt><dd>{endpointProfile?.display_name || selectedEndpoint.endpoint_profile}</dd>
              <dt>运行时声明</dt><dd><Tag color={stateColor(endpointProfile?.runtime_state)}>{label(endpointProfile?.runtime_state)}</Tag></dd>
              <dt>兼容证据</dt><dd><Tag color={stateColor(endpointProfile?.evidence_level)}>{label(endpointProfile?.evidence_level)}</Tag></dd>
              <dt>观测 URL</dt><dd className="ur-mono-break">{selectedEndpoint.observed_base_url || '尚无独立观测'}</dd>
              <dt>观测时间</dt><dd>{selectedEndpoint.observed_at ? formatDateTime(selectedEndpoint.observed_at) : '尚未验证'}</dd>
              <dt>运行时变更</dt><dd>无</dd>
            </dl>
            <div className="ur-governance-actions"><Button onClick={() => openEndpoint(selectedEndpoint)}>编辑期望状态</Button>{selectedProfile && <Button onClick={() => openProfile(selectedProfile)}>编辑部署</Button>}</div>
          </> : <EmptyState title="选择或登记网关入口" description="检查器会持续显示期望、运行时能力声明与独立观测，不用点击抽屉查看原因。" minHeight={300} />}
        </aside>
      </div>

      <SideSheet title={editingProfile ? '编辑附加部署登记' : '登记附加部署'} visible={profileDrawer} onCancel={() => setProfileDrawer(false)} width={520}>
        <Banner type="info" description="此表单只记录一个附加安装的预期形态，保存后不会构建镜像、创建主机或修改正在运行的 UniRouter。" closeIcon={null} style={{ marginBottom: 16 }} />
        <Form getFormApi={setProfileForm} onSubmit={submitProfile}>
          {!editingProfile && <Form.Input field="profile_ref" label="内部登记标识" placeholder="dep_ecs_poc" rules={[{ required: true }]} extraText="只用于引用这条登记，建议小写字母、数字和下划线；不是镜像 Tag 或云资源 ID。" />}
          <Form.Input field="display_name" label="显示名称" rules={[{ required: true }]} extraText="例如“客户生产 Compose”或“华北 ECS 联调”。" />
          <Form.Select field="kind" aria-label="部署形态" label="部署形态" optionList={[{ value: 'ecs_development', label: 'ECS 开发联调' }, { value: 'onprem_compose', label: '客户本地 Compose' }, { value: 'onprem_kubernetes', label: '客户本地 Kubernetes' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Input field="environment" label="环境标识" placeholder="poc / prod" rules={[{ required: true }]} extraText="用于区分测试、POC、预发布或生产，不会创建环境。" />
          <Form.Select field="ingress_mode" aria-label="入口管理" label="入口管理" optionList={[{ value: 'external_load_balancer', label: '客户 / 云负载均衡' }, { value: 'bundled_reverse_proxy', label: '内置反向代理' }, { value: 'cluster_ingress', label: 'Kubernetes Ingress' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Select field="dns_mode" aria-label="名称解析" label="名称解析" optionList={[{ value: 'public_dns', label: '公网 DNS' }, { value: 'private_dns', label: '内网 DNS' }, { value: 'ip_address', label: 'IP 地址' }, { value: 'customer_managed', label: '客户管理' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Select field="outbound_mode" aria-label="出站模式" label="出站模式" optionList={[{ value: 'direct', label: '直接出站' }, { value: 'proxy', label: '企业代理' }, { value: 'restricted', label: '受限出站' }, { value: 'air_gapped', label: '离线隔离' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.TextArea field="notes" label="部署说明" maxCount={2000} />
          <Form.Select field="status" aria-label="附加部署期望状态" label="期望状态" optionList={[{ value: 'active', label: '启用' }, { value: 'disabled', label: '停用' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <div className="ur-form-actions"><Button onClick={() => setProfileDrawer(false)}>取消</Button><Button type="primary" onClick={() => profileForm?.submitForm()}>保存登记</Button></div>
        </Form>
      </SideSheet>

      <SideSheet title={editingEndpoint ? '编辑网关入口' : '登记网关入口'} visible={endpointDrawer} onCancel={() => setEndpointDrawer(false)} width={520}>
        <Banner type="warning" description="不得粘贴证书或私钥；这里只接受证书引用，例如 secret://tls/gateway。" closeIcon={null} style={{ marginBottom: 16 }} />
        <Form getFormApi={setEndpointForm} onSubmit={submitEndpoint}>
          {!editingEndpoint && <Form.Select field="deployment_profile_ref" aria-label="所属部署" label="所属部署" optionList={profiles.map(item => ({ value: item.profile_ref, label: item.display_name }))} rules={[{ required: true }]} style={{ width: '100%' }} />}
          {!editingEndpoint && <Form.Input field="endpoint_ref" label="稳定入口标识" placeholder="gwe_ecs_poc" rules={[{ required: true }]} />}
          <Form.Input field="display_name" label="入口名称" rules={[{ required: true }]} />
          <Form.Input field="base_url" label="基础地址（Base URL）" placeholder="https://gateway.example.internal" rules={[{ required: true }]} />
          <Form.Select field="visibility" aria-label="网关入口可见性" label="可见性" optionList={[{ value: 'public', label: '公网' }, { value: 'internal', label: '客户内网' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Select field="tls_mode" aria-label="TLS 模式" label="TLS 模式" optionList={[{ value: 'external_termination', label: '外部 TLS 终止' }, { value: 'mounted_certificate', label: '挂载证书' }, { value: 'private_ca', label: '私有 CA' }, { value: 'disabled', label: '仅内网 HTTP' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Input field="certificate_ref" label="证书引用（非秘密）" placeholder="secret://tls/gateway" />
          <Form.Select field="endpoint_profile" aria-label="网关接口兼容配置" label="网关接口兼容配置" optionList={(contract?.endpoint_profiles || []).map(item => ({ value: item.profile_id, label: `${item.display_name} · ${label(item.evidence_level)}` }))} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Select field="status" aria-label="网关入口期望状态" label="期望状态" optionList={[{ value: 'active', label: '启用' }, { value: 'disabled', label: '停用' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <div className="ur-form-actions"><Button onClick={() => setEndpointDrawer(false)}>取消</Button><Button type="primary" onClick={() => endpointForm?.submitForm()}>保存期望状态</Button></div>
        </Form>
      </SideSheet>
    </div>
  );
}
