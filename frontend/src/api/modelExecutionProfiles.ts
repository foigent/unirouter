import { request } from './transport';

export type ModelExecutionProfile = {
  id: string;
  version: string;
  label: string;
  current: boolean;
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
};

export const modelExecutionProfileApi = {
  list: () => request<ModelExecutionProfileCatalog>('/model-execution-profiles'),
};
