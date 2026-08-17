import { useEffect, useState } from 'react';
import { Button, Form, Modal, SideSheet, Space, Table, Tag, TextArea, Toast, Typography } from '@douyinfe/semi-ui';
import { IconEdit, IconPlus } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { isBillingRole, isOrgAdminRole, isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, PageHeader, Surface } from '../ui';
import { customerContextBlockerCopy, effectiveCustomerOrgID, resolveCustomerContext } from '../utils/customerContext';
import { loadAllPages } from '../utils/loadAllPages';
import { projectDisplayName } from '../utils/projectDisplay';

const { Text, Paragraph } = Typography;

interface GuardrailPolicy {
  id: number;
  name: string;
  version: number;
  org_id?: number | null;
  org_name?: string;
  project_id?: number | null;
  project_name?: string;
  api_key_id?: number | null;
  api_key_name?: string;
  api_key_prefix?: string;
  model?: string;
  stage: 'input' | 'nonstream_output' | 'stream_output';
  detector_ref: string;
  action: 'audit' | 'redact' | 'block';
  mode: 'disabled' | 'shadow' | 'enforce';
  failure_mode: 'fail_closed' | 'degraded_allow';
  mandatory: boolean;
  config?: { entities?: string[] };
  config_redacted?: boolean;
}

interface KeyOption {
  id: number;
  name: string;
  key_prefix: string;
  org_id: number;
  org_name?: string;
  project_id?: number;
  project_name?: string;
}

interface StreamDetectorCapability {
  synchronous: boolean;
  lookbehind_bytes: number;
}

function selectorText(policy: GuardrailPolicy) {
  const parts: string[] = [policy.org_id
    ? (policy.org_name || `客户组织 #${policy.org_id}`)
    : '部署全局（当前部署全部请求）'];
  if (policy.project_id) parts.push(projectDisplayName({ id: policy.project_id, name: policy.project_name || '', slug: policy.project_name === 'Default Project' ? 'default' : undefined }));
  if (policy.api_key_id) parts.push(policy.api_key_name ? `凭证 ${policy.api_key_name}` : `凭证 #${policy.api_key_id}`);
  if (policy.model) parts.push(`模型 ${policy.model}`);
  return parts.join(' · ');
}

function modeTag(mode: GuardrailPolicy['mode']) {
  if (mode === 'enforce') return <Tag color="red">正式执行</Tag>;
  if (mode === 'shadow') return <Tag color="orange">影子观察</Tag>;
  return <Tag color="grey">已停用</Tag>;
}

function wordsFromText(value: string) {
  return Array.from(new Set(value.split(/\r?\n/).map((word) => word.trim()).filter(Boolean)));
}

const detectorLabels: Record<string, string> = {
  'builtin:sensitive_words': '现有敏感词兼容检测',
  'builtin:pii_secrets': '本地 PII 与密钥检测',
};

const actionLabels: Record<string, string> = {
  audit: '只审计', redact: '脱敏后继续', block: '阻断',
};

const stageLabels: Record<GuardrailPolicy['stage'], string> = {
  input: '请求输入',
  nonstream_output: '非流式输出',
  stream_output: '流式输出',
};

