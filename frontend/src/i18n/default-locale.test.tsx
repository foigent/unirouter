import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { useEffect, useState } from 'react';

const key = 'unirouter.ui.locale';
async function calendarLabels() {
  // Import after resetModules so the consumer and provider share one context.
  const { LocaleConsumer } = await import('@douyinfe/semi-ui');
  return <LocaleConsumer<{ placeholder: { date: string } }> componentName="DatePicker">
    {(calendar, localeCode, dateLocale) => <span data-testid="calendar-locale" data-locale={localeCode} data-date-locale={dateLocale.code}>{calendar.placeholder.date}</span>}
  </LocaleConsumer>;
}

describe('initial and external locale preferences', () => {
  beforeEach(() => {
    vi.resetModules();
    localStorage.clear();
  });
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    localStorage.clear();
  });

  it.each([
    [null, 'en-US', 'Username', 'Select date'],
    ['en-US', 'en-US', 'Username', 'Select date'],
    ['zh-CN', 'zh-CN', '用户名', '请选择日期'],
    ['invalid', 'en-US', 'Username', 'Select date'],
    ['', 'en-US', 'Username', 'Select date'],
  ])('boots consistently from stored value %s', async (stored, expected, label, datePlaceholder) => {
    if (stored !== null) localStorage.setItem(key, stored);
    const { I18nProvider, LanguageSwitcher, translate } = await import('./index');
    render(<I18nProvider><LanguageSwitcher />{await calendarLabels()}</I18nProvider>);
    expect(translate('用户名')).toBe(label);
    expect(document.documentElement.lang).toBe(expected);
    expect(document.title).toBe(expected === 'en-US' ? 'UniRouter - Unified AI gateway' : 'UniRouter - 统一 AI 路由网关');
    expect(screen.getByRole('button', { name: expected === 'en-US' ? 'English' : '中文' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByTestId('calendar-locale')).toHaveTextContent(datePlaceholder);
    expect(screen.getByTestId('calendar-locale')).toHaveAttribute('data-locale', expected);
    expect(screen.getByTestId('calendar-locale')).toHaveAttribute('data-date-locale', expected);
    // A default is not persisted as a replacement for a user's preference.
    expect(localStorage.getItem(key)).toBe(stored);
  });

  it('defaults to English when storage cannot be read, then supports session-only selection', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('storage unavailable'); });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('storage unavailable'); });
    const { I18nProvider, LanguageSwitcher, translate } = await import('./index');
    render(<I18nProvider><LanguageSwitcher />{await calendarLabels()}</I18nProvider>);
    expect(translate('用户名')).toBe('Username');
    expect(document.documentElement.lang).toBe('en-US');
    expect(screen.getByTestId('calendar-locale')).toHaveTextContent('Select date');
    fireEvent.click(screen.getByRole('button', { name: '中文' }));
    expect(translate('用户名')).toBe('用户名');
    expect(document.documentElement.lang).toBe('zh-CN');
    expect(screen.getByTestId('calendar-locale')).toHaveTextContent('请选择日期');
  });

  it.each([
    ['remove', key, null],
    ['clear', null, null],
    ['invalid', key, 'invalid'],
    ['English', key, 'en-US'],
  ])('returns to English on external %s without losing state or repeating work', async (_name, eventKey, newValue) => {
    localStorage.setItem(key, 'zh-CN');
    const { I18nProvider, LanguageSwitcher, useI18n } = await import('./index');
    await import('./finance');
    const read = vi.fn();
    const write = vi.fn();
    function Workspace() {
      const { t } = useI18n();
      const [draft, setDraft] = useState('0.000001');
      const [filter, setFilter] = useState('req-original');
      const [model, setModel] = useState('客户模型-v1');
      useEffect(() => { read(); }, []);
      return <><LanguageSwitcher />
        <input aria-label={t('金额')} value={draft} onChange={e => setDraft(e.target.value)} />
        <input aria-label={t('搜索')} value={filter} onChange={e => setFilter(e.target.value)} />
        <select aria-label={t('模型')} value={model} onChange={e => setModel(e.target.value)}>
          <option value="客户模型-v1">客户模型-v1</option><option value="model-b">model-b</option>
        </select>
        <button onClick={() => write(draft, filter, model)}>{t('保存')}</button>
      </>;
    }
    render(<I18nProvider><Workspace /></I18nProvider>);
    fireEvent.change(screen.getByRole('textbox', { name: '金额' }), { target: { value: '123456789.000001' } });
    fireEvent.change(screen.getByRole('textbox', { name: '搜索' }), { target: { value: 'req-selected' } });
    fireEvent.change(screen.getByRole('combobox', { name: '模型' }), { target: { value: 'model-b' } });
    const reads = read.mock.calls.length;
    act(() => window.dispatchEvent(new StorageEvent('storage', { key: eventKey, newValue })));
    expect(document.documentElement.lang).toBe('en-US');
    expect(screen.getByRole('textbox', { name: 'Amount' })).toHaveValue('123456789.000001');
    expect(screen.getByRole('textbox', { name: 'Search' })).toHaveValue('req-selected');
    expect(screen.getByRole('combobox', { name: 'Model' })).toHaveValue('model-b');
    expect(read).toHaveBeenCalledTimes(reads);
    expect(write).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: '中文' }));
    expect(screen.getByRole('textbox', { name: '金额' })).toHaveValue('123456789.000001');
    act(() => window.dispatchEvent(new StorageEvent('storage', { key: 'unrelated', newValue: 'invalid' })));
    expect(document.documentElement.lang).toBe('zh-CN');
    expect(read).toHaveBeenCalledTimes(reads);
    expect(write).not.toHaveBeenCalled();
  });
});
