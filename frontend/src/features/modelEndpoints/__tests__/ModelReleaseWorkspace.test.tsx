import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { Modal, Toast } from '@douyinfe/semi-ui';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../../api/transport';
import ModelReleaseWorkspace from '../ModelReleaseWorkspace';

const mocks = vi.hoisted(() => ({
  releaseList: vi.fn(),
  author: vi.fn(),
  current: vi.fn(),
  activateInitial: vi.fn(),
  startCanary: vi.fn(),
  promote: vi.fn(),
  cancelCanary: vi.fn(),
  rollbackStable: vi.fn(),
  cutoverLegacy: vi.fn(),
  rollbackLegacy: vi.fn(),
  snapshotModelSets: vi.fn(),
  activateStagingSuccessor: vi.fn(),
}));

vi.mock('../../../api/modelDeploymentReleases', () => ({
  modelDeploymentReleaseApi: { list: mocks.releaseList, author: mocks.author },
}));

vi.mock('../../../api/modelDeploymentLifecycle', () => ({
  modelDeploymentLifecycleApi: {
    current: mocks.current,
    activateInitial: mocks.activateInitial,
    startCanary: mocks.startCanary,
    promote: mocks.promote,
    cancelCanary: mocks.cancelCanary,
    rollbackStable: mocks.rollbackStable,
    cutoverLegacy: mocks.cutoverLegacy,
    rollbackLegacy: mocks.rollbackLegacy,
  },
}));

vi.mock('../../../api/modelSuccessorCutover', () => ({
  modelSuccessorCutoverApi: {
    snapshotModelSets: mocks.snapshotModelSets,
    activateStagingSuccessor: mocks.activateStagingSuccessor,
  },
}));

const summary = {
  id: 80,
  release_ref: 'rcr_v2-candidate',
  public_model_id: 7,
  model_name: 'deepseek-v4-flash',
  execution_profile_id: 'deepseek.v4-flash.api',
  execution_profile_version: '1.0.0',
  public_context_limit: 1000000,
  public_output_limit: 32768,
  candidate_version: 'unirouter.deployment-candidate.v2',
  candidate_hash: 'sha256:candidate',
  route_items_hash: 'sha256:routes',
  route_items_count: 1,
  route_items_sealed_at: '2026-08-20T01:00:00Z',
  stage: 'validated',
  version: 2,
  updated_at: '2026-08-20T01:00:00Z',
};

const collection = {
  contract_version: 'unirouter.model-deployment-release-authoring-options.v1',
  generated_at: '2026-08-20T01:00:00Z',
  models: [{
    public_model: {
      id: 7,
      model_name: 'deepseek-v4-flash',
      display_name: 'DeepSeek V4 Flash',
      lifecycle: 'ga',
    },
    authoring: {
      available: true,
      reason_code: 'ready_for_authoring',
      structurally_available: true,
      authorized: true,
      licensed: true,
      license_feature: 'gateway',
      method: 'POST',
      path: '/api/v1/model-deployment-releases',
      idempotency_required: true,
      required_fields: ['public_model_id', 'baseline', 'selections', 'reason'],
      baseline: {
        kind: 'legacy_cutover',
        expected_slot_version: 8,
        stable_release_ref: 'rcr_legacy-parent',
        expected_stable_release_version: 4,
      },
      slot: {
        exists: true,
        public_model_id: 7,
        model_name: 'deepseek-v4-flash',
        stable_release_id: 70,
        canary_basis_points: 0,
        version: 8,
      },
      stable_release: {
        ...summary,
        id: 70,
        release_ref: 'rcr_legacy-parent',
        candidate_version: 'unirouter.runtime-change.v1',
        stage: 'active',
        version: 4,
      },
    },
    selection_options: [{
      deployment_id: 41,
      operation_id: 'openai.responses.v1',
      upstream_model: 'deepseek-v4-flash-202608',
      connection_name: 'DeepSeek official',
    }],
  }],
  releases: [summary],
};

