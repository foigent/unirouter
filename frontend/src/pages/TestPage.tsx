import { useState, useEffect } from 'react';
import { Card, Form, Button, Toast, Tag, Typography, Table } from '@douyinfe/semi-ui';
import { api } from '../api/client';

const { Title, Text } = Typography;

interface Channel {
  id: number;
  name: string;
  provider: string;
  status: string;
}

interface TestResult {
  success: boolean;
  channel_id: number;
  channel_name: string;
  latency_ms: number;
  status_code: number;
  error?: string;
  model?: string;
  tokens_used?: number;
}

export default function TestPage() {
  const [channels, setChannels] = useState<Channel[]>([]);
  const [testing, setTesting] = useState(false);
  const [results, setResults] = useState<TestResult[]>([]);

  useEffect(() => {
    api.getChannels(1, 100).then((res: any) => {
      setChannels(res.data || []);
    });
  }, []);

  const runTest = async (channelId: number, channelName: string, model: string, prompt: string) => {
    try {
      const res = await api.request(`/channels/${channelId}/test`, {
        method: 'POST',
        body: JSON.stringify({ model, prompt }),
      });
      return { ...res, channel_name: channelName } as TestResult;
    } catch (err: any) {
      return {
        success: false,
        channel_id: channelId,
        channel_name: channelName,
        latency_ms: 0,
        status_code: 0,
        error: err.message,
      } as TestResult;
    }
  };

  const handleTest = async (values: Record<string, any>) => {
    setTesting(true);
    const channel = channels.find(c => c.id === Number(values.channel_id));
    const result = await runTest(
      Number(values.channel_id),
      channel?.name || 'Unknown',
      values.model || 'gpt-3.5-turbo',
      values.prompt || 'Hello, this is a test message.',
    );
    setResults(prev => [result, ...prev]);
    if (result.success) {
      Toast.success({ content: `测试成功 (${result.latency_ms}ms)` });
    } else {
      Toast.error({ content: `测试失败: ${result.error}` });
    }
    setTesting(false);
  };

  const handleBatchTest = async () => {
    setTesting(true);
    const activeChannels = channels.filter(c => c.status === 'active');
    const batchResults: TestResult[] = [];

    for (const channel of activeChannels) {
      const result = await runTest(channel.id, channel.name, 'gpt-3.5-turbo', 'Hello, this is a test message.');
      batchResults.push(result);
    }

    setResults(prev => [...batchResults, ...prev]);
    const successCount = batchResults.filter(r => r.success).length;
    Toast.info({ content: `批量测试完成: ${successCount}/${batchResults.length} 成功` });
    setTesting(false);
  };

  const columns = [
    {
      title: '状态',
      dataIndex: 'success',
      key: 'success',
      width: 80,
      render: (v: boolean) => (
        <Tag color={v ? 'green' : 'red'}>{v ? '成功' : '失败'}</Tag>
      ),
    },
    { title: '渠道', dataIndex: 'channel_name', key: 'channel_name', width: 150 },
    {
      title: '延迟',
      dataIndex: 'latency_ms',
      key: 'latency_ms',
      width: 100,
      render: (v: number) => `${v}ms`,
    },
    {
      title: '状态码',
      dataIndex: 'status_code',
      key: 'status_code',
      width: 80,
      render: (v: number) => (
        <Tag color={v >= 200 && v < 300 ? 'green' : v >= 400 ? 'red' : 'orange'}>
          {v}
        </Tag>
      ),
    },
    { title: '模型', dataIndex: 'model', key: 'model', width: 150 },
    {
      title: 'Token 用量',
      dataIndex: 'tokens_used',
      key: 'tokens_used',
      width: 100,
      render: (v: number) => v || '-',
    },
    {
      title: '错误信息',
      dataIndex: 'error',
      key: 'error',
      ellipsis: true,
      render: (v: string) => v ? <Text type="danger">{v}</Text> : '-',
    },
  ];

  return (
    <div>
      <Title heading={4} style={{ marginBottom: 16 }}>渠道测试</Title>

      <Card style={{ marginBottom: 16 }}>
        <Form onSubmit={handleTest}>
          <Form.Select
            field="channel_id"
            label="选择渠道"
            rules={[{ required: true, message: '请选择渠道' }]}
            placeholder="选择要测试的渠道"
            optionList={channels.map(c => ({
              label: `${c.name} (${c.provider})`,
              value: c.id,
            }))}
            style={{ width: 300 }}
          />
          <Form.Input
            field="model"
            label="测试模型"
            placeholder="gpt-3.5-turbo"
            style={{ width: 200 }}
          />
          <Form.TextArea
            field="prompt"
            label="测试 Prompt"
            placeholder="Hello, this is a test message."
            rows={3}
            style={{ width: 400 }}
          />
          <div style={{ display: 'flex', gap: 12, marginTop: 16 }}>
            <Button type="primary" htmlType="submit" loading={testing}>
              测试选中渠道
            </Button>
            <Button onClick={handleBatchTest} loading={testing}>
              批量测试所有活跃渠道
            </Button>
          </div>
        </Form>
      </Card>

      <Card>
        <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 16 }}>
          <Title heading={5}>测试结果</Title>
          <Button onClick={() => setResults([])}>清空结果</Button>
        </div>
        <Table
          columns={columns}
          dataSource={results}
          rowKey={(record) => record ? `${record.channel_id}-${record.latency_ms}` : String(Math.random())}
          pagination={false}
        />
      </Card>
    </div>
  );
}
