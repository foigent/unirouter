import { useEffect, useState } from 'react';

interface ScopedQuery {
  scope: string | number | null;
}

interface ReadResult<Query, Data> {
  query: Query;
  latestData?: Data;
  failed: boolean;
  error?: unknown;
}

export function useScopedRead<Query extends ScopedQuery, Data>(
  query: Query | null,
  read: (query: Query) => Promise<Data>,
) {
  const [settled, setSettled] = useState<ReadResult<Query, Data>>();

  useEffect(() => {
    if (query === null) {
      setSettled(undefined);
      return;
    }

    let current = true;
    void read(query).then(
      (data) => {
        if (current) setSettled({ query, latestData: data, failed: false });
      },
      (error: unknown) => {
        if (current) {
          setSettled(previous => ({
            query,
            latestData: previous?.query.scope === query.scope ? previous.latestData : undefined,
            failed: true,
            error,
          }));
        }
      },
    );
    return () => { current = false; };
  }, [query, read]);

  const sameScope = query !== null && settled?.query.scope === query.scope;
  const latestData = sameScope ? settled?.latestData : undefined;
  const currentResult = sameScope && settled?.query === query ? settled : undefined;

  return {
    data: currentResult && !currentResult.failed ? currentResult.latestData : undefined,
    latestData,
    failed: currentResult?.failed ?? false,
    error: currentResult?.error,
    loading: query !== null && !currentResult,
  };
}
