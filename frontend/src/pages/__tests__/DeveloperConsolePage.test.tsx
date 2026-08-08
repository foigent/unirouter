import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import DeveloperConsolePage from '../DeveloperConsolePage';
import { useAuthStore } from '../../store/auth';
import type { LicenseAccess } from '../../navigation';

const mockGetProjects = vi.fn();
const mockGetKeys = vi.fn();
const mockGetModelCatalog = vi.fn();
const mockGetLogs = vi.fn();
const mockGetLogDetail = vi.fn();
const mockGetUsageAnalytics = vi.fn();
const mockCreateKey = vi.fn();
const mockDisableKey = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getProjects: (...args: any[]) => mockGetProjects(...args),
    getKeys: (...args: any[]) => mockGetKeys(...args),
    getModelCatalog: (...args: any[]) => mockGetModelCatalog(...args),
    getLogs: (...args: any[]) => mockGetLogs(...args),
    getLogDetail: (...args: any[]) => mockGetLogDetail(...args),
    getUsageAnalytics: (...args: any[]) => mockGetUsageAnalytics(...args),
    createKey: (...args: any[]) => mockCreateKey(...args),
    disableKey: (...args: any[]) => mockDisableKey(...args),
  },
}));

const PROJECTS = {
  data: [
    { id: 7, org_id: 1, name: 'Core Project', slug: 'core', status: 'active', key_count: 1, current_user_membership_role: 'project_user' },
  ],
  total: 1,
};

const KEYS = {
  data: [
    { id: 11, project_id: 7, name: 'frontend', key_prefix: 'sk-test', status: 'active', allowed_models: 'company-chat', allowed_ips: '', rpm_limit: 60, rps_limit: 10, qps_limit: 10, created_at: '2026-07-02T10:00:00Z' },
  ],
  total: 1,
};

const MODELS = {
  data: [
    { id: 21, model_name: 'gpt-4o', display_name: 'Company Chat', provider_family: 'openai', lifecycle: 'deprecated', aliases: 'company-chat', context_length: 128000, capability_tags: 'chat,vision', data_residency: 'us', migration_recommendation: 'Migrate to gpt-4.1-mini' },
  ],
  total: 1,
};

const LOGS = {
  data: [
    { id: 31, project_id: 7, request_id: 'req-001', model: 'company-chat', provider: 'openai', channel: 'primary', prompt_tokens: 10, completion_tokens: 5, total_tokens: 15, cost_micros: '30000', status_code: 200, latency_ms: 320, usage_source: 'api', created_at: '2026-07-02T10:00:00Z' },
  ],
  total: 1,
};

const USAGE_ANALYTICS = {
  summary: {
    total_requests: 12,
    success_requests: 11,
    failed_requests: 1,
    total_tokens: 3456,
    cost_micros: '780000',
    avg_latency_ms: 240,
    estimated_ratio: 0,
  },
  data: [
    { key: 'company-chat', total_requests: 12, success_requests: 11, failed_requests: 1, total_tokens: 3456, cost_micros: '780000', avg_latency_ms: 240, estimated_ratio: 0 },
  ],
};

const DENIED_FEATURES: LicenseAccess['features'] = {
  gateway: false,
  agent_capabilities: false,
  oidc: false,
  guardrails: false,
  notifications: false,
  model_governance: false,
  data_exchange: false,
};

function accessWith(
  enabled: Array<keyof LicenseAccess['features']>,
  status: LicenseAccess['status'] = 'active',
  enforceable = true,
): LicenseAccess {
  return {
    status,
    enforceable,
    features: { ...DENIED_FEATURES, ...Object.fromEntries(enabled.map(feature => [feature, true])) },
  };
}

function setupMocks() {
  mockGetProjects.mockResolvedValue(PROJECTS);
  mockGetKeys.mockResolvedValue(KEYS);
  mockGetModelCatalog.mockResolvedValue(MODELS);
  mockGetLogs.mockImplementation((_page: number, _pageSize: number, filters?: { request_id?: string }) => {
    if (filters?.request_id) return Promise.resolve({ data: [{ id: 31 }], total: 1 });
    return Promise.resolve(LOGS);
  });
  mockGetLogDetail.mockResolvedValue(LOGS.data[0]);
  mockGetUsageAnalytics.mockResolvedValue(USAGE_ANALYTICS);
  mockCreateKey.mockResolvedValue({ id: 41, key: 'sk-created-secret', key_prefix: 'sk-created', project_id: 7 });
  mockDisableKey.mockResolvedValue({ message: 'disabled' });
}

