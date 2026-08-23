import type { DailyPriceBand, RateComponent } from '../api/enterpriseFinance';

export type { DailyPriceBand, RateComponent } from '../api/enterpriseFinance';

export type PriceBandOverrideDraft = {
  start_utc: string;
  end_utc: string;
  input_price_yuan: string;
  output_price_yuan: string;
  cache_read_price_yuan: string;
  cache_creation_price_yuan: string;
  builtin_web_search_price_yuan?: string;
};

export const requestPriceMeters = [
  { key: 'input_price_yuan', meter: 'input_tokens', label: '输入 Token' },
  { key: 'output_price_yuan', meter: 'output_tokens', label: '输出 Token' },
  { key: 'cache_read_price_yuan', meter: 'cache_read_tokens', label: '缓存读取 Token' },
  { key: 'cache_creation_price_yuan', meter: 'cache_creation_tokens', label: '缓存创建 / 写入 Token' },
] as const;

export const requestUnitPriceMeters = [
  {
    key: 'builtin_web_search_price_yuan', meter: 'builtin_web_search_calls',
    label: 'Kimi 内置联网搜索', unitQuantity: '1', unitLabel: '次调用',
  },
] as const;

export function yuanToMicrosString(value: string): string | null {
  const normalized = value.trim();
  if (!/^(?:0|[1-9][0-9]*)(?:\.[0-9]{1,6})?$/.test(normalized)) return null;
  const [whole, fraction = ''] = normalized.split('.');
  const micros = `${whole}${fraction.padEnd(6, '0')}`.replace(/^0+(?=\d)/, '');
  return micros || '0';
}

export function buildRequestRateComponents(
  form: Record<string, string>,
  keyPrefix = '',
  requiredUnitMeters: string[] = [],
): RateComponent[] | null {
  const components = requestPriceMeters.map(item => ({ item, valueMinor: yuanToMicrosString(form[`${keyPrefix}${item.key}`] || '') }));
  if (components.some(item => item.valueMinor === null)) return null;
  const result: RateComponent[] = components.map(({ item, valueMinor }) => ({
    meter: item.meter,
    unit_quantity: '1000000',
    amount: { value_minor: valueMinor as string, currency: 'CNY', scale: 1_000_000 },
  }));
  for (const item of requestUnitPriceMeters) {
    const raw = form[`${keyPrefix}${item.key}`]?.trim() || '';
    if (!raw) {
      if (requiredUnitMeters.includes(item.meter)) return null;
      continue;
    }
    const valueMinor = yuanToMicrosString(raw);
    if (valueMinor === null) return null;
    result.push({
      meter: item.meter,
      unit_quantity: item.unitQuantity,
      amount: { value_minor: valueMinor, currency: 'CNY', scale: 1_000_000 },
    });
  }
  return result;
}

function parseUTCMinute(value: string) {
  const match = /^(?:[01][0-9]|2[0-3]):[0-5][0-9]$/.exec(value.trim());
  if (!match) return null;
  const [hour, minute] = value.split(':').map(Number);
  return hour * 60 + minute;
}

function copyRateComponents(components: RateComponent[]): RateComponent[] {
  return components.map(component => ({ ...component, amount: { ...component.amount } }));
}

// Kept as a compatibility helper for existing migration fixtures. New UI
// input uses buildUTCOverridePriceBands so providers may define multiple
// disjoint peak or valley windows.
export function buildUTCValleyPriceBands(
  form: Record<string, string>,
  baseComponents: RateComponent[],
): DailyPriceBand[] | null {
  if (form.price_schedule !== 'utc_valley') return [];
  const start = parseUTCMinute(form.valley_start_utc || '');
  const end = parseUTCMinute(form.valley_end_utc || '');
  const requiredUnits = requestUnitPriceMeters
    .filter(item => baseComponents.some(component => component.meter === item.meter))
    .map(item => item.meter);
  const valleyComponents = buildRequestRateComponents(form, 'valley_', requiredUnits);
  if (start === null || end === null || start >= end || !valleyComponents) return null;

  const bands: DailyPriceBand[] = [];
  if (start > 0) bands.push({ band_ref: 'base-before', start_minute_utc: 0, end_minute_utc: start, components: copyRateComponents(baseComponents) });
  bands.push({ band_ref: 'valley', start_minute_utc: start, end_minute_utc: end, components: valleyComponents });
  if (end < 24 * 60) bands.push({ band_ref: 'base-after', start_minute_utc: end, end_minute_utc: 24 * 60, components: copyRateComponents(baseComponents) });
  return bands;
}

export function buildUTCOverridePriceBands(
  overrides: PriceBandOverrideDraft[],
  baseComponents: RateComponent[],
): DailyPriceBand[] | null {
  if (overrides.length === 0) return [];
  const requiredUnits = requestUnitPriceMeters
    .filter(item => baseComponents.some(component => component.meter === item.meter))
    .map(item => item.meter);
  const normalized = overrides.map((override, index) => ({
    index,
    start: parseUTCMinute(override.start_utc || ''),
    end: parseUTCMinute(override.end_utc || ''),
    components: buildRequestRateComponents(override as unknown as Record<string, string>, '', requiredUnits),
  }));
  if (normalized.some(item => item.start === null || item.end === null || item.start >= item.end || !item.components)) return null;
  normalized.sort((left, right) => (left.start as number) - (right.start as number));

  const bands: DailyPriceBand[] = [];
  let cursor = 0;
  for (const item of normalized) {
    const start = item.start as number;
    const end = item.end as number;
    if (start < cursor) return null;
    if (start > cursor) bands.push({ band_ref: `base-${cursor}-${start}`, start_minute_utc: cursor, end_minute_utc: start, components: copyRateComponents(baseComponents) });
    bands.push({ band_ref: `override-${item.index + 1}`, start_minute_utc: start, end_minute_utc: end, components: item.components as RateComponent[] });
    cursor = end;
  }
  if (cursor < 24 * 60) bands.push({ band_ref: `base-${cursor}-1440`, start_minute_utc: cursor, end_minute_utc: 24 * 60, components: copyRateComponents(baseComponents) });
  return bands;
}

export function emptyPriceOverride(start_utc = '', end_utc = ''): PriceBandOverrideDraft {
  return {
    start_utc, end_utc,
    input_price_yuan: '', output_price_yuan: '',
    cache_read_price_yuan: '', cache_creation_price_yuan: '',
    builtin_web_search_price_yuan: '',
  };
}
