import { Banner, Button, Form, SideSheet, Space, TextArea, Typography } from '@douyinfe/semi-ui';
import { ExactTokenCountInput } from '../ui';
import { reviewedModelOnboardingLimits } from '../utils/modelOnboarding';
import {
  approvedOptions,
  executionProfilesAvailableForBinding,
  lifecycleOptions,
  statusOptions,
  type ModelAlias,
  type ModelExecutionProfile,
  type ModelRegistryItem,
  type ModelSet,
  modelExecutionProfileKey,
  toList,
} from '../utils/modelGovernance';

const { Text } = Typography;

interface ModelGovernanceSheetsProps {
  registrySheet: { mode: 'create' | 'edit'; record?: ModelRegistryItem } | null;
  registryFormApi: any;
  setRegistryFormApi: (api: any) => void;
  onRegistrySubmit: (values: Record<string, unknown>) => void | Promise<void>;
  closeRegistrySheet: () => void;
  executionProfiles: ModelExecutionProfile[];
  executionProfilesLoading: boolean;
  executionProfilesError: string;
  selectedExecutionProfileKey: string;
  onExecutionProfileChange: (key: string) => void;
  onRegistryModelNameChange: (modelName: string) => void;
  aliasSheet: { mode: 'create' | 'edit'; record?: ModelAlias } | null;
  aliasFormApi: any;
  setAliasFormApi: (api: any) => void;
  onAliasSubmit: (values: Record<string, unknown>) => void | Promise<void>;
  closeAliasSheet: () => void;
  modelSetSheet: boolean;
  modelSetFormApi: any;
  setModelSetFormApi: (api: any) => void;
  onModelSetSubmit: (values: Record<string, unknown>) => void | Promise<void>;
  closeModelSetSheet: () => void;
  itemsSheet: ModelSet | null;
  itemsText: string;
  setItemsText: (value: string) => void;
  itemsLoading: boolean;
  onItemsSubmit: () => void | Promise<void>;
  closeItemsSheet: () => void;
}

