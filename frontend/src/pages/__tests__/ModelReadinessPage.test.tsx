import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import ModelReadinessPage from '../ModelReadinessPage';
import { ApiError } from '../../api/client';

const mocks = vi.hoisted(() => ({ getProjects: vi.fn(), modelReadiness: vi.fn() }));

vi.mock('../../api/client', () => ({
  ApiError: class ApiError extends Error {
    status: number;
    constructor(message: string, status: number) { super(message); this.status = status; }
  },
  api: { getProjects: (...args: unknown[]) => mocks.getProjects(...args) },
}));

vi.mock('../../api/access', () => ({ accessApi: { modelReadiness: (...args: unknown[]) => mocks.modelReadiness(...args) } }));

const axis = (state: string, reason: string, fixRoute?: string) => ({
  state, reason_code: reason, safe_evidence: {}, fix_route: fixRoute, fix_owner: fixRoute ? 'project_admin' : '',
});

const readiness = {
  contract_version: 'unirouter.model-readiness.v1',
  scope: { org_id: 1, project_id: 11, requested_model: 'company-chat', resolved_model: 'gpt-5', endpoint: '/v1/chat/completions', caller: 'self' },
  summary: {
    registered: { value: false, state: 'blocked', reason_code: 'model_unregistered' },
    catalog_visible: { value: false, state: 'blocked', reason_code: 'catalog_hidden' },
    routable: { value: true, state: 'ready', reason_code: 'route_available' },
    current_project_caller_callable: { state: 'unavailable', reason_code: 'caller_evidence_unavailable' },
  },
  platform_model: {
    registration: axis('blocked', 'model_unregistered', '/model-governance?tab=registry&model=company-chat'), lifecycle: axis('ready', 'lifecycle_active'),
    catalog: axis('blocked', 'catalog_hidden'), price_authority: axis('ready', 'price_authority_ready'),
    channel: axis('ready', 'channel_ready'), provider_credential: axis('ready', 'provider_credential_ready'),
    base_route: axis('ready', 'route_available'), runtime_slot: axis('not_applicable', 'runtime_slot_optional'),
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

describe('ModelReadinessPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getProjects.mockResolvedValue({ data: [{ id: 11, name: 'Core', status: 'active' }], total: 1 });
    mocks.modelReadiness.mockResolvedValue(readiness);
  });

  it('renders four independent facts and preserves unavailable as unknown', async () => {
    render(<MemoryRouter initialEntries={['/model-readiness?project_id=11&model=company-chat&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self']}><ModelReadinessPage /></MemoryRouter>);

    expect(await screen.findByText('resolved_model=gpt-5 · runtime_mutation=none · external_requests=none')).toBeTruthy();
    expect(screen.getByText('model_unregistered')).toBeTruthy();
    expect(screen.getByText('catalog_hidden')).toBeTruthy();
    expect(screen.getByText('policy_store_unavailable')).toBeTruthy();
    expect(screen.getAllByText('暂时无法判断').length).toBeGreaterThan(0);
    expect(mocks.modelReadiness).toHaveBeenCalledWith({
      project_id: 11, model: 'company-chat', endpoint: '/v1/chat/completions', caller: 'self', application_ref: undefined,
    });
  });

  it('labels a 503 as unavailable rather than blocked or unconfigured', async () => {
    mocks.modelReadiness.mockRejectedValue(new ApiError('unavailable', 503, {}));
    render(<MemoryRouter initialEntries={['/model-readiness?project_id=11&model=company-chat&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self']}><ModelReadinessPage /></MemoryRouter>);

    expect(await screen.findByText(/当前状态是“暂时无法判断”，不是“未配置”或“受阻”/)).toBeTruthy();
    expect(screen.queryByText('当前项目调用主体不可调用')).toBeNull();
  });

  it('follows the exact actor-accessible fix route including blocker context', async () => {
    render(<MemoryRouter initialEntries={['/model-readiness?project_id=11&model=company-chat&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self']}><ModelReadinessPage /><LocationProbe /></MemoryRouter>);

    await screen.findByText('model_unregistered');
    fireEvent.click(screen.getByRole('button', { name: '前往修复' }));
    expect(screen.getByTestId('location').textContent).toBe('/model-governance?tab=registry&model=company-chat');
  });
});
