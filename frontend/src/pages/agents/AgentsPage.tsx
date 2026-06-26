import { FormEvent, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  getAgents,
  registerAgent,
  sendAgentHeartbeat,
  type Agent,
} from '../../entities/agent/api/agentApi';
import { t } from '../../shared/i18n/i18n';
import { EmptyState } from '../../shared/ui/EmptyState';
import { StatusBadge } from '../../shared/ui/StatusBadge';

function formatDate(value: string): string {
  if (!value) {
    return t('common.notAvailable');
  }

  return new Date(value).toLocaleString();
}

export function AgentsPage() {
  const queryClient = useQueryClient();

  const [serverId, setServerId] = useState('srv-dev-001');
  const [name, setName] = useState('Local Test Agent');
  const [version, setVersion] = useState('0.1.0');
  const [hostname, setHostname] = useState('codespace-agent');

  const agentsQuery = useQuery({
    queryKey: ['agents'],
    queryFn: getAgents,
    refetchInterval: 10_000,
  });

  const registerAgentMutation = useMutation({
    mutationFn: registerAgent,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['agents'] });
      setName('');
      setHostname('');
    },
  });

  const heartbeatMutation = useMutation({
    mutationFn: sendAgentHeartbeat,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['agents'] });
    },
  });

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    registerAgentMutation.mutate({
      serverId,
      name,
      version,
      hostname,
    });
  }

  function handleHeartbeat(agent: Agent) {
    heartbeatMutation.mutate({
      agentId: agent.id,
      version: agent.version,
      hostname: agent.hostname,
      status: 'online',
    });
  }

  const agents = agentsQuery.data?.items ?? [];

  return (
    <section className="page agents-page">
      <div className="page-header">
        <div>
          <h1>{t('agents.title')}</h1>
          <p>{t('agents.subtitle')}</p>
        </div>

        <div className="status-pill">
          <span className="status-dot status-dot-ok" />
          {agents.length} {t('agents.registered')}
        </div>
      </div>

      <div className="split-layout">
        <form className="auth-card" onSubmit={handleSubmit}>
          <div className="panel-title">{t('agents.registerAgent')}</div>

          <label className="field">
            <span>{t('agents.serverId')}</span>
            <input value={serverId} onChange={(event) => setServerId(event.target.value)} />
          </label>

          <label className="field">
            <span>{t('agents.name')}</span>
            <input value={name} onChange={(event) => setName(event.target.value)} />
          </label>

          <label className="field">
            <span>{t('agents.version')}</span>
            <input value={version} onChange={(event) => setVersion(event.target.value)} />
          </label>

          <label className="field">
            <span>{t('agents.hostname')}</span>
            <input value={hostname} onChange={(event) => setHostname(event.target.value)} />
          </label>

          <button
            className="primary-button"
            type="submit"
            disabled={registerAgentMutation.isPending || name.trim() === ''}
          >
            {registerAgentMutation.isPending ? t('agents.registering') : t('agents.registerAgent')}
          </button>

          {registerAgentMutation.isError && (
            <div className="form-message form-message-error">{t('agents.registerError')}</div>
          )}
        </form>

        <div className="panel table-panel">
          <div className="panel-title">{t('agents.panelTitle')}</div>

          {agentsQuery.isLoading && <p className="empty-state">{t('agents.loading')}</p>}

          {agentsQuery.isError && (
            <div className="form-message form-message-error">{t('agents.loadError')}</div>
          )}

          {agentsQuery.isSuccess && agents.length === 0 && (
            <EmptyState title={t('agents.emptyTitle')} description={t('agents.emptyDescription')} />
          )}

          {agents.length > 0 && (
            <div className="table agents-table">
              <div className="table-row table-head agents-table-row">
                <div>{t('agents.name')}</div>
                <div>{t('agents.serverId')}</div>
                <div>{t('agents.version')}</div>
                <div>{t('agents.lastSeen')}</div>
                <div>{t('agents.status')}</div>
                <div>{t('agents.action')}</div>
              </div>

              {agents.map((agent) => (
                <div className="table-row agents-table-row" key={agent.id}>
                  <div>
                    <strong>{agent.name}</strong>
                    <span>{agent.hostname || t('common.notAvailable')}</span>
                  </div>
                  <div>{agent.serverId || t('common.notAvailable')}</div>
                  <div>{agent.version || t('common.notAvailable')}</div>
                  <div>{formatDate(agent.lastSeen)}</div>
                  <div>
                    <StatusBadge status={agent.status} />
                  </div>
                  <div>
                    <button
                      className="small-button"
                      type="button"
                      disabled={heartbeatMutation.isPending}
                      onClick={() => handleHeartbeat(agent)}
                    >
                      {t('agents.heartbeat')}
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </section>
  );
}
