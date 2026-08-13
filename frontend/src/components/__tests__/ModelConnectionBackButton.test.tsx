import { describe, expect, it } from 'vitest';
import { modelConnectionReturnPath } from '../ModelConnectionBackButton';

describe('model connection return path', () => {
  it('returns advanced model pages to the exact connection and model', () => {
    expect(modelConnectionReturnPath('?from=connections&channel_id=12&model=deepseek-v4-pro')).toBe('/channels?channel_id=12&model=deepseek-v4-pro');
  });

  it('does not invent a back destination for unrelated entry points', () => {
    expect(modelConnectionReturnPath('?channel_id=12')).toBe('');
  });
});
