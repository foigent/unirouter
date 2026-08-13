import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Form, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { accessApi, type ModelReadiness, type ReadinessAxis } from '../api/access';
import { ApiError, api } from '../api/client';
import { EmptyState, MetricTile, PageHeader, Surface } from '../ui';
import { loadAllPages } from '../utils/loadAllPages';

const { Text, Title } = Typography;

type Project = { id: number; name: string; status: string; current_user_membership_role?: string };

const statePresentation: Record<ReadinessAxis['state'], { label: string; color: 'green' | 'red' | 'orange' | 'grey' | 'blue' }> = {
  ready: { label: '就绪', color: 'green' },
  blocked: { label: '受阻', color: 'red' },
  warning: { label: '提醒', color: 'orange' },
  not_applicable: { label: '不适用 / 可选', color: 'grey' },
  unavailable: { label: '暂时无法判断', color: 'blue' },
};

const platformLabels: Record<string, string> = {
  registration: '模型录入', lifecycle: '生命周期', catalog: '目录可见', price_authority: '价格权威',
  channel: '通道', provider_credential: 'Provider 凭证', base_route: '基础路由', runtime_slot: 'Runtime slot',
};
const projectLabels: Record<string, string> = {
  project: '项目', alias: 'Alias', model_set: '模型集合', wallet: '钱包', project_budget: '项目预算监控',
  application: 'Workload Application', grant: 'Access Grant', credential: '逻辑凭据',
  model_authorization: '模型调用授权', limit_policy: '限额策略',
};

