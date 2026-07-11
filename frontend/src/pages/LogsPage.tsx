import { useState, useEffect, useCallback, useRef } from 'react';
import { Typography, Table, Tag, Input, Select, Button, Space, Descriptions, Tooltip, Divider, Toast } from '@douyinfe/semi-ui';
import { IconSearch, IconRefresh, IconClose, IconCopy } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { formatDateTime } from '../utils/formatDate';
import { LatencyText, MoneyText, PageHeader, StatusTag } from '../ui';
import { usageSourceLabel, usageSourceOptions } from '../utils/displayLabels';

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
  completion_tokens: number;
  total_tokens: number;
  cost_cents: number;
  is_stream: boolean;
  ip_address: string;
  requested_model: string;
  upstream_status_code: number | null;
  upstream_error: string;
  usage_source: string;
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

function uniqueEvidenceValues(events: TraceEvent[], key: keyof TraceEvent) {
  return [...new Set(events.map(event => event[key]).filter((value): value is string => typeof value === 'string' && value.length > 0))];
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
  const [logs, setLogs] = useState<Log[]>([]);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(50);
  const [total, setTotal] = useState(0);
  const [filters, setFilters] = useState<LogFilters>(paramsToFilters);
  const [selectedLog, setSelectedLog] = useState<Log | null>(null);
  const [showDetail, setShowDetail] = useState(false);
  const [traceEvents, setTraceEvents] = useState<TraceEvent[]>([]);
  const [traceLoading, setTraceLoading] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>();
  const [modelOptions, setModelOptions] = useState<string[]>([]);
  const [providerOptions, setProviderOptions] = useState<string[]>([]);
  const [customTimeStart, setCustomTimeStart] = useState('');
  const [customTimeEnd, setCustomTimeEnd] = useState('');
  const [selectedPreset, setSelectedPreset] = useState<string>('');

  const fetchLogs = useCallback(async (p?: number) => {
    setLoading(true);
    try {
      const currentPage = p ?? page;
      const data = await api.getLogs(currentPage, pageSize, filters);
      setLogs(data.data || []);
      setTotal(data.total || 0);
    } catch (e) {
      console.error('Failed to fetch logs', e);
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, filters]);

  useEffect(() => { fetchLogs(); }, [fetchLogs]);

  useEffect(() => {
    Promise.all([
      api.getModelNames().catch(() => ({ data: [] })),
      api.getChannels(1, 100).catch(() => ({ data: [] })),
    ]).then(([modelData, channelData]: any[]) => {
      const modelSource = Array.isArray(modelData?.data) ? modelData.data : modelData;
      const models = (Array.isArray(modelSource) ? modelSource : [])
        .map((m: any) => typeof m === 'string' ? m : m.model_name || m.model)
        .filter(Boolean);
      const channels = Array.isArray(channelData?.data) ? channelData.data : [];
      const providers = [...new Set(channels.map((c: any) => c.provider).filter(Boolean))] as string[];
      setModelOptions(models);
      setProviderOptions(providers);
    }).catch(() => {});
  }, []);

  useEffect(() => {
    const qs = filtersToParams(filters);
    const url = qs ? `${window.location.pathname}?${qs}` : window.location.pathname;
    window.history.replaceState(null, '', url);
  }, [filters]);

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
      }
      if (e.key === 'Escape' && showDetail) {
        setShowDetail(false);
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [showDetail]);

  const clearFilters = () => {
    setFilters({});
    setPage(1);
  };

  const openLogDetail = useCallback((record: Log) => {
    setSelectedLog(record);
    setShowDetail(true);
    setTraceEvents([]);
    setTraceLoading(true);
    api.getLogTrace(record.id)
      .then((data: any) => {
        setTraceEvents(Array.isArray(data?.events) ? data.events : []);
      })
      .catch((e) => {
        console.error('Failed to fetch request trace', e);
        setTraceEvents([]);
      })
      .finally(() => setTraceLoading(false));
  }, []);

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
      title: '渠道',
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
      title: '来源',
      dataIndex: 'usage_source',
      key: 'usage_source',
      width: 122,
      render: (v: string) => v ? <StatusTag status={v}>{usageSourceLabel(v)}</StatusTag> : '-',
    },
    {
      title: '费用',
      dataIndex: 'cost_cents',
      key: 'cost_cents',
      width: 90,
      render: (v: number) => <MoneyText cents={v} precision={4} tone="cost" />,
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
  const compactColumns = columns
    .filter(col => ['request_id', 'model', 'provider', 'status_code'].includes(col.key as string))
    .map(col => ({ ...col, width: compactColumnWidths[col.key as string] }));
  const tableColumns = showDetail && selectedLog ? compactColumns : columns;
  const traceIDs = uniqueEvidenceValues(traceEvents, 'trace_id');
  const captureModes = uniqueEvidenceValues(traceEvents, 'capture_mode');
  const payloadModes = uniqueEvidenceValues(traceEvents, 'payload_capture_mode');
  const sourceComponents = uniqueEvidenceValues(traceEvents, 'source_component');
  const upstreamRequestIDs = uniqueEvidenceValues(traceEvents, 'upstream_request_id');
  const actorTypes = uniqueEvidenceValues(traceEvents, 'actor_type');
  const captureGapCount = traceEvents.filter(event => event.capture_gap).length;
  const decisionCount = traceEvents.filter(event => event.decision_kind || event.decision_result).length;
  const routeAttempts = routeAttemptsFromTrace(traceEvents);
  const noRoute = traceEvents.some(event => event.event_type === 'routing.no_route');

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="流量治理"
        title="请求日志"
        description="用于定位失败请求、成本异常、延迟抖动和上游供应商问题。"
        actions={(
          <>
            <Tag color="blue">{total} 条</Tag>
            <Tooltip content="刷新">
              <Button icon={<IconRefresh />} size="small" onClick={() => fetchLogs()} />
            </Tooltip>
          </>
        )}
      />

      <div className={`ur-explorer ${showDetail && selectedLog ? 'ur-explorer-with-detail' : ''}`}>
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
            <Text className="ur-filter-label">渠道 / 供应商</Text>
            <Select
              size="small"
              placeholder="供应商名称"
              value={filters.provider || undefined}
              onChange={(v) => updateFilter('provider', v as string)}
              style={{ width: '100%' }}
              filter
            >
              {providerOptions.map(p => <Select.Option key={p} value={p}>{p}</Select.Option>)}
            </Select>
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
            <Text className="ur-panel-hint">点击行查看详情</Text>
          </div>

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
                    openLogDetail(record as Log);
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

        {showDetail && selectedLog && (
          <aside className="ur-explorer-panel ur-explorer-detail">
            <div className="ur-detail-header">
              <div>
                <Text className="ur-panel-hint">请求检查器</Text>
                <Title heading={5} style={{ margin: 0 }}>请求详情</Title>
              </div>
              <Button type="tertiary" size="small" icon={<IconClose />} onClick={() => setShowDetail(false)} />
            </div>

            <div className="ur-detail-stack">
              <div className="ur-detail-hero">
                <div>
                  <Text type="tertiary" size="small">操作 ID（Request ID）</Text>
                  <div className="ur-request-id-full">
                    <Text code>{selectedLog.request_id}</Text>
                    <Button type="tertiary" size="small" icon={<IconCopy />} onClick={() => copyToClipboard(selectedLog.request_id)} className="ur-copy-button" aria-label="复制 Request ID" />
                  </div>
                </div>
                <StatusTag status={selectedLog.status_code}>{selectedLog.status_code}</StatusTag>
              </div>

              <div className="ur-detail-card">
                <Text className="ur-detail-card-title">请求信息</Text>
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
                    <Text type="tertiary" size="small">渠道名称</Text>
                    <div><Text size="small">{selectedLog.channel || '-'}</Text></div>
                  </div>
                  <div>
                    <Text type="tertiary" size="small">供应商 / 协议</Text>
                    <div><Tag size="small" color="blue">{selectedLog.provider || '-'}</Tag></div>
                  </div>
                  <div>
                    <Text type="tertiary" size="small">IP</Text>
                    <div><Text size="small">{selectedLog.ip_address || '-'}</Text></div>
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
                <Text className="ur-detail-card-title">状态 & 性能</Text>
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
                <Text className="ur-detail-card-title">Token & 费用</Text>
                <Descriptions
                  row
                  size="small"
                  data={[
                    { key: '提示词', value: selectedLog.prompt_tokens.toLocaleString() },
                    { key: '生成', value: selectedLog.completion_tokens.toLocaleString() },
                    { key: '合计', value: <Text strong>{selectedLog.total_tokens.toLocaleString()}</Text> },
                    { key: '费用', value: <MoneyText cents={selectedLog.cost_cents} precision={4} strong tone="cost" /> },
                    { key: '来源', value: usageSourceLabel(selectedLog.usage_source) },
                  ]}
                />
              </div>

              {!traceLoading && traceEvents.length > 0 && (
                <div className="ur-detail-card">
                  <Text className="ur-detail-card-title">证据质量</Text>
                  <Descriptions
                    row
                    size="small"
                    data={[
                      { key: 'Trace ID', value: traceIDs.length > 0 ? <Text code>{compactEvidenceValue(traceIDs[0], 28)}</Text> : '-' },
                      { key: '来源组件', value: sourceComponents.length > 0 ? sourceComponents.map(v => <Tag key={v} size="small">{v}</Tag>) : '-' },
                      { key: '采集模式', value: captureModes.length > 0 ? captureModes.map(v => <Tag key={v} size="small" color="blue">{v}</Tag>) : '-' },
                      { key: 'Payload', value: payloadModes.length > 0 ? payloadModes.map(v => <Tag key={v} size="small" color="cyan">{v}</Tag>) : '-' },
                      { key: '上游 ID', value: upstreamRequestIDs.length > 0 ? <Text code>{compactEvidenceValue(upstreamRequestIDs[0], 28)}</Text> : '-' },
                      { key: '决策事件', value: decisionCount > 0 ? `${decisionCount} 个` : '-' },
                      { key: 'Actor', value: actorTypes.length > 0 ? actorTypes.map(v => <Tag key={v} size="small">{v}</Tag>) : '-' },
                      { key: 'Capture gap', value: captureGapCount > 0 ? <Tag size="small" color="orange">{captureGapCount} 个</Tag> : <Tag size="small" color="green">0</Tag> },
                    ]}
                  />
                </div>
              )}

              {!traceLoading && (routeAttempts.length > 0 || noRoute) && (
                <div className="ur-detail-card">
                  <Text className="ur-detail-card-title">路由与故障切换</Text>
                  <Descriptions
                    row
                    size="small"
                    data={[
                      { key: '路由尝试', value: `${routeAttempts.length} 次` },
                      { key: '故障切换', value: routeAttempts.length > 1 ? `${routeAttempts.length - 1} 次` : '0 次' },
                      { key: '最终结果', value: noRoute ? <StatusTag status="failed">无可用路由</StatusTag> : routeAttempts[routeAttempts.length - 1]?.outcome === 'accepted' ? <StatusTag status="success">上游接受</StatusTag> : <StatusTag status="warning">未确认</StatusTag> },
                    ]}
                  />
                  {routeAttempts.length > 0 && (
                    <div style={{ display: 'grid', gap: 6, marginTop: 10 }}>
                      {routeAttempts.map(attempt => (
                        <div key={`${attempt.sequence}-${attempt.attemptID || attempt.channel}`} style={{ display: 'grid', gridTemplateColumns: '52px minmax(0, 1fr) auto', gap: 8, alignItems: 'center' }}>
                          <Tag size="small">#{attempt.sequence}</Tag>
                          <div style={{ minWidth: 0 }}>
                            <Text size="small">{attempt.provider} / {attempt.channel}</Text>
                            <div><Text type="tertiary" size="small">{attempt.mappedModel} · {attempt.eventType}</Text></div>
                            {attempt.attemptID && <div><Text code type="tertiary" size="small">{compactEvidenceValue(attempt.attemptID, 28)}</Text></div>}
                          </div>
                          <StatusTag status={attempt.outcome}>{attempt.statusCode || attempt.outcome}</StatusTag>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}

              <div className="ur-detail-card">
                <div className="ur-panel-title" style={{ marginBottom: 10 }}>
                  <Text className="ur-detail-card-title">请求路径</Text>
                  {traceLoading ? <Tag size="small">加载中</Tag> : <Tag size="small" color="blue">{traceEvents.length} 事件</Tag>}
                </div>
                {traceLoading ? (
                  <Text type="tertiary" size="small">加载中</Text>
                ) : traceEvents.length > 0 ? (
                  <div style={{ display: 'grid', gap: 8 }}>
                    {traceEvents.map((event) => (
                      <div key={event.id} style={{ borderLeft: '2px solid var(--ur-border-strong)', paddingLeft: 10 }}>
                        <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
                          <Tag size="small" color={traceSeverityColor(event.severity)}>{event.stage}</Tag>
                          <Text strong size="small">{event.event_type}</Text>
                          {event.status_code ? <StatusTag status={event.status_code}>{event.status_code}</StatusTag> : null}
                        </div>
                        <div style={{ marginTop: 4, display: 'grid', gap: 3 }}>
                          <Text type="tertiary" size="small">{formatDateTime(event.created_at)} · {event.latency_ms}ms</Text>
                          {(event.provider || event.channel) && (
                            <Text size="small">{event.provider || '-'} / {event.channel || '-'}</Text>
                          )}
                          {(event.decision_kind || event.decision_result || event.decision_reason) && (
                            <Text size="small">
                              decision: {[event.decision_kind, event.decision_result, event.decision_reason].filter(Boolean).join(' / ')}
                            </Text>
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
                      </div>
                    ))}
                  </div>
                ) : (
                  <Text type="tertiary" size="small">暂无请求路径事件</Text>
                )}
              </div>

              {selectedLog.upstream_error && (
                <div className="ur-detail-card" style={{ borderColor: 'var(--ur-red)' }}>
                  <Text className="ur-detail-card-title">错误信息</Text>
                  <Text size="small" style={{ color: 'var(--ur-red)', wordBreak: 'break-all' }}>
                    {selectedLog.upstream_error}
                  </Text>
                </div>
              )}

              <div className="ur-detail-card">
                <Text className="ur-detail-card-title">请求体</Text>
                <Text type="tertiary" size="small" style={{ fontStyle: 'italic' }}>请求体内容未存储（隐私策略）</Text>
              </div>

              <div className="ur-detail-card">
                <Text className="ur-detail-card-title">响应体</Text>
                <Text type="tertiary" size="small" style={{ fontStyle: 'italic' }}>响应体内容未存储（隐私策略）</Text>
              </div>

            </div>
          </aside>
        )}
      </div>
    </div>
  );
}
