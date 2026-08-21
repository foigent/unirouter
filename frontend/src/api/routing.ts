import { request } from './transport';

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

export const routingApi = {
  preview: (model: string) =>
    request<RoutingPreview>(`/routing/preview?model=${encodeURIComponent(model)}`),
};
