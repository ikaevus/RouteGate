import { FormEvent, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  getAgents,
  registerAgent,
  sendAgentHeartbeat,
  type Agent,
} from '../../entities/agent/api/agentApi';

function formatDate(value: string): string {
  if (!value) {
    return '—';
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
    <section className="page">
      <div className="page-header">
        <div>
          <h1>Agents</h1>
          <p>Development agent registry and heartbeat shell.</p>
        </div>

        <div className="status-pill">
          <span className="status-dot status-dot-ok" />
          {agents.length} registered
        </div>
      </div>

      <div className="split-layout">
        <form className="auth-card" onSubmit={handleSubmit}>
          <div className="panel-title">Register agent</div>

          <label className="field">
            <span>Server ID</span>
            <input value={serverId} onChange={(event) => setServerId(event.target.value)} />
          </label>

          <label className="field">
            <span>Name</span>
            <input value={name} onChange={(event) => setName(event.target.value)} />
          </label>

          <label className="field">
            <span>Version</span>
            <input value={version} onChange={(event) => setVersion(event.target.value)} />
          </label>

          <label className="field">
            <span>Hostname</span>
            <input value={hostname} onChange={(event) => setHostname(event.target.value)} />
          </label>

          <button
            className="primary-button"
            type="submit"
            disabled={registerAgentMutation.isPending || name.trim() === ''}
          >
            {registerAgentMutation.isPending ? 'Registering...' : 'Register agent'}
          </button>

          {registerAgentMutation.isError && (
            <div className="form-message form-message-error">Failed to register agent.</div>
          )}
        </form>

        <div className="panel table-panel">
          <div className="panel-title">Registered agents</div>

          {agentsQuery.isLoading && <p className="muted-text">Loading agents...</p>}

          {agentsQuery.isError && (
            <p className="muted-text">Failed to load agents from Manager API.</p>
          )}

          {agentsQuery.isSuccess && agents.length === 0 && (
            <p className="muted-text">No agents registered yet.</p>
          )}

          {agents.length > 0 && (
            <div className="table agents-table">
              <div className="table-row table-head agents-table-row">
                <div>Name</div>
                <div>Server ID</div>
                <div>Version</div>
                <div>Last seen</div>
                <div>Status</div>
                <div>Action</div>
              </div>

              {agents.map((agent) => (
                <div className="table-row agents-table-row" key={agent.id}>
                  <div>
                    <strong>{agent.name}</strong>
                    <span>{agent.hostname || '—'}</span>
                  </div>
                  <div>{agent.serverId || '—'}</div>
                  <div>{agent.version || '—'}</div>
                  <div>{formatDate(agent.lastSeen)}</div>
                  <div>
                    <span className={`badge badge-${agent.status}`}>{agent.status}</span>
                  </div>
                  <div>
                    <button
                      className="small-button"
                      type="button"
                      disabled={heartbeatMutation.isPending}
                      onClick={() => handleHeartbeat(agent)}
                    >
                      Heartbeat
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
