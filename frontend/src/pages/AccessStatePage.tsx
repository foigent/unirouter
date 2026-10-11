import '../i18n/access';
import { translate as t, translateLabel, useI18n } from '../i18n';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Banner, Button, Form, Modal, Select, Space, Table, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconKeyStroked, IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { accessApi, effectiveGrantStatus, isGrantCurrentlyActive, type AccessCredentialFamily, type AccessGrant, type AccessRequest, type IssuedCredential } from '../api/access';
import { ApiError, api } from '../api/client';
import { useAuthStore } from '../store/auth';
import { EmptyState, PageHeader, StatusTag, Surface } from '../ui';
import { formatDateTime } from '../utils/formatDate';
import { accessReasonLabel, accessScopeLabel } from '../utils/accessDisplay';
import { IntentRequestIDs } from '../utils/intentRequestId';
import { loadAllPages } from '../utils/loadAllPages';
import { projectDisplayName, projectOption } from '../utils/projectDisplay';

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
  disabled_by_grant: '访问授权已失效', subject_blocked: '账号或应用不可用', no_active_version: '没有有效版本',
  rotated: '已轮换', grant_expired: '访问授权已过期', grant_suspended: '访问授权已暂停', grant_revoked: '访问授权已撤销',
};

const errorText = (error: unknown, fallback: string) => {
  if (error instanceof ApiError && error.status === 409) return t("状态已发生变化，已刷新当前事实，请重新确认。");
  if (error instanceof ApiError && error.status === 403) return t("当前账号没有执行此操作的实时权限。");
  return fallback;
};

