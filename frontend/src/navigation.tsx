import {
  IconBarChartVStroked,
  IconBookOpenStroked,
  IconChecklistStroked,
  IconConnectionPoint1,
  IconCoinMoney,
  IconCreditCard,
  IconFile,
  IconFlowChartStroked,
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
  IconSettingStroked,
  IconShield,
  IconShieldStroked,
  IconTerminal,
  IconTestScore,
  IconUserGroup,
} from '@douyinfe/semi-icons';
import type { ReactNode } from 'react';

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
  return isOrgAdminRole(role);
}

export function canCreateProjectKey(role?: string | null) {
  return ['admin', 'platform_admin', 'org_admin', 'project_admin', 'developer'].includes(normalizeRole(role));
}

export function canDisableProjectKey(role?: string | null) {
  return ['admin', 'platform_admin', 'org_admin', 'project_admin', 'developer'].includes(normalizeRole(role));
}

export type LicenseFeature =
  | 'gateway'
  | 'agent_capabilities'
  | 'oidc'
  | 'guardrails'
  | 'notifications'
  | 'model_governance'
  | 'data_exchange';

export interface LicenseAccess {
  status: 'missing' | 'active' | 'expiring' | 'expired' | 'invalid' | 'not_yet_valid' | 'deployment_mismatch' | string;
  enforceable: boolean;
  features: Record<LicenseFeature, boolean>;
}

type NavChild = {
  itemKey: string;
  text: string;
  icon: ReactNode;
  licenseFeature?: LicenseFeature;
  licenseLocked?: boolean;
  licenseReason?: string;
  disabled?: boolean;
};
type NavItem = NavChild & { items?: NavChild[] };

// One canonical task taxonomy is filtered by role. A route never moves to a
// different section just because another role is looking at it.
const taskNavItems: NavItem[] = [
  { itemKey: '/', text: '平台总览', icon: <IconHomeStroked /> },
  {
    itemKey: 'g-model-access',
    text: '模型接入',
    icon: <IconSendStroked />,
    items: [
      { itemKey: '/channels', text: '上游通道', icon: <IconRoute /> },
      { itemKey: '/channel-keys', text: '供应商凭证', icon: <IconLockStroked /> },
      { itemKey: '/model-governance', text: '模型治理', icon: <IconLayers />, licenseFeature: 'model_governance' },
      { itemKey: '/test', text: '连通性检测', icon: <IconTestScore />, licenseFeature: 'gateway' },
    ],
  },
  {
    itemKey: 'g-access-security',
    text: '访问与安全',
    icon: <IconShieldStroked />,
    items: [
      { itemKey: '/keys', text: '访问凭证', icon: <IconKeyStroked /> },
      { itemKey: '/organization-members', text: '组织与成员', icon: <IconUserGroup /> },
      { itemKey: '/agent-trust', text: '模型调用授权', icon: <IconShieldStroked /> },
      { itemKey: '/limit-policies', text: '共享限额', icon: <IconPulse /> },
      { itemKey: '/guardrail-policies', text: '内容安全', icon: <IconShieldStroked />, licenseFeature: 'guardrails' },
      { itemKey: '/audit-logs', text: '审计日志', icon: <IconChecklistStroked /> },
    ],
  },
  {
    itemKey: 'g-observability',
    text: '监控与用量',
    icon: <IconMonitorStroked />,
    items: [
      { itemKey: '/logs', text: '请求日志', icon: <IconFile /> },
      { itemKey: '/media-jobs', text: '异步模型任务', icon: <IconFile />, licenseFeature: 'gateway' },
      { itemKey: '/usage-analytics', text: '用量分析', icon: <IconBarChartVStroked /> },
      { itemKey: '/notifications', text: '通知中心', icon: <IconBell />, licenseFeature: 'notifications' },
      { itemKey: '/status', text: '运行状态', icon: <IconPulse /> },
    ],
  },
  {
    itemKey: 'g-billing',
    text: '成本与财务',
    icon: <IconCoinMoney />,
    items: [
      { itemKey: '/wallet', text: '预算与成本中心', icon: <IconCreditCard /> },
      { itemKey: '/model-prices', text: '费率与计价', icon: <IconPriceTag /> },
      { itemKey: '/reconciliation', text: '分摊与对账', icon: <IconFlowChartStroked /> },
    ],
  },
  {
    itemKey: 'g-system-integration',
    text: '系统与集成',
    icon: <IconSettingStroked />,
    items: [
      { itemKey: '/license', text: '商业授权', icon: <IconShield /> },
      { itemKey: '/data-exchange', text: 'X-Insight 数据交换', icon: <IconConnectionPoint1 /> },
      { itemKey: '/docs', text: 'API 文档', icon: <IconBookOpenStroked /> },
    ],
  },
];

