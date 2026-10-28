import {
  type AuthzContext,
  isBillingRole,
  isOrgAdminRole,
  isOrgViewerRole,
  isPlatformAdminRole,
} from '../../navigation';

export type EndpointContract = readonly [method: string, path: string, description: string];

export interface EndpointGroup {
  area: string;
  endpoints: readonly EndpointContract[];
}

export interface ApiReferenceFeatures {
  agentCapabilities: boolean;
}

const declaredAreas = new Set([
  'Auth',
  'Platform',
  'Gateway',
  'Async Media',
  'Private Integration',
  'Access Provisioning',
  '项目工作台',
  '产品与运行状态',
  '成本优化',
  'Admin',
  'Model Governance',
  'Notifications',
]);

const organizationModelGovernanceWrites = new Set([
  'POST /api/v1/model-registry',
  'PUT /api/v1/model-registry/{id}',
  'POST /api/v1/model-aliases',
  'PUT /api/v1/model-aliases/{id}',
  'POST /api/v1/model-sets',
  'PUT /api/v1/model-sets/{id}/items',
  'POST /api/v1/invocation-optimization/policies',
  'PUT /api/v1/invocation-optimization/policies/{ref}',
  'POST /api/v1/invocation-optimization/policies/{ref}/validate',
  'POST /api/v1/invocation-optimization/policies/{ref}/shadow',
  'POST /api/v1/invocation-optimization/policies/{ref}/canary',
  'POST /api/v1/invocation-optimization/policies/{ref}/rollback',
]);

const organizationModelRouteSafetyWrites = new Set([
  'POST /api/v1/provider-connections/{connectionID}/pause',
  'POST /api/v1/provider-credential-bindings/{bindingID}/revoke',
]);

const organizationModelDeploymentLifecycleWrites = new Set([
  'POST /api/v1/model-deployment-releases/{releaseRef}/activate-initial',
  'POST /api/v1/model-deployment-releases/{releaseRef}/activate-successor',
  'POST /api/v1/model-deployment-releases/{releaseRef}/activate-staging-successor',
  'POST /api/v1/model-deployment-releases/{releaseRef}/start-canary',
  'POST /api/v1/model-deployment-releases/{releaseRef}/promote',
  'POST /api/v1/model-deployment-releases/{releaseRef}/cancel-canary',
  'POST /api/v1/model-deployment-releases/{releaseRef}/rollback-stable',
  'POST /api/v1/model-deployment-releases/{releaseRef}/cutover-legacy',
  'POST /api/v1/model-deployment-releases/{releaseRef}/rollback-legacy',
]);

function isAgentCapabilityPath(path: string) {
  return path.startsWith('/v1/agent-capabilities/');
}

