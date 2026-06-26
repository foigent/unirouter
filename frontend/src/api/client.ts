const BASE_URL = '/api/v1';

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
  if (res.status === 401) {
    localStorage.removeItem('token');
    window.location.href = '/login';
    throw new Error('Unauthorized');
  }
  return res.json();
}

export const api = {
  // Auth
  login: (username: string, password: string) =>
    request('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),

  // Dashboard
  getDashboardStats: () => request('/dashboard/stats'),

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
  getLogs: (page = 1, pageSize = 20, filters?: { model?: string; provider?: string; status_code?: string; start_time?: string; end_time?: string }) => {
    let url = `/logs?page=${page}&page_size=${pageSize}`;
    if (filters?.model) url += `&model=${encodeURIComponent(filters.model)}`;
    if (filters?.provider) url += `&provider=${encodeURIComponent(filters.provider)}`;
    if (filters?.status_code) url += `&status_code=${encodeURIComponent(filters.status_code)}`;
    if (filters?.start_time) url += `&start_time=${encodeURIComponent(filters.start_time)}`;
    if (filters?.end_time) url += `&end_time=${encodeURIComponent(filters.end_time)}`;
    return request(url);
  },

  // Model prices
  getModelPrices: () => request('/model-prices'),

  // Settings
  getSettings: () => request('/settings'),

  updateSetting: (key: string, value: any) =>
    request(`/settings/${key}`, {
      method: 'PUT',
      body: JSON.stringify({ value }),
    }),

  // Channel Keys
  getChannelKeys: () => request('/channel-keys'),
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

  // Model Prices
  getModelPriceList: (page = 1, pageSize = 20) =>
    request(`/model-prices?page=${page}&page_size=${pageSize}`),
  createModelPrice: (data: any) =>
    request('/model-prices', { method: 'POST', body: JSON.stringify(data) }),
  updateModelPrice: (id: number, data: any) =>
    request(`/model-prices/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteModelPrice: (id: number) =>
    request(`/model-prices/${id}`, { method: 'DELETE' }),

  // Grants
  getGrants: (page = 1, pageSize = 20) =>
    request(`/grants?page=${page}&page_size=${pageSize}`),
  createGrant: (data: any) =>
    request('/grants', { method: 'POST', body: JSON.stringify(data) }),

  // Audit Logs
  getAuditLogs: (page = 1, pageSize = 20) =>
    request(`/audit-logs?page=${page}&page_size=${pageSize}`),

  // Org Model Prices
  getOrgModelPrices: (orgId = 1) =>
    request(`/org-model-prices?org_id=${orgId}`),
  createOrgModelPrice: (data: any) =>
    request("/org-model-prices", { method: "POST", body: JSON.stringify(data) }),
  updateOrgModelPrice: (id: number, data: any) =>
    request(`/org-model-prices/${id}`, { method: "PUT", body: JSON.stringify(data) }),
  deleteOrgModelPrice: (id: number) =>
    request(`/org-model-prices/${id}`, { method: "DELETE" }),

  // Generic request
  request,
};
