import { useEffect, useState } from 'react';
import { Button, Table, Toast, Typography } from '@douyinfe/semi-ui';
import { IconAlertTriangle, IconBell, IconRefresh, IconServer } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { canReadRuntimeStatus } from '../navigation';
import { useAuthStore } from '../store/auth';
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

interface RuntimeStatus {
  status: string;
  generated_at: string;
  data_freshness_seconds: number;
  runtime_environment: {
    service: string;
    environment: string;
    deployment_mode: string;
    deployment_role: string;
    version?: string;
    commit?: string;
    go_version: string;
    num_cpu: number;
    goroutines: number;
    uptime_seconds: number;
  };
  capacity_health: {
    app_memory: { status: string; alloc_bytes: number; sys_bytes: number; heap_alloc_bytes: number; heap_sys_bytes: number };
    database: { status: string; latency_ms?: number; reason?: string };
    redis: { status: string; latency_ms?: number; reason?: string };
    traffic: {
      status: string;
      window_minutes: number;
      requests: number;
      error_requests: number;
      error_rate: number;
      total_tokens: number;
      cost_cents: number;
      p95_latency_ms: number;
      p99_latency_ms: number;
      reason?: string;
    };
    cpu: { status: string; reason?: string };
    disk: { status: string; reason?: string };
    network: { status: string; reason?: string };
  };
  observability_integrations: {
    prometheus: { status: string; url?: string; reason?: string };
    grafana: { status: string; url?: string; reason?: string };
  };
}

function runtimeStatusLabel(status?: string) {
  switch (status) {
    case 'operational':
      return '正常';
    case 'warning':
      return '预警';
    case 'degraded':
      return '降级';
    default:
      return status || '-';
  }
}

function componentStatusLabel(status?: string) {
  switch (status) {
    case 'connected':
    case 'available':
      return '可用';
    case 'configured':
      return '已配置';
    case 'not_configured':
      return '未配置';
    case 'unavailable':
      return '不可用';
    default:
      return status || '-';
  }
}

