import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { Toast } from '@douyinfe/semi-ui';
import ModelGovernancePage, { comparableModelIdentity, formatModelSetItems, parseModelSetItems } from '../ModelGovernancePage';

const mockGetModelRegistry = vi.fn();
const mockGetModelExecutionProfiles = vi.fn();
const mockCreateModelRegistry = vi.fn();
const mockUpdateModelRegistry = vi.fn();
const mockGetModelAliases = vi.fn();
const mockCreateModelAlias = vi.fn();
const mockUpdateModelAlias = vi.fn();
const mockGetModelSets = vi.fn();
const mockCreateModelSet = vi.fn();
const mockGetModelSetItems = vi.fn();
const mockReplaceModelSetItems = vi.fn();
const mockGetProjects = vi.fn();
const mockGetProjectModelSets = vi.fn();
const mockReplaceProjectModelSets = vi.fn();
const mockGetOrganizations = vi.fn();
const auth = vi.hoisted(() => ({ user: null as any }));

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => { resolve = next; });
  return { promise, resolve };
}

vi.mock('../../api/client', () => ({
  api: {
    getModelRegistry: (...args: any[]) => mockGetModelRegistry(...args),
    createModelRegistry: (...args: any[]) => mockCreateModelRegistry(...args),
    updateModelRegistry: (...args: any[]) => mockUpdateModelRegistry(...args),
    getModelAliases: (...args: any[]) => mockGetModelAliases(...args),
    createModelAlias: (...args: any[]) => mockCreateModelAlias(...args),
    updateModelAlias: (...args: any[]) => mockUpdateModelAlias(...args),
    getModelSets: (...args: any[]) => mockGetModelSets(...args),
    createModelSet: (...args: any[]) => mockCreateModelSet(...args),
    getModelSetItems: (...args: any[]) => mockGetModelSetItems(...args),
    replaceModelSetItems: (...args: any[]) => mockReplaceModelSetItems(...args),
    getProjects: (...args: any[]) => mockGetProjects(...args),
    getProjectModelSets: (...args: any[]) => mockGetProjectModelSets(...args),
    replaceProjectModelSets: (...args: any[]) => mockReplaceProjectModelSets(...args),
    getOrganizations: (...args: any[]) => mockGetOrganizations(...args),
  },
}));

vi.mock('../../api/modelExecutionProfiles', () => ({
  modelExecutionProfileApi: {
    list: (...args: any[]) => mockGetModelExecutionProfiles(...args),
  },
}));

vi.mock('../../store/auth', () => ({
  useAuthStore: (selector: any) => selector({ user: auth.user }),
}));

const REGISTRY = {
  data: [
    {
      id: 1,
      model_name: 'gpt-4o',
      display_name: 'Company Chat',
      provider_family: 'openai',
      lifecycle: 'ga',
      context_length: 128000,
      output_limit: 4096,
      capability_tags: ['chat', 'vision'],
      data_residency: 'us',
      approved_catalog: true,
      migration_recommendation: 'Migrate to gpt-4.1-mini',
    },
  ],
  total: 1,
};

