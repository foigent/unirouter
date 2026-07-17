import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { GrantCreditsPanel } from '../GrantsPage';

const mockGetGrants = vi.fn();
const mockCreateGrant = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getGrants: (...args: any[]) => mockGetGrants(...args),
    createGrant: (...args: any[]) => mockCreateGrant(...args),
  },
}));

describe('GrantCreditsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetGrants.mockResolvedValue({ data: [], total: 0 });
  });

  it('reuses one client identity when a safe retry is needed', async () => {
    mockCreateGrant
      .mockRejectedValueOnce(new Error('temporary failure'))
      .mockResolvedValueOnce({ id: 9 });
    render(<GrantCreditsPanel orgId={22} />);

    fireEvent.click(await screen.findByRole('button', { name: /发放赠送额度/ }));
    fireEvent.change(screen.getByPlaceholderText('100.00'), { target: { value: '12.34' } });
    fireEvent.change(screen.getByPlaceholderText('授信调整 / 商务补偿 / TICKET-001'), { target: { value: ' TICKET-001 ' } });
    fireEvent.click(screen.getByRole('button', { name: '确认发放' }));

    await waitFor(() => expect(mockCreateGrant).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('button', { name: '确认发放' }));
    await waitFor(() => expect(mockCreateGrant).toHaveBeenCalledTimes(2));

    const first = mockCreateGrant.mock.calls[0][0];
    const second = mockCreateGrant.mock.calls[1][0];
    expect(first).toEqual(expect.objectContaining({
      org_id: 22,
      amount_cents: 1234,
      reason: 'TICKET-001',
      client_request_id: expect.stringMatching(/^grant-/),
    }));
    expect(second.client_request_id).toBe(first.client_request_id);
  });

  it('does not submit a grant without an auditable reason', async () => {
    render(<GrantCreditsPanel orgId={22} />);
    fireEvent.click(await screen.findByRole('button', { name: /发放赠送额度/ }));
    fireEvent.change(screen.getByPlaceholderText('100.00'), { target: { value: '10.00' } });
    fireEvent.click(screen.getByRole('button', { name: '确认发放' }));

    await waitFor(() => expect(mockCreateGrant).not.toHaveBeenCalled());
  });

  it('clears records from the previous organization before loading the next scope', async () => {
    let resolveSecond: (value: unknown) => void = () => undefined;
    mockGetGrants
      .mockResolvedValueOnce({
        data: [{ id: 1, org_name: 'North', amount_micros: '1000000', reason: 'North ticket', client_request_id: 'grant-1', created_at: '2026-07-16T00:00:00Z' }],
        total: 1,
      })
      .mockImplementationOnce(() => new Promise(resolve => { resolveSecond = resolve; }));

    const { rerender } = render(<GrantCreditsPanel orgId={1} />);
    expect(await screen.findByText('North ticket')).toBeTruthy();

    rerender(<GrantCreditsPanel orgId={2} />);
    await waitFor(() => expect(mockGetGrants).toHaveBeenLastCalledWith(1, 20, 2));
    expect(screen.queryByText('North ticket')).toBeNull();

    await act(async () => {
      resolveSecond({ data: [], total: 0 });
    });
  });
});
