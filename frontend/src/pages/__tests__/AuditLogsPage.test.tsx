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
        source: 'admin_console',
        detail: { after: { success: false } },
        created_at: '2026-07-02T10:00:00Z',
      }],
      total: 1,
    });
  });

  it('renders PRD2 audit filters and loads with filter argument', async () => {
    const { container } = render(<AuditLogsPage />);

    await waitFor(() => {
      expect(mockGetAuditLogs).toHaveBeenCalledWith(1, 20, {});
    });

    const text = container.textContent || '';
    expect(text).toContain('channel.test');
    expect(text).toContain('admin_console');
    expect(container.querySelector('input[placeholder="org_id"]')).toBeTruthy();
    expect(container.querySelector('input[placeholder="project_id"]')).toBeTruthy();
    expect(container.querySelector('input[placeholder="actor_id"]')).toBeTruthy();
    expect(container.querySelector('input[placeholder="action"]')).toBeTruthy();
    expect(container.querySelector('input[placeholder="resource"]')).toBeTruthy();
    expect(container.querySelector('input[placeholder="resource_id"]')).toBeTruthy();
    expect(container.querySelector('input[placeholder="start_time RFC3339"]')).toBeTruthy();
    expect(container.querySelector('input[placeholder="end_time RFC3339"]')).toBeTruthy();
  });
});
