import { describe, expect, it, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import KeysPage from '../KeysPage';
import { useAuthStore } from '../../store/auth';

const mockGetKeys = vi.fn();
const mockGetModelNames = vi.fn();
const mockCreateKey = vi.fn();
const mockUpdateKey = vi.fn();
const mockDisableKey = vi.fn();
const mockGetOrganizations = vi.fn();
const mockGetProjects = vi.fn();
const mockGetUsers = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getKeys: (...args: any[]) => mockGetKeys(...args),
    getModelNames: (...args: any[]) => mockGetModelNames(...args),
    getOrganizations: (...args: any[]) => mockGetOrganizations(...args),
    getProjects: (...args: any[]) => mockGetProjects(...args),
    getUsers: (...args: any[]) => mockGetUsers(...args),
    createKey: (...args: any[]) => mockCreateKey(...args),
    updateKey: (...args: any[]) => mockUpdateKey(...args),
    disableKey: (...args: any[]) => mockDisableKey(...args),
  },
}));

describe('KeysPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.setState({
      token: 'token',
      isAuthenticated: true,
      profileLoading: false,
      profileError: null,
      user: { id: 1, username: 'admin', role: 'platform_admin', org_id: 1, org_name: '总部' },
    });
    mockGetOrganizations.mockResolvedValue({ data: [{ id: 1, name: '总部' }] });
    mockGetProjects.mockResolvedValue({ data: [{ id: 11, org_id: 1, name: '核心项目' }] });
    mockGetUsers.mockResolvedValue({ data: [{ id: 7, org_id: 1, username: 'alice' }] });
    mockGetKeys.mockResolvedValue({
      data: [{
        id: 1,
        org_id: 1,
        org_name: '总部',
        project_id: 11,
        project_name: '核心项目',
        owner_user_id: 7,
        owner_username: 'alice',
        key_type: 'personal',
        name: 'service-key',
        key_prefix: 'sk-test',
        allowed_ips: '',
        allowed_models: 'company-chat',
        status: 'active',
        rpm_limit: 60,
        rps_limit: 10,
        qps_limit: 10,
        policy_labels: ['production'],
        created_at: '2026-07-07T10:00:00Z',
      }],
      total: 1,
    });
    mockGetModelNames.mockResolvedValue({ data: ['company-chat'] });
    mockCreateKey.mockResolvedValue({ key: 'sk-created', key_prefix: 'sk-created' });
    mockUpdateKey.mockResolvedValue({ message: 'updated' });
    mockDisableKey.mockResolvedValue({ message: 'disabled' });
  });

  it('renders explicit scope, ownership and implemented RPM/RPS limits', async () => {
    render(<KeysPage />);

    expect(await screen.findByText('service-key')).toBeTruthy();
    expect(mockGetKeys).toHaveBeenCalledWith(1, 20);
    expect(mockGetOrganizations).toHaveBeenCalledWith(1, 100);
    expect(mockGetProjects).toHaveBeenCalledWith(1, 100, { status: 'active', org_id: 1 });
    expect(mockGetUsers).toHaveBeenCalledWith(1, 100);
    expect(screen.getByText('RPM 60 / RPS 10')).toBeTruthy();
    expect(screen.queryByText(/TPM/)).toBeNull();
    expect(screen.queryByText(/Scheduler/)).toBeNull();
    expect(screen.getByText('总部 (#1)')).toBeTruthy();
    expect(screen.getByText('核心项目 (#11)')).toBeTruthy();
    expect(screen.getByText('个人 Key')).toBeTruthy();
    expect(screen.getByText('alice')).toBeTruthy();
    expect(screen.getByText('production')).toBeTruthy();
    expect(screen.getByText('启用')).toBeTruthy();
    expect(screen.getByRole('button', { name: '编辑访问凭证 service-key' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '停用访问凭证 service-key' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '编辑访问凭证 service-key' }));
    expect(await screen.findByText('编辑访问凭证')).toBeTruthy();
    expect(screen.getAllByText('所属组织').length).toBeGreaterThan(0);
    expect(screen.getByText('所属项目')).toBeTruthy();
    expect(screen.getByText('凭证类型')).toBeTruthy();
    expect(screen.getByText('Key 所有者')).toBeTruthy();
  });

  it('creates keys with rps_limit rather than visible QPS wording', async () => {
    render(<KeysPage />);

    await screen.findByText('service-key');
    fireEvent.click(screen.getByText('新增访问凭证'));
    fireEvent.change(await screen.findByPlaceholderText('service-prod'), { target: { value: 'new-key' } });
    expect(screen.getByText('RPS 限速')).toBeTruthy();
    expect(screen.queryByText('QPS 限速')).toBeNull();
    fireEvent.click(screen.getByText('创建'));

    await waitFor(() => {
      expect(mockCreateKey).toHaveBeenCalledWith(expect.objectContaining({
        name: 'new-key',
        org_id: 1,
        project_id: 11,
        key_type: 'shared',
        owner_user_id: 0,
        rps_limit: 10,
      }));
    });
  });

  it('does not keep stale credentials visible after a list failure', async () => {
    mockGetKeys.mockRejectedValueOnce(new Error('unavailable'));

    render(<KeysPage />);

    expect(await screen.findByRole('heading', { name: '访问凭证加载失败' })).toBeTruthy();
    expect(screen.queryByText('service-key')).toBeNull();
    expect(screen.getByRole('button', { name: '重新加载访问凭证' })).toBeTruthy();
  });

  it('keeps purpose-specific feed credentials read-only in generic key management', async () => {
    mockGetKeys.mockResolvedValueOnce({
      data: [{
        id: 41,
        org_id: 1,
        org_name: '总部',
        project_id: 11,
        project_name: '核心项目',
        key_type: 'service',
        name: 'X-Insight feed',
        key_prefix: 'ur-feed',
        allowed_ips: '10.0.0.8/32',
        allowed_models: '',
        status: 'revoked',
        purpose: 'xinsight.feed.read',
        rpm_limit: 60,
        rps_limit: 1,
        created_at: '2026-07-25T10:00:00Z',
      }],
      total: 1,
    });

    render(<KeysPage />);

    expect(await screen.findByText('X-Insight feed')).toBeTruthy();
    expect(screen.getByText('由实时连接控制')).toBeTruthy();
    expect(screen.getByText('无模型权限')).toBeTruthy();
    expect(screen.getByText('请前往 X-Insight 数据交换控制')).toBeTruthy();
    expect(screen.queryByRole('button', { name: '编辑访问凭证 X-Insight feed' })).toBeNull();
    expect(screen.queryByRole('button', { name: '启用访问凭证 X-Insight feed' })).toBeNull();
    expect(screen.queryByRole('button', { name: '停用访问凭证 X-Insight feed' })).toBeNull();
  });
});
