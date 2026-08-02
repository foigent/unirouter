import { useState, useEffect, useMemo, useRef } from 'react';
import { Banner, Form, Button, Toast, Typography, Table, Modal } from '@douyinfe/semi-ui';
import { api } from '../api/client';
import { LatencyText, PageHeader, StatusTag, Surface } from '../ui';
import { loadAllPages } from '../utils/loadAllPages';
import {
  channelStatusLabel,
  eligibleCredentialsForModel,
  parseChannelModels,
  type ChannelReadinessCredential,
} from '../utils/channelReadiness';

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

export { parseChannelModels } from '../utils/channelReadiness';

export function firstChannelModel(channel?: Pick<Channel, 'models'>) {
  return parseChannelModels(channel?.models)[0] || '';
}

export function resolveChannelTestModel(channel?: Pick<Channel, 'models'>, selectedModel?: string) {
  return String(selectedModel || '').trim() || firstChannelModel(channel);
}

export function summarizeBatchChannels(channels: Channel[]) {
  return {
    included: channels.filter(channel => channel.status === 'active'),
    excluded: channels.filter(channel => channel.status !== 'active'),
  };
}

export default function TestPage() {
  const [channels, setChannels] = useState<Channel[]>([]);
  const [testing, setTesting] = useState(false);
  const [results, setResults] = useState<TestResult[]>([]);
  const [selectedChannelId, setSelectedChannelId] = useState<number | null>(null);
  const [selectedModel, setSelectedModel] = useState('');
  const [channelKeys, setChannelKeys] = useState<ChannelReadinessCredential[]>([]);
  const [credentialsLoaded, setCredentialsLoaded] = useState(false);
  const [credentialLoadError, setCredentialLoadError] = useState('');
  const formApiRef = useRef<any>(null);

  useEffect(() => {
    loadAllPages<Channel>((page, size) => api.getChannels(page, size))
      .then(setChannels)
      .catch((error: any) => Toast.error({ content: `通道列表加载失败${error?.message ? `：${error.message}` : ''}` }));
    loadAllPages<ChannelReadinessCredential>((page, size) => api.getChannelKeys(page, size))
      .then((items) => {
        setChannelKeys(items);
        setCredentialsLoaded(true);
        setCredentialLoadError('');
      })
      .catch((error: any) => {
        setChannelKeys([]);
        setCredentialsLoaded(false);
        setCredentialLoadError(`供应商凭证状态加载失败${error?.message ? `：${error.message}` : ''}`);
      });
  }, []);

  const withResultKey = (result: Omit<TestResult, 'result_key'>): TestResult => ({
    ...result,
    result_key: globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random()}`,
  });

  const selectedChannel = channels.find(c => c.id === selectedChannelId);
  const modelOptions = parseChannelModels(selectedChannel?.models).map(m => ({ label: m, value: m }));
  const batchScope = useMemo(() => summarizeBatchChannels(channels), [channels]);
  const excludedBatchDescription = batchScope.excluded.length > 0
    ? batchScope.excluded.map(channel => `${channel.name}（通道${channelStatusLabel(channel.status)}）`).join('、')
    : '无';

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
    if (!credentialsLoaded) {
      Toast.error({ content: credentialLoadError || '供应商凭证状态尚未加载，已阻止真实检测请求' });
      return;
    }
    if (eligibleCredentialsForModel(channel, channelKeys, model).length === 0) {
      Toast.error({ content: '该通道没有已启用、未冷却且允许所选模型的供应商凭证，未发起真实请求' });
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
      if (eligibleCredentialsForModel(channel, channelKeys, model).length === 0) {
        batchResults.push(withResultKey({
          success: false,
          channel_id: channel.id,
          channel_name: channel.name,
          latency_ms: 0,
          status_code: 0,
          model,
          error: '没有已启用、未冷却且允许检测模型的供应商凭证；未发起真实请求',
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
    const activeCount = batchScope.included.length;
    if (activeCount === 0) {
      Toast.warning({ content: '当前没有可检测的活跃通道' });
      return;
    }
    if (!credentialsLoaded) {
      Toast.error({ content: credentialLoadError || '供应商凭证状态尚未加载，已阻止真实检测请求' });
      return;
    }
    Modal.confirm({
      title: `确认检测 ${activeCount} 个活跃通道`,
      content: (
        <div>
          <p>当前共 {channels.length} 个通道，本次只纳入 {activeCount} 个“生效”通道；排除 {batchScope.excluded.length} 个：{excludedBatchDescription}。</p>
          <p>每个具备匹配凭证的通道都会向真实供应商发起一次非流式模型请求，可能产生供应商费用和 UniRouter 计费记录。</p>
        </div>
      ),
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

      <Banner
        type={batchScope.excluded.length > 0 ? 'warning' : 'info'}
        description={`批量检测口径：共 ${channels.length} 个通道，只检测 ${batchScope.included.length} 个状态为“生效”的通道；排除 ${batchScope.excluded.length} 个（${excludedBatchDescription}）。手动检测可用于待发布/禁用通道的上线前验证，但不会改变通道状态。`}
        closeIcon={null}
        style={{ marginBottom: 16 }}
      />
      {credentialLoadError && (
        <Banner type="danger" description={`${credentialLoadError}；为避免选错凭证，真实检测操作已阻止。`} closeIcon={null} style={{ marginBottom: 16 }} />
      )}

      <Surface title="检测请求">
        <Form onSubmit={handleTest} getFormApi={(api) => { formApiRef.current = api; }}>
          <Form.Select
            field="channel_id"
            label="选择通道"
            rules={[{ required: true, message: '请选择通道' }]}
            placeholder="选择要检测的上游通道"
            optionList={channels.map(c => ({
              label: `${c.name} (${c.provider}) · 通道${channelStatusLabel(c.status)}`,
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
          {selectedChannel && selectedChannel.status !== 'active' && (
            <Banner
              type="warning"
              description={`当前选择的是“通道${channelStatusLabel(selectedChannel.status)}”配置。手动检测只验证凭证、模型和网络，不会使该通道进入运行时路由。`}
              closeIcon={null}
              style={{ marginBottom: 12 }}
            />
          )}
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
              批量检测 {batchScope.included.length} 个生效通道
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
