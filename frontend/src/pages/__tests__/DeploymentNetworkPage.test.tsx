import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import DeploymentNetworkPage from '../DeploymentNetworkPage';

const mocks = { contract: vi.fn(), profiles: vi.fn(), endpoints: vi.fn(), createProfile: vi.fn(), updateProfile: vi.fn(), createEndpoint: vi.fn(), updateEndpoint: vi.fn() };

vi.mock('../../api/client', () => ({ api: {
  getDeploymentNetworkContract: (...args: any[]) => mocks.contract(...args),
  getDeploymentProfiles: (...args: any[]) => mocks.profiles(...args),
  getGatewayEndpoints: (...args: any[]) => mocks.endpoints(...args),
  createDeploymentProfile: (...args: any[]) => mocks.createProfile(...args),
  updateDeploymentProfile: (...args: any[]) => mocks.updateProfile(...args),
  createGatewayEndpoint: (...args: any[]) => mocks.createEndpoint(...args),
  updateGatewayEndpoint: (...args: any[]) => mocks.updateEndpoint(...args),
} }));

describe('DeploymentNetworkPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.contract.mockResolvedValue({ contract_version: 'unirouter.deployment-profile.v1', tls_private_key_storage: 'forbidden', apply_authority: 'operator_cli_compose_or_helm', runtime_mutation: 'none', endpoint_profiles: [{ profile_id: 'endpoint.openai-chat.v1', display_name: 'OpenAI Chat Completions', paths: ['/v1/chat/completions'], runtime_state: 'runtime_supported', evidence_level: 'verified', streaming: true }] });
    mocks.profiles.mockResolvedValue({ data: [{ id: 1, profile_ref: 'dep_ecs_poc', display_name: 'ECS POC', kind: 'ecs_development', environment: 'poc', ingress_mode: 'bundled_reverse_proxy', dns_mode: 'ip_address', outbound_mode: 'direct', notes: '', status: 'active', desired_revision: 1, updated_at: '2026-08-02T08:00:00Z' }] });
    mocks.endpoints.mockResolvedValue({ data: [{ id: 2, deployment_profile_id: 1, deployment_profile_ref: 'dep_ecs_poc', deployment_name: 'ECS POC', endpoint_ref: 'gwe_ecs_poc', display_name: 'ECS Gateway', base_url: 'https://gateway.example.com', visibility: 'public', tls_mode: 'external_termination', certificate_ref: 'secret://tls/gateway', endpoint_profile: 'endpoint.openai-chat.v1', status: 'active', desired_revision: 1, observed_status: 'unverified', observed_base_url: '', observed_evidence: {}, updated_at: '2026-08-02T08:00:00Z' }] });
  });

  it('separates desired endpoint configuration from independent observed evidence', async () => {
    render(<DeploymentNetworkPage />);
    expect(await screen.findByRole('heading', { name: '部署与网络' })).toBeTruthy();
    expect((await screen.findAllByText('ECS Gateway')).length).toBeGreaterThan(0);
    expect(screen.getByLabelText('部署与网络检查器')).toBeTruthy();
    expect(screen.getAllByText('未独立验证').length).toBeGreaterThan(0);
    expect(screen.getByText('secret://tls/gateway')).toBeTruthy();
    expect(screen.getByText('无')).toBeTruthy();
    expect(screen.getByText(/不保存 TLS 私钥/)).toBeTruthy();
  });
});
