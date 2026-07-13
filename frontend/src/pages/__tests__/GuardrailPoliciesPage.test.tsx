import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import GuardrailPoliciesPage from '../GuardrailPoliciesPage';
import { useAuthStore } from '../../store/auth';

const mocks = vi.hoisted(() => ({
  getGuardrailPolicies: vi.fn(),
  createGuardrailPolicy: vi.fn(),
  updateGuardrailPolicy: vi.fn(),
  getKeys: vi.fn(),
  getModelNames: vi.fn(),
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
    mocks.getModelNames.mockResolvedValue({ data: ['company-chat'] });
  });

  it('explains safe rollout, failure, streaming, and output-cost behavior in plain language', async () => {
    useAuthStore.setState({ user: { id: 2, username: 'org-admin', role: 'org_admin', org_id: 22, org_name: 'Acme' } });
    render(<GuardrailPoliciesPage />);

    expect(await screen.findByText('redact-email')).toBeTruthy();
    await waitFor(() => expect(mocks.getGuardrailPolicies).toHaveBeenCalledWith(1, 20));
    expect(screen.getByText(/推荐从影子观察开始/)).toBeTruthy();
    expect(screen.getByText(/外部检测器必须先由平台注册/)).toBeTruthy();
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
});
