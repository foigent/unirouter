import type {
  CanonicalDeploymentLifecycleRelease,
  CanonicalDeploymentLifecycleReleaseSummary,
  CanonicalDeploymentLifecycleSlotState,
} from './modelDeploymentLifecycle';
import { request } from './transport';

export type CanonicalDeploymentReleaseBaselineKind = 'initial' | 'stable' | 'legacy_cutover';

export type CanonicalDeploymentReleaseBaseline = {
  kind: CanonicalDeploymentReleaseBaselineKind;
  expected_slot_version: number;
  stable_release_ref?: string;
  expected_stable_release_version?: number;
};

export type CanonicalDeploymentReleaseSelection = {
  deployment_id: number;
  operation_id: string;
};

export type CanonicalDeploymentReleaseSelectionOption = CanonicalDeploymentReleaseSelection & {
  upstream_model?: string;
  connection_name?: string;
  profile_definition_digest?: string;
  max_context?: number;
  default_max_output?: number;
  capability_approval_ids?: number[];
};

export type CanonicalDeploymentReleasePublicModel = {
  id: number;
  model_name: string;
  display_name: string;
  lifecycle: string;
};

export type CanonicalDeploymentReleaseAuthoringDescriptor = {
  available: boolean;
  reason_code: string;
  structurally_available: boolean;
  authorized: boolean;
  licensed: boolean;
  license_feature?: 'gateway';
  method: 'POST';
  path: '/api/v1/model-deployment-releases';
  idempotency_required: true;
  required_fields: string[];
  baseline?: CanonicalDeploymentReleaseBaseline;
  slot: CanonicalDeploymentLifecycleSlotState;
  stable_release?: CanonicalDeploymentLifecycleReleaseSummary;
};

export type CanonicalDeploymentReleaseAuthoringOption = {
  public_model: CanonicalDeploymentReleasePublicModel;
  authoring: CanonicalDeploymentReleaseAuthoringDescriptor;
  selection_options: CanonicalDeploymentReleaseSelectionOption[];
};

export type CanonicalDeploymentReleaseCollection = {
  contract_version: 'unirouter.model-deployment-release-authoring-options.v1';
  generated_at: string;
  models: CanonicalDeploymentReleaseAuthoringOption[];
  releases: CanonicalDeploymentLifecycleReleaseSummary[];
};

export type CanonicalDeploymentReleaseAuthoringRequest = {
  profile_definition_digest?: string;
  public_context_limit?: number;
  public_output_limit?: number;
  capability_approval_ids?: number[];
  public_model_id: number;
  baseline: CanonicalDeploymentReleaseBaseline;
  selections: CanonicalDeploymentReleaseSelection[];
  reason: string;
};

export type CanonicalDeploymentReleaseAuthoringReceipt = {
  contract_version: 'unirouter.model-deployment-release-authoring-receipt.v1';
  receipt_id: number;
  customer_org_id: number;
  actor_user_id?: number;
  server_operation_id: string;
  original_http_status: 201;
  original_location: string;
  idempotency_replay: boolean;
  baseline: CanonicalDeploymentReleaseBaseline;
  selections: CanonicalDeploymentReleaseSelection[];
  release: CanonicalDeploymentLifecycleRelease;
  created_at: string;
  completed_at: string;
};

export const modelDeploymentReleaseApi = {
  list: () => request<CanonicalDeploymentReleaseCollection>('/model-deployment-releases'),
  author: (input: CanonicalDeploymentReleaseAuthoringRequest, idempotencyKey: string) =>
    request<CanonicalDeploymentReleaseAuthoringReceipt>('/model-deployment-releases', {
      method: 'POST',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify(input),
    }),
};
