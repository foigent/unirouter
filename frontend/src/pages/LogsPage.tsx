import { useState, useEffect, useCallback, useRef } from 'react';
import { Typography, Card, Table, Tag, Input, Select, Button, Space, Descriptions, Tooltip, Divider } from '@douyinfe/semi-ui';
import { IconSearch, IconRefresh, IconClose, IconCopy } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { formatDateTime } from '../utils/formatDate';

const { Title, Text } = Typography;

/* ── Types ── */

interface Log {
  id: number;
  request_id: string;
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
  start_time?: string;
  end_time?: string;
  request_id?: string;
  latency_min?: string;
  latency_max?: string;
  search?: string;
}

/* ── URL Sync ── */

function filtersToParams(filters: LogFilters): string {
  const params = new URLSearchParams();
  Object.entries(filters).forEach(([k, v]) => { if (v) params.set(k, v); });
  return params.toString();
}

function paramsToFilters(): LogFilters {
  const params = new URLSearchParams(window.location.search);
  const f: LogFilters = {};
  ['model', 'provider', 'status_code', 'start_time', 'end_time', 'request_id', 'latency_min', 'latency_max', 'search'].forEach(k => {
    const v = params.get(k);
    if (v) (f as any)[k] = v;
  });
  return f;
}

/* ── Helpers ── */

function formatCost(cents: number): string {
  return `¥${(cents / 100).toFixed(4)}`;
}

function statusColor(code: number): string {
  if (code >= 200 && code < 300) return 'green';
  if (code === 429) return 'orange';
  if (code >= 500) return 'red';
  return 'orange';
}

function latencyColor(ms: number): string {
  if (ms > 2000) return '#DD2C00';
  if (ms > 1000) return '#F5A623';
  return 'inherit';
}

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text).catch(() => {});
}

