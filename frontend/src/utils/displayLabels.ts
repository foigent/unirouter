export interface SelectOption {
  label: string;
  value: string;
}

export const roleLabels: Record<string, string> = {
  admin: '系统管理员（兼容）',
  platform_admin: '平台管理员',
  org_admin: '组织管理员',
  billing_admin: '账务管理员',
  member: '普通成员',
  project_admin: '项目管理员',
  project_user: '项目使用者（可调用）',
  project_viewer: '项目只读',
  project_developer: '项目使用者（兼容）',
  developer: '项目使用者（兼容）',
  viewer: '项目只读（兼容）',
  org_viewer: '组织只读',
};

export const userManagementOrganizationRoleOptions: SelectOption[] = [
  { label: '平台管理员', value: 'platform_admin' },
  { label: '组织管理员', value: 'org_admin' },
  { label: '账务管理员', value: 'billing_admin' },
  { label: '组织只读', value: 'org_viewer' },
  { label: '普通成员', value: 'member' },
];

// Retained for rolling frontend consumers. New account forms must use the
// organization-only options above; project roles are managed per project.
export const userManagementRoleOptions = userManagementOrganizationRoleOptions;

export function roleLabel(value?: string | null, fallback = '-') {
  return roleLabels[value || ''] || value || fallback;
}

export function organizationRoleLabel(value?: string | null, fallback = '-') {
  return roleLabel(value, fallback);
}

export function roleTagColor(value?: string | null) {
  if (value === 'admin' || value === 'platform_admin') return 'red';
  if (value === 'org_admin') return 'blue';
  if (value === 'billing_admin') return 'orange';
  if (value === 'project_admin') return 'purple';
  if (value === 'project_user' || value === 'project_developer' || value === 'developer') return 'green';
  return 'grey';
}

export const usageAnalyticsGroupOptions: SelectOption[] = [
  { label: '总览（不分组）', value: 'overall' },
  { label: '项目', value: 'project' },
  { label: '访问凭证', value: 'key' },
  { label: '凭证所有者', value: 'owner' },
  { label: '凭证类型', value: 'key_type' },
  { label: '模型', value: 'model' },
  { label: '模型别名', value: 'alias' },
  { label: '供应商', value: 'provider' },
  { label: '上游通道', value: 'channel' },
  { label: '请求状态', value: 'status' },
  { label: '计量来源', value: 'usage_source' },
];

export const usageScopeOptions: SelectOption[] = [
  { label: '当前权限范围', value: '' },
  { label: '我的用量', value: 'my' },
];

export const keyTypeOptions: SelectOption[] = [
  { label: '全部凭证类型', value: '' },
  { label: '个人 Key', value: 'personal' },
  { label: '共享 Key', value: 'shared' },
  { label: '服务 Key', value: 'service' },
  { label: '未知', value: 'unknown' },
];

export const requestStatusOptions: SelectOption[] = [
  { label: '全部状态', value: '' },
  { label: '成功', value: 'success' },
  { label: '失败', value: 'error' },
];

const usageSourceLabels: Record<string, string> = {
  upstream: '上游回传',
  api: '上游回传',
  counter_v2: '流式估算（v2）',
  counter_v3: '流式估算（v3）',
  stream_estimate: '流式计数',
  adapter_estimated: '适配器估算',
  estimated: '本地估算',
  no_route: '无可用通道',
  upstream_failed: '上游失败',
};

export const usageSourceOptions: SelectOption[] = [
  { label: '全部来源', value: '' },
  { label: '上游回传', value: 'upstream' },
  { label: '流式估算（v2）', value: 'counter_v2' },
  { label: '流式估算（v3）', value: 'counter_v3' },
  { label: '适配器估算', value: 'adapter_estimated' },
  { label: '本地估算', value: 'estimated' },
  { label: '无可用通道', value: 'no_route' },
  { label: '上游失败', value: 'upstream_failed' },
];

export function usageSourceLabel(value?: string) {
  return usageSourceLabels[value || ''] || value || '-';
}

const auditSourceLabels: Record<string, string> = {
  admin_console: '管理控制台',
  developer_console: '开发者工作台',
  gateway_api: '网关 API',
  system: '系统任务',
};

