import { useQuery } from '@tanstack/react-query';
import { getAgents, type Agent } from '../../entities/agent/api/agentApi';
import { getManagerHealth } from '../../entities/health/api/healthApi';
import { getSystemVersion } from '../../entities/system/api/systemApi';
import { getCurrentLocale, t, translateStatus } from '../../shared/i18n/i18n';
import { StatusBadge } from '../../shared/ui/StatusBadge';
import { DeliverySettingsPanel } from './DeliverySettingsPanel';
import { TelegramRecipientsPanel } from './TelegramRecipientsPanel';
import './SettingsPage.css';

function formatValue(value?: string | number | null): string {
  if (value === null || value === undefined || String(value).trim() === '') {
    return '—';
  }
  return String(value);
}

function formatDateTime(value?: string | null): string {
  if (!value || value.trim() === '') {
    return '—';
  }

  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }

  return new Intl.DateTimeFormat(getCurrentLocale() === 'ru' ? 'ru-RU' : 'en-US', {
    dateStyle: 'medium',
    timeStyle: 'medium',
  }).format(parsed);
}

function formatSchemaVersion(value?: string | null): string {
  if (!value || value.trim() === '') {
    return '—';
  }

  const normalized = value.trim();
  const match = normalized.match(/^0*(\d+)/);
  if (!match) {
    return normalized;
  }

  const parsed = Number.parseInt(match[1], 10);
  return Number.isNaN(parsed) ? normalized : String(parsed);
}

function formatUpdateMethod(status?: string | null): string {
  return status?.toLowerCase() === 'manual' ? t('settings.manual') : formatValue(status);
}

function formatUpdateChannel(channel?: string | null): string {
  switch (channel?.toLowerCase()) {
    case 'development':
      return t('settings.channelDevelopment');
    case 'stable':
      return t('settings.channelStable');
    default:
      return formatValue(channel);
  }
}

function Fact({ label, value, code = false }: { label: string; value: string | number; code?: boolean }) {
  return (
    <div className="settings-fact">
      <span>{label}</span>
      {code ? <code>{formatValue(value)}</code> : <strong>{formatValue(value)}</strong>}
    </div>
  );
}

function AgentCard({ agent }: { agent: Agent }) {
  const agentVersion = agent.agentVersion?.trim() || agent.version?.trim() || '—';
  const compatibility = agent.compatibility?.status?.trim() || 'unknown';

  return (
    <div className="settings-agent-card">
      <div className="settings-agent-card-header">
        <div>
          <strong>{formatValue(agent.name)}</strong>
          <span>{formatValue(agent.hostname)}</span>
        </div>
        <StatusBadge status={agent.status} />
      </div>
      <div className="settings-agent-facts">
        <Fact label={t('settings.agentVersion')} value={agentVersion} />
        <Fact label={t('settings.protocolVersion')} value={agent.protocolVersion ?? '—'} />
        <div className="settings-fact">
          <span>{t('settings.compatibility')}</span>
          <StatusBadge status={compatibility} label={translateStatus(compatibility)} />
        </div>
        <Fact label={t('settings.lastSeen')} value={formatDateTime(agent.lastSeen)} />
      </div>
    </div>
  );
}

