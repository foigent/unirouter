import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import ApiDocsPage from '../ApiDocsPage';
import { useAuthStore } from '../../store/auth';

const mockGetErrorDocs = vi.fn();
const mockGetWorkloadApplicationContract = vi.fn();
const mockGetGatewayEndpoints = vi.fn();
const mockGetProjects = vi.fn();
const mockGetModelCatalog = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getErrorDocs: (...args: any[]) => mockGetErrorDocs(...args),
    getWorkloadApplicationContract: (...args: any[]) => mockGetWorkloadApplicationContract(...args),
    getGatewayEndpoints: (...args: any[]) => mockGetGatewayEndpoints(...args),
    getProjects: (...args: any[]) => mockGetProjects(...args),
    getModelCatalog: (...args: any[]) => mockGetModelCatalog(...args),
  },
}));

function renderPage(initialEntry = '/docs') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <ApiDocsPage />
    </MemoryRouter>,
  );
}

describe('ApiDocsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.setState({
      user: { id: 1, username: 'admin', role: 'platform_admin', org_id: 1 },
      token: 'token',
      isAuthenticated: true,
      profileLoading: false,
    });
    mockGetErrorDocs.mockResolvedValue({
      data: [{
        code: 'auth_error.invalid_api_key',
        http_status: 401,
        summary: 'Invalid API key',
        resolution: 'Create a valid project API key.',
      }],
    });
    mockGetWorkloadApplicationContract.mockResolvedValue({ client_profiles: [{
      profile_id: 'client.openai-sdk.chat.v1', display_name: 'OpenAI SDK / Chat Completions',
      supported_endpoints: ['/v1/chat/completions'], runtime_state: 'runtime_supported',
      evidence_level: 'verified', recommended_for_poc: true, configuration_note: 'Use configured endpoint.',
    }] });
    mockGetGatewayEndpoints.mockResolvedValue({ data: [] });
    mockGetProjects.mockResolvedValue({ data: [], total: 0 });
    mockGetModelCatalog.mockResolvedValue({
      data: [{ model_name: 'company-chat', aliases: 'chat-default' }],
      total: 1,
    });
  });

  it('renders the current API contract index and post-PRD1 routes', async () => {
    renderPage();

    await waitFor(() => {
      expect(mockGetErrorDocs).toHaveBeenCalled();
    });

    expect(screen.getByText('API 与兼容性')).toBeTruthy();
    expect(screen.getByText(/协议兼容不等于已完成真实客户端认证/)).toBeTruthy();
    expect(screen.getByText('OpenAI SDK / Chat Completions')).toBeTruthy();
    expect(screen.getByText(/模型网关使用项目 API Key 或 Agent 能力令牌鉴权/)).toBeTruthy();
    expect(screen.queryByText(/PRD1 API index/)).toBeNull();
    expect(screen.queryByText(/PRD2 当前可用接口索引/)).toBeNull();

    [
      '/api/v1/auth/oidc/config',
      '/api/v1/usage/analytics',
      '/api/v1/internal-status',
      '/api/v1/error-docs',
      '/api/v1/model-registry',
      '/api/v1/notification-webhooks',
      '/api/v1/license',
      '/api/v1/model-deployment-releases/{releaseRef}/cutover-legacy',
      '/api/v1/model-deployment-releases/{releaseRef}/rollback-legacy',
      '/v1/embeddings',
    ].forEach((path) => {
      expect(screen.getAllByText(path).length).toBeGreaterThan(0);
    });
    expect(screen.getAllByText('/api/v1/model-deployment-releases').length).toBeGreaterThanOrEqual(2);
    expect(screen.getAllByText('已退役：固定返回 HTTP 410（legacy_model_mutation_retired）；请使用 canonical 模型部署与发布 API')).toHaveLength(19);

    [
      '部署级设置（仅平台管理员）',
      'License 状态（仅平台管理员）',
      '全局输入阻断词库（仅平台管理员）',
      '唯一客户的模型注册表；部署管理员也自动绑定该客户上下文',
      '组织模型目录；org_viewer 可只读本组织，member 必须提供 project_id 并通过 live membership',
      '更新本组织别名；越权组织参数返回 403，越权或不存在的 ID 返回 404',
      '原子创建本组织模型集合及其全部初始条目',
      '原子替换本组织模型集合条目；不会暴露部分结果',
      '按组织读取公共模型、服务端权威 baseline/候选项和可操作 canonical v2 release；platform_admin、org_admin、org_viewer 可读且不要求 Gateway License',
      '以服务端 baseline、精确 deployment/operation 选择和 Idempotency-Key 原子编写 sealed validated release；要求组织写角色与 Gateway License，但编写不会激活或改变流量',
      '仅在 Chat 与 Embeddings 可执行计划完整同等后原子替换精确 sealed v1 stable parent；要求组织写角色、Gateway License 和 Idempotency-Key',
      '安全收缩到精确 sealed 且当前仍可执行的 v1 parent；v2 健康失败不阻断，仍要求组织写角色和 Idempotency-Key，但不要求 Gateway License',
    ].forEach((description) => {
      expect(screen.getByText(description)).toBeTruthy();
    });

    expect(await screen.findByText('auth_error.invalid_api_key')).toBeTruthy();
    expect(screen.getByText(/这不等同于当前 UniRouter 服务不可用/)).toBeTruthy();
    expect(screen.queryByText(/不能宣称现场可用/)).toBeNull();
  });

  it('shows organization model governance writes without platform-only financial operations', async () => {
    useAuthStore.setState({ user: { id: 2, username: 'org-admin', role: 'org_admin', org_id: 1 } });

    renderPage();

    await screen.findByText('auth_error.invalid_api_key');

    [
      '在本组织创建模型注册记录；跨组织请求返回 403',
      '更新本组织记录；越权组织参数返回 403，越权或不存在的 ID 返回 404',
      '在本组织创建模型别名；跨组织请求返回 403',
      '更新本组织别名；越权组织参数返回 403，越权或不存在的 ID 返回 404',
      '原子创建本组织模型集合及其全部初始条目',
      '原子替换本组织模型集合条目；不会暴露部分结果',
      '查看 CNY/百万 tokens 基础价格与供应商 Prompt Cache 倍率（平台、组织或账务管理员）',
      '以服务端 baseline、精确 deployment/operation 选择和 Idempotency-Key 原子编写 sealed validated release；要求组织写角色与 Gateway License，但编写不会激活或改变流量',
      '仅在 Chat 与 Embeddings 可执行计划完整同等后原子替换精确 sealed v1 stable parent；要求组织写角色、Gateway License 和 Idempotency-Key',
      '安全收缩到精确 sealed 且当前仍可执行的 v1 parent；v2 健康失败不阻断，仍要求组织写角色和 Idempotency-Key，但不要求 Gateway License',
    ].forEach((description) => {
      expect(screen.getByText(description)).toBeTruthy();
    });

    [
      '创建或覆盖 CNY/百万 tokens 基础价格与 Prompt Cache 倍率（仅平台管理员）',
      '更新 CNY/百万 tokens 基础价格与 Prompt Cache 倍率（仅平台管理员）',
      '删除基础模型价格（仅平台管理员）',
      '创建或覆盖组织计费倍率（仅平台管理员）',
      '更新组织计费倍率（仅平台管理员）',
      '删除组织计费倍率（仅平台管理员）',
      '估算/实际用量及供应商 Prompt Cache 分项对账',
      '平台管理员处理未决供应商成本；请求不接受金额，collect 仅重放权威用量与冻结客户价',
      '平台管理员按明确组织查看最多 100 条未决供应商成本元数据',
    ].forEach((description) => {
      expect(screen.queryByText(description)).toBeNull();
    });
  });

  it('shows project administrators the project update contracts only available to that role', async () => {
    useAuthStore.setState({ user: { id: 3, username: 'project-admin', role: 'project_admin', organization_role: 'member', org_id: 1 } });
    mockGetProjects.mockResolvedValue({
      data: [{ id: 7, name: 'Core', current_user_membership_role: 'project_admin' }],
      total: 1,
    });

    renderPage();

    await screen.findByText('auth_error.invalid_api_key');

    expect(screen.getByText('项目 API 参考')).toBeTruthy();
    expect(screen.getByText('更新项目')).toBeTruthy();
    expect(screen.getByText('更新项目模型集合绑定')).toBeTruthy();
    expect(screen.getByText('查看项目成员与同组织可加入用户')).toBeTruthy();
    expect(screen.getByText('仅更新项目预算金额与周期并原子记录审计')).toBeTruthy();
    expect(screen.getByText('唯一客户的模型集合；项目管理员只读其有权项目的候选集合')).toBeTruthy();
    expect(screen.queryByText('内部状态与事件历史')).toBeNull();
    expect(screen.getByText('通知事件列表；项目管理员必须提供 project_id')).toBeTruthy();
    expect(screen.queryByText('在本组织创建模型注册记录；跨组织请求返回 403')).toBeNull();
    expect(screen.queryByText('X-Insight 使用项目绑定、网络绑定且不可调用模型的专用 service key 拉取终态执行元数据')).toBeNull();
    expect(screen.queryByText('读取 Prompt Cache 净节省、证据覆盖与未接入能力边界')).toBeNull();
  });

  it('shows project users one quickstart instead of the control-plane reference', async () => {
    useAuthStore.setState({ user: { id: 5, username: 'developer', role: 'developer', organization_role: 'member', org_id: 1 } });
    mockGetProjects.mockResolvedValue({
      data: [{ id: 7, name: 'Core', current_user_membership_role: 'project_user' }],
      total: 1,
    });

    renderPage();

    expect(await screen.findByText('API 快速接入')).toBeTruthy();
    expect(await screen.findByText('company-chat')).toBeTruthy();
    expect(screen.getByText('常用兼容接口')).toBeTruthy();
    expect(screen.getByText('具体可用性取决于所选模型已验证的能力；系统不会按模型名称推断。')).toBeTruthy();
    expect(screen.getByText('/v1/chat/completions')).toBeTruthy();
    expect(screen.getByText('仅在项目有已启用向量模型时生成向量')).toBeTruthy();
    expect(screen.getByText(/控制台登录会话不能替代项目 API Key/)).toBeTruthy();
    expect(screen.getByText('常见错误与排查')).toBeTruthy();
    expect(screen.queryByText('项目 API 参考')).toBeNull();
    expect(screen.queryByText('私有集成')).toBeNull();
    expect(screen.queryByText('成本优化')).toBeNull();
    expect(screen.queryByText('上游通道列表')).toBeNull();
    expect(mockGetModelCatalog).toHaveBeenCalledWith(1, 100, { project_id: 7, routable_only: true });
  });

  it('shows billing administrators the model price read contract but no price writes', async () => {
    useAuthStore.setState({ user: { id: 4, username: 'billing-admin', role: 'billing_admin', org_id: 1 } });

    renderPage();

    await screen.findByText('auth_error.invalid_api_key');

    expect(screen.getByText('查看 CNY/百万 tokens 基础价格与供应商 Prompt Cache 倍率（平台、组织或账务管理员）')).toBeTruthy();
    expect(screen.getByText('查看项目预算（账务管理员仅有预算权限）')).toBeTruthy();
    expect(screen.getByText('仅更新项目预算金额与周期并原子记录审计')).toBeTruthy();
    expect(screen.queryByText('查看项目成员与同组织可加入用户')).toBeNull();
    expect(screen.queryByText('通知 Webhook 列表；项目管理员必须提供 project_id')).toBeNull();
    expect(screen.queryByText('创建或覆盖 CNY/百万 tokens 基础价格与 Prompt Cache 倍率（仅平台管理员）')).toBeNull();
  });

  it('shows project viewers only read operations and no paid debug endpoint', async () => {
    useAuthStore.setState({ user: { id: 2, username: 'viewer', role: 'viewer', organization_role: 'member', org_id: 1 } });
    mockGetProjects.mockResolvedValue({
      data: [{ id: 7, name: 'Core', current_user_membership_role: 'project_viewer' }],
      total: 1,
    });

    mockGetErrorDocs.mockResolvedValue({
      data: [
        {
          code: 'auth_error.invalid_api_key',
          http_status: 401,
          summary: 'Invalid API key',
          resolution: 'Create a valid project API key.',
        },
        {
          code: 'guardrail_error.blocked',
          http_status: 400,
          summary: 'Blocked by policy',
          resolution: 'Review the request content.',
        },
      ],
    });

    renderPage('/docs#error-guardrail_error.blocked');

    expect(await screen.findByText('API 快速接入')).toBeTruthy();
    expect(screen.getByText('当前项目角色为只读')).toBeTruthy();
    expect(await screen.findByText('guardrail_error.blocked')).toBeTruthy();
    expect(screen.queryByText('项目 API 参考')).toBeNull();
    expect(screen.queryByText('请求调试台直接使用项目访问凭证调用')).toBeNull();
  });

  it('uses organization role for organization-wide docs without inventing project membership', async () => {
    useAuthStore.setState({ user: { id: 3, username: 'reader', role: 'org_viewer', organization_role: 'org_viewer', org_id: 1 } });

    renderPage();

    expect(screen.getByText('API 与兼容性')).toBeTruthy();
    expect(screen.getByText('Dashboard 聚合数据')).toBeTruthy();
    expect(await screen.findByText('auth_error.invalid_api_key')).toBeTruthy();
    expect(screen.getByText('错误说明')).toBeTruthy();
    expect(screen.getByText('按组织读取公共模型、服务端权威 baseline/候选项和可操作 canonical v2 release；platform_admin、org_admin、org_viewer 可读且不要求 Gateway License')).toBeTruthy();
    expect(screen.queryByText(/原子编写 sealed validated release/)).toBeNull();
    expect(screen.queryByText(/原子替换精确 sealed v1 stable parent/)).toBeNull();
    expect(screen.queryByText(/安全收缩到精确 sealed 且当前仍可执行的 v1 parent/)).toBeNull();
    expect(mockGetErrorDocs).toHaveBeenCalledTimes(1);
    expect(screen.queryByText('创建项目访问凭证')).toBeNull();
  });
});
