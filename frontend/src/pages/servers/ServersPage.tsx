import { FormEvent, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { createServer, getServers } from '../../entities/server/api/serverApi';

export function ServersPage() {
  const queryClient = useQueryClient();

  const [name, setName] = useState('My VPS');
  const [hostname, setHostname] = useState('vps.example.local');
  const [publicIp, setPublicIp] = useState('203.0.113.20');
  const [location, setLocation] = useState('Finland');
  const [provider, setProvider] = useState('Hostkey');

  const serversQuery = useQuery({
    queryKey: ['servers'],
    queryFn: getServers,
  });

  const createServerMutation = useMutation({
    mutationFn: createServer,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['servers'] });
      setName('');
      setHostname('');
      setPublicIp('');
    },
  });

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    createServerMutation.mutate({
      name,
      hostname,
      publicIp,
      location,
      provider,
    });
  }

  const servers = serversQuery.data?.items ?? [];

  return (
    <section className="page">
      <div className="page-header">
        <div>
          <h1>Servers</h1>
          <p>Development server registry shell.</p>
        </div>

        <div className="status-pill">
          <span className="status-dot status-dot-ok" />
          {servers.length} registered
        </div>
      </div>

      <div className="split-layout">
        <form className="auth-card" onSubmit={handleSubmit}>
          <div className="panel-title">Add server</div>

          <label className="field">
            <span>Name</span>
            <input value={name} onChange={(event) => setName(event.target.value)} />
          </label>

          <label className="field">
            <span>Hostname</span>
            <input value={hostname} onChange={(event) => setHostname(event.target.value)} />
          </label>

          <label className="field">
            <span>Public IP</span>
            <input value={publicIp} onChange={(event) => setPublicIp(event.target.value)} />
          </label>

          <label className="field">
            <span>Location</span>
            <input value={location} onChange={(event) => setLocation(event.target.value)} />
          </label>

          <label className="field">
            <span>Provider</span>
            <input value={provider} onChange={(event) => setProvider(event.target.value)} />
          </label>

          <button
            className="primary-button"
            type="submit"
            disabled={createServerMutation.isPending || name.trim() === ''}
          >
            {createServerMutation.isPending ? 'Adding...' : 'Add server'}
          </button>

          {createServerMutation.isError && (
            <div className="form-message form-message-error">Failed to add server.</div>
          )}
        </form>

        <div className="panel table-panel">
          <div className="panel-title">Registered servers</div>

          {serversQuery.isLoading && <p className="muted-text">Loading servers...</p>}

          {serversQuery.isError && (
            <p className="muted-text">Failed to load servers from Manager API.</p>
          )}

          {serversQuery.isSuccess && servers.length === 0 && (
            <p className="muted-text">No servers registered yet.</p>
          )}

          {servers.length > 0 && (
            <div className="table">
              <div className="table-row table-head">
                <div>Name</div>
                <div>Public IP</div>
                <div>Location</div>
                <div>Status</div>
              </div>

              {servers.map((server) => (
                <div className="table-row" key={server.id}>
                  <div>
                    <strong>{server.name}</strong>
                    <span>{server.hostname || '—'}</span>
                  </div>
                  <div>{server.publicIp || '—'}</div>
                  <div>{server.location || '—'}</div>
                  <div>
                    <span className={`badge badge-${server.status}`}>{server.status}</span>
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
