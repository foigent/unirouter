import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import AccessStatePage from '../AccessStatePage';
import { useAuthStore } from '../../store/auth';

const mocks = vi.hoisted(() => ({
  getProjects: vi.fn(), listMyRequests: vi.fn(), listMyGrants: vi.fn(),
  listCredentials: vi.fn(), createRequest: vi.fn(), cancelRequest: vi.fn(),
  issueCredential: vi.fn(), rotateCredential: vi.fn(), revokeCredential: vi.fn(),
}));

vi.mock('../../api/client', () => ({
  ApiError: class ApiError extends Error {
    status: number;
    constructor(message: string, status: number) { super(message); this.status = status; }
  },
  api: { getProjects: (...args: unknown[]) => mocks.getProjects(...args) },
}));

vi.mock('../../api/access', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/access')>();
  return { ...actual, accessApi: {
    listMyRequests: (...args: unknown[]) => mocks.listMyRequests(...args),
    listMyGrants: (...args: unknown[]) => mocks.listMyGrants(...args),
    listCredentials: (...args: unknown[]) => mocks.listCredentials(...args),
    createRequest: (...args: unknown[]) => mocks.createRequest(...args),
    cancelRequest: (...args: unknown[]) => mocks.cancelRequest(...args),
    issueCredential: (...args: unknown[]) => mocks.issueCredential(...args),
    rotateCredential: (...args: unknown[]) => mocks.rotateCredential(...args),
    revokeCredential: (...args: unknown[]) => mocks.revokeCredential(...args),
  } };
});

const page = <T,>(data: T[], hasMore = false, nextAfterID?: number) => ({
  data, pagination: { limit: 20, has_more: hasMore, next_after_id: nextAfterID }, scope: 'my',
});

const request = (id: number, ref: string) => ({
  id, request_ref: ref, org_id: 1, project_id: 11, subject: { type: 'user', user_id: 7 },
  request_kind: 'human_personal', requested_scope: { project_role: 'project_user', scope_mode: 'project_default', models: [], endpoints: [] },
  environment: 'personal', production_use: false, submission_mode: 'self', status: 'needs_review', decision_mode: 'policy',
  policy_reason_codes: ['first_cross_project'], decision_reason_codes: [], submitted_by: 7,
  submitted_at: '2026-08-09T00:00:00Z', revision: 1, actions: { can_approve: false, can_reject: false, can_cancel: true, reason_code: 'requester_can_cancel' },
});

const grant = {
  id: 31, grant_ref: 'grant-one', access_request_id: 21, org_id: 1, project_id: 11,
  subject: { type: 'user', user_id: 7 }, scope: { project_role: 'project_user', scope_mode: 'project_default', models: [], endpoints: [] },
  environment: 'personal', production_use: false, status: 'active', revision: 1,
  valid_from: '2026-08-09T00:00:00Z', policy_reason_codes: ['project_default_scope'], decision_reason_codes: [],
  created_at: '2026-08-09T00:00:00Z', updated_at: '2026-08-09T00:00:00Z',
};

