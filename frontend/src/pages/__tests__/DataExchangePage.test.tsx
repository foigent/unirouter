import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import DataExchangePage, { receiptPayload } from '../DataExchangePage';

const getItems = vi.fn();
const getExchanges = vi.fn();
const getActivity = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getDataExchangeItems: (...args: any[]) => getItems(...args),
    getDataExchanges: (...args: any[]) => getExchanges(...args),
    getDataExchangeActivity: (...args: any[]) => getActivity(...args),
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
  });

  it('renders the metadata-only safety boundary and approval queue', async () => {
    const { container } = render(<DataExchangePage />);
    await waitFor(() => expect(getItems).toHaveBeenCalledWith(1, 100, undefined));
    expect(container.textContent).toContain('metadata only');
    expect(container.textContent).toContain('无原始 prompt/response');
    expect(container.textContent).toContain('req-controlled-001');
    expect(container.textContent).toContain('已批准');
  });

  it('shows source and downstream statuses separately in the ledger', async () => {
    render(<DataExchangePage />);
    await waitFor(() => expect(getExchanges).toHaveBeenCalled());
    fireEvent.click(screen.getByText('交换账本'));
    await waitFor(() => {
      expect(screen.getByText('uxchg_test_001')).toBeTruthy();
      expect(screen.getByText('handed_off')).toBeTruthy();
      expect(screen.getByText('not_received')).toBeTruthy();
    });
  });

  it('loads member, delivery, and receipt history from the activity endpoint', async () => {
    render(<DataExchangePage />);
    fireEvent.click(await screen.findByText('交换账本'));
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
});