function relativeTime(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime();
  if (diff < 60000) return '刚刚';
  if (diff < 3600000) return `${Math.floor(diff / 60000)}分钟前`;
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}小时前`;
  return formatDateTime(dateStr);
}

/* ── Component ── */

export default function LogsPage() {
  const [logs, setLogs] = useState<Log[]>([]);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(50);
  const [total, setTotal] = useState(0);
  const [filters, setFilters] = useState<LogFilters>(paramsToFilters);
  const [selectedLog, setSelectedLog] = useState<Log | null>(null);
  const [showDetail, setShowDetail] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>();
  const [modelOptions, setModelOptions] = useState<string[]>([]);
  const [providerOptions, setProviderOptions] = useState<string[]>([]);

  // Fetch logs
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

  // Fetch model and provider lists for filter dropdowns
  useEffect(() => {
    api.getDashboardStats().then((data: any) => {
      const models = (data.model_distribution || []).map((m: any) => m.model);
      const channels = data.channel_health || [];
      const providers = [...new Set(channels.map((c: any) => c.provider))] as string[];
      setModelOptions(models);
      setProviderOptions(providers);
    }).catch(() => {});
  }, []);

  // Sync filters to URL
  useEffect(() => {
    const qs = filtersToParams(filters);
    const url = qs ? `${window.location.pathname}?${qs}` : window.location.pathname;
    window.history.replaceState(null, '', url);
  }, [filters]);

  // Debounced filter update
  const updateFilter = useCallback((key: keyof LogFilters, value: string) => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      setFilters(prev => ({ ...prev, [key]: value || undefined }));
      setPage(1);
    }, 300);
  }, []);

  // Keyboard shortcut: Cmd+K / Ctrl+K to focus search
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

  // Row click to show detail
  const handleRowClick = (record: Log) => {
    setSelectedLog(record);
    setShowDetail(true);
  };

  // Clear all filters
  const clearFilters = () => {
    setFilters({});
    setPage(1);
  };

  const hasActiveFilters = Object.values(filters).some(v => v);

  // Time range presets
  const timePresets = [
    { label: '最近 15min', value: '15m' },
    { label: '最近 1h', value: '1h' },
    { label: '最近 24h', value: '24h' },
    { label: '最近 7d', value: '7d' },
    { label: '自定义', value: 'custom' },
  ];
  const [customTimeStart, setCustomTimeStart] = useState('');
  const [customTimeEnd, setCustomTimeEnd] = useState('');
  const [selectedPreset, setSelectedPreset] = useState<string>('');

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

  // Table columns
  const columns = [
    {
      title: 'Request ID',
      dataIndex: 'request_id',
      key: 'request_id',
      width: 140,
      render: (v: string) => (
        <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          <Text code style={{ fontSize: 12, maxWidth: 100 }} ellipsis={{ showTooltip: false }}>{v.slice(0, 12)}…</Text>
          <Tooltip content="复制">
            <Button
              type="tertiary"
              size="small"
              icon={<IconCopy />}
              onClick={(e) => { e.stopPropagation(); copyToClipboard(v); }}
              style={{ minWidth: 0, padding: '0 4px' }}
            />
          </Tooltip>
        </div>
      ),
    },
    {
      title: '模型',
      dataIndex: 'model',
      key: 'model',
      width: 140,
      render: (v: string) => <Tag size="small">{v}</Tag>,
    },
    {
      title: '渠道',
      dataIndex: 'provider',
      key: 'provider',
      width: 100,
      render: (v: string) => <Tag size="small" color="blue">{v}</Tag>,
    },
    {
      title: '状态码',
      dataIndex: 'status_code',
      key: 'status_code',
      width: 80,
      render: (v: number) => <Tag size="small" color={statusColor(v) as any}>{v}</Tag>,
    },
    {
      title: '延迟',
      dataIndex: 'latency_ms',
      key: 'latency_ms',
      width: 90,
      render: (v: number) => <Text style={{ color: latencyColor(v), fontSize: 13 }}>{v}ms</Text>,
    },
    {
      title: 'Token',
      dataIndex: 'total_tokens',
      key: 'total_tokens',
      width: 80,
      render: (v: number) => <Text size="small">{v.toLocaleString()}</Text>,
    },
    {
      title: '费用',
      dataIndex: 'cost_cents',
      key: 'cost_cents',
      width: 80,
      render: (v: number) => <Text size="small">{formatCost(v)}</Text>,
    },
    {
      title: '时间',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 160,
      render: (v: string) => {
        const diff = Date.now() - new Date(v).getTime();
        return diff < 86400000 ? (
          <Tooltip content={formatDateTime(v)}>
            <Text size="small">{relativeTime(v)}</Text>
          </Tooltip>
        ) : <Text size="small">{formatDateTime(v)}</Text>;
      },
    },
  ];

  return (
    <div style={{ display: 'flex', gap: 0, height: 'calc(100vh - 120px)' }}>
      {/* ── Left Panel: Filters (280px) ── */}
      <div style={{ width: 280, minWidth: 280, borderRight: '1px solid var(--semi-color-border)', paddingRight: 16, overflowY: 'auto' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
          <Title heading={5} style={{ margin: 0 }}>筛选器</Title>
          {hasActiveFilters && (
            <Button type="tertiary" size="small" onClick={clearFilters}>清除</Button>
          )}
        </div>

        {/* Search */}
        <div style={{ marginBottom: 16 }}>
          <Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 6 }}>搜索</Text>
          <Input
            ref={searchRef as any}
            prefix={<IconSearch />}
            placeholder="Request ID / 模型 / 渠道 ⌘K"
            onChange={(v) => updateFilter('search', v)}
            defaultValue={filters.search || ''}
            showClear
            size="small"
          />
        </div>

        {/* Time Range Presets */}
        <div style={{ marginBottom: 16 }}>
          <Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 6 }}>时间范围</Text>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
            {timePresets.map(p => (
              <Button key={p.value} size="small" type="tertiary" onClick={() => handleTimePreset(p.value)}>
                {p.label}
              </Button>
            ))}
          </div>
          {selectedPreset === 'custom' && (
            <div style={{ marginTop: 8, display: 'flex', gap: 4, alignItems: 'center' }}>
              <Input size="small" type="datetime-local" value={customTimeStart}
                onChange={setCustomTimeStart} style={{ flex: 1 }} />
              <Text type="tertiary" size="small">-</Text>
              <Input size="small" type="datetime-local" value={customTimeEnd}
                onChange={setCustomTimeEnd} style={{ flex: 1 }} />
              <Button size="small" type="primary" onClick={() => handleTimePreset('custom')}>确定</Button>
            </div>
          )}
        </div>

        <Divider style={{ margin: '12px 0' }} />

        {/* Model filter */}
        <div style={{ marginBottom: 16 }}>
          <Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 6 }}>模型</Text>
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

        {/* Provider filter */}
        <div style={{ marginBottom: 16 }}>
          <Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 6 }}>渠道 / Provider</Text>
          <Select
            size="small"
            placeholder="Provider 名称"
            value={filters.provider || undefined}
            onChange={(v) => updateFilter('provider', v as string)}
            style={{ width: '100%' }}
            filter
          >
            {providerOptions.map(p => <Select.Option key={p} value={p}>{p}</Select.Option>)}
          </Select>
        </div>

        {/* Status Code filter */}
        <div style={{ marginBottom: 16 }}>
          <Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 6 }}>状态码</Text>
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

        {/* Latency Range */}
        <div style={{ marginBottom: 16 }}>
          <Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 6 }}>延迟范围 (ms)</Text>
          <Space>
            <Input
              size="small"
              placeholder="最小"
              style={{ width: 100 }}
              onChange={(v) => updateFilter('latency_min', v)}
              defaultValue={filters.latency_min || ''}
            />
            <Text type="tertiary">—</Text>
            <Input
              size="small"
              placeholder="最大"
              style={{ width: 100 }}
              onChange={(v) => updateFilter('latency_max', v)}
              defaultValue={filters.latency_max || ''}
            />
          </Space>
        </div>

        {/* Request ID filter */}
        <div style={{ marginBottom: 16 }}>
          <Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 6 }}>Request ID</Text>
          <Input
            size="small"
            placeholder="精确匹配"
            onChange={(v) => updateFilter('request_id', v)}
            defaultValue={filters.request_id || ''}
            showClear
          />
        </div>

        {/* Active filter tags */}
        {hasActiveFilters && (
          <div style={{ marginTop: 12 }}>
            <Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 6 }}>当前筛选</Text>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
              {Object.entries(filters).filter(([_, v]) => v).map(([k, v]) => (
                <Tag
                  key={k}
                  size="small"
                  closable
                  onClose={() => setFilters(prev => { const n = { ...prev }; delete (n as any)[k]; return n; })}
                >
                  {k}: {v}
                </Tag>
              ))}
            </div>
          </div>
        )}
      </div>

      {/* ── Center Panel: Results Table (flex) ── */}
      <div style={{ flex: 1, paddingLeft: 16, paddingRight: showDetail ? 0 : 16, overflowY: 'auto', minWidth: 0 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <Title heading={5} style={{ margin: 0 }}>日志记录</Title>
            <Tag size="small" color="blue">{total} 条</Tag>
          </div>
          <Tooltip content="刷新">
            <Button icon={<IconRefresh />} size="small" onClick={() => fetchLogs()} />
          </Tooltip>
        </div>

        <Table
          dataSource={logs}
          columns={columns}
          rowKey="id"
          size="small"
          loading={loading}
          onRow={(record) => ({
            onClick: () => record && handleRowClick(record),
            style: { cursor: 'pointer', background: selectedLog?.id === (record as any)?.id ? 'var(--semi-color-fill-0)' : undefined },
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

      {/* ── Right Panel: Details (420px) ── */}
      {showDetail && selectedLog && (
        <div style={{
          width: 420,
          minWidth: 420,
          borderLeft: '1px solid var(--semi-color-border)',
          paddingLeft: 16,
          overflowY: 'auto',
        }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16, position: 'sticky', top: 0, background: 'var(--semi-color-bg-0)', zIndex: 1, paddingBottom: 8 }}>
            <Title heading={5} style={{ margin: 0 }}>请求详情</Title>
            <Button type="tertiary" size="small" icon={<IconClose />} onClick={() => setShowDetail(false)} />
          </div>

          {/* Request Info */}
          <Card style={{ borderRadius: 10, marginBottom: 12 }} bodyStyle={{ padding: 14 }}>
            <Text type="tertiary" size="small" style={{ display: 'block', marginBottom: 8 }}>请求信息</Text>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              <div>
                <Text type="tertiary" size="small">Request ID</Text>
                <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                  <Text code style={{ fontSize: 12, wordBreak: 'break-all' }}>{selectedLog.request_id}</Text>
                  <Button type="tertiary" size="small" icon={<IconCopy />} onClick={() => copyToClipboard(selectedLog.request_id)} style={{ minWidth: 0, padding: '0 4px' }} />
                </div>
              </div>
              <div style={{ display: 'flex', gap: 16 }}>
                <div>
                  <Text type="tertiary" size="small">模型</Text>
                  <div><Tag size="small">{selectedLog.model}</Tag></div>
                </div>
                <div>
                  <Text type="tertiary" size="small">请求模型</Text>
                  <div><Text size="small">{selectedLog.requested_model || '—'}</Text></div>
                </div>
              </div>
              <div style={{ display: 'flex', gap: 16 }}>
                <div>
                  <Text type="tertiary" size="small">渠道</Text>
                  <div><Tag size="small" color="blue">{selectedLog.provider}</Tag></div>
                </div>
                <div>
                  <Text type="tertiary" size="small">IP</Text>
                  <div><Text size="small">{selectedLog.ip_address || '—'}</Text></div>
                </div>
              </div>
            </div>
          </Card>

          {/* Status & Performance */}
          <Card style={{ borderRadius: 10, marginBottom: 12 }} bodyStyle={{ padding: 14 }}>
            <Text type="tertiary" size="small" style={{ display: 'block', marginBottom: 8 }}>状态 & 性能</Text>
            <Descriptions
              row
              size="small"
              data={[
                { key: '状态码', value: <Tag color={statusColor(selectedLog.status_code) as any}>{selectedLog.status_code}</Tag> },
                { key: '上游状态', value: selectedLog.upstream_status_code ? <Tag color={statusColor(selectedLog.upstream_status_code) as any}>{selectedLog.upstream_status_code}</Tag> : '—' },
                { key: '延迟', value: <Text style={{ color: latencyColor(selectedLog.latency_ms) }}>{selectedLog.latency_ms}ms</Text> },
                { key: 'TTFT', value: selectedLog.ttft_ms ? `${selectedLog.ttft_ms}ms` : '—' },
                { key: '流式', value: selectedLog.is_stream ? <Tag size="small" color="blue">是</Tag> : <Tag size="small">否</Tag> },
              ]}
            />
          </Card>

          {/* Tokens & Cost */}
          <Card style={{ borderRadius: 10, marginBottom: 12 }} bodyStyle={{ padding: 14 }}>
            <Text type="tertiary" size="small" style={{ display: 'block', marginBottom: 8 }}>Token & 费用</Text>
            <Descriptions
              row
              size="small"
              data={[
                { key: 'Prompt', value: selectedLog.prompt_tokens.toLocaleString() },
                { key: 'Completion', value: selectedLog.completion_tokens.toLocaleString() },
                { key: 'Total', value: <Text strong>{selectedLog.total_tokens.toLocaleString()}</Text> },
                { key: '费用', value: <Text strong style={{ color: '#7B61FF' }}>{formatCost(selectedLog.cost_cents)}</Text> },
                { key: '来源', value: selectedLog.usage_source || '—' },
              ]}
            />
          </Card>

          {/* Error Info */}
          {selectedLog.upstream_error && (
            <Card style={{ borderRadius: 10, marginBottom: 12, borderColor: '#DD2C00' }} bodyStyle={{ padding: 14 }}>
              <Text type="tertiary" size="small" style={{ display: 'block', marginBottom: 8 }}>错误信息</Text>
              <Text size="small" style={{ color: '#DD2C00', wordBreak: 'break-all' }}>
                {selectedLog.upstream_error}
              </Text>
            </Card>
          )}

          {/* Request Body */}
          <Card style={{ borderRadius: 10, marginBottom: 12 }} bodyStyle={{ padding: 14 }}>
            <Text type="tertiary" size="small" style={{ display: 'block', marginBottom: 8 }}>请求体</Text>
            <Text type="tertiary" size="small" style={{ fontStyle: 'italic' }}>请求体内容未存储（隐私策略）</Text>
          </Card>

          {/* Response Body */}
          <Card style={{ borderRadius: 10, marginBottom: 12 }} bodyStyle={{ padding: 14 }}>
            <Text type="tertiary" size="small" style={{ display: 'block', marginBottom: 8 }}>响应体</Text>
            <Text type="tertiary" size="small" style={{ fontStyle: 'italic' }}>响应体内容未存储（隐私策略）</Text>
          </Card>

          {/* Time */}
          <Card style={{ borderRadius: 10 }} bodyStyle={{ padding: 14 }}>
            <Text type="tertiary" size="small" style={{ display: 'block', marginBottom: 8 }}>时间</Text>
            <Text size="small">{formatDateTime(selectedLog.created_at)}</Text>
          </Card>
        </div>
      )}
    </div>
  );
}
