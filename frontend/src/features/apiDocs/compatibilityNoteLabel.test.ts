import { afterEach, describe, expect, it } from 'vitest';
import { setLocale } from '../../i18n';
import { compatibilityNoteLabel } from './compatibilityNoteLabel';

afterEach(() => setLocale('zh-CN'));
describe('compatibility contract note presentation', () => {
  it('translates known notes while retaining the runtime support boundary', () => {
    const note = 'Endpoint Profile exists as a contract only; runtime use is blocked until independently implemented and verified.';
    setLocale('zh-CN');
    expect(compatibilityNoteLabel(note)).toBe('端点 Profile 目前仅有合同定义；独立实现并验证前不可运行。');
    setLocale('en-US');
    expect(compatibilityNoteLabel(note)).toBe(note);
  });
  it('does not rewrite arbitrary or future server notes', () => {
    setLocale('zh-CN');
    for (const raw of ['New contract boundary from server', 'constructor', 'toString', '__proto__']) {
      expect(compatibilityNoteLabel(raw)).toBe(raw);
    }
  });
});
