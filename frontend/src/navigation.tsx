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

export const ROLE_LABELS: Record<string, string> = {
  admin: '系统管理员',
  platform_admin: '平台管理员',
  org_admin: '组织管理员',
  billing_admin: '账单管理员',
  project_admin: '项目管理员',
  developer: '开发者',
  viewer: '只读成员',
  org_viewer: '组织只读',
};

export function normalizeRole(role?: string | null) {
  return (role || '').trim() || 'viewer';
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

export function isProjectConsoleRole(role?: string | null) {
  return ['project_admin', 'developer', 'viewer'].includes(normalizeRole(role));
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
    text: 'AI 流量',
    icon: <IconSendStroked />,
    items: [
      { itemKey: '/channels', text: '渠道管理', icon: <IconRoute /> },
      { itemKey: '/channel-keys', text: '上游密钥', icon: <IconLockStroked /> },
      { itemKey: '/keys', text: 'API 密钥', icon: <IconKeyStroked /> },
      { itemKey: '/model-governance', text: '模型治理', icon: <IconLayers /> },
      { itemKey: '/logs', text: '调用日志', icon: <IconFile /> },
      { itemKey: '/usage-analytics', text: '用量分析', icon: <IconBarChartVStroked /> },
      { itemKey: '/notifications', text: '通知中心', icon: <IconBell /> },
      { itemKey: '/test', text: '渠道测试', icon: <IconTestScore /> },
    ],
  },
  {
    itemKey: 'g-billing',
    text: '账单',
    icon: <IconCoinMoney />,
    items: [
      { itemKey: '/wallet', text: '钱包管理', icon: <IconCreditCard /> },
      { itemKey: '/model-prices', text: '模型价格', icon: <IconPriceTag /> },
      { itemKey: '/org-model-prices', text: '组织倍率', icon: <IconActivity /> },
      { itemKey: '/reconciliation', text: 'Token对账', icon: <IconFlowChartStroked /> },
      { itemKey: '/grants', text: '赠送管理', icon: <IconGift /> },
    ],
  },
  {
    itemKey: 'g-access',
    text: '权限',
    icon: <IconShieldStroked />,
    items: [
      { itemKey: '/users', text: '用户管理', icon: <IconUserGroup /> },
      { itemKey: '/audit-logs', text: '审计日志', icon: <IconChecklistStroked /> },
    ],
  },
  {
    itemKey: 'g-system',
    text: '系统',
    icon: <IconSettingStroked />,
    items: [
      { itemKey: '/license', text: 'License', icon: <IconShield /> },
      { itemKey: '/status', text: '内部状态', icon: <IconPulse /> },
      { itemKey: '/settings', text: '系统设置', icon: <IconSetting /> },
      { itemKey: '/docs', text: 'API 文档', icon: <IconBookOpenStroked /> },
    ],
  },
];

const billingNavItems = [
  {
    itemKey: 'g-overview',
    text: '概览',
    icon: <IconHomeStroked />,
    items: [
      { itemKey: '/', text: '仪表盘', icon: <IconMonitorStroked /> },
      { itemKey: '/logs', text: '调用日志', icon: <IconFile /> },
    ],
  },
  {
    itemKey: 'g-billing',
    text: '账单',
    icon: <IconCoinMoney />,
    items: [
      { itemKey: '/wallet', text: '钱包管理', icon: <IconCreditCard /> },
      { itemKey: '/reconciliation', text: 'Token对账', icon: <IconFlowChartStroked /> },
      { itemKey: '/usage-analytics', text: '用量分析', icon: <IconBarChartVStroked /> },
      { itemKey: '/notifications', text: '通知中心', icon: <IconBell /> },
      { itemKey: '/grants', text: '赠送管理', icon: <IconGift /> },
      { itemKey: '/audit-logs', text: '审计日志', icon: <IconChecklistStroked /> },
    ],
  },
  {
    itemKey: 'g-system',
    text: '系统',
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
    text: 'Developer Console',
    icon: <IconTerminal />,
    items: [
      { itemKey: '/developer', text: '项目总览', icon: <IconMonitorStroked /> },
      { itemKey: '/docs', text: 'API 文档', icon: <IconBookOpenStroked /> },
    ],
  },
];

export function getNavItemsForRole(role?: string | null) {
  if (isProjectConsoleRole(role)) return developerNavItems;
  if (isBillingRole(role)) return billingNavItems;
  if (isOrgAdminRole(role)) {
    if (isPlatformAdminRole(role)) return fullAdminNavItems;
    return fullAdminNavItems.map(item => item.itemKey === 'g-system'
      ? { ...item, items: item.items?.filter(child => child.itemKey !== '/license') }
      : item);
  }
  return developerNavItems;
}

export function getDefaultOpenKeysForRole(role?: string | null) {
  if (isProjectConsoleRole(role)) return ['g-developer'];
  if (isBillingRole(role)) return ['g-overview', 'g-billing', 'g-system'];
  return ['g-overview', 'g-traffic', 'g-billing', 'g-access', 'g-system'];
}

export function getDefaultPathForRole(role?: string | null) {
  if (isProjectConsoleRole(role)) return '/developer';
  if (isOrgAdminRole(role) || isBillingRole(role)) return '/';
  return '/docs';
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
  if (route === '/docs') return true;
  if (route === '/license') return isPlatformAdminRole(role);

  if (isOrgAdminRole(role)) return true;

  if (isBillingRole(role)) {
    return ['/', '/logs', '/wallet', '/reconciliation', '/usage-analytics', '/notifications', '/grants', '/audit-logs', '/status'].includes(route);
  }

  if (isProjectConsoleRole(role)) {
    return route === '/developer';
  }

  return false;
}
