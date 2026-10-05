import { apiGet, apiPost } from '../../../shared/api/client';

export type TransferState = 'preparing' | 'target_applying' | 'target_ready' | 'client_refresh_pending' | 'source_cleaning' | 'target_cleaning' | 'complete' | 'cancelled' | 'rolled_back';
export interface AccountTransfer {
  id: string;
  accountId: string;
  sourceServerId: string;
  targetServerId: string;
  state: TransferState;
  lastError: string;
  updatedAt: string;
  cutoverAt: string | null;
  completedAt: string | null;
  devices: { id: string; name: string; lastRequestedAt: string | null; requestedAfterCutover: boolean; linkChanged?: boolean }[];
}
const base = (id: string) => `/api/v1/vpn-accounts/${encodeURIComponent(id)}/transfer`;
export const getAccountTransfer = (id: string) => apiGet<{ transfer: AccountTransfer | null; requiresTransfer: boolean }>(base(id));
export const startAccountTransfer = (id: string, targetServerId: string) => apiPost<{ targetServerId: string }, AccountTransfer>(base(id), { targetServerId });
export const actAccountTransfer = (id: string, transferId: string, action: string, confirmed = false) => apiPost<{ action: string; confirmed: boolean }, AccountTransfer>(`${base(id)}/${encodeURIComponent(transferId)}`, { action, confirmed });
