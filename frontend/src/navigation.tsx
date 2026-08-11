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

export type OrganizationRole = 'platform_admin' | 'org_admin' | 'billing_admin' | 'org_viewer' | 'member';
export type ProjectMembershipRole = 'project_admin' | 'project_user' | 'project_viewer';

export interface AuthzContext {
  organizationRole: OrganizationRole | '';
  projectMembershipRole: ProjectMembershipRole | '';
  projectMembershipKnown: boolean;
	hasLiveProjectMembership: boolean;
	hasProjectAdminMembership: boolean;
	canDiscoverAccessReview: boolean;
	canDiscoverWorkloads: boolean;
}

export interface AuthzUserIdentity {
  organization_role?: string | null;
  role?: string | null;
	navigation_capabilities?: {
		has_live_project_membership?: boolean;
		has_project_admin_membership?: boolean;
		can_discover_access_review?: boolean;
		can_discover_workloads?: boolean;
	} | null;
}

const organizationRoles = new Set<OrganizationRole>([
  'platform_admin',
  'org_admin',
  'billing_admin',
  'org_viewer',
  'member',
]);

export function canonicalOrganizationRole(
  organizationRole?: string | null,
  compatibilityRole?: string | null,
): OrganizationRole | '' {
  const explicit = normalizeRole(organizationRole);
  if (organizationRoles.has(explicit as OrganizationRole)) return explicit as OrganizationRole;
  // An explicit but unknown organization role is never replaced by a more
  // privileged compatibility shadow.
  if (explicit) return 'member';

  const fallback = normalizeRole(compatibilityRole);
  if (organizationRoles.has(fallback as OrganizationRole)) return fallback as OrganizationRole;
  // Legacy project-scoped role aliases carry no organization-wide authority.
  return fallback ? 'member' : '';
}

export function canonicalProjectMembershipRole(role?: string | null): ProjectMembershipRole | '' {
  const normalized = normalizeRole(role);
  if (normalized === 'project_admin') return 'project_admin';
  if (normalized === 'project_user' || normalized === 'project_developer' || normalized === 'developer') return 'project_user';
  if (normalized === 'project_viewer' || normalized === 'viewer') return 'project_viewer';
  return '';
}

export function authzContextForUser(
  user?: AuthzUserIdentity | null,
  projectMembershipRole?: string | null,
  projectMembershipKnown = projectMembershipRole !== undefined,
): AuthzContext {
  return {
    organizationRole: canonicalOrganizationRole(user?.organization_role, user?.role),
    projectMembershipRole: canonicalProjectMembershipRole(projectMembershipRole),
    projectMembershipKnown,
		hasLiveProjectMembership: user?.navigation_capabilities?.has_live_project_membership === true || canonicalProjectMembershipRole(projectMembershipRole) !== '',
		hasProjectAdminMembership: user?.navigation_capabilities?.has_project_admin_membership === true || canonicalProjectMembershipRole(projectMembershipRole) === 'project_admin',
		canDiscoverAccessReview: user?.navigation_capabilities?.can_discover_access_review === true,
		canDiscoverWorkloads: user?.navigation_capabilities?.can_discover_workloads === true,
  };
}

export function organizationRoleForUser(user?: AuthzUserIdentity | null) {
  return canonicalOrganizationRole(user?.organization_role, user?.role);
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

export function isProjectMembershipRole(role?: string | null) {
  return canonicalProjectMembershipRole(role) !== '';
}

export function isProjectAdminMembership(role?: string | null) {
  return canonicalProjectMembershipRole(role) === 'project_admin';
}

export function canReadRuntimeStatus(authz: AuthzContext) {
  return isOrgAdminRole(authz.organizationRole);
}

export function canCreateProjectKey(authz: AuthzContext) {
  return isOrgAdminRole(authz.organizationRole) ||
    authz.projectMembershipRole === 'project_admin' ||
    authz.projectMembershipRole === 'project_user';
}

export function canDisableProjectKey(authz: AuthzContext) {
  return canCreateProjectKey(authz);
}

export function canInvokeProject(authz: AuthzContext) {
  return isOrgAdminRole(authz.organizationRole) ||
    authz.projectMembershipRole === 'project_admin' ||
    authz.projectMembershipRole === 'project_user';
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
		{ itemKey: '/model-readiness', text: '模型 Readiness', icon: <IconPulse /> },
      { itemKey: '/test', text: '上游检测', icon: <IconTestScore />, licenseFeature: 'gateway' },
    ],
  },
  {
    itemKey: 'g-access-security',
    text: '访问与安全',
    icon: <IconShieldStroked />,
    items: [
		{ itemKey: '/access-review', text: '访问审核', icon: <IconChecklistStroked /> },
		{ itemKey: '/applications', text: '应用与 Agent', icon: <IconKeyStroked /> },
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
      { itemKey: '/cost-optimization', text: '成本优化', icon: <IconBarChartVStroked /> },
      { itemKey: '/reconciliation', text: '分摊与对账', icon: <IconFlowChartStroked /> },
    ],
  },
  {
    itemKey: 'g-system-integration',
    text: '系统与集成',
    icon: <IconSettingStroked />,
    items: [
      { itemKey: '/deployment', text: '部署与网络', icon: <IconMonitorStroked /> },
      { itemKey: '/integrations', text: '集成管理', icon: <IconConnectionPoint1 /> },
      { itemKey: '/docs', text: 'API 与兼容性', icon: <IconBookOpenStroked /> },
      { itemKey: '/license', text: '商业授权', icon: <IconShield /> },
    ],
  },
];

