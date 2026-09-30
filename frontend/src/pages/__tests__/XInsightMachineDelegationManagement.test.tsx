import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Toast } from '@douyinfe/semi-ui';
import XInsightMachineDelegationManagement from '../XInsightMachineDelegationManagement';
import { xInsightMachineApi, type PolicyDelegation, type SourceEnrollment } from '../../api/xInsightMachine';

// Reuse the native-control pattern from the content-management interaction tests.
// The requests remain mocked: these cases prove human choices and readback logic,
// not a browser or a live UR/XI connection.
vi.mock('@douyinfe/semi-ui', async () => {
  const React = await import('react');
  const element = React.createElement;
  return {
    Toast: { success: vi.fn(), error: vi.fn() },
    Typography: {
      Text: ({ children }: { children: React.ReactNode }) => element('span', null, children),
      Title: ({ children }: { children: React.ReactNode }) => element('h2', null, children),
    },
    Banner: ({ description }: { description: string }) => element('div', null, description),
    Descriptions: ({ data }: { data: { key: string; value: React.ReactNode }[] }) =>
      element('div', null, data.map(({ key, value }) => element('div', { key }, key, ': ', value))),
    Button: ({ children, onClick, disabled }: { children: React.ReactNode; onClick?: () => void; disabled?: boolean }) =>
      element('button', { type: 'button', onClick, disabled }, children),
    Input: ({ value, onChange, ...props }: { value: string; onChange: (value: string) => void; 'aria-label': string }) =>
      element('input', { ...props, value, onChange: (event: React.ChangeEvent<HTMLInputElement>) => onChange(event.target.value) }),
    TextArea: ({ value, onChange, ...props }: { value: string; onChange: (value: string) => void; 'aria-label': string }) =>
      element('textarea', { ...props, value, onChange: (event: React.ChangeEvent<HTMLTextAreaElement>) => onChange(event.target.value) }),
    Select: ({ value, optionList, onChange, multiple, ...props }: {
      value: string | string[]; optionList: { value: string; label: string }[];
      onChange: (value: string | string[]) => void; multiple?: boolean; 'aria-label': string;
    }) => element('select', { ...props, value, multiple,
      onChange: (event: React.ChangeEvent<HTMLSelectElement>) => onChange(multiple
        ? Array.from(event.target.selectedOptions, option => option.value) : event.target.value) },
    optionList.map(option => element('option', { key: option.value, value: option.value }, option.label))),
    Popconfirm: ({ children, onConfirm }: { children: React.ReactNode; onConfirm: () => void }) => {
      const [open, setOpen] = React.useState(false);
      return element('span', null, element('span', { onClick: () => setOpen(true) }, children),
        open && element('button', { type: 'button', onClick: () => { setOpen(false); onConfirm(); } }, '确定'));
    },
  };
});

vi.mock('../../api/xInsightMachine', () => ({ xInsightMachineApi: {
  listPolicyDelegations: vi.fn(), createPolicyDelegation: vi.fn(), revokePolicyDelegation: vi.fn(),
  listSourceEnrollments: vi.fn(), createSourceEnrollment: vi.fn(), revokeSourceEnrollment: vi.fn(),
  listCredentials: vi.fn(), registerCredential: vi.fn(), changeCredential: vi.fn(),
  listBindExistingCandidates: vi.fn(), bindExisting: vi.fn(),
} }));

const props = { orgId: 1, projectId: 2, installationId: 'xci_test', deploymentId: 'xi_test', sourceInstance: 'ur_test' };
let policies: PolicyDelegation[];
let enrollments: SourceEnrollment[];

const confirm = async (label: string) => {
  fireEvent.click(screen.getByRole('button', { name: label }));
  fireEvent.click(await screen.findByRole('button', { name: '确定' }));
};

