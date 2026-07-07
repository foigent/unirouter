import { useCallback, useEffect, useMemo, useState } from 'react';
import { Button, Form, Input, Modal, Select, SideSheet, Space, Table, TextArea, Toast, Tooltip, Typography } from '@douyinfe/semi-ui';
import {
  IconAlertTriangle,
  IconBolt,
  IconCoinMoney,
  IconCopy,
  IconDelete,
  IconFile,
  IconPlus,
  IconRefresh,
  IconSearch,
  IconServer,
} from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { useAuthStore } from '../store/auth';
import { canCreateProjectKey, canDisableProjectKey } from '../navigation';
import { EmptyState, MetricTile, MoneyText, PageHeader, StatusTag, Surface } from '../ui';
import { roleLabel as formatRoleLabel } from '../utils/displayLabels';
import { keyQuotaText } from '../utils/quota';

const { Text } = Typography;

interface Project {
  id: number;
  org_id: number;
  name: string;
  slug: string;
  status: string;
  budget_cents?: number;
  budget_period?: string;
  key_count?: number;
}

interface ApiKey {
  id: number;
  project_id: number;
  name: string;
  key_prefix: string;
  status: string;
  allowed_ips: string;
  allowed_models: string;
  rpm_limit: number;
  rps_limit?: number;
  qps_limit: number;
  quota?: {
    rpm_limit?: number | null;
    rps_limit?: number | null;
    tpm_limit?: number | null;
    budget_limit_cents?: number | null;
  };
  created_at: string;
}

interface CatalogModel {
  id: number;
  model_name: string;
  display_name?: string;
  provider_family?: string;
  lifecycle: string;
  context_length?: number;
  output_limit?: number;
  capability_tags?: string | string[];
  data_residency?: string;
  pricing_model_name?: string;
  migration_recommendation?: string;
  aliases?: string | string[];
}

interface CallLog {
  id: number;
  project_id: number;
  request_id: string;
  model: string;
  requested_model?: string;
  provider: string;
  channel: string;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cost_cents: number;
  status_code: number;
  latency_ms: number;
  ttft_ms?: number;
  upstream_status_code?: number;
  upstream_error?: string;
  usage_source?: string;
  is_stream?: boolean;
  created_at: string;
}

interface UsageAnalyticsRow {
  key: string;
  total_requests: number;
  success_requests: number;
  failed_requests: number;
  total_tokens: number;
  cost_cents: number;
  avg_latency_ms: number;
  estimated_ratio: number;
}

interface UsageAnalyticsSummary {
  total_requests: number;
  success_requests: number;
  failed_requests: number;
  total_tokens: number;
  cost_cents: number;
  avg_latency_ms: number;
  estimated_ratio: number;
}

function splitCSV(value?: string | string[] | null) {
  if (Array.isArray(value)) {
    return value
      .map(item => String(item).trim())
      .filter(Boolean);
  }
  return String(value || '')
    .split(',')
    .map(item => item.trim())
    .filter(Boolean);
}

