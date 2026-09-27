import { beforeEach, describe, expect, it, vi } from 'vitest';
import { contentManagementApi, type ContentControlRequest, type PayloadConfig } from '../contentManagement';

const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);

function response(body: unknown) {
  return { ok: true, status: 200, text: async () => JSON.stringify(body) };
}

describe('content management API contract', () => {
  beforeEach(() => { fetchMock.mockReset(); localStorage.clear(); });

  it('reads policy and C connection with GET and no request body', async () => {
    fetchMock.mockResolvedValue(response({ state_version: '0' }));
    await contentManagementApi.getPolicy(2);
    await contentManagementApi.getConnection(2);
    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/projects/2/payload-policy', expect.not.objectContaining({ method: 'PUT', body: expect.anything() }));
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/data-exchange/xinsight/source-connections/2/content', expect.not.objectContaining({ method: 'POST', body: expect.anything() }));
  });

  it('sends the exact policy CAS and C control request to their existing routes', async () => {
    fetchMock.mockResolvedValue(response({ state_version: '1' }));
    const desired: PayloadConfig = { mode: 'disabled', supported_protocols: ['chat'], observation_points: ['client_request'], allowed_purposes: [] };
    const body: ContentControlRequest = { operation_id: 'a'.repeat(32), expected_state_version: '0', credential_id: '9007199254740993', registration_hash: `sha256:${'b'.repeat(64)}` };
    await contentManagementApi.putPolicy(2, '1', desired);
    await contentManagementApi.control(2, 'pause', body);
    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/projects/2/payload-policy', expect.objectContaining({
      method: 'PUT', body: JSON.stringify({ schema_version: 'unirouter.payload-policy.update.v1', expected_policy_version: '1', desired }),
    }));
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/data-exchange/xinsight/source-connections/2/content/pause', expect.objectContaining({ method: 'POST', body: JSON.stringify(body) }));
  });
});
