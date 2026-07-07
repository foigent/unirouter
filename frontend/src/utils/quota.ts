export interface KeyQuotaLike {
  rpm_limit?: number | null;
  rps_limit?: number | null;
  qps_limit?: number | null;
  quota?: {
    rpm_limit?: number | null;
    rps_limit?: number | null;
    tpm_limit?: number | null;
    budget_limit_cents?: number | null;
  };
}

export function keyRPM(value: KeyQuotaLike) {
  return value.quota?.rpm_limit ?? value.rpm_limit ?? 0;
}

export function keyRPS(value: KeyQuotaLike) {
  return value.quota?.rps_limit ?? value.rps_limit ?? value.qps_limit ?? 0;
}

export function keyTPM(value: KeyQuotaLike) {
  return value.quota?.tpm_limit ?? null;
}

export function keyQuotaText(value: KeyQuotaLike) {
  const tpm = keyTPM(value);
  return `RPM ${keyRPM(value)} / RPS ${keyRPS(value)} / TPM ${tpm ?? '未配置'}`;
}
