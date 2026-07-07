import {
  IconActivity,
  IconBarChartVStroked,
  IconBookOpenStroked,
  IconChecklistStroked,
  IconCoinMoney,
  IconCreditCard,
  IconFile,
  IconFlowChartStroked,
  IconGift,
  IconHomeStroked,
  IconKeyStroked,
  IconBell,
  IconLayers,
  IconLockStroked,
  IconMonitorStroked,
  IconPulse,
  IconPriceTag,
  IconRoute,
  IconSendStroked,
  IconSetting,
  IconSettingStroked,
  IconShield,
  IconShieldStroked,
  IconTerminal,
  IconTestScore,
  IconUserGroup,
} from '@douyinfe/semi-icons';

export function normalizeRole(role?: string | null) {
  const normalized = (role || '').trim();
  if (normalized === 'admin') return 'platform_admin';
  return normalized;
}

export function isPlatformAdminRole(role?: string | null) {
  return ['admin', 'platform_admin'].includes(normalizeRole(role));
}

export function isOrgAdminRole(role?: string | null) {
  return ['admin', 'platform_admin', 'org_admin'].includes(normalizeRole(role));
}

export function isBillingRole(role?: string | null) {
  return normalizeRole(role) === 'billing_admin';
}

export function isOrgViewerRole(role?: string | null) {
  return normalizeRole(role) === 'org_viewer';
}

export function isProjectConsoleRole(role?: string | null) {
  return ['project_admin', 'developer', 'viewer'].includes(normalizeRole(role));
}

export function isProjectAdminRole(role?: string | null) {
  return normalizeRole(role) === 'project_admin';
}

export function canReadRuntimeStatus(role?: string | null) {
  return isOrgAdminRole(role) || isBillingRole(role);
}

export function canCreateProjectKey(role?: string | null) {
  return ['admin', 'platform_admin', 'org_admin', 'project_admin', 'developer'].includes(normalizeRole(role));
}

export function canDisableProjectKey(role?: string | null) {
  return ['admin', 'platform_admin', 'org_admin', 'project_admin'].includes(normalizeRole(role));
}

const fullAdminNavItems = [
  {
    itemKey: 'g-overview',
    text: '概览',
    icon: <IconHomeStroked />,
    items: [
      { itemKey: '/', text: '仪表盘', icon: <IconMonitorStroked /> },
    ],
  },
  {
    itemKey: 'g-traffic',
    text: '流量治理',
    icon: <IconSendStroked />,
    items: [
      { itemKey: '/channels', text: '上游通道', icon: <IconRoute /> },
      { itemKey: '/channel-keys', text: '上游凭证', icon: <IconLockStroked /> },
      { itemKey: '/keys', text: '访问凭证', icon: <IconKeyStroked /> },
      { itemKey: '/model-governance', text: '模型治理', icon: <IconLayers /> },
      { itemKey: '/logs', text: '请求日志', icon: <IconFile /> },
      { itemKey: '/usage-analytics', text: '用量分析', icon: <IconBarChartVStroked /> },
      { itemKey: '/notifications', text: '通知中心', icon: <IconBell /> },
      { itemKey: '/test', text: '连通性检测', icon: <IconTestScore /> },
    ],
  },
  {
    itemKey: 'g-billing',
    text: '财务治理',
    icon: <IconCoinMoney />,
    items: [
      { itemKey: '/wallet', text: '资金账户', icon: <IconCreditCard /> },
      { itemKey: '/model-prices', text: '模型定价', icon: <IconPriceTag /> },
      { itemKey: '/org-model-prices', text: '价格策略', icon: <IconActivity /> },
      { itemKey: '/reconciliation', text: '用量对账', icon: <IconFlowChartStroked /> },
      { itemKey: '/grants', text: '额度调整', icon: <IconGift /> },
    ],
  },
  {
    itemKey: 'g-access',
    text: '访问控制',
    icon: <IconShieldStroked />,
    items: [
      { itemKey: '/users', text: '用户管理', icon: <IconUserGroup /> },
      { itemKey: '/audit-logs', text: '审计日志', icon: <IconChecklistStroked /> },
    ],
  },
  {
    itemKey: 'g-system',
    text: '系统治理',
    icon: <IconSettingStroked />,
    items: [
      { itemKey: '/license', text: 'License', icon: <IconShield /> },
      { itemKey: '/status', text: '内部状态', icon: <IconPulse /> },
      { itemKey: '/settings', text: '系统设置', icon: <IconSetting /> },
      { itemKey: '/docs', text: 'API 文档', icon: <IconBookOpenStroked /> },
    ],
  },
];

function withoutRoutes(items: typeof fullAdminNavItems, routes: string[]) {
  const blocked = new Set(routes);
  return items
    .map(item => ({
      ...item,
      items: item.items?.filter(child => !blocked.has(child.itemKey)),
    }))
    .filter(item => (item.items?.length || 0) > 0);
}

const orgAdminNavItems = withoutRoutes(fullAdminNavItems, [
  '/channels',
  '/channel-keys',
  '/test',
  '/wallet',
  '/reconciliation',
  '/grants',
  '/license',
]);

