import { Banner, Button, Form, SideSheet, Space, TextArea, Typography } from '@douyinfe/semi-ui';
import { ExactTokenCountInput } from '../ui';
import {
  approvedOptions,
  lifecycleOptions,
  statusOptions,
  type ModelAlias,
  type ModelRegistryItem,
  type ModelSet,
} from '../utils/modelGovernance';

const { Text } = Typography;

interface ModelGovernanceSheetsProps {
  registrySheet: { mode: 'create' | 'edit'; record?: ModelRegistryItem } | null;
  registryFormApi: any;
  setRegistryFormApi: (api: any) => void;
  onRegistrySubmit: (values: Record<string, unknown>) => void | Promise<void>;
  closeRegistrySheet: () => void;
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
  return (
    <>
      <SideSheet title={props.registrySheet?.mode === 'edit' ? '编辑模型' : '新增模型'} visible={!!props.registrySheet} onCancel={props.closeRegistrySheet} width={560}>
        <Banner
          type="warning"
          description="UniRouter 不会根据模型名称自动猜测文本、多模态或其他能力。这里保存的是人工声明，不是已验证事实；请依据供应商官方能力说明或真实请求测试证据填写，未验证能力不要加入开发者目录。"
          style={{ marginBottom: 16 }}
        />
        <Form getFormApi={props.setRegistryFormApi} onSubmit={props.onRegistrySubmit}>
          <Form.Input field="model_name" label="模型名称" placeholder="gpt-4o" rules={[{ required: true, message: '请输入模型名称' }]} />
          <Form.Input field="display_name" label="显示名称" placeholder="Company Chat" />
          <Form.Input field="provider_family" label="供应商族" placeholder="openai / anthropic / gemini" />
          <Form.Select field="lifecycle" label="生命周期" optionList={lifecycleOptions} rules={[{ required: true, message: '请选择生命周期' }]} />
          <div className="ur-modelgov-form-grid">
            <ExactTokenCountInput
              field="context_length"
              initialValue={props.registrySheet?.record?.context_length ?? 0}
              resetKey={`${props.registrySheet?.mode || 'closed'}-${props.registrySheet?.record?.id || 'new'}`}
              style={{ width: '100%' }}
            />
            <Form.InputNumber field="output_limit" label="输出上限" min={0} style={{ width: '100%' }} />
          </div>
          <Form.Input
            field="capability_tags"
            label="能力标签（声明）"
            placeholder="chat,vision,json"
            extraText="例如 vision 只有在官方能力说明或真实多模态请求验证后才能填写；标签本身不会执行能力检测。"
          />
          <Form.Input field="data_residency" label="数据驻留" placeholder="us / eu / cn" />
          <Form.Input field="pricing_model_name" label="计价模型名" placeholder="默认使用模型名称" />
          <Form.Select field="approved_catalog" label="开发者目录可见" optionList={approvedOptions} />
          <Form.TextArea field="migration_recommendation" label="迁移建议" placeholder="例如：请迁移至 gpt-4.1-mini" rows={3} />
          <div className="ur-form-actions">
            <Button onClick={props.closeRegistrySheet}>取消</Button>
            <Button type="primary" onClick={() => props.registryFormApi?.submitForm()}>保存</Button>
          </div>
        </Form>
      </SideSheet>

      <SideSheet title={props.aliasSheet?.mode === 'edit' ? '编辑别名' : '新增别名'} visible={!!props.aliasSheet} onCancel={props.closeAliasSheet} width={520}>
        <Form getFormApi={props.setAliasFormApi} onSubmit={props.onAliasSubmit}>
          <Form.Input field="alias" label="别名" placeholder="company-chat" rules={[{ required: true, message: '请输入别名' }]} />
          <Form.Input field="target_model" label="目标模型" placeholder="gpt-4o" rules={[{ required: true, message: '请输入目标模型' }]} />
          <Form.Input field="display_name" label="显示名称" placeholder="Company Chat" />
          <Form.TextArea field="description" label="描述" placeholder="内部稳定聊天模型" rows={3} />
          <Form.Select field="status" label="状态" optionList={statusOptions} />
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
          <Form.Select field="status" label="状态" optionList={statusOptions} />
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
