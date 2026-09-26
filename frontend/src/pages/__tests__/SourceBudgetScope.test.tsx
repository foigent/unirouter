import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import SourceBudgetScope from '../SourceBudgetScope';
import { sourceBudgetBindingsApi, type SourceBudgetBinding } from '../../api/sourceConnections';

vi.mock('../../api/sourceConnections', async original => ({
  ...await original<typeof import('../../api/sourceConnections')>(),
  sourceBudgetBindingsApi: { list: vi.fn(), set: vi.fn() },
}));
const binding: SourceBudgetBinding = { scope_kind: 'project', scope_ref: 'synthetic-enterprise-project', project_id: '2', revision: '3', status: 'active', audit_ref: 'audit:' + 'a'.repeat(64), changed_at: '2026-09-10T00:00:00Z' };
const page = { data: [binding], has_more: false, next_after: '', org_id: '1', project_id: '2' };

describe('explicit project budget binding', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(sourceBudgetBindingsApi.list).mockResolvedValue(page);
    vi.mocked(sourceBudgetBindingsApi.set).mockResolvedValue(binding);
  });
  it('shows only server-returned scope and keeps a read-only role from mutation', async () => {
    render(<SourceBudgetScope orgId={1} projectId={2} canWrite={false} />);
    await screen.findByText(/synthetic-enterprise-project · 版本 3/);
    expect(sourceBudgetBindingsApi.list).toHaveBeenCalledWith(1, 2, '');
    expect(screen.queryByRole('button', { name: '撤销预算绑定' })).not.toBeInTheDocument();
    expect(screen.queryByLabelText('企业项目范围引用')).not.toBeInTheDocument();
    expect(sourceBudgetBindingsApi.set).not.toHaveBeenCalled();
  });
  it('creates only the typed project scope and retains errors without claiming a binding', async () => {
    vi.mocked(sourceBudgetBindingsApi.list).mockResolvedValue({ ...page, data: [] });
    vi.mocked(sourceBudgetBindingsApi.set).mockRejectedValue(new Error('当前会话已撤销'));
    render(<SourceBudgetScope orgId={1} projectId={2} canWrite />);
    await screen.findByText('企业预算来源范围未配置。');
    fireEvent.change(screen.getByLabelText('企业项目范围引用'), { target: { value: 'synthetic-scope' } });
    fireEvent.click(screen.getByRole('button', { name: '绑定本项目预算来源' }));
    await screen.findByText('当前会话已撤销');
    expect(sourceBudgetBindingsApi.set).toHaveBeenCalledWith(1, 2, 'synthetic-scope', '0', 'active');
    expect(screen.getByLabelText('企业项目范围引用')).toHaveValue('synthetic-scope');
  });
  it('uses the current returned revision when reactivating a revoked binding', async () => {
    vi.mocked(sourceBudgetBindingsApi.list).mockResolvedValue({ ...page, data: [{ ...binding, status: 'revoked' }] });
    render(<SourceBudgetScope orgId={1} projectId={2} canWrite />);
    const button = await screen.findByRole('button', { name: '重新绑定本项目' });
    await waitFor(() => expect(button).toBeEnabled());
    fireEvent.click(button);
    await waitFor(() => expect(sourceBudgetBindingsApi.set).toHaveBeenCalledWith(1, 2, binding.scope_ref, '3', 'active'));
    await waitFor(() => expect(sourceBudgetBindingsApi.list).toHaveBeenCalledTimes(2));
  });
});
