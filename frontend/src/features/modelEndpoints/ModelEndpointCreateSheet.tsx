import { useEffect, useMemo, useRef, useState } from 'react';
import { Banner, Button, Form, SideSheet, Toast, Typography } from '@douyinfe/semi-ui';
import {
  modelDeploymentApi,
  type CanonicalModelApplication,
  type CanonicalModelApplicationCreateRequest,
  type CanonicalModelApplicationCreateResult,
  type CanonicalOperationID,
} from '../../api/modelDeployments';
import { ApiError } from '../../api/transport';
import { PasswordField } from '../../ui';
import { providerPreset, providerPresets } from '../../utils/providerEndpoints';
import { endpointError } from './presentation';

const { Text } = Typography;

type Props = {
  open: boolean;
  applications: CanonicalModelApplication[];
  onCancel: () => void;
  onCreated: (result: CanonicalModelApplicationCreateResult) => Promise<void> | void;
};

type CreateIntent = { fingerprint: string; idempotencyKey: string };

const operationOptionsByAdapter: Record<string, { label: string; value: CanonicalOperationID }[]> = {
  openai_compatible: [
    { label: 'Chat Completions · POST /v1/chat/completions', value: 'openai.chat_completions.v1' },
    { label: 'Embeddings · POST /v1/embeddings', value: 'openai.embeddings.v1' },
    { label: 'Responses · POST /v1/responses', value: 'openai.responses.v1' },
  ],
  azure_openai: [
    { label: 'Chat Completions · POST /v1/chat/completions', value: 'openai.chat_completions.v1' },
    { label: 'Embeddings · POST /v1/embeddings', value: 'openai.embeddings.v1' },
  ],
  anthropic_native: [
    { label: 'Messages · POST /v1/messages', value: 'anthropic.messages.v1' },
  ],
};

const exactHTTPSOriginOperations = new Set<CanonicalOperationID>([
  'openai.responses.v1',
  'anthropic.messages.v1',
]);

const maxCanonicalTokenLimit = 2_147_483_647;

function positiveCanonicalTokenLimit(value: unknown) {
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 && parsed <= maxCanonicalTokenLimit ? parsed : 0;
}

function canonicalPublicModelExecutionLimitsReady(model: CanonicalModelApplication['public_model']) {
  return positiveCanonicalTokenLimit(model.context_length) > 0
    && positiveCanonicalTokenLimit(model.output_limit) > 0;
}

const presetOperationBaseURLs: Partial<Record<string, Partial<Record<CanonicalOperationID, string>>>> = {
  openai: { 'openai.responses.v1': 'https://api.openai.com' },
};

export function canonicalOperationBaseURLForPreset(presetValue: string, operationID: CanonicalOperationID) {
  const preset = providerPreset(presetValue);
  return presetOperationBaseURLs[preset.value]?.[operationID] || preset.baseURL;
}

export function canonicalOperationOriginError(operationID: CanonicalOperationID, rawBaseURL: unknown) {
  if (!exactHTTPSOriginOperations.has(operationID)) return '';
  const baseURL = String(rawBaseURL || '').replace(/^ +| +$/gu, '');
  if (!baseURL || /[^\x20-\x7e]/u.test(baseURL)) {
    return '该 operation 要求仅包含 ASCII 的 path-free HTTPS origin，例如 https://api.openai.com';
  }
  try {
    const parsed = new URL(baseURL);
    const authority = baseURL.slice('https://'.length).split('/')[0];
    if (
      parsed.protocol !== 'https:'
      || parsed.username
      || parsed.password
      || !parsed.hostname
      || (parsed.pathname !== '' && parsed.pathname !== '/')
      || parsed.search
      || parsed.hash
      || authority.endsWith(':')
    ) {
      return '该 operation 要求 path-free HTTPS origin；不能包含 /v1、其他路径、凭证、query 或 fragment';
    }
  } catch {
    return '该 operation 要求有效的 path-free HTTPS origin，例如 https://api.openai.com';
  }
  return '';
}

