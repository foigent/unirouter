import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { Modal, Toast } from '@douyinfe/semi-ui';
import NotificationCenterPage from '../NotificationCenterPage';

const mockGetNotificationEvents = vi.fn();
const mockGetNotificationDeliveries = vi.fn();
const mockGetNotificationWebhooks = vi.fn();
const mockCreateNotificationWebhook = vi.fn();
const mockUpdateNotificationWebhook = vi.fn();
const mockVerifyNotificationWebhookScope = vi.fn();
const mockAcknowledgeNotificationEvent = vi.fn();
const mockResolveNotificationEvent = vi.fn();
const mockGetOrganizations = vi.fn();
const mockGetProjects = vi.fn();
const auth = vi.hoisted(() => ({ user: null as any }));

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => { resolve = next; });
  return { promise, resolve };
}

vi.mock('../../api/client', () => ({
  api: {
    getNotificationEvents: (...args: any[]) => mockGetNotificationEvents(...args),
    getNotificationDeliveries: (...args: any[]) => mockGetNotificationDeliveries(...args),
    getNotificationWebhooks: (...args: any[]) => mockGetNotificationWebhooks(...args),
    createNotificationWebhook: (...args: any[]) => mockCreateNotificationWebhook(...args),
    updateNotificationWebhook: (...args: any[]) => mockUpdateNotificationWebhook(...args),
    verifyNotificationWebhookScope: (...args: any[]) => mockVerifyNotificationWebhookScope(...args),
    acknowledgeNotificationEvent: (...args: any[]) => mockAcknowledgeNotificationEvent(...args),
    resolveNotificationEvent: (...args: any[]) => mockResolveNotificationEvent(...args),
    getOrganizations: (...args: any[]) => mockGetOrganizations(...args),
    getProjects: (...args: any[]) => mockGetProjects(...args),
  },
}));

vi.mock('../../store/auth', () => ({
  useAuthStore: (selector: any) => selector({ user: auth.user }),
}));