const projectOverviewItem: NavItem = { itemKey: '/developer', text: '项目工作台', icon: <IconTerminal /> };
const accessStateItem: NavItem = { itemKey: '/access', text: '访问状态', icon: <IconShieldStroked /> };

const platformRoutes = new Set(taskNavItems.flatMap(item => item.items?.map(child => child.itemKey) || [item.itemKey]));
const compatibilityRoutes = new Set([
  '/governance-simulation', '/risk-control', '/member-lifecycle', '/users', '/project-management',
  '/provenance-passport', '/org-model-prices', '/grants', '/agent-trust-evidence',
]);
const orgAdminRoutes = new Set([
  '/', '/model-governance', '/risk-control', '/keys', '/organization-members', '/project-management', '/member-lifecycle', '/agent-trust', '/agent-trust-evidence', '/limit-policies', '/guardrail-policies', '/users',
  '/logs', '/provenance-passport', '/usage-analytics', '/notifications', '/status', '/audit-logs',
  '/media-jobs',
  '/model-prices', '/docs',
]);
const billingRoutes = new Set([
  '/', '/logs', '/usage-analytics', '/notifications',
  '/wallet', '/model-prices', '/reconciliation', '/docs',
]);
const projectAdminRoutes = new Set(['/organization-members', '/media-jobs', '/docs']);
const developerRoutes = new Set(['/media-jobs', '/docs']);
const orgViewerRoutes = new Set(['/', '/docs']);

function filterTaskNavigation(allowedRoutes: Set<string>) {
  return taskNavItems.flatMap(item => {
    if (!item.items) return allowedRoutes.has(item.itemKey) ? [item] : [];
    const items = item.items.filter(child => allowedRoutes.has(child.itemKey));
    return items.length > 0 ? [{ ...item, items }] : [];
  });
}

function labelOverviewForRole(items: NavItem[], role?: string | null) {
  const overviewText = isBillingRole(role)
    ? '财务与用量总览'
    : isPlatformAdminRole(role)
      ? '平台总览'
      : '组织总览';
  return items.map(item => item.itemKey === '/' ? { ...item, text: overviewText } : item);
}

function applyLicenseState(items: NavItem[], licenseAccess?: LicenseAccess | null): NavItem[] {
  if (!licenseAccess) return items;
  const annotate = <T extends NavChild>(item: T): T => {
    if (!item.licenseFeature || hasLicensedFeature(licenseAccess, item.licenseFeature)) return item;
    return {
      ...item,
      text: `${item.text}（授权未包含）`,
      icon: <IconLockStroked />,
      licenseLocked: true,
      licenseReason: licenseRestrictionMessage(licenseAccess, item.licenseFeature),
      disabled: true,
    };
  };
  return items.map(item => ({
    ...annotate(item),
    items: item.items?.map(annotate),
  }));
}

export function getNavItemsForRole(role?: string | null, licenseAccess?: LicenseAccess | null): NavItem[] {
  let items: NavItem[];
  if (isProjectAdminRole(role)) items = [projectOverviewItem, ...filterTaskNavigation(projectAdminRoutes)];
  else if (isProjectConsoleRole(role)) items = [projectOverviewItem, ...filterTaskNavigation(developerRoutes)];
  else if (isBillingRole(role)) items = labelOverviewForRole(filterTaskNavigation(billingRoutes), role);
  else if (isPlatformAdminRole(role)) items = labelOverviewForRole(filterTaskNavigation(platformRoutes), role);
  else if (isOrgAdminRole(role)) items = labelOverviewForRole(filterTaskNavigation(orgAdminRoutes), role);
  else if (isOrgViewerRole(role)) items = labelOverviewForRole(filterTaskNavigation(orgViewerRoutes), role);
  else items = [accessStateItem, ...filterTaskNavigation(new Set(['/docs']))];
  return applyLicenseState(items, licenseAccess);
}

export function getDefaultPathForRole(role?: string | null) {
  if (isProjectConsoleRole(role)) return '/developer';
  if (isOrgAdminRole(role) || isBillingRole(role) || isOrgViewerRole(role)) return '/';
  return '/access';
}

function firstSegmentPath(path: string) {
  const pathname = (path || '/').split('?')[0].split('#')[0];
  if (pathname === '/') return '/';
  const first = pathname.split('/').filter(Boolean)[0];
  return first ? `/${first}` : '/';
}

const routeLicenseFeatures: Partial<Record<string, LicenseFeature>> = {
  '/model-governance': 'model_governance',
  '/test': 'gateway',
  '/guardrail-policies': 'guardrails',
  '/notifications': 'notifications',
  '/media-jobs': 'gateway',
};

