import { beforeEach, describe, expect, it, vi } from 'vitest';
import { routingApi } from '../routing';

const mockFetch = vi.fn();
vi.stubGlobal('fetch', mockFetch);

function response(payload: unknown, status = 200) {
  mockFetch.mockResolvedValueOnce({
    ok: status >= 200 && status < 300,
    status,
    text: () => Promise.resolve(JSON.stringify(payload)),
  });
}

describe('routingApi', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    localStorage.clear();
  });

  it('encodes the exact public model in the gateway-owned preview request', async () => {
    response({ policy_version: 'routing-policy.v1', candidates: [], exclusions: [] });

    await routingApi.preview('company/chat v2');

    expect(mockFetch).toHaveBeenCalledWith(
      '/api/v1/routing/preview?model=company%2Fchat%20v2',
      expect.objectContaining({ headers: expect.any(Object) }),
    );
  });
  it('does not expose the retired runtime-change writer surface', () => {
    expect(Object.keys(routingApi)).toEqual(['preview']);
  });
});
