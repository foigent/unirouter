import { afterEach, describe, expect, it } from 'vitest';
import { setLocale } from '../../../i18n';
import { modelAxisLabel } from '../axisLabels';

afterEach(() => setLocale('zh-CN'));
describe('independent model state labels', () => {
  it('keeps credential and probe readiness distinct from runtime and caller authorization', () => {
    setLocale('en-US');
    expect(modelAxisLabel('credential', 'ready')).toBe('Credentials ready');
    expect(modelAxisLabel('probe', 'ready')).toBe('Current probe passed');
    expect(modelAxisLabel('runtime', 'unassigned')).toBe('No release assigned');
    expect(modelAxisLabel('caller', 'not_evaluated')).toBe('Caller not evaluated');
    expect(modelAxisLabel('runtime', 'ready')).toBe('ready');
  });
  it('localizes known states and preserves unknown server values', () => {
    setLocale('zh-CN');
    expect(modelAxisLabel('admin', 'enabled')).toBe('管理已启用');
    expect(modelAxisLabel('catalog', 'staging')).toBe('目录准备中');
    for (const raw of ['server_future_state', 'constructor', 'toString', '__proto__']) {
      expect(modelAxisLabel('probe', raw)).toBe(raw);
    }
  });
});
