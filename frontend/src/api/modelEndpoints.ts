import { request } from './transport';

export type ModelEndpointProbe = {
  success: boolean;
  current: boolean;
  status_code: number;
  latency_ms: number;
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

export type ModelEndpointActivationRequest = {
  channel_id: number;
  model: string;
  expected_routing_epoch: number;
};

export type ModelEndpointTestResponse = {
  success: boolean;
  model?: string;
  upstream_model?: string;
  error?: string;
  status_code?: number;
  latency_ms?: number;
  tokens_used?: number;
};

export const modelEndpointApi = {
  getModelEndpoints: () => request<ModelEndpointProjection>('/model-endpoints'),
  testChannelModel: (channelId: number, model: string, prompt = 'Reply with exactly: OK') =>
    request<ModelEndpointTestResponse>(`/channels/${channelId}/test`, {
      method: 'POST',
      body: JSON.stringify({ model, prompt }),
    }),
  activateModelEndpoint: (data: ModelEndpointActivationRequest) =>
    request<Record<string, unknown>>('/model-endpoints/activate', { method: 'POST', body: JSON.stringify(data) }),
  startModelEndpointCanary: (data: ModelEndpointActivationRequest & { canary_basis_points?: number }) =>
    request<Record<string, unknown>>('/model-endpoints/canary', { method: 'POST', body: JSON.stringify(data) }),
  recoverModelEndpoint: (data: ModelEndpointActivationRequest) =>
    request<Record<string, unknown>>('/model-endpoints/recover', { method: 'POST', body: JSON.stringify(data) }),
};
