import { beforeEach, describe, expect, it, vi } from 'vitest';
import { modelDeploymentLifecycleApi } from '../modelDeploymentLifecycle';

const mockFetch = vi.fn();
vi.stubGlobal('fetch', mockFetch);

function response(payload: unknown, status = 200) {
  mockFetch.mockResolvedValueOnce({
    ok: status >= 200 && status < 300,
    status,
    text: () => Promise.resolve(JSON.stringify(payload)),
  });
}

const activateInitialInput = { expected_release_version: 2 };
const startCanaryInput = {
  expected_release_version: 2,
  expected_slot_version: 1,
  canary_basis_points: 500,
};
const promoteInput = { expected_release_version: 3, expected_slot_version: 2 };
const cancelCanaryInput = {
  expected_release_version: 3,
  expected_slot_version: 2,
  reason: 'safety',
};
const rollbackStableInput = { expected_release_version: 4, expected_slot_version: 3 };
const cutoverLegacyInput = { expected_release_version: 2, expected_slot_version: 8, reason: 'parity verified' };
const rollbackLegacyInput = { expected_release_version: 3, expected_slot_version: 9, reason: 'emergency' };

describe('modelDeploymentLifecycleApi', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    localStorage.clear();
  });

  it('reads server-owned current state and action descriptors without a mutation header', async () => {
    response({ release: {}, slot: {}, actions: [] });
    await modelDeploymentLifecycleApi.current('rcr_release/encoded');
    expect(mockFetch).toHaveBeenCalledWith(
      '/api/v1/model-deployment-releases/rcr_release%2Fencoded',
      expect.objectContaining({ headers: expect.not.objectContaining({ 'Idempotency-Key': expect.anything() }) }),
    );
  });

  it.each([
    ['activateInitial', 'activate-initial', activateInitialInput,
      (key: string) => modelDeploymentLifecycleApi.activateInitial('rcr_http-one', activateInitialInput, key)],
    ['startCanary', 'start-canary', startCanaryInput,
      (key: string) => modelDeploymentLifecycleApi.startCanary('rcr_http-one', startCanaryInput, key)],
    ['promote', 'promote', promoteInput,
      (key: string) => modelDeploymentLifecycleApi.promote('rcr_http-one', promoteInput, key)],
    ['cancelCanary', 'cancel-canary', cancelCanaryInput,
      (key: string) => modelDeploymentLifecycleApi.cancelCanary('rcr_http-one', cancelCanaryInput, key)],
    ['rollbackStable', 'rollback-stable', rollbackStableInput,
      (key: string) => modelDeploymentLifecycleApi.rollbackStable('rcr_http-one', rollbackStableInput, key)],
    ['cutoverLegacy', 'cutover-legacy', cutoverLegacyInput,
      (key: string) => modelDeploymentLifecycleApi.cutoverLegacy('rcr_http-one', cutoverLegacyInput, key)],
    ['rollbackLegacy', 'rollback-legacy', rollbackLegacyInput,
      (key: string) => modelDeploymentLifecycleApi.rollbackLegacy('rcr_http-one', rollbackLegacyInput, key)],
  ] as const)('uses the stage-specific %s path and mandatory Idempotency-Key', async (
    _method,
    actionPath,
    input,
    invoke,
  ) => {
    response({ receipt_id: 1, idempotency_replay: false });
    await invoke(`key-${actionPath}`);
    expect(mockFetch).toHaveBeenCalledWith(
      `/api/v1/model-deployment-releases/rcr_http-one/${actionPath}`,
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify(input),
        headers: expect.objectContaining({ 'Idempotency-Key': `key-${actionPath}` }),
      }),
    );
  });
});
