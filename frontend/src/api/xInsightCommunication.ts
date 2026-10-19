import { request } from './transport';

export interface CommunicationRun {
  idempotency_key: string;
  installation_id: string;
  revision: number;
  state: 'running' | 'completed' | 'empty' | 'partial' | 'failed' | 'interrupted';
  units: number;
  requests: number;
  started_at: string;
  finished_at?: string;
}
export interface CommunicationState {
  project_id: number;
  installation_id: string;
  automatic_enabled: boolean;
  cadence_ms: number;
  revision: number;
  effective: 'runtime_disabled' | 'paused' | 'automatic' | 'running' | 'manual_running' | 'pausing' | 'completion_unknown';
  peer_state: 'unknown' | 'last_observed';
  peer_observed_at?: string;
  last_attempt_at?: string;
  last_success_at?: string;
  last_code: string;
  last_run?: CommunicationRun;
}
const path = (project: number) => `/data-exchange/xinsight/connections/${project}/communication`;
export const xInsightCommunicationApi = {
  get: (project: number) => request<CommunicationState>(path(project)),
  update: (project: number, state: CommunicationState, automatic: boolean, cadence: number) =>
    request<CommunicationState>(path(project), { method: 'PUT', body: JSON.stringify({ automatic_enabled: automatic, cadence_ms: cadence, expected_revision: state.revision, installation_id: state.installation_id }) }),
  once: (project: number, state: CommunicationState, key: string) =>
    request<CommunicationRun>(`${path(project)}/sync-once`, { method: 'POST', body: JSON.stringify({ idempotency_key: key, expected_revision: state.revision, installation_id: state.installation_id }) }),
};
