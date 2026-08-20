import { beforeEach, describe, expect, it, vi } from 'vitest';
import { modelDeploymentReleaseApi } from '../modelDeploymentReleases';

const mockFetch = vi.fn();
vi.stubGlobal('fetch', mockFetch);

function response(payload: unknown, status = 200) {
  mockFetch.mockResolvedValueOnce({
    ok: status >= 200 && status < 300,
    status,
    text: () => Promise.resolve(JSON.stringify(payload)),
  });
}

describe('modelDeploymentReleaseApi', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    localStorage.clear();
  });

  it('reads tenant-scoped server-owned authoring options without a mutation identity', async () => {
    response({ contract_version: 'unirouter.model-deployment-release-authoring-options.v1', models: [], releases: [] });

    await modelDeploymentReleaseApi.list();

    expect(mockFetch).toHaveBeenCalledWith(
      '/api/v1/model-deployment-releases',
      expect.objectContaining({ headers: expect.not.objectContaining({ 'Idempotency-Key': expect.anything() }) }),
    );
  });

  it('authors only the echoed baseline, exact selections and reason with Idempotency-Key', async () => {
    const input = {
      public_model_id: 7,
      baseline: {
        kind: 'legacy_cutover' as const,
        expected_slot_version: 8,
        stable_release_ref: 'rcr_legacy-parent',
        expected_stable_release_version: 4,
      },
      selections: [{ deployment_id: 41, operation_id: 'openai.responses.v1' }],
      reason: 'atomic cutover candidate',
    };
    response({ original_http_status: 201, idempotency_replay: false, release: {} }, 201);

    await modelDeploymentReleaseApi.author(input, 'ui-model-release-author-test');

    expect(mockFetch).toHaveBeenCalledWith(
      '/api/v1/model-deployment-releases',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify(input),
        headers: expect.objectContaining({ 'Idempotency-Key': 'ui-model-release-author-test' }),
      }),
    );
  });
});
