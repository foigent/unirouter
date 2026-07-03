import { describe, expect, it } from 'vitest';
import { firstChannelModel, parseChannelModels, resolveChannelTestModel } from '../TestPage';

describe('TestPage model selection helpers', () => {
  it('parses configured channel models and trims blanks', () => {
    expect(parseChannelModels('gpt-4o, claude-3-5-sonnet,  qwen-max  ')).toEqual([
      'gpt-4o',
      'claude-3-5-sonnet',
      'qwen-max',
    ]);
  });

  it('uses the selected model before falling back to the first configured channel model', () => {
    const channel = { models: 'gpt-4o,claude-3-5-sonnet' };

    expect(firstChannelModel(channel)).toBe('gpt-4o');
    expect(resolveChannelTestModel(channel, 'claude-3-5-sonnet')).toBe('claude-3-5-sonnet');
    expect(resolveChannelTestModel(channel, '')).toBe('gpt-4o');
  });

  it('does not invent a hardcoded fallback model when the channel has no configured models', () => {
    expect(resolveChannelTestModel({ models: '' }, '')).toBe('');
  });
});
