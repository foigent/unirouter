import { describe, expect, it } from 'vitest';
import { formatMoney, microsRatio, microsToChartYuan, parseMicros } from '../MoneyText';

describe('micro-CNY display helpers', () => {
  it('preserves the smallest canonical ledger unit', () => {
    expect(formatMoney('1', 6)).toBe('¥0.000001');
    expect(formatMoney('-2', 6)).toBe('¥-0.000002');
    expect(microsToChartYuan('999')).toBeCloseTo(0.000999, 9);
  });

  it('rounds display precision half-up without changing the ledger value', () => {
    expect(formatMoney('1234567', 2)).toBe('¥1.23');
    expect(formatMoney('1235000', 2)).toBe('¥1.24');
    expect(parseMicros('1235000')).toBe(1_235_000n);
  });

  it('keeps cent-aligned amounts compact and preserves sub-cent evidence by default', () => {
    expect(formatMoney('1230000')).toBe('¥1.23');
    expect(formatMoney('1230002')).toBe('¥1.230002');
  });

  it('computes ratios with BigInt and rejects noncanonical wire amounts', () => {
    expect(microsRatio('1', '3')).toBeCloseTo(0.333333, 6);
    expect(() => parseMicros('1.5')).toThrow(/Invalid micro-CNY amount/);
    expect(() => parseMicros('1e6')).toThrow(/Invalid micro-CNY amount/);
  });
});
