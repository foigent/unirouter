import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import LimitPoliciesPage from '../LimitPoliciesPage';
import { useAuthStore } from '../../store/auth';

const mocks = vi.hoisted(() => ({
  getLimitPolicies: vi.fn(),
  createLimitPolicy: vi.fn(),
  updateLimitPolicy: vi.fn(),
  getKeys: vi.fn(),
  getOrganizations: vi.fn(),
  getProjects: vi.fn(),
  getModelNames: vi.fn(),
}));

vi.mock('../../api/client', () => ({ api: mocks }));

describe('LimitPoliciesPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getLimitPolicies.mockResolvedValue({
      data: [{
        id: 7,
        name: 'production-chat',
        version: 3,
        org_id: 22,
        org_name: 'Acme',
        project_id: 9,
        project_name: 'Customer API',
        api_key_id: 15,
        api_key_name: 'frontend-prod',
        model: 'company-chat',
        policy_label: 'production',
        mode: 'shadow',
        failure_mode: 'last_known_safe',
        rpm_limit: 600,
        tpm_limit: 100000,
        concurrency_limit: 20,
      }],
      total: 1,
      builtin_limits: {
        global_concurrency: 100,
        organization_concurrency: 10,
        workload_global_capacity: 100,
        workload_organization_capacity: 10,
        workload_global_short_reserve: 10,
        workload_organization_short_reserve: 2,
        workload_interactive_weight: 1,
        workload_stream_weight: 4,
        workload_embedding_weight: 1,
        workload_interactive_max_wait_ms: 50,
        workload_stream_max_wait_ms: 250,
        workload_embedding_max_wait_ms: 50,
      },
    });
    mocks.getKeys.mockResolvedValue({
      data: [{ id: 15, name: 'frontend-prod', key_prefix: 'ur-prod', org_id: 22, org_name: 'Acme', project_id: 9, project_name: 'Customer API' }],
      total: 1,
    });
    mocks.getOrganizations.mockResolvedValue({ data: [{ id: 22, name: 'Acme', status: 'active' }], total: 1 });
    mocks.getProjects.mockResolvedValue({ data: [{ id: 9, org_id: 22, name: 'Customer API', status: 'active' }], total: 1 });
    mocks.getModelNames.mockResolvedValue({ data: ['company-chat'] });
  });

  it('explains quotas, shadow mode, selectors, and bounded failure behavior in plain language', async () => {
    useAuthStore.setState({ user: { id: 2, username: 'org-admin', role: 'org_admin', org_id: 22, org_name: 'Acme' } });
    render(<LimitPoliciesPage />);

    expect(await screen.findByText('production-chat')).toBeTruthy();
    await waitFor(() => expect(mocks.getLimitPolicies).toHaveBeenCalledWith(1, 20));
    expect(screen.getByText(/RPS 是每秒请求数/)).toBeTruthy();
    expect(screen.getByText(/推荐先用“影子观察”/)).toBeTruthy();
    expect(screen.getByText(/约 2 秒失效且每个副本最多 1 次/)).toBeTruthy();
    expect(screen.getByText(/当前部署 100 个容量单位，唯一客户 10 个/)).toBeTruthy();
    expect(screen.getByText(/流式对话占 4 个/)).toBeTruthy();
    expect(screen.getByText(/流式对话最多等待 250 毫秒/)).toBeTruthy();
    expect(screen.getByText(/返回 429 和 Retry-After/)).toBeTruthy();
    expect(screen.getByText('Acme · Customer API · 凭证 frontend-prod · 模型 company-chat · 标签 production')).toBeTruthy();
    expect(screen.getByText('RPM 600 / TPM 100000 / 并发 20')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: /新增策略/ }));
    expect(await screen.findByText(/影子观察（推荐先选）/)).toBeTruthy();
    expect(screen.getByText(/调用方不能通过请求头自行声明/)).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '项目（可选）' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '访问凭证（可选）' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '模型（可选）' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '运行状态' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: 'Redis 无法确认额度时' })).toBeTruthy();
    expect(mocks.getOrganizations).not.toHaveBeenCalled();
    expect(mocks.getProjects).toHaveBeenCalledWith(1, 100, { org_id: 22 });
  });

  it('keeps billing administrators read-only', async () => {
    useAuthStore.setState({ user: { id: 3, username: 'billing', role: 'billing_admin', org_id: 22 } });
    render(<LimitPoliciesPage />);

    expect(await screen.findByText('production-chat')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /新增策略/ })).toBeNull();
    expect(screen.queryByText('操作')).toBeNull();
  });

  it('sends the loaded version when saving an edit', async () => {
    useAuthStore.setState({ user: { id: 2, username: 'org-admin', role: 'org_admin', org_id: 22, org_name: 'Acme' } });
    mocks.updateLimitPolicy.mockResolvedValue({ id: 7, version: 4, runtime_snapshot_applied: true });
    render(<LimitPoliciesPage />);

    fireEvent.click(await screen.findByRole('button', { name: '编辑策略 production-chat' }));
    expect(await screen.findByText('编辑策略：production-chat')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '保存并生成新版本' }));

    await waitFor(() => expect(mocks.updateLimitPolicy).toHaveBeenCalledWith(
      7,
      expect.objectContaining({ version: 3, name: 'production-chat', mode: 'shadow' }),
    ));
  });

  it('defaults platform creation to the canonical customer and preserves an existing deployment-global policy', async () => {
    useAuthStore.setState({ user: {
      id: 1, username: 'platform', role: 'platform_admin', org_id: 99,
      customer_context: { state: 'ready', org_id: 22, org_name: 'Acme' },
    } });
    mocks.getLimitPolicies.mockResolvedValue({
      data: [{
        id: 8, name: 'deployment-cap', version: 1, org_id: null,
        mode: 'shadow', failure_mode: 'fail_closed', rpm_limit: 1200,
      }],
      total: 1,
      builtin_limits: {},
    });
    mocks.createLimitPolicy.mockResolvedValue({ id: 9, version: 1, runtime_snapshot_applied: true });
    mocks.updateLimitPolicy.mockResolvedValue({ id: 8, version: 2, runtime_snapshot_applied: true });

    render(<LimitPoliciesPage />);
    await screen.findByText('deployment-cap');
    expect(screen.getByText('部署全局（当前部署全部请求）')).toBeTruthy();
    expect(mocks.getOrganizations).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: /新增策略/ }));
    fireEvent.change(await screen.findByPlaceholderText('production-chat'), { target: { value: 'customer-default' } });
    fireEvent.click(screen.getByRole('button', { name: '创建策略' }));
    await waitFor(() => expect(mocks.createLimitPolicy).toHaveBeenCalledWith(expect.objectContaining({
      name: 'customer-default', org_id: 22,
    })));

    fireEvent.click(screen.getByRole('button', { name: '编辑策略 deployment-cap' }));
    fireEvent.click(await screen.findByRole('button', { name: '保存并生成新版本' }));
    await waitFor(() => expect(mocks.updateLimitPolicy).toHaveBeenCalledWith(8, expect.objectContaining({
      org_id: 0, project_id: 0, api_key_id: 0,
    })));
  });

  it('fails closed without customer-domain calls when the canonical customer is unavailable', async () => {
    useAuthStore.setState({ user: {
      id: 1, username: 'platform', role: 'platform_admin', org_id: 99,
      customer_context: { state: 'unavailable' },
    } });

    render(<LimitPoliciesPage />);

    expect((await screen.findAllByRole('heading', { name: '无法确认客户组织上下文' })).length).toBeGreaterThan(0);
    expect(mocks.getLimitPolicies).not.toHaveBeenCalled();
    expect(mocks.getKeys).not.toHaveBeenCalled();
    expect(mocks.getOrganizations).not.toHaveBeenCalled();
    expect(mocks.getProjects).not.toHaveBeenCalled();
    expect(mocks.getModelNames).not.toHaveBeenCalled();
  });
});
