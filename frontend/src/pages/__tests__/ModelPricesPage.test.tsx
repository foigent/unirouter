import { describe, expect, it } from 'vitest';
import { parseCNYPerMillionPrice } from '../ModelPricesPage';

describe('ModelPricesPage public price parsing', () => {
  it('accepts the canonical CNY-per-million unit', () => {
    expect(parseCNYPerMillionPrice('2')).toBe(2);
    expect(parseCNYPerMillionPrice('0.00001')).toBe(0.00001);
  });

  it.each(['2 CNY', '2.0tail', '1e3', '-1', '0.000001', ''])('rejects ambiguous price %s', (value) => {
    expect(() => parseCNYPerMillionPrice(value)).toThrow('invalid CNY-per-million price');
  });
});
