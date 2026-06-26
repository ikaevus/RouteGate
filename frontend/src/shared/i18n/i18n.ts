import { en, type TranslationKey } from './locales/en';
import { ru } from './locales/ru';

export type Locale = 'en' | 'ru';

const dictionaries: Record<Locale, Record<TranslationKey, string>> = {
  en,
  ru,
};

const DEFAULT_LOCALE: Locale = 'en';

export function getCurrentLocale(): Locale {
  if (typeof window === 'undefined') {
    return DEFAULT_LOCALE;
  }

  const storedLocale = window.localStorage.getItem('routegate.locale');

  return storedLocale === 'ru' ? 'ru' : DEFAULT_LOCALE;
}

export function setCurrentLocale(locale: Locale): void {
  if (typeof window === 'undefined') {
    return;
  }

  window.localStorage.setItem('routegate.locale', locale);
}

export function t(key: TranslationKey, params?: Record<string, string | number>): string {
  const locale = getCurrentLocale();
  const template = dictionaries[locale][key] ?? dictionaries[DEFAULT_LOCALE][key] ?? key;

  if (!params) {
    return template;
  }

  return Object.entries(params).reduce(
    (value, [paramKey, paramValue]) => value.replaceAll(`{${paramKey}}`, String(paramValue)),
    template,
  );
}

export function translateStatus(status?: string | null): string {
  const normalizedStatus = status && status.trim() !== '' ? status.toLowerCase() : 'unknown';
  const key = `status.${normalizedStatus}` as TranslationKey;
  const translated = t(key);

  return translated === key ? normalizedStatus : translated;
}
