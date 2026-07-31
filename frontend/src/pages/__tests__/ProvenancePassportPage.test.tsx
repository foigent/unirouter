import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import ProvenancePassportPage from '../ProvenancePassportPage';

const mocks = vi.hoisted(() => ({
  getProvenancePassportContract: vi.fn(),
  previewProvenancePassport: vi.fn(),
}));

vi.mock('../../api/client', async () => {
  const actual = await vi.importActual<any>('../../api/client');
  return { ...actual, api: mocks };
});

const digest = (value: string) => `sha256:${value.repeat(64)}`;
const SAMPLE = {
  contract_version: 'unirouter.provenance-passport.v1',
  passport_id: 'passport-settled-001',
};

const connector = {
  connector_id: 'foigent.unirouter.completed-execution',
  connector_version: '1.0.0',
  source_product: 'unirouter',
  source_schema: 'unirouter.completed-execution.v1',
  event_schema: 'foigent://events/model/request-completed/v1',
  manifest_sha256: digest('a'),
  source_mapping_sha256: digest('b'),
  compatibility_sha256: digest('c'),
  conformance_contract_sha256: digest('d'),
  vector_bundle_sha256: digest('e'),
  independent_reports: [digest('f'), digest('1')],
  conformance_cases: 30,
  conformance_classes: 14,
  status: 'pass',
  runtime_registered: true,
  product_runtime_owned: true,
  credential_owned: true,
  checkpoint_owned: false,
  core_vendor_branch_required: false,
};

const REPORT = {
  report_version: 'unirouter.provenance-passport-report.v1',
  passport_id: 'passport-settled-001',
  source: {
    profile: 'unirouter.completedfeed.reference-v1',
    schema_id: 'unirouter.completed-execution.v1',
    schema_sha256: digest('2'),
    source_instance: 'unirouter-development',
    source_revision: '1',
    request_ref: 'req_feed_v1_settled',
    terminal_status: 'settled',
    source_bytes: 981,
    source_hash: digest('3'),
    envelope_hash: digest('4'),
    idempotency_key: digest('5'),
    exact_bytes_preserved: true,
    adapter_fidelity: 'F2',
    raw_payload_present: false,
    payload_capture_mode: 'metadata_only',
    timeline_complete: true,
  },
  field_groups: {
    direct: [{ field: 'scope', classification: 'direct', evidence_path: 'envelope.scope' }],
    derived: [{ field: 'subject', classification: 'derived', evidence_path: 'envelope.subject' }],
    declared: [{ field: 'actor', classification: 'declared', evidence_path: 'envelope.actor' }],
    missing: [{ field: 'capture_gap.call_log', classification: 'missing', evidence_path: 'envelope.payload.capture_gaps.call_log', reason: 'not_available_at_billing_terminal' }],
  },
  capture_gaps: [{
    gap_id: 'req:gap:call_log',
    affected_claim: 'call_log',
    gap_type: 'not_available_from_source',
    reason_code: 'not_available_at_billing_terminal',
    severity: 'warning',
    observed_at: '2026-07-25T04:05:06.000000Z',
    payload_capture_mode: 'none',
  }],
  compatibility: {
    endpoint: '/api/v1/integrations/xinsight/completed-executions',
    page_schema: 'xinsight.completed-execution-feed.v1',
    connector_id: connector.connector_id,
    connector_version: connector.connector_version,
    event_schema_ref: connector.event_schema,
    event_schema_version: '1',
    existing_dto_unchanged: true,
    source_bytes_unchanged: true,
    event_identity_unchanged: true,
    feed_wire_unchanged: true,
    page_behavior_unchanged: true,
    cursor_behavior_unchanged: true,
    credential_unchanged: true,
  },
  protocol: {
    repository: 'https://github.com/foigent/event-connector-spec',
    version: '1.1.1',
    commit: 'e'.repeat(40),
    release_digest: digest('6'),
  },
  reference_connector: connector,
  second_connector: {
    ...connector,
    connector_id: 'foigent.litellm.spend-logs',
    source_product: 'litellm',
    source_schema: 'foigent://sources/litellm/spend-log/v1',
    event_schema: 'foigent://events/model/request-completed/v2',
    conformance_cases: 20,
    conformance_classes: 11,
    runtime_registered: false,
    product_runtime_owned: false,
    credential_owned: false,
  },
  openinference: {
    profile: 'otel.openinference.metadata-only',
    contract_version: 'unirouter.telemetry-export.v1',
    golden_fixture_sha256: digest('7'),
    creates_source_truth: false,
    is_connector_runtime: false,
    raw_content_included: false,
  },
  runtime_boundary: {
    product_registry_connector_ids: ['unirouter.completedfeed.reference-v1'],
    second_connector_registered: false,
    second_connector_runtime_owned: false,
    synchronous_dependency: false,
    runtime_mutation: 'none',
  },
};

describe('ProvenancePassportPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getProvenancePassportContract.mockResolvedValue({
      contract_version: 'unirouter.provenance-passport.v1',
      report_version: 'unirouter.provenance-passport-report.v1',
      sample_request: SAMPLE,
      artifacts: {
        schema: { path: 'schema.json', sha256: digest('8') },
        fixtures: [],
        golden_report: { path: 'report.json', sha256: digest('9') },
      },
      authority_boundary: {
        second_connector_registered: false,
        second_connector_runtime_owned: false,
        credentials_accepted: false,
        cursor_or_checkpoint_accepted: false,
        synchronous_runtime_dependency: false,
        runtime_mutation: 'none',
      },
    });
    mocks.previewProvenancePassport.mockResolvedValue(REPORT);
  });

  it('auto-builds the passport and keeps the inspector visible', async () => {
    render(<ProvenancePassportPage />);

    expect(screen.getByLabelText('Provenance Passport 检查器')).toBeTruthy();
    await waitFor(() => expect(mocks.previewProvenancePassport)
      .toHaveBeenCalledWith(JSON.stringify(SAMPLE, null, 2)));
    expect(await screen.findByText('来源、兼容性与隔离边界')).toBeTruthy();
    expect(screen.getByText('精确保留')).toBeTruthy();
    expect(screen.getByText('保持兼容')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /查看|展开/ })).toBeNull();
  });

  it('switches to the heterogeneous conformance-only Connector in place', async () => {
    render(<ProvenancePassportPage />);
    await waitFor(() => expect(screen.getAllByText('foigent.litellm.spend-logs').length).toBeGreaterThan(0));

    fireEvent.click(screen.getAllByText('foigent.litellm.spend-logs')[0]);

    expect(await screen.findByText('conformance-only')).toBeTruthy();
    expect(screen.getAllByText('未注册').length).toBeGreaterThan(0);
    expect(screen.getByText('20/20 cases · 11 classes · 2 languages')).toBeTruthy();
    expect(screen.getByText('UniRouter 不持有')).toBeTruthy();
  });
});
