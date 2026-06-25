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
  if (!value) return '-';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function formatValue(value?: string | null): string {
  return value && value.trim() !== '' ? value : '-';
}

function StatusBadge({ value }: { value: string }) {
  const className = value.toLowerCase().replace(/[^a-z0-9-]/g, '-');
  return <span className={`badge badge-${className}`}>{value}</span>;
}

function splitList(value: string): string[] {
  return value.split(/[\n,]+/g).map((item) => item.trim()).filter(Boolean);
}

function joinList(values?: string[]): string {
  return values?.join('\n') ?? '';
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

  if (counts.length === 0) return <span className='muted-text'>No matchers</span>;

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
  const canSaveRule = ruleForm.name.trim() !== '' && Number.isInteger(ruleForm.priority) && ruleForm.priority >= 0;

  return (
    <section className='page routing-profiles-page'>
      <div className='page-header'>
        <div>
          <h1>Routing Profiles</h1>
          <p>Manage split-tunnel profiles and direct, VPN, or block routing rules.</p>
        </div>
        <div className='status-pill'><span className='status-dot status-dot-ok' />{profiles.length} profiles</div>
      </div>

      <div className='routing-profiles-layout'>
        <form className='panel' onSubmit={handleCreateProfile}>
          <div className='panel-header'>
            <div>
              <div className='panel-title'>Profiles</div>
              <p className='panel-subtitle'>Select a profile to edit metadata and routing rules.</p>
            </div>
            <button className='small-button' type='submit' disabled={createProfileMutation.isPending}>Create profile</button>
          </div>

          {profilesQuery.isLoading && <p className='empty-state'>Loading routing profiles...</p>}
          {profilesQuery.isError && <div className='form-message form-message-error'>Failed to load routing profiles.</div>}
          {profiles.length > 0 && (
            <div className='admin-table routing-profiles-table'>
              <div className='admin-table-row admin-table-head routing-profiles-table-row'><span>Profile</span><span>Type</span><span>Updated</span></div>
              {profiles.map((profile) => <ProfileRow key={profile.id} profile={profile} selected={profile.id === profileId} />)}
            </div>
          )}
        </form>

        {!profileId && <div className='panel'><p className='empty-state'>Select a routing profile to manage rules.</p></div>}
        {profileQuery.isLoading && <p className='empty-state'>Loading selected routing profile...</p>}
        {profileQuery.isError && <div className='form-message form-message-error'>Failed to load selected routing profile.</div>}

        {selectedProfile && (
          <>
            <form className='panel routing-profile-details-panel' onSubmit={(event) => { event.preventDefault(); updateProfileMutation.mutate(); }}>
              <div className='panel-header'>
                <div>
                  <div className='panel-title'>Profile details</div>
                  <p className='panel-subtitle'>Default profiles are used when a server has no explicit assignment.</p>
                </div>
                <div className='table-actions'>
                  <button className='small-button' type='button' disabled={selectedProfile.isDefault || deleteProfileMutation.isPending} onClick={() => deleteProfileMutation.mutate()}>Delete</button>
                  <button className='small-button' type='submit' disabled={profileName.trim() === '' || updateProfileMutation.isPending}>Save profile</button>
                </div>
              </div>
              {(updateProfileMutation.isError || deleteProfileMutation.isError) && <div className='form-message form-message-error'>Failed to update routing profile.</div>}
              <div className='routing-profile-form-grid'>
                <label className='field'><span>Name</span><input value={profileName} onChange={(event) => setProfileName(event.target.value)} /></label>
                <label className='field'><span>Description</span><input value={profileDescription} onChange={(event) => setProfileDescription(event.target.value)} /></label>
                <div className='traffic-checkbox-field routing-profile-default-field'><label><input checked={makeDefault} type='checkbox' onChange={(event) => setMakeDefault(event.target.checked)} />Default profile</label><p>Updated: {formatDate(selectedProfile.updatedAt)}</p></div>
              </div>
            </form>

            <form className='panel routing-rule-form' onSubmit={handleRuleSubmit}>
              <div className='panel-header'>
                <div>
                  <div className='panel-title'>{editingRuleId ? 'Edit routing rule' : 'Add routing rule'}</div>
                  <p className='panel-subtitle'>One value per line or comma-separated. Disabled rules are saved but ignored by rendering.</p>
                </div>
                <div className='table-actions'>
                  {editingRuleId && <button className='small-button' type='button' onClick={resetRuleForm}>Cancel edit</button>}
                  <button className='small-button' type='submit' disabled={!canSaveRule || saveRuleMutation.isPending}>Save rule</button>
                </div>
              </div>
              {saveRuleMutation.isError && <div className='form-message form-message-error'>Failed to save routing rule.</div>}
              <div className='routing-rule-form-grid'>
                <label className='field'><span>Name</span><input value={ruleForm.name} onChange={(event) => setRuleForm((current) => ({ ...current, name: event.target.value }))} /></label>
                <label className='field'><span>Priority</span><input min='0' type='number' value={ruleForm.priority} onChange={(event) => setRuleForm((current) => ({ ...current, priority: Number(event.target.value) }))} /></label>
                <label className='field'><span>Action</span><select value={ruleForm.action} onChange={(event) => setRuleForm((current) => ({ ...current, action: event.target.value as RoutingRuleAction }))}><option value='direct'>direct</option><option value='vpn'>vpn</option><option value='block'>block</option></select></label>
                <div className='traffic-checkbox-field'><label><input checked={ruleForm.enabled} type='checkbox' onChange={(event) => setRuleForm((current) => ({ ...current, enabled: event.target.checked }))} />Enabled</label><p>Priority controls rule order.</p></div>
              </div>
              <div className='routing-rule-matchers-grid'>
                <label className='field'><span>Exact domains</span><textarea rows={4} value={ruleText.domains} onChange={(event) => setRuleText((current) => ({ ...current, domains: event.target.value }))} /></label>
                <label className='field'><span>Domain suffixes</span><textarea rows={4} value={ruleText.suffixes} onChange={(event) => setRuleText((current) => ({ ...current, suffixes: event.target.value }))} /></label>
                <label className='field'><span>Domain keywords</span><textarea rows={4} value={ruleText.keywords} onChange={(event) => setRuleText((current) => ({ ...current, keywords: event.target.value }))} /></label>
                <label className='field'><span>IP CIDRs</span><textarea rows={4} value={ruleText.cidrs} onChange={(event) => setRuleText((current) => ({ ...current, cidrs: event.target.value }))} /></label>
                <label className='field'><span>GeoSite tags</span><textarea rows={4} value={ruleText.geosite} onChange={(event) => setRuleText((current) => ({ ...current, geosite: event.target.value }))} /></label>
                <label className='field'><span>GeoIP tags</span><textarea rows={4} value={ruleText.geoip} onChange={(event) => setRuleText((current) => ({ ...current, geoip: event.target.value }))} /></label>
              </div>
            </form>

            <div className='panel admin-table-panel routing-rules-panel'>
              <div className='panel-header'><div><div className='panel-title'>Rules</div><p className='panel-subtitle'>Rules are applied by priority, then creation time.</p></div><div className='status-pill'>{rules.length} rules</div></div>
              {deleteRuleMutation.isError && <div className='form-message form-message-error'>Failed to delete routing rule.</div>}
              {rules.length === 0 ? <p className='empty-state'>No routing rules in this profile yet.</p> : (
                <div className='admin-table routing-rules-table'>
                  <div className='admin-table-row admin-table-head routing-rules-table-row'><span>Rule</span><span>Priority</span><span>Action</span><span>Status</span><span>Matchers</span><span>Actions</span></div>
                  {rules.map((rule) => (
                    <div className='admin-table-row routing-rules-table-row' key={rule.id}>
                      <div><strong>{formatValue(rule.name)}</strong><span>Updated {formatDate(rule.updatedAt)}</span></div>
                      <span>{rule.priority}</span>
                      <StatusBadge value={rule.action} />
                      <StatusBadge value={rule.enabled ? 'enabled' : 'disabled'} />
                      <RuleSummary rule={rule} />
                      <div className='table-actions'><button className='small-button' type='button' onClick={() => editRule(rule)}>Edit</button><button className='small-button' type='button' disabled={deleteRuleMutation.isPending} onClick={() => deleteRuleMutation.mutate(rule.id)}>Delete</button></div>
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
