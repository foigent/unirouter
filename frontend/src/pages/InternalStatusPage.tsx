import { useEffect, useState } from 'react';
import { Button, Table, Toast, Typography } from '@douyinfe/semi-ui';
import { IconAlertTriangle, IconBell, IconRefresh, IconServer } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState, MetricTile, PageHeader, StatusTag, Surface } from '../ui';
import { deliveryStateLabel, eventStatusLabel, eventTypeLabel, internalStatusLabel, severityLabel } from '../utils/displayLabels';

const { Text } = Typography;

interface Incident {
  id: number;
  event_type: string;
  severity: string;
  title: string;
  message: string;
  resource: string;
  resource_id: string;
  status: string;
  delivery_state: string;
  created_at: string;
  updated_at: string;
}

interface InternalStatus {
  status: string;
  open_critical: number;
  open_warning: number;
  incidents: Incident[];
  maintenance_windows: unknown[];
}

export default function InternalStatusPage() {
  const [data, setData] = useState<InternalStatus | null>(null);
  const [loading, setLoading] = useState(true);

  const load = () => {
    setLoading(true);
    api.getInternalStatus()
      .then((result: any) => setData(result))
      .catch((err: any) => Toast.error(`内部状态加载失败${err?.message ? `：${err.message}` : ''}`))
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, []);

  const columns = [
    { title: '事件', dataIndex: 'title', key: 'title', render: (value: string, row: Incident) => (
      <div>
        <Text strong>{value}</Text>
        <div><Text type="tertiary" size="small">{row.message}</Text></div>
      </div>
    ) },
    { title: '类型', dataIndex: 'event_type', key: 'event_type', render: (value: string) => <Text>{eventTypeLabel(value)}</Text> },
    { title: '级别', dataIndex: 'severity', key: 'severity', render: (value: string) => <StatusTag status={value}>{severityLabel(value)}</StatusTag> },
    { title: '事件状态', dataIndex: 'status', key: 'status', render: (value: string) => <StatusTag status={value}>{eventStatusLabel(value)}</StatusTag> },
    { title: '投递', dataIndex: 'delivery_state', key: 'delivery_state', render: (value: string) => <StatusTag status={value}>{deliveryStateLabel(value)}</StatusTag> },
    { title: '资源', key: 'resource', render: (_: unknown, row: Incident) => row.resource ? `${row.resource}:${row.resource_id || '-'}` : '-' },
    { title: '时间', dataIndex: 'created_at', key: 'created_at', render: (value: string) => value ? new Date(value).toLocaleString() : '-' },
  ];

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="系统治理"
        title="内部状态"
        description="基于通知事件聚合当前服务状态；当前状态只由 open 严重/预警事件决定，维护窗口调度仍属于 PRD4。"
        actions={<Button icon={<IconRefresh />} onClick={load} loading={loading}>刷新</Button>}
      />

      <div className="ur-kpi-grid">
        <MetricTile label="当前状态" value={<StatusTag status={data?.status || 'operational'}>{internalStatusLabel(data?.status || 'operational')}</StatusTag>} icon={<IconServer />} tone={data?.status === 'degraded' ? 'red' : data?.status === 'warning' ? 'amber' : 'green'} meta="基于通知事件聚合" />
        <MetricTile label="Open 严重事件" value={data?.open_critical || 0} icon={<IconAlertTriangle />} tone={(data?.open_critical || 0) > 0 ? 'red' : 'slate'} meta="severity = critical 且 status = open" />
        <MetricTile label="Open 预警事件" value={data?.open_warning || 0} icon={<IconBell />} tone={(data?.open_warning || 0) > 0 ? 'amber' : 'slate'} meta="severity = warning 且 status = open" />
        <MetricTile label="维护窗口" value="未启用" icon={<IconServer />} tone="slate" meta="当前接口保留空数组，调度能力进入 PRD4" />
      </div>

      <Surface title="通知事件" description="最近 20 条 notification_events 按 open 优先、时间倒序展示。事件状态：未关闭 / 已确认 / 已解决；投递状态只表示 webhook 投递结果。">
        <div className="ur-table-shell">
          <Table
            columns={columns}
            dataSource={data?.incidents || []}
            rowKey="id"
            loading={loading}
            pagination={false}
            size="small"
            empty={<EmptyState title="暂无事件" description="没有 open 或近期通知事件。" minHeight={180} />}
          />
        </div>
      </Surface>
    </div>
  );
}
