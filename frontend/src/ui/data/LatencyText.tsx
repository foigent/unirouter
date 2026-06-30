import { Typography } from '@douyinfe/semi-ui';

const { Text } = Typography;

interface LatencyTextProps {
  value: number;
}

function toneForLatency(value: number) {
  if (value > 2000) return 'danger';
  if (value > 1000) return 'warning';
  return 'healthy';
}

export default function LatencyText({ value }: LatencyTextProps) {
  return <Text className={`ur-latency ur-latency-${toneForLatency(value)}`}>{value}ms</Text>;
}
