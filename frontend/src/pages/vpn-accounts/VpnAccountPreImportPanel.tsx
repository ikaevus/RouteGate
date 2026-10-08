import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { previewUnappliedVLESS } from '../../entities/vpnAccount/api/vpnAccountApi';
import { getCurrentLocale } from '../../shared/i18n/i18n';

// Scoped to a visible pending account and unmounted on navigation, apply or
// account switch. Never persist credential material in Query Cache or storage.
export function VpnAccountPreImportPanel({ accountId }: { accountId: string }) {
  const ru = getCurrentLocale() === 'ru';
  const [acknowledged, setAcknowledged] = useState(false);
  const [link, setLink] = useState('');
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState(false);
  const preview = useMutation({
    mutationFn: () => previewUnappliedVLESS(accountId, { acknowledgeUnapplied: true }),
    onSuccess: (result) => { setLink(result.vlessUri); setCopied(false); },
    onError: () => setLink(''),
  });
  async function copy() {
    setCopyError(false);
    try {
      await navigator.clipboard.writeText(link);
      setCopied(true);
    } catch {
      setCopyError(true);
    }
  }
  return (
    <div className="form-message form-message-warning" role="group"
      aria-label={ru ? 'Предварительный импорт VLESS' : 'Preliminary VLESS import'}>
      <strong>{ru ? 'Предварительный импорт VLESS / Reality' : 'Preliminary VLESS / Reality import'}</strong>
      <p>{ru
        ? 'Можно заранее импортировать прямую VLESS-ссылку, но VPN-узел ещё не подтвердил этот доступ. Подключение может не работать. Это не подписка: она не обновляется автоматически и после применения может потребоваться обычный импорт подписки.'
        : 'You can import a direct VLESS link in advance, but the node has not confirmed this access. It may not connect. This is not a subscription: it will not automatically refresh, and the normal subscription may need to be imported after apply.'}</p>
      {!link && (
        <>
          <label className="field">
            <span>
              <input type="checkbox" checked={acknowledged}
                onChange={(event) => setAcknowledged(event.target.checked)}
                disabled={preview.isPending} />
              {' '}{ru
                ? 'Я понимаю, что доступ ещё не применён и ссылка может устареть.'
                : 'I understand access is not applied and the link may become obsolete.'}
            </span>
          </label>
          <button className="small-button" type="button"
            disabled={!acknowledged || preview.isPending}
            onClick={() => preview.mutate()}>
            {preview.isPending
              ? (ru ? 'Подготовка ссылки…' : 'Preparing link…')
              : (ru ? 'Показать предварительную VLESS-ссылку' : 'Show preliminary VLESS link')}
          </button>
        </>
      )}
      {preview.isError && !link && (
        <p role="alert">{ru
          ? 'Не удалось подготовить ссылку. Проверьте, что аккаунт активен, VLESS выбран и параметры Reality сохранены.'
          : 'Could not prepare the link. Confirm the account is active, VLESS is selected and Reality settings are saved.'}</p>
      )}
      {link && (
        <>
          <p><strong>{ru ? 'Не готово к подключению' : 'Not ready to connect'}</strong></p>
          <textarea readOnly rows={3} value={link}
            aria-label={ru ? 'Предварительная VLESS-ссылка' : 'Preliminary VLESS link'} />
          <div className="form-actions">
            <button type="button" className="small-button" onClick={() => void copy()}>
              {copied ? (ru ? 'Скопировано' : 'Copied') : (ru ? 'Скопировать ссылку' : 'Copy link')}
            </button>
            <button type="button" className="small-button" onClick={() => {
              setLink(''); setCopied(false); setCopyError(false); setAcknowledged(false); preview.reset();
            }}>{ru ? 'Скрыть ссылку' : 'Hide link'}</button>
          </div>
          {copyError && <p role="alert">{ru
            ? 'Не удалось скопировать автоматически. Выделите ссылку вручную.'
            : 'Automatic copy failed. Select the link manually.'}</p>}
        </>
      )}
    </div>
  );
}
