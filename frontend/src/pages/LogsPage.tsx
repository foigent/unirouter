import { useState, useEffect, useCallback, useRef } from 'react';
import { Typography, Table, Tag, Input, Select, Button, Space, Descriptions, Tooltip, Divider, Toast } from '@douyinfe/semi-ui';
import { IconSearch, IconRefresh, IconClose, IconCopy } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { formatDateTime } from '../utils/formatDate';
import { LatencyText, MoneyText, PageHeader, StatusTag } from '../ui';

const { Title, Text } = Typography;

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
  usage_source?: string;
  start_time?: string;
  end_time?: string;
  request_id?: string;
  latency_min?: string;
  latency_max?: string;
  search?: string;
}

function filtersToParams(filters: LogFilters): string {
  const params = new URLSearchParams();
  Object.entries(filters).forEach(([k, v]) => { if (v) params.set(k, v); });
  return params.toString();
}

function paramsToFilters(): LogFilters {
  const params = new URLSearchParams(window.location.search);
  const f: LogFilters = {};
  ['model', 'provider', 'status_code', 'usage_source', 'start_time', 'end_time', 'request_id', 'latency_min', 'latency_max', 'search'].forEach(k => {
    const v = params.get(k);
    if (v) (f as any)[k] = v;
  });
  return f;
}

const usageSourceOptions = [
  { label: 'upstream', value: 'upstream' },
  { label: 'counter_v2', value: 'counter_v2' },
  { label: 'adapter_estimated', value: 'adapter_estimated' },
  { label: 'estimated', value: 'estimated' },
  { label: 'no_route', value: 'no_route' },
  { label: 'upstream_failed', value: 'upstream_failed' },
];

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
      render: (v: string) => v ? <StatusTag status={v}>{v}</StatusTag> : '-',
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

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="Traffic"
        title="调用日志"
        description="用于定位失败请求、成本异常、延迟抖动和上游 provider 问题。"
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
              placeholder="Request ID / 模型 / 渠道"
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
            <Text className="ur-filter-label">渠道 / Provider</Text>
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
            <Text className="ur-filter-label">Usage Source</Text>
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
            <Text className="ur-filter-label">Request ID</Text>
            <Input
              size="small"
              placeholder="精确匹配"
              onChange={(v) => updateFilter('request_id', v)}
              defaultValue={filters.request_id || ''}
              showClear
            />
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
                    {k}: {v}
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
                    setSelectedLog(record);
                    setShowDetail(true);
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
                <Text className="ur-panel-hint">Request inspector</Text>
                <Title heading={5} style={{ margin: 0 }}>请求详情</Title>
              </div>
              <Button type="tertiary" size="small" icon={<IconClose />} onClick={() => setShowDetail(false)} />
            </div>

            <div className="ur-detail-stack">
              <div className="ur-detail-hero">
                <div>
                  <Text type="tertiary" size="small">Request ID</Text>
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
                    <Text type="tertiary" size="small">Provider / 协议</Text>
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
                    { key: 'Prompt', value: selectedLog.prompt_tokens.toLocaleString() },
                    { key: 'Completion', value: selectedLog.completion_tokens.toLocaleString() },
                    { key: 'Total', value: <Text strong>{selectedLog.total_tokens.toLocaleString()}</Text> },
                    { key: '费用', value: <MoneyText cents={selectedLog.cost_cents} precision={4} strong tone="cost" /> },
                    { key: '来源', value: selectedLog.usage_source || '-' },
                  ]}
                />
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