export const auditSourceOptions: SelectOption[] = [
  { label: '管理控制台', value: 'admin_console' },
  { label: '开发者工作台', value: 'developer_console' },
  { label: '网关 API', value: 'gateway_api' },
  { label: '系统任务', value: 'system' },
];

export function auditSourceLabel(value?: string) {
  return auditSourceLabels[value || ''] || value || '-';
}

const eventTypeLabels: Record<string, string> = {
  'project_budget.threshold': '项目预算阈值',
  'api_key.disabled': '访问凭证停用',
  'model.deprecated': '模型弃用',
  'model.retired': '模型下线',
  'provider.channel.failure': '上游通道故障',
  'license.warning': 'License 预警',
  'token_metering.estimated_ratio.high': '估算占比过高',
};

export const eventTypeOptions: SelectOption[] = [
  { label: '全部事件', value: '' },
  ...Object.entries(eventTypeLabels).map(([value, label]) => ({ label, value })),
];

export function eventTypeLabel(value?: string) {
  return eventTypeLabels[value || ''] || value || '-';
}

export function eventTypeListLabel(value: unknown) {
  let values: unknown[] = [];
  if (Array.isArray(value)) {
    values = value;
  } else if (typeof value === 'string') {
    try {
      const parsed = JSON.parse(value);
      values = Array.isArray(parsed) ? parsed : value.split(',');
    } catch {
      values = value.split(',');
    }
  }
  const labels = values.map(item => eventTypeLabel(String(item).trim())).filter(label => label && label !== '-');
  return labels.length ? labels.join(', ') : '-';
}

const severityLabels: Record<string, string> = {
  info: '信息',
  warning: '预警',
  critical: '严重',
};

export const severityOptions: SelectOption[] = [
  { label: '全部级别', value: '' },
  { label: '信息', value: 'info' },
  { label: '预警', value: 'warning' },
  { label: '严重', value: 'critical' },
];

export function severityLabel(value?: string) {
  return severityLabels[value || ''] || value || '-';
}

const eventStatusLabels: Record<string, string> = {
  open: '未关闭',
  acknowledged: '已确认',
  resolved: '已解决',
};

export function eventStatusLabel(value?: string) {
  return eventStatusLabels[value || ''] || value || '-';
}

const deliveryStateLabels: Record<string, string> = {
  not_configured: '未配置 Webhook',
  pending: '待投递',
  delivered: '已投递',
  failed: '投递失败',
};

export const deliveryStateOptions: SelectOption[] = [
  { label: '全部投递状态', value: '' },
  { label: '未配置', value: 'not_configured' },
  { label: '待投递', value: 'pending' },
  { label: '已投递', value: 'delivered' },
  { label: '投递失败', value: 'failed' },
];

export const deliveryStatusOptions: SelectOption[] = [
  { label: '全部状态', value: '' },
  { label: '待投递', value: 'pending' },
  { label: '已投递', value: 'delivered' },
  { label: '投递失败', value: 'failed' },
];

export function deliveryStateLabel(value?: string) {
  return deliveryStateLabels[value || ''] || value || '-';
}

const notificationResourceLabels: Record<string, string> = {
  api_key: '访问凭证',
  channel: '上游通道',
  project: '项目',
  model: '模型',
  license: 'License',
  wallet: '预算与成本中心',
};

export function notificationResourceLabel(resource?: string, resourceId?: string | number | null) {
  if (!resource) return '-';
  const label = notificationResourceLabels[resource] || resource;
  return resourceId !== undefined && resourceId !== null && String(resourceId) !== ''
    ? `${label} #${resourceId}`
    : label;
}

export function notificationTitleLabel(eventType?: string, title?: string) {
  if (eventType === 'api_key.disabled') return '访问凭证已停用';
  return title || '-';
}

export function notificationMessageLabel(eventType?: string, message?: string) {
  if (eventType === 'api_key.disabled') {
    const match = String(message || '').match(/^API key (.+) was disabled$/);
    if (match) return `访问凭证 ${match[1]} 已停用`;
  }
  return message || '-';
}

const internalStatusLabels: Record<string, string> = {
  operational: '正常',
  warning: '预警',
  degraded: '降级',
};

export function internalStatusLabel(value?: string) {
  return internalStatusLabels[value || ''] || value || '-';
}
