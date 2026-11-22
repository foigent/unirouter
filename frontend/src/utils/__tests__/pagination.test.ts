import { act, renderHook, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { useServerPage } from '../pagination';

it('keeps the latest page result when a previous read settles later', async () => {
  let finishPreviousRead!: (value: string) => void;
  const previousRead = new Promise<string>(resolve => { finishPreviousRead = resolve; });
  const fetchPage = vi.fn((page: number, size: number) => {
    if (page === 4) return previousRead;
    return Promise.resolve(`page ${page}, ${size} rows`);
  });
  const { result } = renderHook(() => useServerPage(fetchPage, 'customer'));

  await waitFor(() => expect(result.current.data).toBe('page 1, 20 rows'));
  act(() => result.current.onPageChange(4));
  await waitFor(() => expect(fetchPage).toHaveBeenLastCalledWith(4, 20));
  act(() => result.current.onPageSizeChange(50));
  await waitFor(() => expect(result.current.data).toBe('page 1, 50 rows'));

  await act(async () => finishPreviousRead('superseded page'));
  expect(result.current.data).toBe('page 1, 50 rows');
});

it('restores page and size together and resets the page when changing scope', async () => {
  const fetchPage = vi.fn((page: number, size: number) => Promise.resolve(`page ${page}, ${size} rows`));
  const { result, rerender } = renderHook(
    ({ scope }: { scope: string | null }) => useServerPage(fetchPage, scope, { page: 3, pageSize: 50 }),
    { initialProps: { scope: null } as { scope: string | null } },
  );
  expect(fetchPage).not.toHaveBeenCalled();

  rerender({ scope: 'first' });
  await waitFor(() => expect(result.current.data).toBe('page 3, 50 rows'));
  act(() => result.current.restorePage(4, 100));
  await waitFor(() => expect(result.current.data).toBe('page 4, 100 rows'));
  const reloadPreviousScope = result.current.reload;
  const changePreviousPage = result.current.onPageChange;

  rerender({ scope: 'second' });
  await waitFor(() => expect(result.current.data).toBe('page 1, 100 rows'));
  act(() => {
    reloadPreviousScope();
    changePreviousPage(5);
  });
  expect(fetchPage.mock.calls).toEqual([[3, 50], [4, 100], [1, 100]]);
});
