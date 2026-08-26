import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import DeploymentNetworkPage from '../DeploymentNetworkPage';

const mocks = { contract: vi.fn(), profiles: vi.fn(), endpoints: vi.fn(), runtime: vi.fn(), createProfile: vi.fn(), updateProfile: vi.fn(), createEndpoint: vi.fn(), updateEndpoint: vi.fn() };

vi.mock('../../api/client', () => ({ api: {
  getDeploymentNetworkContract: (...args: any[]) => mocks.contract(...args),
  getDeploymentProfiles: (...args: any[]) => mocks.profiles(...args),
  getGatewayEndpoints: (...args: any[]) => mocks.endpoints(...args),
  getRuntimeStatus: (...args: any[]) => mocks.runtime(...args),
  createDeploymentProfile: (...args: any[]) => mocks.createProfile(...args),
  updateDeploymentProfile: (...args: any[]) => mocks.updateProfile(...args),
  createGatewayEndpoint: (...args: any[]) => mocks.createEndpoint(...args),
  updateGatewayEndpoint: (...args: any[]) => mocks.updateEndpoint(...args),
} }));

describe('DeploymentNetworkPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.contract.mockResolvedValue({
      contract_version: 'unirouter.deployment-profile.v1',
      tls_private_key_storage: 'forbidden',
      apply_authority: 'operator_cli_compose_or_helm',
      runtime_mutation: 'none',
      endpoint_profiles: [
        { profile_id: 'endpoint.openai-chat.v1', display_name: 'OpenAI Chat Completions', paths: ['/v1/chat/completions', '/v1/models'], runtime_state: 'runtime_supported', evidence_level: 'verified', streaming: true },
        { profile_id: 'endpoint.openai-responses.contract.v1', display_name: 'OpenAI Responses (strict text; exact stream subset)', paths: ['/v1/responses'], runtime_state: 'runtime_supported', evidence_level: 'repository_runtime_verified', streaming: true },
        { profile_id: 'endpoint.anthropic-messages.v1', display_name: 'Anthropic Messages (strict non-stream text)', paths: ['/v1/messages'], runtime_state: 'runtime_supported', evidence_level: 'repository_runtime_verified', streaming: false },
      ],
    });
    mocks.profiles.mockResolvedValue({ data: [{ id: 1, profile_ref: 'dep_ecs_poc', display_name: 'ECS POC', kind: 'ecs_development', environment: 'poc', ingress_mode: 'bundled_reverse_proxy', dns_mode: 'ip_address', outbound_mode: 'direct', notes: '', status: 'active', desired_revision: 1, updated_at: '2026-08-02T08:00:00Z' }] });
    mocks.endpoints.mockResolvedValue({ data: [{ id: 2, deployment_profile_id: 1, deployment_profile_ref: 'dep_ecs_poc', deployment_name: 'ECS POC', endpoint_ref: 'gwe_ecs_poc', display_name: 'ECS Gateway', base_url: 'https://gateway.example.com', visibility: 'public', tls_mode: 'external_termination', certificate_ref: 'secret://tls/gateway', endpoint_profile: 'endpoint.anthropic-messages.v1', status: 'active', desired_revision: 1, observed_status: 'unverified', observed_base_url: '', observed_evidence: {}, updated_at: '2026-08-02T08:00:00Z' }] });
    mocks.runtime.mockResolvedValue({
      status: 'operational', generated_at: '2026-08-17T00:00:00Z',
      runtime_environment: { version: '1.1.25', commit: 'abcdef1234567890', deployment_mode: 'customer_compose', environment: 'test', deployment_role: 'single_customer' },
      capacity_health: { database: { status: 'ready', latency_ms: 1 }, redis: { status: 'ready', latency_ms: 2 } },
    });
  });

  it('separates desired endpoint configuration from independent observed evidence', async () => {
    render(<DeploymentNetworkPage />);
    expect(await screen.findByRole('heading', { name: '部署与网络' })).toBeTruthy();
    expect(screen.getByText('1.1.25')).toBeTruthy();
    expect(screen.getByText(/本次探测：数据库 1 毫秒 · Redis 2 毫秒/)).toBeTruthy();
    expect((await screen.findAllByText('ECS Gateway')).length).toBeGreaterThan(0);
    expect(screen.getByLabelText('部署与网络检查器')).toBeTruthy();
    expect(screen.getAllByText('未独立验证').length).toBeGreaterThan(0);
    expect(screen.getByText('Anthropic Messages (strict non-stream text)')).toBeTruthy();
    expect(screen.getByText('仓库运行时已验证')).toBeTruthy();
    expect(screen.getByText('运行时能力（仓库）')).toBeTruthy();
    expect(screen.getByText('secret://tls/gateway')).toBeTruthy();
    expect(screen.getByText('无')).toBeTruthy();
    expect(screen.getByText(/TLS 私钥/)).toBeTruthy();
  });

  it('shows a loading state instead of falsely claiming runtime observation is unavailable', async () => {
    let resolveRuntime: (value: unknown) => void = () => undefined;
    mocks.runtime.mockReturnValue(new Promise(resolve => { resolveRuntime = resolve; }));

    render(<DeploymentNetworkPage />);

    expect(await screen.findByRole('heading', { name: '正在加载当前运行实例...' })).toBeTruthy();
    expect(screen.queryByRole('heading', { name: '当前运行实例观测不可用' })).toBeNull();

    await act(async () => {
      resolveRuntime({
        status: 'operational', generated_at: '2026-08-17T00:00:00Z',
        runtime_environment: { version: '1.1.26', commit: '23db6edcea60', deployment_mode: 'docker-compose', environment: 'local', deployment_role: 'stable' },
        capacity_health: { database: { status: 'connected', latency_ms: 1 }, redis: { status: 'connected', latency_ms: 1 } },
      });
    });

    expect(await screen.findByText('1.1.26')).toBeTruthy();
    expect(screen.queryByRole('heading', { name: '当前运行实例观测不可用' })).toBeNull();
  });
});
