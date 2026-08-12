import { describe, expect, it } from 'vitest';
import { effectiveCustomerOrgID, resolveCustomerContext } from '../customerContext';

describe('customer context', () => {
  it('uses the canonical ready customer instead of the platform technical organization', () => {
    const user = {
      id: 1,
      username: 'admin',
      role: 'platform_admin',
      org_id: 1,
      org_name: 'Platform Admin',
      customer_context: { state: 'ready' as const, org_id: 9, org_name: 'Acme' },
    };

    expect(resolveCustomerContext(user)).toEqual({ state: 'ready', orgID: 9, orgName: 'Acme' });
    expect(effectiveCustomerOrgID(user)).toBe(9);
  });

  it('does not fall back to a platform administrator org when the contract is unavailable', () => {
    const user = { id: 1, username: 'admin', role: 'platform_admin', org_id: 1 };

    expect(resolveCustomerContext(user)).toEqual({ state: 'unavailable' });
    expect(effectiveCustomerOrgID(user)).toBeUndefined();
  });

  it('preserves the authenticated tenant fallback for a customer identity during rolling upgrade', () => {
    const user = { id: 2, username: 'org-admin', role: 'org_admin', org_id: 7, org_name: 'Customer' };

    expect(resolveCustomerContext(user)).toEqual({ state: 'ready', orgID: 7, orgName: 'Customer' });
  });

  it('fails closed when ready lacks a valid organization id', () => {
    const user = {
      id: 1,
      username: 'admin',
      role: 'platform_admin',
      customer_context: { state: 'ready' as const },
    };

    expect(resolveCustomerContext(user)).toEqual({ state: 'unavailable' });
  });

  it('preserves inactive as a distinct blocked state', () => {
    const user = {
      id: 1,
      username: 'admin',
      role: 'platform_admin',
      customer_context: { state: 'inactive' as const, org_id: 9, org_name: 'Acme' },
    };

    expect(resolveCustomerContext(user)).toEqual({ state: 'inactive', orgID: 9, orgName: 'Acme' });
    expect(effectiveCustomerOrgID(user)).toBeUndefined();
  });

  it('does not override an explicit unavailable projection with the authenticated org', () => {
    const user = {
      id: 2,
      username: 'org-admin',
      role: 'org_admin',
      org_id: 7,
      customer_context: { state: 'unavailable' as const },
    };

    expect(resolveCustomerContext(user)).toEqual({ state: 'unavailable' });
  });
});
