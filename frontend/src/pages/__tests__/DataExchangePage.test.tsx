import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import DataExchangePage, { receiptPayload } from '../DataExchangePage';
import { parseReceiverRegistrationBundle } from '../XInsightLiveConnection';
import { useAuthStore } from '../../store/auth';

const getItems = vi.fn();
const getExchanges = vi.fn();
const getActivity = vi.fn();
const getProjects = vi.fn();
const getFeedConnection = vi.fn();
const pauseConnection = vi.fn();
const resumeConnection = vi.fn();
const prepareRotation = vi.fn();
const activateRotation = vi.fn();
const createFeedCredential = vi.fn();

const licensedAccess = {
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
} as const;

vi.mock('../../api/client', () => ({
  api: {
    getDataExchangeItems: (...args: any[]) => getItems(...args),
    getDataExchanges: (...args: any[]) => getExchanges(...args),
    getDataExchangeActivity: (...args: any[]) => getActivity(...args),
    getProjects: (...args: any[]) => getProjects(...args),
    getXInsightFeedConnection: (...args: any[]) => getFeedConnection(...args),
    pauseXInsightFeedConnection: (...args: any[]) => pauseConnection(...args),
    resumeXInsightFeedConnection: (...args: any[]) => resumeConnection(...args),
    prepareXInsightFeedRotation: (...args: any[]) => prepareRotation(...args),
    activateXInsightFeedRotation: (...args: any[]) => activateRotation(...args),
    createXInsightFeedCredential: (...args: any[]) => createFeedCredential(...args),
    activateInitialXInsightFeedCredential: vi.fn(),
    cancelXInsightFeedRotation: vi.fn(),
    revokeXInsightFeedCredential: vi.fn(),
    createDataExchangeItem: vi.fn(),
    updateDataExchangeItem: vi.fn(),
    createDataExchange: vi.fn(),
    createDataExchangeDelivery: vi.fn(),
    createDataExchangeReceipt: vi.fn(),
    markDataExchangeArtifactDeleted: vi.fn(),
  },
}));

