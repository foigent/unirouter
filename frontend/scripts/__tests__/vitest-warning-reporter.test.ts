import process from 'node:process';
import { afterEach, describe, expect, it, vi } from 'vitest';
import WarningRatchetReporter, { diagnosticLines } from '../vitest-warning-reporter';

afterEach(() => {
  vi.restoreAllMocks();
});

describe('WarningRatchetReporter', () => {
  it('normalizes ANSI-colored diagnostics before excluding stack continuations', () => {
    const content = [
      "\u001B[90masync-validator: [ \u001B[32m'username is required'\u001B[39m ]\u001B[39m",
      '\u001B[90m    at runNextTicks (node:internal/process/task_queues:65:5)\u001B[39m',
      '\u001B[90m    at processTimers (node:internal/timers:568:9)\u001B[39m',
    ].join('\n');

    expect(diagnosticLines(content)).toEqual([
      "async-validator: [ 'username is required' ]",
    ]);
  });

  it('accepts an ANSI-colored allowlisted diagnostic without weakening the ratchet', () => {
    const previousExitCode = process.exitCode;
    const log = vi.spyOn(console, 'log').mockImplementation(() => undefined);
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const reporter = new WarningRatchetReporter();
    process.exitCode = undefined;

    try {
      reporter.onTestRunStart();
      reporter.onUserConsoleLog({
        type: 'stderr',
        content: "\u001B[90masync-validator: [ \u001B[32m'username is required'\u001B[39m ]\u001B[39m\n\u001B[90m    at runNextTicks (node:internal/process/task_queues:65:5)\u001B[39m",
      });
      reporter.onTestRunEnd();

      expect(process.exitCode).toBeUndefined();
      expect(error).not.toHaveBeenCalled();
      expect(log).toHaveBeenCalledWith(expect.stringContaining('unexpected stderr 0'));
    } finally {
      process.exitCode = previousExitCode;
    }
  });
});
