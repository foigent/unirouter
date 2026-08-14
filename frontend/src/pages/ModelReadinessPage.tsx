import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Form, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { accessApi, type ModelReadiness, type ReadinessAxis } from '../api/access';
import { ApiError, api } from '../api/client';
import { EmptyState, PageHeader, Surface } from '../ui';
import { loadAllPages } from '../utils/loadAllPages';
import { parseChannelModels } from '../utils/channelReadiness';
import ModelConnectionBackButton from '../components/ModelConnectionBackButton';

const { Text, Title } = Typography;

type Project = { id: number; name: string; status: string; current_user_membership_role?: string };
type Channel = { models?: string };
type ModelAlias = { alias: string; target_model: string; display_name?: string; status: string };
type WorkloadApplication = { application_ref: string; display_name: string; project_id: number; environment?: string; status: string };
type ModelOption = { value: string; label: string };

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
  channel: '供应商连接', provider_credential: '供应商凭证', base_route: '可用路由', runtime_slot: '当前路由版本',
};
const projectLabels: Record<string, string> = {
  project: '项目', alias: '模型别名', model_set: '项目模型范围', wallet: '钱包', project_budget: '项目预算监控',
  application: '应用', grant: '访问授权', credential: '应用 API Key',
  model_authorization: '模型调用授权', limit_policy: '限额策略',
};

function summaryStateCopy(state: ReadinessAxis['state']) {
  return ({
    ready: '条件已满足',
    blocked: '需要处理后才能使用',
    warning: '可以使用，但建议检查',
    not_applicable: '当前流程不要求',
    unavailable: '证据源暂时不可用',
  } as const)[state];
}

function ReadinessSummaryCard({ label, state }: { label: string; state: ReadinessAxis['state'] }) {
  const presentation = statePresentation[state];
  return <div className={`ur-readiness-summary-card ur-readiness-summary-${state}`}>
    <div className="ur-readiness-summary-card-head">
      <Text strong>{label}</Text>
      <Tag color={presentation.color}>{presentation.label}</Tag>
    </div>
    <Text type="tertiary" size="small">{summaryStateCopy(state)}</Text>
  </div>;
}

