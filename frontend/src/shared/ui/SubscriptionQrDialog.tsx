import { useEffect, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { ScannableQrCode } from '../qr/ScannableQrCode';

type SubscriptionQrDialogProps = {
  isOpen: boolean;
  title: string;
  onClose: () => void;
  qrText?: string | null;
  qrTitle?: string;
  qrSubtitle?: string;
  url?: string | null;
  urlLabel?: string;
  onCopyQrText?: () => void;
  copyQrLabel?: string;
  copyCopiedLabel?: string;
  copied?: boolean;
  closeLabel?: string;
  loadingLabel?: string;
  unavailableLabel?: string;
  footerActions?: ReactNode;
  children?: ReactNode;
};

export function SubscriptionQrDialog({
  isOpen,
  title,
  onClose,
  qrText,
  qrTitle,
  qrSubtitle,
  url,
  urlLabel,
  onCopyQrText,
  copyQrLabel,
  copyCopiedLabel,
  copied = false,
  closeLabel,
  loadingLabel,
  unavailableLabel,
  footerActions,
  children,
}: SubscriptionQrDialogProps) {
  useEffect(() => {
    if (!isOpen) {
      return undefined;
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        onClose();
      }
    };

    const previousBodyOverflow = document.body.style.overflow;
    document.addEventListener('keydown', handleKeyDown);
    document.body.style.overflow = 'hidden';

    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      document.body.style.overflow = previousBodyOverflow;
    };
  }, [isOpen, onClose]);

  if (!isOpen) {
    return null;
  }

  const dialog = (
    <div className="subscription-qr-modal-backdrop" onClick={onClose}>
      <div
        className="subscription-qr-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="subscription-qr-dialog-title"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="subscription-qr-modal-header">
          <div>
            <h3 id="subscription-qr-dialog-title">{title}</h3>
            {qrSubtitle && <p>{qrSubtitle}</p>}
          </div>
          <button className="small-button" type="button" onClick={onClose}>
            {closeLabel ?? 'Close'}
          </button>
        </div>

        <div className="subscription-qr-modal-body">
          {qrText ? (
            <ScannableQrCode value={qrText} title={qrTitle} subtitle={qrSubtitle} showHeader={false} />
          ) : (
            <div className="subscription-qr-empty-state">
              <p>{loadingLabel ?? unavailableLabel ?? 'QR data is unavailable.'}</p>
            </div>
          )}

          {url && (
            <div className="subscription-qr-url-preview">
              <div className="subscription-url-label">{urlLabel}</div>
              <code className="subscription-url-code">{url}</code>
            </div>
          )}

          {children}
        </div>

        <div className="subscription-qr-modal-footer">
          <div className="subscription-qr-modal-footer-actions">
            <button className="small-button" type="button" onClick={onCopyQrText} disabled={!qrText}>
              {copied ? copyCopiedLabel : copyQrLabel}
            </button>
            {footerActions}
          </div>
          <button className="primary-button" type="button" onClick={onClose}>
            {closeLabel ?? 'Close'}
          </button>
        </div>
      </div>
    </div>
  );

  return createPortal(dialog, document.body);
}
