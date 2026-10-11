import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { useLocation, useNavigate } from 'react-router-dom';
import { MemoryRouter } from '../../test/MemoryRouter';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ModelReadinessPage from '../ModelReadinessPage';
import { ApiError } from '../../api/transport';
import { setLocale } from '../../i18n';

const mocks = vi.hoisted(() => ({
  getProjects: vi.fn(),
  getModelCatalog: vi.fn(),
  listModelDeployments: vi.fn(),
  getWorkloadApplications: vi.fn(),
  modelReadiness: vi.fn(),
}));

vi.mock('../../api/client', () => ({
  api: {
    getProjects: (...args: unknown[]) => mocks.getProjects(...args),
    getModelCatalog: (...args: unknown[]) => mocks.getModelCatalog(...args),
    getWorkloadApplications: (...args: unknown[]) => mocks.getWorkloadApplications(...args),
  },
}));

vi.mock('../../api/modelDeployments', () => ({
  canonicalOperationPaths: {
    'openai.chat_completions.v1': '/v1/chat/completions',
    'openai.embeddings.v1': '/v1/embeddings',
    'openai.responses.v1': '/v1/responses',
    'anthropic.messages.v1': '/v1/messages',
  },
  modelDeploymentApi: {
    list: (...args: unknown[]) => mocks.listModelDeployments(...args),
  },
}));

vi.mock('../../api/access', () => ({ accessApi: { modelReadiness: (...args: unknown[]) => mocks.modelReadiness(...args) } }));

const axis = (state: string, reason: string, fixRoute?: string) => ({
  state, reason_code: reason, safe_evidence: {}, fix_route: fixRoute, fix_owner: fixRoute ? 'project_admin' : '',
});

const readiness = {
  contract_version: 'unirouter.model-readiness.v2',
  scope: { mode: 'caller', org_id: 1, project_id: 11, requested_model: 'company-chat', resolved_model: 'gpt-5', endpoint: '/v1/chat/completions', model_operation_id: 'openai.chat_completions.v1', caller: 'self' },
  summary: {
    registered: { value: false, state: 'blocked', reason_code: 'model_unregistered' },
    catalog_visible: { value: false, state: 'blocked', reason_code: 'catalog_hidden' },
    routable: { value: true, state: 'ready', reason_code: 'route_available' },
    current_project_caller_callable: { state: 'unavailable', reason_code: 'caller_evidence_unavailable' },
  },
  platform_model: {
    gateway_license: axis('ready', 'gateway_license_authorized'),
    registration: axis('blocked', 'model_unregistered', '/model-governance?tab=registry&model=company-chat'), lifecycle: axis('ready', 'lifecycle_active'),
    catalog: axis('blocked', 'catalog_hidden'), price_authority: axis('ready', 'price_authority_ready'),
    deployment: axis('ready', 'deployment_configured'), provider_credential: axis('ready', 'provider_credential_ready'),
    operation: axis('ready', 'operation_configured'), base_route: axis('ready', 'route_available'), runtime_slot: axis('not_applicable', 'runtime_slot_optional'),
  },
  project_caller: {
    project: axis('ready', 'project_active'), alias: axis('not_applicable', 'alias_not_used'), model_set: axis('ready', 'project_default_catalog'),
    wallet: axis('ready', 'wallet_ready'), project_budget: axis('warning', 'budget_monitoring_only'), application: axis('not_applicable', 'self_caller'),
    grant: axis('ready', 'grant_active'), credential: axis('ready', 'credential_active'), model_authorization: axis('unavailable', 'policy_store_unavailable'),
    limit_policy: axis('ready', 'limit_policy_ready'),
  },
  runtime_mutation: 'none', external_requests: 'none', as_of: '2026-08-10T00:00:00Z',
};

function LocationProbe() {
  const location = useLocation();
  return <output data-testid="location">{location.pathname}{location.search}</output>;
}

