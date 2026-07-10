import { describe, expect, it } from 'vitest';
import {
  canCreateProjectKey,
  canDisableProjectKey,
  getDefaultPathForRole,
  getNavItemsForRole,
  isRouteAllowedForRole,
} from '../navigation';

function flattenItemKeys(items: any[]): string[] {
  return items.flatMap(item => [
    item.itemKey,
    ...((item.items || []).map((child: any) => child.itemKey)),
  ]);
}

describe('navigation role boundaries', () => {
  it('keeps visible navigation routes allowed by the route guard for every current role', () => {
    const roles = [
      'admin',
      'platform_admin',
      'org_admin',
      'billing_admin',
      'project_admin',
      'developer',
      'viewer',
      'org_viewer',
      'owner',
    ];

    roles.forEach((role) => {
      const itemKeys = flattenItemKeys(getNavItemsForRole(role));
      const routeKeys = itemKeys.filter((key) => key.startsWith('/'));
      const defaultPath = getDefaultPathForRole(role);

      routeKeys.forEach((route) => {
        expect(isRouteAllowedForRole(route, role), `${role} nav route ${route}`).toBe(true);
      });
      expect(isRouteAllowedForRole(defaultPath, role), `${role} default path ${defaultPath}`).toBe(true);
      expect(routeKeys, `${role} default path should be visible`).toContain(defaultPath);
    });
  });

  it('shows developer console navigation for project roles', () => {
    const itemKeys = flattenItemKeys(getNavItemsForRole('developer'));

    expect(itemKeys).toContain('/developer');
    expect(itemKeys).toContain('/docs');
    expect(itemKeys).not.toContain('/channels');
    expect(itemKeys).not.toContain('/channel-keys');
    expect(itemKeys).not.toContain('/model-governance');
    expect(itemKeys).not.toContain('/license');
  });

  it('blocks admin routes for project-scoped roles', () => {
    expect(isRouteAllowedForRole('/developer', 'developer')).toBe(true);
    expect(isRouteAllowedForRole('/docs', 'developer')).toBe(true);
    expect(isRouteAllowedForRole('/channels', 'developer')).toBe(false);
    expect(getDefaultPathForRole('developer')).toBe('/developer');
  });

  it('allows project admins to open backend-supported project status surfaces', () => {
    const itemKeys = flattenItemKeys(getNavItemsForRole('project_admin'));

    expect(itemKeys).toContain('/developer');
    expect(itemKeys).toContain('/notifications');
    expect(itemKeys).toContain('/status');
    expect(isRouteAllowedForRole('/notifications', 'project_admin')).toBe(true);
    expect(isRouteAllowedForRole('/status', 'project_admin')).toBe(true);
    expect(isRouteAllowedForRole('/notifications', 'developer')).toBe(false);
  });

  it('aligns org admin navigation with backend org-admin routes', () => {
    const itemKeys = flattenItemKeys(getNavItemsForRole('org_admin'));

    expect(itemKeys).not.toContain('/channels');
    expect(itemKeys).not.toContain('/channel-keys');
    expect(itemKeys).not.toContain('/test');
    expect(itemKeys).not.toContain('/wallet');
    expect(itemKeys).not.toContain('/reconciliation');
    expect(itemKeys).not.toContain('/grants');
    expect(itemKeys).toContain('/model-governance');
    expect(itemKeys).toContain('/notifications');
    expect(itemKeys).toContain('/usage-analytics');
    expect(itemKeys).toContain('/status');
    expect(itemKeys).toContain('/model-prices');
    expect(itemKeys).not.toContain('/license');
    expect(isRouteAllowedForRole('/channels', 'org_admin')).toBe(false);
    expect(isRouteAllowedForRole('/wallet', 'org_admin')).toBe(false);
    expect(isRouteAllowedForRole('/grants', 'org_admin')).toBe(false);
    expect(isRouteAllowedForRole('/model-governance', 'org_admin')).toBe(true);
    expect(isRouteAllowedForRole('/notifications', 'org_admin')).toBe(true);
    expect(isRouteAllowedForRole('/settings', 'org_admin')).toBe(true);
    expect(isRouteAllowedForRole('/license', 'org_admin')).toBe(false);
  });

  it('allows billing admins to inspect notification delivery state', () => {
    const itemKeys = flattenItemKeys(getNavItemsForRole('billing_admin'));

    expect(itemKeys).toContain('/notifications');
    expect(itemKeys).toContain('/usage-analytics');
    expect(itemKeys).toContain('/status');
    expect(isRouteAllowedForRole('/notifications', 'billing_admin')).toBe(true);
    expect(isRouteAllowedForRole('/usage-analytics', 'billing_admin')).toBe(true);
    expect(isRouteAllowedForRole('/status', 'billing_admin')).toBe(true);
  });

  it('keeps license management platform-admin only', () => {
    const itemKeys = flattenItemKeys(getNavItemsForRole('platform_admin'));

    expect(itemKeys).toContain('/license');
    expect(isRouteAllowedForRole('/license', 'platform_admin')).toBe(true);
    expect(isRouteAllowedForRole('/license', 'admin')).toBe(true);
  });

  it('keeps data exchange control platform-admin only', () => {
    const platformKeys = flattenItemKeys(getNavItemsForRole('platform_admin'));
    const orgKeys = flattenItemKeys(getNavItemsForRole('org_admin'));

    expect(platformKeys).toContain('/data-exchange');
    expect(orgKeys).not.toContain('/data-exchange');
    expect(isRouteAllowedForRole('/data-exchange', 'platform_admin')).toBe(true);
    expect(isRouteAllowedForRole('/data-exchange', 'org_admin')).toBe(false);
  });

  it('does not use API docs as the default landing for org viewers or unknown roles', () => {
    const orgViewerKeys = flattenItemKeys(getNavItemsForRole('org_viewer'));
    const unknownKeys = flattenItemKeys(getNavItemsForRole('owner'));

    expect(getDefaultPathForRole('org_viewer')).toBe('/access');
    expect(getDefaultPathForRole('owner')).toBe('/access');
    expect(orgViewerKeys).toContain('/access');
    expect(unknownKeys).toContain('/access');
    expect(isRouteAllowedForRole('/access', 'org_viewer')).toBe(true);
    expect(isRouteAllowedForRole('/developer', 'org_viewer')).toBe(false);
  });

  it('matches current backend key permissions in developer console controls', () => {
    expect(canCreateProjectKey('developer')).toBe(true);
    expect(canDisableProjectKey('developer')).toBe(false);
    expect(canDisableProjectKey('project_admin')).toBe(true);
  });

});
