import type { ReactNode } from 'react';
import { Typography } from '@douyinfe/semi-ui';

const { Title, Text } = Typography;

interface PageHeaderProps {
  eyebrow?: string;
  title: string;
  description?: string;
  actions?: ReactNode;
}

export default function PageHeader({ eyebrow, title, description, actions }: PageHeaderProps) {
  return (
    <header className="ur-page-header">
      <div className="ur-page-header-main">
        {eyebrow && <Text className="ur-page-eyebrow">{eyebrow}</Text>}
        <Title heading={3} className="ur-page-title">{title}</Title>
        {description && <Text className="ur-page-description">{description}</Text>}
      </div>
      {actions && <div className="ur-page-actions">{actions}</div>}
    </header>
  );
}
