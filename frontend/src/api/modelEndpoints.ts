import { request } from './transport';

export type ModelEndpointProbe = {
  success: boolean;
  current: boolean;
  status_code: number;
  latency_ms: number;
  reported_model?: string;
  error_summary?: string;
  tested_at: string;
};

export type ModelEndpointTarget = {
  target_ref: string;
  channel_id: number;
  connection_name: string;
  provider: string;
  base_url: string;
  adapter_type: string;
  public_model: string;
  upstream_model: string;
  config_status: string;
  routing_epoch: number;
  priority: number;
  weight: number;
  circuit_state: string;
  eligible_credential_count: number;
  stable_assignment: boolean;
  canary_assignment: boolean;
  route_candidate: boolean;
  route_exclusion_reason?: string;
  state: string;
  reason_code: string;
  last_probe?: ModelEndpointProbe;
};

export type ModelEndpointModel = {
  public_model: string;
  display_name: string;
  lifecycle?: string;
  catalog_approved: boolean;
  state: string;
  reason_code: string;
  route_available: boolean;
  stable_release_ref?: string;
  stable_release_version?: number;
  canary_release_ref?: string;
  canary_release_version?: number;
  canary_basis_points: number;
  release_history_count: number;
  targets: ModelEndpointTarget[];
};

export type ModelEndpointProjection = {
  contract_version: string;
  generated_at: string;
  models: ModelEndpointModel[];
  summary: { total: number; route_available: number; attention: number; canary: number };
};

export type ProviderModelCatalogItem = {
  id: string;
  owned_by?: string;
  context_length?: number;
};

export type ProviderModelCatalog = {
  authority: 'live_provider_api';
  provider: string;
  channel_id: number;
  endpoint: string;
  fetched_at: string;
  data: ProviderModelCatalogItem[];
  total: number;
};

export const modelEndpointApi = {
  getModelEndpoints: () => request<ModelEndpointProjection>('/model-endpoints'),
  getProviderModels: (channelId: number) =>
    request<ProviderModelCatalog>(`/channels/${channelId}/provider-models`),
};
