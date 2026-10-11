import '../i18n/models';
import { translate as t, useI18n } from '../i18n';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Banner, Button, Input, Modal, SideSheet, Space, Table, Toast, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { useSearchParams } from 'react-router-dom';
import {
  canonicalOperationPaths,
  modelDeploymentApi,
  type CanonicalCredentialBinding,
  type CanonicalModelApplication,
  type CanonicalModelApplicationCreateResult,
  type CanonicalModelApplicationProjection,
  type CanonicalOperation,
  type CanonicalOperationID,
} from '../api/modelDeployments';
import ModelEndpointBatchSheet from '../features/modelEndpoints/ModelEndpointBatchSheet';
import ModelEndpointCreateSheet from '../features/modelEndpoints/ModelEndpointCreateSheet';
import ModelEndpointDetailsSheet from '../features/modelEndpoints/ModelEndpointDetailsSheet';
import ModelReleaseWorkspace from '../features/modelEndpoints/ModelReleaseWorkspace';
import ModelGovernancePage from './ModelGovernancePage';
import ModelReadinessPage from './ModelReadinessPage';
import { endpointError } from '../features/modelEndpoints/presentation';
import {
  hasLicensedFeature,
  isOrgAdminRole,
  isOrgViewerRole,
  licenseRestrictionMessage,
  organizationRoleForUser,
} from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, MetricTile, PageHeader, StatusTag, Surface } from '../ui';

const { Text } = Typography;

type ApplicationOperationRow = {
  key: string;
  application: CanonicalModelApplication;
  operation: CanonicalOperation;
};

function positiveInteger(value: string | null) {
  if (!value || !/^[1-9]\d*$/.test(value)) return 0;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) ? parsed : 0;
}

function canonicalOperationID(value: string | null): CanonicalOperationID | undefined {
  return value && Object.prototype.hasOwnProperty.call(canonicalOperationPaths, value)
    ? value as CanonicalOperationID
    : undefined;
}

function axisTone(state: string) {
  if (['enabled', 'ready', 'success', 'live_probed', 'stable', 'canary'].includes(state)) return 'success';
  if (['disabled', 'paused', 'missing', 'unassigned', 'not_evaluated', 'stale', 'declared'].includes(state)) return 'warning';
  if (['failed', 'blocked', 'retired', 'open'].includes(state)) return 'failed';
  return state;
}

