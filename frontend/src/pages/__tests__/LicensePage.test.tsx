import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import LicensePage from '../LicensePage';
import { useAuthStore } from '../../store/auth';

const mockGetLicense = vi.fn();
const mockUpdateLicense = vi.fn();
const mockGetMe = vi.fn();

vi.mock('../../api/client', async () => {
  const actual = await vi.importActual<typeof import('../../api/client')>('../../api/client');
  return {
    ...actual,
    api: {
      ...actual.api,
      getLicense: (...args: any[]) => mockGetLicense(...args),
      updateLicense: (...args: any[]) => mockUpdateLicense(...args),
      getMe: (...args: any[]) => mockGetMe(...args),
    },
  };
});

const ACTIVE_LICENSE = {
  status: 'active',
  valid: true,
  enforceable: true,
  commercial: true,
  signature_valid: true,
  deployment_match: true,
  warning: false,
  days_until_expiry: 180,
  checked_at: '2026-07-02T00:00:00Z',
  verifier_configured: true,
  schema_version: 'unirouter.license.v1',
  key_id: 'license-2026-a',
  expected_deployment_id: 'prod-cn-1',
  license: {
    license_id: 'lic-acme-1',
    customer: 'Acme Corp',
    deployment_id: 'prod-cn-1',
    tier: 'enterprise',
    issued_at: '2026-07-01T00:00:00Z',
    not_before: '2026-07-01T00:00:00Z',
    expires_at: '2026-12-31T00:00:00Z',
    features: ['gateway', 'model_governance'],
    limits: { active_projects: 20 },
  },
};

describe('LicensePage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetLicense.mockResolvedValue(ACTIVE_LICENSE);
    mockUpdateLicense.mockResolvedValue(ACTIVE_LICENSE);
    mockGetMe.mockResolvedValue({
      id: 1,
      username: 'admin',
      role: 'platform_admin',
      org_id: 1,
      license_access: {
        status: 'active',
        enforceable: true,
        features: {
          gateway: true,
          agent_capabilities: false,
          oidc: false,
          guardrails: false,
          notifications: false,
          model_governance: true,
          data_exchange: false,
        },
      },
    });
    useAuthStore.setState({
      token: 'token',
      isAuthenticated: true,
      user: { id: 1, username: 'admin', role: 'platform_admin', org_id: 1 },
      profileLoading: false,
      profileError: null,
    });
  });

  it('renders license status and metadata', async () => {
    render(<LicensePage />);

    expect(mockGetLicense).toHaveBeenCalled();
    expect(await screen.findByText('Acme Corp')).toBeTruthy();
    expect(screen.getAllByText('有效')).toHaveLength(2);
    expect(screen.getByText('enterprise')).toBeTruthy();
    expect(screen.getByText('模型网关、模型治理')).toBeTruthy();
    expect(screen.getByText('unirouter.license.v1')).toBeTruthy();
    expect(screen.getByText('license-2026-a')).toBeTruthy();
  });

  it('submits pasted offline license JSON', async () => {
    render(<LicensePage />);

    await screen.findByText('Acme Corp');
    const textarea = screen.getByPlaceholderText('{"schema_version":"unirouter.license.v1","key_id":"license-...","payload":{...},"signature":"..."}');
    fireEvent.change(textarea, {
      target: { value: '{"payload":{"customer":"Acme Corp"},"signature":"sig"}' },
    });
    fireEvent.click(screen.getByText('导入'));

    await waitFor(() => {
      expect(mockUpdateLicense).toHaveBeenCalledWith({
        payload: { customer: 'Acme Corp' },
        signature: 'sig',
      });
    });
    await waitFor(() => expect(mockGetMe).toHaveBeenCalledTimes(1));
    expect(useAuthStore.getState().user?.license_access?.features.model_governance).toBe(true);
    expect(useAuthStore.getState().user?.license_access?.features.notifications).toBe(false);
  });

  it('does not turn a status request failure into an invalid-license conclusion', async () => {
    mockGetLicense.mockRejectedValueOnce(new Error('temporarily unavailable'));

    render(<LicensePage />);

    expect(await screen.findByText('无法确认当前授权状态')).toBeTruthy();
    expect(screen.getAllByText(/temporarily unavailable/).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/不会将加载失败推断为授权失效或流量被拒绝/).length).toBeGreaterThan(0);
    expect(screen.queryByText('当前授权不可用于商业运行；新模型请求保持拒绝。')).toBeNull();
    expect(screen.queryByText('拒绝')).toBeNull();
  });
});
