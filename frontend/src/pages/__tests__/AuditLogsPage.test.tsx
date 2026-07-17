import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
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
    expect(text).not.toContain('op_001');
    expect(text).not.toContain('client-001');
    fireEvent.click(screen.getByRole('button', { name: '查看' }));
    expect(await screen.findByText(/操作 ID：/)).toBeTruthy();
    expect(screen.getByText('op_001')).toBeTruthy();
    expect(screen.getByText('client-001')).toBeTruthy();
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

  it('keeps only the latest filtered response and exposes load failures', async () => {
    const oldRequest = deferred<any>();
    const currentRequest = deferred<any>();
    mockGetAuditLogs
      .mockReturnValueOnce(oldRequest.promise)
      .mockReturnValueOnce(currentRequest.promise);

    render(<AuditLogsPage />);
    await waitFor(() => expect(mockGetAuditLogs).toHaveBeenCalledTimes(1));

    fireEvent.change(screen.getByPlaceholderText('动作'), { target: { value: 'user.update' } });
    await waitFor(() => expect(mockGetAuditLogs).toHaveBeenCalledTimes(2));

    await act(async () => currentRequest.resolve({
      data: [{ id: 2, action: 'user.update', resource: 'user', created_at: '2026-07-16T00:00:00Z' }],
      total: 1,
    }));
    expect(await screen.findByText('user.update')).toBeTruthy();

    await act(async () => oldRequest.resolve({
      data: [{ id: 1, action: 'stale.action', resource: 'channel', created_at: '2026-07-15T00:00:00Z' }],
      total: 1,
    }));
    await waitFor(() => expect(screen.queryByText('stale.action')).toBeNull());

    mockGetAuditLogs.mockRejectedValueOnce(new Error('unavailable'));
    fireEvent.click(screen.getByRole('button', { name: /筛选/ }));
    expect(await screen.findByRole('heading', { name: '审计记录加载失败' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '重新加载审计记录' })).toBeTruthy();
  });
});
