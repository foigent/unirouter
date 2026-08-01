import { beforeEach, describe, expect, it } from 'vitest';
import {
  deleteLocalView,
  loadSavedViews,
  readPositiveQueryParam,
  replaceControlledQuery,
  saveLocalView,
} from '../viewState';

describe('viewState', () => {
  beforeEach(() => {
    window.localStorage.clear();
    window.history.replaceState(null, '', '/audit-logs?keep=yes#detail');
  });

  it('replaces only controlled query fields and preserves unrelated state', () => {
    replaceControlledQuery(['page', 'audit_id'], { page: 2, audit_id: 9 });
    const params = new URLSearchParams(window.location.search);
    expect(params.get('keep')).toBe('yes');
    expect(params.get('page')).toBe('2');
    expect(params.get('audit_id')).toBe('9');
    expect(window.location.hash).toBe('#detail');
  });

  it('accepts only positive safe integer query identities', () => {
    expect(readPositiveQueryParam(new URLSearchParams('id=7'), 'id')).toBe(7);
    expect(readPositiveQueryParam(new URLSearchParams('id=0'), 'id')).toBeUndefined();
    expect(readPositiveQueryParam(new URLSearchParams('id=1.5'), 'id')).toBeUndefined();
    expect(readPositiveQueryParam(new URLSearchParams('id=9007199254740992'), 'id')).toBeUndefined();
  });

  it('persists, replaces, validates and deletes bounded local views', () => {
    const key = 'test.saved-views';
    const parseState = (value: unknown) => {
      if (!value || typeof value !== 'object') return null;
      const filter = (value as Record<string, unknown>).filter;
      return typeof filter === 'string' ? { filter } : null;
    };

    const first = saveLocalView(key, [], '  Errors  ', { filter: 'error' });
    expect(first.saved).toBe(true);
    const replaced = saveLocalView(key, first.views, 'Errors', { filter: 'failed' });
    expect(replaced.views).toEqual([{ name: 'Errors', state: { filter: 'failed' } }]);
    expect(loadSavedViews(key, parseState)).toEqual(replaced.views);

    window.localStorage.setItem(key, JSON.stringify([
      ...replaced.views,
      { name: '', state: { filter: 'ignored' } },
      { name: 'Broken', state: { filter: 7 } },
    ]));
    expect(loadSavedViews(key, parseState)).toEqual(replaced.views);

    const removed = deleteLocalView(key, replaced.views, 'Errors');
    expect(removed.deleted).toBe(true);
    expect(loadSavedViews(key, parseState)).toEqual([]);
  });
});
