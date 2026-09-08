import { type FormEvent, useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createManagedRuleSet,
  deleteManagedRuleSet,
  diagnoseRouting,
  getManagedRuleSets,
  refreshManagedRuleSet,
  updateManagedRuleSet,
  type ManagedRuleSetRequest,
  type RoutingDiagnosticResult,
  type RoutingProfile,
} from '../../entities/routingProfile/api/routingProfileApi';
import { t } from '../../shared/i18n/i18n';

const initialRuleSet: ManagedRuleSetRequest = {
  name: '', provider: 'custom', sourceUrl: '', priority: 1000,
  action: 'vpn', enabled: true, refreshIntervalHours: 24,
};

function formatDate(value?: string): string {
  return value ? new Date(value).toLocaleString() : t('common.notAvailable');
}

export function ManagedRuleSetsPanel({ profiles }: { profiles: RoutingProfile[] }) {
  const queryClient = useQueryClient();
  const [profileId, setProfileId] = useState(profiles[0]?.id ?? '');
  const [form, setForm] = useState(initialRuleSet);
  useEffect(() => { if (!profileId && profiles[0]) setProfileId(profiles[0].id); }, [profileId, profiles]);
  const query = useQuery({ queryKey: ['managed-rule-sets', profileId], queryFn: () => getManagedRuleSets(profileId), enabled: Boolean(profileId) });
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['managed-rule-sets', profileId] });
  const create = useMutation({ mutationFn: () => createManagedRuleSet(profileId, { ...form, name: form.name.trim(), sourceUrl: form.sourceUrl.trim() }), onSuccess: async () => { setForm(initialRuleSet); await invalidate(); } });
  const refresh = useMutation({ mutationFn: refreshManagedRuleSet, onSettled: invalidate });
  const remove = useMutation({ mutationFn: deleteManagedRuleSet, onSuccess: invalidate });
  const toggle = useMutation({ mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => updateManagedRuleSet(id, { enabled }), onSuccess: invalidate });
  const error = create.error ?? refresh.error ?? remove.error ?? toggle.error ?? query.error;

  function submit(event: FormEvent) { event.preventDefault(); if (profileId && form.name.trim() && form.sourceUrl.trim()) create.mutate(); }

  return <div className='routing-profiles-layout'>
    <form className='panel' onSubmit={submit}>
      <div className='panel-header'><div><div className='panel-title'>{t('routingProfiles.managedTitle')}</div><p className='panel-subtitle'>{t('routingProfiles.managedSubtitle')}</p></div><button className='small-button' disabled={!profileId || !form.name.trim() || !form.sourceUrl.trim()}>{t('routingProfiles.addManaged')}</button></div>
      {error && <div className='form-message form-message-error'>{error instanceof Error ? error.message : t('routingProfiles.managedError')}</div>}
      <div className='routing-rule-form-grid'>
        <label className='field'><span>{t('routingProfiles.profile')}</span><select value={profileId} onChange={(event) => setProfileId(event.target.value)}>{profiles.map((profile) => <option key={profile.id} value={profile.id}>{profile.name}</option>)}</select></label>
        <label className='field'><span>{t('routingProfiles.name')}</span><input value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} /></label>
        <label className='field'><span>{t('routingProfiles.provider')}</span><select value={form.provider} onChange={(event) => setForm({ ...form, provider: event.target.value })}><option value='custom'>{t('routingProfiles.providerCustom')}</option><option value='refilter'>{t('routingProfiles.providerRefilter')}</option><option value='runetfreedom'>{t('routingProfiles.providerRunetFreedom')}</option></select></label>
        <label className='field'><span>{t('routingProfiles.sourceUrl')}</span><input type='url' value={form.sourceUrl} onChange={(event) => setForm({ ...form, sourceUrl: event.target.value })} placeholder='https://…/rules.json' /></label>
        <label className='field'><span>{t('routingProfiles.priority')}</span><input type='number' min='0' value={form.priority} onChange={(event) => setForm({ ...form, priority: Number(event.target.value) })} /></label>
        <label className='field'><span>{t('routingProfiles.action')}</span><select value={form.action} onChange={(event) => setForm({ ...form, action: event.target.value as ManagedRuleSetRequest['action'] })}><option value='direct'>DIRECT</option><option value='vpn'>VPN</option><option value='block'>BLOCK</option></select></label>
        <label className='field'><span>{t('routingProfiles.refreshHours')}</span><input type='number' min='1' max='720' value={form.refreshIntervalHours} onChange={(event) => setForm({ ...form, refreshIntervalHours: Number(event.target.value) })} /></label>
      </div>
    </form>
    <div className='panel admin-table-panel'>
      <div className='panel-header'><div><div className='panel-title'>{t('routingProfiles.managedList')}</div><p className='panel-subtitle'>{t('routingProfiles.precedenceHelp')}</p></div></div>
      {!query.data?.items.length ? <p className='empty-state'>{t('routingProfiles.noManaged')}</p> : <div className='admin-table'>
        {query.data.items.map((item) => <div className='admin-table-row routing-managed-row' key={item.id}>
          <div><strong>{item.name}</strong><span>{item.provider} · {item.ruleCount} {t('routingProfiles.sourceRules')}</span></div>
          <div><strong>{item.action.toUpperCase()} · {item.priority}</strong><span>{item.status}{item.lastError ? `: ${item.lastError}` : ''}</span></div>
          <div><strong>{formatDate(item.lastSuccessfulAt)}</strong><span>{item.sourceUrl}</span></div>
          <div className='table-actions'><button className='small-button' onClick={() => refresh.mutate(item.id)}>{t('routingProfiles.refresh')}</button><button className='small-button' onClick={() => toggle.mutate({ id: item.id, enabled: !item.enabled })}>{item.enabled ? t('routingProfiles.disable') : t('routingProfiles.enable')}</button><button className='small-button' onClick={() => remove.mutate(item.id)}>{t('routingProfiles.deleteProfile')}</button></div>
        </div>)}
      </div>}
    </div>
  </div>;
}

