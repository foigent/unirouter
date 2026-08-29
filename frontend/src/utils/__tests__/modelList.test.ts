import { describe, expect, it } from 'vitest';
import { normalizeModelList, parseModelList, parseSortedModelList } from '../modelList';

describe('model list parsing', () => {
  it('uses one canonical rule for strings and form arrays', () => {
    expect(parseModelList('kimi-k3;kimi-k2.7-code；kimi-k2.6\n kimi-k3')).toEqual([
      'kimi-k3',
      'kimi-k2.7-code',
      'kimi-k2.6',
    ]);
    expect(parseModelList(['deepseek-v4-flash；deepseek-v4-pro', 'deepseek-v4-flash'])).toEqual([
      'deepseek-v4-flash',
      'deepseek-v4-pro',
    ]);
  });

  it('writes the canonical comma-separated representation', () => {
    expect(normalizeModelList('deepseek-v4-flash; deepseek-v4-pro；deepseek-v4-flash')).toBe(
      'deepseek-v4-flash,deepseek-v4-pro',
    );
  });

  it('matches the service UTF-8 byte order for semantic retry identity', () => {
    expect(parseSortedModelList('模型😀,模型￿,b,a,b')).toEqual(['a', 'b', '模型￿', '模型😀']);
    expect(parseSortedModelList('b,a')).toEqual(parseSortedModelList('a,b'));
  });
});
