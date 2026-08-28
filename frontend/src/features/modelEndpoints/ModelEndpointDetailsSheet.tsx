import { Banner, Button, Descriptions, SideSheet, Space, Typography } from '@douyinfe/semi-ui';
import {
  canonicalOperationProbeAvailability,
  canonicalOperationPaths,
  type CanonicalCredentialBinding,
  type CanonicalModelApplication,
  type CanonicalOperation,
} from '../../api/modelDeployments';
import { LatencyText, StatusTag, Surface } from '../../ui';
import { formatDateTime } from '../../utils/formatDate';
import ModelCapabilityQualificationPanel from './ModelCapabilityQualificationPanel';

const { Text } = Typography;

type Props = {
  selected?: CanonicalModelApplication;
  operation?: CanonicalOperation;
  canManage: boolean;
  canApproveCapabilities: boolean;
  gatewayLicensed: boolean;
  gatewayRestriction: string;
  workingBindingID: number;
  onClose: () => void;
  onProbe: (binding: CanonicalCredentialBinding) => void;
  onInspectRelease: (releaseRef: string) => void;
};

const operationLabels: Record<string, string> = {
  'openai.chat_completions.v1': 'Chat Completions',
  'openai.embeddings.v1': 'Embeddings',
  'openai.responses.v1': 'Responses',
  'anthropic.messages.v1': 'Anthropic Messages',
};

function runtimeMembershipSummary(operation: CanonicalOperation) {
  const stable = operation.runtime.stable_membership;
  const canary = operation.runtime.canary_membership;
  if (stable && canary) return `stable ${stable.release_ref} + canary ${canary.release_ref}`;
  if (canary) return `canary ${canary.release_ref}`;
  if (stable) return `stable ${stable.release_ref}`;
  return '未分配到任何 canonical runtime release';
}

