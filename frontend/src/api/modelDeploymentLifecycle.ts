import { request } from './transport';

export type CanonicalDeploymentLifecycleAction =
  | 'activate_initial'
  | 'start_canary'
  | 'promote'
  | 'cancel_canary'
  | 'rollback_stable'
  | 'cutover_legacy'
  | 'rollback_legacy';

export type CanonicalDeploymentRoute = {
  deployment_id: number;
  model_operation_id: string;
  expected_connection_revision: number;
  expected_deployment_revision: number;
  expected_operation_revision: number;
  protocol_profile_id: string;
  protocol_profile_version: string;
  public_path?: string;
  executor_contract_version?: string;
  // Server-owned all-or-none M77 evidence for qualified public non-stream routes.
  public_execution_binding_id?: number;
  delivery_contract_version?: string;
  charge_contract_version?: string;
  idempotency_contract_version?: string;
  admission: string;
  priority?: number;
  weight?: number;
};

export type CanonicalDeploymentLifecycleRelease = {
  id: number;
  release_ref: string;
  public_model_id: number;
  model_name: string;
  candidate_version: string;
  candidate_hash: string;
  baseline_hash: string;
  route_items_hash: string;
  route_items_count: number;
  route_items_sealed_at?: string;
  candidate: {
    contract_version: string;
    public_model_id: number;
    model_name: string;
    deployment_routes: CanonicalDeploymentRoute[];
  };
  stage: string;
  version: number;
  parent_stable_release_id?: number;
  canary_basis_points: number;
  reason: string;
  created_by: number;
  validated_by?: number;
  activated_by?: number;
  created_at: string;
  validated_at?: string;
  activated_at?: string;
  rolled_back_at?: string;
  updated_at: string;
};

export type CanonicalDeploymentLifecycleSlotState = {
  exists: boolean;
  public_model_id: number;
  assigned_public_model_id?: number;
  model_name: string;
  stable_release_id?: number;
  canary_release_id?: number;
  canary_basis_points: number;
  version: number;
};

export type CanonicalDeploymentLifecycleActionDescriptor = {
  action: CanonicalDeploymentLifecycleAction;
  method: 'POST';
  path: string;
  available: boolean;
  reason_code: string;
  structurally_available: boolean;
  authorized: boolean;
  licensed: boolean;
  license_feature?: 'gateway';
  idempotency_required: true;
  required_fields: string[];
  safety_direction: 'expansion' | 'contraction';
};

export type CanonicalDeploymentLifecycleView = {
  contract_version: 'unirouter.model-deployment-lifecycle-receipt.v1';
  release: CanonicalDeploymentLifecycleRelease;
  slot: CanonicalDeploymentLifecycleSlotState;
  stable_parent?: CanonicalDeploymentLifecycleReleaseSummary;
  actions: CanonicalDeploymentLifecycleActionDescriptor[];
};

export type CanonicalDeploymentLifecycleReleaseSummary = {
  id: number;
  release_ref: string;
  public_model_id: number;
  model_name: string;
  candidate_version: string;
  candidate_hash: string;
  route_items_hash: string;
  route_items_count: number;
  route_items_sealed_at?: string;
  stage: string;
  version: number;
  parent_stable_release_id?: number;
  updated_at: string;
};

export type CanonicalDeploymentLifecycleBaseRequest = {
  expected_release_version: number;
  reason?: string;
};

export type CanonicalDeploymentLifecycleSlotRequest = CanonicalDeploymentLifecycleBaseRequest & {
  expected_slot_version: number;
};

export type CanonicalDeploymentStartCanaryRequest = CanonicalDeploymentLifecycleSlotRequest & {
  canary_basis_points: number;
};

export type CanonicalDeploymentLegacyLifecycleRequest = CanonicalDeploymentLifecycleSlotRequest & {
  reason: string;
};

export type CanonicalDeploymentLifecycleReleaseReceipt = {
  role: 'target' | 'stable_parent';
  release_id: number;
  release_ref: string;
  candidate_hash: string;
  route_items_hash: string;
  before_stage: string;
  before_version: number;
  after_stage: string;
  after_version: number;
};

