import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Toast } from '@douyinfe/semi-ui';
import { ApiError } from '../../api/transport';
import { api } from '../../api/client';
import { contentManagementApi, parseContentRegistration, type ContentConnection, type PayloadPolicy, type ScopedPayloadPolicy } from '../../api/contentManagement';
import XInsightContentManagement from '../XInsightContentManagement';

// The integrated DataExchangePage suite mounts the real Semi widgets. These
// focused state-machine cases use native controls to avoid counting another
// copy of Semi's existing legacy DOM warning against the repository ratchet.
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
    Tag: ({ children }: { children: React.ReactNode }) => element('span', null, children),
    Descriptions: ({ data }: { data: { key: string; value: React.ReactNode }[] }) =>
      element('div', null, data.map(({ key, value }) => element('div', { key }, key, ': ', value))),
    Button: ({ children, onClick, disabled }: { children: React.ReactNode; onClick?: () => void; disabled?: boolean }) =>
      element('button', { type: 'button', onClick, disabled }, children),
    Input: ({ value, onChange, disabled, ...props }: { value: string; onChange: (value: string) => void; disabled?: boolean; 'aria-label': string }) =>
      element('input', { ...props, value, disabled, onChange: (event: React.ChangeEvent<HTMLInputElement>) => onChange(event.target.value) }),
    TextArea: ({ value, onChange, disabled, ...props }: { value: string; onChange: (value: string) => void; disabled?: boolean; 'aria-label': string }) =>
      element('textarea', { ...props, value, disabled, onChange: (event: React.ChangeEvent<HTMLTextAreaElement>) => onChange(event.target.value) }),
    Select: ({ value, optionList, onChange, disabled, filter: _filter, ...props }: { value: string; optionList: { value: string; label: string }[]; onChange: (value: string) => void; disabled?: boolean; filter?: boolean; 'aria-label': string }) =>
      element('select', { ...props, value, disabled, onChange: (event: React.ChangeEvent<HTMLSelectElement>) => onChange(event.target.value) },
        optionList.map(option => element('option', { key: option.value, value: option.value }, option.label))),
    Popconfirm: ({ children, onConfirm }: { children: React.ReactNode; onConfirm: () => void }) => {
      const [open, setOpen] = React.useState(false);
      return element('span', null, element('span', { onClick: () => setOpen(true) }, children),
        open && element('button', { type: 'button', onClick: () => { setOpen(false); onConfirm(); } }, '确定'));
    },
  };
});

vi.mock('../../api/contentManagement', async original => ({
  ...await original<typeof import('../../api/contentManagement')>(),
  contentManagementApi: { getPolicy: vi.fn(), putPolicy: vi.fn(), getScopedPolicy: vi.fn(), getEnterprisePolicy: vi.fn(), putScopedPolicy: vi.fn(), getConnection: vi.fn(), control: vi.fn() },
}));
vi.mock('../../api/client', () => ({ api: { getProjectMembers: vi.fn() } }));

