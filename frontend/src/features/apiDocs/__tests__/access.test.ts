import { describe, expect, it } from 'vitest';
import { authzContextForUser } from '../../../navigation';
import { endpointVisibleForAuthz, visibleEndpointGroups } from '../access';

describe('API reference audience projection', () => {
  it('fails closed for an undeclared API area', () => {
    const member = authzContextForUser(
      { organization_role: 'member' },
      'project_user',
      true,
    );

    expect(endpointVisibleForAuthz(
      'Future Control Plane',
      'GET',
      '/api/v1/future-control-plane',
      member,
      { agentCapabilities: true },
    )).toBe(false);
    expect(endpointVisibleForAuthz(
      'Future Control Plane',
      'GET',
      '/api/v1/future-control-plane',
      authzContextForUser({ organization_role: 'platform_admin' }),
      { agentCapabilities: true },
    )).toBe(false);
  });

  it('keeps private integration inventory platform-only', () => {
    const platform = authzContextForUser({ organization_role: 'platform_admin' });
    const organizationAdmin = authzContextForUser({ organization_role: 'org_admin' });

    expect(endpointVisibleForAuthz(
      'Private Integration',
      'GET',
      '/api/v1/integrations/xinsight/completed-executions',
      platform,
      { agentCapabilities: false },
    )).toBe(true);
    expect(endpointVisibleForAuthz(
      'Private Integration',
      'GET',
      '/api/v1/integrations/xinsight/completed-executions',
      organizationAdmin,
      { agentCapabilities: false },
    )).toBe(false);
  });

  it('does not advertise Agent endpoints without the Agent capability license', () => {
    const platform = authzContextForUser({ organization_role: 'platform_admin' });
    const groups = [{
      area: 'Gateway',
      endpoints: [
        ['GET', '/v1/models', 'models'],
        ['POST', '/v1/agent-capabilities/exchange', 'exchange'],
      ] as const,
    }];

    expect(visibleEndpointGroups(groups, platform, { agentCapabilities: false })[0].endpoints)
      .toEqual([['GET', '/v1/models', 'models']]);
    expect(visibleEndpointGroups(groups, platform, { agentCapabilities: true })[0].endpoints)
      .toHaveLength(2);
  });
});
