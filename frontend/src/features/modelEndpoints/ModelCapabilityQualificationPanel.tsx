import '../../i18n/models';
import { translate as t, translateLabel, useI18n } from '../../i18n';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Banner, Button, Input, Modal, Space, Spin, Toast, Typography } from '@douyinfe/semi-ui';
import {
  modelDeploymentApi,
  type CanonicalCapabilityQualification,
  type CanonicalCredentialBinding,
  type CanonicalModelApplication,
  type CanonicalOperation,
} from '../../api/modelDeployments';
import { LatencyText, StatusTag, Surface } from '../../ui';
import { formatDateTime } from '../../utils/formatDate';
import { endpointError, endpointFailureText, type EndpointFailure } from './presentation';

const { Text } = Typography;

const streamDiagnosticStages = new Set([
  'sse', 'frame_json', 'normalization', 'model_identity', 'envelope',
  'usage', 'frame_shape', 'stream_state', 'terminal', 'read',
]);
const streamDiagnosticMembers = new Set([
  'unsupported_field', 'event_too_large', 'invalid_event', 'invalid_json',
  'identity', 'json', 'usage', 'tool_normalization', 'builtin_metering',
  'provider_envelope', 'cache_projection', 'unknown', 'mismatch',
  'invalid_shape', 'invalid_usage', 'invalid_transition', 'invalid_state',
  'missing_done', 'read_error', 'response_too_large',
]);

function probeFailureDetail(stage?: string, member?: string): string {
  if (!stage) return member ? t('（成员：{member}）', { member }) : '';
  if (stage === 'model_identity' && member === 'mismatch') return t("（响应模型身份与绑定不一致）");
  if (stage === 'sse' && member === 'unsupported_field') return t("（SSE 包含未支持字段）");
  return member && streamDiagnosticStages.has(stage) && streamDiagnosticMembers.has(member)
    ? t('（阶段：{stage}；原因：{member}）', { stage, member }) : '';
}

export type QualifiedCapabilityID = 'streaming' | 'reasoning' | 'reasoning_effort' | 'reasoning_budget' |
  'tools' | 'function_tools' | 'tool_choice_auto' | 'tool_choice_named' |
  'tool_choice_required' | 'json_object' | 'cache_read_usage' | 'prompt_cache';

