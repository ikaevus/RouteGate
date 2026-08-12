import { apiDelete, apiGet, apiPost, apiPut } from '../../../shared/api/client';

export type DeliveryStatus = 'queued' | 'sending' | 'retrying' | 'sent' | 'delivered' | 'failed' | 'uncertain';
export type DeliveryLocale = 'en' | 'ru';
export type DeliveryTemplate = 'vpn_access' | 'vpn_access_reissued';
export type DeliveryChannel = 'email' | 'telegram';
export type DeliveryProviderName = 'smtp' | 'telegram';
export type DeliveryProviderSource = 'managed' | 'environment' | 'none' | 'static';

export interface DeliveryProviderCapabilities {
  HTML: boolean;
  Attachments: boolean;
  DeliveryReceipts: boolean;
}

export interface DeliveryProvider {
  name: DeliveryProviderName | string;
  channel: DeliveryChannel | string;
  configured: boolean;
  ready: boolean;
  configurationError?: string;
  capabilities: DeliveryProviderCapabilities;
  source?: DeliveryProviderSource | string;
  secretConfigured?: boolean;
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

export type DeliveryProviderConfigValue = string | number | boolean | null;
export type DeliveryProviderConfig = Record<string, DeliveryProviderConfigValue>;

export interface DeliveryProviderSettings {
  provider: DeliveryProviderName;
  channel: DeliveryChannel;
  source: DeliveryProviderSource;
  enabled: boolean;
  configured: boolean;
  ready: boolean;
  secretConfigured: boolean;
  configurationError?: string;
  config: DeliveryProviderConfig;
}

export interface SaveDeliveryProviderSettingsRequest {
  enabled?: boolean;
  config: DeliveryProviderConfig;
  secret?: string;
}

export interface TestDeliveryProviderSettingsResponse {
  ok: boolean;
  errorCode?: string;
}

export interface DeliveryRecipient {
  id: string;
  channel: DeliveryChannel;
  provider: DeliveryProviderName | string;
  recipient: string;
  displayName: string;
  username?: string;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface DeliveryRecipientListResponse {
  items: DeliveryRecipient[];
}

export type TelegramPairingState = 'pending' | 'paired' | 'expired';

export interface TelegramPairingSession {
  id: string;
  state: TelegramPairingState;
  botUsername: string;
  deepLink?: string;
  expiresAt: string;
  recipient?: DeliveryRecipient;
  errorCode?: string;
}

export interface TelegramRecipientTestResponse {
  ok: boolean;
  errorCode?: string;
}

export function providerNameForChannel(channel: DeliveryChannel): DeliveryProviderName {
  return channel === 'email' ? 'smtp' : 'telegram';
}

export function getDeliveryProviders(): Promise<DeliveryProviderListResponse> {
  return apiGet<DeliveryProviderListResponse>('/api/v1/delivery/providers');
}

export function getDeliveryProviderSettings(provider: DeliveryProviderName): Promise<DeliveryProviderSettings> {
  return apiGet<DeliveryProviderSettings>(`/api/v1/delivery/providers/${encodeURIComponent(provider)}/settings`);
}

export function saveDeliveryProviderSettings(
  provider: DeliveryProviderName,
  request: SaveDeliveryProviderSettingsRequest,
): Promise<DeliveryProviderSettings> {
  return apiPut<SaveDeliveryProviderSettingsRequest, DeliveryProviderSettings>(
    `/api/v1/delivery/providers/${encodeURIComponent(provider)}/settings`,
    request,
  );
}

export function testDeliveryProviderSettings(
  provider: DeliveryProviderName,
  request: SaveDeliveryProviderSettingsRequest,
): Promise<TestDeliveryProviderSettingsResponse> {
  return apiPost<SaveDeliveryProviderSettingsRequest, TestDeliveryProviderSettingsResponse>(
    `/api/v1/delivery/providers/${encodeURIComponent(provider)}/settings/test`,
    request,
  );
}

export function removeDeliveryProviderSettings(provider: DeliveryProviderName): Promise<void> {
  return apiDelete(`/api/v1/delivery/providers/${encodeURIComponent(provider)}/settings`);
}

export function getTelegramRecipients(): Promise<DeliveryRecipientListResponse> {
  return apiGet<DeliveryRecipientListResponse>('/api/v1/delivery/telegram/recipients');
}

export function startTelegramPairing(): Promise<TelegramPairingSession> {
  return apiPost<undefined, TelegramPairingSession>('/api/v1/delivery/telegram/pairings');
}

export function getTelegramPairing(pairingId: string): Promise<TelegramPairingSession> {
  return apiGet<TelegramPairingSession>(`/api/v1/delivery/telegram/pairings/${encodeURIComponent(pairingId)}`);
}

export function testTelegramRecipient(recipientId: string): Promise<TelegramRecipientTestResponse> {
  return apiPost<undefined, TelegramRecipientTestResponse>(
    `/api/v1/delivery/telegram/recipients/${encodeURIComponent(recipientId)}/test`,
  );
}

export function removeTelegramRecipient(recipientId: string): Promise<void> {
  return apiDelete(`/api/v1/delivery/telegram/recipients/${encodeURIComponent(recipientId)}`);
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
