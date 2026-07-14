import { describe, expect, it } from 'vitest';
import { parseCachePriceRatio, parseCNYPerMillionPrice } from '../ModelPricesPage';

describe('ModelPricesPage public price parsing', () => {
  it('accepts the canonical CNY-per-million unit', () => {
		expect(parseCNYPerMillionPrice('2')).toBe('2');
		expect(parseCNYPerMillionPrice('0002.50000')).toBe('2.5');
		expect(parseCNYPerMillionPrice('0.00001')).toBe('0.00001');
		expect(parseCNYPerMillionPrice('9223372036854.77580')).toBe('9223372036854.7758');
  });

	it.each(['2 CNY', '2.0tail', '1e3', '-1', '0.000001', '9223372036854.77581', ''])('rejects ambiguous price %s', (value) => {
    expect(() => parseCNYPerMillionPrice(value)).toThrow('invalid CNY-per-million price');
	});

	it('preserves exact cache-ratio text without binary floating point', () => {
		expect(parseCachePriceRatio('0.100000')).toBe('0.1');
		expect(parseCachePriceRatio('100')).toBe('100');
		expect(() => parseCachePriceRatio('0.0000001')).toThrow('invalid cache price ratio');
	});
});
