import { request } from './transport';

export const canonicalOperationPaths = {
  'openai.chat_completions.v1': '/v1/chat/completions',
  'openai.embeddings.v1': '/v1/embeddings',
  'openai.responses.v1': '/v1/responses',
  'anthropic.messages.v1': '/v1/messages',
} as const;

export type CanonicalOperationID = keyof typeof canonicalOperationPaths;

export type CanonicalPublicModel = {
  id: number;
  model_name: string;
  display_name: string;
  provider_family: string;
  lifecycle: string;
  context_length: number;
  output_limit: number;
  approved_catalog: boolean;
  execution_profile_id: string;
  execution_profile_version: string;
};

export type CanonicalDeployment = {
  id: number;
  upstream_model: string;
  admin_state: string;
  config_revision: number;
  priority: number;
  routing_weight: number;
  max_context: number;
  default_max_output: number;
  degradation_threshold: number;
  circuit_state: string;
  circuit_revision: number;
  fail_count: number;
  retired_at?: string;
};

export type CanonicalConnection = {
  id: number;
  name: string;
  provider: string;
  base_url: string;
  adapter_type: string;
  admin_state: string;
  config_revision: number;
  retired_at?: string;
};

export type CanonicalCredentialBinding = {
  id: number;
  status: string;
  state_revision: number;
  fail_count: number;
  last_used_at?: string;
  cooldown_until?: string;
  eligible: boolean;
};

export type CanonicalCredentialAxis = {
  state: string;
  reason_code: string;
  bound_count: number;
  eligible_count: number;
  bindings: CanonicalCredentialBinding[];
};

export type CanonicalRuntimeMembership = {
  release_ref: string;
  release_stage: string;
  release_version: number;
  route_items_hash: string;
  ordinal: number;
  admission: string;
  priority?: number;
  weight?: number;
  expected_connection_revision: number;
  expected_deployment_revision: number;
  expected_operation_revision: number;
  protocol_profile_id: string;
  protocol_profile_version: string;
  public_path?: string;
  executor_contract_version?: string;
};

export type CanonicalExecutionSupport = {
  operation_id: CanonicalOperationID;
  public_path: string;
  protocol_profile_id: string;
  protocol_profile_version: string;
  executor_contract_version: string;
};

export type CanonicalProductionQualification = {
  required: boolean;
  qualified: boolean;
  state: string;
  approval_id?: number;
  probe_id?: number;
  https_origin?: string;
  approved_connection_revision?: number;
  approved_deployment_revision?: number;
  approved_operation_revision?: number;
  current_connection_revision?: number;
  current_deployment_revision?: number;
  current_operation_revision?: number;
};

export type CanonicalOperationProbe = {
  probe_id: number;
  success: boolean;
  current: boolean;
  status_code: number;
  latency_ms: number;
  reported_model?: string;
  error_summary?: string;
  credential_binding_id?: number;
  connection_revision: number;
  deployment_revision: number;
  credential_state_revision: number;
  probe_profile_id: string;
  probe_profile_version: string;
  operation_revision?: number;
  public_path?: string;
  executor_contract_version?: string;
  https_origin?: string;
  source_kind: string;
  tested_at: string;
  tested_by?: number;
};

export type CanonicalOperation = {
  operation_id: CanonicalOperationID;
  protocol_profile_id: string;
  protocol_profile_version: string;
  support_state: string;
  evidence_state: string;
  evidence_ref?: string;
  revision: number;
  verified_at?: string;
  circuit_state: string;
  circuit_revision: number;
  circuit_fail_count: number;
  probed_binding_count: number;
  current_success_count: number;
  current_failure_count: number;
  stale_probed_binding_count: number;
  probe_state: string;
  probe_reason_code: string;
  probe_available: boolean;
  probe_availability_reason_code: string;
  execution_support?: CanonicalExecutionSupport;
  production_qualification: CanonicalProductionQualification;
  latest_probe?: CanonicalOperationProbe;
  runtime: {
    state: string;
    reason_code: string;
    slot_version?: number;
    stable_membership?: CanonicalRuntimeMembership;
    canary_membership?: CanonicalRuntimeMembership;
  };
};

