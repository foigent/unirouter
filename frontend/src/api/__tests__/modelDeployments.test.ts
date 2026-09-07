import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  canonicalOperationPaths,
  canonicalOperationProbeAvailability,
  modelDeploymentApi,
  type CanonicalModelApplicationCreateRequest,
  type CanonicalOperation,
} from '../modelDeployments';

const mockFetch = vi.fn();
vi.stubGlobal('fetch', mockFetch);

function response(payload: unknown, status = 200) {
  mockFetch.mockResolvedValueOnce({
    ok: status >= 200 && status < 300,
    status,
    text: () => Promise.resolve(JSON.stringify(payload)),
  });
}

function operation(overrides: Partial<CanonicalOperation> = {}): CanonicalOperation {
  return {
    operation_id: 'openai.responses.v1',
    protocol_profile_id: 'openai.responses.text',
    protocol_profile_version: '1.0.0',
    support_state: 'enabled',
    evidence_state: 'declared',
    revision: 1,
    circuit_state: 'closed',
    circuit_revision: 1,
    circuit_fail_count: 0,
    probed_binding_count: 0,
    current_success_count: 0,
    current_failure_count: 0,
    stale_probed_binding_count: 0,
    probe_state: 'missing',
    probe_reason_code: 'no_probe_recorded',
    probe_available: false,
    probe_availability_reason_code: 'operation_probe_contract_unavailable',
    capability_probe_contracts: [],
    production_qualification: { required: false, qualified: false, state: 'not_required' },
    runtime: { state: 'unassigned', reason_code: 'no_runtime_membership' },
    ...overrides,
  };
}

describe('modelDeploymentApi', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    localStorage.clear();
  });

  it('uses deployment_id plus operation_id for exact detail and probe routes', async () => {
    response({ deployment: { id: 41 }, operations: [] });
    await modelDeploymentApi.get(41, 'openai.responses.v1');
    expect(mockFetch).toHaveBeenLastCalledWith(
      '/api/v1/model-deployments/41?operation=openai.responses.v1',
      expect.objectContaining({ headers: expect.any(Object) }),
    );

    response({ deployment_id: 41, operation_id: 'openai.responses.v1' });
    await modelDeploymentApi.probe(41, 'openai.responses.v1', { credential_binding_id: 91 });
    expect(mockFetch).toHaveBeenLastCalledWith(
      '/api/v1/model-deployments/41/operations/openai.responses.v1/probes',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ credential_binding_id: 91 }),
      }),
    );
  });

  it('uses exact planned members and physical source refs without transport retries', async () => {
    response({ groups: [], separate_capabilities: [] });
    await modelDeploymentApi.getCapabilityProbePlan(41, 'openai.chat_completions.v1', 91);
    expect(mockFetch.mock.calls[0][0]).toBe('/api/v1/model-deployments/41/operations/openai.chat_completions.v1/capability-probe-plan?credential_binding_id=91');
    const input = { credential_binding_id: 91, plan_digest: 'a'.repeat(64), capability_ids: ['function_tools', 'streaming'] };
    response({ error: { source_ref: 'op_' + '1'.repeat(32) } }, 503);
    await expect(modelDeploymentApi.probeCapabilityGroup(41, 'openai.chat_completions.v1', input)).rejects.toMatchObject({ status: 503 });
    expect(mockFetch).toHaveBeenCalledTimes(2);
    expect(mockFetch.mock.calls[1][1]).toMatchObject({ method: 'POST', body: JSON.stringify(input) });
    response({ observation: {} });
    await modelDeploymentApi.getCapabilityProbeGroup(41, 'openai.chat_completions.v1', 'op_' + '1'.repeat(32));
    expect(mockFetch.mock.calls[2][0]).toBe('/api/v1/model-deployments/41/operations/openai.chat_completions.v1/capability-probe-groups/op_' + '1'.repeat(32));
  });

  it('requires the caller-owned Idempotency-Key on canonical create', async () => {
    const input: CanonicalModelApplicationCreateRequest = {
      connection: { id: 13 },
      public_model: { id: 7 },
      deployment: { upstream_model: 'deepseek-v4-flash' },
      credential: { binding_id: 91 },
      operation: { operation_id: 'openai.chat_completions.v1' },
    };
    response({ application: {}, idempotency_replay: false, reused: {} }, 201);

    await modelDeploymentApi.create(input, 'ui-model-deployment-test-retry');

    expect(mockFetch).toHaveBeenCalledWith(
      '/api/v1/model-deployments',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify(input),
        headers: expect.objectContaining({ 'Idempotency-Key': 'ui-model-deployment-test-retry' }),
      }),
    );
  });

  it('uses server probe capability as the only action truth and fails closed on a malformed contract', () => {
    expect(canonicalOperationProbeAvailability(operation({
      probe_available: true,
      probe_availability_reason_code: 'exact_probe_contract_available',
    }))).toEqual({
      available: true,
      reason: 'exact_probe_contract_available',
      source: 'server',
    });
    expect(canonicalOperationProbeAvailability(operation({
      probe_available: false,
      probe_availability_reason_code: 'operation_profile_not_probeable',
    }))).toEqual({
      available: false,
      reason: 'operation_profile_not_probeable',
      source: 'server',
    });
    const missingCapability = {
      ...operation(),
      probe_available: undefined,
      probe_availability_reason_code: undefined,
    } as unknown as CanonicalOperation;
    expect(canonicalOperationProbeAvailability(missingCapability)).toEqual({
      available: false,
      reason: 'probe_capability_contract_missing',
      source: 'contract_missing',
    });
  });

  it.each([
    ['openai.chat_completions.v1', '/v1/chat/completions'],
    ['openai.embeddings.v1', '/v1/embeddings'],
    ['openai.responses.v1', '/v1/responses'],
    ['anthropic.messages.v1', '/v1/messages'],
  ] as const)('maps %s to the canonical public path and honors server probe support', (operationID, publicPath) => {
    expect(canonicalOperationPaths[operationID]).toBe(publicPath);
    expect(canonicalOperationProbeAvailability(operation({
      operation_id: operationID,
      probe_available: true,
      probe_availability_reason_code: 'exact_probe_contract_available',
    }))).toEqual({
      available: true,
      reason: 'exact_probe_contract_available',
      source: 'server',
    });
  });
});
