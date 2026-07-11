import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import NotificationCenterPage from '../NotificationCenterPage';

const mockGetNotificationEvents = vi.fn();
const mockGetNotificationDeliveries = vi.fn();
const mockGetNotificationWebhooks = vi.fn();
const mockCreateNotificationWebhook = vi.fn();
const mockUpdateNotificationWebhook = vi.fn();
const mockVerifyNotificationWebhookScope = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getNotificationEvents: (...args: any[]) => mockGetNotificationEvents(...args),
    getNotificationDeliveries: (...args: any[]) => mockGetNotificationDeliveries(...args),
    getNotificationWebhooks: (...args: any[]) => mockGetNotificationWebhooks(...args),
    createNotificationWebhook: (...args: any[]) => mockCreateNotificationWebhook(...args),
    updateNotificationWebhook: (...args: any[]) => mockUpdateNotificationWebhook(...args),
    verifyNotificationWebhookScope: (...args: any[]) => mockVerifyNotificationWebhookScope(...args),
  },
}));

describe('NotificationCenterPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetNotificationEvents.mockResolvedValue({
      data: [{
        id: 1,
        event_type: 'project_budget.threshold',
        severity: 'critical',
        title: 'Budget reached',
        message: 'Project Core reached budget',
        resource: 'project',
        resource_id: '9',
        status: 'open',
        delivery_state: 'failed',
        created_at: '2026-07-02T10:00:00Z',
      }],
      total: 1,
    });
    mockGetNotificationDeliveries.mockResolvedValue({
      data: [{
        id: 2,
        event_id: 1,
        webhook_name: 'Ops webhook',
        event_type: 'project_budget.threshold',
        status: 'failed',
        attempts: 1,
        response_status: 500,
        error: 'webhook returned status 500',
        next_retry_at: '2026-07-02T10:05:00Z',
        updated_at: '2026-07-02T10:00:00Z',
      }],
      total: 1,
    });
    mockGetNotificationWebhooks.mockResolvedValue({
      data: [{
        id: 3,
        org_id: 7,
        name: 'Ops webhook',
        url: 'https://ops.example/hook',
        event_types: ['project_budget.threshold'],
        enabled: false,
        scope_verified: false,
        scope_quarantine_previous_enabled: true,
        updated_at: '2026-07-02T10:00:00Z',
      }],
      total: 1,
    });
  });

  it('loads event, delivery, and webhook data', async () => {
    render(<NotificationCenterPage />);

    await waitFor(() => {
      expect(mockGetNotificationEvents).toHaveBeenCalledWith(1, 20, {
        event_type: '',
        severity: '',
        delivery_state: '',
      });
      expect(mockGetNotificationDeliveries).toHaveBeenCalledWith(1, 20, { status: '' });
      expect(mockGetNotificationWebhooks).toHaveBeenCalledWith(1, 20);
    });

    await screen.findByText('Budget reached');
    expect(screen.getAllByText('项目预算阈值').length).toBeGreaterThan(0);
    expect(screen.getByText('Budget reached')).toBeTruthy();
    expect(screen.getAllByText('投递失败').length).toBeGreaterThan(0);
  });

  it('shows quarantined historical webhooks and requires explicit scope confirmation', async () => {
    mockVerifyNotificationWebhookScope.mockResolvedValue({ scope_verified: true });
    render(<NotificationCenterPage />);

    fireEvent.click(await screen.findByRole('tab', { name: 'Webhook' }));
    expect(await screen.findByText('已隔离')).toBeTruthy();
    fireEvent.click(screen.getByText('确认范围'));
    expect(await screen.findByText('确认 Webhook 路由范围')).toBeTruthy();
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('https://ops.example/hook')).toBeTruthy();
    expect(within(dialog).getByText(/升级前失败投递不会自动重放/)).toBeTruthy();
    const confirmButtons = screen.getAllByText('确认范围');
    fireEvent.click(confirmButtons[confirmButtons.length - 1]);

    await waitFor(() => expect(mockVerifyNotificationWebhookScope).toHaveBeenCalledWith(3, '2026-07-02T10:00:00Z'));
  });
});
