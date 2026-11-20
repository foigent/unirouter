import { appendParams, request } from './transport';

export interface RequestLog {
  id: number;
  project_id: number;
  request_id: string;
  client_request_id?: string;
  workflow_id?: string;
  agent_id?: string;
  attempt_id?: string;
  model: string;
  provider: string;
  channel: string;
  status_code: number;
  latency_ms: number;
  ttft_ms: number;
  prompt_tokens: number;
  uncached_prompt_tokens?: number;
  cache_read_tokens?: number;
  cache_creation_tokens?: number;
  completion_tokens: number;
  total_tokens: number;
  cost_micros: string;
  currency: 'CNY';
  amount_scale: number;
  is_stream: boolean;
  ip_address: string;
  requested_model: string;
  upstream_status_code: number | null;
  upstream_error: string;
  usage_source: string;
  cache_usage_schema?: string;
  cache_usage_status?: string;
  pricing_version?: string;
  cache_read_ratio?: number;
  cache_creation_ratio?: number;
  effective_input_price_micros_per_million?: string;
  effective_cache_read_price_micros_per_million?: string;
  effective_cache_creation_price_micros_per_million?: string;
  effective_output_price_micros_per_million?: string;
  cache_read_cost_micros?: string;
  cache_creation_cost_micros?: string;
  created_at: string;
}

export interface RequestLogFilters {
  model?: string;
  provider?: string;
  status_code?: string;
  usage_source?: string;
  start_time?: string;
  end_time?: string;
  request_id?: string;
  client_request_id?: string;
  trace_id?: string;
  workflow_id?: string;
  agent_id?: string;
  attempt_id?: string;
  upstream_request_id?: string;
  evidence_ref?: string;
  source_event_hash?: string;
  latency_min?: string;
  latency_max?: string;
  search?: string;
}

export interface RequestTraceEvent {
  id: number;
  event_type: string;
  stage: string;
  severity: 'info' | 'warning' | 'error';
  model: string;
  requested_model: string;
  provider: string;
  channel: string;
  status_code: number | null;
  latency_ms: number;
  metadata: Record<string, unknown>;
  created_at: string;
  trace_id?: string | null;
  span_id?: string | null;
  parent_span_id?: string | null;
  schema_version?: string | null;
  source_component?: string | null;
  capture_mode?: string | null;
  capture_gap?: string | null;
  evidence_ref?: string | null;
  upstream_request_id?: string | null;
  decision_kind?: string | null;
  decision_result?: string | null;
  decision_reason?: string | null;
  actor_type?: string | null;
  actor_ref?: string | null;
  on_behalf_of?: string | null;
  session_id?: string | null;
  client_request_id?: string | null;
  workflow_id?: string | null;
  agent_id?: string | null;
  attempt_id?: string | null;
  payload_capture_mode?: string | null;
  payload_ref?: string | null;
  source_event_hash?: string | null;
}

type RequestLogScope = { org_id?: number; project_id?: number };

export const requestLogsApi = {
  getLogs: (page = 1, pageSize = 20, filters?: RequestLogFilters & RequestLogScope) =>
    request<{ data: RequestLog[]; total: number; org_id: number }>(
      appendParams('/logs', { ...filters, page, page_size: pageSize }),
    ),
  getLogDetail: (id: number, orgId?: number) =>
    request<RequestLog & { org_id: number }>(appendParams(`/logs/${id}`, { org_id: orgId })),
  getLogTrace: (id: number, orgId?: number) =>
    request<{ request_id: string; events: RequestTraceEvent[] }>(
      appendParams(`/logs/${id}/trace`, { org_id: orgId }),
    ),
};
