import type { ReactNode } from 'react';
import clsx from 'clsx';
import { Typography } from '@douyinfe/semi-ui';

const { Title, Text } = Typography;

interface SurfaceProps {
  title?: string;
  description?: string;
  actions?: ReactNode;
  className?: string;
  children: ReactNode;
}

export default function Surface({ title, description, actions, className, children }: SurfaceProps) {
  return (
    <section className={clsx('ur-surface', className)}>
      {(title || description || actions) && (
        <div className="ur-surface-header">
          <div>
            {title && <Title heading={5} className="ur-surface-title">{title}</Title>}
            {description && <Text className="ur-surface-description">{description}</Text>}
          </div>
          {actions && <div className="ur-surface-actions">{actions}</div>}
        </div>
      )}
      <div className="ur-surface-body">{children}</div>
    </section>
  );
}
