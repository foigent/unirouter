import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { Typography, Table, Tag, Input, Select, Button, Space, Descriptions, Tooltip, Divider, Toast } from '@douyinfe/semi-ui';
import { IconSearch, IconRefresh, IconCopy } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { formatDateTime } from '../utils/formatDate';
import { isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { useAuthStore } from '../store/auth';
import { LatencyText, microsRatio, MoneyText, PageHeader, StatusTag } from '../ui';
import { usageSourceLabel, usageSourceOptions } from '../utils/displayLabels';
import { loadAllPages } from '../utils/loadAllPages';

const { Title, Text } = Typography;

interface Log {
  id: number;
  request_id: string;
  client_request_id?: string;
  workflow_id?: string;
  agent_id?: string;
  attempt_id?: string;
  model: string;
  provider: string;
  channel: string;
  status_code: number;
  latency_ms: number;
  ttft_ms: number;
  prompt_tokens: number;
  uncached_prompt_tokens?: number;
  cache_read_tokens?: number;
  cache_creation_tokens?: number;
  completion_tokens: number;
  total_tokens: number;
  cost_micros: string;
  is_stream: boolean;
  ip_address: string;
  requested_model: string;
  upstream_status_code: number | null;
  upstream_error: string;
  usage_source: string;
  cache_usage_schema?: string;
  cache_usage_status?: string;
  pricing_version?: string;
  cache_read_ratio?: number;
  cache_creation_ratio?: number;
  effective_input_price_micros_per_million?: string;
  effective_cache_read_price_micros_per_million?: string;
  effective_cache_creation_price_micros_per_million?: string;
  effective_output_price_micros_per_million?: string;
  cache_read_cost_micros?: string;
  cache_creation_cost_micros?: string;
  created_at: string;
}

interface LogFilters {
  model?: string;
  provider?: string;
  status_code?: string;
  usage_source?: string;
  start_time?: string;
  end_time?: string;
  request_id?: string;
  client_request_id?: string;
  trace_id?: string;
  workflow_id?: string;
  agent_id?: string;
  attempt_id?: string;
  upstream_request_id?: string;
  evidence_ref?: string;
  source_event_hash?: string;
  latency_min?: string;
  latency_max?: string;
  search?: string;
}

interface Organization {
  id: number;
  name: string;
}

interface TraceEvent {
  id: number;
  event_type: string;
  stage: string;
  severity: 'info' | 'warning' | 'error';
  model: string;
  requested_model: string;
  provider: string;
  channel: string;
  status_code: number | null;
  latency_ms: number;
  metadata: Record<string, unknown>;
  created_at: string;
  trace_id?: string | null;
  span_id?: string | null;
  parent_span_id?: string | null;
  schema_version?: string | null;
  source_component?: string | null;
  capture_mode?: string | null;
  capture_gap?: string | null;
  evidence_ref?: string | null;
  upstream_request_id?: string | null;
  decision_kind?: string | null;
  decision_result?: string | null;
  decision_reason?: string | null;
  actor_type?: string | null;
  actor_ref?: string | null;
  on_behalf_of?: string | null;
  session_id?: string | null;
  client_request_id?: string | null;
  workflow_id?: string | null;
  agent_id?: string | null;
  attempt_id?: string | null;
  payload_capture_mode?: string | null;
  payload_ref?: string | null;
  source_event_hash?: string | null;
}

type ReceiptTab = 'decision' | 'attempts' | 'policy' | 'cost' | 'provenance';

const receiptTabs: Array<{ key: ReceiptTab; label: string }> = [
  { key: 'decision', label: 'Decision' },
  { key: 'attempts', label: 'Attempts' },
  { key: 'policy', label: 'Policy & Guardrail' },
  { key: 'cost', label: 'Cost & Finality' },
  { key: 'provenance', label: 'Provenance & Gaps' },
];

function filtersToParams(filters: LogFilters): string {
  const params = new URLSearchParams();
  Object.entries(filters).forEach(([k, v]) => { if (v) params.set(k, v); });
  return params.toString();
}

function paramsToFilters(): LogFilters {
  const params = new URLSearchParams(window.location.search);
  const f: LogFilters = {};
  ['model', 'provider', 'status_code', 'usage_source', 'start_time', 'end_time', 'request_id', 'client_request_id', 'trace_id', 'workflow_id', 'agent_id', 'attempt_id', 'upstream_request_id', 'evidence_ref', 'source_event_hash', 'latency_min', 'latency_max', 'search'].forEach(k => {
    const v = params.get(k);
    if (v) (f as any)[k] = v;
  });
  return f;
}

function selectedLogIDFromParams(): number | undefined {
  const value = Number(new URLSearchParams(window.location.search).get('log_id'));
  return Number.isSafeInteger(value) && value > 0 ? value : undefined;
}

const filterLabels: Record<keyof LogFilters, string> = {
  model: '模型',
  provider: '供应商',
  status_code: '状态码',
  usage_source: '计量来源',
  start_time: '开始时间',
  end_time: '结束时间',
  request_id: '操作 ID',
  client_request_id: '客户端请求 ID',
  trace_id: 'Trace ID',
  workflow_id: '工作流 ID',
  agent_id: 'Agent ID',
  attempt_id: '尝试 ID',
  upstream_request_id: '上游 Request ID',
  evidence_ref: 'Evidence Ref',
  source_event_hash: 'Source Event Hash',
  latency_min: '最小延迟',
  latency_max: '最大延迟',
  search: '搜索',
};

function filterValueLabel(key: string, value: string | undefined) {
  if (key === 'usage_source') return usageSourceLabel(value);
  return value || '-';
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

async function copyToClipboard(text: string) {
  try {
    if (navigator.clipboard?.writeText && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
    } else if (!fallbackCopyText(text)) {
      throw new Error('fallback copy failed');
    }
    Toast.success('已复制 Request ID');
  } catch {
    Toast.error('复制失败，请手动复制');
  }
}

function traceSeverityColor(severity: TraceEvent['severity']) {
  if (severity === 'error') return 'red';
  if (severity === 'warning') return 'orange';
  return 'green';
}

function formatMetadataValue(value: unknown) {
  if (typeof value === 'boolean') return value ? 'true' : 'false';
  if (typeof value === 'number') return value.toLocaleString();
  if (typeof value === 'string') return value || '-';
  return '-';
}

function compactEvidenceValue(value?: string | null, max = 18) {
  if (!value) return '';
  return value.length > max ? `${value.slice(0, max)}...` : value;
}

function cacheUsageLabel(status?: string) {
  if (status === 'cache_read_reported') return '已回传缓存读取';
  if (status === 'cache_write_reported') return '已回传缓存写入';
  if (status === 'cache_hit_miss_reported') return '已回传命中/未命中';
  if (status === 'cache_read_write_reported') return '已回传缓存读/写';
  return '供应商未回传';
}

function priceRatio(ratio?: number, cachePrice?: string, inputPrice?: string) {
  if (ratio !== undefined) return `${ratio.toFixed(3)}×`;
  if (!inputPrice || !cachePrice) return '-';
  return `${microsRatio(cachePrice, inputPrice).toFixed(3)}×`;
}

function uniqueEvidenceValues(events: TraceEvent[], key: keyof TraceEvent) {
  return [...new Set(events.map(event => event[key]).filter((value): value is string => typeof value === 'string' && value.length > 0))];
}

function metadataString(event: TraceEvent | undefined, key: string) {
  const value = event?.metadata?.[key];
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return '';
}

function isPolicyEvent(event: TraceEvent) {
  const decisionKind = event.decision_kind || '';
  return ['admission', 'auth', 'quota', 'budget', 'payload_capture', 'policy'].includes(decisionKind) ||
    ['auth', 'quota', 'budget', 'guardrail', 'payload_policy', 'policy'].includes(event.stage) ||
    /(guardrail|policy|quota|budget|auth|payload)/i.test(event.event_type);
}

function traceEvidenceTags(event: TraceEvent) {
  const tags = [
    { key: 'source', label: 'source', value: event.source_component, color: undefined },
    { key: 'capture', label: 'capture', value: event.capture_mode, color: 'blue' },
    { key: 'payload', label: 'payload', value: event.payload_capture_mode, color: 'cyan' },
    { key: 'schema', label: 'schema', value: event.schema_version, color: undefined },
    { key: 'gap', label: 'gap', value: event.capture_gap, color: 'orange' },
    { key: 'trace', label: 'trace', value: event.trace_id, color: undefined },
    { key: 'span', label: 'span', value: event.span_id, color: undefined },
    { key: 'parent', label: 'parent', value: event.parent_span_id, color: undefined },
    { key: 'client', label: 'client', value: event.client_request_id, color: undefined },
    { key: 'workflow', label: 'workflow', value: event.workflow_id, color: undefined },
    { key: 'agent', label: 'agent', value: event.agent_id, color: undefined },
    { key: 'attempt', label: 'attempt', value: event.attempt_id, color: undefined },
    { key: 'upstream', label: 'upstream', value: event.upstream_request_id, color: undefined },
    { key: 'actor', label: 'actor', value: event.actor_ref ? `${event.actor_type || 'unknown'}:${event.actor_ref}` : event.actor_type, color: undefined },
    { key: 'session', label: 'session', value: event.session_id, color: undefined },
    { key: 'evidence', label: 'evidence', value: event.evidence_ref, color: undefined },
    { key: 'hash', label: 'hash', value: event.source_event_hash, color: undefined },
  ].filter(tag => tag.value);

  if (tags.length === 0) return null;
  return (
    <div className="ur-chip-row">
      {tags.map(tag => (
        <Tag key={tag.key} size="small" color={tag.color as any}>
          {tag.label}: {compactEvidenceValue(tag.value)}
        </Tag>
      ))}
    </div>
  );
}

interface RouteAttempt {
  sequence: number;
  attemptID: string;
  provider: string;
  channel: string;
  mappedModel: string;
  outcome: 'selected' | 'accepted' | 'failed';
  statusCode: number | null;
  eventType: string;
}

function routeAttemptsFromTrace(events: TraceEvent[]): RouteAttempt[] {
  const attempts: RouteAttempt[] = [];
  for (const event of events) {
    if (event.event_type === 'routing.route_selected') {
      attempts.push({
        sequence: attempts.length + 1,
        attemptID: event.attempt_id || '',
        provider: event.provider || '-',
        channel: event.channel || '-',
        mappedModel: typeof event.metadata?.mapped_model === 'string' ? event.metadata.mapped_model : event.model || '-',
        outcome: 'selected',
        statusCode: null,
        eventType: event.event_type,
      });
      continue;
    }
    if (attempts.length === 0 || !event.event_type.startsWith('provider.')) continue;
    const attempt = event.attempt_id
      ? [...attempts].reverse().find(item => item.attemptID === event.attempt_id) || attempts[attempts.length - 1]
      : attempts[attempts.length - 1];
    attempt.eventType = event.event_type;
    attempt.statusCode = event.status_code;
    attempt.outcome = event.event_type === 'provider.response_accepted' ? 'accepted' : 'failed';
  }
  return attempts;
}

export default function LogsPage() {
  const user = useAuthStore((state) => state.user);
  const platform = isPlatformAdminRole(organizationRoleForUser(user));
  const [selectedOrgID, setSelectedOrgID] = useState<number | undefined>(platform ? user?.org_id : undefined);
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [logs, setLogs] = useState<Log[]>([]);
  const [loading, setLoading] = useState(true);
  const [logsError, setLogsError] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(50);
  const [total, setTotal] = useState(0);
  const [filters, setFilters] = useState<LogFilters>(paramsToFilters);
  const [selectedLog, setSelectedLog] = useState<Log | null>(null);
  const [receiptTab, setReceiptTab] = useState<ReceiptTab>('decision');
  const [traceEvents, setTraceEvents] = useState<TraceEvent[]>([]);
  const [traceLoading, setTraceLoading] = useState(false);
  const [traceError, setTraceError] = useState<string | null>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>();
  const selectedLogIDRef = useRef<number | undefined>(selectedLogIDFromParams());
  const traceRequestRef = useRef(0);
  const [modelOptions, setModelOptions] = useState<string[]>([]);
  const [customTimeStart, setCustomTimeStart] = useState('');
  const [customTimeEnd, setCustomTimeEnd] = useState('');
  const [selectedPreset, setSelectedPreset] = useState<string>('');
  const targetOrgID = platform ? selectedOrgID : undefined;

  const loadTrace = useCallback((record: Log) => {
    const requestSequence = ++traceRequestRef.current;
    setTraceEvents([]);
    setTraceLoading(true);
    setTraceError(null);
    (targetOrgID ? api.getLogTrace(record.id, targetOrgID) : api.getLogTrace(record.id))
      .then((data: any) => {
        if (requestSequence !== traceRequestRef.current) return;
        setTraceEvents(Array.isArray(data?.events) ? data.events : []);
      })
      .catch((e) => {
        if (requestSequence !== traceRequestRef.current) return;
        console.error('Failed to fetch request trace', e);
        setTraceEvents([]);
        setTraceError('执行证据加载失败；当前状态不能解释为“未捕获”。');
      })
      .finally(() => {
        if (requestSequence === traceRequestRef.current) setTraceLoading(false);
      });
  }, [targetOrgID]);

  const selectLog = useCallback((record: Log) => {
    selectedLogIDRef.current = record.id;
    setSelectedLog(record);
    loadTrace(record);
  }, [loadTrace]);

  const fetchLogs = useCallback(async (p?: number) => {
    setLoading(true);
    setLogsError(null);
    try {
      const currentPage = p ?? page;
      const data = await api.getLogs(currentPage, pageSize, targetOrgID ? { ...filters, org_id: targetOrgID } : filters);
      const nextLogs = Array.isArray(data.data) ? data.data : [];
      setLogs(nextLogs);
      setTotal(data.total || 0);
      const nextSelected = nextLogs.find((log: Log) => log.id === selectedLogIDRef.current) || nextLogs[0] || null;
      if (nextSelected) {
        selectLog(nextSelected);
      } else {
        selectedLogIDRef.current = undefined;
        traceRequestRef.current += 1;
        setSelectedLog(null);
        setTraceEvents([]);
        setTraceLoading(false);
        setTraceError(null);
      }
    } catch (e) {
      console.error('Failed to fetch logs', e);
      setLogs([]);
      setTotal(0);
      selectedLogIDRef.current = undefined;
      traceRequestRef.current += 1;
      setSelectedLog(null);
      setTraceEvents([]);
      setTraceLoading(false);
      setTraceError(null);
      setLogsError('请求日志加载失败；当前筛选结果不可用。');
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, filters, targetOrgID, selectLog]);

  useEffect(() => { fetchLogs(); }, [fetchLogs]);

  useEffect(() => {
    api.getModelNames().then((modelData: any) => {
      const modelSource = Array.isArray(modelData?.data) ? modelData.data : modelData;
      const models = (Array.isArray(modelSource) ? modelSource : [])
        .map((m: any) => typeof m === 'string' ? m : m.model_name || m.model)
        .filter(Boolean);
      setModelOptions(models);
    }).catch(() => {});
  }, []);

  useEffect(() => {
    if (!platform) return;
    if (user?.org_id) setSelectedOrgID(current => current || user.org_id);
    loadAllPages<Organization>((page, size) => api.getOrganizations(page, size))
      .then(setOrganizations)
      .catch((err: any) => Toast.error(`组织列表加载失败${err?.message ? `：${err.message}` : ''}`));
  }, [platform, user?.org_id]);

  useEffect(() => {
    const qs = filtersToParams(filters);
    const params = new URLSearchParams(qs);
    if (selectedLog?.id) params.set('log_id', String(selectedLog.id));
    const query = params.toString();
    const url = query ? `${window.location.pathname}?${query}` : window.location.pathname;
    window.history.replaceState(null, '', url);
  }, [filters, selectedLog?.id]);

  const updateFilter = useCallback((key: keyof LogFilters, value: string) => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      setFilters(prev => ({ ...prev, [key]: value || undefined }));
      setPage(1);
    }, 300);
  }, []);

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault();
        searchRef.current?.focus();
        return;
      }
      const target = e.target;
      if (
        target instanceof Element
        && target.closest('input, textarea, select, button, [role="combobox"], [role="listbox"], [contenteditable="true"]')
      ) return;
      if ((e.key !== 'ArrowDown' && e.key !== 'ArrowUp') || logs.length === 0) return;
      e.preventDefault();
      const currentIndex = logs.findIndex(log => log.id === selectedLog?.id);
      const nextIndex = e.key === 'ArrowDown'
        ? Math.min(currentIndex < 0 ? 0 : currentIndex + 1, logs.length - 1)
        : Math.max(currentIndex < 0 ? 0 : currentIndex - 1, 0);
      selectLog(logs[nextIndex]);
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [logs, selectedLog?.id, selectLog]);

  const clearFilters = () => {
    setFilters({});
    setPage(1);
  };

  const changeOrganization = (value: unknown) => {
    const orgID = Number(value);
    if (!Number.isSafeInteger(orgID) || orgID <= 0) return;
    setSelectedOrgID(orgID);
    setPage(1);
    setLogs([]);
    setTotal(0);
    selectedLogIDRef.current = undefined;
    traceRequestRef.current += 1;
    setSelectedLog(null);
    setTraceEvents([]);
    setTraceLoading(false);
    setTraceError(null);
  };

  const hasActiveFilters = Object.values(filters).some(v => v);
  const timePresets = [
    { label: '最近 15min', value: '15m' },
    { label: '最近 1h', value: '1h' },
    { label: '最近 24h', value: '24h' },
    { label: '最近 7d', value: '7d' },
    { label: '自定义', value: 'custom' },
  ];

  const handleTimePreset = (preset: string) => {
    setSelectedPreset(preset);
    if (preset === 'custom') {
      if (customTimeStart && customTimeEnd) {
        setFilters(prev => ({ ...prev, start_time: new Date(customTimeStart).toISOString(), end_time: new Date(customTimeEnd).toISOString() }));
        setPage(1);
      }
      return;
    }
    const now = new Date();
    const fmt = (d: Date) => d.toISOString();
    let start: Date;
    switch (preset) {
      case '15m': start = new Date(now.getTime() - 15 * 60000); break;
      case '1h': start = new Date(now.getTime() - 3600000); break;
      case '24h': start = new Date(now.getTime() - 86400000); break;
      case '7d': start = new Date(now.getTime() - 7 * 86400000); break;
      default: return;
    }
    setFilters(prev => ({ ...prev, start_time: fmt(start), end_time: fmt(now) }));
    setPage(1);
  };

  const tableColumns = useMemo(() => {
    const columns = [
    {
      title: 'Request ID',
      dataIndex: 'request_id',
      key: 'request_id',
      width: 140,
      render: (v: string) => (
        <div className="ur-request-id-cell">
          <Text code style={{ fontSize: 12, maxWidth: 100 }} ellipsis={{ showTooltip: false }}>{v.slice(0, 12)}…</Text>
          <Tooltip content="复制">
            <Button
              type="tertiary"
              size="small"
              icon={<IconCopy />}
              onClick={(e) => { e.stopPropagation(); copyToClipboard(v); }}
              className="ur-copy-button"
              aria-label="复制 Request ID"
            />
          </Tooltip>
        </div>
      ),
    },
    {
      title: '模型',
      dataIndex: 'model',
      key: 'model',
      width: 150,
      render: (v: string) => <Tag size="small">{v}</Tag>,
    },
    {
      title: '供应商',
      dataIndex: 'provider',
      key: 'provider',
      width: 110,
      render: (v: string) => <Tag size="small" color="blue">{v}</Tag>,
    },
    {
      title: '状态码',
      dataIndex: 'status_code',
      key: 'status_code',
      width: 86,
      render: (v: number) => <StatusTag status={v}>{v}</StatusTag>,
    },
    {
      title: '延迟',
      dataIndex: 'latency_ms',
      key: 'latency_ms',
      width: 90,
      render: (v: number) => <LatencyText value={v} />,
    },
    {
      title: 'Token',
      dataIndex: 'total_tokens',
      key: 'total_tokens',
      width: 86,
      render: (v: number) => <Text size="small">{v.toLocaleString()}</Text>,
    },
    {
      title: 'Prompt Cache',
      dataIndex: 'cache_usage_status',
      key: 'cache_usage_status',
      width: 132,
      render: (_: string, record: Log) => record.cache_usage_schema
        ? <Tag size="small" color={(record.cache_read_tokens || 0) > 0 ? 'green' : 'blue'}>读 {(record.cache_read_tokens || 0).toLocaleString()} / 写 {(record.cache_creation_tokens || 0).toLocaleString()}</Tag>
        : <Tag size="small">未回传</Tag>,
    },
    {
      title: '来源',
      dataIndex: 'usage_source',
      key: 'usage_source',
      width: 122,
      render: (v: string) => v ? <StatusTag status={v}>{usageSourceLabel(v)}</StatusTag> : '-',
    },
    {
      title: '费用',
      dataIndex: 'cost_micros',
      key: 'cost_micros',
      width: 90,
      render: (v: string) => <MoneyText micros={v || '0'} precision={6} tone="cost" />,
    },
    {
      title: '时间',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 160,
      render: (v: string) => <Text size="small">{formatDateTime(v)}</Text>,
    },
    ];
    const compactColumnWidths: Record<string, number> = {
      request_id: 124,
      model: 116,
      provider: 86,
      status_code: 68,
    };
    return columns
      .filter(col => ['request_id', 'model', 'provider', 'status_code'].includes(col.key as string))
      .map(col => ({ ...col, width: compactColumnWidths[col.key as string] }));
  }, []);
  const traceIDs = uniqueEvidenceValues(traceEvents, 'trace_id');
  const captureModes = uniqueEvidenceValues(traceEvents, 'capture_mode');
  const payloadModes = uniqueEvidenceValues(traceEvents, 'payload_capture_mode');
  const sourceComponents = uniqueEvidenceValues(traceEvents, 'source_component');
  const upstreamRequestIDs = uniqueEvidenceValues(traceEvents, 'upstream_request_id');
  const actorTypes = uniqueEvidenceValues(traceEvents, 'actor_type');
  const actorRefs = uniqueEvidenceValues(traceEvents, 'actor_ref');
  const onBehalfOf = uniqueEvidenceValues(traceEvents, 'on_behalf_of');
  const evidenceRefs = uniqueEvidenceValues(traceEvents, 'evidence_ref');
  const sourceEventHashes = uniqueEvidenceValues(traceEvents, 'source_event_hash');
  const schemaVersions = uniqueEvidenceValues(traceEvents, 'schema_version');
  const gapEvents = traceEvents.filter(event => event.capture_gap);
  const decisionEvents = traceEvents.filter(event => event.decision_kind || event.decision_result);
  const policyEvents = traceEvents.filter(isPolicyEvent);
  const routeAttempts = routeAttemptsFromTrace(traceEvents);
  const noRoute = traceEvents.some(event => event.event_type === 'routing.no_route');
  const billingTerminalEvent = [...traceEvents].reverse().find(event =>
    [
      'billing.settle.succeeded',
      'billing.settle.pending_recovery',
      'billing.settlement_usage.failed',
      'billing.cancelled.no_billable_output',
    ].includes(event.event_type)
  );
  const exactBillingStatus = metadataString(billingTerminalEvent, 'billing_status');
  const billingTerminalStatus = traceError
    ? 'unavailable'
    : billingTerminalEvent?.event_type === 'billing.settle.succeeded'
    ? exactBillingStatus || 'settled'
    : billingTerminalEvent?.event_type === 'billing.settle.pending_recovery'
      ? exactBillingStatus || 'pending recovery'
      : billingTerminalEvent?.event_type === 'billing.settlement_usage.failed'
        ? exactBillingStatus || 'settlement usage failed'
        : billingTerminalEvent?.event_type === 'billing.cancelled.no_billable_output'
          ? exactBillingStatus || 'cancellation outcome captured'
        : 'not captured';
  const billingTerminalTone = billingTerminalStatus === 'settled' || billingTerminalStatus === 'cancelled_no_billable_output'
    ? 'success'
    : billingTerminalStatus.includes('failed')
      ? 'failed'
      : 'warning';
  const authoritySummary = traceLoading
    ? 'loading'
    : traceError
    ? 'unavailable'
    : actorRefs.length > 0
    ? `${actorTypes[0] || 'unknown'}:${compactEvidenceValue(actorRefs[0], 20)}`
    : actorTypes[0] || '未捕获';

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="Operations"
        title="执行请求"
        description="以执行决策凭证定位权限、路由、策略、安全、成本终态和证据缺口。"
        actions={(
          <>
            {platform && (
              <Space spacing={6}>
                <Text type="tertiary">目标组织</Text>
                <Select
                  aria-label="目标组织"
                  placeholder="选择目标组织"
                  value={selectedOrgID}
                  optionList={organizations.map(org => ({ label: `${org.name}（ID ${org.id}）`, value: org.id }))}
                  onChange={changeOrganization}
                  disabled={loading}
                  style={{ width: 220 }}
                />
              </Space>
            )}
            <Tag color="blue">{total} 条</Tag>
            <Tooltip content="刷新">
              <Button icon={<IconRefresh />} size="small" onClick={() => fetchLogs()} />
            </Tooltip>
          </>
        )}
      />

      <div className="ur-explorer ur-explorer-with-detail">
        <aside className="ur-explorer-panel ur-explorer-filters">
          <div className="ur-panel-title">
            <Title heading={5} style={{ margin: 0 }}>筛选器</Title>
            {hasActiveFilters && (
              <Button type="tertiary" size="small" onClick={clearFilters}>清除</Button>
            )}
          </div>

          <div className="ur-filter-group">
            <Text className="ur-filter-label">搜索</Text>
            <Input
              ref={searchRef as any}
              prefix={<IconSearch />}
              placeholder="请求、模型、供应商或证据标识"
              onChange={(v) => updateFilter('search', v)}
              defaultValue={filters.search || ''}
              showClear
              size="small"
            />
          </div>

          <div className="ur-filter-group">
            <Text className="ur-filter-label">时间范围</Text>
            <div className="ur-chip-row">
              {timePresets.map(p => (
                <Button
                  key={p.value}
                  size="small"
                  type={selectedPreset === p.value ? 'primary' : 'tertiary'}
                  theme={selectedPreset === p.value ? 'solid' : 'borderless'}
                  onClick={() => handleTimePreset(p.value)}
                >
                  {p.label}
                </Button>
              ))}
            </div>
            {selectedPreset === 'custom' && (
              <div style={{ marginTop: 8, display: 'grid', gap: 6 }}>
                <Input size="small" type="datetime-local" value={customTimeStart} onChange={setCustomTimeStart} />
                <Input size="small" type="datetime-local" value={customTimeEnd} onChange={setCustomTimeEnd} />
                <Button size="small" type="primary" onClick={() => handleTimePreset('custom')}>确定</Button>
              </div>
            )}
          </div>

          <Divider style={{ margin: '12px 0' }} />

          <div className="ur-filter-group">
            <Text className="ur-filter-label">供应商</Text>
            <Input
              size="small"
              placeholder="供应商名称"
              value={filters.provider || undefined}
              onChange={(v) => updateFilter('provider', v)}
              style={{ width: '100%' }}
              showClear
            />
          </div>

          <div className="ur-filter-group">
            <Text className="ur-filter-label">模型</Text>
            <Select
              size="small"
              placeholder="模型名称"
              value={filters.model || undefined}
              onChange={(v) => updateFilter('model', v as string)}
              style={{ width: '100%' }}
              filter
            >
              {modelOptions.map(m => <Select.Option key={m} value={m}>{m}</Select.Option>)}
            </Select>
          </div>

          <div className="ur-filter-group">
            <Text className="ur-filter-label">状态码</Text>
            <Select
              size="small"
              placeholder="选择状态码"
              style={{ width: '100%' }}
              onChange={(v) => setFilters(prev => ({ ...prev, status_code: v as string || undefined }))}
              value={filters.status_code || undefined}
            >
              <Select.Option value="2xx">2xx 成功</Select.Option>
              <Select.Option value="4xx">4xx 客户端错误</Select.Option>
              <Select.Option value="429">429 限流</Select.Option>
              <Select.Option value="5xx">5xx 服务端错误</Select.Option>
            </Select>
          </div>

          <div className="ur-filter-group">
            <Text className="ur-filter-label">计量来源</Text>
            <Select
              size="small"
              placeholder="来源"
              style={{ width: '100%' }}
              onChange={(v) => { setFilters(prev => ({ ...prev, usage_source: v as string || undefined })); setPage(1); }}
              value={filters.usage_source || undefined}
            >
              {usageSourceOptions.map(option => (
                <Select.Option key={option.value} value={option.value}>{option.label}</Select.Option>
              ))}
            </Select>
          </div>

          <div className="ur-filter-group">
            <Text className="ur-filter-label">延迟范围 (ms)</Text>
            <Space>
              <Input
                size="small"
                placeholder="最小"
                style={{ width: 104 }}
                onChange={(v) => updateFilter('latency_min', v)}
                defaultValue={filters.latency_min || ''}
              />
              <Text type="tertiary">-</Text>
              <Input
                size="small"
                placeholder="最大"
                style={{ width: 104 }}
                onChange={(v) => updateFilter('latency_max', v)}
                defaultValue={filters.latency_max || ''}
              />
            </Space>
          </div>

          <div className="ur-filter-group">
            <Text className="ur-filter-label">操作 ID（Request ID）</Text>
            <Input
              size="small"
              placeholder="精确匹配"
              onChange={(v) => updateFilter('request_id', v)}
              defaultValue={filters.request_id || ''}
              showClear
            />
            <Input
              size="small"
              placeholder="客户端请求 ID（精确匹配）"
              onChange={(v) => updateFilter('client_request_id', v)}
              defaultValue={filters.client_request_id || ''}
              showClear
              style={{ marginTop: 6 }}
            />
          </div>

          <Divider style={{ margin: '12px 0' }} />

          <div className="ur-filter-group">
            <Text className="ur-filter-label">证据标识</Text>
            <div style={{ display: 'grid', gap: 6 }}>
              <Input size="small" placeholder="Trace ID（精确匹配）" onChange={(v) => updateFilter('trace_id', v)} defaultValue={filters.trace_id || ''} showClear />
              <Input size="small" placeholder="工作流 ID（精确匹配）" onChange={(v) => updateFilter('workflow_id', v)} defaultValue={filters.workflow_id || ''} showClear />
              <Input size="small" placeholder="Agent ID（精确匹配）" onChange={(v) => updateFilter('agent_id', v)} defaultValue={filters.agent_id || ''} showClear />
              <Input size="small" placeholder="尝试 ID（精确匹配）" onChange={(v) => updateFilter('attempt_id', v)} defaultValue={filters.attempt_id || ''} showClear />
              <Input size="small" placeholder="上游 Request ID（精确匹配）" onChange={(v) => updateFilter('upstream_request_id', v)} defaultValue={filters.upstream_request_id || ''} showClear />
              <Input size="small" placeholder="Evidence Ref（精确匹配）" onChange={(v) => updateFilter('evidence_ref', v)} defaultValue={filters.evidence_ref || ''} showClear />
              <Input size="small" placeholder="Source Event Hash（精确匹配）" onChange={(v) => updateFilter('source_event_hash', v)} defaultValue={filters.source_event_hash || ''} showClear />
            </div>
          </div>

          {hasActiveFilters && (
            <div className="ur-filter-group">
              <Text className="ur-filter-label">当前筛选</Text>
              <div className="ur-chip-row">
                {Object.entries(filters).filter(([, v]) => v).map(([k, v]) => (
                  <Tag
                    key={k}
                    size="small"
                    closable
                    onClose={() => setFilters(prev => { const n = { ...prev }; delete (n as any)[k]; return n; })}
                  >
                    {filterLabels[k as keyof LogFilters] || k}: {filterValueLabel(k, v)}
                  </Tag>
                ))}
              </div>
            </div>
          )}
        </aside>

        <main className="ur-explorer-panel">
          <div className="ur-panel-title">
            <Title heading={5} style={{ margin: 0 }}>日志记录</Title>
            <Text className="ur-panel-hint">默认选择最新记录 · ↑/↓ 切换</Text>
          </div>

          {logsError && (
            <div className="ur-detail-card ur-detail-card-error" role="alert">
              <Text className="ur-detail-card-title">请求列表不可用</Text>
              <Text size="small">{logsError}</Text>
              <div>
                <Button size="small" type="primary" onClick={() => fetchLogs()}>重试列表</Button>
              </div>
            </div>
          )}

          <div className="ur-table-shell">
            <Table
              dataSource={logs}
              columns={tableColumns}
              rowKey="id"
              size="small"
              loading={loading}
              onRow={(record) => ({
                onClick: () => {
                  if (record) {
                    selectLog(record as Log);
                  }
                },
                style: { cursor: 'pointer', background: selectedLog?.id === (record as any)?.id ? 'var(--ur-bg-subtle)' : undefined },
              })}
              pagination={{
                currentPage: page,
                pageSize,
                total,
                showSizeChanger: true,
                pageSizeOpts: [10, 20, 50, 100],
                onChange: (p, ps) => { setPage(p); setPageSize(ps); },
              }}
              empty="暂无日志记录"
            />
          </div>
        </main>

        <aside className="ur-explorer-panel ur-explorer-detail" aria-label="执行决策凭证">
          <div className="ur-detail-header">
            <div>
              <Text className="ur-panel-hint">Execution Decision Receipt</Text>
              <Title heading={5} style={{ margin: 0 }}>执行决策凭证</Title>
            </div>
            <Tag size="small" color={selectedLog ? 'blue' : undefined}>{selectedLog ? '常驻检查器' : '等待记录'}</Tag>
          </div>

          {!selectedLog ? (
            <div className="ur-receipt-empty" role="status">
              <Title heading={5}>{logsError ? '等待可验证的请求列表' : '暂无可检查的执行'}</Title>
              <Text type="tertiary">
                {logsError
                  ? '列表加载失败时不会保留旧凭证或把失败解释为空结果。'
                  : '日志出现后将自动选择最新记录；无需再次点击打开详情。'}
              </Text>
            </div>
          ) : (
            <div className="ur-detail-stack">
              <div className="ur-detail-hero">
                <div>
                  <Text type="tertiary" size="small">操作 ID（Request ID）</Text>
                  <div className="ur-request-id-full">
                    <Text code>{selectedLog.request_id}</Text>
                    <Button type="tertiary" size="small" icon={<IconCopy />} onClick={() => copyToClipboard(selectedLog.request_id)} className="ur-copy-button" aria-label="复制当前 Request ID" />
                  </div>
                </div>
                <StatusTag status={selectedLog.status_code}>{selectedLog.status_code}</StatusTag>
              </div>

              <div className="ur-receipt-summary" aria-label="执行凭证摘要">
                <div>
                  <Text type="tertiary" size="small">Authority</Text>
                  <Text strong size="small">{authoritySummary}</Text>
                </div>
                <div>
                  <Text type="tertiary" size="small">Route</Text>
                  <Text strong size="small">{traceLoading ? '加载中' : traceError ? 'unavailable' : `${routeAttempts.length} attempt${routeAttempts.length === 1 ? '' : 's'}`}</Text>
                </div>
                <div>
                  <Text type="tertiary" size="small">Financial finality</Text>
                  <StatusTag status={billingTerminalTone}>{traceLoading ? 'loading' : billingTerminalStatus}</StatusTag>
                </div>
                <div>
                  <Text type="tertiary" size="small">Declared gaps</Text>
                  <StatusTag status={traceError ? 'warning' : gapEvents.length > 0 ? 'warning' : 'success'}>{traceLoading ? 'loading' : traceError ? 'unavailable' : gapEvents.length}</StatusTag>
                </div>
              </div>

              {traceLoading ? (
                <div className="ur-detail-card" role="status">
                  <Text className="ur-detail-card-title">执行证据加载中</Text>
                  <Text type="tertiary" size="small">正在读取当前请求的结构化 Trace；加载完成前不作缺失或终态判断。</Text>
                </div>
              ) : traceError ? (
                <div className="ur-detail-card ur-detail-card-error" role="alert">
                  <Text className="ur-detail-card-title">执行证据不可用</Text>
                  <Text size="small">{traceError}</Text>
                  <div>
                    <Button size="small" type="primary" onClick={() => loadTrace(selectedLog)}>重试证据</Button>
                  </div>
                </div>
              ) : (
                <>
                  <nav className="ur-receipt-tabs" aria-label="执行凭证视图">
                    {receiptTabs.map(tab => (
                      <Button
                        key={tab.key}
                        size="small"
                        type={receiptTab === tab.key ? 'primary' : 'tertiary'}
                        theme={receiptTab === tab.key ? 'solid' : 'borderless'}
                        aria-pressed={receiptTab === tab.key}
                        onClick={() => setReceiptTab(tab.key)}
                      >
                        {tab.label}
                      </Button>
                    ))}
                  </nav>

                  {receiptTab === 'decision' && (
                <>
                  <div className="ur-detail-card">
                    <Text className="ur-detail-card-title">执行结论</Text>
                    <div className="ur-detail-grid">
                      <div>
                        <Text type="tertiary" size="small">模型</Text>
                        <div><Tag size="small">{selectedLog.model}</Tag></div>
                      </div>
                      <div>
                        <Text type="tertiary" size="small">请求模型</Text>
                        <div><Text size="small">{selectedLog.requested_model || '-'}</Text></div>
                      </div>
                      <div>
                        <Text type="tertiary" size="small">供应商 / 通道</Text>
                        <div><Text size="small">{selectedLog.provider || '-'} / {selectedLog.channel || '-'}</Text></div>
                      </div>
                      <div>
                        <Text type="tertiary" size="small">时间</Text>
                        <div><Text size="small">{formatDateTime(selectedLog.created_at)}</Text></div>
                      </div>
                      <div>
                        <Text type="tertiary" size="small">客户端请求 ID</Text>
                        <div><Text code size="small">{selectedLog.client_request_id || '-'}</Text></div>
                      </div>
                      <div>
                        <Text type="tertiary" size="small">工作流 / Agent</Text>
                        <div><Text code size="small">{selectedLog.workflow_id || '-'} / {selectedLog.agent_id || '-'}</Text></div>
                      </div>
                    </div>
                  </div>

                  <div className="ur-detail-card">
                    <Text className="ur-detail-card-title">权限与委托</Text>
                    <Descriptions
                      row
                      size="small"
                      data={[
                        { key: 'Actor type', value: actorTypes.length > 0 ? actorTypes.map(v => <Tag key={v} size="small">{v}</Tag>) : '未捕获' },
                        { key: 'Actor ref', value: actorRefs.length > 0 ? actorRefs.map(v => <Text key={v} code size="small">{compactEvidenceValue(v, 28)}</Text>) : '未捕获' },
                        { key: 'On behalf of', value: onBehalfOf.length > 0 ? onBehalfOf.map(v => <Text key={v} code size="small">{compactEvidenceValue(v, 28)}</Text>) : '未声明' },
                        { key: 'IP', value: selectedLog.ip_address || '-' },
                      ]}
                    />
                    {actorTypes.length === 0 && !traceLoading && (
                      <Text type="tertiary" size="small">本次 Trace 未捕获权限主体；不会据此推断为匿名或未鉴权。</Text>
                    )}
                  </div>

                  <div className="ur-detail-card">
                    <div className="ur-panel-title" style={{ marginBottom: 10 }}>
                      <Text className="ur-detail-card-title">决策链</Text>
                      <Tag size="small" color="blue">{decisionEvents.length} 个</Tag>
                    </div>
                    {traceLoading ? (
                      <Text type="tertiary" size="small">加载中</Text>
                    ) : decisionEvents.length > 0 ? (
                      <div className="ur-receipt-event-list">
                        {decisionEvents.map(event => (
                          <div key={event.id}>
                            <div className="ur-receipt-event-heading">
                              <Tag size="small">{event.decision_kind || event.stage}</Tag>
                              <StatusTag status={event.decision_result || 'unknown'}>{event.decision_result || 'unknown'}</StatusTag>
                            </div>
                            <Text size="small">{event.decision_reason || event.event_type}</Text>
                            <Text type="tertiary" size="small">{event.source_component || event.stage} · {formatDateTime(event.created_at)}</Text>
                          </div>
                        ))}
                      </div>
                    ) : (
                      <Text type="tertiary" size="small">本次 Trace 未捕获结构化决策事件；不会从最终 HTTP 状态反推决策原因。</Text>
                    )}
                  </div>
                </>
                  )}

                  {receiptTab === 'attempts' && (
                <>
                  <div className="ur-detail-card">
                    <Text className="ur-detail-card-title">状态与性能</Text>
                    <Descriptions
                      row
                      size="small"
                      data={[
                        { key: '状态码', value: <StatusTag status={selectedLog.status_code}>{selectedLog.status_code}</StatusTag> },
                        { key: '上游状态', value: selectedLog.upstream_status_code ? <StatusTag status={selectedLog.upstream_status_code}>{selectedLog.upstream_status_code}</StatusTag> : '-' },
                        { key: '延迟', value: <LatencyText value={selectedLog.latency_ms} /> },
                        { key: 'TTFT', value: selectedLog.ttft_ms ? `${selectedLog.ttft_ms}ms` : '-' },
                        { key: '流式', value: selectedLog.is_stream ? <Tag size="small" color="blue">是</Tag> : <Tag size="small">否</Tag> },
                      ]}
                    />
                  </div>

                  <div className="ur-detail-card">
                    <Text className="ur-detail-card-title">路由与故障切换</Text>
                    <Descriptions
                      row
                      size="small"
                      data={[
                        { key: '路由尝试', value: traceLoading ? '加载中' : `${routeAttempts.length} 次` },
                        { key: '故障切换', value: traceLoading ? '加载中' : routeAttempts.length > 1 ? `${routeAttempts.length - 1} 次` : '0 次' },
                        { key: '最终结果', value: traceLoading ? '-' : noRoute ? <StatusTag status="failed">无可用路由</StatusTag> : routeAttempts[routeAttempts.length - 1]?.outcome === 'accepted' ? <StatusTag status="success">上游接受</StatusTag> : <StatusTag status="warning">未确认</StatusTag> },
                      ]}
                    />
                    {!traceLoading && routeAttempts.length > 0 ? (
                      <div className="ur-receipt-event-list">
                        {routeAttempts.map(attempt => (
                          <div key={`${attempt.sequence}-${attempt.attemptID || attempt.channel}`} className="ur-route-attempt">
                            <Tag size="small">#{attempt.sequence}</Tag>
                            <div>
                              <Text size="small">{attempt.provider} / {attempt.channel}</Text>
                              <div><Text type="tertiary" size="small">{attempt.mappedModel} · {attempt.eventType}</Text></div>
                              {attempt.attemptID && <div><Text code type="tertiary" size="small">{compactEvidenceValue(attempt.attemptID, 28)}</Text></div>}
                            </div>
                            <StatusTag status={attempt.outcome}>{attempt.statusCode || attempt.outcome}</StatusTag>
                          </div>
                        ))}
                      </div>
                    ) : !traceLoading ? (
                      <Text type="tertiary" size="small">本次 Trace 未捕获路由尝试；不会将日志中的供应商字段解释为完整路由链。</Text>
                    ) : null}
                  </div>

                  {selectedLog.upstream_error && (
                    <div className="ur-detail-card ur-detail-card-error">
                      <Text className="ur-detail-card-title">上游错误</Text>
                      <Text size="small">{selectedLog.upstream_error}</Text>
                    </div>
                  )}
                </>
                  )}

                  {receiptTab === 'policy' && (
                <>
                  <div className="ur-detail-card">
                    <Text className="ur-detail-card-title">Policy & Guardrail 决策</Text>
                    {traceLoading ? (
                      <Text type="tertiary" size="small">加载中</Text>
                    ) : policyEvents.length > 0 ? (
                      <div className="ur-receipt-event-list">
                        {policyEvents.map(event => (
                          <div key={event.id}>
                            <div className="ur-receipt-event-heading">
                              <Tag size="small">{event.decision_kind || event.stage}</Tag>
                              <StatusTag status={event.decision_result || event.severity}>{event.decision_result || event.severity}</StatusTag>
                            </div>
                            <Text size="small">{event.decision_reason || event.event_type}</Text>
                            <Text type="tertiary" size="small">{event.event_type}</Text>
                            {event.capture_gap && <Tag size="small" color="orange">gap: {event.capture_gap}</Tag>}
                          </div>
                        ))}
                      </div>
                    ) : (
                      <Text type="tertiary" size="small">本次 Trace 未捕获 Policy/Guardrail 决策；这不表示策略未执行，只表示当前凭证不能证明。</Text>
                    )}
                  </div>

                  <div className="ur-detail-card">
                    <Text className="ur-detail-card-title">Payload 策略边界</Text>
                    <Descriptions
                      row
                      size="small"
                      data={[
                        { key: 'Payload capture', value: payloadModes.length > 0 ? payloadModes.map(v => <Tag key={v} size="small" color="cyan">{v}</Tag>) : '未捕获' },
                        { key: 'Payload ref', value: traceEvents.some(event => event.payload_ref) ? '已声明' : '未声明' },
                        { key: 'Raw body', value: '不在此页面存储或展示' },
                      ]}
                    />
                  </div>
                </>
                  )}

                  {receiptTab === 'cost' && (
                <>
                  <div className="ur-detail-card">
                    <Text className="ur-detail-card-title">财务终态</Text>
                    <Descriptions
                      row
                      size="small"
                      data={[
                        { key: 'Terminal', value: traceLoading ? '加载中' : <StatusTag status={billingTerminalTone}>{billingTerminalStatus}</StatusTag> },
                        { key: 'Billing status', value: exactBillingStatus || '未捕获' },
                        { key: 'Usage source', value: metadataString(billingTerminalEvent, 'usage_source') || '未捕获' },
                        { key: 'Usage confidence', value: metadataString(billingTerminalEvent, 'usage_confidence') || '未捕获' },
                        { key: 'Actual', value: metadataString(billingTerminalEvent, 'actual_cost_micros') ? <MoneyText micros={metadataString(billingTerminalEvent, 'actual_cost_micros')} precision={6} /> : '未捕获' },
                        { key: 'Charged', value: metadataString(billingTerminalEvent, 'charged_cost_micros') ? <MoneyText micros={metadataString(billingTerminalEvent, 'charged_cost_micros')} precision={6} strong tone="cost" /> : '未捕获' },
                        { key: 'Money schema', value: metadataString(billingTerminalEvent, 'money_schema') || '未捕获' },
                        { key: 'Pricing version', value: metadataString(billingTerminalEvent, 'pricing_version') || '未捕获' },
                        { key: 'Evidence event', value: billingTerminalEvent?.event_type || '未捕获' },
                      ]}
                    />
                    {!billingTerminalEvent && !traceLoading && (
                      <Text type="tertiary" size="small">HTTP 成功和日志费用不会被当作财务终态；只有 Billing terminal evidence 能证明 settled。</Text>
                    )}
                    <Text type="tertiary" size="small">当前脱敏 Trace 合同不暴露 usage_authority、usage_finality 或 usage_purpose；本凭证不会从事件名或日志字段补推这些值。</Text>
                  </div>

                  <div className="ur-detail-card">
                    <Text className="ur-detail-card-title">Token & 日志费用</Text>
                    <Descriptions
                      row
                      size="small"
                      data={[
                        { key: '提示词', value: selectedLog.prompt_tokens.toLocaleString() },
                        { key: '生成', value: selectedLog.completion_tokens.toLocaleString() },
                        { key: '合计', value: <Text strong>{selectedLog.total_tokens.toLocaleString()}</Text> },
                        { key: '日志费用', value: <MoneyText micros={selectedLog.cost_micros || '0'} precision={6} strong tone="cost" /> },
                        { key: '计量来源', value: usageSourceLabel(selectedLog.usage_source) },
                      ]}
                    />
                  </div>

                  <div className="ur-detail-card">
                    <Text className="ur-detail-card-title">供应商 Prompt Cache</Text>
                    <Descriptions
                      row
                      size="small"
                      data={[
                        { key: '回传状态', value: <StatusTag status={selectedLog.cache_usage_schema ? 'success' : 'pending'}>{cacheUsageLabel(selectedLog.cache_usage_status)}</StatusTag> },
                        { key: '普通输入', value: (selectedLog.uncached_prompt_tokens ?? selectedLog.prompt_tokens).toLocaleString() },
                        { key: '缓存读取', value: (selectedLog.cache_read_tokens || 0).toLocaleString() },
                        { key: '缓存写入', value: (selectedLog.cache_creation_tokens || 0).toLocaleString() },
                        { key: '读取倍率', value: priceRatio(selectedLog.cache_read_ratio, selectedLog.effective_cache_read_price_micros_per_million, selectedLog.effective_input_price_micros_per_million) },
                        { key: '写入倍率', value: priceRatio(selectedLog.cache_creation_ratio, selectedLog.effective_cache_creation_price_micros_per_million, selectedLog.effective_input_price_micros_per_million) },
                        { key: '读取费用', value: <MoneyText micros={selectedLog.cache_read_cost_micros || '0'} precision={6} /> },
                        { key: '写入费用', value: <MoneyText micros={selectedLog.cache_creation_cost_micros || '0'} precision={6} /> },
                        { key: '价格版本', value: selectedLog.pricing_version ? <Text code size="small">{compactEvidenceValue(selectedLog.pricing_version, 28)}</Text> : '-' },
                        { key: '字段合同', value: selectedLog.cache_usage_schema || '-' },
                      ]}
                    />
                    <Text type="tertiary" size="small">只展示供应商明确回传的 Prompt Cache 用量；“未回传”不会推测命中，也不会获得缓存折扣。</Text>
                  </div>
                </>
                  )}

                  {receiptTab === 'provenance' && (
                <>
                  <div className="ur-detail-card">
                    <Text className="ur-detail-card-title">Provenance 摘要</Text>
                    <Descriptions
                      row
                      size="small"
                      data={[
                        { key: 'Trace ID', value: traceIDs.length > 0 ? <Text code>{compactEvidenceValue(traceIDs[0], 28)}</Text> : '未捕获' },
                        { key: 'Schema', value: schemaVersions.length > 0 ? schemaVersions.map(v => <Tag key={v} size="small">{v}</Tag>) : '未捕获' },
                        { key: '来源组件', value: sourceComponents.length > 0 ? sourceComponents.map(v => <Tag key={v} size="small">{v}</Tag>) : '未捕获' },
                        { key: '采集模式', value: captureModes.length > 0 ? captureModes.map(v => <Tag key={v} size="small" color="blue">{v}</Tag>) : '未捕获' },
                        { key: '上游 ID', value: upstreamRequestIDs.length > 0 ? <Text code>{compactEvidenceValue(upstreamRequestIDs[0], 28)}</Text> : '未捕获' },
                        { key: 'Evidence refs', value: evidenceRefs.length },
                        { key: 'Source hashes', value: sourceEventHashes.length },
                        { key: 'Declared gaps', value: <StatusTag status={gapEvents.length > 0 ? 'warning' : 'success'}>{gapEvents.length}</StatusTag> },
                      ]}
                    />
                    {!traceLoading && gapEvents.length === 0 && (
                      <Text type="tertiary" size="small">0 仅表示本次 Trace 没有声明 Gap，不代表其他产品、Feed 或外部系统实现了完整采集。</Text>
                    )}
                  </div>

                  <div className="ur-detail-card">
                    <Text className="ur-detail-card-title">Capture Gaps</Text>
                    {traceLoading ? (
                      <Text type="tertiary" size="small">加载中</Text>
                    ) : gapEvents.length > 0 ? (
                      <div className="ur-receipt-event-list">
                        {gapEvents.map(event => (
                          <div key={event.id}>
                            <Tag size="small" color="orange">{event.capture_gap}</Tag>
                            <Text size="small">{event.event_type}</Text>
                            <Text type="tertiary" size="small">{event.source_component || event.stage} · {formatDateTime(event.created_at)}</Text>
                          </div>
                        ))}
                      </div>
                    ) : (
                      <Text type="tertiary" size="small">本次 Trace 没有声明 Capture Gap。</Text>
                    )}
                  </div>

                  <div className="ur-detail-card">
                    <div className="ur-panel-title" style={{ marginBottom: 10 }}>
                      <Text className="ur-detail-card-title">请求路径</Text>
                      {traceLoading ? <Tag size="small">加载中</Tag> : <Tag size="small" color="blue">{traceEvents.length} 事件</Tag>}
                    </div>
                    {traceLoading ? (
                      <Text type="tertiary" size="small">加载中</Text>
                    ) : traceEvents.length > 0 ? (
                      <div className="ur-receipt-event-list">
                        {traceEvents.map(event => (
                          <div key={event.id}>
                            <div className="ur-receipt-event-heading">
                              <Tag size="small" color={traceSeverityColor(event.severity)}>{event.stage}</Tag>
                              <Text strong size="small">{event.event_type}</Text>
                              {event.status_code !== null && event.status_code !== undefined ? <StatusTag status={event.status_code}>{event.status_code}</StatusTag> : null}
                            </div>
                            <Text type="tertiary" size="small">{formatDateTime(event.created_at)} · {event.latency_ms}ms</Text>
                            {(event.provider || event.channel) && <Text size="small">{event.provider || '-'} / {event.channel || '-'}</Text>}
                            {(event.decision_kind || event.decision_result || event.decision_reason) && (
                              <Text size="small">decision: {[event.decision_kind, event.decision_result, event.decision_reason].filter(Boolean).join(' / ')}</Text>
                            )}
                            {traceEvidenceTags(event)}
                            {Object.keys(event.metadata || {}).length > 0 && (
                              <div className="ur-chip-row">
                                {Object.entries(event.metadata).slice(0, 8).map(([key, value]) => (
                                  <Tag key={key} size="small">{key}: {formatMetadataValue(value)}</Tag>
                                ))}
                              </div>
                            )}
                          </div>
                        ))}
                      </div>
                    ) : (
                      <Text type="tertiary" size="small">暂无请求路径事件。</Text>
                    )}
                  </div>

                  <div className="ur-detail-card">
                    <Text className="ur-detail-card-title">隐私边界</Text>
                    <Text type="tertiary" size="small">请求体与响应体内容未在本页面存储或展示。Payload 状态只来自结构化采集字段，不从日志文本推断。</Text>
                  </div>
                </>
                  )}
                </>
              )}
            </div>
          )}
        </aside>
      </div>
    </div>
  );
}
