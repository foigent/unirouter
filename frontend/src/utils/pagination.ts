import { useCallback, useEffect, useState } from 'react';
import { useScopedRead } from './scopedRead';

export const SERVER_PAGE_SIZES = [20, 50, 100];
export const COMPACT_PAGE_SIZES = [10, ...SERVER_PAGE_SIZES];

interface PageQuery {
  scope: number | string | null;
  page: number;
  pageSize: number;
}

// The query identifies the latest read, including retries for the same page.
export function useServerPage<T>(
  fetchPage: (page: number, pageSize: number) => Promise<T>,
  scope: PageQuery['scope'],
  initialPage = { page: 1, pageSize: SERVER_PAGE_SIZES[0] },
) {
  const [query, setQuery] = useState<PageQuery>(() => ({
    scope, page: initialPage.page, pageSize: initialPage.pageSize,
  }));

  useEffect(() => {
    if (query.scope !== scope) {
      setQuery({ scope, page: query.scope === null ? query.page : 1, pageSize: query.pageSize });
    }
  }, [query, scope]);

  const readPage = useCallback(({ page, pageSize }: PageQuery) => fetchPage(page, pageSize), [fetchPage]);
  const sameScope = query.scope === scope;
  const result = useScopedRead(scope !== null && sameScope ? query : null, readPage);
  const updatePage = useCallback((page?: number, pageSize?: number) => {
    setQuery(previous => scope !== null && previous.scope === scope
      ? { scope, page: page ?? previous.page, pageSize: pageSize ?? previous.pageSize }
      : previous);
  }, [scope]);
  const reload = useCallback(() => updatePage(), [updatePage]);

  return {
    ...result,
    loading: scope !== null && (!sameScope || result.loading),
    page: sameScope ? query.page : 1,
    pageSize: query.pageSize,
    onPageChange: (page: number) => updatePage(page),
    onPageSizeChange: (pageSize: number) => updatePage(1, pageSize),
    restorePage: (page: number, pageSize: number) => updatePage(page, pageSize),
    reload,
  };
}
