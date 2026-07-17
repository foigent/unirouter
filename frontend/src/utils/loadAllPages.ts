interface PagedResponse<T> {
  data?: T[];
  items?: T[];
  total?: number;
}

export async function loadAllPages<T>(
  fetchPage: (page: number, pageSize: number) => Promise<PagedResponse<T>>,
  pageSize = 100,
): Promise<T[]> {
  const first = await fetchPage(1, pageSize);
  const rows = [...(first.data || first.items || [])];
  const total = Number(first.total || rows.length);
  const pageCount = Math.ceil(total / pageSize);

  for (let page = 2; page <= pageCount; page += 1) {
    const response = await fetchPage(page, pageSize);
    rows.push(...(response.data || response.items || []));
  }
  return rows;
}
