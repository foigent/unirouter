import { describe, expect, it } from 'vitest';
import { legacyTestDestination } from '../TestPage';

describe('legacy detection route', () => {
  it('returns old detection links to the same model connection', () => {
    expect(legacyTestDestination('?channel_id=8&model=deepseek-v4-pro')).toBe('/channels?channel_id=8&model=deepseek-v4-pro');
  });

  it('drops invalid legacy scope instead of inventing a connection', () => {
    expect(legacyTestDestination('?channel_id=invalid')).toBe('/channels');
  });
});
