import { en, type TranslationKey as BaseTranslationKey } from './locales/en';
import { ru } from './locales/ru';
import { settingsEn, settingsRu, type SettingsTranslationKey } from './settingsTranslations';
import { deliveryEn, deliveryRu, type DeliveryTranslationKey } from './deliveryTranslations';

export type Locale = 'en' | 'ru';
export type TranslationKey = BaseTranslationKey | SettingsTranslationKey | DeliveryTranslationKey;
type LocaleListener = () => void;

const dictionaries: Record<Locale, Record<TranslationKey, string>> = {
  en: { ...en, ...settingsEn, ...deliveryEn },
  ru: { ...ru, ...settingsRu, ...deliveryRu },
};

const DEFAULT_LOCALE: Locale = 'en';
const LOCALE_STORAGE_KEY = 'routegate.locale';
const localeListeners = new Set<LocaleListener>();

function normalizeLocale(locale: string | null): Locale {
  return locale === 'ru' ? 'ru' : DEFAULT_LOCALE;
}

function notifyLocaleListeners(): void {
  localeListeners.forEach((listener) => listener());
}

export function getCurrentLocale(): Locale {
  if (typeof window === 'undefined') {
    return DEFAULT_LOCALE;
  }

  const storedLocale = window.localStorage.getItem(LOCALE_STORAGE_KEY);

  return normalizeLocale(storedLocale);
}

export function setCurrentLocale(locale: Locale): void {
  if (typeof window === 'undefined') {
    return;
  }

  const previousLocale = getCurrentLocale();
  window.localStorage.setItem(LOCALE_STORAGE_KEY, locale);

  if (locale !== previousLocale) {
    notifyLocaleListeners();
  }
}

export function subscribeLocale(listener: LocaleListener): () => void {
  localeListeners.add(listener);

  const handleStorage = (event: StorageEvent) => {
    if (event.key === LOCALE_STORAGE_KEY) {
      listener();
    }
  };

  if (typeof window !== 'undefined') {
    window.addEventListener('storage', handleStorage);
  }

  return () => {
    localeListeners.delete(listener);

    if (typeof window !== 'undefined') {
      window.removeEventListener('storage', handleStorage);
    }
  };
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
