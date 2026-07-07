import { describe, expect, it } from 'vitest';
import { keyQuotaText, keyRPS, keyTPM } from '../quota';

describe('quota helpers', () => {
  it('uses RPS from the quota payload before legacy qps_limit', () => {
    const key = {
      rpm_limit: 60,
      qps_limit: 8,
      quota: { rpm_limit: 120, rps_limit: 12, tpm_limit: 10000 },
    };

    expect(keyRPS(key)).toBe(12);
    expect(keyTPM(key)).toBe(10000);
    expect(keyQuotaText(key)).toBe('RPM 120 / RPS 12 / TPM 10000');
  });

  it('falls back to legacy qps_limit and marks missing TPM explicitly', () => {
    expect(keyQuotaText({ rpm_limit: 60, qps_limit: 10 })).toBe('RPM 60 / RPS 10 / TPM 未配置');
  });
});
