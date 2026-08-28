import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Modal, Toast } from '@douyinfe/semi-ui';
import { MemoryRouter } from '../../test/MemoryRouter';
import { pasteValue } from '../../test/user';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ModelEndpointsPage from '../ModelEndpointsPage';
import {
  canonicalOperationBaseURLForPreset,
  canonicalOperationOriginError,
  providerPresetBaseURLError,
} from '../../features/modelEndpoints/ModelEndpointCreateSheet';
import { useAuthStore } from '../../store/auth';

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  get: vi.fn(),
  create: vi.fn(),
  probe: vi.fn(),
  getCapabilityQualification: vi.fn(),
  releaseList: vi.fn(),
  authorRelease: vi.fn(),
  lifecycleCurrent: vi.fn(),
  activateInitial: vi.fn(),
  startCanary: vi.fn(),
  promote: vi.fn(),
  cancelCanary: vi.fn(),
  rollbackStable: vi.fn(),
  cutoverLegacy: vi.fn(),
  rollbackLegacy: vi.fn(),
  profileList: vi.fn(),
  importOfficialRateCardDraft: vi.fn(),
  getRateCards: vi.fn(),
}));

vi.mock('../../api/modelDeploymentReleases', async importOriginal => {
  const actual = await importOriginal<typeof import('../../api/modelDeploymentReleases')>();
  return {
    ...actual,
    modelDeploymentReleaseApi: {
      list: mocks.releaseList,
      author: mocks.authorRelease,
    },
  };
});

vi.mock('../../api/modelDeploymentLifecycle', async importOriginal => {
  const actual = await importOriginal<typeof import('../../api/modelDeploymentLifecycle')>();
  return {
    ...actual,
    modelDeploymentLifecycleApi: {
      current: mocks.lifecycleCurrent,
      activateInitial: mocks.activateInitial,
      startCanary: mocks.startCanary,
      promote: mocks.promote,
      cancelCanary: mocks.cancelCanary,
      rollbackStable: mocks.rollbackStable,
      cutoverLegacy: mocks.cutoverLegacy,
      rollbackLegacy: mocks.rollbackLegacy,
    },
  };
});

vi.mock('../../api/modelDeployments', async importOriginal => {
  const actual = await importOriginal<typeof import('../../api/modelDeployments')>();
  return {
    ...actual,
    modelDeploymentApi: {
      list: mocks.list,
      get: mocks.get,
      create: mocks.create,
      probe: mocks.probe,
      getCapabilityQualification: mocks.getCapabilityQualification,
    },
  };
});

vi.mock('../../api/modelExecutionProfiles', async importOriginal => {
  const actual = await importOriginal<typeof import('../../api/modelExecutionProfiles')>();
  return {
    ...actual,
    modelExecutionProfileApi: { list: mocks.profileList },
  };
});

vi.mock('../../api/enterpriseFinance', async importOriginal => {
  const actual = await importOriginal<typeof import('../../api/enterpriseFinance')>();
  return {
    ...actual,
    enterpriseFinanceApi: {
      ...actual.enterpriseFinanceApi,
      importOfficialRateCardDraft: mocks.importOfficialRateCardDraft,
      getRateCards: mocks.getRateCards,
    },
  };
});

vi.mock('../ModelGovernancePage', () => ({
  default: ({ embedded }: { embedded?: boolean }) => <div>模型目录嵌入视图 · embedded={String(Boolean(embedded))}</div>,
}));

vi.mock('../ModelReadinessPage', () => ({
  default: ({ embedded, forceConnectionMode }: { embedded?: boolean; forceConnectionMode?: boolean }) => (
    <div>执行诊断嵌入视图 · embedded={String(Boolean(embedded))} · connection={String(Boolean(forceConnectionMode))}</div>
  ),
}));

function application(overrides: Record<string, unknown> = {}) {
  return {
    contract_version: 'unirouter.model-deployment.v1',
    public_model: {
      id: 7,
      model_name: 'deepseek-v4-flash',
      display_name: 'DeepSeek V4 Flash',
      provider_family: 'deepseek',
      lifecycle: 'ga',
      context_length: 128000,
      output_limit: 8192,
      approved_catalog: true,
      execution_profile_id: 'deepseek.v4-flash.api',
      execution_profile_version: '1.1.0',
    },
    deployment: {
      id: 41,
      upstream_model: 'deepseek-v4-flash-202608',
      admin_state: 'enabled',
      config_revision: 4,
      priority: 100,
      routing_weight: 100,
      max_context: 128000,
      default_max_output: 4096,
      degradation_threshold: 10,
      circuit_state: 'healthy',
      circuit_revision: 2,
      fail_count: 0,
    },
    connection: {
      id: 13,
      name: 'DeepSeek official',
      provider: 'deepseek',
      base_url: 'https://api.deepseek.com',
      adapter_type: 'openai_compatible',
      adapter_config: {},
      admin_state: 'enabled',
      config_revision: 5,
    },
    credential: {
      state: 'ready',
      reason_code: 'eligible_credential_bound',
      bound_count: 1,
      eligible_count: 1,
      bindings: [{ id: 91, status: 'active', state_revision: 3, fail_count: 0, eligible: true }],
    },
    operations: [{
      operation_id: 'openai.responses.v1',
      protocol_profile_id: 'openai.responses.text',
      protocol_profile_version: '1.0.0',
      support_state: 'enabled',
      evidence_state: 'declared',
      revision: 2,
      circuit_state: 'closed',
      circuit_revision: 1,
      circuit_fail_count: 0,
      probed_binding_count: 0,
      current_success_count: 0,
      current_failure_count: 0,
      stale_probed_binding_count: 1,
      probe_state: 'stale',
      probe_reason_code: 'binding_probe_revision_stale',
      probe_available: true,
      probe_availability_reason_code: 'exact_probe_contract_available',
      execution_support: {
        operation_id: 'openai.responses.v1',
        public_path: '/v1/responses',
        protocol_profile_id: 'openai.responses.text',
        protocol_profile_version: '1.0.0',
        executor_contract_version: 'responses.executor.v1',
      },
      production_qualification: {
        required: true,
        qualified: false,
        state: 'missing_approval',
      },
      runtime: { state: 'unassigned', reason_code: 'no_runtime_membership' },
    }],
    runtime: { state: 'empty', reason_code: 'runtime_slot_has_no_release', slot_version: 6 },
    callability: { evaluated: false, state: 'not_evaluated', reason_code: 'caller_context_absent' },
    actions: {
      identity_type: 'deployment_id+operation_id',
      required_fields: ['deployment_id', 'operation_id', 'expected_release_version', 'expected_slot_version'],
      lifecycle_writer: 'runtimechange.v2_only',
      license_middleware: 'route_owned',
      rbac_middleware: 'route_owned',
    },
    ...overrides,
  };
}

