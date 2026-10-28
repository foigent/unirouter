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

  it('limits credential-backed discovery to model administrators despite using GET', () => {
    const path = '/api/v1/model-deployments/{deploymentID}/model-discovery';
    for (const role of ['platform_admin', 'org_admin', 'org_viewer', 'member']) {
      const authz = authzContextForUser({ organization_role: role });
      expect(endpointVisibleForAuthz('Admin', 'GET', path, authz, { agentCapabilities: false }))
        .toBe(role === 'platform_admin' || role === 'org_admin');
    }
  });

  it('shows canonical model deployment reads to both organization read roles', () => {
    const organizationAdmin = authzContextForUser({ organization_role: 'org_admin' });
    const organizationViewer = authzContextForUser({ organization_role: 'org_viewer' });
    const projectUser = authzContextForUser(
      { organization_role: 'member' },
      'project_user',
      true,
    );

    for (const authz of [organizationAdmin, organizationViewer]) {
      expect(endpointVisibleForAuthz(
        'Admin',
        'GET',
        '/api/v1/model-deployment-releases',
        authz,
        { agentCapabilities: false },
      )).toBe(true);
      expect(endpointVisibleForAuthz(
        'Admin',
        'GET',
        '/api/v1/model-deployments',
        authz,
        { agentCapabilities: false },
      )).toBe(true);
      expect(endpointVisibleForAuthz(
        'Admin',
        'GET',
        '/api/v1/model-deployments/{deploymentID}',
        authz,
        { agentCapabilities: false },
      )).toBe(true);
    }

    expect(endpointVisibleForAuthz(
      'Admin',
      'POST',
      '/api/v1/model-deployments',
      organizationAdmin,
      { agentCapabilities: false },
    )).toBe(true);

    expect(endpointVisibleForAuthz(
      'Admin',
      'POST',
      '/api/v1/model-deployments',
      organizationViewer,
      { agentCapabilities: false },
    )).toBe(false);
    expect(endpointVisibleForAuthz(
      'Admin',
      'POST',
      '/api/v1/model-deployments/{deploymentID}/operations/{operationID}/probes',
      organizationAdmin,
      { agentCapabilities: false },
    )).toBe(true);
    expect(endpointVisibleForAuthz(
      'Admin',
      'POST',
      '/api/v1/model-deployments/{deploymentID}/operations/{operationID}/probes',
      organizationViewer,
      { agentCapabilities: false },
    )).toBe(false);
    expect(endpointVisibleForAuthz(
      'Admin',
      'GET',
      '/api/v1/model-deployments',
      projectUser,
      { agentCapabilities: false },
    )).toBe(false);

    for (const authz of [organizationAdmin, organizationViewer]) {
      expect(endpointVisibleForAuthz(
        'Admin',
        'GET',
        '/api/v1/model-deployment-releases/{releaseRef}',
        authz,
        { agentCapabilities: false },
      )).toBe(true);
    }
    expect(endpointVisibleForAuthz(
      'Admin',
      'POST',
      '/api/v1/model-deployment-releases',
      organizationAdmin,
      { agentCapabilities: false },
    )).toBe(true);
    expect(endpointVisibleForAuthz(
      'Admin',
      'POST',
      '/api/v1/model-deployment-releases',
      organizationViewer,
      { agentCapabilities: false },
    )).toBe(false);
    for (const action of [
      'activate-initial', 'start-canary', 'promote', 'cancel-canary', 'rollback-stable',
      'cutover-legacy', 'rollback-legacy',
    ]) {
      const path = `/api/v1/model-deployment-releases/{releaseRef}/${action}`;
      expect(endpointVisibleForAuthz(
        'Admin', 'POST', path, organizationAdmin, { agentCapabilities: false },
      )).toBe(true);
      expect(endpointVisibleForAuthz(
        'Admin', 'POST', path, organizationViewer, { agentCapabilities: false },
      )).toBe(false);
    }
    expect(endpointVisibleForAuthz(
      'Admin',
      'POST',
      '/api/v1/model-deployment-releases/{releaseRef}/future-expansion',
      organizationAdmin,
      { agentCapabilities: false },
    )).toBe(false);
  });

  it('shows declaration reads and writes only to the existing deployment roles', () => {
    const path = '/api/v1/model-deployments/{deploymentID}/upstream-release-observation';
    for (const role of ['platform_admin', 'org_admin', 'org_viewer', 'billing_admin', 'member'] as const) {
      const context = authzContextForUser({organization_role: role});
      expect(endpointVisibleForAuthz('Admin','GET',path,context,{agentCapabilities:false})).toBe(['platform_admin','org_admin','org_viewer'].includes(role));
      expect(endpointVisibleForAuthz('Admin','PUT',path,context,{agentCapabilities:false})).toBe(['platform_admin','org_admin'].includes(role));
    }
  });
  it('keeps Responses and Messages production qualification approval platform-only', () => {
    const path = '/api/v1/model-deployments/{deploymentID}/operations/{operationID}/production-approvals';
    expect(endpointVisibleForAuthz(
      'Admin',
      'POST',
      path,
      authzContextForUser({ organization_role: 'platform_admin' }),
      { agentCapabilities: false },
    )).toBe(true);
    for (const organizationRole of ['org_admin', 'org_viewer', 'billing_admin'] as const) {
      expect(endpointVisibleForAuthz(
        'Admin',
        'POST',
        path,
        authzContextForUser({ organization_role: organizationRole }),
        { agentCapabilities: false },
      )).toBe(false);
    }
  });

  it('shows License-independent model route safety actions only to organization admins', () => {
    const organizationAdmin = authzContextForUser({ organization_role: 'org_admin' });
    const organizationViewer = authzContextForUser({ organization_role: 'org_viewer' });
    const billing = authzContextForUser({ organization_role: 'billing_admin' });
    const projectAdmin = authzContextForUser(
      { organization_role: 'member' },
      'project_admin',
      true,
    );
    const safetyEndpoints = [
      '/api/v1/provider-connections/{connectionID}/pause',
      '/api/v1/provider-credential-bindings/{bindingID}/revoke',
    ];

    for (const path of safetyEndpoints) {
      expect(endpointVisibleForAuthz(
        'Admin', 'POST', path, organizationAdmin, { agentCapabilities: false },
      )).toBe(true);
      for (const authz of [organizationViewer, billing, projectAdmin]) {
        expect(endpointVisibleForAuthz(
          'Admin', 'POST', path, authz, { agentCapabilities: false },
        )).toBe(false);
      }
    }
  });
});
