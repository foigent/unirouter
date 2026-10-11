import { translate as t, translateLabel, useI18n } from '../i18n';
import '../i18n/models';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Form, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { accessApi, type ModelReadiness, type ReadinessAxis } from '../api/access';
import { api } from '../api/client';
import {
  canonicalOperationPaths,
  modelDeploymentApi,
  type CanonicalModelApplication,
  type CanonicalOperationID,
} from '../api/modelDeployments';
import { ApiError } from '../api/transport';
import { EmptyState, PageHeader, Surface } from '../ui';
import { loadAllPages } from '../utils/loadAllPages';
import ModelConnectionBackButton from '../components/ModelConnectionBackButton';
import { projectOption } from '../utils/projectDisplay';

const { Text, Title } = Typography;

type Project = { id: number; name: string; slug?: string; status: string; current_user_membership_role?: string };
type CatalogModel = { model_name: string; request_model?: string; display_name?: string; aliases?: string | string[] };
type WorkloadApplication = { application_ref: string; display_name: string; project_id: number; environment?: string; status: string };
type ModelOption = { value: string; label: string };

const canonicalReadinessEndpoints: Array<{ value: string; label: string; operationID: CanonicalOperationID }> = [
  { value: canonicalOperationPaths['openai.chat_completions.v1'], label: '聊天 / 多模态对话', operationID: 'openai.chat_completions.v1' },
  { value: canonicalOperationPaths['openai.embeddings.v1'], label: '文本向量（Embedding）', operationID: 'openai.embeddings.v1' },
  { value: canonicalOperationPaths['openai.responses.v1'], label: 'OpenAI Responses', operationID: 'openai.responses.v1' },
  { value: canonicalOperationPaths['anthropic.messages.v1'], label: 'Anthropic Messages', operationID: 'anthropic.messages.v1' },
];

function canonicalOperationForEndpoint(endpoint: string): CanonicalOperationID | undefined {
  return canonicalReadinessEndpoints.find(option => option.value === endpoint)?.operationID;
}

function canonicalApplicationMatches(
  application: CanonicalModelApplication,
  model: string,
  endpoint: string,
) {
  const operationID = canonicalOperationForEndpoint(endpoint);
  return Boolean(model && operationID && application.public_model.model_name === model &&
    application.operations.some(operation => operation.operation_id === operationID));
}

const statePresentation: Record<ReadinessAxis['state'], { label: string; color: 'green' | 'red' | 'orange' | 'grey' | 'blue' }> = {
  ready: { label: '就绪', color: 'green' },
  blocked: { label: '受阻', color: 'red' },
  warning: { label: '提醒', color: 'orange' },
  not_applicable: { label: '不适用 / 可选', color: 'grey' },
  unavailable: { label: '暂时无法判断', color: 'blue' },
};

const platformLabels: Record<string, string> = {
  gateway_license: '模型网关授权',
  registration: '模型记录', lifecycle: '生命周期', catalog: '目录可见', price_authority: '计价信息',
  deployment: '模型部署', provider_credential: '供应商凭证', operation: '调用操作', base_route: '可用路由', runtime_slot: '当前路由版本',
};
const projectLabels: Record<string, string> = {
  project: '项目', alias: '模型别名', model_set: '项目模型范围', wallet: '钱包', project_budget: '项目预算监控',
  application: '应用', grant: '访问授权', credential: '调用 API Key',
  model_authorization: '高级模型限制', limit_policy: '限额策略',
};

