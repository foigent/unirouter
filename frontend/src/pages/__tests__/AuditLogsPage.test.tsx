import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import AuditLogsPage from '../AuditLogsPage';

const mockGetAuditLogs = vi.fn();

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
    expect(text).toContain('op_001');
    expect(text).toContain('client-001');
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
});
