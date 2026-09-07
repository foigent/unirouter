import { describe, expect, it, vi } from 'vitest';
import { modelBatchRetryWindowMS, prepareModelBatch, runModelBatch } from '../modelOnboardingBatch';
import type { CanonicalModelApplicationCreateRequest, CanonicalModelApplicationCreateResult } from '../../api/modelDeployments';
const input = (model: string): CanonicalModelApplicationCreateRequest => ({ connection: { id: 14 }, credential: { binding_id: 14 }, public_model: { model_name: model }, deployment: { upstream_model: model }, operation: { operation_id: 'openai.chat_completions.v1' } });
const result = { application: { deployment: { id: 40 } }, idempotency_replay: false } as CanonicalModelApplicationCreateResult;
describe('model onboarding batch', () => {
  it('preserves each exact request and key across an ambiguous partial batch', async () => {
    const payload = input('a'), batch = prepareModelBatch('actor', [{ model: 'a', payload }, { model: 'b', payload: input('b') }], 100);
    payload.connection.id = 999;
    const create = vi.fn().mockResolvedValueOnce(result).mockRejectedValueOnce(new Error('timeout'));
    const deps = { actor: () => 'actor', now: () => 200, create, changed: vi.fn() };
    await runModelBatch(batch, deps);
    expect(batch.items.map(i => i.state)).toEqual(['succeeded', 'unconfirmed']);
    expect(create.mock.calls[0][0].connection.id).toBe(14);
    const originalSecond = create.mock.calls[1];
    create.mockResolvedValueOnce({ ...result, idempotency_replay: true });
    await runModelBatch(batch, deps);
    expect(create).toHaveBeenCalledTimes(3);
    expect(create.mock.calls[2]).toEqual(originalSecond);
    expect(batch.items.every(i => i.state === 'succeeded')).toBe(true);
  });
  it('fences actor changes, expiry and clock regression before another write', async () => {
    for (const [actor, now] of [['other', 101], ['actor', 100 + modelBatchRetryWindowMS], ['actor', 99]] as const) {
      const batch = prepareModelBatch('actor', [{ model: 'a', payload: input('a') }], 100), create = vi.fn();
      await expect(runModelBatch(batch, { actor: () => actor, now: () => now, create, changed: vi.fn() })).rejects.toThrow('窗口');
      expect(create).not.toHaveBeenCalled();
    }
  });
  it('checks actor identity between individual models', async () => {
    let actor = 'one';
    const batch = prepareModelBatch(actor, ['a', 'b'].map(model => ({ model, payload: input(model) })), 100);
    const create = vi.fn(async () => { actor = 'two'; return result; });
    await expect(runModelBatch(batch, { actor: () => actor, now: () => 101, create, changed: vi.fn() })).rejects.toThrow();
    expect(create).toHaveBeenCalledTimes(1); expect(batch.items[1].state).toBe('pending');
  });
  it('rejects secrets and implicit connection creation', () => {
    for (const payload of [{ ...input('a'), credential: { api_key: 'secret' } }, { ...input('a'), connection: { provider: 'dashscope' } }]) {
      expect(() => prepareModelBatch('actor', [{ model: 'a', payload }], 100)).toThrow('现有连接');
    }
  });
});