describe('AccessStatePage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.setState({
      token: 'token', isAuthenticated: true, profileLoading: false, profileError: null,
      user: { id: 7, username: 'alice', organization_role: 'member', role: 'member', org_id: 1, org_name: 'Acme' },
    });
    mocks.getProjects.mockResolvedValue({ data: [{ id: 11, org_id: 1, name: 'Core', status: 'active', current_user_membership_role: 'project_user' }], total: 1 });
    mocks.listMyRequests.mockResolvedValue(page([]));
    mocks.listMyGrants.mockResolvedValue(page([grant]));
    mocks.listCredentials.mockResolvedValue(page([]));
  });

  it('routes an existing project member to the daily project workbench instead of offering a duplicate request', async () => {
    render(<MemoryRouter initialEntries={['/access?project_id=11']}><AccessStatePage /></MemoryRouter>);

    expect(await screen.findByText(/你已经是“Core”的项目成员/)).toBeTruthy();
    expect(screen.getByRole('button', { name: '进入项目工作台' })).toBeTruthy();
    expect(screen.getByRole('button', { name: /已加入项目/ })).toBeDisabled();
  });

  it('keeps Grant facts visible when the independent request feed fails', async () => {
    mocks.listMyRequests.mockRejectedValue(new Error('request store unavailable'));
    render(<MemoryRouter initialEntries={['/access?project_id=11&grant_ref=grant-one']}><AccessStatePage /></MemoryRouter>);

    expect(await screen.findByText('请求列表暂时不可用')).toBeTruthy();
    expect(await screen.findByText('grant-one')).toBeTruthy();
    expect(screen.getByText('正在管理“Core”的 API Key。新密钥只显示一次，请立即安全保存。')).toBeTruthy();
  });

  it('uses the server keyset cursor without replacing the first page', async () => {
    mocks.listMyRequests
      .mockResolvedValueOnce(page([request(20, 'request-new')], true, 20))
      .mockResolvedValueOnce(page([request(10, 'request-old')]));
    render(<MemoryRouter initialEntries={['/access?project_id=11']}><AccessStatePage /></MemoryRouter>);

    expect(await screen.findByText('request-new')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '加载更早请求' }));
    expect(await screen.findByText('request-old')).toBeTruthy();
    expect(screen.getByText('request-new')).toBeTruthy();
    expect(mocks.listMyRequests).toHaveBeenNthCalledWith(2, 20, 20);
  });

  it('shows a secret once and renders secret-free replay evidence truthfully', async () => {
    mocks.issueCredential
      .mockResolvedValueOnce({
        id: 91, credential_ref: 'cred-one', key: 'ur-secret-once', key_prefix: 'ur-once', name: 'Laptop', grant_ref: 'grant-one',
        status: 'active', allowed_ips: '', allowed_models: [], allowed_endpoints: [], rpm_limit: 60, rps_limit: 10,
        classification: 'grant_bound', idempotent: false, secret_available: true,
      })
      .mockResolvedValueOnce({
        id: 91, credential_ref: 'cred-one', key_prefix: 'ur-once', name: 'Laptop', grant_ref: 'grant-one', status: 'active',
        allowed_ips: '', allowed_models: [], allowed_endpoints: [], rpm_limit: 60, rps_limit: 10,
        classification: 'grant_bound', idempotent: true, secret_available: false, recovery_action: 'rotate_active_version',
      });
    render(<MemoryRouter initialEntries={['/access?project_id=11']}><AccessStatePage /></MemoryRouter>);

    fireEvent.click(await screen.findByRole('button', { name: '管理 API Key' }));
    fireEvent.click(await screen.findByRole('button', { name: /创建 API Key/ }));
    fireEvent.change(await screen.findByLabelText('Key 名称'), { target: { value: 'Laptop' } });
    fireEvent.click(screen.getByRole('button', { name: '创建' }));
    expect(await screen.findByText('ur-secret-once')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'confirm' }));
    await waitFor(() => expect(screen.queryByText('ur-secret-once')).toBeNull());

    fireEvent.click(await screen.findByRole('button', { name: /创建 API Key/ }));
    fireEvent.change(await screen.findByLabelText('Key 名称'), { target: { value: 'Laptop' } });
    fireEvent.click(screen.getByRole('button', { name: '创建' }));
    expect(await screen.findByText(/之前成功操作的安全重放/)).toBeTruthy();
    expect(screen.queryByText(/rotate_active_version/)).toBeNull();
    expect(screen.queryByText('ur-secret-once')).toBeNull();
  }, 20_000);

  it('shows an expired active-row Grant truthfully and blocks new secret material', async () => {
    mocks.listMyGrants.mockResolvedValue(page([{ ...grant, valid_until: '2026-08-09T00:00:01Z' }]));
    mocks.listCredentials.mockResolvedValue(page([{
      id: 81, credential_ref: 'cred-expired', name: 'Expired family', status: 'active', effective_status: 'grant_expired',
      allowed_ips: '', allowed_models: [], allowed_endpoints: [], rpm_limit: 60, rps_limit: 10,
      created_at: '2026-08-09T00:00:00Z', version_count: 1,
      versions: [{ id: 91, key_prefix: 'ur-expired', status: 'active', effective_status: 'grant_expired', created_at: '2026-08-09T00:00:00Z' }],
    }]));
    render(<MemoryRouter initialEntries={['/access?project_id=11&grant_ref=grant-one']}><AccessStatePage /></MemoryRouter>);

    expect(await screen.findByText('已过期')).toBeTruthy();
    expect(screen.queryByRole('button', { name: '创建 API Key' })).toBeNull();
    const rotate = await screen.findByRole('button', { name: '安全轮换' }) as HTMLButtonElement;
    expect(rotate.disabled).toBe(true);
    expect(mocks.issueCredential).not.toHaveBeenCalled();
    expect(mocks.rotateCredential).not.toHaveBeenCalled();
  });
});
