import { describe, expect, it } from 'vitest';
import { ApiError } from '../../api/client';
import { IntentRequestIDs } from '../intentRequestId';

describe('IntentRequestIDs', () => {
  it('reuses an ID after an uncertain failure and releases it after a definitive result', () => {
    const ids = new IntentRequestIDs();
    const first = ids.get('issue:grant:name');
    ids.settleError('issue:grant:name', new Error('transport result unknown'));
    expect(ids.get('issue:grant:name')).toBe(first);

    ids.settleError('issue:grant:name', new ApiError('conflict', 409, {}));
    expect(ids.get('issue:grant:name')).not.toBe(first);

    const completed = ids.get('rotate:credential:version');
    ids.complete('rotate:credential:version');
    expect(ids.get('rotate:credential:version')).not.toBe(completed);
  });
});
