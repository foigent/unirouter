import { useEffect, useState } from 'react';
import { Banner, Button, Form, SideSheet, Toast, Typography } from '@douyinfe/semi-ui';
import { channelApi } from '../../api/channels';
import { ApiError } from '../../api/transport';
import type { ModelEndpointModel } from '../../api/modelEndpoints';
import { PasswordField } from '../../ui';
import { providerPreset, providerPresets } from '../../utils/providerEndpoints';
import { endpointError } from './presentation';

const { Text } = Typography;

type Props = {
  open: boolean;
  lockedPublicModel: string;
  models: ModelEndpointModel[];
  onCancel: () => void;
  onCreated: (publicModel: string) => Promise<void> | void;
};

export default function ModelEndpointCreateSheet({ open, lockedPublicModel, models, onCancel, onCreated }: Props) {
  const [formApi, setFormApi] = useState<any>(null);
  const [selectedPreset, setSelectedPreset] = useState('deepseek');
  const [publicModelMode, setPublicModelMode] = useState<'existing' | 'new'>('new');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open || !formApi) return;
    formApi.reset();
    formApi.setValues({
      preset: 'deepseek',
      base_url: providerPreset('deepseek').baseURL,
      adapter_type: providerPreset('deepseek').adapterType,
      public_model: lockedPublicModel,
      upstream_model: lockedPublicModel,
      route_role: 'primary',
    });
    setSelectedPreset('deepseek');
    setPublicModelMode(lockedPublicModel ? 'existing' : 'new');
  }, [formApi, lockedPublicModel, open]);

  const submit = async (values: Record<string, any>) => {
    const preset = providerPreset(values.preset);
    const publicModel = String(values.public_model || '').trim();
    const upstreamModel = String(values.upstream_model || publicModel).trim();
    const provider = preset.value === 'custom' ? String(values.provider || 'custom').trim() : preset.provider;
    const baseURL = String(values.base_url || preset.baseURL).trim();
    const adapterType = preset.value === 'custom'
      ? String(values.adapter_type || 'openai_compatible') as 'openai_compatible' | 'anthropic_native'
      : preset.adapterType;
    const adapterConfig = adapterType === 'anthropic_native'
      ? { anthropic_version: '2023-06-01' }
      : adapterType === 'azure_openai'
        ? { api_version: String(values.azure_api_version || '').trim(), deployment: upstreamModel }
        : {};
    setSaving(true);
    try {
      await channelApi.create({
        name: String(values.connection_name || `${provider} · ${upstreamModel}`).trim(),
        provider,
        base_url: baseURL,
        models: publicModel,
        // Always persist the identity mapping explicitly, even when both
        // strings currently match. An empty map used to make a display label
        // silently become the provider API model ID.
        model_mapping: { [publicModel]: upstreamModel },
        adapter_type: adapterType,
        adapter_config: adapterConfig,
        priority: values.route_role === 'backup' ? 200 : 100,
        routing_weight: 100,
        degradation_threshold: 10,
        api_key: String(values.api_key || '').trim(),
      });
      Toast.success('供应商端点与加密凭证已创建；下一步只需真实检测');
      await onCreated(publicModel);
    } catch (error) {
      if (error instanceof ApiError) {
        const payload = error.payload as { code?: string; canonical_model?: string } | null;
        if (payload?.code === 'model_identity_conflict' && payload.canonical_model) {
          formApi?.setValue('public_model', payload.canonical_model);
          formApi?.setValue('upstream_model', upstreamModel);
        }
      }
      Toast.error(endpointError(error, '创建供应商端点失败'));
    } finally {
      setSaving(false);
    }
  };

  return <SideSheet title="新增供应商端点" visible={open} onCancel={onCancel} width={600}>
    <Banner type="info" description="公共模型 ID 是应用调用 UniRouter 时使用的名字；上游模型 ID 是这个供应商实际接受的模型或部署名。相同公共模型可以绑定多个供应商端点。" closeIcon={null} style={{ marginBottom: 16 }} />
    <Form getFormApi={setFormApi} onSubmit={submit} initValues={{ preset: 'deepseek', base_url: providerPreset('deepseek').baseURL, public_model: lockedPublicModel, upstream_model: lockedPublicModel, route_role: 'primary' }}>
      <Form.Select
        field="preset"
        label={<span id="endpoint-preset-label">接入模板</span>}
        aria-labelledby="endpoint-preset-label"
        optionList={providerPresets.map(preset => ({ label: preset.label, value: preset.value }))}
        rules={[{ required: true, message: '请选择接入模板' }]}
        onChange={(value) => {
          const preset = providerPresets.find(item => item.value === value);
          setSelectedPreset(String(value));
          if (preset) {
            formApi?.setValue('base_url', preset.baseURL);
            formApi?.setValue('adapter_type', preset.adapterType);
          }
        }}
        extraText={providerPresets.find(item => item.value === selectedPreset)?.note || '选择模板后仍以你的供应商控制台配置为准。'}
      />
      {selectedPreset !== 'custom' && <Button theme="borderless" onClick={() => {
        setSelectedPreset('custom');
        formApi?.setValue('preset', 'custom');
        formApi?.setValue('base_url', '');
        formApi?.setValue('adapter_type', 'openai_compatible');
      }}>列表中没有？新增自定义兼容供应商</Button>}
      {selectedPreset === 'custom' && <div className="ur-model-endpoint-form-grid">
        <Form.Input field="provider" label="供应商标识" placeholder="例如 minimax、mimo、volcengine" rules={[{ required: true, message: '必填' }]} extraText="这是审计和运营使用的稳定名称，不限制必须属于内置供应商。" />
        <Form.Select field="adapter_type" label={<span id="endpoint-adapter-label">兼容协议</span>} aria-labelledby="endpoint-adapter-label" optionList={[
          { label: 'OpenAI Chat Completions 兼容', value: 'openai_compatible' },
          { label: 'Anthropic Messages 兼容', value: 'anthropic_native' },
        ]} rules={[{ required: true, message: '必填' }]} extraText="选择供应商明确声明兼容的协议；UniRouter 不会仅凭 URL 猜测。" />
      </div>}
      <Form.Input field="connection_name" label="连接名称（可选）" placeholder="例如 DeepSeek 官方 · 华北生产" />
      <Form.Input field="base_url" label="Base URL" placeholder="https://..." rules={[{ required: true, message: '必填' }]} />
      {!lockedPublicModel && <Form.RadioGroup
        field="public_model_mode"
        label={<span id="endpoint-public-model-mode-label">公共模型</span>}
        aria-labelledby="endpoint-public-model-mode-label"
        initValue="new"
        type="button"
        buttonSize="middle"
        options={[
          { label: '创建新的公共模型 ID', value: 'new' },
          { label: '连接到已有公共模型', value: 'existing' },
        ]}
        onChange={(event) => {
          const mode = String(event?.target?.value || 'new') as 'existing' | 'new';
          setPublicModelMode(mode);
          formApi?.setValue('public_model', '');
        }}
        extraText="同一个模型来自官方、OpenRouter、百炼、Azure 等供应商时，选择已有公共模型；只有应用需要一个全新的对外调用 ID 时才创建。"
      />}
      <div className="ur-model-endpoint-form-grid">
        {lockedPublicModel ? <Form.Input
          field="public_model"
          label="公共模型 ID"
          disabled
          rules={[{ required: true, message: '必填' }]}
          extraText="正在为当前公共模型增加另一个供应商端点，公共 ID 不可改变。"
        /> : publicModelMode === 'existing' ? <Form.Select
          field="public_model"
          label={<span id="endpoint-existing-model-label">已有公共模型 ID</span>}
          aria-labelledby="endpoint-existing-model-label"
          placeholder="选择一个已有公共模型"
          filter
          optionList={models.filter(model => model.lifecycle !== 'retired').map(model => ({
            value: model.public_model,
            label: model.display_name && model.display_name !== model.public_model ? `${model.display_name} · ${model.public_model}` : model.public_model,
          }))}
          rules={[{ required: true, message: '请选择已有公共模型' }]}
          onChange={(value) => {
            if (!String(formApi?.getValue('upstream_model') || '').trim()) formApi?.setValue('upstream_model', value);
          }}
        /> : <Form.Input
          field="public_model"
          label="新的公共模型 ID"
          placeholder="例如 deepseek-v4-flash"
          rules={[
            { required: true, message: '请输入新的公共模型 ID' },
            { validator: (_rule, value) => !value || /^[a-z0-9][a-z0-9._-]{0,159}$/.test(String(value).trim()), message: '请使用小写字母、数字、点、短横线或下划线；显示名称稍后在模型目录维护' },
          ]}
          extraText="这是应用请求 UniRouter 时使用的稳定 ID，不是展示标题。只使用小写字母、数字、点、短横线或下划线；供应商展示名与上游 ID 分开维护。"
        />}
        <Form.Input field="upstream_model" label="上游模型 / 部署 ID" placeholder="供应商实际接受的精确 ID，例如 kimi-k2.7-code" rules={[
          { required: true, message: '必填' },
          { validator: (_rule, value) => !value || (!/\s/u.test(String(value)) && String(value).trim().length <= 200), message: '请输入供应商返回的精确 ID，不能包含空格或换行' },
        ]} extraText="必须来自供应商 API 文档或实时模型列表，不能填写 Kimi K2.7 Code 这类展示名称。保存后可在端点详情读取供应商实时列表核对。" />
      </div>
      {selectedPreset === 'azure' && <div className="ur-model-endpoint-form-grid">
        <Text type="tertiary">上面的“上游模型 / 部署 ID”就是 Azure 部署名，无需重复填写。</Text>
        <Form.Input field="azure_api_version" label="API Version" placeholder="2024-10-21" rules={[{ required: true, message: '必填' }]} />
      </div>}
      <PasswordField field="api_key" label="供应商 API Key" rules={[{ required: true, message: '必填' }]} autoComplete="new-password" visibilityResetKey={open ? 'create-open' : 'closed'} />
      <Form.Select field="route_role" label={<span id="endpoint-route-role-label">路由角色</span>} aria-labelledby="endpoint-route-role-label" optionList={[
        { label: '主端点（优先使用）', value: 'primary' },
        { label: '备用端点（主端点不可用时使用）', value: 'backup' },
      ]} rules={[{ required: true, message: '请选择路由角色' }]} extraText="稍后可在高级发布中调整精确优先级和同级权重。" />
      <div className="ur-form-actions">
        <Button onClick={onCancel}>取消</Button>
        <Button type="primary" loading={saving} onClick={() => formApi?.submitForm()}>保存端点与凭证</Button>
      </div>
    </Form>
  </SideSheet>;
}
