import { describe, expect, it } from 'vitest';
import {
  type AuthzContext,
  authzContextForUser,
  canCreateProjectKey,
  canDisableProjectKey,
  canOpenUnscopedNotifications,
  canReadRuntimeStatus,
  canSearchRequestLogs,
  getDefaultPathForAuthz,
  getNavItemsForAuthz,
  getOpenKeysForPath,
  hasLicensedFeature,
  isRouteAllowedForAuthz,
  isRouteLicensed,
  licenseRestrictionMessage,
  type LicenseAccess,
  type OrganizationRole,
  type ProjectMembershipRole,
} from '../navigation';

const DENIED_FEATURES: LicenseAccess['features'] = {
  gateway: false,
  agent_capabilities: false,
  oidc: false,
  guardrails: false,
  notifications: false,
  model_governance: false,
  data_exchange: false,
};

function licenseAccess(
  status: LicenseAccess['status'],
  enforceable: boolean,
  enabled: Array<keyof LicenseAccess['features']> = [],
): LicenseAccess {
  return {
    status,
    enforceable,
    features: { ...DENIED_FEATURES, ...Object.fromEntries(enabled.map(feature => [feature, true])) },
  };
}

function context(
  organizationRole: OrganizationRole | '' = 'member',
  projectMembershipRole: ProjectMembershipRole | '' = '',
  projectMembershipKnown = true,
): AuthzContext {
	return {
		organizationRole, projectMembershipRole, projectMembershipKnown,
		hasLiveProjectMembership: projectMembershipRole !== '',
		hasProjectAdminMembership: projectMembershipRole === 'project_admin',
		canDiscoverAccessReview: organizationRole === 'platform_admin' || organizationRole === 'org_admin' || projectMembershipRole === 'project_admin',
		canDiscoverWorkloads: organizationRole === 'platform_admin' || organizationRole === 'org_admin' || projectMembershipRole === 'project_admin',
	};
}

function routeKeys(authz: AuthzContext): string[] {
  return getNavItemsForAuthz(authz).flatMap(item => [
    ...(item.itemKey.startsWith('/') ? [item.itemKey] : []),
    ...((item.items || []).map(child => child.itemKey)),
  ]);
}

const scenarios: Record<string, { authz: AuthzContext; routes: string[] }> = {
  platform: {
		authz: context('platform_admin'),
		routes: [
		'/', '/channels', '/channel-keys', '/model-governance', '/model-readiness',
		'/access-review', '/applications', '/organization-members', '/agent-trust', '/limit-policies', '/guardrail-policies', '/audit-logs',
      '/logs', '/media-jobs', '/usage-analytics', '/notifications', '/status',
      '/wallet', '/model-prices', '/cost-optimization', '/reconciliation',
      '/deployment', '/integrations', '/docs', '/license',
    ],
  },
  organizationAdmin: {
	authz: context('org_admin'),
	routes: [
		'/', '/model-governance', '/model-readiness', '/access-review', '/applications', '/organization-members', '/agent-trust', '/limit-policies', '/guardrail-policies', '/audit-logs',
      '/logs', '/media-jobs', '/usage-analytics', '/notifications', '/status',
      '/model-prices', '/cost-optimization', '/docs',
    ],
  },
  billing: {
    authz: context('billing_admin'),
	routes: ['/', '/usage-analytics', '/wallet', '/model-prices', '/cost-optimization', '/reconciliation', '/docs'],
  },
  organizationViewer: {
    authz: context('org_viewer'),
	routes: ['/', '/model-governance', '/model-readiness', '/organization-members', '/docs'],
  },
  projectAdmin: {
    authz: context('member', 'project_admin'),
	routes: ['/access', '/developer', '/model-readiness', '/access-review', '/applications', '/organization-members', '/media-jobs', '/docs'],
  },
  projectUser: {
    authz: context('member', 'project_user'),
	routes: ['/access', '/developer', '/model-readiness', '/media-jobs', '/docs'],
  },
  projectViewer: {
    authz: context('member', 'project_viewer'),
	routes: ['/access', '/developer', '/model-readiness', '/media-jobs', '/docs'],
  },
  memberWithoutMembership: {
    authz: context('member'),
	routes: ['/access', '/docs'],
  },
  unknownLegacyIdentity: {
    authz: context(''),
    routes: ['/access', '/docs'],
  },
};