const reasonCopy: Record<string, string> = {
	model_unregistered: '模型尚未加入模型目录',
	model_retired: '模型已经退役，不能继续调用',
	catalog_not_published: '模型尚未对项目目录开放',
	canonical_deployment_missing: '没有匹配这个模型与调用操作的部署',
	canonical_deployment_present: '已找到精确模型部署',
	canonical_model_operation_missing: '所选部署没有声明这个精确调用操作',
	canonical_model_operation_unverified: '调用操作尚缺少当前有效验证',
	canonical_model_operation_verified: '调用操作已有当前有效验证',
	canonical_provider_credential_unavailable: '没有可用于所选部署的供应商凭证',
	canonical_provider_credential_available: '供应商凭证可用于所选部署',
	canonical_runtime_membership_missing: '该部署操作尚未进入当前运行发布',
	canonical_runtime_membership_present: '该部署操作已进入当前运行发布',
	canonical_deployment_operation_unavailable: '所选部署操作当前还不能承载请求',
	canonical_deployment_operation_ready: '所选部署操作当前可以承载请求',
	canonical_route_plan_available: 'canonical Planner 已生成当前可用路由',
	canonical_route_unavailable: 'canonical Planner 当前没有可用路由',
	provider_credential_unavailable: '没有可用于该模型的供应商凭证',
	base_route_unavailable: '当前没有可实际承载请求的连接',
	project_inactive: '所选项目未启用',
	project_model_set_denied: '项目的模型范围不包含该模型',
	access_grant_missing: '当前身份尚未获得项目访问授权',
	gateway_license_authorized: '商业授权有效',
	model_registered: '已有模型目录记录',
	model_callable_lifecycle: '模型未退役',
	catalog_published: '项目目录可以看到该模型',
	deployment_configured: '已有精确模型部署',
	operation_configured: '所选部署已声明这个调用操作',
	provider_credential_available: '供应商凭证可用于该模型',
	base_route_active: '当前至少有一条可用路由',
	runtime_slot_configured: '已有稳定发布记录',
	project_active: '项目处于启用状态',
	direct_model_optional: '本次直接使用模型 ID，不需要 Alias',
	unrestricted_optional: '项目未设置额外模型集限制',
	self_application_not_applicable: '当前选择个人 API 访问，不需要应用对象',
	access_grant_active: '访问授权有效',
	access_credential_missing: '所选调用身份缺少可用的 API Key',
	legacy_authority_unchanged: '没有额外模型授权策略阻止调用',
	limit_policy_evaluated: '限额策略已完成检查',
};

function summaryStateCopy(state: ReadinessAxis['state'], reasonCode?: string) {
	if (reasonCode && reasonCopy[reasonCode]) return translateLabel(reasonCopy[reasonCode]);
  if (reasonCode?.startsWith('license_error.')) return t("当前部署的模型网关商业授权未就绪，请由平台管理员检查许可证");
  return ({
    ready: t("条件已满足"),
    blocked: t("需要处理后才能使用"),
    warning: t("可以使用，但建议检查"),
    not_applicable: t("当前流程不要求"),
    unavailable: t("证据源暂时不可用"),
  } as const)[state];
}

function ReadinessSummaryCard({ label, state, reasonCode }: { label: string; state: ReadinessAxis['state']; reasonCode?: string }) {
  const presentation = statePresentation[state];
  return <div className={`ur-readiness-summary-card ur-readiness-summary-${state}`}>
    <div className="ur-readiness-summary-card-head">
      <Text strong>{label}</Text>
      <Tag color={presentation.color}>{translateLabel(presentation.label)}</Tag>
    </div>
	<Text type={state === 'blocked' ? 'danger' : 'tertiary'} size="small">{summaryStateCopy(state, reasonCode)}</Text>
  </div>;
}

