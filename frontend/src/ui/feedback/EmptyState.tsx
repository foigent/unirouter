import type { ReactNode } from 'react';
import { Empty } from '@douyinfe/semi-ui';

interface EmptyStateProps {
  title: string;
  description?: string;
  actions?: ReactNode;
  minHeight?: number;
}

export default function EmptyState({ title, description, actions, minHeight = 128 }: EmptyStateProps) {
  return (
    <div className="ur-empty-state" style={{ minHeight }}>
      <Empty title={title} description={description} />
      {actions && <div className="ur-empty-actions">{actions}</div>}
    </div>
  );
}
