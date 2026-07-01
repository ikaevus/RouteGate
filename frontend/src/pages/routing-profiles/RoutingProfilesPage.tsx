import { type FormEvent, useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
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
    <Link className={`admin-table-row routing-profiles-table-row vpn-account-row-link${selected ? ' vpn-account-row-selected' : ''}`} to={`/routing-profiles/${profile.id}`}>
      <div>
        <strong>{formatValue(profile.name)}</strong>
        <span>{formatValue(profile.description)}</span>
      </div>
      <StatusBadge value={profile.isDefault ? 'default' : 'custom'} />
      <span>{formatDate(profile.updatedAt)}</span>
    </Link>
  );
}

function RuleSummary({ rule }: { rule: RoutingProfileRule }) {
  const counts = [
    ['domains', rule.domains?.length ?? 0],
    ['suffixes', rule.domainSuffixes?.length ?? 0],
    ['keywords', rule.domainKeywords?.length ?? 0],
    ['cidrs', rule.ipCidrs?.length ?? 0],
    ['geosite', rule.geoSites?.length ?? 0],
    ['geoip', rule.geoIps?.length ?? 0],
  ].filter(([, count]) => Number(count) > 0);

  if (counts.length === 0) return <span className='muted-text'>{t('routingProfiles.noMatchers')}</span>;

  return (
    <div className='stage-summary'>
      {counts.map(([label, count]) => (
        <span className='stage-pill' key={String(label)}>
          <span>{label}</span>
          <strong>{count}</strong>
        </span>
      ))}
    </div>
  );
}