describe('two-axis navigation boundaries', () => {
  it('exposes the exact navigation surface and keeps every visible route loadable', () => {
    Object.entries(scenarios).forEach(([name, scenario]) => {
      const actual = routeKeys(scenario.authz);
      expect([...actual].sort(), `${name} visible routes`).toEqual([...scenario.routes].sort());
      actual.forEach(route => expect(isRouteAllowedForAuthz(route, scenario.authz), `${name} ${route}`).toBe(true));
      expect(actual).toContain(getDefaultPathForAuthz(scenario.authz));
    });
  });

  it('keeps organization authority and selected-project membership orthogonal', () => {
    const orgViewerWithoutMembership = context('org_viewer');
    const orgViewerProjectAdmin = context('org_viewer', 'project_admin');
    const billingWithoutMembership = context('billing_admin');
    const billingProjectViewer = context('billing_admin', 'project_viewer');
    expect(routeKeys(orgViewerWithoutMembership)).not.toContain('/developer');
    expect(routeKeys(orgViewerWithoutMembership)).not.toContain('/access');
	expect(routeKeys(orgViewerProjectAdmin)).not.toContain('/access');
	expect(routeKeys(orgViewerProjectAdmin)).not.toContain('/developer');
    expect(routeKeys(billingWithoutMembership)).not.toContain('/developer');
    expect(routeKeys(billingWithoutMembership)).not.toContain('/access');
	expect(routeKeys(billingProjectViewer)).not.toContain('/access');
	expect(routeKeys(billingProjectViewer)).not.toContain('/developer');
    expect(isRouteAllowedForAuthz('/project-management', orgViewerWithoutMembership)).toBe(true);
    expect(canCreateProjectKey(orgViewerWithoutMembership)).toBe(false);
    expect(canCreateProjectKey(orgViewerProjectAdmin)).toBe(true);
    expect(canCreateProjectKey(billingWithoutMembership)).toBe(false);
    expect(canCreateProjectKey(billingProjectViewer)).toBe(false);
    expect(isRouteAllowedForAuthz('/notifications?project_id=7', orgViewerProjectAdmin)).toBe(true);

    const projectA = context('member', 'project_admin');
    const projectB = context('member', 'project_viewer');
    expect(canCreateProjectKey(projectA)).toBe(true);
    expect(canDisableProjectKey(projectA)).toBe(true);
    expect(canCreateProjectKey(projectB)).toBe(false);
    expect(canDisableProjectKey(projectB)).toBe(false);
  });

  it('does not turn a legacy v2 project shadow into organization or project authority', () => {
    const legacyDeveloper = authzContextForUser({ role: 'developer' });
    const legacyProjectAdmin = authzContextForUser({ role: 'project_admin' });
    expect(legacyDeveloper).toEqual(context('member', '', false));
    expect(legacyProjectAdmin).toEqual(context('member', '', false));
    expect(canCreateProjectKey(legacyDeveloper)).toBe(false);
    expect(canCreateProjectKey(legacyProjectAdmin)).toBe(false);
    expect(isRouteAllowedForAuthz('/users', legacyProjectAdmin)).toBe(false);
  });

  it('uses one task taxonomy and explicit authz context for shared sections', () => {
    expect(getNavItemsForAuthz(context('platform_admin')).map(item => item.text)).toEqual([
		'平台总览', '模型接入', '访问与安全', '监控与用量', '成本与财务', '系统与集成',
    ]);
	expect(getNavItemsForAuthz(context('org_admin'))[0]?.text).toBe('组织总览');
	expect(getNavItemsForAuthz(context('org_viewer'))[0]?.text).toBe('组织总览');
	expect(getNavItemsForAuthz(context('billing_admin'))[0]?.text).toBe('财务与用量总览');
	expect(getNavItemsForAuthz(context('org_admin', 'project_user')).map(item => item.text).slice(0, 2)).toEqual(['组织总览', '模型接入']);
	expect(getNavItemsForAuthz(context('member', 'project_admin'))[0]?.text).toBe('我的访问');

    expect(getOpenKeysForPath('/notifications', context('platform_admin'))).toEqual(['g-observability']);
    expect(getOpenKeysForPath('/notifications', context('member', 'project_admin'))).toEqual([]);
    expect(getOpenKeysForPath('/docs', context('platform_admin'))).toEqual(['g-system-integration']);
	expect(getOpenKeysForPath('/model-governance', context('org_admin'))).toEqual(['g-model-access']);
  });

	it('uses shell discovery capabilities without granting selected-project actions', () => {
		const capabilities = {
			has_live_project_membership: true,
			has_project_admin_membership: true,
			can_discover_access_review: true,
			can_discover_workloads: true,
		};
		const mixed = authzContextForUser({
			organization_role: 'member', role: 'member',
			navigation_capabilities: capabilities,
		});
		expect(routeKeys(mixed)).toEqual(expect.arrayContaining(['/access', '/developer', '/access-review', '/applications']));
		expect(canCreateProjectKey(mixed)).toBe(false);
		const managementOnly = authzContextForUser({ organization_role: 'platform_admin' });
		expect(routeKeys(managementOnly)).not.toContain('/access');
		expect(routeKeys(managementOnly)).not.toContain('/developer');
		const managementMember = authzContextForUser({
			organization_role: 'platform_admin',
			navigation_capabilities: capabilities,
		});
		expect(routeKeys(managementMember)).not.toContain('/access');
		expect(routeKeys(managementMember)).not.toContain('/developer');
		const selectedViewer = authzContextForUser({ organization_role: 'member', navigation_capabilities: capabilities }, 'project_viewer', true);
		expect(canCreateProjectKey(selectedViewer)).toBe(false);
		const selectedAdmin = authzContextForUser({ organization_role: 'member', navigation_capabilities: capabilities }, 'project_admin', true);
		expect(canCreateProjectKey(selectedAdmin)).toBe(true);
	});

	it('keeps default routes aligned with management and end-user entry points', () => {
		expect(getDefaultPathForAuthz(context('platform_admin'))).toBe('/');
		expect(getDefaultPathForAuthz(context('org_admin', 'project_user'))).toBe('/');
		expect(getDefaultPathForAuthz(context('member'))).toBe('/access');
		expect(getDefaultPathForAuthz(context('member', 'project_viewer'))).toBe('/developer');
		expect(isRouteAllowedForAuthz('/access', context('platform_admin'))).toBe(false);
		expect(isRouteAllowedForAuthz('/developer', context('platform_admin'))).toBe(false);
	});

  it('keeps account settings avatar-only while allowing every authenticated identity to open it', () => {
    Object.values(scenarios).forEach(({ authz }) => {
      expect(routeKeys(authz)).not.toContain('/settings');
      expect(isRouteAllowedForAuthz('/settings', authz)).toBe(true);
    });
  });

  it('keeps billing financial-only and organization viewers organization-read-only', () => {
    const billing = context('billing_admin');
    ['/limit-policies', '/guardrail-policies', '/status', '/audit-logs', '/model-governance'].forEach(route => {
      expect(isRouteAllowedForAuthz(route, billing)).toBe(false);
    });
    expect(isRouteAllowedForAuthz('/wallet', billing)).toBe(true);
    expect(canReadRuntimeStatus(billing)).toBe(false);
    expect(canReadRuntimeStatus(context('org_admin'))).toBe(true);

    const orgViewer = context('org_viewer');
    expect(isRouteAllowedForAuthz('/project-management', orgViewer)).toBe(true);
    expect(isRouteAllowedForAuthz('/model-governance', orgViewer)).toBe(true);
    expect(canCreateProjectKey(orgViewer)).toBe(false);
    expect(canOpenUnscopedNotifications(orgViewer)).toBe(false);
  });

  it('requires a positive project scope for membership-scoped notifications', () => {
    const projectAdmin = context('member', 'project_admin');
    expect(isRouteAllowedForAuthz('/notifications?project_id=7', projectAdmin)).toBe(true);
    [
      '/notifications', '/notifications?project_id=', '/notifications?project_id=0',
      '/notifications?project_id=-1', '/notifications?project_id=1.5',
      '/notifications?project_id=project-1', '/notifications?project_id=9007199254740992',
    ].forEach(path => expect(isRouteAllowedForAuthz(path, projectAdmin), path).toBe(false));
    expect(canOpenUnscopedNotifications(projectAdmin)).toBe(false);
    expect(canOpenUnscopedNotifications(context('org_admin'))).toBe(true);
  });

  it('rejects unknown routes and protects platform-only controls', () => {
    Object.values(scenarios).forEach(({ authz }) => {
      expect(isRouteAllowedForAuthz('/unknown-route', authz)).toBe(false);
    });
    expect(isRouteAllowedForAuthz('/license', context('platform_admin'))).toBe(true);
    expect(isRouteAllowedForAuthz('/license', context('org_admin'))).toBe(false);
    expect(isRouteAllowedForAuthz('/deployment', context('org_admin'))).toBe(false);
    expect(isRouteAllowedForAuthz('/data-exchange', context('org_admin'))).toBe(false);
    expect(isRouteAllowedForAuthz('/connectors/x-insight/install', context('platform_admin'))).toBe(true);
    expect(isRouteAllowedForAuthz('/connectors/x-insight/install', context('org_admin'))).toBe(false);
    expect(isRouteAllowedForAuthz('/risk-control', context('billing_admin'))).toBe(false);
  });

  it('shows request-log search only to organization-scoped operators', () => {
    ['platform_admin', 'org_admin', 'billing_admin'].forEach(role => {
      expect(canSearchRequestLogs(context(role as OrganizationRole))).toBe(true);
    });
    [context('org_viewer'), context('member', 'project_admin'), context('member', 'project_user')].forEach(authz => {
      expect(canSearchRequestLogs(authz)).toBe(false);
    });
  });

  it('keeps licensed-route annotation independent from role axes', () => {
    const partial = licenseAccess('active', true, ['gateway', 'notifications']);
    const platformItems = getNavItemsForAuthz(context('platform_admin'), partial);
    const platformChildren = platformItems.flatMap(item => item.items || []);

    expect(platformChildren.find(item => item.itemKey === '/test')?.licenseLocked).toBeUndefined();
	expect(platformChildren.find(item => item.itemKey === '/model-governance')?.licenseLocked).toBe(true);
    expect(platformChildren.find(item => item.itemKey === '/integrations')?.licenseLocked).toBeUndefined();
    expect(isRouteLicensed('/guardrail-policies', partial)).toBe(false);
    expect(isRouteLicensed('/notifications?project_id=7', partial)).toBe(true);
    expect(isRouteLicensed('/connectors/x-insight/install', partial)).toBe(false);
  });

  it('treats wildcard projection as all capabilities and fail-closes missing or expired projections', () => {
    const wildcard = licenseAccess('active', true, Object.keys(DENIED_FEATURES) as Array<keyof LicenseAccess['features']>);
    const missing = licenseAccess('missing', false);
    const expired = licenseAccess('expired', false, Object.keys(DENIED_FEATURES) as Array<keyof LicenseAccess['features']>);

    Object.keys(DENIED_FEATURES).forEach(feature => {
      expect(hasLicensedFeature(wildcard, feature as keyof LicenseAccess['features'])).toBe(true);
      expect(hasLicensedFeature(missing, feature as keyof LicenseAccess['features'])).toBe(false);
      expect(hasLicensedFeature(expired, feature as keyof LicenseAccess['features'])).toBe(false);
    });
    expect(licenseRestrictionMessage(missing, 'gateway')).toContain('尚未导入有效商业授权');
    expect(licenseRestrictionMessage(expired, 'notifications')).toContain('已过期');
    expect(isRouteLicensed('/test', expired)).toBe(false);
    expect(hasLicensedFeature(null, 'gateway')).toBe(true);
  });
});
