import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Toast } from '@douyinfe/semi-ui';
import { ApiError } from '../../api/transport';
import { contentManagementApi, parseContentRegistration, type ContentConnection, type PayloadPolicy } from '../../api/contentManagement';
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
    Select: ({ value, optionList, onChange, disabled, ...props }: { value: string; optionList: { value: string; label: string }[]; onChange: (value: string) => void; disabled?: boolean; 'aria-label': string }) =>
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
  contentManagementApi: { getPolicy: vi.fn(), putPolicy: vi.fn(), getConnection: vi.fn(), control: vi.fn() },
}));

const config = { mode: 'disabled' as const, supported_protocols: ['chat'], observation_points: ['client_request'], allowed_purposes: [] };
const policy: PayloadPolicy = {
  schema_version: 'unirouter.payload-policy.v1', source_instance: 'synthetic-source', org_id: '1', project_id: '2',
  policy_ref: 'content-policy:1:2', desired: { policy_version: '1', config }, effective: { policy_version: '1', config },
  effective_at: '2026-09-28T00:00:00.000000Z', application_status: 'applied', application_reason: 'none', storage_status: 'not_configured',
  limits: { max_saved_bytes: 4096, max_read_bytes: 4096, max_buffer_bytes: 4096, max_manifest_bytes: 4096, max_manifest_items: 1 },
};
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
    vi.clearAllMocks();
    vi.mocked(contentManagementApi.getPolicy).mockResolvedValue(policy);
    vi.mocked(contentManagementApi.getConnection).mockResolvedValue(initial);
  });

  it('only reads on entry and leaves content off', async () => {
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    expect(await screen.findByText(/关闭 v1 \/ 关闭 v1/)).toBeTruthy();
    expect(screen.getByText(/存储状态/)).toBeTruthy();
    expect(contentManagementApi.getPolicy).toHaveBeenCalledWith(2);
    expect(contentManagementApi.getConnection).toHaveBeenCalledWith(2);
    expect(contentManagementApi.putPolicy).not.toHaveBeenCalled();
    expect(contentManagementApi.control).not.toHaveBeenCalled();
    expect(screen.queryByLabelText('正文保留秒数')).toBeNull();
  });

  it('accepts only the project-bound public content registration', () => {
    expect(parseContentRegistration(JSON.stringify(registration), initial)).toEqual(registration);
    for (const change of [{ bearer: 'secret' }, { project_id: '3' }, { profiles: ['unirouter.execution-context.v1'] }, { source_instance: 'elsewhere' }]) {
      expect(() => parseContentRegistration(JSON.stringify({ ...registration, ...change }), initial)).toThrow();
    }
  });

  it('requires explicit retention and limits before a full-content save', async () => {
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    await screen.findByText(/关闭 v1 \/ 关闭 v1/);
    fireEvent.change(screen.getByLabelText('正文策略模式'), { target: { value: 'full_content' } });
    expect(screen.getByLabelText('正文保留秒数')).toHaveValue('');
    expect(screen.getByLabelText('正文项目配额字节数')).toHaveValue('');
    fireEvent.click(screen.getByRole('button', { name: '保存项目策略' }));
    fireEvent.click(screen.getByRole('button', { name: '确定' }));
    expect(await screen.findByText('保留秒数必须显式填写正整数')).toBeTruthy();
    expect(contentManagementApi.putPolicy).not.toHaveBeenCalled();
  });

  it('saves with the read policy version and re-reads before reporting success', async () => {
    const next = { ...policy, desired: { policy_version: '2', config }, effective: { policy_version: '2', config } };
    vi.mocked(contentManagementApi.putPolicy).mockResolvedValue(next);
    vi.mocked(contentManagementApi.getPolicy).mockResolvedValueOnce(policy).mockResolvedValue(next);
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    await screen.findByText(/关闭 v1 \/ 关闭 v1/);
    fireEvent.click(screen.getByRole('button', { name: '保存项目策略' }));
    fireEvent.click(await screen.findByRole('button', { name: '确定' }));
    await waitFor(() => expect(contentManagementApi.putPolicy).toHaveBeenCalledWith(2, '1', config));
    await screen.findByText(/关闭 v2 \/ 关闭 v2/);
    expect(contentManagementApi.control).not.toHaveBeenCalled();
  });

  it('refreshes a version conflict and never reports it as saved', async () => {
    vi.mocked(contentManagementApi.putPolicy).mockRejectedValue(new ApiError('Content policy version changed', 409, {}));
    vi.mocked(contentManagementApi.getPolicy).mockResolvedValueOnce(policy).mockResolvedValue({ ...policy, desired: { policy_version: '2', config }, effective: { policy_version: '2', config } });
    const success = vi.spyOn(Toast, 'success');
    render(<XInsightContentManagement projectId={2} expansionAllowed />);
    await screen.findByText(/关闭 v1 \/ 关闭 v1/);
    fireEvent.click(screen.getByRole('button', { name: '保存项目策略' }));
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
    expect(screen.getByRole('button', { name: '保存项目策略' })).toBeDisabled();
    fireEvent.change(screen.getByLabelText('正文策略模式'), { target: { value: 'disabled' } });
    expect(screen.getByRole('button', { name: '保存项目策略' })).not.toBeDisabled();
    fireEvent.click(screen.getByRole('button', { name: '保存项目策略' }));
    fireEvent.click(screen.getByRole('button', { name: '确定' }));
    await waitFor(() => expect(contentManagementApi.putPolicy).toHaveBeenCalledWith(2, '1', config));
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
    expect(contentManagementApi.putPolicy).not.toHaveBeenCalled();
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