function AxisCard({ label, axis, onFix }: { label: string; axis: ReadinessAxis; onFix: (route: string) => void }) {
  const presentation = statePresentation[axis.state];
  return (
    <div style={{ border: '1px solid var(--semi-color-border)', borderRadius: 10, padding: 14, minHeight: 150 }}>
      <Space style={{ justifyContent: 'space-between', width: '100%' }}>
        <Text strong>{label}</Text><Tag color={presentation.color}>{presentation.label}</Tag>
      </Space>
      <div style={{ marginTop: 10 }}><Text code>{axis.reason_code}</Text></div>
      {Object.keys(axis.safe_evidence || {}).length > 0 && <pre style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word', fontSize: 12, color: 'var(--semi-color-text-2)' }}>{JSON.stringify(axis.safe_evidence, null, 2)}</pre>}
      {axis.fix_route ? <Button size="small" onClick={() => onFix(axis.fix_route!)}>前往修复</Button> : axis.fix_owner ? <Text type="tertiary">修复责任：{axis.fix_owner}</Text> : null}
    </div>
  );
}

export default function ModelReadinessPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const [search, setSearch] = useSearchParams();
  const connectionMode = search.get('mode') === 'connection' || search.get('from') === 'connections';
  const [formApi, setFormApi] = useState<any>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectsError, setProjectsError] = useState('');
  const [modelOptions, setModelOptions] = useState<ModelOption[]>([]);
  const [modelOptionsError, setModelOptionsError] = useState('');
  const [applications, setApplications] = useState<WorkloadApplication[]>([]);
  const [applicationsError, setApplicationsError] = useState('');
  const [draftProjectID, setDraftProjectID] = useState(0);
  const [draftCaller, setDraftCaller] = useState<'self' | 'workload_application'>('self');
  const [readiness, setReadiness] = useState<ModelReadiness | null>(null);
  const [readinessScopeKey, setReadinessScopeKey] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const readinessGeneration = useRef(0);
  const applicationGeneration = useRef(0);

  const query = useMemo(() => ({
    mode: connectionMode ? 'connection' as const : 'caller' as const,
    project_id: Number(search.get('project_id') || 0),
    model: search.get('model') || '',
    endpoint: search.get('endpoint') || '/v1/chat/completions',
    caller: connectionMode ? undefined : (search.get('caller') || 'self') as 'self' | 'workload_application',
    application_ref: connectionMode ? undefined : search.get('application_ref') || undefined,
  }), [connectionMode, search]);
  const queryScopeKey = useMemo(() => JSON.stringify(query), [query]);

  const responseMatchesQuery = useCallback((result: ModelReadiness) => {
    const resultMode = result.scope.mode || 'caller';
    if (resultMode !== query.mode || result.scope.requested_model !== query.model || result.scope.endpoint !== query.endpoint) return false;
    if (query.mode === 'connection') return result.scope.project_id === 0 && !result.scope.caller;
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
    setModelOptionsError('');
    const [channelResult, aliasResult] = await Promise.allSettled([
      loadAllPages<Channel>((page, size) => api.getChannels(page, size)),
      connectionMode
        ? Promise.resolve([] as ModelAlias[])
        : loadAllPages<ModelAlias>((page, size) => api.getModelAliases(page, size)),
    ]);
    const options = new Map<string, string>();
    if (channelResult.status === 'fulfilled') {
      for (const channel of channelResult.value) {
        for (const model of parseChannelModels(channel.models)) options.set(model, model);
      }
    }
    if (aliasResult.status === 'fulfilled') {
      for (const alias of aliasResult.value) {
        if (alias.status === 'active') options.set(alias.alias, `${alias.display_name || alias.alias}（Alias → ${alias.target_model}）`);
      }
    }
    if (query.model && !options.has(query.model)) options.set(query.model, query.model);
    setModelOptions([...options.entries()]
      .map(([value, label]) => ({ value, label }))
      .sort((a, b) => a.label.localeCompare(b.label)));
    if (connectionMode && channelResult.status === 'rejected') {
      setModelOptionsError('模型连接列表暂时不可用，无法安全选择诊断模型。');
    } else if (channelResult.status === 'rejected' && aliasResult.status === 'rejected') {
      setModelOptionsError('模型连接和 Alias 列表均暂时不可用，无法安全选择诊断模型。');
    } else if (channelResult.status === 'rejected' || aliasResult.status === 'rejected') {
      setModelOptionsError('部分模型来源暂时不可用；当前下拉只显示已成功读取的模型。');
    }
  }, [connectionMode, query.model]);

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
    const missingScope = query.mode === 'connection'
      ? !query.model || !query.endpoint
      : !query.project_id || !query.model || !query.endpoint || (query.caller === 'workload_application' && !query.application_ref);
    if (missingScope) {
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      const result = await accessApi.modelReadiness(query.mode === 'connection'
        ? { mode: 'connection', model: query.model, endpoint: query.endpoint }
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
        ? '诊断证据源暂时不可用，当前状态是“暂时无法判断”，不是“未配置”或“受阻”。'
        : query.mode === 'connection'
          ? '无法读取当前模型连接的诊断结果。'
          : '无法读取所选项目与调用主体的诊断结果。');
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
    if (formApi) formApi.setValues(query);
  }, [formApi, query]);

  const submit = (values: Record<string, any>) => {
    const next = new URLSearchParams();
    next.set('model', String(values.model).trim());
    next.set('endpoint', values.endpoint);
    if (connectionMode) {
      next.set('mode', 'connection');
    } else {
      const projectID = projects.length === 1 ? projects[0].id : Number(values.project_id || 0);
      next.set('project_id', String(projectID));
      next.set('caller', values.caller);
      if (values.caller === 'workload_application') next.set('application_ref', String(values.application_ref || '').trim());
    }
    if (search.get('from') === 'connections') {
      next.set('from', 'connections');
      const channelID = search.get('channel_id');
      if (channelID) next.set('channel_id', channelID);
    }
    setSearch(next);
  };

  const scopedReadiness = readinessScopeKey === queryScopeKey ? readiness : null;
  const platformAxes = scopedReadiness ? Object.entries(scopedReadiness.platform_model)
    .filter(([key]) => !connectionMode || ['gateway_license', 'channel', 'provider_credential', 'base_route'].includes(key)) : [];
  const projectAxes = scopedReadiness && !connectionMode ? Object.entries(scopedReadiness.project_caller) : [];
  const applicationOptions = applications.map(application => ({
    value: application.application_ref,
    label: `${application.display_name}${application.environment ? ` · ${application.environment}` : ''}`,
  }));
  if (query.application_ref && !applicationOptions.some(option => option.value === query.application_ref)) {
    applicationOptions.push({ value: query.application_ref, label: query.application_ref });
  }

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="高级诊断"
        title={connectionMode ? '模型连接诊断' : '调用条件诊断'}
        description={connectionMode
          ? '检查模型网关授权、供应商连接、供应商凭证和当前路由；不需要项目，不会请求供应商，也不会自动修改配置。'
          : '回答“这个项目或应用为什么还不能调用该模型”。诊断本身不会请求供应商，也不会自动修改配置。'}
        actions={<Space><ModelConnectionBackButton /><Button icon={<IconRefresh />} loading={loading} onClick={loadReadiness}>刷新诊断</Button></Space>}
      />
      <Surface
        title={connectionMode ? '选择要检查的模型连接' : '选择要诊断的调用'}
        description={connectionMode
          ? '从当前已录入的模型中选择；这里只检查连接层，不要求先建立项目或应用。'
          : '调用授权、应用 API Key 和模型范围按项目隔离；只有一个有效项目时系统会自动使用它。'}
      >
        {!connectionMode && projectsError && <div><Text type="warning">{projectsError}</Text></div>}
        {modelOptionsError && <div><Text type="warning">{modelOptionsError}</Text></div>}
        {!connectionMode && applicationsError && <div><Text type="warning">{applicationsError}</Text></div>}
        <Form layout="horizontal" getFormApi={setFormApi} onSubmit={submit}>
          {!connectionMode && projects.length > 1 && <Form.Select
            field="project_id"
            label="项目"
            filter
            optionList={projects.map(project => ({ value: project.id, label: project.name }))}
            rules={[{ required: true, message: '请选择项目' }]}
            style={{ width: 220 }}
            onChange={(value) => {
              setDraftProjectID(Number(value || 0));
              formApi?.setValue('application_ref', undefined);
            }}
          />}
          {!connectionMode && projects.length === 1 && <div className="ur-readiness-sole-project" role="status">
            <Text type="tertiary" size="small">项目</Text>
            <Text strong>{projects[0].name}</Text>
            <Tag color="blue">唯一有效项目，已自动选择</Tag>
          </div>}
          <Form.Select
            field="model"
            label="模型或 Alias"
            filter
            optionList={modelOptions}
            placeholder="选择或搜索已录入模型"
            rules={[{ required: true, message: '请选择模型' }]}
            style={{ width: 250 }}
          />
          <Form.Select
            field="endpoint"
            label="调用类型"
            optionList={[
              { value: '/v1/chat/completions', label: '聊天 / 多模态对话' },
              { value: '/v1/embeddings', label: '文本向量（Embedding）' },
              { value: '/v1/models', label: '读取模型列表' },
              { value: '/v1/media/jobs', label: '异步媒体任务' },
            ]}
            rules={[{ required: true, message: '请选择调用类型' }]}
            style={{ width: 230 }}
          />
          {!connectionMode && <Form.Select
            field="caller"
            label="谁来调用"
            optionList={[
              { value: 'self', label: '当前账号' },
              { value: 'workload_application', label: '应用 / Agent' },
            ]}
            rules={[{ required: true, message: '请选择调用主体' }]}
            style={{ width: 190 }}
            onChange={(value) => {
              const caller = value === 'workload_application' ? 'workload_application' : 'self';
              setDraftCaller(caller);
              if (caller === 'self') formApi?.setValue('application_ref', undefined);
            }}
          />}
          {!connectionMode && draftCaller === 'workload_application' && <Form.Select
            field="application_ref"
            label="应用 / Agent"
            filter
            optionList={applicationOptions}
            placeholder={draftProjectID ? '选择所选项目中的应用' : '请先选择项目'}
            disabled={!draftProjectID}
            rules={[{ required: true, message: '请选择应用' }]}
            style={{ width: 240 }}
          />}
          <Button
            htmlType="submit"
            type="primary"
            loading={loading}
            disabled={!connectionMode && projects.length === 0}
          >{connectionMode ? '诊断连接条件' : '诊断调用条件'}</Button>
        </Form>
      </Surface>

      {error ? <Surface><EmptyState title="暂时无法判断" description={error} actions={<Button onClick={loadReadiness}>重试</Button>} /></Surface> : !scopedReadiness ? <Surface><EmptyState title={connectionMode ? '选择模型后开始连接诊断' : '选择项目、模型与调用主体后开始诊断'} /></Surface> : <>
        <Surface title="诊断结果" description={`当前模型：${scopedReadiness.scope.resolved_model}`}>
          <div className="ur-readiness-summary-grid">
            {connectionMode ? <>
              <ReadinessSummaryCard label="模型网关授权" state={scopedReadiness.platform_model.gateway_license.state} />
              <ReadinessSummaryCard label="供应商连接" state={scopedReadiness.platform_model.channel.state} />
              <ReadinessSummaryCard label="供应商凭证" state={scopedReadiness.platform_model.provider_credential.state} />
              <ReadinessSummaryCard label="当前可用路由" state={scopedReadiness.platform_model.base_route.state} />
            </> : <>
              <ReadinessSummaryCard label="模型记录" state={scopedReadiness.summary.registered.state} />
              <ReadinessSummaryCard label="项目目录可见" state={scopedReadiness.summary.catalog_visible.state} />
              <ReadinessSummaryCard label="当前可用路由" state={scopedReadiness.summary.routable.state} />
              <ReadinessSummaryCard label="所选主体可调用" state={scopedReadiness.summary.current_project_caller_callable.state} />
            </>}
          </div>
        </Surface>
        <Surface>
          <details>
            <summary style={{ cursor: 'pointer', fontWeight: 600 }}>查看高级诊断证据</summary>
            <div style={{ marginTop: 16 }}>
              <Title heading={6}>模型与供应商连接</Title>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: 12 }}>{platformAxes.map(([key, axis]) => <AxisCard key={key} label={platformLabels[key] || key} axis={axis} onFix={navigate} />)}</div>
              {!connectionMode && <>
                <Title heading={6} style={{ marginTop: 20 }}>项目、应用与权限</Title>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: 12 }}>{projectAxes.map(([key, axis]) => <AxisCard key={key} label={projectLabels[key] || key} axis={axis} onFix={navigate} />)}</div>
              </>}
              <Space vertical align="start" style={{ marginTop: 16 }}>
                <Text type="tertiary">诊断时间：{scopedReadiness.as_of}</Text>
                <Text type="tertiary">只展示安全证据；不包含供应商 Key、Base URL、密文、Token 或 Secret。</Text>
              </Space>
            </div>
          </details>
        </Surface>
      </>}
    </div>
  );
}
