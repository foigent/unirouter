import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Banner,
  Button,
  Descriptions,
  Input,
  Modal,
  Select,
  SideSheet,
  Space,
  Table,
  Tag,
  TextArea,
  Toast,
  Typography,
} from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import {
  modelDeploymentLifecycleApi,
  type CanonicalDeploymentLifecycleActionDescriptor,
  type CanonicalDeploymentLifecycleBaseRequest,
  type CanonicalDeploymentLegacyLifecycleRequest,
  type CanonicalDeploymentLifecycleReceipt,
  type CanonicalDeploymentLifecycleSlotRequest,
  type CanonicalDeploymentLifecycleView,
  type CanonicalDeploymentStartCanaryRequest,
} from '../../api/modelDeploymentLifecycle';
import {
  modelDeploymentReleaseApi,
  type CanonicalDeploymentReleaseAuthoringReceipt,
  type CanonicalDeploymentReleaseAuthoringRequest,
  type CanonicalDeploymentReleaseAuthoringOption,
  type CanonicalDeploymentReleaseCollection,
  type CanonicalDeploymentReleaseSelectionOption,
} from '../../api/modelDeploymentReleases';
import {
  modelSuccessorCutoverApi,
  type StagingModelSuccessorCutoverReceipt,
  type StagingModelSuccessorCutoverRequest,
} from '../../api/modelSuccessorCutover';
import { ApiError } from '../../api/transport';
import { EmptyState, StatusTag, Surface } from '../../ui';
import { formatDateTime } from '../../utils/formatDate';

const { Text } = Typography;

type Props = {
  canManage: boolean;
  selectedReleaseRef?: string;
  onSelectRelease: (releaseRef: string) => void;
  onCanonicalStateChanged: () => Promise<void> | void;
};

type MutationIntent = { fingerprint: string; idempotencyKey: string };
type StagingSuccessorMutationIntent = MutationIntent & { input: StagingModelSuccessorCutoverRequest };

type ReceiptState =
  | { kind: 'authoring'; value: CanonicalDeploymentReleaseAuthoringReceipt }
  | { kind: 'lifecycle'; value: CanonicalDeploymentLifecycleReceipt }
  | { kind: 'staging_successor'; value: StagingModelSuccessorCutoverReceipt };

const actionLabels: Record<CanonicalDeploymentLifecycleActionDescriptor['action'], string> = {
  activate_initial: '首次启用',
  start_canary: '启动灰度',
  promote: '提升为稳定',
  cancel_canary: '取消灰度',
  rollback_stable: '回滚稳定版本',
  cutover_legacy: '切换至 Canonical V2',
  rollback_legacy: '紧急恢复 Legacy V1',
};

function newIdempotencyKey(scope: 'author' | 'lifecycle') {
  const random = globalThis.crypto?.randomUUID?.()
    || `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
  return `ui-model-release-${scope}-${random}`;
}

function selectionKey(selection: CanonicalDeploymentReleaseSelectionOption) {
  return `${selection.deployment_id}:${selection.operation_id}`;
}

function validPublicLimit(value: number) {
  return Number.isSafeInteger(value) && value > 0;
}

function errorText(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback;
}

function apiErrorCode(error: unknown) {
  if (!(error instanceof ApiError) || !error.payload || typeof error.payload !== 'object') return '';
  const code = (error.payload as { code?: unknown }).code;
  return typeof code === 'string' ? code : '';
}

function shouldRotateIntent(error: unknown) {
  return error instanceof ApiError && error.status >= 400 && error.status < 500;
}

function optionalIdentity(value?: number) {
  return value === undefined ? '未分配' : String(value);
}

function stageTone(stage: string) {
  if (stage === 'active') return 'success';
  if (stage === 'canary' || stage === 'validated') return 'warning';
  if (stage === 'rolled_back' || stage === 'rejected') return 'disabled';
  return stage;
}

function AuthoringReceiptPanel({ receipt }: { receipt: CanonicalDeploymentReleaseAuthoringReceipt }) {
  return <Surface
    title={`原子发布编排收据 #${receipt.receipt_id}`}
    description="收据冻结了服务端接受的 baseline、selection 与 sealed candidate；authoring 本身不激活流量。"
  >
    <Descriptions row size="small" data={[
      { key: 'Release', value: <Text code>{receipt.release.release_ref}</Text> },
      { key: '结果', value: <Space><StatusTag status={receipt.release.stage}>{receipt.release.stage}</StatusTag><Text>version {receipt.release.version}</Text></Space> },
      { key: 'Baseline', value: <Text code>{receipt.baseline.kind} · slot {receipt.baseline.expected_slot_version}</Text> },
      { key: '幂等结论', value: receipt.idempotency_replay ? '安全重放原收据' : '首次完成' },
      { key: '原始 HTTP / Location', value: <Text code>{receipt.original_http_status} · {receipt.original_location}</Text> },
      { key: 'Route items', value: `${receipt.release.route_items_count} · ${receipt.release.route_items_hash}` },
      { key: '完成时间', value: formatDateTime(receipt.completed_at) },
    ]} />
    <div className="ur-model-endpoint-probe">
      <Text strong>冻结的 Selection</Text>
      {receipt.selections.map(selection => <Text code key={`${selection.deployment_id}:${selection.operation_id}`}>
        deployment_id={selection.deployment_id} · {selection.operation_id}
      </Text>)}
    </div>
  </Surface>;
}