const release = {
  ...summary,
  baseline_hash: 'sha256:baseline',
  candidate: {
    contract_version: 'unirouter.deployment-candidate.v2',
    public_model_id: 7,
    model_name: 'deepseek-v4-flash',
    deployment_routes: [],
  },
  parent_stable_release_id: 70,
  canary_basis_points: 0,
  reason: 'candidate',
  created_by: 1,
  created_at: '2026-08-20T01:00:00Z',
  validated_at: '2026-08-20T01:00:00Z',
};

function descriptor(
  action: 'activate_initial' | 'start_canary' | 'promote' | 'cancel_canary' | 'rollback_stable' | 'cutover_legacy' | 'rollback_legacy',
  overrides: Record<string, unknown> = {},
) {
  return {
    action,
    method: 'POST',
    path: `/api/v1/model-deployment-releases/{releaseRef}/${action.replace(/_/gu, '-')}`,
    available: true,
    reason_code: 'ready_for_transactional_preflight',
    structurally_available: true,
    authorized: true,
    licensed: true,
    idempotency_required: true,
    required_fields: action === 'activate_initial'
      ? ['expected_release_version']
      : action === 'cutover_legacy' || action === 'rollback_legacy'
        ? ['expected_release_version', 'expected_slot_version', 'reason']
        : ['expected_release_version', 'expected_slot_version'],
    safety_direction: action === 'rollback_legacy' ? 'contraction' : 'expansion',
    ...overrides,
  };
}

function lifecycleView(actions = [descriptor('cutover_legacy')]) {
  return {
    contract_version: 'unirouter.model-deployment-lifecycle-receipt.v1',
    release,
    slot: {
      exists: true,
      public_model_id: 7,
      model_name: 'deepseek-v4-flash',
      stable_release_id: 70,
      canary_basis_points: 0,
      version: 8,
    },
    stable_parent: collection.models[0].authoring.stable_release,
    actions,
  };
}

function lifecycleReceipt(action: string) {
  const legacySlotIdentity = action === 'cutover_legacy'
    ? { after_public_model_id: 7 }
    : action === 'rollback_legacy'
      ? { before_public_model_id: 7 }
      : {};
  const legacyPlanEvidence = action === 'cutover_legacy' || action === 'rollback_legacy'
    ? { legacy_plan_hash: 'sha256:legacy', deployment_plan_hash: 'sha256:legacy' }
    : {};
  return {
    contract_version: 'unirouter.model-deployment-lifecycle-receipt.v1',
    receipt_id: 501,
    action,
    target_release_ref: release.release_ref,
    customer_org_id: 1,
    server_operation_id: 'op-501',
    original_http_status: 200,
    idempotency_replay: false,
    ...legacyPlanEvidence,
    slot: {
      public_model_id: 7,
      model_name: 'deepseek-v4-flash',
      before_stable_release_id: 70,
      before_canary_basis_points: 0,
      before_version: 8,
      ...legacySlotIdentity,
      after_stable_release_id: 80,
      after_canary_basis_points: 0,
      after_version: 9,
    },
    releases: [{
      role: 'target',
      release_id: 80,
      release_ref: release.release_ref,
      candidate_hash: 'sha256:candidate',
      route_items_hash: 'sha256:routes',
      before_stage: 'validated',
      before_version: 2,
      after_stage: 'active',
      after_version: 3,
    }],
    qualifications: [{
      release_id: 80,
      release_role: 'target',
      ordinal: 1,
      deployment_id: 41,
      operation_id: 'openai.responses.v1',
      qualification_required: true,
      qualified: true,
      qualification_state: 'qualified',
      expected_connection_revision: 5,
      expected_deployment_revision: 4,
      expected_operation_revision: 2,
    }],
    created_at: '2026-08-20T01:00:00Z',
    completed_at: '2026-08-20T01:00:01Z',
  };
}

function authoringReceipt() {
  const authoringRelease: Partial<typeof release> = { ...release };
  delete authoringRelease.execution_profile_id;
  delete authoringRelease.execution_profile_version;
  delete authoringRelease.public_context_limit;
  delete authoringRelease.public_output_limit;
  return {
    contract_version: 'unirouter.model-deployment-release-authoring-receipt.v1',
    receipt_id: 401,
    customer_org_id: 1,
    server_operation_id: 'op-401',
    original_http_status: 201,
    original_location: `/api/v1/model-deployment-releases/${release.release_ref}`,
    idempotency_replay: false,
    baseline: collection.models[0].authoring.baseline,
    selections: [{ deployment_id: 41, operation_id: 'openai.responses.v1' }],
    release: authoringRelease,
    created_at: '2026-08-20T01:00:00Z',
    completed_at: '2026-08-20T01:00:01Z',
  };
}

