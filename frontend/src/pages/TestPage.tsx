import { useState, useEffect, useRef } from 'react';
import { Form, Button, Toast, Typography, Table } from '@douyinfe/semi-ui';
import { api } from '../api/client';
import { LatencyText, PageHeader, StatusTag, Surface } from '../ui';

const { Text } = Typography;

interface Channel {
  id: number;
  name: string;
  provider: string;
  status: string;
  models: string;
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
  const [selectedChannelId, setSelectedChannelId] = useState<number | null>(null);
  const formApiRef = useRef<any>(null);

  useEffect(() => {
    api.getChannels(1, 100).then((res: any) => {
      setChannels(res.data || []);
    });
  }, []);

  const selectedChannel = channels.find(c => c.id === selectedChannelId);
  const modelOptions = selectedChannel?.models
    ? selectedChannel.models.split(',').map(m => m.trim()).filter(Boolean).map(m => ({ label: m, value: m }))
    : [];

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
        <StatusTag status={v ? 'success' : 'failed'}>{v ? '成功' : '失败'}</StatusTag>
      ),
    },
    { title: '渠道', dataIndex: 'channel_name', key: 'channel_name', width: 150 },
    {
      title: '延迟',
      dataIndex: 'latency_ms',
      key: 'latency_ms',
      width: 100,
      render: (v: number) => <LatencyText value={v} />,
    },
    {
      title: '状态码',
      dataIndex: 'status_code',
      key: 'status_code',
      width: 80,
      render: (v: number) => <StatusTag status={v}>{v}</StatusTag>,
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
    <div className="ur-page">
      <PageHeader
        eyebrow="Traffic"
        title="渠道测试"
        description="对单个渠道或全部活跃渠道发起非流式探测，记录延迟、状态码和错误信息。"
      />

      <Surface title="测试请求">
        <Form onSubmit={handleTest} getFormApi={(api) => { formApiRef.current = api; }}>
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
            onChange={(v) => {
              setSelectedChannelId(Number(v));
              formApiRef.current?.setValue('model', undefined as any);
            }}
          />
          <Form.Select
            field="model"
            label="测试模型"
            placeholder="选择模型"
            optionList={modelOptions}
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
      </Surface>

      <Surface title="测试结果" actions={<Button onClick={() => setResults([])}>清空结果</Button>}>
        <div className="ur-table-shell">
          <Table
            columns={columns}
            dataSource={results}
            rowKey={(record) => record ? `${record.channel_id}-${record.latency_ms}` : String(Math.random())}
            pagination={false}
          />
        </div>
      </Surface>
    </div>
  );
}
