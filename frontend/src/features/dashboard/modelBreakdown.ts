import type { DashboardModelHealth } from '../../api/dashboard';

export type BreakdownItem = {
  key: string;
  model: string | null;
  value: bigint;
  share: number;
  color: string;
};
export type ModelBreakdown = {
  state: 'ready' | 'empty' | 'unavailable' | 'signed';
  total: bigint;
  items: BreakdownItem[];
};
const colors = ['#5576df', '#35a095', '#9470d9', '#d2a05b', '#7798b9', '#a16487'];

// Values remain integer facts. Floating-point conversion is confined to chart
// proportions; formatting and aggregation never round micro-CNY to cents.
export function modelBreakdown(rows: DashboardModelHealth[] | undefined, metric: 'requests' | 'cost'): ModelBreakdown {
  if (rows === undefined) return { state: 'unavailable', total: 0n, items: [] };
  const names = [...rows].map(row => row.model).sort();
  const values: { model: string; value: bigint }[] = [];
  for (const row of rows) {
    if (metric === 'cost' && (typeof row.cost_micros !== 'string' || !/^-?\d+$/.test(row.cost_micros))) {
      return { state: 'unavailable', total: 0n, items: [] };
    }
    if (metric === 'requests' && (!Number.isSafeInteger(row.requests) || row.requests < 0)) {
      return { state: 'unavailable', total: 0n, items: [] };
    }
    values.push({ model: row.model, value: metric === 'cost' ? BigInt(row.cost_micros!) : BigInt(row.requests) });
  }
  const total = values.reduce((sum, row) => sum + row.value, 0n);
  if (values.some(row => row.value < 0n)) return { state: 'signed', total, items: [] };
  if (total === 0n) return { state: 'empty', total, items: [] };
  values.sort((a, b) => a.value === b.value ? a.model.localeCompare(b.model) : a.value > b.value ? -1 : 1);
  const positive = values.filter(row => row.value > 0n);
  const items: BreakdownItem[] = positive.slice(0, 5).map(row => ({
    key: 'model:' + row.model, model: row.model, value: row.value,
    share: Number(row.value * 1000000n / total) / 1000000,
    color: colors[names.indexOf(row.model) % colors.length],
  }));
  if (positive.length > 5) {
    const value = positive.slice(5).reduce((sum, row) => sum + row.value, 0n);
    items.push({ key: 'remainder', model: null, value, share: Number(value * 1000000n / total) / 1000000, color: '#9298a3' });
  }
  return { state: 'ready', total, items };
}