function renderWorkspace(overrides: Partial<Parameters<typeof ModelReleaseWorkspace>[0]> = {}) {
  const props = {
    canManage: true,
    selectedReleaseRef: release.release_ref,
    onSelectRelease: vi.fn(),
    onCanonicalStateChanged: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
  return { props, ...render(<ModelReleaseWorkspace {...props} />) };
}

describe('ModelReleaseWorkspace', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    Modal.destroyAll();
  });

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.releaseList.mockResolvedValue(collection);
    mocks.current.mockResolvedValue(lifecycleView());
    mocks.author.mockResolvedValue(authoringReceipt());
    mocks.activateInitial.mockResolvedValue(lifecycleReceipt('activate_initial'));
    mocks.startCanary.mockResolvedValue(lifecycleReceipt('start_canary'));
    mocks.promote.mockResolvedValue(lifecycleReceipt('promote'));
    mocks.cancelCanary.mockResolvedValue(lifecycleReceipt('cancel_canary'));
    mocks.rollbackStable.mockResolvedValue(lifecycleReceipt('rollback_stable'));
    mocks.cutoverLegacy.mockResolvedValue(lifecycleReceipt('cutover_legacy'));
    mocks.rollbackLegacy.mockResolvedValue(lifecycleReceipt('rollback_legacy'));
    mocks.snapshotModelSets.mockResolvedValue([]);
  });

  it('atomically cuts over a compatible never-activated staging predecessor with a fresh complete model-set snapshot', async () => {
    let confirmation: any;
    vi.spyOn(Modal, 'confirm').mockImplementation((config: any) => {
      confirmation = config;
      return { destroy: vi.fn(), update: vi.fn() } as any;
    });
    const predecessor = {
      public_model: {
        id: 962,
        model_name: 'minimax-m3',
        display_name: 'MiniMax M3 PAYG',
        lifecycle: 'staging',
      },
      authoring: {
        ...collection.models[0].authoring,
        slot: {
          exists: false,
          public_model_id: 962,
          model_name: 'minimax-m3',
          canary_basis_points: 0,
          version: 0,
        },
        baseline: { kind: 'initial', expected_slot_version: 0 },
      },
      selection_options: [{
        deployment_id: 15,
        operation_id: 'openai.chat_completions.v1',
        upstream_model: 'MiniMax-M3',
        connection_name: 'MiniMax official',
      }],
    };
    const successor = {
      ...predecessor,
      public_model: {
        id: 988,
        model_name: 'minimax-m3-20260903',
        display_name: 'MiniMax M3',
        lifecycle: 'staging',
      },
      selection_options: [{
        ...predecessor.selection_options[0],
        deployment_id: 19,
      }],
    };
    const successorRelease = {
      ...release,
      id: 90,
      release_ref: 'rcr_minimax_m3_successor',
      public_model_id: 988,
      model_name: 'minimax-m3-20260903',
      stage: 'validated',
      version: 2,
      parent_stable_release_id: undefined,
      candidate: {
        ...release.candidate,
        public_model_id: 988,
        model_name: 'minimax-m3-20260903',
      },
    };
    mocks.releaseList.mockResolvedValue({
      ...collection,
      models: [predecessor, successor],
      releases: [{
        ...successorRelease,
        baseline_hash: undefined,
        candidate: undefined,
        reason: undefined,
        created_by: undefined,
        created_at: undefined,
        canary_basis_points: undefined,
      }],
    });
    mocks.current.mockResolvedValue({
      contract_version: 'unirouter.model-deployment-lifecycle-receipt.v1',
      release: successorRelease,
      slot: {
        exists: false,
        public_model_id: 988,
        model_name: 'minimax-m3-20260903',
        canary_basis_points: 0,
        version: 0,
      },
      actions: [descriptor('activate_initial')],
    });
    mocks.snapshotModelSets.mockResolvedValue([{ model_set_id: 3, before_alias_exists: false }]);
    const cutoverReceipt = {
      contract_version: 'unirouter.model-successor-cutover-receipt.v2',
      predecessor_authority_mode: 'never_activated_staging',
      stable_request_model: 'minimax-m3',
      predecessor_before: { public_model_id: 962, model_name: 'minimax-m3', lifecycle: 'staging', approved_catalog: false },
      predecessor_after: { public_model_id: 962, model_name: 'minimax-m3', lifecycle: 'retired', approved_catalog: false },
      successor_before: { public_model_id: 988, model_name: 'minimax-m3-20260903', lifecycle: 'staging', approved_catalog: false },
      successor_after: { public_model_id: 988, model_name: 'minimax-m3-20260903', lifecycle: 'ga', approved_catalog: true },
      alias_after: { alias: 'minimax-m3', target_model: 'minimax-m3-20260903', is_primary: true, status: 'active' },
      model_sets: [{ model_set_id: 3, before_model_name: 'minimax-m3', before_alias_exists: false, after_alias_name: 'minimax-m3' }],
      lifecycle: { ...lifecycleReceipt('activate_initial'), contract_version: 'unirouter.model-deployment-lifecycle-receipt.v3' },
    };
    mocks.activateStagingSuccessor
      .mockRejectedValueOnce(new Error('connection reset after request write'))
      .mockResolvedValueOnce(cutoverReceipt);

    renderWorkspace({ selectedReleaseRef: successorRelease.release_ref });

    expect(await screen.findByText('minimax-m3-20260903 · rcr_minimax_m3_successor')).toBeTruthy();
    expect(await screen.findByText('Never-activated staging successor')).toBeTruthy();
    fireEvent.click(await screen.findByText('选择将被原子退役的旧 staging canonical'));
    fireEvent.click(await screen.findByText('MiniMax M3 PAYG · minimax-m3'));
    fireEvent.change(screen.getByLabelText('Staging successor cutover reason'), {
      target: { value: 'Replace the never-activated staging canonical with the exact qualified successor.' },
    });
    fireEvent.click(screen.getByRole('button', { name: '原子激活 Staging successor' }));
    await act(async () => { await confirmation.onOk(); });

    fireEvent.click(screen.getByRole('button', { name: '原子激活 Staging successor' }));
    await act(async () => { await confirmation.onOk(); });

    expect(mocks.snapshotModelSets).toHaveBeenCalledTimes(1);
    expect(mocks.snapshotModelSets).toHaveBeenCalledWith('minimax-m3');
    expect(mocks.activateStagingSuccessor).toHaveBeenNthCalledWith(
      2,
      'rcr_minimax_m3_successor',
      {
        expected_release_version: 2,
        stable_request_model: 'minimax-m3',
        predecessor_public_model_id: 962,
        predecessor_model_name: 'minimax-m3',
        expected_predecessor_lifecycle: 'staging',
        expected_predecessor_approved_catalog: false,
        successor_public_model_id: 988,
        successor_model_name: 'minimax-m3-20260903',
        expected_successor_lifecycle: 'staging',
        expected_successor_approved_catalog: false,
        expected_model_sets: [{ model_set_id: 3, before_alias_exists: false }],
        reason: 'Replace the never-activated staging canonical with the exact qualified successor.',
      },
      expect.stringMatching(/^ui-model-release-lifecycle-/),
    );
    expect(mocks.activateStagingSuccessor.mock.calls[0][2]).toBe(
      mocks.activateStagingSuccessor.mock.calls[1][2],
    );
    expect(await screen.findByText('Staging successor cutover 收据 #501')).toBeTruthy();
    expect(screen.getByText('minimax-m3 → minimax-m3-20260903')).toBeTruthy();
  });

  it('echoes the server baseline and ordered selection options into authoring without auto-running lifecycle', async () => {
    const { props } = renderWorkspace();

    const authorButton = (await screen.findByText('编排新 Release')).closest('button');
    expect(authorButton).toBeTruthy();
    fireEvent.click(authorButton!);
    fireEvent.click(await screen.findByLabelText('选择 deployment 41 openai.responses.v1'));
    fireEvent.change(screen.getByLabelText('Authoring reason'), { target: { value: 'atomic cutover candidate' } });
    fireEvent.click(screen.getByRole('button', { name: '生成 Sealed Validated Release' }));

    await waitFor(() => expect(mocks.author).toHaveBeenCalledTimes(1));
    expect(mocks.author.mock.calls[0][0]).toEqual({
      public_model_id: 7,
      baseline: collection.models[0].authoring.baseline,
      selections: [{ deployment_id: 41, operation_id: 'openai.responses.v1' }],
      reason: 'atomic cutover candidate',
    });
    expect(mocks.author.mock.calls[0][1]).toMatch(/^ui-model-release-author-/);
    expect(mocks.activateInitial).not.toHaveBeenCalled();
    expect(mocks.startCanary).not.toHaveBeenCalled();
    expect(mocks.cutoverLegacy).not.toHaveBeenCalled();
    expect(props.onSelectRelease).toHaveBeenCalledWith(release.release_ref);
    expect(await screen.findByText('原子发布编排收据 #401')).toBeTruthy();
  });

  it('uses descriptor availability instead of inferring an action from the release stage', async () => {
    mocks.current.mockResolvedValue(lifecycleView([
      descriptor('activate_initial', {
        available: false,
        structurally_available: false,
        reason_code: 'server_says_no',
      }),
    ]));
    renderWorkspace();

    const button = await screen.findByRole('button', { name: '首次启用' });
    expect(button).toBeDisabled();
    fireEvent.click(button);
    expect(mocks.activateInitial).not.toHaveBeenCalled();
    expect(await screen.findByText('activate_initial: server_says_no')).toBeTruthy();
  });

  it('shows the M82 execution contract pinned to the exact release', async () => {
    renderWorkspace();

    expect(await screen.findByText(
      'deepseek.v4-flash.api@1.0.0 · context=1000000 · output=32768',
    )).toBeTruthy();
  });

  it('renders canonical receipt logical identity without inventing legacy raw public-model IDs', async () => {
    let confirmation: any;
    vi.spyOn(Modal, 'confirm').mockImplementation((config: any) => {
      confirmation = config;
      return { destroy: vi.fn(), update: vi.fn() } as any;
    });
    mocks.current.mockResolvedValue(lifecycleView([descriptor('activate_initial')]));
    renderWorkspace();

    fireEvent.click(await screen.findByRole('button', { name: '首次启用' }));
    await act(async () => { await confirmation.onOk(); });

    expect(await screen.findByText('Lifecycle 收据 #501')).toBeTruthy();
    expect(screen.getByText('public_model_id=7 · deepseek-v4-flash')).toBeTruthy();
    expect(screen.getByText(/canonical action 的 v1 收据冻结 logical public_model_id/)).toBeTruthy();
    expect(screen.queryByText('未分配 → 未分配')).toBeNull();
  });

  it('fails closed on the nested authoring descriptor without depending on redacted raw slot IDs', async () => {
    mocks.releaseList.mockResolvedValue({
      ...collection,
      models: [{
        ...collection.models[0],
        authoring: {
          ...collection.models[0].authoring,
          available: false,
          structurally_available: false,
          reason_code: 'runtime_slot_identity_mismatch',
          baseline: undefined,
          slot: {
            exists: true,
            public_model_id: 7,
            model_name: 'deepseek-v4-flash',
            canary_basis_points: 0,
            version: 0,
          },
          stable_release: undefined,
        },
      }],
    });
    renderWorkspace();

    const button = (await screen.findByText('编排新 Release')).closest('button');
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute('title', 'runtime_slot_identity_mismatch');
    fireEvent.click(button!);
    expect(mocks.author).not.toHaveBeenCalled();
  });

  it('retains authoring identity after an uncertain failure and rotates it after a determinate 4xx', async () => {
    mocks.author
      .mockRejectedValueOnce(new TypeError('network unavailable'))
      .mockRejectedValueOnce(new ApiError('invalid', 400, { code: 'invalid' }))
      .mockResolvedValueOnce(authoringReceipt());
    renderWorkspace();

    const authorButton = (await screen.findByText('编排新 Release')).closest('button');
    fireEvent.click(authorButton!);
    fireEvent.click(await screen.findByLabelText('选择 deployment 41 openai.responses.v1'));
    fireEvent.change(screen.getByLabelText('Authoring reason'), { target: { value: 'same exact intent' } });
    const submit = screen.getByRole('button', { name: '生成 Sealed Validated Release' });

    fireEvent.click(submit);
    await waitFor(() => expect(mocks.author).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(submit).not.toBeDisabled());
    fireEvent.click(submit);
    await waitFor(() => expect(mocks.author).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(submit).not.toBeDisabled());
    fireEvent.click(submit);
    await waitFor(() => expect(mocks.author).toHaveBeenCalledTimes(3));

    expect(mocks.author.mock.calls[0][1]).toBe(mocks.author.mock.calls[1][1]);
    expect(mocks.author.mock.calls[2][1]).not.toBe(mocks.author.mock.calls[1][1]);
  });

  it('reports a general authoring state conflict instead of claiming every 409 is baseline drift', async () => {
    const warningToast = vi.spyOn(Toast, 'warning');
    mocks.author.mockRejectedValue(new ApiError('conflict', 409, {
      code: 'canonical_deployment_release_authoring_conflict',
    }));
    renderWorkspace();

    fireEvent.click((await screen.findByText('编排新 Release')).closest('button')!);
    fireEvent.click(await screen.findByLabelText('选择 deployment 41 openai.responses.v1'));
    fireEvent.change(screen.getByLabelText('Authoring reason'), { target: { value: 'conflicting candidate' } });
    fireEvent.click(screen.getByRole('button', { name: '生成 Sealed Validated Release' }));

    await waitFor(() => expect(mocks.author).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(warningToast).toHaveBeenCalledWith(
      'Release 编排与当前服务端事实冲突；不会自动重试，正在刷新当前事实。',
    ));
    expect(mocks.author).toHaveBeenCalledTimes(1);
  });

  it('does not reinterpret an idempotency 409 as state drift', async () => {
    const errorToast = vi.spyOn(Toast, 'error');
    const warningToast = vi.spyOn(Toast, 'warning');
    mocks.author.mockRejectedValue(new ApiError('idempotency key conflict', 409, {
      code: 'canonical_deployment_release_authoring_idempotency_conflict',
    }));
    renderWorkspace();

    fireEvent.click((await screen.findByText('编排新 Release')).closest('button')!);
    fireEvent.click(await screen.findByLabelText('选择 deployment 41 openai.responses.v1'));
    fireEvent.change(screen.getByLabelText('Authoring reason'), { target: { value: 'duplicate key candidate' } });
    fireEvent.click(screen.getByRole('button', { name: '生成 Sealed Validated Release' }));

    await waitFor(() => expect(errorToast).toHaveBeenCalledWith('idempotency key conflict'));
    expect(warningToast).not.toHaveBeenCalled();
    expect(mocks.releaseList).toHaveBeenCalledTimes(1);
  });

  it('keeps a successful authoring receipt authoritative when the follow-up read refresh fails', async () => {
    const errorToast = vi.spyOn(Toast, 'error');
    const warningToast = vi.spyOn(Toast, 'warning');
    renderWorkspace({ onCanonicalStateChanged: vi.fn().mockRejectedValue(new Error('detail refresh unavailable')) });

    fireEvent.click((await screen.findByText('编排新 Release')).closest('button')!);
    fireEvent.click(await screen.findByLabelText('选择 deployment 41 openai.responses.v1'));
    fireEvent.change(screen.getByLabelText('Authoring reason'), { target: { value: 'author once' } });
    fireEvent.click(screen.getByRole('button', { name: '生成 Sealed Validated Release' }));

    expect(await screen.findByText('原子发布编排收据 #401')).toBeTruthy();
    expect(mocks.author).toHaveBeenCalledTimes(1);
    expect(errorToast).not.toHaveBeenCalled();
    await waitFor(() => expect(warningToast).toHaveBeenCalledWith(expect.stringContaining('Release 已成功生成')));
  });

  it('allows the server-owned legacy rollback contraction without a Gateway license and renders its receipt', async () => {
    let confirmation: any;
    vi.spyOn(Modal, 'confirm').mockImplementation((config: any) => {
      confirmation = config;
      return { destroy: vi.fn(), update: vi.fn() } as any;
    });
    mocks.current.mockResolvedValue(lifecycleView([
      descriptor('rollback_legacy', { licensed: true, safety_direction: 'contraction' }),
    ]));
    renderWorkspace();

    const button = await screen.findByRole('button', { name: '紧急恢复 Legacy V1' });
    expect(button).toBeDisabled();
    fireEvent.change(screen.getByLabelText('Lifecycle reason'), { target: { value: 'restore legacy during incident' } });
    expect(button).not.toBeDisabled();
    fireEvent.click(button);
    await act(async () => { await confirmation.onOk(); });

    expect(mocks.rollbackLegacy).toHaveBeenCalledWith(
      release.release_ref,
      { expected_release_version: 2, expected_slot_version: 8, reason: 'restore legacy during incident' },
      expect.stringMatching(/^ui-model-release-lifecycle-/),
    );
    expect(await screen.findByText('Lifecycle 收据 #501')).toBeTruthy();
    expect(screen.getAllByText('sha256:legacy')).toHaveLength(2);
    expect(screen.getByText('7 → 未分配')).toBeTruthy();
  });

  it('refreshes current facts after a 409 and never retries the mutation automatically', async () => {
    let confirmation: any;
    vi.spyOn(Modal, 'confirm').mockImplementation((config: any) => {
      confirmation = config;
      return { destroy: vi.fn(), update: vi.fn() } as any;
    });
    mocks.cutoverLegacy.mockRejectedValue(new ApiError('conflict', 409, {
      code: 'canonical_deployment_lifecycle_conflict',
    }));
    const { props } = renderWorkspace();

    fireEvent.change(await screen.findByLabelText('Lifecycle reason'), { target: { value: 'cut over after parity review' } });
    fireEvent.click(await screen.findByRole('button', { name: '切换至 Canonical V2' }));
    await act(async () => { await confirmation.onOk(); });

    await waitFor(() => expect(mocks.releaseList).toHaveBeenCalledTimes(2));
    expect(mocks.current).toHaveBeenCalledTimes(2);
    expect(mocks.cutoverLegacy).toHaveBeenCalledTimes(1);
    expect(props.onCanonicalStateChanged).toHaveBeenCalledTimes(1);
    expect(screen.queryByText('Lifecycle 收据 #501')).toBeNull();
  });

  it('retains one Idempotency-Key across an uncertain retry of the same lifecycle intent', async () => {
    let confirmation: any;
    vi.spyOn(Modal, 'confirm').mockImplementation((config: any) => {
      confirmation = config;
      return { destroy: vi.fn(), update: vi.fn() } as any;
    });
    mocks.current.mockResolvedValue(lifecycleView([descriptor('promote')]));
    mocks.promote
      .mockRejectedValueOnce(new TypeError('network unavailable'))
      .mockResolvedValueOnce(lifecycleReceipt('promote'));
    renderWorkspace();

    const button = await screen.findByRole('button', { name: '提升为稳定' });
    fireEvent.click(button);
    await act(async () => { await confirmation.onOk(); });
    await waitFor(() => expect(mocks.promote).toHaveBeenCalledTimes(1));

    fireEvent.click(button);
    await act(async () => { await confirmation.onOk(); });
    await waitFor(() => expect(mocks.promote).toHaveBeenCalledTimes(2));

    expect(mocks.promote.mock.calls[0][2]).toBe(mocks.promote.mock.calls[1][2]);
  });

  it('keeps an org_viewer on release reads while exposing no authoring or lifecycle write control', async () => {
    renderWorkspace({ canManage: false });

    expect(await screen.findByText(/组织只读角色可查看 canonical release/)).toBeTruthy();
    expect(mocks.releaseList).toHaveBeenCalledTimes(1);
    expect(mocks.current).toHaveBeenCalledTimes(1);
    expect(screen.queryByText('编排新 Release')).toBeNull();
    expect(screen.queryByRole('button', { name: '切换至 Canonical V2' })).toBeNull();
    expect(screen.getByText('只读视图不提供 lifecycle 写操作。')).toBeTruthy();
  });
});
