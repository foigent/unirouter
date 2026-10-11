import { describe, expect, it } from 'vitest';
import { modelBreakdown } from './modelBreakdown';
import type { DashboardModelHealth } from '../../api/dashboard';

const row = (model: string, requests: number, cost_micros?: string): DashboardModelHealth => ({ model, requests, cost_micros, success_count: requests, fail_count: 0, success_rate: 1, avg_latency_ms: 2, p95_latency_ms: 3 });

describe('model composition', () => {
  it('keeps costs above Number.MAX_SAFE_INTEGER exact and includes every model beyond fifty', () => {
    const rows = Array.from({ length: 61 }, (_, i) => row(`model-${i}`, 1, String(i + 1)));
    rows[0].cost_micros = '9007199254740993';
    const result = modelBreakdown(rows, 'cost');
    expect(result.state).toBe('ready');
    expect(result.total).toBe(9007199254742883n);
    expect(result.items).toHaveLength(6);
    expect(result.items[0].value).toBe(9007199254740993n);
    expect(result.items[5].model).toBeNull();
    expect(result.items.reduce((sum, item) => sum + item.value, 0n)).toBe(result.total);
  });

  it('does not invent zero costs when an older backend omits some or all amounts', () => {
    expect(modelBreakdown(undefined, 'requests').state).toBe('unavailable');
    expect(modelBreakdown(undefined, 'cost').state).toBe('unavailable');
    expect(modelBreakdown([row('first', 3, '200'), row('second', 2)], 'cost').state).toBe('unavailable');
    expect(modelBreakdown([row('old', 1)], 'cost').state).toBe('unavailable');
    expect(modelBreakdown([row('bad', 1, 'NaN')], 'cost').state).toBe('unavailable');
    expect(modelBreakdown([row('free', 2, '0')], 'cost').state).toBe('empty');
  });

  it('uses real cost weights, preserves identifiers and separates reserved Other from a model name', () => {
    const rows = [row('模型', 100, '1'), row('其他模型', 1, '100')];
    expect(modelBreakdown(rows, 'requests').items[0].model).toBe('模型');
    expect(modelBreakdown(rows, 'cost').items[0].model).toBe('其他模型');
    expect(modelBreakdown(rows, 'cost').items[0].key).toBe('model:其他模型');
  });

  it('does not draw invalid negative, missing, zero or unsafe request totals as a pie', () => {
    expect(modelBreakdown([], 'requests').state).toBe('empty');
    expect(modelBreakdown([row('x', 0, '0')], 'requests').state).toBe('empty');
    expect(modelBreakdown([row('x', 1, '-1')], 'cost').state).toBe('signed');
    expect(modelBreakdown([row('x', Number.MAX_SAFE_INTEGER + 1)], 'requests').state).toBe('unavailable');
  });
});
