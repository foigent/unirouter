import { useState, useEffect } from 'react';
import { Typography, Card, Table, Tag, Input, Select, Button, Space, Modal, Descriptions } from '@douyinfe/semi-ui';
import { IconSearch, IconRefresh, IconClose } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { formatDateTime } from '../utils/formatDate';

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
  start_time?: string;
  end_time?: string;
  request_id?: string;
  latency_min?: string;
  latency_max?: string;
  search?: string;
}

// Sync filters to URL params
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

export default function LogsPage() {
  const [logs, setLogs] = useState<Log[]>([]);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [total, setTotal] = useState(0);
  const [filters, setFilters] = useState<LogFilters>(paramsToFilters());
  const [providers, setProviders] = useState<string[]>([]);
  const [selectedLog, setSelectedLog] = useState<Log | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [globalSearch, setGlobalSearch] = useState('');

  const loadLogs = (p: number, f?: LogFilters) => {
    setLoading(true);
    const useFilters = f ?? filters;
    api.getLogs(p, pageSize, useFilters)
      .then((data) => {
        setLogs(data.data || []);
        setTotal(data.total || 0);
      })
      .finally(() => setLoading(false));
  };

  // Sync filters to URL
  useEffect(() => {
    const qs = filtersToParams(filters);
    const url = qs ? `${window.location.pathname}?${qs}` : window.location.pathname;
    window.history.replaceState(null, '', url);
  }, [filters]);

  useEffect(() => {
    api.getChannels(1, 100).then((data) => {
      const channels = data.data || data.channels || [];
      const unique = [...new Set(channels.map((c: any) => c.provider).filter(Boolean))] as string[];
      setProviders(unique);
    }).catch(() => {});
  }, []);

  useEffect(() => { loadLogs(page); }, [page, pageSize]);

  // Cmd+K global search
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault();
        setSearchOpen(true);
      }
      if (e.key === 'Escape') {
        setSelectedLog(null);
        setSearchOpen(false);
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);

  const handleSearch = () => {
    setPage(1);
    loadLogs(1);
  };

  const handleReset = () => {
    setFilters({});
    setPage(1);
    setTimeout(() => loadLogs(1, {}), 0);
  };

  const handleFilterChange = (key: keyof LogFilters, value: string) => {
    setFilters(prev => ({ ...prev, [key]: value || undefined }));
  };

  const handleGlobalSearch = () => {
    const newFilters = { ...filters, search: globalSearch || undefined };
    setFilters(newFilters);
    setPage(1);
    setSearchOpen(false);
    setTimeout(() => loadLogs(1, newFilters), 0);
  };

  const handleRowClick = (record: Log | undefined) => {
    if (!record) return;
    setDetailLoading(true);
    api.getLogDetail(record.id)
      .then((data: any) => setSelectedLog(data))
      .catch(() => setSelectedLog(record))
      .finally(() => setDetailLoading(false));
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 60 },
    { title: 'Request ID', dataIndex: 'request_id', key: 'request_id', ellipsis: true, width: 180 },
    { title: '模型', dataIndex: 'model', key: 'model', width: 140 },
    { title: '供应商', dataIndex: 'provider', key: 'provider', width: 100 },
    {
      title: '状态', dataIndex: 'status_code', key: 'status', width: 70,
      render: (v: number) =>
        v >= 200 && v < 300 ? <Tag color="green">{v}</Tag> :
        v >= 400 ? <Tag color="red">{v}</Tag> : <Tag>{v}</Tag>,
    },
    { title: '延迟', dataIndex: 'latency_ms', key: 'latency', width: 80, render: (v: number) => `${v}ms` },
    { title: '输入', dataIndex: 'prompt_tokens', key: 'in_tok', width: 70 },
    { title: '输出', dataIndex: 'completion_tokens', key: 'out_tok', width: 70 },
    { title: '费用', dataIndex: 'cost_cents', key: 'cost', width: 80, render: (v: number) => `¥${(v / 100).toFixed(4)}` },
    { title: '时间', dataIndex: 'created_at', key: 'time', width: 160, render: (v: string) => v ? formatDateTime(v) : '-' },
  ];

  return (
    <div style={{ display: 'flex', gap: 0, height: 'calc(100vh - 120px)' }}>
      {/* Left: filter panel */}
      <div style={{ width: 240, flexShrink: 0, marginRight: 16 }}>
        <Card style={{ height: '100%' }}>
          <Title heading={6} style={{ marginBottom: 16 }}>筛选条件</Title>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <Input
              prefix={<IconSearch />}
              placeholder="模型名称"
              value={filters.model || ''}
              onChange={(v) => handleFilterChange('model', v)}
              onEnterPress={handleSearch}
            />
            <Select
              placeholder="供应商"
              value={filters.provider || undefined}
              onChange={(v) => handleFilterChange('provider', v as string)}
              style={{ width: '100%' }}
            >
              <Select.Option value="">全部</Select.Option>
              {providers.map((p) => (
                <Select.Option key={p} value={p}>{p}</Select.Option>
              ))}
            </Select>
            <Select
              placeholder="状态码"
              value={filters.status_code || undefined}
              onChange={(v) => handleFilterChange('status_code', v as string)}
              style={{ width: '100%' }}
            >
              <Select.Option value="">全部</Select.Option>
              <Select.Option value="200">200</Select.Option>
              <Select.Option value="400">400</Select.Option>
              <Select.Option value="401">401</Select.Option>
              <Select.Option value="402">402</Select.Option>
              <Select.Option value="429">429</Select.Option>
              <Select.Option value="500">500</Select.Option>
              <Select.Option value="502">502</Select.Option>
            </Select>
            <Input
              placeholder="Request ID"
              value={filters.request_id || ''}
              onChange={(v) => handleFilterChange('request_id', v)}
              onEnterPress={handleSearch}
            />
            <div style={{ display: 'flex', gap: 8 }}>
              <Input
                placeholder="最小延迟ms"
                value={filters.latency_min || ''}
                onChange={(v) => handleFilterChange('latency_min', v)}
                style={{ width: '50%' }}
              />
              <Input
                placeholder="最大延迟ms"
                value={filters.latency_max || ''}
                onChange={(v) => handleFilterChange('latency_max', v)}
                style={{ width: '50%' }}
              />
            </div>
            <Input
              type="datetime-local"
              placeholder="开始时间"
              value={filters.start_time || ''}
              onChange={(v) => handleFilterChange('start_time', v)}
            />
            <Input
              type="datetime-local"
              placeholder="结束时间"
              value={filters.end_time || ''}
              onChange={(v) => handleFilterChange('end_time', v)}
            />
            <Space>
              <Button type="primary" icon={<IconSearch />} onClick={handleSearch} block>搜索</Button>
              <Button icon={<IconRefresh />} onClick={handleReset} block>重置</Button>
            </Space>
          </div>
          <div style={{ marginTop: 16, padding: '8px 0', borderTop: '1px solid var(--semi-color-border)' }}>
            <Text size="small" type="secondary">
              快捷键: <Tag size="small">⌘K</Tag> 全局搜索
            </Text>
          </div>
        </Card>
      </div>

      {/* Center: log table */}
      <div style={{ flex: 1, minWidth: 0 }}>
        <Card style={{ height: '100%' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
            <Title heading={5} style={{ margin: 0 }}>调用日志</Title>
            <Button
              icon={<IconSearch />}
              onClick={() => setSearchOpen(true)}
              style={{ borderRadius: 6 }}
            >
              搜索 (⌘K)
            </Button>
          </div>
          <Table
            columns={columns}
            dataSource={logs}
            loading={loading}
            rowKey="id"
            size="small"
            onRow={(record) => ({
              onClick: () => record && handleRowClick(record as Log),
              style: { cursor: 'pointer', background: selectedLog?.id === (record as any)?.id ? 'var(--semi-color-fill-0)' : undefined },
            })}
            pagination={{
              currentPage: page,
              pageSize,
              total,
              pageSizeOpts: [20, 50, 100],
              showSizeChanger: true,
              onPageChange: (p) => setPage(p),
              onPageSizeChange: (s) => { setPageSize(s); setPage(1); },
            }}
          />
        </Card>
      </div>

      {/* Right: detail panel */}
      {selectedLog && (
        <div style={{ width: 380, flexShrink: 0, marginLeft: 16 }}>
          <Card style={{ height: '100%', overflow: 'auto' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
              <Title heading={6} style={{ margin: 0 }}>请求详情</Title>
              <Button icon={<IconClose />} type="tertiary" size="small" onClick={() => setSelectedLog(null)} />
            </div>
            {detailLoading ? (
              <div style={{ textAlign: 'center', padding: 40 }}>加载中...</div>
            ) : (
              <Descriptions
                row
                data={[
                  { key: 'Request ID', value: selectedLog.request_id },
                  { key: '模型', value: selectedLog.model },
                  { key: '请求模型', value: selectedLog.requested_model || '—' },
                  { key: '供应商', value: selectedLog.provider },
                  { key: '渠道', value: selectedLog.channel || '—' },
                  { key: '状态码', value: selectedLog.status_code },
                  { key: '上游状态码', value: selectedLog.upstream_status_code ?? '—' },
                  { key: '延迟', value: `${selectedLog.latency_ms}ms` },
                  { key: 'TTFT', value: selectedLog.ttft_ms ? `${selectedLog.ttft_ms}ms` : '—' },
                  { key: '输入 Tokens', value: selectedLog.prompt_tokens },
                  { key: '输出 Tokens', value: selectedLog.completion_tokens },
                  { key: '总 Tokens', value: selectedLog.total_tokens },
                  { key: '费用', value: `¥${(selectedLog.cost_cents / 100).toFixed(4)}` },
                  { key: '流式', value: selectedLog.is_stream ? '是' : '否' },
                  { key: 'IP', value: selectedLog.ip_address || '—' },
                  { key: '来源', value: selectedLog.usage_source || '—' },
                  { key: '上游错误', value: selectedLog.upstream_error || '—' },
                  { key: '时间', value: selectedLog.created_at ? formatDateTime(selectedLog.created_at) : '—' },
                ].map(d => ({ ...d, value: String(d.value) }))}
              />
            )}
          </Card>
        </div>
      )}

      {/* Global search modal (Cmd+K) */}
      <Modal
        title="全局搜索"
        visible={searchOpen}
        onCancel={() => setSearchOpen(false)}
        onOk={handleGlobalSearch}
        okText="搜索"
        cancelText="取消"
        style={{ width: 480 }}
      >
        <Input
          prefix={<IconSearch />}
          placeholder="搜索 Request ID、模型、供应商、错误信息..."
          value={globalSearch}
          onChange={(v) => setGlobalSearch(v)}
          onEnterPress={handleGlobalSearch}
          autoFocus
        />
      </Modal>
    </div>
  );
}
