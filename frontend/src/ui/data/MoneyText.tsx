import { Typography } from '@douyinfe/semi-ui';

const { Text } = Typography;

interface MoneyTextProps {
  cents: number;
  precision?: number;
  strong?: boolean;
  tone?: 'default' | 'cost' | 'income' | 'warning';
}

export function formatMoney(cents: number, precision = 2) {
  return `¥${(cents / 100).toFixed(precision)}`;
}

export default function MoneyText({ cents, precision = 2, strong = false, tone = 'default' }: MoneyTextProps) {
  return (
    <Text strong={strong} className={`ur-money ur-money-${tone}`}>
      {formatMoney(cents, precision)}
    </Text>
  );
}
