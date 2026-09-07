import { describe, expect, it, vi } from 'vitest';
import { loadOnboardingProfiles, type ModelExecutionProfile } from '../modelExecutionProfiles';

const profile = (n: number) => ({ id: `fixture.${n}`, version: '1.0.0', current: false,
  profile_definition_digest: n.toString(16).padStart(64, '0'), product_lineage_id: `fixture/${n}` }) as ModelExecutionProfile;
const page = (number: number, total: number, data: ModelExecutionProfile[]) => ({
  contract_version: 'unirouter.model-execution-profiles.v1' as const, page: number, page_size: 100, total, data,
});

describe('published onboarding catalogue', () => {
  it('reads every page with exact immutable identities and leaves the compiled source explicit', async () => {
    const data = Array.from({ length: 101 }, (_, n) => profile(n));
    const api = { list: vi.fn(), forStableModel: vi.fn(), published: vi.fn()
      .mockResolvedValueOnce(page(1, 101, data.slice(0, 100))).mockResolvedValueOnce(page(2, 101, data.slice(100))) };
    expect((await loadOnboardingProfiles('published', api)).data).toEqual(data);
    expect(api.published.mock.calls).toEqual([[1], [2]]);
    expect(api.list).not.toHaveBeenCalled();
    api.list.mockResolvedValue({ data: [] });
    await loadOnboardingProfiles('compiled', api);
    expect(api.list).toHaveBeenCalledOnce();
  });
  it.each(['changed total', 'duplicate', 'missing digest', 'short page', 'failed load'])('rejects %s without returning a partial or unverified catalogue', async kind => {
    const data = Array.from({ length: 100 }, (_, n) => profile(n));
    const api = { list: vi.fn(), forStableModel: vi.fn(), published: vi.fn().mockResolvedValueOnce(page(1, 101, data)) };
    if (kind === 'failed load') api.published.mockRejectedValueOnce(new Error('offline'));
    else api.published.mockResolvedValueOnce(page(2, kind === 'changed total' ? 102 : 101,
      kind === 'short page' ? [] : [kind === 'duplicate' ? data[0] : kind === 'missing digest' ? { ...profile(100), profile_definition_digest: undefined } : profile(100)]));
    await expect(loadOnboardingProfiles('published', api)).rejects.toThrow();
    expect(api.list).not.toHaveBeenCalled();
  });
});
