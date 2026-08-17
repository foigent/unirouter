import { beforeEach, describe, expect, it, vi } from 'vitest';
import { channelApi, providerCredentialApi } from '../channels';

const mockFetch = vi.fn();
vi.stubGlobal('fetch', mockFetch);

function response(payload: unknown, status = 200) {
  mockFetch.mockResolvedValueOnce({
    ok: status >= 200 && status < 300,
    status,
    text: () => Promise.resolve(JSON.stringify(payload)),
  });
}

describe('channelApi', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    localStorage.clear();
  });

  it('loads the paged provider connection contract', async () => {
    response({ data: [], total: 0 });

    await channelApi.list(2, 10);

    expect(mockFetch).toHaveBeenCalledWith(
      '/api/v1/channels?page=2&page_size=10',
      expect.objectContaining({ headers: expect.any(Object) }),
    );
  });

  it('keeps credential rotation bound to one credential and one probe model', async () => {
    response({ id: 12, channel_id: 3, model: 'deepseek-v4-flash', probe: { success: true, status_code: 200, latency_ms: 9 } }, 201);

    await providerCredentialApi.rotate(7, {
      key: 'test-only-secret',
      model: 'deepseek-v4-flash',
      allowed_models: 'deepseek-v4-flash',
    });

    expect(mockFetch).toHaveBeenCalledWith(
      '/api/v1/channel-keys/7/rotate',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          key: 'test-only-secret',
          model: 'deepseek-v4-flash',
          allowed_models: 'deepseek-v4-flash',
        }),
      }),
    );
  });
});