export default function ModelEndpointDetailsSheet(props: Props) {
  const { selected, operation } = props;
  const visible = Boolean(selected && operation);
  const probeAvailability = operation ? canonicalOperationProbeAvailability(operation) : { available: false, reason: '', source: 'server' as const };
  const eligibleBindings = selected?.credential.bindings.filter(binding => binding.eligible) || [];
  const licensedActionTitle = props.gatewayLicensed ? undefined : props.gatewayRestriction;

  return <SideSheet
    title={selected && operation
      ? `${selected.public_model.display_name || selected.public_model.model_name} · Deployment #${selected.deployment.id}`
      : 'Canonical Deployment'}
    visible={visible}
    onCancel={props.onClose}
    width={800}
  >
    {selected && operation && <div className="ur-model-endpoint-sheet">
      <div className="ur-model-endpoint-sheet-summary">
        <div>
          <Text type="tertiary">精确持久身份</Text>
          <Text code>deployment_id={selected.deployment.id}</Text>
          <Text code>operation_id={operation.operation_id}</Text>
        </div>
        <div>
          <Text type="tertiary">公开路径映射</Text>
          <Text code>{operation.execution_support?.public_path || canonicalOperationPaths[operation.operation_id] || 'unknown'}</Text>
        </div>
        <div>
          <Text type="tertiary">Profile</Text>
          <Text code>{operation.protocol_profile_id}@{operation.protocol_profile_version}</Text>
        </div>
      </div>

      <Banner
        type="info"
        closeIcon={null}
        description="以下五个状态轴彼此独立：凭证可用不代表探测通过，探测通过不代表已发布，已发布也不代表某个项目或调用者已获授权。"
      />

      <Surface title="1 · 配置与管理状态" description="连接与 deployment 是两个有独立 revision 的持久对象；这里不从凭证或探测结果反推启停状态。">
        <Descriptions row size="small" data={[
          { key: '公共模型', value: `${selected.public_model.model_name} · catalog ${selected.public_model.lifecycle}` },
          { key: 'Deployment', value: `#${selected.deployment.id} · ${selected.deployment.upstream_model}` },
          { key: 'Deployment 管理状态', value: <Space><StatusTag status={selected.deployment.admin_state}>{selected.deployment.admin_state}</StatusTag><Text type="tertiary">revision {selected.deployment.config_revision}</Text></Space> },
          { key: '连接', value: `#${selected.connection.id} · ${selected.connection.name} · ${selected.connection.provider}` },
          { key: '连接管理状态', value: <Space><StatusTag status={selected.connection.admin_state}>{selected.connection.admin_state}</StatusTag><Text type="tertiary">revision {selected.connection.config_revision}</Text></Space> },
          { key: '协议与 Base URL', value: `${selected.connection.adapter_type} · ${selected.connection.base_url}` },
          { key: 'Deployment 熔断', value: `${selected.deployment.circuit_state} · revision ${selected.deployment.circuit_revision} · failures ${selected.deployment.fail_count}` },
        ]} />
      </Surface>

      <Surface title="2 · 凭证状态" description="只表示此 deployment 当前是否绑定可选凭证；不会因此把 operation 标记为探测通过。">
        <Space vertical align="start" style={{ width: '100%' }}>
          <Space wrap>
            <StatusTag status={selected.credential.state}>{selected.credential.state}</StatusTag>
            <Text>{selected.credential.eligible_count}/{selected.credential.bound_count} 个绑定当前 eligible</Text>
            <Text type="tertiary">reason: {selected.credential.reason_code}</Text>
          </Space>
          {props.canManage && selected.credential.bindings.map(binding => <div key={binding.id} className="ur-canonical-binding-row">
            <Space wrap>
              <Text code>binding_id={binding.id}</Text>
              <StatusTag status={binding.status}>{binding.status}</StatusTag>
              <Text type="tertiary">revision {binding.state_revision}</Text>
              {binding.cooldown_until && <Text type="warning">cooldown 至 {formatDateTime(binding.cooldown_until)}</Text>}
              {probeAvailability.available && <Button
                type="primary"
                disabled={!binding.eligible || !props.gatewayLicensed || props.workingBindingID !== 0}
                loading={props.workingBindingID === binding.id}
                title={licensedActionTitle}
                onClick={() => props.onProbe(binding)}
              >用此绑定探测精确 Operation</Button>}
            </Space>
          </div>)}
          {!props.canManage && <Text type="tertiary">只读角色仅查看聚合凭证状态；不展示绑定身份，也不提供凭证或探测操作。</Text>}
          {props.canManage && !probeAvailability.available && <Banner
            type="warning"
            closeIcon={null}
            description={`${operationLabels[operation.operation_id] || operation.operation_id} 当前不可发起 canonical production probe（${probeAvailability.reason}）；本页不会调用旧 channel test 作为替代证据。`}
          />}
        </Space>
      </Surface>

      <Surface title="3 · Operation 支持与探测证据" description="证据必须同时绑定当前 connection、deployment、operation、profile 与 credential revision。">
        <Descriptions row size="small" data={[
          { key: 'Operation', value: `${operationLabels[operation.operation_id] || operation.operation_id} · ${operation.operation_id}` },
          { key: '支持声明', value: <Space><StatusTag status={operation.support_state}>{operation.support_state}</StatusTag><Text type="tertiary">operation revision {operation.revision}</Text></Space> },
          { key: '证据状态', value: <Space><StatusTag status={operation.evidence_state}>{operation.evidence_state}</StatusTag><Text type="tertiary">{operation.evidence_ref || '无 evidence_ref'}</Text></Space> },
          { key: '绑定探测汇总', value: `${operation.current_success_count} 当前成功 · ${operation.current_failure_count} 当前失败 · ${operation.stale_probed_binding_count} 已失效` },
          { key: '探测结论', value: <Space><StatusTag status={operation.probe_state}>{operation.probe_state}</StatusTag><Text type="tertiary">{operation.probe_reason_code}</Text></Space> },
          { key: 'Exact probe 合同', value: <Space><StatusTag status={probeAvailability.available ? 'ready' : 'missing'}>{probeAvailability.available ? 'available' : 'unavailable'}</StatusTag><Text type="tertiary">{probeAvailability.reason}</Text></Space> },
          { key: '执行支持绑定', value: operation.execution_support
            ? <Space wrap><Text code>{operation.execution_support.public_path}</Text><Text type="tertiary">executor {operation.execution_support.executor_contract_version}</Text></Space>
            : <Text type="tertiary">当前 operation/profile 没有独立 execution-support binding</Text> },
          { key: '生产资格', value: operation.production_qualification
            ? <Space wrap><StatusTag status={operation.production_qualification.qualified ? 'ready' : operation.production_qualification.state}>{operation.production_qualification.state}</StatusTag><Text type="tertiary">{operation.production_qualification.required ? '需要绑定当前 revision 的独立审批' : '此 operation 不要求独立生产资格'}</Text></Space>
            : <Text type="tertiary">旧服务端未投影 production qualification</Text> },
          { key: 'Operation 熔断', value: `${operation.circuit_state} · revision ${operation.circuit_revision} · failures ${operation.circuit_fail_count}` },
        ]} />
        <div className="ur-model-endpoint-probe">
          <Text strong>最近一次精确探测</Text>
          {!operation.latest_probe ? <Text type="tertiary">尚无探测记录</Text> : <Space wrap>
            <StatusTag status={operation.latest_probe.current && operation.latest_probe.success ? 'success' : operation.latest_probe.current ? 'failed' : 'stale'}>
              {operation.latest_probe.current ? (operation.latest_probe.success ? '当前成功' : '当前失败') : '历史证据已失效'}
            </StatusTag>
            {operation.latest_probe.latency_ms > 0 && <LatencyText value={operation.latest_probe.latency_ms} />}
            <Text type="tertiary">HTTP {operation.latest_probe.status_code || '-'} · {formatDateTime(operation.latest_probe.tested_at)}</Text>
            {operation.latest_probe.reported_model && <Text>供应商回报：{operation.latest_probe.reported_model}</Text>}
            {operation.latest_probe.error_summary && <Text type="danger">{operation.latest_probe.error_summary}</Text>}
          </Space>}
        </div>
      </Surface>

      {operation.operation_id === 'openai.chat_completions.v1' &&
        selected.public_model.execution_profile_id === 'deepseek.v4-flash.api' &&
        ['1.4.0', '1.5.0'].includes(selected.public_model.execution_profile_version) && <>
          {(['streaming', 'reasoning', 'reasoning_effort', 'tools', 'function_tools',
            'tool_choice_auto', 'tool_choice_named', 'tool_choice_required', 'json_object'] as const).map(capabilityID => (
            <ModelCapabilityQualificationPanel
              key={capabilityID}
              selected={selected}
              operation={operation}
              canManage={props.canManage}
              canApprove={props.canApproveCapabilities}
              gatewayLicensed={props.gatewayLicensed}
              gatewayRestriction={props.gatewayRestriction}
              capabilityID={capabilityID}
            />
          ))}
        </>}

      <Surface title="4 · Canonical Runtime" description="只展示不可变发布/slot 的当前归属；探测通过不会自动产生 runtime membership。">
        <Space vertical align="start">
          <Space wrap><StatusTag status={operation.runtime.state}>{operation.runtime.state}</StatusTag><Text>{runtimeMembershipSummary(operation)}</Text></Space>
          <Text type="tertiary">operation reason: {operation.runtime.reason_code} · application runtime: {selected.runtime.state} / {selected.runtime.reason_code}</Text>
          <Space wrap>
            {operation.runtime.stable_membership && <Button
              theme="borderless"
              onClick={() => props.onInspectRelease(operation.runtime.stable_membership!.release_ref)}
            >查看 Stable Release</Button>}
            {operation.runtime.canary_membership && <Button
              theme="borderless"
              onClick={() => props.onInspectRelease(operation.runtime.canary_membership!.release_ref)}
            >查看 Canary Release</Button>}
          </Space>
          <Banner type="info" closeIcon={null} description="发布编排和流量切换由 Canonical Release Workspace 执行，并以服务端 baseline、slot 与 action descriptor 为准；本详情不调用旧 channel/runtime-change writer。" />
        </Space>
      </Surface>

      <Surface title="5 · 调用者可调用性" description="当前 read contract 没有项目、应用、Grant 或 API Key 上下文，因此不能给出最终调用授权。">
        <Space wrap>
          <StatusTag status={selected.callability.state}>{selected.callability.state}</StatusTag>
          <Text>{selected.callability.evaluated ? '已针对明确调用者评估' : '未针对调用者评估'}</Text>
          <Text type="tertiary">reason: {selected.callability.reason_code}</Text>
        </Space>
      </Surface>

      {props.canManage && !props.gatewayLicensed && <Banner
        type="warning"
        closeIcon={null}
        description={`${props.gatewayRestriction || '模型网关商业授权不可用。'} Canonical 状态仍可读取；创建与真实探测保持禁用。`}
      />}
      {props.canManage && eligibleBindings.length === 0 && <Text type="tertiary">当前没有 eligible credential binding，因此不能发起真实探测。</Text>}
    </div>}
  </SideSheet>;
}
