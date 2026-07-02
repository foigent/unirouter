import {
  IconBolt,
  IconCoinMoney,
  IconFile,
  IconGift,
  IconGridRectangle,
  IconKey,
  IconBell,
  IconPriceTag,
  IconSearch,
  IconServer,
  IconSetting,
  IconShield,
  IconUser,
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
    icon: <IconGridRectangle />,
    items: [
      { itemKey: '/', text: '仪表盘', icon: <IconGridRectangle /> },
    ],
  },
  {
    itemKey: 'g-traffic',
    text: 'AI 流量',
    icon: <IconServer />,
    items: [
      { itemKey: '/channels', text: '渠道管理', icon: <IconServer /> },
      { itemKey: '/channel-keys', text: '上游密钥', icon: <IconKey /> },
      { itemKey: '/keys', text: 'API 密钥', icon: <IconKey /> },
      { itemKey: '/model-governance', text: '模型治理', icon: <IconServer /> },
      { itemKey: '/logs', text: '调用日志', icon: <IconFile /> },
      { itemKey: '/notifications', text: '通知中心', icon: <IconBell /> },
      { itemKey: '/test', text: '渠道测试', icon: <IconBolt /> },
    ],
  },
  {
    itemKey: 'g-billing',
    text: '账单',
    icon: <IconCoinMoney />,
    items: [
      { itemKey: '/wallet', text: '钱包管理', icon: <IconCoinMoney /> },
      { itemKey: '/model-prices', text: '模型价格', icon: <IconPriceTag /> },
      { itemKey: '/org-model-prices', text: '组织倍率', icon: <IconPriceTag /> },
      { itemKey: '/reconciliation', text: 'Token对账', icon: <IconSearch /> },
      { itemKey: '/grants', text: '赠送管理', icon: <IconGift /> },
    ],
  },
  {
    itemKey: 'g-access',
    text: '权限',
    icon: <IconUser />,
    items: [
      { itemKey: '/users', text: '用户管理', icon: <IconUser /> },
      { itemKey: '/audit-logs', text: '审计日志', icon: <IconSearch /> },
    ],
  },
  {
    itemKey: 'g-system',
    text: '系统',
    icon: <IconSetting />,
    items: [
      { itemKey: '/license', text: 'License', icon: <IconShield /> },
      { itemKey: '/settings', text: '系统设置', icon: <IconSetting /> },
      { itemKey: '/docs', text: 'API 文档', icon: <IconFile /> },
    ],
  },
];

const billingNavItems = [
  {
    itemKey: 'g-overview',
    text: '概览',
    icon: <IconGridRectangle />,
    items: [
      { itemKey: '/', text: '仪表盘', icon: <IconGridRectangle /> },
      { itemKey: '/logs', text: '调用日志', icon: <IconFile /> },
    ],
  },
  {
    itemKey: 'g-billing',
    text: '账单',
    icon: <IconCoinMoney />,
    items: [
      { itemKey: '/wallet', text: '钱包管理', icon: <IconCoinMoney /> },
      { itemKey: '/reconciliation', text: 'Token对账', icon: <IconSearch /> },
      { itemKey: '/notifications', text: '通知中心', icon: <IconBell /> },
      { itemKey: '/grants', text: '赠送管理', icon: <IconGift /> },
      { itemKey: '/audit-logs', text: '审计日志', icon: <IconSearch /> },
    ],
  },
  {
    itemKey: 'g-system',
    text: '系统',
    icon: <IconSetting />,
    items: [
      { itemKey: '/docs', text: 'API 文档', icon: <IconFile /> },
    ],
  },
];

const developerNavItems = [
  {
    itemKey: 'g-developer',
    text: 'Developer Console',
    icon: <IconGridRectangle />,
    items: [
      { itemKey: '/developer', text: '项目总览', icon: <IconGridRectangle /> },
      { itemKey: '/docs', text: 'API 文档', icon: <IconFile /> },
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
    return ['/', '/logs', '/wallet', '/reconciliation', '/notifications', '/grants', '/audit-logs'].includes(route);
  }

  if (isProjectConsoleRole(role)) {
    return route === '/developer';
  }

  return false;
}