describe('NotificationCenterPage', () => {
  beforeEach(() => {
    Modal.destroyAll();
    Toast.destroyAll();
    vi.clearAllMocks();
    auth.user = { id: 1, username: 'org-admin', role: 'org_admin', organization_role: 'org_admin', org_id: 7 };
    window.history.replaceState({}, '', '/notifications');
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
        url: 'https://ops.example/...',
        event_types: ['project_budget.threshold'],
        enabled: false,
        scope_verified: false,
        scope_quarantine_previous_enabled: true,
        updated_at: '2026-07-02T10:00:00Z',
      }],
      total: 1,
    });
    mockGetOrganizations.mockResolvedValue({ data: [{ id: 7, name: '总部' }], total: 1 });
    mockGetProjects.mockResolvedValue({ data: [], total: 0 });
  });

  afterEach(() => {
    Modal.destroyAll();
    Toast.destroyAll();
    document.querySelectorAll('.semi-portal').forEach(portal => portal.remove());
    document.body.removeAttribute('style');
  });

  it('loads event, delivery, and webhook data', async () => {
    render(<NotificationCenterPage />);

    await waitFor(() => {
      expect(mockGetNotificationEvents).toHaveBeenCalledWith(1, 20, {
        event_type: '',
        severity: '',
        status: '',
        delivery_state: '',
        project_id: undefined,
      });
      expect(mockGetNotificationDeliveries).toHaveBeenCalledWith(1, 20, { status: '', event_id: undefined, project_id: undefined });
      expect(mockGetNotificationWebhooks).toHaveBeenCalledWith(1, 20, { project_id: undefined });
    });

    await screen.findByText('Budget reached');
    expect(screen.getAllByText('项目预算阈值').length).toBeGreaterThan(0);
    expect(screen.getByText('Budget reached')).toBeTruthy();
    expect(screen.getAllByText('投递失败').length).toBeGreaterThan(0);
  });

  it('gives billing administrators a finance-only center without webhook management', async () => {
    auth.user = { id: 4, username: 'billing-admin', role: 'billing_admin', org_id: 7 };
    render(<NotificationCenterPage />);

    expect(await screen.findByText('财务提醒')).toBeTruthy();
    expect(screen.getByText(/投递结果仅供核对，Webhook 路由由组织或平台管理员维护/)).toBeTruthy();
    expect(screen.getByRole('tab', { name: '财务事件' })).toBeTruthy();
    expect(screen.getByRole('tab', { name: '投递记录（只读）' })).toBeTruthy();
    expect(screen.queryByRole('tab', { name: 'Webhook' })).toBeNull();
    expect(screen.queryByRole('button', { name: '新增 Webhook' })).toBeNull();

    await waitFor(() => {
      expect(mockGetNotificationEvents).toHaveBeenCalled();
      expect(mockGetNotificationDeliveries).toHaveBeenCalled();
    });
    expect(mockGetNotificationWebhooks).not.toHaveBeenCalled();
    expect(mockGetOrganizations).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: /刷新/ }));
    await waitFor(() => expect(mockGetNotificationEvents.mock.calls.length).toBeGreaterThan(1));
    expect(mockGetNotificationWebhooks).not.toHaveBeenCalled();
  });

  it('uses one explicit platform organization for events, deliveries, and webhooks', async () => {
    auth.user = { role: 'platform_admin', org_id: 7 };
    render(<NotificationCenterPage />);

    expect(await screen.findByText('目标组织')).toBeTruthy();
    await waitFor(() => {
      expect(mockGetNotificationEvents).toHaveBeenCalledWith(1, 20, expect.objectContaining({ org_id: 7 }));
      expect(mockGetNotificationDeliveries).toHaveBeenCalledWith(1, 20, expect.objectContaining({ org_id: 7 }));
      expect(mockGetNotificationWebhooks).toHaveBeenCalledWith(1, 20, expect.objectContaining({ org_id: 7 }));
    });
    expect(mockGetOrganizations).toHaveBeenCalledWith(1, 100);
  });

  it('shows quarantined historical webhooks and requires explicit scope confirmation', async () => {
    mockVerifyNotificationWebhookScope.mockResolvedValue({ scope_verified: true });
    render(<NotificationCenterPage />);

    fireEvent.click(await screen.findByRole('tab', { name: 'Webhook' }));
    expect(await screen.findByText('已隔离')).toBeTruthy();
    fireEvent.click(screen.getByText('确认范围'));
    expect(await screen.findByText('确认 Webhook 路由范围')).toBeTruthy();
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('https://ops.example/...')).toBeTruthy();
    expect(within(dialog).getByText(/升级前失败投递不会自动重放/)).toBeTruthy();
    const confirmButtons = screen.getAllByText('确认范围');
    fireEvent.click(confirmButtons[confirmButtons.length - 1]);

    await waitFor(() => expect(mockVerifyNotificationWebhookScope).toHaveBeenCalledWith(3, '2026-07-02T10:00:00Z'));
  });

  it('acknowledges events and drills into their delivery attempts', async () => {
    mockAcknowledgeNotificationEvent.mockResolvedValue({ id: 1, status: 'acknowledged' });
    render(<NotificationCenterPage />);

    fireEvent.click(await screen.findByRole('button', { name: '确认' }));
    await waitFor(() => expect(mockAcknowledgeNotificationEvent).toHaveBeenCalledWith(1, undefined));

    fireEvent.click(screen.getByRole('button', { name: '查看投递' }));
    await waitFor(() => expect(mockGetNotificationDeliveries).toHaveBeenCalledWith(1, 20, {
      status: '',
      event_id: 1,
      project_id: undefined,
    }));
    expect(await screen.findByText(/仅显示事件 #1/)).toBeTruthy();
  }, 20_000);

  it('passes explicit project context to every project-scoped list and action', async () => {
    window.history.replaceState({}, '', '/notifications?project_id=9');
    auth.user = { id: 5, username: 'project-admin', role: 'project_admin', organization_role: 'member', org_id: 7 };
    mockGetProjects.mockResolvedValue({
      data: [{ id: 9, current_user_membership_role: 'project_admin' }],
      total: 1,
    });
    mockResolveNotificationEvent.mockResolvedValue({ id: 1, status: 'resolved' });
    render(<NotificationCenterPage />);

    await waitFor(() => {
      expect(mockGetNotificationEvents).toHaveBeenCalledWith(1, 20, expect.objectContaining({ project_id: 9 }));
      expect(mockGetNotificationDeliveries).toHaveBeenCalledWith(1, 20, expect.objectContaining({ project_id: 9 }));
      expect(mockGetNotificationWebhooks).toHaveBeenCalledWith(1, 20, { project_id: 9 });
    });
    fireEvent.click(await screen.findByRole('button', { name: '解决' }));
    const dialog = await screen.findByRole('dialog', undefined, { timeout: 5_000 });
    expect(within(dialog).getByText('确认解决事件')).toBeTruthy();
    fireEvent.click(within(dialog).getByRole('button', { name: '确认解决' }));
    await waitFor(() => expect(mockResolveNotificationEvent).toHaveBeenCalledWith(1, 9));
  }, 20_000);

  it('discards stale event responses and shows an explicit retry state', async () => {
    const oldRequest = deferred<any>();
    const currentRequest = deferred<any>();
    mockGetNotificationEvents
      .mockReturnValueOnce(oldRequest.promise)
      .mockReturnValueOnce(currentRequest.promise);

    render(<NotificationCenterPage />);
    await waitFor(() => expect(mockGetNotificationEvents).toHaveBeenCalledTimes(1));

    fireEvent.click(screen.getByRole('button', { name: /刷新/ }));
    await waitFor(() => expect(mockGetNotificationEvents).toHaveBeenCalledTimes(2));

    await act(async () => currentRequest.resolve({
      data: [{
        id: 2,
        event_type: 'project_budget.threshold',
        severity: 'warning',
        title: 'Current event',
        message: 'current',
        status: 'open',
        delivery_state: 'not_configured',
        created_at: '2026-07-16T00:00:00Z',
      }],
      total: 1,
    }));
    expect(await screen.findByText('Current event')).toBeTruthy();

    await act(async () => oldRequest.resolve({
      data: [{ id: 1, event_type: 'project_budget.threshold', severity: 'warning', title: 'Stale event', message: 'stale', status: 'open', delivery_state: 'failed', created_at: '2026-07-15T00:00:00Z' }],
      total: 1,
    }));
    await waitFor(() => expect(screen.queryByText('Stale event')).toBeNull());

    mockGetNotificationEvents.mockRejectedValueOnce(new Error('unavailable'));
    const callCount = mockGetNotificationEvents.mock.calls.length;
    fireEvent.click(screen.getByRole('button', { name: /刷新/ }));
    await waitFor(() => expect(mockGetNotificationEvents.mock.calls.length).toBe(callCount + 1));
    expect(await screen.findByRole('heading', { name: '通知事件加载失败' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '重新加载通知事件' })).toBeTruthy();
  });
});
