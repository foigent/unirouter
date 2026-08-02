import { describe, expect, it } from 'vitest';
import {
  credentialModelCoverage,
  eligibleCredentialsForModel,
  evaluateCredentialCombination,
  isCredentialCoolingDown,
  parseChannelModels,
} from '../channelReadiness';

const kimiChannel = {
  id: 6,
  name: 'kimi',
  status: 'disabled',
  models: 'moonshot-v1-8k,moonshot-v1-32k,moonshot-v1-128k',
};

const kimiCredential = {
  channel_id: 6,
  status: 'active',
  allowed_models: 'Kimi K2.7 Code,kimi-k2.6',
  cooldown_until: '2026-07-01T00:52:45Z',
};

describe('channel readiness', () => {
  it('normalizes comma variants and removes duplicate model identifiers', () => {
    expect(parseChannelModels('a， b\na,a')).toEqual(['a', 'b']);
  });

  it('keeps credential, parent channel and model coverage as independent facts', () => {
    const coverage = credentialModelCoverage(kimiChannel.models, kimiCredential.allowed_models);

    expect(coverage.kind).toBe('none');
    expect(evaluateCredentialCombination(kimiCredential, kimiChannel, Date.parse('2026-08-02T00:00:00Z'))).toBe('model_mismatch');
    expect(eligibleCredentialsForModel(kimiChannel, [kimiCredential], 'moonshot-v1-8k', Date.parse('2026-08-02T00:00:00Z'))).toEqual([]);
  });

  it('does not present an expired historical cooldown as currently cooling', () => {
    expect(isCredentialCoolingDown(kimiCredential, Date.parse('2026-08-02T00:00:00Z'))).toBe(false);
  });

  it('treats an empty enabled non-cooling credential scope as all configured models', () => {
    const credential = { channel_id: 6, status: 'active', allowed_models: '' };
    expect(eligibleCredentialsForModel(kimiChannel, [credential], 'moonshot-v1-8k')).toEqual([credential]);
  });

  it('does not invent wildcard semantics that runtime routing does not support', () => {
    const credential = { channel_id: 6, status: 'active', allowed_models: '*' };
    expect(eligibleCredentialsForModel(kimiChannel, [credential], 'moonshot-v1-8k')).toEqual([]);
  });
});