export function RoutingProfilesPage() {
  const { profileId } = useParams<{ profileId: string }>();
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
    if (!profileQuery.data) return;
    setProfileName(profileQuery.data.name);
    setProfileDescription(profileQuery.data.description ?? '');
    setMakeDefault(profileQuery.data.isDefault);
  }, [profileQuery.data]);

  function resetRuleForm() {
    setEditingRuleId(null);
    setRuleForm(emptyRule);
    setRuleText({ domains: '', suffixes: '', keywords: '', cidrs: '', geosite: '', geoip: '' });
  }

  const createProfileMutation = useMutation({
    mutationFn: createRoutingProfile,
    onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['routing-profiles'] }),
  });

  const updateProfileMutation = useMutation({
    mutationFn: () => updateRoutingProfile(profileId ?? '', { name: profileName.trim(), description: profileDescription.trim(), isDefault: makeDefault }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['routing-profiles'] });
      await queryClient.invalidateQueries({ queryKey: ['routing-profile', profileId] });
    },
  });

  const deleteProfileMutation = useMutation({
    mutationFn: () => deleteRoutingProfile(profileId ?? ''),
    onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['routing-profiles'] }),
  });

  const saveRuleMutation = useMutation({
    mutationFn: (request: RuleForm) => editingRuleId
      ? updateRoutingProfileRule(profileId ?? '', editingRuleId, request)
      : createRoutingProfileRule(profileId ?? '', request),
    onSuccess: async () => {
      resetRuleForm();
      await queryClient.invalidateQueries({ queryKey: ['routing-profile', profileId] });
    },
  });

  const deleteRuleMutation = useMutation({
    mutationFn: (ruleId: string) => deleteRoutingProfileRule(profileId ?? '', ruleId),
    onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['routing-profile', profileId] }),
  });

  function handleCreateProfile(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    createProfileMutation.mutate({ name: 'New routing profile', description: '', isDefault: false });
  }

  function handleRuleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canSaveRule) return;

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
        <div className='status-pill'><span className='status-dot status-dot-ok' />{t('routingProfiles.profileCount', { count: profiles.length })}</div>
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
          <>
            <form className='panel routing-profile-details-panel' onSubmit={(event) => { event.preventDefault(); updateProfileMutation.mutate(); }}>
              <div className='panel-header'>
                <div>
                  <div className='panel-title'>{t('routingProfiles.detailsTitle')}</div>
                  <p className='panel-subtitle'>{t('routingProfiles.detailsSubtitle')}</p>
                </div>
                <div className='table-actions'>
                  <button className='small-button' type='button' disabled={selectedProfile.isDefault || deleteProfileMutation.isPending} onClick={() => deleteProfileMutation.mutate()}>{t('routingProfiles.deleteProfile')}</button>
                  <button className='small-button' type='submit' disabled={profileName.trim() === '' || updateProfileMutation.isPending}>{t('routingProfiles.saveProfile')}</button>
                </div>
              </div>
              {updateProfileMutation.isError && <div className='form-message form-message-error'>{getErrorMessage(updateProfileMutation.error, t('routingProfiles.updateError'))}</div>}
              {deleteProfileMutation.isError && <div className='form-message form-message-error'>{getErrorMessage(deleteProfileMutation.error, t('routingProfiles.deleteError'))}</div>}
              <div className='routing-profile-form-grid'>
                <label className='field'><span>{t('routingProfiles.name')}</span><input value={profileName} onChange={(event) => setProfileName(event.target.value)} /></label>
                <label className='field'><span>{t('routingProfiles.description')}</span><input value={profileDescription} onChange={(event) => setProfileDescription(event.target.value)} /></label>
                <div className='traffic-checkbox-field routing-profile-default-field'><label><input checked={makeDefault} type='checkbox' onChange={(event) => setMakeDefault(event.target.checked)} />{t('routingProfiles.defaultProfile')}</label><p>{t('routingProfiles.updatedValue', { value: formatDate(selectedProfile.updatedAt) })}</p></div>
              </div>
            </form>

            <form className='panel routing-rule-form' onSubmit={handleRuleSubmit}>
              <div className='panel-header'>
                <div>
                  <div className='panel-title'>{editingRuleId ? t('routingProfiles.editRule') : t('routingProfiles.addRule')}</div>
                  <p className='panel-subtitle'>{t('routingProfiles.ruleHelp')}</p>
                </div>
                <div className='table-actions'>
                  {editingRuleId && <button className='small-button' type='button' onClick={resetRuleForm}>{t('routingProfiles.cancelEdit')}</button>}
                  <button className='small-button' type='submit' disabled={!canSaveRule || saveRuleMutation.isPending}>{t('routingProfiles.saveRule')}</button>
                </div>
              </div>
              {saveRuleMutation.isError && <div className='form-message form-message-error'>{getErrorMessage(saveRuleMutation.error, t('routingProfiles.saveRuleError'))}</div>}
              {!hasMatcherText(ruleText) && <div className='form-message form-message-warning'>{t('routingProfiles.matcherWarning')}</div>}
              <div className='routing-rule-form-grid'>
                <label className='field'><span>{t('routingProfiles.name')}</span><input value={ruleForm.name} onChange={(event) => setRuleForm((current) => ({ ...current, name: event.target.value }))} /></label>
                <label className='field'><span>{t('routingProfiles.priority')}</span><input min='0' type='number' value={ruleForm.priority} onChange={(event) => setRuleForm((current) => ({ ...current, priority: Number(event.target.value) }))} /></label>
                <label className='field'><span>{t('routingProfiles.action')}</span><select value={ruleForm.action} onChange={(event) => setRuleForm((current) => ({ ...current, action: event.target.value as RoutingRuleAction }))}><option value='direct'>{t('routingProfiles.actionDirect')}</option><option value='vpn'>vpn</option><option value='block'>{t('routingProfiles.actionBlock')}</option></select></label>
                <div className='traffic-checkbox-field'><label><input checked={ruleForm.enabled} type='checkbox' onChange={(event) => setRuleForm((current) => ({ ...current, enabled: event.target.checked }))} />{t('routingProfiles.enabled')}</label><p>{t('routingProfiles.priorityHelp')}</p></div>
              </div>
              <div className='routing-rule-matchers-grid'>
                <label className='field'><span>{t('routingProfiles.exactDomains')}</span><textarea rows={4} value={ruleText.domains} onChange={(event) => setRuleText((current) => ({ ...current, domains: event.target.value }))} /></label>
                <label className='field'><span>{t('routingProfiles.domainSuffixes')}</span><textarea rows={4} value={ruleText.suffixes} onChange={(event) => setRuleText((current) => ({ ...current, suffixes: event.target.value }))} /></label>
                <label className='field'><span>{t('routingProfiles.domainKeywords')}</span><textarea rows={4} value={ruleText.keywords} onChange={(event) => setRuleText((current) => ({ ...current, keywords: event.target.value }))} /></label>
                <label className='field'><span>{t('routingProfiles.ipCidrs')}</span><textarea rows={4} value={ruleText.cidrs} onChange={(event) => setRuleText((current) => ({ ...current, cidrs: event.target.value }))} /></label>
                <label className='field'><span>{t('routingProfiles.geoSiteTags')}</span><textarea rows={4} value={ruleText.geosite} onChange={(event) => setRuleText((current) => ({ ...current, geosite: event.target.value }))} /></label>
                <label className='field'><span>{t('routingProfiles.geoIpTags')}</span><textarea rows={4} value={ruleText.geoip} onChange={(event) => setRuleText((current) => ({ ...current, geoip: event.target.value }))} /></label>
              </div>
            </form>

            <div className='panel admin-table-panel routing-rules-panel'>
              <div className='panel-header'><div><div className='panel-title'>{t('routingProfiles.rules')}</div><p className='panel-subtitle'>{t('routingProfiles.rulesSubtitle')}</p></div><div className='status-pill'>{t('routingProfiles.ruleCount', { count: rules.length })}</div></div>
              {deleteRuleMutation.isError && <div className='form-message form-message-error'>{getErrorMessage(deleteRuleMutation.error, t('routingProfiles.deleteRuleError'))}</div>}
              {rules.length === 0 ? <p className='empty-state'>{t('routingProfiles.noRules')}</p> : (
                <div className='admin-table routing-rules-table'>
                  <div className='admin-table-row admin-table-head routing-rules-table-row'><span>{t('routingProfiles.rule')}</span><span>{t('routingProfiles.priority')}</span><span>{t('routingProfiles.action')}</span><span>{t('vpnAccounts.status')}</span><span>{t('routingProfiles.matchers')}</span><span>{t('routingProfiles.actions')}</span></div>
                  {rules.map((rule) => (
                    <div className='admin-table-row routing-rules-table-row' key={rule.id}>
                      <div><strong>{formatValue(rule.name)}</strong><span>{t('routingProfiles.updatedValue', { value: formatDate(rule.updatedAt) })}</span></div>
                      <span>{rule.priority}</span>
                      <StatusBadge value={rule.action} />
                      <StatusBadge value={rule.enabled ? 'enabled' : 'disabled'} />
                      <RuleSummary rule={rule} />
                      <div className='table-actions'><button className='small-button' type='button' onClick={() => editRule(rule)}>{t('routingProfiles.edit')}</button><button className='small-button' type='button' disabled={deleteRuleMutation.isPending} onClick={() => deleteRuleMutation.mutate(rule.id)}>{t('routingProfiles.deleteProfile')}</button></div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </>
        )}
      </div>
    </section>
  );
}