function LifecycleReceiptPanel({ receipt }: { receipt: CanonicalDeploymentLifecycleReceipt }) {
  const isLegacyIdentityTransition = receipt.action === 'cutover_legacy' || receipt.action === 'rollback_legacy';
  return <Surface
    title={`Lifecycle 收据 #${receipt.receipt_id}`}
    description="展示本次 CAS 的 before/after slot、release 状态和资格快照；重放不会把当前状态冒充为历史执行时状态。"
  >
    <Descriptions row size="small" data={[
      { key: 'Action', value: <Text code>{receipt.action}</Text> },
      { key: 'Release', value: <Text code>{receipt.target_release_ref}</Text> },
      { key: '幂等结论', value: receipt.idempotency_replay ? '安全重放原收据' : '首次完成' },
      { key: 'Slot version', value: `${receipt.slot.before_version} → ${receipt.slot.after_version}` },
      { key: 'Logical public model', value: <Text code>public_model_id={receipt.slot.public_model_id} · {receipt.slot.model_name}</Text> },
      { key: 'Raw public model identity', value: isLegacyIdentityTransition
        ? `${optionalIdentity(receipt.slot.before_public_model_id)} → ${optionalIdentity(receipt.slot.after_public_model_id)}`
        : '不适用：canonical action 的 v1 收据冻结 logical public_model_id；raw before/after identity 仅为 legacy cutover/rollback 保留' },
      { key: 'Stable release ID', value: `${optionalIdentity(receipt.slot.before_stable_release_id)} → ${optionalIdentity(receipt.slot.after_stable_release_id)}` },
      { key: 'Legacy parity hash', value: receipt.legacy_plan_hash || '不适用' },
      { key: 'Deployment parity hash', value: receipt.deployment_plan_hash || '不适用' },
      { key: '完成时间', value: formatDateTime(receipt.completed_at) },
    ]} />
    <div className="ur-model-endpoint-probe">
      <Text strong>Release 状态快照</Text>
      {receipt.releases.map(release => <Text code key={`${release.role}:${release.release_id}`}>
        {release.role} · {release.release_ref} · {release.before_stage}@{release.before_version} → {release.after_stage}@{release.after_version}
      </Text>)}
    </div>
    <div className="ur-model-endpoint-probe">
      <Text strong>资格快照（{receipt.qualifications.length}）</Text>
      {receipt.qualifications.map(qualification => <Text code key={`${qualification.release_id}:${qualification.ordinal}`}>
        deployment_id={qualification.deployment_id} · {qualification.operation_id} · {qualification.qualification_state}
      </Text>)}
    </div>
  </Surface>;
}

function StagingSuccessorReceiptPanel({ receipt }: { receipt: StagingModelSuccessorCutoverReceipt }) {
  return <Surface
    title={`Staging successor cutover 收据 #${receipt.lifecycle.receipt_id}`}
    description="一个事务完成首个 stable Slot、旧 staging canonical 退役、稳定 Alias、model-set 转换与审计。"
  >
    <Descriptions row size="small" data={[
      { key: 'Stable request identity', value: <Text code>{receipt.stable_request_model}</Text> },
      { key: 'Predecessor', value: <Text code>{receipt.predecessor_before.model_name} · {receipt.predecessor_before.lifecycle} → {receipt.predecessor_after.lifecycle}</Text> },
      { key: 'Successor', value: <Text code>{receipt.successor_before.model_name} · {receipt.successor_before.lifecycle} → {receipt.successor_after.lifecycle}</Text> },
      { key: 'Alias', value: <Text code>{receipt.alias_after.alias} → {receipt.alias_after.target_model}</Text> },
      { key: 'Model sets', value: `${receipt.model_sets.length} 个完整 before-state 已转换` },
      { key: '幂等结论', value: receipt.lifecycle.idempotency_replay ? '安全重放原收据' : '首次完成' },
    ]} />
  </Surface>;
}