const billingNavItems = [
  {
    itemKey: 'g-overview',
    text: '概览',
    icon: <IconHomeStroked />,
    items: [
      { itemKey: '/', text: '仪表盘', icon: <IconMonitorStroked /> },
      { itemKey: '/logs', text: '请求日志', icon: <IconFile /> },
    ],
  },
  {
    itemKey: 'g-billing',
    text: '财务治理',
    icon: <IconCoinMoney />,
    items: [
      { itemKey: '/wallet', text: '资金账户', icon: <IconCreditCard /> },
      { itemKey: '/reconciliation', text: '用量对账', icon: <IconFlowChartStroked /> },
      { itemKey: '/usage-analytics', text: '用量分析', icon: <IconBarChartVStroked /> },
      { itemKey: '/notifications', text: '通知中心', icon: <IconBell /> },
      { itemKey: '/grants', text: '额度调整', icon: <IconGift /> },
      { itemKey: '/audit-logs', text: '审计日志', icon: <IconChecklistStroked /> },
    ],
  },
  {
    itemKey: 'g-system',
    text: '系统治理',
    icon: <IconSetting />,
    items: [
      { itemKey: '/status', text: '内部状态', icon: <IconPulse /> },
      { itemKey: '/docs', text: 'API 文档', icon: <IconBookOpenStroked /> },
    ],
  },
];

const developerNavItems = [
  {
    itemKey: 'g-developer',
    text: '开发者工作台',
    icon: <IconTerminal />,
    items: [
      { itemKey: '/developer', text: '项目概览', icon: <IconMonitorStroked /> },
      { itemKey: '/docs', text: 'API 文档', icon: <IconBookOpenStroked /> },
    ],
  },
];

const projectAdminNavItems = [
  {
    itemKey: 'g-developer',
    text: '项目工作台',
    icon: <IconTerminal />,
    items: [
      { itemKey: '/developer', text: '项目概览', icon: <IconMonitorStroked /> },
      { itemKey: '/notifications', text: '通知中心', icon: <IconBell /> },
      { itemKey: '/status', text: '内部状态', icon: <IconPulse /> },
      { itemKey: '/docs', text: 'API 文档', icon: <IconBookOpenStroked /> },
    ],
  },
];

const accessStateNavItems = [
  {
    itemKey: 'g-access-state',
    text: '访问状态',
    icon: <IconShieldStroked />,
    items: [
      { itemKey: '/access', text: '访问状态', icon: <IconShieldStroked /> },
      { itemKey: '/docs', text: 'API 文档', icon: <IconBookOpenStroked /> },
    ],
  },
];

export function getNavItemsForRole(role?: string | null) {
  if (isProjectAdminRole(role)) return projectAdminNavItems;
  if (isProjectConsoleRole(role)) return developerNavItems;
  if (isBillingRole(role)) return billingNavItems;
  if (isPlatformAdminRole(role)) return fullAdminNavItems;
  if (isOrgAdminRole(role)) return orgAdminNavItems;
  return accessStateNavItems;
}

export function getDefaultOpenKeysForRole(role?: string | null) {
  if (isProjectConsoleRole(role)) return ['g-developer'];
  if (isBillingRole(role)) return ['g-overview', 'g-billing', 'g-system'];
  if (isPlatformAdminRole(role) || isOrgAdminRole(role)) return ['g-overview', 'g-traffic', 'g-billing', 'g-access', 'g-system'];
  return ['g-access-state'];
}

export function getDefaultPathForRole(role?: string | null) {
  if (isProjectConsoleRole(role)) return '/developer';
  if (isOrgAdminRole(role) || isBillingRole(role)) return '/';
  return '/access';
}

function firstSegmentPath(path: string) {
  const pathname = (path || '/').split('?')[0].split('#')[0];
  if (pathname === '/') return '/';
  const first = pathname.split('/').filter(Boolean)[0];
  return first ? `/${first}` : '/';
}

export function isRouteAllowedForRole(path: string, role?: string | null) {
  const route = firstSegmentPath(path);
  if (route === '/login') return true;
  if (route === '/access') return true;
  if (route === '/docs') return true;
  if (route === '/license') return isPlatformAdminRole(role);

  if (isPlatformAdminRole(role)) return true;

  if (isOrgAdminRole(role)) {
    return [
      '/',
      '/keys',
      '/logs',
      '/usage-analytics',
      '/notifications',
      '/users',
      '/audit-logs',
      '/status',
      '/settings',
      '/model-governance',
      '/model-prices',
      '/org-model-prices',
    ].includes(route);
  }

  if (isBillingRole(role)) {
    return ['/', '/logs', '/wallet', '/reconciliation', '/usage-analytics', '/notifications', '/grants', '/audit-logs', '/status'].includes(route);
  }

  if (isProjectConsoleRole(role)) {
    if (isProjectAdminRole(role)) return ['/developer', '/notifications', '/status'].includes(route);
    return route === '/developer';
  }

  return false;
}
