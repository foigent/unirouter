import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { Modal } from '@douyinfe/semi-ui';
import { MemoryRouter, useNavigate } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import AccessReviewPage from '../AccessReviewPage';
import { useAuthStore } from '../../store/auth';
import { ApiError } from '../../api/client';

const mocks = vi.hoisted(() => ({ getProjects: vi.fn(), listProjectRequests: vi.fn(), decideRequest: vi.fn() }));

vi.mock('../../api/client', () => ({
  ApiError: class ApiError extends Error {
    status: number;
    constructor(message: string, status: number) { super(message); this.status = status; }
  },
  api: { getProjects: (...args: unknown[]) => mocks.getProjects(...args) },
}));

vi.mock('../../api/access', () => ({
  accessApi: {
    listProjectRequests: (...args: unknown[]) => mocks.listProjectRequests(...args),
    decideRequest: (...args: unknown[]) => mocks.decideRequest(...args),
  },
}));

const queueRequest = (id: number, ref: string, actionable: boolean, projectID = 11) => ({
  id, request_ref: ref, org_id: 1, project_id: projectID, subject: { type: 'user', user_id: 9 }, subject_display_name: '张三', request_kind: 'human_personal',
  requested_scope: { project_role: 'project_user', scope_mode: 'project_default', models: [], endpoints: [] },
  environment: 'personal', production_use: false, submission_mode: 'self', status: 'needs_review', decision_mode: 'policy',
  policy_reason_codes: ['first_cross_project'], decision_reason_codes: [], submitted_by: 9, submitted_at: '2026-08-09T00:00:00Z', revision: 2,
  actions: { can_approve: actionable, can_reject: actionable, can_cancel: false, reason_code: actionable ? 'project_admin_actionable' : 'organization_admin_required' },
});

function ProjectSwitch() {
  const navigate = useNavigate();
  return <button onClick={() => navigate('/access-review?project_id=12')}>switch-project</button>;
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((complete, fail) => { resolve = complete; reject = fail; });
  return { promise, resolve, reject };
}

