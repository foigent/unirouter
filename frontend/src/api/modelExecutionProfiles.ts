import { request } from './transport';

export type ParameterControl = {
  object?: string;
  field: string;
  wire_type: 'string' | 'boolean';
  values: string[];
  provider_default?: string;
  disabled_values?: string[];
  aliases?: Record<string, string>;
};

export type ParameterControls = {
  version: 'unirouter.parameter-controls.v1';
  output?: {
    field: string;
    accepted_fields: string[];
    minimum: number;
    maximum: number;
    field_maximums?: Record<string, number>;
    mutually_exclusive: boolean;
  };
  reasoning?: {
    profile_id: string;
    mode: 'toggle' | 'required' | 'always_on';
    default_authority: string;
    toggle?: ParameterControl;
    effort?: ParameterControl;
    budget?: {
      field: string;
      minimum: number;
      maximum: number;
      provider_default: number;
      requires_active_reasoning: boolean;
      mutually_exclusive_with_effort: boolean;
    };
    active_output_field_maximums?: Record<string, number>;
    thinking_clear_supported: boolean;
    toggle_and_effort_mutually_exclusive: boolean;
  };
};

export type ModelExecutionProfile = {
  parameter_controls?: ParameterControls;
  id: string;
  version: string;
  label: string;
  current: boolean;
  profile_definition_digest?: string;
  product_lineage_id?: string;
  tokenizer_profile_id: string;
  tokenizer_profile_version: string;
  reasoning_profile: string;
  cache_usage_profile: string;
  protocol_profile: string;
  protocol_profile_version?: string;
  processing_tier_policy?: string;
  operation_policy_profile?: string;
  provider: string;
  region: string;
  api_surface: 'openai_chat_completions' | 'openai_responses' | 'anthropic_messages';
  operations: CanonicalModelOperationID[];
  endpoint_classes: string[];
  upstream_model_ids: string[];
  upstream_model_family?: string;
  upstream_version_policy?: 'pinned' | 'floating';
  default_output_authority: 'provider_documented' | 'unirouter_policy' | 'customer_override';
  catalog_evidence_state: 'unverified' | 'documented' | 'locally_conformant';
  provider_context_window: number;
  provider_max_output: number;
  executable_context_window?: number;
  executable_max_output?: number;
  default_context_window: number;
  default_output_limit: number;
  provider_capabilities: string[];
  capabilities: string[];
  always_required_capabilities: string[];
  aliases: string[];
  evidence_refs: string[];
};

type CanonicalModelOperationID = 'openai.chat_completions.v1' | 'openai.responses.v1' | 'anthropic.messages.v1';

export type ModelExecutionProfileCatalog = {
  contract_version: 'unirouter.model-execution-profiles.v1';
  data: ModelExecutionProfile[];
  page?: number;
  page_size?: number;
  total?: number;
};

export type ProfileCatalogSource = 'compiled' | 'published';

export const modelExecutionProfileApi = {
  list: () => request<ModelExecutionProfileCatalog>('/model-execution-profiles'),
  forStableModel: (id: number) => request<ModelExecutionProfileCatalog>(`/model-execution-profiles?public_model_id=${id}`),
  published: (page: number) => request<ModelExecutionProfileCatalog>(`/model-execution-profiles?source=published&page=${page}&page_size=100`),
};

export async function loadOnboardingProfiles(source: ProfileCatalogSource, api = modelExecutionProfileApi): Promise<ModelExecutionProfileCatalog> {
  if (source === 'compiled') return api.list();
  const data: ModelExecutionProfile[] = [];
  const seen = new Set<string>();
  let total: number | undefined;
  for (let page = 1; ; page++) {
    const result = await api.published(page);
    if (result.contract_version !== 'unirouter.model-execution-profiles.v1' || result.page !== page || result.page_size !== 100 ||
      !Number.isSafeInteger(result.total) || result.total! < 0 || (total !== undefined && total !== result.total) ||
      !Array.isArray(result.data) || result.data.length !== Math.min(100, Math.max(0, result.total! - data.length))) {
      throw new Error('已发布合同目录发生变化，请重新加载');
    }
    total = result.total!;
    for (const profile of result.data) {
      const digest = profile.profile_definition_digest;
      if (!digest || !/^[a-f0-9]{64}$/.test(digest) || !profile.product_lineage_id || seen.has(digest)) {
        throw new Error('已发布合同身份不完整，请重新加载');
      }
      seen.add(digest);
      data.push(profile);
    }
    if (data.length === total) return { contract_version: result.contract_version, data };
  }
}
