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

export const api = {
  // Auth
  login: (username: string, password: string) =>
    request('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),

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
  getKeys: (page = 1, pageSize = 20) =>
    request(`/keys?page=${page}&page_size=${pageSize}`),
  createKey: (data: any) =>
    request('/keys', { method: 'POST', body: JSON.stringify(data) }),
  updateKey: (id: number, data: any) =>
    request(`/keys/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  disableKey: (id: number) =>
    request(`/keys/${id}/disable`, { method: 'POST' }),

  // Wallet
  getWallet: () => request('/wallet'),
  getWalletTransactions: (page = 1, pageSize = 20) =>
    request(`/wallet/transactions?page=${page}&page_size=${pageSize}`),
  topUp: (data: any) =>
    request('/wallet/topup', { method: 'POST', body: JSON.stringify(data) }),

  // Logs (with optional filters)
  getLogs: (page = 1, pageSize = 20, filters?: {
    model?: string; provider?: string; status_code?: string;
    start_time?: string; end_time?: string; request_id?: string;
    latency_min?: string; latency_max?: string; search?: string;
  }) => {
    let url = `/logs?page=${page}&page_size=${pageSize}`;
    if (filters?.model) url += `&model=${encodeURIComponent(filters.model)}`;
    if (filters?.provider) url += `&provider=${encodeURIComponent(filters.provider)}`;
    if (filters?.status_code) url += `&status_code=${encodeURIComponent(filters.status_code)}`;
    if (filters?.start_time) url += `&start_time=${encodeURIComponent(filters.start_time)}`;
    if (filters?.end_time) url += `&end_time=${encodeURIComponent(filters.end_time)}`;
    if (filters?.request_id) url += `&request_id=${encodeURIComponent(filters.request_id)}`;
    if (filters?.latency_min) url += `&latency_min=${encodeURIComponent(filters.latency_min)}`;
    if (filters?.latency_max) url += `&latency_max=${encodeURIComponent(filters.latency_max)}`;
    if (filters?.search) url += `&search=${encodeURIComponent(filters.search)}`;
    return request(url);
  },
  getLogDetail: (id: number) => request(`/logs/${id}`),

  // Model prices
  getModelPrices: () => request('/model-prices'),
  getModelPriceList: (page = 1, pageSize = 20) =>
    request(`/model-prices?page=${page}&page_size=${pageSize}`),
  getModelNames: () => request('/model-names'),
  createModelPrice: (data: any) =>
    request('/model-prices', { method: 'POST', body: JSON.stringify(data) }),
  updateModelPrice: (id: number, data: any) =>
    request(`/model-prices/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteModelPrice: (id: number) =>
    request(`/model-prices/${id}`, { method: 'DELETE' }),

  // Settings
  getSettings: () => request('/settings'),
  updateSetting: (key: string, value: any) =>
    request(`/settings/${key}`, { method: 'PUT', body: JSON.stringify({ value }) }),

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
  getGrants: (page = 1, pageSize = 20) =>
    request(`/grants?page=${page}&page_size=${pageSize}`),
  createGrant: (data: any) =>
    request('/grants', { method: 'POST', body: JSON.stringify(data) }),

  // Audit Logs
  getAuditLogs: (page = 1, pageSize = 20) =>
    request(`/audit-logs?page=${page}&page_size=${pageSize}`),

  // Org Model Prices
  getOrgModelPrices: (orgId = 1, page = 1, pageSize = 20) =>
    request(`/org-model-prices?org_id=${orgId}&page=${page}&page_size=${pageSize}`),
  createOrgModelPrice: (data: any) =>
    request('/org-model-prices', { method: 'POST', body: JSON.stringify(data) }),
  updateOrgModelPrice: (id: number, data: any) =>
    request(`/org-model-prices/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteOrgModelPrice: (id: number) =>
    request(`/org-model-prices/${id}`, { method: 'DELETE' }),

  // User profile
  getMe: () => request('/me'),
  changePassword: (data: { old_password: string; new_password: string }) =>
    request('/me/password', { method: 'PUT', body: JSON.stringify(data) }),

  // Reconciliation
  getReconciliation: (page = 1, pageSize = 20, filters?: { model?: string; status?: string }) => {
    let url = `/reconciliation?page=${page}&page_size=${pageSize}`;
    if (filters?.model) url += `&model=${encodeURIComponent(filters.model)}`;
    if (filters?.status) url += `&status=${encodeURIComponent(filters.status)}`;
    return request(url);
  },

  // Generic request
  request,
};
