import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
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

const queueRequest = (id: number, ref: string, actionable: boolean) => ({
  id, request_ref: ref, org_id: 1, project_id: 11, subject: { type: 'user', user_id: 9 }, request_kind: 'human_personal',
  requested_scope: { project_role: 'project_user', scope_mode: 'project_default', models: [], endpoints: [] },
  environment: 'personal', production_use: false, submission_mode: 'self', status: 'needs_review', decision_mode: 'policy',
  policy_reason_codes: ['first_cross_project'], decision_reason_codes: [], submitted_by: 9, submitted_at: '2026-08-09T00:00:00Z', revision: 2,
  actions: { can_approve: actionable, can_reject: actionable, can_cancel: false, reason_code: actionable ? 'project_admin_can_decide' : 'decision_requires_org_admin' },
});

describe('AccessReviewPage', () => {
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
    expect(screen.getByText('decision_requires_org_admin')).toBeTruthy();
    expect(screen.getAllByRole('button', { name: '批准' })).toHaveLength(1);
    expect(screen.getAllByRole('button', { name: '拒绝' })).toHaveLength(1);
  });

  it('refreshes the authoritative queue after an optimistic revision conflict', async () => {
    mocks.decideRequest.mockRejectedValue(new ApiError('conflict', 409, {}));
    render(<MemoryRouter initialEntries={['/access-review?project_id=11']}><AccessReviewPage /></MemoryRouter>);

    fireEvent.click(await screen.findByRole('button', { name: '批准' }));
    await screen.findByText('批准访问请求');
    const modalButtons = screen.getAllByRole('button');
    fireEvent.click(modalButtons[modalButtons.length - 1]);
    await waitFor(() => expect(mocks.decideRequest).toHaveBeenCalledWith('request-actionable', {
      expected_revision: 2, decision: 'approve', reason_codes: ['manual_approved'],
    }, expect.any(String)));
    await waitFor(() => expect(mocks.listProjectRequests).toHaveBeenCalledTimes(2));
  });
});
