import { translate as t, translateLabel, localizeOptions, useI18n } from '../i18n';
import '../i18n/models';
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
  useI18n();
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
    { label: t("自动匹配已验证 Profile"), value: 'auto' },
    ...selectableProfiles.map(profile => ({
      label: `${profile.label} · ${profile.id}@${profile.version}${profile.current ? '' : t(" · 历史绑定（仅保留）")}`,
      value: modelExecutionProfileKey(profile),
    })),
  ];
  return (
    <>
      <SideSheet title={props.registrySheet?.mode === 'edit' ? t("编辑模型") : t("新增模型")} visible={!!props.registrySheet} onCancel={props.closeRegistrySheet} width="min(560px, 100vw)">
        {props.registrySheet && <>
        <Banner
          type="warning"
          closeIcon={null}
          description={t("已验证模型可以绑定版本冻结的执行 Profile；自动匹配只接受 Profile 中唯一的精确别名。多个 API surface 共用别名时必须明确选择 Profile。未命中的模型使用字节级保守估算，不会按名称猜测 tokenizer、推理、缓存或多模态能力。")}
          style={{ marginBottom: 16 }}
        />
        <Form
          key={`registry-${props.registrySheet?.mode || 'closed'}-${registryRecord?.id || 'new'}`}
          getFormApi={props.setRegistryFormApi}
          initValues={registryInitialValues}
          onSubmit={props.onRegistrySubmit}
        >
          <Form.Input field="model_name" label={t("模型名称")} placeholder="gpt-4o" onChange={(value) => props.onRegistryModelNameChange(String(value || ''))} rules={[{ required: true, message: t("请输入模型名称") }]} />
          <Form.Input field="display_name" label={t("显示名称")} placeholder="Company Chat" />
          <Form.Input field="provider_family" label={t("供应商族")} placeholder="openai / anthropic / gemini" />
          <Form.Select field="lifecycle" label={<span id="model-lifecycle-label">{t("模型生命周期")}</span>} aria-labelledby="model-lifecycle-label" optionList={localizeOptions(lifecycleOptions)} rules={[{ required: true, message: t("请选择模型生命周期") }]} />
          <Form.Select
            field="execution_profile_key"
            label={<span id="model-execution-profile-label">{t("版本化执行 Profile")}</span>}
            aria-labelledby="model-execution-profile-label"
            optionList={profileOptions}
            loading={props.executionProfilesLoading}
            disabled={props.executionProfilesLoading || !!props.executionProfilesError}
            onChange={(value) => props.onExecutionProfileChange(String(value || 'auto'))}
            rules={[{ required: true, message: t("请选择执行 Profile") }]}
            extraText={t("Profile 冻结 tokenizer、推理、缓存字段与协议合同；目录中的上下文/输出值可以向下调整，但不能超过供应商能力或 UniRouter 已审查可执行策略上限。")}
          />
          {props.executionProfilesError && <Banner type="danger" closeIcon={null} description={translateLabel(props.executionProfilesError)} style={{ marginBottom: 12 }} />}
          {selectedProfile && <div className="ur-modelgov-profile-facts">
            <Text strong>{selectedProfile.label}</Text>
            <Text type="tertiary" size="small">Tokenizer：{selectedProfile.tokenizer_profile_id}@{selectedProfile.tokenizer_profile_version}</Text>
            <Text type="tertiary" size="small">{t("推理：")}{selectedProfile.reasoning_profile}  {t("· 缓存：")}{selectedProfile.cache_usage_profile}</Text>
            <Text type="tertiary" size="small">{t("协议：")}{selectedProfile.protocol_profile}{selectedProfile.protocol_profile_version ? `@${selectedProfile.protocol_profile_version}` : ''}</Text>
            <Text type="tertiary" size="small">{t("Catalog 证据：")}{selectedProfile.catalog_evidence_state}{t("（不代表 live verified 或 released）")}</Text>
            <Text type="tertiary" size="small">{t("供应商上限：context")} {selectedProfile.provider_context_window || t("待验证")} · output {selectedProfile.provider_max_output || t("未文档化或无固定上限")}{t("；UniRouter 可执行 output 上限")} {selectedProfile.executable_max_output || selectedProfile.provider_max_output || t("未单独收窄")}{t("；请求默认 output")} {selectedProfile.default_output_limit || t("未知")}</Text>
            <Text type="tertiary" size="small">{t("UniRouter 已验证：")}{selectedProfile.capabilities.join('、') || (selectedProfile.catalog_evidence_state === 'locally_conformant' ? t("严格文本基础合同") : t("尚无本地一致性能力"))}</Text>
            <Text type="tertiary" size="small">{t("每次调用必须证明：")}{selectedProfile.always_required_capabilities.join('、') || t("无")}</Text>
            {(selectedProfile.provider_capabilities || []).some(capability => !selectedProfile.capabilities.includes(capability)) && <Text type="warning" size="small">

              {t("供应商另有声明但网关尚未放行：")}{(selectedProfile.provider_capabilities || []).filter(capability => !selectedProfile.capabilities.includes(capability)).join('、')}{t("。这些能力不会自动写入模型目录。")}
            </Text>}
          </div>}
          <div className="ur-modelgov-form-grid">
            <ExactTokenCountInput
              field="context_length"
              initialValue={props.registrySheet?.record?.context_length ?? (selectedProfile ? reviewedModelOnboardingLimits(selectedProfile).publicContextLength : 0)}
              resetKey={`${props.registrySheet?.mode || 'closed'}-${props.registrySheet?.record?.id || 'new'}-${props.selectedExecutionProfileKey}`}
              style={{ width: '100%' }}
            />
            <Form.InputNumber field="output_limit" label={t("公开执行输出上限（Token）")} min={0} style={{ width: '100%' }} extraText={t("这是运行时硬上限，不是请求默认输出。新建时留空或 0 使用所选 Profile 的可执行上限；可显式下调。")} />
          </div>
          <Form.Input
            field="capability_tags"
            label={t("能力标签（声明）")}
            placeholder="chat,vision,json"
            extraText={t("例如 vision 只有在官方能力说明或真实多模态请求验证后才能填写；标签本身不会执行能力检测。")}
          />
          <Form.Input field="data_residency" label={t("数据驻留")} placeholder="us / eu / cn" />
          <Form.Input field="pricing_model_name" label={t("计价模型名")} placeholder={t("默认使用模型名称")} />
          <Form.Select field="approved_catalog" label={<span id="model-catalog-visibility-label">{t("开发者目录可见")}</span>} aria-labelledby="model-catalog-visibility-label" optionList={localizeOptions(approvedOptions)} />
          <Form.TextArea field="migration_recommendation" label={t("迁移建议")} placeholder={t("例如：请迁移至 gpt-4.1-mini")} rows={3} />
          <div className="ur-form-actions">
            <Button onClick={props.closeRegistrySheet}>{t("取消")}</Button>
            <Button type="primary" disabled={props.executionProfilesLoading || !!props.executionProfilesError || props.executionProfiles.length === 0} onClick={() => props.registryFormApi?.submitForm()}>{t("保存")}</Button>
          </div>
        </Form>
        </>}
      </SideSheet>

      <SideSheet title={props.aliasSheet?.mode === 'edit' ? t("编辑别名") : t("新增别名")} visible={!!props.aliasSheet} onCancel={props.closeAliasSheet} width="min(520px, 100vw)">
        <Form getFormApi={props.setAliasFormApi} onSubmit={props.onAliasSubmit}>
          <Form.Input field="alias" label={t("别名")} placeholder="company-chat" rules={[{ required: true, message: t("请输入别名") }]} />
          <Form.Input field="target_model" label={t("目标模型")} placeholder="gpt-4o" rules={[{ required: true, message: t("请输入目标模型") }]} />
          <Form.Input field="display_name" label={t("显示名称")} placeholder="Company Chat" />
          <Form.TextArea field="description" label={t("描述")} placeholder={t("内部稳定聊天模型")} rows={3} />
          <Form.Select field="status" label={<span id="model-alias-status-label">{t("状态")}</span>} aria-labelledby="model-alias-status-label" optionList={localizeOptions(statusOptions)} />
          <div className="ur-form-actions">
            <Button onClick={props.closeAliasSheet}>{t("取消")}</Button>
            <Button type="primary" onClick={() => props.aliasFormApi?.submitForm()}>{t("保存")}</Button>
          </div>
        </Form>
      </SideSheet>

      <SideSheet title={t("新增模型集")} visible={props.modelSetSheet} onCancel={props.closeModelSetSheet} width="min(540px, 100vw)">
        <Form getFormApi={props.setModelSetFormApi} onSubmit={props.onModelSetSubmit}>
          <Form.Input field="name" label={t("名称")} placeholder="Core Chat Models" rules={[{ required: true, message: t("请输入名称") }]} />
          <Form.Input field="slug" label={t("标识 Slug")} placeholder="core-chat-models" />
          <Form.TextArea field="description" label={t("描述")} placeholder={t("核心业务应用可用模型集合")} rows={2} />
          <Form.Select field="status" label={<span id="model-set-status-label">{t("状态")}</span>} aria-labelledby="model-set-status-label" optionList={localizeOptions(statusOptions)} />
          <Form.TextArea field="items" label={t("条目")} placeholder={'alias:company-chat\nmodel:gpt-4o-mini'} rows={7} />
          <div className="ur-form-actions">
            <Button onClick={props.closeModelSetSheet}>{t("取消")}</Button>
            <Button type="primary" onClick={() => props.modelSetFormApi?.submitForm()}>{t("创建")}</Button>
          </div>
        </Form>
      </SideSheet>

      <SideSheet title={t('模型集条目：{name}', { name: props.itemsSheet?.name || '' })} visible={!!props.itemsSheet} onCancel={props.closeItemsSheet} width="min(520px, 100vw)">
        <Space vertical align="start" style={{ width: '100%' }}>
          <Text type="tertiary">{t("每行一条，格式为 model:gpt-4o 或 alias:company-chat。")}</Text>
          <TextArea value={props.itemsText} onChange={props.setItemsText} rows={12} style={{ width: '100%' }} disabled={props.itemsLoading} />
          <div className="ur-form-actions">
            <Button onClick={props.closeItemsSheet}>{t("取消")}</Button>
            <Button type="primary" loading={props.itemsLoading} onClick={props.onItemsSubmit}>{t("保存条目")}</Button>
          </div>
        </Space>
      </SideSheet>
    </>
  );
}
