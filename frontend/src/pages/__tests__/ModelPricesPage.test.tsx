import { describe, expect, it } from 'vitest';
import { yuanPerKToCents } from '../ModelPricesPage';

describe('ModelPricesPage price conversion', () => {
  it('preserves sub-cent prices per 1K tokens', () => {
    expect(yuanPerKToCents('0.002')).toBe(0.2);
    expect(yuanPerKToCents('0.001')).toBe(0.1);
  });
});
