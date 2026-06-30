import type { ReactNode } from 'react';
import clsx from 'clsx';
import { Typography } from '@douyinfe/semi-ui';

const { Text } = Typography;

interface MetricTileProps {
  label: string;
  value: ReactNode;
  icon?: ReactNode;
  tone?: 'blue' | 'green' | 'amber' | 'violet' | 'red' | 'slate';
  meta?: ReactNode;
}

export default function MetricTile({ label, value, icon, tone = 'slate', meta }: MetricTileProps) {
  return (
    <div className={clsx('ur-metric', `ur-metric-${tone}`)}>
      <div className="ur-metric-head">
        {icon && <div className="ur-metric-icon">{icon}</div>}
        <Text className="ur-metric-label">{label}</Text>
      </div>
      <div className="ur-metric-value">{value}</div>
      {meta && <div className="ur-metric-meta">{meta}</div>}
    </div>
  );
}
