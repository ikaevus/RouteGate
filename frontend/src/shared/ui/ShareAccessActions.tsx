import { useEffect, useState, type ReactNode } from 'react';
import { getCurrentLocale } from '../i18n/i18n';
import { encodeQrPayload } from '../qr/ScannableQrCode';

type ShareAccessActionsProps = {
  vlessLink?: string | null;
  profileName?: string | null;
  includeQrShare?: boolean;
  compact?: boolean;
};

type ShareCopy = {
  email: string;
  telegram: string;
  whatsapp: string;
  copyQr: string;
  qrCopied: string;
  shareAccess: string;
  downloadQr: string;
  subject: string;
  intro: string;
  openHint: string;
  fallbackHint: string;
  warning: string;
};

const QR_QUIET_ZONE_MODULES = 4;
const QR_SCALE = 6;

function getShareCopy(): ShareCopy {
  if (getCurrentLocale() === 'ru') {
    return {
      email: 'Отправить по Email',
      telegram: 'Поделиться в Telegram',
      whatsapp: 'Поделиться в WhatsApp',
      copyQr: 'Копировать QR',
      qrCopied: 'QR скопирован',
      shareAccess: 'Поделиться',
      downloadQr: 'Скачать QR',
      subject: 'Доступ к RouteGate VPN',
      intro: 'Доступ к RouteGate VPN',
      openHint: 'Открыть VPN-профиль:',
      fallbackHint: 'Если ссылка не открылась, импортируйте VLESS вручную:',
      warning: 'QR-код и ссылки предоставляют доступ к VPN. Не передавайте их посторонним.',
    };
  }

  return {
    email: 'Send by email',
    telegram: 'Share in Telegram',
    whatsapp: 'Share in WhatsApp',
    copyQr: 'Copy QR',
    qrCopied: 'QR copied',
    shareAccess: 'Share',
    downloadQr: 'Download QR',
    subject: 'RouteGate VPN access',
    intro: 'RouteGate VPN access',
    openHint: 'Open VPN profile:',
    fallbackHint: 'If the link does not open, import the VLESS profile manually:',
    warning: 'The QR code and links grant VPN access. Do not share them with unauthorized people.',
  };
}

function sanitizeFileName(value: string): string {
  const safe = value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, '-')
    .replace(/^-+|-+$/g, '');

  return safe || 'vpn';
}

function createQrPngFile(payload: string, profileName: string): Promise<File> {
  const encoded = encodeQrPayload(payload);
  const canvasModules = encoded.size + QR_QUIET_ZONE_MODULES * 2;
  const canvas = document.createElement('canvas');
  canvas.width = canvasModules * QR_SCALE;
  canvas.height = canvasModules * QR_SCALE;

  const context = canvas.getContext('2d');
  if (!context) {
    return Promise.reject(new Error('Canvas is unavailable'));
  }

  context.imageSmoothingEnabled = false;
  context.fillStyle = '#ffffff';
  context.fillRect(0, 0, canvas.width, canvas.height);
  context.fillStyle = '#020617';

  encoded.modules.forEach((row, y) => {
    row.forEach((isDark, x) => {
      if (!isDark) {
        return;
      }

      context.fillRect(
        (x + QR_QUIET_ZONE_MODULES) * QR_SCALE,
        (y + QR_QUIET_ZONE_MODULES) * QR_SCALE,
        QR_SCALE,
        QR_SCALE,
      );
    });
  });

  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (!blob) {
        reject(new Error('Could not render QR PNG'));
        return;
      }

      resolve(new File([blob], `routegate-${sanitizeFileName(profileName)}-qr.png`, { type: 'image/png' }));
    }, 'image/png');
  });
}

function downloadFile(file: File) {
  const objectUrl = URL.createObjectURL(file);
  const anchor = document.createElement('a');
  anchor.href = objectUrl;
  anchor.download = file.name;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  window.setTimeout(() => URL.revokeObjectURL(objectUrl), 0);
}

function openExternalShare(url: string) {
  window.open(url, '_blank', 'noopener,noreferrer');
}

function encodeConnectPayload(value: string): string {
  const bytes = new TextEncoder().encode(value);
  let binary = '';
  bytes.forEach((byte) => { binary += String.fromCharCode(byte); });
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
}

function EmailIcon(): ReactNode {
  return (
    <svg className="vpn-share-channel-icon" viewBox="0 0 24 24" aria-hidden="true">
      <rect x="3.5" y="5.5" width="17" height="13" rx="2.5" />
      <path d="m4.5 7 7.5 5.8L19.5 7" />
    </svg>
  );
}

function TelegramIcon(): ReactNode {
  return (
    <svg className="vpn-share-channel-icon" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M21 3.8 17.7 20c-.2 1-1 1.2-1.8.7l-5-3.7-2.4 2.3c-.3.3-.5.5-1 .5l.4-5.1 9.2-8.3c.4-.4-.1-.6-.6-.2L5.1 13.4.2 11.9c-1-.3-1-1 .2-1.5L19.5 3c.9-.3 1.7.2 1.5.8Z" fill="currentColor" stroke="none" />
    </svg>
  );
}

function WhatsAppIcon(): ReactNode {
  return (
    <svg className="vpn-share-channel-icon" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M20.5 11.7a8.3 8.3 0 0 1-12.3 7.2L3.5 20l1.2-4.5A8.3 8.3 0 1 1 20.5 11.7Z" />
      <path d="M8.4 7.8c.2-.4.4-.4.7-.4h.5c.2 0 .4.1.5.4l.8 1.9c.1.3.1.5-.1.7l-.6.8c-.2.2-.2.4-.1.6.7 1.3 1.7 2.3 3.1 3 .2.1.4.1.6-.1l.9-1.1c.2-.2.4-.3.7-.2l1.9.9c.3.1.4.3.4.5 0 .3-.2 1.5-1.1 2-.7.5-1.5.7-2.5.4-1.2-.3-2.7-.9-4.4-2.4-1.5-1.3-2.5-3-2.8-4.2-.3-1.1 0-2.1.5-2.8Z" fill="currentColor" stroke="none" />
    </svg>
  );
}