export default function ModelGovernanceSheets(props: ModelGovernanceSheetsProps) {
  const registryRecord = props.registrySheet?.record;
  const retainedProfileKey = props.registrySheet?.mode === 'edit' && registryRecord?.execution_profile_id && registryRecord.execution_profile_version
    ? `${registryRecord.execution_profile_id}@${registryRecord.execution_profile_version}`
    : '';
  const selectableProfiles = executionProfilesAvailableForBinding(props.executionProfiles, retainedProfileKey);
  const selectedProfile = selectableProfiles.find(profile => modelExecutionProfileKey(profile) === props.selectedExecutionProfileKey);
  const registryInitialValues = registryRecord ? {
    model_name: registryRecord.model_name,
    display_name: registryRecord.display_name || registryRecord.model_name,
    provider_family: registryRecord.provider_family || '',
    lifecycle: registryRecord.lifecycle || 'staging',
    context_length: registryRecord.context_length || 0,
    output_limit: registryRecord.output_limit || 0,
    capability_tags: toList(registryRecord.capability_tags).join(','),
    data_residency: registryRecord.data_residency || '',
    pricing_model_name: registryRecord.pricing_model_name || registryRecord.model_name,
    approved_catalog: String(Boolean(registryRecord.approved_catalog)),
    migration_recommendation: registryRecord.migration_recommendation || '',
    execution_profile_key: registryRecord.execution_profile_id && registryRecord.execution_profile_version
      ? `${registryRecord.execution_profile_id}@${registryRecord.execution_profile_version}`
      : 'unverified.chat@1.0.0',
  } : {
    lifecycle: 'staging',
    context_length: 0,
    output_limit: 0,
    approved_catalog: 'false',
    execution_profile_key: 'auto',
  };
  const profileOptions = [
    { label: '自动匹配已验证 Profile', value: 'auto' },
    ...selectableProfiles.map(profile => ({
      label: `${profile.label} · ${profile.id}@${profile.version}${profile.current ? '' : ' · 历史绑定（仅保留）'}`,
      value: modelExecutionProfileKey(profile),
    })),
  ];
  return (
    <>
      <SideSheet title={props.registrySheet?.mode === 'edit' ? '编辑模型' : '新增模型'} visible={!!props.registrySheet} onCancel={props.closeRegistrySheet} width={560}>
        {props.registrySheet && <>
        <Banner
          type="warning"
          closeIcon={null}
          description="已验证模型可以绑定版本冻结的执行 Profile；自动匹配只接受 Profile 中唯一的精确别名。多个 API surface 共用别名时必须明确选择 Profile。未命中的模型使用字节级保守估算，不会按名称猜测 tokenizer、推理、缓存或多模态能力。"
          style={{ marginBottom: 16 }}
        />
        <Form
          key={`registry-${props.registrySheet?.mode || 'closed'}-${registryRecord?.id || 'new'}`}
          getFormApi={props.setRegistryFormApi}
          initValues={registryInitialValues}
          onSubmit={props.onRegistrySubmit}
        >
          <Form.Input field="model_name" label="模型名称" placeholder="gpt-4o" onChange={(value) => props.onRegistryModelNameChange(String(value || ''))} rules={[{ required: true, message: '请输入模型名称' }]} />
          <Form.Input field="display_name" label="显示名称" placeholder="Company Chat" />
          <Form.Input field="provider_family" label="供应商族" placeholder="openai / anthropic / gemini" />
          <Form.Select field="lifecycle" label={<span id="model-lifecycle-label">模型生命周期</span>} aria-labelledby="model-lifecycle-label" optionList={lifecycleOptions} rules={[{ required: true, message: '请选择模型生命周期' }]} />
          <Form.Select
            field="execution_profile_key"
            label={<span id="model-execution-profile-label">版本化执行 Profile</span>}
            aria-labelledby="model-execution-profile-label"
            optionList={profileOptions}
            loading={props.executionProfilesLoading}
            disabled={props.executionProfilesLoading || !!props.executionProfilesError}
            onChange={(value) => props.onExecutionProfileChange(String(value || 'auto'))}
            rules={[{ required: true, message: '请选择执行 Profile' }]}
            extraText="Profile 冻结 tokenizer、推理、缓存字段与协议合同；目录中的上下文/输出值可以向下调整，但不能超过供应商能力或 UniRouter 已审查可执行策略上限。"
          />
          {props.executionProfilesError && <Banner type="danger" closeIcon={null} description={props.executionProfilesError} style={{ marginBottom: 12 }} />}
          {selectedProfile && <div className="ur-modelgov-profile-facts">
            <Text strong>{selectedProfile.label}</Text>
            <Text type="tertiary" size="small">Tokenizer：{selectedProfile.tokenizer_profile_id}@{selectedProfile.tokenizer_profile_version}</Text>
            <Text type="tertiary" size="small">推理：{selectedProfile.reasoning_profile} · 缓存：{selectedProfile.cache_usage_profile}</Text>
            <Text type="tertiary" size="small">协议：{selectedProfile.protocol_profile}{selectedProfile.protocol_profile_version ? `@${selectedProfile.protocol_profile_version}` : ''}</Text>
            <Text type="tertiary" size="small">Catalog 证据：{selectedProfile.catalog_evidence_state}（不代表 live verified 或 released）</Text>
            <Text type="tertiary" size="small">供应商上限：context {selectedProfile.provider_context_window || '待验证'} · output {selectedProfile.provider_max_output || '未文档化或无固定上限'}；UniRouter 可执行 output 上限 {selectedProfile.executable_max_output || selectedProfile.provider_max_output || '未单独收窄'}；请求默认 output {selectedProfile.default_output_limit || '未知'}</Text>
            <Text type="tertiary" size="small">UniRouter 已验证：{selectedProfile.capabilities.join('、') || (selectedProfile.catalog_evidence_state === 'locally_conformant' ? '严格文本基础合同' : '尚无本地一致性能力')}</Text>
            <Text type="tertiary" size="small">每次调用必须证明：{selectedProfile.always_required_capabilities.join('、') || '无'}</Text>
            {(selectedProfile.provider_capabilities || []).some(capability => !selectedProfile.capabilities.includes(capability)) && <Text type="warning" size="small">
              供应商另有声明但网关尚未放行：{(selectedProfile.provider_capabilities || []).filter(capability => !selectedProfile.capabilities.includes(capability)).join('、')}。这些能力不会自动写入模型目录。
            </Text>}
          </div>}
          <div className="ur-modelgov-form-grid">
            <ExactTokenCountInput
              field="context_length"
              initialValue={props.registrySheet?.record?.context_length ?? (selectedProfile ? reviewedModelOnboardingLimits(selectedProfile).publicContextLength : 0)}
              resetKey={`${props.registrySheet?.mode || 'closed'}-${props.registrySheet?.record?.id || 'new'}-${props.selectedExecutionProfileKey}`}
              style={{ width: '100%' }}
            />
            <Form.InputNumber field="output_limit" label="公开执行输出上限（Token）" min={0} style={{ width: '100%' }} extraText="这是运行时硬上限，不是请求默认输出。新建时留空或 0 使用所选 Profile 的可执行上限；可显式下调。" />
          </div>
          <Form.Input
            field="capability_tags"
            label="能力标签（声明）"
            placeholder="chat,vision,json"
            extraText="例如 vision 只有在官方能力说明或真实多模态请求验证后才能填写；标签本身不会执行能力检测。"
          />
          <Form.Input field="data_residency" label="数据驻留" placeholder="us / eu / cn" />
          <Form.Input field="pricing_model_name" label="计价模型名" placeholder="默认使用模型名称" />
          <Form.Select field="approved_catalog" label={<span id="model-catalog-visibility-label">开发者目录可见</span>} aria-labelledby="model-catalog-visibility-label" optionList={approvedOptions} />
          <Form.TextArea field="migration_recommendation" label="迁移建议" placeholder="例如：请迁移至 gpt-4.1-mini" rows={3} />
          <div className="ur-form-actions">
            <Button onClick={props.closeRegistrySheet}>取消</Button>
            <Button type="primary" disabled={props.executionProfilesLoading || !!props.executionProfilesError || props.executionProfiles.length === 0} onClick={() => props.registryFormApi?.submitForm()}>保存</Button>
          </div>
        </Form>
        </>}
      </SideSheet>

      <SideSheet title={props.aliasSheet?.mode === 'edit' ? '编辑别名' : '新增别名'} visible={!!props.aliasSheet} onCancel={props.closeAliasSheet} width={520}>
        <Form getFormApi={props.setAliasFormApi} onSubmit={props.onAliasSubmit}>
          <Form.Input field="alias" label="别名" placeholder="company-chat" rules={[{ required: true, message: '请输入别名' }]} />
          <Form.Input field="target_model" label="目标模型" placeholder="gpt-4o" rules={[{ required: true, message: '请输入目标模型' }]} />
          <Form.Input field="display_name" label="显示名称" placeholder="Company Chat" />
          <Form.TextArea field="description" label="描述" placeholder="内部稳定聊天模型" rows={3} />
          <Form.Select field="status" label={<span id="model-alias-status-label">状态</span>} aria-labelledby="model-alias-status-label" optionList={statusOptions} />
          <div className="ur-form-actions">
            <Button onClick={props.closeAliasSheet}>取消</Button>
            <Button type="primary" onClick={() => props.aliasFormApi?.submitForm()}>保存</Button>
          </div>
        </Form>
      </SideSheet>

      <SideSheet title="新增模型集" visible={props.modelSetSheet} onCancel={props.closeModelSetSheet} width={540}>
        <Form getFormApi={props.setModelSetFormApi} onSubmit={props.onModelSetSubmit}>
          <Form.Input field="name" label="名称" placeholder="Core Chat Models" rules={[{ required: true, message: '请输入名称' }]} />
          <Form.Input field="slug" label="标识 Slug" placeholder="core-chat-models" />
          <Form.TextArea field="description" label="描述" placeholder="核心业务应用可用模型集合" rows={2} />
          <Form.Select field="status" label={<span id="model-set-status-label">状态</span>} aria-labelledby="model-set-status-label" optionList={statusOptions} />
          <Form.TextArea field="items" label="条目" placeholder={'alias:company-chat\nmodel:gpt-4o-mini'} rows={7} />
          <div className="ur-form-actions">
            <Button onClick={props.closeModelSetSheet}>取消</Button>
            <Button type="primary" onClick={() => props.modelSetFormApi?.submitForm()}>创建</Button>
          </div>
        </Form>
      </SideSheet>

      <SideSheet title={`模型集条目：${props.itemsSheet?.name || ''}`} visible={!!props.itemsSheet} onCancel={props.closeItemsSheet} width={520}>
        <Space vertical align="start" style={{ width: '100%' }}>
          <Text type="tertiary">每行一条，格式为 model:gpt-4o 或 alias:company-chat。</Text>
          <TextArea value={props.itemsText} onChange={props.setItemsText} rows={12} style={{ width: '100%' }} disabled={props.itemsLoading} />
          <div className="ur-form-actions">
            <Button onClick={props.closeItemsSheet}>取消</Button>
            <Button type="primary" loading={props.itemsLoading} onClick={props.onItemsSubmit}>保存条目</Button>
          </div>
        </Space>
      </SideSheet>
    </>
  );
}
