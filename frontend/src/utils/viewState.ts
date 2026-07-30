export interface SavedView<T> {
  name: string;
  state: T;
}

export function readPositiveQueryParam(params: URLSearchParams, key: string) {
  const value = Number(params.get(key));
  return Number.isSafeInteger(value) && value > 0 ? value : undefined;
}

export function replaceControlledQuery(
  controlledKeys: readonly string[],
  values: Record<string, string | number | undefined>,
) {
  const params = new URLSearchParams(window.location.search);
  controlledKeys.forEach(key => params.delete(key));
  Object.entries(values).forEach(([key, value]) => {
    if (value !== undefined && value !== '') params.set(key, String(value));
  });
  const query = params.toString();
  window.history.replaceState(
    null,
    '',
    `${window.location.pathname}${query ? `?${query}` : ''}${window.location.hash}`,
  );
}

export function loadSavedViews<T>(
  storageKey: string,
  parseState: (value: unknown) => T | null,
): SavedView<T>[] {
  try {
    const raw = window.localStorage.getItem(storageKey);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    const views: SavedView<T>[] = [];
    parsed.forEach((candidate) => {
      if (!candidate || typeof candidate !== 'object') return;
      const record = candidate as Record<string, unknown>;
      const name = typeof record.name === 'string' ? record.name.trim() : '';
      const state = parseState(record.state);
      if (!name || name.length > 40 || state === null) return;
      if (views.some(view => view.name === name)) return;
      views.push({ name, state });
    });
    return views.slice(0, 12).sort((left, right) => left.name.localeCompare(right.name));
  } catch {
    return [];
  }
}

export function saveLocalView<T>(
  storageKey: string,
  views: SavedView<T>[],
  name: string,
  state: T,
) {
  const normalizedName = name.trim();
  if (!normalizedName || normalizedName.length > 40) {
    return { views, saved: false };
  }
  const next = [
    ...views.filter(view => view.name !== normalizedName),
    { name: normalizedName, state },
  ]
    .sort((left, right) => left.name.localeCompare(right.name))
    .slice(0, 12);
  try {
    window.localStorage.setItem(storageKey, JSON.stringify(next));
    return { views: next, saved: true };
  } catch {
    return { views, saved: false };
  }
}

export function deleteLocalView<T>(
  storageKey: string,
  views: SavedView<T>[],
  name: string,
) {
  const next = views.filter(view => view.name !== name);
  try {
    window.localStorage.setItem(storageKey, JSON.stringify(next));
    return { views: next, deleted: next.length !== views.length };
  } catch {
    return { views, deleted: false };
  }
}
