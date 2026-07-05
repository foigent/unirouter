import { describe, expect, it } from 'vitest';
import {
  formatCompactNumber,
  formatInteger,
  formatLatencyMs,
  formatPercent,
  formatSignedInteger,
  safeNumber,
} from '../formatNumber';

describe('formatNumber helpers', () => {
  it('normalizes unsafe values', () => {
    expect(safeNumber(undefined)).toBe(0);
    expect(safeNumber(null)).toBe(0);
    expect(safeNumber(Number.NaN)).toBe(0);
  });

  it('formats compact numbers', () => {
    expect(formatCompactNumber(999)).toBe('999');
    expect(formatCompactNumber(12345)).toBe('12.3K');
    expect(formatCompactNumber(1234567)).toBe('1.2M');
  });

  it('formats integer and signed integer values', () => {
    expect(formatInteger(12345)).toBe('12,345');
    expect(formatSignedInteger(10)).toBe('+10');
    expect(formatSignedInteger(-10)).toBe('-10');
    expect(formatSignedInteger(0)).toBe('0');
  });

  it('formats ratio and latency values', () => {
    expect(formatPercent(0.1234)).toBe('12.3%');
    expect(formatPercent(0.1234, 2)).toBe('12.34%');
    expect(formatLatencyMs(123.6)).toBe('124ms');
  });
});
