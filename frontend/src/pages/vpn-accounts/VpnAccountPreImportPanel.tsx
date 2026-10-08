import { useEffect, useRef, useState } from 'react';
import { previewUnappliedVLESS } from '../../entities/vpnAccount/api/vpnAccountApi';
import { getCurrentLocale } from '../../shared/i18n/i18n';

// Credentials must never enter TanStack Query's shared query or mutation
// cache. The direct URI stays in component-local state and is invalidated on
// navigation/account switch, even if its HTTP response arrives afterwards.
export function VpnAccountPreImportPanel({ accountId }: { accountId: string }) {
  const ru = getCurrentLocale() === 'ru';
  const [acknowledged, setAcknowledged] = useState(false);
  const [link, setLink] = useState('');
  const [pending, setPending] = useState(false);
  const [requestError, setRequestError] = useState(false);
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState(false);
  const requestGeneration = useRef(0);

  useEffect(() => {
    return () => { requestGeneration.current += 1; };
  }, [accountId]);

  async function showPreview() {
    if (!acknowledged || pending) return;
    const generation = ++requestGeneration.current;
    setPending(true);
    setRequestError(false);
    try {
      const result = await previewUnappliedVLESS(accountId, { acknowledgeUnapplied: true });
      if (requestGeneration.current === generation) {
        setLink(result.vlessUri);
        setCopied(false);
      }
    } catch {
      if (requestGeneration.current === generation) setRequestError(true);
    } finally {
      if (requestGeneration.current === generation) setPending(false);
    }
  }

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
                disabled={pending} />
              {' '}{ru
                ? 'Я понимаю, что доступ ещё не применён и ссылка может устареть.'
                : 'I understand access is not applied and the link may become obsolete.'}
            </span>
          </label>
          <button className="small-button" type="button"
            disabled={!acknowledged || pending}
            onClick={() => void showPreview()}>
            {pending
              ? (ru ? 'Подготовка ссылки…' : 'Preparing link…')
              : (ru ? 'Показать предварительную VLESS-ссылку' : 'Show preliminary VLESS link')}
          </button>
        </>
      )}
      {requestError && !link && (
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
              requestGeneration.current += 1;
              setLink(''); setCopied(false); setCopyError(false); setAcknowledged(false); setRequestError(false);
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
