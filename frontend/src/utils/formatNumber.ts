export function safeNumber(value: number | null | undefined): number {
  const n = Number(value || 0);
  return Number.isFinite(n) ? n : 0;
}

export function formatCompactNumber(value: number | null | undefined): string {
  const n = safeNumber(value);
  if (Math.abs(n) >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (Math.abs(n) >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toLocaleString();
}

export function formatInteger(value: number | null | undefined): string {
  return safeNumber(value).toLocaleString();
}

export function formatSignedInteger(value: number | null | undefined): string {
  const n = safeNumber(value);
  return `${n > 0 ? '+' : ''}${n.toLocaleString()}`;
}

export function formatPercent(value: number | null | undefined, precision = 1): string {
  return `${(safeNumber(value) * 100).toFixed(precision)}%`;
}

export function formatLatencyMs(value: number | null | undefined): string {
  return `${safeNumber(value).toFixed(0)}ms`;
}
