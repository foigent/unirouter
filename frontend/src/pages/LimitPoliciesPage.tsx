import { useEffect, useState } from 'react';
import { Button, Form, SideSheet, Space, Table, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconEdit, IconPlus } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { isBillingRole, isOrgAdminRole, isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, PageHeader, Surface } from '../ui';
import { customerContextBlockerCopy, effectiveCustomerOrgID, resolveCustomerContext } from '../utils/customerContext';
import { loadAllPages } from '../utils/loadAllPages';
import { projectDisplayName } from '../utils/projectDisplay';

const { Text, Paragraph } = Typography;

interface LimitPolicy {
  id: number;
  name: string;
  version: number;
  org_id?: number | null;
  org_name?: string;
  project_id?: number | null;
  project_name?: string;
  api_key_id?: number | null;
  api_key_name?: string;
  api_key_prefix?: string;
  model?: string;
  policy_label?: string;
  mode: 'disabled' | 'shadow' | 'enforce';
  failure_mode: 'fail_closed' | 'last_known_safe';
  rps_limit?: number | null;
  rpm_limit?: number | null;
  tpm_limit?: number | null;
  concurrency_limit?: number | null;
  runtime_snapshot_applied?: boolean;
}

interface KeyOption {
  id: number;
  name: string;
  key_prefix: string;
  org_id: number;
  org_name?: string;
  project_id?: number;
  project_name?: string;
}

interface BuiltinLimits {
  global_concurrency?: number;
  organization_concurrency?: number;
  workload_global_capacity?: number;
  workload_organization_capacity?: number;
  workload_global_short_reserve?: number;
  workload_organization_short_reserve?: number;
  workload_interactive_weight?: number;
  workload_stream_weight?: number;
  workload_embedding_weight?: number;
  workload_interactive_max_wait_ms?: number;
  workload_stream_max_wait_ms?: number;
  workload_embedding_max_wait_ms?: number;
}

function selectorText(policy: LimitPolicy) {
  const parts: string[] = [policy.org_id
    ? (policy.org_name || `客户组织 #${policy.org_id}`)
    : '部署全局（当前部署全部请求）'];
  if (policy.project_id) parts.push(projectDisplayName({ id: policy.project_id, name: policy.project_name || '', slug: policy.project_name === 'Default Project' ? 'default' : undefined }));
  if (policy.api_key_id) parts.push(policy.api_key_name ? `凭证 ${policy.api_key_name}` : `凭证 #${policy.api_key_id}`);
  if (policy.model) parts.push(`模型 ${policy.model}`);
  if (policy.policy_label) parts.push(`标签 ${policy.policy_label}`);
  return parts.join(' · ');
}

function limitsText(policy: LimitPolicy) {
  const parts: string[] = [];
  if (policy.rps_limit) parts.push(`RPS ${policy.rps_limit}`);
  if (policy.rpm_limit) parts.push(`RPM ${policy.rpm_limit}`);
  if (policy.tpm_limit) parts.push(`TPM ${policy.tpm_limit}`);
  if (policy.concurrency_limit) parts.push(`并发 ${policy.concurrency_limit}`);
  return parts.join(' / ');
}

function modeTag(mode: LimitPolicy['mode']) {
  if (mode === 'enforce') return <Tag color="red">正式拦截</Tag>;
  if (mode === 'shadow') return <Tag color="orange">影子观察</Tag>;
  return <Tag color="grey">已停用</Tag>;
}