export type CanonicalDeploymentLifecycleQualificationReceipt = {
  release_id: number;
  release_role: 'target' | 'stable_parent';
  ordinal: number;
  deployment_id: number;
  operation_id: string;
  qualification_required: boolean;
  qualified: boolean;
  qualification_state: string;
  approval_id?: number;
  probe_id?: number;
  connection_id?: number;
  https_origin?: string;
  public_path?: string;
  executor_contract_version?: string;
  expected_connection_revision: number;
  expected_deployment_revision: number;
  expected_operation_revision: number;
  current_connection_revision?: number;
  current_deployment_revision?: number;
  current_operation_revision?: number;
  approved_connection_revision?: number;
  approved_deployment_revision?: number;
  approved_operation_revision?: number;
};

export type CanonicalDeploymentLifecycleReceipt = {
  contract_version: 'unirouter.model-deployment-lifecycle-receipt.v1';
  receipt_id: number;
  action: CanonicalDeploymentLifecycleAction;
  target_release_ref: string;
  customer_org_id: number;
  actor_user_id?: number;
  server_operation_id: string;
  original_http_status: 200;
  idempotency_replay: boolean;
  reason?: string;
  legacy_plan_hash?: string;
  deployment_plan_hash?: string;
  slot: {
    public_model_id: number;
    model_name: string;
    before_public_model_id?: number;
    before_stable_release_id?: number;
    before_canary_release_id?: number;
    before_canary_basis_points: number;
    before_version: number;
    after_public_model_id?: number;
    after_stable_release_id?: number;
    after_canary_release_id?: number;
    after_canary_basis_points: number;
    after_version: number;
  };
  releases: CanonicalDeploymentLifecycleReleaseReceipt[];
  qualifications: CanonicalDeploymentLifecycleQualificationReceipt[];
  created_at: string;
  completed_at: string;
};

function releasePath(releaseRef: string) {
  return `/model-deployment-releases/${encodeURIComponent(releaseRef)}`;
}

function mutate(
  releaseRef: string,
  actionPath: 'activate-initial' | 'start-canary' | 'promote' | 'cancel-canary' | 'rollback-stable' | 'cutover-legacy' | 'rollback-legacy',
  input: CanonicalDeploymentLifecycleBaseRequest | CanonicalDeploymentLifecycleSlotRequest | CanonicalDeploymentStartCanaryRequest,
  idempotencyKey: string,
) {
  return request<CanonicalDeploymentLifecycleReceipt>(`${releasePath(releaseRef)}/${actionPath}`, {
    method: 'POST',
    headers: { 'Idempotency-Key': idempotencyKey },
    body: JSON.stringify(input),
  });
}

export const modelDeploymentLifecycleApi = {
  current: (releaseRef: string) => request<CanonicalDeploymentLifecycleView>(releasePath(releaseRef)),
  activateInitial: (
    releaseRef: string,
    input: CanonicalDeploymentLifecycleBaseRequest,
    idempotencyKey: string,
  ) => mutate(releaseRef, 'activate-initial', input, idempotencyKey),
  startCanary: (
    releaseRef: string,
    input: CanonicalDeploymentStartCanaryRequest,
    idempotencyKey: string,
  ) => mutate(releaseRef, 'start-canary', input, idempotencyKey),
  promote: (
    releaseRef: string,
    input: CanonicalDeploymentLifecycleSlotRequest,
    idempotencyKey: string,
  ) => mutate(releaseRef, 'promote', input, idempotencyKey),
  cancelCanary: (
    releaseRef: string,
    input: CanonicalDeploymentLifecycleSlotRequest,
    idempotencyKey: string,
  ) => mutate(releaseRef, 'cancel-canary', input, idempotencyKey),
  rollbackStable: (
    releaseRef: string,
    input: CanonicalDeploymentLifecycleSlotRequest,
    idempotencyKey: string,
  ) => mutate(releaseRef, 'rollback-stable', input, idempotencyKey),
  cutoverLegacy: (
    releaseRef: string,
    input: CanonicalDeploymentLegacyLifecycleRequest,
    idempotencyKey: string,
  ) => mutate(releaseRef, 'cutover-legacy', input, idempotencyKey),
  rollbackLegacy: (
    releaseRef: string,
    input: CanonicalDeploymentLegacyLifecycleRequest,
    idempotencyKey: string,
  ) => mutate(releaseRef, 'rollback-legacy', input, idempotencyKey),
};