const EXECUTION_PROFILES = {
  data: [
    {
      id: 'unverified.chat',
      version: '1.0.0',
      current: true,
      label: '自定义 / 未验证模型',
      tokenizer_profile_id: 'tokenizer.byte-upper',
      tokenizer_profile_version: '1.0.0',
      reasoning_profile: 'reasoning.unverified.v1',
      cache_usage_profile: 'cache-usage.unverified.v1',
      protocol_profile: 'protocol.openai-compatible.unverified.v1',
      catalog_evidence_state: 'unverified',
      provider_context_window: 0,
      provider_max_output: 0,
      default_context_window: 0,
      default_output_limit: 0,
      provider_capabilities: [],
      capabilities: [],
      always_required_capabilities: [],
      aliases: [],
      evidence_refs: [],
    },
    {
      id: 'minimax.m3.token-plan',
      version: '1.3.0',
      current: true,
      label: 'MiniMax M3 官方 API（OpenAI Chat Embedded Reasoning）',
      tokenizer_profile_id: 'tokenizer.byte-upper',
      tokenizer_profile_version: '1.0.0',
      reasoning_profile: 'reasoning.minimax-m3-adaptive-embedded.v1',
      cache_usage_profile: 'cache-usage.minimax-m3.v1',
      protocol_profile: 'protocol.minimax-m3-openai-chat.v5',
      catalog_evidence_state: 'locally_conformant',
      provider_context_window: 1_000_000,
      provider_max_output: 524_288,
      executable_max_output: 131_072,
      default_context_window: 1_000_000,
      default_output_limit: 131_072,
      provider_capabilities: ['reasoning', 'tools', 'vision', 'video', 'prompt_cache'],
      capabilities: ['streaming', 'reasoning', 'tools', 'function_tools', 'tool_choice_auto'],
      always_required_capabilities: [],
      aliases: ['MiniMax M3', 'MiniMax-M3'],
      evidence_refs: ['https://platform.minimaxi.com/docs/api-reference/text-chat-openai'],
    },
    {
      id: 'doubao.seed-evolving.api',
      version: '1.1.0',
      current: false,
      label: 'Doubao Seed Evolving Ark Floating Alias Identity',
      tokenizer_profile_id: 'tokenizer.byte-upper',
      tokenizer_profile_version: '1.0.0',
      reasoning_profile: 'reasoning.doubao-documented.v1',
      cache_usage_profile: 'cache-usage.doubao-documented.v1',
      protocol_profile: 'protocol.doubao-openai-chat.documented.v1',
      catalog_evidence_state: 'documented',
      provider_context_window: 1_048_576,
      provider_max_output: 262_144,
      default_context_window: 1_048_576,
      default_output_limit: 4_096,
      provider_capabilities: ['mutable_alias'],
      capabilities: [],
      always_required_capabilities: [],
      aliases: ['Doubao Seed Evolving', 'doubao-seed-evolving'],
      evidence_refs: ['https://ark.volcengine.com/region:cn-beijing/model/comparison'],
    },
  ],
};

const ALIASES = {
  data: [
    { id: 2, alias: 'company-chat', target_model: 'gpt-4o', display_name: 'Company Chat', description: 'stable chat', status: 'active' },
  ],
  total: 1,
};

const MODEL_SETS = {
  data: [
    { id: 10, name: 'Core Chat Models', slug: 'core-chat', description: 'core apps', status: 'active', item_count: 1 },
  ],
  total: 1,
};

const PROJECTS = {
  data: [
    { id: 7, name: 'Core Project', slug: 'core', status: 'active' },
  ],
  total: 1,
};

function setupMocks() {
  mockGetModelRegistry.mockResolvedValue(REGISTRY);
  mockGetModelExecutionProfiles.mockResolvedValue(EXECUTION_PROFILES);
  mockCreateModelRegistry.mockResolvedValue({ id: 3 });
  mockUpdateModelRegistry.mockResolvedValue({ success: true });
  mockGetModelAliases.mockResolvedValue(ALIASES);
  mockCreateModelAlias.mockResolvedValue({ id: 4 });
  mockUpdateModelAlias.mockResolvedValue({ success: true });
  mockGetModelSets.mockResolvedValue(MODEL_SETS);
  mockCreateModelSet.mockResolvedValue({ id: 10 });
  mockGetModelSetItems.mockResolvedValue({ data: [{ item_type: 'alias', item_name: 'company-chat' }], total: 1 });
  mockReplaceModelSetItems.mockResolvedValue({ success: true });
  mockGetProjects.mockResolvedValue(PROJECTS);
  mockGetProjectModelSets.mockResolvedValue(MODEL_SETS);
  mockReplaceProjectModelSets.mockResolvedValue({ success: true });
  mockGetOrganizations.mockResolvedValue({ data: [{ id: 7, name: '总部' }], total: 1 });
}

