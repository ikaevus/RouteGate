import { useQuery } from '@tanstack/react-query';
import { getVpnAccount } from '../../entities/vpnAccount/api/vpnAccountManagementApi';
import { getCurrentLocale } from '../../shared/i18n/i18n';
import { VpnAccountProtocolPreferencePanel } from './VpnAccountProtocolPreferencePanel';
import { VpnClientConnectionPanel } from './VpnClientConnectionPanel';

function getCopy() {
  if (getCurrentLocale() === 'ru') {
    return {
      title: 'Подключение VPN-клиента',
      loading: 'Проверяем назначение VPN-узла...',
      unavailable: 'Не удалось загрузить назначение VPN-узла.',
      nodeRequiredTitle: 'Сначала назначьте VPN-узел',
      nodeRequiredDescription: 'Этот VPN-аккаунт ещё не назначен серверу. Выберите VPN-узел в блоке управления аккаунтом выше. После назначения здесь появятся выбор протокола, QR-код и готовая конфигурация клиента.',
      assignNodeAction: 'Перейти к назначению VPN-узла',
    } as const;
  }

  return {
    title: 'Connect VPN client',
    loading: 'Checking VPN node assignment...',
    unavailable: 'Could not load the VPN node assignment.',
    nodeRequiredTitle: 'Assign a VPN node first',
    nodeRequiredDescription: 'This VPN account is not assigned to a server yet. Choose a VPN node in the account management panel above. After assignment, protocol selection, QR code, and the ready client configuration will appear here.',
    assignNodeAction: 'Go to VPN node assignment',
  } as const;
}

export function VpnAccountConnectionPanels({ accountId }: { accountId: string }) {
  const copy = getCopy();
  const accountQuery = useQuery({
    queryKey: ['vpn-account', accountId],
    queryFn: () => getVpnAccount(accountId),
  });

  const scrollToAssignment = () => {
    document.querySelector('.vpn-account-management-panel')?.scrollIntoView({
      behavior: 'smooth',
      block: 'start',
    });
  };

  if (accountQuery.isLoading) {
    return (
      <div className="panel feature-detail-panel">
        <div className="panel-title">{copy.title}</div>
        <p className="empty-state">{copy.loading}</p>
      </div>
    );
  }

  if (accountQuery.isError) {
    return (
      <div className="panel feature-detail-panel">
        <div className="panel-title">{copy.title}</div>
        <div className="form-message form-message-error">{copy.unavailable}</div>
      </div>
    );
  }

  if (!accountQuery.data?.serverId) {
    return (
      <div className="panel feature-detail-panel vpn-client-connection-panel">
        <div className="panel-header">
          <div className="panel-title">{copy.title}</div>
        </div>
        <div className="empty-state empty-state-card vpn-client-empty-state">
          <strong>{copy.nodeRequiredTitle}</strong>
          <p>{copy.nodeRequiredDescription}</p>
          <button className="small-button" type="button" onClick={scrollToAssignment}>
            {copy.assignNodeAction}
          </button>
        </div>
      </div>
    );
  }

  return (
    <>
      <VpnAccountProtocolPreferencePanel accountId={accountId} />
      <VpnClientConnectionPanel accountId={accountId} />
    </>
  );
}
