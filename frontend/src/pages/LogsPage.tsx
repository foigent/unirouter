import { useState, useEffect } from 'react';
import { Typography, Card, Table, Tag, Input, Select, Button, Row, Col, Space } from '@douyinfe/semi-ui';
import { IconSearch, IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { formatDateTime } from '../utils/formatDate';

const { Title } = Typography;

interface Log {
  id: number;
  request_id: string;
  model: string;
  provider: string;
  status_code: number;
  latency_ms: number;
  prompt_tokens: number;
  completion_tokens: number;
  cost_cents: number;
  created_at: string;
}

interface LogFilters {
  model?: string;
  provider?: string;
  status_code?: string;
  start_time?: string;
  end_time?: string;
}

export default function LogsPage() {
  const [logs, setLogs] = useState<Log[]>([]);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [filters, setFilters] = useState<LogFilters>({});
  const [providers, setProviders] = useState<string[]>([]);

  const loadLogs = (p: number) => {
    setLoading(true);
    api.getLogs(p, 20, filters)
      .then((data) => {
        setLogs(data.data || []);
        setTotal(data.total || 0);
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    api.getChannels(1, 100).then((data) => {
      const channels = data.data || data.channels || [];
      const unique = [...new Set(channels.map((c: any) => c.provider).filter(Boolean))] as string[];
      setProviders(unique);
    }).catch(() => {});
  }, []);

  useEffect(() => { loadLogs(page); }, [page]);

  const handleSearch = () => {
    setPage(1);
    loadLogs(1);
  };

  const handleReset = () => {
    setFilters({});
    setPage(1);
    setTimeout(() => loadLogs(1), 0);
  };

  const handleFilterChange = (key: keyof LogFilters, value: string) => {
    setFilters(prev => ({ ...prev, [key]: value || undefined }));
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 60 },
    { title: 'Request ID', dataIndex: 'request_id', key: 'request_id', ellipsis: true },
    { title: '模型', dataIndex: 'model', key: 'model' },
    { title: '提供商', dataIndex: 'provider', key: 'provider' },
    {
      title: '状态',
      dataIndex: 'status_code',
      key: 'status',
      render: (v: number) =>
        v >= 200 && v < 300 ? <Tag color="green">{v}</Tag> :
        v >= 400 ? <Tag color="red">{v}</Tag> : <Tag>{v}</Tag>,
    },
    { title: '延迟', dataIndex: 'latency_ms', key: 'latency', render: (v: number) => `${v}ms` },
    { title: '输入 token', dataIndex: 'prompt_tokens', key: 'in_tok' },
    { title: '输出 token', dataIndex: 'completion_tokens', key: 'out_tok' },
    { title: '费用', dataIndex: 'cost_cents', key: 'cost', render: (v: number) => `¥${(v / 100).toFixed(4)}` },
    {
      title: '时间',
      dataIndex: 'created_at',
      key: 'time',
      render: (v: string) => v ? formatDateTime(v) : '-',
    },
  ];

  return (
    <div>
      <Title heading={4} style={{ marginBottom: 24 }}>调用日志</Title>
      <Card style={{ marginBottom: 16 }}>
        <Row gutter={[16, 16]} align="middle">
          <Col span={5}>
            <Input
              prefix={<IconSearch />}
              placeholder="模型名称"
              value={filters.model || ''}
              onChange={(v) => handleFilterChange('model', v)}
              onEnterPress={handleSearch}
            />
          </Col>
          <Col span={4}>
            <Select
              placeholder="提供商"
              value={filters.provider || undefined}
              onChange={(v) => handleFilterChange('provider', v as string)}
              style={{ width: '100%' }}
            >
              <Select.Option value="">全部</Select.Option>
              {providers.map((p) => (
                <Select.Option key={p} value={p}>{p}</Select.Option>
              ))}
            </Select>
          </Col>
          <Col span={3}>
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
          </Col>
          <Col span={4}>
            <Input
              type="datetime-local"
              placeholder="开始时间"
              value={filters.start_time || ''}
              onChange={(v) => handleFilterChange('start_time', v)}
            />
          </Col>
          <Col span={4}>
            <Input
              type="datetime-local"
              placeholder="结束时间"
              value={filters.end_time || ''}
              onChange={(v) => handleFilterChange('end_time', v)}
            />
          </Col>
          <Col span={4}>
            <Space>
              <Button type="primary" icon={<IconSearch />} onClick={handleSearch}>搜索</Button>
              <Button icon={<IconRefresh />} onClick={handleReset}>重置</Button>
            </Space>
          </Col>
        </Row>
      </Card>
      <Card>
        <Table
          columns={columns}
          dataSource={logs}
          loading={loading}
          rowKey="id"
          pagination={{
            currentPage: page,
            pageSize: 20,
            total: total,
            onPageChange: (p) => { setPage(p); },
          }}
        />
      </Card>
    </div>
  );
}
