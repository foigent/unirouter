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

const labels: Record<string, string> = {
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
  const [profileDrawer, setProfileDrawer] = useState(false);
  const [endpointDrawer, setEndpointDrawer] = useState(false);
  const [editingProfile, setEditingProfile] = useState<DeploymentProfile | null>(null);
  const [editingEndpoint, setEditingEndpoint] = useState<GatewayEndpoint | null>(null);
  const [profileForm, setProfileForm] = useState<any>(null);
  const [endpointForm, setEndpointForm] = useState<any>(null);

  const load = async () => {
    setLoading(true);
    setLoadError('');
    try {
      const [contractResult, profileResult, endpointResult] = await Promise.all([
        api.getDeploymentNetworkContract(), api.getDeploymentProfiles(), api.getGatewayEndpoints(),
      ]);
      const profileItems = profileResult?.data || [];
      const endpointItems = endpointResult?.data || [];
      setContract(contractResult);
      setProfiles(profileItems);
      setEndpoints(endpointItems);
      setSelectedProfile(current => profileItems.find((item: DeploymentProfile) => item.profile_ref === current?.profile_ref) || profileItems[0] || null);
      setSelectedEndpoint(current => endpointItems.find((item: GatewayEndpoint) => item.endpoint_ref === current?.endpoint_ref) || endpointItems[0] || null);
    } catch (error: any) {
      setLoadError(error?.message || '无法加载部署与网络状态。');
    } finally {
      setLoading(false);
    }
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
        Toast.success('Deployment Profile 期望状态已更新；未修改运行时');
      } else {
        await api.createDeploymentProfile(values);
        Toast.success('Deployment Profile 已创建');
      }
      setProfileDrawer(false);
      await load();
    } catch (error: any) { Toast.error(error?.message || '保存 Deployment Profile 失败'); }
  };

  const submitEndpoint = async (values: Record<string, any>) => {
    try {
      if (editingEndpoint) {
        await api.updateGatewayEndpoint(editingEndpoint.endpoint_ref, { ...values, expected_revision: editingEndpoint.desired_revision });
        Toast.success('Gateway Endpoint 期望状态已更新；未修改 Ingress');
      } else {
        await api.createGatewayEndpoint(values);
        Toast.success('Gateway Endpoint 已登记，当前仍需独立验证');
      }
      setEndpointDrawer(false);
      await load();
    } catch (error: any) { Toast.error(error?.message || '保存 Gateway Endpoint 失败'); }
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
    { title: 'Gateway Endpoint', render: (_: unknown, row: GatewayEndpoint) => <div className="ur-table-primary"><strong>{row.display_name}</strong><code>{row.endpoint_ref}</code></div> },
    { title: 'Base URL', dataIndex: 'base_url', render: (value: string) => <code className="ur-mono-break">{value}</code> },
    { title: '可见性', dataIndex: 'visibility', render: label },
    { title: 'TLS', dataIndex: 'tls_mode', render: (value: string) => label(value === 'disabled' ? 'disabled_tls' : value) },
    { title: '期望', dataIndex: 'status', render: (value: string) => <Tag color={stateColor(value)}>{label(value)}</Tag> },
    { title: '观测', dataIndex: 'observed_status', render: (value: string) => <Tag color={stateColor(value)}>{label(value)}</Tag> },
  ], []);

  const endpointProfile = contract?.endpoint_profiles.find(item => item.profile_id === selectedEndpoint?.endpoint_profile);

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="系统与集成"
        title="部署与网络"
        description="用 Deployment Profile 描述安装形态，用 Gateway Endpoint 描述客户端入口；期望状态与运行时观测严格分离。"
        actions={<><Button onClick={() => openProfile()} icon={<IconPlus />}>新建部署</Button><Button type="primary" onClick={() => openEndpoint()} icon={<IconPlus />} disabled={!profiles.length}>登记入口</Button></>}
      />
      <Banner
        type="info"
        description="UI 只保存非秘密的期望状态和证书引用，不保存 TLS 私钥、不重启 Ingress。实际应用由 Operator / CLI / Compose / Helm 完成，并需回写独立观测证据。"
        style={{ marginBottom: 16 }}
      />
      <div className="ur-context-explorer ur-deployment-explorer">
        <main className="ur-explorer-panel">
          <div className="ur-kpi-grid ur-workload-kpis">
            <MetricTile label="Deployment Profile" value={profiles.length} tone="blue" />
            <MetricTile label="Gateway Endpoint" value={endpoints.length} tone="violet" />
            <MetricTile label="HTTPS 期望入口" value={endpoints.filter(item => item.base_url.startsWith('https://') && item.status === 'active').length} tone="green" />
            <MetricTile label="独立验证通过" value={endpoints.filter(item => item.observed_status === 'ready').length} tone={endpoints.some(item => item.observed_status === 'ready') ? 'green' : 'amber'} />
          </div>
          {loadError && <Banner type="danger" description={loadError} style={{ marginBottom: 14 }} />}
          <Surface title="部署形态" description="ECS 联调与客户本地部署使用同一对象，不为短期测试牺牲长期产品边界。" actions={<Button icon={<IconRefresh />} onClick={() => void load()}>刷新</Button>}>
            <Table rowKey="profile_ref" columns={profileColumns} dataSource={profiles} loading={loading} pagination={false} empty={<EmptyState title="尚未登记部署" minHeight={150} />} onRow={(row?: DeploymentProfile) => ({ onClick: () => row && setSelectedProfile(row), 'aria-selected': row?.profile_ref === selectedProfile?.profile_ref })} />
          </Surface>
          <Surface title="客户端入口" description="公开入口必须使用 HTTPS；Responses 目前只能登记为合同，不会升级为运行时支持。">
            <Table rowKey="endpoint_ref" columns={endpointColumns} dataSource={endpoints} loading={loading} pagination={false} empty={<EmptyState title="尚未登记 Gateway Endpoint" minHeight={160} />} onRow={(row?: GatewayEndpoint) => ({ onClick: () => row && setSelectedEndpoint(row), 'aria-selected': row?.endpoint_ref === selectedEndpoint?.endpoint_ref })} />
          </Surface>
        </main>
        <aside className="ur-explorer-panel ur-explorer-detail" aria-label="部署与网络检查器">
          {selectedEndpoint ? <>
            <div className="ur-detail-header"><div><Text className="ur-panel-hint">Gateway Endpoint Inspector</Text><Title heading={5} style={{ margin: 0 }}>{selectedEndpoint.display_name}</Title></div><Tag color={stateColor(selectedEndpoint.observed_status)}>{label(selectedEndpoint.observed_status)}</Tag></div>
            <dl className="ur-financial-truth-detail">
              <dt>部署</dt><dd>{selectedEndpoint.deployment_name}</dd>
              <dt>Base URL</dt><dd className="ur-mono-break">{selectedEndpoint.base_url}</dd>
              <dt>可见性</dt><dd>{label(selectedEndpoint.visibility)}</dd>
              <dt>TLS 模式</dt><dd>{label(selectedEndpoint.tls_mode === 'disabled' ? 'disabled_tls' : selectedEndpoint.tls_mode)}</dd>
              <dt>证书引用</dt><dd className="ur-mono-break">{selectedEndpoint.certificate_ref || '未填写（不存私钥）'}</dd>
              <dt>Endpoint Profile</dt><dd>{endpointProfile?.display_name || selectedEndpoint.endpoint_profile}</dd>
              <dt>运行时声明</dt><dd><Tag color={stateColor(endpointProfile?.runtime_state)}>{label(endpointProfile?.runtime_state)}</Tag></dd>
              <dt>兼容证据</dt><dd><Tag color={stateColor(endpointProfile?.evidence_level)}>{label(endpointProfile?.evidence_level)}</Tag></dd>
              <dt>观测 URL</dt><dd className="ur-mono-break">{selectedEndpoint.observed_base_url || '尚无独立观测'}</dd>
              <dt>观测时间</dt><dd>{selectedEndpoint.observed_at ? formatDateTime(selectedEndpoint.observed_at) : '尚未验证'}</dd>
              <dt>期望修订</dt><dd>r{selectedEndpoint.desired_revision}</dd>
              <dt>运行时变更</dt><dd>无</dd>
            </dl>
            <div className="ur-governance-actions"><Button onClick={() => openEndpoint(selectedEndpoint)}>编辑期望状态</Button>{selectedProfile && <Button onClick={() => openProfile(selectedProfile)}>编辑部署</Button>}</div>
          </> : <EmptyState title="选择或登记 Gateway Endpoint" description="检查器会持续显示期望、运行时能力声明与独立观测，不用点击抽屉查看原因。" minHeight={300} />}
        </aside>
      </div>

      <SideSheet title={editingProfile ? '编辑 Deployment Profile' : '新建 Deployment Profile'} visible={profileDrawer} onCancel={() => setProfileDrawer(false)} width={520}>
        <Form getFormApi={setProfileForm} onSubmit={submitProfile}>
          {!editingProfile && <Form.Input field="profile_ref" label="稳定部署标识" placeholder="dep_ecs_poc" rules={[{ required: true }]} />}
          <Form.Input field="display_name" label="部署名称" rules={[{ required: true }]} />
          <Form.Select field="kind" label="部署形态" optionList={[{ value: 'ecs_development', label: 'ECS 开发联调' }, { value: 'onprem_compose', label: '客户本地 Compose' }, { value: 'onprem_kubernetes', label: '客户本地 Kubernetes' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Input field="environment" label="环境" placeholder="poc / prod" rules={[{ required: true }]} />
          <Form.Select field="ingress_mode" label="入口管理" optionList={[{ value: 'external_load_balancer', label: '客户 / 云负载均衡' }, { value: 'bundled_reverse_proxy', label: '内置反向代理' }, { value: 'cluster_ingress', label: 'Kubernetes Ingress' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Select field="dns_mode" label="名称解析" optionList={[{ value: 'public_dns', label: '公网 DNS' }, { value: 'private_dns', label: '内网 DNS' }, { value: 'ip_address', label: 'IP 地址' }, { value: 'customer_managed', label: '客户管理' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Select field="outbound_mode" label="出站模式" optionList={[{ value: 'direct', label: '直接出站' }, { value: 'proxy', label: '企业代理' }, { value: 'restricted', label: '受限出站' }, { value: 'air_gapped', label: '离线隔离' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.TextArea field="notes" label="部署说明" maxCount={2000} />
          <Form.Select field="status" label="期望状态" optionList={[{ value: 'active', label: '启用' }, { value: 'disabled', label: '停用' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <div className="ur-form-actions"><Button onClick={() => setProfileDrawer(false)}>取消</Button><Button type="primary" onClick={() => profileForm?.submitForm()}>保存期望状态</Button></div>
        </Form>
      </SideSheet>

      <SideSheet title={editingEndpoint ? '编辑 Gateway Endpoint' : '登记 Gateway Endpoint'} visible={endpointDrawer} onCancel={() => setEndpointDrawer(false)} width={520}>
        <Banner type="warning" description="不得粘贴证书或私钥；这里只接受证书引用，例如 secret://tls/gateway。" style={{ marginBottom: 16 }} />
        <Form getFormApi={setEndpointForm} onSubmit={submitEndpoint}>
          {!editingEndpoint && <Form.Select field="deployment_profile_ref" label="所属部署" optionList={profiles.map(item => ({ value: item.profile_ref, label: item.display_name }))} rules={[{ required: true }]} style={{ width: '100%' }} />}
          {!editingEndpoint && <Form.Input field="endpoint_ref" label="稳定入口标识" placeholder="gwe_ecs_poc" rules={[{ required: true }]} />}
          <Form.Input field="display_name" label="入口名称" rules={[{ required: true }]} />
          <Form.Input field="base_url" label="Base URL" placeholder="https://gateway.example.internal" rules={[{ required: true }]} />
          <Form.Select field="visibility" label="可见性" optionList={[{ value: 'public', label: '公网' }, { value: 'internal', label: '客户内网' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Select field="tls_mode" label="TLS 模式" optionList={[{ value: 'external_termination', label: '外部 TLS 终止' }, { value: 'mounted_certificate', label: '挂载证书' }, { value: 'private_ca', label: '私有 CA' }, { value: 'disabled', label: '仅内网 HTTP' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Input field="certificate_ref" label="证书引用（非秘密）" placeholder="secret://tls/gateway" />
          <Form.Select field="endpoint_profile" label="Endpoint Profile" optionList={(contract?.endpoint_profiles || []).map(item => ({ value: item.profile_id, label: `${item.display_name} · ${label(item.evidence_level)}` }))} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Select field="status" label="期望状态" optionList={[{ value: 'active', label: '启用' }, { value: 'disabled', label: '停用' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <div className="ur-form-actions"><Button onClick={() => setEndpointDrawer(false)}>取消</Button><Button type="primary" onClick={() => endpointForm?.submitForm()}>保存期望状态</Button></div>
        </Form>
      </SideSheet>
    </div>
  );
}
