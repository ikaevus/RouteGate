import { type FormEvent, useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { getNodeGroups } from '../../entities/nodeGroup/api/nodeGroupApi';
import { getRoutingProfiles } from '../../entities/routingProfile/api/routingProfileApi';
import {
  assignVpnAccountNodeGroup,
  assignVpnAccountRoutingProfile,
  clearVpnAccountNodeGroup,
  clearVpnAccountRoutingProfile,
  getVpnAccountRoutingPolicy,
  type RoutingProfileSource,
} from '../../entities/vpnAccount/api/vpnAccountApi';
import { t } from '../../shared/i18n/i18n';
import './vpnAccountRoutingPolicy.css';

function sourceLabel(source: RoutingProfileSource): string {
  switch (source) {
    case 'account': return t('routingPolicy.sourceAccount');
    case 'server': return t('routingPolicy.sourceServer');
    case 'default': return t('routingPolicy.sourceDefault');
    default: return t('routingPolicy.sourceNone');
  }
}

export function VpnAccountRoutingPolicyPanel({ accountId }: { accountId: string }) {
  const queryClient = useQueryClient();
  const [routingProfileId, setRoutingProfileId] = useState('');
  const [nodeGroupId, setNodeGroupId] = useState('');

  const policyQuery = useQuery({
    queryKey: ['vpn-account-routing-policy', accountId],
    queryFn: () => getVpnAccountRoutingPolicy(accountId),
  });
  const profilesQuery = useQuery({ queryKey: ['routing-profiles'], queryFn: getRoutingProfiles });
  const groupsQuery = useQuery({ queryKey: ['node-groups'], queryFn: getNodeGroups });

  useEffect(() => {
    setRoutingProfileId(policyQuery.data?.explicitRoutingProfile?.id ?? '');
    setNodeGroupId(policyQuery.data?.nodeGroup?.id ?? '');
  }, [policyQuery.data]);

  async function refreshPolicy() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['vpn-account-routing-policy', accountId] }),
      queryClient.invalidateQueries({ queryKey: ['vpn-account-client-connection', accountId] }),
      queryClient.invalidateQueries({ queryKey: ['vpn-account-credentials', accountId] }),
      queryClient.invalidateQueries({ queryKey: ['vpn-accounts'] }),
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

  function saveProfile(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    profileMutation.mutate();
  }

  function saveGroup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    groupMutation.mutate();
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
      {(profileMutation.isError || groupMutation.isError) && <div className="form-message form-message-error">{t('routingPolicy.saveError')}</div>}
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
            {policy.nodeGroup && (
              <div className={`form-message ${policy.currentServerInGroup ? 'form-message-success' : 'form-message-warning'}`}>
                {policy.currentServerInGroup ? t('routingPolicy.currentInGroup') : t('routingPolicy.currentOutsideGroup')}
              </div>
            )}
            <p className="routing-policy-automation-note">{t('routingPolicy.noAutomaticSelection')}</p>
            <div className="form-actions">
              <button className="small-button" type="submit" disabled={groupMutation.isPending}>{nodeGroupId ? t('routingPolicy.saveGroup') : t('routingPolicy.clearGroup')}</button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
}