export function endpointVisibleForAuthz(
  area: string,
  method: string,
  path: string,
  authz: AuthzContext,
  features: ApiReferenceFeatures,
) {
  const organizationRole = authz.organizationRole;
  const membershipRole = authz.projectMembershipRole;
  const hasProjectRead = Boolean(membershipRole);
  const hasProjectWrite = membershipRole === 'project_admin' || membershipRole === 'project_user';
  const hasProjectAdmin = membershipRole === 'project_admin';

  if (!declaredAreas.has(area)) return false;
  if (isAgentCapabilityPath(path) && !features.agentCapabilities) return false;
  if (isPlatformAdminRole(organizationRole)) return true;

  if (area === 'Auth') {
    return ['/api/v1/auth/logout', '/api/v1/me', '/api/v1/me/password'].includes(path);
  }
  if (area === 'Platform') {
    return isOrgAdminRole(organizationRole) || isBillingRole(organizationRole) || isOrgViewerRole(organizationRole);
  }
  if (area === 'Gateway') {
    if (isAgentCapabilityPath(path)) return isOrgAdminRole(organizationRole) || hasProjectAdmin;
    return Boolean(organizationRole) || hasProjectRead;
  }
  if (area === 'Private Integration') return false;
  if (area === '成本优化') return isOrgAdminRole(organizationRole) || isBillingRole(organizationRole);
  if (area === 'Async Media') {
    if (path.startsWith('/v1/')) return isOrgAdminRole(organizationRole) || hasProjectRead;
    if (isOrgAdminRole(organizationRole)) return true;
    return method === 'GET' ? hasProjectRead : hasProjectWrite;
  }
  if (area === 'Access Provisioning') {
    if (path.endsWith('/legacy-preview')) return isOrgAdminRole(organizationRole);
    if (path.includes('/credentials')) {
      return isOrgAdminRole(organizationRole) || hasProjectAdmin || membershipRole === 'project_user';
    }
    if (path.endsWith('/decision') || path.endsWith('/suspend') || path.endsWith('/resume') || path.endsWith('/revoke')) {
      return isOrgAdminRole(organizationRole) || hasProjectAdmin;
    }
    return Boolean(organizationRole);
  }
  if (area === '项目工作台') {
    const isMemberPath = path.includes('/members');
    const isBudgetPath = path.endsWith('/budget');
    if (isBillingRole(organizationRole) && isBudgetPath) return true;
    if (isMemberPath || isBudgetPath) return isOrgAdminRole(organizationRole) || hasProjectAdmin;
    if (isOrgAdminRole(organizationRole) || hasProjectAdmin) return true;
    if (membershipRole === 'project_user') {
      return method === 'GET' || (method === 'POST' && (
        path === '/api/v1/keys' ||
        path === '/api/v1/keys/{id}/disable' ||
        path === '/v1/chat/completions'
      ));
    }
    if (membershipRole === 'project_viewer') return method === 'GET';
    if (isBillingRole(organizationRole)) return method === 'GET' && path === '/api/v1/projects';
    return false;
  }
  if (area === '产品与运行状态') {
    if (path.endsWith('/error-docs') || path.endsWith('/usage/analytics')) {
      return isOrgAdminRole(organizationRole) || isBillingRole(organizationRole) || isOrgViewerRole(organizationRole) || hasProjectRead;
    }
    if (path.endsWith('/internal-status') || path.endsWith('/runtime/status')) return isOrgAdminRole(organizationRole);
    return false;
  }
  if (area === 'Notifications') {
    if (isBillingRole(organizationRole)) return !path.includes('/notification-webhooks');
    if (isOrgAdminRole(organizationRole)) return true;
    if (path.includes('/notification-webhooks')) return method === 'GET' ? hasProjectRead : hasProjectAdmin;
    return method === 'GET' ? hasProjectRead : hasProjectAdmin;
  }
  if (area === 'Model Governance') {
    if (isOrgAdminRole(organizationRole)) {
      if (method === 'GET') return !path.includes('/provider-cost/') && !path.endsWith('/reconciliation');
      return organizationModelGovernanceWrites.has(`${method} ${path}`);
    }
    if (isBillingRole(organizationRole)) {
      return method === 'GET' && (
        path.includes('/model-prices') || path.includes('/model-names') ||
        path.includes('/org-model-prices') || path.endsWith('/reconciliation') ||
        path.endsWith('/provider-cogs/offline-contract')
      ) || (method === 'POST' && path.endsWith('/provider-cogs/offline-preview'));
    }
    if (isOrgViewerRole(organizationRole)) {
      return method === 'GET' && (path === '/api/v1/model-catalog' || path === '/api/v1/model-sets');
    }
    if (hasProjectRead) return method === 'GET' && path === '/api/v1/model-sets';
    return false;
  }
  if (area === 'Admin') {
    const isCanonicalModelDeploymentRead = method === 'GET' && (
      path === '/api/v1/model-deployments' ||
      path === '/api/v1/model-deployments/{deploymentID}' ||
      path === '/api/v1/model-deployments/{deploymentID}/upstream-release-observation' ||
      path === '/api/v1/model-deployment-releases' ||
      path === '/api/v1/model-deployment-releases/{releaseRef}'
    );
    if (isOrgViewerRole(organizationRole)) {
      return isCanonicalModelDeploymentRead || (
        method === 'GET' && (path.endsWith('/dashboard/stats') || path.includes('/projects'))
      );
    }
    if (isOrgAdminRole(organizationRole)) {
      const isUpstreamDeclarationWrite = method === 'PUT' && path === '/api/v1/model-deployments/{deploymentID}/upstream-release-observation';
      const isCanonicalModelDiscovery = method === 'GET' && path === '/api/v1/model-deployments/{deploymentID}/model-discovery';
      const isCanonicalModelDeploymentCreate = method === 'POST' && path === '/api/v1/model-deployments';
      const isCanonicalModelDeploymentReleaseAuthor = method === 'POST' &&
        path === '/api/v1/model-deployment-releases';
      const isCanonicalModelDeploymentProbe = method === 'POST' &&
        path === '/api/v1/model-deployments/{deploymentID}/operations/{operationID}/probes';
      const isCanonicalModelDeploymentLifecycleWrite =
        organizationModelDeploymentLifecycleWrites.has(`${method} ${path}`);
      const isModelRouteSafetyWrite = organizationModelRouteSafetyWrites.has(`${method} ${path}`);
      return isCanonicalModelDeploymentRead || isUpstreamDeclarationWrite || isCanonicalModelDiscovery || isCanonicalModelDeploymentCreate ||
        isCanonicalModelDeploymentReleaseAuthor || isCanonicalModelDeploymentProbe ||
        isCanonicalModelDeploymentLifecycleWrite || isModelRouteSafetyWrite || [
        '/dashboard/stats', '/keys', '/limit-policies', '/guardrail-policies',
        '/projects', '/logs', '/users', '/audit-logs',
      ].some(fragment => path.includes(fragment)) && !path.includes('/wallet');
    }
    if (isBillingRole(organizationRole)) {
      return ['/dashboard/stats', '/logs', '/wallet', '/grants'].some(fragment => path.includes(fragment));
    }
    if (hasProjectRead) return method === 'GET' && ['/keys', '/projects', '/logs'].some(fragment => path.includes(fragment));
    return false;
  }

  // A newly added API area is invisible until its audience is explicitly
  // declared here. This avoids leaking control-plane inventory through a
  // permissive presentation fallback; backend authorization remains the gate.
  return false;
}

export function visibleEndpointGroups(
  groups: readonly EndpointGroup[],
  authz: AuthzContext,
  features: ApiReferenceFeatures,
) {
  return groups
    .map(group => ({
      ...group,
      endpoints: group.endpoints.filter(([method, path]) => (
        endpointVisibleForAuthz(group.area, method, path, authz, features)
      )),
    }))
    .filter(group => group.endpoints.length > 0);
}