function executionProfile(overrides: Record<string, unknown> = {}) {
  return {
    id: 'deepseek.v4-flash.api',
    version: '1.5.0',
    label: 'DeepSeek V4 Flash 官方 API',
    current: true,
    tokenizer_profile_id: 'tokenizer.byte-upper.v1',
    tokenizer_profile_version: '1.0.0',
    reasoning_profile: 'reasoning.deepseek-v4-flash-thinking-toggle.v5',
    cache_usage_profile: 'cache-usage.deepseek-v4.v1',
    protocol_profile: 'protocol.deepseek-openai-chat.v4',
    provider: 'deepseek',
    region: 'cn',
    api_surface: 'openai_chat_completions',
    operations: ['openai.chat_completions.v1'],
    endpoint_classes: ['deepseek_official_openai'],
    upstream_model_ids: ['deepseek-v4-flash'],
    default_output_authority: 'unirouter_policy',
    catalog_evidence_state: 'locally_conformant',
    provider_context_window: 1_000_000,
    provider_max_output: 384_000,
    executable_context_window: 1_000_000,
    default_context_window: 1_000_000,
    default_output_limit: 32_768,
    provider_capabilities: ['streaming', 'reasoning', 'tools', 'prompt_cache'],
    capabilities: ['streaming', 'reasoning', 'tools', 'prompt_cache'],
    aliases: ['DeepSeek V4 Flash', 'DeepSeek-V4-Flash', 'deepseek-v4-flash'],
    evidence_refs: ['https://example.test/deepseek-v4-flash'],
    ...overrides,
  };
}

function projection(applications = [application()]) {
  return {
    contract_version: 'unirouter.model-deployment.v1',
    generated_at: '2026-08-20T01:00:00Z',
    applications,
    summary: {
      deployments: applications.length,
      connections_paused: 0,
      credentials_ready: applications.length,
      probes_ready: 0,
      runtime_assigned_operations: 0,
    },
  };
}

const activeLicense = {
  status: 'active',
  enforceable: true,
  features: {
    gateway: true,
    agent_capabilities: true,
    oidc: true,
    guardrails: true,
    notifications: true,
    model_governance: true,
    data_exchange: true,
  },
};

function setRole(role: 'platform_admin' | 'org_admin' | 'org_viewer' | 'member') {
  useAuthStore.setState({
    user: {
      id: 1,
      username: role,
      role,
      organization_role: role,
      license_access: activeLicense,
    },
  });
}

function renderPage(entry = '/channels') {
  return render(<MemoryRouter initialEntries={[entry]}><ModelEndpointsPage /></MemoryRouter>);
}

async function chooseSelectOption(
  label: string,
  option: string | RegExp,
  user = userEvent.setup({ skipHover: true }),
) {
  const combobox = screen.getByRole('combobox', { name: label });
  await user.click(combobox);
  await waitFor(() => expect(combobox).toHaveAttribute('aria-expanded', 'true'));
  const controlledID = combobox.getAttribute('aria-controls');
  if (!controlledID) throw new Error(`missing controlled option list for ${label}`);
  const currentChoice = () => {
    const controlledLists = Array.from(document.querySelectorAll<HTMLElement>('[id]'))
      .filter(node => node.id === controlledID);
    for (let index = controlledLists.length - 1; index >= 0; index -= 1) {
      const candidateChoices = within(controlledLists[index]).queryAllByRole('option');
      const choice = candidateChoices.find(node => {
        const text = node.textContent || '';
        return typeof option === 'string' ? text === option : option.test(text);
      });
      if (choice) return choice;
    }
    return undefined;
  };
  await waitFor(() => expect(currentChoice()).toBeTruthy());
  const choice = currentChoice();
  if (!choice) throw new Error(`missing option ${String(option)} for ${label}`);
  const popup = choice.closest('[role="dialog"]');
  if (!popup) throw new Error(`missing option popup for ${label}`);
  // jsdom does not emit CSS animation events. Exercise the positioned popup,
  // not the timing-dependent pre-positioning path where Semi disables motion.
  await waitFor(() => expect(popup).toHaveClass('semi-tooltip-animation-show'));
  fireEvent.animationEnd(popup);
  await user.click(choice);
  await waitFor(() => expect(popup).toHaveClass('semi-tooltip-animation-hide'));
  fireEvent.animationEnd(popup);
  // Wait for the controlled Form.Select to commit before editing fields whose
  // Input/Select identity changes with the provider template.
  await waitFor(() => expect(combobox).toHaveTextContent(option));
  return choice;
}

