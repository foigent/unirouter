import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Form, Modal, Select, Space, Table, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconKeyStroked, IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import { useSearchParams } from 'react-router-dom';
import { accessApi, effectiveGrantStatus, isGrantCurrentlyActive, type AccessCredentialFamily, type AccessGrant, type AccessRequest, type IssuedCredential } from '../api/access';
import { ApiError, api } from '../api/client';
import { useAuthStore } from '../store/auth';
import { EmptyState, PageHeader, StatusTag, Surface } from '../ui';
import { formatDateTime } from '../utils/formatDate';
import { IntentRequestIDs } from '../utils/intentRequestId';
import { loadAllPages } from '../utils/loadAllPages';
import { projectOption } from '../utils/projectDisplay';

const { Text } = Typography;

type Project = {
  id: number;
  org_id: number;
  name: string;
  slug?: string;
  status: string;
  current_user_membership_role?: string;
};

const statusText: Record<string, string> = {
  approved: '已批准', needs_review: '待审核', rejected: '已拒绝', cancelled: '已取消',
  active: '有效', suspended: '已暂停', revoked: '已撤销', disabled: '已停用', expired: '已过期',
};

const errorText = (error: unknown, fallback: string) => {
  if (error instanceof ApiError && error.status === 409) return '状态已发生变化，已刷新当前事实，请重新确认。';
  if (error instanceof ApiError && error.status === 403) return '当前账号没有执行此操作的实时权限。';
  return fallback;
};

