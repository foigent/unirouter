import { describe, expect, it } from 'vitest';
import {
  canCreateProjectKey,
  canDisableProjectKey,
  canOpenUnscopedNotifications,
  canReadRuntimeStatus,
  canSearchRequestLogs,
  getDefaultPathForRole,
  getNavItemsForRole,
  getOpenKeysForPath,
  hasLicensedFeature,
  isRouteAllowedForRole,
  isRouteLicensed,
  licenseRestrictionMessage,
  type LicenseAccess,
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

function routeKeys(role: string): string[] {
  return getNavItemsForRole(role).flatMap(item => [
    ...(item.itemKey.startsWith('/') ? [item.itemKey] : []),
    ...((item.items || []).map(child => child.itemKey)),
  ]);
}

const expectedRoutes: Record<string, string[]> = {
  platform_admin: [
    '/', '/channels', '/channel-keys', '/model-governance', '/test',
    '/keys', '/organization-members', '/agent-trust', '/limit-policies', '/guardrail-policies', '/audit-logs',
    '/logs', '/media-jobs', '/usage-analytics', '/notifications', '/status',
    '/wallet', '/model-prices', '/reconciliation',
    '/license', '/data-exchange', '/docs',
  ],
  org_admin: [
    '/', '/model-governance', '/keys', '/organization-members', '/agent-trust', '/limit-policies', '/guardrail-policies', '/audit-logs',
    '/logs', '/media-jobs', '/usage-analytics', '/notifications', '/status',
    '/model-prices', '/docs',
  ],
  billing_admin: [
    '/', '/logs', '/usage-analytics', '/notifications',
    '/wallet', '/model-prices', '/reconciliation', '/docs',
  ],
  project_admin: ['/developer', '/organization-members', '/media-jobs', '/docs'],
  developer: ['/developer', '/media-jobs', '/docs'],
  viewer: ['/developer', '/media-jobs', '/docs'],
  org_viewer: ['/', '/docs'],
  owner: ['/access', '/docs'],
};

describe('navigation role boundaries', () => {
  it('exposes the complete role-specific entry set and keeps every visible route allowed', () => {
    Object.entries(expectedRoutes).forEach(([role, expected]) => {
      const actual = routeKeys(role);
      expect([...actual].sort(), `${role} visible routes`).toEqual([...expected].sort());
      actual.forEach(route => expect(isRouteAllowedForRole(route, role), `${role} nav route ${route}`).toBe(true));
      expect(actual, `${role} default route is visible`).toContain(getDefaultPathForRole(role));
    });

    expect(routeKeys('admin').sort()).toEqual(routeKeys('platform_admin').sort());
  });

  it('uses one task taxonomy and keeps a shared route in the same section for every role', () => {
    expect(getNavItemsForRole('platform_admin').map(item => item.text)).toEqual([
      '平台总览', '模型接入', '访问与安全', '监控与用量', '成本与财务', '系统与集成',
    ]);
    expect(getNavItemsForRole('org_admin')[0]?.text).toBe('组织总览');
    expect(getNavItemsForRole('org_viewer')[0]?.text).toBe('组织总览');
    expect(getNavItemsForRole('billing_admin')[0]?.text).toBe('财务与用量总览');
    ['project_admin', 'developer', 'viewer'].forEach(role => {
      expect(getNavItemsForRole(role)[0]?.text).toBe('项目工作台');
    });

    ['platform_admin', 'org_admin', 'billing_admin'].forEach(role => {
      expect(getOpenKeysForPath('/notifications', role)).toEqual(['g-observability']);
    });
    expect(getOpenKeysForPath('/notifications', 'project_admin')).toEqual([]);
    expect(getOpenKeysForPath('/docs', 'platform_admin')).toEqual(['g-system-integration']);
    expect(getOpenKeysForPath('/model-governance', 'org_admin')).toEqual(['g-model-access']);
    expect(getOpenKeysForPath('/', 'org_viewer')).toEqual([]);
    expect(getOpenKeysForPath('/notifications', 'platform_admin')).toHaveLength(1);
    expect(getOpenKeysForPath('/audit-logs', 'platform_admin')).toEqual(['g-access-security']);
  });

  it('keeps account settings avatar-only while allowing every authenticated role to open it', () => {
    Object.keys(expectedRoutes).forEach(role => {
      expect(routeKeys(role)).not.toContain('/settings');
      expect(isRouteAllowedForRole('/settings', role)).toBe(true);
    });
  });

  it('lands organization viewers on a usable read-only overview', () => {
    expect(getDefaultPathForRole('org_viewer')).toBe('/');
    expect(routeKeys('org_viewer')).toContain('/');
    expect(isRouteAllowedForRole('/', 'org_viewer')).toBe(true);
    expect(isRouteAllowedForRole('/developer', 'org_viewer')).toBe(false);
  });

  it('shows request-log search only to roles that can open request logs', () => {
    ['admin', 'platform_admin', 'org_admin', 'billing_admin'].forEach(role => {
      expect(canSearchRequestLogs(role)).toBe(true);
    });
    ['project_admin', 'developer', 'viewer', 'org_viewer', 'owner'].forEach(role => {
      expect(canSearchRequestLogs(role)).toBe(false);
    });
  });

  it('keeps platform-only controls out of tenant roles', () => {
    expect(isRouteAllowedForRole('/license', 'platform_admin')).toBe(true);
    expect(isRouteAllowedForRole('/data-exchange', 'platform_admin')).toBe(true);
    expect(isRouteAllowedForRole('/governance-simulation', 'platform_admin')).toBe(true);
    expect(isRouteAllowedForRole('/risk-control', 'platform_admin')).toBe(true);
    expect(isRouteAllowedForRole('/risk-control', 'org_admin')).toBe(true);
    expect(isRouteAllowedForRole('/member-lifecycle', 'platform_admin')).toBe(true);
    expect(isRouteAllowedForRole('/member-lifecycle', 'org_admin')).toBe(true);
    expect(isRouteAllowedForRole('/project-management', 'project_admin')).toBe(true);
    expect(isRouteAllowedForRole('/organization-members', 'project_admin')).toBe(true);
    expect(isRouteAllowedForRole('/agent-trust', 'platform_admin')).toBe(true);
    expect(isRouteAllowedForRole('/agent-trust', 'org_admin')).toBe(true);
    expect(isRouteAllowedForRole('/provenance-passport', 'platform_admin')).toBe(true);
    expect(isRouteAllowedForRole('/provenance-passport', 'org_admin')).toBe(true);
    expect(isRouteAllowedForRole('/license', 'org_admin')).toBe(false);
    expect(isRouteAllowedForRole('/data-exchange', 'org_admin')).toBe(false);
    expect(isRouteAllowedForRole('/governance-simulation', 'org_admin')).toBe(false);
    expect(isRouteAllowedForRole('/risk-control', 'billing_admin')).toBe(false);
    expect(isRouteAllowedForRole('/member-lifecycle', 'billing_admin')).toBe(false);
    expect(isRouteAllowedForRole('/agent-trust', 'billing_admin')).toBe(false);
    expect(isRouteAllowedForRole('/provenance-passport', 'billing_admin')).toBe(false);
  });

  it('enforces the direct-route contract for the role-specific project workbench', () => {
    ['admin', 'platform_admin', 'org_admin', 'billing_admin', 'org_viewer', 'owner'].forEach(role => {
      expect(isRouteAllowedForRole('/developer', role), `${role} hidden project workbench`).toBe(false);
    });
    ['project_admin', 'developer', 'viewer'].forEach(role => {
      expect(isRouteAllowedForRole('/developer', role), `${role} project workbench`).toBe(true);
    });
  });

  it('rejects unknown routes for every role so the router can return users to their role home', () => {
    Object.keys(expectedRoutes).forEach(role => {
      expect(isRouteAllowedForRole('/unknown-route', role), role).toBe(false);
    });
  });

  it('requires a valid project scope for project-admin notification routes', () => {
    expect(isRouteAllowedForRole('/notifications?project_id=7', 'project_admin')).toBe(true);
    expect(isRouteAllowedForRole('/notifications?source=workbench&project_id=7', 'project_admin')).toBe(true);

    [
      '/notifications',
      '/notifications?project_id=',
      '/notifications?project_id=0',
      '/notifications?project_id=-1',
      '/notifications?project_id=1.5',
      '/notifications?project_id=project-1',
      '/notifications?project_id=9007199254740992',
    ].forEach(path => {
      expect(isRouteAllowedForRole(path, 'project_admin'), path).toBe(false);
    });

    ['admin', 'platform_admin', 'org_admin', 'billing_admin'].forEach(role => {
      expect(isRouteAllowedForRole('/notifications', role), `${role} unscoped notifications`).toBe(true);
    });
  });

  it('keeps billing and project-admin navigation at their minimum-authority surfaces', () => {
    ['/limit-policies', '/guardrail-policies', '/status', '/audit-logs'].forEach(route => {
      expect(routeKeys('billing_admin')).not.toContain(route);
      expect(isRouteAllowedForRole(route, 'billing_admin')).toBe(false);
    });
    expect(routeKeys('billing_admin')).toContain('/model-prices');
    expect(isRouteAllowedForRole('/model-prices', 'billing_admin')).toBe(true);
    expect(canReadRuntimeStatus('billing_admin')).toBe(false);
    expect(canReadRuntimeStatus('org_admin')).toBe(true);

    expect(routeKeys('project_admin')).not.toContain('/status');
    expect(isRouteAllowedForRole('/status', 'project_admin')).toBe(false);
    expect(routeKeys('project_admin')).toContain('/developer');
    expect(routeKeys('project_admin')).toContain('/organization-members');
    expect(routeKeys('project_admin')).not.toContain('/project-management');
    expect(isRouteAllowedForRole('/project-management', 'project_admin')).toBe(true);
    expect(routeKeys('project_admin')).not.toContain('/notifications');
    expect(isRouteAllowedForRole('/notifications?project_id=7', 'project_admin')).toBe(true);
    expect(canOpenUnscopedNotifications('project_admin')).toBe(false);
    expect(canOpenUnscopedNotifications('org_admin')).toBe(true);
  });

  it('keeps legacy pricing and grant paths as route-compatible redirects, not duplicate navigation entries', () => {
    ['platform_admin', 'org_admin'].forEach(role => {
      expect(routeKeys(role)).not.toContain('/org-model-prices');
      expect(isRouteAllowedForRole('/org-model-prices', role)).toBe(true);
    });
    ['platform_admin', 'billing_admin'].forEach(role => {
      expect(routeKeys(role)).not.toContain('/grants');
      expect(isRouteAllowedForRole('/grants', role)).toBe(true);
    });
  });

  it('matches current backend key permissions in developer console controls', () => {
    expect(canCreateProjectKey('developer')).toBe(true);
    expect(canDisableProjectKey('developer')).toBe(true);
    expect(canDisableProjectKey('project_admin')).toBe(true);
  });

  it('keeps the X-Insight safety route reachable while locking unrelated licensed routes', () => {
    const partial = licenseAccess('active', true, ['gateway', 'notifications']);
    const platformItems = getNavItemsForRole('platform_admin', partial);
    const platformChildren = platformItems.flatMap(item => item.items || []);

    expect(platformChildren.find(item => item.itemKey === '/test')?.licenseLocked).toBeUndefined();
    expect(platformChildren.find(item => item.itemKey === '/notifications')?.licenseLocked).toBeUndefined();
    expect(platformChildren.find(item => item.itemKey === '/model-governance')).toMatchObject({
      text: '模型治理（授权未包含）',
      licenseLocked: true,
      disabled: true,
    });
    expect(platformChildren.find(item => item.itemKey === '/data-exchange')).toMatchObject({
      text: 'X-Insight 数据交换',
    });
    expect(platformChildren.find(item => item.itemKey === '/data-exchange')?.licenseLocked).toBeUndefined();
    expect(platformChildren.find(item => item.itemKey === '/data-exchange')?.disabled).toBeUndefined();

    expect(routeKeys('billing_admin')).not.toContain('/data-exchange');
    expect(getNavItemsForRole('billing_admin', partial).flatMap(item => item.items || []).some(item => item.itemKey === '/data-exchange')).toBe(false);
    expect(isRouteLicensed('/guardrail-policies', partial)).toBe(false);
    expect(isRouteLicensed('/notifications?project_id=7', partial)).toBe(true);
    expect(isRouteLicensed('/data-exchange', partial)).toBe(true);
  });

  it('treats wildcard projection as all concrete capabilities and fail-closes missing or expired projections', () => {
    const wildcardProjection = licenseAccess('active', true, Object.keys(DENIED_FEATURES) as Array<keyof LicenseAccess['features']>);
    const missing = licenseAccess('missing', false);
    const expired = licenseAccess('expired', false, Object.keys(DENIED_FEATURES) as Array<keyof LicenseAccess['features']>);

    Object.keys(DENIED_FEATURES).forEach(feature => {
      expect(hasLicensedFeature(wildcardProjection, feature as keyof LicenseAccess['features'])).toBe(true);
      expect(hasLicensedFeature(missing, feature as keyof LicenseAccess['features'])).toBe(false);
      expect(hasLicensedFeature(expired, feature as keyof LicenseAccess['features'])).toBe(false);
    });
    expect(licenseRestrictionMessage(missing, 'gateway')).toContain('尚未导入有效商业授权');
    expect(licenseRestrictionMessage(expired, 'notifications')).toContain('已过期');
    expect(isRouteLicensed('/test', expired)).toBe(false);

    // A rolling-upgrade response without license_access is unknown, not a
    // positive entitlement assertion. The backend gate remains authoritative.
    expect(hasLicensedFeature(null, 'gateway')).toBe(true);
  });
});
