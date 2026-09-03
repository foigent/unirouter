import { appendParams, request } from './transport';

export type ModelSuccessorExpectedModelSet = {
  model_set_id: number;
  before_alias_exists: boolean;
};

export type StagingModelSuccessorCutoverRequest = {
  expected_release_version: number;
  stable_request_model: string;
  predecessor_public_model_id: number;
  predecessor_model_name: string;
  expected_predecessor_lifecycle: 'staging';
  expected_predecessor_approved_catalog: false;
  successor_public_model_id: number;
  successor_model_name: string;
  expected_successor_lifecycle: 'staging';
  expected_successor_approved_catalog: false;
  expected_model_sets: ModelSuccessorExpectedModelSet[];
  reason?: string;
};

type ModelSetSummary = { id: number };
type ModelSetItem = { item_type: 'model' | 'alias'; item_name: string };
type ListResponse<T> = { data: T[]; total: number; page?: number; page_size?: number };

export type StagingModelSuccessorCutoverReceipt = {
  contract_version: 'unirouter.model-successor-cutover-receipt.v2';
  predecessor_authority_mode: 'never_activated_staging';
  stable_request_model: string;
  predecessor_before: { public_model_id: number; model_name: string; lifecycle: string; approved_catalog: boolean };
  predecessor_after: { public_model_id: number; model_name: string; lifecycle: string; approved_catalog: boolean };
  successor_before: { public_model_id: number; model_name: string; lifecycle: string; approved_catalog: boolean };
  successor_after: { public_model_id: number; model_name: string; lifecycle: string; approved_catalog: boolean };
  alias_after: { alias: string; target_model: string; is_primary: true; status: 'active' };
  model_sets: Array<{
    model_set_id: number;
    before_model_name: string;
    before_alias_exists: boolean;
    after_alias_name: string;
  }>;
  lifecycle: {
    contract_version: 'unirouter.model-deployment-lifecycle-receipt.v3';
    receipt_id: number;
    action: 'activate_initial';
    target_release_ref: string;
    customer_org_id: number;
    actor_user_id?: number;
    server_operation_id: string;
    original_http_status: 200;
    idempotency_replay: boolean;
    reason?: string;
    slot: {
      public_model_id: number;
      model_name: string;
      before_public_model_id?: number;
      before_stable_release_id?: number;
      before_canary_release_id?: number;
      before_canary_basis_points: number;
      before_version: number;
      after_public_model_id?: number;
      after_stable_release_id?: number;
      after_canary_release_id?: number;
      after_canary_basis_points: number;
      after_version: number;
    };
    releases: Array<{
      role: 'target' | 'stable_parent';
      release_id: number;
      release_ref: string;
      candidate_hash: string;
      route_items_hash: string;
      before_stage: string;
      before_version: number;
      after_stage: string;
      after_version: number;
    }>;
    qualifications: Array<Record<string, unknown>>;
    created_at: string;
    completed_at: string;
  };
};

async function listAllModelSets() {
  const pageSize = 100;
  const first = await request<ListResponse<ModelSetSummary>>(appendParams('/model-sets', {
    page: 1,
    page_size: pageSize,
  }));
  const result = [...first.data];
  const pageCount = Math.ceil(first.total / pageSize);
  for (let page = 2; page <= pageCount; page += 1) {
    const next = await request<ListResponse<ModelSetSummary>>(appendParams('/model-sets', {
      page,
      page_size: pageSize,
    }));
    result.push(...next.data);
  }
  return result;
}

async function snapshotModelSets(predecessorModelName: string): Promise<ModelSuccessorExpectedModelSet[]> {
  const sets = await listAllModelSets();
  const result: ModelSuccessorExpectedModelSet[] = [];
  for (const set of sets) {
    const response = await request<ListResponse<ModelSetItem>>(`/model-sets/${set.id}/items`);
    const referencesPredecessor = response.data.some(item =>
      item.item_type === 'model' && item.item_name === predecessorModelName);
    if (!referencesPredecessor) continue;
    result.push({
      model_set_id: set.id,
      before_alias_exists: response.data.some(item =>
        item.item_type === 'alias' && item.item_name === predecessorModelName),
    });
  }
  return result.sort((left, right) => left.model_set_id - right.model_set_id);
}

function releasePath(releaseRef: string) {
  return `/model-deployment-releases/${encodeURIComponent(releaseRef)}/activate-staging-successor`;
}

export const modelSuccessorCutoverApi = {
  snapshotModelSets,
  activateStagingSuccessor: (
    releaseRef: string,
    input: StagingModelSuccessorCutoverRequest,
    idempotencyKey: string,
  ) => request<StagingModelSuccessorCutoverReceipt>(releasePath(releaseRef), {
    method: 'POST',
    headers: { 'Idempotency-Key': idempotencyKey },
    body: JSON.stringify(input),
  }),
};
