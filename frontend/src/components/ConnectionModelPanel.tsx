import { useMemo, useState } from 'react';
import { Banner, Button, Modal, Space, Table, Toast, Typography } from '@douyinfe/semi-ui';
import { LatencyText, StatusTag, Surface } from '../ui';
import { api } from '../api/client';
import {
  eligibleCredentialsForModel,
  parseChannelModels,
  type ChannelReadinessCredential,
} from '../utils/channelReadiness';

const { Text } = Typography;

export interface ModelConnectionChannel {
  id: number;
  name: string;
  provider: string;
  models: string;
  status: string;
  effective_status?: string;
  published_models?: string;
  routing_epoch: number;
}

export interface ConnectionModelTestResult {
  requested_model: string;
  response_model: string;
  upstream_model: string;
  success: boolean;
  latency_ms: number;
  status_code: number;
  tokens_used?: number;
  error?: string;
  tested_at: string;
  enabled?: boolean;
  route_conflict?: boolean;
}

type CredentialLoadState = 'loading' | 'ready' | 'error';

interface ConnectionModelPanelProps {
  channel: ModelConnectionChannel;
  credentials: ChannelReadinessCredential[];
  credentialLoadState: CredentialLoadState;
  credentialLoadError?: string;
  modelsPublishedElsewhere: Set<string>;
  onModelEnabled: (model: string) => void;
  onManageCredentials: () => void;
  onOpenRouteManagement: (model: string, intent?: 'replace' | 'recover') => void;
  onDiagnose: (model: string) => void;
  gatewayLicensed?: boolean;
  gatewayRestrictionMessage?: string;
  onOpenLicense?: () => void;
}

function setMembership(current: Set<string>, model: string, present: boolean) {
  const next = new Set(current);
  if (present) next.add(model); else next.delete(model);
  return next;
}

export function modelIsPublished(channel: Pick<ModelConnectionChannel, 'published_models'>, model: string) {
  return parseChannelModels(channel.published_models).includes(model);
}

function effectiveConnectionStatus(channel: ModelConnectionChannel) {
  return channel.effective_status || channel.status;
}

export function exactModelTestResult(
  requestedModel: string,
  response: Record<string, unknown>,
  testedAt = new Date().toISOString(),
): ConnectionModelTestResult {
  const responseModel = String(response.model || '').trim();
  const upstreamModel = String(response.upstream_model || responseModel || requestedModel).trim();
  if (responseModel !== requestedModel) {
    return {
      requested_model: requestedModel,
      response_model: responseModel,
      upstream_model: upstreamModel,
      success: false,
      latency_ms: Number(response.latency_ms || 0),
      status_code: Number(response.status_code || 0),
      error: `检测结果模型不匹配：请求 ${requestedModel}，返回 ${responseModel || '空值'}。已阻止启用。`,
      tested_at: testedAt,
    };
  }
  return {
    requested_model: requestedModel,
    response_model: responseModel,
    upstream_model: upstreamModel,
    success: response.success === true,
    latency_ms: Number(response.latency_ms || 0),
    status_code: Number(response.status_code || 0),
    tokens_used: Number(response.tokens_used || 0) || undefined,
    error: String(response.error || '').trim() || undefined,
    tested_at: testedAt,
  };
}