const projectOverviewItem: NavItem = { itemKey: '/developer', text: '项目工作台', icon: <IconTerminal /> };
const accessStateItem: NavItem = { itemKey: '/access', text: '我的访问', icon: <IconShieldStroked /> };

const platformRoutes = new Set(taskNavItems.flatMap(item => item.items?.map(child => child.itemKey) || [item.itemKey]));
const compatibilityRoutes = new Set([
  '/governance-simulation', '/risk-control', '/member-lifecycle', '/users', '/project-management',
  '/provenance-passport', '/org-model-prices', '/grants', '/agent-trust-evidence', '/keys', '/data-exchange',
	'/connectors',
]);
const orgAdminRoutes = new Set([
	'/', '/model-governance', '/model-readiness', '/risk-control', '/access-review', '/applications', '/organization-members', '/project-management', '/member-lifecycle', '/agent-trust', '/agent-trust-evidence', '/limit-policies', '/guardrail-policies', '/users',
  '/logs', '/provenance-passport', '/usage-analytics', '/notifications', '/status', '/audit-logs',
  '/media-jobs',
  '/model-prices', '/cost-optimization', '/docs',
]);
const billingRoutes = new Set([
  '/', '/usage-analytics',
  '/wallet', '/model-prices', '/cost-optimization', '/reconciliation', '/docs',
]);
const projectAdminRoutes = new Set(['/organization-members', '/access-review', '/applications', '/model-readiness', '/media-jobs', '/docs']);
const developerRoutes = new Set(['/media-jobs', '/docs']);
const orgViewerRoutes = new Set(['/', '/organization-members', '/model-governance', '/docs']);

function filterTaskNavigation(allowedRoutes: Set<string>) {
  return taskNavItems.flatMap(item => {
    if (!item.items) return allowedRoutes.has(item.itemKey) ? [item] : [];
    const items = item.items.filter(child => allowedRoutes.has(child.itemKey));
    return items.length > 0 ? [{ ...item, items }] : [];
  });
}

