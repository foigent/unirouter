import { describe, expect, it } from 'vitest';
import { adapterTypeForProvider, providerPreset, providerPresets } from '../providerEndpoints';

describe('provider endpoint presets', () => {
  it('keeps custom providers discoverable without treating presets as an allowlist', () => {
    expect(providerPresets[0].value).toBe('custom');
    expect(providerPreset('minimax')).toMatchObject({ provider: 'minimax', baseURL: 'https://api.minimaxi.com/v1' });
    expect(providerPreset('mimo')).toMatchObject({ provider: 'mimo', baseURL: 'https://api.xiaomimimo.com/v1' });
    expect(providerPreset('not-built-in').value).toBe('custom');
  });

  it('defaults an unknown provider identity to the supported OpenAI-compatible protocol', () => {
    expect(adapterTypeForProvider('custom-aggregator')).toBe('openai_compatible');
    expect(adapterTypeForProvider('anthropic')).toBe('anthropic_native');
    expect(adapterTypeForProvider('azure')).toBe('azure_openai');
  });
});
