import { FormEvent, useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  generateRealityKeypair,
  getProtocolSettings,
  updateProtocolSettings,
  type UpdateProtocolSettingsRequest,
} from '../../entities/server/api/serverApi';

interface ProtocolSettingsFormState {
  vlessPort: string;
  vlessFlow: string;
  vlessNetwork: string;
  realityPublicKey: string;
  realityShortId: string;
  realityServerName: string;
}

const emptyFormState: ProtocolSettingsFormState = {
  vlessPort: '',
  vlessFlow: '',
  vlessNetwork: '',
  realityPublicKey: '',
  realityShortId: '',
  realityServerName: '',
};

function formatDate(value?: string | null): string {
  if (!value) {
    return '-';
  }

  const date = new Date(value);

  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function toFormState(settings: Awaited<ReturnType<typeof getProtocolSettings>>): ProtocolSettingsFormState {
  return {
    vlessPort: String(settings.vless.port),
    vlessFlow: settings.vless.flow ?? '',
    vlessNetwork: settings.vless.network ?? '',
    realityPublicKey: settings.reality.publicKey ?? '',
    realityShortId: settings.reality.shortId ?? '',
    realityServerName: settings.reality.serverName ?? '',
  };
}

function toRequest(form: ProtocolSettingsFormState): UpdateProtocolSettingsRequest {
  return {
    vlessPort: Number(form.vlessPort),
    vlessFlow: form.vlessFlow.trim(),
    vlessNetwork: form.vlessNetwork.trim(),
    realityPublicKey: form.realityPublicKey.trim(),
    realityShortId: form.realityShortId.trim(),
    realityServerName: form.realityServerName.trim(),
  };
}

export function ServerProtocolSettingsPanel({ serverId }: { serverId: string }) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState<ProtocolSettingsFormState>(emptyFormState);

  const settingsQuery = useQuery({
    queryKey: ['server-protocol-settings', serverId],
    queryFn: () => getProtocolSettings(serverId),
    enabled: Boolean(serverId),
  });

  useEffect(() => {
    if (settingsQuery.data) {
      setForm(toFormState(settingsQuery.data));
    }
  }, [settingsQuery.data]);

  const updateSettingsMutation = useMutation({
    mutationFn: (request: UpdateProtocolSettingsRequest) =>
      updateProtocolSettings(serverId, request),
    onSuccess: async (response) => {
      setForm(toFormState(response));
      await queryClient.invalidateQueries({ queryKey: ['server-protocol-settings', serverId] });
    },
  });

  const realityKeypairMutation = useMutation({
    mutationFn: () => generateRealityKeypair(serverId),
    onSuccess: async (response) => {
      setForm(toFormState(response));
      await queryClient.invalidateQueries({ queryKey: ['server-protocol-settings', serverId] });
    },
  });

  function updateField(field: keyof ProtocolSettingsFormState, value: string) {
    setForm((current) => ({
      ...current,
      [field]: value,
    }));
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    updateSettingsMutation.mutate(toRequest(form));
  }

  const portNumber = Number(form.vlessPort);
  const canSave =
    Number.isInteger(portNumber) &&
    portNumber >= 1 &&
    portNumber <= 65535 &&
    !updateSettingsMutation.isPending &&
    !realityKeypairMutation.isPending;
  const keypairActionLabel = form.realityPublicKey.trim() === '' ? 'Generate Reality keypair' : 'Rotate Reality keypair';

  return (
    <form className="panel protocol-settings-panel" onSubmit={handleSubmit}>
      <div className="panel-header">
        <div>
          <div className="panel-title">VLESS / Reality protocol settings</div>
          <p className="panel-subtitle">
            Server-side public settings used when rendering account credentials and client configs.
            Reality private keys are stored server-side and are never displayed here.
          </p>
        </div>
        <div className="table-actions">
          <button
            className="small-button"
            type="button"
            disabled={realityKeypairMutation.isPending || updateSettingsMutation.isPending || !settingsQuery.data}
            onClick={() => realityKeypairMutation.mutate()}
          >
            {realityKeypairMutation.isPending ? 'Generating...' : keypairActionLabel}
          </button>
          <button className="small-button" type="submit" disabled={!canSave}>
            {updateSettingsMutation.isPending ? 'Saving...' : 'Save settings'}
          </button>
        </div>
      </div>

      {settingsQuery.isLoading && <p className="empty-state">Loading protocol settings...</p>}

      {settingsQuery.isError && (
        <div className="form-message form-message-error">Failed to load protocol settings.</div>
      )}

      {updateSettingsMutation.isError && (
        <div className="form-message form-message-error">
          Failed to save protocol settings. Check port, network, and flow values.
        </div>
      )}

      {realityKeypairMutation.isError && (
        <div className="form-message form-message-error">Failed to generate Reality keypair.</div>
      )}

      {updateSettingsMutation.isSuccess && (
        <div className="form-message">Protocol settings saved.</div>
      )}

      {realityKeypairMutation.isSuccess && (
        <div className="form-message">Reality keypair generated. Only the public key is shown.</div>
      )}

      {settingsQuery.data && (
        <>
          <div className="protocol-settings-grid">
            <label className="field">
              <span>VLESS port</span>
              <input
                inputMode="numeric"
                min="1"
                max="65535"
                type="number"
                value={form.vlessPort}
                onChange={(event) => updateField('vlessPort', event.target.value)}
              />
            </label>

            <label className="field">
              <span>VLESS flow</span>
              <select
                value={form.vlessFlow}
                onChange={(event) => updateField('vlessFlow', event.target.value)}
              >
                <option value="">Default</option>
                <option value="xtls-rprx-vision">xtls-rprx-vision</option>
              </select>
            </label>

            <label className="field">
              <span>VLESS network</span>
              <select
                value={form.vlessNetwork}
                onChange={(event) => updateField('vlessNetwork', event.target.value)}
              >
                <option value="">Default</option>
                <option value="tcp">tcp</option>
                <option value="ws">ws</option>
                <option value="grpc">grpc</option>
                <option value="http">http</option>
              </select>
            </label>

            <label className="field">
              <span>Reality public key</span>
              <input
                value={form.realityPublicKey}
                onChange={(event) => updateField('realityPublicKey', event.target.value)}
              />
            </label>

            <label className="field">
              <span>Reality short ID</span>
              <input
                value={form.realityShortId}
                onChange={(event) => updateField('realityShortId', event.target.value)}
              />
            </label>

            <label className="field">
              <span>Reality server name</span>
              <input
                value={form.realityServerName}
                onChange={(event) => updateField('realityServerName', event.target.value)}
              />
            </label>
          </div>

          <div className="protocol-settings-meta">
            <span>Protocol: {settingsQuery.data.protocol}</span>
            <span>Reality: {settingsQuery.data.reality.enabled ? 'enabled' : 'disabled'}</span>
            <span>Private key: server-side only</span>
            <span>Updated: {formatDate(settingsQuery.data.updatedAt)}</span>
          </div>
        </>
      )}
    </form>
  );
}