const capabilityPresentation: Record<QualifiedCapabilityID, {
  title: string;
  shortName: string;
  description: string;
  defaultReason: string;
  emptyEvidence: string;
  probeContractSummary: string;
}> = {
  streaming: {
    title: '能力验证 · 流式输出（SSE）',
    shortName: '流式能力',
    description: '探测和批准是两个独立的不可变动作，但在同一页面连续完成；成功探测不会自动获得路由资格。',
    defaultReason: '已审阅当前环境的流式能力探测证据',
    emptyEvidence: '尚未运行 typed streaming probe。',
    probeContractSummary: '服务端固定使用 SSE、最终 usage 与 [DONE] 合同；不会接受任意测试 JSON，也不会自动批准路由资格。',
  },
  reasoning: {
    title: '能力验证 · 思考模式',
    shortName: '思考模式能力',
    description: '验证当前精确部署是否按服务端 typed contract 执行显式或供应商默认思考模式，并返回可审计但不泄露私有思维文本的 reasoning 证据。',
    defaultReason: '已审阅当前环境的 typed 思考模式探测证据',
    emptyEvidence: '尚未运行 typed reasoning probe。',
    probeContractSummary: '服务端按 exact Probe Contract 固定思考模式、精确答案、reasoning 存在性与最终 usage；不会保存私有思维文本，也不会由前端决定 wire 参数。',
  },
  reasoning_effort: {
    title: '能力验证 · 推理强度',
    shortName: '推理强度能力',
    description: '验证当前精确部署是否接受 typed reasoning_effort，并将供应商默认思考语义形成独立路由资格。',
    defaultReason: '已审阅当前环境的推理强度探测证据',
    emptyEvidence: '尚未运行 typed reasoning_effort probe。',
    probeContractSummary: '服务端按当前精确探测合同选择推理强度、流式模式和测试请求，并验证响应与最终用量；推理强度可显式设置或采用供应商默认值。',
  },
  reasoning_budget: {
    title: '能力验证 · 思考预算', shortName: '思考预算能力',
    description: '验证当前精确合同接受思考预算参数，并保留工具调用与最终用量。',
    defaultReason: '已审阅当前环境的思考预算探测证据',
    emptyEvidence: '尚未运行思考预算探测。',
    probeContractSummary: '服务端按精确合同限定总输出与思考预算，并验证工具调用、流式结束和最终用量；不把参数被接受解释为供应商逐 Token 严格计数保证。',
  },
  tools: {
    title: '能力验证 · Tools 请求面', shortName: 'Tools 请求面',
    description: '验证当前精确部署接受标准 function tools 请求，并返回受约束的工具调用。',
    defaultReason: '已审阅当前环境的 Tools 请求面探测证据', emptyEvidence: '尚未运行 typed tools probe。',
    probeContractSummary: '服务端固定声明一个无副作用测试函数并要求调用；不会执行函数，也不会接受任意工具定义。',
  },
  function_tools: {
    title: '能力验证 · Function Tools', shortName: 'Function Tools 能力',
    description: '验证当前精确部署的函数定义、参数 schema 与 tool_calls 响应合同。',
    defaultReason: '已审阅当前环境的 Function Tools 探测证据', emptyEvidence: '尚未运行 typed function_tools probe。',
    probeContractSummary: '服务端固定声明一个无副作用测试函数并校验函数名、参数 JSON、finish reason 与最终 usage。',
  },
  tool_choice_auto: {
    title: '能力验证 · tool_choice=auto', shortName: '自动工具选择能力',
    description: '验证当前精确部署在 auto 模式下可选择已声明函数。',
    defaultReason: '已审阅当前环境的 tool_choice=auto 探测证据', emptyEvidence: '尚未运行 typed tool_choice_auto probe。',
    probeContractSummary: '服务端固定发送 tool_choice=auto，并要求供应商选择唯一测试函数。',
  },
  tool_choice_named: {
    title: '能力验证 · Named tool_choice', shortName: '指定工具选择能力',
    description: '验证当前精确部署接受 OpenAI-compatible named tool_choice 对象。',
    defaultReason: '已审阅当前环境的 named tool_choice 探测证据', emptyEvidence: '尚未运行 typed tool_choice_named probe。',
    probeContractSummary: '服务端固定指定唯一测试函数；响应必须调用同名函数并返回合法参数 JSON。',
  },
  tool_choice_required: {
    title: '能力验证 · tool_choice=required', shortName: '强制工具选择能力',
    description: '验证当前精确部署在 required 模式下返回唯一受约束工具调用。',
    defaultReason: '已审阅当前环境的 tool_choice=required 探测证据', emptyEvidence: '尚未运行 typed tool_choice_required probe。',
    probeContractSummary: '服务端固定发送 tool_choice=required；响应必须是唯一测试函数调用。',
  },
  json_object: {
    title: '能力验证 · JSON Object', shortName: 'JSON Object 能力',
    description: '验证当前精确部署接受 response_format=json_object，并返回严格可解析的目标对象。',
    defaultReason: '已审阅当前环境的 JSON Object 探测证据', emptyEvidence: '尚未运行 typed json_object probe。',
    probeContractSummary: '服务端固定 JSON 提示和 response_format=json_object；响应必须是无重复字段的精确对象。',
  },
  cache_read_usage: {
    title: '能力验证 · 缓存读取计量', shortName: '缓存读取计量',
    description: '验证当前精确部署在缓存适用请求中明确报告缓存读取计数；显式零与未报告是不同证据。',
    defaultReason: '已审阅当前环境的缓存读取计量探测证据', emptyEvidence: '尚未运行 typed cache_read_usage probe。',
    probeContractSummary: '服务端固定缓存适用请求并校验供应商输入 token 门槛及缓存计数；显式零可证明计数报告，但不证明缓存命中。',
  },
  prompt_cache: {
    title: '能力验证 · Prompt Cache', shortName: 'Prompt Cache 能力',
    description: '验证当前精确部署在服务端固定请求中实际发生缓存命中；报告零计数不足以取得此资格。',
    defaultReason: '已审阅当前环境的 Prompt Cache 命中探测证据', emptyEvidence: '尚未运行 typed prompt_cache probe。',
    probeContractSummary: '服务端固定缓存适用请求并要求正的缓存命中计数；一次未命中不会自动重试，也不会由前端放宽资格。',
  },
};