describe('AccessReviewPage', () => {
  afterEach(() => { Modal.destroyAll(); });

  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.setState({
      token: 'token', isAuthenticated: true, profileLoading: false, profileError: null,
      user: { id: 1, username: 'reviewer', organization_role: 'member', role: 'member', org_id: 1, org_name: 'Acme' },
    });
    mocks.getProjects.mockResolvedValue({ data: [{ id: 11, org_id: 1, name: 'Core', status: 'active', current_user_membership_role: 'project_admin' }], total: 1 });
    mocks.listProjectRequests.mockResolvedValue({
      data: [queueRequest(20, 'request-actionable', true), queueRequest(19, 'request-visible-only', false)],
      pagination: { limit: 25, has_more: false }, scope: 'project',
    });
  });

  it('uses the server needs-review filter and never infers actions from visibility', async () => {
    render(<MemoryRouter initialEntries={['/access-review?project_id=11']}><AccessReviewPage /></MemoryRouter>);

    expect(await screen.findByText('request-actionable')).toBeTruthy();
    expect(screen.getByText('request-visible-only')).toBeTruthy();
    expect(mocks.listProjectRequests).toHaveBeenCalledWith(11, { status: 'needs_review', limit: 25, after_id: undefined });
    expect(screen.getByText('需要组织管理员处理')).toBeTruthy();
    expect(screen.getAllByText('首次加入此项目，需要管理员确认')).toHaveLength(2);
    expect(screen.getAllByRole('button', { name: '批准' })).toHaveLength(1);
    expect(screen.getAllByRole('button', { name: '拒绝' })).toHaveLength(1);
    expect(screen.getAllByText('张三')).toHaveLength(2);
    expect(screen.queryByText('用户 #9')).toBeNull();
  });

  it('shows an application name instead of an internal workload id', async () => {
    mocks.listProjectRequests.mockResolvedValue({
      data: [{
        ...queueRequest(22, 'request-application', true),
        subject: { type: 'workload_application', workload_application_id: 41 },
        subject_display_name: '订单处理 Agent',
      }],
      pagination: { limit: 25, has_more: false }, scope: 'project',
    });

    render(<MemoryRouter initialEntries={['/access-review?project_id=11']}><AccessReviewPage /></MemoryRouter>);

    expect(await screen.findByText('订单处理 Agent')).toBeTruthy();
    expect(screen.getByText('应用或 Agent')).toBeTruthy();
    expect(screen.queryByText(/Workload #41|应用 #41/)).toBeNull();
  });

  it('shows the single default project as a human label without its database id', async () => {
    mocks.getProjects.mockResolvedValue({
      data: [{ id: 37, org_id: 1, name: 'Default Project', slug: 'default', status: 'active', current_user_membership_role: 'project_admin' }],
      total: 1,
    });
    mocks.listProjectRequests.mockResolvedValue({ data: [], pagination: { limit: 25, has_more: false }, scope: 'project' });

    render(<MemoryRouter initialEntries={['/access-review?project_id=37']}><AccessReviewPage /></MemoryRouter>);

    expect(await screen.findByText('默认项目')).toBeTruthy();
    expect(screen.queryByText(/#37/)).toBeNull();
    expect(screen.queryByRole('combobox', { name: '选择审核项目' })).toBeNull();
  });

  it('refreshes the authoritative queue after an optimistic revision conflict', async () => {
    mocks.decideRequest.mockRejectedValue(new ApiError('conflict', 409, {}));
    render(<MemoryRouter initialEntries={['/access-review?project_id=11']}><AccessReviewPage /></MemoryRouter>);

    fireEvent.click(await screen.findByRole('button', { name: '批准' }));
    await screen.findByText('批准访问请求');
    expect(screen.getByText('确认批准张三的访问申请？系统会在保存前再次核对你的权限、项目状态和申请范围。')).toBeTruthy();
    expect(screen.queryByText(/revision/i)).toBeNull();
    const modalButtons = screen.getAllByRole('button');
    fireEvent.click(modalButtons[modalButtons.length - 1]);
    await waitFor(() => expect(mocks.decideRequest).toHaveBeenCalledWith('request-actionable', {
      expected_revision: 2, decision: 'approve', reason_codes: ['manual_approved'],
    }, expect.any(String)));
    await waitFor(() => expect(mocks.listProjectRequests).toHaveBeenCalledTimes(2));
  });

  it('never renders a slower prior-project queue response under the current project', async () => {
    const projectA = deferred<{ data: ReturnType<typeof queueRequest>[]; pagination: { limit: number; has_more: boolean }; scope: string }>();
    mocks.listProjectRequests.mockImplementation((projectID: number) => projectID === 11
      ? projectA.promise
      : Promise.resolve({ data: [queueRequest(30, 'request-project-b', true, 12)], pagination: { limit: 25, has_more: false }, scope: 'project' }));

    render(<MemoryRouter initialEntries={['/access-review?project_id=11']}><AccessReviewPage /><ProjectSwitch /></MemoryRouter>);
    await waitFor(() => expect(mocks.listProjectRequests).toHaveBeenCalledWith(11, expect.any(Object)));
    fireEvent.click(screen.getByRole('button', { name: 'switch-project' }));
    expect(await screen.findByText('request-project-b')).toBeTruthy();

    projectA.resolve({ data: [queueRequest(20, 'request-project-a', true)], pagination: { limit: 25, has_more: false }, scope: 'project' });
    await waitFor(() => expect(screen.queryByText('request-project-a')).toBeNull());
    expect(screen.getByText('request-project-b')).toBeTruthy();
  });

  it('binds a decision confirmation to the project that rendered the request', async () => {
    mocks.listProjectRequests.mockImplementation((projectID: number) => Promise.resolve({
      data: [queueRequest(projectID, `request-project-${projectID}`, true, projectID)],
      pagination: { limit: 25, has_more: false }, scope: 'project',
    }));
    render(<MemoryRouter initialEntries={['/access-review?project_id=11']}><AccessReviewPage /><ProjectSwitch /></MemoryRouter>);

    fireEvent.click(await screen.findByRole('button', { name: '批准' }));
    await waitFor(() => expect(screen.getAllByText('批准访问请求').length).toBeGreaterThan(0));
    fireEvent.click(screen.getByRole('button', { name: 'switch-project' }));
    expect(await screen.findByText('request-project-12')).toBeTruthy();
    const modalButtons = screen.getAllByRole('button');
    fireEvent.click(modalButtons[modalButtons.length - 1]);

    await waitFor(() => expect(mocks.decideRequest).not.toHaveBeenCalled());
  });

  it('keeps the current project queue stable when a prior-project decision later conflicts', async () => {
    const projectADecision = deferred<unknown>();
    let projectBReads = 0;
    mocks.getProjects.mockResolvedValue({
      data: [
        { id: 11, org_id: 1, name: 'Core', status: 'active', current_user_membership_role: 'project_admin' },
        { id: 12, org_id: 1, name: 'Next', status: 'active', current_user_membership_role: 'project_admin' },
      ],
      total: 2,
    });
    mocks.listProjectRequests.mockImplementation((projectID: number) => {
      if (projectID === 11) {
        return Promise.resolve({ data: [queueRequest(20, 'request-project-a', true, 11)], pagination: { limit: 25, has_more: false }, scope: 'project' });
      }
      projectBReads += 1;
      return Promise.resolve({
        data: [queueRequest(30 + projectBReads, projectBReads === 1 ? 'request-project-b' : 'request-project-b-refreshed', true, 12)],
        pagination: { limit: 25, has_more: false },
        scope: 'project',
      });
    });
    mocks.decideRequest.mockReturnValue(projectADecision.promise);

    render(<MemoryRouter initialEntries={['/access-review?project_id=11']}><AccessReviewPage /><ProjectSwitch /></MemoryRouter>);
    fireEvent.click(await screen.findByRole('button', { name: '批准' }));
    await waitFor(() => expect(screen.getAllByText('批准访问请求').length).toBeGreaterThan(0));
    const confirmButtons = screen.getAllByRole('button', { name: 'confirm' });
    fireEvent.click(confirmButtons[confirmButtons.length - 1]);
    await waitFor(() => expect(mocks.decideRequest).toHaveBeenCalledTimes(1));

    fireEvent.click(screen.getByRole('button', { name: 'switch-project' }));
    expect(await screen.findByText('request-project-b')).toBeTruthy();

    await act(async () => {
      projectADecision.reject(new ApiError('conflict', 409, {}));
      await Promise.resolve();
    });

    expect(screen.getByText('request-project-b')).toBeTruthy();
    expect(screen.queryByText('request-project-a')).toBeNull();
    expect(screen.queryByText('审核队列暂时不可用')).toBeNull();
    expect(mocks.listProjectRequests.mock.calls.filter(([projectID]) => projectID === 11)).toHaveLength(1);

    await act(async () => {
      Modal.destroyAll();
      await Promise.resolve();
    });
    fireEvent.click(screen.getByRole('button', { name: /刷新/ }));
    expect(await screen.findByText('request-project-b-refreshed')).toBeTruthy();
    expect(mocks.listProjectRequests.mock.calls.filter(([projectID]) => projectID === 12)).toHaveLength(2);
  });
});
