import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { setLocale } from '../../i18n';
import { ApiError } from '../../api/transport';
import { xInsightCommunicationApi, type CommunicationState } from '../../api/xInsightCommunication';
import XInsightCommunicationControls from '../XInsightCommunicationControls';
vi.mock('../../api/xInsightCommunication', () => ({ xInsightCommunicationApi: { get: vi.fn(), update: vi.fn(), once: vi.fn() } }));
const state = (project = 1): CommunicationState => ({ project_id: project, installation_id: `installation-${project}`, automatic_enabled: false, cadence_ms: 60000, revision: 1, effective: 'paused', peer_state: 'unknown', last_code: '' });
const receipt = { idempotency_key: 'once', installation_id: 'installation-1', revision: 1, state: 'empty' as const, units: 1, requests: 1, started_at: '2026-10-19T00:00:00Z' };
beforeEach(() => { setLocale('en-US'); vi.mocked(xInsightCommunicationApi.get).mockResolvedValue(state()); vi.mocked(xInsightCommunicationApi.once).mockResolvedValue(receipt); });
afterEach(() => { vi.resetAllMocks(); act(() => setLocale('zh-CN')); });
describe('connection communication controls', () => {
  it('shows manual and unknown peer in English and explicitly runs a finite request', async () => {
    render(<XInsightCommunicationControls projectId={1} expansionEnabled />);
    await screen.findByText('Automatic communication stopped');
    expect(screen.getByRole('switch', { name: 'Automatic communication' })).not.toBeChecked();
    expect(screen.getByText(/Pause XI separately/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Sync once' }));
    await waitFor(() => expect(xInsightCommunicationApi.once).toHaveBeenCalledTimes(1));
    expect(vi.mocked(xInsightCommunicationApi.once).mock.calls[0][1]).toEqual(state());
    expect(screen.getByText(/does not mean XI data has caught up/)).toBeInTheDocument();
  });
  it('does not allow once while automatic work is enabled', async () => {
    vi.mocked(xInsightCommunicationApi.get).mockResolvedValue({ ...state(), automatic_enabled: true, effective: 'automatic' });
    render(<XInsightCommunicationControls projectId={1} expansionEnabled />);
    await screen.findByText('Automatic communication enabled');
    expect(screen.getByRole('button', { name: 'Sync once' })).toBeDisabled();
    expect(xInsightCommunicationApi.once).not.toHaveBeenCalled();
  });
  it('retries an unconfirmed result with the same bound idempotency key', async () => {
    vi.mocked(xInsightCommunicationApi.once).mockRejectedValueOnce(new Error('Synthetic response loss')).mockResolvedValueOnce(receipt);
    render(<XInsightCommunicationControls projectId={1} expansionEnabled />);
    await screen.findByText('Automatic communication stopped');
    await userEvent.click(screen.getByRole('button', { name: 'Sync once' }));
    await screen.findByText('Synthetic response loss');
    await userEvent.click(screen.getByRole('button', { name: 'Reread or retry original request' }));
    await waitFor(() => expect(xInsightCommunicationApi.once).toHaveBeenCalledTimes(2));
    const calls = vi.mocked(xInsightCommunicationApi.once).mock.calls;
    expect(calls[0]).toEqual(calls[1]);
  });
  it('discards old-project responses and retry state on a project change', async () => {
    let resolveOld!: (value: CommunicationState) => void;
    vi.mocked(xInsightCommunicationApi.get).mockImplementation(project => project === 1 ? new Promise(resolve => { resolveOld = resolve; }) : Promise.resolve(state(2)));
    const { rerender } = render(<XInsightCommunicationControls projectId={1} expansionEnabled />);
    await waitFor(() => expect(xInsightCommunicationApi.get).toHaveBeenCalledWith(1));
    rerender(<XInsightCommunicationControls projectId={2} expansionEnabled />);
    await screen.findByText('Automatic communication stopped');
    resolveOld({ ...state(1), automatic_enabled: true, effective: 'automatic' });
    await userEvent.click(screen.getByRole('button', { name: 'Sync once' }));
    await waitFor(() => expect(xInsightCommunicationApi.once).toHaveBeenCalledTimes(1));
    expect(vi.mocked(xInsightCommunicationApi.once).mock.calls[0][0]).toBe(2);
    expect(vi.mocked(xInsightCommunicationApi.once).mock.calls[0][1].installation_id).toBe('installation-2');
  });
  it('reports unsupported old servers without substituting Feed pause', async () => {
    vi.mocked(xInsightCommunicationApi.get).mockRejectedValue(new ApiError('Unsupported', 404, null));
    render(<XInsightCommunicationControls projectId={1} expansionEnabled />);
    await screen.findByText(/Neither end is confirmed quiet/);
    expect(screen.queryByRole('switch')).not.toBeInTheDocument();
    expect(xInsightCommunicationApi.update).not.toHaveBeenCalled();
  });
});

it('localizes the finite run outcome and counters without claiming XI has caught up', async () => {
  vi.mocked(xInsightCommunicationApi.get).mockResolvedValue({ ...state(), last_run: { ...receipt, units: 3, requests: 8 } });
  render(<XInsightCommunicationControls projectId={1} expansionEnabled />);
  await screen.findByText('No work in this run');
  expect(screen.getByText(/3\/8 operation units · 8\/24 requests/)).toBeInTheDocument();
  expect(screen.getByText('No work in this run')).toHaveAttribute('title', 'empty');
  act(() => setLocale('zh-CN'));
  await screen.findByText('本轮结果为空');
  expect(screen.getByText(/3\/8 个操作单元 · 8\/24 次请求/)).toBeInTheDocument();
  expect(xInsightCommunicationApi.update).not.toHaveBeenCalled();
  expect(xInsightCommunicationApi.once).not.toHaveBeenCalled();
});