describe('X-Insight human delegation management', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    policies = [];
    enrollments = [];
    vi.mocked(xInsightMachineApi.listPolicyDelegations).mockImplementation(async () => ({ data: [...policies] }));
    vi.mocked(xInsightMachineApi.listSourceEnrollments).mockImplementation(async () => ({ data: [...enrollments] }));
    vi.mocked(xInsightMachineApi.listCredentials).mockResolvedValue({ data: [] });
    vi.mocked(xInsightMachineApi.createPolicyDelegation).mockImplementation(async (_org, body) => {
      policies = [{ delegation_id: '11', target_kind: body.target_kind, coverage: body.coverage,
        project_id: body.project_id || '', user_id: body.user_id || '', read: body.read, write: body.write,
        grantor_user_id: '7', revision: '1', state: 'active' }];
      return { delegation_id: '11', revision: '1' };
    });
    vi.mocked(xInsightMachineApi.createSourceEnrollment).mockImplementation(async (_org, body) => {
      enrollments = [{ enrollment_id: '21', coverage: body.coverage, project_id: body.project_id || '',
        profiles: body.profiles, include_content: body.include_content, grantor_user_id: '7',
        revision: '1', state: 'active' }];
      return { enrollment_id: '21', revision: '1' };
    });
    vi.mocked(xInsightMachineApi.revokePolicyDelegation).mockImplementation(async (_org, grant) => {
      policies = policies.map(item => item.delegation_id === grant.delegation_id
        ? { ...item, revision: '2', state: 'revoked' } : item);
      return { delegation_id: grant.delegation_id, revision: '2' };
    });
    vi.mocked(xInsightMachineApi.revokeSourceEnrollment).mockImplementation(async (_org, grant) => {
      enrollments = enrollments.map(item => item.enrollment_id === grant.enrollment_id
        ? { ...item, revision: '2', state: 'revoked' } : item);
      return { enrollment_id: grant.enrollment_id, revision: '2' };
    });
  });

  it('grants current and future projects separately for policy and data, re-reads, then revokes each', async () => {
    render(<XInsightMachineDelegationManagement {...props} orgAdmin />);
    await screen.findByText('尚未授权策略访问。');
    expect(screen.getByLabelText('策略范围')).toHaveValue('all_current_future');
    expect(screen.getByLabelText('供数范围')).toHaveValue('all_current_future');
    await confirm('授权策略访问');
    await waitFor(() => expect(xInsightMachineApi.createPolicyDelegation).toHaveBeenCalledWith(1, {
      installation_id: 'xci_test', target_kind: 'project_policy', coverage: 'all_current_future',
      read: true, write: true,
    }));
    expect(await screen.findByText('项目策略 · 当前及未来项目 · 可查看与修改 · 已授权')).toBeTruthy();
    await confirm('授权数据读取');
    await waitFor(() => expect(xInsightMachineApi.createSourceEnrollment).toHaveBeenCalledWith(1, {
      installation_id: 'xci_test', coverage: 'all_current_future',
      profiles: ['unirouter.cost-basis.v1', 'unirouter.execution-context.v1', 'unirouter.execution-identity.v1'],
      include_content: true,
    }));
    expect(await screen.findByText(/当前及未来项目 · 费用依据、调用事实、账号归属 · 完整正文供人工核查 · 已授权/)).toBeTruthy();
    expect(xInsightMachineApi.listPolicyDelegations).toHaveBeenCalledTimes(3);
    expect(xInsightMachineApi.listSourceEnrollments).toHaveBeenCalledTimes(3);
    expect(xInsightMachineApi.registerCredential).not.toHaveBeenCalled();

    fireEvent.click(screen.getAllByRole('button', { name: '撤销' })[0]);
    fireEvent.click(await screen.findByRole('button', { name: '确定' }));
    await waitFor(() => expect(xInsightMachineApi.revokePolicyDelegation).toHaveBeenCalledWith(1,
      expect.objectContaining({ delegation_id: '11', revision: '1' })));
    expect(await screen.findByText('项目策略 · 当前及未来项目 · 可查看与修改 · 已撤销')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '撤销' }));
    fireEvent.click(await screen.findByRole('button', { name: '确定' }));
    await waitFor(() => expect(xInsightMachineApi.revokeSourceEnrollment).toHaveBeenCalledWith(1,
      expect.objectContaining({ enrollment_id: '21', revision: '1' })));
    expect(await screen.findByText(/当前及未来项目 · 费用依据、调用事实、账号归属 · 完整正文供人工核查 · 已撤销/)).toBeTruthy();
  });

  it('limits a project administrator to the current project and keeps machine purposes distinct', async () => {
    render(<XInsightMachineDelegationManagement {...props} orgAdmin={false} />);
    await screen.findByText('尚未授权策略访问。');
    expect(screen.getByLabelText('策略范围')).toHaveValue('exact');
    expect(screen.getByLabelText('供数范围')).toHaveValue('exact');
    expect(screen.getByLabelText('策略范围').querySelector('option[value="all_current_future"]')).toBeNull();
    expect(screen.getByLabelText('供数范围').querySelector('option[value="all_current_future"]')).toBeNull();
    await confirm('授权策略访问');
    expect(await screen.findByText('项目策略 · 指定项目 · 可查看与修改 · 已授权')).toBeTruthy();
    await confirm('授权数据读取');
    expect(await screen.findByText(/指定项目 · 费用依据、调用事实、账号归属 · 完整正文供人工核查 · 已授权/)).toBeTruthy();
    expect(xInsightMachineApi.createPolicyDelegation).toHaveBeenCalledWith(1,
      expect.objectContaining({ coverage: 'exact', project_id: '2' }));
    expect(xInsightMachineApi.createSourceEnrollment).toHaveBeenCalledWith(1,
      expect.objectContaining({ coverage: 'exact', project_id: '2' }));
    expect(xInsightMachineApi.listCredentials).not.toHaveBeenCalled();
    expect(screen.queryByLabelText('机器凭据用途')).toBeNull();
  });

  it('lets an organization administrator narrow both grants to the current project', async () => {
    render(<XInsightMachineDelegationManagement {...props} orgAdmin />);
    await screen.findByText('尚未授权策略访问。');
    fireEvent.change(screen.getByLabelText('策略范围'), { target: { value: 'exact' } });
    fireEvent.change(screen.getByLabelText('供数范围'), { target: { value: 'exact' } });
    fireEvent.change(screen.getByLabelText('策略权限'), { target: { value: 'read' } });
    await confirm('授权策略访问');
    expect(await screen.findByText('项目策略 · 指定项目 · 仅可查看 · 已授权')).toBeTruthy();
    await confirm('授权数据读取');
    expect(await screen.findByText(/指定项目 · 费用依据、调用事实、账号归属 · 完整正文供人工核查 · 已授权/)).toBeTruthy();
    expect(xInsightMachineApi.createPolicyDelegation).toHaveBeenCalledWith(1,
      expect.objectContaining({ coverage: 'exact', project_id: '2', read: true, write: false }));
    expect(xInsightMachineApi.createSourceEnrollment).toHaveBeenCalledWith(1,
      expect.objectContaining({ coverage: 'exact', project_id: '2' }));
  });

  it('labels policy.manage and source.enroll credentials separately and withholds success on failed readback', async () => {
    vi.mocked(xInsightMachineApi.listCredentials).mockResolvedValue({ data: [
      { key_id: '31', key_prefix: 'ur-xpm1-test', purpose: 'xinsight.policy.manage', status: 'feed_active', operation_id: 'a' },
      { key_id: '32', key_prefix: 'ur-xse1-test', purpose: 'xinsight.source.enroll', status: 'feed_staged', operation_id: 'b' },
    ] });
    render(<XInsightMachineDelegationManagement {...props} orgAdmin />);
    expect(await screen.findByText('策略管理 · 已激活')).toBeTruthy();
    expect(screen.getByText('数据接入 · 待激活')).toBeTruthy();
    expect(screen.getByLabelText('机器凭据用途')).toHaveValue('xinsight.policy.manage');
    fireEvent.change(screen.getByLabelText('机器凭据用途'), { target: { value: 'xinsight.source.enroll' } });
    expect(screen.getByLabelText('机器凭据用途')).toHaveValue('xinsight.source.enroll');
    vi.mocked(xInsightMachineApi.listPolicyDelegations).mockRejectedValueOnce(new Error('readback failed'));
    await confirm('授权策略访问');
    expect(await screen.findByText('readback failed')).toBeTruthy();
    expect(Toast.success).not.toHaveBeenCalled();
  });
});