describe('ModelGovernancePage helpers', () => {
  it('parses and formats model set items with de-duplication', () => {
    const items = parseModelSetItems('alias:company-chat\nmodel:gpt-4o\nalias:company-chat');

    expect(items).toEqual([
      { item_type: 'alias', item_name: 'company-chat' },
      { item_type: 'model', item_name: 'gpt-4o' },
    ]);
    expect(formatModelSetItems(items)).toBe('alias:company-chat\nmodel:gpt-4o');
  });

	it('detects display-format variants of the same model identity', () => {
		expect(comparableModelIdentity(' Kimi K3 ')).toBe('kimi-k3');
		expect(comparableModelIdentity('DeepSeek-V4-Pro')).toBe(comparableModelIdentity('deepseek-v4-pro'));
	});
});

describe('ModelGovernancePage', () => {
  beforeEach(() => {
    act(() => { Toast.destroyAll(); });
    vi.clearAllMocks();
    window.history.replaceState(null, '', '/model-governance');
    auth.user = null;
    setupMocks();
  });

  it('loads the model registry by default', async () => {
    render(<ModelGovernancePage />);

    await waitFor(() => {
      expect(mockGetModelRegistry).toHaveBeenCalledWith(1, 20, {
        lifecycle: '',
        approved_only: false,
        q: '',
      });
    });

    expect(await screen.findByText('Company Chat')).toBeTruthy();
    expect(screen.getByText('gpt-4o')).toBeTruthy();
    expect(screen.getByText('Migrate to gpt-4.1-mini')).toBeTruthy();
  });

  it('does not mount or advertise the retired runtime-change workspace for its former deep link', async () => {
    auth.user = {
      role: 'platform_admin',
      org_id: 99,
      customer_context: { state: 'ready', org_id: 7, org_name: '总部' },
      identity_scope: 'deployment_operator',
    };
    window.history.replaceState(null, '', '/model-governance?tab=releases');

    render(<ModelGovernancePage />);

    await waitFor(() => expect(mockGetModelRegistry).toHaveBeenCalled());
    expect(screen.queryByRole('tab', { name: '运行时发布' })).toBeNull();
    expect(screen.getByRole('tab', { name: '模型目录' })).toHaveAttribute('aria-selected', 'true');
    expect(window.location.search).toBe('?tab=releases');
  });

	it('separates lifecycle totals from project visibility and flags duplicate test records', async () => {
		mockGetModelRegistry.mockResolvedValueOnce({
			data: [
				{ ...REGISTRY.data[0], id: 1, model_name: 'Kimi K3', lifecycle: 'ga', approved_catalog: true },
				{ ...REGISTRY.data[0], id: 2, model_name: 'kimi-k3', lifecycle: 'staging', approved_catalog: true },
			],
			total: 2,
		});
		render(<ModelGovernancePage />);

		expect(await screen.findByText(/发现 1 组疑似重复模型标识/)).toBeTruthy();
		expect(screen.getByText('当前页 GA 记录').nextElementSibling?.textContent).toBe('1');
		expect(screen.getByText('当前筛选记录总数').nextElementSibling?.textContent).toBe('2');
		expect(screen.getByText('项目可见')).toBeTruthy();
		expect(screen.getByText('已批准·待上线')).toBeTruthy();
		expect(screen.getAllByRole('button', { name: '退役并移出目录' })).toHaveLength(2);
	});

  it('binds platform model governance to the canonical customer without an organization selector', async () => {
    auth.user = {
      role: 'platform_admin',
      org_id: 99,
      customer_context: { state: 'ready', org_id: 7, org_name: '总部' },
      identity_scope: 'deployment_operator',
    };

    render(<ModelGovernancePage />);

    expect(await screen.findByText('客户范围：总部')).toBeTruthy();
    expect(screen.queryByLabelText('目标组织')).toBeNull();
    await waitFor(() => {
      expect(mockGetModelRegistry).toHaveBeenCalledWith(1, 20, expect.objectContaining({ org_id: 7 }));
    });
    expect(mockGetOrganizations).not.toHaveBeenCalled();
  });

  it('fails closed for a platform operator without a verified customer context', () => {
    auth.user = { role: 'platform_admin', org_id: 99, customer_context: { state: 'ambiguous' } };

    render(<ModelGovernancePage />);

    expect(screen.getAllByText('客户组织上下文不唯一').length).toBeGreaterThanOrEqual(1);
    expect(mockGetModelRegistry).not.toHaveBeenCalled();
  });

  it('loads aliases when the alias tab is selected', async () => {
    render(<ModelGovernancePage />);

    fireEvent.click(screen.getByRole('tab', { name: '企业别名' }));

    await waitFor(() => {
      expect(mockGetModelAliases).toHaveBeenCalledWith(1, 20);
    });
    expect(await screen.findByText('company-chat')).toBeTruthy();
  });

  it('normalizes decimal-SI context shorthand before creating a model', async () => {
    render(<ModelGovernancePage />);

    fireEvent.click(await screen.findByRole('button', { name: /新增模型/ }));
    expect(await screen.findByText(/自动匹配只接受 Profile 中唯一的精确别名/)).toBeTruthy();
    expect(screen.getByText(/标签本身不会执行能力检测/)).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '模型生命周期' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '开发者目录可见' })).toBeTruthy();
    fireEvent.change(await screen.findByPlaceholderText('gpt-4o'), { target: { value: 'exact-context-model' } });
    const contextInput = await screen.findByPlaceholderText('128K / 1M / 131072');
    fireEvent.change(contextInput, { target: { value: '128K' } });
    expect(await screen.findByText(/规范化：128K → 128,000 tokens/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '保存' }));

    await waitFor(() => expect(mockCreateModelRegistry).toHaveBeenCalledWith(expect.objectContaining({
      model_name: 'exact-context-model',
      context_length: 128_000,
    })));
  });

  it('auto-matches a verified MiniMax profile without advertising unimplemented multimodal execution', async () => {
    render(<ModelGovernancePage />);

    fireEvent.click(await screen.findByRole('button', { name: /新增模型/ }));
    fireEvent.change(await screen.findByPlaceholderText('gpt-4o'), { target: { value: 'MiniMax-M3' } });

    expect(await screen.findByText('协议：protocol.minimax-m3-openai-chat.v5')).toBeTruthy();
    expect(screen.getByText('Catalog 证据：locally_conformant（不代表 live verified 或 released）')).toBeTruthy();
    expect(screen.getByText(/UniRouter 已验证：streaming、reasoning、tools、function_tools、tool_choice_auto/)).toBeTruthy();
    expect(screen.getByText(/供应商另有声明但网关尚未放行：vision、video、prompt_cache/)).toBeTruthy();
    expect(await screen.findByText(/规范化：1000000 → 1,000,000 tokens/)).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '保存' }));
    await waitFor(() => expect(mockCreateModelRegistry).toHaveBeenCalledWith(expect.objectContaining({
      model_name: 'MiniMax-M3',
      context_length: 1_000_000,
      output_limit: 131_072,
      capability_tags: 'streaming,reasoning,tools,function_tools,tool_choice_auto',
      execution_profile_id: 'minimax.m3.token-plan',
      execution_profile_version: '1.3.0',
    })));
  });

  it('hides non-current Profile candidates from new model binding and auto-match', async () => {
    render(<ModelGovernancePage />);

    fireEvent.click(await screen.findByRole('button', { name: /新增模型/ }));
    await act(async () => {
      fireEvent.change(await screen.findByPlaceholderText('gpt-4o'), { target: { value: 'doubao-seed-evolving' } });
    });

    expect(screen.queryByText('Doubao Seed Evolving Ark Floating Alias Identity')).toBeNull();
    expect(screen.queryByText(/历史绑定（仅保留）/)).toBeNull();
  });

  it.each([false, true])('requires an explicit surface for ambiguous GPT aliases (reverse=%s)', async (reverse) => {
    const candidates = ['chat', 'responses'].map(surface => ({
      ...EXECUTION_PROFILES.data[1],
      id: `openai.gpt-5.6-sol.${surface}`, version: '1.3.0',
      label: `GPT Sol ${surface}`, aliases: ['gpt-5.6-sol'],
    }));
    mockGetModelExecutionProfiles.mockResolvedValue({ data: reverse ? [...candidates].reverse() : candidates });
    render(<ModelGovernancePage />);
    fireEvent.click(await screen.findByRole('button', { name: /新增模型/ }));
    await act(async () => {
      fireEvent.change(await screen.findByPlaceholderText('gpt-4o'), { target: { value: 'gpt-5.6-sol' } });
    });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: '保存' })); });
    expect(await screen.findByText('模型名称对应多个 API surface，请明确选择执行 Profile')).toBeTruthy();
    expect(mockCreateModelRegistry).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('combobox', { name: '版本化执行 Profile' }));
    await act(async () => {
      fireEvent.click(await screen.findByText('GPT Sol responses · openai.gpt-5.6-sol.responses@1.3.0'));
    });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: '保存' })); });
    await waitFor(() => expect(mockCreateModelRegistry).toHaveBeenCalledWith(expect.objectContaining({
      execution_profile_id: 'openai.gpt-5.6-sol.responses', execution_profile_version: '1.3.0',
    })));
  });

  it.each([384_000, 4096])('uses executable output rather than request default and permits a lower cap (%s)', async (output) => {
    const profile = {
      ...EXECUTION_PROFILES.data[1], id: 'deepseek.v4-flash.api', version: '1.5.0',
      label: 'DeepSeek V4 Flash', aliases: ['deepseek-v4-flash'],
      provider_max_output: 384_000, executable_max_output: 384_000, default_output_limit: 32_768,
    };
    mockGetModelExecutionProfiles.mockResolvedValue({ data: [
      { ...profile, version: '1.0.0', current: false }, profile,
    ] });
    render(<ModelGovernancePage />);
    fireEvent.click(await screen.findByRole('button', { name: /新增模型/ }));
    fireEvent.change(await screen.findByPlaceholderText('gpt-4o'), { target: { value: 'deepseek-v4-flash' } });
    const outputInput = await screen.findByDisplayValue('384000');
    expect(await screen.findByText(/请求默认 output 32768/)).toBeTruthy();
    if (output !== 384_000) fireEvent.change(outputInput, { target: { value: String(output) } });
    fireEvent.click(screen.getByRole('button', { name: '保存' }));
    await waitFor(() => expect(mockCreateModelRegistry).toHaveBeenCalledWith(expect.objectContaining({
      output_limit: output, execution_profile_version: '1.5.0',
    })));
  });

  it('keeps a non-current Profile visible only for its existing historical binding', async () => {
    mockGetModelRegistry.mockResolvedValue({
      data: [{
        ...REGISTRY.data[0],
        execution_profile_id: 'doubao.seed-evolving.api',
        execution_profile_version: '1.1.0',
      }],
      total: 1,
    });
    render(<ModelGovernancePage />);

    fireEvent.click(await screen.findByRole('button', { name: '编辑模型' }));

    expect(await screen.findByText('Doubao Seed Evolving Ark Floating Alias Identity')).toBeTruthy();
    expect(screen.getByText(/历史绑定（仅保留）/)).toBeTruthy();
    expect(screen.getByDisplayValue('128000')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '保存' }));
    await waitFor(() => expect(mockUpdateModelRegistry).toHaveBeenCalledWith(1, expect.objectContaining({
      context_length: 128_000, output_limit: 4096,
      execution_profile_id: 'doubao.seed-evolving.api', execution_profile_version: '1.1.0',
    })));
  });

  it('normalizes sparse profile collections before opening an existing model', async () => {
    mockGetModelExecutionProfiles.mockResolvedValue({
      data: [{
        ...EXECUTION_PROFILES.data[0],
        provider_capabilities: null,
        capabilities: null,
        always_required_capabilities: null,
        aliases: null,
        evidence_refs: null,
      }],
    });
    mockGetModelRegistry.mockResolvedValue({
      data: [{
        ...REGISTRY.data[0],
        execution_profile_id: 'unverified.chat',
        execution_profile_version: '1.0.0',
      }],
      total: 1,
    });

    render(<ModelGovernancePage />);
    fireEvent.click(await screen.findByRole('button', { name: '编辑模型' }));

    expect(await screen.findByText('自定义 / 未验证模型')).toBeTruthy();
    expect(screen.getByText('UniRouter 已验证：尚无本地一致性能力')).toBeTruthy();
    expect(screen.getByText('每次调用必须证明：无')).toBeTruthy();
  });

  it('loads model set items from the model set table', async () => {
    render(<ModelGovernancePage />);

    fireEvent.click(screen.getByRole('tab', { name: '模型集' }));
    expect(await screen.findByText('Core Chat Models')).toBeTruthy();
    fireEvent.click(screen.getByText('条目'));

    await waitFor(() => {
      expect(mockGetModelSetItems).toHaveBeenCalledWith(10);
    });
    expect(await screen.findByDisplayValue('alias:company-chat')).toBeTruthy();
  });

  it('saves project model set assignments', async () => {
    render(<ModelGovernancePage />);

    fireEvent.click(screen.getByRole('tab', { name: '项目分配' }));

    await waitFor(() => {
      expect(mockGetProjects).toHaveBeenCalledWith(1, 100, { status: 'active' });
      expect(mockGetProjectModelSets).toHaveBeenCalledWith(7);
    });
    expect(screen.getByRole('combobox', { name: '项目' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '模型集' })).toBeTruthy();
    fireEvent.click(await screen.findByText('保存分配'));

    await waitFor(() => {
      expect(mockReplaceProjectModelSets).toHaveBeenCalledWith(7, [10]);
    });
  });

  it('does not carry a previous project assignment into a project whose load failed', async () => {
    mockGetProjects.mockResolvedValue({
      data: [
        ...PROJECTS.data,
        { id: 8, name: 'Other Project', slug: 'other', status: 'active' },
      ],
      total: 2,
    });
    mockGetProjectModelSets.mockImplementation((projectId: number) => projectId === 7
      ? Promise.resolve(MODEL_SETS)
      : Promise.reject(new Error('temporarily unavailable')));

    render(<ModelGovernancePage />);

    fireEvent.click(screen.getByRole('tab', { name: '项目分配' }));
    await waitFor(() => expect(mockGetProjectModelSets).toHaveBeenCalledWith(7));
    expect(await screen.findByText('Core Chat Models')).toBeTruthy();

    fireEvent.click(screen.getByRole('combobox', { name: '项目' }));
    fireEvent.click(await screen.findByText('Other Project (other)'));

    await waitFor(() => expect(mockGetProjectModelSets).toHaveBeenCalledWith(8));
    await waitFor(() => expect(screen.getByRole('button', { name: '保存分配' })).toBeDisabled());
    expect(screen.queryByText('Core Chat Models')).toBeNull();
    expect(mockReplaceProjectModelSets).not.toHaveBeenCalled();
  });

  it('keeps only the latest registry search and exposes failures inline', async () => {
    const oldRequest = deferred<any>();
    const currentRequest = deferred<any>();
    mockGetModelRegistry
      .mockReturnValueOnce(oldRequest.promise)
      .mockReturnValueOnce(currentRequest.promise);

    render(<ModelGovernancePage />);
    await waitFor(() => expect(mockGetModelRegistry).toHaveBeenCalledTimes(1));

    fireEvent.change(screen.getByPlaceholderText('搜索模型、显示名或供应商族'), { target: { value: 'fresh' } });
    await waitFor(() => expect(mockGetModelRegistry).toHaveBeenCalledTimes(2));

    await act(async () => currentRequest.resolve({
      data: [{ id: 3, model_name: 'fresh-model', display_name: 'Fresh Model', lifecycle: 'ga' }],
      total: 1,
    }));
    expect(await screen.findByText('Fresh Model')).toBeTruthy();

    await act(async () => oldRequest.resolve({
      data: [{ id: 4, model_name: 'stale-model', display_name: 'Stale Model', lifecycle: 'ga' }],
      total: 1,
    }));
    await waitFor(() => expect(screen.queryByText('Stale Model')).toBeNull());

    mockGetModelRegistry.mockRejectedValueOnce(new Error('unavailable'));
    fireEvent.click(screen.getByRole('button', { name: /刷新/ }));
    expect(await screen.findByRole('heading', { name: '模型目录加载失败' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '重新加载模型目录' })).toBeTruthy();
  });
});
