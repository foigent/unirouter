import { describe, expect, it } from 'vitest';
import { modelConnectionReturnPath } from '../ModelConnectionBackButton';

describe('model connection return path', () => {
  it('returns advanced model pages to the exact canonical deployment operation', () => {
    expect(modelConnectionReturnPath('?from=connections&deployment_id=12&operation_id=openai.chat_completions.v1')).toBe('/model-endpoints?deployment_id=12&operation_id=openai.chat_completions.v1');
  });

  it('does not invent a back destination for unrelated entry points', () => {
    expect(modelConnectionReturnPath('?deployment_id=12')).toBe('');
  });
});
