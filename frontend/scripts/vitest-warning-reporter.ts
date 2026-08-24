import process from 'node:process';
import type { Reporter } from 'vitest';

type UserConsoleLog = {
  content: string;
  origin?: string;
  taskId?: string;
  type: 'stdout' | 'stderr';
};

type AllowedStderr = {
  id: string;
  max: number;
};

const findDOMNodeFingerprint =
  'Warning: findDOMNode is deprecated and will be removed in the next major release. Instead, add a ref directly to the element you want to reference. Learn more about using refs safely here: https://reactjs.org/link/strict-mode-find-node';

const allowedStderr = new Map<string, AllowedStderr>([
  [
    findDOMNodeFingerprint,
    { id: 'FE-REACT18-SEMI-FINDDOMNODE-001', max: 10 },
  ],
  ["async-validator: [ '至少 12 个 Unicode 字符（按码点计）' ]", { id: 'FE-ASYNC-VALIDATOR-MIN-001', max: 4 }],
  ["async-validator: [ '最多 72 个 UTF-8 字节' ]", { id: 'FE-ASYNC-VALIDATOR-MAX-001', max: 4 }],
  ["async-validator: [ 'username is required' ]", { id: 'FE-ASYNC-VALIDATOR-USERNAME-001', max: 1 }],
  ["async-validator: [ 'password is required' ]", { id: 'FE-ASYNC-VALIDATOR-PASSWORD-001', max: 1 }],
  ["async-validator: [ 'reason is required' ]", { id: 'FE-ASYNC-VALIDATOR-REASON-001', max: 1 }],
  [
    'Failed to fetch request trace Error: trace unavailable',
    { id: 'FE-EXPECTED-TRACE-ERROR-001', max: 1 },
  ],
  [
    'Failed to fetch logs Error: Network error',
    { id: 'FE-EXPECTED-LOG-LIST-ERROR-001', max: 1 },
  ],
  [
    'Failed to load dashboard stats Error: Network error',
    { id: 'FE-EXPECTED-DASHBOARD-ERROR-001', max: 1 },
  ],
]);

function diagnosticLines(content: string) {
  return content
    .split(/\r?\n/u)
    .filter((line) => line.trim().length > 0 && !/^\s+at(?:\s|$)/u.test(line))
    .map((line) => line.trim());
}

export default class WarningRatchetReporter implements Reporter {
  private counts = new Map<string, number>();
  private unknown: Array<{ fingerprint: string; log: UserConsoleLog }> = [];

  onTestRunStart() {
    this.counts.clear();
    this.unknown = [];
  }

  onUserConsoleLog(log: UserConsoleLog) {
    if (log.type !== 'stderr') return;
    for (const fingerprint of diagnosticLines(log.content)) {
      if (!allowedStderr.has(fingerprint)) {
        this.unknown.push({ fingerprint, log });
        continue;
      }
      this.counts.set(fingerprint, (this.counts.get(fingerprint) || 0) + 1);
    }
  }

  onTestRunEnd() {
    const overBudget = [...allowedStderr].flatMap(([fingerprint, allowance]) => {
      const count = this.counts.get(fingerprint) || 0;
      return count > allowance.max ? [{ fingerprint, allowance, count }] : [];
    });
    const failures = [
      ...this.unknown.map(({ fingerprint, log }) => `unexpected stderr: ${fingerprint}${log.origin ? ` (${log.origin})` : ''}`),
      ...overBudget.map(({ fingerprint, allowance, count }) =>
        `${allowance.id} increased from max ${allowance.max} to ${count}: ${fingerprint}`),
    ];

    const findDOMNodeCount = this.counts.get(findDOMNodeFingerprint) || 0;
    const validatorCount = [...this.counts].reduce(
      (total, [fingerprint, count]) => total + (fingerprint.startsWith('async-validator:') ? count : 0),
      0,
    );
    const expectedErrorCount = [...this.counts].reduce(
      (total, [fingerprint, count]) => total + (fingerprint.startsWith('Failed to ') ? count : 0),
      0,
    );

    if (failures.length > 0) {
      failures.forEach((failure) => console.error(`Frontend warning ratchet: ${failure}`));
      process.exitCode = 1;
      return;
    }

    console.log(
      `Frontend warning ratchet: unexpected stderr 0; React act 0; Semi Form 0; `
      + `findDOMNode ${findDOMNodeCount}/10 (FE-REACT18-SEMI-FINDDOMNODE-001); `
      + `expected async-validator diagnostics ${validatorCount}/11; `
      + `expected failure-path stderr ${expectedErrorCount}/3.`,
    );
  }
}