function renderPage(role = 'developer', licenseAccess?: LicenseAccess | null) {
  PROJECTS.data[0].current_user_membership_role = role === 'project_admin'
    ? 'project_admin'
    : role === 'viewer' || role === 'project_viewer'
      ? 'project_viewer'
      : 'project_user';
  useAuthStore.setState({
    token: 'token',
    isAuthenticated: true,
    user: { id: 5, username: 'dev', role, organization_role: 'member', org_id: 1, license_access: licenseAccess },
    profileLoading: false,
  });
  return render(
    <MemoryRouter initialEntries={['/developer']}>
      <DeveloperConsolePage />
      <LocationProbe />
    </MemoryRouter>,
  );
}

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname}{location.search}</div>;
}

describe('DeveloperConsolePage', () => {
  beforeEach(() => {
    vi.resetAllMocks();
    setupMocks();
  });

  it('distinguishes a project-list failure from an account with no assigned projects and supports retry', async () => {
    mockGetProjects.mockRejectedValueOnce(new Error('projects down'));

    renderPage();

    expect(await screen.findByText('项目列表加载失败')).toBeTruthy();
    expect(screen.queryByText('没有可访问项目')).toBeNull();
    expect(mockGetKeys).not.toHaveBeenCalled();

    fireEvent.click(screen.getByText('重新加载项目'));

    expect(await screen.findByText('Core Project')).toBeTruthy();
    await waitFor(() => expect(mockGetKeys).toHaveBeenCalledWith(1, 100, { project_id: 7 }));
  });

  it('loads project-scoped keys, approved models, and logs', async () => {
    renderPage();

    await waitFor(() => {
      expect(mockGetKeys).toHaveBeenCalledWith(1, 100, { project_id: 7 });
      expect(mockGetModelCatalog).toHaveBeenCalledWith(1, 100, { project_id: 7 });
      expect(mockGetLogs).toHaveBeenCalledWith(1, 20, { project_id: 7 });
      expect(mockGetUsageAnalytics).toHaveBeenCalledWith({ project_id: 7, scope: 'my', group_by: 'alias' });
    });

    expect(await screen.findByText('Core Project')).toBeTruthy();
    expect(screen.getByText('我的模型用量')).toBeTruthy();
    expect(screen.getAllByText('我的 Key 聚合').length).toBeGreaterThan(0);
    expect(screen.getAllByText('3.5K').length).toBeGreaterThan(0);
    expect(screen.getByText('Company Chat')).toBeTruthy();
    expect(screen.getByText('RPM 60 / RPS 10 / TPM 未配置')).toBeTruthy();
    expect(screen.getByText('Migrate to gpt-4.1-mini')).toBeTruthy();
    expect(screen.getByText('req-001')).toBeTruthy();
    expect(screen.getAllByText('¥0.780000').length).toBeGreaterThan(0);
    expect(screen.getByText('¥0.030000')).toBeTruthy();
    expect(screen.getByText('请求调试台')).toBeTruthy();
    expect(screen.getByPlaceholderText('粘贴项目访问凭证')).toBeTruthy();
    expect(screen.getByText('创建项目访问凭证、停用本人拥有的凭证、查看已批准模型、调试调用并排查当前项目请求。')).toBeTruthy();
    const taskNav = screen.getByRole('navigation', { name: '项目工作台任务入口' });
    expect(within(taskNav).getByRole('link', { name: '概览与用量' }).getAttribute('href')).toBe('#project-usage');
    expect(within(taskNav).getByRole('link', { name: '凭证与接入' }).getAttribute('href')).toBe('#project-access');
    expect(within(taskNav).getByRole('link', { name: '调试与排障' }).getAttribute('href')).toBe('#project-debug');
    expect(within(taskNav).getByRole('link', { name: '已批准模型' }).getAttribute('href')).toBe('#project-models');
  });

  it('loads every server page and exposes later keys and models through client pagination', async () => {
    const firstPageKeys = Array.from({ length: 100 }, (_, index) => ({
      ...KEYS.data[0],
      id: 1000 + index,
      name: `key-${index + 1}`,
      key_prefix: `sk-page-${index + 1}`,
    }));
    const firstPageModels = Array.from({ length: 100 }, (_, index) => ({
      ...MODELS.data[0],
      id: 2000 + index,
      model_name: `model-${index + 1}`,
      display_name: `Model ${index + 1}`,
      aliases: '',
    }));

    mockGetProjects.mockImplementation((page: number) => Promise.resolve(page === 1
      ? { data: PROJECTS.data, total: 101 }
      : { data: [{ id: 8, org_id: 1, name: 'Page Two Project', slug: 'page-two', status: 'active' }], total: 101 }));
    mockGetKeys.mockImplementation((page: number) => Promise.resolve(page === 1
      ? { data: firstPageKeys, total: 101 }
      : { data: [{ ...KEYS.data[0], id: 9999, name: 'server-page-two-key', key_prefix: 'sk-last' }], total: 101 }));
    mockGetModelCatalog.mockImplementation((page: number) => Promise.resolve(page === 1
      ? { data: firstPageModels, total: 101 }
      : { data: [{ ...MODELS.data[0], id: 9999, model_name: 'server-page-two-model', display_name: 'Server Page Two Model', aliases: '' }], total: 101 }));

    renderPage();

    await waitFor(() => {
      expect(mockGetProjects).toHaveBeenCalledWith(2, 100, { status: 'active' });
      expect(mockGetKeys).toHaveBeenCalledWith(2, 100, { project_id: 7 });
      expect(mockGetModelCatalog).toHaveBeenCalledWith(2, 100, { project_id: 7 });
    });

    const keySurface = screen.getByText('项目访问凭证').closest('section');
    expect(keySurface).toBeTruthy();
    fireEvent.click(within(keySurface as HTMLElement).getByLabelText('Page 6'));
    expect(await within(keySurface as HTMLElement).findByText('server-page-two-key')).toBeTruthy();

    const modelSurface = screen.getByText('已批准模型', { selector: 'h5' }).closest('section');
    expect(modelSurface).toBeTruthy();
    fireEvent.click(within(modelSurface as HTMLElement).getByLabelText('Page 6'));
    expect(await within(modelSurface as HTMLElement).findByText('Server Page Two Model')).toBeTruthy();
  });

  it('looks up request detail inside the selected project', async () => {
    renderPage();

    await screen.findByText('req-001');
    fireEvent.change(screen.getByPlaceholderText('req-...'), { target: { value: 'req-001' } });
    fireEvent.click(screen.getByText('查询'));

    await waitFor(() => {
      expect(mockGetLogs).toHaveBeenCalledWith(1, 1, { project_id: 7, request_id: 'req-001' });
      expect(mockGetLogDetail).toHaveBeenCalledWith(31);
    });
    expect(screen.getByText('路由模型')).toBeTruthy();
  });

  it('keeps project data visible when self usage analytics fails', async () => {
    mockGetUsageAnalytics.mockRejectedValueOnce(new Error('analytics down'));

    renderPage();

    await waitFor(() => {
      expect(mockGetKeys).toHaveBeenCalledWith(1, 100, { project_id: 7 });
      expect(mockGetModelCatalog).toHaveBeenCalledWith(1, 100, { project_id: 7 });
      expect(mockGetLogs).toHaveBeenCalledWith(1, 20, { project_id: 7 });
      expect(mockGetUsageAnalytics).toHaveBeenCalledWith({ project_id: 7, scope: 'my', group_by: 'alias' });
    });

    expect(await screen.findByText('frontend')).toBeTruthy();
    expect(screen.getByText('Company Chat')).toBeTruthy();
    expect(screen.getByText('req-001')).toBeTruthy();
    expect(screen.getAllByText('最近 20 条合计').length).toBeGreaterThan(0);
    expect(screen.getByText('个人用量加载失败')).toBeTruthy();
    expect(screen.getByText('重新加载当前项目')).toBeTruthy();
  });

  it('clears the previous project data while switching and shows a retryable error instead of stale values', async () => {
    mockGetProjects.mockResolvedValue({
      data: [
        ...PROJECTS.data,
        { id: 8, org_id: 1, name: 'Secondary Project', slug: 'secondary', status: 'active', key_count: 0, current_user_membership_role: 'project_user' },
      ],
      total: 2,
    });

    renderPage();

    expect(await screen.findByText('req-001')).toBeTruthy();
    let rejectKeys: (reason?: unknown) => void = () => undefined;
    mockGetKeys.mockImplementationOnce(() => new Promise((_resolve, reject) => {
      rejectKeys = reject;
    }));

    fireEvent.click(screen.getAllByRole('combobox')[0]);
    const secondaryLabels = await screen.findAllByText('Secondary Project (secondary)');
    fireEvent.click(secondaryLabels[secondaryLabels.length - 1]);

    expect(await screen.findByText(
      '正在加载当前项目数据',
      undefined,
      { timeout: 5000 },
    )).toBeTruthy();
    expect(screen.queryByText('req-001')).toBeNull();
    expect(screen.queryByText('请求数')).toBeNull();
    await waitFor(() => expect(mockGetKeys).toHaveBeenCalledWith(1, 100, { project_id: 8 }));

    await act(async () => {
      rejectKeys(new Error('keys down'));
    });

    expect(await screen.findByText('项目数据加载失败')).toBeTruthy();
    expect(screen.queryByText('req-001')).toBeNull();
    expect(screen.queryByText('请求数')).toBeNull();

    fireEvent.click(screen.getByText('重新加载当前项目'));
    await waitFor(() => expect(mockGetKeys).toHaveBeenLastCalledWith(1, 100, { project_id: 8 }));
    expect(await screen.findByText('req-001')).toBeTruthy();
  });

  it('clears current values while a manual refresh is in progress', async () => {
    renderPage();

    expect(await screen.findByText('req-001')).toBeTruthy();
    let resolveKeys: (value: typeof KEYS) => void = () => undefined;
    mockGetKeys.mockImplementationOnce(() => new Promise((resolve) => {
      resolveKeys = resolve;
    }));

    fireEvent.click(screen.getByText('刷新'));

    expect(await screen.findByText(
      '正在加载当前项目数据',
      undefined,
      { timeout: 5000 },
    )).toBeTruthy();
    expect(screen.queryByText('req-001')).toBeNull();
    expect(screen.queryByText('请求数')).toBeNull();

    await act(async () => {
      resolveKeys(KEYS);
    });

    expect(await screen.findByText('req-001')).toBeTruthy();
  });

  it('creates project-scoped keys with the selected project_id', async () => {
    renderPage('project_admin');

    await screen.findByText('frontend');
    fireEvent.click(screen.getByText('创建项目凭证'));
    const nameInput = await screen.findByPlaceholderText('frontend-prod');
    fireEvent.change(nameInput, { target: { value: 'dev-key' } });
    fireEvent.click(screen.getByText(/^创建$/));

    await waitFor(() => {
      expect(mockCreateKey).toHaveBeenCalledWith(expect.objectContaining({
        org_id: 1,
        project_id: 7,
        name: 'dev-key',
        rps_limit: 10,
      }));
    });
    expect(await screen.findByText('sk-created-secret')).toBeTruthy();
  });

  it('lets a developer stop only their own active project key', async () => {
    mockGetKeys.mockResolvedValue({
      data: [
        { ...KEYS.data[0], id: 51, name: 'owned-by-developer', owner_user_id: 5, status: 'active' },
        { ...KEYS.data[0], id: 52, name: 'owned-by-another-user', owner_user_id: 9, status: 'active' },
        { ...KEYS.data[0], id: 53, name: 'owned-but-disabled', owner_user_id: 5, status: 'disabled' },
      ],
      total: 3,
    });

    renderPage('developer');

    const ownRow = (await screen.findByText('owned-by-developer')).closest('tr');
    const otherRow = screen.getByText('owned-by-another-user').closest('tr');
    const disabledRow = screen.getByText('owned-but-disabled').closest('tr');
    expect(ownRow).toBeTruthy();
    expect(otherRow).toBeTruthy();
    expect(disabledRow).toBeTruthy();
    expect(within(ownRow as HTMLElement).getByRole('button', { name: '停用项目 Key' })).toBeTruthy();
    expect(within(otherRow as HTMLElement).queryByRole('button', { name: '停用项目 Key' })).toBeNull();
    expect(within(disabledRow as HTMLElement).queryByRole('button', { name: '停用项目 Key' })).toBeNull();
  });

  it('keeps project administrators able to stop any active key in their project', async () => {
    mockGetKeys.mockResolvedValue({
      data: [
        { ...KEYS.data[0], id: 61, name: 'project-admin-own-key', owner_user_id: 5, status: 'active' },
        { ...KEYS.data[0], id: 62, name: 'project-admin-other-key', owner_user_id: 9, status: 'active' },
      ],
      total: 2,
    });

    renderPage('project_admin');

    await screen.findByText('project-admin-own-key');
    expect(screen.getAllByRole('button', { name: '停用项目 Key' })).toHaveLength(2);
  });

  it('opens notifications only with the selected project context for project administrators', async () => {
    renderPage('project_admin');

    fireEvent.click(await screen.findByRole('button', { name: '打开当前项目通知' }));

    await waitFor(() => {
      expect(screen.getByTestId('location')).toHaveTextContent('/notifications?project_id=7');
    });
  });

  it('keeps the read-only viewer from issuing paid debug requests', async () => {
    renderPage('viewer');

    expect(await screen.findByText('Core Project')).toBeTruthy();
    expect(screen.getByText('项目空间')).toBeTruthy();
    expect(screen.getByText('查看当前项目的个人用量、已批准模型和最近调用，并按 Request ID 排查请求。')).toBeTruthy();
    expect(screen.getByText('只读查看当前项目的凭证前缀和使用范围；创建或停用请联系项目管理员。')).toBeTruthy();
    expect(screen.queryByText('创建后只显示一次完整凭证；列表仅显示前缀。')).toBeNull();
    expect(screen.queryByText('请求调试台')).toBeNull();
    expect(screen.queryByPlaceholderText('粘贴项目访问凭证')).toBeNull();
    expect(screen.queryByText('创建项目凭证')).toBeNull();
    const taskNav = screen.getByRole('navigation', { name: '项目工作台任务入口' });
    expect(within(taskNav).queryByRole('link', { name: '调试与排障' })).toBeNull();
    expect(within(taskNav).getByRole('link', { name: '概览与用量' })).toBeTruthy();
    expect(within(taskNav).getByRole('link', { name: '凭证与接入' })).toBeTruthy();
    expect(within(taskNav).getByRole('link', { name: '已批准模型' })).toBeTruthy();
  });

  it('degrades only the model-governance section when a partial license omits it', async () => {
    renderPage('project_admin', accessWith(['gateway', 'notifications']));

    expect(await screen.findByText('frontend')).toBeTruthy();
    expect(screen.getByText('req-001')).toBeTruthy();
    expect(screen.getByText('我的模型用量')).toBeTruthy();
    expect(mockGetModelCatalog).not.toHaveBeenCalled();
    expect(screen.getByText('模型目录暂不可用')).toBeTruthy();
    expect(screen.getAllByText(/当前商业授权未包含「模型治理」/).length).toBeGreaterThan(0);
    expect(screen.getByPlaceholderText('输入现有模型或别名')).toBeTruthy();
    expect(screen.getByRole('button', { name: '打开当前项目通知' }).hasAttribute('disabled')).toBe(false);
  });

  it('locks gateway-dependent help and debugging while preserving keys, logs, usage, and model catalog', async () => {
    renderPage('developer', accessWith(['model_governance']));

    expect(await screen.findByText('frontend')).toBeTruthy();
    expect(screen.getByText('Company Chat')).toBeTruthy();
    expect(screen.getByText('req-001')).toBeTruthy();
    expect(screen.getByText('我的模型用量')).toBeTruthy();
    expect(screen.getByText('模型接入暂不可用')).toBeTruthy();
    expect(screen.getByText('调试调用暂不可用')).toBeTruthy();
    expect(screen.getAllByText(/当前商业授权未包含「模型网关」/).length).toBeGreaterThan(0);
    expect(screen.getByText('创建项目凭证')).toBeTruthy();
    expect(screen.queryByPlaceholderText('粘贴项目访问凭证')).toBeNull();
  });

  it('keeps the project notification entry visible but locked when notifications are unlicensed', async () => {
    renderPage('project_admin', accessWith(['gateway', 'model_governance']));

    expect(await screen.findByText('Core Project')).toBeTruthy();
    const notificationButton = screen.getByRole('button', { name: '项目通知（授权未包含）' });
    expect(notificationButton.hasAttribute('disabled')).toBe(true);
    fireEvent.click(notificationButton);
    expect(screen.getByTestId('location')).toHaveTextContent('/developer');
  });

  it('fail-closes every licensed section for an expired wildcard projection without hiding unrelated data', async () => {
    const allFeatures = Object.keys(DENIED_FEATURES) as Array<keyof LicenseAccess['features']>;
    renderPage('project_admin', accessWith(allFeatures, 'expired', false));

    expect(await screen.findByText('frontend')).toBeTruthy();
    expect(screen.getByText('req-001')).toBeTruthy();
    expect(screen.getByText('我的模型用量')).toBeTruthy();
    expect(mockGetModelCatalog).not.toHaveBeenCalled();
    expect(screen.getByText('模型目录暂不可用')).toBeTruthy();
    expect(screen.getByText('模型接入暂不可用')).toBeTruthy();
    expect(screen.getByText('调试调用暂不可用')).toBeTruthy();
    expect(screen.getByRole('button', { name: '项目通知（授权未包含）' }).hasAttribute('disabled')).toBe(true);
    expect(screen.getAllByText(/当前商业授权已过期/).length).toBeGreaterThan(0);
  });
});
