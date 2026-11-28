import { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Modal, Space, Typography } from '@douyinfe/semi-ui';
import { modelDeploymentApi, upstreamObservationConfirmation, type CanonicalDeployment, type CanonicalOperationID, type CanonicalOperationProbeResult } from '../../api/modelDeployments';
import { ApiError } from '../../api/transport';
import '../../i18n/models';
import { useI18n } from '../../i18n';

const { Text } = Typography;
// The async operation receipt advertises Retry-After: 15 while it is pending.
const readInterval = 15_000;

type Props = {
  deployment: CanonicalDeployment;
  operationID: CanonicalOperationID;
  bindingID: number;
  onVerified?: () => void | Promise<void>;
};

type ProbeState =
  | { stage: 'discovering' | 'ready' | 'submitting' }
  | { stage: 'receipt'; receipt: CanonicalOperationProbeResult }
  | { stage: 'unconfirmed'; ref?: string; conflict?: boolean; initial?: boolean };

export default function AsyncOperationProbe({ deployment, operationID, bindingID, onVerified }: Props) {
  const { t } = useI18n();
  const deploymentID = deployment.id;
  const observation = upstreamObservationConfirmation(deployment).expected_upstream_observation_identity;
  const [state, setState] = useState<ProbeState>({ stage: 'discovering' });
  const [reading, setReading] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const busy = useRef(false);
  const generation = useRef(0);

  const accept = useCallback((receipt: CanonicalOperationProbeResult, ref?: string) => {
    if (receipt?.deployment_id !== deploymentID || receipt.operation_id !== operationID
      || receipt.credential_binding_id !== bindingID
      || typeof receipt.probe_run_ref !== 'string' || !/^op_[a-f0-9]{32}$/.test(receipt.probe_run_ref)
      || (ref !== undefined && receipt.probe_run_ref !== ref)
      || typeof receipt.reason_code !== 'string'
      || (receipt.pending !== undefined && typeof receipt.pending !== 'boolean')
      || (receipt.pending === true
        ? receipt.success !== false || receipt.provider_success !== false
        : typeof receipt.success !== 'boolean' || typeof receipt.provider_success !== 'boolean'
          || typeof receipt.current !== 'boolean' || typeof receipt.stale !== 'boolean')) return false;
    setState({ stage: 'receipt', receipt });
    return true;
  }, [bindingID, deploymentID, operationID]);

  const read = useCallback(async (ref?: string, initial = false) => {
    if (busy.current) return;
    busy.current = true;
    const stamp = generation.current;
    setReading(true);
    try {
      const result = await modelDeploymentApi.readOperationProbe(deploymentID, operationID, {
        credential_binding_id: bindingID, expected_upstream_observation_identity: observation,
      }, ref);
      if (generation.current !== stamp) return;
      if (!accept(result, ref)) {
        setState({ stage: 'unconfirmed', ref, initial });
      } else if (ref && result.pending !== true && result.success === true && result.current === true && result.stale !== true) {
        void onVerified?.();
      }
    } catch (error) {
      if (generation.current !== stamp) return;
      if (initial && error instanceof ApiError && error.status === 404) {
        setState({ stage: 'ready' });
      } else {
        setState({ stage: 'unconfirmed', ref, initial, conflict: error instanceof ApiError && error.status === 409 });
      }
    } finally {
      if (generation.current === stamp) {
        busy.current = false;
        setReading(false);
      }
    }
  }, [accept, bindingID, deploymentID, observation, onVerified, operationID]);

  useEffect(() => {
    void read(undefined, true);
    return () => {
      generation.current += 1;
      busy.current = false;
    };
  }, [read]);

  useEffect(() => {
    if (state.stage !== 'receipt' || state.receipt.pending !== true) return;
    const timer = window.setTimeout(() => void read(state.receipt.probe_run_ref), readInterval);
    return () => window.clearTimeout(timer);
  }, [read, state]);

  const submit = async (stamp: number) => {
    busy.current = true;
    setState({ stage: 'submitting' });
    try {
      const result = await modelDeploymentApi.probe(deploymentID, operationID, {
        credential_binding_id: bindingID, ...upstreamObservationConfirmation(deployment),
      });
      if (generation.current !== stamp) return;
      if (accept(result)) {
        if (result.pending !== true && result.success && result.current && !result.stale) void onVerified?.();
        return;
      }
    } catch {
      // A missing response does not establish whether the supplier received the submission.
    } finally {
      if (generation.current === stamp) busy.current = false;
    }
    if (generation.current !== stamp) return;
    setState({ stage: 'unconfirmed' });
    await read();
  };

  if (state.stage === 'ready') {
    return <>
      <Button type="primary" onClick={() => setConfirming(true)}>{t("提交异步探测")}</Button>
      {confirming && <Modal
        visible
        title={t('探测 Deployment #{id} · {operation}？', { id: deploymentID, operation: operationID })}
        cancelText={t("取消")}
        okText={t("发起精确探测")}
        okButtonProps={{ 'aria-label': t("发起精确探测") }}
        onCancel={() => setConfirming(false)}
        onOk={() => {
          if (busy.current) return;
          setConfirming(false);
          void submit(generation.current);
        }}
      >
        <Space vertical align="start">
          <Text>{t("仅提交一次生成请求，可能产生供应商费用。后续查询和成品校验沿用同一任务。")}</Text>
          <Text>{t("可以离开此页，重新打开同一绑定后查看探测进度。")}</Text>
          <Text code>credential_binding_id={bindingID}</Text>
        </Space>
      </Modal>}
    </>;
  }
  if (state.stage === 'discovering' || state.stage === 'submitting') {
    return <Text role="status">{state.stage === 'discovering' ? t("正在查找原探测记录") : t("正在提交一次生成请求")}</Text>;
  }

  const receipt = state.stage === 'receipt' ? state.receipt : undefined;
  const ref = state.stage === 'unconfirmed' ? state.ref : receipt?.probe_run_ref;
  let message;
  if (state.stage === 'unconfirmed') {
    message = state.conflict ? t("无法核对本次探测，请检查凭证绑定和配置修订。") : t("探测状态未确认，请查询原记录。");
  } else if (receipt?.pending === true) {
    const reason = receipt.reason_code;
    if (reason === 'provider_task_submission_unconfirmed') {
      message = t("生成提交结果未确认，正在核对同一次探测。");
    } else if (['provider_task_accepted', 'provider_task_pending', 'provider_task_not_due', 'provider_task_status_available'].includes(reason)) {
      message = t("供应商已受理，等待同一任务的结果。");
    } else {
      message = t("结果尚未验证，将继续查询同一任务。");
    }
  } else if (receipt?.reason_code === 'provider_task_read_window_expired') {
    message = t("状态查询已到期，供应商结果未确认。");
  } else if (receipt?.reason_code === 'provider_task_unavailable') {
    message = t("供应商任务已不可查询，结果未确认。");
  } else if (receipt?.stale === true || receipt?.current === false) {
    message = t("配置已变化，此探测仅作为历史记录。");
  } else if (receipt?.success === true && receipt.current === true) {
    message = t("异步探测通过，成品已验证。");
  } else {
    message = t("异步探测失败。");
  }

  return <Space wrap>
    <Text role="status">{message}</Text>
    {ref && <Text code>{ref}</Text>}
    {(receipt?.pending === true || state.stage === 'unconfirmed') && (
      <Button loading={reading} disabled={reading} onClick={() => void read(ref, state.stage === 'unconfirmed' && state.initial)}>
        {ref ? t("查询本次探测") : t("查找原探测")}
      </Button>
    )}
  </Space>;
}