const config = { mode: 'disabled' as const, supported_protocols: ['chat'], observation_points: ['client_request'], allowed_purposes: [] };
const policy: PayloadPolicy = {
  schema_version: 'unirouter.payload-policy.v1', source_instance: 'synthetic-source', org_id: '1', project_id: '2',
  policy_ref: 'content-policy:1:2', desired: { policy_version: '1', config }, effective: { policy_version: '1', config },
  effective_at: '2026-09-28T00:00:00.000000Z', application_status: 'applied', application_reason: 'none', storage_status: 'not_configured',
  limits: { max_saved_bytes: 32 * 1024 * 1024, max_read_bytes: 32 * 1024 * 1024, max_buffer_bytes: 4096, max_manifest_bytes: 4096, max_manifest_items: 1 },
};
// Keep the historical scalar fixtures for input edge cases, while presenting
// the V2 scoped response that the current page actually reads and writes.
function scopedPolicy(value: PayloadPolicy): ScopedPayloadPolicy {
  const version = value.desired.policy_version;
  return {
    schema_version: 'unirouter.payload-policy.v2', source_instance: value.source_instance,
    org_id: value.org_id, project_id: value.project_id,
    enterprise: {
      policy_ref: 'content-policy:1', explicit: true,
      desired: { policy_version: '1', config }, effective: { policy_version: '1', config },
      desired_execution_storage_mode: 'temporary_allowed', effective_execution_storage_mode: 'temporary_allowed',
    },
    project: {
      policy_ref: value.policy_ref, explicit: true, desired: value.desired, effective: value.effective,
      desired_execution_storage_mode: 'temporary_allowed', effective_execution_storage_mode: 'temporary_allowed',
      application_status: value.application_status, application_reason: value.application_reason,
      project_quota_bytes: value.desired.config.quota_bytes ?? 5 * 1024 ** 3,
    },
    effective: {
      scope: 'project', policy_ref: value.policy_ref, effective: value.effective,
      execution_storage_mode: 'temporary_allowed', project_quota_bytes: value.desired.config.quota_bytes ?? 5 * 1024 ** 3,
      version_vector: `vector-${version}`,
    },
    version_vector: `vector-${version}`, storage_status: value.storage_status,
    deployment_mode: 'full_content', full_content_enabled: true, limits: value.limits,
  };
}
const initial: ContentConnection = {
  schema_version: 'unirouter.source-content-control.v1', source_instance: 'synthetic-source', org_id: '1', project_id: '2',
  connection_status: 'paused', content_status: 'not_configured', state_version: '0', content_scope_revision: '0',
};
const key = { credential_id: '9007199254740993', registration_hash: `sha256:${'a'.repeat(64)}` };
const registration = {
  schema_version: 'unirouter.xinsight.source-credential-registration.v1', token_format: 'ur-xf1-hex256.v1',
  verifier_scheme: 'receiver_sha256_v1', key_prefix: 'ur-xf1-aaaaaaaaaaaaa', verifier_sha256: 'b'.repeat(64),
  operation_id: 'c'.repeat(32), source_instance: 'synthetic-source', org_id: '1', project_id: '2',
  profiles: ['unirouter.interaction-content.v1'],
};

