import { useEffect, useSyncExternalStore, type ReactNode } from 'react';
import { LocaleProvider } from '@douyinfe/semi-ui';
import zhCN from '@douyinfe/semi-ui/lib/es/locale/source/zh_CN';
import enUS from '@douyinfe/semi-ui/lib/es/locale/source/en_US';
import './catalog';
import type { MessageKey } from './catalog';
import { englishMessage } from './registry';

export type Locale = 'zh-CN' | 'en-US';
const storageKey = 'unirouter.ui.locale';
const defaultLocale: Locale = 'en-US';
function storedLocale(value: string | null): Locale {
  return value === 'zh-CN' ? 'zh-CN' : defaultLocale;
}
function readLocale(): Locale {
  try { return storedLocale(localStorage.getItem(storageKey)); }
  catch { return defaultLocale; }
}
let locale: Locale = readLocale();
const listeners = new Set<() => void>();
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};
export function setLocale(next: Locale) {
  if (next !== 'zh-CN' && next !== 'en-US') return;
  try { localStorage.setItem(storageKey, next); } catch { /* Session-only preference. */ }
  if (next === locale) return;
  locale = next;
  listeners.forEach(listener => listener());
}
export function translate(key: MessageKey, values: Record<string, string | number> = {}): string {
  const message = locale === 'en-US' ? englishMessage(key) ?? key : key;
  return message.replace(/\{(\w+)\}/g, (match, name) => String(values[name] ?? match));
}
export function useI18n() {
  const language = useSyncExternalStore(subscribe, () => locale, () => defaultLocale);
  return { locale: language, t: translate, setLocale };
}
// For known product labels only. Never use this on model IDs, audit content,
// API enum values, names supplied by customers, or backend error payloads.
export function translateLabel(label: string): string {
  return englishMessage(label) !== undefined ? translate(label as MessageKey) : label;
}
export function labelFor(labels: Readonly<Record<string, string>>, value: string): string {
  return Object.prototype.hasOwnProperty.call(labels, value) ? translateLabel(labels[value]) : value;
}
export function localizeOptions<T extends { label: string }>(options: readonly T[]): T[] {
  return options.map(option => ({ ...option, label: translateLabel(option.label) }));
}
export function I18nProvider({ children }: { children: ReactNode }) {
  const { locale: language } = useI18n();
  useEffect(() => {
    document.documentElement.lang = language;
    document.title = `UniRouter - ${translate('统一 AI 路由网关')}`;
  }, [language]);
  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.key !== storageKey && event.key !== null) return;
      const next = storedLocale(event.newValue);
      if (locale === next) return;
      locale = next;
      listeners.forEach(listener => listener());
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, []);
  return <LocaleProvider locale={language === 'en-US' ? enUS : zhCN}>{children}</LocaleProvider>;
}
export function LanguageSwitcher() {
  const { locale: language, t } = useI18n();
  return <div className="ur-language-switch" role="group" aria-label={t('显示语言')}>
    <button type="button" lang="zh-CN" aria-pressed={language === 'zh-CN'} onClick={() => setLocale('zh-CN')}>中文</button>
    <button type="button" lang="en-US" aria-pressed={language === 'en-US'} onClick={() => setLocale('en-US')}>English</button>
  </div>;
}
