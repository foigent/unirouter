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

export type CreateProviderChannel = {
  name: string;
  provider: string;
  base_url: string;
  models: string;
  model_mapping?: Record<string, string>;
  adapter_type: string;
  adapter_config?: Record<string, unknown>;
  priority?: number;
  routing_weight?: number;
  degradation_threshold?: number;
  api_key?: string;
};

export type UpdateProviderChannel = Partial<Omit<CreateProviderChannel, 'api_key'>> & {
  status?: 'active' | 'staged' | 'disabled';
  expected_routing_epoch?: number;
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

export type CreateProviderCredential = {
  channel_id: number;
  key: string;
  allowed_models?: string;
};

export type UpdateProviderCredential = {
  channel_id?: number;
  key?: string;
  status?: 'active' | 'disabled';
  allowed_models?: string;
};

export type RotateProviderCredential = {
  key: string;
  model: string;
  allowed_models?: string;
  prompt?: string;
};

export type RotateProviderCredentialResult = {
  id: number;
  key_prefix: string;
  rotated_from_id: number;
  channel_id: number;
  model: string;
  upstream_model: string;
  probe: {
    success: boolean;
    status_code: number;
    latency_ms: number;
  };
};

export const channelApi = {
  list: (page = 1, pageSize = 20) =>
    request<ProviderChannelPage>(appendParams('/channels', { page, page_size: pageSize })),
  create: (input: CreateProviderChannel) =>
    request<{ id: number; credential_configured: boolean }>('/channels', {
      method: 'POST',
      body: JSON.stringify(input),
    }),
  get: (id: number) => request<ProviderChannel>(`/channels/${id}`),
  update: (id: number, input: UpdateProviderChannel) =>
    request<{ message: string }>(`/channels/${id}`, {
      method: 'PUT',
      body: JSON.stringify(input),
    }),
  remove: (id: number) =>
    request<{ message: string }>(`/channels/${id}`, { method: 'DELETE' }),
};

export const providerCredentialApi = {
  list: (page = 1, pageSize = 20, channelId?: number) =>
    request<ProviderCredentialPage>(appendParams('/channel-keys', {
      page,
      page_size: pageSize,
      channel_id: channelId,
    })),
  create: (input: CreateProviderCredential) =>
    request<{ id: number }>('/channel-keys', {
      method: 'POST',
      body: JSON.stringify(input),
    }),
  update: (id: number, input: UpdateProviderCredential) =>
    request<{ success: boolean }>(`/channel-keys/${id}`, {
      method: 'PUT',
      body: JSON.stringify(input),
    }),
  rotate: (id: number, input: RotateProviderCredential) =>
    request<RotateProviderCredentialResult>(`/channel-keys/${id}/rotate`, {
      method: 'POST',
      body: JSON.stringify(input),
    }),
  remove: (id: number) =>
    request<{ success: boolean }>(`/channel-keys/${id}`, { method: 'DELETE' }),
};
