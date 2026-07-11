const BASE_URL = '/api/v1';

export class ApiError extends Error {
  status: number;
  payload: unknown;

  constructor(message: string, status: number, payload: unknown) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.payload = payload;
  }
}

function getErrorMessage(payload: unknown, fallback: string) {
  if (payload && typeof payload === 'object') {
    const data = payload as { message?: unknown; error?: unknown };
    if (typeof data.message === 'string') return data.message;
    if (typeof data.error === 'string') return data.error;
    if (data.error && typeof data.error === 'object') {
      const nested = data.error as { message?: unknown };
      if (typeof nested.message === 'string') return nested.message;
    }
  }
  return fallback;
}

async function parseResponse(res: Response) {
  const text = await res.text();
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

async function request(path: string, options: RequestInit = {}) {
  const token = localStorage.getItem('token');
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...((options.headers as Record<string, string>) || {}),
  };
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const res = await fetch(`${BASE_URL}${path}`, { ...options, headers });
  const payload = await parseResponse(res);

  if (res.status === 401) {
    localStorage.removeItem('token');
    window.location.href = '/login';
    throw new ApiError(getErrorMessage(payload, 'Unauthorized'), res.status, payload);
  }

  if (!res.ok) {
    throw new ApiError(getErrorMessage(payload, `Request failed with status ${res.status}`), res.status, payload);
  }

  return payload;
}

function appendParams(path: string, params: Record<string, string | number | undefined | null>) {
  const search = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') {
      search.set(key, String(value));
    }
  });
  const query = search.toString();
  return query ? `${path}?${query}` : path;
}

