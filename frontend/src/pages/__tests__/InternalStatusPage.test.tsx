import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import InternalStatusPage from '../InternalStatusPage';

const mockGetInternalStatus = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getInternalStatus: (...args: any[]) => mockGetInternalStatus(...args),
  },
}));

describe('InternalStatusPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetInternalStatus.mockResolvedValue({
      status: 'warning',
      open_critical: 0,
      open_warning: 1,
      maintenance_windows: [],
      incidents: [{
        id: 1,
        event_type: 'provider.channel.failure',
        severity: 'warning',
        title: 'Provider channel failure',
        message: 'primary channel failed',
        resource: 'channel',
        resource_id: '3',
        status: 'open',
        delivery_state: 'failed',
        created_at: '2026-07-04T10:00:00Z',
        updated_at: '2026-07-04T10:00:00Z',
      }],
    });
  });

  it('renders internal status and incident history', async () => {
    render(<InternalStatusPage />);

    await waitFor(() => {
      expect(mockGetInternalStatus).toHaveBeenCalled();
    });

    expect(await screen.findByText('内部状态')).toBeTruthy();
    expect(screen.getByText('Provider channel failure')).toBeTruthy();
    expect(screen.getByText('上游通道故障')).toBeTruthy();
  });
});
