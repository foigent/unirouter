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

  it('keeps full admin routes available to org admins', () => {
    const itemKeys = flattenItemKeys(getNavItemsForRole('org_admin'));

    expect(itemKeys).toContain('/channels');
    expect(itemKeys).toContain('/model-governance');
    expect(itemKeys).toContain('/model-prices');
    expect(itemKeys).not.toContain('/license');
    expect(isRouteAllowedForRole('/model-governance', 'org_admin')).toBe(true);
    expect(isRouteAllowedForRole('/settings', 'org_admin')).toBe(true);
    expect(isRouteAllowedForRole('/license', 'org_admin')).toBe(false);
  });

  it('keeps license management platform-admin only', () => {
    const itemKeys = flattenItemKeys(getNavItemsForRole('platform_admin'));

    expect(itemKeys).toContain('/license');
    expect(isRouteAllowedForRole('/license', 'platform_admin')).toBe(true);
    expect(isRouteAllowedForRole('/license', 'admin')).toBe(true);
  });

  it('matches current backend key permissions in developer console controls', () => {
    expect(canCreateProjectKey('developer')).toBe(true);
    expect(canDisableProjectKey('developer')).toBe(false);
    expect(canDisableProjectKey('project_admin')).toBe(true);
  });
});
