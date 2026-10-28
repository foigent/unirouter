import '../../i18n/models';
import { translate as t, useI18n } from '../../i18n';
import { useState } from 'react';
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
import { upstreamScopeLabel } from './presentation';
import UpstreamReleaseObservationPanel from './UpstreamReleaseObservationPanel';
import ModelQualificationWorkspace from './ModelQualificationWorkspace';
import ModelClientSetup from './ModelClientSetup';
import ModelCapabilityQualificationPanel, { isQualifiedCapabilityID } from './ModelCapabilityQualificationPanel';

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
  onCanonicalStateChanged?: () => void | Promise<void>;
  onProbe: (binding: CanonicalCredentialBinding) => void;
  onInspectRelease: (releaseRef: string) => void;
  onOpenReleaseWorkspace: () => void;
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
  return t("未分配到任何 canonical runtime release");
}

export default function ModelEndpointDetailsSheet(props: Props) {
  useI18n();
  const { selected, operation } = props;
  const [detailsOpen, setDetailsOpen] = useState(false);
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
    width="min(800px, 100vw)"
  >
    {selected && operation && <div className="ur-model-endpoint-sheet">
      <UpstreamReleaseObservationPanel selected={selected} operation={operation} canManage={props.canManage} onChanged={props.onCanonicalStateChanged} />
      <div className="ur-model-endpoint-sheet-summary">
        <div>
          <Text type="tertiary">{t("精确持久身份")}</Text>
          <Text code>deployment_id={selected.deployment.id}</Text>
          <Text code>operation_id={operation.operation_id}</Text>
        </div>
        <div>
          <Text type="tertiary">{t("公开路径映射")}</Text>
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
        description={t("以下五个状态轴彼此独立：凭证可用不代表探测通过，探测通过不代表已发布，已发布也不代表某个项目或调用者已获授权。")}
      />

      <Surface title={t("1 · 配置与管理状态")} description={t("连接与 deployment 是两个有独立 revision 的持久对象；这里不从凭证或探测结果反推启停状态。")}>
        <Descriptions row size="small" data={[
          { key: t("公共模型"), value: `${selected.public_model.model_name} · catalog ${selected.public_model.lifecycle}` },
          { key: 'Deployment', value: `#${selected.deployment.id} · ${selected.deployment.upstream_model}` },
          { key: t("Deployment 管理状态"), value: <Space><StatusTag status={selected.deployment.admin_state}>{selected.deployment.admin_state}</StatusTag><Text type="tertiary">revision {selected.deployment.config_revision}</Text></Space> },
          { key: t("连接"), value: `#${selected.connection.id} · ${selected.connection.name} · ${selected.connection.provider}` },
          { key: t("连接管理状态"), value: <Space><StatusTag status={selected.connection.admin_state}>{selected.connection.admin_state}</StatusTag><Text type="tertiary">revision {selected.connection.config_revision}</Text></Space> },
          { key: t("协议与 Base URL"), value: `${selected.connection.adapter_type} · ${selected.connection.base_url}` },
          { key: t("Deployment 熔断"), value: `${selected.deployment.circuit_state} · revision ${selected.deployment.circuit_revision} · failures ${selected.deployment.fail_count}` },
        ]} />
      </Surface>

      <Surface title={t("2 · 凭证状态")} description={t("只表示此 deployment 当前是否绑定可选凭证；不会因此把 operation 标记为探测通过。")}>
        <Space vertical align="start" style={{ width: '100%' }}>
          <Space wrap>
            <StatusTag status={selected.credential.state}>{selected.credential.state}</StatusTag>
            <Text>{t('{eligible}/{bound} 个绑定当前 eligible', { eligible: selected.credential.eligible_count, bound: selected.credential.bound_count })}</Text>
            <Text type="tertiary">reason: {selected.credential.reason_code}</Text>
          </Space>
          {props.canManage && selected.credential.bindings.map(binding => <div key={binding.id} className="ur-canonical-binding-row">
            <Space wrap>
              <Text code>binding_id={binding.id}</Text>
              <StatusTag status={binding.status}>{binding.status}</StatusTag>
              <Text type="tertiary">revision {binding.state_revision}</Text>
              {binding.cooldown_until && <Text type="warning">{t('cooldown 至 {time}', { time: formatDateTime(binding.cooldown_until) })}</Text>}
              {probeAvailability.available && <Button
                type="primary"
                disabled={!binding.eligible || !props.gatewayLicensed || props.workingBindingID !== 0}
                loading={props.workingBindingID === binding.id}
                title={licensedActionTitle}
                onClick={() => props.onProbe(binding)}
              >{t("用此绑定探测精确 Operation")}</Button>}
            </Space>
          </div>)}
          {!props.canManage && <Text type="tertiary">{t("只读角色仅查看聚合凭证状态；不展示绑定身份，也不提供凭证或探测操作。")}</Text>}
          {props.canManage && !probeAvailability.available && <Banner
            type="warning"
            closeIcon={null}
            description={t('{operation} 当前不可发起 canonical production probe（{reason}）；本页不会调用旧 channel test 作为替代证据。', { operation: operationLabels[operation.operation_id] || operation.operation_id, reason: probeAvailability.reason })}
          />}
        </Space>
      </Surface>

      <Surface title={t("3 · Operation 支持与探测证据")} description={t("证据必须同时绑定当前 connection、deployment、operation、profile 与 credential revision。")}>
        <Descriptions row size="small" data={[
          { key: 'Operation', value: `${operationLabels[operation.operation_id] || operation.operation_id} · ${operation.operation_id}` },
          { key: t("支持声明"), value: <Space><StatusTag status={operation.support_state}>{operation.support_state}</StatusTag><Text type="tertiary">operation revision {operation.revision}</Text></Space> },
          { key: t("证据状态"), value: <Space><StatusTag status={operation.evidence_state}>{operation.evidence_state}</StatusTag><Text type="tertiary">{operation.evidence_ref || t("无 evidence_ref")}</Text></Space> },
          { key: t("绑定探测汇总"), value: t('{success} 当前成功 · {failed} 当前失败 · {stale} 已失效', { success: operation.current_success_count, failed: operation.current_failure_count, stale: operation.stale_probed_binding_count }) },
          { key: t("探测结论"), value: <Space><StatusTag status={operation.probe_state}>{operation.probe_state}</StatusTag><Text type="tertiary">{operation.probe_reason_code}</Text></Space> },
          { key: t("Exact probe 合同"), value: <Space><StatusTag status={probeAvailability.available ? 'ready' : 'missing'}>{probeAvailability.available ? 'available' : 'unavailable'}</StatusTag><Text type="tertiary">{probeAvailability.reason}</Text></Space> },
          { key: t("执行支持绑定"), value: operation.execution_support
            ? <Space wrap><Text code>{operation.execution_support.public_path}</Text><Text type="tertiary">executor {operation.execution_support.executor_contract_version}</Text></Space>
            : <Text type="tertiary">{t("当前 operation/profile 没有独立 execution-support binding")}</Text> },
          { key: t("生产资格"), value: operation.production_qualification
            ? <Space wrap><StatusTag status={operation.production_qualification.qualified ? 'ready' : operation.production_qualification.state}>{operation.production_qualification.state}</StatusTag>{operation.production_qualification.upstream_scope && <Text>{upstreamScopeLabel(operation.production_qualification.upstream_scope.state)}</Text>}<Text type="tertiary">{operation.production_qualification.required ? t("需要绑定当前 revision 的独立审批") : t("此 operation 不要求独立生产资格")}</Text></Space>
            : <Text type="tertiary">{t("旧服务端未投影 production qualification")}</Text> },
          { key: t("Operation 熔断"), value: `${operation.circuit_state} · revision ${operation.circuit_revision} · failures ${operation.circuit_fail_count}` },
        ]} />
        <div className="ur-model-endpoint-probe">
          <Text strong>{t("最近一次精确探测")}</Text>
          {!operation.latest_probe ? <Text type="tertiary">{t("尚无探测记录")}</Text> : <Space wrap>
            <StatusTag status={operation.latest_probe.current && operation.latest_probe.success ? 'success' : operation.latest_probe.current ? 'failed' : 'stale'}>
              {operation.latest_probe.current ? (operation.latest_probe.success ? t("当前成功") : t("当前失败")) : t("历史证据已失效")}
            </StatusTag>
            {operation.latest_probe.latency_ms > 0 && <LatencyText value={operation.latest_probe.latency_ms} />}
            <Text type="tertiary">HTTP {operation.latest_probe.status_code || '-'} · {formatDateTime(operation.latest_probe.tested_at)}</Text>
            {operation.latest_probe.reported_model && <Text>{t('供应商回报：{model}', { model: operation.latest_probe.reported_model })}</Text>}
            {operation.latest_probe.error_summary && <Text type="danger">{operation.latest_probe.error_summary}</Text>}
          </Space>}
        </div>
      </Surface>

      {operation.capability_probe_contracts.length > 0 && <>
          <ModelQualificationWorkspace key={`${selected.deployment.id}/${operation.operation_id}`}
            selected={selected} operation={operation} canManage={props.canManage}
            canApprove={props.canApproveCapabilities} gatewayLicensed={props.gatewayLicensed}/>
          <Button theme="borderless" onClick={() => setDetailsOpen(!detailsOpen)}>{detailsOpen ? t("收起详细能力验证") : t("查看详细能力验证")}</Button>
          {detailsOpen && operation.capability_probe_contracts.map(contract => isQualifiedCapabilityID(contract.capability_id) ? (
            <ModelCapabilityQualificationPanel
              key={contract.capability_id}
              selected={selected}
              operation={operation}
              canManage={props.canManage}
              canApprove={props.canApproveCapabilities}
              gatewayLicensed={props.gatewayLicensed}
              gatewayRestriction={props.gatewayRestriction}
              capabilityID={contract.capability_id}
            />
          ) : null)}
        </>}

      <Surface title="4 · Canonical Runtime" description={t("只展示不可变发布/slot 的当前归属；探测通过不会自动产生 runtime membership。")}>
        <Space vertical align="start">
          <Space wrap><StatusTag status={operation.runtime.state}>{operation.runtime.state}</StatusTag><Text>{runtimeMembershipSummary(operation)}</Text></Space>
          <Text type="tertiary">operation reason: {operation.runtime.reason_code} · application runtime: {selected.runtime.state} / {selected.runtime.reason_code}</Text>
          <Space wrap>
            {props.canManage && <Button onClick={props.onOpenReleaseWorkspace}>{t("前往发布工作台")}</Button>}
            {operation.runtime.stable_membership && <Button
              theme="borderless"
              onClick={() => props.onInspectRelease(operation.runtime.stable_membership!.release_ref)}
            >{t("查看 Stable Release")}</Button>}
            {operation.runtime.canary_membership && <Button
              theme="borderless"
              onClick={() => props.onInspectRelease(operation.runtime.canary_membership!.release_ref)}
            >{t("查看 Canary Release")}</Button>}
          </Space>
          <Banner type="info" closeIcon={null} description={t("发布编排和流量切换由 Canonical Release Workspace 执行，并以服务端 baseline、slot 与 action descriptor 为准；本详情不调用旧 channel/runtime-change writer。")} />
        </Space>
      </Surface>

      <Surface title={t("5 · 调用者可调用性")} description={t("当前 read contract 没有项目、应用、Grant 或 API Key 上下文，因此不能给出最终调用授权。")}>
        <Space wrap>
          <StatusTag status={selected.callability.state}>{selected.callability.state}</StatusTag>
          <Text>{selected.callability.evaluated ? t("已针对明确调用者评估") : t("未针对调用者评估")}</Text>
          <Text type="tertiary">reason: {selected.callability.reason_code}</Text>
        </Space>
      </Surface>

      <ModelClientSetup model={selected.public_model.model_name} active={Boolean(operation.runtime.stable_membership)} chat={operation.operation_id === 'openai.chat_completions.v1'}/>

      {props.canManage && !props.gatewayLicensed && <Banner
        type="warning"
        closeIcon={null}
        description={t('{reason} Canonical 状态仍可读取；创建与真实探测保持禁用。', { reason: props.gatewayRestriction || t('模型网关商业授权不可用。') })}
      />}
      {props.canManage && eligibleBindings.length === 0 && <Text type="tertiary">{t("当前没有 eligible credential binding，因此不能发起真实探测。")}</Text>}
    </div>}
  </SideSheet>;
}
