import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import WorkloadApplicationsPage from '../WorkloadApplicationsPage';
import { useAuthStore } from '../../store/auth';

const mocks = {
  getContract: vi.fn(), getOrganizations: vi.fn(), getProjects: vi.fn(), getUsers: vi.fn(),
  getEndpoints: vi.fn(), getApplications: vi.fn(), getKeys: vi.fn(), getCredentials: vi.fn(),
  getCatalog: vi.fn(), getReadiness: vi.fn(), createApplication: vi.fn(), updateApplication: vi.fn(),
  archiveApplication: vi.fn(), createKey: vi.fn(), bindCredential: vi.fn(),
};

vi.mock('../../api/client', () => ({
  api: {
    getWorkloadApplicationContract: (...args: any[]) => mocks.getContract(...args),
    getOrganizations: (...args: any[]) => mocks.getOrganizations(...args),
    getProjects: (...args: any[]) => mocks.getProjects(...args),
    getUsers: (...args: any[]) => mocks.getUsers(...args),
    getGatewayEndpoints: (...args: any[]) => mocks.getEndpoints(...args),
    getWorkloadApplications: (...args: any[]) => mocks.getApplications(...args),
    getKeys: (...args: any[]) => mocks.getKeys(...args),
    getWorkloadApplicationCredentials: (...args: any[]) => mocks.getCredentials(...args),
    getModelCatalog: (...args: any[]) => mocks.getCatalog(...args),
    getWorkloadApplicationReadiness: (...args: any[]) => mocks.getReadiness(...args),
    createWorkloadApplication: (...args: any[]) => mocks.createApplication(...args),
    updateWorkloadApplication: (...args: any[]) => mocks.updateApplication(...args),
    archiveWorkloadApplication: (...args: any[]) => mocks.archiveApplication(...args),
    createKey: (...args: any[]) => mocks.createKey(...args),
    bindWorkloadApplicationCredential: (...args: any[]) => mocks.bindCredential(...args),
  },
}));

describe('WorkloadApplicationsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.setState({ token: 'token', isAuthenticated: true, profileLoading: false, user: { id: 1, username: 'admin', role: 'platform_admin', org_id: 1, org_name: '总部' } });
    mocks.getContract.mockResolvedValue({
      contract_version: 'unirouter.workload-application.v1', readiness_version: 'unirouter.workload-readiness.v1',
      client_profiles: [{
        profile_id: 'client.openai-sdk.chat.v1', display_name: 'OpenAI SDK / Chat Completions', client_family: 'openai_sdk',
        api_style: 'openai_chat_completions', supported_endpoints: ['/v1/chat/completions', '/v1/models'],
        runtime_state: 'runtime_supported', evidence_level: 'verified', recommended_for_poc: true, configuration_note: 'configured endpoint',
      }], identity_authority: 'product_local_model_execution_only', tool_or_mcp_authority: 'none', legacy_credentials: 'legacy_unbound_compatible', runtime_mutation: 'none',
    });
    mocks.getOrganizations.mockResolvedValue({ data: [{ id: 1, name: '总部' }] });
    mocks.getProjects.mockResolvedValue({ data: [{ id: 11, org_id: 1, name: '理赔 POC', status: 'active' }] });
    mocks.getUsers.mockResolvedValue({ data: [{ id: 7, org_id: 1, username: 'alice' }] });
    mocks.getEndpoints.mockResolvedValue({ data: [{ base_url: 'https://gateway.example.com', status: 'active', observed_status: 'unverified', endpoint_profile: 'endpoint.openai-chat.v1' }] });
    mocks.getApplications.mockResolvedValue({ data: [{
      id: 31, org_id: 1, project_id: 11, project_name: '理赔 POC', project_status: 'active',
      application_ref: 'app_claims_agent', display_name: '理赔 Agent', kind: 'agent', environment: 'poc',
      identity_mode: 'service_credential', client_profile: 'client.openai-sdk.chat.v1', owner_team: 'AI 平台',
      description: '理赔助手', status: 'active', revision: 2, credential_count: 1, active_credential_count: 1,
      created_at: '2026-08-02T08:00:00Z', updated_at: '2026-08-02T09:00:00Z',
    }], total: 1 });
    mocks.getKeys.mockResolvedValue({ data: [{ id: 91, org_id: 1, project_id: 11, project_name: '理赔 POC', name: '旧共享 Key', key_prefix: 'sk-old', status: 'active', key_type: 'shared', allowed_models: '', rpm_limit: 60, qps_limit: 10, workload_application_ref: '', purpose: '', created_at: '2026-08-01T08:00:00Z' }], total: 1 });
    mocks.getCredentials.mockResolvedValue({ data: [{ id: 32, name: 'Claims primary', key_prefix: 'sk-app', status: 'active', key_type: 'service', allowed_models: 'company-chat', rpm_limit: 60, rps_limit: 10, created_at: '2026-08-02T08:30:00Z' }] });
    mocks.getCatalog.mockResolvedValue({ data: [{ model: 'company-chat' }] });
    mocks.getReadiness.mockResolvedValue({
      contract_version: 'unirouter.workload-readiness.v1', overall: 'needs_input', endpoint: '/v1/chat/completions', runtime_mutation: 'none',
      axes: [
        { key: 'application', state: 'ready', reason_code: 'application_active', description: '应用处于启用状态。', evidence: ['workload_applications.status'] },
        { key: 'application_credential', state: 'context_required', reason_code: 'model_required_for_credential_scope', description: '选择模型后才能验证凭证的模型范围。', evidence: ['api_keys.allowed_models'] },
      ],
    });
  });

  it('keeps the effective-access inspector visible and uses the configured Gateway Endpoint', async () => {
    const { container } = render(<MemoryRouter><WorkloadApplicationsPage /></MemoryRouter>);

    expect(await screen.findByRole('heading', { name: '应用与 Agent 接入' })).toBeTruthy();
    expect((await screen.findAllByText('理赔 Agent')).length).toBeGreaterThan(0);
    expect(screen.getByLabelText('应用接入检查器')).toBeTruthy();
    expect(await screen.findByText('应用处于启用状态。')).toBeTruthy();
    expect(screen.getByText('旧共享 Key')).toBeTruthy();
    expect(screen.getByText('legacy_unbound')).toBeTruthy();
    expect(container.querySelector('.ur-code-block')?.textContent).toContain('https://gateway.example.com/v1');
    expect(screen.getByText(/尚未完成独立验证/)).toBeTruthy();
    expect(container.querySelector('.ur-code-block')?.textContent).not.toContain('window.location');
    expect(screen.getByText('无')).toBeTruthy();
  });
});
