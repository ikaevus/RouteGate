import { type FormEvent, useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom';
import {
  createRoutingProfile,
  createRoutingProfileRule,
  deleteRoutingProfile,
  deleteRoutingProfileRule,
  getRoutingProfile,
  getRoutingProfiles,
  updateRoutingProfile,
  updateRoutingProfileRule,
  type CreateRoutingProfileRuleRequest,
  type RoutingProfile,
  type RoutingProfileRule,
  type RoutingRuleAction,
} from '../../entities/routingProfile/api/routingProfileApi';
import { WorkspaceNav } from '../../shared/ui/WorkspaceNav';
import './routingWorkspace.css';
import { t, translateStatus } from '../../shared/i18n/i18n';

type RuleForm = CreateRoutingProfileRuleRequest;

const emptyRule: RuleForm = {
  name: '',
  priority: 1000,
  action: 'vpn',
  enabled: true,
  domains: [],
  domainSuffixes: [],
  domainKeywords: [],
  ipCidrs: [],
  geoSites: [],
  geoIps: [],
};

function formatDate(value?: string | null): string {
  if (!value) return t('common.notAvailable');
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function formatValue(value?: string | null): string {
  return value && value.trim() !== '' ? value : t('common.notAvailable');
}

function getErrorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message.trim() !== '' ? error.message : fallback;
}

function StatusBadge({ value }: { value: string }) {
  const className = value.toLowerCase().replace(/[^a-z0-9-]/g, '-');
  return <span className={`badge badge-${className}`}>{translateStatus(value)}</span>;
}

function splitList(value: string): string[] {
  return value.split(/[\n,]+/g).map((item) => item.trim()).filter(Boolean);
}

function joinList(values?: string[]): string {
  return values?.join('\n') ?? '';
}

function hasMatcherText(ruleText: Record<string, string>): boolean {
  return Object.values(ruleText).some((value) => splitList(value).length > 0);
}

function ruleToForm(rule?: RoutingProfileRule): RuleForm {
  if (!rule) return emptyRule;
  return {
    name: rule.name,
    priority: rule.priority,
    action: rule.action,
    enabled: rule.enabled,
    domains: rule.domains ?? [],
    domainSuffixes: rule.domainSuffixes ?? [],
    domainKeywords: rule.domainKeywords ?? [],
    ipCidrs: rule.ipCidrs ?? [],
    geoSites: rule.geoSites ?? [],
    geoIps: rule.geoIps ?? [],
  };
}

function ProfileRow({ profile, selected }: { profile: RoutingProfile; selected: boolean }) {
  return (
    <Link className={`admin-table-row routing-profiles-table-row vpn-account-row-link${selected ? ' vpn-account-row-selected' : ''}`} to={`/routing-profiles/${encodeURIComponent(profile.id)}/overview`} aria-current={selected ? 'page' : undefined}>
      <div>
        <strong>{formatValue(profile.name)}</strong>
        <span>{formatValue(profile.description)}</span>
      </div>
      <StatusBadge value={profile.isDefault ? 'default' : 'custom'} />
      <span>{formatDate(profile.updatedAt)}</span>
    </Link>
  );
}

const matcherFields = [
  { key: 'domains', text: 'domains', label: 'routingProfiles.exactDomains', example: 'example.com' },
  { key: 'domainSuffixes', text: 'suffixes', label: 'routingProfiles.domainSuffixes', example: 'example.org' },
  { key: 'ipCidrs', text: 'cidrs', label: 'routingProfiles.ipCidrs', example: '192.0.2.0/24, 2001:db8::/32' },
  { key: 'domainKeywords', text: 'keywords', label: 'routingProfiles.domainKeywords', example: 'example' },
  { key: 'geoSites', text: 'geosite', label: 'routingProfiles.geoSiteTags', example: 'category-ads-all' },
  { key: 'geoIps', text: 'geoip', label: 'routingProfiles.geoIpTags', example: 'private' },
] as const;

function actionLabel(action: RoutingRuleAction) {
  return action === 'vpn' ? t('routingWorkspace.actionVpn')
    : t(action === 'direct' ? 'routingProfiles.actionDirect' : 'routingProfiles.actionBlock');
}

function RuleMatchers({ rule }: { rule: RoutingProfileRule }) {
  const populated = matcherFields.filter(field => rule[field.key]?.length);
  return populated.length ? <dl className='routing-rule-matchers'>
    {populated.map(field => <div key={field.key}>
      <dt>{t(field.label)}</dt>
      <dd><ul>{rule[field.key]?.map((value, index) => <li key={`${index}:${value}`}><code>{value}</code></li>)}</ul></dd>
    </div>)}
  </dl> : <p>{t('routingProfiles.noMatchers')}</p>;
}

export function RoutingProfilesPage() {
  const { profileId } = useParams<{ profileId: string }>();
  return <RoutingWorkspace key={profileId ?? 'list'} />;
}

const sections = ['overview', 'rules', 'settings'] as const;

function RoutingWorkspace() {
  const { profileId, section } = useParams<{ profileId: string; section: string }>();
  const navigate = useNavigate();
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const location = useLocation();
  const activeSection = sections.includes(section as typeof sections[number]) ? section : 'overview';
  const sectionPath = (value: string) => `/routing-profiles/${encodeURIComponent(profileId ?? '')}/${value}${location.search}`;
  const [profileDirty, setProfileDirty] = useState(false);
  const [ruleEditorOpen, setRuleEditorOpen] = useState(false);
  const [selectedRuleId, setSelectedRuleId] = useState<string | null>(null);
  const [advancedMatchersOpen, setAdvancedMatchersOpen] = useState(false);
  const ruleEditorRef = useRef<HTMLFormElement>(null);
  const rulesPanelRef = useRef<HTMLDivElement>(null);
  function restoreRuleFocus() {
    requestAnimationFrame(() => {
      const panel = rulesPanelRef.current;
      (panel?.querySelector<HTMLButtonElement>('.routing-rule-choice[aria-pressed="true"]')
        ?? panel?.querySelector<HTMLButtonElement>('.panel-header button'))?.focus();
    });
  }
  useEffect(() => {
    if (profileId && section !== activeSection) navigate(sectionPath('overview'), { replace: true, state: location.state });
  }, [profileId, section, activeSection, location.search, navigate]);
  useEffect(() => {
    if (ruleEditorOpen && activeSection === 'rules') {
      ruleEditorRef.current?.scrollIntoView({ block: 'nearest' });
      ruleEditorRef.current?.querySelector<HTMLInputElement>('input')?.focus({ preventScroll: true });
    }
  }, [ruleEditorOpen, activeSection]);
  const queryClient = useQueryClient();
  const [profileName, setProfileName] = useState('');
  const [profileDescription, setProfileDescription] = useState('');
  const [makeDefault, setMakeDefault] = useState(false);
  const [ruleForm, setRuleForm] = useState<RuleForm>(emptyRule);
  const [editingRuleId, setEditingRuleId] = useState<string | null>(null);
  const [ruleText, setRuleText] = useState({ domains: '', suffixes: '', keywords: '', cidrs: '', geosite: '', geoip: '' });

  const profilesQuery = useQuery({ queryKey: ['routing-profiles'], queryFn: getRoutingProfiles });
  const profileQuery = useQuery({
    queryKey: ['routing-profile', profileId],
    queryFn: () => getRoutingProfile(profileId ?? ''),
    enabled: Boolean(profileId),
  });

  useEffect(() => {
    if (!profileQuery.data || profileDirty) return;
    setProfileName(profileQuery.data.name);
    setProfileDescription(profileQuery.data.description ?? '');
    setMakeDefault(profileQuery.data.isDefault);
  }, [profileQuery.data, profileDirty]);

  function resetRuleForm() {
    setRuleEditorOpen(false);
    setAdvancedMatchersOpen(false);
    setEditingRuleId(null);
    setRuleForm(emptyRule);
    setRuleText({ domains: '', suffixes: '', keywords: '', cidrs: '', geosite: '', geoip: '' });
  }

  const createProfileMutation = useMutation({
    mutationFn: createRoutingProfile,
    onSuccess: async (profile) => {
      await queryClient.invalidateQueries({ queryKey: ['routing-profiles'] });
      if (mounted.current) navigate(`/routing-profiles/${encodeURIComponent(profile.id)}/rules${location.search}`);
    },
  });

  const updateProfileMutation = useMutation({
    mutationFn: () => updateRoutingProfile(profileId ?? '', { name: profileName.trim(), description: profileDescription.trim(), isDefault: makeDefault }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['routing-profiles'] });
      await queryClient.invalidateQueries({ queryKey: ['routing-profile', profileId] });
      setProfileDirty(false);
    },
  });

  const deleteProfileMutation = useMutation({
    mutationFn: () => deleteRoutingProfile(profileId ?? ''),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['routing-profiles'] });
      if (mounted.current) navigate(`/routing-profiles${location.search}`, { replace: true });
    },
  });

  const saveRuleMutation = useMutation({
    mutationFn: (request: RuleForm) => editingRuleId
      ? updateRoutingProfileRule(profileId ?? '', editingRuleId, request)
      : createRoutingProfileRule(profileId ?? '', request),
    onSuccess: async (rule) => {
      setSelectedRuleId(rule.id);
      resetRuleForm();
      await queryClient.invalidateQueries({ queryKey: ['routing-profile', profileId] });
      restoreRuleFocus();
    },
  });

  const deleteRuleMutation = useMutation({
    mutationFn: (ruleId: string) => deleteRoutingProfileRule(profileId ?? '', ruleId),
    onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['routing-profile', profileId] }),
  });

  function handleCreateProfile(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    createProfileMutation.mutate({ name: t('routingWorkspace.newProfileName'), description: '', isDefault: false });
  }

  function handleRuleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canSaveRule || actionPending) return;

    saveRuleMutation.mutate({
      ...ruleForm,
      name: ruleForm.name.trim(),
      domains: splitList(ruleText.domains),
      domainSuffixes: splitList(ruleText.suffixes),
      domainKeywords: splitList(ruleText.keywords),
      ipCidrs: splitList(ruleText.cidrs),
      geoSites: splitList(ruleText.geosite),
      geoIps: splitList(ruleText.geoip),
    });
  }

  function editRule(rule: RoutingProfileRule) {
    setAdvancedMatchersOpen(Boolean(rule.domainKeywords?.length || rule.geoSites?.length || rule.geoIps?.length));
    saveRuleMutation.reset();
    setRuleEditorOpen(true);
    setEditingRuleId(rule.id);
    setRuleForm(ruleToForm(rule));
    setRuleText({
      domains: joinList(rule.domains),
      suffixes: joinList(rule.domainSuffixes),
      keywords: joinList(rule.domainKeywords),
      cidrs: joinList(rule.ipCidrs),
      geosite: joinList(rule.geoSites),
      geoip: joinList(rule.geoIps),
    });
  }

  const profiles = profilesQuery.data?.items ?? [];
  const selectedProfile = profileQuery.data;
  const rules = selectedProfile?.rules ?? [];
  const selectedRule = rules.find(rule => rule.id === selectedRuleId) ?? rules[0];
  const actionPending = updateProfileMutation.isPending || deleteProfileMutation.isPending
    || saveRuleMutation.isPending || deleteRuleMutation.isPending;
  const canSaveRule = ruleForm.name.trim() !== ''
    && Number.isInteger(ruleForm.priority)
    && ruleForm.priority >= 0
    && hasMatcherText(ruleText);

  return (
    <section className='page routing-profiles-page'>
      <div className='page-header'>
        <div>
          <h1>{t('routingProfiles.title')}</h1>
          <p>{t('routingProfiles.subtitle')}</p>
        </div>
        {profilesQuery.isSuccess && <div className='status-pill'>{t('routingProfiles.profileCount', { count: profiles.length })}</div>}
      </div>

      <div className='routing-profiles-layout'>
        <form className='panel' onSubmit={handleCreateProfile}>
          <div className='panel-header'>
            <div>
              <div className='panel-title'>{t('routingProfiles.profilesPanelTitle')}</div>
              <p className='panel-subtitle'>{t('routingProfiles.profilesPanelSubtitle')}</p>
            </div>
            <button className='small-button' type='submit' disabled={createProfileMutation.isPending}>{t('routingProfiles.createProfile')}</button>
          </div>

          {createProfileMutation.isError && <div className='form-message form-message-error'>{getErrorMessage(createProfileMutation.error, t('routingProfiles.createError'))}</div>}
          {profilesQuery.isLoading && <p className='empty-state'>{t('routingProfiles.loading')}</p>}
          {profilesQuery.isError && <div className='form-message form-message-error'>{getErrorMessage(profilesQuery.error, t('routingProfiles.loadError'))}</div>}
          {profilesQuery.isSuccess && profiles.length === 0 && <p className='empty-state'>{t('routingWorkspace.noProfiles')}</p>}
          {profiles.length > 0 && (
            <div className='admin-table routing-profiles-table'>
              <div className='admin-table-row admin-table-head routing-profiles-table-row'><span>{t('routingProfiles.profile')}</span><span>{t('routingProfiles.type')}</span><span>{t('routingProfiles.updated')}</span></div>
              {profiles.map((profile) => <ProfileRow key={profile.id} profile={profile} selected={profile.id === profileId} />)}
            </div>
          )}
        </form>

        {!profileId && <div className='panel'><p className='empty-state'>{t('routingProfiles.selectProfile')}</p></div>}
        {profileQuery.isLoading && <p className='empty-state'>{t('common.loading')}</p>}
        {profileQuery.isError && <div className='form-message form-message-error'>{getErrorMessage(profileQuery.error, t('routingProfiles.selectedLoadError'))}</div>}

        {selectedProfile && (
          <div className='routing-workspace'>
            <header className='panel routing-workspace-header'>
              <div><h2>{selectedProfile.name}</h2><p>{formatValue(selectedProfile.description)}</p></div>
              <StatusBadge value={selectedProfile.isDefault ? 'default' : 'custom'} />
            </header>
            <WorkspaceNav label={t('routingWorkspace.navigation')} items={sections.map(value => ({ href: sectionPath(value), label: t(`routingWorkspace.${value}`) }))} />
            <div className='routing-workspace-content' data-route-scroll-target tabIndex={-1}>
              {activeSection === 'overview' && <section className='panel routing-workspace-overview'>
                <h3>{t('routingWorkspace.overview')}</h3>
                <p>{t('routingProfiles.ruleCount', { count: rules.length })}</p>
                <p>{t('routingProfiles.updatedValue', { value: formatDate(selectedProfile.updatedAt) })}</p>
                <p>{t(rules.length === 0 ? 'routingWorkspace.nextAddRule' : 'routingWorkspace.nextReviewRules')}</p>
                <Link className='primary-button' to={sectionPath('rules')}>{t('routingWorkspace.openRules')}</Link>
              </section>}
              <div className='routing-workspace-domain' hidden={activeSection !== 'settings'}>
            <form className='panel routing-profile-details-panel' onSubmit={(event) => { event.preventDefault(); if (!actionPending && profileName.trim()) updateProfileMutation.mutate(); }}>
              <div className='panel-header'>
                <div>
                  <div className='panel-title'>{t('routingProfiles.detailsTitle')}</div>
                  <p className='panel-subtitle'>{t('routingProfiles.detailsSubtitle')}</p>
                </div>
                <div className='table-actions'>
                  <button className='small-button' type='button' disabled={selectedProfile.isDefault || actionPending} onClick={() => { if (window.confirm(t('routingWorkspace.deleteProfileConfirm', { name: selectedProfile.name }))) deleteProfileMutation.mutate(); }}>{t('routingProfiles.deleteProfile')}</button>
                  <button className='small-button' type='submit' disabled={profileName.trim() === '' || actionPending}>{t('routingProfiles.saveProfile')}</button>
                </div>
              </div>
              {updateProfileMutation.isError && <div className='form-message form-message-error'>{getErrorMessage(updateProfileMutation.error, t('routingProfiles.updateError'))}</div>}
              {deleteProfileMutation.isError && <div className='form-message form-message-error'>{getErrorMessage(deleteProfileMutation.error, t('routingProfiles.deleteError'))}</div>}
              <fieldset className='routing-profile-form-grid' disabled={actionPending}>
                <label className='field'><span>{t('routingProfiles.name')}</span><input value={profileName} onChange={(event) => { setProfileDirty(true); setProfileName(event.target.value); }} /></label>
                <label className='field'><span>{t('routingProfiles.description')}</span><input value={profileDescription} onChange={(event) => { setProfileDirty(true); setProfileDescription(event.target.value); }} /></label>
                <div className='traffic-checkbox-field routing-profile-default-field'><label><input checked={makeDefault} type='checkbox' onChange={(event) => { setProfileDirty(true); setMakeDefault(event.target.checked); }} />{t('routingProfiles.defaultProfile')}</label><p>{t('routingProfiles.updatedValue', { value: formatDate(selectedProfile.updatedAt) })}</p></div>
              </fieldset>
            </form>
              </div>
              <div className='routing-workspace-domain routing-workspace-rules' hidden={activeSection !== 'rules'}>
            <div ref={rulesPanelRef} className='panel admin-table-panel routing-rules-panel'>
              <div className='panel-header'><div><div className='panel-title'>{t('routingProfiles.rules')}</div><p className='panel-subtitle'>{t('routingProfiles.rulesSubtitle')}</p></div><button className='small-button' type='button' disabled={actionPending || ruleEditorOpen} onClick={() => { resetRuleForm(); saveRuleMutation.reset(); setRuleEditorOpen(true); }}>{t('routingProfiles.addRule')}</button></div>
              {deleteRuleMutation.isError && <div className='form-message form-message-error'>{getErrorMessage(deleteRuleMutation.error, t('routingProfiles.deleteRuleError'))}</div>}
              {rules.length === 0 ? <p className='empty-state'>{t('routingProfiles.noRules')}</p> : (
                <div className={`routing-rule-browser${ruleEditorOpen ? ' is-editing' : ''}`}>
                  <div className='routing-rule-list' role='group' aria-label={t('routingProfiles.rules')}>
                    {rules.map(rule => <button key={rule.id} type='button' className='routing-rule-choice'
                      aria-pressed={selectedRule?.id === rule.id} disabled={ruleEditorOpen}
                      onClick={() => setSelectedRuleId(rule.id)}>
                      <strong>{rule.name}</strong>
                      <span>{t('routingProfiles.priority')}: {rule.priority} · {actionLabel(rule.action)}</span>
                      <StatusBadge value={rule.enabled ? 'enabled' : 'disabled'} />
                    </button>)}
                  </div>
                  {selectedRule && !ruleEditorOpen && <section className='routing-rule-detail' aria-label={selectedRule.name}>
                    <h3>{selectedRule.name}</h3>
                    <div className='routing-rule-context'>
                      <span>{actionLabel(selectedRule.action)}</span>
                      <StatusBadge value={selectedRule.enabled ? 'enabled' : 'disabled'} />
                      <span>{t('routingProfiles.priority')}: {selectedRule.priority}</span>
                    </div>
                    <p className='muted-text'>{t('routingProfiles.updatedValue', { value: formatDate(selectedRule.updatedAt) })}</p>
                    <h4>{t('routingProfiles.matchers')}</h4>
                    <RuleMatchers rule={selectedRule} />
                    <div className='table-actions'>
                      <button className='small-button' type='button' disabled={actionPending || ruleEditorOpen} onClick={() => editRule(selectedRule)}>{t('routingProfiles.edit')}</button>
                      <button className='small-button' type='button' disabled={actionPending || ruleEditorOpen} onClick={() => { if (window.confirm(t('routingWorkspace.deleteRuleConfirm', { name: selectedRule.name }))) deleteRuleMutation.mutate(selectedRule.id); }}>{t('routingProfiles.deleteProfile')}</button>
                    </div>
                  </section>}
                </div>
              )}
            </div>
            <form ref={ruleEditorRef} hidden={!ruleEditorOpen} className='panel routing-rule-form' onSubmit={handleRuleSubmit}>
              <div className='panel-header'>
                <div>
                  <div className='panel-title'>{editingRuleId ? t('routingProfiles.editRule') : t('routingProfiles.addRule')}</div>
                  <p className='panel-subtitle'>{t('routingProfiles.ruleHelp')}</p>
                </div>
                <div className='table-actions'>
                  <button className='small-button' type='button' disabled={actionPending} onClick={() => { resetRuleForm(); restoreRuleFocus(); }}>{t('routingProfiles.cancelEdit')}</button>
                  <button className='small-button' type='submit' disabled={!canSaveRule || actionPending}>{t('routingProfiles.saveRule')}</button>
                </div>
              </div>
              {saveRuleMutation.isError && <div className='form-message form-message-error'>{getErrorMessage(saveRuleMutation.error, t('routingProfiles.saveRuleError'))}</div>}
              {!hasMatcherText(ruleText) && <div className='form-message form-message-warning'>{t('routingProfiles.matcherWarning')}</div>}
              <fieldset disabled={actionPending} className='routing-rule-inputs'>
              <div className='routing-rule-form-grid'>
                <label className='field'><span>{t('routingProfiles.name')}</span><input value={ruleForm.name} onChange={(event) => setRuleForm((current) => ({ ...current, name: event.target.value }))} /></label>
                <label className='field'><span>{t('routingProfiles.priority')}</span><input min='0' type='number' value={ruleForm.priority} onChange={(event) => setRuleForm((current) => ({ ...current, priority: Number(event.target.value) }))} /></label>
                <label className='field'><span>{t('routingProfiles.action')}</span><select value={ruleForm.action} onChange={(event) => setRuleForm((current) => ({ ...current, action: event.target.value as RoutingRuleAction }))}><option value='direct'>{t('routingProfiles.actionDirect')}</option><option value='vpn'>{t('routingWorkspace.actionVpn')}</option><option value='block'>{t('routingProfiles.actionBlock')}</option></select></label>
                <div className='traffic-checkbox-field'><label><input checked={ruleForm.enabled} type='checkbox' onChange={(event) => setRuleForm((current) => ({ ...current, enabled: event.target.checked }))} />{t('routingProfiles.enabled')}</label><p>{t('routingProfiles.priorityHelp')}</p></div>
              </div>
              <section className='routing-matcher-group'>
                <h3>{t('routingWorkspace.domainsGroup')}</h3>
                <p>{t('routingWorkspace.domainsHelp')}</p>
                <div className='routing-rule-matchers-grid'>
                  {matcherFields.slice(0, 2).map(field => <label className='field' key={field.key}>
                    <span>{t(field.label)}</span>
                    <textarea rows={3} placeholder={field.example} value={ruleText[field.text]} onChange={event => setRuleText(current => ({ ...current, [field.text]: event.target.value }))} />
                  </label>)}
                </div>
              </section>
              <section className='routing-matcher-group'>
                <h3>{t('routingWorkspace.networkGroup')}</h3>
                <p>{t('routingWorkspace.networkHelp')}</p>
                <label className='field'><span>{t('routingProfiles.ipCidrs')}</span><textarea rows={3} placeholder={matcherFields[2].example} value={ruleText.cidrs} onChange={event => setRuleText(current => ({ ...current, cidrs: event.target.value }))} /></label>
              </section>
              <details className='routing-matcher-group' open={advancedMatchersOpen} onToggle={event => setAdvancedMatchersOpen(event.currentTarget.open)}>
                <summary>{t('routingWorkspace.advancedMatchers')}</summary>
                <p>{t('routingWorkspace.advancedHelp')}</p>
                <div className='routing-rule-matchers-grid'>
                  {matcherFields.slice(3).map(field => <label className='field' key={field.key}>
                    <span>{t(field.label)}</span>
                    <textarea rows={3} placeholder={field.example} value={ruleText[field.text]} onChange={event => setRuleText(current => ({ ...current, [field.text]: event.target.value }))} />
                  </label>)}
                </div>
              </details>
              </fieldset>
            </form>
              </div>
            </div>
          </div>
        )}
      </div>
    </section>
  );
}
