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
import { endpointError } from './presentation';

const { Text } = Typography;
const streamingCapabilityID = 'streaming';

type Props = {
  selected: CanonicalModelApplication;
  operation: CanonicalOperation;
  canManage: boolean;
  canApprove: boolean;
  gatewayLicensed: boolean;
  gatewayRestriction: string;
};

function capabilityTone(state: string) {
  if (state === 'approved_current') return 'success';
  if (state === 'probed_awaiting_approval') return 'warning';
  if (state === 'probe_failed' || state === 'probe_stale') return 'failed';
  return 'declared';
}

export default function ModelCapabilityQualificationPanel(props: Props) {
  const { selected, operation } = props;
  const [qualification, setQualification] = useState<CanonicalCapabilityQualification | null>(null);
  const [loading, setLoading] = useState(false);
  const [workingBindingID, setWorkingBindingID] = useState(0);
  const [approving, setApproving] = useState(false);
  const [error, setError] = useState('');
  const [reason, setReason] = useState('已审阅当前环境的流式能力探测证据');
  const deploymentID = selected.deployment.id;
  const operationID = operation.operation_id;
  const identity = [
    `connection=${selected.connection.id}@${selected.connection.config_revision}`,
    `deployment=${deploymentID}@${selected.deployment.config_revision}`,
    `operation=${operationID}@${operation.revision}`,
    `execution=${selected.public_model.execution_profile_id}@${selected.public_model.execution_profile_version}`,
    `protocol=${operation.protocol_profile_id}@${operation.protocol_profile_version}`,
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
    connectionID: selected.connection.id,
    connectionRevision: selected.connection.config_revision,
    deploymentRevision: selected.deployment.config_revision,
    operationRevision: operation.revision,
    executionProfile: `${selected.public_model.execution_profile_id}@${selected.public_model.execution_profile_version}`,
    protocolProfile: `${operation.protocol_profile_id}@${operation.protocol_profile_version}`,
  }), [
    deploymentID,
    identity,
    operation.revision,
    operation.protocol_profile_id,
    operation.protocol_profile_version,
    operationID,
    selected.connection.config_revision,
    selected.connection.id,
    selected.deployment.config_revision,
    selected.public_model.execution_profile_id,
    selected.public_model.execution_profile_version,
  ]);

  const load = useCallback(async () => {
    const requestIdentity = identity;
    const generation = ++loadGenerationRef.current;
    setLoading(true);
    setError('');
    try {
      const result = await modelDeploymentApi.getCapabilityQualification(
        deploymentID,
        operationID,
        streamingCapabilityID,
      );
      if (identityRef.current !== requestIdentity || loadGenerationRef.current !== generation) return;
      setQualification(result);
    } catch (loadError) {
      if (identityRef.current !== requestIdentity || loadGenerationRef.current !== generation) return;
      setQualification(null);
      setError(endpointError(loadError, '无法读取流式能力资格。'));
    } finally {
      if (identityRef.current === requestIdentity && loadGenerationRef.current === generation) setLoading(false);
    }
  }, [deploymentID, identity, operationID]);

  useEffect(() => {
    confirmationRef.current?.destroy?.();
    confirmationRef.current = null;
    setQualification(null);
    setWorkingBindingID(0);
    setApproving(false);
    setReason('已审阅当前环境的流式能力探测证据');
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
        streamingCapabilityID,
        { credential_binding_id: binding.id },
      );
      if (identityRef.current !== target.identity) return;
      if (result.success && result.current) {
        Toast.success('流式能力探测通过；请审阅证据后再批准进入路由池');
      } else if (result.provider_success && result.stale) {
        Toast.warning('供应商流式响应成功，但配置已漂移；证据仅保留为历史记录');
      } else {
        Toast.error(`流式能力探测未通过：${result.contract_error_code || result.reason_code || result.provider_reason_code}`);
      }
      await load();
    } catch (probeError) {
      if (identityRef.current !== target.identity) return;
      Toast.error(endpointError(probeError, '流式能力探测失败'));
    } finally {
      if (identityRef.current === target.identity) setWorkingBindingID(0);
    }
  };

  const confirmProbe = (binding: CanonicalCredentialBinding) => {
    const target = targetSummary;
    confirmationRef.current = Modal.confirm({
      title: '运行流式能力探测？',
      content: <div>
        <p>预计发起 1 次真实供应商调用，并计入已批准的测试调用上限。</p>
        <p>供应商成本当前不可得；这不表示费用为 ¥0。</p>
        <p><Text code>Connection #{target.connectionID}@{target.connectionRevision} · Deployment #{target.deploymentID}@{target.deploymentRevision}</Text></p>
        <p><Text code>{target.operationID}@{target.operationRevision} · {target.executionProfile} · {target.protocolProfile}</Text></p>
        <p>服务端固定使用 SSE、最终 usage 与 [DONE] 合同；不会接受任意测试 JSON，也不会自动批准路由资格。</p>
      </div>,
      okText: '运行流式能力探测（预计 1 次调用）',
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
        streamingCapabilityID,
        { capability_probe_id: probeID, reason: approvalReason },
      );
      if (identityRef.current !== target.identity) return;
      Toast.success('已批准当前环境的精确流式能力进入路由池');
      await load();
    } catch (approvalError) {
      if (identityRef.current !== target.identity) return;
      Toast.error(endpointError(approvalError, '流式能力批准失败；请刷新并重新核对证据'));
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
      title: '批准在当前环境进入路由池？',
      content: <div>
        <p>此动作会追加不可变批准记录，并绑定当前 Deployment、Operation、Execution Profile、协议 Profile 与探测合同。</p>
        <p><Text code>Connection #{target.connectionID}@{target.connectionRevision} · Deployment #{target.deploymentID}@{target.deploymentRevision}</Text></p>
        <p><Text code>{target.operationID}@{target.operationRevision} · {target.executionProfile} · {target.protocolProfile}</Text></p>
        <p><Text code>Probe #{probeID}</Text></p>
        <p>它不等于 released、SLA 已验证或长期稳定性证明。</p>
      </div>,
      okText: '批准在当前环境进入路由池',
      onOk: () => {
        confirmationRef.current = null;
        if (identityRef.current !== target.identity) return;
        return approve(target, probeID, approvalReason);
      },
      onCancel: () => { confirmationRef.current = null; },
    });
  };

  return <Surface
    title="能力验证 · 流式输出（SSE）"
    description="探测和批准是两个独立的不可变动作，但在同一页面连续完成；成功探测不会自动获得路由资格。"
  >
    {loading && !qualification ? <Spin /> : <Space vertical align="start" style={{ width: '100%' }}>
      {error && <Banner type="danger" closeIcon={null} description={error} />}
      {qualification && <>
        <Space wrap>
          <StatusTag status={capabilityTone(qualification.state)}>{qualification.state}</StatusTag>
          <Text type="tertiary">qualification: {qualification.qualification_state}</Text>
          <Text code>{qualification.probe_contract_id}@{qualification.probe_contract_version}</Text>
        </Space>

        {qualification.latest_probe ? <div>
          <Space wrap>
            <Text strong>最近证据</Text>
            <StatusTag status={qualification.latest_probe.success && qualification.latest_probe.current ? 'success' : qualification.latest_probe.current ? 'failed' : 'stale'}>
              {qualification.latest_probe.success && qualification.latest_probe.current ? '当前探测通过' : qualification.latest_probe.current ? '探测失败' : '探测已失效'}
            </StatusTag>
            {qualification.latest_probe.latency_ms > 0 && <LatencyText value={qualification.latest_probe.latency_ms} />}
            <Text type="tertiary">HTTP {qualification.latest_probe.status_code || '-'} · {formatDateTime(qualification.latest_probe.tested_at)}</Text>
            {qualification.latest_probe.finish_reason && <Text>finish: {qualification.latest_probe.finish_reason}</Text>}
            {qualification.latest_probe.total_tokens !== undefined && <Text>usage: {qualification.latest_probe.prompt_tokens || 0} + {qualification.latest_probe.completion_tokens || 0} = {qualification.latest_probe.total_tokens}</Text>}
          </Space>
          {qualification.latest_probe.error_summary && <Text type="danger">{qualification.latest_probe.error_summary}</Text>}
          {qualification.latest_probe.contract_error_code && <Text type="danger">合同校验：{qualification.latest_probe.contract_error_code}</Text>}
        </div> : <Text type="tertiary">尚未运行 typed streaming probe。</Text>}

        {qualification.qualified && <Banner
          type="success"
          closeIcon={null}
          description={`已批准当前环境使用；approval_id=${qualification.preferred_approval_id || '-'}。这不是 released 或 SLA 证明。`}
        />}

        {props.canManage && qualification.probe_available && <Space vertical align="start" style={{ width: '100%' }}>
          {eligibleBindings.map(binding => <Button
            key={binding.id}
            type="primary"
            theme="borderless"
            disabled={!props.gatewayLicensed || workingBindingID !== 0 || approving}
            loading={workingBindingID === binding.id}
            title={props.gatewayLicensed ? undefined : props.gatewayRestriction}
            onClick={() => confirmProbe(binding)}
          >运行流式能力探测（预计 1 次调用） · binding #{binding.id}</Button>)}
          {eligibleBindings.length === 0 && <Banner type="warning" closeIcon={null} description="当前批准仍可保持有效，但没有可用 credential binding；路由健康与能力批准是两个独立状态。" />}
        </Space>}

        {qualification.state === 'probed_awaiting_approval' && <Space vertical align="start" style={{ width: '100%' }}>
          {props.canApprove ? <>
            <Input
              aria-label="流式能力批准理由"
              value={reason}
              maxLength={512}
              onChange={setReason}
              disabled={approving}
            />
            <Button
              type="primary"
              loading={approving}
              disabled={!reason.trim() || !props.gatewayLicensed}
              title={props.gatewayLicensed ? undefined : props.gatewayRestriction}
              onClick={confirmApproval}
            >批准在当前环境进入路由池</Button>
          </> : <Banner type="warning" closeIcon={null} description="探测已通过，等待平台管理员批准进入当前环境的路由池。" />}
        </Space>}

        {!qualification.probe_available && <Banner type="warning" closeIcon={null} description={`当前精确 Profile 没有可执行的 typed streaming probe（${qualification.probe_availability_reason_code}）。`} />}
      </>}
    </Space>}
  </Surface>;
}