export default function LimitPoliciesPage() {
  const user = useAuthStore((state) => state.user);
  const organizationRole = organizationRoleForUser(user);
  const customerContext = resolveCustomerContext(user);
  const customerOrgID = effectiveCustomerOrgID(user);
  const customerContextReady = customerContext.state === 'ready' && Boolean(customerOrgID);
  const canManage = isOrgAdminRole(organizationRole) && !isBillingRole(organizationRole);
  const platform = isPlatformAdminRole(organizationRole);
  const [policies, setPolicies] = useState<LimitPolicy[]>([]);
  const [keys, setKeys] = useState<KeyOption[]>([]);
  const [models, setModels] = useState<string[]>([]);
  const [projects, setProjects] = useState<Array<{ value: number; label: string }>>([]);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [total, setTotal] = useState(0);
  const [builtinLimits, setBuiltinLimits] = useState<BuiltinLimits>({});
  const [drawerVisible, setDrawerVisible] = useState(false);
  const [editing, setEditing] = useState<LimitPolicy | null>(null);
  const [policyScope, setPolicyScope] = useState<'customer' | 'deployment_global'>('customer');
  const [formApi, setFormApi] = useState<any>(null);

  const load = (nextPage = page, nextSize = pageSize) => {
    if (!customerContextReady) return;
    setLoading(true);
    api.getLimitPolicies(nextPage, nextSize)
      .then((data: any) => {
        setPolicies(data?.data || []);
        setTotal(data?.total || 0);
        setBuiltinLimits(data?.builtin_limits || {});
      })
      .catch((err: any) => Toast.error(err?.message || '共享限额策略加载失败'))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    if (customerContextReady) load(1, pageSize);
  }, [customerContextReady, customerOrgID]);
  useEffect(() => {
    if (!customerContextReady || !customerOrgID) return;
    void (async () => {
      try {
        const [keyData, projectData, modelData] = await Promise.all([
          loadAllPages<KeyOption>((page, size) => api.getKeys(page, size)),
          loadAllPages<any>((page, size) => api.getProjects(page, size, { org_id: customerOrgID })),
          api.getModelNames(),
        ]);
        setKeys((keyData || []).filter((key) => key.org_id === customerOrgID));
        setProjects((projectData || [])
          .filter((project: any) => project.org_id === customerOrgID)
          .map((project: any) => ({ value: project.id, label: projectDisplayName(project) })));
        const source = Array.isArray(modelData?.data) ? modelData.data : modelData;
        setModels((Array.isArray(source) ? source : []).map((item: any) => typeof item === 'string' ? item : item.model).filter(Boolean));
      } catch {
        Toast.error('策略选项加载失败，请刷新后重试');
      }
    })();
  }, [customerContextReady, customerOrgID]);

  const openCreate = () => {
    setEditing(null);
    setDrawerVisible(true);
  };
  const openEdit = (policy: LimitPolicy) => {
    setEditing(policy);
    setDrawerVisible(true);
  };
  const closeDrawer = () => {
    setDrawerVisible(false);
    setEditing(null);
    formApi?.reset();
  };

  useEffect(() => {
    if (!drawerVisible || !formApi) return;
    formApi.reset();
    if (editing) {
      const nextScope = editing.org_id ? 'customer' : 'deployment_global';
      setPolicyScope(nextScope);
      formApi.setValues({
        ...editing,
        scope: nextScope,
        org_id: editing.org_id || undefined,
        project_id: editing.project_id || undefined,
        api_key_id: editing.api_key_id || undefined,
      });
      return;
    }
    setPolicyScope('customer');
    formApi.setValues({ scope: 'customer', mode: 'shadow', failure_mode: 'fail_closed' });
  }, [drawerVisible, editing, formApi]);

  const submit = async (values: Record<string, any>) => {
    if (!customerContextReady || !customerOrgID) {
      Toast.error('无法确认唯一客户组织，限额策略未保存');
      return;
    }
    const deploymentGlobal = platform && values.scope === 'deployment_global';
    const payload: Record<string, any> = {
      name: values.name,
      ...(editing ? { version: editing.version } : {}),
      org_id: deploymentGlobal ? 0 : customerOrgID,
      project_id: deploymentGlobal ? 0 : Number(values.project_id || 0),
      api_key_id: deploymentGlobal ? 0 : Number(values.api_key_id || 0),
      model: values.model || '',
      policy_label: (values.policy_label || '').trim(),
      mode: values.mode,
      failure_mode: values.failure_mode,
      rps_limit: Number(values.rps_limit || 0),
      rpm_limit: Number(values.rpm_limit || 0),
      tpm_limit: Number(values.tpm_limit || 0),
      concurrency_limit: Number(values.concurrency_limit || 0),
    };
    if (!editing) {
      ['org_id', 'project_id', 'api_key_id', 'rps_limit', 'rpm_limit', 'tpm_limit', 'concurrency_limit']
        .forEach((field) => { if (!payload[field]) delete payload[field]; });
    }
    try {
      const result = editing
        ? await api.updateLimitPolicy(editing.id, payload)
        : await api.createLimitPolicy(payload);
      if (result?.runtime_snapshot_applied === false) {
        Toast.warning('策略已保存，运行副本将在下一次刷新时收敛');
      } else {
        Toast.success(editing ? '策略已更新并生成新版本' : '策略已创建');
      }
      closeDrawer();
      load();
    } catch (err: any) {
      Toast.error(err?.message || '策略保存失败');
    }
  };

  const columns = [
    { title: '策略', dataIndex: 'name', render: (value: string, record: LimitPolicy) => <div><Text strong>{value}</Text><br /><Text type="tertiary" size="small">版本 v{record.version}</Text></div> },
    { title: '适用范围（全部条件同时满足）', render: (_: unknown, record: LimitPolicy) => selectorText(record) },
    { title: '限额', render: (_: unknown, record: LimitPolicy) => limitsText(record) },
    { title: '状态', dataIndex: 'mode', width: 110, render: (value: LimitPolicy['mode']) => modeTag(value) },
    {
      title: 'Redis 不确定时', dataIndex: 'failure_mode', width: 160,
      render: (value: LimitPolicy['failure_mode']) => value === 'last_known_safe' ? '最多短暂放行 1 次' : '立即拒绝（503）',
    },
    ...(canManage ? [{ title: '操作', width: 100, render: (_: unknown, record: LimitPolicy) => (
      platform || record.org_id === customerOrgID
        ? <Button aria-label={`编辑策略 ${record.name}`} icon={<IconEdit />} size="small" onClick={() => openEdit(record)} />
        : <Text type="tertiary" size="small">部署全局只读</Text>
    ) }] : []),
  ];

  if (!customerContextReady) {
    const blocker = customerContextBlockerCopy(customerContext.state === 'ready' ? 'unavailable' : customerContext.state);
    return (
      <div className="ur-page">
        <PageHeader eyebrow="访问与安全" title={blocker.title} description="共享限额策略必须绑定唯一客户；系统不会把部署技术组织当作客户组织。" />
        <Surface><EmptyState title={blocker.title} description={blocker.description} minHeight={260} /></Surface>
      </div>
    );
  }

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="访问与安全"
        title="共享限额"
        description="跨所有 UniRouter 副本统一限制请求速率、Token 和并发；增加服务器不会把客户额度成倍放大。"
        actions={canManage ? <Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增策略</Button> : undefined}
      />

      <Surface title="限额策略" description="策略按范围条件叠加匹配；先从列表确认现状，再新增或编辑。">
        <div className="ur-table-shell">
          <Table
            rowKey="id"
            columns={columns}
            dataSource={policies}
            loading={loading}
            pagination={{
              currentPage: page, pageSize, total, pageSizeOpts: [20, 50, 100], showSizeChanger: true,
              onPageChange: (next: number) => { setPage(next); load(next, pageSize); },
              onPageSizeChange: (next: number) => { setPageSize(next); setPage(1); load(1, next); },
            }}
          />
        </div>
      </Surface>

      <Surface title="使用说明与故障策略">
        <Paragraph>RPS 是每秒请求数，RPM 是每分钟请求数，TPM 是每分钟可预留并最终按实际用量结算的 Token；并发是同一时刻正在执行的请求数。</Paragraph>
        <Paragraph><Text strong>推荐先用“影子观察”：</Text>只记录“本来会拦截”的结果，不影响业务；确认阈值后再切到“正式拦截”。所有匹配策略会叠加，较具体的策略只能进一步收紧。</Paragraph>
        <Paragraph>每个访问凭证自身的 RPM/RPS 也是内置共享策略；表格中的客户、项目、模型和标签策略会在它之上继续收紧。</Paragraph>
        <Paragraph><Text strong>当前内置加权容量：</Text>当前部署 {builtinLimits.workload_global_capacity ?? builtinLimits.global_concurrency ?? '加载中'} 个容量单位，唯一客户 {builtinLimits.workload_organization_capacity ?? builtinLimits.organization_concurrency ?? '加载中'} 个。普通对话和 Embeddings 每次占 {builtinLimits.workload_interactive_weight ?? 1} 个，流式对话占 {builtinLimits.workload_stream_weight ?? 4} 个；部署和客户分别至少为短请求保留 {builtinLimits.workload_global_short_reserve ?? 10} / {builtinLimits.workload_organization_short_reserve ?? 2} 个单位。</Paragraph>
        <Paragraph><Text strong>公平等待：</Text>普通对话最多等待 {builtinLimits.workload_interactive_max_wait_ms ?? 50} 毫秒，Embeddings 最多等待 {builtinLimits.workload_embedding_max_wait_ms ?? 50} 毫秒，流式对话最多等待 {builtinLimits.workload_stream_max_wait_ms ?? 250} 毫秒。仍无容量时返回 429 和 Retry-After，客户端可以稍后重试；增加 UniRouter 副本不会把这些数字成倍放大。</Paragraph>
        <Paragraph>表格中的“并发”仍表示匹配策略允许的同时请求数，并且只能进一步收紧内置容量，不能把它放宽。</Paragraph>
        <Paragraph><Text strong>故障策略：</Text>默认在 Redis 无法确认额度时立即返回 503。选择“短暂安全许可”时，也只能使用健康期预先拿到、约 2 秒失效且每个副本最多 1 次的许可，不会无限放行。</Paragraph>
      </Surface>

      <SideSheet title={editing ? `编辑策略：${editing.name}` : '新增共享限额策略'} visible={drawerVisible} onCancel={closeDrawer} width={560}>
        <Form getFormApi={(instance: any) => setFormApi(instance)} onSubmit={submit} initValues={{ mode: 'shadow', failure_mode: 'fail_closed' }}>
          <Form.Input field="name" label="策略名称" placeholder="production-chat" rules={[{ required: true, message: '请输入策略名称' }]} />
          {platform ? (
            <Form.Select
              field="scope"
              label={<span id="limit-policy-scope-label">作用域</span>}
              aria-labelledby="limit-policy-scope-label"
              optionList={[
                { value: 'customer', label: `唯一客户（推荐）· ${customerContext.orgName || `组织 #${customerOrgID}`}` },
                { value: 'deployment_global', label: '部署全局（当前部署全部请求）' },
              ]}
              onChange={(value: any) => {
                const nextScope = value === 'deployment_global' ? 'deployment_global' : 'customer';
                setPolicyScope(nextScope);
                if (nextScope === 'deployment_global') {
                  formApi?.setValue('project_id', undefined);
                  formApi?.setValue('api_key_id', undefined);
                }
              }}
              rules={[{ required: true }]}
              style={{ width: '100%' }}
            />
          ) : <Text type="tertiary">客户范围：{customerContext.orgName || `组织 #${customerOrgID}`}</Text>}
          <Form.Select field="project_id" label={<span id="limit-policy-project-label">项目（可选）</span>} aria-labelledby="limit-policy-project-label" optionList={projects} showClear filter disabled={policyScope === 'deployment_global'} style={{ width: '100%' }} />
          <Form.Select
            field="api_key_id"
            label={<span id="limit-policy-key-label">访问凭证（可选）</span>}
            aria-labelledby="limit-policy-key-label"
            optionList={keys.map((key) => ({ value: key.id, label: `${key.name} · ${projectDisplayName({ id: key.project_id || 0, name: key.project_name || '' })} · ${key.key_prefix}***` }))}
            showClear filter disabled={policyScope === 'deployment_global'} style={{ width: '100%' }}
          />
          <Form.Select field="model" label={<span id="limit-policy-model-label">模型（可选）</span>} aria-labelledby="limit-policy-model-label" optionList={models.map((model) => ({ value: model, label: model }))} showClear filter style={{ width: '100%' }} />
          <Form.Input field="policy_label" label="策略标签（可选）" placeholder="production" />
          <Text type="tertiary" size="small">已填写的客户、项目、凭证、模型和标签必须同时匹配；标签由管理员绑定到访问凭证，调用方不能通过请求头自行声明。</Text>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginTop: 16 }}>
            <Form.InputNumber field="rps_limit" label="RPS（每秒请求）" min={0} style={{ width: '100%' }} />
            <Form.InputNumber field="rpm_limit" label="RPM（每分钟请求）" min={0} style={{ width: '100%' }} />
            <Form.InputNumber field="tpm_limit" label="TPM（每分钟 Token）" min={0} style={{ width: '100%' }} />
            <Form.InputNumber field="concurrency_limit" label="并发请求数" min={0} style={{ width: '100%' }} />
          </div>
          <Form.Select
            field="mode"
            label={<span id="limit-policy-mode-label">运行状态</span>}
            aria-labelledby="limit-policy-mode-label"
            optionList={[
              { value: 'shadow', label: '影子观察（推荐先选）' },
              { value: 'enforce', label: '正式拦截' },
              { value: 'disabled', label: '停用' },
            ]}
            rules={[{ required: true }]}
            style={{ width: '100%' }}
          />
          <Form.Select
            field="failure_mode"
            label={<span id="limit-policy-failure-mode-label">Redis 无法确认额度时</span>}
            aria-labelledby="limit-policy-failure-mode-label"
            optionList={[
              { value: 'fail_closed', label: '立即拒绝并提示稍后重试（推荐）' },
              { value: 'last_known_safe', label: '只用健康期预发的短暂安全许可' },
            ]}
            rules={[{ required: true }]}
            style={{ width: '100%' }}
          />
          <Space style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 24 }}>
            <Button onClick={closeDrawer}>取消</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>{editing ? '保存并生成新版本' : '创建策略'}</Button>
          </Space>
        </Form>
      </SideSheet>
    </div>
  );
}
