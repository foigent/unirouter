import { request } from './transport';

export type ModelExecutionProfile = {
  id: string;
  version: string;
  label: string;
  tokenizer_profile_id: string;
  tokenizer_profile_version: string;
  reasoning_profile: string;
  cache_usage_profile: string;
  protocol_profile: string;
  provider_context_window: number;
  provider_max_output: number;
  default_context_window: number;
  default_output_limit: number;
  provider_capabilities: string[];
  capabilities: string[];
  aliases: string[];
  evidence_refs: string[];
};

export type ModelExecutionProfileCatalog = {
  contract_version: 'unirouter.model-execution-profiles.v1';
  data: ModelExecutionProfile[];
};

export const modelExecutionProfileApi = {
  list: () => request<ModelExecutionProfileCatalog>('/model-execution-profiles'),
};
