type ExactDecimalOptions = {
  decimalPlaces: number;
  maxWholeDigits: number;
  minScaled?: bigint;
  maxScaled?: bigint;
  errorMessage: string;
};

export function canonicalExactDecimal(value: unknown, options: ExactDecimalOptions): string {
  const raw = String(value ?? '').trim();
  if (raw.length === 0 || raw.length > 64 || !/^\d+(?:\.\d+)?$/.test(raw)) {
    throw new Error(options.errorMessage);
  }
  const [rawWhole, rawFraction = ''] = raw.split('.');
  if (rawFraction.length > options.decimalPlaces) {
    throw new Error(options.errorMessage);
  }
  const whole = rawWhole.replace(/^0+(?=\d)/, '');
  if (whole.length > options.maxWholeDigits) {
    throw new Error(options.errorMessage);
  }
  const scale = 10n ** BigInt(options.decimalPlaces);
  const fractionPadded = rawFraction.padEnd(options.decimalPlaces, '0');
  const scaled = BigInt(whole) * scale + BigInt(fractionPadded || '0');
  if ((options.minScaled !== undefined && scaled < options.minScaled)
    || (options.maxScaled !== undefined && scaled > options.maxScaled)) {
    throw new Error(options.errorMessage);
  }
  const fraction = rawFraction.replace(/0+$/, '');
  return fraction ? `${whole}.${fraction}` : whole;
}

export function groupExactDecimal(value: string): string {
  const [whole, fraction] = value.split('.');
  const groupedWhole = whole.replace(/\B(?=(\d{3})+(?!\d))/g, ',');
  return fraction ? `${groupedWhole}.${fraction}` : groupedWhole;
}

export function padExactDecimal(value: string, minimumFractionDigits: number): string {
  const [whole, fraction = ''] = value.split('.');
  if (fraction.length >= minimumFractionDigits) return value;
  return `${whole}.${fraction.padEnd(minimumFractionDigits, '0')}`;
}