function newIdempotencyKey() {
  const random = globalThis.crypto?.randomUUID?.()
    || `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
  return `ui-model-deployment-${random}`;
}

export default function ModelEndpointCreateSheet({ open, applications, onCancel, onCreated }: Props) {
  const [formApi, setFormApi] = useState<any>(null);
  const [connectionMode, setConnectionMode] = useState<'new' | 'existing'>('new');
  const [publicModelMode, setPublicModelMode] = useState<'new' | 'existing'>('new');
  const [credentialMode, setCredentialMode] = useState<'new' | 'existing'>('new');
  const [selectedPreset, setSelectedPreset] = useState('deepseek');
  const [adapterType, setAdapterType] = useState('openai_compatible');
  const [operationID, setOperationID] = useState<CanonicalOperationID>('openai.chat_completions.v1');
  const [newConnectionBaseURL, setNewConnectionBaseURL] = useState('');
  const [selectedConnectionID, setSelectedConnectionID] = useState(0);
  const [saving, setSaving] = useState(false);
  const createIntent = useRef<CreateIntent | null>(null);

  const connections = useMemo(() => Array.from(new Map(
    applications.filter(item => !item.connection.retired_at).map(item => [item.connection.id, item.connection]),
  ).values()), [applications]);
  const publicModels = useMemo(() => Array.from(new Map(
    applications.filter(item => item.public_model.lifecycle !== 'retired').map(item => [item.public_model.id, item.public_model]),
  ).values()), [applications]);
  const existingBindings = useMemo(() => Array.from(new Map(
    applications
      .filter(item => item.connection.id === selectedConnectionID)
      .flatMap(item => item.credential.bindings)
      .filter(binding => binding.status !== 'retired')
      .map(binding => [binding.id, binding]),
  ).values()), [applications, selectedConnectionID]);

  useEffect(() => {
    if (!open || !formApi) return;
    const preset = providerPreset('deepseek');
    formApi.reset();
    formApi.setValues({
      connection_mode: 'new',
      public_model_mode: 'new',
      credential_mode: 'new',
      preset: preset.value,
      provider: preset.provider,
      base_url: preset.baseURL,
      adapter_type: preset.adapterType,
      lifecycle: 'staging',
      route_role: 'primary',
      operation_id: 'openai.chat_completions.v1',
    });
    setConnectionMode('new');
    setPublicModelMode('new');
    setCredentialMode('new');
    setSelectedPreset(preset.value);
    setAdapterType(preset.adapterType);
    setOperationID('openai.chat_completions.v1');
    setNewConnectionBaseURL(preset.baseURL);
    setSelectedConnectionID(0);
    createIntent.current = null;
  }, [formApi, open]);

  const adapterOperations = operationOptionsByAdapter[adapterType] || [];
  const selectedExistingConnection = connections.find(connection => connection.id === selectedConnectionID);
  const effectiveBaseURL = connectionMode === 'existing'
    ? selectedExistingConnection?.base_url || ''
    : newConnectionBaseURL;
  const operationOriginError = canonicalOperationOriginError(operationID, effectiveBaseURL);

  const submit = async (values: Record<string, unknown>) => {
    const upstreamModel = String(values.upstream_model || '').trim();
    const submittedOperationID = String(values.operation_id || '') as CanonicalOperationID;
    const publicContextLength = positiveCanonicalTokenLimit(values.public_context_length);
    const publicOutputLimit = positiveCanonicalTokenLimit(values.public_output_limit);
    const deploymentMaxContext = positiveCanonicalTokenLimit(values.deployment_max_context);
    const deploymentDefaultMaxOutput = positiveCanonicalTokenLimit(values.deployment_default_max_output);
    const selectedExistingConnection = connectionMode === 'existing'
      ? connections.find(item => item.id === Number(values.connection_id))
      : undefined;
    const selectedExistingPublicModel = publicModelMode === 'existing'
      ? publicModels.find(item => item.id === Number(values.public_model_id))
      : undefined;
    if (!deploymentMaxContext || !deploymentDefaultMaxOutput) {
      Toast.error('请填写有效的 Deployment 执行边界；每项必须是正整数 Token');
      return;
    }
    if (publicModelMode === 'new' && (!publicContextLength || !publicOutputLimit)) {
      Toast.error('请填写有效的公共模型执行边界；每项必须是正整数 Token');
      return;
    }
    if (publicModelMode === 'existing'
      && (!selectedExistingPublicModel || !canonicalPublicModelExecutionLimitsReady(selectedExistingPublicModel))) {
      Toast.error('所选公共模型缺少正整数 context/output 执行边界；请先在公共模型目录修复后再复用');
      return;
    }
    const submittedOriginError = canonicalOperationOriginError(
      submittedOperationID,
      connectionMode === 'existing' ? selectedExistingConnection?.base_url : values.base_url,
    );
    if (submittedOriginError) {
      Toast.error(`无法创建 Deployment：${submittedOriginError}`);
      return;
    }
    const connection = connectionMode === 'existing'
      ? { id: Number(values.connection_id) }
      : {
        name: String(values.connection_name || '').trim(),
        provider: String(values.provider || '').trim(),
        base_url: String(values.base_url || '').trim(),
        adapter_type: adapterType,
        adapter_config: adapterType === 'anthropic_native'
          ? { anthropic_version: '2023-06-01' }
          : adapterType === 'azure_openai'
            ? { api_version: String(values.azure_api_version || '').trim(), deployment: upstreamModel }
            : {},
      };
    const publicModel = publicModelMode === 'existing'
      ? { id: Number(values.public_model_id) }
      : {
        model_name: String(values.model_name || '').trim(),
        display_name: String(values.display_name || values.model_name || '').trim(),
        provider_family: String(values.provider_family || selectedExistingConnection?.provider || values.provider || '').trim(),
        lifecycle: String(values.lifecycle || 'staging'),
        context_length: publicContextLength,
        output_limit: publicOutputLimit,
      };
    const credential = credentialMode === 'existing'
      ? { binding_id: Number(values.binding_id) }
      : { api_key: String(values.api_key || '').trim() };
    const input: CanonicalModelApplicationCreateRequest = {
      connection,
      public_model: publicModel,
      deployment: {
        upstream_model: upstreamModel,
        priority: values.route_role === 'backup' ? 200 : 100,
        routing_weight: 100,
        max_context: deploymentMaxContext,
        default_max_output: deploymentDefaultMaxOutput,
      },
      credential,
      operation: { operation_id: submittedOperationID },
    };
    const fingerprint = JSON.stringify(input);
    if (!createIntent.current || createIntent.current.fingerprint !== fingerprint) {
      createIntent.current = { fingerprint, idempotencyKey: newIdempotencyKey() };
    }
    setSaving(true);
    try {
      const result = await modelDeploymentApi.create(input, createIntent.current.idempotencyKey);
      createIntent.current = null;
      Toast.success(result.idempotency_replay
        ? '已安全重放原创建结果；没有重复创建连接、deployment 或凭证'
        : 'Canonical deployment 与精确 operation 已创建；尚未发布到运行时');
      await onCreated(result);
    } catch (error) {
      if (error instanceof ApiError && error.status >= 400 && error.status < 500) {
        createIntent.current = null;
      }
      Toast.error(endpointError(error, 'Canonical deployment 创建失败'));
    } finally {
      setSaving(false);
    }
  };

  const cancel = () => {
    createIntent.current = null;
    onCancel();
  };

  return <SideSheet title="新增 Canonical Deployment" visible={open} onCancel={cancel} width={640}>
    <Banner
      type="info"
      closeIcon={null}
      description="一次提交原子创建或精确复用连接、公共模型、deployment、凭证绑定与一个 operation。创建不等于探测通过，也不等于已发布或对某个调用者可用。"
      style={{ marginBottom: 16 }}
    />
    <Form getFormApi={setFormApi} onSubmit={submit}>
      <Form.RadioGroup
        field="connection_mode"
        label="供应商连接"
        type="button"
        options={[{ label: '新建连接', value: 'new' }, { label: '复用现有连接', value: 'existing' }]}
        onChange={(event) => {
          const mode = String(event?.target?.value || 'new') as 'new' | 'existing';
          setConnectionMode(mode);
          setCredentialMode('new');
          setSelectedConnectionID(0);
          formApi?.setValue('credential_mode', 'new');
          formApi?.setValue('binding_id', undefined);
          formApi?.setValue('connection_id', undefined);
          if (mode === 'new') {
            const preset = providerPreset(selectedPreset);
            setAdapterType(preset.adapterType);
            const firstOperation = operationOptionsByAdapter[preset.adapterType][0].value;
            setOperationID(firstOperation);
            setNewConnectionBaseURL(preset.baseURL);
            formApi?.setValue('base_url', preset.baseURL);
            formApi?.setValue('operation_id', firstOperation);
          } else {
            setAdapterType('');
            setOperationID('' as CanonicalOperationID);
            formApi?.setValue('operation_id', undefined);
          }
        }}
      />
      {connectionMode === 'existing' ? <Form.Select
        field="connection_id"
        label={<span id="canonical-connection-label">现有连接</span>}
        aria-labelledby="canonical-connection-label"
        filter
        optionList={connections.map(connection => ({
          value: connection.id,
          label: `${connection.name} · ${connection.provider} · #${connection.id}`,
          disabled: connection.admin_state !== 'enabled',
        }))}
        rules={[{ required: true, message: '请选择连接' }]}
        onChange={(value) => {
          const id = Number(value);
          const selected = connections.find(connection => connection.id === id);
          setSelectedConnectionID(id);
          setCredentialMode('new');
          setAdapterType(selected?.adapter_type || '');
          setNewConnectionBaseURL('');
          formApi?.setValue('credential_mode', 'new');
          formApi?.setValue('binding_id', undefined);
          const firstOperation = operationOptionsByAdapter[selected?.adapter_type || '']?.[0]?.value;
          setOperationID((firstOperation || '') as CanonicalOperationID);
          formApi?.setValue('operation_id', firstOperation || '');
        }}
        extraText="已暂停或 retired 的连接不能通过创建流程重新启用。"
      /> : <>
        <Form.Select
          field="preset"
          label={<span id="canonical-preset-label">接入模板</span>}
          aria-labelledby="canonical-preset-label"
          optionList={providerPresets.map(preset => ({ label: preset.label, value: preset.value }))}
          rules={[{ required: true, message: '请选择模板' }]}
          onChange={(value) => {
            const preset = providerPreset(value);
            setSelectedPreset(preset.value);
            setAdapterType(preset.adapterType);
            const firstOperation = operationOptionsByAdapter[preset.adapterType][0].value;
            const nextBaseURL = canonicalOperationBaseURLForPreset(preset.value, firstOperation);
            setOperationID(firstOperation);
            setNewConnectionBaseURL(nextBaseURL);
            formApi?.setValue('provider', preset.value === 'custom' ? '' : preset.provider);
            formApi?.setValue('base_url', nextBaseURL);
            formApi?.setValue('adapter_type', preset.adapterType);
            formApi?.setValue('operation_id', firstOperation);
          }}
          extraText={providerPreset(selectedPreset).note}
        />
        <div className="ur-model-endpoint-form-grid">
          <Form.Input field="connection_name" label="连接名称" rules={[{ required: true, message: '必填' }]} />
          <Form.Input field="provider" label="供应商标识" disabled={selectedPreset !== 'custom'} rules={[{ required: true, message: '必填' }]} />
        </div>
        <Form.Input
          field="base_url"
          label="Base URL"
          placeholder="https://..."
          onChange={value => setNewConnectionBaseURL(String(value || ''))}
          rules={[
            { required: true, message: '必填' },
            {
              validator: (_rule, value) => !canonicalOperationOriginError(operationID, value),
              message: canonicalOperationOriginError(operationID, newConnectionBaseURL) || '该 operation 的 Base URL 不合法',
            },
          ]}
        />
        <Text type="tertiary">协议适配器由模板确定：<Text code>{adapterType || '-'}</Text>。服务端仍会验证 adapter 与 operation 的组合。</Text>
        {adapterType === 'azure_openai' && <Form.Input field="azure_api_version" label="Azure API Version" placeholder="2024-10-21" rules={[{ required: true, message: '必填' }]} />}
      </>}

      <Form.RadioGroup
        field="public_model_mode"
        label="公共模型"
        type="button"
        options={[{ label: '新建公共模型', value: 'new' }, { label: '复用现有公共模型', value: 'existing' }]}
        onChange={(event) => setPublicModelMode(String(event?.target?.value || 'new') as 'new' | 'existing')}
      />
      {publicModelMode === 'existing' ? <Form.Select
        field="public_model_id"
        label={<span id="canonical-public-model-label">现有公共模型</span>}
        aria-labelledby="canonical-public-model-label"
        filter
        optionList={publicModels.map(model => {
          const executionLimitsReady = canonicalPublicModelExecutionLimitsReady(model);
          return {
            value: model.id,
            label: `${model.display_name || model.model_name} · ${model.model_name} · #${model.id}${executionLimitsReady ? '' : ' · 缺少 context/output 执行边界'}`,
            disabled: !executionLimitsReady,
          };
        })}
        rules={[{ required: true, message: '请选择公共模型' }]}
        extraText="缺少正整数 context_length 或 output_limit 的公共模型不可复用；请先在公共模型目录修复执行边界。"
      /> : <>
        <div className="ur-model-endpoint-form-grid">
          <Form.Input field="model_name" label="公共模型 ID" placeholder="例如 deepseek-v4-flash" rules={[
            { required: true, message: '必填' },
            { validator: (_rule, value) => !value || /^[a-z0-9][a-z0-9._-]{0,99}$/.test(String(value).trim()), message: '只使用小写字母、数字、点、短横线或下划线' },
          ]} />
          <Form.Input field="display_name" label="展示名称（可选）" />
          <Form.Input field="provider_family" label="模型家族（可选）" extraText="留空时服务端使用连接供应商。" />
          <Form.Select field="lifecycle" label="目录生命周期" optionList={[
            { label: 'Staging', value: 'staging' },
            { label: 'GA', value: 'ga' },
            { label: 'Deprecated', value: 'deprecated' },
          ]} rules={[{ required: true, message: '必填' }]} />
        </div>
        <div className="ur-model-endpoint-form-grid">
          <Form.InputNumber
            field="public_context_length"
            label="公共模型上下文上限（Token）"
            min={1}
            max={maxCanonicalTokenLimit}
            precision={0}
            style={{ width: '100%' }}
            rules={[
              { required: true, message: '必填' },
              { validator: (_rule, value) => positiveCanonicalTokenLimit(value) > 0, message: '必须是正整数 Token' },
            ]}
            extraText="面向调用者公开的模型级执行边界；不能为空或为 0。"
          />
          <Form.InputNumber
            field="public_output_limit"
            label="公共模型单次输出上限（Token）"
            min={1}
            max={maxCanonicalTokenLimit}
            precision={0}
            style={{ width: '100%' }}
            rules={[
              { required: true, message: '必填' },
              { validator: (_rule, value) => positiveCanonicalTokenLimit(value) > 0, message: '必须是正整数 Token' },
            ]}
            extraText="面向调用者公开的单次输出边界；不能为空或为 0。"
          />
        </div>
      </>}

      <Form.Input field="upstream_model" label="上游模型 / 部署 ID" rules={[
        { required: true, message: '必填' },
        { validator: (_rule, value) => !value || (!/\s/u.test(String(value)) && String(value).trim().length <= 200), message: '必须是无空格的精确供应商 ID' },
      ]} />
      <div className="ur-model-endpoint-form-grid">
        <Form.InputNumber
          field="deployment_max_context"
          label="Deployment 上下文上限（Token）"
          min={1}
          max={maxCanonicalTokenLimit}
          precision={0}
          style={{ width: '100%' }}
          rules={[
            { required: true, message: '必填' },
            { validator: (_rule, value) => positiveCanonicalTokenLimit(value) > 0, message: '必须是正整数 Token' },
          ]}
          extraText="该精确供应商 Deployment 已核实的上下文执行边界。"
        />
        <Form.InputNumber
          field="deployment_default_max_output"
          label="Deployment 单次输出上限（Token）"
          min={1}
          max={maxCanonicalTokenLimit}
          precision={0}
          style={{ width: '100%' }}
          rules={[
            { required: true, message: '必填' },
            { validator: (_rule, value) => positiveCanonicalTokenLimit(value) > 0, message: '必须是正整数 Token' },
          ]}
          extraText="该精确供应商 Deployment 已核实的默认输出执行边界。"
        />
      </div>

      {connectionMode === 'existing' && existingBindings.length > 0 && <Form.RadioGroup
        field="credential_mode"
        label="凭证绑定"
        type="button"
        options={[{ label: '新增凭证', value: 'new' }, { label: '复用已有绑定', value: 'existing' }]}
        onChange={(event) => setCredentialMode(String(event?.target?.value || 'new') as 'new' | 'existing')}
      />}
      {credentialMode === 'existing' && connectionMode === 'existing' ? <Form.Select
        field="binding_id"
        label={<span id="canonical-binding-label">已有凭证绑定</span>}
        aria-labelledby="canonical-binding-label"
        optionList={existingBindings.map(binding => ({
          value: binding.id,
          label: `Binding #${binding.id} · ${binding.status} · revision ${binding.state_revision}`,
          disabled: !binding.eligible,
        }))}
        rules={[{ required: true, message: '请选择凭证绑定' }]}
      /> : <PasswordField field="api_key" label="供应商 API Key" rules={[{ required: true, message: '必填' }]} autoComplete="new-password" visibilityResetKey={open ? 'canonical-create-open' : 'closed'} />}

      <Form.Select
        field="operation_id"
        label={<span id="canonical-operation-label">Operation</span>}
        aria-labelledby="canonical-operation-label"
        optionList={adapterOperations}
        rules={[{ required: true, message: '请选择与连接协议一致的 operation' }]}
        onChange={(value) => {
          const nextOperation = String(value) as CanonicalOperationID;
          setOperationID(nextOperation);
          if (connectionMode !== 'new') return;
          const preset = providerPreset(selectedPreset);
          const currentBaseURL = String(formApi?.getValue('base_url') || '');
          const knownPresetURLs = new Set([
            preset.baseURL,
            ...Object.values(presetOperationBaseURLs[preset.value] || {}),
          ]);
          if (knownPresetURLs.has(currentBaseURL)) {
            const nextBaseURL = canonicalOperationBaseURLForPreset(preset.value, nextOperation);
            setNewConnectionBaseURL(nextBaseURL);
            formApi?.setValue('base_url', nextBaseURL);
          }
        }}
        extraText="operation_id 是持久身份；公开路径仅是该身份的传输映射。"
      />
      {exactHTTPSOriginOperations.has(operationID) && <Text type={operationOriginError ? 'danger' : 'success'}>
        {operationOriginError || `已使用 path-free HTTPS origin：${effectiveBaseURL}`}
      </Text>}
      <Form.Select field="route_role" label="候选优先级" optionList={[
        { label: '主候选（priority 100）', value: 'primary' },
        { label: '备用候选（priority 200）', value: 'backup' },
      ]} rules={[{ required: true, message: '必填' }]} extraText="只写 deployment 配置，不会创建或推进运行时发布。" />

      <div className="ur-form-actions">
        <Button onClick={cancel}>取消</Button>
        <Button
          type="primary"
          loading={saving}
          disabled={Boolean(operationOriginError)}
          title={operationOriginError || undefined}
          onClick={() => formApi?.submitForm()}
        >创建 Deployment</Button>
      </div>
    </Form>
  </SideSheet>;
}