export function canonicalOperationProbeAvailability(operation: CanonicalOperation) {
  if (typeof operation.probe_available !== 'boolean' || !operation.probe_availability_reason_code) {
    return {
      available: false,
      reason: 'probe_capability_contract_missing',
      source: 'contract_missing' as const,
    };
  }
  return {
    available: operation.probe_available,
    reason: operation.probe_availability_reason_code,
    source: 'server' as const,
  };
}

export type CanonicalModelApplication = {
  contract_version: string;
  public_model: CanonicalPublicModel;
  deployment: CanonicalDeployment;
  connection: CanonicalConnection;
  credential: CanonicalCredentialAxis;
  operations: CanonicalOperation[];
  runtime: {
    state: string;
    reason_code: string;
    slot_version?: number;
    stable_release_ref?: string;
    stable_release_stage?: string;
    stable_release_version?: number;
    canary_release_ref?: string;
    canary_release_stage?: string;
    canary_release_version?: number;
    canary_basis_points?: number;
  };
  callability: {
    evaluated: boolean;
    state: string;
    reason_code: string;
  };
  actions: {
    identity_type: string;
    required_fields: string[];
    lifecycle_writer: string;
    license_middleware: string;
    rbac_middleware: string;
  };
};

export type CanonicalModelApplicationProjection = {
  contract_version: string;
  generated_at: string;
  applications: CanonicalModelApplication[];
  summary: {
    deployments: number;
    connections_paused: number;
    credentials_ready: number;
    probes_ready: number;
    runtime_assigned_operations: number;
  };
};

export type CanonicalModelApplicationCreateRequest = {
  connection: {
    id?: number;
    name?: string;
    provider?: string;
    base_url?: string;
    adapter_type?: string;
    adapter_config?: Record<string, unknown>;
  };
  public_model: {
    id?: number;
    model_name?: string;
    display_name?: string;
    provider_family?: string;
    lifecycle?: string;
    context_length?: number;
    output_limit?: number;
    execution_profile_id?: string;
    execution_profile_version?: string;
  };
  deployment: {
    upstream_model: string;
    priority?: number;
    routing_weight?: number;
    max_context?: number;
    default_max_output?: number;
    degradation_threshold?: number;
  };
  credential: {
    binding_id?: number;
    api_key?: string;
  };
  operation: {
    operation_id: CanonicalOperationID;
  };
};

export type CanonicalModelApplicationCreateResult = {
  application: CanonicalModelApplication;
  idempotency_replay: boolean;
  reused: {
    connection: boolean;
    public_model: boolean;
    deployment: boolean;
    credential: boolean;
    operation: boolean;
  };
};

export type CanonicalOperationProbeRequest = {
  credential_binding_id: number;
  probe_text?: string;
};

export type CanonicalOperationProbeResult = {
  probe_id: number;
  deployment_id: number;
  operation_id: CanonicalOperationID;
  credential_binding_id: number;
  success: boolean;
  provider_success: boolean;
  current: boolean;
  stale: boolean;
  operation_advanced: boolean;
  reason_code: string;
  provider_reason_code: string;
  status_code: number;
  latency_ms: number;
  reported_model?: string;
  evidence_ref?: string;
};

export const modelDeploymentApi = {
  list: (operationID?: CanonicalOperationID) => request<CanonicalModelApplicationProjection>(
    operationID ? `/model-deployments?operation=${encodeURIComponent(operationID)}` : '/model-deployments',
  ),
  get: (deploymentID: number, operationID: CanonicalOperationID) => request<CanonicalModelApplication>(
    `/model-deployments/${deploymentID}?operation=${encodeURIComponent(operationID)}`,
  ),
  create: (input: CanonicalModelApplicationCreateRequest, idempotencyKey: string) =>
    request<CanonicalModelApplicationCreateResult>('/model-deployments', {
      method: 'POST',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify(input),
    }),
  probe: (
    deploymentID: number,
    operationID: CanonicalOperationID,
    input: CanonicalOperationProbeRequest,
  ) => request<CanonicalOperationProbeResult>(
    `/model-deployments/${deploymentID}/operations/${encodeURIComponent(operationID)}/probes`,
    { method: 'POST', body: JSON.stringify(input) },
  ),
};