export function SettingsPage() {
  const systemQuery = useQuery({
    queryKey: ['system-version'],
    queryFn: getSystemVersion,
    refetchInterval: 60_000,
  });
  const healthQuery = useQuery({
    queryKey: ['manager-health'],
    queryFn: getManagerHealth,
    refetchInterval: 10_000,
  });
  const agentsQuery = useQuery({
    queryKey: ['agents'],
    queryFn: getAgents,
    refetchInterval: 30_000,
  });

  const system = systemQuery.data;
  const agents = agentsQuery.data?.items ?? [];

  return (
    <section className="page system-settings-page">
      <div className="page-header">
        <div>
          <h1>{t('settings.title')}</h1>
          <p>{t('settings.subtitle')}</p>
        </div>
      </div>

      {systemQuery.isLoading && <p className="empty-state">{t('settings.loading')}</p>}
      {systemQuery.isError && <div className="form-message form-message-error">{t('settings.loadError')}</div>}

      {system && (
        <div className="settings-layout">
          <div className="settings-main-column">
            <section className="panel settings-panel">
              <div className="settings-panel-heading">
                <div>
                  <div className="panel-title">{t('settings.systemTitle')}</div>
                  <p className="panel-subtitle">{t('settings.systemSubtitle')}</p>
                </div>
                <StatusBadge
                  status={healthQuery.isSuccess ? 'online' : 'unknown'}
                  label={healthQuery.isSuccess ? t('settings.online') : t('settings.unavailable')}
                />
              </div>

              <div className="settings-component-grid">
                <section className="settings-component-card">
                  <h2>{t('settings.managerTitle')}</h2>
                  <div className="settings-fact-grid">
                    <Fact label={t('settings.version')} value={system.manager.version} />
                    <Fact label={t('settings.gitCommit')} value={system.manager.gitCommit} code />
                    <Fact label={t('settings.buildDate')} value={formatDateTime(system.manager.buildDate)} />
                    <Fact
                      label={t('settings.managerStatus')}
                      value={healthQuery.isSuccess ? t('settings.online') : t('settings.unavailable')}
                    />
                    <Fact label={t('settings.serverTime')} value={formatDateTime(healthQuery.data?.timestamp)} />
                  </div>
                </section>

                <section className="settings-component-card">
                  <h2>{t('settings.webUiTitle')}</h2>
                  <div className="settings-fact-grid">
                    <Fact label={t('settings.version')} value={system.webUi.version} />
                  </div>
                </section>

                <section className="settings-component-card">
                  <h2>{t('settings.databaseTitle')}</h2>
                  <div className="settings-fact-grid">
                    <Fact label={t('settings.expectedSchema')} value={system.database.expectedSchemaVersion} />
                    <Fact label={t('settings.appliedSchema')} value={formatSchemaVersion(system.database.appliedSchemaVersion)} />
                  </div>
                </section>
              </div>
            </section>

            <DeliverySettingsPanel />
            <TelegramRecipientsPanel />

            <section className="panel settings-panel">
              <div className="settings-panel-heading">
                <div>
                  <div className="panel-title">{t('settings.agentsTitle')}</div>
                  <p className="panel-subtitle">{t('settings.agentsSubtitle')}</p>
                </div>
                <span className="status-pill">{agents.length}</span>
              </div>

              {agentsQuery.isError && (
                <div className="form-message form-message-error">{t('settings.agentsLoadError')}</div>
              )}
              {agentsQuery.isSuccess && agents.length === 0 && (
                <p className="empty-state">{t('settings.noAgents')}</p>
              )}
              {agents.length > 0 && (
                <div className="settings-agent-list">
                  {agents.map((agent) => <AgentCard agent={agent} key={agent.id} />)}
                </div>
              )}
            </section>
          </div>

          <aside className="panel settings-panel settings-update-panel">
            <div className="settings-panel-heading">
              <div>
                <div className="panel-title">{t('settings.updateTitle')}</div>
                <p className="panel-subtitle">{t('settings.updateSubtitle')}</p>
              </div>
            </div>

            <div className="settings-update-facts">
              <Fact label={t('settings.updateMethod')} value={formatUpdateMethod(system.update.status)} />
              <Fact label={t('settings.updateChannel')} value={formatUpdateChannel(system.update.channel)} />
              <Fact
                label={t('settings.automaticUpdates')}
                value={system.update.automaticUpdatesSupported ? t('settings.supported') : t('settings.notSupported')}
              />
              <Fact label={t('settings.managerProtocol')} value={system.agentCompatibility.protocolVersion} />
              <Fact label={t('settings.minimumProtocol')} value={system.agentCompatibility.minimumProtocolVersion} />
              <Fact label={t('settings.recommendedAgent')} value={system.agentCompatibility.recommendedAgentVersion} />
            </div>

            {!system.update.automaticUpdatesSupported && (
              <div className="settings-update-notice">{t('settings.manualUpdateNotice')}</div>
            )}
          </aside>
        </div>
      )}
    </section>
  );
}