function factText(fact?: { value?: boolean; state: string }) {
  if (!fact || fact.state === 'unavailable' || fact.value === undefined) return '暂时无法判断';
  return fact.value ? '是' : '否';
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
  const [search, setSearch] = useSearchParams();
  const [formApi, setFormApi] = useState<any>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectsError, setProjectsError] = useState('');
  const [readiness, setReadiness] = useState<ModelReadiness | null>(null);
  const [readinessScopeKey, setReadinessScopeKey] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const readinessGeneration = useRef(0);

  const query = useMemo(() => ({
    project_id: Number(search.get('project_id') || 0),
    model: search.get('model') || '',
    endpoint: search.get('endpoint') || '/v1/chat/completions',
    caller: (search.get('caller') || 'self') as 'self' | 'workload_application',
    application_ref: search.get('application_ref') || undefined,
  }), [search]);
  const queryScopeKey = useMemo(() => JSON.stringify(query), [query]);

  const responseMatchesQuery = useCallback((result: ModelReadiness) => result.scope.project_id === query.project_id &&
    result.scope.requested_model === query.model &&
    result.scope.endpoint === query.endpoint &&
    result.scope.caller === query.caller &&
    (result.scope.application_ref || undefined) === query.application_ref, [query]);

  const loadProjects = useCallback(async () => {
    setProjectsError('');
    try {
      setProjects(await loadAllPages<Project>((page, size) => api.getProjects(page, size, { status: 'active' })));
    } catch {
      setProjects([]);
      setProjectsError('项目范围暂时不可用；不会把它解释成“无项目”。');
    }
  }, []);

  const loadReadiness = useCallback(async () => {
    const generation = ++readinessGeneration.current;
    const scopeKey = queryScopeKey;
    setReadiness(null);
    setReadinessScopeKey('');
    setError('');
    if (!query.project_id || !query.model || !query.endpoint || (query.caller === 'workload_application' && !query.application_ref)) {
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      const result = await accessApi.modelReadiness(query);
      if (readinessGeneration.current !== generation || queryScopeKey !== scopeKey || !responseMatchesQuery(result)) return;
      setReadiness(result);
      setReadinessScopeKey(scopeKey);
    } catch (readinessError) {
      if (readinessGeneration.current !== generation || queryScopeKey !== scopeKey) return;
      setError(readinessError instanceof ApiError && readinessError.status === 503
        ? 'Readiness 证据源暂时不可用，当前状态是“暂时无法判断”，不是“未配置”或“受阻”。'
        : '无法读取此项目与调用主体的 readiness。');
    } finally {
      if (readinessGeneration.current === generation && queryScopeKey === scopeKey) setLoading(false);
    }
  }, [query, queryScopeKey, responseMatchesQuery]);

  useEffect(() => { loadProjects(); }, [loadProjects]);
  useEffect(() => { loadReadiness(); }, [loadReadiness]);
  useEffect(() => {
    if (!formApi) return;
    formApi.setValues(query);
  }, [formApi, query]);

  const submit = (values: Record<string, any>) => {
    const next = new URLSearchParams();
    next.set('project_id', String(values.project_id));
    next.set('model', String(values.model).trim());
    next.set('endpoint', values.endpoint);
    next.set('caller', values.caller);
    if (values.caller === 'workload_application') next.set('application_ref', String(values.application_ref || '').trim());
    setSearch(next);
  };

  const scopedReadiness = readinessScopeKey === queryScopeKey ? readiness : null;
  const platformAxes = scopedReadiness ? Object.entries(scopedReadiness.platform_model) : [];
  const projectAxes = scopedReadiness ? Object.entries(scopedReadiness.project_caller) : [];

  return (
    <div className="ur-page">
	  <PageHeader eyebrow="高级诊断" title="模型调用条件诊断（Readiness）" description="Readiness 就是“调用条件是否齐全”的只读排查页，不是模型接入步骤。它不会发 Provider、Feed 或上游请求，也不会自动修改配置。" actions={<Button icon={<IconRefresh />} loading={loading} onClick={loadReadiness}>刷新诊断</Button>} />
      <Surface title="检查范围" description="模型保留为 query value；Workload caller 必须精确绑定同一项目的 application_ref。">
        {projectsError && <Text type="warning">{projectsError}</Text>}
        <Form layout="horizontal" getFormApi={setFormApi} onSubmit={submit}>
          <Form.Select field="project_id" label="项目" optionList={projects.map((project) => ({ value: project.id, label: `${project.name} (#${project.id})` }))} rules={[{ required: true, message: '请选择项目' }]} style={{ width: 240 }} />
          <Form.Input field="model" label="模型 / Alias" rules={[{ required: true, message: '请输入模型' }]} style={{ width: 220 }} />
          <Form.Select field="endpoint" label="执行端点" optionList={['/v1/chat/completions', '/v1/embeddings', '/v1/models', '/v1/media/jobs'].map((value) => ({ value, label: value }))} rules={[{ required: true, message: '请选择端点' }]} style={{ width: 230 }} />
          <Form.Select field="caller" label="调用主体" optionList={[{ value: 'self', label: '当前账号（self）' }, { value: 'workload_application', label: 'Workload Application' }]} rules={[{ required: true, message: '请选择主体' }]} style={{ width: 220 }} />
          <Form.Input field="application_ref" label="Application ref（Workload 时必填）" style={{ width: 240 }} />
          <Button htmlType="submit" type="primary" loading={loading}>检查</Button>
        </Form>
      </Surface>

      {error ? <Surface><EmptyState title="暂时无法判断" description={error} actions={<Button onClick={loadReadiness}>重试</Button>} /></Surface> : !scopedReadiness ? <Surface><EmptyState title="选择项目、模型与调用主体后检查" /></Surface> : <>
        <Surface title="四个独立事实" description={`resolved_model=${scopedReadiness.scope.resolved_model} · runtime_mutation=${scopedReadiness.runtime_mutation} · external_requests=${scopedReadiness.external_requests}`}>
          <div className="ur-metric-grid">
            <MetricTile label="已录入" value={factText(scopedReadiness.summary.registered)} />
            <MetricTile label="目录可见" value={factText(scopedReadiness.summary.catalog_visible)} />
            <MetricTile label="可路由" value={factText(scopedReadiness.summary.routable)} />
            <MetricTile label="当前项目调用主体可调用" value={factText(scopedReadiness.summary.current_project_caller_callable)} />
          </div>
        </Surface>
        <Surface title="平台 / 模型层" description="注册、生命周期、目录、价格和纯读 route preview 各自保真。">
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: 12 }}>{platformAxes.map(([key, axis]) => <AxisCard key={key} label={platformLabels[key] || key} axis={axis} onFix={navigate} />)}</div>
        </Surface>
        <Surface title="所选项目 / 调用主体层" description="Grant、credential、runtime boundary、model authorization 与 limit policy 复用当前权威。">
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: 12 }}>{projectAxes.map(([key, axis]) => <AxisCard key={key} label={projectLabels[key] || key} axis={axis} onFix={navigate} />)}</div>
        </Surface>
        <Surface>
          <Space vertical align="start"><Title heading={6}>证据边界</Title><Text type="tertiary">as_of: {scopedReadiness.as_of}</Text><Text type="tertiary">页面只展示 safe_evidence；不包含 provider/key ID、base URL、密文、hash、token 或 secret。</Text></Space>
        </Surface>
      </>}
    </div>
  );
}
