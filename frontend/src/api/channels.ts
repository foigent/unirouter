import { appendParams, request } from './transport';

export type ProviderChannel = {
  id: number;
  name: string;
  provider: string;
  base_url: string;
  models: string;
  model_mapping: Record<string, string>;
  adapter_type: string;
  adapter_config: Record<string, unknown>;
  priority: number;
  degradation_threshold: number;
  status: string;
  effective_status: string;
  published_models: string;
  routing_weight: number;
  routing_epoch: number;
  circuit_state: string;
  fail_count: number;
  created_at: string;
};

export type ProviderChannelPage = {
  data: ProviderChannel[];
  total: number;
};

export type ProviderCredential = {
  id: number;
  channel_id: number;
  channel_name: string;
  channel_status: string;
  key_prefix: string;
  allowed_models: string;
  status: string;
  fail_count: number;
  last_used_at?: string | null;
  cooldown_until?: string | null;
  state_epoch: number;
  created_at: string;
  updated_at: string;
};

export type ProviderCredentialPage = {
  data: ProviderCredential[];
  total: number;
};

export const channelApi = {
  list: (page = 1, pageSize = 20) =>
    request<ProviderChannelPage>(appendParams('/channels', { page, page_size: pageSize })),
  get: (id: number) => request<ProviderChannel>(`/channels/${id}`),
};

export const providerCredentialApi = {
  list: (page = 1, pageSize = 20, channelId?: number) =>
    request<ProviderCredentialPage>(appendParams('/channel-keys', {
      page,
      page_size: pageSize,
      channel_id: channelId,
    })),
};
