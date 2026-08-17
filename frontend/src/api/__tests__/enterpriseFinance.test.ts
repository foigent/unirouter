import { beforeEach, describe, expect, it, vi } from 'vitest';
import { enterpriseFinanceApi } from '../enterpriseFinance';

const mockFetch = vi.fn();
vi.stubGlobal('fetch', mockFetch);

function response(payload: unknown) {
  mockFetch.mockResolvedValueOnce({ ok: true, status: 200, text: () => Promise.resolve(JSON.stringify(payload)) });
}

describe('enterprise finance API contracts', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    localStorage.clear();
  });

  it('binds Rate Card authority reads to the selected customer organization', async () => {
    response({ data: [], total: 0, request_pricing_authority: { current_mode: 'legacy', strict_active: false } });

    const result = await enterpriseFinanceApi.getRateCards(9);

    expect(result.request_pricing_authority.current_mode).toBe('legacy');
    expect(mockFetch).toHaveBeenCalledWith(
      '/api/v1/enterprise-finance/rate-cards?org_id=9',
      expect.objectContaining({ headers: expect.any(Object) }),
    );
  });

  it('preserves all four exact-price components in a draft mutation', async () => {
    response({ id: 1, card: { rate_card_ref: 'rate_1' } });
    const components = ['input_tokens', 'output_tokens', 'cache_read_tokens', 'cache_creation_tokens'].map(meter => ({
      meter,
      unit_quantity: '1000000',
      amount: { value_minor: '1000000', currency: 'CNY', scale: 1_000_000 },
    }));

    await enterpriseFinanceApi.createRateCardDraft({
      org_id: 9,
      provider_ref: 'deepseek',
      model_ref: 'deepseek-v4-pro',
      layer: 'provider_list',
      effective_from: '2026-08-17T00:00:00.000000Z',
      effective_to: null,
      components,
      daily_price_bands: [],
    });

    const [, init] = mockFetch.mock.calls[0];
    expect(JSON.parse(init.body).components).toEqual(components);
    expect(mockFetch).toHaveBeenCalledWith(
      '/api/v1/enterprise-finance/rate-cards',
      expect.objectContaining({ method: 'POST' }),
    );
  });
});
