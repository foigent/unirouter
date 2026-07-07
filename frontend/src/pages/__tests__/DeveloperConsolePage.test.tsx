import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import DeveloperConsolePage from '../DeveloperConsolePage';
import { useAuthStore } from '../../store/auth';

const mockGetProjects = vi.fn();
const mockGetKeys = vi.fn();
const mockGetModelCatalog = vi.fn();
const mockGetLogs = vi.fn();
const mockGetLogDetail = vi.fn();
const mockCreateKey = vi.fn();
const mockDisableKey = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getProjects: (...args: any[]) => mockGetProjects(...args),
    getKeys: (...args: any[]) => mockGetKeys(...args),
    getModelCatalog: (...args: any[]) => mockGetModelCatalog(...args),
    getLogs: (...args: any[]) => mockGetLogs(...args),
    getLogDetail: (...args: any[]) => mockGetLogDetail(...args),
    createKey: (...args: any[]) => mockCreateKey(...args),
    disableKey: (...args: any[]) => mockDisableKey(...args),
  },
}));

const PROJECTS = {
  data: [
    { id: 7, org_id: 1, name: 'Core Project', slug: 'core', status: 'active', key_count: 1 },
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
    { id: 31, project_id: 7, request_id: 'req-001', model: 'company-chat', provider: 'openai', channel: 'primary', prompt_tokens: 10, completion_tokens: 5, total_tokens: 15, cost_cents: 3, status_code: 200, latency_ms: 320, usage_source: 'api', created_at: '2026-07-02T10:00:00Z' },
  ],
  total: 1,
};

function setupMocks() {
  mockGetProjects.mockResolvedValue(PROJECTS);
  mockGetKeys.mockResolvedValue(KEYS);
  mockGetModelCatalog.mockResolvedValue(MODELS);
  mockGetLogs.mockImplementation((_page: number, _pageSize: number, filters?: { request_id?: string }) => {
    if (filters?.request_id) return Promise.resolve({ data: [{ id: 31 }], total: 1 });
    return Promise.resolve(LOGS);
  });
  mockGetLogDetail.mockResolvedValue(LOGS.data[0]);
  mockCreateKey.mockResolvedValue({ id: 41, key: 'sk-created-secret', key_prefix: 'sk-created', project_id: 7 });
  mockDisableKey.mockResolvedValue({ message: 'disabled' });
}

function renderPage(role = 'developer') {
  useAuthStore.setState({
    token: 'token',
    isAuthenticated: true,
    user: { id: 5, username: 'dev', role, org_id: 1 },
    profileLoading: false,
  });
  return render(<DeveloperConsolePage />);
}

describe('DeveloperConsolePage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setupMocks();
  });

  it('loads project-scoped keys, approved models, and logs', async () => {
    renderPage();

    await waitFor(() => {
      expect(mockGetKeys).toHaveBeenCalledWith(1, 50, { project_id: 7 });
      expect(mockGetModelCatalog).toHaveBeenCalledWith(1, 100, { project_id: 7 });
      expect(mockGetLogs).toHaveBeenCalledWith(1, 20, { project_id: 7 });
    });

    expect(await screen.findByText('Core Project')).toBeTruthy();
    expect(screen.getByText('Company Chat')).toBeTruthy();
    expect(screen.getByText('RPM 60 / RPS 10 / TPM 未配置')).toBeTruthy();
    expect(screen.getByText('Migrate to gpt-4.1-mini')).toBeTruthy();
    expect(screen.getByText('req-001')).toBeTruthy();
    expect(screen.getByText('请求调试台')).toBeTruthy();
    expect(screen.getByPlaceholderText('粘贴项目访问凭证')).toBeTruthy();
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
});