function AxisCard({ label, axis, onFix }: { label: string; axis: ReadinessAxis; onFix: (route: string) => void }) {
  const presentation = statePresentation[axis.state];
  return (
    <section aria-label={t('{label}诊断', { label })} style={{ border: '1px solid var(--semi-color-border)', borderRadius: 10, padding: 14, minHeight: 150 }}>
      <Space style={{ justifyContent: 'space-between', width: '100%' }}>
        <Text strong>{label}</Text><Tag color={presentation.color}>{translateLabel(presentation.label)}</Tag>
      </Space>
	  <div style={{ marginTop: 10 }}><Text type="tertiary">{summaryStateCopy(axis.state, axis.reason_code)}</Text></div>
	  <details style={{ marginTop: 8 }}>
		<summary style={{ cursor: 'pointer' }}>{t("技术证据")}</summary>
		<div style={{ marginTop: 8 }}><Text code>{axis.reason_code}</Text></div>
		{Object.keys(axis.safe_evidence || {}).length > 0 && <pre style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word', fontSize: 12, color: 'var(--semi-color-text-2)' }}>{JSON.stringify(axis.safe_evidence, null, 2)}</pre>}
	  </details>
      {axis.fix_route ? <Button size="small" onClick={() => onFix(axis.fix_route!)}>{t("前往修复")}</Button> : axis.fix_owner ? <Text type="tertiary">{t("修复责任：")}{axis.fix_owner}</Text> : null}
    </section>
  );
}

export default function ModelReadinessPage({ embedded = false, forceConnectionMode = false }: { embedded?: boolean; forceConnectionMode?: boolean } = {}) {
  useI18n();
  const navigate = useNavigate();
  const location = useLocation();
  const [search, setSearch] = useSearchParams();
  const connectionMode = forceConnectionMode || search.get('mode') === 'connection' || search.get('from') === 'connections';
  const [formApi, setFormApi] = useState<any>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectsError, setProjectsError] = useState('');
  const [modelOptions, setModelOptions] = useState<ModelOption[]>([]);
  const [modelOptionsError, setModelOptionsError] = useState('');
  const [modelApplications, setModelApplications] = useState<CanonicalModelApplication[]>([]);
  const [applications, setApplications] = useState<WorkloadApplication[]>([]);
  const [applicationsError, setApplicationsError] = useState('');
  const [draftProjectID, setDraftProjectID] = useState(0);
  const [draftCaller, setDraftCaller] = useState<'self' | 'workload_application'>('self');
  const [draftModel, setDraftModel] = useState('');
  const [draftEndpoint, setDraftEndpoint] = useState<string>(canonicalOperationPaths['openai.chat_completions.v1']);
  const [readiness, setReadiness] = useState<ModelReadiness | null>(null);
  const [readinessScopeKey, setReadinessScopeKey] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const readinessGeneration = useRef(0);
  const applicationGeneration = useRef(0);
  const modelOptionsGeneration = useRef(0);

  const query = useMemo(() => {
    const endpoint = search.get('endpoint') || canonicalOperationPaths['openai.chat_completions.v1'];
    return {
      mode: connectionMode ? 'connection' as const : 'caller' as const,
      project_id: Number(search.get('project_id') || 0),
      deployment_id: connectionMode ? Number(search.get('deployment_id') || 0) : 0,
      model: search.get('model') || '',
      endpoint,
      operation_id: search.get('operation_id') || canonicalOperationForEndpoint(endpoint) || '',
      caller: connectionMode ? undefined : (search.get('caller') || 'self') as 'self' | 'workload_application',
      application_ref: connectionMode ? undefined : search.get('application_ref') || undefined,
    };
  }, [connectionMode, search]);
  const queryScopeKey = useMemo(() => JSON.stringify(query), [query]);

  const responseMatchesQuery = useCallback((result: ModelReadiness) => {
    const resultMode = result.scope.mode || 'caller';
    if (resultMode !== query.mode || result.scope.requested_model !== query.model || result.scope.endpoint !== query.endpoint ||
      result.scope.model_operation_id !== query.operation_id) return false;
    if (query.mode === 'connection') return result.scope.project_id === 0 && result.scope.deployment_id === query.deployment_id && !result.scope.caller;
    return result.scope.project_id === query.project_id && result.scope.caller === query.caller &&
      (result.scope.application_ref || undefined) === query.application_ref;
  }, [query]);

  const loadProjects = useCallback(async () => {
    if (connectionMode) {
      setProjects([]);
      setProjectsError('');
      return;
    }
    setProjectsError('');
    try {
      setProjects(await loadAllPages<Project>((page, size) => api.getProjects(page, size, { status: 'active' })));
    } catch {
      setProjects([]);
      setProjectsError('项目列表暂时不可用；不会把它解释成“没有项目”。');
    }
  }, [connectionMode]);

  const loadModelOptions = useCallback(async () => {
    const generation = ++modelOptionsGeneration.current;
    setModelOptions([]);
    setModelApplications([]);
    setModelOptionsError('');
    const options = new Map<string, string>();
    let nextApplications: CanonicalModelApplication[] = [];
    let nextError = '';
    if (connectionMode) {
      try {
        const result = await modelDeploymentApi.list();
        nextApplications = result.applications || [];
        for (const application of nextApplications) {
          options.set(application.public_model.model_name, application.public_model.display_name || application.public_model.model_name);
        }
      } catch {
        nextError = "模型部署列表暂时不可用，无法安全选择诊断模型。";
      }
    } else if (query.project_id) {
      try {
        const catalog = await loadAllPages<CatalogModel>((page, size) => api.getModelCatalog(page, size, {
          project_id: query.project_id,
          routable_only: true,
        }));
        for (const model of catalog) {
          const requestModel = String(model.request_model || model.model_name || '').trim();
          if (requestModel) options.set(requestModel, model.display_name || requestModel);
        }
      } catch {
        nextError = "当前项目的已启用模型目录暂时不可用，无法安全选择诊断模型。";
      }
    }
    if (modelOptionsGeneration.current !== generation) return;
    if (query.model && !options.has(query.model)) options.set(query.model, query.model);
    setModelApplications(nextApplications);
    setModelOptionsError(nextError);
    setModelOptions([...options.entries()]
      .map(([value, label]) => ({ value, label }))
      .sort((a, b) => a.label.localeCompare(b.label)));
  }, [connectionMode, query.model, query.project_id]);

  const loadApplications = useCallback(async () => {
    const generation = ++applicationGeneration.current;
    setApplications([]);
    setApplicationsError('');
    if (connectionMode || draftCaller !== 'workload_application' || !draftProjectID) return;
    try {
      const result = await api.getWorkloadApplications({ project_id: draftProjectID, status: 'active' }) as { data?: WorkloadApplication[] };
      if (generation !== applicationGeneration.current) return;
      setApplications((result.data || []).filter(item => item.project_id === draftProjectID && item.status === 'active'));
    } catch {
      if (generation !== applicationGeneration.current) return;
      setApplicationsError('无法读取所选项目的应用列表。');
    }
  }, [connectionMode, draftCaller, draftProjectID]);

  const loadReadiness = useCallback(async () => {
    const generation = ++readinessGeneration.current;
    const scopeKey = queryScopeKey;
    setReadiness(null);
    setReadinessScopeKey('');
    setError('');
    const operationMatchesEndpoint = query.operation_id === canonicalOperationForEndpoint(query.endpoint);
    const missingScope = query.mode === 'connection'
      ? !query.deployment_id || !query.model || !query.endpoint || !operationMatchesEndpoint
      : !query.project_id || !query.model || !query.endpoint || !operationMatchesEndpoint ||
        (query.caller === 'workload_application' && !query.application_ref);
    if (missingScope) {
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      const result = await accessApi.modelReadiness(query.mode === 'connection'
        ? { mode: 'connection', deployment_id: query.deployment_id, model: query.model, endpoint: query.endpoint }
        : {
            project_id: query.project_id,
            model: query.model,
            endpoint: query.endpoint,
            caller: query.caller || 'self',
            application_ref: query.application_ref,
          });
      if (readinessGeneration.current !== generation || queryScopeKey !== scopeKey || !responseMatchesQuery(result)) return;
      setReadiness(result);
      setReadinessScopeKey(scopeKey);
    } catch (readinessError) {
      if (readinessGeneration.current !== generation || queryScopeKey !== scopeKey) return;
      setError(readinessError instanceof ApiError && readinessError.status === 503
        ? "诊断证据源暂时不可用，当前状态是“暂时无法判断”，不是“未配置”或“受阻”。"
        : query.mode === 'connection'
          ? "无法读取当前模型连接的诊断结果。"
          : "无法读取所选项目与调用主体的诊断结果。");
    } finally {
      if (readinessGeneration.current === generation && queryScopeKey === scopeKey) setLoading(false);
    }
  }, [query, queryScopeKey, responseMatchesQuery]);

  useEffect(() => { void loadProjects(); }, [loadProjects]);
  useEffect(() => { void loadModelOptions(); }, [loadModelOptions]);
  useEffect(() => { void loadApplications(); }, [loadApplications]);
  useEffect(() => { void loadReadiness(); }, [loadReadiness]);
  useEffect(() => {
    if (location.pathname !== '/model-readiness' || connectionMode || projects.length !== 1 || query.project_id || projectsError) return;
    const next = new URLSearchParams(search);
    next.set('project_id', String(projects[0].id));
    if (!next.get('caller')) next.set('caller', 'self');
    setSearch(next, { replace: true });
  }, [connectionMode, location.pathname, projects, projectsError, query.project_id, search, setSearch]);
  useEffect(() => {
    setDraftProjectID(query.project_id);
    setDraftCaller(query.caller || 'self');
    setDraftModel(query.model);
    setDraftEndpoint(query.endpoint);
    if (formApi) formApi.setValues({
      ...query,
      project_id: query.project_id || undefined,
      deployment_id: query.deployment_id || undefined,
    });
  }, [formApi, query]);

  const submit = (values: Record<string, any>) => {
    const next = new URLSearchParams();
    next.set('model', String(values.model).trim());
    next.set('endpoint', values.endpoint);
    next.set('operation_id', canonicalOperationForEndpoint(values.endpoint) || '');
    if (connectionMode) {
      next.set('mode', 'connection');
      next.set('tab', 'advanced');
      next.set('view', 'diagnostics');
      next.set('deployment_id', String(Number(values.deployment_id || 0)));
    } else {
      const projectID = projects.length === 1 ? projects[0].id : Number(values.project_id || 0);
      next.set('project_id', String(projectID));
      next.set('caller', values.caller);
      if (values.caller === 'workload_application') next.set('application_ref', String(values.application_ref || '').trim());
    }
    if (search.get('from') === 'connections') {
      next.set('from', 'connections');
    }
    setSearch(next);
  };

  const scopedReadiness = readinessScopeKey === queryScopeKey ? readiness : null;
  const platformAxes = scopedReadiness ? Object.entries(scopedReadiness.platform_model)
    .filter(([key]) => !connectionMode || ['gateway_license', 'deployment', 'provider_credential', 'operation', 'base_route'].includes(key)) : [];
  const projectAxes = scopedReadiness && !connectionMode ? Object.entries(scopedReadiness.project_caller) : [];
  const applicationOptions = applications.map(application => ({
    value: application.application_ref,
    label: `${application.display_name}${application.environment ? ` · ${application.environment}` : ''}`,
  }));
  if (query.application_ref && !applicationOptions.some(option => option.value === query.application_ref)) {
    applicationOptions.push({ value: query.application_ref, label: query.application_ref });
  }
  const deploymentOptions = modelApplications
    .filter(application => canonicalApplicationMatches(application, draftModel, draftEndpoint))
    .map(application => ({
      value: application.deployment.id,
      label: `${application.connection.name} · ${application.connection.provider} · ${canonicalOperationForEndpoint(draftEndpoint)}`,
    }));

  return (
    <div className={embedded ? 'ur-embedded-page' : 'ur-page'}>
      {!embedded && <PageHeader
        eyebrow={t("高级诊断")}
        title={connectionMode ? t("模型连接诊断") : t("调用条件诊断")}
        description={connectionMode
          ? t("检查供应商连接、凭证、操作与当前路由；不请求供应商，也不会自动修改配置。")
          : t("回答“这个项目或应用为什么还不能调用该模型”。诊断本身不会请求供应商，也不会自动修改配置。")}
        actions={<Space><ModelConnectionBackButton /><Button icon={<IconRefresh />} loading={loading} onClick={loadReadiness}>{t("刷新诊断")}</Button></Space>}
      />}
      {embedded && <div className="ur-embedded-heading">
        <div><Title heading={5} style={{ margin: 0 }}>{connectionMode ? t("模型连接诊断") : t("调用条件诊断")}</Title><Text type="tertiary">{t("只读取当前证据，不请求供应商，也不会自动修改配置。")}</Text></div>
        <Button icon={<IconRefresh />} loading={loading} onClick={loadReadiness}>{t("刷新诊断")}</Button>
      </div>}
      <Surface
        title={connectionMode ? t("选择要检查的模型连接") : t("选择要诊断的调用")}
        description={connectionMode
          ? t("从当前已录入的模型中选择；这里只检查连接层，不要求先建立项目或应用。")
          : t("调用授权、API Key 和模型范围按项目隔离；只有一个有效项目时系统会自动使用它。")}
      >
        {!connectionMode && projectsError && <div><Text type="warning">{translateLabel(projectsError)}</Text></div>}
        {modelOptionsError && <div><Text type="warning">{translateLabel(modelOptionsError)}</Text></div>}
        {!connectionMode && applicationsError && <div><Text type="warning">{translateLabel(applicationsError)}</Text></div>}
        <Form layout="horizontal" getFormApi={setFormApi} onSubmit={submit}>
          {!connectionMode && projects.length > 1 && <Form.Select
            field="project_id"
            label={<span id="readiness-project-label">{t("项目")}</span>}
            aria-labelledby="readiness-project-label"
            filter
            optionList={projects.map(projectOption)}
            rules={[{ required: true, message: t("请选择项目") }]}
            style={{ width: 220 }}
            onChange={(value) => {
              setDraftProjectID(Number(value || 0));
              formApi?.setValue('application_ref', undefined);
            }}
          />}
          {!connectionMode && projects.length === 1 && <div className="ur-readiness-sole-project" role="status">
            <Text type="tertiary" size="small">{t("项目")}</Text>
            <Text strong>{projects[0].name}</Text>
            <Tag color="blue">{t("唯一有效项目，已自动选择")}</Tag>
          </div>}
          <Form.Select
            field="model"
            label={<span id="readiness-model-label">{t("模型或 Alias")}</span>}
            aria-labelledby="readiness-model-label"
            filter
            optionList={modelOptions}
            placeholder={t("选择或搜索已录入模型")}
            rules={[{ required: true, message: t("请选择模型") }]}
            style={{ width: 250 }}
            onChange={(value) => {
              const model = String(value || '');
              setDraftModel(model);
              const selectedDeploymentID = Number(formApi?.getValue('deployment_id') || 0);
              const selectedStillMatches = modelApplications.some(application =>
                application.deployment.id === selectedDeploymentID && canonicalApplicationMatches(application, model, draftEndpoint));
              if (!selectedStillMatches) formApi?.setValue('deployment_id', undefined);
            }}
          />
          <Form.Select
            field="endpoint"
            label={<span id="readiness-endpoint-label">{t("调用类型")}</span>}
            aria-labelledby="readiness-endpoint-label"
            optionList={canonicalReadinessEndpoints.map(({ value, label }) => ({ value, label: translateLabel(label) }))}
            rules={[{ required: true, message: t("请选择调用类型") }]}
            style={{ width: 230 }}
            onChange={(value) => {
              const endpoint = String(value || '');
              setDraftEndpoint(endpoint);
              const selectedDeploymentID = Number(formApi?.getValue('deployment_id') || 0);
              const selectedStillMatches = modelApplications.some(application =>
                application.deployment.id === selectedDeploymentID && canonicalApplicationMatches(application, draftModel, endpoint));
              if (!selectedStillMatches) formApi?.setValue('deployment_id', undefined);
            }}
          />
          {connectionMode && <Form.Select
            field="deployment_id"
            label={<span id="readiness-deployment-label">{t("模型部署")}</span>}
            aria-labelledby="readiness-deployment-label"
            filter
            optionList={deploymentOptions}
            placeholder={draftModel ? t("选择支持该调用操作的精确部署") : t("请先选择模型")}
            disabled={!draftModel}
            rules={[{ required: true, message: t("请选择模型部署") }]}
            style={{ width: 300 }}
          />}
          {!connectionMode && <Form.Select
            field="caller"
            label={<span id="readiness-caller-label">{t("谁来调用")}</span>}
            aria-labelledby="readiness-caller-label"
            optionList={[
			  { value: 'self', label: t("当前账号的个人 API Key") },
              { value: 'workload_application', label: t("应用 / Agent") },
            ]}
            rules={[{ required: true, message: t("请选择调用主体") }]}
            style={{ width: 190 }}
            onChange={(value) => {
              const caller = value === 'workload_application' ? 'workload_application' : 'self';
              setDraftCaller(caller);
              if (caller === 'self') formApi?.setValue('application_ref', undefined);
            }}
          />}
          {!connectionMode && draftCaller === 'workload_application' && <Form.Select
            field="application_ref"
            label={<span id="readiness-application-label">{t("应用 / Agent")}</span>}
            aria-labelledby="readiness-application-label"
            filter
            optionList={applicationOptions}
            placeholder={draftProjectID ? t("选择所选项目中的应用") : t("请先选择项目")}
            disabled={!draftProjectID}
            rules={[{ required: true, message: t("请选择应用") }]}
            style={{ width: 240 }}
          />}
          <Button
            htmlType="submit"
            type="primary"
            loading={loading}
            disabled={!connectionMode && projects.length === 0}
          >{connectionMode ? t("诊断连接条件") : t("诊断调用条件")}</Button>
        </Form>
      </Surface>

      {error ? <Surface><EmptyState title={t("暂时无法判断")} description={translateLabel(error)} actions={<Button onClick={loadReadiness}>{t("重试")}</Button>} /></Surface> : !scopedReadiness ? <Surface><EmptyState title={connectionMode ? t("选择模型后开始连接诊断") : t("选择项目、模型与调用主体后开始诊断")} /></Surface> : <>
        <Surface title={t("诊断结果")} description={t('当前模型：{model}', { model: scopedReadiness.scope.resolved_model })}>
          <div className="ur-readiness-summary-grid">
            {connectionMode ? <>
			  {scopedReadiness.platform_model.gateway_license.reason_code !== 'commercial_license_not_required' && <ReadinessSummaryCard label={t("模型网关授权")} state={scopedReadiness.platform_model.gateway_license.state} reasonCode={scopedReadiness.platform_model.gateway_license.reason_code} />}
			  <ReadinessSummaryCard label={t("模型部署")} state={scopedReadiness.platform_model.deployment.state} reasonCode={scopedReadiness.platform_model.deployment.reason_code} />
			  <ReadinessSummaryCard label={t("供应商凭证")} state={scopedReadiness.platform_model.provider_credential.state} reasonCode={scopedReadiness.platform_model.provider_credential.reason_code} />
			  <ReadinessSummaryCard label={t("调用操作")} state={scopedReadiness.platform_model.operation.state} reasonCode={scopedReadiness.platform_model.operation.reason_code} />
			  <ReadinessSummaryCard label={t("当前可用路由")} state={scopedReadiness.platform_model.base_route.state} reasonCode={scopedReadiness.platform_model.base_route.reason_code} />
            </> : <>
			  <ReadinessSummaryCard label={t("模型记录")} state={scopedReadiness.summary.registered.state} reasonCode={scopedReadiness.summary.registered.reason_code} />
			  <ReadinessSummaryCard label={t("项目目录可见")} state={scopedReadiness.summary.catalog_visible.state} reasonCode={scopedReadiness.summary.catalog_visible.reason_code} />
			  <ReadinessSummaryCard label={t("当前可用路由")} state={scopedReadiness.summary.routable.state} reasonCode={scopedReadiness.summary.routable.reason_code} />
			  <ReadinessSummaryCard label={t("所选调用身份可用")} state={scopedReadiness.summary.current_project_caller_callable.state} reasonCode={scopedReadiness.summary.current_project_caller_callable.reason_code} />
            </>}
          </div>
        </Surface>
        <Surface>
          <details>
            <summary style={{ cursor: 'pointer', fontWeight: 600 }}>{t("查看高级诊断证据")}</summary>
            <div style={{ marginTop: 16 }}>
              <Title heading={6}>{t("模型与供应商连接")}</Title>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: 12 }}>{platformAxes.filter(([key, axis]) => key !== 'gateway_license' || axis.reason_code !== 'commercial_license_not_required').map(([key, axis]) => <AxisCard key={key} label={platformLabels[key] ? translateLabel(platformLabels[key]) : key} axis={axis} onFix={navigate} />)}</div>
              {!connectionMode && <>
                <Title heading={6} style={{ marginTop: 20 }}>{t("项目、应用与权限")}</Title>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: 12 }}>{projectAxes.map(([key, axis]) => <AxisCard key={key} label={projectLabels[key] ? translateLabel(projectLabels[key]) : key} axis={axis} onFix={navigate} />)}</div>
              </>}
              <Space vertical align="start" style={{ marginTop: 16 }}>
                <Text type="tertiary">{t("诊断时间：")}{scopedReadiness.as_of}</Text>
                <Text type="tertiary">{t("只展示安全证据；不包含供应商 Key、Base URL、密文、Token 或 Secret。")}</Text>
              </Space>
            </div>
          </details>
        </Surface>
      </>}
    </div>
  );
}
