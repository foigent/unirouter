import { useState, useEffect, useRef } from 'react';
import { Form, Button, Toast, Typography, Table, Modal } from '@douyinfe/semi-ui';
import { api } from '../api/client';
import { LatencyText, PageHeader, StatusTag, Surface } from '../ui';
import { loadAllPages } from '../utils/loadAllPages';

const { Text } = Typography;

export interface Channel {
  id: number;
  name: string;
  provider: string;
  status: string;
  models: string;
}

interface TestResult {
  result_key: string;
  success: boolean;
  channel_id: number;
  channel_name: string;
  latency_ms: number;
  status_code: number;
  error?: string;
  model?: string;
  tokens_used?: number;
}

export function parseChannelModels(models?: string) {
  return String(models || '')
    .split(',')
    .map(model => model.trim())
    .filter(Boolean);
}

export function firstChannelModel(channel?: Pick<Channel, 'models'>) {
  return parseChannelModels(channel?.models)[0] || '';
}

export function resolveChannelTestModel(channel?: Pick<Channel, 'models'>, selectedModel?: string) {
  return String(selectedModel || '').trim() || firstChannelModel(channel);
}

export default function TestPage() {
  const [channels, setChannels] = useState<Channel[]>([]);
  const [testing, setTesting] = useState(false);
  const [results, setResults] = useState<TestResult[]>([]);
  const [selectedChannelId, setSelectedChannelId] = useState<number | null>(null);
  const [selectedModel, setSelectedModel] = useState('');
  const formApiRef = useRef<any>(null);

  useEffect(() => {
    loadAllPages<Channel>((page, size) => api.getChannels(page, size))
      .then(setChannels)
      .catch((error: any) => Toast.error({ content: `通道列表加载失败${error?.message ? `：${error.message}` : ''}` }));
  }, []);

  const withResultKey = (result: Omit<TestResult, 'result_key'>): TestResult => ({
    ...result,
    result_key: globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random()}`,
  });

  const selectedChannel = channels.find(c => c.id === selectedChannelId);
  const modelOptions = parseChannelModels(selectedChannel?.models).map(m => ({ label: m, value: m }));

  const runTest = async (channelId: number, channelName: string, model: string, prompt: string) => {
    try {
      const res = await api.request(`/channels/${channelId}/test`, {
        method: 'POST',
        body: JSON.stringify({ model, prompt }),
      });
      return withResultKey({ ...res, channel_name: channelName } as Omit<TestResult, 'result_key'>);
    } catch (err: any) {
      return withResultKey({
        success: false,
        channel_id: channelId,
        channel_name: channelName,
        latency_ms: 0,
        status_code: 0,
        error: err.message,
      });
    }
  };

  const handleTest = async (values: Record<string, any>) => {
    const channelId = Number(values.channel_id || selectedChannelId);
    const channel = channels.find(c => c.id === channelId);
    const model = resolveChannelTestModel(channel, values.model || selectedModel);
    if (!channel || !model) {
      Toast.error({ content: '请选择已配置模型的上游通道' });
      return;
    }
    setTesting(true);
    const result = await runTest(
      channelId,
      channel.name,
      model,
      values.prompt || 'Hello, this is a test message.',
    );
    setResults(prev => [result, ...prev]);
    if (result.success) {
      Toast.success({ content: `检测成功 (${result.latency_ms}ms)` });
    } else {
      Toast.error({ content: `检测失败: ${result.error}` });
    }
    setTesting(false);
  };

  const executeBatchTest = async () => {
    setTesting(true);
    const activeChannels = channels.filter(c => c.status === 'active');
    const batchResults: TestResult[] = [];

    for (const channel of activeChannels) {
      const model = firstChannelModel(channel);
      if (!model) {
        batchResults.push(withResultKey({
          success: false,
          channel_id: channel.id,
          channel_name: channel.name,
          latency_ms: 0,
          status_code: 0,
          error: '通道未配置检测模型',
        }));
        continue;
      }
      const result = await runTest(channel.id, channel.name, model, 'Hello, this is a test message.');
      batchResults.push(result);
    }

    setResults(prev => [...batchResults, ...prev]);
    const successCount = batchResults.filter(r => r.success).length;
    Toast.info({ content: `批量检测完成: ${successCount}/${batchResults.length} 成功` });
    setTesting(false);
  };

  const handleBatchTest = () => {
    const activeCount = channels.filter((channel) => channel.status === 'active').length;
    if (activeCount === 0) {
      Toast.warning({ content: '当前没有可检测的活跃通道' });
      return;
    }
    Modal.confirm({
      title: `确认检测 ${activeCount} 个活跃通道`,
      content: '每个通道都会向真实供应商发起一次非流式模型请求，可能产生供应商费用和 UniRouter 计费记录。确定继续吗？',
      okText: '确认并发起真实请求',
      onOk: executeBatchTest,
    });
  };

  const submitSelectedChannelTest = () => {
    if (!formApiRef.current) {
      Toast.error({ content: '检测表单尚未就绪' });
      return;
    }
    handleTest(formApiRef.current.getValues() || {});
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
    { title: '通道', dataIndex: 'channel_name', key: 'channel_name', width: 150 },
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
        eyebrow="模型接入"
        title="连通性检测"
        description="向真实供应商发起非流式模型请求，验证凭证、模型和网络链路；检测可能产生供应商费用，并写入正常用量与计费记录。"
      />

      <Surface title="检测请求">
        <Form onSubmit={handleTest} getFormApi={(api) => { formApiRef.current = api; }}>
          <Form.Select
            field="channel_id"
            label="选择通道"
            rules={[{ required: true, message: '请选择通道' }]}
            placeholder="选择要检测的上游通道"
            optionList={channels.map(c => ({
              label: `${c.name} (${c.provider})`,
              value: c.id,
            }))}
            style={{ width: 300 }}
            onChange={(v) => {
              const nextChannel = channels.find(c => c.id === Number(v));
              const nextModel = firstChannelModel(nextChannel);
              setSelectedChannelId(nextChannel ? Number(v) : null);
              setSelectedModel(nextModel);
              formApiRef.current?.setValue('model', (nextModel || undefined) as any);
            }}
          />
          <Form.Select
            field="model"
            label="检测模型"
            placeholder="选择模型"
            optionList={modelOptions}
            style={{ width: 200 }}
            onChange={(v) => setSelectedModel(String(v || ''))}
          />
          <Form.TextArea
            field="prompt"
            label="检测提示词"
            placeholder="Hello, this is a test message."
            rows={3}
            style={{ width: 400 }}
          />
          <div style={{ display: 'flex', gap: 12, marginTop: 16 }}>
            <Button type="primary" onClick={submitSelectedChannelTest} loading={testing}>
              检测选中通道
            </Button>
            <Button onClick={handleBatchTest} loading={testing}>
              批量检测活跃通道
            </Button>
          </div>
        </Form>
      </Surface>

      <Surface title="检测结果" actions={<Button onClick={() => setResults([])}>清空结果</Button>}>
        <div className="ur-table-shell">
          <Table
            columns={columns}
            dataSource={results}
            rowKey="result_key"
            pagination={false}
          />
        </div>
      </Surface>
    </div>
  );
}
