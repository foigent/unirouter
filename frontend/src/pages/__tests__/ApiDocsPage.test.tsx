import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import ApiDocsPage from '../ApiDocsPage';
import { useAuthStore } from '../../store/auth';

const mockGetErrorDocs = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getErrorDocs: (...args: any[]) => mockGetErrorDocs(...args),
  },
}));

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
  });

  it('renders the current API contract index and post-PRD1 routes', async () => {
    render(<ApiDocsPage />);

    await waitFor(() => {
      expect(mockGetErrorDocs).toHaveBeenCalled();
    });

    expect(screen.getByText('开发者文档')).toBeTruthy();
    expect(screen.getByText(/管理 API 按当前控制台角色过滤/)).toBeTruthy();
    expect(screen.getByText(/Gateway 使用项目 API Key 或 Agent 能力令牌鉴权/)).toBeTruthy();
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
      '/v1/embeddings',
    ].forEach((path) => {
      expect(screen.getAllByText(path).length).toBeGreaterThan(0);
    });

    [
      '部署级设置（仅平台管理员）',
      'License 状态（仅平台管理员）',
      '全局输入阻断词库（仅平台管理员）',
      '本组织模型注册表；平台管理员可显式选择组织',
      '组织模型目录；project_id 决定组织，项目角色必须提供 project_id',
      '更新本组织别名；越权组织参数返回 403，越权或不存在的 ID 返回 404',
      '原子创建本组织模型集合及其全部初始条目',
      '原子替换本组织模型集合条目；不会暴露部分结果',
    ].forEach((description) => {
      expect(screen.getByText(description)).toBeTruthy();
    });

    expect(await screen.findByText('auth_error.invalid_api_key')).toBeTruthy();
  });

  it('shows organization model governance writes without platform-only financial operations', async () => {
    useAuthStore.setState({ user: { id: 2, username: 'org-admin', role: 'org_admin', org_id: 1 } });

    render(<ApiDocsPage />);

    await screen.findByText('auth_error.invalid_api_key');

    [
      '在本组织创建模型注册记录；跨组织请求返回 403',
      '更新本组织记录；越权组织参数返回 403，越权或不存在的 ID 返回 404',
      '在本组织创建模型别名；跨组织请求返回 403',
      '更新本组织别名；越权组织参数返回 403，越权或不存在的 ID 返回 404',
      '原子创建本组织模型集合及其全部初始条目',
      '原子替换本组织模型集合条目；不会暴露部分结果',
      '查看 CNY/百万 tokens 基础价格与供应商 Prompt Cache 倍率（平台、组织或账务管理员）',
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
    useAuthStore.setState({ user: { id: 3, username: 'project-admin', role: 'project_admin', org_id: 1 } });

    render(<ApiDocsPage />);

    await screen.findByText('auth_error.invalid_api_key');

    expect(screen.getByText('更新项目')).toBeTruthy();
    expect(screen.getByText('更新项目模型集合绑定')).toBeTruthy();
    expect(screen.getByText('查看项目成员与同组织可加入用户')).toBeTruthy();
    expect(screen.getByText('仅更新项目预算金额与周期并原子记录审计')).toBeTruthy();
    expect(screen.getByText('本组织模型集合；项目管理员只读候选集合，平台管理员可显式选择组织')).toBeTruthy();
    expect(screen.queryByText('内部状态与事件历史')).toBeNull();
    expect(screen.getByText('通知事件列表；项目管理员必须提供 project_id')).toBeTruthy();
    expect(screen.queryByText('在本组织创建模型注册记录；跨组织请求返回 403')).toBeNull();
  });

  it('documents developer-owned key disable without exposing general key updates', async () => {
    useAuthStore.setState({ user: { id: 5, username: 'developer', role: 'developer', org_id: 1 } });

    render(<ApiDocsPage />);

    expect(await screen.findByText('停用访问凭证；developer 仅限本人拥有且仍有项目成员关系的 active Key')).toBeTruthy();
    expect(screen.queryByText('更新访问凭证')).toBeNull();
  });

  it('shows billing administrators the model price read contract but no price writes', async () => {
    useAuthStore.setState({ user: { id: 4, username: 'billing-admin', role: 'billing_admin', org_id: 1 } });

    render(<ApiDocsPage />);

    await screen.findByText('auth_error.invalid_api_key');

    expect(screen.getByText('查看 CNY/百万 tokens 基础价格与供应商 Prompt Cache 倍率（平台、组织或账务管理员）')).toBeTruthy();
    expect(screen.getByText('查看项目预算（账务管理员仅有预算权限）')).toBeTruthy();
    expect(screen.getByText('仅更新项目预算金额与周期并原子记录审计')).toBeTruthy();
    expect(screen.queryByText('查看项目成员与同组织可加入用户')).toBeNull();
    expect(screen.queryByText('通知 Webhook 列表；项目管理员必须提供 project_id')).toBeNull();
    expect(screen.queryByText('创建或覆盖 CNY/百万 tokens 基础价格与 Prompt Cache 倍率（仅平台管理员）')).toBeNull();
  });

  it('shows project viewers only read operations and no paid debug endpoint', async () => {
    useAuthStore.setState({ user: { id: 2, username: 'viewer', role: 'viewer', org_id: 1 } });

    render(<ApiDocsPage />);

    expect(await screen.findByText('项目访问凭证列表')).toBeTruthy();
    expect(screen.queryByText('创建项目访问凭证')).toBeNull();
    expect(screen.queryByText('更新访问凭证')).toBeNull();
    expect(screen.queryByText('停用访问凭证；developer 仅限本人拥有且仍有项目成员关系的 active Key')).toBeNull();
    expect(screen.queryByText('请求调试台直接使用项目访问凭证调用')).toBeNull();
  });

  it('does not call a role-protected error registry for organization viewers', async () => {
    useAuthStore.setState({ user: { id: 3, username: 'reader', role: 'org_viewer', org_id: 1 } });

    render(<ApiDocsPage />);

    expect(screen.getByText('开发者文档')).toBeTruthy();
    expect(screen.getByText('Dashboard 聚合数据')).toBeTruthy();
    expect(screen.queryByText('Error Docs')).toBeNull();
    expect(mockGetErrorDocs).not.toHaveBeenCalled();
  });
});