describe('DataExchangePage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.setState({
      user: {
        id: 1,
        username: 'platform',
        role: 'platform_admin',
        org_id: 1,
        license_access: licensedAccess,
      },
    });
    getItems.mockResolvedValue({
      data: [{
        id: 7,
        approval_mode: 'exact_request',
        request_id: 'req-controlled-001',
        scenario_label: 'model denied',
        source_instance: 'dev-a',
        data_classes: ['request_trace_events', 'capture_gaps'],
        safety_profile: 'metadata_only',
        approval_status: 'approved',
        reason: 'controlled evidence',
        created_at: '2026-07-10T08:00:00Z',
      }],
      total: 1,
    });
    getExchanges.mockResolvedValue({
      data: [{
        id: 9,
        exchange_id: 'uxchg_test_001',
        downstream_system: 'x-insight',
        source_instance: 'dev-a',
        schema_version: 'unirouter.prd5.xinsight.snapshot.v1',
        approval_manifest_sha256: `sha256:${'a'.repeat(64)}`,
        artifact_sha256: `sha256:${'b'.repeat(64)}`,
        artifact_ref: 'evidence/snapshot.json',
        workflow_status: 'handed_off',
        receipt_status: 'not_received',
        request_count: 1,
        input_event_count: 2,
        item_ids: [7],
        delivery_count: 1,
        warning_codes: [],
        generated_at: '2026-07-10T08:00:00Z',
      }],
      total: 1,
    });
    getActivity.mockResolvedValue({
      members: [{ id: 7, request_id: 'req-controlled-001', scenario_label: 'model denied', approval_status: 'approved' }],
      deliveries: [{ id: 3, action: 'handed_off', handoff_ref: 'evidence/handoff.md', delivered_at: '2026-07-10T08:10:00Z' }],
      receipts: [{ id: 4, receipt_status: 'persisted', input_events: 2, accepted: 2, rerun_duplicate_noop: 2, rejected: 0, checked_at: '2026-07-10T08:20:00Z' }],
    });
    getProjects.mockResolvedValue({
      data: [{ id: 22, name: 'Development', org_name: 'UniRouter', status: 'active' }],
      total: 1,
    });
    getFeedConnection.mockResolvedValue({
      project_id: 22,
      project_name: 'Development',
      project_status: 'active',
      global_configured: true,
      global_enabled: true,
      source_instance: 'unirouter-dev-149a8220bf1e',
      connection_status: 'paused',
      active_credential: { id: 41, key_prefix: 'ur-feed-active', lifecycle: 'active', created_at: '2026-07-25T08:00:00Z' },
      staged_credential: null,
      last_revoked_credential: null,
      last_authenticated_pull_at: '2026-07-25T08:10:00Z',
      source_high_watermark: 'cursor-high',
      receiver_acknowledged_cursor: 'cursor-ack',
      receiver_acknowledgement_semantics: 'next_requested_cursor_after_receiver_persistence',
      pending_item_count: 2,
      pending_item_count_capped: false,
      oldest_pending_at: '2026-07-25T08:08:00Z',
      oldest_pending_age_seconds: 120,
      cursor_outside_retention: false,
      connector_product_status: 'connected',
      connector_installation: {
        schema_version: 'unirouter.xinsight.connector-installation.v1',
        installation_id: 'installation-development',
        x_deployment_id: 'x-development',
        event_scope: 'model.request.completed',
        source_instance: 'unirouter-dev-149a8220bf1e',
        source_binding_sha256: `sha256:${'c'.repeat(64)}`,
        desired_state: 'active',
        observed_state: 'connected',
        state_revision: 2,
        control_fence: 1,
        last_successful_sync_at: '2026-07-25T08:10:00Z',
        consented_at: '2026-07-25T08:00:00Z',
        updated_at: '2026-07-25T08:10:00Z',
      },
      connector_latest_operation: {
        schema_version: 'unirouter.xinsight.connector-operation.v1',
        operation_ref: `xcop_${'d'.repeat(32)}`,
        installation_id: 'installation-development',
        action: 'connect',
        desired_state: 'active',
        state: 'succeeded',
        requested_revision: 1,
        control_fence: 1,
        attempt_count: 1,
        created_at: '2026-07-25T08:00:00Z',
        updated_at: '2026-07-25T08:10:00Z',
      },
    });
    createFeedCredential.mockResolvedValue({
      id: 42,
      key_prefix: 'ur-xf1-0123456789abc',
      lifecycle: 'staged',
      project_id: 22,
      purpose: 'xinsight.feed.read',
      key_type: 'service',
      verifier_scheme: 'receiver_sha256_v1',
      operation_id: 'b'.repeat(32),
    });
    prepareRotation.mockResolvedValue({
      id: 42,
      key_prefix: 'ur-xf1-0123456789abc',
      lifecycle: 'staged',
      project_id: 22,
      purpose: 'xinsight.feed.read',
      key_type: 'service',
      verifier_scheme: 'receiver_sha256_v1',
      operation_id: 'b'.repeat(32),
    });
  });

  it('defaults to the product live connection and labels receiver acknowledgement truthfully', async () => {
    const { container } = render(<DataExchangePage />);
    await waitFor(() => expect(getFeedConnection).toHaveBeenCalledWith(22));
    expect(getProjects).toHaveBeenCalledWith(1, 100);
    expect(container.textContent).toContain('metadata only');
    expect(container.textContent).toContain('无原始 prompt/response');
    expect(container.textContent).toContain('接收端提交确认 cursor');
    expect(container.textContent).toContain('不代表搜索、API 或 UI 已可见');
    expect(container.textContent).toContain('systemctl restart（不可 reload）');
    expect(container.textContent).toContain('receiver-generated credential');
    expect(container.textContent).toContain('Connector 状态');
    expect(container.textContent).toContain('已连接');
    expect(container.textContent).toContain('最旧待同步延迟');
    expect(container.textContent).toContain('connect · succeeded');
    expect(container.textContent).toContain('高级 Source / 凭证诊断');
  });

  it('registers only the strict non-secret receiver bundle and never renders a plaintext key surface', async () => {
    const registration = {
      schema_version: 'unirouter.xinsight.feed-credential-registration.v1',
      token_format: 'ur-xf1-hex256.v1',
      verifier_scheme: 'receiver_sha256_v1',
      key_prefix: 'ur-xf1-0123456789abc',
      verifier_sha256: 'a'.repeat(64),
      operation_id: 'b'.repeat(32),
    } as const;
    getFeedConnection.mockResolvedValueOnce({
      project_id: 22,
      project_name: 'Development',
      project_status: 'active',
      global_configured: true,
      global_enabled: true,
      source_instance: 'unirouter-dev-149a8220bf1e',
      connection_status: 'paused',
      active_credential: null,
      staged_credential: null,
      pending_item_count: 0,
    });
    const { container } = render(<DataExchangePage />);
    const registrationInput = await screen.findByLabelText('X-Insight feed credential registration bundle');
    fireEvent.change(registrationInput, { target: { value: JSON.stringify(registration) } });
    fireEvent.change(screen.getByLabelText('X-Insight worker exact IP'), {
      target: { value: '10.0.0.8/32' },
    });
    fireEvent.click(screen.getByRole('button', { name: '登记初始 staged 凭证' }));

    await waitFor(() => expect(createFeedCredential).toHaveBeenCalledWith(22, {
      allowed_ip: '10.0.0.8/32',
      registration,
    }));
    expect(container.textContent).toContain('UniRouter 不接收、不返回、也不展示明文 token');
    expect(screen.queryByLabelText('一次性 feed key')).toBeNull();
    expect(container.textContent).not.toContain('关闭后不可再次读取明文');
  });

  it('rejects noncanonical or extended receiver registration bundles client-side', () => {
    const valid = JSON.stringify({
      schema_version: 'unirouter.xinsight.feed-credential-registration.v1',
      token_format: 'ur-xf1-hex256.v1',
      verifier_scheme: 'receiver_sha256_v1',
      key_prefix: 'ur-xf1-0123456789abc',
      verifier_sha256: 'a'.repeat(64),
      operation_id: 'b'.repeat(32),
    });
    expect(parseReceiverRegistrationBundle(valid).key_prefix).toBe('ur-xf1-0123456789abc');
    expect(() => parseReceiverRegistrationBundle(valid.replace(/a/g, 'A'))).toThrow();
    expect(() => parseReceiverRegistrationBundle(
      valid.replace(/}$/, ',"unknown":true}'),
    )).toThrow('字段必须与 Frozen Registration Bundle V1 完全一致');
  });

  it('keeps the manual approval flow in a clearly historical tab', async () => {
    const { container } = render(<DataExchangePage />);
    fireEvent.click(await screen.findByText('历史人工审批'));
    await waitFor(() => expect(getItems).toHaveBeenCalledWith(1, 100, undefined));
    expect(container.textContent).toContain('req-controlled-001');
    expect(container.textContent).toContain('已批准');
  });

  it('shows source and downstream statuses separately in the ledger', async () => {
    render(<DataExchangePage />);
    fireEvent.click(screen.getByText('历史交换账本'));
    await waitFor(() => expect(getExchanges).toHaveBeenCalled());
    await waitFor(() => {
      expect(screen.getByText('uxchg_test_001')).toBeTruthy();
      expect(screen.getByText('handed_off')).toBeTruthy();
      expect(screen.getByText('not_received')).toBeTruthy();
    });
  });

  it('defaults new exchanges to the micro-CNY snapshot v2 contract', async () => {
    render(<DataExchangePage />);
    fireEvent.click(await screen.findByText('历史人工审批'));
    fireEvent.click(await screen.findByRole('button', { name: '登记交换制品' }));
    expect(await screen.findByDisplayValue('unirouter.prd5.xinsight.snapshot.v2')).toBeTruthy();
  });

  it('loads member, delivery, and receipt history from the activity endpoint', async () => {
    render(<DataExchangePage />);
    fireEvent.click(await screen.findByText('历史交换账本'));
    const preview = await screen.findByRole('button', { name: '查看交换' });
    fireEvent.click(preview);

    await waitFor(() => expect(getActivity).toHaveBeenCalledWith(9));
    expect(await screen.findByText('纳入候选')).toBeTruthy();
    expect(screen.getByText('交付历史')).toBeTruthy();
    expect(screen.getByText('回执历史')).toBeTruthy();
    expect(screen.getByText('evidence/handoff.md')).toBeTruthy();
    expect(screen.getByText('2 / 2 / 2 / 0')).toBeTruthy();
  });

  it('rejects unimplemented X-Insight durable job fields in a pasted receipt', () => {
    expect(() => receiptPayload(JSON.stringify({
      import_job_id: '',
    }))).toThrow('X-Insight 尚未提供 durable import job 或 deep link 合同');
  });

  it('renders two-phase rotation only while paused and requires restart plus staged 503', async () => {
    getFeedConnection.mockResolvedValueOnce({
      project_id: 22,
      project_name: 'Development',
      project_status: 'active',
      global_configured: true,
      global_enabled: true,
      source_instance: 'unirouter-dev-149a8220bf1e',
      connection_status: 'paused',
      active_credential: { id: 41, key_prefix: 'ur-feed-active', lifecycle: 'active', created_at: '2026-07-25T08:00:00Z' },
      staged_credential: { id: 42, key_prefix: 'ur-feed-staged', lifecycle: 'staged', created_at: '2026-07-25T08:20:00Z' },
      pending_item_count: 0,
    });
    const { container } = render(<DataExchangePage />);
    await waitFor(() => expect(container.textContent).toContain('确认重启与 503，激活轮换'));
    expect(container.textContent).toContain('systemctl restart（不可 reload）');
    expect(container.textContent).toContain('旧凭证保持 active');
    expect(container.textContent).not.toContain('撤销当前 feed 凭证');
  });

  it('allows an undelivered initial staged credential to be revoked without an active key', async () => {
    getFeedConnection.mockResolvedValueOnce({
      project_id: 22,
      project_name: 'Development',
      project_status: 'active',
      global_configured: true,
      global_enabled: true,
      source_instance: 'unirouter-dev-149a8220bf1e',
      connection_status: 'paused',
      active_credential: null,
      staged_credential: { id: 42, key_prefix: 'ur-feed-staged', lifecycle: 'staged', created_at: '2026-07-25T08:20:00Z' },
      pending_item_count: 0,
    });
    const { container } = render(<DataExchangePage />);
    await waitFor(() => expect(container.textContent).toContain('撤销未安装的 staged 凭证'));
    expect(container.textContent).toContain('激活初始凭证');
  });

  it('keeps only safety controls and locks legacy flows without the Data Exchange entitlement', async () => {
    useAuthStore.setState({
      user: {
        id: 1,
        username: 'platform',
        role: 'platform_admin',
        org_id: 1,
        license_access: {
          ...licensedAccess,
          status: 'expired',
          enforceable: false,
          features: { ...licensedAccess.features, data_exchange: false },
        },
      },
    });
    const { container } = render(<DataExchangePage />);

    await waitFor(() => expect(container.textContent).toContain('仅保留安全控制'));
    expect(screen.getByRole('button', { name: '恢复连接' }).hasAttribute('disabled')).toBe(true);
    expect(screen.getByRole('button', { name: '准备两阶段轮换' }).hasAttribute('disabled')).toBe(true);
    expect(screen.getByRole('button', { name: '撤销当前 feed 凭证' }).hasAttribute('disabled')).toBe(false);
    expect(screen.getByRole('button', { name: /刷新历史账本/ }).hasAttribute('disabled')).toBe(true);

    fireEvent.click(screen.getByText('历史人工审批（授权锁定）'));
    expect(getItems).not.toHaveBeenCalled();
    expect(container.textContent).toContain('扩张动作和历史人工流程已锁定');
  });

  it('keeps pause and revoke reachable for a disabled project loaded without an active-only filter', async () => {
    getProjects.mockResolvedValueOnce({
      data: [{ id: 22, name: 'Development', org_name: 'UniRouter', status: 'disabled' }],
      total: 1,
    });
    getFeedConnection.mockResolvedValueOnce({
      project_id: 22,
      project_name: 'Development',
      project_status: 'disabled',
      global_configured: true,
      global_enabled: true,
      source_instance: 'unirouter-dev-149a8220bf1e',
      connection_status: 'active',
      active_credential: { id: 41, key_prefix: 'ur-feed-active', lifecycle: 'active', created_at: '2026-07-25T08:00:00Z' },
      staged_credential: null,
      pending_item_count: 0,
    });
    const { container } = render(<DataExchangePage />);

    await waitFor(() => expect(container.textContent).toContain('项目已停用'));
    expect(container.textContent).toContain('Development · UniRouter · 已停用');
    expect(screen.getByRole('button', { name: '暂停连接' }).hasAttribute('disabled')).toBe(false);
    expect(screen.getByRole('button', { name: '撤销当前 feed 凭证' }).hasAttribute('disabled')).toBe(false);
    expect(getProjects).toHaveBeenCalledWith(1, 100);
  });

  it('labels archived projects and keeps cancel while blocking rotation activation', async () => {
    getProjects.mockResolvedValueOnce({
      data: [{ id: 22, name: 'Archive', org_name: 'UniRouter', status: 'archived' }],
      total: 1,
    });
    getFeedConnection.mockResolvedValueOnce({
      project_id: 22,
      project_name: 'Archive',
      project_status: 'archived',
      global_configured: true,
      global_enabled: true,
      source_instance: 'unirouter-dev-149a8220bf1e',
      connection_status: 'paused',
      active_credential: { id: 41, key_prefix: 'ur-feed-active', lifecycle: 'active', created_at: '2026-07-25T08:00:00Z' },
      staged_credential: { id: 42, key_prefix: 'ur-feed-staged', lifecycle: 'staged', created_at: '2026-07-25T08:20:00Z' },
      pending_item_count: 0,
    });
    const { container } = render(<DataExchangePage />);

    await waitFor(() => expect(container.textContent).toContain('项目已归档'));
    expect(container.textContent).toContain('Archive · UniRouter · 已归档');
    expect(screen.getByRole('button', { name: '确认重启与 503，激活轮换' }).hasAttribute('disabled')).toBe(true);
    expect(screen.getByRole('button', { name: '取消轮换' }).hasAttribute('disabled')).toBe(false);
  });
});
