import { useState } from 'react';
import { t } from '../../shared/i18n/i18n';
import { SubscriptionQrDialog } from '../../shared/ui/SubscriptionQrDialog';

type ClientRoutingImportProps = {
  clientType: string;
  subscriptionUrl: string;
};

function withFormat(url: string, format: string): string {
  return `${url}${url.includes('?') ? '&' : '?'}format=${encodeURIComponent(format)}`;
}

export function ClientRoutingImport({ clientType, subscriptionUrl }: ClientRoutingImportProps) {
  const [copied, setCopied] = useState(false);
  const [isQrOpen, setIsQrOpen] = useState(false);
  const [v2boxLink, setV2boxLink] = useState('');
  const [isPreparing, setIsPreparing] = useState(false);
  const [error, setError] = useState(false);

  if (clientType !== 'v2rayn' && clientType !== 'v2box') return null;

  const copyValue = async (value: string) => {
    if (!navigator.clipboard || value.trim() === '') return;
    await navigator.clipboard.writeText(value);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1800);
  };

  if (clientType === 'v2rayn') {
    const routingUrl = withFormat(subscriptionUrl, 'v2rayn-routing');
    return (
      <div className="subscription-url-stack">
        <div className="subscription-url-meta">
          <div className="subscription-url-label">{t('clientCompatibility.nativeRouting')}</div>
          <p className="subscription-url-helper">{t('clientCompatibility.v2raynRoutingHelp')}</p>
        </div>
        <div className="subscription-url-header">
          <div className="subscription-url-meta">
            <div className="subscription-url-label">{t('clientCompatibility.v2raynRoutingUrl')}</div>
          </div>
          <div className="table-actions">
            <button className="small-button" type="button" onClick={() => setIsQrOpen(true)}>
              {t('clientCompatibility.showRoutingQr')}
            </button>
            <button className="small-button" type="button" onClick={() => void copyValue(routingUrl)}>
              {copied ? t('clientCompatibility.copied') : t('clientCompatibility.copyRoutingUrl')}
            </button>
          </div>
        </div>
        <code className="subscription-url-value">{routingUrl}</code>
        <SubscriptionQrDialog
          isOpen={isQrOpen}
          title={t('clientCompatibility.routingQrTitle')}
          onClose={() => setIsQrOpen(false)}
          qrText={routingUrl}
          qrTitle={t('clientCompatibility.v2raynRoutingUrl')}
          qrSubtitle={t('clientCompatibility.routingQrSubtitle')}
          url={routingUrl}
          urlLabel={t('clientCompatibility.v2raynRoutingUrl')}
          onCopyQrText={() => void copyValue(routingUrl)}
          copyQrLabel={t('clientCompatibility.copyRoutingUrl')}
          copyCopiedLabel={t('clientCompatibility.copied')}
          copied={copied}
          closeLabel={t('clientCompatibility.close')}
        />
      </div>
    );
  }

  const prepareV2BoxLink = async () => {
    setIsPreparing(true);
    setError(false);
    try {
      const response = await fetch(withFormat(subscriptionUrl, 'v2box-routing'), { cache: 'no-store' });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const value = (await response.text()).trim();
      if (!value.startsWith('v2box://routes?multi=')) throw new Error('Unexpected V2Box routing payload');
      setV2boxLink(value);
    } catch {
      setError(true);
    } finally {
      setIsPreparing(false);
    }
  };

  return (
    <div className="subscription-url-stack">
      <div className="subscription-url-meta">
        <div className="subscription-url-label">{t('clientCompatibility.nativeRouting')}</div>
        <p className="subscription-url-helper">{t('clientCompatibility.v2boxRoutingHelp')}</p>
      </div>
      {!v2boxLink && (
        <button className="small-button" type="button" disabled={isPreparing} onClick={() => void prepareV2BoxLink()}>
          {isPreparing ? t('clientCompatibility.routingPreparing') : t('clientCompatibility.prepareV2boxRouting')}
        </button>
      )}
      {error && <div className="form-message form-message-error">{t('clientCompatibility.routingError')}</div>}
      {v2boxLink && (
        <>
          <div className="subscription-url-header">
            <div className="subscription-url-meta">
              <div className="subscription-url-label">{t('clientCompatibility.v2boxRoutingLink')}</div>
            </div>
            <button className="small-button" type="button" onClick={() => void copyValue(v2boxLink)}>
              {copied ? t('clientCompatibility.copied') : t('clientCompatibility.copyV2boxRouting')}
            </button>
          </div>
          <code className="subscription-url-value">{v2boxLink}</code>
        </>
      )}
    </div>
  );
}