export function capabilityShortName(value: string): string {
  return isQualifiedCapabilityID(value) ? translateLabel(capabilityPresentation[value].shortName) : value;
}

export function isQualifiedCapabilityID(value: string): value is QualifiedCapabilityID {
  return Object.prototype.hasOwnProperty.call(capabilityPresentation, value);
}

type Props = {
  selected: CanonicalModelApplication;
  operation: CanonicalOperation;
  canManage: boolean;
  canApprove: boolean;
  gatewayLicensed: boolean;
  gatewayRestriction: string;
  capabilityID?: QualifiedCapabilityID;
};

function capabilityTone(state: string) {
  if (state === 'approved_current') return 'success';
  if (state === 'probed_awaiting_approval') return 'warning';
  if (state === 'probe_failed' || state === 'probe_stale') return 'failed';
  return 'declared';
}

export default function ModelCapabilityQualificationPanel(props: Props) {
  const { selected, operation } = props;
  const capabilityID = props.capabilityID || 'streaming';
  useI18n();
  const presentation = capabilityPresentation[capabilityID];
  const [qualification, setQualification] = useState<CanonicalCapabilityQualification | null>(null);
  const [loading, setLoading] = useState(false);
  const [workingBindingID, setWorkingBindingID] = useState(0);
  const [approving, setApproving] = useState(false);
  const [error, setError] = useState<EndpointFailure | null>(null);
  const [reason, setReason] = useState(() => translateLabel(presentation.defaultReason));
  const deploymentID = selected.deployment.id;
  const operationID = operation.operation_id;
  const definitionDigest = operation.profile_definition_digest || '';
  const profileIdentityReady = !definitionDigest || Boolean(operation.execution_profile_id && operation.execution_profile_version);
  const executionProfileID = definitionDigest ? operation.execution_profile_id : operation.execution_profile_id || selected.public_model.execution_profile_id;
  const executionProfileVersion = definitionDigest ? operation.execution_profile_version : operation.execution_profile_version || selected.public_model.execution_profile_version;
  const executionProfile = `${executionProfileID || 'unavailable'}@${executionProfileVersion || 'unavailable'}`;
  const identity = [
    `connection=${selected.connection.id}@${selected.connection.config_revision}`,
    `deployment=${deploymentID}@${selected.deployment.config_revision}`,
    `operation=${operationID}@${operation.revision}`,
    `execution=${executionProfile}`,
    `definition=${definitionDigest}`,
    `protocol=${operation.protocol_profile_id}@${operation.protocol_profile_version}`,
    `capability=${capabilityID}`,
  ].join(':');
  const identityRef = useRef(identity);
  const loadGenerationRef = useRef(0);
  const confirmationRef = useRef<{ destroy?: () => void } | null>(null);
  identityRef.current = identity;
  const eligibleBindings = useMemo(() => selected.credential.bindings.filter(binding => binding.eligible), [selected.credential.bindings]);
  const targetSummary = useMemo(() => ({
    identity,
    deploymentID,
    operationID,
    capabilityID,
    connectionID: selected.connection.id,
    connectionRevision: selected.connection.config_revision,
    deploymentRevision: selected.deployment.config_revision,
    operationRevision: operation.revision,
    executionProfile,
    protocolProfile: `${operation.protocol_profile_id}@${operation.protocol_profile_version}`,
  }), [
    capabilityID,
    deploymentID,
    identity,
    operation.revision,
    operation.protocol_profile_id,
    operation.protocol_profile_version,
    operationID,
    selected.connection.config_revision,
    selected.connection.id,
    selected.deployment.config_revision,
    executionProfile,
  ]);

  const load = useCallback(async () => {
    const requestIdentity = identity;
    const generation = ++loadGenerationRef.current;
    setLoading(true);
    setError(null);
    try {
      const result = await modelDeploymentApi.getCapabilityQualification(
        deploymentID,
        operationID,
        capabilityID,
      );
      if (identityRef.current !== requestIdentity || loadGenerationRef.current !== generation) return;
      setQualification(result);
    } catch (loadError) {
      if (identityRef.current !== requestIdentity || loadGenerationRef.current !== generation) return;
      setQualification(null);
      setError({ cause: loadError, fallback: '无法读取{capability}资格。' });
    } finally {
      if (identityRef.current === requestIdentity && loadGenerationRef.current === generation) setLoading(false);
    }
  }, [capabilityID, deploymentID, identity, operationID, presentation.shortName]);

  useEffect(() => {
    confirmationRef.current?.destroy?.();
    confirmationRef.current = null;
    setQualification(null);
    setWorkingBindingID(0);
    setApproving(false);
    setReason(translateLabel(presentation.defaultReason));
    void load();
  }, [identity, load]);

  useEffect(() => () => {
    confirmationRef.current?.destroy?.();
    confirmationRef.current = null;
  }, []);

  const runProbe = async (binding: CanonicalCredentialBinding, target: typeof targetSummary) => {
    if (identityRef.current !== target.identity) return;
    setWorkingBindingID(binding.id);
    try {
      const result = await modelDeploymentApi.probeCapability(
        target.deploymentID,
        target.operationID,
        target.capabilityID,
        { credential_binding_id: binding.id },
      );
      if (identityRef.current !== target.identity) return;
      if (result.success && result.current) {
        Toast.success(t('{capability}探测通过；请审阅证据后再批准进入路由池', { capability: translateLabel(presentation.shortName) }));
      } else if (result.provider_success && result.stale) {
        Toast.warning(t('供应商{capability}响应成功，但配置已漂移；证据仅保留为历史记录', { capability: translateLabel(presentation.shortName) }));
      } else {
        const diagnostic = probeFailureDetail(result.diagnostic_stage, result.diagnostic_member);
        Toast.error(t('{capability}探测未通过：{reason}{diagnostic}', { capability: translateLabel(presentation.shortName), reason: result.contract_error_code || result.reason_code || result.provider_reason_code, diagnostic }));
      }
      await load();
    } catch (probeError) {
      if (identityRef.current !== target.identity) return;
      Toast.error(endpointError(probeError, t('{capability}探测失败', { capability: translateLabel(presentation.shortName) })));
    } finally {
      if (identityRef.current === target.identity) setWorkingBindingID(0);
    }
  };

  const confirmProbe = (binding: CanonicalCredentialBinding) => {
    if (!profileIdentityReady) return;
    const target = targetSummary;
    confirmationRef.current = Modal.confirm({
      title: t('运行{capability}探测？', { capability: translateLabel(presentation.shortName) }),
      content: <div>
        <p>{t("预计发起 1 次真实供应商调用，并计入已批准的测试调用上限。")}</p>
        <p>{t("供应商成本当前不可得；这不表示费用为 ¥0。")}</p>
        <p><Text code>Connection #{target.connectionID}@{target.connectionRevision} · Deployment #{target.deploymentID}@{target.deploymentRevision}</Text></p>
        <p><Text code>{target.operationID}@{target.operationRevision} · {target.executionProfile} · {target.protocolProfile}</Text></p>
        <p>{translateLabel(presentation.probeContractSummary)}</p>
      </div>,
      okText: t('运行{capability}探测（预计 1 次调用）', { capability: translateLabel(presentation.shortName) }),
      onOk: () => {
        confirmationRef.current = null;
        if (identityRef.current !== target.identity) return;
        return runProbe(binding, target);
      },
      onCancel: () => { confirmationRef.current = null; },
    });
  };

  const approve = async (target: typeof targetSummary, probeID: number, approvalReason: string) => {
    if (identityRef.current !== target.identity || !probeID || !approvalReason) return;
    setApproving(true);
    try {
      await modelDeploymentApi.approveCapability(
        target.deploymentID,
        target.operationID,
        target.capabilityID,
        { capability_probe_id: probeID, reason: approvalReason },
      );
      if (identityRef.current !== target.identity) return;
      Toast.success(t('已批准当前环境的精确{capability}进入路由池', { capability: translateLabel(presentation.shortName) }));
      await load();
    } catch (approvalError) {
      if (identityRef.current !== target.identity) return;
      Toast.error(endpointError(approvalError, t('{capability}批准失败；请刷新并重新核对证据', { capability: translateLabel(presentation.shortName) })));
    } finally {
      if (identityRef.current === target.identity) setApproving(false);
    }
  };

  const confirmApproval = () => {
    const target = targetSummary;
    const probeID = qualification?.latest_probe?.capability_probe_id || 0;
    const approvalReason = reason.trim();
    if (!probeID || !approvalReason) return;
    confirmationRef.current = Modal.confirm({
      title: t("批准在当前环境进入路由池？"),
      content: <div>
        <p>{t("此动作会追加不可变批准记录，并绑定当前 Deployment、Operation、Execution Profile、协议 Profile 与探测合同。")}</p>
        <p><Text code>Connection #{target.connectionID}@{target.connectionRevision} · Deployment #{target.deploymentID}@{target.deploymentRevision}</Text></p>
        <p><Text code>{target.operationID}@{target.operationRevision} · {target.executionProfile} · {target.protocolProfile}</Text></p>
        <p><Text code>Probe #{probeID}</Text></p>
        <p>{t("它不等于 released、SLA 已验证或长期稳定性证明。")}</p>
      </div>,
      okText: t("批准在当前环境进入路由池"),
      onOk: () => {
        confirmationRef.current = null;
        if (identityRef.current !== target.identity) return;
        return approve(target, probeID, approvalReason);
      },
      onCancel: () => { confirmationRef.current = null; },
    });
  };

  return <Surface
    title={translateLabel(presentation.title)}
    description={translateLabel(presentation.description)}
  >
    {loading && !qualification ? <Spin /> : <Space vertical align="start" style={{ width: '100%' }}>
      {error && <Banner type="danger" closeIcon={null} description={endpointFailureText({ ...error, values: { capability: translateLabel(presentation.shortName) } })} />}
      {!profileIdentityReady && <Banner type="warning" closeIcon={null} description={t("执行合同身份不完整；请刷新后再探测或批准。")} />}
      {qualification && <>
        <Space wrap>
          <StatusTag status={capabilityTone(qualification.state)}>{qualification.state}</StatusTag>
          <Text type="tertiary">qualification: {qualification.qualification_state}</Text>
          <Text code>{qualification.probe_contract_id}@{qualification.probe_contract_version}</Text>
        </Space>

        {qualification.qualification_state === 'probe_contract_stale' && <Banner
          type="warning"
          closeIcon={null}
          description={t("探测合同已升级，原批准保留为历史证据但不再具备当前路由资格；请按当前合同重新探测并批准。")}
        />}

        {qualification.latest_probe ? <div>
          <Space wrap>
            <Text strong>{t("最近证据")}</Text>
            <StatusTag status={qualification.latest_probe.success && qualification.latest_probe.current ? 'success' : qualification.latest_probe.current ? 'failed' : 'stale'}>
              {qualification.latest_probe.success && qualification.latest_probe.current ? t("当前探测通过") : qualification.latest_probe.current ? t("探测失败") : t("探测已失效")}
            </StatusTag>
            {qualification.latest_probe.latency_ms > 0 && <LatencyText value={qualification.latest_probe.latency_ms} />}
            <Text type="tertiary">HTTP {qualification.latest_probe.status_code || '-'} · {formatDateTime(qualification.latest_probe.tested_at)}</Text>
            {qualification.latest_probe.finish_reason && <Text>finish: {qualification.latest_probe.finish_reason}</Text>}
            {qualification.latest_probe.reasoning_observed && <Text>reasoning evidence: observed</Text>}
            {qualification.latest_probe.tool_call_observed && <Text>tool call evidence: observed</Text>}
            {qualification.latest_probe.structured_output_observed && <Text>structured output evidence: observed</Text>}
            {qualification.latest_probe.total_tokens !== undefined && <Text>usage: {qualification.latest_probe.prompt_tokens || 0} + {qualification.latest_probe.completion_tokens || 0} = {qualification.latest_probe.total_tokens}</Text>}
            {qualification.latest_probe.cache_read_counter_observed === true && qualification.latest_probe.cached_tokens !== undefined
              ? <Text>cache read usage: {qualification.latest_probe.cached_tokens} tokens</Text>
              : (capabilityID === 'cache_read_usage' || capabilityID === 'prompt_cache') && <Text>cache read usage: not_reported</Text>}
          </Space>
          {qualification.latest_probe.error_summary && <Text type="danger">{qualification.latest_probe.error_summary}</Text>}
          {qualification.latest_probe.contract_error_code && <Text type="danger">

            {t("合同校验：")}{qualification.latest_probe.contract_error_code}
            {probeFailureDetail(qualification.latest_probe.diagnostic_stage, qualification.latest_probe.diagnostic_member)}
          </Text>}
        </div> : <Text type="tertiary">{translateLabel(presentation.emptyEvidence)}</Text>}

        {qualification.qualified && <Banner
          type="success"
          closeIcon={null}
          description={t('已批准当前环境使用；approval_id={id}。这不是 released 或 SLA 证明。', { id: qualification.preferred_approval_id || '—' })}
        />}

        {props.canManage && qualification.probe_available && <Space vertical align="start" style={{ width: '100%' }}>
          {eligibleBindings.map(binding => <Button
            key={binding.id}
            type="primary"
            theme="borderless"
            disabled={!profileIdentityReady || !props.gatewayLicensed || workingBindingID !== 0 || approving}
            loading={workingBindingID === binding.id}
            title={props.gatewayLicensed ? undefined : props.gatewayRestriction}
            onClick={() => confirmProbe(binding)}
          >{t('运行{capability}探测（预计 1 次调用）', { capability: translateLabel(presentation.shortName) })} · binding #{binding.id}</Button>)}
          {eligibleBindings.length === 0 && <Banner type="warning" closeIcon={null} description={t("当前批准仍可保持有效，但没有可用 credential binding；路由健康与能力批准是两个独立状态。")} />}
        </Space>}

        {qualification.state === 'probed_awaiting_approval' && <Space vertical align="start" style={{ width: '100%' }}>
          {props.canApprove ? <>
            <Input
              aria-label={t('{capability}批准理由', { capability: translateLabel(presentation.shortName) })}
              value={reason}
              maxLength={512}
              onChange={setReason}
              disabled={approving}
            />
            <Button
              type="primary"
              loading={approving}
              disabled={!profileIdentityReady || !reason.trim() || !props.gatewayLicensed}
              title={props.gatewayLicensed ? undefined : props.gatewayRestriction}
              onClick={confirmApproval}
            >{t("批准在当前环境进入路由池")}</Button>
          </> : <Banner type="warning" closeIcon={null} description={t("探测已通过，等待平台管理员批准进入当前环境的路由池。")} />}
        </Space>}

        {!qualification.probe_available && <Banner type="warning" closeIcon={null} description={t('当前精确 Profile 没有可执行的 typed {capability} probe（{reason}）。', { capability: capabilityID, reason: qualification.probe_availability_reason_code })} />}
      </>}
    </Space>}
  </Surface>;
}
