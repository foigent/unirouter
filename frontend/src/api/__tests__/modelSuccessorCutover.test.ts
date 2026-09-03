import { beforeEach, describe, expect, it, vi } from 'vitest';
import { modelSuccessorCutoverApi } from '../modelSuccessorCutover';

const mockFetch = vi.fn();
vi.stubGlobal('fetch', mockFetch);

function response(payload: unknown, status = 200) {
  mockFetch.mockResolvedValueOnce({
    ok: status >= 200 && status < 300,
    status,
    text: () => Promise.resolve(JSON.stringify(payload)),
  });
}

describe('modelSuccessorCutoverApi', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    localStorage.clear();
  });

  it('freezes every direct predecessor model-set reference and its same-name alias state', async () => {
    response({ data: [{ id: 7 }], total: 101, page: 1, page_size: 100 });
    response({ data: [{ id: 3 }], total: 101, page: 2, page_size: 100 });
    response({ data: [
      { item_type: 'model', item_name: 'minimax-m3' },
      { item_type: 'alias', item_name: 'minimax-m3' },
    ], total: 2 });
    response({ data: [
      { item_type: 'model', item_name: 'minimax-m3' },
      { item_type: 'alias', item_name: 'other-model' },
    ], total: 2 });

    await expect(modelSuccessorCutoverApi.snapshotModelSets('minimax-m3')).resolves.toEqual([
      { model_set_id: 3, before_alias_exists: false },
      { model_set_id: 7, before_alias_exists: true },
    ]);
    expect(mockFetch.mock.calls.map(call => call[0])).toEqual([
      '/api/v1/model-sets?page=1&page_size=100',
      '/api/v1/model-sets?page=2&page_size=100',
      '/api/v1/model-sets/7/items',
      '/api/v1/model-sets/3/items',
    ]);
  });

  it('posts the exact staging successor CAS with its independent idempotency identity', async () => {
    const input = {
      expected_release_version: 2,
      stable_request_model: 'minimax-m3',
      predecessor_public_model_id: 962,
      predecessor_model_name: 'minimax-m3',
      expected_predecessor_lifecycle: 'staging' as const,
      expected_predecessor_approved_catalog: false as const,
      successor_public_model_id: 988,
      successor_model_name: 'minimax-m3-20260903',
      expected_successor_lifecycle: 'staging' as const,
      expected_successor_approved_catalog: false as const,
      expected_model_sets: [],
      reason: 'exact successor cutover',
    };
    response({ contract_version: 'unirouter.model-successor-cutover-receipt.v2' });

    await modelSuccessorCutoverApi.activateStagingSuccessor(
      'rcr_minimax_m3_successor',
      input,
      'ui-model-release-lifecycle-test',
    );

    expect(mockFetch).toHaveBeenCalledWith(
      '/api/v1/model-deployment-releases/rcr_minimax_m3_successor/activate-staging-successor',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify(input),
        headers: expect.objectContaining({ 'Idempotency-Key': 'ui-model-release-lifecycle-test' }),
      }),
    );
  });
});
