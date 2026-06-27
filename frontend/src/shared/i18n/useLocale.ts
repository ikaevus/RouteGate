import { useSyncExternalStore } from 'react';
import { getCurrentLocale, setCurrentLocale, subscribeLocale, type Locale } from './i18n';

export function useLocale(): { locale: Locale; setLocale: (locale: Locale) => void } {
  const locale = useSyncExternalStore(subscribeLocale, getCurrentLocale, getCurrentLocale);

  return {
    locale,
    setLocale: setCurrentLocale,
  };
}