function formatBytes(value?: number) {
  if (!value) return '-';
  const units = ['B', 'KB', 'MB', 'GB'];
  let amount = value;
  let unit = 0;
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024;
    unit += 1;
  }
  return `${amount.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

function formatPercent(value?: number) {
  if (value === undefined || value === null) return '-';
  return `${(value * 100).toFixed(2)}%`;
}

export default function InternalStatusPage() {
  const [data, setData] = useState<InternalStatus | null>(null);
  const [runtimeData, setRuntimeData] = useState<RuntimeStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const role = useAuthStore((s) => s.user?.role);
  const canViewRuntime = canReadRuntimeStatus(role);

  const load = () => {
    setLoading(true);
    const requests: Promise<void>[] = [
      api.getInternalStatus()
        .then((result: any) => setData(result))
        .catch((err: any) => {
          Toast.error(`内部状态加载失败${err?.message ? `：${err.message}` : ''}`);
        }),
    ];
    if (canViewRuntime) {
      requests.push(
        api.getRuntimeStatus()
          .then((result: any) => setRuntimeData(result))
          .catch((err: any) => {
            Toast.error(`运行时状态加载失败${err?.message ? `：${err.message}` : ''}`);
          }),
      );
    } else {
      setRuntimeData(null);
    }
    Promise.allSettled(requests).finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, [canViewRuntime]);

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
        description="事件状态保持 notification_events 聚合语义；运行时观测独立展示应用、依赖和外部监控配置。"
        actions={<Button icon={<IconRefresh />} onClick={load} loading={loading}>刷新</Button>}
      />

      <div className="ur-kpi-grid">
        <MetricTile label="当前状态" value={<StatusTag status={data?.status || 'operational'}>{internalStatusLabel(data?.status || 'operational')}</StatusTag>} icon={<IconServer />} tone={data?.status === 'degraded' ? 'red' : data?.status === 'warning' ? 'amber' : 'green'} meta="基于通知事件聚合" />
        <MetricTile label="Open 严重事件" value={data?.open_critical || 0} icon={<IconAlertTriangle />} tone={(data?.open_critical || 0) > 0 ? 'red' : 'slate'} meta="severity = critical 且 status = open" />
        <MetricTile label="Open 预警事件" value={data?.open_warning || 0} icon={<IconBell />} tone={(data?.open_warning || 0) > 0 ? 'amber' : 'slate'} meta="severity = warning 且 status = open" />
        <MetricTile label="维护窗口" value="未启用" icon={<IconServer />} tone="slate" meta="当前接口保留空数组，调度能力进入 PRD4" />
      </div>

      {canViewRuntime && (
        <Surface title="运行时观测" description="应用运行时、依赖健康、近 5 分钟流量和外部监控入口。">
          <div className="ur-kpi-grid">
            <MetricTile
              label="运行状态"
              value={<StatusTag status={runtimeData?.status || 'pending'}>{runtimeStatusLabel(runtimeData?.status)}</StatusTag>}
              icon={<IconServer />}
              tone={runtimeData?.status === 'degraded' ? 'red' : runtimeData?.status === 'warning' ? 'amber' : 'green'}
              meta={`${runtimeData?.runtime_environment.environment || '-'} / ${runtimeData?.runtime_environment.deployment_role || '-'}`}
            />
            <MetricTile
              label="数据库"
              value={<StatusTag status={runtimeData?.capacity_health.database.status || 'pending'}>{componentStatusLabel(runtimeData?.capacity_health.database.status)}</StatusTag>}
              icon={<IconServer />}
              tone={runtimeData?.capacity_health.database.status === 'connected' ? 'green' : 'red'}
              meta={runtimeData?.capacity_health.database.latency_ms ? `${runtimeData.capacity_health.database.latency_ms} ms` : runtimeData?.capacity_health.database.reason || '-'}
            />
            <MetricTile
              label="Redis"
              value={<StatusTag status={runtimeData?.capacity_health.redis.status || 'pending'}>{componentStatusLabel(runtimeData?.capacity_health.redis.status)}</StatusTag>}
              icon={<IconServer />}
              tone={runtimeData?.capacity_health.redis.status === 'connected' ? 'green' : 'amber'}
              meta={runtimeData?.capacity_health.redis.latency_ms ? `${runtimeData.capacity_health.redis.latency_ms} ms` : runtimeData?.capacity_health.redis.reason || '-'}
            />
            <MetricTile
              label="近 5 分钟请求"
              value={runtimeData?.capacity_health.traffic.requests ?? '-'}
              icon={<IconBell />}
              tone={(runtimeData?.capacity_health.traffic.error_rate || 0) >= 0.05 ? 'amber' : 'blue'}
              meta={`错误率 ${formatPercent(runtimeData?.capacity_health.traffic.error_rate)} / tokens ${runtimeData?.capacity_health.traffic.total_tokens ?? '-'}`}
            />
          </div>
          <div className="ur-kpi-grid" style={{ marginTop: 16 }}>
            <MetricTile
              label="应用内存"
              value={formatBytes(runtimeData?.capacity_health.app_memory.alloc_bytes)}
              icon={<IconServer />}
              tone={runtimeData?.capacity_health.app_memory.status === 'available' ? 'green' : 'slate'}
              meta={`heap ${formatBytes(runtimeData?.capacity_health.app_memory.heap_alloc_bytes)} / goroutines ${runtimeData?.runtime_environment.goroutines ?? '-'}`}
            />
            <MetricTile
              label="主机遥测"
              value={<StatusTag status={runtimeData?.capacity_health.cpu.status || 'pending'}>{componentStatusLabel(runtimeData?.capacity_health.cpu.status)}</StatusTag>}
              icon={<IconAlertTriangle />}
              tone="slate"
              meta={`CPU ${componentStatusLabel(runtimeData?.capacity_health.cpu.status)} / 磁盘 ${componentStatusLabel(runtimeData?.capacity_health.disk.status)} / 网络 ${componentStatusLabel(runtimeData?.capacity_health.network.status)}`}
            />
            <MetricTile
              label="Prometheus"
              value={<StatusTag status={runtimeData?.observability_integrations.prometheus.status || 'pending'}>{componentStatusLabel(runtimeData?.observability_integrations.prometheus.status)}</StatusTag>}
              icon={<IconServer />}
              tone={runtimeData?.observability_integrations.prometheus.status === 'configured' ? 'green' : 'slate'}
              meta={runtimeData?.observability_integrations.prometheus.url || runtimeData?.observability_integrations.prometheus.reason || '-'}
            />
            <MetricTile
              label="Grafana"
              value={<StatusTag status={runtimeData?.observability_integrations.grafana.status || 'pending'}>{componentStatusLabel(runtimeData?.observability_integrations.grafana.status)}</StatusTag>}
              icon={<IconServer />}
              tone={runtimeData?.observability_integrations.grafana.status === 'configured' ? 'green' : 'slate'}
              meta={runtimeData?.observability_integrations.grafana.url || runtimeData?.observability_integrations.grafana.reason || '-'}
            />
          </div>
        </Surface>
      )}

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
