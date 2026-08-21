import { type FormEvent, useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from 'react-router-dom';
import {
  createNodeGroup,
  deleteNodeGroup,
  deleteNodeGroupMember,
  getNodeGroup,
  getNodeGroupCandidates,
  getNodeGroups,
  updateNodeGroup,
  upsertNodeGroupMember,
  type NodeGroupCandidate,
  type NodeGroupMember,
  type NodeGroupSelectionStrategy,
} from '../../entities/nodeGroup/api/nodeGroupApi';
import { getServers } from '../../entities/server/api/serverApi';
import { t } from '../../shared/i18n/i18n';
import './nodeGroups.css';

function errorText(error: unknown, fallback: string): string {
  return error instanceof Error && error.message.trim() !== '' ? error.message : fallback;
}

function formatDate(value?: string): string {
  if (!value) return t('common.notAvailable');
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}

function signalLabel(signal: string): string {
  switch (signal) {
    case 'member_disabled': return t('nodeGroups.signal.member_disabled');
    case 'not_vpn_node': return t('nodeGroups.signal.not_vpn_node');
    case 'node_not_active': return t('nodeGroups.signal.node_not_active');
    case 'agent_missing': return t('nodeGroups.signal.agent_missing');
    case 'agent_not_online': return t('nodeGroups.signal.agent_not_online');
    case 'heartbeat_stale': return t('nodeGroups.signal.heartbeat_stale');
    case 'protocol_unsupported': return t('nodeGroups.signal.protocol_unsupported');
    case 'runtime_not_reported': return t('nodeGroups.signal.runtime_not_reported');
    case 'runtime_not_running': return t('nodeGroups.signal.runtime_not_running');
    case 'high_load': return t('nodeGroups.signal.high_load');
    default: return signal;
  }
}

function healthLabel(candidate: NodeGroupCandidate): string {
  if (candidate.health === 'ready') return t('nodeGroups.healthReady');
  if (candidate.health === 'degraded') return t('nodeGroups.healthDegraded');
  return t('nodeGroups.healthUnavailable');
}

export function NodeGroupsPage() {
  const { groupId } = useParams<{ groupId: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [createName, setCreateName] = useState('');
  const [createDescription, setCreateDescription] = useState('');
  const [createStrategy, setCreateStrategy] = useState<NodeGroupSelectionStrategy>('priority');
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [strategy, setStrategy] = useState<NodeGroupSelectionStrategy>('priority');
  const [memberServerId, setMemberServerId] = useState('');
  const [memberPriority, setMemberPriority] = useState(100);
  const [memberWeight, setMemberWeight] = useState(100);
  const [memberEnabled, setMemberEnabled] = useState(true);

  const groupsQuery = useQuery({ queryKey: ['node-groups'], queryFn: getNodeGroups });
  const groupQuery = useQuery({
    queryKey: ['node-group', groupId],
    queryFn: () => getNodeGroup(groupId ?? ''),
    enabled: Boolean(groupId),
  });
  const candidatesQuery = useQuery({
    queryKey: ['node-group-candidates', groupId],
    queryFn: () => getNodeGroupCandidates(groupId ?? ''),
    enabled: Boolean(groupId),
    refetchInterval: 30_000,
  });
  const serversQuery = useQuery({ queryKey: ['servers'], queryFn: getServers });

  useEffect(() => {
    if (!groupQuery.data) return;
    setName(groupQuery.data.name);
    setDescription(groupQuery.data.description ?? '');
    setStrategy(groupQuery.data.selectionStrategy);
  }, [groupQuery.data]);

  async function refreshGroup() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['node-groups'] }),
      queryClient.invalidateQueries({ queryKey: ['node-group', groupId] }),
      queryClient.invalidateQueries({ queryKey: ['node-group-candidates', groupId] }),
      queryClient.invalidateQueries({ queryKey: ['vpn-account-routing-policy'] }),
    ]);
  }

  const createMutation = useMutation({
    mutationFn: () => createNodeGroup({
      name: createName.trim(),
      description: createDescription.trim(),
      selectionStrategy: createStrategy,
    }),
    onSuccess: async (group) => {
      setCreateName('');
      setCreateDescription('');
      setCreateStrategy('priority');
      await queryClient.invalidateQueries({ queryKey: ['node-groups'] });
      navigate(`/node-groups/${group.id}`);
    },
  });

  const saveMutation = useMutation({
    mutationFn: () => updateNodeGroup(groupId ?? '', {
      name: name.trim(),
      description: description.trim(),
      selectionStrategy: strategy,
    }),
    onSuccess: refreshGroup,
  });

  const deleteMutation = useMutation({
    mutationFn: () => deleteNodeGroup(groupId ?? ''),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['node-groups'] });
      navigate('/node-groups');
    },
  });

  const memberMutation = useMutation({
    mutationFn: () => upsertNodeGroupMember(groupId ?? '', memberServerId, {
      priority: memberPriority,
      weight: memberWeight,
      enabled: memberEnabled,
    }),
    onSuccess: async () => {
      setMemberServerId('');
      setMemberPriority(100);
      setMemberWeight(100);
      setMemberEnabled(true);
      await refreshGroup();
    },
  });

  const removeMemberMutation = useMutation({
    mutationFn: (serverId: string) => deleteNodeGroupMember(groupId ?? '', serverId),
    onSuccess: refreshGroup,
  });

  function createGroup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (createName.trim() !== '') createMutation.mutate();
  }

  function saveGroup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (groupId && name.trim() !== '') saveMutation.mutate();
  }

  function saveMember(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (groupId && memberServerId) memberMutation.mutate();
  }

  function editMember(member: NodeGroupMember) {
    setMemberServerId(member.serverId);
    setMemberPriority(member.priority);
    setMemberWeight(member.weight);
    setMemberEnabled(member.enabled);
  }

  const groups = groupsQuery.data?.items ?? [];
  const selectedGroup = groupQuery.data;
  const members = selectedGroup?.members ?? [];
  const candidates = candidatesQuery.data?.candidates ?? [];
  const vpnServers = (serversQuery.data?.items ?? []).filter((server) => server.deploymentRole !== 'management');

  return (
    <section className="page node-groups-page feature-screen-page">
      <div className="page-header feature-page-header">
        <div>
          <h1>{t('nodeGroups.title')}</h1>
          <p>{t('nodeGroups.subtitle')}</p>
        </div>
        <div className="status-pill">{t('nodeGroups.groupCount', { count: groups.length })}</div>
      </div>

      <div className="node-groups-layout">
        <div className="node-groups-sidebar-stack">
          <form className="panel node-group-create-panel" onSubmit={createGroup}>
            <div className="panel-header">
              <div className="panel-title">{t('nodeGroups.create')}</div>
            </div>
            <label className="field"><span>{t('nodeGroups.name')}</span><input value={createName} onChange={(event) => setCreateName(event.target.value)} /></label>
            <label className="field"><span>{t('nodeGroups.description')}</span><input value={createDescription} onChange={(event) => setCreateDescription(event.target.value)} /></label>
            <label className="field"><span>{t('nodeGroups.strategy')}</span><select value={createStrategy} onChange={(event) => setCreateStrategy(event.target.value as NodeGroupSelectionStrategy)}><option value="priority">{t('nodeGroups.strategyPriority')}</option><option value="weighted">{t('nodeGroups.strategyWeighted')}</option></select></label>
            {createMutation.isError && <div className="form-message form-message-error">{errorText(createMutation.error, t('nodeGroups.createError'))}</div>}
            <button className="primary-button" type="submit" disabled={createName.trim() === '' || createMutation.isPending}>{createMutation.isPending ? t('nodeGroups.creating') : t('nodeGroups.create')}</button>
          </form>

          <div className="panel node-group-list-panel">
            <div className="panel-header"><div><div className="panel-title">{t('nodeGroups.groups')}</div></div></div>
            {groupsQuery.isError && <div className="form-message form-message-error">{t('nodeGroups.loadError')}</div>}
            {groups.length === 0 ? <p className="empty-state">{t('nodeGroups.empty')}</p> : (
              <div className="node-group-list">
                {groups.map((group) => (
                  <Link className={`node-group-list-item${group.id === groupId ? ' node-group-list-item-active' : ''}`} key={group.id} to={`/node-groups/${group.id}`}>
                    <strong>{group.name}</strong>
                    <span>{t('nodeGroups.memberCount', { count: group.memberCount })} · {group.selectionStrategy === 'weighted' ? t('nodeGroups.strategyWeighted') : t('nodeGroups.strategyPriority')}</span>
                  </Link>
                ))}
              </div>
            )}
          </div>
        </div>

        <div className="node-groups-detail-stack">
          {!groupId && <div className="panel"><p className="empty-state">{t('nodeGroups.select')}</p></div>}
          {groupQuery.isError && <div className="form-message form-message-error">{t('nodeGroups.loadError')}</div>}
          {selectedGroup && (
            <>
              <form className="panel" onSubmit={saveGroup}>
                <div className="panel-header">
                  <div><div className="panel-title">{t('nodeGroups.details')}</div><p className="panel-subtitle">{t('nodeGroups.strategyHelp')}</p></div>
                  <div className="table-actions"><button className="small-button" type="button" onClick={() => deleteMutation.mutate()} disabled={deleteMutation.isPending}>{t('nodeGroups.delete')}</button><button className="primary-button" type="submit" disabled={saveMutation.isPending || name.trim() === ''}>{t('nodeGroups.save')}</button></div>
                </div>
                {(saveMutation.isError || deleteMutation.isError) && <div className="form-message form-message-error">{errorText(saveMutation.error ?? deleteMutation.error, saveMutation.isError ? t('nodeGroups.saveError') : t('nodeGroups.deleteError'))}</div>}
                <div className="node-group-policy-grid">
                  <label className="field"><span>{t('nodeGroups.name')}</span><input value={name} onChange={(event) => setName(event.target.value)} /></label>
                  <label className="field"><span>{t('nodeGroups.description')}</span><input value={description} onChange={(event) => setDescription(event.target.value)} /></label>
                  <label className="field"><span>{t('nodeGroups.strategy')}</span><select value={strategy} onChange={(event) => setStrategy(event.target.value as NodeGroupSelectionStrategy)}><option value="priority">{t('nodeGroups.strategyPriority')}</option><option value="weighted">{t('nodeGroups.strategyWeighted')}</option></select></label>
                </div>
              </form>

              <div className="panel">
                <div className="panel-header"><div><div className="panel-title">{t('nodeGroups.members')}</div><p className="panel-subtitle">{t('nodeGroups.membersHelp')}</p></div></div>
                <form className="node-group-member-form" onSubmit={saveMember}>
                  <label className="field"><span>{t('nodeGroups.node')}</span><select value={memberServerId} onChange={(event) => setMemberServerId(event.target.value)}><option value="">—</option>{vpnServers.map((server) => <option key={server.id} value={server.id}>{server.name} · {server.deploymentRole}</option>)}</select></label>
                  <label className="field"><span>{t('nodeGroups.priority')}</span><input min="0" max="10000" type="number" value={memberPriority} onChange={(event) => setMemberPriority(Number(event.target.value))} /></label>
                  <label className="field"><span>{t('nodeGroups.weight')}</span><input min="1" max="1000" type="number" value={memberWeight} onChange={(event) => setMemberWeight(Number(event.target.value))} /></label>
                  <label className="node-group-enabled-field"><input type="checkbox" checked={memberEnabled} onChange={(event) => setMemberEnabled(event.target.checked)} />{t('nodeGroups.enabled')}</label>
                  <button className="small-button" type="submit" disabled={!memberServerId || memberMutation.isPending}>{t('nodeGroups.addMember')}</button>
                </form>
                {(memberMutation.isError || removeMemberMutation.isError) && <div className="form-message form-message-error">{t('nodeGroups.memberError')}</div>}
                {members.length === 0 ? <p className="empty-state">{t('nodeGroups.noMembers')}</p> : (
                  <div className="admin-table node-group-members-table">
                    <div className="admin-table-row admin-table-head node-group-member-row"><span>{t('nodeGroups.node')}</span><span>{t('nodeGroups.priority')}</span><span>{t('nodeGroups.weight')}</span><span>{t('nodeGroups.enabled')}</span><span /></div>
                    {members.map((member) => <div className="admin-table-row node-group-member-row" key={member.serverId}><div><strong>{member.serverName}</strong><span>{member.protocol} · {member.deploymentRole}</span></div><span>{member.priority}</span><span>{member.weight}</span><span>{member.enabled ? t('vpnAccounts.enabled') : t('vpnAccounts.disabled')}</span><div className="table-actions"><button className="small-button" type="button" onClick={() => editMember(member)}>{t('nodeGroups.configure')}</button><button className="small-button" type="button" onClick={() => removeMemberMutation.mutate(member.serverId)}>{t('nodeGroups.remove')}</button></div></div>)}
                  </div>
                )}
              </div>

              <div className="panel">
                <div className="panel-header"><div><div className="panel-title">{t('nodeGroups.candidates')}</div><p className="panel-subtitle">{t('nodeGroups.candidatesHelp')}</p></div><div className="status-pill">{candidatesQuery.data?.selectionStrategy === 'weighted' ? t('nodeGroups.strategyWeighted') : t('nodeGroups.strategyPriority')}</div></div>
                {candidatesQuery.isError && <div className="form-message form-message-error">{t('nodeGroups.candidateError')}</div>}
                {candidates.length === 0 ? <p className="empty-state">{t('nodeGroups.noMembers')}</p> : (
                  <div className="admin-table node-group-candidates-table">
                    <div className="admin-table-row admin-table-head node-group-candidate-row"><span>{t('nodeGroups.node')}</span><span>{t('nodeGroups.health')}</span><span>{t('nodeGroups.load')}</span><span>{t('nodeGroups.lastSeen')}</span><span>{t('nodeGroups.signals')}</span></div>
                    {candidates.map((candidate) => <div className="admin-table-row node-group-candidate-row" key={candidate.serverId}><div><strong>{candidate.serverName}</strong><span>{candidate.protocol} · P{candidate.priority} · W{candidate.weight}</span></div><span className={`badge badge-${candidate.health === 'ready' ? 'active' : candidate.health === 'degraded' ? 'pending' : 'error'}`}>{healthLabel(candidate)}</span><span>{candidate.loadPerCpu === undefined ? t('common.notAvailable') : candidate.loadPerCpu.toFixed(2)}</span><span>{formatDate(candidate.lastSeenAt)}</span><span>{candidate.signals.length === 0 ? '—' : candidate.signals.map(signalLabel).join(' · ')}</span></div>)}
                  </div>
                )}
              </div>
            </>
          )}
        </div>
      </div>
    </section>
  );
}
