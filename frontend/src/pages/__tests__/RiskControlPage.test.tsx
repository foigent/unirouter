import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import RiskControlPage from '../RiskControlPage';

const mocks = vi.hoisted(() => ({
  getRiskControlContract: vi.fn(),
  previewRiskControl: vi.fn(),
}));

vi.mock('../../api/client', async () => {
  const actual = await vi.importActual<any>('../../api/client');
  return { ...actual, api: mocks };
});

const digest = (value: string) => `sha256:${value.repeat(64)}`;
const SAMPLE = {
  contract_version: 'unirouter.risk-control-review.v1',
  review_id: 'risk-review-001',
};

const REPORT = {
  report_version: 'unirouter.risk-control-report.v1',
  review_id: 'risk-review-001',
  tenant_ref: digest('a'),
  input_semantic_hash: digest('1'),
  evidence_binding_hash: digest('2'),
  evidence: {
    agent_trust_decision: 'shadow_only',
    agent_trust_report_hash: digest('3'),
    receipt_payload_hash: digest('4'),
    receipt_cryptographically_valid: true,
    receipt_runtime_trusted: false,
    provenance_passport_hash: digest('5'),
    source_hash: digest('6'),
    envelope_hash: digest('7'),
    terminal_status: 'settled',
    capture_gap_count: 4,
    governance_request_hash: digest('8'),
    governance_overall_risk: 'review_required',
    source_scope: {
      org_ref: '101',
      project_ref: '202',
      source_instance: 'unirouter-development',
      scope_hash: digest('f'),
      tenant_binding_status: 'unverified_product_local_mapping',
    },
  },
  advice: 'shadow_only',
  dimensions: {
    authority: 'elevated',
    change: 'elevated',
    provenance: 'known_gap',
    financial: 'low',
  },
  assertion: {
    assertion_version: 'unirouter.bounded-risk-assertion.v1',
    assertion_id: 'risk-review-001.assertion',
    tenant_ref: digest('a'),
    audience: 'unirouter-risk-control-review',
    subject_ref: digest('b'),
    issued_at: '2026-07-31T08:05:00.000000Z',
    expires_at: '2026-07-31T08:10:00.000000Z',
    risk_epoch: '1',
    scope: 'routing_advice_only',
    advice: 'shadow_only',
    evidence_binding_hash: digest('2'),
    authority_effect: 'downscope_only',
    signature_state: 'not_issued_offline',
    missing_evidence_can_increase_privilege: false,
    synchronous_dependency: false,
    runtime_enforcement_authorized: false,
    assertion_hash: digest('9'),
  },
  proposal: {
    proposal_version: 'unirouter.evidence-control-proposal.v1',
    proposal_id: 'risk-review-001.proposal',
    state: 'simulated_review_required',
    trigger: 'bounded_evidence_review',
    controls: [
      {
        control_id: 'authority-downscope-review',
        target_owner: 'unirouter.iam',
        proposed_effect: 'preserve_or_reduce_current_authority_only',
        rationale_code: 'agent_trust_not_allow',
        evidence_refs: [digest('3')],
        enforcement_authorized: false,
      },
      {
        control_id: 'route-candidate-shadow-review',
        target_owner: 'unirouter.routing',
        proposed_effect: 'evaluate_candidate_off_path_without_client_delivery',
        rationale_code: 'governance_change_requires_review',
        evidence_refs: [digest('8')],
        enforcement_authorized: false,
      },
      {
        control_id: 'tenant-source-scope-binding-review',
        target_owner: 'unirouter.iam',
        proposed_effect: 'verify_product_local_tenant_to_source_scope_mapping',
        rationale_code: 'tenant_source_scope_binding_unverified',
        evidence_refs: [digest('2'), digest('6')],
        enforcement_authorized: false,
      },
    ],
    lifecycle: [
      { state: 'evidence_derived_draft', occurred_at: '2026-07-31T08:05:00.000000Z', evidence_ref: digest('2') },
      { state: 'offline_simulated', occurred_at: '2026-07-31T08:05:00.000000Z', evidence_ref: digest('8') },
      { state: 'human_review_required', occurred_at: '2026-07-31T08:05:00.000000Z', evidence_ref: digest('9') },
    ],
    simulation: {
      contract_version: 'unirouter.governance-simulation.v1',
      input_semantic_hash: digest('8'),
      evaluation_mode: 'shadow',
      overall_risk: 'review_required',
      release_decision: 'simulated_only',
      runtime_mutation: 'none',
      reused_directly: true,
    },
    human_review_required: true,
    self_promoted: false,
    runtime_promotion_authorized: false,
    promotion_gate: 'P7-D04',
    proposal_hash: digest('c'),
  },
  replay_status: 'accepted',
  runtime_boundary: {
    mode: 'offline_advice_and_shadow_evidence',
    persistent: false,
    remote_risk_dependency: false,
    provider_or_shadow_traffic: false,
    credentials_accepted: false,
    runtime_policy_read: false,
    runtime_enforcement_authorized: false,
    runtime_promotion_authorized: false,
    runtime_mutation: 'none',
    external_decision_gate: 'P7-D04',
  },
};

describe('RiskControlPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getRiskControlContract.mockResolvedValue({
      contract_version: 'unirouter.risk-control-review.v1',
      report_version: 'unirouter.risk-control-report.v1',
      assertion_version: 'unirouter.bounded-risk-assertion.v1',
      proposal_version: 'unirouter.evidence-control-proposal.v1',
      policy_version: 'unirouter.risk-advice-policy.v1',
      sample_request: SAMPLE,
      artifacts: {
        schema: { path: 'schema.json', sha256: digest('d') },
        fixtures: [],
        golden_report: { path: 'report.json', sha256: digest('e') },
        dependencies: [],
      },
      authority_boundary: {
        remote_risk_dependency: false,
        provider_or_shadow_traffic: false,
        runtime_policy_read: false,
        runtime_enforcement_authorized: false,
        runtime_promotion_authorized: false,
        human_review_required: true,
        external_decision_gate: 'P7-D04',
        runtime_mutation: 'none',
      },
    });
    mocks.previewRiskControl.mockResolvedValue(REPORT);
  });

  it('auto-composes safe evidence and keeps the inspector visible', async () => {
    render(<RiskControlPage />);

    expect(screen.getByLabelText('Risk Control 检查器')).toBeTruthy();
    await waitFor(() => expect(mocks.previewRiskControl)
      .toHaveBeenCalledWith(JSON.stringify(SAMPLE, null, 2)));
    expect(await screen.findByText('Assertion、证据与晋级边界')).toBeTruthy();
    expect(screen.getAllByText('仅建议 / 影子评估').length).toBeGreaterThan(0);
    expect(screen.getByText('not_issued_offline')).toBeTruthy();
    expect(screen.getByText('P7-D04')).toBeTruthy();
    expect(screen.getByText('101 / 202 / unirouter-development')).toBeTruthy();
    expect(screen.getByText('未验证（需人工复核）')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /查看|展开/ })).toBeNull();
  });

  it('switches control evidence in place without authorizing enforcement', async () => {
    render(<RiskControlPage />);
    await waitFor(() => expect(screen.getAllByText('route-candidate-shadow-review').length)
      .toBeGreaterThan(0));

    fireEvent.click(screen.getAllByText('route-candidate-shadow-review')[0]);

    expect((await screen.findAllByText('unirouter.routing')).length).toBeGreaterThan(0);
    expect(screen.getAllByText('evaluate_candidate_off_path_without_client_delivery').length)
      .toBeGreaterThan(0);
    expect(screen.getAllByText('无 / 无').length).toBeGreaterThan(0);
  });
});