export function RoutingDiagnosticsPanel({ profiles }: { profiles: RoutingProfile[] }) {
  const [profileId, setProfileId] = useState(profiles[0]?.id ?? '');
  const [target, setTarget] = useState('');
  const [result, setResult] = useState<RoutingDiagnosticResult | null>(null);
  useEffect(() => { if (!profileId && profiles[0]) setProfileId(profiles[0].id); }, [profileId, profiles]);
  const mutation = useMutation({ mutationFn: () => diagnoseRouting(profileId, target.trim()), onSuccess: setResult });
  return <form className='panel' onSubmit={(event) => { event.preventDefault(); if (profileId && target.trim()) mutation.mutate(); }}>
    <div className='panel-header'><div><div className='panel-title'>{t('routingProfiles.diagnosticsTitle')}</div><p className='panel-subtitle'>{t('routingProfiles.diagnosticsSubtitle')}</p></div><button className='small-button' disabled={!profileId || !target.trim()}>{t('routingProfiles.testRoute')}</button></div>
    <div className='routing-rule-form-grid'><label className='field'><span>{t('routingProfiles.profile')}</span><select value={profileId} onChange={(event) => { setProfileId(event.target.value); setResult(null); }}>{profiles.map((profile) => <option key={profile.id} value={profile.id}>{profile.name}</option>)}</select></label><label className='field'><span>{t('routingProfiles.target')}</span><input value={target} onChange={(event) => setTarget(event.target.value)} placeholder='example.org / 203.0.113.8' /></label></div>
    {mutation.error && <div className='form-message form-message-error'>{mutation.error.message}</div>}
    {result && <div className='routing-diagnostic-result'><span className={`badge badge-${result.action}`}>{result.action.toUpperCase()}</span><strong>{result.profileName}</strong><span>{result.matched ? `${result.matchedName} · ${result.source} · ${t('routingProfiles.priority')} ${result.priority}` : t('routingProfiles.profileDefaultResult')}</span><p>{result.precedence}</p></div>}
  </form>;
}