export default function GuardrailPoliciesPage() {
  const user = useAuthStore((state) => state.user);
  const organizationRole = organizationRoleForUser(user);
  const customerContext = resolveCustomerContext(user);
  const customerOrgID = effectiveCustomerOrgID(user);
  const customerContextReady = customerContext.state === 'ready' && Boolean(customerOrgID);
  const canManage = isOrgAdminRole(organizationRole) && !isBillingRole(organizationRole);
  const platform = isPlatformAdminRole(organizationRole);
  const [policies, setPolicies] = useState<GuardrailPolicy[]>([]);
  const [keys, setKeys] = useState<KeyOption[]>([]);
  const [models, setModels] = useState<string[]>([]);
  const [projects, setProjects] = useState<Array<{ value: number; label: string }>>([]);
  const [detectors, setDetectors] = useState<string[]>(['builtin:sensitive_words', 'builtin:pii_secrets']);
  const [streamCapabilities, setStreamCapabilities] = useState<Record<string, StreamDetectorCapability>>({
    'builtin:sensitive_words': { synchronous: true, lookbehind_bytes: 4 * 1024 },
    'builtin:pii_secrets': { synchronous: true, lookbehind_bytes: 512 },
  });
  const [streamBufferMaxBytes, setStreamBufferMaxBytes] = useState(4 * 1024);
  const [streamBufferMaxWaitMs, setStreamBufferMaxWaitMs] = useState(200);
  const [streamCheckpointIntervalMs, setStreamCheckpointIntervalMs] = useState(1000);
  const [localDetectorMaxBytes, setLocalDetectorMaxBytes] = useState(128 * 1024);
  const [externalDetectorMaxBytes, setExternalDetectorMaxBytes] = useState(256 * 1024);
  const [detectorMaxTexts, setDetectorMaxTexts] = useState(256);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [total, setTotal] = useState(0);
  const [drawerVisible, setDrawerVisible] = useState(false);
  const [editing, setEditing] = useState<GuardrailPolicy | null>(null);
  const [policyScope, setPolicyScope] = useState<'customer' | 'deployment_global'>('customer');
  const [formApi, setFormApi] = useState<any>(null);
  const [sensitiveWordsText, setSensitiveWordsText] = useState('');
  const [sensitiveWordsLoaded, setSensitiveWordsLoaded] = useState(false);
  const [sensitiveWordsLoading, setSensitiveWordsLoading] = useState(false);
  const [sensitiveWordsSaving, setSensitiveWordsSaving] = useState(false);
  const [sensitiveWordsError, setSensitiveWordsError] = useState('');

  const load = (nextPage = page, nextSize = pageSize) => {
    if (!customerContextReady) return;
    setLoading(true);
    api.getGuardrailPolicies(nextPage, nextSize)
      .then((data: any) => {
        setPolicies(data?.data || []);
        setTotal(data?.total || 0);
        setDetectors(data?.approved_detectors || ['builtin:sensitive_words', 'builtin:pii_secrets']);
        setStreamCapabilities(data?.stream_detector_capabilities || {
          'builtin:sensitive_words': { synchronous: true, lookbehind_bytes: 4 * 1024 },
          'builtin:pii_secrets': { synchronous: true, lookbehind_bytes: 512 },
        });
        setStreamBufferMaxBytes(data?.stream_enforce_buffer_max_bytes || 4 * 1024);
        setStreamBufferMaxWaitMs(data?.stream_enforce_buffer_max_wait_ms || 200);
        setStreamCheckpointIntervalMs(data?.stream_checkpoint_interval_ms || 1000);
        setLocalDetectorMaxBytes(data?.local_detector_max_bytes || 128 * 1024);
        setExternalDetectorMaxBytes(data?.external_detector_max_bytes || 256 * 1024);
        setDetectorMaxTexts(data?.detector_max_texts || 256);
      })
      .catch((err: any) => Toast.error(err?.message || '安全策略加载失败'))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    if (customerContextReady) load(1, pageSize);
  }, [customerContextReady, customerOrgID]);
  const loadSensitiveWords = async () => {
    setSensitiveWordsLoading(true);
    setSensitiveWordsLoaded(false);
    setSensitiveWordsError('');
    try {
      const result = await api.getSensitiveWords();
      if (!Array.isArray(result?.data) || result.data.some((word: unknown) => typeof word !== 'string')) {
        throw new Error('服务返回了无法识别的词库数据');
      }
      setSensitiveWordsText(result.data.join('\n'));
      setSensitiveWordsLoaded(true);
    } catch (err: any) {
      setSensitiveWordsError(err?.message || '全局输入阻断词库加载失败');
    } finally {
      setSensitiveWordsLoading(false);
    }
  };

  useEffect(() => {
    if (platform && customerContextReady) void loadSensitiveWords();
  }, [customerContextReady, platform]);

  useEffect(() => {
    if (!customerContextReady || !customerOrgID) return;
    void (async () => {
      try {
        const [keyData, projectData, modelData] = await Promise.all([
          loadAllPages<KeyOption>((page, size) => api.getKeys(page, size)),
          loadAllPages<any>((page, size) => api.getProjects(page, size, { org_id: customerOrgID })),
          api.getModelNames(),
        ]);
        setKeys((keyData || []).filter((key) => key.org_id === customerOrgID));
        setProjects((projectData || [])
          .filter((project: any) => project.org_id === customerOrgID)
          .map((project: any) => ({ value: project.id, label: projectDisplayName(project) })));
        const source = Array.isArray(modelData?.data) ? modelData.data : modelData;
        setModels((Array.isArray(source) ? source : []).map((item: any) => typeof item === 'string' ? item : item.model).filter(Boolean));
      } catch {
        Toast.error('安全策略选项加载失败，请刷新后重试');
      }
    })();
  }, [customerContextReady, customerOrgID]);

  const closeDrawer = () => {
    setDrawerVisible(false);
    setEditing(null);
    formApi?.reset();
  };

  useEffect(() => {
    if (!drawerVisible || !formApi) return;
    formApi.reset();
    if (editing) {
      const nextScope = editing.org_id ? 'customer' : 'deployment_global';
      setPolicyScope(nextScope);
      formApi.setValues({
        ...editing,
        scope: nextScope,
        org_id: editing.org_id || undefined,
        project_id: editing.project_id || undefined,
        api_key_id: editing.api_key_id || undefined,
        mandatory: editing.mandatory ? 'yes' : 'no',
        entities: editing.config?.entities || ['email', 'phone', 'credit_card', 'secret'],
      });
      return;
    }
    setPolicyScope('customer');
    formApi.setValues({
      scope: 'customer',
      stage: 'input', detector_ref: 'builtin:pii_secrets', action: 'audit',
      mode: 'shadow', failure_mode: 'degraded_allow', mandatory: 'no',
      entities: ['email', 'phone', 'credit_card', 'secret'],
    });
  }, [drawerVisible, editing, formApi]);

  const submit = async (values: Record<string, any>) => {
    if (!customerContextReady || !customerOrgID) {
      Toast.error('无法确认唯一客户组织，安全策略未保存');
      return;
    }
    const mandatory = platform && values.mandatory === 'yes';
    const deploymentGlobal = platform && (mandatory || values.scope === 'deployment_global');
    const streamCapability = streamCapabilities[values.detector_ref];
    if (values.stage === 'stream_output' && (mandatory || values.mode === 'enforce') && values.action !== 'audit' && !streamCapability?.synchronous) {
      Toast.error('该检测器不能在流式输出写出前同步完成，只能先使用“影子观察”或“只审计”。');
      return;
    }
    const payload: Record<string, any> = {
      name: values.name,
      ...(editing ? { version: editing.version } : {}),
      org_id: deploymentGlobal ? 0 : customerOrgID,
      project_id: deploymentGlobal ? 0 : Number(values.project_id || 0),
      api_key_id: deploymentGlobal ? 0 : Number(values.api_key_id || 0),
      model: values.model || '',
      stage: values.stage,
      detector_ref: values.detector_ref,
      action: values.action,
      mode: mandatory ? 'enforce' : values.mode,
      failure_mode: mandatory ? 'fail_closed' : values.failure_mode,
      mandatory,
      config: values.detector_ref === 'builtin:pii_secrets' ? { entities: values.entities || [] } : {},
    };
    if (!editing) {
      ['org_id', 'project_id', 'api_key_id'].forEach((field) => { if (!payload[field]) delete payload[field]; });
    }
    try {
      const result = editing
        ? await api.updateGuardrailPolicy(editing.id, payload)
        : await api.createGuardrailPolicy(payload);
      if (result?.runtime_snapshot_applied === false) Toast.warning('策略已保存，运行副本将在下一次刷新时收敛');
      else Toast.success(editing ? '安全策略已更新并生成新版本' : '安全策略已创建');
      closeDrawer();
      load();
    } catch (err: any) {
      Toast.error(err?.message || '安全策略保存失败');
    }
  };

  const confirmSensitiveWordsReplace = () => {
    const words = wordsFromText(sensitiveWordsText);
    const clearingAll = words.length === 0;
    Modal.confirm({
      title: clearingAll ? '确认清空全局输入阻断词库？' : '确认替换全局输入阻断词库？',
      content: clearingAll
        ? '当前内容为空。继续后将清空全部部署级输入阻断词，当前部署的所有项目和访问凭证都会受到影响。'
        : `将以当前 ${words.length} 个词完整替换部署级词库，并影响当前部署的所有项目和访问凭证；这不是追加操作。`,
      okText: clearingAll ? '确认清空' : '确认全局替换',
      cancelText: '取消',
      onOk: async () => {
        setSensitiveWordsSaving(true);
        setSensitiveWordsError('');
        try {
          const result = await api.replaceSensitiveWords(words);
          if (!Array.isArray(result?.data) || result.data.some((word: unknown) => typeof word !== 'string')) {
            throw new Error('保存成功状态无法确认：服务返回了无法识别的词库数据');
          }
          setSensitiveWordsText(result.data.join('\n'));
          Toast.success('全局输入阻断词库已替换');
        } catch (err: any) {
          setSensitiveWordsError(err?.message || '全局输入阻断词库保存失败，当前编辑内容尚未确认生效');
          throw err;
        } finally {
          setSensitiveWordsSaving(false);
        }
      },
    });
  };

  const columns = [
    { title: '策略', dataIndex: 'name', render: (value: string, record: GuardrailPolicy) => <div><Text strong>{value}</Text><br /><Text type="tertiary" size="small">版本 v{record.version}{record.mandatory ? ' · 平台强制' : ''}</Text></div> },
    { title: '适用范围', render: (_: unknown, record: GuardrailPolicy) => selectorText(record) },
    { title: '阶段', dataIndex: 'stage', width: 130, render: (value: GuardrailPolicy['stage']) => stageLabels[value] || value },
    { title: '检测与动作', render: (_: unknown, record: GuardrailPolicy) => `${detectorLabels[record.detector_ref] || record.detector_ref} · ${actionLabels[record.action] || record.action}` },
    { title: '状态', dataIndex: 'mode', width: 100, render: (value: GuardrailPolicy['mode']) => modeTag(value) },
    { title: '检测器不可用时', dataIndex: 'failure_mode', width: 150, render: (value: string) => value === 'fail_closed' ? '安全拒绝（推荐强制规则）' : '留痕后继续' },
    ...(canManage ? [{ title: '操作', width: 100, render: (_: unknown, record: GuardrailPolicy) => (
      platform || record.org_id === customerOrgID
        ? <Button aria-label={`编辑安全策略 ${record.name}`} icon={<IconEdit />} size="small" onClick={() => { setEditing(record); setDrawerVisible(true); }} />
        : <Text type="tertiary" size="small">平台只读</Text>
    ) }] : []),
  ];

  if (!customerContextReady) {
    const blocker = customerContextBlockerCopy(customerContext.state === 'ready' ? 'unavailable' : customerContext.state);
    return (
      <div className="ur-page">
        <PageHeader eyebrow="访问与安全" title={blocker.title} description="内容安全策略必须绑定唯一客户；系统不会把部署技术组织当作客户组织。" />
        <Surface><EmptyState title={blocker.title} description={blocker.description} minHeight={260} /></Surface>
      </div>
    );
  }

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="访问与安全"
        title="内容安全"
        description="在请求发往模型前，以及非流式或流式结果交付客户前，执行版本固定的审计、脱敏或阻断。"
        actions={canManage ? <Button icon={<IconPlus />} type="primary" onClick={() => { setEditing(null); setDrawerVisible(true); }}>新增策略</Button> : undefined}
      />

      <Surface title="安全策略" description="策略列表是权威管理入口；先确认当前范围、检查阶段和执行模式，再新增或编辑。">
        <div className="ur-table-shell">
          <Table rowKey="id" columns={columns} dataSource={policies} loading={loading} pagination={{
            currentPage: page, pageSize, total, pageSizeOpts: [20, 50, 100], showSizeChanger: true,
            onPageChange: (next: number) => { setPage(next); load(next, pageSize); },
            onPageSizeChange: (next: number) => { setPageSize(next); setPage(1); load(1, next); },
          }} />
        </div>
      </Surface>

      {platform ? (
        <Surface
          title="全局输入阻断词库"
          description="部署级兼容词库，一行一词。保存会完整替换当前词库，而不是追加。"
        >
          <Paragraph>
            <Text strong>全局影响：</Text>
            修改会影响当前部署所有项目和访问凭证的请求输入；当前处理实例在提交后立即应用，其他实例通过周期刷新收敛。
          </Paragraph>
          {sensitiveWordsError ? <Paragraph><Text type="danger">{sensitiveWordsError}</Text></Paragraph> : null}
          {!sensitiveWordsLoaded ? (
            <Button onClick={() => void loadSensitiveWords()} loading={sensitiveWordsLoading}>
              {sensitiveWordsLoading ? '正在加载词库' : '重新加载词库'}
            </Button>
          ) : (
            <>
              <TextArea
                aria-label="全局输入阻断词库"
                value={sensitiveWordsText}
                onChange={setSensitiveWordsText}
                placeholder={'例如：\n测试词\n禁止内容'}
                autosize={{ minRows: 8, maxRows: 18 }}
                disabled={sensitiveWordsLoading || sensitiveWordsSaving}
              />
              <Space style={{ display: 'flex', justifyContent: 'space-between', marginTop: 12 }}>
                <Text type="tertiary">当前编辑内容共 {wordsFromText(sensitiveWordsText).length} 个非空去重词条</Text>
                <Button
                  type="primary"
                  loading={sensitiveWordsSaving}
                  disabled={sensitiveWordsLoading}
                  onClick={confirmSensitiveWordsReplace}
                >
                  保存并替换全局词库
                </Button>
              </Space>
            </>
          )}
        </Surface>
      ) : null}

      <Surface title="安全边界与使用说明">
        <Paragraph><Text strong>推荐从影子观察开始：</Text>检测器只报告发现，由 UniRouter 内核统一决定继续、脱敏或阻断；影子模式不会改变客户请求。</Paragraph>
        <Paragraph>部署强制规则对当前部署全部请求生效；唯一客户、项目和访问凭证策略只能继续收紧，不能关闭或放宽部署强制规则。外部检测器必须先由部署管理员登记，不能填写任意网址。</Paragraph>
        <Paragraph><Text strong>外部检测会传输内容：</Text>启用外部检测策略会把所选文本发送到平台预先登记的固定服务。只有在租户授权、数据驻留、保留与供应商条款均已确认后才能启用；当前默认未登记真实外部检测器。</Paragraph>
        <Paragraph><Text strong>故障规则：</Text>强制安全检测器不可用时拒绝请求；普通审计可选择“留痕后继续”。外部检测默认最多等待 1 秒且不自动重试，避免同一次请求被重复处理。</Paragraph>
        <Paragraph><Text strong>同步扫描边界：</Text>每次策略评估最多检查 {detectorMaxTexts} 个文本单元；本地 PII/密钥检测最多处理 {Math.round(localDetectorMaxBytes / 1024)} KiB，外部检测最多处理 {Math.round(externalDetectorMaxBytes / 1024)} KiB。超出时不会假装已检查，而是标记为“无法完整判断”，再按上面的故障规则安全拒绝或留痕后继续。</Paragraph>
        <Paragraph><Text strong>流式输出不是零泄露承诺：</Text>每个完整的模型数据帧都在写出前同步检查；已标准化的模型数据 JSON 最多聚合 {streamBufferMaxWaitMs} 毫秒或 {Math.round(streamBufferMaxBytes / 1024)} KiB，先到即关闭本批，并只保留检测器申报的有限跨帧上下文。单个标准化模型数据帧超过该上限会在交付前安全中断；更大的 SSE 传输行上限不代表强制检查缓冲也允许同样大小。影子/只审计检测可能在内容写出后才形成结论。</Paragraph>
        <Paragraph><Text strong>流式中断与恢复：</Text>命中阻断或硬预算无法扩容时立即停止当前上游，不再切换通道。已安全写出的前缀按实际用量结算；未写出的内容不向客户收费。系统约每 {Math.max(1, Math.round(streamCheckpointIntervalMs / 1000))} 秒保存一次不含正文的计数 checkpoint，实例故障后明确标记中断，不会从中间偷偷重放或迁移。</Paragraph>
        <Paragraph><Text strong>输出阻断与费用：</Text>未交付的被阻断内容不向客户收费；上游可能已经计费，因此该部分会进入上游成本待处理，直到平台操作员明确退款或承担成本。</Paragraph>
        <Paragraph>当前策略覆盖 Chat 输入、Embeddings 文本输入，以及 Chat 非流式和流式普通文本、文本分段、函数/工具参数；无法识别的结构化输出不会假装检查通过。</Paragraph>
      </Surface>

      <SideSheet title={editing ? `编辑安全策略：${editing.name}` : '新增内容安全策略'} visible={drawerVisible} onCancel={closeDrawer} width={580}>
        <Form getFormApi={(instance: any) => setFormApi(instance)} onSubmit={submit}>
          <Form.Input field="name" label="策略名称" placeholder="customer-pii-redaction" rules={[{ required: true, message: '请输入策略名称' }]} />
          {platform ? (
            <Form.Select
              field="scope"
              label="作用域"
              optionList={[
                { value: 'customer', label: `唯一客户（推荐）· ${customerContext.orgName || `组织 #${customerOrgID}`}` },
                { value: 'deployment_global', label: '部署全局（当前部署全部请求）' },
              ]}
              onChange={(value: any) => {
                const nextScope = value === 'deployment_global' ? 'deployment_global' : 'customer';
                setPolicyScope(nextScope);
                if (nextScope === 'deployment_global') {
                  formApi?.setValue('project_id', undefined);
                  formApi?.setValue('api_key_id', undefined);
                }
              }}
              rules={[{ required: true }]}
              style={{ width: '100%' }}
            />
          ) : <Text type="tertiary">客户范围：{customerContext.orgName || `组织 #${customerOrgID}`}</Text>}
          <Form.Select field="project_id" label="项目（可选）" optionList={projects} showClear filter disabled={policyScope === 'deployment_global'} style={{ width: '100%' }} />
          <Form.Select field="api_key_id" label="访问凭证（可选）" optionList={keys.map((key) => ({ value: key.id, label: `${key.name} · ${projectDisplayName({ id: key.project_id || 0, name: key.project_name || '' })} · ${key.key_prefix}***` }))} showClear filter disabled={policyScope === 'deployment_global'} style={{ width: '100%' }} />
          <Form.Select field="model" label="模型（可选）" optionList={models.map((model) => ({ value: model, label: model }))} showClear filter style={{ width: '100%' }} />
          <Form.Select field="stage" label="检查阶段" optionList={[{ value: 'input', label: '请求输入（发往上游前）' }, { value: 'nonstream_output', label: '非流式输出（完整结果交付前）' }, { value: 'stream_output', label: '流式输出（每个模型数据帧写出前）' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Select field="detector_ref" label="检测器" optionList={detectors.map((value) => ({
            value,
            label: `${detectorLabels[value] || value}${streamCapabilities[value]?.synchronous ? '' : '（流式仅影子/审计）'}`,
          }))} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Select field="entities" label="PII/密钥类别" multiple optionList={[
            { value: 'email', label: '邮箱' }, { value: 'phone', label: '电话号码' },
            { value: 'credit_card', label: '银行卡号（含校验）' }, { value: 'secret', label: 'API Key / Token / Secret' },
          ]} style={{ width: '100%' }} />
          <Form.Select field="action" label="命中后动作" optionList={[{ value: 'audit', label: '只审计，不改变内容' }, { value: 'redact', label: '脱敏后继续（仅支持可定位的 PII）' }, { value: 'block', label: '阻断，不交付内容' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Select field="mode" label="运行状态" optionList={[{ value: 'shadow', label: '影子观察（推荐先选）' }, { value: 'enforce', label: '正式执行' }, { value: 'disabled', label: '停用' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Select field="failure_mode" label="检测器不可用时" optionList={[{ value: 'degraded_allow', label: '记录降级后继续（适合普通审计）' }, { value: 'fail_closed', label: '安全拒绝（适合强制规则）' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          {platform ? <Form.Select field="mandatory" label="是否为部署强制规则" optionList={[{ value: 'no', label: '否' }, { value: 'yes', label: '是（自动设为部署全局、正式执行、安全拒绝）' }]} onChange={(value: any) => {
            if (value === 'yes') {
              setPolicyScope('deployment_global');
              formApi?.setValue('scope', 'deployment_global');
              formApi?.setValue('project_id', undefined);
              formApi?.setValue('api_key_id', undefined);
            }
          }} rules={[{ required: true }]} style={{ width: '100%' }} /> : null}
          <Space style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 24 }}>
            <Button onClick={closeDrawer}>取消</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>{editing ? '保存并生成新版本' : '创建策略'}</Button>
          </Space>
        </Form>
      </SideSheet>
    </div>
  );
}
