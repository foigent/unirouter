import type { ReactNode } from 'react';
import { Empty, Typography } from '@douyinfe/semi-ui';

const { Text } = Typography;

interface EmptyStateProps {
  title: string;
  description?: string;
  actions?: ReactNode;
  minHeight?: number;
}

export default function EmptyState({ title, description, actions, minHeight = 220 }: EmptyStateProps) {
  return (
    <div className="ur-empty-state" style={{ minHeight }}>
      <Empty title={title} description={description} />
      {description && <Text className="ur-empty-description">{description}</Text>}
      {actions && <div className="ur-empty-actions">{actions}</div>}
    </div>
  );
}
