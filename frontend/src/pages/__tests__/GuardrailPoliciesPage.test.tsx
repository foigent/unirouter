import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Modal } from '@douyinfe/semi-ui';
import GuardrailPoliciesPage from '../GuardrailPoliciesPage';
import { useAuthStore } from '../../store/auth';

const mocks = vi.hoisted(() => ({
  getGuardrailPolicies: vi.fn(),
  createGuardrailPolicy: vi.fn(),
  updateGuardrailPolicy: vi.fn(),
  getKeys: vi.fn(),
  getOrganizations: vi.fn(),
  getProjects: vi.fn(),
  getModelNames: vi.fn(),
  getSensitiveWords: vi.fn(),
  replaceSensitiveWords: vi.fn(),
}));

vi.mock('../../api/client', () => ({ api: mocks }));

describe('GuardrailPoliciesPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getGuardrailPolicies.mockResolvedValue({
      data: [{
        id: 8, name: 'redact-email', version: 2,
        org_id: 22, org_name: 'Acme', project_id: 9, project_name: 'Customer API',
        api_key_id: 15, api_key_name: 'frontend-prod', model: 'company-chat',
        stage: 'input', detector_ref: 'builtin:pii_secrets', action: 'redact',
        mode: 'shadow', failure_mode: 'degraded_allow', mandatory: false,
        config: { entities: ['email'] },
      }, {
        id: 9, name: 'platform-mandatory', version: 1,
        stage: 'input', detector_ref: 'builtin:pii_secrets', action: 'block',
        mode: 'enforce', failure_mode: 'fail_closed', mandatory: true,
        config: {}, config_redacted: true,
      }],
      total: 2,
      approved_detectors: ['builtin:sensitive_words', 'builtin:pii_secrets'],
      external_deadline_ms: 1000,
      automatic_detector_retry: false,
      local_detector_max_bytes: 131072,
      external_detector_max_bytes: 262144,
      detector_max_texts: 256,
      stream_detector_capabilities: {
        'builtin:sensitive_words': { synchronous: true, lookbehind_bytes: 4096 },
        'builtin:pii_secrets': { synchronous: true, lookbehind_bytes: 512 },
      },
      stream_enforce_buffer_max_bytes: 4096,
      stream_enforce_buffer_max_wait_ms: 200,
      stream_checkpoint_interval_ms: 1000,
    });
    mocks.getKeys.mockResolvedValue({
      data: [{ id: 15, name: 'frontend-prod', key_prefix: 'ur-prod', org_id: 22, org_name: 'Acme', project_id: 9, project_name: 'Customer API' }],
      total: 1,
    });
    mocks.getOrganizations.mockResolvedValue({ data: [{ id: 22, name: 'Acme', status: 'active' }], total: 1 });
    mocks.getProjects.mockResolvedValue({ data: [{ id: 9, org_id: 22, name: 'Customer API', status: 'active' }], total: 1 });
    mocks.getModelNames.mockResolvedValue({ data: ['company-chat'] });
    mocks.getSensitiveWords.mockResolvedValue({ data: ['blocked', 'restricted'] });
    mocks.replaceSensitiveWords.mockResolvedValue({ data: ['blocked', 'new-word'] });
  });

  afterEach(() => vi.restoreAllMocks());

  it('explains safe rollout, failure, streaming, and output-cost behavior in plain language', async () => {
    useAuthStore.setState({ user: { id: 2, username: 'org-admin', role: 'org_admin', org_id: 22, org_name: 'Acme' } });
    render(<GuardrailPoliciesPage />);

    expect(await screen.findByText('redact-email')).toBeTruthy();
    await waitFor(() => expect(mocks.getGuardrailPolicies).toHaveBeenCalledWith(1, 20));
    expect(screen.getByText(/推荐从影子观察开始/)).toBeTruthy();
    expect(screen.getByText(/外部检测器必须先由部署管理员登记/)).toBeTruthy();
    expect(screen.getByText(/外部检测会传输内容/)).toBeTruthy();
    expect(screen.getByText(/最多等待 1 秒且不自动重试/)).toBeTruthy();
    expect(screen.getByText(/每次策略评估最多检查 256 个文本单元/)).toBeTruthy();
    expect(screen.getByText(/本地 PII\/密钥检测最多处理 128 KiB/)).toBeTruthy();
    expect(screen.getByText(/流式输出不是零泄露承诺/)).toBeTruthy();
    expect(screen.getByText(/最多聚合 200 毫秒或 4 KiB，先到即关闭本批/)).toBeTruthy();
    expect(screen.getByText(/单个标准化模型数据帧超过该上限会在交付前安全中断/)).toBeTruthy();
    expect(screen.getByText(/实例故障后明确标记中断/)).toBeTruthy();
    expect(screen.getByText(/未交付的被阻断内容不向客户收费/)).toBeTruthy();
    expect(screen.getByText(/无法识别的结构化输出不会假装检查通过/)).toBeTruthy();
    expect(screen.getByText('Acme · Customer API · 凭证 frontend-prod · 模型 company-chat')).toBeTruthy();
    expect(screen.getByText('平台只读')).toBeTruthy();
    expect(screen.queryByRole('button', { name: '编辑安全策略 platform-mandatory' })).toBeNull();
    expect(mocks.getSensitiveWords).not.toHaveBeenCalled();
    expect(screen.queryByText('全局输入阻断词库')).toBeNull();
  });

  it('keeps billing administrators read-only', async () => {
    useAuthStore.setState({ user: { id: 3, username: 'billing', role: 'billing_admin', org_id: 22 } });
    render(<GuardrailPoliciesPage />);
    expect(await screen.findByText('redact-email')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /新增策略/ })).toBeNull();
    expect(screen.queryByText('操作')).toBeNull();
  });

  it('sends the loaded version when saving an edit', async () => {
    useAuthStore.setState({ user: { id: 2, username: 'org-admin', role: 'org_admin', org_id: 22, org_name: 'Acme' } });
    mocks.updateGuardrailPolicy.mockResolvedValue({ id: 8, version: 3, runtime_snapshot_applied: true });
    render(<GuardrailPoliciesPage />);
    fireEvent.click(await screen.findByRole('button', { name: '编辑安全策略 redact-email' }));
    expect(await screen.findByText('编辑安全策略：redact-email')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '保存并生成新版本' }));
    await waitFor(() => expect(mocks.updateGuardrailPolicy).toHaveBeenCalledWith(
      8,
      expect.objectContaining({ version: 2, name: 'redact-email', mode: 'shadow', action: 'redact' }),
    ));
  });

  it('lets only a platform administrator replace the global input blocking words after confirmation', async () => {
    useAuthStore.setState({ user: {
      id: 1, username: 'platform', role: 'platform_admin', org_id: 1,
      customer_context: { state: 'ready', org_id: 22, org_name: 'Acme' },
    } });
    let confirmation: any;
    const confirmSpy = vi.spyOn(Modal, 'confirm').mockImplementation((config: any) => {
      confirmation = config;
      return {} as any;
    });

    render(<GuardrailPoliciesPage />);

    const editor = await screen.findByRole('textbox', { name: '全局输入阻断词库' });
    expect(mocks.getSensitiveWords).toHaveBeenCalledTimes(1);
    expect((editor as HTMLTextAreaElement).value).toBe('blocked\nrestricted');
    expect(screen.getByText(/修改会影响当前部署所有项目和访问凭证/)).toBeTruthy();
    expect(mocks.getOrganizations).not.toHaveBeenCalled();
    expect(mocks.getProjects).toHaveBeenCalledWith(1, 100, { org_id: 22 });

    fireEvent.change(editor, { target: { value: ' blocked \nnew-word\nblocked\n' } });
    fireEvent.click(screen.getByRole('button', { name: '保存并替换全局词库' }));
    expect(confirmSpy).toHaveBeenCalledTimes(1);
    expect(confirmation.title).toBe('确认替换全局输入阻断词库？');
    expect(confirmation.content).toContain('完整替换部署级词库');

    await act(async () => { await confirmation.onOk(); });
    await waitFor(() => expect(mocks.replaceSensitiveWords).toHaveBeenCalledWith(['blocked', 'new-word']));
    expect((editor as HTMLTextAreaElement).value).toBe('blocked\nnew-word');
  });

  it('shows a platform word-list load failure instead of an empty editable list', async () => {
    useAuthStore.setState({ user: {
      id: 1, username: 'platform', role: 'platform_admin', org_id: 1,
      customer_context: { state: 'ready', org_id: 22, org_name: 'Acme' },
    } });
    mocks.getSensitiveWords.mockRejectedValue(new Error('词库服务不可用'));

    render(<GuardrailPoliciesPage />);

    expect(await screen.findByText('词库服务不可用')).toBeTruthy();
    expect(screen.queryByRole('textbox', { name: '全局输入阻断词库' })).toBeNull();
    expect(screen.getByRole('button', { name: '重新加载词库' })).toBeTruthy();
  });

  it('keeps the edited words visible and reports a failed global replacement', async () => {
    useAuthStore.setState({ user: {
      id: 1, username: 'platform', role: 'platform_admin', org_id: 1,
      customer_context: { state: 'ready', org_id: 22, org_name: 'Acme' },
    } });
    mocks.replaceSensitiveWords.mockRejectedValue(new Error('词库保存被服务拒绝'));
    let confirmation: any;
    vi.spyOn(Modal, 'confirm').mockImplementation((config: any) => {
      confirmation = config;
      return {} as any;
    });

    render(<GuardrailPoliciesPage />);

    const editor = await screen.findByRole('textbox', { name: '全局输入阻断词库' });
    fireEvent.change(editor, { target: { value: 'unsaved-word' } });
    fireEvent.click(screen.getByRole('button', { name: '保存并替换全局词库' }));
    await act(async () => { await expect(confirmation.onOk()).rejects.toThrow('词库保存被服务拒绝'); });

    expect(await screen.findByText('词库保存被服务拒绝')).toBeTruthy();
    expect(screen.getByRole('textbox', { name: '全局输入阻断词库' })).toHaveValue('unsaved-word');
  });

  it('defaults platform creation to the canonical customer and preserves an existing deployment-global rule', async () => {
    useAuthStore.setState({ user: {
      id: 1, username: 'platform', role: 'platform_admin', org_id: 99,
      customer_context: { state: 'ready', org_id: 22, org_name: 'Acme' },
    } });
    mocks.createGuardrailPolicy.mockResolvedValue({ id: 10, version: 1, runtime_snapshot_applied: true });
    mocks.updateGuardrailPolicy.mockResolvedValue({ id: 9, version: 2, runtime_snapshot_applied: true });

    render(<GuardrailPoliciesPage />);
    await screen.findByText('redact-email');
    expect(mocks.getOrganizations).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: /新增策略/ }));
    fireEvent.change(await screen.findByPlaceholderText('customer-pii-redaction'), { target: { value: 'customer-default' } });
    fireEvent.click(screen.getByRole('button', { name: '创建策略' }));
    await waitFor(() => expect(mocks.createGuardrailPolicy).toHaveBeenCalledWith(expect.objectContaining({
      name: 'customer-default', org_id: 22, mandatory: false,
    })));

    fireEvent.click(screen.getByRole('button', { name: '编辑安全策略 platform-mandatory' }));
    fireEvent.click(await screen.findByRole('button', { name: '保存并生成新版本' }));
    await waitFor(() => expect(mocks.updateGuardrailPolicy).toHaveBeenCalledWith(9, expect.objectContaining({
      org_id: 0, project_id: 0, api_key_id: 0, mandatory: true,
    })));
  });

  it('fails closed without making customer-domain calls when the canonical customer is ambiguous', async () => {
    useAuthStore.setState({ user: {
      id: 1, username: 'platform', role: 'platform_admin', org_id: 99,
      customer_context: { state: 'ambiguous' },
    } });

    render(<GuardrailPoliciesPage />);

    expect((await screen.findAllByRole('heading', { name: '客户组织上下文不唯一' })).length).toBeGreaterThan(0);
    expect(mocks.getGuardrailPolicies).not.toHaveBeenCalled();
    expect(mocks.getKeys).not.toHaveBeenCalled();
    expect(mocks.getOrganizations).not.toHaveBeenCalled();
    expect(mocks.getProjects).not.toHaveBeenCalled();
    expect(mocks.getModelNames).not.toHaveBeenCalled();
    expect(mocks.getSensitiveWords).not.toHaveBeenCalled();
  });
});
