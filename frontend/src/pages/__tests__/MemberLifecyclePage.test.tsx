import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { useAuthStore } from '../../store/auth';
import MemberLifecyclePage from '../MemberLifecyclePage';

const mocks = vi.hoisted(() => ({
  getMemberLifecycleContract: vi.fn(),
  previewMemberLifecycle: vi.fn(),
  getEnterpriseDirectoryIAMActions: vi.fn(),
}));

vi.mock('../../api/client', async () => {
  const actual = await vi.importActual<any>('../../api/client');
  return { ...actual, api: mocks };
});

const EVENTS = [
  {
    contract_version: 'unirouter.member-lifecycle.v1',
    event_id: 'create-001',
    subject_ref: `sha256:${'a'.repeat(64)}`,
    operation: 'create',
    revision: '1',
    after: { status: 'active', memberships: [{}], capabilities: [{}] },
  },
  {
    contract_version: 'unirouter.member-lifecycle.v1',
    event_id: 'suspend-002',
    subject_ref: `sha256:${'a'.repeat(64)}`,
    operation: 'suspend',
    revision: '2',
    after: { status: 'suspended', memberships: [{}], capabilities: [{}] },
  },
];

const REPORT = {
  report_version: 'unirouter.member-lifecycle-report.v1',
  event_id: 'create-001',
  subject_ref: `sha256:${'a'.repeat(64)}`,
  operation: 'create',
  revision: '1',
  state_status: 'active',
  input_semantic_hash: `sha256:${'b'.repeat(64)}`,
  effective_memberships: [{}],
  effective_capabilities: [{}],
  revocations: [],
  controls: {
    deny_admin_sessions: false,
    revoke_live_admin_sessions: false,
    disable_personal_keys_before_delete: false,
    agent_capability_recheck_seconds: '15',
  },
  local_projection: {
    user_status: 'active',
    membership_storage: 'replace_atomically',
    personal_key_handling: 'authority_gated',
    session_handling: 'revoke_on_role_or_status_change',
  },
  vendor_adapter_authorized: false,
  runtime_mutation: 'none',
};

describe('MemberLifecyclePage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.setState({
      user: {
        id: 1,
        username: 'admin',
        role: 'platform_admin',
        organization_role: 'platform_admin',
        org_id: 1,
        license_access: null,
        customer_context: { state: 'ready', org_id: 9, org_name: 'First Customer' },
        identity_scope: 'deployment_operator',
      },
    });
    mocks.getMemberLifecycleContract.mockResolvedValue({
      contract_version: 'unirouter.member-lifecycle.v1',
      report_version: 'unirouter.member-lifecycle-report.v1',
      operations: ['create', 'update', 'suspend', 'deprovision'],
      sample_events: EVENTS,
      artifacts: {
        schema: { path: 'schema.json', sha256: `sha256:${'c'.repeat(64)}` },
        fixtures: [],
        golden_report: { path: 'report.json', sha256: `sha256:${'d'.repeat(64)}` },
      },
      authority_boundary: {
        directory_vendor_selected: false,
        idp_network_access: false,
        runtime_mutation: 'none',
      },
    });
    mocks.previewMemberLifecycle.mockResolvedValue(REPORT);
    mocks.getEnterpriseDirectoryIAMActions.mockResolvedValue({ data: [], total: 0 });
  });

  it('auto-selects and validates the first safe event with a persistent inspector', async () => {
    render(<MemberLifecyclePage />);

    expect(screen.getByLabelText('成员生命周期检查器')).toBeTruthy();
    await waitFor(() => expect(mocks.getMemberLifecycleContract).toHaveBeenCalledTimes(1));
		await waitFor(() => expect(mocks.getEnterpriseDirectoryIAMActions).toHaveBeenCalledWith(20, 9));
    await waitFor(() => expect(mocks.previewMemberLifecycle).toHaveBeenCalledWith(JSON.stringify(EVENTS[0], null, 2)));
    expect(await screen.findByText('访问收敛证据')).toBeTruthy();
    expect(screen.getByText('replace_atomically')).toBeTruthy();
    expect(screen.getAllByText('否').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('none')).toBeTruthy();
    expect(screen.getByText(/只统计当前离线示例校验结果/)).toBeTruthy();
    expect(screen.getByText('示例保留项目关系')).toBeTruthy();
  });

	it('shows durable local convergence evidence separately from offline fixtures', async () => {
		mocks.getEnterpriseDirectoryIAMActions.mockResolvedValue({
			data: [{
				id: 17, snapshot_ref: `sha256:${'f'.repeat(64)}`, subject_kind: 'account',
				subject_ref: 'account-17', action: 'disable_user', reason: 'idp_account_disabled',
				result: 'applied', before_status: 'active', after_status: 'disabled',
				created_at: '2026-08-02T08:00:00Z',
			}],
		});
		render(<MemberLifecyclePage />);

		expect(await screen.findByText('account:account-17')).toBeTruthy();
		expect(screen.getByText('idp_account_disabled · active → disabled')).toBeTruthy();
		expect(screen.getByText('disable_user')).toBeTruthy();
	});

  it('switches fixture stages without an open-detail action', async () => {
    mocks.previewMemberLifecycle
      .mockResolvedValueOnce(REPORT)
      .mockResolvedValueOnce({
        ...REPORT,
        event_id: 'suspend-002',
        operation: 'suspend',
        revision: '2',
        state_status: 'suspended',
        effective_memberships: [],
        effective_capabilities: [],
        revocations: [{ kind: 'membership', ref: `sha256:${'e'.repeat(64)}`, reason: 'subject_suspended' }],
        controls: { ...REPORT.controls, deny_admin_sessions: true, revoke_live_admin_sessions: true },
        local_projection: { ...REPORT.local_projection, user_status: 'disabled', membership_storage: 'retained_but_ineffective' },
    });
    render(<MemberLifecyclePage />);
    await waitFor(() => expect(screen.getAllByText('create-001').length).toBeGreaterThanOrEqual(1));

    fireEvent.click(screen.getByText('suspend-002'));

    await waitFor(() => expect(mocks.previewMemberLifecycle).toHaveBeenLastCalledWith(JSON.stringify(EVENTS[1], null, 2)));
    expect(await screen.findByText('retained_but_ineffective')).toBeTruthy();
    expect(screen.getByText('membership:subject_suspended')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /查看|展开/ })).toBeNull();
  });
});