export default function AccessStatePage() {
  const { locale } = useI18n();
  const user = useAuthStore((state) => state.user);
  const navigate = useNavigate();
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
  const selectedProjectMembershipRole = (selectedProject?.current_user_membership_role || '').trim();
  const alreadyProjectMember = selectedProjectMembershipRole !== '';
  const visibleProjectName = (projectID: number) => {
    const project = projects.find((item) => item.id === projectID);
    return project ? projectDisplayName(project) : t("历史或当前不可见项目");
  };

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
      setCredentialsError('无法读取此授权的 API Key；授权记录仍保留在上方。');
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
      Toast.success(result.request.status === 'approved' ? t("访问已批准") : t("访问请求已提交审核"));
      await refreshAccess();
    } catch (error) {
      intents.settleError(intent, error);
      Toast.error(errorText(error, t("提交失败；如网络结果不确定，可直接重试同一意图。")));
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
      Toast.success(t("待审核请求已取消"));
      await loadRequests();
    } catch (error) {
      intents.settleError(intent, error);
      Toast.error(errorText(error, t("取消失败")));
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
      Toast.error(errorText(error, t("API Key 创建失败")));
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
      Toast.error(errorText(error, t("API Key 轮换失败")));
      if (error instanceof ApiError && error.status === 409) await loadCredentials();
    } finally {
      setMutating(false);
    }
  };

  const revokeCredential = (family: AccessCredentialFamily) => {
    const locator = family.versions.find((version) => version.status === 'active') || family.versions[0];
    if (!selectedGrant || !locator) return;
    Modal.confirm({
      okText: t("确认操作"),
      cancelText: t("取消"),
      title: t("撤销 API Key"),
      content: t("此操作会使这个 Key 的全部历史版本立即失效，且不能恢复。"),
      onOk: async () => {
        const intent = `revoke:${family.credential_ref}:${locator.id}`;
        setMutating(true);
        try {
          await accessApi.revokeCredential(selectedGrant.grant_ref, locator.id, intents.get(intent));
          intents.complete(intent);
          Toast.success(t("API Key 已撤销"));
          await loadCredentials();
        } catch (error) {
          intents.settleError(intent, error);
          Toast.error(errorText(error, t("撤销失败")));
          if (error instanceof ApiError && error.status === 409) await loadCredentials();
        } finally {
          setMutating(false);
        }
      },
    });
  };

  const requestColumns = useMemo(() => [
    { title: t("项目"), render: (_: unknown, row: AccessRequest) => <><Text strong>{visibleProjectName(row.project_id)}</Text><details><summary>{t("高级审计标识")}</summary><Text code>{row.request_ref}</Text></details></> },
    { title: t("申请范围"), render: (_: unknown, row: AccessRequest) => accessScopeLabel(row.requested_scope.project_role, row.requested_scope.scope_mode) },
    { title: t("状态"), dataIndex: 'status', width: 100, render: (value: string) => <StatusTag status={value}>{Object.prototype.hasOwnProperty.call(statusText, value) ? translateLabel(statusText[value]) : value}</StatusTag> },
    { title: t("原因"), render: (_: unknown, row: AccessRequest) => (row.policy_reason_codes || []).map(accessReasonLabel).join('、') || '—' },
    { title: t("提交时间"), dataIndex: 'submitted_at', render: (value: string) => formatDateTime(value) },
    { title: t("操作"), width: 90, render: (_: unknown, row: AccessRequest) => row.actions?.can_cancel ? <Button size="small" disabled={mutating} onClick={() => cancelRequest(row)}>{t("取消")}</Button> : <Text type="tertiary">—</Text> },
  ], [mutating, projects, locale]);

  const grantColumns = useMemo(() => [
    { title: t("项目"), render: (_: unknown, row: AccessGrant) => <><Text strong>{visibleProjectName(row.project_id)}</Text><details><summary>{t("高级审计标识")}</summary><Text code>{row.grant_ref}</Text></details></> },
    { title: t("可用范围"), render: (_: unknown, row: AccessGrant) => accessScopeLabel(row.scope.project_role, row.scope.scope_mode) },
    { title: t("状态"), width: 100, render: (_: unknown, row: AccessGrant) => { const status = effectiveGrantStatus(row); return <StatusTag status={status}>{Object.prototype.hasOwnProperty.call(statusText, status) ? translateLabel(statusText[status]) : status}</StatusTag>; } },
    { title: t("有效期"), render: (_: unknown, row: AccessGrant) => row.valid_until ? formatDateTime(row.valid_until) : t("长期") },
    { title: t("操作"), width: 120, render: (_: unknown, row: AccessGrant) => <Button size="small" onClick={() => replaceSearch({ grant_ref: row.grant_ref })}>{row.grant_ref === selectedGrantRef ? t("正在管理") : t("管理 API Key")}</Button> },
  ], [projects, replaceSearch, selectedGrantRef, locale]);

  const credentialColumns = useMemo(() => [
    { title: 'API Key', render: (_: unknown, row: AccessCredentialFamily) => <><Text strong>{row.name}</Text><details><summary>{t("高级审计标识")}</summary><Text code>{row.credential_ref}</Text></details></> },
    { title: t("有效状态"), dataIndex: 'effective_status', render: (value: string) => <StatusTag status={value}>{Object.prototype.hasOwnProperty.call(statusText, value) ? translateLabel(statusText[value]) : value}</StatusTag> },
    { title: t("使用限制"), render: (_: unknown, row: AccessCredentialFamily) => <Text type="tertiary">{row.allowed_endpoints.join(', ') || t("跟随项目接口")} · {t('每分钟 {rpm} 次 / 每秒 {rps} 次', { rpm: row.rpm_limit, rps: row.rps_limit })}</Text> },
    { title: t("Key 版本"), render: (_: unknown, row: AccessCredentialFamily) => <Space wrap>{row.versions.map((version) => <Tag key={version.id} color={version.status === 'active' ? 'green' : 'grey'}>{version.key_prefix} · {Object.prototype.hasOwnProperty.call(statusText, version.status) ? translateLabel(statusText[version.status]) : version.status}</Tag>)}</Space> },
    { title: t("操作"), width: 160, render: (_: unknown, row: AccessCredentialFamily) => <Space><Button size="small" disabled={mutating || row.effective_status !== 'active' || !selectedGrant || !isGrantCurrentlyActive(selectedGrant)} onClick={() => rotateCredential(row)}>{t("安全轮换")}</Button><Button size="small" type="danger" disabled={mutating || row.status !== 'active'} onClick={() => revokeCredential(row)}>{t("撤销")}</Button></Space> },
  ], [mutating, selectedGrant, locale]);

  return (
    <div className="ur-page ur-access-workspace">
      <PageHeader eyebrow={t("访问与安全")} title={t("访问申请与授权")} description={t("申请尚未加入的项目，并查看正式授权与 API Key 生命周期。已经加入项目的日常调用入口是“项目工作台”。")} actions={<Button icon={<IconRefresh />} onClick={refreshAccess}>{t("刷新")}</Button>} />

      {alreadyProjectMember ? (
        <Banner
          type="info"
          closeIcon={null}
          description={t('你已经是“{name}”的项目成员。创建个人 Key、调试调用和查看模型请使用项目工作台；如需留下正式访问申请记录或申请项目管理员权限，仍可在本页提交。', { name: projectDisplayName(selectedProject!) })}
          style={{ marginBottom: 16 }}
        >
          <Button size="small" onClick={() => navigate('/developer')}>{t("进入项目工作台")}</Button>
        </Banner>
      ) : null}

      <Surface title={t("申请项目访问")} description={t("用于建立正式访问记录或申请更高项目角色；页面只显示当前账号已获准发现的项目，不会扩大同组织项目枚举。")}>
        {projectError ? <EmptyState title={t("项目列表暂时不可用")} description={translateLabel(projectError)} actions={<Button onClick={loadProjects}>{t("重试")}</Button>} /> : projects.length === 0 && !projectLoading ? (
          <EmptyState title={t("暂无可申请项目")} description={t("请让管理员提供明确的项目加入路径；当前页面不会枚举同组织全部项目。")} />
        ) : (
          <Space wrap>
            <Select aria-label={t("选择项目")} loading={projectLoading} value={selectedProjectID} style={{ width: 280 }} placeholder={t("选择项目")} optionList={projects.map(projectOption)} onChange={(value) => replaceSearch({ project_id: Number(value) })} />
            <Button type="primary" icon={<IconPlus />} disabled={!selectedProject} onClick={() => setRequestDrawer(true)}>{t("提交访问请求")}</Button>
          </Space>
        )}
      </Surface>

      <Surface title={t("我的访问申请")} description={t("查看申请状态和需要审核的原因。列表读取失败不会影响已经生效的授权或 API Key。")} actions={<Button icon={<IconRefresh />} onClick={() => loadRequests()}>{t("刷新")}</Button>}>
        {requestsError ? <EmptyState title={t("请求列表暂时不可用")} description={translateLabel(requestsError)} actions={<Button onClick={() => loadRequests()}>{t("重试")}</Button>} /> : (
          <><Table columns={requestColumns} dataSource={requests} rowKey="id" loading={requestsLoading && requests.length === 0} pagination={false} empty={<EmptyState title={t("暂无访问请求")} />} />{requestCursor && <Button block loading={requestsLoading} onClick={() => loadRequests(true)}>{t("加载更早请求")}</Button>}</>
        )}
      </Surface>

      <Surface title={t("我的有效与历史授权")} description={t("包含有效、暂停、撤销和过期记录；选择一项有效授权后即可管理对应 API Key。")} actions={<Button icon={<IconRefresh />} onClick={() => loadGrants()}>{t("刷新")}</Button>}>
        {grantsError ? <EmptyState title={t("授权列表暂时不可用")} description={translateLabel(grantsError)} actions={<Button onClick={() => loadGrants()}>{t("重试")}</Button>} /> : (
          <><Table columns={grantColumns} dataSource={grants} rowKey="id" loading={grantsLoading && grants.length === 0} pagination={false} empty={<EmptyState title={t("暂无访问授权")} />} />{grantCursor && <Button block loading={grantsLoading} onClick={() => loadGrants(true)}>{t("加载更早授权")}</Button>}</>
        )}
      </Surface>

      <Surface title={t("我的 API Key")} description={selectedGrant ? t('正在管理“{name}”的 API Key。新密钥只显示一次，请立即安全保存。', { name: visibleProjectName(selectedGrant.project_id) }) : t("请先从上方选择一项有效授权。")} actions={selectedGrant && isGrantCurrentlyActive(selectedGrant) ? <Button icon={<IconKeyStroked />} type="primary" onClick={() => setCredentialDrawer(true)}>{t("创建 API Key")}</Button> : undefined}>
        {!selectedGrant ? <EmptyState title={t("尚未选择访问授权")} /> : credentialsError ? <EmptyState title={t("API Key 列表暂时不可用")} description={translateLabel(credentialsError)} actions={<Button onClick={() => loadCredentials()}>{t("重试")}</Button>} /> : (
          <><Table columns={credentialColumns} dataSource={credentials} rowKey="id" loading={credentialsLoading && credentials.length === 0} pagination={false} empty={<EmptyState title={t("此授权尚未创建 API Key")} />} />{credentialCursor && <Button block loading={credentialsLoading} onClick={() => loadCredentials(true)}>{t("加载更多 API Key")}</Button>}</>
        )}
      </Surface>

      <Modal title={t("提交访问请求")} visible={requestDrawer} onCancel={() => setRequestDrawer(false)} footer={null} closeOnEsc={!mutating}>
        <Form getFormApi={setRequestForm} onSubmit={submitRequest}>
          <Form.Select field="project_role" aria-label={t("请求的项目角色")} label={t("请求的项目角色")} initValue="project_user" optionList={[{ value: 'project_user', label: t("项目使用者") }, { value: 'project_admin', label: t("项目管理员（通常需要审核）") }]} rules={[{ required: true, message: t("请选择项目角色") }]} style={{ width: '100%' }} />
          <Text type="tertiary">{t("默认请求动态项目模型目录与 chat / embeddings / models 端点。额外模型、端点或角色提升由策略进入审核。")}</Text>
          <div style={{ marginTop: 20, textAlign: 'right' }}><Button onClick={() => setRequestDrawer(false)}>{t("取消")}</Button><Button type="primary" htmlType="submit" loading={mutating} style={{ marginLeft: 8 }}>{t("提交")}</Button></div>
        </Form>
      </Modal>

      <Modal title={t("创建 API Key")} visible={credentialDrawer} onCancel={() => setCredentialDrawer(false)} footer={null} closeOnEsc={!mutating}>
        <Form getFormApi={setCredentialForm} onSubmit={issueCredential}>
          <Form.Input field="name" label={t("Key 名称")} placeholder={t("例如：我的开发电脑")} rules={[{ required: true, message: t("请输入名称") }]} />
          <Form.Input field="allowed_ips" label={t("允许的 IP / CIDR（可选）")} placeholder={t("例如 203.0.113.10/32")} extraText={t("留空表示不额外限制来源 IP；填写后只有这些地址可以使用此 Key。")} />
          <div style={{ textAlign: 'right' }}><Button onClick={() => setCredentialDrawer(false)}>{t("取消")}</Button><Button type="primary" htmlType="submit" loading={mutating} style={{ marginLeft: 8 }}>{t("创建")}</Button></div>
        </Form>
      </Modal>

      <Modal title={t("一次性 API Key")} visible={!!secret} onCancel={() => setSecret(null)} onOk={() => setSecret(null)} okText={t("我已安全保存")} cancelButtonProps={{ style: { display: 'none' } }} closeOnEsc={false} maskClosable={false}>
        {secret?.secret_available && secret.key ? (
          <><Text type="warning">{t("关闭后系统不会保留或再次显示完整 Key。")}</Text><div style={{ marginTop: 16, wordBreak: 'break-all' }}><Text code copyable>{secret.key}</Text></div></>
        ) : (
          <Text type="warning">{t("这次请求是之前成功操作的安全重放，原 Key 无法再次显示。请对当前有效版本执行一次安全轮换以获得新 Key。")}</Text>
        )}
      </Modal>
    </div>
  );
}