export default function AccessStatePage() {
  const user = useAuthStore((state) => state.user);
  const [search, setSearch] = useSearchParams();
  const intents = useRef(new IntentRequestIDs()).current;
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectError, setProjectError] = useState('');
  const [projectLoading, setProjectLoading] = useState(true);
  const selectedProjectID = Number(search.get('project_id') || 0) || undefined;
  const selectedGrantRef = search.get('grant_ref') || '';

  const [requests, setRequests] = useState<AccessRequest[]>([]);
  const [requestCursor, setRequestCursor] = useState<number>();
  const [requestsLoading, setRequestsLoading] = useState(true);
  const [requestsError, setRequestsError] = useState('');
  const [grants, setGrants] = useState<AccessGrant[]>([]);
  const [grantCursor, setGrantCursor] = useState<number>();
  const [grantsLoading, setGrantsLoading] = useState(true);
  const [grantsError, setGrantsError] = useState('');
  const [credentials, setCredentials] = useState<AccessCredentialFamily[]>([]);
  const [credentialCursor, setCredentialCursor] = useState<number>();
  const [credentialsLoading, setCredentialsLoading] = useState(false);
  const [credentialsError, setCredentialsError] = useState('');
  const [requestDrawer, setRequestDrawer] = useState(false);
  const [credentialDrawer, setCredentialDrawer] = useState(false);
  const [requestForm, setRequestForm] = useState<any>(null);
  const [credentialForm, setCredentialForm] = useState<any>(null);
  const [secret, setSecret] = useState<IssuedCredential | null>(null);
  const [mutating, setMutating] = useState(false);

  const selectedGrant = grants.find((grant) => grant.grant_ref === selectedGrantRef);
  const selectedProject = projects.find((project) => project.id === selectedProjectID);

  const replaceSearch = useCallback((values: Record<string, string | number | undefined>) => {
    const next = new URLSearchParams(search);
    Object.entries(values).forEach(([key, value]) => {
      if (value === undefined || value === '') next.delete(key);
      else next.set(key, String(value));
    });
    setSearch(next, { replace: true });
  }, [search, setSearch]);

  const loadProjects = useCallback(async () => {
    setProjectLoading(true);
    setProjectError('');
    try {
      const items = await loadAllPages<Project>((page, size) => api.getProjects(page, size, { status: 'active' }));
      setProjects(items);
      if (!selectedProjectID && items[0]) replaceSearch({ project_id: items[0].id });
    } catch {
      setProjects([]);
      setProjectError('无法读取当前账号可发现的项目。');
    } finally {
      setProjectLoading(false);
    }
  }, [replaceSearch, selectedProjectID]);

  const loadRequests = useCallback(async (append = false) => {
    setRequestsLoading(true);
    setRequestsError('');
    try {
      const page = await accessApi.listMyRequests(20, append ? requestCursor : undefined);
      setRequests((current) => append ? [...current, ...page.data] : page.data);
      setRequestCursor(page.pagination.has_more ? page.pagination.next_after_id : undefined);
    } catch {
      if (!append) setRequests([]);
      setRequestsError('无法读取我的访问请求。');
    } finally {
      setRequestsLoading(false);
    }
  }, [requestCursor]);

  const loadGrants = useCallback(async (append = false) => {
    setGrantsLoading(true);
    setGrantsError('');
    try {
      const page = await accessApi.listMyGrants(20, append ? grantCursor : undefined);
      setGrants((current) => append ? [...current, ...page.data] : page.data);
      setGrantCursor(page.pagination.has_more ? page.pagination.next_after_id : undefined);
    } catch {
      if (!append) setGrants([]);
      setGrantsError('无法读取我的有效或历史授权。');
    } finally {
      setGrantsLoading(false);
    }
  }, [grantCursor]);

  const loadCredentials = useCallback(async (append = false) => {
    if (!selectedGrantRef) {
      setCredentials([]);
      setCredentialCursor(undefined);
      setCredentialsError('');
      return;
    }
    setCredentialsLoading(true);
    setCredentialsError('');
    try {
      const page = await accessApi.listCredentials(selectedGrantRef, 20, append ? credentialCursor : undefined);
      setCredentials((current) => append ? [...current, ...page.data] : page.data);
      setCredentialCursor(page.pagination.has_more ? page.pagination.next_after_id : undefined);
    } catch {
      if (!append) setCredentials([]);
      setCredentialsError('无法读取此授权的凭据清单；授权本身的事实仍保留在上方。');
    } finally {
      setCredentialsLoading(false);
    }
  }, [credentialCursor, selectedGrantRef]);

  useEffect(() => { loadProjects(); }, [loadProjects]);
  useEffect(() => { loadRequests(); loadGrants(); }, []);
  useEffect(() => { loadCredentials(); }, [selectedGrantRef]);

  useEffect(() => {
    if (selectedGrantRef && grants.length > 0 && !selectedGrant) replaceSearch({ grant_ref: undefined });
  }, [grants, replaceSearch, selectedGrant, selectedGrantRef]);

  const refreshAccess = async () => {
    await Promise.all([loadRequests(), loadGrants()]);
    if (selectedGrantRef) await loadCredentials();
  };

  const submitRequest = async (values: { project_role: string }) => {
    if (!selectedProject || !user) return;
    const intent = `request:${selectedProject.id}:${values.project_role}`;
    const clientID = intents.get(intent);
    setMutating(true);
    try {
      const result = await accessApi.createRequest({
        org_id: selectedProject.org_id,
        project_id: selectedProject.id,
        subject: { type: 'user', user_id: user.id },
        request_kind: 'human_personal',
        requested_scope: { project_role: values.project_role, scope_mode: 'project_default', models: [], endpoints: [] },
        environment: 'personal',
        production_use: false,
        submission_mode: 'self',
      }, clientID);
      intents.complete(intent);
      setRequestDrawer(false);
      requestForm?.reset();
      Toast.success(result.request.status === 'approved' ? '访问已批准' : '访问请求已提交审核');
      await refreshAccess();
    } catch (error) {
      intents.settleError(intent, error);
      Toast.error(errorText(error, '提交失败；如网络结果不确定，可直接重试同一意图。'));
      if (error instanceof ApiError && error.status === 409) await refreshAccess();
    } finally {
      setMutating(false);
    }
  };

  const cancelRequest = async (request: AccessRequest) => {
    const intent = `cancel:${request.request_ref}:${request.revision}`;
    setMutating(true);
    try {
      await accessApi.cancelRequest(request.request_ref, request.revision, intents.get(intent));
      intents.complete(intent);
      Toast.success('待审核请求已取消');
      await loadRequests();
    } catch (error) {
      intents.settleError(intent, error);
      Toast.error(errorText(error, '取消失败'));
      if (error instanceof ApiError && error.status === 409) await loadRequests();
    } finally {
      setMutating(false);
    }
  };

  const issueCredential = async (values: { name: string; allowed_ips?: string }) => {
    if (!selectedGrant) return;
    const intent = `issue:${selectedGrant.grant_ref}:${values.name.trim()}:${(values.allowed_ips || '').trim()}`;
    setMutating(true);
    try {
      const issued = await accessApi.issueCredential(selectedGrant.grant_ref, {
        name: values.name.trim(), allowed_ips: (values.allowed_ips || '').trim(),
        allowed_models: [], allowed_endpoints: [],
      }, intents.get(intent));
      intents.complete(intent);
      setCredentialDrawer(false);
      credentialForm?.reset();
      setSecret(issued);
      await loadCredentials();
    } catch (error) {
      intents.settleError(intent, error);
      Toast.error(errorText(error, '凭据签发失败'));
      if (error instanceof ApiError && error.status === 409) await loadCredentials();
    } finally {
      setMutating(false);
    }
  };

  const rotateCredential = async (family: AccessCredentialFamily) => {
    const active = family.versions.find((version) => version.status === 'active');
    if (!selectedGrant || !active) return;
    const intent = `rotate:${family.credential_ref}:${active.id}`;
    setMutating(true);
    try {
      const result = await accessApi.rotateCredential(selectedGrant.grant_ref, active.id, intents.get(intent));
      intents.complete(intent);
      setSecret(result.credential);
      await loadCredentials();
    } catch (error) {
      intents.settleError(intent, error);
      Toast.error(errorText(error, '凭据轮换失败'));
      if (error instanceof ApiError && error.status === 409) await loadCredentials();
    } finally {
      setMutating(false);
    }
  };

  const revokeCredential = (family: AccessCredentialFamily) => {
    const locator = family.versions.find((version) => version.status === 'active') || family.versions[0];
    if (!selectedGrant || !locator) return;
    Modal.confirm({
      title: '撤销逻辑凭据',
      content: '此操作会撤销整个凭据 family，所有 secret version 都将失效，且不能恢复。',
      onOk: async () => {
        const intent = `revoke:${family.credential_ref}:${locator.id}`;
        setMutating(true);
        try {
          await accessApi.revokeCredential(selectedGrant.grant_ref, locator.id, intents.get(intent));
          intents.complete(intent);
          Toast.success('逻辑凭据已撤销');
          await loadCredentials();
        } catch (error) {
          intents.settleError(intent, error);
          Toast.error(errorText(error, '撤销失败'));
          if (error instanceof ApiError && error.status === 409) await loadCredentials();
        } finally {
          setMutating(false);
        }
      },
    });
  };

  const requestColumns = useMemo(() => [
    { title: '请求', dataIndex: 'request_ref', render: (value: string) => <Text code>{value}</Text> },
    { title: '项目', dataIndex: 'project_id', width: 90, render: (value: number) => `#${value}` },
    { title: '范围', render: (_: unknown, row: AccessRequest) => `${row.requested_scope.project_role} · ${row.requested_scope.scope_mode}` },
    { title: '状态', dataIndex: 'status', width: 100, render: (value: string) => <StatusTag status={value}>{statusText[value] || value}</StatusTag> },
    { title: '策略原因', render: (_: unknown, row: AccessRequest) => (row.policy_reason_codes || []).join('、') || '—' },
    { title: '提交时间', dataIndex: 'submitted_at', render: (value: string) => formatDateTime(value) },
    { title: '操作', width: 90, render: (_: unknown, row: AccessRequest) => row.actions?.can_cancel ? <Button size="small" disabled={mutating} onClick={() => cancelRequest(row)}>取消</Button> : <Text type="tertiary">—</Text> },
  ], [mutating]);

  const grantColumns = useMemo(() => [
    { title: '授权', dataIndex: 'grant_ref', render: (value: string) => <Text code>{value}</Text> },
    { title: '项目', dataIndex: 'project_id', width: 90, render: (value: number) => `#${value}` },
    { title: '范围', render: (_: unknown, row: AccessGrant) => `${row.scope.project_role} · ${row.scope.scope_mode}` },
    { title: '状态', width: 100, render: (_: unknown, row: AccessGrant) => { const status = effectiveGrantStatus(row); return <StatusTag status={status}>{statusText[status] || status}</StatusTag>; } },
    { title: '有效期', render: (_: unknown, row: AccessGrant) => row.valid_until ? formatDateTime(row.valid_until) : '长期' },
    { title: '操作', width: 110, render: (_: unknown, row: AccessGrant) => <Button size="small" onClick={() => replaceSearch({ grant_ref: row.grant_ref })}>{row.grant_ref === selectedGrantRef ? '已选择' : '管理凭据'}</Button> },
  ], [replaceSearch, selectedGrantRef]);

  const credentialColumns = useMemo(() => [
    { title: '逻辑凭据', render: (_: unknown, row: AccessCredentialFamily) => <><Text strong>{row.name}</Text><br /><Text code>{row.credential_ref}</Text></> },
    { title: '有效状态', dataIndex: 'effective_status', render: (value: string) => <StatusTag status={value}>{statusText[value] || value}</StatusTag> },
    { title: '边界', render: (_: unknown, row: AccessCredentialFamily) => <Text type="tertiary">{row.allowed_endpoints.join(', ') || '项目默认端点'} · {row.rpm_limit} RPM / {row.rps_limit} RPS</Text> },
    { title: 'Secret versions', render: (_: unknown, row: AccessCredentialFamily) => <Space wrap>{row.versions.map((version) => <Tag key={version.id} color={version.status === 'active' ? 'green' : 'grey'}>{version.key_prefix} · {version.status}</Tag>)}</Space> },
    { title: '操作', width: 160, render: (_: unknown, row: AccessCredentialFamily) => <Space><Button size="small" disabled={mutating || row.effective_status !== 'active' || !selectedGrant || !isGrantCurrentlyActive(selectedGrant)} onClick={() => rotateCredential(row)}>原子轮换</Button><Button size="small" type="danger" disabled={mutating || row.status !== 'active'} onClick={() => revokeCredential(row)}>撤销</Button></Space> },
  ], [mutating, selectedGrant]);

  return (
    <div className="ur-page">
      <PageHeader eyebrow="访问与安全" title="我的访问" description="查看本人提交的请求、本人 Grant 与逻辑凭据。授权、凭据 family 和 secret version 是相互独立的事实。" actions={<Button icon={<IconRefresh />} onClick={refreshAccess}>刷新</Button>} />

      <Surface title="申请项目访问" description="仅显示当前账号已获准发现的项目；没有项目时不会扩大同组织项目枚举。">
        {projectError ? <EmptyState title="项目列表暂时不可用" description={projectError} actions={<Button onClick={loadProjects}>重试</Button>} /> : projects.length === 0 && !projectLoading ? (
          <EmptyState title="暂无可申请项目" description="请让管理员提供明确的项目加入路径；当前页面不会枚举同组织全部项目。" />
        ) : (
          <Space wrap>
            <Select loading={projectLoading} value={selectedProjectID} style={{ width: 280 }} placeholder="选择项目" optionList={projects.map(projectOption)} onChange={(value) => replaceSearch({ project_id: Number(value) })} />
            <Button type="primary" icon={<IconPlus />} disabled={!selectedProject} onClick={() => setRequestDrawer(true)}>提交访问请求</Button>
          </Space>
        )}
      </Surface>

      <Surface title="我的请求" description="Actor-owned、服务端 keyset 分页；请求失败不会清空 Grant 或凭据事实。" actions={<Button icon={<IconRefresh />} onClick={() => loadRequests()}>刷新</Button>}>
        {requestsError ? <EmptyState title="请求列表暂时不可用" description={requestsError} actions={<Button onClick={() => loadRequests()}>重试</Button>} /> : (
          <><Table columns={requestColumns} dataSource={requests} rowKey="id" loading={requestsLoading && requests.length === 0} pagination={false} empty={<EmptyState title="暂无访问请求" />} />{requestCursor && <Button block loading={requestsLoading} onClick={() => loadRequests(true)}>加载更早请求</Button>}</>
        )}
      </Surface>

      <Surface title="我的 Grant" description="包含有效、暂停、撤销和过期历史；不依赖当前项目枚举。" actions={<Button icon={<IconRefresh />} onClick={() => loadGrants()}>刷新</Button>}>
        {grantsError ? <EmptyState title="Grant 列表暂时不可用" description={grantsError} actions={<Button onClick={() => loadGrants()}>重试</Button>} /> : (
          <><Table columns={grantColumns} dataSource={grants} rowKey="id" loading={grantsLoading && grants.length === 0} pagination={false} empty={<EmptyState title="暂无个人 Grant" />} />{grantCursor && <Button block loading={grantsLoading} onClick={() => loadGrants(true)}>加载更早 Grant</Button>}</>
        )}
      </Surface>

      <Surface title="逻辑凭据与 Secret versions" description={selectedGrant ? `当前 Grant：${selectedGrant.grant_ref}。Secret 只在签发/轮换成功后的当前内存弹层显示一次。` : '先从上方选择一个 Grant。'} actions={selectedGrant && isGrantCurrentlyActive(selectedGrant) ? <Button icon={<IconKeyStroked />} type="primary" onClick={() => setCredentialDrawer(true)}>签发逻辑凭据</Button> : undefined}>
        {!selectedGrant ? <EmptyState title="尚未选择 Grant" /> : credentialsError ? <EmptyState title="凭据列表暂时不可用" description={credentialsError} actions={<Button onClick={() => loadCredentials()}>重试</Button>} /> : (
          <><Table columns={credentialColumns} dataSource={credentials} rowKey="id" loading={credentialsLoading && credentials.length === 0} pagination={false} empty={<EmptyState title="此 Grant 暂无逻辑凭据" />} />{credentialCursor && <Button block loading={credentialsLoading} onClick={() => loadCredentials(true)}>加载更多凭据</Button>}</>
        )}
      </Surface>

      <Modal title="提交访问请求" visible={requestDrawer} onCancel={() => setRequestDrawer(false)} footer={null} closeOnEsc={!mutating}>
        <Form getFormApi={setRequestForm} onSubmit={submitRequest}>
          <Form.Select field="project_role" label="请求的项目角色" initValue="project_user" optionList={[{ value: 'project_user', label: '项目使用者' }, { value: 'project_admin', label: '项目管理员（通常需要审核）' }]} rules={[{ required: true, message: '请选择项目角色' }]} style={{ width: '100%' }} />
          <Text type="tertiary">默认请求动态项目模型目录与 chat / embeddings / models 端点。额外模型、端点或角色提升由策略进入审核。</Text>
          <div style={{ marginTop: 20, textAlign: 'right' }}><Button onClick={() => setRequestDrawer(false)}>取消</Button><Button type="primary" htmlType="submit" loading={mutating} style={{ marginLeft: 8 }}>提交</Button></div>
        </Form>
      </Modal>

      <Modal title="签发逻辑凭据" visible={credentialDrawer} onCancel={() => setCredentialDrawer(false)} footer={null} closeOnEsc={!mutating}>
        <Form getFormApi={setCredentialForm} onSubmit={issueCredential}>
          <Form.Input field="name" label="凭据名称" rules={[{ required: true, message: '请输入名称' }]} />
          <Form.Input field="allowed_ips" label="IP / CIDR（可选）" placeholder="例如 203.0.113.10/32" extraText="留空表示不增加凭据级 IP 收窄；服务端会规范化并验证。" />
          <div style={{ textAlign: 'right' }}><Button onClick={() => setCredentialDrawer(false)}>取消</Button><Button type="primary" htmlType="submit" loading={mutating} style={{ marginLeft: 8 }}>签发</Button></div>
        </Form>
      </Modal>

      <Modal title="一次性 Secret" visible={!!secret} onCancel={() => setSecret(null)} onOk={() => setSecret(null)} okText="我已安全保存" cancelButtonProps={{ style: { display: 'none' } }} closeOnEsc={false} maskClosable={false}>
        {secret?.secret_available && secret.key ? (
          <><Text type="warning">关闭后前端不会保留或再次显示此 secret。</Text><div style={{ marginTop: 16, wordBreak: 'break-all' }}><Text code copyable>{secret.key}</Text></div></>
        ) : (
          <Text type="warning">本次是已提交操作的幂等重放，原 secret 不可恢复。{secret?.recovery_action ? `恢复动作：${secret.recovery_action}` : '请对当前 active version 执行新的原子轮换。'}</Text>
        )}
      </Modal>
    </div>
  );
}
