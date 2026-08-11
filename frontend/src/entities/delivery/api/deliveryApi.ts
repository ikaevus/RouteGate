import { apiGet, apiPost } from '../../../shared/api/client';

export type DeliveryStatus = 'queued' | 'sending' | 'retrying' | 'sent' | 'delivered' | 'failed' | 'uncertain';
export type DeliveryLocale = 'en' | 'ru';
export type DeliveryTemplate = 'vpn_access' | 'vpn_access_reissued';
export type DeliveryChannel = 'email' | 'telegram' | 'whatsapp';

export interface DeliveryProviderCapabilities {
  HTML: boolean;
  Attachments: boolean;
  DeliveryReceipts: boolean;
}

export interface DeliveryProvider {
  name: string;
  channel: DeliveryChannel | string;
  configured: boolean;
  ready: boolean;
  configurationError?: string;
  capabilities: DeliveryProviderCapabilities;
}

export interface DeliveryProviderListResponse {
  items: DeliveryProvider[];
}

export interface DeliveryRecord {
  id: string;
  vpnAccountId?: string;
  channel: string;
  provider: string;
  recipientDisplay: string;
  template: DeliveryTemplate | string;
  locale: DeliveryLocale | string;
  attachQr: boolean;
  status: DeliveryStatus;
  attemptCount: number;
  maxAttempts: number;
  nextAttemptAt?: string | null;
  lastErrorClass?: string;
  lastErrorCode?: string;
  createdAt: string;
  updatedAt: string;
  sentAt?: string | null;
  completedAt?: string | null;
}

export interface DeliveryListResponse {
  items: DeliveryRecord[];
}

export interface CreateDeliveryRequest {
  channel: DeliveryChannel;
  recipient: string;
  locale: DeliveryLocale;
  template: DeliveryTemplate;
  attachQr: boolean;
}

export interface DeliveryPreviewRequest {
  locale: DeliveryLocale;
  template: DeliveryTemplate;
}

export interface DeliveryPreviewResponse {
  subject: string;
  text: string;
}

export function getDeliveryProviders(): Promise<DeliveryProviderListResponse> {
  return apiGet<DeliveryProviderListResponse>('/api/v1/delivery/providers');
}

export function previewVpnAccountDelivery(
  vpnAccountId: string,
  request: DeliveryPreviewRequest,
): Promise<DeliveryPreviewResponse> {
  return apiPost<DeliveryPreviewRequest, DeliveryPreviewResponse>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/deliveries/preview`,
    request,
  );
}

export function createVpnAccountDelivery(
  vpnAccountId: string,
  request: CreateDeliveryRequest,
  idempotencyKey: string,
): Promise<DeliveryRecord> {
  return apiPost<CreateDeliveryRequest, DeliveryRecord>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/deliveries`,
    request,
    { 'Idempotency-Key': idempotencyKey },
  );
}

export function getVpnAccountDeliveries(vpnAccountId: string): Promise<DeliveryListResponse> {
  return apiGet<DeliveryListResponse>(
    `/api/v1/vpn-accounts/${encodeURIComponent(vpnAccountId)}/deliveries`,
  );
}

export function retryDelivery(deliveryId: string): Promise<DeliveryRecord> {
  return apiPost<undefined, DeliveryRecord>(
    `/api/v1/deliveries/${encodeURIComponent(deliveryId)}/retry`,
  );
}
