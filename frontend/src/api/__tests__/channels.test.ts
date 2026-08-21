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

  it('keeps the retired credential surface read-only', async () => {
    response({ data: [], total: 0 });

    await providerCredentialApi.list(1, 20, 7);

    expect(mockFetch).toHaveBeenCalledWith(
      '/api/v1/channel-keys?page=1&page_size=20&channel_id=7',
      expect.objectContaining({ headers: expect.any(Object) }),
    );
    expect(Object.keys(providerCredentialApi)).toEqual(['list']);
  });
});
