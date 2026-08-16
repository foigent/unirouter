import { Banner, Button, Form, SideSheet } from '@douyinfe/semi-ui';
import { adapterTypeForProvider, providerIdentityOptions } from '../utils/providerEndpoints';
import { channelStatusOptions, type Channel } from '../utils/channelAdmin';

export default function ChannelEditorSheet({
  visible,
  editing,
  editingIsActive,
  formApi,
  setFormApi,
  onSubmit,
  onCancel,
}: {
  visible: boolean;
  editing: Channel | null;
  editingIsActive: boolean;
  formApi: any;
  setFormApi: (api: any) => void;
  onSubmit: (values: Record<string, any>) => void | Promise<void>;
  onCancel: () => void;
}) {
  return <SideSheet
    title={editing ? '编辑通道' : '新增通道'}
    visible={visible}
    onCancel={onCancel}
    width={480}
  >
    {editingIsActive && <Banner
      type="warning"
      description="该连接当前由运行时发布生效。为避免绕过发布门禁，本表单只执行紧急禁用；连接参数不会随本次操作修改。禁用后可前往“供应商凭证”新增或轮换 Key。"
      closeIcon={null}
      style={{ marginBottom: 16 }}
    />}
    <Form getFormApi={setFormApi} onSubmit={onSubmit}>
      <Form.Input field="name" label="通道名称" disabled={editingIsActive} rules={[{ required: true, message: '必填' }]} />
      <Form.Select
        field="provider"
        label="供应商身份"
        rules={[{ required: true, message: '必填' }]}
        optionList={providerIdentityOptions}
        filter
        allowCreate
        placeholder="选择或输入供应商标识"
        disabled={editingIsActive}
        onChange={(value) => formApi?.setValue('adapter_type', adapterTypeForProvider(value))}
        extraText="列表只是快捷项；MiniMax、MiMo 或其他供应商可直接输入稳定标识。协议能力仍由下方请求协议决定。"
      />
      <Form.Input field="base_url" label="服务地址（Base URL）" disabled={editingIsActive} rules={[{ required: true, message: '必填' }]} />
      <Form.Input
        field="models"
        label="支持模型"
        placeholder="如 gpt-4o；gpt-4o-mini"
        extraText="可用中英文逗号、分号或换行分隔；保存时会去空格、去重并统一为英文逗号。"
        disabled={editingIsActive}
        rules={[{ required: true, message: '必填' }]}
      />
      {!editing && <Form.Input
        field="api_key"
        label="供应商 API Key"
        mode="password"
        extraText="与连接一起加密保存，提交后不回显。"
        rules={[{ required: true, message: '请输入供应商 API Key' }]}
      />}
      <Form.Select
        field="adapter_type"
        label="请求协议（Adapter Type）"
        initValue="openai_compatible"
        optionList={[
          { label: 'OpenAI Compatible', value: 'openai_compatible' },
          { label: 'Anthropic Native', value: 'anthropic_native' },
          { label: 'Azure OpenAI (deployment-scoped)', value: 'azure_openai' },
        ]}
        disabled={editingIsActive}
        extraText="Adapter 只决定 UniRouter 如何组装和解析请求；不会因为选择 OpenAI-compatible 协议而把 DeepSeek、Kimi 或自定义服务改名为 OpenAI。离线协议夹具不等于真实供应商连通验证。"
      />
      <Form.TextArea
        field="adapter_config"
        label="Adapter Config (JSON)"
        placeholder='Anthropic: {"anthropic_version":"2023-06-01"} · Azure: {"api_version":"2024-10-21","deployment":"gpt-4o-prod"}'
        rows={4}
        disabled={editingIsActive}
      />
      <Form.InputNumber field="priority" label="优先级 (越小越优先)" initValue={100} disabled={editingIsActive} />
      <Form.InputNumber field="routing_weight" label="同优先级权重 (1-1000)" initValue={100} disabled={editingIsActive} extraText="只在优先级相同时分配流量；例如 100 与 200 约为 1:2。" />
      <Form.InputNumber field="degradation_threshold" label="熔断阈值 (连续失败次数)" initValue={10} disabled={editingIsActive} extraText="达到次数后暂停该通道，恢复窗口到期后只允许一个探针请求。" />
      {editing && <Form.Select
        field="status"
        label="状态"
        optionList={editingIsActive ? [{ label: '紧急禁用（disabled）', value: 'disabled' }] : channelStatusOptions}
        extraText={editingIsActive
          ? '仅停止当前连接；不会删除历史发布记录，禁用状态不会被旧发布重新启用。'
          : 'staged/disabled 不能直接改回 active；请通过运行时发布提升。'}
      />}
      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 24 }}>
        <Button onClick={onCancel}>取消</Button>
        <Button type="primary" onClick={() => formApi?.submitForm()}>{editingIsActive ? '确认紧急禁用' : editing ? '更新' : '保存并进入检测'}</Button>
      </div>
    </Form>
  </SideSheet>;
}
