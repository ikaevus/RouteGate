import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { createManagedSet, updateManagedSet, deleteManagedSet, refreshManagedSet, getRuleSetProviders, diagnoseRouting,
  type ManagedSetInput, type ManagedRuleSet, type RoutingProfile, type RoutingRuleAction } from '../../entities/routingProfile/api/routingProfileApi';
import { t } from '../../shared/i18n/i18n';
import './managedRouting.css';
import { managedSetInput, canEnableManagedSet, managedSourceHealth } from './managedRoutingModel';

const emptySet: ManagedSetInput = { name: '', provider: 'custom', sourceUrl: '', priority: 2000, action: 'vpn', enabled: false, refreshHours: 24 };
const date = (value: string | null) => value ? new Date(value).toLocaleString() : t('common.notAvailable');
const errorText = (error: unknown) => error instanceof Error ? error.message : t('managedRouting.error');
export function ManagedRoutingPanel({ profile }: { profile: RoutingProfile }) {
  const queryClient = useQueryClient();
  const [tab, setTab] = useState<'sets' | 'diagnostics'>('sets');
  const [form, setForm] = useState<ManagedSetInput>(emptySet);
  const [editing, setEditing] = useState<string>();
  const [destination, setDestination] = useState('');
  const [resolvedIP, setResolvedIP] = useState('');
  const providers = useQuery({ queryKey: ['routing-rule-set-providers'], queryFn: getRuleSetProviders });
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['routing-profile', profile.id] });
  const save = useMutation({ mutationFn: () => editing ? updateManagedSet(profile.id, editing, form) : createManagedSet(profile.id, form),
    onSuccess: async () => { setEditing(undefined); setForm(emptySet); await invalidate(); } });
  const refresh = useMutation({ mutationFn: (id: string) => refreshManagedSet(profile.id, id), onSuccess: invalidate });
  const remove = useMutation({ mutationFn: (id: string) => deleteManagedSet(profile.id, id), onSuccess: invalidate });
  const toggle = useMutation({ mutationFn: (s: ManagedRuleSet) => updateManagedSet(profile.id, s.id, { ...managedSetInput(s), enabled: !s.enabled }), onSuccess: invalidate });
  const diagnostic = useMutation({ mutationFn: () => diagnoseRouting(profile.id, destination.trim(), resolvedIP.trim()) });
  const pending = save.isPending || refresh.isPending || remove.isPending || toggle.isPending;
  const result = diagnostic.data;
  const resetDiagnostic = diagnostic.reset;
  useEffect(() => { resetDiagnostic(); }, [profile, resetDiagnostic]);
  return <div className='panel managed-routing-panel'>
    <div className='panel-header'><div className='table-actions' role='tablist' aria-label={t('managedRouting.title')}>
      <button type='button' role='tab' aria-selected={tab === 'sets'} className='small-button' onClick={() => setTab('sets')}>{t('managedRouting.title')}</button>
      <button type='button' role='tab' aria-selected={tab === 'diagnostics'} className='small-button' onClick={() => setTab('diagnostics')}>{t('managedRouting.diagnostics')}</button>
    </div></div>
    {tab === 'sets' ? <>
      <p className='panel-subtitle'>{t('managedRouting.help')}</p>
      <p className='panel-subtitle'>{t('managedRouting.precedence')}</p>
      {[save.error, refresh.error, remove.error, toggle.error, providers.error].filter(Boolean).map((e, i) => <p key={i} role='alert' className='form-message form-message-error'>{errorText(e)}</p>)}
      {(profile.managedSets ?? []).length === 0 && <p>{t('managedRouting.empty')}</p>}
      <div className='managed-routing-list'>
        {(profile.managedSets ?? []).map(s => <article className='managed-routing-item' key={s.id}>
          <div><strong>{s.name}</strong><span className='stage-pill'>{s.action.toUpperCase()}</span><span>{t('routingProfiles.priority')}: {s.priority}</span></div>
          <p className='managed-routing-source'>{providers.data?.items.find(p => p.id === s.provider)?.name ?? s.provider} · {s.sourceUrl}</p>
          <p>{s.enabled ? t('routingProfiles.enabled') : t('managedRouting.disabled')} · {t(`managedRouting.${managedSourceHealth(s)}`)}</p>
          <p>{t('managedRouting.lastSuccess')}: {date(s.lastSuccessAt)} · {t('managedRouting.lastAttempt')}: {date(s.lastAttemptAt)}</p>
          {s.lastError && <p role='alert' className='form-message form-message-error'>{s.lastError} {s.lastSuccessAt ? t('managedRouting.lastGood') : t('managedRouting.noSnapshot')}</p>}
          <div className='table-actions'>
            <button type='button' className='small-button' disabled={pending} onClick={() => refresh.mutate(s.id)}>{refresh.isPending && refresh.variables === s.id ? t('common.loading') : t('managedRouting.refresh')}</button>
            <button type='button' className='small-button' disabled={pending || (!s.enabled && !canEnableManagedSet(s))} onClick={() => toggle.mutate(s)}>{s.enabled ? t('managedRouting.disable') : t('managedRouting.enable')}</button>
            <button type='button' className='small-button' disabled={pending} onClick={() => { setEditing(s.id); setForm(managedSetInput(s)); }}>{t('routingProfiles.edit')}</button>
            <button type='button' className='small-button' disabled={pending} onClick={() => remove.mutate(s.id)}>{t('routingProfiles.deleteProfile')}</button>
          </div>
        </article>)}
      </div>
      <form onSubmit={e => { e.preventDefault(); save.mutate(); }}>
        <h3>{editing ? t('managedRouting.edit') : t('managedRouting.add')}</h3>
        <div className='routing-rule-form-grid'>
          <label className='field'><span>{t('routingProfiles.name')}</span><input required maxLength={120} value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} /></label>
          <label className='field'><span>{t('managedRouting.provider')}</span><select disabled={!!editing} value={form.provider} onChange={e => {
            const p = providers.data?.items.find(p => p.id === e.target.value);
            setForm({ ...form, provider: e.target.value, sourceUrl: p?.url ?? '', name: p?.id === 'custom' ? '' : p?.name ?? '', action: p?.action ?? 'vpn' });
          }}><option value='custom'>{t('managedRouting.custom')}</option>{providers.data?.items.filter(p => p.id !== 'custom').map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select></label>
          <label className='field'><span>{t('routingProfiles.priority')}</span><input type='number' required min={0} max={1000000} value={form.priority} onChange={e => setForm({ ...form, priority: Number(e.target.value) })} /></label>
          <label className='field'><span>{t('routingProfiles.action')}</span><select value={form.action} onChange={e => setForm({ ...form, action: e.target.value as RoutingRuleAction })}><option value='direct'>{t('routingProfiles.actionDirect')}</option><option value='vpn'>VPN</option><option value='block'>{t('routingProfiles.actionBlock')}</option></select></label>
          <label className='field'><span>{t('managedRouting.refreshHours')}</span><input type='number' required min={1} max={168} value={form.refreshHours} onChange={e => setForm({ ...form, refreshHours: Number(e.target.value) })} /></label>
        </div>
        <label className='field'><span>{t('managedRouting.url')}</span><input required type='url' disabled={!!editing} value={form.sourceUrl} onChange={e => setForm({ ...form, sourceUrl: e.target.value })} /></label>
        <p className='panel-subtitle'>{t('managedRouting.sourceHelp')}</p>
        <div className='table-actions'><button className='small-button' disabled={pending} type='submit'>{t('routingProfiles.saveRule')}</button>
          {editing && <button className='small-button' type='button' onClick={() => { setEditing(undefined); setForm(emptySet); }}>{t('routingProfiles.cancelEdit')}</button>}</div>
      </form>
    </> : <>
      <p className='panel-subtitle'>{t('managedRouting.diagnosticHelp')}</p>
      <form onSubmit={e => { e.preventDefault(); diagnostic.mutate(); }}>
        <div className='routing-profile-form-grid'>
          <label className='field'><span>{t('managedRouting.destination')}</span><input required maxLength={253} value={destination} onChange={e => { setDestination(e.target.value); diagnostic.reset(); }} placeholder='example.org' /></label>
          <label className='field'><span>{t('managedRouting.resolvedIp')}</span><input value={resolvedIP} onChange={e => { setResolvedIP(e.target.value); diagnostic.reset(); }} /></label>
        </div>
        <button type='submit' className='small-button' disabled={diagnostic.isPending || !destination.trim()}>{t('managedRouting.test')}</button>
      </form>
      {diagnostic.error && <p role='alert' className='form-message form-message-error'>{errorText(diagnostic.error)}</p>}
      {result && <div className='managed-routing-result' aria-live='polite'>
        <h3>{result.profileName}: {result.action?.toUpperCase() ?? t('managedRouting.indeterminate')}</h3>
        <p>{result.destination}{result.resolvedIp ? ` · ${result.resolvedIp}` : ''}</p>
        {result.winner && <><p>{result.winner.name} · {result.winner.kind === 'managed' ? t('managedRouting.title') : t('managedRouting.manual')}</p>
          <p>{t('routingProfiles.priority')}: {result.winner.priority} · {t('managedRouting.order')}: {result.order}</p>
          <p>{t('managedRouting.created')}: {date(result.winner.createdAt)} · {result.winner.id}</p>
          {result.winner.snapshotSha256 && <p className='managed-routing-source'>SHA-256: {result.winner.snapshotSha256}</p>}</>}
        <p>{result.status === 'indeterminate' ? t('managedRouting.runtimeRequired') : result.status === 'default' ? t('managedRouting.defaultWon') : t('managedRouting.precedence')}</p>
      </div>}
    </>}
  </div>;
}
