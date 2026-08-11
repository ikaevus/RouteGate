import { useEffect, useState } from 'react';
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
  shareQr: string;
  downloadQr: string;
  subject: string;
  intro: string;
  importHint: string;
  warning: string;
};

const QR_QUIET_ZONE_MODULES = 4;
const QR_SCALE = 6;

function getShareCopy(): ShareCopy {
  if (getCurrentLocale() === 'ru') {
    return {
      email: 'Email',
      telegram: 'Telegram',
      whatsapp: 'WhatsApp',
      shareQr: 'Поделиться QR',
      downloadQr: 'Скачать QR',
      subject: 'Доступ к RouteGate VPN',
      intro: 'Доступ к RouteGate VPN',
      importHint: 'Импортируйте эту VLESS-ссылку в совместимый VPN-клиент:',
      warning: 'QR-код и VLESS-ссылка предоставляют доступ к VPN. Не передавайте их посторонним.',
    };
  }

  return {
    email: 'Email',
    telegram: 'Telegram',
    whatsapp: 'WhatsApp',
    shareQr: 'Share QR',
    downloadQr: 'Download QR',
    subject: 'RouteGate VPN access',
    intro: 'RouteGate VPN access',
    importHint: 'Import this VLESS link into a compatible VPN client:',
    warning: 'The QR code and VLESS link grant VPN access. Do not share them with unauthorized people.',
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

  const message = [
    `${copy.intro}: ${normalizedProfileName}`,
    '',
    copy.importHint,
    normalizedLink,
    '',
    copy.warning,
  ].join('\n');

  useEffect(() => {
    let cancelled = false;
    setQrFile(null);

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

  const emailUrl = `mailto:?subject=${encodeURIComponent(copy.subject)}&body=${encodeURIComponent(message)}`;
  const telegramUrl = `https://t.me/share/url?url=${encodeURIComponent(window.location.origin)}&text=${encodeURIComponent(message)}`;
  const whatsappUrl = `https://wa.me/?text=${encodeURIComponent(message)}`;

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

  const handleQrShare = async () => {
    if (!qrFile) {
      return;
    }

    if (canNativeShareQr) {
      try {
        await navigator.share({
          files: [qrFile],
          title: copy.subject,
          text: message,
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
      <button className="small-button vpn-share-button" type="button" onClick={() => { window.location.href = emailUrl; }}>
        {copy.email}
      </button>
      <button className="small-button vpn-share-button" type="button" onClick={() => openExternalShare(telegramUrl)}>
        {copy.telegram}
      </button>
      <button className="small-button vpn-share-button" type="button" onClick={() => openExternalShare(whatsappUrl)}>
        {copy.whatsapp}
      </button>
      {includeQrShare && (
        <button
          className="small-button vpn-share-button"
          type="button"
          disabled={!qrFile}
          onClick={() => void handleQrShare()}
        >
          {canNativeShareQr ? copy.shareQr : copy.downloadQr}
        </button>
      )}
    </div>
  );
}