export const api = {
  // Auth
  login: (username: string, password: string) =>
    request('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  getOIDCConfig: () => request('/auth/oidc/config'),
  getOIDCStartURL: (redirect = '/') =>
    `${BASE_URL}/auth/oidc/start?redirect=${encodeURIComponent(redirect)}`,

  // Dashboard
  getDashboardStats: (startTime?: string, endTime?: string) => {
    let url = '/dashboard/stats';
    const params: string[] = [];
    if (startTime) params.push(`start_time=${encodeURIComponent(startTime)}`);
    if (endTime) params.push(`end_time=${encodeURIComponent(endTime)}`);
    if (params.length) url += '?' + params.join('&');
    return request(url);
  },

  // Channels
  getChannels: (page = 1, pageSize = 20) =>
    request(`/channels?page=${page}&page_size=${pageSize}`),
  createChannel: (data: any) =>
    request('/channels', { method: 'POST', body: JSON.stringify(data) }),
  updateChannel: (id: number, data: any) =>
    request(`/channels/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteChannel: (id: number) =>
    request(`/channels/${id}`, { method: 'DELETE' }),

  // Keys
  getKeys: (page = 1, pageSize = 20, filters?: { project_id?: number }) =>
    request(appendParams('/keys', { page, page_size: pageSize, project_id: filters?.project_id })),
  createKey: (data: any) =>
    request('/keys', { method: 'POST', body: JSON.stringify(data) }),
  updateKey: (id: number, data: any) =>
    request(`/keys/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  disableKey: (id: number) =>
    request(`/keys/${id}/disable`, { method: 'POST' }),

  // Wallet
  getWallet: (orgId?: number) => request(appendParams('/wallet', { org_id: orgId })),
  getWalletTransactions: (page = 1, pageSize = 20, orgId?: number) =>
    request(appendParams('/wallet/transactions', { page, page_size: pageSize, org_id: orgId })),
  topUp: (data: { org_id?: number; amount_cents: number; description?: string; client_request_id?: string }) =>
    request('/wallet/topup', { method: 'POST', body: JSON.stringify(data) }),
  adjustWallet: (data: { org_id?: number; target_balance: number; description?: string }) =>
    request('/wallet/adjust', { method: 'POST', body: JSON.stringify(data) }),

  // Logs (with optional filters)
  getLogs: (page = 1, pageSize = 20, filters?: {
    model?: string; provider?: string; status_code?: string;
    start_time?: string; end_time?: string; request_id?: string; client_request_id?: string;
    latency_min?: string; latency_max?: string; search?: string;
    trace_id?: string; workflow_id?: string; agent_id?: string; attempt_id?: string; upstream_request_id?: string;
    evidence_ref?: string; source_event_hash?: string;
    org_id?: number; project_id?: number; usage_source?: string;
  }) => {
    return request(appendParams('/logs', {
      page,
      page_size: pageSize,
      model: filters?.model,
      provider: filters?.provider,
      status_code: filters?.status_code,
      start_time: filters?.start_time,
      end_time: filters?.end_time,
      request_id: filters?.request_id,
      client_request_id: filters?.client_request_id,
      trace_id: filters?.trace_id,
      workflow_id: filters?.workflow_id,
      agent_id: filters?.agent_id,
      attempt_id: filters?.attempt_id,
      upstream_request_id: filters?.upstream_request_id,
      evidence_ref: filters?.evidence_ref,
      source_event_hash: filters?.source_event_hash,
      latency_min: filters?.latency_min,
      latency_max: filters?.latency_max,
      search: filters?.search,
      org_id: filters?.org_id,
      project_id: filters?.project_id,
      usage_source: filters?.usage_source,
    }));
  },
  getLogDetail: (id: number, orgId?: number) => request(appendParams(`/logs/${id}`, { org_id: orgId })),
  getLogTrace: (id: number, orgId?: number) => request(appendParams(`/logs/${id}/trace`, { org_id: orgId })),

  // PRD5 Data Exchange Control (platform admin only)
  getDataExchangeItems: (page = 1, pageSize = 20, status?: string) =>
    request(appendParams('/data-exchange/items', { page, page_size: pageSize, status })),
  createDataExchangeItem: (data: any) =>
    request('/data-exchange/items', { method: 'POST', body: JSON.stringify(data) }),
  updateDataExchangeItem: (id: number, data: any) =>
    request(`/data-exchange/items/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  getDataExchanges: (page = 1, pageSize = 20) =>
    request(appendParams('/data-exchange/exchanges', { page, page_size: pageSize })),
  createDataExchange: (data: any) =>
    request('/data-exchange/exchanges', { method: 'POST', body: JSON.stringify(data) }),
  getDataExchangeActivity: (id: number) =>
    request(`/data-exchange/exchanges/${id}/activity`),
  createDataExchangeDelivery: (id: number, data: any) =>
    request(`/data-exchange/exchanges/${id}/deliveries`, { method: 'POST', body: JSON.stringify(data) }),
  createDataExchangeReceipt: (id: number, data: any) =>
    request(`/data-exchange/exchanges/${id}/receipts`, { method: 'POST', body: JSON.stringify(data) }),
  markDataExchangeArtifactDeleted: (id: number) =>
    request(`/data-exchange/exchanges/${id}/artifact-delete`, { method: 'POST' }),

  // PRD2 Batch 23 product surfaces
  getUsageAnalytics: (filters?: {
    group_by?: string; org_id?: string | number; project_id?: string | number; key_id?: string | number;
    owner_user_id?: string | number; key_type?: string; scope?: string;
    model?: string; alias?: string; provider?: string; channel?: string; status?: string;
    usage_source?: string; start_time?: string; end_time?: string; format?: string;
  }) => request(appendParams('/usage/analytics', {
    group_by: filters?.group_by,
    org_id: filters?.org_id,
    project_id: filters?.project_id,
    key_id: filters?.key_id,
    owner_user_id: filters?.owner_user_id,
    key_type: filters?.key_type,
    scope: filters?.scope,
    model: filters?.model,
    alias: filters?.alias,
    provider: filters?.provider,
    channel: filters?.channel,
    status: filters?.status,
    usage_source: filters?.usage_source,
    start_time: filters?.start_time,
    end_time: filters?.end_time,
    format: filters?.format,
  })),
  getInternalStatus: (filters?: { org_id?: string | number; project_id?: string | number }) =>
    request(appendParams('/internal-status', { org_id: filters?.org_id, project_id: filters?.project_id })),
  getRuntimeStatus: (orgId?: number) => request(appendParams('/runtime/status', { org_id: orgId })),
  getErrorDocs: () => request('/error-docs'),

  // Projects and developer catalog
  getProjects: (page = 1, pageSize = 20, filters?: { status?: string; org_id?: number }) =>
    request(appendParams('/projects', { page, page_size: pageSize, status: filters?.status, org_id: filters?.org_id })),
  getProject: (id: number) => request(`/projects/${id}`),
  getProjectModelSets: (id: number) => request(`/projects/${id}/model-sets`),
  replaceProjectModelSets: (id: number, modelSetIds: number[]) =>
    request(`/projects/${id}/model-sets`, { method: 'PUT', body: JSON.stringify({ model_set_ids: modelSetIds }) }),
  getModelCatalog: (page = 1, pageSize = 20, filters?: { project_id?: number; org_id?: number }) =>
    request(appendParams('/model-catalog', { page, page_size: pageSize, project_id: filters?.project_id, org_id: filters?.org_id })),

  // Model governance
  getModelRegistry: (page = 1, pageSize = 20, filters?: { lifecycle?: string; approved_only?: boolean; q?: string; org_id?: number }) =>
    request(appendParams('/model-registry', {
      page,
      page_size: pageSize,
      lifecycle: filters?.lifecycle,
      approved_only: filters?.approved_only ? 'true' : undefined,
      q: filters?.q,
      org_id: filters?.org_id,
    })),
  createModelRegistry: (data: any) =>
    request('/model-registry', { method: 'POST', body: JSON.stringify(data) }),
  updateModelRegistry: (id: number, data: any, orgId?: number) =>
    request(appendParams(`/model-registry/${id}`, { org_id: orgId }), { method: 'PUT', body: JSON.stringify(data) }),
  getModelAliases: (page = 1, pageSize = 20, filters?: { org_id?: number }) =>
    request(appendParams('/model-aliases', { page, page_size: pageSize, org_id: filters?.org_id })),
  createModelAlias: (data: any) =>
    request('/model-aliases', { method: 'POST', body: JSON.stringify(data) }),
  updateModelAlias: (id: number, data: any, orgId?: number) =>
    request(appendParams(`/model-aliases/${id}`, { org_id: orgId }), { method: 'PUT', body: JSON.stringify(data) }),
  getModelSets: (page = 1, pageSize = 20, filters?: { org_id?: number }) =>
    request(appendParams('/model-sets', { page, page_size: pageSize, org_id: filters?.org_id })),
  createModelSet: (data: any) =>
    request('/model-sets', { method: 'POST', body: JSON.stringify(data) }),
  getModelSetItems: (id: number, filters?: { org_id?: number }) =>
    request(appendParams(`/model-sets/${id}/items`, { org_id: filters?.org_id })),
  replaceModelSetItems: (id: number, data: { items: { item_type: string; item_name: string }[] }, orgId?: number) =>
    request(appendParams(`/model-sets/${id}/items`, { org_id: orgId }), { method: 'PUT', body: JSON.stringify(data) }),

  // Model prices
  getModelPrices: () => request('/model-prices'),
  getModelPriceList: (page = 1, pageSize = 20) =>
    request(`/model-prices?page=${page}&page_size=${pageSize}`),
  getModelNames: () => request('/model-names'),
  createModelPrice: (data: { model_name: string; input_price_cents: number; output_price_cents: number }) =>
    request('/model-prices', { method: 'POST', body: JSON.stringify(data) }),
  updateModelPrice: (id: number, data: { model_name?: string; input_price_cents?: number; output_price_cents?: number }) =>
    request(`/model-prices/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteModelPrice: (id: number) =>
    request(`/model-prices/${id}`, { method: 'DELETE' }),

  // Settings
  getSettings: () => request('/settings'),
  updateSetting: (key: string, value: any) =>
    request(`/settings/${key}`, { method: 'PUT', body: JSON.stringify({ value }) }),

  // License
  getLicense: () => request('/license'),
  updateLicense: (license: any) =>
    request('/license', { method: 'PUT', body: JSON.stringify({ license }) }),

  // Channel Keys
  getChannelKeys: (page = 1, pageSize = 20) =>
    request(`/channel-keys?page=${page}&page_size=${pageSize}`),
  createChannelKey: (data: any) =>
    request('/channel-keys', { method: 'POST', body: JSON.stringify(data) }),
  updateChannelKey: (id: number, data: any) =>
    request(`/channel-keys/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteChannelKey: (id: number) =>
    request(`/channel-keys/${id}`, { method: 'DELETE' }),

  // Users
  getUsers: (page = 1, pageSize = 20) =>
    request(`/users?page=${page}&page_size=${pageSize}`),
  createUser: (data: any) =>
    request('/users', { method: 'POST', body: JSON.stringify(data) }),
  updateUser: (id: number, data: any) =>
    request(`/users/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteUser: (id: number) =>
    request(`/users/${id}`, { method: 'DELETE' }),

  // Grants
  getGrants: (page = 1, pageSize = 20, orgId?: number) =>
    request(appendParams('/grants', { page, page_size: pageSize, org_id: orgId })),
  createGrant: (data: any) =>
    request('/grants', { method: 'POST', body: JSON.stringify(data) }),

  // Audit Logs
  getAuditLogs: (page = 1, pageSize = 20, filters?: {
    org_id?: string; project_id?: string; actor_id?: string; action?: string; resource?: string; resource_id?: string;
    request_id?: string; client_request_id?: string; source?: string; start_time?: string; end_time?: string;
  }) => request(appendParams('/audit-logs', {
    page,
    page_size: pageSize,
    org_id: filters?.org_id,
    project_id: filters?.project_id,
    actor_id: filters?.actor_id,
    action: filters?.action,
    resource: filters?.resource,
    resource_id: filters?.resource_id,
    request_id: filters?.request_id,
    client_request_id: filters?.client_request_id,
    source: filters?.source,
    start_time: filters?.start_time,
    end_time: filters?.end_time,
  })),

  // Org Model Prices
  getOrgModelPrices: (page = 1, pageSize = 20, orgId?: number) =>
    request(appendParams('/org-model-prices', { page, page_size: pageSize, org_id: orgId })),
  createOrgModelPrice: (data: { org_id: number; model_name: string; multiplier: number }) =>
    request('/org-model-prices', { method: 'POST', body: JSON.stringify(data) }),
  updateOrgModelPrice: (id: number, data: { multiplier: number }) =>
    request(`/org-model-prices/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteOrgModelPrice: (id: number) =>
    request(`/org-model-prices/${id}`, { method: 'DELETE' }),

  // User profile
  getMe: () => request('/me'),
  changePassword: (data: { old_password: string; new_password: string }) =>
    request('/me/password', { method: 'PUT', body: JSON.stringify(data) }),

  // Reconciliation
  getReconciliation: (page = 1, pageSize = 20, filters?: { org_id?: number; model?: string; status?: string; usage_source?: string }) => {
    let url = `/reconciliation?page=${page}&page_size=${pageSize}`;
    if (filters?.org_id) url += `&org_id=${filters.org_id}`;
    if (filters?.model) url += `&model=${encodeURIComponent(filters.model)}`;
    if (filters?.status) url += `&status=${encodeURIComponent(filters.status)}`;
    if (filters?.usage_source) url += `&usage_source=${encodeURIComponent(filters.usage_source)}`;
    return request(url);
  },

  // Notifications
  getNotificationEvents: (page = 1, pageSize = 20, filters?: {
    event_type?: string; severity?: string; status?: string; delivery_state?: string; project_id?: number;
  }) => request(appendParams('/notification-events', {
    page,
    page_size: pageSize,
    event_type: filters?.event_type,
    severity: filters?.severity,
    status: filters?.status,
    delivery_state: filters?.delivery_state,
    project_id: filters?.project_id,
  })),
  getNotificationDeliveries: (page = 1, pageSize = 20, filters?: { status?: string; event_id?: number; project_id?: number }) =>
    request(appendParams('/notification-deliveries', {
      page,
      page_size: pageSize,
      status: filters?.status,
      event_id: filters?.event_id,
      project_id: filters?.project_id,
    })),
  getNotificationWebhooks: (page = 1, pageSize = 20, filters?: { enabled?: boolean; project_id?: number }) =>
    request(appendParams('/notification-webhooks', {
      page,
      page_size: pageSize,
      enabled: filters?.enabled === undefined ? undefined : String(filters.enabled),
      project_id: filters?.project_id,
    })),
  createNotificationWebhook: (data: any) =>
    request('/notification-webhooks', { method: 'POST', body: JSON.stringify(data) }),
  updateNotificationWebhook: (id: number, data: any) =>
    request(`/notification-webhooks/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  verifyNotificationWebhookScope: (id: number, expectedUpdatedAt: string) =>
    request(`/notification-webhooks/${id}/verify-scope`, {
      method: 'POST',
      body: JSON.stringify({ expected_updated_at: expectedUpdatedAt }),
    }),

  // Generic request
  request,
};
