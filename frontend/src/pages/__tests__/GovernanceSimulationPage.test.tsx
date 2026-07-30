import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import GovernanceSimulationPage from '../GovernanceSimulationPage';

const mockGetContract = vi.fn();
const mockPreview = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getGovernanceSimulationContract: (...args: unknown[]) => mockGetContract(...args),
    previewGovernanceSimulation: (...args: unknown[]) => mockPreview(...args),
  },
}));

const SAMPLE_REQUEST = {
  contract_version: 'unirouter.governance-simulation.v1',
  simulation_id: 'sim-ui-001',
};

const REPORT = {
  report_version: 'unirouter.governance-simulation-report.v1',
  contract_version: 'unirouter.governance-simulation.v1',
  simulation_id: 'sim-ui-001',
  input_payload_hash: `sha256:${'1'.repeat(64)}`,
  input_semantic_hash: `sha256:${'2'.repeat(64)}`,
  evaluation_mode: 'shadow',
  baseline: {
    snapshot_id: 'baseline',
    snapshot_hash: `sha256:${'3'.repeat(64)}`,
    candidates: [{ channel_id: '1', channel_name: 'primary', provider: 'openai', key_id: '11', priority: 10, weight: 100, order: 1, reason: 'priority_failover' }],
    exclusions: [],
  },
  candidate: {
    snapshot_id: 'candidate',
    snapshot_hash: `sha256:${'4'.repeat(64)}`,
    candidates: [{ channel_id: '2', channel_name: 'secondary', provider: 'azure', key_id: '22', priority: 20, weight: 100, order: 1, reason: 'priority_failover' }],
    exclusions: [],
  },
  route_changes: [{
    field: 'route/candidate_order',
    baseline: '1:11',
    candidate: '2:22',
    risk: 'review_required',
    reason: 'deterministic_candidate_order_changed',
  }],
  provider_pack_changes: [{
    pack_id: 'unirouter.openai.core',
    baseline_version: '1.0.0',
    candidate_version: '1.1.0',
    risk: 'review_required',
    reasons: ['endpoint_added', 'fixture_added'],
  }],
  summary: { unchanged: 0, compatible: 0, review_required: 2, breaking: 0 },
  overall_risk: 'review_required',
  release: {
    release_id: 'release-ui-001',
    revision: '1',
    action: 'advance',
    from_stage: 'draft',
    simulated_stage: 'validated',
    rollback_to_revision: '',
    audit_event_id: `sha256:${'5'.repeat(64)}`,
    decision: 'simulated_only',
    promotion_gate: 'external_explicit_authority_required',
    runtime_promotion_authorized: false,
    runtime_mutation: 'none',
  },
  runtime_mutation: 'none',
};

describe('GovernanceSimulationPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetContract.mockResolvedValue({
      contract_version: 'unirouter.governance-simulation.v1',
      report_version: 'unirouter.governance-simulation-report.v1',
      release_version: 'unirouter.progressive-release.v1',
      policy_version: 'routing-config.v2',
      selection_version: 'routing-policy.v1',
      sample_request: SAMPLE_REQUEST,
      artifacts: {
        schema: { path: 'schema.json', sha256: `sha256:${'6'.repeat(64)}` },
        fixtures: [],
        golden_report: { path: 'report.json', sha256: `sha256:${'7'.repeat(64)}` },
      },
    });
    mockPreview.mockResolvedValue(REPORT);
  });

  it('loads the safe fixture and keeps the right inspector visible before a run', async () => {
    render(<GovernanceSimulationPage />);

    expect(screen.getByLabelText('治理变更检查器')).toBeTruthy();
    await waitFor(() => expect(mockGetContract).toHaveBeenCalledTimes(1));
    expect(screen.getByLabelText('治理模拟 JSON')).toHaveValue(
      JSON.stringify(SAMPLE_REQUEST, null, 2),
    );
    expect(screen.getByText('等待离线模拟')).toBeTruthy();
  });

  it('runs the exact local input and auto-selects the first Provider change', async () => {
    render(<GovernanceSimulationPage />);
    const input = await screen.findByLabelText('治理模拟 JSON');
    await waitFor(() => expect(input).toHaveValue(JSON.stringify(SAMPLE_REQUEST, null, 2)));

    fireEvent.click(screen.getByRole('button', { name: '运行离线模拟' }));

    await waitFor(() => expect(mockPreview).toHaveBeenCalledWith(JSON.stringify(SAMPLE_REQUEST, null, 2)));
    expect(await screen.findByText('unirouter.openai.core')).toBeTruthy();
    expect(screen.getByText(/unirouter.openai.core · 需要评审/)).toBeTruthy();
    expect(screen.getByText('endpoint_added、fixture_added')).toBeTruthy();
    expect(screen.getByText('none')).toBeTruthy();
    expect(screen.getByText('否')).toBeTruthy();
  });

  it('does not claim that enforced simulation authorizes runtime promotion', async () => {
    mockPreview.mockResolvedValue({
      ...REPORT,
      evaluation_mode: 'enforced',
      release: { ...REPORT.release, runtime_promotion_authorized: false },
    });
    render(<GovernanceSimulationPage />);
    await waitFor(() => expect(mockGetContract).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('button', { name: '运行离线模拟' }));

    expect(await screen.findByText('enforced')).toBeTruthy();
    expect(screen.getByText('external_explicit_authority_required')).toBeTruthy();
    expect(screen.getByText('否')).toBeTruthy();
  });
});
