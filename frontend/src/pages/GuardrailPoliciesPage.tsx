import { useEffect, useMemo, useState } from 'react';
import { Button, Form, SideSheet, Space, Table, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconEdit, IconPlus } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { isBillingRole, isOrgAdminRole, isPlatformAdminRole } from '../navigation';
import { useAuthStore } from '../store/auth';
import { PageHeader, Surface } from '../ui';

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
  stage: 'input' | 'nonstream_output';
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

function selectorText(policy: GuardrailPolicy) {
  const parts: string[] = [];
  if (policy.org_id) parts.push(policy.org_name || `组织 #${policy.org_id}`);
  if (policy.project_id) parts.push(policy.project_name || `项目 #${policy.project_id}`);
  if (policy.api_key_id) parts.push(policy.api_key_name ? `凭证 ${policy.api_key_name}` : `凭证 #${policy.api_key_id}`);
  if (policy.model) parts.push(`模型 ${policy.model}`);
  return parts.length ? parts.join(' · ') : '平台全局（所有租户）';
}

function modeTag(mode: GuardrailPolicy['mode']) {
  if (mode === 'enforce') return <Tag color="red">正式执行</Tag>;
  if (mode === 'shadow') return <Tag color="orange">影子观察</Tag>;
  return <Tag color="grey">已停用</Tag>;
}

const detectorLabels: Record<string, string> = {
  'builtin:sensitive_words': '现有敏感词兼容检测',
  'builtin:pii_secrets': '本地 PII 与密钥检测',
};

const actionLabels: Record<string, string> = {
  audit: '只审计', redact: '脱敏后继续', block: '阻断',
};