export default function ModelReleaseWorkspace({
  canManage,
  selectedReleaseRef,
  onSelectRelease,
  onCanonicalStateChanged,
}: Props) {
  const [collection, setCollection] = useState<CanonicalDeploymentReleaseCollection | null>(null);
  const [collectionLoading, setCollectionLoading] = useState(true);
  const [collectionError, setCollectionError] = useState('');
  const [lifecycle, setLifecycle] = useState<CanonicalDeploymentLifecycleView | null>(null);
  const [lifecycleLoading, setLifecycleLoading] = useState(false);
  const [lifecycleError, setLifecycleError] = useState('');
  const [authorOpen, setAuthorOpen] = useState(false);
  const [authorModelID, setAuthorModelID] = useState(0);
  const [authorSelections, setAuthorSelections] = useState<string[]>([]);
  const [authorReason, setAuthorReason] = useState('');
  const [authorContextLimit, setAuthorContextLimit] = useState('');
  const [authorOutputLimit, setAuthorOutputLimit] = useState('');
  const [authoring, setAuthoring] = useState(false);
  const [lifecycleReason, setLifecycleReason] = useState('');
  const [canaryBasisPoints, setCanaryBasisPoints] = useState(500);
  const [mutatingAction, setMutatingAction] = useState('');
  const [receipt, setReceipt] = useState<ReceiptState | null>(null);
  const [stagingPredecessorID, setStagingPredecessorID] = useState(0);
  const [stagingCutoverReason, setStagingCutoverReason] = useState('');
  const [stagingCutoverRunning, setStagingCutoverRunning] = useState(false);
  const stagingCutoverIntent = useRef<StagingSuccessorMutationIntent | null>(null);
  const collectionVersion = useRef(0);
  const lifecycleVersion = useRef(0);
  const authorIntent = useRef<MutationIntent | null>(null);
  const lifecycleIntent = useRef<MutationIntent | null>(null);

  const loadCollection = useCallback(async () => {
    const version = ++collectionVersion.current;
    setCollectionLoading(true);
    setCollectionError('');
    try {
      const result = await modelDeploymentReleaseApi.list();
      if (version === collectionVersion.current) setCollection(result);
    } catch (error) {
      if (version !== collectionVersion.current) return;
      setCollection(null);
      setCollectionError(errorText(error, '无法读取 canonical release collection。'));
    } finally {
      if (version === collectionVersion.current) setCollectionLoading(false);
    }
  }, []);

  useEffect(() => { void loadCollection(); }, [loadCollection]);

  const effectiveReleaseRef = useMemo(() => {
    if (selectedReleaseRef && collection?.releases.some(release => release.release_ref === selectedReleaseRef)) {
      return selectedReleaseRef;
    }
    return collection?.releases[0]?.release_ref || '';
  }, [collection, selectedReleaseRef]);

  const loadLifecycle = useCallback(async (releaseRef: string) => {
    const version = ++lifecycleVersion.current;
    if (!releaseRef) {
      setLifecycle(null);
      setLifecycleError('');
      setLifecycleLoading(false);
      return;
    }
    setLifecycleLoading(true);
    setLifecycleError('');
    try {
      const result = await modelDeploymentLifecycleApi.current(releaseRef);
      if (version === lifecycleVersion.current) setLifecycle(result);
    } catch (error) {
      if (version !== lifecycleVersion.current) return;
      setLifecycle(null);
      setLifecycleError(errorText(error, '无法读取 canonical lifecycle。'));
    } finally {
      if (version === lifecycleVersion.current) setLifecycleLoading(false);
    }
  }, []);

  useEffect(() => { void loadLifecycle(effectiveReleaseRef); }, [effectiveReleaseRef, loadLifecycle]);

  useEffect(() => {
    setStagingPredecessorID(0);
    setStagingCutoverReason('');
    stagingCutoverIntent.current = null;
  }, [lifecycle?.release.release_ref]);

  const refreshFacts = useCallback(async (releaseRef = effectiveReleaseRef) => {
    await Promise.all([
      loadCollection(),
      loadLifecycle(releaseRef),
      Promise.resolve(onCanonicalStateChanged()),
    ]);
  }, [effectiveReleaseRef, loadCollection, loadLifecycle, onCanonicalStateChanged]);

  const authorOption = collection?.models.find(option => option.public_model.id === authorModelID);
  const lifecycleModel = collection?.models.find(option =>
    option.public_model.id === lifecycle?.release.public_model_id);
  const stagingPredecessors = useMemo(() => {
    if (!collection || !lifecycle || !lifecycleModel) return [];
    const releaseOwners = new Set(collection.releases.map(item => item.public_model_id));
    const successorRoutes = new Set(lifecycleModel.selection_options.map(option =>
      `${option.upstream_model || ''}\u0000${option.operation_id}`));
    return collection.models.filter(option =>
      option.public_model.id !== lifecycle.release.public_model_id
      && option.public_model.lifecycle === 'staging'
      && !option.authoring.slot.exists
      && !releaseOwners.has(option.public_model.id)
      && option.selection_options.some(selection =>
        successorRoutes.has(`${selection.upstream_model || ''}\u0000${selection.operation_id}`)));
  }, [collection, lifecycle, lifecycleModel]);
  const selectedStagingPredecessor = stagingPredecessors.find(option =>
    option.public_model.id === stagingPredecessorID);
  const stagingSuccessorReady = Boolean(
    canManage
    && lifecycle
    && lifecycle.release.stage === 'validated'
    && !lifecycle.slot.exists
    && lifecycleModel?.public_model.lifecycle === 'staging'
    && selectedStagingPredecessor
    && stagingCutoverReason.trim(),
  );

  const resetAuthorLimits = (option?: CanonicalDeploymentReleaseAuthoringOption) => {
    const stable = option?.authoring.baseline?.kind === 'stable' ? option.authoring.stable_release : undefined;
    setAuthorContextLimit(validPublicLimit(stable?.public_context_limit || 0) ? String(stable!.public_context_limit) : '');
    setAuthorOutputLimit(validPublicLimit(stable?.public_output_limit || 0) ? String(stable!.public_output_limit) : '');
  };

  const openAuthor = () => {
    const initial = collection?.models.find(option => option.authoring.available)
      || collection?.models[0];
    setAuthorModelID(initial?.public_model.id || 0);
    setAuthorSelections([]);
    setAuthorReason('');
    resetAuthorLimits(initial);
    authorIntent.current = null;
    setAuthorOpen(true);
  };

  const changeAuthorModel = (value: unknown) => {
    setAuthorModelID(Number(value));
    setAuthorSelections([]);
    setAuthorReason('');
    resetAuthorLimits(collection?.models.find(option => option.public_model.id === Number(value)));
    authorIntent.current = null;
  };

  const toggleSelection = (option: CanonicalDeploymentReleaseSelectionOption, checked: boolean) => {
    const key = selectionKey(option);
    setAuthorSelections(current => checked
      ? Array.from(new Set([...current, key]))
      : current.filter(value => value !== key));
    authorIntent.current = null;
  };

  const submitAuthor = async () => {
    if (!canManage || !authorOption?.authoring.available || !authorOption.authoring.baseline) return;
    const reason = authorReason.trim();
    const selected = authorOption.selection_options.filter(option => authorSelections.includes(selectionKey(option)));
    const revisions = new Set(selected.map(option => option.profile_definition_digest || ''));
    if (revisions.size !== 1) {
      Toast.error('一个 Release 的所有 Deployment 必须使用同一执行合同 revision');
      return;
    }
    const definitionDigest = selected[0]?.profile_definition_digest;
    const contextLimit = Number(authorContextLimit);
    const outputLimit = Number(authorOutputLimit);
    if (definitionDigest && (!validPublicLimit(contextLimit) || !validPublicLimit(outputLimit))) {
      Toast.error('请输入有效的公开上下文与输出上限（正整数）');
      return;
    }
    const selections = selected.map(option => ({ deployment_id: option.deployment_id, operation_id: option.operation_id }));
    if (!reason || selections.length === 0) return;
    const input: CanonicalDeploymentReleaseAuthoringRequest = {
      public_model_id: authorOption.public_model.id,
      ...(definitionDigest ? {
        profile_definition_digest: definitionDigest,
        public_context_limit: contextLimit,
        public_output_limit: outputLimit,
        capability_approval_ids: Array.from(new Set(selected.flatMap(option => option.capability_approval_ids || []))).sort((a, b) => a - b),
      } : {}),
      baseline: { ...authorOption.authoring.baseline },
      selections,
      reason,
    };
    const fingerprint = JSON.stringify(input);
    if (!authorIntent.current || authorIntent.current.fingerprint !== fingerprint) {
      authorIntent.current = { fingerprint, idempotencyKey: newIdempotencyKey('author') };
    }
    setAuthoring(true);
    try {
      const result = await modelDeploymentReleaseApi.author(input, authorIntent.current.idempotencyKey);
      authorIntent.current = null;
      setReceipt({ kind: 'authoring', value: result });
      setAuthorOpen(false);
      Toast.success(result.idempotency_replay
        ? '已安全重放原 authoring 收据；没有重复创建 release'
        : '已原子生成 sealed validated release；尚未激活任何流量');
      onSelectRelease(result.release.release_ref);
      try {
        await Promise.all([loadCollection(), Promise.resolve(onCanonicalStateChanged())]);
      } catch (refreshError) {
        Toast.warning(`Release 已成功生成，但关联 canonical 状态刷新不完整：${errorText(refreshError, '请手动刷新')}`);
      }
    } catch (error) {
      if (shouldRotateIntent(error)) authorIntent.current = null;
      if (error instanceof ApiError && error.status === 409
        && apiErrorCode(error) === 'canonical_deployment_release_authoring_conflict') {
        Toast.warning('Release 编排与当前服务端事实冲突；不会自动重试，正在刷新当前事实。');
        try {
          await refreshFacts();
        } catch (refreshError) {
          Toast.error(`Authoring 未重试，且关联 canonical 状态刷新不完整：${errorText(refreshError, '请手动刷新')}`);
        }
      } else {
        Toast.error(errorText(error, 'Canonical release authoring 失败'));
      }
    } finally {
      setAuthoring(false);
    }
  };

  const submitStagingSuccessorCutover = async () => {
    if (!lifecycle || !lifecycleModel || !selectedStagingPredecessor || !stagingSuccessorReady) return;
    const predecessor = selectedStagingPredecessor.public_model;
    const successor = lifecycleModel.public_model;
    setStagingCutoverRunning(true);
    try {
      const stableInput: Omit<StagingModelSuccessorCutoverRequest, 'expected_model_sets'> = {
        expected_release_version: lifecycle.release.version,
        stable_request_model: predecessor.model_name,
        predecessor_public_model_id: predecessor.id,
        predecessor_model_name: predecessor.model_name,
        expected_predecessor_lifecycle: 'staging',
        expected_predecessor_approved_catalog: false,
        successor_public_model_id: successor.id,
        successor_model_name: successor.model_name,
        expected_successor_lifecycle: 'staging',
        expected_successor_approved_catalog: false,
        reason: stagingCutoverReason.trim(),
      };
      const stableFingerprint = JSON.stringify(stableInput);
      if (!stagingCutoverIntent.current
        || !stagingCutoverIntent.current.fingerprint.startsWith(`${stableFingerprint}\u0000`)) {
        const expectedModelSets = await modelSuccessorCutoverApi.snapshotModelSets(predecessor.model_name);
        const input: StagingModelSuccessorCutoverRequest = { ...stableInput, expected_model_sets: expectedModelSets };
        stagingCutoverIntent.current = {
          fingerprint: `${stableFingerprint}\u0000${JSON.stringify(expectedModelSets)}`,
          idempotencyKey: newIdempotencyKey('lifecycle'),
          input,
        };
      }
      const result = await modelSuccessorCutoverApi.activateStagingSuccessor(
        lifecycle.release.release_ref,
        stagingCutoverIntent.current.input,
        stagingCutoverIntent.current.idempotencyKey,
      );
      stagingCutoverIntent.current = null;
      setReceipt({ kind: 'staging_successor', value: result });
      setStagingPredecessorID(0);
      setStagingCutoverReason('');
      Toast.success(result.lifecycle.idempotency_replay
        ? '已安全重放原 staging successor 收据'
        : '已原子完成 staging successor 切换');
      try {
        await refreshFacts(lifecycle.release.release_ref);
      } catch (refreshError) {
        Toast.warning(`Staging successor 已成功完成，但关联 canonical 状态刷新不完整：${errorText(refreshError, '请手动刷新')}`);
      }
    } catch (error) {
      if (shouldRotateIntent(error)) stagingCutoverIntent.current = null;
      Toast.error(errorText(error, 'Staging successor cutover 失败'));
      if (error instanceof ApiError && error.status === 409) {
        try {
          await refreshFacts(lifecycle.release.release_ref);
        } catch {
          // Preserve the original conflict as the actionable result.
        }
      }
    } finally {
      setStagingCutoverRunning(false);
    }
  };

  const confirmStagingSuccessorCutover = () => {
    if (!lifecycle || !selectedStagingPredecessor || !stagingSuccessorReady) return;
    Modal.confirm({
      title: '原子激活 Staging successor？',
      content: <div>
        <p>稳定请求身份：<Text code>{selectedStagingPredecessor.public_model.model_name}</Text></p>
        <p>旧 staging canonical：<Text code>{selectedStagingPredecessor.public_model.model_name}</Text></p>
        <p>不可变 successor：<Text code>{lifecycle.release.model_name}</Text></p>
        <p>提交前将重新读取完整 model-set before-state；服务端随后在一个事务中验证 Release、Slot、Alias、registry 与 model-set 状态。</p>
      </div>,
      okText: '原子激活 Staging successor',
      onOk: submitStagingSuccessorCutover,
    });
  };

  const descriptorInputReady = (descriptor: CanonicalDeploymentLifecycleActionDescriptor) =>
    !descriptor.required_fields.includes('reason') || Boolean(lifecycleReason.trim());

  const lifecycleRequest = (descriptor: CanonicalDeploymentLifecycleActionDescriptor) => {
    if (!lifecycle) return null;
    const reason = lifecycleReason.trim();
    if (!descriptorInputReady(descriptor)) return null;
    const base: CanonicalDeploymentLifecycleBaseRequest = {
      expected_release_version: lifecycle.release.version,
      ...(reason ? { reason } : {}),
    };
    if (!descriptor.required_fields.includes('expected_slot_version')) return base;
    const slot: CanonicalDeploymentLifecycleSlotRequest = {
      ...base,
      expected_slot_version: lifecycle.slot.version,
    };
    if (descriptor.required_fields.includes('reason')) {
      return slot as CanonicalDeploymentLegacyLifecycleRequest;
    }
    if (!descriptor.required_fields.includes('canary_basis_points')) return slot;
    return { ...slot, canary_basis_points: canaryBasisPoints } as CanonicalDeploymentStartCanaryRequest;
  };

  const callLifecycle = (
    descriptor: CanonicalDeploymentLifecycleActionDescriptor,
    input: CanonicalDeploymentLifecycleBaseRequest | CanonicalDeploymentLifecycleSlotRequest | CanonicalDeploymentStartCanaryRequest | CanonicalDeploymentLegacyLifecycleRequest,
    idempotencyKey: string,
  ) => {
    if (!lifecycle) throw new Error('Canonical lifecycle view is unavailable');
    const releaseRef = lifecycle.release.release_ref;
    switch (descriptor.action) {
    case 'activate_initial':
      return modelDeploymentLifecycleApi.activateInitial(releaseRef, input as CanonicalDeploymentLifecycleBaseRequest, idempotencyKey);
    case 'start_canary':
      return modelDeploymentLifecycleApi.startCanary(releaseRef, input as CanonicalDeploymentStartCanaryRequest, idempotencyKey);
    case 'promote':
      return modelDeploymentLifecycleApi.promote(releaseRef, input as CanonicalDeploymentLifecycleSlotRequest, idempotencyKey);
    case 'cancel_canary':
      return modelDeploymentLifecycleApi.cancelCanary(releaseRef, input as CanonicalDeploymentLifecycleSlotRequest, idempotencyKey);
    case 'rollback_stable':
      return modelDeploymentLifecycleApi.rollbackStable(releaseRef, input as CanonicalDeploymentLifecycleSlotRequest, idempotencyKey);
    case 'cutover_legacy':
      return modelDeploymentLifecycleApi.cutoverLegacy(releaseRef, input as CanonicalDeploymentLegacyLifecycleRequest, idempotencyKey);
    case 'rollback_legacy':
      return modelDeploymentLifecycleApi.rollbackLegacy(releaseRef, input as CanonicalDeploymentLegacyLifecycleRequest, idempotencyKey);
    }
  };

  const executeLifecycle = async (descriptor: CanonicalDeploymentLifecycleActionDescriptor) => {
    if (!canManage || !descriptor.available || !descriptorInputReady(descriptor) || !lifecycle) return;
    const input = lifecycleRequest(descriptor);
    if (!input) return;
    const fingerprint = JSON.stringify({ action: descriptor.action, release_ref: lifecycle.release.release_ref, ...input });
    if (!lifecycleIntent.current || lifecycleIntent.current.fingerprint !== fingerprint) {
      lifecycleIntent.current = { fingerprint, idempotencyKey: newIdempotencyKey('lifecycle') };
    }
    setMutatingAction(descriptor.action);
    try {
      const result = await callLifecycle(descriptor, input, lifecycleIntent.current.idempotencyKey);
      lifecycleIntent.current = null;
      setReceipt({ kind: 'lifecycle', value: result });
      Toast.success(result.idempotency_replay
        ? '已安全重放原 lifecycle 收据；没有重复切换流量'
        : `${actionLabels[descriptor.action]}已完成`);
      try {
        await refreshFacts(lifecycle.release.release_ref);
      } catch (refreshError) {
        Toast.warning(`Lifecycle 已成功完成，但关联 canonical 状态刷新不完整：${errorText(refreshError, '请手动刷新')}`);
      }
    } catch (error) {
      if (shouldRotateIntent(error)) lifecycleIntent.current = null;
      if (error instanceof ApiError && error.status === 409) {
        Toast.warning('Lifecycle 状态已变化；不会自动重试，正在刷新当前事实。');
        try {
          await refreshFacts(lifecycle.release.release_ref);
        } catch (refreshError) {
          Toast.error(`Lifecycle 未重试，且关联 canonical 状态刷新不完整：${errorText(refreshError, '请手动刷新')}`);
        }
      } else {
        Toast.error(errorText(error, `${actionLabels[descriptor.action]}失败`));
      }
    } finally {
      setMutatingAction('');
    }
  };

  const confirmLifecycle = (descriptor: CanonicalDeploymentLifecycleActionDescriptor) => {
    if (!canManage || !descriptor.available || !lifecycle) return;
    Modal.confirm({
      title: `${actionLabels[descriptor.action]}？`,
      content: <div>
        <p><Text code>{lifecycle.release.release_ref}</Text></p>
        <p>将提交 release version {lifecycle.release.version}
          {descriptor.required_fields.includes('expected_slot_version') ? ` 与 slot version ${lifecycle.slot.version}` : ''} 的精确 CAS。</p>
        <p>服务端会在事务内重新核对 route、资格、权限与当前版本。</p>
      </div>,
      okText: actionLabels[descriptor.action],
      onOk: () => executeLifecycle(descriptor),
    });
  };

  const releaseColumns = [
    {
      title: 'Canonical Release', key: 'release',
      render: (_: unknown, release: CanonicalDeploymentReleaseCollection['releases'][number]) => <div className="ur-model-endpoint-name">
        <Text strong>{release.model_name}</Text>
        <Text code size="small">{release.release_ref}</Text>
      </div>,
    },
    {
      title: '阶段', dataIndex: 'stage', width: 130,
      render: (stage: string) => <StatusTag status={stageTone(stage)}>{stage}</StatusTag>,
    },
    { title: 'Version', dataIndex: 'version', width: 100 },
    { title: 'Routes', dataIndex: 'route_items_count', width: 90 },
    { title: '更新时间', dataIndex: 'updated_at', render: (value: string) => formatDateTime(value) },
    {
      title: '操作', width: 100,
      render: (_: unknown, release: CanonicalDeploymentReleaseCollection['releases'][number]) => <Button
        type="primary"
        theme="borderless"
        onClick={(event) => {
          event.stopPropagation();
          onSelectRelease(release.release_ref);
        }}
      >查看发布</Button>,
    },
  ];

  const selectedAuthorOptions = authorOption?.selection_options.filter(option => authorSelections.includes(selectionKey(option))) || [];
  const selectedAuthorRevisions = new Set(selectedAuthorOptions.map(option => option.profile_definition_digest || ''));
  const authorHasData = selectedAuthorOptions.some(option => Boolean(option.profile_definition_digest));
  const authorDataLimitsValid = validPublicLimit(Number(authorContextLimit)) && validPublicLimit(Number(authorOutputLimit));
  const selectedAuthorReady = Boolean(
    authorOption?.authoring.available
    && authorOption.authoring.baseline
    && authorSelections.length > 0
    && (!authorHasData || selectedAuthorRevisions.size !== 1 || authorDataLimitsValid)
    && authorReason.trim(),
  );

  return <div className="ur-runtime-release-workspace">
    <Surface
      title="Canonical Release Workspace"
      description="先以服务端 baseline 和 selection options 原子生成 sealed validated release，再由服务端 action descriptors 驱动独立 lifecycle；authoring 不会自动获得流量。"
      actions={<Space>
        <Button icon={<IconRefresh />} loading={collectionLoading} onClick={() => void refreshFacts()}>刷新发布事实</Button>
        {canManage && <Button
          type="primary"
          icon={<IconPlus />}
          disabled={!collection?.models.some(option => option.authoring.available)}
          title={collection?.models.length && !collection.models.some(option => option.authoring.available)
            ? collection.models[0].authoring.reason_code
            : undefined}
          onClick={openAuthor}
        >编排新 Release</Button>}
      </Space>}
    >
      {!canManage && <Banner
        type="info"
        closeIcon={null}
        description="组织只读角色可查看 canonical release、slot、父级与 action descriptor，但不会收到 authoring 或 lifecycle 写入口。"
        style={{ marginBottom: 12 }}
      />}
      {collectionError ? <EmptyState
        title="Canonical release collection 暂时不可用"
        description={collectionError}
        actions={<Button type="primary" onClick={() => void loadCollection()}>重试</Button>}
        minHeight={220}
      /> : <Table
        rowKey="release_ref"
        loading={collectionLoading}
        dataSource={collection?.releases || []}
        columns={releaseColumns}
        pagination={false}
        onRow={(release) => ({
          onClick: () => onSelectRelease((release as CanonicalDeploymentReleaseCollection['releases'][number]).release_ref),
          style: { cursor: 'pointer' },
        })}
        empty={<EmptyState
          title="尚无可推进的 Canonical Release"
          description={canManage ? '使用服务端给出的 baseline 与 deployment operation 选项编排第一个 sealed candidate。' : '当前组织尚无 validated、canary 或 active canonical release。'}
          minHeight={200}
        />}
      />}
    </Surface>

    {lifecycleError && <Banner
      type="danger"
      closeIcon={null}
      description={lifecycleError}
      style={{ marginTop: 12 }}
    />}

    {lifecycleLoading && !lifecycle ? <Surface><Text type="tertiary">正在读取精确 lifecycle 与 action descriptors…</Text></Surface> : lifecycle && <Surface
      title={`${lifecycle.release.model_name} · ${lifecycle.release.release_ref}`}
      description="按钮是否可用、需要哪些并发字段、是否要求 Gateway License 均来自本次服务端 descriptor。"
    >
      <Descriptions row size="small" data={[
        { key: 'Release', value: <Space><StatusTag status={stageTone(lifecycle.release.stage)}>{lifecycle.release.stage}</StatusTag><Text>version {lifecycle.release.version}</Text></Space> },
        { key: 'Candidate', value: <Text code>{lifecycle.release.candidate_version} · {lifecycle.release.route_items_count} routes</Text> },
        { key: 'Execution contract', value: lifecycle.release.execution_profile_id
          ? <Text code>{lifecycle.release.execution_profile_id}@{lifecycle.release.execution_profile_version} · context={lifecycle.release.public_context_limit} · output={lifecycle.release.public_output_limit}</Text>
          : '当前读取未包含密封执行 Profile' },
        { key: 'Slot', value: lifecycle.slot.exists
          ? <Text code>version {lifecycle.slot.version} · assigned_public_model_id={optionalIdentity(lifecycle.slot.assigned_public_model_id)} · stable={optionalIdentity(lifecycle.slot.stable_release_id)} · canary={optionalIdentity(lifecycle.slot.canary_release_id)}</Text>
          : '尚无 runtime slot' },
        { key: 'Stable parent', value: lifecycle.stable_parent
          ? <Text code>{lifecycle.stable_parent.release_ref} · {lifecycle.stable_parent.candidate_version} · {lifecycle.stable_parent.stage}@{lifecycle.stable_parent.version}</Text>
          : '无父稳定版本' },
      ]} />

      {canManage && <div className="ur-model-endpoint-form-grid" style={{ marginTop: 14 }}>
        <label>
          <Text strong>Lifecycle reason{lifecycle.actions.some(action => action.required_fields.includes('reason')) ? '（Legacy 动作必填）' : '（可选）'}</Text>
          <TextArea
            aria-label="Lifecycle reason"
            value={lifecycleReason}
            maxCount={1000}
            onChange={setLifecycleReason}
            placeholder="记录本次流量切换的业务原因"
          />
        </label>
        {lifecycle.actions.some(action => action.required_fields.includes('canary_basis_points')) && <label>
          <Text strong>灰度基点（1–5000）</Text>
          <input
            aria-label="灰度基点"
            type="number"
            min={1}
            max={5000}
            value={canaryBasisPoints}
            onChange={event => setCanaryBasisPoints(Math.max(1, Math.min(5000, Number(event.target.value) || 1)))}
            style={{ width: '100%', boxSizing: 'border-box', minHeight: 34, marginTop: 8 }}
          />
        </label>}
      </div>}

      <Space wrap style={{ marginTop: 14 }}>
        {canManage ? lifecycle.actions.map(descriptor => <Button
          key={descriptor.action}
          type={descriptor.safety_direction === 'contraction' ? 'danger' : 'primary'}
          disabled={!descriptor.available || Boolean(mutatingAction)
            || !descriptorInputReady(descriptor)}
          loading={mutatingAction === descriptor.action}
          title={!descriptor.available
            ? descriptor.reason_code
            : !descriptorInputReady(descriptor)
              ? '请先填写 Lifecycle reason'
              : undefined}
          onClick={() => confirmLifecycle(descriptor)}
        >{actionLabels[descriptor.action]}</Button>) : <Text type="tertiary">只读视图不提供 lifecycle 写操作。</Text>}
      </Space>
      {canManage && lifecycle.release.stage === 'validated' && !lifecycle.slot.exists
        && lifecycleModel?.public_model.lifecycle === 'staging' && <Surface
        title="Never-activated staging successor"
        description="仅用于替换从未获得 Slot、sealed/executable predecessor Release 或 active Alias 的 staging canonical；服务端在一个事务中重新核对全部 before-state。"
      >
        <Space vertical align="start" style={{ width: '100%' }}>
          <label style={{ width: '100%' }}>
            <Text strong>Predecessor staging canonical</Text>
            <Select
              aria-label="Staging predecessor"
              value={stagingPredecessorID || undefined}
              onChange={value => {
                setStagingPredecessorID(Number(value));
                stagingCutoverIntent.current = null;
              }}
              optionList={stagingPredecessors.map(option => ({
                value: option.public_model.id,
                label: `${option.public_model.display_name || option.public_model.model_name} · ${option.public_model.model_name}`,
              }))}
              placeholder="选择将被原子退役的旧 staging canonical"
              style={{ width: '100%', marginTop: 8 }}
            />
          </label>
          {selectedStagingPredecessor && <Descriptions row size="small" data={[
            { key: 'Stable request model', value: <Text code>{selectedStagingPredecessor.public_model.model_name}</Text> },
            { key: 'Immutable successor', value: <Text code>{lifecycle.release.model_name}</Text> },
            { key: 'Release', value: <Text code>{lifecycle.release.release_ref}@{lifecycle.release.version}</Text> },
          ]} />}
          <label style={{ width: '100%' }}>
            <Text strong>Cutover reason</Text>
            <TextArea
              aria-label="Staging successor cutover reason"
              value={stagingCutoverReason}
              maxCount={1000}
              onChange={value => {
                setStagingCutoverReason(value);
                stagingCutoverIntent.current = null;
              }}
              placeholder="记录为什么该 never-activated staging identity 必须由当前 immutable successor 替换"
              style={{ marginTop: 8 }}
            />
          </label>
          <Button
            type="primary"
            loading={stagingCutoverRunning}
            disabled={!stagingSuccessorReady || stagingCutoverRunning}
            onClick={confirmStagingSuccessorCutover}
          >原子激活 Staging successor</Button>
        </Space>
      </Surface>}
      <Space wrap style={{ marginTop: 10 }}>
        {lifecycle.actions.map(descriptor => <Tag key={`${descriptor.action}:${descriptor.reason_code}`} color={descriptor.available ? 'green' : 'grey'}>
          {descriptor.action}: {descriptor.reason_code}
        </Tag>)}
      </Space>
    </Surface>}

    {receipt?.kind === 'authoring' && <AuthoringReceiptPanel receipt={receipt.value} />}
    {receipt?.kind === 'lifecycle' && <LifecycleReceiptPanel receipt={receipt.value} />}
    {receipt?.kind === 'staging_successor' && <StagingSuccessorReceiptPanel receipt={receipt.value} />}

    <SideSheet
      title="编排 Canonical Release"
      visible={authorOpen}
      width={720}
      onCancel={() => {
        authorIntent.current = null;
        setAuthorOpen(false);
      }}
    >
      <Banner
        type="info"
        closeIcon={null}
        description="Baseline、slot、stable parent 和可选 deployment operation 均由服务端投影。提交只生成 sealed validated release；不会自动调用任何 lifecycle action。"
        style={{ marginBottom: 14 }}
      />
      <Space vertical align="start" style={{ width: '100%' }}>
        <label style={{ width: '100%' }}>
          <Text strong>公共模型</Text>
          <Select
            aria-label="Release 公共模型"
            value={authorModelID || undefined}
            onChange={changeAuthorModel}
            optionList={(collection?.models || []).map(option => ({
              value: option.public_model.id,
              label: `${option.public_model.display_name || option.public_model.model_name} · ${option.public_model.model_name}`,
            }))}
            style={{ width: '100%', marginTop: 8 }}
          />
        </label>

        {authorOption && <Descriptions row size="small" data={[
          { key: 'Authoring readiness', value: <Space><StatusTag status={authorOption.authoring.available ? 'ready' : 'blocked'}>{authorOption.authoring.available ? 'available' : 'blocked'}</StatusTag><Text code>{authorOption.authoring.reason_code}</Text></Space> },
          { key: 'Server baseline', value: authorOption.authoring.baseline
            ? <Text code>{authorOption.authoring.baseline.kind} · slot {authorOption.authoring.baseline.expected_slot_version}{authorOption.authoring.baseline.stable_release_ref ? ` · ${authorOption.authoring.baseline.stable_release_ref}@${authorOption.authoring.baseline.expected_stable_release_version}` : ''}</Text>
            : '服务端未提供可提交 baseline' },
          { key: 'Raw slot identity', value: authorOption.authoring.slot.exists
            ? <Text code>assigned_public_model_id={optionalIdentity(authorOption.authoring.slot.assigned_public_model_id)} · stable={optionalIdentity(authorOption.authoring.slot.stable_release_id)} · version {authorOption.authoring.slot.version}</Text>
            : '无 slot（initial）' },
        ]} />}

        <div style={{ width: '100%' }}>
          <Text strong>Deployment × Operation Selection</Text>
          <div className="ur-model-endpoint-sheet" style={{ marginTop: 8 }}>
            {(authorOption?.selection_options || []).map(option => {
              const key = selectionKey(option);
              return <label key={key} className="ur-canonical-binding-row" style={{ display: 'flex', gap: 10, alignItems: 'flex-start' }}>
                <input
                  type="checkbox"
                  aria-label={`选择 deployment ${option.deployment_id} ${option.operation_id}`}
                  checked={authorSelections.includes(key)}
                  onChange={event => toggleSelection(option, event.target.checked)}
                />
                <span className="ur-model-endpoint-name">
                  <Text code>deployment_id={option.deployment_id} · {option.operation_id}</Text>
                  <Text type="tertiary">{option.upstream_model || '未提供 upstream label'} · {option.connection_name || '未提供 connection label'}</Text>
                  {option.profile_definition_digest && <Text type="tertiary">合同 {option.profile_definition_digest.slice(0, 12)} · Deployment 上下文 {option.max_context} / 请求默认输出 {option.default_max_output} · {option.capability_approval_ids?.length || 0} 项资格批准</Text>}
                </span>
              </label>;
            })}
            {authorOption && authorOption.selection_options.length === 0 && <Text type="tertiary">服务端没有返回可 author 的 deployment operation。</Text>}
          </div>
        </div>

        {authorHasData && <Space vertical align="start" style={{ width: '100%' }}>
          <Text type="tertiary">公开上限独立于请求默认值；已有稳定 Release 的上限可预填，最终由服务端校验所选合同。</Text>
          <label style={{ width: '100%' }}>
            <Text strong>公开上下文上限</Text>
            <Input aria-label="公开上下文上限" inputMode="numeric" value={authorContextLimit} onChange={setAuthorContextLimit} />
          </label>
          <label style={{ width: '100%' }}>
            <Text strong>公开输出上限</Text>
            <Input aria-label="公开输出上限" inputMode="numeric" value={authorOutputLimit} onChange={setAuthorOutputLimit} />
          </label>
        </Space>}

        <label style={{ width: '100%' }}>
          <Text strong>Authoring reason</Text>
          <TextArea
            aria-label="Authoring reason"
            value={authorReason}
            maxCount={500}
            onChange={value => {
              setAuthorReason(value);
              authorIntent.current = null;
            }}
            placeholder="说明为什么需要这一 sealed candidate"
            style={{ marginTop: 8 }}
          />
        </label>

        <Button
          block
          type="primary"
          loading={authoring}
          disabled={!selectedAuthorReady || authoring}
          title={authorOption && !authorOption.authoring.available ? authorOption.authoring.reason_code : undefined}
          onClick={() => void submitAuthor()}
        >生成 Sealed Validated Release</Button>
      </Space>
    </SideSheet>
  </div>;
}
