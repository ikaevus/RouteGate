import { FormEvent, useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  generateRealityKeypair,
  getProtocolSettings,
  updateProtocolSettings,
  type UpdateProtocolSettingsRequest,
} from '../../entities/server/api/serverApi';
import { t } from '../../shared/i18n/i18n';

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

function toRequest(
  form: ProtocolSettingsFormState,
  savedRealityPublicKey: string,
): UpdateProtocolSettingsRequest {
  const request: UpdateProtocolSettingsRequest = {
    vlessPort: Number(form.vlessPort),
    vlessFlow: form.vlessFlow.trim(),
    vlessNetwork: form.vlessNetwork.trim(),
    realityShortId: form.realityShortId.trim(),
    realityServerName: form.realityServerName.trim(),
  };
  const realityPublicKey = form.realityPublicKey.trim();
  if (realityPublicKey !== savedRealityPublicKey.trim()) {
    request.realityPublicKey = realityPublicKey;
  }
  return request;
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
    onSuccess: (response) => {
      setForm((current) => ({
        ...current,
        realityPublicKey: response.reality.publicKey ?? '',
      }));
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
    updateSettingsMutation.mutate(
      toRequest(form, settingsQuery.data?.reality.publicKey ?? ''),
    );
  }

  const portNumber = Number(form.vlessPort);
  const canSave =
    Number.isInteger(portNumber) &&
    portNumber >= 1 &&
    portNumber <= 65535 &&
    !updateSettingsMutation.isPending &&
    !realityKeypairMutation.isPending;
  const translatedKeypairActionLabel = form.realityPublicKey.trim() === ''
    ? t('protocolSettings.generateRealityKeypair')
    : t('protocolSettings.rotateRealityKeypair');

  return (
    <form className="panel protocol-settings-panel" onSubmit={handleSubmit}>
      <div className="panel-header">
        <div>
          <div className="panel-title">{t('protocolSettings.protocolSettingsTitle')}</div>
          <p className="panel-subtitle">{t('protocolSettings.protocolSettingsSubtitle')}</p>
        </div>
        <div className="table-actions">
          <button
            className="small-button"
            type="button"
            disabled={realityKeypairMutation.isPending || updateSettingsMutation.isPending || !settingsQuery.data}
            onClick={() => realityKeypairMutation.mutate()}
          >
            {realityKeypairMutation.isPending ? t('protocolSettings.generating') : translatedKeypairActionLabel}
          </button>
          <button className="small-button" type="submit" disabled={!canSave}>
            {updateSettingsMutation.isPending ? t('protocolSettings.saving') : t('protocolSettings.saveSettings')}
          </button>
        </div>
      </div>

      {settingsQuery.isLoading && <p className="empty-state">{t('protocolSettings.loading')}</p>}

      {settingsQuery.isError && (
        <div className="form-message form-message-error">{t('protocolSettings.protocolLoadError')}</div>
      )}

      {updateSettingsMutation.isError && (
        <div className="form-message form-message-error">
          {t('protocolSettings.protocolSaveError')}
        </div>
      )}

      {realityKeypairMutation.isError && (
        <div className="form-message form-message-error">{t('protocolSettings.keypairError')}</div>
      )}

      {updateSettingsMutation.isSuccess && (
        <div className="form-message">{t('protocolSettings.saved')}</div>
      )}

      {realityKeypairMutation.isSuccess && (
        <div className="form-message">{t('protocolSettings.keypairGenerated')}</div>
      )}

      {settingsQuery.data && (
        <>
          <div className="protocol-settings-grid">
            <label className="field">
              <span>{t('protocolSettings.vlessPort')}</span>
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
              <span>{t('protocolSettings.vlessFlow')}</span>
              <select
                value={form.vlessFlow}
                onChange={(event) => updateField('vlessFlow', event.target.value)}
              >
                <option value="">{t('protocolSettings.default')}</option>
                <option value="xtls-rprx-vision">xtls-rprx-vision</option>
              </select>
            </label>

            <label className="field">
              <span>{t('protocolSettings.vlessNetwork')}</span>
              <select
                value={form.vlessNetwork}
                onChange={(event) => updateField('vlessNetwork', event.target.value)}
              >
                <option value="">{t('protocolSettings.default')}</option>
                <option value="tcp">tcp</option>
                <option value="ws">ws</option>
                <option value="grpc">grpc</option>
                <option value="http">http</option>
              </select>
            </label>

            <label className="field">
              <span>{t('protocolSettings.realityPublicKey')}</span>
              <input
                value={form.realityPublicKey}
                onChange={(event) => updateField('realityPublicKey', event.target.value)}
              />
            </label>

            <label className="field">
              <span>{t('protocolSettings.realityShortId')}</span>
              <input
                value={form.realityShortId}
                onChange={(event) => updateField('realityShortId', event.target.value)}
              />
            </label>

            <label className="field">
              <span>{t('protocolSettings.realityServerName')}</span>
              <input
                value={form.realityServerName}
                onChange={(event) => updateField('realityServerName', event.target.value)}
              />
            </label>
          </div>

          <div className="protocol-settings-meta">
            <span>{t('protocolSettings.protocolValue', { value: settingsQuery.data.protocol })}</span>
            <span>{t('protocolSettings.realityValue', { value: settingsQuery.data.reality.enabled ? t('vpnAccounts.enabled') : t('vpnAccounts.disabled') })}</span>
            <span>{t('protocolSettings.privateKeyServerSide')}</span>
            <span>{t('protocolSettings.updatedValue', { value: formatDate(settingsQuery.data.updatedAt) })}</span>
          </div>
        </>
      )}
    </form>
  );
}