describe('ModelEndpointsPage canonical deployment UI', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    Modal.destroyAll();
  });

  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(Date, 'now').mockReturnValue(Date.parse('2026-08-24T12:00:00Z'));
    setRole('org_admin');
    mocks.list.mockResolvedValue(projection());
    mocks.releaseList.mockResolvedValue({
      contract_version: 'unirouter.model-deployment-release-authoring-options.v1',
      generated_at: '2026-08-20T01:00:00Z',
      models: [],
      releases: [],
    });
    mocks.get.mockResolvedValue(application());
    mocks.probe.mockResolvedValue({
      probe_id: 301,
      deployment_id: 41,
      operation_id: 'openai.responses.v1',
      credential_binding_id: 91,
      success: true,
      provider_success: true,
      current: true,
      stale: false,
      operation_advanced: true,
      reason_code: 'probe_current_success',
      provider_reason_code: 'provider_success',
      status_code: 200,
      latency_ms: 21,
    });
    mocks.create.mockResolvedValue({
      application: application(),
      idempotency_replay: false,
      reused: { connection: false, public_model: false, deployment: false, credential: false, operation: false },
    });
    mocks.importOfficialRateCardDraft.mockResolvedValue({
      import_state: 'draft', automatic_activation: false, network_fetch_performed: false,
    });
    mocks.getRateCards.mockResolvedValue({ data: [], total: 0 });
    mocks.getCapabilityQualification.mockImplementation(async (
      deploymentID: number, operationID: string, capabilityID: string,
    ) => ({
      contract_version: 'unirouter.model-capability-qualification.v1',
      deployment_id: deploymentID,
      operation_id: operationID,
      capability_id: capabilityID,
      support_state: 'enabled',
      support_revision: 1,
      execution_profile_id: 'deepseek.v4-flash.api',
      execution_profile_version: '1.5.0',
      protocol_profile_id: 'protocol.deepseek-openai-chat.v4',
      protocol_profile_version: '1.0.0',
      probe_contract_id: capabilityID === 'streaming'
        ? 'unirouter.capability-probe.deepseek-chat-sse.v1'
        : 'unirouter.capability-probe.deepseek-chat-function-tools.v1',
      probe_contract_version: capabilityID === 'streaming' ? '2.0.0' : '1.0.0',
      probe_available: true,
      probe_availability_reason_code: 'typed_probe_available',
      state: 'not_probed',
      qualified: false,
      qualification_state: 'approval_missing',
    }));
    mocks.profileList.mockResolvedValue({
      contract_version: 'unirouter.model-execution-profiles.v1',
      data: [executionProfile()],
    });
  });

  it('derives the OpenAI Responses origin and rejects pathful public operation connections before submit', () => {
    expect(canonicalOperationBaseURLForPreset('openai-responses', 'openai.responses.v1')).toBe('https://api.openai.com');
    expect(canonicalOperationBaseURLForPreset('openai', 'openai.chat_completions.v1')).toBe('https://api.openai.com/v1');
    expect(canonicalOperationOriginError('openai.responses.v1', 'https://api.openai.com')).toBe('');
    expect(canonicalOperationOriginError('openai.responses.v1', 'https://api.openai.com/v1')).toMatch(/path-free HTTPS origin/);
    expect(canonicalOperationOriginError('anthropic.messages.v1', 'https://api.anthropic.com\u00a0')).toMatch(/ASCII/);
    expect(canonicalOperationOriginError('openai.chat_completions.v1', 'https://api.openai.com/v1')).toBe('');
    expect(providerPresetBaseURLError(
      'openai',
      'openai.chat_completions.v1',
      'https://api.openai.com/v1/',
    )).toBe('');
    expect(providerPresetBaseURLError(
      'openai',
      'openai.chat_completions.v1',
      'https://arbitrary.example/v1',
    )).toMatch(/OpenAI 官方模板/);
    expect(providerPresetBaseURLError(
      'openai',
      'openai.chat_completions.v1',
      'https://api.openai.com:443/v1',
    )).toMatch(/OpenAI 官方模板/);
    expect(providerPresetBaseURLError(
      'openai',
      'openai.chat_completions.v1',
      'https://api.openai.com/v1?',
    )).toMatch(/query 或 fragment/);
    expect(providerPresetBaseURLError(
      'openai-responses',
      'openai.responses.v1',
      'https://api.openai.com/v1',
    )).toMatch(/path-free HTTPS origin/);
    expect(providerPresetBaseURLError(
      'openai',
      'openai.responses.v1',
      'https://api.openai.com',
    )).toMatch(/Chat Completions 模板/);
    expect(providerPresetBaseURLError(
      'gemini',
      'openai.chat_completions.v1',
      'https://generativelanguage.googleapis.com/v1beta/openai/',
    )).toBe('');
    expect(providerPresetBaseURLError(
      'gemini',
      'openai.chat_completions.v1',
      'https://arbitrary.example/v1beta/openai',
    )).toMatch(/Gemini 官方模板/);
    expect(providerPresetBaseURLError(
      'gemini',
      'openai.chat_completions.v1',
      'https://generativelanguage.googleapis.com/v1beta/openai#',
    )).toMatch(/query 或 fragment/);
    expect(providerPresetBaseURLError(
      'deepseek',
      'openai.chat_completions.v1',
      'https://api.deepseek.com/',
    )).toBe('');
    expect(providerPresetBaseURLError(
      'deepseek',
      'openai.chat_completions.v1',
      'https://arbitrary.example',
    )).toMatch(/DeepSeek 官方模板/);
    expect(providerPresetBaseURLError(
      'deepseek',
      'openai.chat_completions.v1',
      'https://api.deepseek.com/beta',
    )).toMatch(/DeepSeek 官方模板/);
    expect(providerPresetBaseURLError(
      'deepseek',
      'openai.chat_completions.v1',
      'https://api.deepseek.com?',
    )).toMatch(/query 或 fragment/);
    expect(providerPresetBaseURLError(
      'kimi',
      'openai.chat_completions.v1',
      'https://api.moonshot.cn/v1/',
    )).toBe('');
    for (const endpoint of [
      'https://api.kimi.com/coding/v1',
      'https://arbitrary.example/v1',
      'https://api.moonshot.cn/v1/chat/completions',
      'https://api.moonshot.cn:443/v1',
      'https://api.moonshot.cn/v1?',
    ]) {
      expect(providerPresetBaseURLError(
        'kimi',
        'openai.chat_completions.v1',
        endpoint,
      )).toBeTruthy();
    }
    expect(providerPresetBaseURLError(
      'minimax',
      'openai.chat_completions.v1',
      'https://api.minimaxi.com/v1/',
    )).toBe('');
    for (const endpoint of [
      'https://arbitrary.example/v1',
      'https://api.minimaxi.com',
      'https://api.minimaxi.com/v1/chat/completions',
      'https://api.minimaxi.com:443/v1',
      'https://api.minimaxi.com/v1?',
    ]) {
      expect(providerPresetBaseURLError(
        'minimax',
        'openai.chat_completions.v1',
        endpoint,
      )).toBeTruthy();
    }
    expect(providerPresetBaseURLError(
      'xai',
      'openai.chat_completions.v1',
      'https://api.x.ai/v1/',
    )).toBe('');
    for (const endpoint of [
      'https://arbitrary.example/v1',
      'https://api.x.ai',
      'https://api.x.ai/v1/chat/completions',
      'https://api.x.ai:443/v1',
      'https://api.x.ai/v1?',
    ]) {
      expect(providerPresetBaseURLError(
        'xai',
        'openai.chat_completions.v1',
        endpoint,
      )).toBeTruthy();
    }
    expect(providerPresetBaseURLError(
      'dashscope',
      'openai.chat_completions.v1',
      'https://workspace.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1',
    )).toMatch(/独立 Profile 与价格证据/);
    expect(providerPresetBaseURLError(
      'dashscope',
      'openai.chat_completions.v1',
      'https://workspace.cn-beijing.maas.aliyuncs.com/compatible-mode/v1',
    )).toBe('');
    for (const endpoint of [
      'https://trial.cn-beijing.maas.aliyuncs.com/compatible-mode/v1',
      'https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1',
      'https://nested.workspace.cn-beijing.maas.aliyuncs.com/compatible-mode/v1',
    ]) {
      expect(providerPresetBaseURLError('dashscope', 'openai.chat_completions.v1', endpoint)).toBeTruthy();
    }
    expect(providerPresetBaseURLError(
      'dashscope-shared',
      'openai.chat_completions.v1',
      'https://dashscope.aliyuncs.com/compatible-mode/v1',
    )).toBe('');
    expect(providerPresetBaseURLError(
      'dashscope-shared',
      'openai.chat_completions.v1',
      'https://trial.cn-beijing.maas.aliyuncs.com/compatible-mode/v1',
    )).toBeTruthy();
    expect(providerPresetBaseURLError(
      'dashscope-shared',
      'openai.chat_completions.v1',
      'https://dashscope.aliyuncs.com/compatible-mode/v1?',
    )).toMatch(/query 或 fragment/);
  });

  it('keeps the custom provider template selected when the execution Profile catalog is unavailable', async () => {
    const user = userEvent.setup({ skipHover: true });
    mocks.profileList.mockRejectedValueOnce(new Error('profile catalog unavailable'));
    renderPage();

    const createButton = (await screen.findByText('接入模型')).closest('button');
    expect(createButton).toBeTruthy();
    await user.click(createButton!);
    expect(await screen.findByText(/无法加载版本化执行 Profile/)).toBeTruthy();
    await waitFor(() => expect(screen.getByRole('combobox', { name: '接入模板' })).not.toBeDisabled());

    await chooseSelectOption('接入模板', '自定义 OpenAI 兼容供应商', user);

    await waitFor(() => expect(screen.getByRole('combobox', { name: '接入模板' })).toHaveTextContent('自定义 OpenAI 兼容供应商'));
    expect(screen.getByLabelText('供应商标识')).not.toBeDisabled();
    expect(screen.getByLabelText('Base URL')).not.toBeDisabled();
  });


  it('projects the Gemini 3.7 processing-tier boundary without claiming billing or release', async () => {
		mocks.profileList.mockResolvedValueOnce({
			contract_version: 'unirouter.model-execution-profiles.v1',
			data: [executionProfile({
				id: 'gemini.3.7-flash.api',
				version: '1.7.0',
				label: 'Gemini 3.7 Flash OpenAI-compatible Core Chat 子集',
				provider: 'gemini',
				region: 'global',
				protocol_profile: 'protocol.gemini-openai-chat.v6',
				processing_tier_policy: 'processing-tier.gemini-openai-compatibility.v1',
				endpoint_classes: ['gemini_openai_compatibility_v1beta'],
				upstream_model_ids: ['gemini-3.7-flash'],
				provider_context_window: 1_048_576,
				provider_max_output: 65_536,
				default_context_window: 1_048_576,
				default_output_limit: 32_768,
				provider_capabilities: ['streaming', 'service_tier'],
				capabilities: ['streaming', 'service_tier'],
				aliases: ['Gemini 3.7 Flash', 'gemini-3.7-flash'],
			}),
		],
	});
	const user = userEvent.setup({ skipHover: true, delay: null });
	renderPage();

	await user.click((await screen.findByText('接入模型')).closest('button')!);
	await chooseSelectOption('接入模板', 'Google Gemini 官方', user);

	expect(await screen.findByText(/processing-tier\.gemini-openai-compatibility\.v1/)).toBeTruthy();
	expect(screen.getByText(/显式 flex\/priority 目前只允许 cost_observation，不代表客户计费、live verified 或 released/)).toBeTruthy();
  });

  it('flattens the list by exact deployment_id and operation_id and reads detail through the exact route', async () => {
    renderPage();

    expect(await screen.findByText('DeepSeek V4 Flash')).toBeTruthy();
    expect(screen.getByText('deployment_id=41')).toBeTruthy();
    expect(screen.getByText('openai.responses.v1')).toBeTruthy();
    expect(screen.getByText('/v1/responses')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '查看详情' }));

    await waitFor(() => expect(mocks.get).toHaveBeenCalledWith(41, 'openai.responses.v1'));
    expect(await screen.findByText('精确持久身份')).toBeTruthy();
    expect(screen.getAllByText('deployment_id=41').length).toBeGreaterThan(1);
    expect(screen.getAllByText('operation_id=openai.responses.v1').length).toBeGreaterThan(0);
  });

  it('preserves an exact deployment operation deep link without falling back by model or connection', async () => {
    renderPage('/channels?deployment_id=41&operation_id=openai.responses.v1');

    await waitFor(() => expect(mocks.get).toHaveBeenCalledWith(41, 'openai.responses.v1'));
    expect(await screen.findByText('1 · 配置与管理状态')).toBeTruthy();
    expect(screen.queryByText(/旧 channel_id\/public_model 深链/)).toBeNull();
  });

  it('honors the platform catalog redirect and does not load canonical deployment facts behind it', async () => {
    setRole('platform_admin');
    renderPage('/channels?tab=advanced&view=catalog');

    expect(await screen.findByText('模型目录嵌入视图 · embedded=true')).toBeTruthy();
    expect(screen.getByRole('heading', { name: '公共模型目录' })).toBeTruthy();
    expect(mocks.list).not.toHaveBeenCalled();
    expect(mocks.releaseList).not.toHaveBeenCalled();
  });

  it('honors the platform diagnostics redirect and keeps the exact connection-oriented view', async () => {
    setRole('platform_admin');
    renderPage('/channels?tab=advanced&view=diagnostics');

    expect(await screen.findByText('执行诊断嵌入视图 · embedded=true · connection=true')).toBeTruthy();
    expect(screen.getByRole('heading', { name: '执行诊断' })).toBeTruthy();
    expect(mocks.list).not.toHaveBeenCalled();
    expect(mocks.releaseList).not.toHaveBeenCalled();
  });

  it('refuses to infer a canonical identity from a legacy channel deep link', async () => {
    renderPage('/channels?channel_id=13&model=deepseek-v4-flash');

    expect(await screen.findByText(/旧 channel_id\/public_model 深链不是 canonical deployment 身份/)).toBeTruthy();
    expect(mocks.get).not.toHaveBeenCalled();
  });

  it('renders five independent axes and never turns probe, runtime or caller state into one ready flag', async () => {
    renderPage('/channels?deployment_id=41&operation_id=openai.responses.v1');

    expect(await screen.findByText('1 · 配置与管理状态')).toBeTruthy();
    expect(screen.getByText('2 · 凭证状态')).toBeTruthy();
    expect(screen.getByText('3 · Operation 支持与探测证据')).toBeTruthy();
    expect(screen.getByText('4 · Canonical Runtime')).toBeTruthy();
    expect(screen.getByText('5 · 调用者可调用性')).toBeTruthy();
    expect(screen.getByText(/binding_probe_revision_stale/)).toBeTruthy();
    expect(screen.getByText(/exact_probe_contract_available/)).toBeTruthy();
    expect(screen.getByText(/responses\.executor\.v1/)).toBeTruthy();
    expect(screen.getByText(/missing_approval/)).toBeTruthy();
    expect(screen.getByText(/未分配到任何 canonical runtime release/)).toBeTruthy();
    expect(screen.getByText(/未针对调用者评估/)).toBeTruthy();
    expect(screen.getAllByText(/^ready$/i)).toHaveLength(1);
  });

  it('renders the exact capability qualification workflow for the current DeepSeek Flash profile', async () => {
    const base = application();
    const chat = application({
      public_model: {
        ...base.public_model,
        execution_profile_version: '1.5.0',
      },
      operations: [{
        ...base.operations[0],
        operation_id: 'openai.chat_completions.v1',
        protocol_profile_id: 'protocol.deepseek-openai-chat.v4',
        protocol_profile_version: '1.0.0',
        execution_support: {
          operation_id: 'openai.chat_completions.v1',
          public_path: '/v1/chat/completions',
          protocol_profile_id: 'protocol.deepseek-openai-chat.v4',
          protocol_profile_version: '1.0.0',
          executor_contract_version: 'chat-completions.executor.v1',
        },
      }],
    });
    mocks.list.mockResolvedValue(projection([chat]));
    mocks.get.mockResolvedValue(chat);

    renderPage('/channels?deployment_id=41&operation_id=openai.chat_completions.v1');

    expect(await screen.findByText('能力验证 · 流式输出（SSE）')).toBeTruthy();
    expect(screen.getByText('能力验证 · Named tool_choice')).toBeTruthy();
    expect(screen.getByText('能力验证 · JSON Object')).toBeTruthy();
    await waitFor(() => expect(mocks.getCapabilityQualification).toHaveBeenCalledTimes(9));
    expect(mocks.getCapabilityQualification).toHaveBeenCalledWith(
      41, 'openai.chat_completions.v1', 'streaming',
    );
  });

  it('renders the same exact capability qualification workflow for DeepSeek Pro 1.5', async () => {
    const base = application();
    const pro = application({
      public_model: {
        ...base.public_model,
        model_name: 'deepseek-v4-pro',
        display_name: 'DeepSeek V4 Pro',
        execution_profile_id: 'deepseek.v4-pro.api',
        execution_profile_version: '1.5.0',
      },
      operations: [{
        ...base.operations[0],
        operation_id: 'openai.chat_completions.v1',
        protocol_profile_id: 'protocol.deepseek-openai-chat.v4',
        protocol_profile_version: '1.0.0',
      }],
    });
    mocks.list.mockResolvedValue(projection([pro]));
    mocks.get.mockResolvedValue(pro);

    renderPage('/channels?deployment_id=41&operation_id=openai.chat_completions.v1');

    expect(await screen.findByText('能力验证 · 流式输出（SSE）')).toBeTruthy();
    expect(screen.getByText('能力验证 · 推理强度')).toBeTruthy();
    expect(screen.getByText('能力验证 · JSON Object')).toBeTruthy();
    await waitFor(() => expect(mocks.getCapabilityQualification).toHaveBeenCalledTimes(9));
  });

  it('does not offer Pro capability qualification for the unbound generic Chat tuple', async () => {
    const base = application();
    const genericPro = application({
      public_model: {
        ...base.public_model,
        model_name: 'deepseek-v4-pro',
        display_name: 'DeepSeek V4 Pro',
        execution_profile_id: 'deepseek.v4-pro.api',
        execution_profile_version: '1.5.0',
      },
      operations: [{
        ...base.operations[0],
        operation_id: 'openai.chat_completions.v1',
        protocol_profile_id: 'openai.chat_completions',
        protocol_profile_version: '1.0.0',
      }],
    });
    mocks.list.mockResolvedValue(projection([genericPro]));
    mocks.get.mockResolvedValue(genericPro);

    renderPage('/channels?deployment_id=41&operation_id=openai.chat_completions.v1');

    expect(await screen.findByText('3 · Operation 支持与探测证据')).toBeTruthy();
    expect(screen.queryByText('能力验证 · 流式输出（SSE）')).toBeNull();
    expect(mocks.getCapabilityQualification).not.toHaveBeenCalled();
  });

  it('links exact runtime membership from deployment detail into the canonical release workspace', async () => {
    const base = application();
    const withStableMembership = application({
      operations: [{
        ...base.operations[0],
        runtime: {
          state: 'stable',
          reason_code: 'assigned_to_stable_release',
          slot_version: 8,
          stable_membership: {
            release_ref: 'rcr_v2-candidate',
            release_stage: 'active',
            release_version: 3,
            route_items_hash: 'sha256:routes',
            ordinal: 1,
            admission: 'allow',
            expected_connection_revision: 5,
            expected_deployment_revision: 4,
            expected_operation_revision: 2,
            protocol_profile_id: 'openai.responses.text',
            protocol_profile_version: '1.0.0',
          },
        },
      }],
    });
    mocks.list.mockResolvedValue(projection([withStableMembership]));
    mocks.get.mockResolvedValue(withStableMembership);
    renderPage('/channels?deployment_id=41&operation_id=openai.responses.v1');

    const button = await screen.findByRole('button', { name: '查看 Stable Release' });
    fireEvent.click(button);

    await waitFor(() => expect(screen.queryByText('1 · 配置与管理状态')).toBeNull());
    expect(screen.getByText('Canonical Release Workspace')).toBeTruthy();
  });

  it('keeps connection and deployment administration state distinct in the list configuration axis', async () => {
    const base = application();
    mocks.list.mockResolvedValue(projection([application({
      deployment: { ...base.deployment, admin_state: 'paused' },
    })]));

    renderPage();

    expect(await screen.findByText('连接 enabled')).toBeTruthy();
    expect(screen.getByText('Deployment paused')).toBeTruthy();
    expect(screen.queryByText('Deployment enabled')).toBeNull();
  });

  it('probes only the selected deployment operation and exact credential binding', async () => {
    let confirmationConfig: any;
    vi.spyOn(Modal, 'confirm').mockImplementation((config: any) => {
      confirmationConfig = config;
      return { destroy: vi.fn(), update: vi.fn() } as any;
    });
    renderPage('/channels?deployment_id=41&operation_id=openai.responses.v1');

    const button = await screen.findByRole('button', { name: '用此绑定探测精确 Operation' });
    fireEvent.click(button);
    expect(confirmationConfig.title).toBe('探测 Deployment #41？');
    expect(confirmationConfig.okText).toBe('发起精确探测');
    await act(async () => { await confirmationConfig.onOk(); });

    await waitFor(() => expect(mocks.probe).toHaveBeenCalledWith(41, 'openai.responses.v1', {
      credential_binding_id: 91,
    }));
  });

  it('keeps org_viewer on canonical list/detail while hiding create, probe and credential binding identity', async () => {
    setRole('org_viewer');
    renderPage('/channels?deployment_id=41&operation_id=openai.responses.v1');

    expect(await screen.findByText(/当前为组织只读视图/)).toBeTruthy();
    expect(await screen.findByText('1 · 配置与管理状态')).toBeTruthy();
    expect(screen.queryByRole('button', { name: '接入模型' })).toBeNull();
    expect(screen.queryByRole('button', { name: '用此绑定探测精确 Operation' })).toBeNull();
    expect(screen.queryByText('binding_id=91')).toBeNull();
    expect(screen.getByText(/只读角色仅查看聚合凭证状态/)).toBeTruthy();
  });

  it('does not fetch or reveal provider and credential state to an ordinary project identity', async () => {
    setRole('member');
    renderPage();

    expect(await screen.findByText('当前身份无权读取供应商部署')).toBeTruthy();
    expect(screen.queryByText('DeepSeek official')).toBeNull();
    expect(screen.queryByText('binding_id=91')).toBeNull();
    expect(mocks.list).not.toHaveBeenCalled();
    expect(mocks.get).not.toHaveBeenCalled();
    expect(mocks.releaseList).not.toHaveBeenCalled();
  });

  it('mounts the canonical release workspace and exposes no old inferred release action', async () => {
    renderPage('/channels?deployment_id=41&operation_id=openai.responses.v1');

    expect(await screen.findByText('Canonical Release Workspace')).toBeTruthy();
    expect(mocks.releaseList).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('button', { name: /首次启用|灰度|提升为稳定|回滚/ })).toBeNull();
  });

  it('disables a zero-limit existing public model and rejects a stale selection after its limits regress', async () => {
    const current = application();
    const toast = vi.spyOn(Toast, 'error');
    const user = userEvent.setup({ skipHover: true, delay: null });
    renderPage();

    await user.click((await screen.findByText('接入模型')).closest('button')!);
    await user.click(screen.getByText('复用现有公共模型', { exact: true }));
    await chooseSelectOption('现有公共模型', /DeepSeek V4 Flash/, user);

    mocks.list.mockResolvedValue(projection([application({
      public_model: { ...current.public_model, context_length: 0, output_limit: 0 },
    })]));
    await user.click(screen.getByRole('button', { name: /刷新$/ }));
    await waitFor(() => expect(mocks.list).toHaveBeenCalledTimes(2));

    await user.click(screen.getByRole('combobox', { name: '现有公共模型' }));
    const unavailable = await screen.findByRole('option', { name: /缺少 context\/output 执行边界/ });
    expect(unavailable).toHaveAttribute('aria-disabled', 'true');
    expect(screen.getByText(/请先在公共模型目录修复执行边界/)).toBeTruthy();
    await user.click(screen.getByRole('combobox', { name: '现有公共模型' }));

    await user.type(screen.getByLabelText('连接名称'), 'DeepSeek second deployment');
    await user.type(screen.getByLabelText('上游模型 / 部署 ID'), 'deepseek-v4-flash-second');
    fireEvent.change(screen.getByLabelText('Deployment 上下文上限（Token）'), { target: { value: '128000' } });
    fireEvent.change(screen.getByLabelText('Deployment 默认单次输出（Token）'), { target: { value: '4096' } });
    await user.type(screen.getByLabelText('供应商 API Key'), 'sk-test-only');
    await user.click(screen.getByRole('button', { name: '创建模型接入' }));

    await waitFor(() => expect(toast).toHaveBeenCalledWith(expect.stringContaining('请先在公共模型目录修复后再复用')));
    expect(mocks.create).not.toHaveBeenCalled();
  });

  it('hard-blocks a pathful existing connection for Responses in both the rendered button and submit guard', async () => {
    const current = application();
    const pathful = application({
      connection: {
        ...current.connection,
        id: 29,
        name: 'Pathful OpenAI connection',
        provider: 'openai',
        base_url: 'https://api.openai.com/v1',
      },
    });
    mocks.list.mockResolvedValue(projection([pathful]));
    const toast = vi.spyOn(Toast, 'error');
    const user = userEvent.setup({ skipHover: true, delay: null });
    renderPage();

    await user.click((await screen.findByText('接入模型')).closest('button')!);
    await user.click(screen.getByText('复用现有连接', { exact: true }));
    await chooseSelectOption('现有连接', /Pathful OpenAI connection/, user);
    await chooseSelectOption('Operation', /Responses · POST \/v1\/responses/, user);

    expect(await screen.findByText(/该 operation 要求 path-free HTTPS origin/)).toBeTruthy();
    await user.type(screen.getByLabelText('公共模型 ID'), 'pathful-response-model');
    fireEvent.change(screen.getByLabelText('公共模型上下文上限（Token）'), { target: { value: '128000' } });
    fireEvent.change(screen.getByLabelText('公共模型单次输出上限（Token）'), { target: { value: '8192' } });
    await user.type(screen.getByLabelText('上游模型 / 部署 ID'), 'response-upstream');
    fireEvent.change(screen.getByLabelText('Deployment 上下文上限（Token）'), { target: { value: '128000' } });
    fireEvent.change(screen.getByLabelText('Deployment 默认单次输出（Token）'), { target: { value: '4096' } });
    await user.type(screen.getByLabelText('供应商 API Key'), 'sk-test-only');

    const createButton = screen.getByRole('button', { name: '创建模型接入' });
    expect(createButton).toBeDisabled();
    await user.click(createButton);
    expect(mocks.create).not.toHaveBeenCalled();

    await act(async () => {
      fireEvent.submit(createButton.closest('form')!);
      await Promise.resolve();
    });
    await waitFor(() => expect(toast).toHaveBeenCalledWith(expect.stringContaining('path-free HTTPS origin')));
    expect(mocks.create).not.toHaveBeenCalled();
  });

  it('downgrades a malformed DeepSeek existing connection to custom instead of offering a reviewed profile', async () => {
    const current = application();
    mocks.list.mockResolvedValue(projection([application({
      connection: {
        ...current.connection,
        adapter_config: { endpoint_path_mode: 'base_relative', unknown: true },
      },
    })]));
    const user = userEvent.setup({ skipHover: true, delay: null });
    renderPage();

    await user.click((await screen.findByText('接入模型')).closest('button')!);
    await user.click(screen.getByText('复用现有连接', { exact: true }));
    await chooseSelectOption('现有连接', /DeepSeek official/, user);

    await waitFor(() => expect(screen.queryByLabelText('模型模板')).toBeNull());
    expect(screen.getByLabelText('公共模型 ID')).not.toBeDisabled();
  });

  it('downgrades a Gemini existing connection with the default path mode to custom', async () => {
    const current = application();
    mocks.list.mockResolvedValue(projection([application({
      connection: {
        ...current.connection,
        name: 'Gemini malformed path mode',
        provider: 'gemini',
        base_url: 'https://generativelanguage.googleapis.com/v1beta/openai',
        adapter_config: {},
      },
    })]));
    mocks.profileList.mockResolvedValueOnce({
      contract_version: 'unirouter.model-execution-profiles.v1',
      data: [executionProfile({
        id: 'gemini.3.7-flash.api',
        version: '1.6.0',
        label: 'Gemini 3.7 Flash OpenAI-compatible Core Chat 子集',
        provider: 'gemini',
        region: 'global',
        endpoint_classes: ['gemini_openai_compatibility_v1beta'],
        upstream_model_ids: ['gemini-3.7-flash'],
        aliases: ['Gemini 3.7 Flash', 'gemini-3.7-flash'],
      })],
    });
    const user = userEvent.setup({ skipHover: true, delay: null });
    renderPage();

    await user.click((await screen.findByText('接入模型')).closest('button')!);
    await user.click(screen.getByText('复用现有连接', { exact: true }));
    await chooseSelectOption('现有连接', /Gemini malformed path mode/, user);

    await waitFor(() => expect(screen.queryByLabelText('模型模板')).toBeNull());
    expect(screen.getByLabelText('公共模型 ID')).not.toBeDisabled();
  });

  it('downgrades a Kimi Code subscription connection instead of offering platform profiles', async () => {
    const current = application();
    mocks.list.mockResolvedValue(projection([application({
      connection: {
        ...current.connection,
        name: 'Kimi Code subscription',
        provider: 'kimi',
        base_url: 'https://api.kimi.com/coding/v1',
        adapter_config: { endpoint_path_mode: 'canonical_v1' },
      },
    })]));
    mocks.profileList.mockResolvedValueOnce({
      contract_version: 'unirouter.model-execution-profiles.v1',
      data: [executionProfile({
        id: 'kimi.k3.api',
        version: '1.6.0',
        label: 'Kimi K3 官方 API',
        provider: 'kimi',
        region: 'cn',
        endpoint_classes: ['kimi_platform_openai_v1'],
        upstream_model_ids: ['kimi-k3'],
        aliases: ['Kimi K3', 'kimi-k3'],
      })],
    });
    const user = userEvent.setup({ skipHover: true, delay: null });
    renderPage();

    await user.click((await screen.findByText('接入模型')).closest('button')!);
    await user.click(screen.getByText('复用现有连接', { exact: true }));
    await chooseSelectOption('现有连接', /Kimi Code subscription/, user);

    await waitFor(() => expect(screen.queryByLabelText('模型模板')).toBeNull());
    expect(screen.getByLabelText('公共模型 ID')).not.toBeDisabled();
  });

  it('creates through the canonical transaction with one retained idempotency key for an uncertain retry', async () => {
    const user = userEvent.setup({ skipHover: true });
    mocks.create
      .mockRejectedValueOnce(new TypeError('network unavailable'))
      .mockResolvedValueOnce({
        application: application(),
        idempotency_replay: true,
        reused: { connection: true, public_model: true, deployment: true, credential: true, operation: true },
      });
    renderPage();

    const createButton = (await screen.findByText('接入模型')).closest('button');
    expect(createButton).toBeTruthy();
    await user.click(createButton!);
    await screen.findByText(/deepseek\.v4-flash\.api@1\.5\.0/);
    await chooseSelectOption('接入模板', '自定义 OpenAI 兼容供应商', user);
    fireEvent.change(await screen.findByLabelText('连接名称'), { target: { value: 'DeepSeek new production' } });
    fireEvent.change(await screen.findByLabelText('供应商标识'), { target: { value: 'deepseek' } });
    fireEvent.change(await screen.findByLabelText('Base URL'), { target: { value: 'https://api.deepseek.com' } });
    fireEvent.change(await screen.findByLabelText('公共模型 ID'), { target: { value: 'deepseek-v5' } });
    fireEvent.change(await screen.findByLabelText('公共模型上下文上限（Token）'), { target: { value: '128000' } });
    fireEvent.change(await screen.findByLabelText('公共模型单次输出上限（Token）'), { target: { value: '8192' } });
    fireEvent.change(await screen.findByLabelText('上游模型 / 部署 ID'), { target: { value: 'deepseek-v5-202608' } });
    fireEvent.change(await screen.findByLabelText('Deployment 上下文上限（Token）'), { target: { value: '128000' } });
    fireEvent.change(await screen.findByLabelText('Deployment 默认单次输出（Token）'), { target: { value: '4096' } });
    await pasteValue(user, await screen.findByLabelText('供应商 API Key'), 'sk-test-only');
    await user.click(screen.getByRole('button', { name: '创建模型接入' }));

    await waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(1));
    await user.click(screen.getByRole('button', { name: '创建模型接入' }));
    await waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(2));

    const firstKey = mocks.create.mock.calls[0][1];
    const secondKey = mocks.create.mock.calls[1][1];
    expect(firstKey).toMatch(/^ui-model-deployment-/);
    expect(secondKey).toBe(firstKey);
    expect(mocks.create.mock.calls[0][0]).toEqual(expect.objectContaining({
      public_model: expect.objectContaining({
        model_name: 'deepseek-v5',
        context_length: 128000,
        output_limit: 8192,
      }),
      deployment: expect.objectContaining({
        upstream_model: 'deepseek-v5-202608',
        max_context: 128000,
        default_max_output: 4096,
      }),
      operation: { operation_id: 'openai.chat_completions.v1' },
    }));
    await waitFor(() => expect(mocks.get).toHaveBeenCalledWith(41, 'openai.responses.v1'));
  }, 20_000);

  it('composes an official provider preset with the latest reviewed model profile and pins it on create', async () => {
    const user = userEvent.setup({ skipHover: true, delay: null });
    renderPage();

    await user.click((await screen.findByText('接入模型')).closest('button')!);
    expect(await screen.findByText(/deepseek\.v4-flash\.api@1\.5\.0/)).toBeTruthy();
    expect(screen.getByText(/deepseek \/ cn \/ openai_chat_completions/)).toBeTruthy();
    expect(screen.getByText(/Catalog 证据 locally_conformant（不代表 live\/released）/)).toBeTruthy();
    expect(screen.getByText(/这里只展示本地合同子集，不代表该模型已完整适配/)).toBeTruthy();
    expect(screen.getByText(/供应商上限 context 1,000,000 \/ output 384,000/)).toBeTruthy();
    expect(screen.getByText(/请求默认 context 1,000,000 \/ output 32,768/)).toBeTruthy();
    expect(screen.getByText(/官方价格已记录（cn \/ CNY）/)).toBeTruthy();
    expect(screen.queryByLabelText('同时创建待审核 Pricing Profile 草稿')).toBeNull();
    expect(screen.getByLabelText('供应商标识')).toBeDisabled();
    expect(screen.getByLabelText('Base URL')).toBeDisabled();
    expect(screen.getByLabelText('公共模型 ID')).not.toBeDisabled();
    expect(screen.getByLabelText('公共模型上下文上限（Token）')).toBeDisabled();
    expect(screen.getByLabelText('请求默认单次输出（Profile 投影，Token）')).toBeDisabled();

    await act(async () => {
      fireEvent.change(screen.getByLabelText('公共模型 ID'), { target: { value: 'deepseek-v4-flash-20260828' } });
    });

    await pasteValue(user, screen.getByLabelText('供应商 API Key'), 'sk-test-only');
    await user.click(screen.getByRole('button', { name: '创建模型接入' }));

    await waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(1));
    expect(mocks.create.mock.calls[0][0]).toEqual(expect.objectContaining({
      connection: expect.objectContaining({
        provider: 'deepseek', base_url: 'https://api.deepseek.com',
        adapter_config: { endpoint_path_mode: 'base_relative' },
      }),
      public_model: expect.objectContaining({
        model_name: 'deepseek-v4-flash-20260828',
        context_length: 1_000_000,
        output_limit: 384_000,
        execution_profile_id: 'deepseek.v4-flash.api',
        execution_profile_version: '1.5.0',
      }),
      deployment: expect.objectContaining({
        upstream_model: 'deepseek-v4-flash',
        max_context: 1_000_000,
        default_max_output: 32_768,
      }),
      operation: { operation_id: 'openai.chat_completions.v1' },
    }));
    expect(mocks.importOfficialRateCardDraft).not.toHaveBeenCalled();
  });

  it('requires an explicit Beijing workspace Host for the recommended Qwen preset', async () => {
    const user = userEvent.setup({ skipHover: true, delay: null });
    mocks.profileList.mockResolvedValueOnce({
      contract_version: 'unirouter.model-execution-profiles.v1',
      data: [executionProfile({
        id: 'qwen.3.8-max.api',
        version: '1.7.0',
        label: 'Qwen3.8-Max 官方 API',
        provider: 'dashscope',
        region: 'cn-beijing',
        endpoint_classes: ['dashscope_cn_beijing_workspace_payg', 'dashscope_cn_beijing_shared_payg'],
        upstream_model_ids: ['qwen3.8-max'],
        protocol_profile: 'opaque.profile.name.without.operation.hints',
        default_output_authority: 'provider_documented',
        provider_context_window: 1_000_000,
        provider_max_output: 131_072,
        default_context_window: 1_000_000,
        default_output_limit: 131_072,
        aliases: ['Qwen3.8-Max', 'qwen3.8-max'],
      })],
    });
    renderPage();

    await user.click((await screen.findByText('接入模型')).closest('button')!);
    await chooseSelectOption('接入模板', '阿里云百炼（北京业务空间）', user);

    expect(await screen.findByText(/qwen\.3\.8-max\.api@1\.7\.0/)).toBeTruthy();
    expect(screen.getByText(/dashscope \/ cn-beijing \/ openai_chat_completions/)).toBeTruthy();
    expect(screen.getAllByText(/provider_documented/)).toHaveLength(2);
    expect(screen.getByText(/官方价格已记录（cn-beijing \/ CNY）/)).toBeTruthy();
    expect(screen.getByLabelText('Base URL')).not.toBeDisabled();
    expect(screen.getByLabelText('Base URL')).toHaveValue('');
    await pasteValue(
      user,
      screen.getByLabelText('Base URL'),
      'https://workspace.cn-beijing.maas.aliyuncs.com/compatible-mode/v1',
    );
    expect(screen.getByLabelText('Base URL')).toHaveValue('https://workspace.cn-beijing.maas.aliyuncs.com/compatible-mode/v1');
  });

  it('treats an exact official pricing draft readback as an idempotent replay after an uncertain import failure', async () => {
    const user = userEvent.setup({ skipHover: true, delay: null });
    mocks.importOfficialRateCardDraft.mockRejectedValueOnce(new TypeError('network unavailable'));
    mocks.profileList.mockResolvedValueOnce({
      contract_version: 'unirouter.model-execution-profiles.v1',
      data: [executionProfile({
        id: 'mimo.v2.5.api',
        version: '1.6.0',
        label: 'MiMo V2.5 官方 API',
        provider: 'mimo',
        endpoint_classes: ['provider_https'],
        protocol_profile: 'protocol.mimo-openai-chat.v4',
        provider_context_window: 1_000_000,
        provider_max_output: 131_072,
        default_context_window: 1_000_000,
        default_output_limit: 32_768,
        aliases: ['MiMo V2.5', 'mimo-v2.5'],
        upstream_model_ids: ['mimo-v2.5'],
      })],
    });
    mocks.getRateCards.mockResolvedValueOnce({
      total: 1,
      data: [{
        id: 1,
        card: {
          rate_card_ref: 'onboarding-rate-mimo-cn-mimo-v2.5-retrieved-2026-08-24-official-onboarding-rate.v1-2e9d779a1629',
          provider_ref: 'mimo', model_ref: 'mimo-v2.5', layer: 'provider_list',
          version: 1, state: 'draft', effective_from: '2026-08-24T08:00:00Z', created_by: 'user:1',
          components: [
            ['input_tokens', '1000000'], ['output_tokens', '2000000'],
            ['cache_read_tokens', '20000'], ['cache_creation_tokens', '0'],
          ].map(([meter, value_minor]) => ({
            meter, unit_quantity: '1000000', amount: { value_minor, currency: 'CNY', scale: 1_000_000 },
          })),
          daily_price_bands: [],
        },
        source_evidence: {
          evidence_ref: 'onboarding-source-mimo-cn-mimo-v2.5-retrieved-2026-08-24-official-onboarding-rate.v1-2e9d779a1629',
          source_kind: 'official_import', source_url: 'https://mimo.mi.com/docs/en-US/price/pay-as-you-go',
          source_revision: 'retrieved-2026-08-24', fetched_at: '2026-08-24T08:00:00Z',
          payload_sha256: 'sha256:2e9d779a162942b1b8afb359e4242084564b65f1a02bfc914ee91b24aae398fe',
          transform_version: 'official-onboarding-rate.v1', last_success_at: '2026-08-24T08:00:00Z',
          stale_after: '2026-08-31T08:00:00Z',
        },
      }],
    });
    renderPage();

    await user.click((await screen.findByText('接入模型')).closest('button')!);
    await chooseSelectOption('接入模板', '小米 MiMo 官方', user);
    await screen.findByText(/^待审核 Pricing Profile 草稿 ·/);
    await user.click(screen.getByLabelText('同时创建待审核 Pricing Profile 草稿'));
    await pasteValue(user, screen.getByLabelText('供应商 API Key'), 'sk-test-only');
    await user.click(screen.getByRole('button', { name: '创建模型接入' }));

    await waitFor(() => expect(mocks.getRateCards).toHaveBeenCalledTimes(1));
    expect(mocks.importOfficialRateCardDraft).toHaveBeenCalledTimes(1);
    expect(mocks.create).toHaveBeenCalledTimes(1);
  });
});
