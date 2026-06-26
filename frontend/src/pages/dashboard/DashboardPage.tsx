import { useQuery } from '@tanstack/react-query';
import { getMe } from '../../entities/auth/api/authApi';
import { getAgents } from '../../entities/agent/api/agentApi';
import { getServers } from '../../entities/server/api/serverApi';
import { getManagerHealth } from '../../entities/health/api/healthApi';
import { t } from '../../shared/i18n/i18n';

export function DashboardPage() {
  const managerHealthQuery = useQuery({
    queryKey: ['manager-health'],
    queryFn: getManagerHealth,
    refetchInterval: 10_000,
  });

  const serversQuery = useQuery({
    queryKey: ['servers'],
    queryFn: getServers,
    refetchInterval: 10_000,
  });

  const agentsQuery = useQuery({
    queryKey: ['agents'],
    queryFn: getAgents,
    refetchInterval: 10_000,
  });

  const meQuery = useQuery({
    queryKey: ['me'],
    queryFn: getMe,
    retry: false,
  });

  const managerStatusLabel = managerHealthQuery.isSuccess
    ? managerHealthQuery.data.status.toUpperCase()
    : managerHealthQuery.isError
      ? 'OFFLINE'
      : 'CHECKING';

  const managerServiceLabel = managerHealthQuery.isSuccess
    ? managerHealthQuery.data.service
    : 'Waiting for /api/admin/health';

  const managerTimestamp = managerHealthQuery.isSuccess
    ? managerHealthQuery.data.timestamp
    : t('common.notAvailable');

  const serversCount = serversQuery.data?.items.length ?? 0;
  const agentsCount = agentsQuery.data?.items.length ?? 0;
  const onlineAgentsCount =
    agentsQuery.data?.items.filter((agent) => agent.status === 'online').length ?? 0;

  return (
    <section className="page dashboard-page">
      <div className="page-header">
        <div>
          <h1>{t('dashboard.title')}</h1>
          <p>{t('dashboard.subtitle')}</p>
        </div>

        <div className="status-pill">
          <span
            className={
              managerHealthQuery.isSuccess ? 'status-dot status-dot-ok' : 'status-dot status-dot-warn'
            }
          />
          {managerHealthQuery.isSuccess ? t('dashboard.managerOnline') : t('dashboard.checkingManager')}
        </div>
      </div>

      <div className="card-grid dashboard-kpi-grid">
        <div className="card">
          <div className="card-title">{t('dashboard.managerApi')}</div>
          <div className="card-value">{managerStatusLabel}</div>
          <div className="card-meta">{managerServiceLabel}</div>
        </div>

        <div className="card">
          <div className="card-title">{t('dashboard.servers')}</div>
          <div className="card-value">
            {serversQuery.isLoading ? '...' : serversCount}
          </div>
          <div className="card-meta">
            {serversQuery.isError
              ? t('servers.loadError')
              : t('dashboard.registeredServersMeta')}
          </div>
        </div>

        <div className="card">
          <div className="card-title">{t('dashboard.agents')}</div>
          <div className="card-value">
            {agentsQuery.isLoading ? '...' : agentsCount}
          </div>
          <div className="card-meta">
            {agentsQuery.isError
              ? t('agents.loadError')
              : `${onlineAgentsCount} ${t('dashboard.onlineAgentsMeta')} · ${agentsCount} ${t('dashboard.registeredAgentsMeta')}`}
          </div>
        </div>

        <div className="card">
          <div className="card-title">{t('dashboard.currentUser')}</div>
          <div className="card-value card-value-small">
            {meQuery.isSuccess ? meQuery.data.user.displayName : t('common.guest')}
          </div>
          <div className="card-meta">
            {meQuery.isSuccess
              ? meQuery.data.user.email
              : t('dashboard.loginHint')}
          </div>
        </div>
      </div>

      <div className="panel">
        <div className="panel-title">{t('dashboard.foundationStatus')}</div>

        <div className="status-list">
          <div className="status-row">
            <span>{t('dashboard.frontend')}</span>
            <strong>{t('common.online')}</strong>
          </div>

          <div className="status-row">
            <span>{t('dashboard.managerApi')}</span>
            <strong>
              {managerHealthQuery.isSuccess
                ? t('common.online')
                : managerHealthQuery.isError
                  ? t('common.offline')
                  : t('common.checking')}
            </strong>
          </div>

          <div className="status-row">
            <span>{t('dashboard.serverRegistry')}</span>
            <strong>
              {serversQuery.isSuccess
                ? `${serversCount} ${t('common.registered')}`
                : serversQuery.isError
                  ? t('common.error')
                  : t('common.checking')}
            </strong>
          </div>

          <div className="status-row">
            <span>{t('dashboard.agentRegistry')}</span>
            <strong>
              {agentsQuery.isSuccess
                ? `${agentsCount} ${t('common.registered')}`
                : agentsQuery.isError
                  ? t('common.error')
                  : t('common.checking')}
            </strong>
          </div>

          <div className="status-row">
            <span>{t('dashboard.lastHealthTimestamp')}</span>
            <strong>{managerTimestamp}</strong>
          </div>
        </div>
      </div>
    </section>
  );
}