function compactNumber(value: number) {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`;
  return String(value);
}

function fallbackCopyText(text: string) {
  const textarea = document.createElement('textarea');
  textarea.value = text;
  textarea.setAttribute('readonly', 'true');
  textarea.style.position = 'fixed';
  textarea.style.left = '-9999px';
  document.body.appendChild(textarea);
  textarea.select();
  try {
    return document.execCommand('copy');
  } finally {
    document.body.removeChild(textarea);
  }
}

export default function DeveloperConsolePage() {
  const role = useAuthStore((s) => s.user?.role);
  const roleLabel = formatRoleLabel(role, '项目成员');
  const mayCreateKey = canCreateProjectKey(role);
  const mayDisableKey = canDisableProjectKey(role);

  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedProjectId, setSelectedProjectId] = useState<number>();
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [models, setModels] = useState<CatalogModel[]>([]);
  const [logs, setLogs] = useState<CallLog[]>([]);
  const [logTotal, setLogTotal] = useState(0);
  const [usageSummary, setUsageSummary] = useState<UsageAnalyticsSummary | null>(null);
  const [usageByModel, setUsageByModel] = useState<UsageAnalyticsRow[]>([]);
  const [initialLoading, setInitialLoading] = useState(true);
  const [dataLoading, setDataLoading] = useState(false);
  const [showKeySheet, setShowKeySheet] = useState(false);
  const [formApi, setFormApi] = useState<any>(null);
  const [createdKey, setCreatedKey] = useState<{ key: string; key_prefix: string; name: string } | null>(null);
  const [lookupRequestId, setLookupRequestId] = useState('');
  const [lookupLoading, setLookupLoading] = useState(false);
  const [lookupResult, setLookupResult] = useState<CallLog | null>(null);
  const [playgroundKey, setPlaygroundKey] = useState('');
  const [playgroundModel, setPlaygroundModel] = useState('');
  const [playgroundPrompt, setPlaygroundPrompt] = useState('请用一句话返回当前项目的健康检查结果。');
  const [playgroundMode, setPlaygroundMode] = useState('non_stream');
  const [playgroundLoading, setPlaygroundLoading] = useState(false);
  const [playgroundResult, setPlaygroundResult] = useState<{
    status: number;
    content: string;
    request_id?: string;
    cost_cents?: number;
    total_tokens?: number;
    latency_ms?: number;
    usage_source?: string;
    doc_url?: string;
    error?: string;
  } | null>(null);

  const selectedProject = projects.find(project => project.id === selectedProjectId);

  const modelOptions = useMemo(() => {
    const aliases = new Set<string>();
    const modelNames = new Set<string>();
    models.forEach(model => {
      splitCSV(model.aliases).forEach(alias => aliases.add(alias));
      if (model.model_name) modelNames.add(model.model_name);
    });
    const values = [...Array.from(aliases).sort(), ...Array.from(modelNames).sort()];
    return values.filter((value, index) => values.indexOf(value) === index);
  }, [models]);

  const sampleModel = modelOptions[0] || 'company-chat';
  const effectivePlaygroundModel = playgroundModel || sampleModel;
  const sampleKey = createdKey?.key || '<YOUR_PROJECT_API_KEY>';
  const baseUrl = typeof window !== 'undefined' ? window.location.origin : 'https://your-unirouter.example';
  const quickstartCurl = `curl ${baseUrl}/v1/chat/completions \\
  -H "Authorization: Bearer ${sampleKey}" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"${sampleModel}","messages":[{"role":"user","content":"Hello"}]}'`;

  const usageSnapshot = useMemo(() => {
    if (usageSummary) {
      return {
        requestCount: usageSummary.total_requests || 0,
        totalTokens: usageSummary.total_tokens || 0,
        totalCost: usageSummary.cost_cents || 0,
        errorRate: usageSummary.total_requests ? (usageSummary.failed_requests || 0) / usageSummary.total_requests * 100 : 0,
        avgLatency: usageSummary.avg_latency_ms || 0,
        sourceLabel: '我的 Key 聚合',
      };
    }
    const requestCount = logTotal;
    const totalTokens = logs.reduce((sum, log) => sum + (log.total_tokens || 0), 0);
    const totalCost = logs.reduce((sum, log) => sum + (log.cost_cents || 0), 0);
    const failed = logs.filter(log => log.status_code >= 400 || log.status_code === 0).length;
    const avgLatency = logs.length
      ? logs.reduce((sum, log) => sum + (log.latency_ms || 0), 0) / logs.length
      : 0;
    return {
      requestCount,
      totalTokens,
      totalCost,
      errorRate: logs.length ? failed / logs.length * 100 : 0,
      avgLatency,
      sourceLabel: '最近 20 条合计',
    };
  }, [logTotal, logs, usageSummary]);

  const loadProjects = useCallback(async () => {
    setInitialLoading(true);
    try {
      const data = await api.getProjects(1, 100, { status: 'active' });
      const items = (data?.data || []) as Project[];
      setProjects(items);
      setSelectedProjectId(current => {
        if (current && items.some(project => project.id === current)) return current;
        return items[0]?.id;
      });
    } catch (err: any) {
      Toast.error(`项目加载失败${err?.message ? `：${err.message}` : ''}`);
    } finally {
      setInitialLoading(false);
    }
  }, []);

  const loadProjectData = useCallback(async () => {
    if (!selectedProjectId) return;
    setDataLoading(true);
    try {
      const [keyData, modelData, logData, usageData] = await Promise.all([
        api.getKeys(1, 50, { project_id: selectedProjectId }),
        api.getModelCatalog(1, 100, { project_id: selectedProjectId }),
        api.getLogs(1, 20, { project_id: selectedProjectId }),
        api.getUsageAnalytics({ project_id: selectedProjectId, scope: 'my', group_by: 'alias' }).catch((err: any) => {
          Toast.warning(`我的用量加载失败${err?.message ? `：${err.message}` : ''}`);
          return null;
        }),
      ]);
      setKeys((keyData?.data || []) as ApiKey[]);
      setModels((modelData?.data || []) as CatalogModel[]);
      setLogs((logData?.data || []) as CallLog[]);
      setLogTotal(Number(logData?.total || 0));
      setUsageSummary((usageData?.summary || null) as UsageAnalyticsSummary | null);
      setUsageByModel((usageData?.data || []) as UsageAnalyticsRow[]);
    } catch (err: any) {
      Toast.error(`项目数据加载失败${err?.message ? `：${err.message}` : ''}`);
    } finally {
      setDataLoading(false);
    }
  }, [selectedProjectId]);

  useEffect(() => { loadProjects(); }, [loadProjects]);
  useEffect(() => { loadProjectData(); }, [loadProjectData]);
  useEffect(() => {
    if (!playgroundModel && modelOptions[0]) setPlaygroundModel(modelOptions[0]);
  }, [modelOptions, playgroundModel]);

  const openCreateKey = () => {
    setShowKeySheet(true);
    setTimeout(() => formApi?.reset(), 0);
  };

  const closeCreateKey = () => {
    setShowKeySheet(false);
    formApi?.reset();
  };

  const handleCreateKey = async (values: Record<string, any>) => {
    if (!selectedProject) return;
    const allowedModels = Array.isArray(values.allowed_models)
      ? values.allowed_models.join(',')
      : values.allowed_models || '';
    try {
      const result = await api.createKey({
        org_id: selectedProject.org_id,
        project_id: selectedProject.id,
        name: values.name,
        allowed_ips: values.allowed_ips || '',
        allowed_models: allowedModels,
        rpm_limit: Number(values.rpm_limit || 60),
        rps_limit: Number(values.rps_limit || 10),
      });
      setCreatedKey({
        key: result.key,
        key_prefix: result.key_prefix,
        name: values.name,
      });
      setPlaygroundKey(result.key);
      Toast.success('项目 Key 已创建');
      closeCreateKey();
      loadProjectData();
    } catch (err: any) {
      Toast.error(`创建失败${err?.message ? `：${err.message}` : ''}`);
    }
  };

  const handleDisableKey = (record: ApiKey) => {
    Modal.confirm({
      title: '确认停用项目 Key',
      content: `停用 ${record.name} 后，使用该 Key 的项目调用会被拒绝。`,
      onOk: async () => {
        try {
          await api.disableKey(record.id);
          Toast.success('项目 Key 已停用');
          loadProjectData();
        } catch (err: any) {
          Toast.error(`停用失败${err?.message ? `：${err.message}` : ''}`);
        }
      },
    });
  };

  const copyCreatedKey = async () => {
    if (!createdKey?.key) return;
    try {
      if (navigator.clipboard?.writeText && window.isSecureContext) {
        await navigator.clipboard.writeText(createdKey.key);
      } else if (!fallbackCopyText(createdKey.key)) {
        throw new Error('fallback copy failed');
      }
      Toast.success('已复制完整 Key');
    } catch {
      Toast.error('复制失败，请手动复制');
    }
  };

  const handleLookup = async () => {
    const requestID = lookupRequestId.trim();
    if (!selectedProjectId || !requestID) return;
    setLookupLoading(true);
    setLookupResult(null);
    try {
      const list = await api.getLogs(1, 1, { project_id: selectedProjectId, request_id: requestID });
      const first = list?.data?.[0] as CallLog | undefined;
      if (!first) {
        Toast.warning('未找到该 Request ID');
        return;
      }
      const detail = await api.getLogDetail(first.id);
      setLookupResult(detail as CallLog);
    } catch (err: any) {
      Toast.error(`查询失败${err?.message ? `：${err.message}` : ''}`);
    } finally {
      setLookupLoading(false);
    }
  };

  const handlePlaygroundRun = async () => {
    const key = playgroundKey.trim();
    const prompt = playgroundPrompt.trim();
    if (!key || !prompt || !effectivePlaygroundModel) {
      Toast.warning('请填写项目访问凭证、模型和提示词');
      return;
    }
    setPlaygroundLoading(true);
    setPlaygroundResult(null);
    const started = performance.now();
    try {
      const stream = playgroundMode === 'stream';
      const response = await fetch('/v1/chat/completions', {
        method: 'POST',
        headers: {
          Authorization: `Bearer ${key}`,
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          model: effectivePlaygroundModel,
          stream,
          messages: [{ role: 'user', content: prompt }],
        }),
      });
      const latency = Math.max(0, Math.round(performance.now() - started));

      if (stream && response.ok && response.body) {
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';
        let content = '';
        let finalMeta: any = null;
        while (true) {
          const { value, done } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split('\n');
          buffer = lines.pop() || '';
          for (const line of lines) {
            const trimmed = line.trim();
            if (!trimmed.startsWith('data:')) continue;
            const data = trimmed.slice(5).trim();
            if (!data || data === '[DONE]') continue;
            try {
              const chunk = JSON.parse(data);
              content += chunk?.choices?.[0]?.delta?.content || '';
              if (chunk?._uniroute) finalMeta = chunk._uniroute;
              if (chunk?.usage) finalMeta = { ...(finalMeta || {}), usage: chunk.usage };
            } catch {
              // Ignore malformed SSE lines from intermediaries; the gateway error path is handled below.
            }
          }
        }
        setPlaygroundResult({
          status: response.status,
          content: content || '(empty stream)',
          request_id: finalMeta?.request_id,
          cost_cents: finalMeta?.cost?.charged,
          total_tokens: finalMeta?.usage?.total_tokens,
          latency_ms: latency,
          usage_source: finalMeta?.usage_source,
        });
        return;
      }

      const payload = await response.json().catch(() => null);
      if (!response.ok) {
        const error = payload?.error;
        setPlaygroundResult({
          status: response.status,
          content: '',
          latency_ms: latency,
          error: error?.message || `HTTP ${response.status}`,
          doc_url: error?.doc_url,
        });
        return;
      }
      const uniroute = payload?._uniroute || {};
      setPlaygroundResult({
        status: response.status,
        content: payload?.choices?.[0]?.message?.content || JSON.stringify(payload, null, 2),
        request_id: uniroute?.request_id || payload?.id,
        cost_cents: uniroute?.cost?.charged,
        total_tokens: payload?.usage?.total_tokens,
        latency_ms: latency,
        usage_source: uniroute?.usage_source || payload?.usage_source,
      });
    } catch (err: any) {
      setPlaygroundResult({
        status: 0,
        content: '',
        latency_ms: 0,
        error: err?.message || '调试请求失败',
      });
    } finally {
      setPlaygroundLoading(false);
    }
  };

  const keyColumns = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: 'Key 前缀', dataIndex: 'key_prefix', key: 'key_prefix', render: (value: string) => <Text code>{value}***</Text> },
    { title: '允许模型', dataIndex: 'allowed_models', key: 'allowed_models', render: (value: string) => value || '继承项目模型集' },
    { title: '限额', key: 'limits', render: (_: unknown, record: ApiKey) => keyQuotaText(record) },
    { title: '状态', dataIndex: 'status', key: 'status', render: (value: string) => <StatusTag status={value}>{value}</StatusTag> },
    {
      title: '操作',
      key: 'actions',
      width: 96,
      render: (_: unknown, record: ApiKey) => (
        mayDisableKey && record.status !== 'disabled'
          ? (
            <Tooltip content="停用项目 Key">
              <Button icon={<IconDelete />} size="small" type="danger" onClick={() => handleDisableKey(record)} aria-label="停用项目 Key" />
            </Tooltip>
          )
          : <Text type="tertiary">-</Text>
      ),
    },
  ];

  const modelColumns = [
    {
      title: '模型 / 别名',
      dataIndex: 'model_name',
      key: 'model_name',
      render: (value: string, record: CatalogModel) => (
        <div className="ur-dev-model-name">
          <Text strong>{record.display_name || value}</Text>
          <Text type="tertiary" size="small">{value}</Text>
          {splitCSV(record.aliases).length > 0 && (
            <div className="ur-chip-row">
              {splitCSV(record.aliases).slice(0, 3).map(alias => <StatusTag key={alias} status="alias">{alias}</StatusTag>)}
            </div>
          )}
          {record.lifecycle === 'deprecated' && record.migration_recommendation && (
            <div className="ur-dev-migration">
              <IconAlertTriangle />
              <Text type="warning" size="small">{record.migration_recommendation}</Text>
            </div>
          )}
        </div>
      ),
    },
    { title: '供应商族', dataIndex: 'provider_family', key: 'provider_family', render: (value: string) => value || '-' },
    { title: '生命周期', dataIndex: 'lifecycle', key: 'lifecycle', render: (value: string) => <StatusTag status={value}>{value}</StatusTag> },
    { title: '上下文', dataIndex: 'context_length', key: 'context_length', render: (value: number) => value ? compactNumber(value) : '-' },
    { title: '能力', dataIndex: 'capability_tags', key: 'capability_tags', render: (value: string) => splitCSV(value).slice(0, 4).join(', ') || '-' },
    { title: '数据驻留', dataIndex: 'data_residency', key: 'data_residency', render: (value: string) => value || '-' },
  ];

  const logColumns = [
    { title: 'Request ID', dataIndex: 'request_id', key: 'request_id', render: (value: string) => <Text code copyable={{ content: value }}>{value}</Text> },
    { title: '模型', dataIndex: 'model', key: 'model' },
    { title: '状态', dataIndex: 'status_code', key: 'status_code', render: (value: number) => <StatusTag status={value}>{value}</StatusTag> },
    { title: 'Token', dataIndex: 'total_tokens', key: 'total_tokens', render: (value: number) => compactNumber(value || 0) },
    { title: '费用', dataIndex: 'cost_cents', key: 'cost_cents', render: (value: number) => <MoneyText cents={value || 0} precision={4} tone="cost" /> },
    { title: '延迟', dataIndex: 'latency_ms', key: 'latency_ms', render: (value: number) => `${value || 0}ms` },
  ];

  const usageModelColumns = [
    { title: '模型 / 别名', dataIndex: 'key', key: 'key', render: (value: string) => <Text code>{value || '(unknown)'}</Text> },
    { title: '请求数', dataIndex: 'total_requests', key: 'total_requests', render: (value: number) => compactNumber(value || 0) },
    { title: '成功 / 失败', key: 'status_mix', render: (_: unknown, row: UsageAnalyticsRow) => `${row.success_requests || 0} / ${row.failed_requests || 0}` },
    { title: 'Token', dataIndex: 'total_tokens', key: 'total_tokens', render: (value: number) => compactNumber(value || 0) },
    { title: '费用', dataIndex: 'cost_cents', key: 'cost_cents', render: (value: number) => <MoneyText cents={value || 0} precision={4} tone="cost" /> },
    { title: '平均延迟', dataIndex: 'avg_latency_ms', key: 'avg_latency_ms', render: (value: number) => `${Math.round(value || 0)}ms` },
  ];

  if (initialLoading) {
    return (
      <div className="ur-page">
        <Surface>
          <div className="ur-dev-loading"><IconRefresh /> 正在加载项目上下文</div>
        </Surface>
      </div>
    );
  }

  if (projects.length === 0) {
    return (
      <div className="ur-page">
        <PageHeader
          eyebrow="开发者工作台"
          title="项目工作台"
          description="当前账号没有可访问项目。"
        />
        <EmptyState
          title="没有可访问项目"
          description="请联系组织管理员把你的账号加入项目成员后再创建项目 Key 和查看用量。"
          minHeight={320}
        />
      </div>
    );
  }

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="开发者工作台"
        title="项目工作台"
        description="项目成员可在这里创建项目访问凭证、查看已批准模型、定位请求，并复制兼容 OpenAI 协议的调用示例。"
        actions={(
          <Space>
            <Select
              value={selectedProjectId}
              onChange={(value) => setSelectedProjectId(Number(value))}
              optionList={projects.map(project => ({
                label: `${project.name} (${project.slug})`,
                value: project.id,
              }))}
              style={{ width: 280 }}
              aria-label="选择项目"
            />
            <Button icon={<IconRefresh />} onClick={loadProjectData} loading={dataLoading}>刷新</Button>
          </Space>
        )}
      />

      <section className="ur-dev-context">
        <div>
          <Text className="ur-page-eyebrow">当前项目</Text>
          <strong>{selectedProject?.name}</strong>
          <Text type="tertiary">/{selectedProject?.slug}</Text>
        </div>
        <StatusTag status={selectedProject?.status || 'active'}>{selectedProject?.status || 'active'}</StatusTag>
        <StatusTag status={roleLabel}>{roleLabel}</StatusTag>
        <Text type="tertiary">项目 ID：{selectedProject?.id}</Text>
      </section>

      <div className="ur-kpi-grid">
        <MetricTile label="请求数" value={compactNumber(usageSnapshot.requestCount)} icon={<IconFile />} tone="blue" meta={usageSnapshot.sourceLabel} />
        <MetricTile label="Token" value={compactNumber(usageSnapshot.totalTokens)} icon={<IconServer />} tone="violet" meta={usageSnapshot.sourceLabel} />
        <MetricTile label="费用" value={<MoneyText cents={usageSnapshot.totalCost} precision={4} strong tone="cost" />} icon={<IconCoinMoney />} tone="green" meta={usageSnapshot.sourceLabel} />
        <MetricTile label="错误率" value={`${usageSnapshot.errorRate.toFixed(1)}%`} icon={<IconAlertTriangle />} tone={usageSnapshot.errorRate > 5 ? 'red' : 'amber'} meta={`平均延迟 ${usageSnapshot.avgLatency.toFixed(0)}ms`} />
      </div>

      <Surface title="我的模型用量" description="按当前账号名下 API Key 聚合当前项目的模型用量和费用；历史未归属 Owner 的调用不会被计入。">
        <div className="ur-table-shell">
          <Table
            columns={usageModelColumns}
            dataSource={usageByModel}
            rowKey="key"
            loading={dataLoading}
            pagination={false}
            empty={<EmptyState title="暂无我的用量" description="使用当前账号名下的项目 Key 调用后，会在这里按模型显示请求、Token 和费用。" minHeight={180} />}
            size="small"
          />
        </div>
      </Surface>

      <div className="ur-dev-main-grid">
        <Surface
          title="项目访问凭证"
          description="创建后只显示一次完整凭证；列表仅显示前缀。"
          actions={mayCreateKey && <Button icon={<IconPlus />} type="primary" onClick={openCreateKey}>创建项目凭证</Button>}
        >
          <div className="ur-table-shell">
            <Table
              columns={keyColumns}
              dataSource={keys}
              rowKey="id"
              loading={dataLoading}
              pagination={false}
              empty={<EmptyState title="暂无项目凭证" description="创建项目凭证后即可从内部应用调用网关。" minHeight={180} />}
              size="small"
            />
          </div>
        </Surface>

        <Surface title="项目接入示例" description="示例使用当前项目的已批准模型或别名。">
          <div className="ur-dev-docs">
            <div>
              <Text className="ur-filter-label">Base URL</Text>
              <Text code>{baseUrl}/v1</Text>
            </div>
            <div>
              <Text className="ur-filter-label">推荐模型</Text>
              <Text code>{sampleModel}</Text>
            </div>
            <pre className="ur-dev-code">{quickstartCurl}</pre>
          </div>
        </Surface>
      </div>

      <Surface
        title="请求调试台"
        description="使用项目访问凭证直接调用兼容 OpenAI 协议的网关；提示词和响应仅保留在当前页面，不写入历史。"
      >
        <div className="ur-playground-grid">
          <div className="ur-playground-controls">
            <div className="ur-inline-controls">
              <Input
                value={playgroundKey}
                onChange={setPlaygroundKey}
                type="password"
                placeholder="粘贴项目访问凭证"
                style={{ width: 260 }}
              />
              <Select
                value={effectivePlaygroundModel}
                optionList={modelOptions.map(model => ({ label: model, value: model }))}
                onChange={(value) => setPlaygroundModel(String(value || ''))}
                style={{ width: 220 }}
                placeholder="选择已批准模型或别名"
              />
              <Select
                value={playgroundMode}
                optionList={[
                  { label: '非流式', value: 'non_stream' },
                  { label: '流式', value: 'stream' },
                ]}
                onChange={(value) => setPlaygroundMode(String(value || 'non_stream'))}
                style={{ width: 120 }}
              />
              <Button type="primary" icon={<IconBolt />} loading={playgroundLoading} onClick={handlePlaygroundRun}>运行</Button>
            </div>
            <TextArea
              value={playgroundPrompt}
              onChange={setPlaygroundPrompt}
              rows={5}
              placeholder="输入本次测试提示词"
              style={{ width: '100%', marginTop: 12 }}
            />
          </div>
          <div className="ur-playground-result">
            {playgroundResult ? (
              <>
                <div className="ur-chip-row">
                  <StatusTag status={playgroundResult.status}>{playgroundResult.status || 'network_error'}</StatusTag>
                  {playgroundResult.request_id && <Text code>{playgroundResult.request_id}</Text>}
                  {playgroundResult.total_tokens !== undefined && <StatusTag status="tokens">{playgroundResult.total_tokens} tokens</StatusTag>}
                  {playgroundResult.cost_cents !== undefined && <MoneyText cents={playgroundResult.cost_cents} precision={4} tone="cost" />}
                  {playgroundResult.latency_ms !== undefined && <Text type="tertiary">{playgroundResult.latency_ms}ms</Text>}
                </div>
                {playgroundResult.error ? (
                  <div className="ur-playground-error">
                    <Text type="danger">{playgroundResult.error}</Text>
                    {playgroundResult.doc_url && <a href={playgroundResult.doc_url}>查看错误文档</a>}
                  </div>
                ) : (
                  <pre className="ur-dev-code">{playgroundResult.content}</pre>
                )}
              </>
            ) : (
              <EmptyState title="等待运行" description="选择 approved model 或 alias，粘贴项目 Key 后运行一次测试。" minHeight={180} />
            )}
          </div>
        </div>
      </Surface>

      <Surface title="Approved Models" description="只展示当前项目可用且进入 approved catalog 的 GA / Deprecated 模型。">
        <div className="ur-table-shell">
          <Table
            columns={modelColumns}
            dataSource={models}
            rowKey="id"
            loading={dataLoading}
            pagination={false}
            empty={<EmptyState title="暂无已批准模型" description="请联系组织管理员把模型加入项目模型集或批准目录。" minHeight={180} />}
            size="small"
          />
        </div>
      </Surface>

      <div className="ur-dev-main-grid">
        <Surface title="最近调用" description="项目最近请求，用于快速确认状态、token、费用和延迟。">
          <div className="ur-table-shell">
            <Table
              columns={logColumns}
              dataSource={logs}
              rowKey="id"
              loading={dataLoading}
              pagination={false}
              empty={<EmptyState title="暂无调用记录" description="项目完成调用后会显示最近请求。" minHeight={180} />}
              size="small"
            />
          </div>
        </Surface>

        <Surface title="请求定位" description="按 Request ID 定位项目内请求，不展示上游凭证、完整访问凭证、提示词或响应。">
          <div className="ur-dev-lookup">
            <Space align="start">
              <Input
                value={lookupRequestId}
                onChange={setLookupRequestId}
                placeholder="req-..."
                showClear
                style={{ width: 300 }}
                onEnterPress={handleLookup}
              />
              <Button icon={<IconSearch />} type="primary" loading={lookupLoading} onClick={handleLookup}>查询</Button>
            </Space>
            {lookupResult ? (
              <div className="ur-dev-lookup-grid">
                <div><Text type="tertiary">状态</Text><StatusTag status={lookupResult.status_code}>{lookupResult.status_code}</StatusTag></div>
                <div><Text type="tertiary">模型</Text><strong>{lookupResult.requested_model || lookupResult.model}</strong></div>
                <div><Text type="tertiary">路由模型</Text><strong>{lookupResult.model}</strong></div>
                <div><Text type="tertiary">供应商</Text><strong>{lookupResult.provider || '-'}</strong></div>
                <div><Text type="tertiary">渠道</Text><strong>{lookupResult.channel || '-'}</strong></div>
                <div><Text type="tertiary">Token</Text><strong>{compactNumber(lookupResult.total_tokens || 0)}</strong></div>
                <div><Text type="tertiary">费用</Text><MoneyText cents={lookupResult.cost_cents || 0} precision={4} strong tone="cost" /></div>
                <div><Text type="tertiary">延迟</Text><strong>{lookupResult.latency_ms || 0}ms</strong></div>
                {lookupResult.upstream_error && (
                  <div className="ur-dev-lookup-wide">
                    <Text type="tertiary">上游错误</Text>
                    <Text>{lookupResult.upstream_error}</Text>
                  </div>
                )}
              </div>
            ) : (
              <EmptyState title="等待查询" description="输入项目内 Request ID 后显示排障摘要。" minHeight={180} />
            )}
          </div>
        </Surface>
      </div>

      <SideSheet
        title="创建项目访问凭证"
        visible={showKeySheet}
        onCancel={closeCreateKey}
        width={500}
      >
        <Form getFormApi={(apiRef: any) => setFormApi(apiRef)} onSubmit={handleCreateKey} initValues={{ rpm_limit: 60, rps_limit: 10 }}>
          <Form.Input field="name" label="凭证名称" placeholder="frontend-prod" rules={[{ required: true, message: '请输入凭证名称' }]} />
          <Form.Select
            field="allowed_models"
            label="允许模型 / 别名"
            placeholder="留空表示继承项目模型集"
            multiple
            optionList={modelOptions.map(model => ({ label: model, value: model }))}
            style={{ width: '100%' }}
          />
          <Form.Input field="allowed_ips" label="允许 IP" placeholder="留空表示不限制，多个 IP 用逗号分隔" />
          <Form.InputNumber field="rpm_limit" label="RPM 限速" min={1} style={{ width: '100%' }} />
          <Form.InputNumber field="rps_limit" label="RPS 限速" min={1} style={{ width: '100%' }} />
          <Text type="tertiary" size="small">TPM 暂未配置为 Key 级限额；当前仅展示口径，不启用 Scheduler V2。</Text>
          <div className="ur-form-actions">
            <Button onClick={closeCreateKey}>取消</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>创建</Button>
          </div>
        </Form>
      </SideSheet>

      <Modal
        title="请立即保存完整项目 Key"
        visible={!!createdKey}
        onCancel={() => setCreatedKey(null)}
        footer={(
          <Space>
            <Button icon={<IconCopy />} onClick={copyCreatedKey}>复制 Key</Button>
            <Button type="primary" onClick={() => setCreatedKey(null)}>我已保存</Button>
          </Space>
        )}
      >
        <Text type="warning">
          完整 Key 只在创建后显示一次。关闭后系统只保留哈希，无法再次查看。
        </Text>
        <div className="ur-secret-reveal">
          <Text strong>{createdKey?.name}</Text>
          <code>{createdKey?.key}</code>
          <Text type="tertiary">前缀：{createdKey?.key_prefix}***</Text>
        </div>
      </Modal>
    </div>
  );
}
