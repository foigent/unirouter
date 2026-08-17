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

  it('binds every release transition to the server revision', async () => {
    response({ release_ref: 'rcr_company_fast', version: 8, stage: 'canary' });

    await routingApi.startReleaseCanary('rcr_company_fast', 7, 500);

    expect(mockFetch).toHaveBeenCalledWith(
      '/api/v1/runtime-changes/rcr_company_fast/canary',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ expected_version: 7, canary_basis_points: 500 }),
      }),
    );
  });
});
