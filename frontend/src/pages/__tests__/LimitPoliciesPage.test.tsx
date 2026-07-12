import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import LimitPoliciesPage from '../LimitPoliciesPage';
import { useAuthStore } from '../../store/auth';

const mocks = vi.hoisted(() => ({
  getLimitPolicies: vi.fn(),
  createLimitPolicy: vi.fn(),
  updateLimitPolicy: vi.fn(),
  getKeys: vi.fn(),
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
    expect(screen.getByText(/全平台 100 个容量单位，每个组织 10 个/)).toBeTruthy();
    expect(screen.getByText(/流式对话占 4 个/)).toBeTruthy();
    expect(screen.getByText(/流式对话最多等待 250 毫秒/)).toBeTruthy();
    expect(screen.getByText(/返回 429 和 Retry-After/)).toBeTruthy();
    expect(screen.getByText('Acme · Customer API · 凭证 frontend-prod · 模型 company-chat · 标签 production')).toBeTruthy();
    expect(screen.getByText('RPM 600 / TPM 100000 / 并发 20')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: /新增策略/ }));
    expect(await screen.findByText(/影子观察（推荐先选）/)).toBeTruthy();
    expect(screen.getByText(/调用方不能通过请求头自行声明/)).toBeTruthy();
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
});
