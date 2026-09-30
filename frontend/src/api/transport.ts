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

async function parseResponse(res: Response): Promise<unknown> {
  const text = await res.text();
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

// Every domain client must name its response contract. The legacy aggregate
// client keeps its compatibility boundary locally instead of weakening the
// transport used by new or migrated domains.
export async function request<T>(path: string, options: RequestInit = {}, version: 'v1' | 'v2' = 'v1'): Promise<T> {
  const token = localStorage.getItem('token');
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...((options.headers as Record<string, string>) || {}),
  };
  if (token) headers.Authorization = `Bearer ${token}`;

  const res = await fetch(`${version === 'v2' ? '/api/v2' : BASE_URL}${path}`, { ...options, headers });
  const payload = await parseResponse(res);

  if (res.status === 401) {
    localStorage.removeItem('token');
    window.location.href = '/login';
    throw new ApiError(getErrorMessage(payload, 'Unauthorized'), res.status, payload);
  }
  if (!res.ok) {
    throw new ApiError(getErrorMessage(payload, `Request failed with status ${res.status}`), res.status, payload);
  }
  return payload as T;
}

export async function requestBlob(path: string) {
  const token = localStorage.getItem('token');
  const headers: Record<string, string> = {};
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch(`${BASE_URL}${path}`, { headers });
  if (res.status === 401) {
    localStorage.removeItem('token');
    window.location.href = '/login';
    throw new ApiError('Unauthorized', res.status, null);
  }
  if (!res.ok) {
    const payload = await parseResponse(res);
    throw new ApiError(
      getErrorMessage(payload, `Request failed with status ${res.status}`),
      res.status,
      payload,
    );
  }
  return res.blob();
}

export function appendParams(path: string, params: Record<string, string | number | boolean | undefined | null>) {
  const search = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') search.set(key, String(value));
  });
  const query = search.toString();
  return query ? `${path}?${query}` : path;
}

export function apiPath(path: string) {
  return `${BASE_URL}${path}`;
}
