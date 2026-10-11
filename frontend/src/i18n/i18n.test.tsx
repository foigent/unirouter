import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { I18nProvider, LanguageSwitcher, setLocale, translate, translateLabel, useI18n } from './index';

function FormFixture() {
  const { t } = useI18n();
  const [value, setValue] = useState('');
  return <><LanguageSwitcher /><label>{t('用户名')}<input value={value} onChange={e => setValue(e.target.value)} /></label></>;
}
describe('UI locale', () => {
  beforeEach(() => setLocale('zh-CN'));
  afterEach(() => { setLocale('zh-CN'); vi.restoreAllMocks(); });
  it('changes labels and document language without remounting user input', () => {
    render(<I18nProvider><FormFixture /></I18nProvider>);
    fireEvent.change(screen.getByRole('textbox', { name: '用户名' }), { target: { value: '保留 draft 42' } });
    fireEvent.click(screen.getByRole('button', { name: 'English' }));
    expect(screen.getByRole('textbox', { name: 'Username' })).toHaveValue('保留 draft 42');
    expect(document.documentElement.lang).toBe('en-US');
    expect(localStorage.getItem('unirouter.ui.locale')).toBe('en-US');
    fireEvent.click(screen.getByRole('button', { name: '中文' }));
    expect(screen.getByRole('textbox', { name: '用户名' })).toHaveValue('保留 draft 42');
  });
  it('still switches when preference persistence is unavailable', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
    expect(() => setLocale('en-US')).not.toThrow();
    expect(translate('概览')).toBe('Overview');
  });
  it('interpolates names verbatim and leaves unknown identifiers unchanged', () => {
    setLocale('en-US');
    expect(translate('数据范围：{name}', { name: '中文组织 {name}' })).toBe('Scope: 中文组织 {name}');
    expect(translateLabel('model 中文-v1')).toBe('model 中文-v1');
    for (const identifier of ['constructor', 'toString', '__proto__']) {
      expect(translateLabel(identifier)).toBe(identifier);
    }
  });
});