export default function ModelEndpointsPage() {
  useI18n();
  const [search, setSearch] = useSearchParams();
  const user = useAuthStore(state => state.user);
  const organizationRole = organizationRoleForUser(user);
  const canManage = isOrgAdminRole(organizationRole);
  const canApproveCapabilities = organizationRole === 'platform_admin';
  const canRead = !user || canManage || isOrgViewerRole(organizationRole);
  const gatewayLicensed = hasLicensedFeature(user?.license_access, 'gateway');
  const gatewayRestriction = user?.license_access ? licenseRestrictionMessage(user.license_access, 'gateway') : '';
  const [projection, setProjection] = useState<CanonicalModelApplicationProjection | null>(null);
  const [detail, setDetail] = useState<CanonicalModelApplication | null>(null);
  const [loading, setLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [detailError, setDetailError] = useState<unknown>(null);
  const [filter, setFilter] = useState('');
  const [entryOpen, setEntryOpen] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [batchInitial, setBatchInitial] = useState({ connectionID: 0, bindingID: 0 });
  const [batchOpen, setBatchOpen] = useState(false);
  const [workingBindingID, setWorkingBindingID] = useState(0);
  const listVersion = useRef(0);
  const detailVersion = useRef(0);

  const deploymentID = positiveInteger(search.get('deployment_id'));
  const operationID = canonicalOperationID(search.get('operation_id'));
  const selectedReleaseRef = String(search.get('release_ref') || '').trim();
  const selectedView = search.get('view');
  const auxiliaryView = selectedView === 'catalog' || selectedView === 'diagnostics';
  const hasLegacyIdentity = Boolean(search.get('channel_id') || search.get('model')) && !(deploymentID && operationID);

  const load = useCallback(async () => {
    if (!canRead || auxiliaryView) {
      setProjection(null);
      setLoading(false);
      return;
    }
    const version = ++listVersion.current;
    setLoading(true);
    setLoadError(null);
    try {
      const result = await modelDeploymentApi.list();
      if (version === listVersion.current) setProjection(result);
    } catch (error) {
      if (version !== listVersion.current) return;
      setProjection(null);
      setLoadError(error);
    } finally {
      if (version === listVersion.current) setLoading(false);
    }
  }, [auxiliaryView, canRead]);

  useEffect(() => { void load(); }, [load]);

  useEffect(() => {
    if (!canRead || auxiliaryView || !deploymentID || !operationID) {
      setDetail(null);
      setDetailError(null);
      setDetailLoading(false);
      return;
    }
    const version = ++detailVersion.current;
    setDetail(null);
    setDetailLoading(true);
    setDetailError(null);
    void modelDeploymentApi.get(deploymentID, operationID).then(result => {
      if (version === detailVersion.current) setDetail(result);
    }).catch(error => {
      if (version !== detailVersion.current) return;
      setDetail(null);
      setDetailError(error);
    }).finally(() => {
      if (version === detailVersion.current) setDetailLoading(false);
    });
  }, [auxiliaryView, canRead, deploymentID, operationID]);

  const selectView = (view?: 'catalog' | 'diagnostics') => {
    const next = new URLSearchParams(search);
    if (view) next.set('view', view);
    else next.delete('view');
    next.delete('deployment_id');
    next.delete('operation_id');
    next.delete('release_ref');
    next.delete('channel_id');
    next.delete('model');
    next.delete('from');
    next.delete('tab');
    setSearch(next, { replace: true });
  };

  const viewNavigation = <Space wrap>
    <Button type={!auxiliaryView ? 'primary' : 'tertiary'} onClick={() => selectView()}>{t("模型执行目标")}</Button>
    <Button type={selectedView === 'catalog' ? 'primary' : 'tertiary'} onClick={() => selectView('catalog')}>{t("公共模型目录")}</Button>
    <Button type={selectedView === 'diagnostics' ? 'primary' : 'tertiary'} onClick={() => selectView('diagnostics')}>{t("执行诊断")}</Button>
  </Space>;

  const rows = useMemo<ApplicationOperationRow[]>(() => (projection?.applications || []).flatMap(application =>
    application.operations.map(operation => ({
      key: `${application.deployment.id}:${operation.operation_id}`,
      application,
      operation,
    }))), [projection]);
  const filteredRows = useMemo(() => {
    const keyword = filter.trim().toLowerCase();
    if (!keyword) return rows;
    return rows.filter(({ application, operation }) => [
      application.public_model.model_name,
      application.public_model.display_name,
      String(application.deployment.id),
      application.deployment.upstream_model,
      application.connection.name,
      application.connection.provider,
      operation.operation_id,
      canonicalOperationPaths[operation.operation_id],
    ].some(value => value.toLowerCase().includes(keyword)));
  }, [filter, rows]);

  const exactDetail = detail?.deployment.id === deploymentID ? detail : undefined;
  const selectedOperation = exactDetail?.operations.find(operation => operation.operation_id === operationID);

  const selectExactIdentity = (selectedDeploymentID: number, selectedOperationID: CanonicalOperationID) => {
    const next = new URLSearchParams(search);
    next.set('deployment_id', String(selectedDeploymentID));
    next.set('operation_id', selectedOperationID);
    next.delete('channel_id');
    next.delete('model');
    next.delete('from');
    next.delete('tab');
    next.delete('view');
    setSearch(next, { replace: true });
  };

  const closeDetail = () => {
    detailVersion.current += 1;
    setDetail(null);
    setDetailError(null);
    const next = new URLSearchParams(search);
    next.delete('deployment_id');
    next.delete('operation_id');
    setSearch(next, { replace: true });
  };

  const selectRelease = (releaseRef: string) => {
    const next = new URLSearchParams(search);
    next.set('release_ref', releaseRef);
    setSearch(next, { replace: true });
  };

  const inspectReleaseFromDetail = (releaseRef: string) => {
    detailVersion.current += 1;
    setDetail(null);
    setDetailError(null);
    const next = new URLSearchParams(search);
    next.delete('deployment_id');
    next.delete('operation_id');
    next.set('release_ref', releaseRef);
    setSearch(next, { replace: true });
  };

  const refreshCanonicalState = useCallback(async () => {
    await load();
    if (!canRead || !deploymentID || !operationID) return;
    const version = ++detailVersion.current;
    try {
      const refreshed = await modelDeploymentApi.get(deploymentID, operationID);
      if (version === detailVersion.current) setDetail(refreshed);
    } catch (error) {
      if (version !== detailVersion.current) return;
      setDetail(null);
      setDetailError(error);
    }
  }, [canRead, deploymentID, load, operationID]);

  const created = async (result: CanonicalModelApplicationCreateResult) => {
    setCreateOpen(false);
    const operation = result.application.operations[0];
    await load();
    if (operation) selectExactIdentity(result.application.deployment.id, operation.operation_id);
    if (operation?.operation_id === 'openai.chat_completions.v1') {
      // The server identifies the exact fresh binding. A replay without that
      // identity requires an explicit binding selection in the next step.
      setProjection(current => current ? {
        ...current,
        applications: [result.application, ...current.applications.filter(a => a.deployment.id !== result.application.deployment.id)],
      } : current);
      setBatchInitial({ connectionID: result.application.connection.id, bindingID: result.selected_credential_binding_id || 0 });
      setBatchOpen(true);
    }
  };

  const executeProbe = async (binding: CanonicalCredentialBinding) => {
    if (!detail || !selectedOperation || !canManage || !gatewayLicensed) return;
    setWorkingBindingID(binding.id);
    try {
      const result = await modelDeploymentApi.probe(detail.deployment.id, selectedOperation.operation_id, {
        credential_binding_id: binding.id,
      });
      if (result.success && result.current) {
        Toast.success(t('Deployment #{id} · {operation} 精确探测通过', { id: result.deployment_id, operation: result.operation_id }));
      } else if (result.provider_success && result.stale) {
        Toast.warning(t("供应商请求成功，但提交后的 revision 已变化；结果仅保留为历史证据"));
      } else {
        Toast.error(t('精确探测未通过：{reason}', { reason: result.reason_code || result.provider_reason_code }));
      }
      await load();
      const refreshed = await modelDeploymentApi.get(detail.deployment.id, selectedOperation.operation_id);
      setDetail(refreshed);
    } catch (error) {
      Toast.error(endpointError(error, t("Canonical operation 精确探测失败")));
      await load();
    } finally {
      setWorkingBindingID(0);
    }
  };

  const confirmProbe = (binding: CanonicalCredentialBinding) => {
    if (!detail || !selectedOperation || !canManage || !gatewayLicensed) return;
    Modal.confirm({
      title: t('探测 Deployment #{id} · {operation}？', { id: detail.deployment.id, operation: selectedOperation.operation_id }),
      content: <div>
        <p>{t("将向真实供应商发起一次最小请求，可能产生少量供应商费用。")}</p>
        <p><Text code>deployment_id={detail.deployment.id}</Text></p>
        <p><Text code>operation_id={selectedOperation.operation_id}</Text></p>
        <p><Text code>credential_binding_id={binding.id}</Text></p>
        <p>{t("服务端只解析这组精确绑定；不会回退到同模型、同连接或其他 credential。")}</p>
      </div>,
      okText: t("发起精确探测"),
      onOk: () => executeProbe(binding),
    });
  };

  const columns = [
    {
      title: t("公共模型"), key: 'model', width: 210,
      render: (_: unknown, row: ApplicationOperationRow) => <div className="ur-model-endpoint-name">
        <Text strong>{row.application.public_model.display_name || row.application.public_model.model_name}</Text>
        <Text code size="small">{row.application.public_model.model_name}</Text>
      </div>,
    },
    {
      title: 'Deployment', key: 'deployment', width: 210,
      render: (_: unknown, row: ApplicationOperationRow) => <div className="ur-model-endpoint-name">
        <Text code>deployment_id={row.application.deployment.id}</Text>
        <Text>{row.application.deployment.upstream_model}</Text>
        <Text type="tertiary" size="small">{row.application.connection.name} · {row.application.connection.provider}</Text>
      </div>,
    },
    {
      title: 'Operation', key: 'operation', width: 250,
      render: (_: unknown, row: ApplicationOperationRow) => <div className="ur-model-endpoint-name">
        <Text code>{row.operation.operation_id}</Text>
        <Text type="tertiary" size="small">{canonicalOperationPaths[row.operation.operation_id]}</Text>
      </div>,
    },
    {
      title: t("独立状态轴"), key: 'axes',
      render: (_: unknown, row: ApplicationOperationRow) => <div className="ur-model-endpoint-targets">
        <StatusTag status={axisTone(row.application.connection.admin_state)}>{t("连接")} {row.application.connection.admin_state}</StatusTag>
        <StatusTag status={axisTone(row.application.deployment.admin_state)}>Deployment {row.application.deployment.admin_state}</StatusTag>
        <StatusTag status={axisTone(row.application.credential.state)}>{t("凭证")} {row.application.credential.state}</StatusTag>
        <StatusTag status={axisTone(row.operation.probe_state)}>{t("探测")} {row.operation.probe_state}</StatusTag>
        <StatusTag status={axisTone(row.operation.runtime.state)}>{t("运行时")} {row.operation.runtime.state}</StatusTag>
        <StatusTag status={axisTone(row.application.callability.state)}>{t("调用")} {row.application.callability.state}</StatusTag>
      </div>,
    },
    {
      title: t("操作"), key: 'actions', width: 100,
      render: (_: unknown, row: ApplicationOperationRow) => <Button
        type="primary"
        theme="borderless"
        onClick={(event) => {
          event.stopPropagation();
          selectExactIdentity(row.application.deployment.id, row.operation.operation_id);
        }}
      >{t("查看详情")}</Button>,
    },
  ];

  if (!canRead) {
    return <div className="ur-page ur-page-wide">
      <PageHeader eyebrow={t("模型接入")} title={t("模型接入")} description={t("供应商连接与模型执行目标只对平台管理员、组织管理员和组织只读角色开放。")} />
      <EmptyState title={t("当前身份无权读取供应商部署")} description={t("项目成员不会收到 provider、Base URL、credential 或 deployment 管理状态。请从项目工作台使用已授权的公共模型。")} minHeight={360} />
    </div>;
  }

  if (selectedView === 'catalog') {
    return <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow={t("模型接入")}
        title={t("公共模型目录")}
        description={t("维护经过验证的模型执行边界、企业别名和项目可见范围；不会修改 canonical runtime。")}
      />
      <nav className="ur-workspace-tabs">{viewNavigation}</nav>
      <ModelGovernancePage embedded />
    </div>;
  }

  if (selectedView === 'diagnostics') {
    return <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow={t("模型接入")}
        title={t("执行诊断")}
        description={t("按 deployment_id + operation_id 检查调用者、路由、凭证、探测、运行时与授权事实。")}
      />
      <nav className="ur-workspace-tabs">{viewNavigation}</nav>
      <ModelReadinessPage embedded forceConnectionMode />
    </div>;
  }

  return <div className="ur-page ur-page-wide">
    <PageHeader
      eyebrow={t("模型接入")}
      title={t("模型接入")}
      description={t("连接模型平台，复用凭证批量配置模型，并分别查看验证、发布和应用授权状态。")}
      actions={<Space>
        <Button icon={<IconRefresh />} loading={loading} onClick={() => void load()}>{t("刷新")}</Button>
        {canManage && <Button
          icon={<IconPlus />}
          type="primary"
          disabled={!gatewayLicensed}
          title={!gatewayLicensed ? gatewayRestriction : undefined}
          onClick={() => setEntryOpen(true)}
        >{t("接入模型")}</Button>}
      </Space>}
    />

    <nav className="ur-workspace-tabs">{viewNavigation}</nav>

    {!canManage && <Banner
      type="info"
      closeIcon={null}
      description={t("当前为组织只读视图：可读取 canonical list/detail 和五轴状态；不展示 credential binding 身份，也不提供创建、探测或发布操作。")}
      style={{ marginBottom: 16 }}
    />}
    {canManage && !gatewayLicensed && <Banner
      type="warning"
      closeIcon={null}
      description={t('{reason} Canonical 状态仍可读取；创建与真实探测保持禁用。', { reason: gatewayRestriction || t('模型网关商业授权不可用。') })}
      style={{ marginBottom: 16 }}
    />}
    {hasLegacyIdentity && <Banner
      type="warning"
      closeIcon={null}
      description={t("旧 channel_id/public_model 深链不是 canonical deployment 身份，系统不会猜测映射。请从列表选择精确 deployment_id + operation_id。")}
      style={{ marginBottom: 16 }}
    />}
    {!!detailError && <Banner
      type="danger"
      closeIcon={null}
      description={endpointError(detailError, t('无法读取该 deployment 与 operation 的精确详情。'))}
      style={{ marginBottom: 16 }}
    />}

    <div className="ur-model-endpoint-kpis">
      <MetricTile label={t("执行目标")} value={projection?.summary.deployments ?? '-'} meta={t("持久 deployment identity")} />
      <MetricTile label={t("凭证可选")} value={projection?.summary.credentials_ready ?? '-'} tone="green" meta={t('暂停连接 {count}', { count: projection?.summary.connections_paused ?? '—' })} />
      <MetricTile label={t("探测就绪")} value={projection?.summary.probes_ready ?? '-'} tone="blue" meta={t("operation 级当前证据")} />
      <MetricTile label={t("运行时成员")} value={projection?.summary.runtime_assigned_operations ?? '-'} tone="amber" meta={t("不等同调用者已授权")} />
    </div>

    <Surface
      title={t("模型执行目标")}
      description={t("每行对应一个精确 Deployment × Operation；可查看内部持久身份，系统不会按公共模型或旧 channel 回退选择。")}
      actions={<Input
        aria-label={t("搜索模型执行目标")}
        prefix={<IconSearch />}
        value={filter}
        onChange={setFilter}
        placeholder={t("模型、deployment、operation 或供应商")}
        style={{ width: 320 }}
      />}
    >
      {loadError ? <EmptyState title={t("模型接入状态暂时不可用")} description={endpointError(loadError, t('无法读取模型接入状态。'))} actions={<Button type="primary" onClick={() => void load()}>{t("重试")}</Button>} minHeight={320} />
        : loading && !projection ? <EmptyState title={t("正在读取模型执行目标")} description={t("正在加载持久身份与五轴状态。")} minHeight={320} />
          : <div className="ur-table-shell"><Table
            rowKey="key"
            loading={loading}
            dataSource={filteredRows}
            columns={columns}
            pagination={false}
            onRow={(row) => ({
              onClick: () => selectExactIdentity((row as ApplicationOperationRow).application.deployment.id, (row as ApplicationOperationRow).operation.operation_id),
              style: { cursor: 'pointer' },
            })}
            empty={<EmptyState
              title={t("还没有模型执行目标")}
              description={canManage ? t("接入第一个模型；创建后仍需分别完成精确 Operation 探测与发布。") : t("当前组织还没有可读取的模型执行目标。")}
              actions={canManage ? <Button type="primary" disabled={!gatewayLicensed} title={!gatewayLicensed ? gatewayRestriction : undefined} onClick={() => setEntryOpen(true)}>{t("接入模型")}</Button> : undefined}
              minHeight={300}
            />}
          /></div>}
    </Surface>

    <div id="model-release-workspace"><ModelReleaseWorkspace
      canManage={canManage}
      selectedReleaseRef={selectedReleaseRef || undefined}
      onSelectRelease={selectRelease}
      onCanonicalStateChanged={refreshCanonicalState}
    /></div>

    <ModelEndpointDetailsSheet
      selected={entryOpen || createOpen || batchOpen ? undefined : exactDetail}
      operation={selectedOperation}
      canManage={canManage}
      canApproveCapabilities={canApproveCapabilities}
      gatewayLicensed={gatewayLicensed}
      gatewayRestriction={gatewayRestriction}
      workingBindingID={workingBindingID}
      onClose={closeDetail}
      onProbe={confirmProbe}
      onInspectRelease={inspectReleaseFromDetail}
      onOpenReleaseWorkspace={() => { closeDetail(); document.getElementById('model-release-workspace')?.scrollIntoView({ behavior: 'smooth' }); }}
    />
    {detailLoading && deploymentID > 0 && operationID && !detail && <Text type="tertiary">{t("正在读取精确 deployment 详情…")}</Text>}
    {canManage && entryOpen && <SideSheet title={t("接入模型")} visible onCancel={() => setEntryOpen(false)} width="min(680px, 100vw)">
      <Typography.Paragraph>{t("一个平台连接可复用 API Key，配置多个模型。先选择连接方式，再选择模型。")}</Typography.Paragraph>
      <Space vertical align="start">
        <Button type="primary" disabled={!projection?.applications.some(a => !a.connection.retired_at)} onClick={() => {
          setEntryOpen(false); setBatchInitial({ connectionID: 0, bindingID: 0 }); setBatchOpen(true);
        }}>{t("向已有连接添加模型")}</Button>
        <Text type="tertiary">{t("复用已保存的地域、业务空间和凭证，多选模型后统一保存。")}</Text>
        <Button onClick={() => { setEntryOpen(false); setCreateOpen(true); }}>{t("新建平台连接")}</Button>
        <Text type="tertiary">{t("选择平台并保存首个模型，随后继续为该连接选择更多模型。手动 ID 和详细配置也在此完成。")}</Text>
      </Space>
    </SideSheet>}
    {canManage && batchOpen && <ModelEndpointBatchSheet
      applications={projection?.applications || []}
      initialConnectionID={batchInitial.connectionID}
      initialBindingID={batchInitial.bindingID}
      onClose={() => setBatchOpen(false)}
      onChanged={load}
      onContinue={application => { const chat = application.operations.find(o => o.operation_id === 'openai.chat_completions.v1'); if (chat) { setBatchOpen(false); selectExactIdentity(application.deployment.id, chat.operation_id); } }}
    />}
    {canManage && <ModelEndpointCreateSheet
      open={createOpen}
      applications={projection?.applications || []}
      onCancel={() => setCreateOpen(false)}
      onCreated={created}
    />}
  </div>;
}