describe('project content management', () => {
  beforeEach(() => {
    vi.resetAllMocks();
    vi.mocked(contentManagementApi.getPolicy).mockResolvedValue(policy);
    vi.mocked(contentManagementApi.getScopedPolicy).mockImplementation(async () => scopedPolicy(await contentManagementApi.getPolicy(2)));
    vi.mocked(contentManagementApi.getEnterprisePolicy).mockResolvedValue(scopedPolicy(policy));
    vi.mocked(api.getProjectMembers).mockResolvedValue({ data: [] } as never);
    vi.mocked(contentManagementApi.putScopedPolicy).mockImplementation(async (_scope, _org, projectId, _user, body) =>
      scopedPolicy(await contentManagementApi.putPolicy(projectId, body.expected_version, body.desired)));
    vi.mocked(contentManagementApi.getConnection).mockResolvedValue(initial);
  });

  it('only reads on entry and leaves content off', async () => {
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    expect(await screen.findByText(/关闭 v1 \/ 关闭 v1/)).toBeTruthy();
    expect(screen.getByText(/存储状态/)).toBeTruthy();
    expect(screen.getByText(/不脱敏（当前版本尚不支持脱敏）/)).toBeTruthy();
    expect(contentManagementApi.getScopedPolicy).toHaveBeenCalledWith(2, undefined);
    expect(contentManagementApi.getConnection).toHaveBeenCalledWith(2);
    expect(contentManagementApi.putScopedPolicy).not.toHaveBeenCalled();
    expect(contentManagementApi.control).not.toHaveBeenCalled();
    expect(screen.queryByLabelText('正文保留期限')).toBeNull();
    expect(screen.getByText(/已有任务与正文不会因修改期限自动延长或删除/)).toBeTruthy();
  });

  it('accepts only the project-bound public content registration', () => {
    expect(parseContentRegistration(JSON.stringify(registration), initial)).toEqual(registration);
    for (const change of [{ bearer: 'secret' }, { project_id: '3' }, { profiles: ['unirouter.execution-context.v1'] }, { source_instance: 'elsewhere' }]) {
      expect(() => parseContentRegistration(JSON.stringify({ ...registration, ...change }), initial)).toThrow();
    }
  });

  it('reads enterprise scope through its own V2 endpoint after obtaining the organization', async () => {
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    await screen.findByText(/关闭 v1 \/ 关闭 v1/);
    fireEvent.change(screen.getByLabelText('策略作用域'), { target: { value: 'enterprise' } });
    await waitFor(() => expect(contentManagementApi.getEnterprisePolicy).toHaveBeenCalledWith(1));
    expect(contentManagementApi.getScopedPolicy).toHaveBeenCalledTimes(2);
    expect(screen.getByLabelText('策略作用域')).toHaveValue('enterprise');
  });

  it('uses source capabilities for an unset enterprise draft instead of a project policy', async () => {
    const enterprise = scopedPolicy(policy);
    enterprise.enterprise = { policy_ref: 'content-policy:1:enterprise', explicit: false };
    enterprise.effective = undefined;
    enterprise.policy_capabilities = { supported_protocols: ['openai.responses'], observation_points: ['provider_response'] };
    vi.mocked(contentManagementApi.getEnterprisePolicy).mockResolvedValue(enterprise);
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    await screen.findByText(/关闭 v1 \/ 关闭 v1/);
    fireEvent.change(screen.getByLabelText('策略作用域'), { target: { value: 'enterprise' } });
    expect(await screen.findByText(/企业默认尚未设置；以下是未保存的关闭模式草稿/)).toBeTruthy();
    expect(screen.getByText(/openai.responses \/ provider_response/)).toBeTruthy();
  });

  it('recommends full content without saving when an unset enterprise source is ready', async () => {
    const enterprise = scopedPolicy(policy);
    enterprise.enterprise = { policy_ref: 'content-policy:1:enterprise', explicit: false };
    enterprise.effective = undefined;
    enterprise.storage_status = 'ready';
    enterprise.limits = { ...enterprise.limits, max_saved_bytes: 1024 };
    enterprise.policy_capabilities = { supported_protocols: ['openai.responses'], observation_points: ['provider_response'] };
    vi.mocked(contentManagementApi.getEnterprisePolicy).mockResolvedValue(enterprise);
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    await screen.findByText(/关闭 v1 \/ 关闭 v1/);
    fireEvent.change(screen.getByLabelText('策略作用域'), { target: { value: 'enterprise' } });
    expect(await screen.findByText(/未保存的完整正文推荐草稿/)).toBeTruthy();
    expect(screen.getByLabelText('正文策略模式')).toHaveValue('full_content');
    expect(screen.getByText(/openai.responses \/ provider_response/)).toBeTruthy();
    expect(screen.getByLabelText('正文项目容量')).toHaveValue('5');
    expect(screen.getByLabelText('正文单项上限')).toHaveValue('1024');
    fireEvent.click(screen.getByRole('button', { name: '使用推荐设置' }));
    expect(screen.getByLabelText('正文单项上限')).toHaveValue('1024');
    expect(contentManagementApi.putScopedPolicy).not.toHaveBeenCalled();
  });

  it('loads only active project members before reading an exact personal exception', async () => {
    vi.mocked(api.getProjectMembers).mockResolvedValue({ data: [
      { user_id: 3, username: 'active', status: 'active' },
      { user_id: 4, username: 'inactive', status: 'disabled' },
    ] } as never);
    vi.mocked(contentManagementApi.getScopedPolicy).mockImplementation(async (_projectId, userId) => {
      const view = scopedPolicy(policy);
      return userId === 3 ? { ...view, user_id: '3', user: { ...view.project!, policy_ref: 'content-policy:1:2:3' } } : view;
    });
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    await screen.findByText(/关闭 v1 \/ 关闭 v1/);
    fireEvent.change(screen.getByLabelText('策略作用域'), { target: { value: 'user' } });
    await waitFor(() => expect(api.getProjectMembers).toHaveBeenCalledWith(2, 1, 100, ''));
    expect(screen.getByRole('option', { name: /active/ })).toBeTruthy();
    expect(screen.queryByRole('option', { name: /inactive/ })).toBeNull();
    fireEvent.change(screen.getByLabelText('选择项目成员'), { target: { value: '3' } });
    await waitFor(() => expect(contentManagementApi.getScopedPolicy).toHaveBeenCalledWith(2, 3));
    expect(screen.getByLabelText('策略作用域')).toHaveValue('user');
  });

  it('shows recommendations before save, restores them without writing, and uses them for cleared fields', async () => {
    const fullConfig = { ...config, mode: 'full_content' as const, retention_seconds: 30 * 86400, quota_bytes: 5 * 1024 ** 3, max_saved_bytes: 32 * 1024 ** 2, allowed_purposes: ['human_review'], on_limit: 'record_capture_gap' as const };
    const next = { ...policy, desired: { policy_version: '2', config: fullConfig }, effective: { policy_version: '2', config: fullConfig } };
    vi.mocked(contentManagementApi.putPolicy).mockResolvedValue(next);
    vi.mocked(contentManagementApi.getPolicy).mockResolvedValueOnce(policy).mockResolvedValue(next);
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    await screen.findByText(/关闭 v1 \/ 关闭 v1/);
    fireEvent.change(screen.getByLabelText('正文策略模式'), { target: { value: 'full_content' } });
    expect(screen.getByLabelText('正文保留期限')).toHaveValue('30');
    expect(screen.getByLabelText('正文项目容量')).toHaveValue('5');
    expect(screen.getByLabelText('正文单项上限')).toHaveValue('32');
    expect(screen.getByText('保留期限')).toBeTruthy();
    expect(screen.getByText('项目容量')).toBeTruthy();
    expect(screen.getByText('单项上限')).toBeTruthy();
    expect(screen.getByText(/保存将使用：保留期限 30 天；项目容量 5 GiB；单项上限 32 MiB/)).toBeTruthy();
    fireEvent.change(screen.getByLabelText('正文保留期限'), { target: { value: '14' } });
    fireEvent.click(screen.getByRole('button', { name: '使用推荐设置' }));
    expect(screen.getByLabelText('正文保留期限')).toHaveValue('30');
    expect(contentManagementApi.putScopedPolicy).not.toHaveBeenCalled();
    for (const label of ['正文保留期限', '正文项目容量', '正文单项上限']) {
      fireEvent.change(screen.getByLabelText(label), { target: { value: '' } });
    }
    expect(screen.getByText(/保存将使用：保留期限 30 天；项目容量 5 GiB；单项上限 32 MiB/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '保存作用域策略' }));
    fireEvent.click(await screen.findByRole('button', { name: '确定' }));
    await waitFor(() => expect(contentManagementApi.putScopedPolicy).toHaveBeenCalledWith('project', 1, 2, undefined, expect.objectContaining({ expected_policy_ref: policy.policy_ref, expected_version: '1', expected_vector: 'vector-1', desired: fullConfig, execution_storage_mode: 'temporary_allowed' })));
    expect(contentManagementApi.control).not.toHaveBeenCalled();
  });

  it('keeps custom server values exact through refresh and save, including non-divisible units', async () => {
    const fullConfig = { ...config, mode: 'full_content' as const, retention_seconds: 86401, quota_bytes: 5 * 1024 ** 3 + 1, max_saved_bytes: 32 * 1024 ** 2 - 1, allowed_purposes: ['human_review'], on_limit: 'record_capture_gap' as const };
    const existing = { ...policy, desired: { policy_version: '1', config: fullConfig }, effective: { policy_version: '1', config: fullConfig } };
    const next = { ...existing, desired: { policy_version: '2', config: fullConfig }, effective: { policy_version: '2', config: fullConfig } };
    vi.mocked(contentManagementApi.getPolicy).mockResolvedValueOnce(existing).mockResolvedValueOnce(existing).mockResolvedValue(next);
    vi.mocked(contentManagementApi.putPolicy).mockResolvedValue(next);
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    await screen.findByText(/完整正文 v1 \/ 完整正文 v1/);
    expect(screen.getByLabelText('正文保留期限')).toHaveValue('86401');
    expect(screen.getByLabelText('正文保留期限单位')).toHaveValue('秒');
    expect(screen.getByLabelText('正文项目容量')).toHaveValue(String(fullConfig.quota_bytes));
    expect(screen.getByLabelText('正文项目容量单位')).toHaveValue('字节');
    expect(screen.getByLabelText('正文单项上限')).toHaveValue(String(fullConfig.max_saved_bytes));
    fireEvent.click(screen.getByRole('button', { name: '刷新策略' }));
    await waitFor(() => expect(contentManagementApi.getScopedPolicy).toHaveBeenCalledTimes(2));
    expect(screen.getByLabelText('正文保留期限')).toHaveValue('86401');
    fireEvent.click(screen.getByRole('button', { name: '保存作用域策略' }));
    fireEvent.click(await screen.findByRole('button', { name: '确定' }));
    await waitFor(() => expect(contentManagementApi.putScopedPolicy).toHaveBeenCalledWith('project', 1, 2, undefined, expect.objectContaining({ expected_policy_ref: policy.policy_ref, expected_version: '1', expected_vector: 'vector-1', desired: fullConfig, execution_storage_mode: 'temporary_allowed' })));
  });

  it('accepts user-chosen hours and MiB without changing the integer API contract', async () => {
    const fullConfig = { ...config, mode: 'full_content' as const, retention_seconds: 48 * 3600, quota_bytes: 512 * 1024 ** 2, max_saved_bytes: 16 * 1024 ** 2, allowed_purposes: ['human_review'], on_limit: 'record_capture_gap' as const };
    const next = { ...policy, desired: { policy_version: '2', config: fullConfig }, effective: { policy_version: '2', config: fullConfig } };
    vi.mocked(contentManagementApi.getPolicy).mockResolvedValueOnce(policy).mockResolvedValue(next);
    vi.mocked(contentManagementApi.putPolicy).mockResolvedValue(next);
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    await screen.findByText(/关闭 v1 \/ 关闭 v1/);
    fireEvent.change(screen.getByLabelText('正文策略模式'), { target: { value: 'full_content' } });
    fireEvent.change(screen.getByLabelText('正文保留期限单位'), { target: { value: '小时' } });
    fireEvent.change(screen.getByLabelText('正文保留期限'), { target: { value: '48' } });
    fireEvent.change(screen.getByLabelText('正文项目容量单位'), { target: { value: 'MiB' } });
    fireEvent.change(screen.getByLabelText('正文项目容量'), { target: { value: '512' } });
    fireEvent.change(screen.getByLabelText('正文单项上限'), { target: { value: '16' } });
    expect(screen.getByText(/保留期限 48 小时；项目容量 512 MiB；单项上限 16 MiB/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '保存作用域策略' }));
    fireEvent.click(await screen.findByRole('button', { name: '确定' }));
    await waitFor(() => expect(contentManagementApi.putScopedPolicy).toHaveBeenCalledWith('project', 1, 2, undefined, expect.objectContaining({ expected_policy_ref: policy.policy_ref, expected_version: '1', expected_vector: 'vector-1', desired: fullConfig, execution_storage_mode: 'temporary_allowed' })));
  });

  it.each([
    ['正文保留期限', '-1', '保留期限必须显式填写正整数'],
    ['正文项目容量', 'abc', '项目容量必须显式填写正整数'],
    ['正文单项上限', '9007199254740992', '单项上限超出安全整数范围'],
    ['正文单项上限', '33', '单项上限不得超过部署上限'],
  ])('rejects invalid or over-limit input in %s', async (label, value, message) => {
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    await screen.findByText(/关闭 v1 \/ 关闭 v1/);
    fireEvent.change(screen.getByLabelText('正文策略模式'), { target: { value: 'full_content' } });
    fireEvent.change(screen.getByLabelText(label), { target: { value } });
    fireEvent.click(screen.getByRole('button', { name: '保存作用域策略' }));
    fireEvent.click(await screen.findByRole('button', { name: '确定' }));
    expect(await screen.findByText(message)).toBeTruthy();
    expect(contentManagementApi.putScopedPolicy).not.toHaveBeenCalled();
  });

  it('rejects an item larger than the project capacity', async () => {
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    await screen.findByText(/关闭 v1 \/ 关闭 v1/);
    fireEvent.change(screen.getByLabelText('正文策略模式'), { target: { value: 'full_content' } });
    fireEvent.change(screen.getByLabelText('正文项目容量单位'), { target: { value: 'MiB' } });
    fireEvent.change(screen.getByLabelText('正文项目容量'), { target: { value: '1' } });
    fireEvent.click(screen.getByRole('button', { name: '保存作用域策略' }));
    fireEvent.click(await screen.findByRole('button', { name: '确定' }));
    expect(await screen.findByText('单项上限不得超过项目容量')).toBeTruthy();
    expect(contentManagementApi.putScopedPolicy).not.toHaveBeenCalled();
  });

  it('does not round a non-divisible server value when changing units', async () => {
    const fullConfig = { ...config, mode: 'full_content' as const, retention_seconds: 86401, quota_bytes: 5 * 1024 ** 3, max_saved_bytes: 32 * 1024 ** 2, allowed_purposes: ['human_review'], on_limit: 'record_capture_gap' as const };
    vi.mocked(contentManagementApi.getPolicy).mockResolvedValue({ ...policy, desired: { policy_version: '1', config: fullConfig }, effective: { policy_version: '1', config: fullConfig } });
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    await screen.findByText(/完整正文 v1 \/ 完整正文 v1/);
    fireEvent.change(screen.getByLabelText('正文保留期限单位'), { target: { value: '天' } });
    expect(await screen.findByText(/保留期限不能整除为天/)).toBeTruthy();
    expect(screen.getByLabelText('正文保留期限单位')).toHaveValue('秒');
    expect(screen.getByLabelText('正文保留期限')).toHaveValue('86401');
    expect(contentManagementApi.putScopedPolicy).not.toHaveBeenCalled();
  });

  it('saves with the read policy version and re-reads before reporting success', async () => {
    const next = { ...policy, desired: { policy_version: '2', config }, effective: { policy_version: '2', config } };
    vi.mocked(contentManagementApi.putPolicy).mockResolvedValue(next);
    vi.mocked(contentManagementApi.getPolicy).mockResolvedValueOnce(policy).mockResolvedValue(next);
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    await screen.findByText(/关闭 v1 \/ 关闭 v1/);
    fireEvent.click(screen.getByRole('button', { name: '保存作用域策略' }));
    fireEvent.click(await screen.findByRole('button', { name: '确定' }));
    await waitFor(() => expect(contentManagementApi.putScopedPolicy).toHaveBeenCalledWith('project', 1, 2, undefined, expect.objectContaining({ expected_policy_ref: policy.policy_ref, expected_version: '1', expected_vector: 'vector-1', desired: config, execution_storage_mode: 'temporary_allowed' })));
    await screen.findByText(/关闭 v2 \/ 关闭 v2/);
    expect(contentManagementApi.control).not.toHaveBeenCalled();
  });

  it('keeps metadata-only storage-free and never offers or sends redaction', async () => {
    const metadataConfig = { ...config, mode: 'metadata_only' as const };
    const next = { ...policy, desired: { policy_version: '2', config: metadataConfig }, effective: { policy_version: '2', config: metadataConfig } };
    vi.mocked(contentManagementApi.putPolicy).mockResolvedValue(next);
    vi.mocked(contentManagementApi.getPolicy).mockResolvedValueOnce(policy).mockResolvedValue(next);
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    await screen.findByText(/关闭 v1 \/ 关闭 v1/);
    fireEvent.change(screen.getByLabelText('正文策略模式'), { target: { value: 'metadata_only' } });
    expect(contentManagementApi.putScopedPolicy).not.toHaveBeenCalled();
    expect(screen.queryByLabelText('正文保留期限')).toBeNull();
    expect(screen.queryByRole('switch', { name: /脱敏/ })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '保存作用域策略' }));
    fireEvent.click(await screen.findByRole('button', { name: '确定' }));
    await waitFor(() => expect(contentManagementApi.putScopedPolicy).toHaveBeenCalledWith('project', 1, 2, undefined, expect.objectContaining({ expected_policy_ref: policy.policy_ref, expected_version: '1', expected_vector: 'vector-1', desired: metadataConfig, execution_storage_mode: 'temporary_allowed' })));
    expect(JSON.stringify(vi.mocked(contentManagementApi.putScopedPolicy).mock.calls[0][4])).not.toContain('redaction_ref');
  });

  it('refreshes a version conflict and never reports it as saved', async () => {
    vi.mocked(contentManagementApi.putPolicy).mockRejectedValue(new ApiError('Content policy version changed', 409, {}));
    vi.mocked(contentManagementApi.getPolicy).mockResolvedValueOnce(policy).mockResolvedValue({ ...policy, desired: { policy_version: '2', config }, effective: { policy_version: '2', config } });
    const success = vi.spyOn(Toast, 'success');
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    await screen.findByText(/关闭 v1 \/ 关闭 v1/);
    fireEvent.click(screen.getByRole('button', { name: '保存作用域策略' }));
    fireEvent.click(await screen.findByRole('button', { name: '确定' }));
    expect(await screen.findByText('Content policy version changed')).toBeTruthy();
    expect(success).not.toHaveBeenCalled();
    await screen.findByText(/关闭 v2 \/ 关闭 v2/);
    success.mockRestore();
  });

  it('keeps a project policy disable action available without expansion entitlement', async () => {
    const fullConfig = { ...config, mode: 'full_content' as const, retention_seconds: 3600, quota_bytes: 8192, max_saved_bytes: 4096, allowed_purposes: ['human_review'], on_limit: 'record_capture_gap' as const };
    const full = { ...policy, desired: { policy_version: '1', config: fullConfig }, effective: { policy_version: '1', config: fullConfig } };
    const disabled = { ...policy, desired: { policy_version: '2', config }, effective: { policy_version: '2', config } };
    vi.mocked(contentManagementApi.getPolicy).mockResolvedValueOnce(full).mockResolvedValue(disabled);
    vi.mocked(contentManagementApi.putPolicy).mockResolvedValue(disabled);
    render(<XInsightContentManagement projectId={2} expansionAllowed={false} />);
    await screen.findByText(/完整正文 v1 \/ 完整正文 v1/);
    expect(screen.getByRole('button', { name: '保存作用域策略' })).toBeDisabled();
    fireEvent.change(screen.getByLabelText('正文策略模式'), { target: { value: 'disabled' } });
    expect(screen.getByRole('button', { name: '保存作用域策略' })).not.toBeDisabled();
    fireEvent.click(screen.getByRole('button', { name: '保存作用域策略' }));
    fireEvent.click(screen.getByRole('button', { name: '确定' }));
    await waitFor(() => expect(contentManagementApi.putScopedPolicy).toHaveBeenCalledWith('project', 1, 2, undefined, expect.objectContaining({ expected_policy_ref: policy.policy_ref, expected_version: '1', expected_vector: 'vector-1', desired: config, execution_storage_mode: 'temporary_allowed' })));
  });

  it('uses exact read-back state version, credential ID and hash across preparation, activation, pause and resume', async () => {
    let current: ContentConnection = initial;
    vi.mocked(contentManagementApi.getConnection).mockImplementation(async () => current);
    vi.mocked(contentManagementApi.control).mockImplementation(async (_project, action, body) => {
      expect(body.expected_state_version).toBe(current.state_version);
      if (action === 'prepare') current = { ...current, content_status: 'paused', state_version: '1', content_scope_revision: '1', staged: key };
      if (action === 'activate') current = { ...current, content_status: 'active', state_version: '2', content_scope_revision: '2', staged: undefined, active: key };
      if (action === 'pause') current = { ...current, content_status: 'paused', state_version: '3', content_scope_revision: '3' };
      if (action === 'resume') current = { ...current, content_status: 'active', state_version: '4' };
      return { action, operation_id: body.operation_id, state_version: current.state_version, content_scope_revision: current.content_scope_revision, ...key };
    });
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    await screen.findByRole('button', { name: '准备正文连接' });
    fireEvent.change(screen.getByLabelText('正文凭据注册信息'), { target: { value: JSON.stringify(registration) } });
    fireEvent.change(screen.getByLabelText('正文接收端精确 IP'), { target: { value: '127.0.0.1/32' } });
    fireEvent.click(screen.getByRole('button', { name: '准备正文连接' }));
    await waitFor(() => expect(contentManagementApi.control).toHaveBeenCalledWith(2, 'prepare', {
      operation_id: registration.operation_id, expected_state_version: '0', registration, allowed_ip: '127.0.0.1/32',
    }));
    fireEvent.click(await screen.findByRole('button', { name: '激活正文凭据' }));
    fireEvent.click(await screen.findByRole('button', { name: '确定' }));
    await waitFor(() => expect(contentManagementApi.control).toHaveBeenCalledWith(2, 'activate', expect.objectContaining({
      expected_state_version: '1', credential_id: key.credential_id, registration_hash: key.registration_hash,
    })));
    fireEvent.click(await screen.findByRole('button', { name: '暂停正文连接' }));
    await waitFor(() => expect(contentManagementApi.control).toHaveBeenCalledWith(2, 'pause', expect.objectContaining({ expected_state_version: '2', ...key })));
    fireEvent.click(await screen.findByRole('button', { name: '恢复正文连接' }));
    await waitFor(() => expect(contentManagementApi.control).toHaveBeenCalledWith(2, 'resume', expect.objectContaining({ expected_state_version: '3', ...key })));
    expect(contentManagementApi.putScopedPolicy).not.toHaveBeenCalled();
  });

  it('does not report control success when the read-back version differs', async () => {
    const success = vi.spyOn(Toast, 'success');
    vi.mocked(contentManagementApi.control).mockResolvedValue({ action: 'prepare', operation_id: registration.operation_id, state_version: '1', content_scope_revision: '1', ...key });
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    await screen.findByRole('button', { name: '准备正文连接' });
    fireEvent.change(screen.getByLabelText('正文凭据注册信息'), { target: { value: JSON.stringify(registration) } });
    fireEvent.change(screen.getByLabelText('正文接收端精确 IP'), { target: { value: '127.0.0.1/32' } });
    fireEvent.click(screen.getByRole('button', { name: '准备正文连接' }));
    expect(await screen.findByText(/操作响应与回读状态不一致/)).toBeTruthy();
    expect(success).not.toHaveBeenCalled();
    success.mockRestore();
  });

  it('refreshes C state on a version conflict without reporting success', async () => {
    const paused: ContentConnection = { ...initial, content_status: 'paused', state_version: '1', content_scope_revision: '1', active: key };
    vi.mocked(contentManagementApi.getConnection).mockResolvedValueOnce(paused).mockResolvedValue({ ...paused, state_version: '2' });
    vi.mocked(contentManagementApi.control).mockRejectedValue(new ApiError('state version conflict', 409, {}));
    const success = vi.spyOn(Toast, 'success');
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    fireEvent.click(await screen.findByRole('button', { name: '恢复正文连接' }));
    expect(await screen.findByText('state version conflict')).toBeTruthy();
    expect(success).not.toHaveBeenCalled();
    expect(await screen.findByText(/状态版本 \/ 访问修订: 2 \/ 1/)).toBeTruthy();
    success.mockRestore();
  });
});
