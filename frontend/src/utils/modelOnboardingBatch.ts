import type { CanonicalModelApplicationCreateRequest, CanonicalModelApplicationCreateResult } from '../api/modelDeployments';
export const modelBatchRetryWindowMS = 60 * 60 * 1000;
export type ModelBatchItem = {
  model: string;
  payload: CanonicalModelApplicationCreateRequest;
  idempotencyKey: string;
  state: 'pending' | 'sending' | 'succeeded' | 'unconfirmed';
  result?: CanonicalModelApplicationCreateResult;
  error?: string;
};
export type ModelBatch = {
  actor: string;
  startedAt: number;
  lastCheckedAt: number;
  items: ModelBatchItem[];
};
// In-memory only. No API Key is accepted or retained by the batch flow.
export function prepareModelBatch(actor: string, inputs: {
  model: string;
  payload: CanonicalModelApplicationCreateRequest;
}[], now: number): ModelBatch {
  if (!actor || inputs.length < 1 || inputs.length > 50)
    throw new Error('请选择 1–50 个模型');
  const identities = new Set<string>();
  const items = inputs.map(({ model, payload }) => {
    if (!payload.connection.id || !payload.credential.binding_id || payload.credential.api_key || identities.has(model)) {
      throw new Error('批量添加必须使用明确的现有连接和凭证，且模型不能重复');
    }
    identities.add(model);
    return { model, payload: structuredClone(payload), idempotencyKey: `model-batch-${crypto.randomUUID()}`, state: 'pending' as const };
  });
  return { actor, startedAt: now, lastCheckedAt: now, items };
}
export async function runModelBatch(batch: ModelBatch, d: {
  actor: () => string;
  now: () => number;
  create: (input: CanonicalModelApplicationCreateRequest, key: string) => Promise<CanonicalModelApplicationCreateResult>;
  changed: () => void;
}) {
  for (const item of batch.items) {
    if (item.state === 'succeeded')
      continue;
    const now = d.now();
    if (d.actor() !== batch.actor || now < batch.lastCheckedAt || now - batch.startedAt >= modelBatchRetryWindowMS) {
      throw new Error('登录身份或重试时间窗口已变化。请刷新现有配置，核对未确认项后重新选择。');
    }
    batch.lastCheckedAt = now;
    item.state = 'sending';
    item.error = undefined;
    d.changed();
    try {
      item.result = await d.create(item.payload, item.idempotencyKey);
      item.state = 'succeeded';
      d.changed();
    }
    catch {
      // A transport error does not determine whether the transaction committed.
      // Explicit retries retain this payload and key, never a new credential.
      item.state = 'unconfirmed';
      item.error = '未确认创建结果；可在本页重试原请求，或刷新后核对现有配置。';
      d.changed();
      return;
    }
  }
}
