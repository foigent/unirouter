import { useCallback, useEffect, useState } from 'react';
import { Button, Form, Modal, Select, SideSheet, Table, Tabs, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconBell, IconPlus, IconRefresh, IconSetting } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { formatDateTime } from '../utils/formatDate';
import { PageHeader, StatusTag, Surface } from '../ui';
import {
  deliveryStateLabel,
  deliveryStateOptions,
  deliveryStatusOptions,
  eventTypeLabel,
  eventTypeListLabel,
  eventTypeOptions,
  notificationMessageLabel,
  notificationResourceLabel,
  notificationTitleLabel,
  severityLabel,
  severityOptions,
} from '../utils/displayLabels';

const { Text } = Typography;
const { TabPane } = Tabs;

type TabKey = 'events' | 'deliveries' | 'webhooks';

interface NotificationEvent {
  id: number;
  project_id?: number | null;
  event_type: string;
  severity: string;
  title: string;
  message: string;
  resource?: string;
  resource_id?: string;
  status: string;
  delivery_state: string;
  last_delivery_at?: string | null;
  created_at: string;
}

interface NotificationDelivery {
  id: number;
  event_id: number;
  webhook_id?: number | null;
  webhook_name?: string;
  event_type?: string;
  event_title?: string;
  status: string;
  attempts: number;
  response_status?: number | null;
  error?: string;
  next_retry_at?: string | null;
  retry_scope_quarantined?: boolean;
  scope_quarantine_previous_next_retry_at?: string | null;
  delivered_at?: string | null;
  updated_at: string;
}

interface NotificationWebhook {
  id: number;
  org_id: number;
  project_id?: number | null;
  name: string;
  url: string;
  event_types?: string[] | string;
  enabled: boolean;
  scope_verified: boolean;
  scope_quarantine_previous_enabled?: boolean | null;
  updated_at: string;
}

function severityColor(severity: string) {
  if (severity === 'critical') return 'red';
  if (severity === 'warning') return 'orange';
  return 'blue';
}

function toEventTypeList(value: unknown) {
  if (Array.isArray(value)) return value.filter(Boolean).join(', ');
  if (typeof value === 'string') {
    try {
      const parsed = JSON.parse(value);
      if (Array.isArray(parsed)) return parsed.join(', ');
    } catch {
      return value;
    }
  }
  return '-';
}

function parseEventTypes(value?: string) {
  return (value || '')
    .split(/[\n,]/)
    .map(item => item.trim())
    .filter(Boolean);
}

