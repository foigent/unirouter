import type { ReactNode } from 'react';
import { Tag } from '@douyinfe/semi-ui';

interface StatusTagProps {
  status: string | number | boolean;
  children?: ReactNode;
  title?: string;
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
  if (['healthy', 'active', 'enabled', 'success', 'passed', 'ok', 'connected', 'available', 'configured', 'ready', 'canary_active'].includes(normalized)) return 'green';
  if (['warning', 'degraded', 'cooldown', 'pending', 'pending_shadow', 'half_open', 'deprecated', 'stale', 'misconfigured', 'unknown'].includes(normalized)) return 'orange';
  if (['failed', 'error', 'unhealthy', 'blocked', 'open', 'unavailable'].includes(normalized)) return 'red';
  if (['disabled', 'inactive', 'closed', 'not_configured', 'not_targeted', 'not_applicable', 'rolled_back'].includes(normalized)) return 'grey';
  return 'blue';
}

export default function StatusTag({ status, children, title }: StatusTagProps) {
  return (
    <Tag size="small" color={colorForStatus(status) as never}>
      {title ? <span title={title}>{children ?? String(status)}</span> : children ?? String(status)}
    </Tag>
  );
}