export function getRequiredLicenseFeatureForRoute(path: string): LicenseFeature | undefined {
  return routeLicenseFeatures[firstSegmentPath(path)];
}

export function hasLicensedFeature(licenseAccess: LicenseAccess | null | undefined, feature: LicenseFeature) {
  // ProtectedRoute refreshes old/login-only profiles before rendering. The
  // permissive undefined branch keeps isolated component tests and rolling
  // frontend/backend upgrades from inventing a client-side security boundary;
  // the backend License gate remains authoritative.
  if (!licenseAccess) return true;
  return licenseAccess.enforceable && licenseAccess.features?.[feature] === true;
}

export function isRouteLicensed(path: string, licenseAccess?: LicenseAccess | null) {
  const feature = getRequiredLicenseFeatureForRoute(path);
  return !feature || hasLicensedFeature(licenseAccess, feature);
}

export function licenseFeatureLabel(feature: LicenseFeature) {
  const labels: Record<LicenseFeature, string> = {
    gateway: '模型网关',
    agent_capabilities: 'Agent 能力令牌',
    oidc: '企业身份登录（OIDC）',
    guardrails: '内容安全',
    notifications: '通知中心',
    model_governance: '模型治理',
    data_exchange: 'X-Insight 数据交换',
  };
  return labels[feature];
}

export function licenseRestrictionMessage(licenseAccess: LicenseAccess, feature: LicenseFeature) {
  const label = licenseFeatureLabel(feature);
  switch (licenseAccess.status) {
    case 'missing':
      return `当前部署尚未导入有效商业授权，因此「${label}」不可用。`;
    case 'expired':
      return `当前商业授权已过期，因此「${label}」不可用。`;
    case 'not_yet_valid':
      return `当前商业授权尚未生效，因此「${label}」不可用。`;
    case 'deployment_mismatch':
      return `当前商业授权与本部署不匹配，因此「${label}」不可用。`;
    case 'invalid':
      return `当前商业授权未通过校验，因此「${label}」不可用。`;
    default:
      if (!licenseAccess.enforceable) {
        return `当前商业授权不可执行，因此「${label}」不可用。`;
      }
      return `当前商业授权未包含「${label}」。`;
  }
}

function hasPositiveProjectID(path: string) {
  const queryStart = path.indexOf('?');
  if (queryStart < 0) return false;

  const search = path.slice(queryStart + 1).split('#')[0];
  const rawProjectID = new URLSearchParams(search).get('project_id');
  if (!rawProjectID || !/^[1-9]\d*$/.test(rawProjectID)) return false;

  return Number.isSafeInteger(Number(rawProjectID));
}

export function isRouteAllowedForRole(path: string, role?: string | null) {
  const route = firstSegmentPath(path);
  if (route === '/login') return true;
  if (route === '/access') return true;
  if (route === '/docs') return true;
  if (route === '/settings') return true;
  if (route === '/license') return isPlatformAdminRole(role);

  // The project workbench is a role-specific home, not a hidden platform-admin
  // surface. Platform and organization admins use their own task dashboard.
  if (route === '/developer') return isProjectConsoleRole(role);

  if (!platformRoutes.has(route) && !compatibilityRoutes.has(route)) return false;

  if (isPlatformAdminRole(role)) return true;

  if (isOrgAdminRole(role)) {
    return [
      '/',
      '/keys',
      '/organization-members',
      '/project-management',
      '/member-lifecycle',
      '/agent-trust',
      '/agent-trust-evidence',
      '/risk-control',
      '/limit-policies',
      '/guardrail-policies',
      '/logs',
      '/media-jobs',
      '/provenance-passport',
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
    return ['/', '/logs', '/wallet', '/model-prices', '/reconciliation', '/usage-analytics', '/notifications', '/grants'].includes(route);
  }

  if (isProjectConsoleRole(role)) {
    if (isProjectAdminRole(role) && ['/organization-members', '/project-management'].includes(route)) return true;
    if (isProjectAdminRole(role) && route === '/notifications') return hasPositiveProjectID(path);
    if (route === '/media-jobs') return true;
    return false;
  }

  if (isOrgViewerRole(role)) return route === '/';

  return false;
}

export function getOpenKeysForPath(path: string, role?: string | null, licenseAccess?: LicenseAccess | null) {
  const route = firstSegmentPath(path);
  const parent = getNavItemsForRole(role, licenseAccess).find(item => item.items?.some(child => child.itemKey === route));
  return parent ? [parent.itemKey] : [];
}

export function canSearchRequestLogs(role?: string | null) {
  return isPlatformAdminRole(role) || isOrgAdminRole(role) || isBillingRole(role);
}

export function canOpenUnscopedNotifications(role?: string | null) {
  return isPlatformAdminRole(role) || isOrgAdminRole(role) || isBillingRole(role);
}
