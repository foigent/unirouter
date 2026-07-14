import { Typography } from '@douyinfe/semi-ui';

const { Text } = Typography;

interface MoneyTextProps {
	micros: string | bigint;
  precision?: number;
  strong?: boolean;
  tone?: 'default' | 'cost' | 'income' | 'warning';
}

export function parseMicros(value: string | bigint): bigint {
	if (typeof value === 'bigint') return value;
	if (!/^-?\d+$/.test(value)) throw new Error(`Invalid micro-CNY amount: ${value}`);
	return BigInt(value);
}

export function formatMoney(micros: string | bigint, precision?: number) {
	const value = parseMicros(micros);
	const negative = value < 0n;
	const absolute = negative ? -value : value;
	// Keep cent-aligned balances compact without hiding canonical sub-cent
	// ledger evidence behind an implicit two-decimal rounding.
	const requestedDigits = precision ?? (absolute % 10_000n === 0n ? 2 : 6);
	const digits = Math.max(0, Math.min(12, Math.trunc(requestedDigits)));
	let whole: bigint;
	let fraction: string;
	if (digits < 6) {
		const factor = 10n ** BigInt(6 - digits);
		const rounded = (absolute + factor / 2n) / factor;
		const displayScale = 10n ** BigInt(digits);
		whole = rounded / displayScale;
		fraction = digits > 0 ? (rounded % displayScale).toString().padStart(digits, '0') : '';
	} else {
		whole = absolute / 1_000_000n;
		fraction = (absolute % 1_000_000n).toString().padStart(6, '0') + '0'.repeat(digits - 6);
	}
	const rendered = fraction ? `${whole}.${fraction}` : whole.toString();
	const sign = negative && (whole !== 0n || /[1-9]/.test(fraction)) ? '-' : '';
	return `¥${sign}${rendered}`;
}

export function microsRatio(numerator: string | bigint, denominator: string | bigint): number {
	const top = parseMicros(numerator);
	const bottom = parseMicros(denominator);
	if (bottom <= 0n || top <= 0n) return 0;
	return Number((top * 1_000_000n) / bottom) / 1_000_000;
}

export function microsToChartYuan(micros: string | bigint): number {
	const value = parseMicros(micros);
	return Number(value / 1_000_000n) + Number(value % 1_000_000n) / 1_000_000;
}

export default function MoneyText({ micros, precision, strong = false, tone = 'default' }: MoneyTextProps) {
  return (
    <Text strong={strong} className={`ur-money ur-money-${tone}`}>
			{formatMoney(micros, precision)}
    </Text>
  );
}