function ScopeSwitch() {
  const navigate = useNavigate();
  return <button onClick={() => navigate('/model-readiness?project_id=12&model=project-b-model&endpoint=%2Fv1%2Fembeddings&caller=self')}>switch-readiness-scope</button>;
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((complete) => { resolve = complete; });
  return { promise, resolve };
}

describe('ModelReadinessPage', () => {
  afterEach(() => { act(() => setLocale('zh-CN')); });
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getProjects.mockResolvedValue({ data: [{ id: 11, name: 'Core', status: 'active' }], total: 1 });
    mocks.listModelDeployments.mockResolvedValue({
      contract_version: 'unirouter.model-deployments.v1',
      generated_at: '2026-08-10T00:00:00Z',
      summary: {},
      applications: [
        {
          public_model: { id: 21, model_name: 'deepseek-v4-pro', display_name: 'DeepSeek V4 Pro' },
          deployment: { id: 12 },
          connection: { id: 31, name: 'DeepSeek production', provider: 'deepseek' },
          operations: [{ operation_id: 'openai.chat_completions.v1' }],
        },
        {
          public_model: { id: 22, model_name: 'company-chat', display_name: 'Company Chat' },
          deployment: { id: 13 },
          connection: { id: 32, name: 'Company production', provider: 'openai' },
          operations: [
            { operation_id: 'openai.chat_completions.v1' },
            { operation_id: 'openai.embeddings.v1' },
          ],
        },
      ],
    });
    mocks.getModelCatalog.mockResolvedValue({
      data: [
        { model_name: 'deepseek-v4-pro', display_name: 'DeepSeek V4 Pro', aliases: [] },
        { model_name: 'company-chat', display_name: 'Company Chat', aliases: ['chat-default'] },
      ],
      total: 2,
    });
    mocks.getWorkloadApplications.mockResolvedValue({ data: [{ application_ref: 'app_core', display_name: 'Core App', project_id: 11, environment: 'production', status: 'active' }] });
    mocks.modelReadiness.mockResolvedValue(readiness);
  });

  it('preserves connection scope and evidence across locale changes, without showing retired commercial licensing', async () => {
    mocks.modelReadiness.mockResolvedValue({
      ...readiness,
      scope: { mode: 'connection', org_id: 1, project_id: 0, deployment_id: 12, requested_model: 'deepseek-v4-pro', resolved_model: 'deepseek-v4-pro', endpoint: '/v1/chat/completions', model_operation_id: 'openai.chat_completions.v1', caller: '' },
      platform_model: { ...readiness.platform_model, gateway_license: axis('not_applicable', 'commercial_license_not_required') },
    });
    setLocale('en-US');
    render(<MemoryRouter initialEntries={['/model-readiness?from=connections&deployment_id=12&operation_id=openai.chat_completions.v1&model=deepseek-v4-pro']}><ModelReadinessPage /></MemoryRouter>);
    expect(await screen.findByText('Model: deepseek-v4-pro')).toBeInTheDocument();
    const reads = mocks.modelReadiness.mock.calls.length;
    const lists = mocks.listModelDeployments.mock.calls.length;
    expect(screen.queryByText('Gateway entitlement')).not.toBeInTheDocument();
    expect(screen.getByRole('combobox', { name: 'Model or alias' })).toHaveTextContent('DeepSeek V4 Pro');
    await act(async () => setLocale('zh-CN'));
    expect(screen.getByText('当前模型：deepseek-v4-pro')).toBeInTheDocument();
    expect(screen.getByRole('combobox', { name: '模型或 Alias' })).toHaveTextContent('DeepSeek V4 Pro');
    expect(screen.queryByText('模型网关授权')).not.toBeInTheDocument();
    expect(mocks.modelReadiness).toHaveBeenCalledTimes(reads);
    expect(mocks.listModelDeployments).toHaveBeenCalledTimes(lists);
  });

  it('leaves an unselected deployment empty and does not diagnose an incomplete connection', async () => {
    render(<MemoryRouter initialEntries={['/model-readiness?mode=connection']}><ModelReadinessPage /></MemoryRouter>);
    await waitFor(() => expect(mocks.listModelDeployments).toHaveBeenCalled());
    expect(screen.getByRole('combobox', { name: '模型部署' })).toHaveTextContent('请先选择模型');
    expect(mocks.modelReadiness).not.toHaveBeenCalled();
  });

  it('renders four independent facts and preserves unavailable as unknown', async () => {
    render(<MemoryRouter initialEntries={['/model-readiness?project_id=11&model=company-chat&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self']}><ModelReadinessPage /></MemoryRouter>);

    expect(await screen.findByText('当前模型：gpt-5')).toBeTruthy();
    expect(screen.getAllByText('model_unregistered').length).toBeGreaterThan(0);
    expect(screen.getAllByText('catalog_hidden').length).toBeGreaterThan(0);
    expect(screen.getByText('policy_store_unavailable')).toBeTruthy();
    expect(screen.getAllByText('暂时无法判断').length).toBeGreaterThan(0);
    expect(document.querySelector('.ur-metric-value')).toBeNull();
    expect(mocks.modelReadiness).toHaveBeenCalledWith({
      project_id: 11, model: 'company-chat', endpoint: '/v1/chat/completions', caller: 'self', application_ref: undefined,
    });
  });

  it('auto-selects the only active project instead of requiring a redundant project choice', async () => {
    render(<MemoryRouter initialEntries={['/model-readiness?model=company-chat&caller=self']}><ModelReadinessPage /></MemoryRouter>);

    expect(await screen.findByText('唯一有效项目，已自动选择')).toBeTruthy();
    expect(screen.queryByRole('combobox', { name: '项目' })).toBeNull();
    await waitFor(() => expect(mocks.modelReadiness).toHaveBeenCalledWith(expect.objectContaining({ project_id: 11, model: 'company-chat' })));
  });

	it('shows the concrete caller blocker instead of a generic unavailable subject label', async () => {
		mocks.modelReadiness.mockResolvedValueOnce({
			...readiness,
			summary: {
				...readiness.summary,
				registered: { value: true, state: 'ready', reason_code: 'model_registered' },
				catalog_visible: { value: true, state: 'ready', reason_code: 'catalog_published' },
				current_project_caller_callable: { value: false, state: 'blocked', reason_code: 'access_credential_missing' },
			},
			project_caller: { ...readiness.project_caller, credential: axis('blocked', 'access_credential_missing', '/applications') },
		});
		render(<MemoryRouter initialEntries={['/model-readiness?project_id=11&model=company-chat&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self']}><ModelReadinessPage /></MemoryRouter>);

		expect(await screen.findByText('所选调用身份可用')).toBeTruthy();
		expect(screen.getAllByText('所选调用身份缺少可用的 API Key').length).toBeGreaterThan(0);
		expect(screen.getByRole('combobox', { name: '谁来调用' })).toHaveTextContent('当前账号的个人 API Key');
		expect(screen.getByText('调用 API Key')).toBeTruthy();
	});

  it('explains a missing gateway license instead of showing a generic blocker', async () => {
    mocks.modelReadiness.mockResolvedValueOnce({
      ...readiness,
      summary: {
        ...readiness.summary,
        current_project_caller_callable: { value: false, state: 'blocked', reason_code: 'license_error.missing' },
      },
      platform_model: { ...readiness.platform_model, gateway_license: axis('blocked', 'license_error.missing') },
    });
    render(<MemoryRouter initialEntries={['/model-readiness?project_id=11&model=company-chat&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self']}><ModelReadinessPage /></MemoryRouter>);

    expect((await screen.findAllByText('当前部署的模型网关商业授权未就绪，请由平台管理员检查许可证')).length).toBeGreaterThan(0);
  });

  it('diagnoses a model connection without loading or requiring project and caller scope', async () => {
    mocks.modelReadiness.mockResolvedValueOnce({
      ...readiness,
      scope: { mode: 'connection', org_id: 1, project_id: 0, deployment_id: 12, requested_model: 'deepseek-v4-pro', resolved_model: 'deepseek-v4-pro', endpoint: '/v1/chat/completions', model_operation_id: 'openai.chat_completions.v1', caller: '' },
      summary: { ...readiness.summary, current_project_caller_callable: { state: 'not_applicable', reason_code: 'connection_mode_has_no_caller' } },
    });
    render(<MemoryRouter initialEntries={['/model-readiness?from=connections&deployment_id=12&operation_id=openai.chat_completions.v1&model=deepseek-v4-pro']}><ModelReadinessPage /></MemoryRouter>);

    expect(await screen.findByRole('heading', { name: '模型连接诊断' })).toBeTruthy();
    await waitFor(() => expect(mocks.modelReadiness).toHaveBeenCalledWith({
      mode: 'connection', deployment_id: 12, model: 'deepseek-v4-pro', endpoint: '/v1/chat/completions',
    }));
    expect(mocks.getProjects).not.toHaveBeenCalled();
    expect(mocks.getModelCatalog).not.toHaveBeenCalled();
    expect(mocks.listModelDeployments).toHaveBeenCalled();
    expect(screen.queryByRole('combobox', { name: '项目' })).toBeNull();
    expect(screen.queryByRole('combobox', { name: '谁来调用' })).toBeNull();
    expect(screen.getByRole('combobox', { name: '模型部署' })).toHaveTextContent('DeepSeek production');
    expect(screen.getAllByText('模型网关授权').length).toBeGreaterThan(0);
    expect(screen.queryByText('项目、应用与权限')).toBeNull();
  });

  it('labels a 503 as unavailable rather than blocked or unconfigured', async () => {
    mocks.modelReadiness.mockRejectedValue(new ApiError('unavailable', 503, {}));
    render(<MemoryRouter initialEntries={['/model-readiness?project_id=11&model=company-chat&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self']}><ModelReadinessPage /></MemoryRouter>);

    expect(await screen.findByText(/当前状态是“暂时无法判断”，不是“未配置”或“受阻”/)).toBeTruthy();
    expect(screen.queryByText('当前项目调用主体不可调用')).toBeNull();
  });

  it('follows the exact actor-accessible fix route including blocker context', async () => {
    render(<MemoryRouter initialEntries={['/model-readiness?project_id=11&model=company-chat&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self']}><ModelReadinessPage /><LocationProbe /></MemoryRouter>);

    const registrationCard = await screen.findByRole('region', { name: '模型记录诊断' });
    fireEvent.click(within(registrationCard).getByRole('button', { name: '前往修复' }));
    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('/model-governance?tab=registry&model=company-chat'));
  });

  it('never renders a slower prior-scope readiness response after project/model/caller scope changes', async () => {
    const scopeA = deferred<typeof readiness>();
    const scopeB = {
      ...readiness,
      scope: {
        ...readiness.scope,
        project_id: 12,
        requested_model: 'project-b-model',
        resolved_model: 'project-b-resolved',
        endpoint: '/v1/embeddings',
        model_operation_id: 'openai.embeddings.v1',
      },
    };
    mocks.modelReadiness.mockImplementation((input: { project_id: number }) => input.project_id === 11 ? scopeA.promise : Promise.resolve(scopeB));

    render(<MemoryRouter initialEntries={['/model-readiness?project_id=11&model=company-chat&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self']}><ModelReadinessPage /><ScopeSwitch /></MemoryRouter>);
    await waitFor(() => expect(mocks.modelReadiness).toHaveBeenCalledWith(expect.objectContaining({ project_id: 11 })));
    fireEvent.click(screen.getByRole('button', { name: 'switch-readiness-scope' }));
    expect(await screen.findByText('当前模型：project-b-resolved')).toBeTruthy();

    scopeA.resolve(readiness);
    await waitFor(() => expect(screen.queryByText('当前模型：gpt-5')).toBeNull());
    expect(screen.getByText('当前模型：project-b-resolved')).toBeTruthy();
  });

  it('never renders a slower prior-project catalog after the readiness scope changes', async () => {
    const projectACatalog = deferred<{ data: Array<{ model_name: string; display_name: string; aliases: string[] }>; total: number }>();
    mocks.getModelCatalog.mockImplementation((_page: number, _size: number, filters: { project_id: number }) => filters.project_id === 11
      ? projectACatalog.promise
      : Promise.resolve({ data: [{ model_name: 'project-b-model', display_name: 'Project B', aliases: ['project-b-alias'] }], total: 1 }));

    render(<MemoryRouter initialEntries={['/model-readiness?project_id=11&model=company-chat&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self']}><ModelReadinessPage /><ScopeSwitch /></MemoryRouter>);
    await waitFor(() => expect(mocks.getModelCatalog).toHaveBeenCalledWith(1, 100, { project_id: 11, routable_only: true }));
    fireEvent.click(screen.getByRole('button', { name: 'switch-readiness-scope' }));
    await waitFor(() => expect(mocks.getModelCatalog).toHaveBeenCalledWith(1, 100, { project_id: 12, routable_only: true }));

    fireEvent.click(screen.getByRole('combobox', { name: '模型或 Alias' }));
	    expect((await screen.findAllByText('Project B')).length).toBeGreaterThan(0);
	    expect(screen.queryByText('project-b-alias（Alias → project-b-model）')).toBeNull();
    projectACatalog.resolve({ data: [{ model_name: 'project-a-model', display_name: 'Project A', aliases: ['project-a-alias'] }], total: 1 });
	    await waitFor(() => expect(screen.queryByText('Project A')).toBeNull());
  });

  it('uses searchable existing-object selectors instead of manual model and application refs', async () => {
    render(<MemoryRouter initialEntries={['/model-readiness?model=deepseek-v4-pro&caller=self']}><ModelReadinessPage /></MemoryRouter>);

    expect(await screen.findByRole('combobox', { name: '模型或 Alias' })).toBeTruthy();
    await waitFor(() => expect(mocks.getModelCatalog).toHaveBeenCalledWith(1, 100, { project_id: 11, routable_only: true }));
    expect(mocks.listModelDeployments).not.toHaveBeenCalled();
    expect(screen.queryByRole('textbox', { name: '模型或 Alias' })).toBeNull();
    fireEvent.click(screen.getByRole('combobox', { name: '模型或 Alias' }));
	    expect(await screen.findByText('Company Chat')).toBeTruthy();
	    expect(screen.queryByText('chat-default（Alias → company-chat）')).toBeNull();
    fireEvent.click(screen.getByRole('combobox', { name: '谁来调用' }));
    fireEvent.click(await screen.findByText('应用 / Agent'));
    expect(await screen.findByRole('combobox', { name: '应用 / Agent' })).toBeTruthy();
    expect(screen.queryByText('Application ref（Workload 时必填）')).toBeNull();
  });

  it('gives every readiness selector a stable user-facing accessible name', async () => {
    mocks.getProjects.mockResolvedValueOnce({
      data: [
        { id: 11, name: 'Core', status: 'active' },
        { id: 12, name: 'Research', status: 'active' },
      ],
      total: 2,
    });
    render(<MemoryRouter initialEntries={['/model-readiness?project_id=11&model=company-chat&caller=self']}><ModelReadinessPage /></MemoryRouter>);

    expect(await screen.findByRole('combobox', { name: '项目' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '模型或 Alias' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '调用类型' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '谁来调用' })).toBeTruthy();
  });
});
