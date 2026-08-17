import { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Form, Modal, Select, SideSheet, Table, Tabs, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconBell, IconPlus, IconRefresh, IconSetting } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { canonicalProjectMembershipRole, isBillingRole, isOrgAdminRole, isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { useAuthStore } from '../store/auth';
import { customerContextBlockerCopy, effectiveCustomerOrgID, resolveCustomerContext } from '../utils/customerContext';
import { formatDateTime } from '../utils/formatDate';
import { EmptyState, PageHeader, StatusTag, Surface } from '../ui';
import { loadAllPages } from '../utils/loadAllPages';
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
  org_id?: number;
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

function projectIDFromLocation() {
  const raw = new URLSearchParams(window.location.search).get('project_id');
  if (!raw || !/^\d+$/.test(raw)) return undefined;
  const value = Number(raw);
  return Number.isSafeInteger(value) && value > 0 ? value : undefined;
}

const eventStatusOptions = [
  { label: '未确认', value: 'open' },
  { label: '已确认', value: 'acknowledged' },
  { label: '已解决', value: 'resolved' },
];

const financialEventTypeOptions = eventTypeOptions.filter(option => (
  !option.value
  || option.value === 'project_budget.threshold'
  || option.value === 'token_metering.estimated_ratio.high'
));

function notificationEventStatusLabel(status?: string) {
  return eventStatusOptions.find(option => option.value === status)?.label || status || '-';
}

export default function NotificationCenterPage() {
  const user = useAuthStore((state) => state.user);
  const organizationRole = organizationRoleForUser(user);
  const platform = isPlatformAdminRole(organizationRole);
  const organizationAdmin = isOrgAdminRole(organizationRole);
  const billing = isBillingRole(organizationRole);
  const customerContext = resolveCustomerContext(user);
  const customerContextReady = customerContext.state === 'ready';
  const [activeTab, setActiveTab] = useState<TabKey>('events');
  const projectID = projectIDFromLocation();
  const targetOrgID = platform ? effectiveCustomerOrgID(user) : undefined;
  const [projectMembershipRole, setProjectMembershipRole] = useState('');
  const [projectMembershipLoaded, setProjectMembershipLoaded] = useState(!projectID);
  const hasOrganizationNotificationScope = organizationAdmin || billing;
  const canReadProjectNotifications = Boolean(canonicalProjectMembershipRole(projectMembershipRole));
  const canWriteProjectNotifications = canonicalProjectMembershipRole(projectMembershipRole) === 'project_admin';
  const canReadNotifications = hasOrganizationNotificationScope || (Boolean(projectID) && projectMembershipLoaded && canReadProjectNotifications);
  const canWriteEvents = hasOrganizationNotificationScope || (Boolean(projectID) && canWriteProjectNotifications);
  const canManageWebhooks = organizationAdmin || (Boolean(projectID) && canWriteProjectNotifications);

  const [events, setEvents] = useState<NotificationEvent[]>([]);
  const [eventTotal, setEventTotal] = useState(0);
  const [eventPage, setEventPage] = useState(1);
  const [eventPageSize, setEventPageSize] = useState(20);
  const [eventLoading, setEventLoading] = useState(true);
  const [eventLoadError, setEventLoadError] = useState('');
  const [eventType, setEventType] = useState('');
  const [severity, setSeverity] = useState('');
  const [eventStatus, setEventStatus] = useState('');
  const [deliveryState, setDeliveryState] = useState('');
  const [eventActionID, setEventActionID] = useState<number | null>(null);

  const [deliveries, setDeliveries] = useState<NotificationDelivery[]>([]);
  const [deliveryTotal, setDeliveryTotal] = useState(0);
  const [deliveryPage, setDeliveryPage] = useState(1);
  const [deliveryPageSize, setDeliveryPageSize] = useState(20);
  const [deliveryLoading, setDeliveryLoading] = useState(true);
  const [deliveryLoadError, setDeliveryLoadError] = useState('');
  const [deliveryStatus, setDeliveryStatus] = useState('');
  const [deliveryEventID, setDeliveryEventID] = useState<number | undefined>();

  const [webhooks, setWebhooks] = useState<NotificationWebhook[]>([]);
  const [webhookTotal, setWebhookTotal] = useState(0);
  const [webhookPage, setWebhookPage] = useState(1);
  const [webhookPageSize, setWebhookPageSize] = useState(20);
  const [webhookLoading, setWebhookLoading] = useState(true);
  const [webhookLoadError, setWebhookLoadError] = useState('');
  const [webhookSheet, setWebhookSheet] = useState<{ mode: 'create' | 'edit'; record?: NotificationWebhook } | null>(null);
  const [webhookFormApi, setWebhookFormApi] = useState<any>(null);
  const eventRequestVersion = useRef(0);
  const deliveryRequestVersion = useRef(0);
  const webhookRequestVersion = useRef(0);

  const loadEvents = useCallback(async (page = eventPage, pageSize = eventPageSize) => {
    if (!customerContextReady || !canReadNotifications) { setEventLoading(false); return; }
    const version = ++eventRequestVersion.current;
    setEventLoading(true);
    setEventLoadError('');
    setEvents([]);
    setEventTotal(0);
    try {
      const data = await api.getNotificationEvents(page, pageSize, {
        event_type: eventType,
        severity,
        status: eventStatus,
        delivery_state: deliveryState,
        ...(targetOrgID ? { org_id: targetOrgID } : {}),
        project_id: projectID,
      });
      if (version !== eventRequestVersion.current) return;
      setEvents((data?.data || []) as NotificationEvent[]);
      setEventTotal(Number(data?.total || 0));
    } catch {
      if (version !== eventRequestVersion.current) return;
      setEventLoadError('无法加载当前范围的通知事件。');
      Toast.error('通知事件加载失败');
    } finally {
      if (version === eventRequestVersion.current) setEventLoading(false);
    }
  }, [canReadNotifications, customerContextReady, deliveryState, eventPage, eventPageSize, eventStatus, eventType, projectID, severity, targetOrgID]);

  const loadDeliveries = useCallback(async (page = deliveryPage, pageSize = deliveryPageSize) => {
    if (!customerContextReady || !canReadNotifications) { setDeliveryLoading(false); return; }
    const version = ++deliveryRequestVersion.current;
    setDeliveryLoading(true);
    setDeliveryLoadError('');
    setDeliveries([]);
    setDeliveryTotal(0);
    try {
      const data = await api.getNotificationDeliveries(page, pageSize, {
        status: deliveryStatus,
        event_id: deliveryEventID,
        ...(targetOrgID ? { org_id: targetOrgID } : {}),
        project_id: projectID,
      });
      if (version !== deliveryRequestVersion.current) return;
      setDeliveries((data?.data || []) as NotificationDelivery[]);
      setDeliveryTotal(Number(data?.total || 0));
    } catch {
      if (version !== deliveryRequestVersion.current) return;
      setDeliveryLoadError('无法加载当前范围的投递记录。');
      Toast.error('投递记录加载失败');
    } finally {
      if (version === deliveryRequestVersion.current) setDeliveryLoading(false);
    }
  }, [canReadNotifications, customerContextReady, deliveryEventID, deliveryPage, deliveryPageSize, deliveryStatus, projectID, targetOrgID]);

  const loadWebhooks = useCallback(async (page = webhookPage, pageSize = webhookPageSize) => {
    if (!customerContextReady || !canReadNotifications || billing) { setWebhookLoading(false); return; }
    const version = ++webhookRequestVersion.current;
    setWebhookLoading(true);
    setWebhookLoadError('');
    setWebhooks([]);
    setWebhookTotal(0);
    try {
      const data = await api.getNotificationWebhooks(page, pageSize, {
        ...(targetOrgID ? { org_id: targetOrgID } : {}),
        project_id: projectID,
      });
      if (version !== webhookRequestVersion.current) return;
      setWebhooks((data?.data || []) as NotificationWebhook[]);
      setWebhookTotal(Number(data?.total || 0));
    } catch {
      if (version !== webhookRequestVersion.current) return;
      setWebhookLoadError('无法加载当前范围的 Webhook 路由。');
      Toast.error('Webhook 路由加载失败');
    } finally {
      if (version === webhookRequestVersion.current) setWebhookLoading(false);
    }
  }, [billing, canReadNotifications, customerContextReady, projectID, targetOrgID, webhookPage, webhookPageSize]);

  const invalidateEvents = () => {
    eventRequestVersion.current += 1;
    setEventLoading(true);
    setEvents([]);
    setEventTotal(0);
    setEventLoadError('');
  };

  const invalidateDeliveries = () => {
    deliveryRequestVersion.current += 1;
    setDeliveryLoading(true);
    setDeliveries([]);
    setDeliveryTotal(0);
    setDeliveryLoadError('');
  };

  useEffect(() => { loadEvents(); }, [loadEvents]);
  useEffect(() => { loadDeliveries(); }, [loadDeliveries]);
  useEffect(() => {
    if (!billing) loadWebhooks();
  }, [billing, loadWebhooks]);

  useEffect(() => {
    if (!customerContextReady) {
      setProjectMembershipRole('');
      setProjectMembershipLoaded(false);
      return;
    }
    if (!projectID) {
      setProjectMembershipRole('');
      setProjectMembershipLoaded(true);
      return;
    }
    setProjectMembershipLoaded(false);
    setProjectMembershipRole('');
    loadAllPages<{ id: number; current_user_membership_role?: string }>((page, size) => (
      api.getProjects(page, size, { status: 'active' })
    )).then(projects => {
      setProjectMembershipRole(projects.find(project => project.id === projectID)?.current_user_membership_role || '');
      setProjectMembershipLoaded(true);
    }).catch(() => {
      setProjectMembershipRole('');
      setProjectMembershipLoaded(true);
    });
  }, [customerContextReady, projectID]);

  useEffect(() => {
    if (!webhookSheet || !webhookFormApi) return;
    const record = webhookSheet.record;
    webhookFormApi.setValues(record ? {
      name: record.name,
      url: '',
      event_types: toEventTypeList(record.event_types) === '-' ? '' : toEventTypeList(record.event_types),
      enabled: (record.scope_verified ? record.enabled : record.scope_quarantine_previous_enabled) ? 'true' : 'false',
      project_id: record.project_id || projectID || undefined,
    } : { enabled: 'true', project_id: projectID });
  }, [projectID, webhookFormApi, webhookSheet]);

  const openWebhookSheet = (record?: NotificationWebhook) => {
    setWebhookSheet(record ? { mode: 'edit', record } : { mode: 'create' });
  };

  const saveWebhook = async (values: any) => {
    if (!customerContextReady) {
      Toast.error('无法确认唯一客户组织，未保存 Webhook');
      return;
    }
    const enteredProjectID = String(values.project_id || '').trim();
    const selectedProjectID = projectID || (enteredProjectID ? Number(enteredProjectID) : 0);
    if (enteredProjectID && (!Number.isSafeInteger(selectedProjectID) || selectedProjectID <= 0)) {
      Toast.error('项目 ID 必须是正整数');
      return;
    }
    const payload: Record<string, unknown> = {
      name: values.name,
      ...(targetOrgID ? { org_id: targetOrgID } : {}),
      project_id: selectedProjectID,
      event_types: parseEventTypes(values.event_types),
      enabled: values.enabled !== 'false',
    };
    if (webhookSheet?.mode !== 'edit' || String(values.url || '').trim()) {
      payload.url = String(values.url || '').trim();
    }
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

  const updateEventStatus = async (record: NotificationEvent, action: 'acknowledge' | 'resolve') => {
    if (!customerContextReady) {
      Toast.error('无法确认唯一客户组织，未更新通知事件');
      return;
    }
    setEventActionID(record.id);
    try {
      if (action === 'acknowledge') {
        if (targetOrgID) await api.acknowledgeNotificationEvent(record.id, projectID, targetOrgID);
        else await api.acknowledgeNotificationEvent(record.id, projectID);
        Toast.success('事件已确认');
      } else {
        if (targetOrgID) await api.resolveNotificationEvent(record.id, projectID, targetOrgID);
        else await api.resolveNotificationEvent(record.id, projectID);
        Toast.success('事件已解决');
      }
      await loadEvents();
    } catch (err: any) {
      Toast.error(`事件处理失败${err?.message ? `：${err.message}` : ''}`);
    } finally {
      setEventActionID(null);
    }
  };

  const showEventDeliveries = (record: NotificationEvent) => {
    invalidateDeliveries();
    setDeliveryEventID(record.id);
    setDeliveryPage(1);
    setActiveTab('deliveries');
  };

  const confirmEventResolution = (record: NotificationEvent) => {
    Modal.confirm({
      title: '确认解决事件',
      content: '解决后当前版本不能重新打开该事件；Webhook 投递记录不会被删除或重试。',
      okText: '确认解决',
      cancelText: '取消',
      okButtonProps: { 'aria-label': '确认解决' },
      cancelButtonProps: { 'aria-label': '取消' },
      onOk: () => updateEventStatus(record, 'resolve'),
    });
  };

  const verifyWebhookScope = (record: NotificationWebhook) => {
    if (!customerContextReady) {
      Toast.error('无法确认唯一客户组织，未确认 Webhook 范围');
      return;
    }
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
      okButtonProps: { 'aria-label': '确认范围' },
      cancelButtonProps: { 'aria-label': '取消' },
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
    { title: '处理状态', dataIndex: 'status', width: 120, render: (v: string) => <StatusTag status={v}>{notificationEventStatusLabel(v)}</StatusTag> },
    { title: '投递状态', dataIndex: 'delivery_state', width: 130, render: (v: string) => <StatusTag status={v}>{deliveryStateLabel(v)}</StatusTag> },
    { title: '时间', dataIndex: 'created_at', width: 180, render: (v: string) => formatDateTime(v) },
    {
      title: '操作',
      width: 230,
      fixed: 'right' as const,
      render: (_: unknown, row: NotificationEvent) => (
        <div style={{ display: 'flex', gap: 6 }}>
          <Button size="small" onClick={() => showEventDeliveries(row)}>查看投递</Button>
          {canWriteEvents && row.status === 'open' && <Button size="small" loading={eventActionID === row.id} onClick={() => updateEventStatus(row, 'acknowledge')}>确认</Button>}
          {canWriteEvents && row.status !== 'resolved' && <Button size="small" type="primary" loading={eventActionID === row.id} onClick={() => confirmEventResolution(row)}>解决</Button>}
        </div>
      ),
    },
  ];

  const deliveryColumns = [
    { title: 'ID', dataIndex: 'id', width: 72 },
    { title: '事件', dataIndex: 'event_type', width: 220, render: (v: string) => <Text>{eventTypeLabel(v)}</Text> },
    { title: billing ? '投递路由' : 'Webhook', dataIndex: 'webhook_name', width: 180, render: (v: string) => v || '-' },
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
    { title: '目标（路径已隐藏）', dataIndex: 'url', ellipsis: true },
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
          {canManageWebhooks && <Button size="small" icon={<IconSetting />} onClick={() => openWebhookSheet(row)}>编辑</Button>}
          {canManageWebhooks && !row.scope_verified && <Button size="small" type="primary" onClick={() => verifyWebhookScope(row)}>确认范围</Button>}
        </div>
      ),
    },
  ];

  if (!customerContextReady) {
    const blocker = customerContextBlockerCopy(customerContext.state as Exclude<typeof customerContext.state, 'ready'>);
    return (
      <div className="ur-page ur-page-wide">
        <PageHeader eyebrow={billing ? '成本与财务' : '监控与用量'} title={billing ? '财务提醒' : '通知中心'} description="通知数据和 Webhook 必须绑定当前部署唯一客户组织。" />
        <Surface><EmptyState title={blocker.title} description={blocker.description} minHeight={320} /></Surface>
      </div>
    );
  }

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow={billing ? '成本与财务' : '监控与用量'}
        title={billing ? '财务提醒' : '通知中心'}
        description={billing
          ? `查看并处理预算与计量质量提醒${projectID ? `；当前项目 #${projectID}` : ''}。投递结果仅供核对，Webhook 路由由组织或平台管理员维护。“未配置”表示事件已记录但没有匹配的启用路由。`
          : `查看并处理通知事件、核对投递结果和管理 Webhook 路由${projectID ? `；当前项目 #${projectID}` : ''}。“未配置”表示事件已记录但没有匹配的启用路由。`}
        actions={(
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            {platform && <Text type="tertiary">客户范围：{customerContext.orgName || `组织 #${targetOrgID}`}</Text>}
            <Button icon={<IconRefresh />} onClick={() => {
              loadEvents();
              loadDeliveries();
              if (!billing) loadWebhooks();
            }}>刷新</Button>
          </div>
        )}
      />

      <Surface>
        <Tabs activeKey={activeTab} onChange={(key) => setActiveTab(key as TabKey)}>
          <TabPane tab={billing ? '财务事件' : '事件'} itemKey="events">
            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 16 }}>
              <Select optionList={billing ? financialEventTypeOptions : eventTypeOptions} value={eventType || undefined} placeholder={billing ? '财务事件类型' : '事件类型'} style={{ width: 250 }} onChange={(v) => { invalidateEvents(); setEventType((v as string) || ''); setEventPage(1); }} />
              <Select optionList={severityOptions} value={severity || undefined} placeholder="级别" style={{ width: 160 }} onChange={(v) => { invalidateEvents(); setSeverity((v as string) || ''); setEventPage(1); }} />
              <Select optionList={eventStatusOptions} value={eventStatus || undefined} placeholder="处理状态" style={{ width: 160 }} onChange={(v) => { invalidateEvents(); setEventStatus((v as string) || ''); setEventPage(1); }} />
              <Select optionList={deliveryStateOptions} value={deliveryState || undefined} placeholder="投递状态" style={{ width: 190 }} onChange={(v) => { invalidateEvents(); setDeliveryState((v as string) || ''); setEventPage(1); }} />
            </div>
            <div className="ur-table-shell">
              <Table
                columns={eventColumns}
                dataSource={events}
                loading={eventLoading}
                rowKey="id"
                empty={eventLoading && events.length === 0 ? (
                  <EmptyState title="正在加载通知事件" description="正在读取当前通知范围，请稍候。" minHeight={220} />
                ) : eventLoadError ? (
                  <EmptyState
                    title="通知事件加载失败"
                    description={eventLoadError}
                    actions={<Button type="primary" onClick={() => loadEvents()}>重新加载通知事件</Button>}
                    minHeight={220}
                  />
                ) : <EmptyState title="暂无通知事件" minHeight={220} />}
                pagination={{
                  currentPage: eventPage,
                  pageSize: eventPageSize,
                  total: eventTotal,
                  onPageChange: (p: number) => { invalidateEvents(); setEventPage(p); },
                  onPageSizeChange: (s: number) => { invalidateEvents(); setEventPageSize(s); setEventPage(1); },
                }}
              />
            </div>
          </TabPane>
          <TabPane tab={billing ? '投递记录（只读）' : '投递'} itemKey="deliveries">
            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 16 }}>
              <Select optionList={deliveryStatusOptions} value={deliveryStatus || undefined} placeholder="投递状态" style={{ width: 180 }} onChange={(v) => { invalidateDeliveries(); setDeliveryStatus((v as string) || ''); setDeliveryPage(1); }} />
              {deliveryEventID && <Text>仅显示事件 #{deliveryEventID} <Button size="small" onClick={() => { invalidateDeliveries(); setDeliveryEventID(undefined); setDeliveryPage(1); }}>显示全部</Button></Text>}
            </div>
            <div className="ur-table-shell">
              <Table
                columns={deliveryColumns}
                dataSource={deliveries}
                loading={deliveryLoading}
                rowKey="id"
                empty={deliveryLoading && deliveries.length === 0 ? (
                  <EmptyState title="正在加载投递记录" description="正在读取当前通知范围的投递事实，请稍候。" minHeight={220} />
                ) : deliveryLoadError ? (
                  <EmptyState
                    title="投递记录加载失败"
                    description={deliveryLoadError}
                    actions={<Button type="primary" onClick={() => loadDeliveries()}>重新加载投递记录</Button>}
                    minHeight={220}
                  />
                ) : <EmptyState title="暂无投递记录" minHeight={220} />}
                pagination={{
                  currentPage: deliveryPage,
                  pageSize: deliveryPageSize,
                  total: deliveryTotal,
                  onPageChange: (p: number) => { invalidateDeliveries(); setDeliveryPage(p); },
                  onPageSizeChange: (s: number) => { invalidateDeliveries(); setDeliveryPageSize(s); setDeliveryPage(1); },
                }}
              />
            </div>
          </TabPane>
          {!billing && (
            <TabPane tab="Webhook" itemKey="webhooks">
              <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 16 }}>
                {canManageWebhooks && <Button icon={<IconPlus />} type="primary" onClick={() => openWebhookSheet()}>新增 Webhook</Button>}
              </div>
              <div className="ur-table-shell">
                <Table
                  columns={webhookColumns}
                  dataSource={webhooks}
                  loading={webhookLoading}
                  rowKey="id"
                  empty={webhookLoading && webhooks.length === 0 ? (
                    <EmptyState title="正在加载 Webhook 路由" description="正在读取当前范围的 Webhook 配置，请稍候。" minHeight={220} />
                  ) : webhookLoadError ? (
                    <EmptyState
                      title="Webhook 路由加载失败"
                      description={webhookLoadError}
                      actions={<Button type="primary" onClick={() => loadWebhooks()}>重新加载 Webhook 路由</Button>}
                      minHeight={220}
                    />
                  ) : <EmptyState title="暂无 Webhook 路由" minHeight={220} />}
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
          )}
        </Tabs>
      </Surface>

      {!billing && canManageWebhooks && (
        <SideSheet
          title={webhookSheet?.mode === 'edit' ? '编辑 Webhook' : '新增 Webhook'}
          visible={!!webhookSheet}
          width={520}
          onCancel={() => setWebhookSheet(null)}
        >
          <Form getFormApi={(formApi: any) => setWebhookFormApi(formApi)} onSubmit={saveWebhook}>
            <Form.Input field="name" label="名称" rules={[{ required: true, message: '必填' }]} />
            <Form.Input
              field="url"
              label={webhookSheet?.mode === 'edit' ? '新 URL（留空保持现有目标）' : 'URL'}
              rules={webhookSheet?.mode === 'edit' ? [] : [{ required: true, message: '必填' }]}
              placeholder={webhookSheet?.mode === 'edit' ? `当前：${webhookSheet.record?.url || '-'}` : 'https://ops.example.com/webhook'}
              extraText="允许客户内网地址；本机、链路本地和云元数据地址会被拒绝。"
            />
            <Form.Input field="project_id" label="项目 ID" placeholder="留空为组织级路由" disabled={webhookSheet?.mode === 'edit' || !!projectID} />
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
      )}
    </div>
  );
}