function labelOverviewForRole(items: NavItem[], organizationRole?: string | null) {
  const overviewText = isBillingRole(organizationRole)
    ? '财务与用量总览'
    : isPlatformAdminRole(organizationRole)
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

export function getNavItemsForAuthz(authz: AuthzContext, licenseAccess?: LicenseAccess | null): NavItem[] {
  const organizationRole = authz.organizationRole;
  let allowedRoutes: Set<string>;
  if (isPlatformAdminRole(organizationRole)) allowedRoutes = new Set(platformRoutes);
  else if (isOrgAdminRole(organizationRole)) allowedRoutes = new Set(orgAdminRoutes);
  else if (isBillingRole(organizationRole)) allowedRoutes = new Set(billingRoutes);
  else if (isOrgViewerRole(organizationRole)) allowedRoutes = new Set(orgViewerRoutes);
  else allowedRoutes = new Set(['/docs']);

  if (authz.projectMembershipRole === 'project_admin') {
    projectAdminRoutes.forEach(route => allowedRoutes.add(route));
  } else if (authz.projectMembershipRole) {
    developerRoutes.forEach(route => allowedRoutes.add(route));
  }
	if (authz.canDiscoverAccessReview || authz.hasProjectAdminMembership) allowedRoutes.add('/access-review');
	if (authz.canDiscoverWorkloads || authz.hasProjectAdminMembership) allowedRoutes.add('/applications');
	if (authz.hasLiveProjectMembership || isOrgAdminRole(organizationRole) || isOrgViewerRole(organizationRole)) allowedRoutes.add('/model-readiness');

  let items = filterTaskNavigation(allowedRoutes);
  // End-user project navigation follows the live aggregate membership
  // projection from /me. Pure organization-management identities keep the
  // management taxonomy only; direct-route and backend authority are unchanged.
  const exposeProjectWorkbench = authz.hasLiveProjectMembership;
  if (exposeProjectWorkbench) items = [projectOverviewItem, ...items];
  if (organizationRole && organizationRole !== 'member') items = labelOverviewForRole(items, organizationRole);
	const exposeAccessState = !organizationRole || organizationRole === 'member' || authz.hasLiveProjectMembership;
	if (exposeAccessState) items = [accessStateItem, ...items];
  return applyLicenseState(items, licenseAccess);
}

export function getDefaultPathForAuthz(authz: AuthzContext) {
	if (authz.organizationRole === 'member') return authz.hasLiveProjectMembership ? '/developer' : '/access';
  if (isOrgAdminRole(authz.organizationRole) || isBillingRole(authz.organizationRole) || isOrgViewerRole(authz.organizationRole)) return '/';
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
	'/connectors': 'data_exchange',
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

function projectRouteAllowed(path: string, authz: AuthzContext) {
  const route = firstSegmentPath(path);
  if (route === '/developer') return true;

  if (authz.projectMembershipRole === 'project_admin') {
    if (route === '/notifications') return hasPositiveProjectID(path);
    return ['/organization-members', '/project-management', '/media-jobs'].includes(route);
  }
  if (authz.projectMembershipRole === 'project_user' || authz.projectMembershipRole === 'project_viewer') {
    return route === '/media-jobs';
  }

  // The shell has no authoritative selected-project membership. These routes
  // are allowed only to load a project list; their pages must derive every
  // capability from that project's live current_user_membership_role. The
  // backend remains authoritative if the user has no membership.
  if (!authz.projectMembershipKnown) {
    if (route === '/notifications') return hasPositiveProjectID(path);
    return ['/organization-members', '/project-management', '/media-jobs'].includes(route);
  }
  return false;
}

export function isRouteAllowedForAuthz(path: string, authz: AuthzContext) {
  const route = firstSegmentPath(path);
  if (route === '/login') return true;
  if (route === '/access') return true;
	if (route === '/access-review') return isOrgAdminRole(authz.organizationRole) || authz.canDiscoverAccessReview || authz.hasProjectAdminMembership;
	if (route === '/applications') return isOrgAdminRole(authz.organizationRole) || authz.canDiscoverWorkloads || authz.hasProjectAdminMembership;
	if (route === '/model-readiness') return isOrgAdminRole(authz.organizationRole) || isOrgViewerRole(authz.organizationRole) || authz.hasLiveProjectMembership;
  if (route === '/docs') return true;
  if (route === '/settings') return true;
  if (route === '/license') return isPlatformAdminRole(authz.organizationRole);
  if (route === '/developer') return Boolean(authz.organizationRole);

  if (!platformRoutes.has(route) && !compatibilityRoutes.has(route)) return false;

  if (isPlatformAdminRole(authz.organizationRole)) return true;

  if (isOrgAdminRole(authz.organizationRole)) {
    return [
      '/',
		'/access-review',
      '/applications',
		'/model-readiness',
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
      '/cost-optimization',
      '/org-model-prices',
    ].includes(route);
  }

  if (isBillingRole(authz.organizationRole)) {
    if (['/', '/wallet', '/model-prices', '/cost-optimization', '/reconciliation', '/usage-analytics', '/grants'].includes(route)) return true;
    return projectRouteAllowed(path, authz);
  }

  if (isOrgViewerRole(authz.organizationRole)) {
    if (['/', '/organization-members', '/project-management', '/model-governance'].includes(route)) return true;
    return projectRouteAllowed(path, authz);
  }

  if (authz.organizationRole === 'member') return projectRouteAllowed(path, authz);
  return projectRouteAllowed(path, authz);
}

export function getOpenKeysForPath(path: string, authz: AuthzContext, licenseAccess?: LicenseAccess | null) {
  const route = firstSegmentPath(path);
  const parent = getNavItemsForAuthz(authz, licenseAccess).find(item => item.items?.some(child => child.itemKey === route));
  return parent ? [parent.itemKey] : [];
}

export function canSearchRequestLogs(authz: AuthzContext) {
  return isPlatformAdminRole(authz.organizationRole) || isOrgAdminRole(authz.organizationRole) || isBillingRole(authz.organizationRole);
}

export function canOpenUnscopedNotifications(authz: AuthzContext) {
  return isPlatformAdminRole(authz.organizationRole) || isOrgAdminRole(authz.organizationRole) || isBillingRole(authz.organizationRole);
}
