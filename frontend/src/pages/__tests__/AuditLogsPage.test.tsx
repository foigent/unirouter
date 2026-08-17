import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import AuditLogsPage from '../AuditLogsPage';

const mockGetAuditLogs = vi.fn();

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => { resolve = next; });
  return { promise, resolve };
}

vi.mock('../../api/client', () => ({
  api: {
    getAuditLogs: (...args: any[]) => mockGetAuditLogs(...args),
  },
}));

describe('AuditLogsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    window.localStorage.clear();
    window.history.replaceState(null, '', '/audit-logs');
    mockGetAuditLogs.mockResolvedValue({
      data: [{
        id: 1,
        org_id: 3,
        project_id: 9,
        user_id: 7,
        action: 'channel.test',
        resource: 'channel',
        resource_id: '42',
        request_id: 'op_001',
        client_request_id: 'client-001',
        source: 'admin_console',
        detail: { after: { success: false } },
        created_at: '2026-07-02T10:00:00Z',
      }],
      total: 1,
    });
  });

  it('renders audit filters and loads with filter argument', async () => {
    const { container } = render(<AuditLogsPage />);

    await waitFor(() => {
      expect(mockGetAuditLogs).toHaveBeenCalledWith(1, 20, {});
    });

    const text = container.textContent || '';
    expect(text).toContain('channel.test');
    expect(text).toContain('管理控制台');
    expect(await screen.findByText('操作 ID')).toBeTruthy();
    expect(screen.getByText('op_001')).toBeTruthy();
    expect(screen.getByText('client-001')).toBeTruthy();
    expect(screen.getByLabelText('审计事件检查器')).toBeTruthy();
    expect(new URLSearchParams(window.location.search).get('audit_id')).toBe('1');
    expect(container.querySelector('input[placeholder="组织 ID"]')).toBeTruthy();
    expect(container.querySelector('input[placeholder="项目 ID"]')).toBeTruthy();
    expect(container.querySelector('input[placeholder="操作者 ID"]')).toBeTruthy();
    expect(container.querySelector('input[placeholder="动作"]')).toBeTruthy();
    expect(container.querySelector('input[placeholder="目标类型"]')).toBeTruthy();
    expect(container.querySelector('input[placeholder="目标 ID"]')).toBeTruthy();
    expect(container.querySelector('input[placeholder="操作 ID（Request ID）"]')).toBeTruthy();
    expect(container.querySelector('input[placeholder="客户端请求 ID"]')).toBeTruthy();
    expect(container.querySelector('input[placeholder="开始时间"]')).toBeTruthy();
    expect(container.querySelector('input[placeholder="结束时间"]')).toBeTruthy();
  });

  it('does not render zero events or an empty audit result before the request completes', async () => {
    const auditRequest = deferred<any>();
    mockGetAuditLogs.mockReturnValue(auditRequest.promise);

    render(<AuditLogsPage />);

    expect(await screen.findByText('读取中')).toBeTruthy();
    expect(screen.getByRole('heading', { name: '正在加载审计记录' })).toBeTruthy();
    expect(screen.queryByText('0 events')).toBeNull();
    expect(screen.queryByRole('heading', { name: '暂无审计记录' })).toBeNull();

    await act(async () => {
      auditRequest.resolve({ data: [], total: 0 });
      await Promise.resolve();
    });
    expect(await screen.findByText('0 events')).toBeTruthy();
    expect(screen.getByRole('heading', { name: '暂无审计记录' })).toBeTruthy();
  });

  it('keeps only the latest filtered response and exposes load failures', async () => {
    const oldRequest = deferred<any>();
    const currentRequest = deferred<any>();
    mockGetAuditLogs
      .mockReturnValueOnce(oldRequest.promise)
      .mockReturnValueOnce(currentRequest.promise);

    render(<AuditLogsPage />);
    await waitFor(() => expect(mockGetAuditLogs).toHaveBeenCalledTimes(1));

    fireEvent.change(screen.getByPlaceholderText('动作'), { target: { value: 'user.update' } });
    expect(mockGetAuditLogs).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole('button', { name: '应用筛选' }));
    await waitFor(() => expect(mockGetAuditLogs).toHaveBeenCalledTimes(2));
    expect(mockGetAuditLogs).toHaveBeenLastCalledWith(1, 20, { action: 'user.update' });
    expect(new URLSearchParams(window.location.search).get('action')).toBe('user.update');

    await act(async () => currentRequest.resolve({
      data: [{ id: 2, action: 'user.update', resource: 'user', created_at: '2026-07-16T00:00:00Z' }],
      total: 1,
    }));
    expect(await within(screen.getByLabelText('审计事件检查器')).findByText('user.update')).toBeTruthy();

    await act(async () => oldRequest.resolve({
      data: [{ id: 1, action: 'stale.action', resource: 'channel', created_at: '2026-07-15T00:00:00Z' }],
      total: 1,
    }));
    await waitFor(() => expect(screen.queryByText('stale.action')).toBeNull());

    mockGetAuditLogs.mockRejectedValueOnce(new Error('unavailable'));
    fireEvent.click(screen.getByRole('button', { name: '应用筛选' }));
    expect(await screen.findByRole('heading', { name: '审计记录加载失败' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '重新加载审计记录' })).toBeTruthy();
  });

  it('renders a zero system actor as 系统 instead of missing attribution', async () => {
    mockGetAuditLogs.mockResolvedValueOnce({
      data: [{
        id: 3,
        actor_id: 0,
        action: 'wallet.grant',
        resource: 'grant',
        source: 'system',
        created_at: '2026-07-24T10:10:28Z',
      }],
      total: 1,
    });

    render(<AuditLogsPage />);

    const inspector = await screen.findByLabelText('审计事件检查器');
    expect(await within(inspector).findByText('系统')).toBeTruthy();
    expect(within(inspector).getByText('系统任务')).toBeTruthy();
  });

  it('restores filters, pagination and selection from a deep link without a detail-open click', async () => {
    window.history.replaceState(null, '', '/audit-logs?action=user.update&page=2&page_size=50&audit_id=8');
    mockGetAuditLogs.mockResolvedValueOnce({
      data: [
        { id: 9, action: 'user.update', resource: 'user', resource_id: '9', created_at: '2026-07-24T10:10:28Z' },
        { id: 8, action: 'user.update', resource: 'user', resource_id: '8', request_id: 'op_008', created_at: '2026-07-24T10:09:28Z' },
      ],
      total: 90,
    });

    render(<AuditLogsPage />);

    await waitFor(() => expect(mockGetAuditLogs).toHaveBeenCalledWith(2, 50, { action: 'user.update' }));
    expect(await screen.findByText('op_008')).toBeTruthy();
    expect(screen.getByText('audit:8')).toBeTruthy();
    expect(screen.queryByRole('button', { name: '查看' })).toBeNull();
  });

  it('saves filter metadata locally and reapplies the named view', async () => {
    render(<AuditLogsPage />);
    await waitFor(() => expect(mockGetAuditLogs).toHaveBeenCalledTimes(1));

    fireEvent.change(screen.getByPlaceholderText('动作'), { target: { value: 'channel.update' } });
    fireEvent.change(screen.getByLabelText('审计视图名称'), { target: { value: '通道变更' } });
    fireEvent.click(screen.getByRole('button', { name: '保存 / 覆盖' }));

    expect(JSON.parse(window.localStorage.getItem('unirouter.audit.saved-views.v1') || '[]')).toEqual([
      { name: '通道变更', state: { action: 'channel.update' } },
    ]);

    fireEvent.click(screen.getByRole('button', { name: '重置筛选' }));
    await waitFor(() => expect(mockGetAuditLogs).toHaveBeenLastCalledWith(1, 20, {}));

    fireEvent.click(screen.getByRole('group', { name: '已保存的审计视图' }).querySelector('[role="combobox"]') as HTMLElement);
    fireEvent.click(await screen.findByText('通道变更'));
    await waitFor(() => expect(mockGetAuditLogs).toHaveBeenLastCalledWith(1, 20, { action: 'channel.update' }));
  });
});