export default function GuardrailPoliciesPage() {
  const role = useAuthStore((state) => state.user?.role);
  const userOrgId = useAuthStore((state) => state.user?.org_id);
  const canManage = isOrgAdminRole(role) && !isBillingRole(role);
  const platform = isPlatformAdminRole(role);
  const [policies, setPolicies] = useState<GuardrailPolicy[]>([]);
  const [keys, setKeys] = useState<KeyOption[]>([]);
  const [models, setModels] = useState<string[]>([]);
  const [detectors, setDetectors] = useState<string[]>(['builtin:sensitive_words', 'builtin:pii_secrets']);
	const [localDetectorMaxBytes, setLocalDetectorMaxBytes] = useState(128 * 1024);
  const [externalDetectorMaxBytes, setExternalDetectorMaxBytes] = useState(256 * 1024);
  const [detectorMaxTexts, setDetectorMaxTexts] = useState(256);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [total, setTotal] = useState(0);
  const [drawerVisible, setDrawerVisible] = useState(false);
  const [editing, setEditing] = useState<GuardrailPolicy | null>(null);
  const [formApi, setFormApi] = useState<any>(null);

  const load = (nextPage = page, nextSize = pageSize) => {
    setLoading(true);
    api.getGuardrailPolicies(nextPage, nextSize)
      .then((data: any) => {
        setPolicies(data?.data || []);
        setTotal(data?.total || 0);
        setDetectors(data?.approved_detectors || ['builtin:sensitive_words', 'builtin:pii_secrets']);
		setLocalDetectorMaxBytes(data?.local_detector_max_bytes || 128 * 1024);
		setExternalDetectorMaxBytes(data?.external_detector_max_bytes || 256 * 1024);
		setDetectorMaxTexts(data?.detector_max_texts || 256);
      })
      .catch((err: any) => Toast.error(err?.message || '安全策略加载失败'))
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(1, pageSize); }, []);
  useEffect(() => {
    Promise.all([api.getKeys(1, 100), api.getModelNames()])
      .then(([keyData, modelData]: any[]) => {
        setKeys(keyData?.data || []);
        const source = Array.isArray(modelData?.data) ? modelData.data : modelData;
        setModels((Array.isArray(source) ? source : []).map((item: any) => typeof item === 'string' ? item : item.model).filter(Boolean));
      })
      .catch(() => {});
  }, []);

  const organizations = useMemo(() => {
    const map = new Map<number, string>();
    keys.forEach((key) => map.set(key.org_id, key.org_name || `组织 #${key.org_id}`));
    return Array.from(map.entries()).map(([value, label]) => ({ value, label }));
  }, [keys]);
  const projects = useMemo(() => {
    const map = new Map<number, string>();
    keys.forEach((key) => {
      if (key.project_id) map.set(key.project_id, `${key.project_name || `项目 #${key.project_id}`} · ${key.org_name || `组织 #${key.org_id}`}`);
    });
    return Array.from(map.entries()).map(([value, label]) => ({ value, label }));
  }, [keys]);

  const closeDrawer = () => {
    setDrawerVisible(false);
    setEditing(null);
    formApi?.reset();
  };

  useEffect(() => {
    if (!drawerVisible || !formApi) return;
    formApi.reset();
    if (editing) {
      formApi.setValues({
        ...editing,
        org_id: editing.org_id || undefined,
        project_id: editing.project_id || undefined,
        api_key_id: editing.api_key_id || undefined,
        mandatory: editing.mandatory ? 'yes' : 'no',
        entities: editing.config?.entities || ['email', 'phone', 'credit_card', 'secret'],
      });
      return;
    }
    formApi.setValues({
      stage: 'input', detector_ref: 'builtin:pii_secrets', action: 'audit',
      mode: 'shadow', failure_mode: 'degraded_allow', mandatory: 'no',
      entities: ['email', 'phone', 'credit_card', 'secret'],
    });
  }, [drawerVisible, editing, formApi]);

  const submit = async (values: Record<string, any>) => {
    const mandatory = platform && values.mandatory === 'yes';
    const payload: Record<string, any> = {
      name: values.name,
      ...(editing ? { version: editing.version } : {}),
      org_id: mandatory ? 0 : (platform ? Number(values.org_id || 0) : userOrgId),
      project_id: mandatory ? 0 : Number(values.project_id || 0),
      api_key_id: mandatory ? 0 : Number(values.api_key_id || 0),
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

  const columns = [
    { title: '策略', dataIndex: 'name', render: (value: string, record: GuardrailPolicy) => <div><Text strong>{value}</Text><br /><Text type="tertiary" size="small">版本 v{record.version}{record.mandatory ? ' · 平台强制' : ''}</Text></div> },
    { title: '适用范围', render: (_: unknown, record: GuardrailPolicy) => selectorText(record) },
    { title: '阶段', dataIndex: 'stage', width: 130, render: (value: string) => value === 'input' ? '请求输入' : '非流式输出' },
    { title: '检测与动作', render: (_: unknown, record: GuardrailPolicy) => `${detectorLabels[record.detector_ref] || record.detector_ref} · ${actionLabels[record.action] || record.action}` },
    { title: '状态', dataIndex: 'mode', width: 100, render: (value: GuardrailPolicy['mode']) => modeTag(value) },
    { title: '检测器不可用时', dataIndex: 'failure_mode', width: 150, render: (value: string) => value === 'fail_closed' ? '安全拒绝（推荐强制规则）' : '留痕后继续' },
    ...(canManage ? [{ title: '操作', width: 100, render: (_: unknown, record: GuardrailPolicy) => (
      platform || record.org_id === userOrgId
        ? <Button aria-label={`编辑安全策略 ${record.name}`} icon={<IconEdit />} size="small" onClick={() => { setEditing(record); setDrawerVisible(true); }} />
        : <Text type="tertiary" size="small">平台只读</Text>
    ) }] : []),
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="安全治理"
        title="内容安全策略"
        description="在请求发往模型前、以及非流式结果交付客户前执行版本固定的审计、脱敏或阻断。"
        actions={canManage ? <Button icon={<IconPlus />} type="primary" onClick={() => { setEditing(null); setDrawerVisible(true); }}>新增策略</Button> : undefined}
      />

      <Surface title="如何安全使用">
        <Paragraph><Text strong>推荐从影子观察开始：</Text>检测器只报告发现，由 UniRouter 内核统一决定继续、脱敏或阻断；影子模式不会改变客户请求。</Paragraph>
        <Paragraph>平台强制规则对所有租户生效，组织、项目和访问凭证策略只能继续收紧，不能关闭或放宽平台规则。外部检测器必须先由平台注册，不能填写任意网址。</Paragraph>
		<Paragraph><Text strong>外部检测会传输内容：</Text>启用外部检测策略会把所选文本发送到平台预先登记的固定服务。只有在租户授权、数据驻留、保留与供应商条款均已确认后才能启用；当前默认未登记真实外部检测器。</Paragraph>
        <Paragraph><Text strong>故障规则：</Text>强制安全检测器不可用时拒绝请求；普通审计可选择“留痕后继续”。外部检测默认最多等待 1 秒且不自动重试，避免同一次请求被重复处理。</Paragraph>
		<Paragraph><Text strong>同步扫描边界：</Text>每次策略评估最多检查 {detectorMaxTexts} 个文本单元；本地 PII/密钥检测最多处理 {Math.round(localDetectorMaxBytes / 1024)} KiB，外部检测最多处理 {Math.round(externalDetectorMaxBytes / 1024)} KiB。超出时不会假装已检查，而是标记为“无法完整判断”，再按上面的故障规则安全拒绝或留痕后继续。</Paragraph>
        <Paragraph><Text strong>输出阻断与费用：</Text>被阻断的内容不会交付、不会向客户收费；上游可能已经计费，因此该请求会进入上游成本待处理并暂时阻止该凭证继续产生新费用，直到平台操作员明确退款或承担成本。</Paragraph>
		<Paragraph>当前策略覆盖 Chat 输入、Embeddings 文本输入，以及 Chat 非流式普通文本、文本分段和函数/工具参数；无法识别的结构化输出不会假装检查通过。流式逐段拦截属于独立的流式安全能力。</Paragraph>
      </Surface>

      <Surface>
        <div className="ur-table-shell">
          <Table rowKey="id" columns={columns} dataSource={policies} loading={loading} pagination={{
            currentPage: page, pageSize, total, pageSizeOpts: [20, 50, 100], showSizeChanger: true,
            onPageChange: (next: number) => { setPage(next); load(next, pageSize); },
            onPageSizeChange: (next: number) => { setPageSize(next); setPage(1); load(1, next); },
          }} />
        </div>
      </Surface>

      <SideSheet title={editing ? `编辑安全策略：${editing.name}` : '新增内容安全策略'} visible={drawerVisible} onCancel={closeDrawer} width={580}>
        <Form getFormApi={(instance: any) => setFormApi(instance)} onSubmit={submit}>
          <Form.Input field="name" label="策略名称" placeholder="customer-pii-redaction" rules={[{ required: true, message: '请输入策略名称' }]} />
          {platform ? <Form.Select field="org_id" label="组织范围（可选）" placeholder="留空表示平台全局" optionList={organizations} showClear style={{ width: '100%' }} /> : <Text type="tertiary">组织范围：当前组织（{userOrgId || '由登录身份确定'}）</Text>}
          <Form.Select field="project_id" label="项目（可选）" optionList={projects} showClear filter style={{ width: '100%' }} />
          <Form.Select field="api_key_id" label="访问凭证（可选）" optionList={keys.map((key) => ({ value: key.id, label: `${key.name} · ${key.org_name || `组织 #${key.org_id}`} · ${key.key_prefix}***` }))} showClear filter style={{ width: '100%' }} />
          <Form.Select field="model" label="模型（可选）" optionList={models.map((model) => ({ value: model, label: model }))} showClear filter style={{ width: '100%' }} />
          <Form.Select field="stage" label="检查阶段" optionList={[{ value: 'input', label: '请求输入（发往上游前）' }, { value: 'nonstream_output', label: '非流式输出（交付客户前）' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Select field="detector_ref" label="检测器" optionList={detectors.map((value) => ({ value, label: detectorLabels[value] || value }))} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Select field="entities" label="PII/密钥类别" multiple optionList={[
            { value: 'email', label: '邮箱' }, { value: 'phone', label: '电话号码' },
            { value: 'credit_card', label: '银行卡号（含校验）' }, { value: 'secret', label: 'API Key / Token / Secret' },
          ]} style={{ width: '100%' }} />
          <Form.Select field="action" label="命中后动作" optionList={[{ value: 'audit', label: '只审计，不改变内容' }, { value: 'redact', label: '脱敏后继续（仅支持可定位的 PII）' }, { value: 'block', label: '阻断，不交付内容' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Select field="mode" label="运行状态" optionList={[{ value: 'shadow', label: '影子观察（推荐先选）' }, { value: 'enforce', label: '正式执行' }, { value: 'disabled', label: '停用' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          <Form.Select field="failure_mode" label="检测器不可用时" optionList={[{ value: 'degraded_allow', label: '记录降级后继续（适合普通审计）' }, { value: 'fail_closed', label: '安全拒绝（适合强制规则）' }]} rules={[{ required: true }]} style={{ width: '100%' }} />
          {platform ? <Form.Select field="mandatory" label="是否为平台强制规则" optionList={[{ value: 'no', label: '否' }, { value: 'yes', label: '是（自动设为全局、正式执行、安全拒绝）' }]} rules={[{ required: true }]} style={{ width: '100%' }} /> : null}
          <Space style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 24 }}>
            <Button onClick={closeDrawer}>取消</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>{editing ? '保存并生成新版本' : '创建策略'}</Button>
          </Space>
        </Form>
      </SideSheet>
    </div>
  );
}
