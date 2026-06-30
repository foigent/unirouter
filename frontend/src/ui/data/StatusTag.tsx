import type { ReactNode } from 'react';
import { Tag } from '@douyinfe/semi-ui';

interface StatusTagProps {
  status: string | number | boolean;
  children?: ReactNode;
}

function colorForStatus(status: string | number | boolean) {
  if (typeof status === 'number') {
    if (status >= 200 && status < 300) return 'green';
    if (status === 429) return 'orange';
    if (status >= 500) return 'red';
    return 'orange';
  }
  if (typeof status === 'boolean') return status ? 'green' : 'grey';

  const normalized = status.toLowerCase();
  if (['healthy', 'active', 'enabled', 'success', 'passed', 'ok'].includes(normalized)) return 'green';
  if (['warning', 'degraded', 'cooldown', 'pending', 'half_open', 'deprecated'].includes(normalized)) return 'orange';
  if (['failed', 'error', 'unhealthy', 'blocked', 'open'].includes(normalized)) return 'red';
  if (['disabled', 'inactive', 'closed'].includes(normalized)) return 'grey';
  return 'blue';
}

export default function StatusTag({ status, children }: StatusTagProps) {
  return (
    <Tag size="small" color={colorForStatus(status) as never}>
      {children ?? String(status)}
    </Tag>
  );
}
