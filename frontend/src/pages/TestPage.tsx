import { useState, useEffect, useMemo, useRef, useCallback } from 'react';
import { Banner, Form, Button, Toast, Typography, Table, Modal } from '@douyinfe/semi-ui';
import { api } from '../api/client';
import { LatencyText, MetricTile, PageHeader, StatusTag, Surface } from '../ui';
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
  effective_status?: string;
  published_models?: string;
  models: string;
  routing_epoch?: number;
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
  enabled?: boolean;
}

type ChannelLoadState = 'loading' | 'ready' | 'error';

export { parseChannelModels } from '../utils/channelReadiness';

export function firstChannelModel(channel?: Pick<Channel, 'models'>) {
  return parseChannelModels(channel?.models)[0] || '';
}

export function resolveChannelTestModel(channel?: Pick<Channel, 'models'>, selectedModel?: string) {
  return String(selectedModel || '').trim() || firstChannelModel(channel);
}

export function summarizeBatchChannels(channels: Channel[]) {
  return {
    included: channels.filter(channel => effectiveChannelStatus(channel) === 'active'),
    excluded: channels.filter(channel => effectiveChannelStatus(channel) !== 'active'),
  };
}

function effectiveChannelStatus(channel?: Channel) {
  return channel?.effective_status || channel?.status || '';
}

export function channelPublishesModel(channel: Channel | undefined, model: string | undefined) {
  const normalizedModel = String(model || '').trim();
  return Boolean(normalizedModel) && parseChannelModels(channel?.published_models).includes(normalizedModel);
}

