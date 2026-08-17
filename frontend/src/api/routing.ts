import { appendParams, request } from './transport';

export type RoutingPreviewCandidate = {
  channel_id: number;
  channel_name: string;
  provider: string;
  priority: number;
  weight: number;
  order?: number;
  circuit_state: string;
  eligible_key_count: number;
  reason: string;
  requires_probe: boolean;
};

export type RoutingPreview = {
  policy_version: string;
  config_version: string;
  snapshot_version: string;
  snapshot_source: string;
  degraded: boolean;
  model: string;
  candidates: RoutingPreviewCandidate[];
  exclusions: RoutingPreviewCandidate[];
};

export type RuntimeChannelOverride = {
  channel_id: number;
  expected_routing_epoch: number;
  admission: 'inherit' | 'allow' | 'deny';
  priority?: number;
  weight?: number;
};

export type RuntimeProviderPackPin = {
  pack_id: string;
  pack_version: string;
  manifest_sha256: string;
};

export type RuntimeSimulationReport = {
  report_version: string;
  overall_risk: string;
  summary: { unchanged: number; compatible: number; review_required: number; breaking: number };
  route_changes: Array<{ field: string; baseline: string; candidate: string; risk: string; reason: string }>;
};

export type RuntimeRelease = {
  id: number;
  release_ref: string;
  model_name: string;
  candidate_version: string;
  candidate_hash: string;
  baseline_hash: string;
  candidate: {
    contract_version: string;
    model_name: string;
    overrides: RuntimeChannelOverride[];
  };
  provider_pack_pins: RuntimeProviderPackPin[];
  simulation_report: RuntimeSimulationReport;
  overall_risk: string;
  stage: 'draft' | 'validated' | 'shadow' | 'canary' | 'active' | 'rolled_back' | 'rejected' | string;
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

export type RuntimeReleaseSummary = {
  active_records: number;
  current_stable: number;
  retained_active: number;
  canary: number;
  pending: number;
  history: number;
};

export type RuntimeReleasePage = {
  data: RuntimeRelease[];
  total: number;
  page: number;
  page_size: number;
  summary: RuntimeReleaseSummary;
};

export type RuntimeChangeSlot = {
  model_name: string;
  stable_release_ref?: string;
  canary_release_ref?: string;
  canary_basis_points: number;
  slot_version: number;
  runtime_mutation: string;
};

export type CreateRuntimeRelease = {
  model_name: string;
  reason: string;
  overrides: RuntimeChannelOverride[];
};

function transition(releaseRef: string, action: string, body: Record<string, unknown>) {
  return request<RuntimeRelease>(`/runtime-changes/${encodeURIComponent(releaseRef)}/${action}`, {
    method: 'POST',
    body: JSON.stringify(body),
  });
}

export const routingApi = {
  preview: (model: string) =>
    request<RoutingPreview>(`/routing/preview?model=${encodeURIComponent(model)}`),
  listReleases: (model?: string, page = 1, pageSize = 20) =>
    request<RuntimeReleasePage>(appendParams('/runtime-changes', { model, page, page_size: pageSize })),
  getRelease: (releaseRef: string) =>
    request<RuntimeRelease>(`/runtime-changes/${encodeURIComponent(releaseRef)}`),
  getSlot: (model: string) =>
    request<RuntimeChangeSlot>(appendParams('/runtime-changes/slot', { model })),
  createRelease: (input: CreateRuntimeRelease) =>
    request<RuntimeRelease>('/runtime-changes', { method: 'POST', body: JSON.stringify(input) }),
  validateRelease: (releaseRef: string, expectedVersion: number) =>
    transition(releaseRef, 'validate', { expected_version: expectedVersion }),
  activateInitialRelease: (releaseRef: string, expectedVersion: number) =>
    transition(releaseRef, 'activate-initial', { expected_version: expectedVersion }),
  markReleaseShadow: (releaseRef: string, expectedVersion: number) =>
    transition(releaseRef, 'shadow', { expected_version: expectedVersion }),
  startReleaseCanary: (releaseRef: string, expectedVersion: number, canaryBasisPoints: number) =>
    transition(releaseRef, 'canary', { expected_version: expectedVersion, canary_basis_points: canaryBasisPoints }),
  promoteRelease: (releaseRef: string, expectedVersion: number) =>
    transition(releaseRef, 'promote', { expected_version: expectedVersion }),
  rollbackRelease: (releaseRef: string, expectedVersion: number) =>
    transition(releaseRef, 'rollback', { expected_version: expectedVersion }),
  rejectRelease: (releaseRef: string, expectedVersion: number, reason: string) =>
    transition(releaseRef, 'reject', { expected_version: expectedVersion, reason }),
};