export default function ConnectionModelPanel({
  channel,
  credentials,
  credentialLoadState,
  credentialLoadError,
  modelsPublishedElsewhere,
  onModelEnabled,
  onManageCredentials,
  onOpenRouteManagement,
  onDiagnose,
  gatewayLicensed = true,
  gatewayRestrictionMessage,
  onOpenLicense,
}: ConnectionModelPanelProps) {
  const models = useMemo(() => parseChannelModels(channel.models), [channel.models]);
  const connectionStatus = effectiveConnectionStatus(channel);
  const hasRetainedRelease = models.some(model => modelIsPublished(channel, model)) && connectionStatus !== 'active';
  const [results, setResults] = useState<Record<string, ConnectionModelTestResult>>({});
  const [testingModels, setTestingModels] = useState<Set<string>>(new Set());
  const [batchTesting, setBatchTesting] = useState(false);
  const [enablingModel, setEnablingModel] = useState('');
  const [checkingRouteModel, setCheckingRouteModel] = useState('');

  const testModel = async (model: string, notify = true) => {
    if (!gatewayLicensed) {
      if (notify) Toast.error(gatewayRestrictionMessage || '模型网关商业授权未就绪，未发起真实请求');
      return undefined;
    }
    if (credentialLoadState !== 'ready') {
      if (notify) Toast.error(credentialLoadError || '供应商凭证状态尚未就绪，未发起真实请求');
      return undefined;
    }
    if (eligibleCredentialsForModel(channel, credentials, model).length === 0) {
      const result: ConnectionModelTestResult = {
        requested_model: model,
        response_model: model,
        upstream_model: model,
        success: false,
        latency_ms: 0,
        status_code: 0,
        error: '没有已启用、未冷却且允许该模型的供应商凭证；未发起真实请求。',
        tested_at: new Date().toISOString(),
      };
      setResults(current => ({ ...current, [model]: result }));
      if (notify) Toast.error(result.error!);
      return result;
    }

    setTestingModels(current => setMembership(current, model, true));
    try {
      const response = await api.request(`/channels/${channel.id}/test`, {
        method: 'POST',
        body: JSON.stringify({ model, prompt: 'Hello, this is a test message.' }),
      }) as Record<string, unknown>;
      const result = exactModelTestResult(model, response);
      setResults(current => ({ ...current, [model]: result }));
      if (notify) {
        if (result.success) Toast.success(`${model} 检测成功（${result.latency_ms}ms）`);
        else Toast.error(`${model} 检测失败：${result.error || '上游未返回成功状态'}`);
      }
      return result;
    } catch (error: any) {
      const result: ConnectionModelTestResult = {
        requested_model: model,
        response_model: model,
        upstream_model: model,
        success: false,
        latency_ms: 0,
        status_code: Number(error?.status || 0),
        error: String(error?.message || '检测请求失败'),
        tested_at: new Date().toISOString(),
      };
      setResults(current => ({ ...current, [model]: result }));
      if (notify) Toast.error(`${model} 检测失败：${result.error}`);
      return result;
    } finally {
      setTestingModels(current => setMembership(current, model, false));
    }
  };

  const executeAllTests = async () => {
    setBatchTesting(true);
    let successCount = 0;
    try {
      for (const model of models) {
        const result = await testModel(model, false);
        if (result?.success) successCount += 1;
      }
      Toast.info(`检测完成：${successCount}/${models.length} 个模型成功`);
    } finally {
      setBatchTesting(false);
    }
  };

  const confirmAllTests = () => {
    Modal.confirm({
      title: `检测 ${channel.name} 的全部 ${models.length} 个模型？`,
      content: (
        <div>
          <p>将依次向真实供应商发起 {models.length} 次最小非流式请求：</p>
          <p><Text code>{models.join('、')}</Text></p>
          <p>请求可能产生供应商费用；不会检测其他模型连接，也不会自动启用模型。</p>
        </div>
      ),
      okText: '确认检测全部模型',
      onOk: executeAllTests,
    });
  };

  const enableModel = async (model: string) => {
    const result = results[model];
    if (!result?.success || result.requested_model !== model || result.response_model !== model) {
      Toast.error('缺少该模型最新且身份一致的成功检测结果，请重新检测');
      return;
    }
    if (!channel.routing_epoch) {
      Toast.error('无法读取当前连接版本，请刷新后重新检测');
      return;
    }
    setEnablingModel(model);
    try {
      const created = await api.createRuntimeChange({
        model_name: model,
        reason: '启用已通过真实上游检测的模型连接',
        overrides: [{
          channel_id: channel.id,
          expected_routing_epoch: channel.routing_epoch,
          admission: 'allow',
        }],
      });
      const validated = await api.validateRuntimeChange(created.release_ref, created.version);
      await api.activateInitialRuntimeChange(validated.release_ref, validated.version);
      setResults(current => ({ ...current, [model]: { ...current[model], enabled: true } }));
      onModelEnabled(model);
      Toast.success(`${model} 已启用，可通过 UniRouter 调用`);
    } catch (error: any) {
      const message = String(error?.message || '启用失败');
      const conflict = message.toLowerCase().includes('conflict');
      if (conflict) {
        setResults(current => ({ ...current, [model]: { ...current[model], route_conflict: true } }));
        Toast.error('该模型已有生效连接；请使用“替换现有连接”进行灰度切换');
      } else {
        Toast.error(`启用 ${model} 失败：${message}`);
      }
    } finally {
      setEnablingModel('');
    }
  };

  const confirmEnable = (model: string) => {
    Modal.confirm({
      title: `启用 ${model}？`,
      content: `启用后，应用可以通过 UniRouter 调用 ${model}。以后更换该模型的供应商连接时，系统会使用灰度切换。`,
      okText: '确认启用此模型',
      onOk: () => enableModel(model),
    });
  };

  const prepareEnable = async (model: string) => {
    setCheckingRouteModel(model);
    try {
      const slot = await api.getRuntimeChangeSlot(model) as { stable_release_ref?: string; canary_release_ref?: string };
      if (slot.stable_release_ref || slot.canary_release_ref) {
        setResults(current => ({ ...current, [model]: { ...current[model], route_conflict: true } }));
        Toast.warning(`${model} 已有生效或灰度连接，请使用“替换现有连接”`);
        return;
      }
      confirmEnable(model);
    } catch (error: any) {
      Toast.error(`无法确认 ${model} 当前路由状态：${String(error?.message || '请稍后重试')}`);
    } finally {
      setCheckingRouteModel('');
    }
  };

  const columns = [
    {
      title: '模型',
      dataIndex: 'model',
      key: 'model',
      width: 210,
      render: (model: string) => {
        const result = results[model];
        return <Space vertical align="start" spacing={2}>
          <Text code>{model}</Text>
          {result?.upstream_model && result.upstream_model !== model && <Text type="tertiary" size="small">上游：{result.upstream_model}</Text>}
        </Space>;
      },
    },
    {
      title: '真实检测',
      key: 'test',
      width: 180,
      render: (_: unknown, row: { model: string }) => {
        const result = results[row.model];
        if (testingModels.has(row.model)) return <StatusTag status="pending">检测中</StatusTag>;
        if (!result) return <StatusTag status="pending">未检测</StatusTag>;
        return <Space>
          <StatusTag status={result.success ? 'success' : 'failed'}>{result.success ? '通过' : '失败'}</StatusTag>
          {result.latency_ms > 0 && <LatencyText value={result.latency_ms} />}
        </Space>;
      },
    },
    {
      title: '调用状态',
      key: 'route',
      width: 220,
      render: (_: unknown, row: { model: string }) => modelIsPublished(channel, row.model) && connectionStatus !== 'active'
        ? <StatusTag status="failed">当前不可调用（发布记录保留）</StatusTag>
        : modelIsPublished(channel, row.model) || results[row.model]?.enabled
          ? <StatusTag status="success">已启用</StatusTag>
          : modelsPublishedElsewhere.has(row.model)
            ? <StatusTag status="warning">其他连接已启用</StatusTag>
            : <StatusTag status="pending">未启用</StatusTag>,
    },
    {
      title: '结果',
      key: 'result',
      render: (_: unknown, row: { model: string }) => {
        const result = results[row.model];
        if (!result) return <Text type="tertiary">检测后显示网络、Key 与模型权限结果</Text>;
        if (result.error) return <Text type="danger">{result.error}</Text>;
        return <Text type="tertiary">HTTP {result.status_code} · Token {result.tokens_used || 0}</Text>;
      },
    },
    {
      title: '操作',
      key: 'actions',
      width: 290,
      render: (_: unknown, row: { model: string }) => {
        const result = results[row.model];
        const publishedHere = modelIsPublished(channel, row.model) || result?.enabled;
        const retainedRelease = modelIsPublished(channel, row.model) && connectionStatus !== 'active';
        const replacement = modelsPublishedElsewhere.has(row.model) || result?.route_conflict;
        return <Space wrap>
          <Button
            size="small"
            loading={testingModels.has(row.model)}
            disabled={!gatewayLicensed || batchTesting || credentialLoadState !== 'ready'}
            onClick={() => void testModel(row.model)}
          >检测</Button>
          {retainedRelease && <Button size="small" type="primary" onClick={() => onOpenRouteManagement(row.model, 'recover')}>查看恢复步骤</Button>}
          {!publishedHere && result?.success && (replacement
            ? <Button size="small" type="primary" onClick={() => onOpenRouteManagement(row.model, 'replace')}>替换现有连接</Button>
            : <Button size="small" type="primary" loading={enablingModel === row.model || checkingRouteModel === row.model} onClick={() => void prepareEnable(row.model)}>启用此模型</Button>)}
          <Button size="small" type="tertiary" onClick={() => onDiagnose(row.model)}>调用诊断</Button>
        </Space>;
      },
    },
  ];

  return (
    <Surface
      title={`${channel.name} · 模型上线`}
      description="每个模型独立检测、独立启用；当前连接的操作不会触发其他供应商连接。"
      actions={<Space>
        <Button onClick={onManageCredentials}>管理当前连接凭证</Button>
        <Button
          type="primary"
          loading={batchTesting}
          disabled={!gatewayLicensed || models.length === 0 || credentialLoadState !== 'ready' || testingModels.size > 0}
          onClick={confirmAllTests}
        >检测当前连接全部模型（{models.length}）</Button>
      </Space>}
    >
      {!gatewayLicensed && <Banner
        type="danger"
        description={<Space vertical align="start">
          <Text>{gatewayRestrictionMessage || '模型网关商业授权未就绪；已阻止真实检测，不会向供应商发送请求。'}</Text>
          {onOpenLicense && <Button size="small" onClick={onOpenLicense}>查看商业授权</Button>}
        </Space>}
        closeIcon={null}
        style={{ marginBottom: 12 }}
      />}
      {credentialLoadState === 'loading' && <Banner type="info" description="正在核对当前连接的供应商凭证；完成前不会发起真实请求。" closeIcon={null} style={{ marginBottom: 12 }} />}
      {credentialLoadState === 'error' && <Banner type="danger" description={credentialLoadError || '供应商凭证状态不可用；已阻止真实检测。'} closeIcon={null} style={{ marginBottom: 12 }} />}
      {hasRetainedRelease && <Banner
        type="warning"
        description="连接当前不可调用，保留的发布记录只用于审计，不代表模型仍可调用。恢复时先处理旧发布占位，再将连接改为待发布、完成真实检测并重新启用；旧发布不会自动复活连接。"
        closeIcon={null}
        style={{ marginBottom: 12 }}
      />}
      <Table
        dataSource={models.map(model => ({ model }))}
        columns={columns}
        rowKey="model"
        pagination={false}
        empty="当前连接没有配置模型"
      />
    </Surface>
  );
}