export default function TestPage() {
  const [channels, setChannels] = useState<Channel[]>([]);
  const [selectedChannelTesting, setSelectedChannelTesting] = useState(false);
  const [batchTesting, setBatchTesting] = useState(false);
  const [results, setResults] = useState<TestResult[]>([]);
  const [selectedChannelId, setSelectedChannelId] = useState<number | null>(null);
  const [selectedModel, setSelectedModel] = useState('');
  const [channelKeys, setChannelKeys] = useState<ChannelReadinessCredential[]>([]);
  const [channelLoadState, setChannelLoadState] = useState<ChannelLoadState>('loading');
  const [channelLoadError, setChannelLoadError] = useState('');
  const [credentialsLoaded, setCredentialsLoaded] = useState(false);
  const [credentialLoadError, setCredentialLoadError] = useState('');
  const [formApi, setFormApi] = useState<any>(null);
  const [enablingResultKey, setEnablingResultKey] = useState('');
  const formApiRef = useRef<any>(null);
  const deepLinkApplied = useRef(false);

  const loadChannels = useCallback(async () => {
    setChannelLoadState('loading');
    setChannelLoadError('');
    try {
      const items = await loadAllPages<Channel>((page, size) => api.getChannels(page, size));
      setChannels(items);
      setChannelLoadState('ready');
    } catch (error: any) {
      setChannels([]);
      setSelectedChannelId(null);
      setSelectedModel('');
      setChannelLoadError(`通道列表加载失败${error?.message ? `：${error.message}` : ''}`);
      setChannelLoadState('error');
    }
  }, []);

  useEffect(() => {
    void loadChannels();
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
  }, [loadChannels]);

  useEffect(() => {
    if (deepLinkApplied.current || channelLoadState !== 'ready' || !formApi) return;
    deepLinkApplied.current = true;
    const params = new URLSearchParams(window.location.search);
    const channelID = Number(params.get('channel_id'));
    const channel = channels.find(item => item.id === channelID);
    if (!channel) return;
    const requestedModel = String(params.get('model') || '').trim();
    const model = parseChannelModels(channel.models).includes(requestedModel)
      ? requestedModel
      : firstChannelModel(channel);
    setSelectedChannelId(channel.id);
    setSelectedModel(model);
    formApi.setValues({ channel_id: channel.id, model });
  }, [channelLoadState, channels, formApi]);

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
  const executableBatchCount = credentialsLoaded
    ? batchScope.included.filter(channel => {
        const model = firstChannelModel(channel);
        return Boolean(model) && eligibleCredentialsForModel(channel, channelKeys, model).length > 0;
      }).length
    : 0;

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
    if (channelLoadState !== 'ready') {
      Toast.error({ content: channelLoadError || '通道列表尚未加载，已阻止真实检测请求' });
      return;
    }
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
    setSelectedChannelTesting(true);
    try {
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
    } finally {
      setSelectedChannelTesting(false);
    }
  };

  const executeBatchTest = async () => {
    if (channelLoadState !== 'ready') {
      Toast.error({ content: channelLoadError || '通道列表尚未加载，已阻止真实检测请求' });
      return;
    }
    setBatchTesting(true);
    try {
      const activeChannels = batchScope.included;
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
    } finally {
      setBatchTesting(false);
    }
  };

  const handleBatchTest = () => {
    if (channelLoadState !== 'ready') {
      Toast.error({ content: channelLoadError || '通道列表尚未加载，已阻止真实检测请求' });
      return;
    }
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
      title: `确认检查 ${activeCount} 个生效通道`,
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

  const enableInitialModel = async (result: TestResult) => {
    const channel = channels.find(item => item.id === result.channel_id);
    const model = String(result.model || '').trim();
    if (!channel || !model || !channel.routing_epoch) {
      Toast.error({ content: '无法读取当前连接快照，请刷新后重新检测' });
      return;
    }
    setEnablingResultKey(result.result_key);
    try {
      const created = await api.createRuntimeChange({
        model_name: model,
        reason: '首次启用已通过真实上游检测的模型连接',
        overrides: [{
          channel_id: channel.id,
          expected_routing_epoch: channel.routing_epoch,
          admission: 'allow',
        }],
      });
      const validated = await api.validateRuntimeChange(created.release_ref, created.version);
      await api.activateInitialRuntimeChange(validated.release_ref, validated.version);
      setResults(current => current.map(item => item.result_key === result.result_key ? { ...item, enabled: true } : item));
      setChannels(current => current.map(item => item.id === channel.id
        ? { ...item, effective_status: 'active', published_models: [item.published_models, model].filter(Boolean).join(',') }
        : item));
      Toast.success({ content: `${model} 已成为首个稳定路由，可创建应用 Key 后调用` });
    } catch (error: any) {
      const message = String(error?.message || '启用失败');
      Toast.error({
        content: message.toLowerCase().includes('conflict')
          ? '该模型已有稳定或灰度路由；替换已有路由请使用“模型目录与灰度发布”'
          : `首次启用失败：${message}`,
      });
    } finally {
      setEnablingResultKey('');
    }
  };

  const confirmInitialEnable = (result: TestResult) => {
    Modal.confirm({
      title: `启用 ${result.model || '所选模型'}？`,
      content: '这只用于该模型尚无稳定路由的首次启用。检测证据和可回滚发布记录仍会保留；以后替换路由时需要灰度。',
      okText: '确认首次启用',
      onOk: () => enableInitialModel(result),
    });
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
	{
	  title: '下一步',
	  key: 'action',
	  width: 130,
	  render: (_: unknown, result: TestResult) => {
		if (!result.success) return '-';
		if (result.enabled || channelPublishesModel(channels.find(item => item.id === result.channel_id), result.model)) {
		  return <StatusTag status="success">已启用</StatusTag>;
		}
		return <Button size="small" type="primary" loading={enablingResultKey === result.result_key} onClick={() => confirmInitialEnable(result)}>首次启用</Button>;
	  },
	},
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="模型接入"
		title="检测并启用"
		description="向真实供应商发起一次最小非流式请求，验证网络、Key 和模型权限。首次检测成功后可直接启用；替换已有路由时才进入灰度。"
      />

      {channelLoadState === 'ready' ? (
        <Banner
          type={batchScope.excluded.length > 0 ? 'warning' : 'info'}
          description={`批量检测口径：共 ${channels.length} 个通道，只检测 ${batchScope.included.length} 个状态为“生效”的通道；排除 ${batchScope.excluded.length} 个（${excludedBatchDescription}）。手动检测可用于待发布/禁用通道的上线前验证，但不会改变通道状态。`}
          closeIcon={null}
          style={{ marginBottom: 16 }}
        />
      ) : channelLoadState === 'error' ? (
        <Banner
          type="danger"
          description={(
            <div className="ur-inline-alert-action">
              <span>{channelLoadError}；当前数量未知，真实检测操作已阻止。</span>
              <Button size="small" onClick={() => void loadChannels()}>重试通道列表</Button>
            </div>
          )}
          closeIcon={null}
          style={{ marginBottom: 16 }}
        />
      ) : (
        <Banner type="info" description="正在加载通道列表；完成前不显示数量，也不会发起真实检测请求。" closeIcon={null} style={{ marginBottom: 16 }} />
      )}
      {credentialLoadError && (
        <Banner type="danger" description={`${credentialLoadError}；为避免选错凭证，真实检测操作已阻止。`} closeIcon={null} style={{ marginBottom: 16 }} />
      )}

      <div className="ur-kpi-grid ur-workload-kpis">
        <MetricTile label="全部通道" value={channelLoadState === 'ready' ? channels.length : channelLoadState === 'error' ? '读取失败' : '加载中'} tone={channelLoadState === 'error' ? 'red' : 'blue'} />
        <MetricTile label="生效通道" value={channelLoadState === 'ready' ? batchScope.included.length : '未知'} tone={channelLoadState === 'ready' ? 'green' : 'slate'} meta="由通道生命周期决定" />
        <MetricTile label="可发起真实检测" value={channelLoadState !== 'ready' ? '未知' : credentialsLoaded ? executableBatchCount : '待凭证状态'} tone={channelLoadState === 'ready' && credentialsLoaded && executableBatchCount === batchScope.included.length ? 'green' : 'amber'} meta="还需凭证、模型和冷却状态" />
        <MetricTile label="待发布 / 禁用" value={channelLoadState === 'ready' ? batchScope.excluded.length : '未知'} tone={channelLoadState === 'ready' && batchScope.excluded.length ? 'amber' : 'slate'} meta="凭证启用不会改变此状态" />
      </div>

      <Surface title="检测请求">
        <Form onSubmit={handleTest} getFormApi={(api) => { formApiRef.current = api; setFormApi(api); }}>
          <Form.Select
            field="channel_id"
            label="选择通道"
            rules={[{ required: true, message: '请选择通道' }]}
            placeholder="选择要检测的上游通道"
            optionList={channels.map(c => ({
			  label: `${c.name} (${c.provider}) · ${effectiveChannelStatus(c) === 'active' ? '已启用' : `通道${channelStatusLabel(c.status)}`}`,
              value: c.id,
            }))}
            disabled={channelLoadState !== 'ready'}
            style={{ width: 300 }}
            onChange={(v) => {
              const nextChannel = channels.find(c => c.id === Number(v));
              const nextModel = firstChannelModel(nextChannel);
              setSelectedChannelId(nextChannel ? Number(v) : null);
              setSelectedModel(nextModel);
              formApiRef.current?.setValue('model', (nextModel || undefined) as any);
            }}
          />
		  {selectedChannel && !channelPublishesModel(selectedChannel, selectedModel) && (
            <Banner
              type="warning"
			  description={`当前选择的是“通道${channelStatusLabel(selectedChannel.status)}”配置。检测成功后可点击结果中的“首次启用”。`}
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
            <Button type="primary" onClick={submitSelectedChannelTest} loading={selectedChannelTesting} disabled={channelLoadState !== 'ready' || !credentialsLoaded || batchTesting}>
              检测选中通道
            </Button>
            <Button
              onClick={handleBatchTest}
              loading={batchTesting}
              disabled={channelLoadState !== 'ready' || batchScope.included.length === 0 || selectedChannelTesting}
            >
              {channelLoadState !== 'ready'
                ? '批量检测（通道未就绪）'
                : batchScope.included.length === 0
                  ? '无生效通道可批量检测'
                  : `批量检测 ${batchScope.included.length} 个生效通道`}
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