export default function NotificationCenterPage() {
  const [activeTab, setActiveTab] = useState<TabKey>('events');

  const [events, setEvents] = useState<NotificationEvent[]>([]);
  const [eventTotal, setEventTotal] = useState(0);
  const [eventPage, setEventPage] = useState(1);
  const [eventPageSize, setEventPageSize] = useState(20);
  const [eventLoading, setEventLoading] = useState(false);
  const [eventType, setEventType] = useState('');
  const [severity, setSeverity] = useState('');
  const [deliveryState, setDeliveryState] = useState('');

  const [deliveries, setDeliveries] = useState<NotificationDelivery[]>([]);
  const [deliveryTotal, setDeliveryTotal] = useState(0);
  const [deliveryPage, setDeliveryPage] = useState(1);
  const [deliveryPageSize, setDeliveryPageSize] = useState(20);
  const [deliveryLoading, setDeliveryLoading] = useState(false);
  const [deliveryStatus, setDeliveryStatus] = useState('');

  const [webhooks, setWebhooks] = useState<NotificationWebhook[]>([]);
  const [webhookTotal, setWebhookTotal] = useState(0);
  const [webhookPage, setWebhookPage] = useState(1);
  const [webhookPageSize, setWebhookPageSize] = useState(20);
  const [webhookLoading, setWebhookLoading] = useState(false);
  const [webhookSheet, setWebhookSheet] = useState<{ mode: 'create' | 'edit'; record?: NotificationWebhook } | null>(null);
  const [webhookFormApi, setWebhookFormApi] = useState<any>(null);

  const loadEvents = useCallback(async (page = eventPage, pageSize = eventPageSize) => {
    setEventLoading(true);
    try {
      const data = await api.getNotificationEvents(page, pageSize, {
        event_type: eventType,
        severity,
        delivery_state: deliveryState,
      });
      setEvents((data?.data || []) as NotificationEvent[]);
      setEventTotal(Number(data?.total || 0));
    } catch (err: any) {
      Toast.error(`通知事件加载失败${err?.message ? `：${err.message}` : ''}`);
    } finally {
      setEventLoading(false);
    }
  }, [deliveryState, eventPage, eventPageSize, eventType, severity]);

  const loadDeliveries = useCallback(async (page = deliveryPage, pageSize = deliveryPageSize) => {
    setDeliveryLoading(true);
    try {
      const data = await api.getNotificationDeliveries(page, pageSize, { status: deliveryStatus });
      setDeliveries((data?.data || []) as NotificationDelivery[]);
      setDeliveryTotal(Number(data?.total || 0));
    } catch (err: any) {
      Toast.error(`投递记录加载失败${err?.message ? `：${err.message}` : ''}`);
    } finally {
      setDeliveryLoading(false);
    }
  }, [deliveryPage, deliveryPageSize, deliveryStatus]);

  const loadWebhooks = useCallback(async (page = webhookPage, pageSize = webhookPageSize) => {
    setWebhookLoading(true);
    try {
      const data = await api.getNotificationWebhooks(page, pageSize);
      setWebhooks((data?.data || []) as NotificationWebhook[]);
      setWebhookTotal(Number(data?.total || 0));
    } catch (err: any) {
      Toast.error(`Webhook 路由加载失败${err?.message ? `：${err.message}` : ''}`);
    } finally {
      setWebhookLoading(false);
    }
  }, [webhookPage, webhookPageSize]);

  useEffect(() => { loadEvents(); }, [loadEvents]);
  useEffect(() => { loadDeliveries(); }, [loadDeliveries]);
  useEffect(() => { loadWebhooks(); }, [loadWebhooks]);

  const openWebhookSheet = (record?: NotificationWebhook) => {
    setWebhookSheet(record ? { mode: 'edit', record } : { mode: 'create' });
    setTimeout(() => {
      webhookFormApi?.setValues(record ? {
        name: record.name,
        url: record.url,
        event_types: toEventTypeList(record.event_types) === '-' ? '' : toEventTypeList(record.event_types),
        enabled: (record.scope_verified ? record.enabled : record.scope_quarantine_previous_enabled) ? 'true' : 'false',
        project_id: record.project_id || undefined,
      } : { enabled: 'true' });
    }, 0);
  };

  const saveWebhook = async (values: any) => {
    const payload = {
      name: values.name,
      url: values.url,
      project_id: values.project_id ? Number(values.project_id) : 0,
      event_types: parseEventTypes(values.event_types),
      enabled: values.enabled !== 'false',
    };
    try {
      if (webhookSheet?.mode === 'edit' && webhookSheet.record) {
        await api.updateNotificationWebhook(webhookSheet.record.id, payload);
        Toast.warning('Webhook 已更新并隔离，请核对后确认范围');
      } else {
        await api.createNotificationWebhook(payload);
        Toast.success('Webhook 已创建');
      }
      setWebhookSheet(null);
      loadWebhooks(1);
    } catch (err: any) {
      Toast.error(`保存失败${err?.message ? `：${err.message}` : ''}`);
    }
  };

  const verifyWebhookScope = (record: NotificationWebhook) => {
    Modal.confirm({
      title: '确认 Webhook 路由范围',
      content: (
        <div>
          <p>请逐项核对以下不可编辑快照。确认后只影响后续新通知；升级前失败投递不会自动重放。</p>
          <p><strong>名称：</strong>{record.name}</p>
          <p><strong>组织：</strong>#{record.org_id}</p>
          <p><strong>项目：</strong>{record.project_id ? `#${record.project_id}` : '组织级'}</p>
          <p><strong>目标 URL：</strong><code style={{ wordBreak: 'break-all' }}>{record.url}</code></p>
          <p><strong>事件类型：</strong>{toEventTypeList(record.event_types)}</p>
          <p><strong>确认后状态：</strong>{record.scope_quarantine_previous_enabled ? '启用' : '停用'}</p>
        </div>
      ),
      okText: '确认范围',
      cancelText: '取消',
      onOk: async () => {
        try {
          await api.verifyNotificationWebhookScope(record.id, record.updated_at);
          Toast.success('Webhook 路由范围已确认');
          await loadWebhooks(1);
        } catch (err: any) {
          Toast.error(`确认失败${err?.message ? `：${err.message}` : ''}`);
          throw err;
        }
      },
    });
  };

  const eventColumns = [
    { title: 'ID', dataIndex: 'id', width: 72 },
    { title: '事件类型', dataIndex: 'event_type', width: 220, render: (v: string) => <Text>{eventTypeLabel(v)}</Text> },
    { title: '级别', dataIndex: 'severity', width: 100, render: (v: string) => <Tag color={severityColor(v)}>{severityLabel(v)}</Tag> },
    { title: '标题', dataIndex: 'title', width: 220, render: (v: string, row: NotificationEvent) => notificationTitleLabel(row.event_type, v) },
    { title: '消息', dataIndex: 'message', ellipsis: true, render: (v: string, row: NotificationEvent) => notificationMessageLabel(row.event_type, v) },
    { title: '资源', width: 160, render: (_: unknown, row: NotificationEvent) => notificationResourceLabel(row.resource, row.resource_id) },
    { title: '投递', dataIndex: 'delivery_state', width: 130, render: (v: string) => <StatusTag status={v}>{deliveryStateLabel(v)}</StatusTag> },
    { title: '时间', dataIndex: 'created_at', width: 180, render: (v: string) => formatDateTime(v) },
  ];

  const deliveryColumns = [
    { title: 'ID', dataIndex: 'id', width: 72 },
    { title: '事件', dataIndex: 'event_type', width: 220, render: (v: string) => <Text>{eventTypeLabel(v)}</Text> },
    { title: 'Webhook', dataIndex: 'webhook_name', width: 180, render: (v: string) => v || '-' },
    { title: '状态', dataIndex: 'status', width: 120, render: (v: string, row: NotificationDelivery) => row.retry_scope_quarantined ? <Tag color="orange">安全隔离</Tag> : <StatusTag status={v}>{deliveryStateLabel(v)}</StatusTag> },
    { title: '尝试', dataIndex: 'attempts', width: 88 },
    { title: '响应', dataIndex: 'response_status', width: 88, render: (v: number) => v || '-' },
    {
      title: '下次重试',
      dataIndex: 'next_retry_at',
      width: 260,
      render: (v: string, row: NotificationDelivery) => row.retry_scope_quarantined
        ? `已取消自动重试${row.scope_quarantine_previous_next_retry_at ? `（原 ${formatDateTime(row.scope_quarantine_previous_next_retry_at)}）` : ''}`
        : (v ? formatDateTime(v) : '-'),
    },
    { title: '错误', dataIndex: 'error', ellipsis: true, render: (v: string) => v || '-' },
    { title: '更新时间', dataIndex: 'updated_at', width: 180, render: (v: string) => formatDateTime(v) },
  ];

  const webhookColumns = [
    { title: 'ID', dataIndex: 'id', width: 72 },
    { title: '组织', dataIndex: 'org_id', width: 100, render: (v: number) => `#${v}` },
    { title: '名称', dataIndex: 'name', width: 180 },
    { title: 'URL', dataIndex: 'url', ellipsis: true },
    { title: '事件类型', dataIndex: 'event_types', width: 260, render: (v: unknown) => eventTypeListLabel(v) },
    { title: '范围', dataIndex: 'project_id', width: 120, render: (v: number) => v ? `项目 #${v}` : '组织级' },
    {
      title: '启用',
      dataIndex: 'enabled',
      width: 150,
      render: (v: boolean, row: NotificationWebhook) => row.scope_verified
        ? <StatusTag status={v ? 'active' : 'disabled'}>{v ? '启用' : '停用'}</StatusTag>
        : <Tag color="orange">隔离（确认后{row.scope_quarantine_previous_enabled ? '启用' : '停用'}）</Tag>,
    },
    { title: '安全确认', dataIndex: 'scope_verified', width: 120, render: (v: boolean) => <Tag color={v ? 'green' : 'orange'}>{v ? '已确认' : '已隔离'}</Tag> },
    {
      title: '操作',
      width: 190,
      render: (_: unknown, row: NotificationWebhook) => (
        <div style={{ display: 'flex', gap: 8 }}>
          <Button size="small" icon={<IconSetting />} onClick={() => openWebhookSheet(row)}>编辑</Button>
          {!row.scope_verified && <Button size="small" type="primary" onClick={() => verifyWebhookScope(row)}>确认范围</Button>}
        </div>
      ),
    },
  ];

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="流量治理"
        title="通知中心"
        description="查看事件、Webhook 投递状态和项目预算通知路由；“未配置 Webhook”表示事件已入库，但没有匹配的启用投递路由。"
        actions={<Button icon={<IconRefresh />} onClick={() => { loadEvents(); loadDeliveries(); loadWebhooks(); }}>刷新</Button>}
      />

      <Surface>
        <Tabs activeKey={activeTab} onChange={(key) => setActiveTab(key as TabKey)}>
          <TabPane tab="事件" itemKey="events">
            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 16 }}>
              <Select optionList={eventTypeOptions} value={eventType || undefined} placeholder="事件类型" style={{ width: 250 }} onChange={(v) => { setEventType((v as string) || ''); setEventPage(1); }} />
              <Select optionList={severityOptions} value={severity || undefined} placeholder="级别" style={{ width: 160 }} onChange={(v) => { setSeverity((v as string) || ''); setEventPage(1); }} />
              <Select optionList={deliveryStateOptions} value={deliveryState || undefined} placeholder="投递状态" style={{ width: 190 }} onChange={(v) => { setDeliveryState((v as string) || ''); setEventPage(1); }} />
            </div>
            <div className="ur-table-shell">
              <Table
                columns={eventColumns}
                dataSource={events}
                loading={eventLoading}
                rowKey="id"
                pagination={{
                  currentPage: eventPage,
                  pageSize: eventPageSize,
                  total: eventTotal,
                  onPageChange: (p: number) => setEventPage(p),
                  onPageSizeChange: (s: number) => { setEventPageSize(s); setEventPage(1); },
                }}
              />
            </div>
          </TabPane>
          <TabPane tab="投递" itemKey="deliveries">
            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 16 }}>
              <Select optionList={deliveryStatusOptions} value={deliveryStatus || undefined} placeholder="投递状态" style={{ width: 180 }} onChange={(v) => { setDeliveryStatus((v as string) || ''); setDeliveryPage(1); }} />
            </div>
            <div className="ur-table-shell">
              <Table
                columns={deliveryColumns}
                dataSource={deliveries}
                loading={deliveryLoading}
                rowKey="id"
                pagination={{
                  currentPage: deliveryPage,
                  pageSize: deliveryPageSize,
                  total: deliveryTotal,
                  onPageChange: (p: number) => setDeliveryPage(p),
                  onPageSizeChange: (s: number) => { setDeliveryPageSize(s); setDeliveryPage(1); },
                }}
              />
            </div>
          </TabPane>
          <TabPane tab="Webhook" itemKey="webhooks">
            <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 16 }}>
              <Button icon={<IconPlus />} type="primary" onClick={() => openWebhookSheet()}>新增 Webhook</Button>
            </div>
            <div className="ur-table-shell">
              <Table
                columns={webhookColumns}
                dataSource={webhooks}
                loading={webhookLoading}
                rowKey="id"
                pagination={{
                  currentPage: webhookPage,
                  pageSize: webhookPageSize,
                  total: webhookTotal,
                  onPageChange: (p: number) => setWebhookPage(p),
                  onPageSizeChange: (s: number) => { setWebhookPageSize(s); setWebhookPage(1); },
                }}
              />
            </div>
          </TabPane>
        </Tabs>
      </Surface>

      <SideSheet
        title={webhookSheet?.mode === 'edit' ? '编辑 Webhook' : '新增 Webhook'}
        visible={!!webhookSheet}
        width={520}
        onCancel={() => setWebhookSheet(null)}
      >
        <Form getFormApi={(formApi: any) => setWebhookFormApi(formApi)} onSubmit={saveWebhook}>
          <Form.Input field="name" label="名称" rules={[{ required: true, message: '必填' }]} />
          <Form.Input field="url" label="URL" rules={[{ required: true, message: '必填' }]} placeholder="https://ops.example.com/webhook" />
          <Form.Input field="project_id" label="项目 ID" placeholder="留空为组织级路由" />
          <Form.TextArea field="event_types" label="事件类型" placeholder="留空表示全部事件；可用逗号或换行分隔" autosize={{ minRows: 3, maxRows: 6 }} />
          <Form.Select field="enabled" label="状态" initValue="true" optionList={[
            { label: '启用', value: 'true' },
            { label: '停用', value: 'false' },
          ]} />
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 24 }}>
            <Button onClick={() => setWebhookSheet(null)}>取消</Button>
            <Button type="primary" icon={<IconBell />} onClick={() => webhookFormApi?.submitForm()}>保存</Button>
          </div>
        </Form>
      </SideSheet>
    </div>
  );
}
