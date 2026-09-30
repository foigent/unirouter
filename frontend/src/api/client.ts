import { apiPath, appendParams, request as typedRequest, requestBlob } from './transport';

export { ApiError } from './transport';

type JsonObject = Record<string, unknown>;

// Compatibility boundary for domains not yet migrated to a named API module.
// Keep this private: new call sites must use typedRequest<T> in a domain client.
type LegacyApiResponse = any;
const legacyRequest = (path: string, options?: RequestInit) =>
  typedRequest<LegacyApiResponse>(path, options);

export const api = {
  // Auth
  login: (username: string, password: string) =>
    legacyRequest('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  logout: () => legacyRequest('/auth/logout', { method: 'POST' }),
  getOIDCConfig: () => legacyRequest('/auth/oidc/config'),
  getOIDCStartURL: (redirect = '/') =>
    apiPath(`/auth/oidc/start?redirect=${encodeURIComponent(redirect)}`),

  // Product-local invocation optimization. Policies always require explicit
  // request opt-in; only an evidence-gated canary stage may affect execution.
  getInvocationOptimizationContract: () => legacyRequest('/invocation-optimization/contract'),
  getInvocationOptimizationPolicies: (orgId?: number) =>
    legacyRequest(appendParams('/invocation-optimization/policies', { org_id: orgId })),
  getInvocationOptimizationPolicyReadiness: (policyRef: string, orgId?: number) =>
    legacyRequest(appendParams(`/invocation-optimization/policies/${encodeURIComponent(policyRef)}/readiness`, { org_id: orgId })),
  createInvocationOptimizationPolicy: (data: JsonObject) =>
    legacyRequest('/invocation-optimization/policies', { method: 'POST', body: JSON.stringify(data) }),
  updateInvocationOptimizationPolicy: (policyRef: string, data: JsonObject) =>
    legacyRequest(`/invocation-optimization/policies/${encodeURIComponent(policyRef)}`, { method: 'PUT', body: JSON.stringify(data) }),
  transitionInvocationOptimizationPolicy: (
    policyRef: string,
    action: 'validate' | 'shadow' | 'canary' | 'rollback',
    expectedRevision: number,
    reason: string,
  ) => legacyRequest(`/invocation-optimization/policies/${encodeURIComponent(policyRef)}/${action}`, {
    method: 'POST',
    body: JSON.stringify({ expected_revision: expectedRevision, reason }),
  }),

  // Keys
  getKeys: (page = 1, pageSize = 20, filters?: { project_id?: number }) =>
    legacyRequest(appendParams('/keys', { page, page_size: pageSize, project_id: filters?.project_id })),
  createKey: (data: JsonObject) =>
    legacyRequest('/keys', { method: 'POST', body: JSON.stringify(data) }),
  updateKey: (id: number, data: JsonObject) =>
    legacyRequest(`/keys/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  disableKey: (id: number) =>
    legacyRequest(`/keys/${id}/disable`, { method: 'POST' }),

  // Product-local workload/application identity. This surface composes model
  // execution authority only; it never grants Tool or MCP capabilities.
  getWorkloadApplicationContract: () => legacyRequest('/workload-applications/contract'),
  getWorkloadApplications: (filters?: { org_id?: number; project_id?: number; status?: string }) =>
    legacyRequest(appendParams('/workload-applications', {
      org_id: filters?.org_id,
      project_id: filters?.project_id,
      status: filters?.status,
    })),
  getWorkloadApplication: (applicationRef: string, orgId?: number) =>
    legacyRequest(appendParams(`/workload-applications/${encodeURIComponent(applicationRef)}`, { org_id: orgId })),
  createWorkloadApplication: (data: JsonObject) =>
    legacyRequest('/workload-applications', { method: 'POST', body: JSON.stringify(data) }),
  updateWorkloadApplication: (applicationRef: string, data: JsonObject) =>
    legacyRequest(`/workload-applications/${encodeURIComponent(applicationRef)}`, { method: 'PUT', body: JSON.stringify(data) }),
  archiveWorkloadApplication: (applicationRef: string, data: { org_id?: number; expected_revision: number }) =>
    legacyRequest(`/workload-applications/${encodeURIComponent(applicationRef)}/archive`, { method: 'POST', body: JSON.stringify(data) }),
  getWorkloadApplicationCredentials: (applicationRef: string, orgId?: number, page = 1, pageSize = 20) =>
    legacyRequest(appendParams(`/workload-applications/${encodeURIComponent(applicationRef)}/credentials`, {
      org_id: orgId,
      page,
      page_size: pageSize,
    })),
  bindWorkloadApplicationCredential: (applicationRef: string, keyId: number, data: { org_id?: number; expected_revision: number }) =>
    legacyRequest(`/workload-applications/${encodeURIComponent(applicationRef)}/credentials/${keyId}/bind`, { method: 'POST', body: JSON.stringify(data) }),
  getWorkloadApplicationReadiness: (applicationRef: string, filters?: { org_id?: number; model?: string; endpoint?: string }) =>
    legacyRequest(appendParams(`/workload-applications/${encodeURIComponent(applicationRef)}/readiness`, {
      org_id: filters?.org_id,
      model: filters?.model,
      endpoint: filters?.endpoint,
    })),

  // Desired deployment/network state. The API stores no TLS private keys and
  // does not apply or restart runtime infrastructure.
  getDeploymentNetworkContract: () => legacyRequest('/deployment/contract'),
  getDeploymentProfiles: () => legacyRequest('/deployment/profiles'),
  createDeploymentProfile: (data: JsonObject) =>
    legacyRequest('/deployment/profiles', { method: 'POST', body: JSON.stringify(data) }),
  updateDeploymentProfile: (profileRef: string, data: JsonObject) =>
    legacyRequest(`/deployment/profiles/${encodeURIComponent(profileRef)}`, { method: 'PUT', body: JSON.stringify(data) }),
  getGatewayEndpoints: (deploymentProfileRef?: string) =>
    legacyRequest(appendParams('/deployment/endpoints', { deployment_profile_ref: deploymentProfileRef })),
  createGatewayEndpoint: (data: JsonObject) =>
    legacyRequest('/deployment/endpoints', { method: 'POST', body: JSON.stringify(data) }),
  updateGatewayEndpoint: (endpointRef: string, data: JsonObject) =>
    legacyRequest(`/deployment/endpoints/${encodeURIComponent(endpointRef)}`, { method: 'PUT', body: JSON.stringify(data) }),

  // Shared RPM/RPS/TPM/concurrency policies
  getLimitPolicies: (page = 1, pageSize = 20, filters?: { org_id?: number; mode?: string }) =>
    legacyRequest(appendParams('/limit-policies', { page, page_size: pageSize, org_id: filters?.org_id, mode: filters?.mode })),
  createLimitPolicy: (data: JsonObject) =>
    legacyRequest('/limit-policies', { method: 'POST', body: JSON.stringify(data) }),
  updateLimitPolicy: (id: number, data: JsonObject) =>
    legacyRequest(`/limit-policies/${id}`, { method: 'PUT', body: JSON.stringify(data) }),

  // Versioned input/non-stream output guardrail policies
  getGuardrailPolicies: (page = 1, pageSize = 20, filters?: { stage?: string }) =>
    legacyRequest(appendParams('/guardrail-policies', { page, page_size: pageSize, stage: filters?.stage })),
  createGuardrailPolicy: (data: JsonObject) =>
    legacyRequest('/guardrail-policies', { method: 'POST', body: JSON.stringify(data) }),
  updateGuardrailPolicy: (id: number, data: JsonObject) =>
    legacyRequest(`/guardrail-policies/${id}`, { method: 'PUT', body: JSON.stringify(data) }),

  // Deployment-wide input blocking words (platform admin only)
  getSensitiveWords: () => legacyRequest('/sensitive-words'),
  replaceSensitiveWords: (words: string[]) =>
    legacyRequest('/sensitive-words', { method: 'PUT', body: JSON.stringify({ words }) }),

  // Wallet
  getWallet: (orgId?: number) => legacyRequest(appendParams('/wallet', { org_id: orgId })),
  getWalletTransactions: (page = 1, pageSize = 20, orgId?: number) =>
    legacyRequest(appendParams('/wallet/transactions', { page, page_size: pageSize, org_id: orgId })),
  exportWalletTransactions: (orgId?: number) =>
    legacyRequest(appendParams('/wallet/transactions/export', { org_id: orgId })),
  topUp: (data: { org_id?: number; amount_cents: number; description: string; client_request_id: string }) =>
    legacyRequest('/wallet/topup', { method: 'POST', body: JSON.stringify(data) }),
  adjustWallet: (data: { org_id?: number; delta_cents: number; description: string; client_request_id: string }) =>
    legacyRequest('/wallet/adjust', { method: 'POST', body: JSON.stringify(data) }),

  // Logs (with optional filters)
  getLogs: (page = 1, pageSize = 20, filters?: {
    model?: string; provider?: string; status_code?: string;
    start_time?: string; end_time?: string; request_id?: string; client_request_id?: string;
    latency_min?: string; latency_max?: string; search?: string;
    trace_id?: string; workflow_id?: string; agent_id?: string; attempt_id?: string; upstream_request_id?: string;
    evidence_ref?: string; source_event_hash?: string;
    org_id?: number; project_id?: number; usage_source?: string;
  }) => {
    return legacyRequest(appendParams('/logs', {
      page,
      page_size: pageSize,
      model: filters?.model,
      provider: filters?.provider,
      status_code: filters?.status_code,
      start_time: filters?.start_time,
      end_time: filters?.end_time,
      request_id: filters?.request_id,
      client_request_id: filters?.client_request_id,
      trace_id: filters?.trace_id,
      workflow_id: filters?.workflow_id,
      agent_id: filters?.agent_id,
      attempt_id: filters?.attempt_id,
      upstream_request_id: filters?.upstream_request_id,
      evidence_ref: filters?.evidence_ref,
      source_event_hash: filters?.source_event_hash,
      latency_min: filters?.latency_min,
      latency_max: filters?.latency_max,
      search: filters?.search,
      org_id: filters?.org_id,
      project_id: filters?.project_id,
      usage_source: filters?.usage_source,
    }));
  },
  getLogDetail: (id: number, orgId?: number) => legacyRequest(appendParams(`/logs/${id}`, { org_id: orgId })),
  getLogTrace: (id: number, orgId?: number) => legacyRequest(appendParams(`/logs/${id}/trace`, { org_id: orgId })),

  // PRD5 Data Exchange Control (platform admin only)
  getDataExchangeItems: (page = 1, pageSize = 20, status?: string) =>
    legacyRequest(appendParams('/data-exchange/items', { page, page_size: pageSize, status })),
  createDataExchangeItem: (data: JsonObject) =>
    legacyRequest('/data-exchange/items', { method: 'POST', body: JSON.stringify(data) }),
  updateDataExchangeItem: (id: number, data: JsonObject) =>
    legacyRequest(`/data-exchange/items/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  getDataExchanges: (page = 1, pageSize = 20) =>
    legacyRequest(appendParams('/data-exchange/exchanges', { page, page_size: pageSize })),
  createDataExchange: (data: JsonObject) =>
    legacyRequest('/data-exchange/exchanges', { method: 'POST', body: JSON.stringify(data) }),
  getDataExchangeActivity: (id: number) =>
    legacyRequest(`/data-exchange/exchanges/${id}/activity`),
  createDataExchangeDelivery: (id: number, data: JsonObject) =>
    legacyRequest(`/data-exchange/exchanges/${id}/deliveries`, { method: 'POST', body: JSON.stringify(data) }),
  createDataExchangeReceipt: (id: number, data: JsonObject) =>
    legacyRequest(`/data-exchange/exchanges/${id}/receipts`, { method: 'POST', body: JSON.stringify(data) }),
  markDataExchangeArtifactDeleted: (id: number) =>
    legacyRequest(`/data-exchange/exchanges/${id}/artifact-delete`, { method: 'POST' }),
  getXInsightFeedConnection: (projectId: number) =>
    legacyRequest(`/data-exchange/xinsight/connections/${projectId}`),
  installXInsightConnector: (projectId: number, data: { invitation: unknown }) =>
    legacyRequest(`/data-exchange/xinsight/connections/${projectId}/install`, {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  pauseXInsightFeedConnection: (projectId: number) =>
    legacyRequest(`/data-exchange/xinsight/connections/${projectId}/pause`, { method: 'POST' }),
  resumeXInsightFeedConnection: (projectId: number) =>
    legacyRequest(`/data-exchange/xinsight/connections/${projectId}/resume`, { method: 'POST' }),
  createXInsightFeedCredential: (projectId: number, data: {
    allowed_ip: string;
    registration: {
      schema_version: string;
      token_format: string;
      verifier_scheme: string;
      key_prefix: string;
      verifier_sha256: string;
      operation_id: string;
    };
  }) =>
    legacyRequest(`/data-exchange/xinsight/connections/${projectId}/credentials`, { method: 'POST', body: JSON.stringify(data) }),
  activateInitialXInsightFeedCredential: (projectId: number, stagedCredentialId: number) =>
    legacyRequest(`/data-exchange/xinsight/connections/${projectId}/credentials/activate`, {
      method: 'POST',
      body: JSON.stringify({ staged_credential_id: stagedCredentialId }),
    }),
  revokeXInsightFeedCredential: (projectId: number, credentialId: number) =>
    legacyRequest(`/data-exchange/xinsight/connections/${projectId}/credentials/${credentialId}/revoke`, { method: 'POST' }),
  prepareXInsightFeedRotation: (projectId: number, data: {
    registration: {
      schema_version: string;
      token_format: string;
      verifier_scheme: string;
      key_prefix: string;
      verifier_sha256: string;
      operation_id: string;
    };
  }) =>
    legacyRequest(`/data-exchange/xinsight/connections/${projectId}/rotation/prepare`, {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  activateXInsightFeedRotation: (projectId: number, stagedCredentialId: number) =>
    legacyRequest(`/data-exchange/xinsight/connections/${projectId}/rotation/activate`, {
      method: 'POST',
      body: JSON.stringify({ staged_credential_id: stagedCredentialId }),
    }),
  cancelXInsightFeedRotation: (projectId: number, stagedCredentialId: number) =>
    legacyRequest(`/data-exchange/xinsight/connections/${projectId}/rotation/cancel`, {
      method: 'POST',
      body: JSON.stringify({ staged_credential_id: stagedCredentialId }),
    }),

  // PRD2 Batch 23 product surfaces
  getUsageAnalytics: (filters?: {
    group_by?: string; org_id?: string | number; project_id?: string | number; key_id?: string | number;
    owner_user_id?: string | number; key_type?: string; scope?: string;
    model?: string; alias?: string; provider?: string; channel?: string; status?: string;
    usage_source?: string; start_time?: string; end_time?: string; format?: string;
  }) => legacyRequest(appendParams('/usage/analytics', {
    group_by: filters?.group_by,
    org_id: filters?.org_id,
    project_id: filters?.project_id,
    key_id: filters?.key_id,
    owner_user_id: filters?.owner_user_id,
    key_type: filters?.key_type,
    scope: filters?.scope,
    model: filters?.model,
    alias: filters?.alias,
    provider: filters?.provider,
    channel: filters?.channel,
    status: filters?.status,
    usage_source: filters?.usage_source,
    start_time: filters?.start_time,
    end_time: filters?.end_time,
    format: filters?.format,
  })),
  getCostOptimizationSummary: (filters?: {
    org_id?: string | number; project_id?: string | number; model?: string;
    provider?: string; channel?: string; start_time?: string; end_time?: string;
  }) => legacyRequest(appendParams('/cost-optimization/summary', {
    org_id: filters?.org_id,
    project_id: filters?.project_id,
    model: filters?.model,
    provider: filters?.provider,
    channel: filters?.channel,
    start_time: filters?.start_time,
    end_time: filters?.end_time,
  })),
  getCostOptimizationTrend: (filters?: {
    org_id?: string | number; project_id?: string | number; model?: string;
    provider?: string; channel?: string; start_time?: string; end_time?: string;
  }) => legacyRequest(appendParams('/cost-optimization/trend', {
    org_id: filters?.org_id,
    project_id: filters?.project_id,
    model: filters?.model,
    provider: filters?.provider,
    channel: filters?.channel,
    start_time: filters?.start_time,
    end_time: filters?.end_time,
  })),
  getCostOptimizationBreakdown: (filters?: {
    group_by?: string; org_id?: string | number; project_id?: string | number; model?: string;
    provider?: string; channel?: string; start_time?: string; end_time?: string;
  }) => legacyRequest(appendParams('/cost-optimization/breakdown', {
    group_by: filters?.group_by,
    org_id: filters?.org_id,
    project_id: filters?.project_id,
    model: filters?.model,
    provider: filters?.provider,
    channel: filters?.channel,
    start_time: filters?.start_time,
    end_time: filters?.end_time,
  })),
  getCostOptimizationOpportunities: (filters?: {
    group_by?: string; org_id?: string | number; project_id?: string | number; model?: string;
    provider?: string; channel?: string; start_time?: string; end_time?: string;
  }) => legacyRequest(appendParams('/cost-optimization/opportunities', {
    group_by: filters?.group_by,
    org_id: filters?.org_id,
    project_id: filters?.project_id,
    model: filters?.model,
    provider: filters?.provider,
    channel: filters?.channel,
    start_time: filters?.start_time,
    end_time: filters?.end_time,
  })),
  getInternalStatus: (filters?: { org_id?: string | number; project_id?: string | number }) =>
    legacyRequest(appendParams('/internal-status', { org_id: filters?.org_id, project_id: filters?.project_id })),
  getRuntimeStatus: (orgId?: number) => legacyRequest(appendParams('/runtime/status', { org_id: orgId })),
  getErrorDocs: () => legacyRequest('/error-docs'),
  getMediaJobContract: (projectId: number, orgId?: number) =>
    legacyRequest(appendParams('/media-jobs/contract', { project_id: projectId, org_id: orgId })),
  getMediaJobs: (projectId: number, orgId?: number, cursor?: string, limit = 50) =>
    legacyRequest(appendParams('/media-jobs', {
      project_id: projectId,
      org_id: orgId,
      cursor,
      limit,
    })),
  getMediaJob: (jobId: string, projectId: number, orgId?: number) =>
    legacyRequest(appendParams(`/media-jobs/${encodeURIComponent(jobId)}`, {
      project_id: projectId,
      org_id: orgId,
    })),
  cancelMediaJob: (jobId: string, projectId: number, orgId?: number) =>
    legacyRequest(appendParams(`/media-jobs/${encodeURIComponent(jobId)}/cancel`, {
      project_id: projectId,
      org_id: orgId,
    }), { method: 'POST' }),
  getMediaJobContent: (jobId: string, projectId: number, orgId?: number) =>
    requestBlob(appendParams(`/media-jobs/${encodeURIComponent(jobId)}/content`, {
      project_id: projectId,
      org_id: orgId,
    })),

  // Projects and developer catalog
  getProjects: (page = 1, pageSize = 20, filters?: { status?: string; org_id?: number }) =>
    legacyRequest(appendParams('/projects', { page, page_size: pageSize, status: filters?.status, org_id: filters?.org_id })),
  createProject: (data: JsonObject) =>
    legacyRequest('/projects', { method: 'POST', body: JSON.stringify(data) }),
  getProject: (id: number) => legacyRequest(`/projects/${id}`),
  updateProject: (id: number, data: JsonObject) =>
    legacyRequest(`/projects/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  getProjectMembers: (id: number, page = 1, pageSize = 20, query?: string) =>
    legacyRequest(appendParams(`/projects/${id}/members`, { page, page_size: pageSize, q: query })),
  upsertProjectMember: (projectId: number, userId: number, role: string) =>
    legacyRequest(`/projects/${projectId}/members/${userId}`, { method: 'PUT', body: JSON.stringify({ role }) }),
  deleteProjectMember: (projectId: number, userId: number) =>
    legacyRequest(`/projects/${projectId}/members/${userId}`, { method: 'DELETE' }),
  getProjectBudget: (id: number) => legacyRequest(`/projects/${id}/budget`),
  updateProjectBudget: (id: number, data: { budget_cents?: number; budget_period?: string }) =>
    legacyRequest(`/projects/${id}/budget`, { method: 'PUT', body: JSON.stringify(data) }),
  getProjectModelSets: (id: number) => legacyRequest(`/projects/${id}/model-sets`),
  replaceProjectModelSets: (id: number, modelSetIds: number[]) =>
    legacyRequest(`/projects/${id}/model-sets`, { method: 'PUT', body: JSON.stringify({ model_set_ids: modelSetIds }) }),
  getModelCatalog: (page = 1, pageSize = 20, filters?: { project_id?: number; org_id?: number; routable_only?: boolean }) =>
    legacyRequest(appendParams('/model-catalog', {
      page,
      page_size: pageSize,
      project_id: filters?.project_id,
      org_id: filters?.org_id,
      routable_only: filters?.routable_only,
    })),

  // Product-local model-call authorization. These policies can only narrow
  // existing API-key/project/user/Agent authority and never grant Tool/MCP use.
  getModelAuthorizationContract: () => legacyRequest('/model-call-authorizations/contract'),
  getModelAuthorizations: (orgId?: number, status?: string) =>
    legacyRequest(appendParams('/model-call-authorizations', { org_id: orgId, status })),
  createModelAuthorization: (data: JsonObject) =>
    legacyRequest('/model-call-authorizations', { method: 'POST', body: JSON.stringify(data) }),
  activateModelAuthorization: (authorizationRef: string, expectedRevision: number, orgId?: number) =>
    legacyRequest(`/model-call-authorizations/${encodeURIComponent(authorizationRef)}/activate`, {
      method: 'POST', body: JSON.stringify({ expected_revision: expectedRevision, ...(orgId ? { org_id: orgId } : {}) }),
    }),
  revokeModelAuthorization: (authorizationRef: string, expectedRevision: number, orgId?: number) =>
    legacyRequest(`/model-call-authorizations/${encodeURIComponent(authorizationRef)}/revoke`, {
      method: 'POST', body: JSON.stringify({ expected_revision: expectedRevision, ...(orgId ? { org_id: orgId } : {}) }),
    }),

  // Model governance
  getModelRegistry: (page = 1, pageSize = 20, filters?: { lifecycle?: string; approved_only?: boolean; q?: string; org_id?: number }) =>
    legacyRequest(appendParams('/model-registry', {
      page,
      page_size: pageSize,
      lifecycle: filters?.lifecycle,
      approved_only: filters?.approved_only ? 'true' : undefined,
      q: filters?.q,
      org_id: filters?.org_id,
    })),
  createModelRegistry: (data: JsonObject) =>
    legacyRequest('/model-registry', { method: 'POST', body: JSON.stringify(data) }),
  updateModelRegistry: (id: number, data: JsonObject, orgId?: number) =>
    legacyRequest(appendParams(`/model-registry/${id}`, { org_id: orgId }), { method: 'PUT', body: JSON.stringify(data) }),
  getModelAliases: (page = 1, pageSize = 20, filters?: { org_id?: number }) =>
    legacyRequest(appendParams('/model-aliases', { page, page_size: pageSize, org_id: filters?.org_id })),
  createModelAlias: (data: JsonObject) =>
    legacyRequest('/model-aliases', { method: 'POST', body: JSON.stringify(data) }),
  updateModelAlias: (id: number, data: JsonObject, orgId?: number) =>
    legacyRequest(appendParams(`/model-aliases/${id}`, { org_id: orgId }), { method: 'PUT', body: JSON.stringify(data) }),
  getModelSets: (page = 1, pageSize = 20, filters?: { org_id?: number }) =>
    legacyRequest(appendParams('/model-sets', { page, page_size: pageSize, org_id: filters?.org_id })),
  createModelSet: (data: JsonObject) =>
    legacyRequest('/model-sets', { method: 'POST', body: JSON.stringify(data) }),
  getModelSetItems: (id: number, filters?: { org_id?: number }) =>
    legacyRequest(appendParams(`/model-sets/${id}/items`, { org_id: filters?.org_id })),
  replaceModelSetItems: (id: number, data: { items: { item_type: string; item_name: string }[] }, orgId?: number) =>
    legacyRequest(appendParams(`/model-sets/${id}/items`, { org_id: orgId }), { method: 'PUT', body: JSON.stringify(data) }),

  // Model prices
  getModelPrices: () => legacyRequest('/model-prices'),
  getModelPriceList: (page = 1, pageSize = 20) =>
    legacyRequest(`/model-prices?page=${page}&page_size=${pageSize}`),
  getModelNames: () => legacyRequest('/model-names'),
  createModelPrice: (data: { model_name: string; input_price_cny_per_million_tokens: string; output_price_cny_per_million_tokens: string; cache_read_ratio?: string; cache_creation_ratio?: string }) =>
    legacyRequest('/model-prices', { method: 'POST', body: JSON.stringify(data) }),
  updateModelPrice: (id: number, data: { input_price_cny_per_million_tokens?: string; output_price_cny_per_million_tokens?: string; cache_read_ratio?: string; cache_creation_ratio?: string }) =>
    legacyRequest(`/model-prices/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteModelPrice: (id: number) =>
    legacyRequest(`/model-prices/${id}`, { method: 'DELETE' }),

  // Settings
  getSettings: () => legacyRequest('/settings'),
  updateSetting: (key: string, value: unknown) =>
    legacyRequest(`/settings/${key}`, { method: 'PUT', body: JSON.stringify({ value }) }),

  // License
  getLicense: () => legacyRequest('/license'),
  updateLicense: (license: unknown) =>
    legacyRequest('/license', { method: 'PUT', body: JSON.stringify({ license }) }),

  // Users
  getOrganizations: (page = 1, pageSize = 100) =>
    legacyRequest(`/organizations?page=${page}&page_size=${pageSize}`),
  bootstrapFirstCustomer: (organizationName: string, clientRequestId: string, options?: { dryRun?: boolean }) =>
    legacyRequest('/bootstrap/first-customer', {
      method: 'POST',
      headers: { 'X-UniRouter-Client-Request-ID': clientRequestId },
      body: JSON.stringify({ organization_name: organizationName, ...(options?.dryRun ? { dry_run: true } : {}) }),
    }),
  getEnterpriseDirectorySources: (orgId?: number) =>
    legacyRequest(appendParams('/enterprise-directory/sources', { org_id: orgId })),
  getEnterpriseDirectoryTree: (sourceRef: string, effectiveAt?: string, orgId?: number) =>
    legacyRequest(appendParams('/enterprise-directory/tree', {
      source_ref: sourceRef,
      effective_at: effectiveAt,
      org_id: orgId,
    })),
  getEnterpriseDirectoryIAMActions: (limit = 100, orgId?: number) =>
    legacyRequest(appendParams('/enterprise-directory/iam-actions', { limit, org_id: orgId })),
  getUsers: (page = 1, pageSize = 20, authProvider?: 'local' | 'enterprise') =>
    legacyRequest(appendParams('/users', { page, page_size: pageSize, auth_provider: authProvider })),
  createUser: (data: JsonObject) =>
    legacyRequest('/users', { method: 'POST', body: JSON.stringify(data) }),
  updateUser: (id: number, data: JsonObject) =>
    legacyRequest(`/users/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteUser: (id: number) =>
    legacyRequest(`/users/${id}`, { method: 'DELETE' }),

  // Grants
  getGrants: (page = 1, pageSize = 20, orgId?: number) =>
    legacyRequest(appendParams('/grants', { page, page_size: pageSize, org_id: orgId })),
  createGrant: (data: JsonObject) =>
    legacyRequest('/grants', { method: 'POST', body: JSON.stringify(data) }),

  // Audit Logs
  getAuditLogs: (page = 1, pageSize = 20, filters?: {
    org_id?: string; project_id?: string; actor_id?: string; action?: string; resource?: string; resource_id?: string;
    actor_kind?: string; actor_ref?: string;
    request_id?: string; client_request_id?: string; source?: string; start_time?: string; end_time?: string;
  }) => typedRequest<LegacyApiResponse>(appendParams('/audit-logs', {
    page,
    page_size: pageSize,
    org_id: filters?.org_id,
    project_id: filters?.project_id,
    actor_id: filters?.actor_id,
    actor_kind: filters?.actor_kind,
    actor_ref: filters?.actor_ref,
    action: filters?.action,
    resource: filters?.resource,
    resource_id: filters?.resource_id,
    request_id: filters?.request_id,
    client_request_id: filters?.client_request_id,
    source: filters?.source,
    start_time: filters?.start_time,
    end_time: filters?.end_time,
  }), {}, 'v2'),

  // Org Model Prices
  getOrgModelPrices: (page = 1, pageSize = 20, orgId?: number) =>
    legacyRequest(appendParams('/org-model-prices', { page, page_size: pageSize, org_id: orgId })),
  createOrgModelPrice: (data: { org_id: number; model_name: string; multiplier: string }) =>
    legacyRequest('/org-model-prices', { method: 'POST', body: JSON.stringify(data) }),
  updateOrgModelPrice: (id: number, data: { multiplier: string }) =>
    legacyRequest(`/org-model-prices/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  deleteOrgModelPrice: (id: number) =>
    legacyRequest(`/org-model-prices/${id}`, { method: 'DELETE' }),

  // User profile
  getMe: () => legacyRequest('/me'),
  changePassword: (data: { old_password: string; new_password: string }) =>
    legacyRequest('/me/password', { method: 'PUT', body: JSON.stringify(data) }),

  // Reconciliation
  getReconciliation: (page = 1, pageSize = 20, filters?: { org_id?: number; model?: string; status?: string; usage_source?: string }) => {
    let url = `/reconciliation?page=${page}&page_size=${pageSize}`;
    if (filters?.org_id) url += `&org_id=${filters.org_id}`;
    if (filters?.model) url += `&model=${encodeURIComponent(filters.model)}`;
    if (filters?.status) url += `&status=${encodeURIComponent(filters.status)}`;
    if (filters?.usage_source) url += `&usage_source=${encodeURIComponent(filters.usage_source)}`;
    return legacyRequest(url);
  },
  getProviderCOGSOfflineContract: () => legacyRequest('/provider-cogs/offline-contract'),
  previewProviderCOGSOffline: (body: string) =>
    legacyRequest('/provider-cogs/offline-preview', { method: 'POST', body }),
  getGovernanceSimulationContract: () => legacyRequest('/governance-simulation/offline-contract'),
  previewGovernanceSimulation: (body: string) =>
    legacyRequest('/governance-simulation/offline-preview', { method: 'POST', body }),
  getMemberLifecycleContract: () => legacyRequest('/member-lifecycle/offline-contract'),
  previewMemberLifecycle: (body: string) =>
    legacyRequest('/member-lifecycle/offline-preview', { method: 'POST', body }),
  getAgentTrustContract: () => legacyRequest('/agent-trust/offline-contract'),
  previewAgentTrust: (body: string) =>
    legacyRequest('/agent-trust/offline-preview', { method: 'POST', body }),
  getProvenancePassportContract: () => legacyRequest('/provenance-passport/offline-contract'),
  previewProvenancePassport: (body: string) =>
    legacyRequest('/provenance-passport/offline-preview', { method: 'POST', body }),
  getRiskControlContract: () => legacyRequest('/risk-control/offline-contract'),
  previewRiskControl: (body: string) =>
    legacyRequest('/risk-control/offline-preview', { method: 'POST', body }),

  // Notifications
  getNotificationEvents: (page = 1, pageSize = 20, filters?: {
    event_type?: string; severity?: string; status?: string; delivery_state?: string; org_id?: number; project_id?: number;
  }) => legacyRequest(appendParams('/notification-events', {
    page,
    page_size: pageSize,
    event_type: filters?.event_type,
    severity: filters?.severity,
    status: filters?.status,
    delivery_state: filters?.delivery_state,
    org_id: filters?.org_id,
    project_id: filters?.project_id,
  })),
  acknowledgeNotificationEvent: (id: number, projectId?: number, orgId?: number) =>
    legacyRequest(appendParams(`/notification-events/${id}/acknowledge`, { project_id: projectId, org_id: orgId }), { method: 'POST' }),
  resolveNotificationEvent: (id: number, projectId?: number, orgId?: number) =>
    legacyRequest(appendParams(`/notification-events/${id}/resolve`, { project_id: projectId, org_id: orgId }), { method: 'POST' }),
  getNotificationDeliveries: (page = 1, pageSize = 20, filters?: { status?: string; event_id?: number; org_id?: number; project_id?: number }) =>
    legacyRequest(appendParams('/notification-deliveries', {
      page,
      page_size: pageSize,
      status: filters?.status,
      event_id: filters?.event_id,
      org_id: filters?.org_id,
      project_id: filters?.project_id,
    })),
  getNotificationWebhooks: (page = 1, pageSize = 20, filters?: { enabled?: boolean; org_id?: number; project_id?: number }) =>
    legacyRequest(appendParams('/notification-webhooks', {
      page,
      page_size: pageSize,
      enabled: filters?.enabled === undefined ? undefined : String(filters.enabled),
      org_id: filters?.org_id,
      project_id: filters?.project_id,
    })),
  createNotificationWebhook: (data: JsonObject) =>
    legacyRequest('/notification-webhooks', { method: 'POST', body: JSON.stringify(data) }),
  updateNotificationWebhook: (id: number, data: JsonObject) =>
    legacyRequest(`/notification-webhooks/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  verifyNotificationWebhookScope: (id: number, expectedUpdatedAt: string) =>
    legacyRequest(`/notification-webhooks/${id}/verify-scope`, {
      method: 'POST',
      body: JSON.stringify({ expected_updated_at: expectedUpdatedAt }),
    }),
};
