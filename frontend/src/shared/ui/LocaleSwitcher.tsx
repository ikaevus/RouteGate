import { t, type Locale } from '../i18n/i18n';
import { useLocale } from '../i18n/useLocale';

const localeOptions: Array<{ locale: Locale; label: string; ariaLabelKey: 'locale.switchToEnglish' | 'locale.switchToRussian' }> = [
  { locale: 'en', label: 'EN', ariaLabelKey: 'locale.switchToEnglish' },
  { locale: 'ru', label: 'RU', ariaLabelKey: 'locale.switchToRussian' },
];

export function LocaleSwitcher() {
  const { locale, setLocale } = useLocale();

  return (
    <div className="rg-locale-switcher" role="group" aria-label="Language">
      {localeOptions.map((option) => (
        <button
          aria-label={t(option.ariaLabelKey)}
          aria-pressed={locale === option.locale}
          className={locale === option.locale ? 'rg-locale-option rg-locale-option-active' : 'rg-locale-option'}
          key={option.locale}
          onClick={() => setLocale(option.locale)}
          title={option.locale === 'en' ? t('locale.english') : t('locale.russian')}
          type="button"
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}
