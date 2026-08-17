import { appendParams, request } from './transport';

export type DashboardRecentLog = {
  request_id: string;
  model: string;
  routing_model?: string;
  route_channel_id?: number;
  provider?: string;
  channel?: string;
  status_code: number;
  latency_ms: number;
  cost_micros: string;
  created_at: string;
};

export type DashboardModelPrice = {
  model_name: string;
  input_price_cny_per_million: string;
  output_price_cny_per_million: string;
};

export type DashboardTrendPoint = {
  date: string;
  requests: number;
  cost_micros: string;
};

export type DashboardModelHealth = {
  model: string;
  requests: number;
  success_count: number;
  fail_count: number;
  success_rate: number;
  avg_latency_ms: number;
  p95_latency_ms: number;
};

export type DashboardChannelHealth = {
  id: number;
  name: string;
  provider: string;
  base_url?: string;
  public_models?: string[];
  base_status: string;
  runtime_published: boolean;
  enabled: boolean;
  credential_ready: boolean;
  available: boolean;
  circuit_state: string;
  projection_version: string;
  status: string;
  success_count: number;
  fail_count: number;
};

export type DashboardStats = {
  total_requests: number;
  today_cost_micros: string;
  currency: 'CNY';
  amount_scale: 1_000_000;
  avg_latency_ms: number;
  p95_latency_ms: number;
  p99_latency_ms: number;
  success_rate: number;
  recent_logs: DashboardRecentLog[];
  model_prices?: DashboardModelPrice[];
  wallet_balance_micros?: string;
  wallet_granted_micros?: string;
  wallet_frozen_micros?: string;
  wallet_threshold_micros?: string;
  daily_trend?: DashboardTrendPoint[];
  model_distribution?: DashboardModelHealth[];
  channel_health?: DashboardChannelHealth[];
  delta_vs_yesterday?: { cost_micros: string; latency_ms: number; success_rate: number };
};

export const dashboardApi = {
  getStats: (startTime?: string, endTime?: string, orgId?: number) =>
    request<DashboardStats>(appendParams('/dashboard/stats', {
      start_time: startTime,
      end_time: endTime,
      org_id: orgId,
    })),
};
