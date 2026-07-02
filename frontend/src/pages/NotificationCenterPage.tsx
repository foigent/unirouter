import { useCallback, useEffect, useState } from 'react';
import { Button, Form, Select, SideSheet, Table, Tabs, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconBell, IconPlus, IconRefresh, IconSetting } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { formatDateTime } from '../utils/formatDate';
import { PageHeader, StatusTag, Surface } from '../ui';

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
  delivered_at?: string | null;
  updated_at: string;
}

interface NotificationWebhook {
  id: number;
  project_id?: number | null;
  name: string;
  url: string;
  event_types?: string[] | string;
  enabled: boolean;
  updated_at: string;
}

const eventTypeOptions = [
  { label: '全部事件', value: '' },
  { label: 'project_budget.threshold', value: 'project_budget.threshold' },
  { label: 'api_key.disabled', value: 'api_key.disabled' },
  { label: 'model.deprecated', value: 'model.deprecated' },
  { label: 'model.retired', value: 'model.retired' },
  { label: 'provider.channel.failure', value: 'provider.channel.failure' },
  { label: 'license.warning', value: 'license.warning' },
  { label: 'token_metering.estimated_ratio.high', value: 'token_metering.estimated_ratio.high' },
];

const severityOptions = [
  { label: '全部级别', value: '' },
  { label: 'info', value: 'info' },
  { label: 'warning', value: 'warning' },
  { label: 'critical', value: 'critical' },
];

const deliveryStateOptions = [
  { label: '全部投递状态', value: '' },
  { label: 'not_configured', value: 'not_configured' },
  { label: 'pending', value: 'pending' },
  { label: 'delivered', value: 'delivered' },
  { label: 'failed', value: 'failed' },
];

const deliveryStatusOptions = [
  { label: '全部状态', value: '' },
  { label: 'pending', value: 'pending' },
  { label: 'delivered', value: 'delivered' },
  { label: 'failed', value: 'failed' },
];

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
        enabled: record.enabled ? 'true' : 'false',
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
        Toast.success('Webhook 已更新');
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

  const eventColumns = [
    { title: 'ID', dataIndex: 'id', width: 72 },
    { title: '事件类型', dataIndex: 'event_type', width: 220, render: (v: string) => <Text code>{v}</Text> },
    { title: '级别', dataIndex: 'severity', width: 100, render: (v: string) => <Tag color={severityColor(v)}>{v}</Tag> },
    { title: '标题', dataIndex: 'title', width: 220 },
    { title: '消息', dataIndex: 'message', ellipsis: true },
    { title: '资源', width: 140, render: (_: unknown, row: NotificationEvent) => row.resource ? `${row.resource}:${row.resource_id || '-'}` : '-' },
    { title: '投递', dataIndex: 'delivery_state', width: 130, render: (v: string) => <StatusTag status={v}>{v}</StatusTag> },
    { title: '时间', dataIndex: 'created_at', width: 180, render: (v: string) => formatDateTime(v) },
  ];

  const deliveryColumns = [
    { title: 'ID', dataIndex: 'id', width: 72 },
    { title: '事件', dataIndex: 'event_type', width: 220, render: (v: string) => <Text code>{v || '-'}</Text> },
    { title: 'Webhook', dataIndex: 'webhook_name', width: 180, render: (v: string) => v || '-' },
    { title: '状态', dataIndex: 'status', width: 120, render: (v: string) => <StatusTag status={v}>{v}</StatusTag> },
    { title: '尝试', dataIndex: 'attempts', width: 88 },
    { title: '响应', dataIndex: 'response_status', width: 88, render: (v: number) => v || '-' },
    { title: '下次重试', dataIndex: 'next_retry_at', width: 180, render: (v: string) => v ? formatDateTime(v) : '-' },
    { title: '错误', dataIndex: 'error', ellipsis: true, render: (v: string) => v || '-' },
    { title: '更新时间', dataIndex: 'updated_at', width: 180, render: (v: string) => formatDateTime(v) },
  ];

  const webhookColumns = [
    { title: 'ID', dataIndex: 'id', width: 72 },
    { title: '名称', dataIndex: 'name', width: 180 },
    { title: 'URL', dataIndex: 'url', ellipsis: true },
    { title: '事件类型', dataIndex: 'event_types', width: 260, render: (v: unknown) => toEventTypeList(v) },
    { title: '范围', dataIndex: 'project_id', width: 120, render: (v: number) => v ? `Project ${v}` : 'Org' },
    { title: '启用', dataIndex: 'enabled', width: 92, render: (v: boolean) => <StatusTag status={v ? 'active' : 'disabled'}>{v ? '启用' : '停用'}</StatusTag> },
    { title: '操作', width: 90, render: (_: unknown, row: NotificationWebhook) => <Button size="small" icon={<IconSetting />} onClick={() => openWebhookSheet(row)} /> },
  ];

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="Operations"
        title="通知中心"
        description="查看事件、Webhook 投递状态和项目预算通知路由。"
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
          <Form.Input field="project_id" label="Project ID" placeholder="留空为组织级路由" />
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
