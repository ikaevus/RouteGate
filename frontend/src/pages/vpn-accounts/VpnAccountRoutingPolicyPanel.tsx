import { type FormEvent, useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { getNodeGroups } from '../../entities/nodeGroup/api/nodeGroupApi';
import { getRoutingProfiles } from '../../entities/routingProfile/api/routingProfileApi';
import {
  assignVpnAccountNodeGroup,
  assignVpnAccountRoutingProfile,
  applyVpnAccountAutomaticSelection,
  clearVpnAccountNodeGroup,
  clearVpnAccountRoutingProfile,
  getVpnAccountRoutingPolicy,
  previewVpnAccountAutomaticSelection,
  updateVpnAccountAutomaticSelection,
  type RoutingProfileSource,
} from '../../entities/vpnAccount/api/vpnAccountApi';
import { getCurrentLocale, t } from '../../shared/i18n/i18n';
import './vpnAccountRoutingPolicy.css';

function selectionStatusLabel(status: 'selected' | 'current' | 'no_eligible_candidates' | 'node_group_required' | 'cooldown'): string {
  switch (status) {
    case 'selected': return t('automaticSelection.status.selected');
    case 'current': return t('automaticSelection.status.current');
    case 'no_eligible_candidates': return t('automaticSelection.status.no_eligible_candidates');
    case 'node_group_required': return t('automaticSelection.status.node_group_required');
    case 'cooldown': return t('automaticSelection.status.cooldown');
  }
}

function sourceLabel(source: RoutingProfileSource): string {
  switch (source) {
    case 'account': return t('routingPolicy.sourceAccount');
    case 'server': return t('routingPolicy.sourceServer');
    case 'default': return t('routingPolicy.sourceDefault');
    default: return t('routingPolicy.sourceNone');
  }
}

function protocolDisplayName(protocol: string): string {
  switch (protocol.trim().toLowerCase()) {
    case 'vless': return 'VLESS / Reality';
    case 'wireguard': return 'WireGuard';
    case 'hysteria2': return 'Hysteria2';
    case 'shadowsocks': return 'Shadowsocks 2022';
    case 'mtproto': return 'MTProto / FakeTLS';
    default: return protocol;
  }
}

function getCopy() {
  if (getCurrentLocale() === 'ru') {
    return {
      unsavedNodeGroup: 'Группа узлов изменена, но ещё не сохранена.',
      saveNodeGroupFirst: 'Сначала сохраните выбранную группу узлов. Предпросмотр и применение используют только сохранённую группу.',
      savePolicyFirst: 'Настройки автоматического выбора изменены. Сначала сохраните их, чтобы предпросмотр и применение использовали именно эти значения.',
    } as const;
  }

  return {
    unsavedNodeGroup: 'The node group has changed but is not saved yet.',
    saveNodeGroupFirst: 'Save the selected node group first. Preview and Apply use only the persisted group.',
    savePolicyFirst: 'Automatic-selection settings have changed. Save them first so Preview and Apply use these exact values.',
  } as const;
}

export function VpnAccountRoutingPolicyPanel({ accountId }: { accountId: string }) {
  const queryClient = useQueryClient();
  const copy = getCopy();
  const [routingProfileId, setRoutingProfileId] = useState('');
  const [nodeGroupId, setNodeGroupId] = useState('');
  const [automaticSelectionEnabled, setAutomaticSelectionEnabled] = useState(false);
  const [allowDegraded, setAllowDegraded] = useState(false);
  const [cooldownSeconds, setCooldownSeconds] = useState(300);

  const policyQuery = useQuery({
    queryKey: ['vpn-account-routing-policy', accountId],
    queryFn: () => getVpnAccountRoutingPolicy(accountId),
  });
  const profilesQuery = useQuery({ queryKey: ['routing-profiles'], queryFn: getRoutingProfiles });
  const groupsQuery = useQuery({ queryKey: ['node-groups'], queryFn: getNodeGroups });

  const savedNodeGroupId = policyQuery.data?.nodeGroup?.id ?? '';
  const savedSelectionPolicy = policyQuery.data?.automaticSelectionPolicy;
  const nodeGroupDirty = Boolean(policyQuery.data) && nodeGroupId !== savedNodeGroupId;
  const selectionPolicyDirty = Boolean(savedSelectionPolicy)
    && (automaticSelectionEnabled !== savedSelectionPolicy?.enabled
      || allowDegraded !== savedSelectionPolicy?.allowDegraded
      || cooldownSeconds !== savedSelectionPolicy?.cooldownSeconds);
  const automaticSelectionDirty = nodeGroupDirty || selectionPolicyDirty;

  const selectionPreviewQuery = useQuery({
    queryKey: ['vpn-account-automatic-selection-preview', accountId],
    queryFn: () => previewVpnAccountAutomaticSelection(accountId),
    enabled: Boolean(policyQuery.data?.nodeGroup) && !automaticSelectionDirty,
  });

  useEffect(() => {
    setRoutingProfileId(policyQuery.data?.explicitRoutingProfile?.id ?? '');
    setNodeGroupId(policyQuery.data?.nodeGroup?.id ?? '');
    setAutomaticSelectionEnabled(policyQuery.data?.automaticSelectionPolicy.enabled ?? false);
    setAllowDegraded(policyQuery.data?.automaticSelectionPolicy.allowDegraded ?? false);
    setCooldownSeconds(policyQuery.data?.automaticSelectionPolicy.cooldownSeconds ?? 300);
  }, [policyQuery.data]);

  async function refreshPolicy() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['vpn-account-routing-policy', accountId] }),
      queryClient.invalidateQueries({ queryKey: ['vpn-account-client-connection', accountId] }),
      queryClient.invalidateQueries({ queryKey: ['vpn-account-credentials', accountId] }),
      queryClient.invalidateQueries({ queryKey: ['vpn-accounts'] }),
      queryClient.invalidateQueries({ queryKey: ['vpn-account-automatic-selection-preview', accountId] }),
    ]);
  }

  const profileMutation = useMutation({
    mutationFn: () => routingProfileId
      ? assignVpnAccountRoutingProfile(accountId, routingProfileId)
      : clearVpnAccountRoutingProfile(accountId),
    onSuccess: refreshPolicy,
  });
  const groupMutation = useMutation({
    mutationFn: () => nodeGroupId
      ? assignVpnAccountNodeGroup(accountId, nodeGroupId)
      : clearVpnAccountNodeGroup(accountId),
    onSuccess: refreshPolicy,
  });
  const selectionPolicyMutation = useMutation({
    mutationFn: () => updateVpnAccountAutomaticSelection(accountId, {
      enabled: automaticSelectionEnabled,
      allowDegraded,
      cooldownSeconds,
    }),
    onSuccess: refreshPolicy,
  });
  const selectionApplyMutation = useMutation({
    mutationFn: () => applyVpnAccountAutomaticSelection(accountId),
    onSuccess: refreshPolicy,
  });

  function saveProfile(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    profileMutation.mutate();
  }

  function saveGroup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    groupMutation.mutate();
  }

  function saveSelectionPolicy(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    selectionPolicyMutation.mutate();
  }

  const policy = policyQuery.data;
  const hasError = policyQuery.isError || profilesQuery.isError || groupsQuery.isError;

  return (
    <div className="panel feature-detail-panel vpn-account-routing-policy-panel">
      <div className="panel-header">
        <div>
          <div className="panel-title">{t('routingPolicy.title')}</div>
          <p className="panel-subtitle">{t('routingPolicy.subtitle')}</p>
        </div>
      </div>
      {hasError && <div className="form-message form-message-error">{t('routingPolicy.loadError')}</div>}
      {(profileMutation.isError || groupMutation.isError || selectionPolicyMutation.isError || selectionApplyMutation.isError) && <div className="form-message form-message-error">{t('routingPolicy.saveError')}</div>}
      {policy && (
        <div className="vpn-account-routing-policy-grid">
          <form className="routing-policy-card" onSubmit={saveProfile}>
            <label className="field">
              <span>{t('routingPolicy.profile')}</span>
              <select value={routingProfileId} onChange={(event) => setRoutingProfileId(event.target.value)}>
                <option value="">{t('routingPolicy.inherit')}</option>
                {(profilesQuery.data?.items ?? []).map((profile) => <option key={profile.id} value={profile.id}>{profile.name}</option>)}
              </select>
            </label>
            <div className="routing-policy-effective">
              <span>{t('routingPolicy.effective')}</span>
              <strong>{policy.effectiveRoutingProfile?.name ?? t('common.notAvailable')}</strong>
              <small>{t('routingPolicy.source', { source: sourceLabel(policy.routingProfileSource) })}</small>
            </div>
            {!policy.clientRoutingSupported && <div className="form-message form-message-warning">{t('routingPolicy.clientRoutingUnsupported')}</div>}
            <div className="form-actions">
              <button className="small-button" type="submit" disabled={profileMutation.isPending}>{routingProfileId ? t('routingPolicy.saveProfile') : t('routingPolicy.clearProfile')}</button>
            </div>
          </form>

          <form className="routing-policy-card" onSubmit={saveGroup}>
            <label className="field">
              <span>{t('routingPolicy.nodeGroup')}</span>
              <select value={nodeGroupId} onChange={(event) => setNodeGroupId(event.target.value)}>
                <option value="">{t('routingPolicy.noNodeGroup')}</option>
                {(groupsQuery.data?.items ?? []).map((group) => <option key={group.id} value={group.id}>{group.name} · {group.memberCount}</option>)}
              </select>
            </label>
            {policy.nodeGroup && !nodeGroupDirty && (
              <div className={`form-message ${policy.currentServerInGroup ? 'form-message-success' : 'form-message-warning'}`}>
                {policy.currentServerInGroup ? t('routingPolicy.currentInGroup') : t('routingPolicy.currentOutsideGroup')}
              </div>
            )}
            {nodeGroupDirty && <div className="form-message form-message-warning">{copy.unsavedNodeGroup}</div>}
            <p className="routing-policy-automation-note">{t('automaticSelection.groupHelp')}</p>
            <div className="form-actions">
              <button className="small-button" type="submit" disabled={groupMutation.isPending}>{nodeGroupId ? t('routingPolicy.saveGroup') : t('routingPolicy.clearGroup')}</button>
            </div>
          </form>

          <form className="routing-policy-card routing-policy-selection-card" onSubmit={saveSelectionPolicy}>
            <div>
              <strong>{t('automaticSelection.title')}</strong>
              <p className="routing-policy-automation-note">{t('automaticSelection.subtitle')}</p>
            </div>
            <label className="field checkbox-field">
              <input
                type="checkbox"
                checked={automaticSelectionEnabled}
                disabled={!policy.nodeGroup || nodeGroupDirty}
                onChange={(event) => setAutomaticSelectionEnabled(event.target.checked)}
              />
              <span>{t('automaticSelection.enabled')}</span>
            </label>
            <label className="field checkbox-field">
              <input type="checkbox" checked={allowDegraded} disabled={!policy.nodeGroup || nodeGroupDirty} onChange={(event) => setAllowDegraded(event.target.checked)} />
              <span>{t('automaticSelection.allowDegraded')}</span>
            </label>
            <label className="field">
              <span>{t('automaticSelection.cooldown')}</span>
              <input
                type="number"
                min={1}
                max={1440}
                value={Math.round(cooldownSeconds / 60)}
                disabled={!policy.nodeGroup || nodeGroupDirty}
                onChange={(event) => setCooldownSeconds(Math.max(60, Number(event.target.value || 1) * 60))}
              />
            </label>
            <div className="form-actions">
              <button className="small-button" type="submit" disabled={selectionPolicyMutation.isPending || !policy.nodeGroup || nodeGroupDirty}>{t('automaticSelection.save')}</button>
              <button
                className="small-button secondary"
                type="button"
                disabled={!policy.nodeGroup || automaticSelectionDirty || selectionPreviewQuery.isFetching}
                onClick={() => selectionPreviewQuery.refetch()}
              >
                {t('automaticSelection.refresh')}
              </button>
            </div>
            {!policy.nodeGroup && <div className="form-message form-message-warning">{t('automaticSelection.nodeGroupRequired')}</div>}
            {nodeGroupDirty && <div className="form-message form-message-warning">{copy.saveNodeGroupFirst}</div>}
            {!nodeGroupDirty && selectionPolicyDirty && <div className="form-message form-message-warning">{copy.savePolicyFirst}</div>}
            {!automaticSelectionDirty && selectionPreviewQuery.isError && <div className="form-message form-message-error">{t('automaticSelection.previewError')}</div>}
            {!automaticSelectionDirty && selectionPreviewQuery.data && (
              <div className="automatic-selection-decision">
                <span>{selectionStatusLabel(selectionPreviewQuery.data.status)}</span>
                {selectionPreviewQuery.data.selectedCandidate && (
                  <strong>{selectionPreviewQuery.data.selectedCandidate.serverName} · {protocolDisplayName(selectionPreviewQuery.data.selectedCandidate.protocol)}</strong>
                )}
                <small>{t('automaticSelection.eligible', { count: selectionPreviewQuery.data.eligibleCandidates })}</small>
                {selectionPreviewQuery.data.blockedUntil && <small>{t('automaticSelection.blockedUntil', { date: new Date(selectionPreviewQuery.data.blockedUntil).toLocaleString() })}</small>}
              </div>
            )}
            {selectionApplyMutation.data?.configDeploymentRequired && (
              <div className="automatic-selection-next-action">
                <div className="form-message form-message-warning">{t('automaticSelection.deployRequired')}</div>
                <div className="form-actions">
                  {selectionApplyMutation.data.affectedServerIds.map((serverId, index) => (
                    <Link className="small-button" key={serverId} to={`/servers/${encodeURIComponent(serverId)}`}>
                      {index === 0 ? t('automaticSelection.openSelectedNode') : t('automaticSelection.openPreviousNode')}
                    </Link>
                  ))}
                </div>
              </div>
            )}
            <div className="form-actions">
              <button
                className="small-button"
                type="button"
                disabled={automaticSelectionDirty || !selectionPreviewQuery.data?.canApply || selectionApplyMutation.isPending}
                onClick={() => selectionApplyMutation.mutate()}
              >
                {t('automaticSelection.apply')}
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
}