function ShareIcon(): ReactNode {
  return (
    <svg className="vpn-share-system-icon" viewBox="0 0 24 24" aria-hidden="true">
      <circle cx="18" cy="5" r="2.5" />
      <circle cx="6" cy="12" r="2.5" />
      <circle cx="18" cy="19" r="2.5" />
      <path d="m8.2 10.8 7.6-4.5M8.2 13.2l7.6 4.5" />
    </svg>
  );
}

export function ShareAccessActions({
  vlessLink,
  profileName,
  includeQrShare = false,
  compact = false,
}: ShareAccessActionsProps) {
  const copy = getShareCopy();
  const normalizedLink = vlessLink?.trim() ?? '';
  const normalizedProfileName = profileName?.trim() || 'VPN';
  const [qrFile, setQrFile] = useState<File | null>(null);
  const [qrCopied, setQrCopied] = useState(false);

  const connectUrl = normalizedLink === ''
    ? ''
    : `${window.location.origin}/connect.html#vless=${encodeConnectPayload(normalizedLink)}`;

  const messengerMessage = [
    `${copy.intro}: ${normalizedProfileName}`,
    '',
    copy.openHint,
    connectUrl,
    '',
    copy.fallbackHint,
    normalizedLink,
    '',
    copy.warning,
  ].join('\n');

  const emailMessage = messengerMessage;

  useEffect(() => {
    let cancelled = false;
    setQrFile(null);
    setQrCopied(false);

    if (!includeQrShare || normalizedLink === '') {
      return undefined;
    }

    void createQrPngFile(normalizedLink, normalizedProfileName)
      .then((file) => {
        if (!cancelled) {
          setQrFile(file);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setQrFile(null);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [includeQrShare, normalizedLink, normalizedProfileName]);

  if (normalizedLink === '') {
    return null;
  }

  const emailUrl = `mailto:?subject=${encodeURIComponent(copy.subject)}&body=${encodeURIComponent(emailMessage)}`;
  const telegramText = `${copy.intro}: ${normalizedProfileName}\n\n${copy.warning}`;
  const telegramUrl = `tg://msg_url?url=${encodeURIComponent(connectUrl)}&text=${encodeURIComponent(telegramText)}`;
  const whatsappUrl = `https://wa.me/?text=${encodeURIComponent(messengerMessage)}`;

  const canNativeShareQr = (() => {
    if (!qrFile || typeof navigator.share !== 'function') {
      return false;
    }

    if (typeof navigator.canShare !== 'function') {
      return true;
    }

    try {
      return navigator.canShare({ files: [qrFile] });
    } catch {
      return false;
    }
  })();

  const canCopyQrImage = Boolean(
    qrFile
      && navigator.clipboard
      && typeof navigator.clipboard.write === 'function'
      && typeof ClipboardItem !== 'undefined',
  );

  const handleCopyQr = async () => {
    if (!qrFile || !canCopyQrImage) {
      return;
    }

    try {
      await navigator.clipboard.write([
        new ClipboardItem({ 'image/png': qrFile }),
      ]);
      setQrCopied(true);
      window.setTimeout(() => setQrCopied(false), 1800);
    } catch {
      setQrCopied(false);
    }
  };

  const handleQrShare = async () => {
    if (!qrFile) {
      return;
    }

    if (canNativeShareQr) {
      try {
        await navigator.share({
          files: [qrFile],
          title: copy.subject,
          text: messengerMessage,
        });
        return;
      } catch (error) {
        if (error instanceof DOMException && error.name === 'AbortError') {
          return;
        }
      }
    }

    downloadFile(qrFile);
  };

  return (
    <div className={`vpn-share-actions${compact ? ' vpn-share-actions-compact' : ''}`}>
      {includeQrShare && (
        <button
          className="small-button vpn-share-button vpn-system-share-button"
          type="button"
          disabled={!qrFile}
          onClick={() => void handleQrShare()}
          title={canNativeShareQr ? copy.shareAccess : copy.downloadQr}
        >
          <ShareIcon />
          <span>{canNativeShareQr ? copy.shareAccess : copy.downloadQr}</span>
        </button>
      )}
      <button
        className="small-button vpn-share-button vpn-share-channel-button vpn-share-email"
        type="button"
        aria-label={copy.email}
        title={copy.email}
        onClick={() => { window.location.href = emailUrl; }}
      >
        <EmailIcon />
      </button>
      <a
        className="small-button vpn-share-button vpn-share-channel-button vpn-share-telegram"
        href={telegramUrl}
        aria-label={copy.telegram}
        title={copy.telegram}
      >
        <TelegramIcon />
      </a>
      <button
        className="small-button vpn-share-button vpn-share-channel-button vpn-share-whatsapp"
        type="button"
        aria-label={copy.whatsapp}
        title={copy.whatsapp}
        onClick={() => openExternalShare(whatsappUrl)}
      >
        <WhatsAppIcon />
      </button>
      {includeQrShare && canCopyQrImage && (
        <button
          className="small-button vpn-share-button vpn-share-qr-button"
          type="button"
          disabled={!qrFile}
          onClick={() => void handleCopyQr()}
        >
          {qrCopied ? copy.qrCopied : copy.copyQr}
        </button>
      )}
    </div>
  );
}
