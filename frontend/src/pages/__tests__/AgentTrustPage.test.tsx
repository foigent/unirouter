import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import AgentTrustPage from '../AgentTrustPage';

const mocks = vi.hoisted(() => ({
  getAgentTrustContract: vi.fn(),
  previewAgentTrust: vi.fn(),
}));

vi.mock('../../api/client', async () => {
  const actual = await vi.importActual<any>('../../api/client');
  return { ...actual, api: mocks };
});

const rootRef = `sha256:${'b'.repeat(64)}`;
const revokedRef = `sha256:${'d'.repeat(64)}`;

const SAMPLE = {
  contract_version: 'unirouter.agent-trust-review.v1',
  review_id: 'review-001',
};

const limits = {
  max_depth: 3,
  max_children: 4,
  max_descendants: 12,
  cost_budget_cents: 5000,
  token_budget: 50000,
  rpm_limit: 60,
  rps_limit: 6,
  concurrency_limit: 4,
  allowed_models: ['gpt-4.1'],
  allowed_endpoints: ['/v1/chat/completions'],
};

const REPORT = {
  report_version: 'unirouter.agent-trust-report.v1',
  review_id: 'review-001',
  tenant_ref: `sha256:${'a'.repeat(64)}`,
  authority_snapshot_hash: `sha256:${'1'.repeat(64)}`,
  effective_authority_tree: [
    {
      agent_ref: rootRef,
      parent_agent_ref: '',
      depth: 0,
      state: 'active',
      lineage: [rootRef],
      effective_limits: limits,
      remaining_ttl_seconds: '14100',
      revocation_event_ref: '',
    },
    {
      agent_ref: revokedRef,
      parent_agent_ref: rootRef,
      depth: 1,
      state: 'revoked',
      lineage: [rootRef, revokedRef],
      effective_limits: { ...limits, cost_budget_cents: 0, token_budget: 0, concurrency_limit: 0 },
      remaining_ttl_seconds: '3300',
      revocation_event_ref: `sha256:${'5'.repeat(64)}`,
    },
  ],
  attenuation_diffs: [{
    agent_ref: revokedRef,
    parent_agent_ref: rootRef,
    reduced_fields: ['cost_budget_cents'],
    removed_models: [],
    removed_endpoints: [],
  }],
  ancestor_budgets: [
    { agent_ref: rootRef, lineage: [rootRef], effective_limits: limits, ancestor_budget_refs: [rootRef], remaining_ttl_seconds: '14100' },
    { agent_ref: revokedRef, lineage: [rootRef, revokedRef], effective_limits: { ...limits, cost_budget_cents: 1000 }, ancestor_budget_refs: [rootRef, revokedRef], remaining_ttl_seconds: '3300' },
  ],
  revocation_timeline: [{ event_ref: `sha256:${'5'.repeat(64)}`, agent_ref: revokedRef, revoked_at: '2026-07-31T08:04:00.000000Z', reason: 'operator_revoked' }],
  digital_twin: {
    overall_risk: 'review_required',
    route_changes: [{ field: 'route' }],
    provider_pack_changes: [{ pack_id: 'unirouter.openai.core' }],
    release_decision: 'advance',
    synchronous_dependency: false,
    runtime_mutation: 'none',
  },
  receipt_verification: {
    receipt_id: 'review-001.receipt',
    key_id: 'fixture-key',
    key_version: '1',
    payload_hash: `sha256:${'9'.repeat(64)}`,
    cryptographically_valid: true,
    tenant_bound: true,
    audience_bound: true,
    decision_bound: true,
    trust_profile: 'fixture_local',
    runtime_trusted: false,
  },
  decision: 'shadow_only',
  synchronous_runtime_dependency: false,
  runtime_enforcement_authorized: false,
  runtime_mutation: 'none',
};

describe('AgentTrustPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getAgentTrustContract.mockResolvedValue({
      contract_version: 'unirouter.agent-trust-review.v1',
      report_version: 'unirouter.agent-trust-report.v1',
      receipt_version: 'unirouter.signed-decision-receipt.v1',
      sample_request: SAMPLE,
      artifacts: {
        schema: { path: 'schema.json', sha256: `sha256:${'2'.repeat(64)}` },
        fixtures: [],
        golden_report: { path: 'report.json', sha256: `sha256:${'3'.repeat(64)}` },
      },
      authority_boundary: {
        signing_key_accepted: false,
        fixture_public_key_runtime_trust: false,
        synchronous_runtime_dependency: false,
        runtime_enforcement_authorized: false,
        runtime_mutation: 'none',
      },
    });
    mocks.previewAgentTrust.mockResolvedValue(REPORT);
  });

  it('auto-runs the safe review and keeps the inspector present', async () => {
    render(<AgentTrustPage />);

    expect(screen.getByLabelText('Agent Trust 检查器')).toBeTruthy();
    await waitFor(() => expect(mocks.previewAgentTrust).toHaveBeenCalledWith(JSON.stringify(SAMPLE, null, 2)));
    expect(await screen.findByText('权限与收据证据')).toBeTruthy();
    expect(screen.getByText('密码学有效')).toBeTruthy();
    expect(screen.getByText('未授权（fixture）')).toBeTruthy();
    expect(screen.getAllByText('否').length).toBeGreaterThanOrEqual(2);
  });

  it('changes the selected Agent without a detail-open action', async () => {
    render(<AgentTrustPage />);
    await waitFor(() => expect(screen.getAllByText(/sha256:bbbb/).length).toBeGreaterThanOrEqual(1));

    fireEvent.click(screen.getByText(/sha256:dddd/));

    expect(await screen.findByText('cost_budget_cents')).toBeTruthy();
    expect(screen.getAllByText('已撤销').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('2 层共同扣减')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /查看|展开/ })).toBeNull();
  });
});
