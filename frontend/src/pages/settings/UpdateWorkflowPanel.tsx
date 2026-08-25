import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import {
  createUpdateApply,
  createUpdateDiscovery,
  createUpdatePreflight,
  createUpdateStage,
  type UpdateDiscoveryResult,
  type UpdateJob,
  type UpdatePreflightResult,
  type UpdateStageResult,
} from '../../entities/system/api/systemApi';
import { ApiError } from '../../shared/api/client';
import { t } from '../../shared/i18n/i18n';

type WorkflowState =
  | { kind: 'idle' }
  | { kind: 'blocked'; blockers: string[] }
  | { kind: 'current'; availability: string; currentVersion: string; candidateVersion?: string }
  | { kind: 'candidate'; discoveryJobId: string; result: UpdateDiscoveryResult }
  | { kind: 'verified'; stageJobId: string; result: UpdateStageResult }
  | { kind: 'completed'; job: UpdateJob }
  | { kind: 'unknown' }
  | { kind: 'error'; message: string };

const definiteApplyFailureCodes = new Set([
  'apply_in_progress',
  'apply_admission_failed',
  'apply_stage_pin_failed',
  'apply_state_transition_failed',
  'privileged_apply_failed',
  'stage_job_not_applicable',
  'stage_job_not_found',
  'update_apply_unavailable',
  'update_job_create_failed',
  'update_job_lookup_failed',
]);

function resultAs<T>(job: UpdateJob): T {
  return job.resultPayload as T;
}

function mutationError(error: unknown): string {
  if (error instanceof ApiError && error.message.trim() !== '') {
    return error.message;
  }
  return t('settings.updateWorkflowError');
}

function applyOutcomeIsAmbiguous(error: unknown): boolean {
  if (!(error instanceof ApiError)) {
    return true;
  }
  if (error.code === 'apply_outcome_unknown') {
    return true;
  }
  if (error.code && definiteApplyFailureCodes.has(error.code)) {
    return false;
  }
  return error.status >= 500;
}

export function UpdateWorkflowPanel() {
  const queryClient = useQueryClient();
  const [state, setState] = useState<WorkflowState>({ kind: 'idle' });
  const [confirming, setConfirming] = useState(false);

  const checkMutation = useMutation({
    mutationFn: async () => {
      const preflight = (await createUpdatePreflight()).job;
      if (preflight.status !== 'succeeded') {
        throw new Error(preflight.errorCode || 'preflight_failed');
      }
      const preflightResult = resultAs<UpdatePreflightResult>(preflight);
      if (preflightResult.decision !== 'proceed') {
        return { kind: 'blocked' as const, blockers: preflightResult.blockers ?? [] };
      }

      const discovery = (await createUpdateDiscovery()).job;
      if (discovery.status !== 'succeeded') {
        throw new Error(discovery.errorCode || 'discovery_failed');
      }
      const result = resultAs<UpdateDiscoveryResult>(discovery);
      if (result.availability !== 'update_available' || !result.candidateVersion) {
        return {
          kind: 'current' as const,
          availability: result.availability,
          currentVersion: result.currentVersion,
          candidateVersion: result.candidateVersion,
        };
      }
      return { kind: 'candidate' as const, discoveryJobId: discovery.id, result };
    },
    onMutate: () => {
      setConfirming(false);
      setState({ kind: 'idle' });
    },
    onSuccess: (nextState) => setState(nextState),
    onError: (error) => setState({ kind: 'error', message: mutationError(error) }),
  });

  const stageMutation = useMutation({
    mutationFn: async (discoveryJobId: string) => {
      const stage = (await createUpdateStage(discoveryJobId)).job;
      if (stage.status !== 'succeeded') {
        throw new Error(stage.errorCode || 'stage_failed');
      }
      return { stageJobId: stage.id, result: resultAs<UpdateStageResult>(stage) };
    },
    onSuccess: ({ stageJobId, result }) => {
      setConfirming(false);
      setState({ kind: 'verified', stageJobId, result });
    },
    onError: (error) => setState({ kind: 'error', message: mutationError(error) }),
  });

  const applyMutation = useMutation({
    mutationFn: async (stageJobId: string) => (await createUpdateApply(stageJobId)).job,
    retry: false,
    onSuccess: async (job) => {
      setConfirming(false);
      setState({ kind: 'completed', job });
      await queryClient.invalidateQueries({ queryKey: ['system-version'] });
    },
    onError: (error) => {
      setConfirming(false);
      if (applyOutcomeIsAmbiguous(error)) {
        setState({ kind: 'unknown' });
        return;
      }
      setState({ kind: 'error', message: mutationError(error) });
    },
  });

  const busy = checkMutation.isPending || stageMutation.isPending || applyMutation.isPending;

  return (
    <div className="settings-update-workflow">
      <div className="settings-update-workflow-heading">
        <strong>{t('settings.updateWorkflowTitle')}</strong>
        <span>{t('settings.updateWorkflowSubtitle')}</span>
      </div>

      {state.kind === 'idle' && !busy && (
        <p className="settings-update-workflow-copy">{t('settings.updateWorkflowIdle')}</p>
      )}

      {checkMutation.isPending && (
        <div className="settings-update-progress" role="status">{t('settings.updateChecking')}</div>
      )}
      {stageMutation.isPending && (
        <div className="settings-update-progress" role="status">{t('settings.updateVerifying')}</div>
      )}
      {applyMutation.isPending && (
        <div className="settings-update-progress settings-update-progress-critical" role="status">
          {t('settings.updateApplying')}
        </div>
      )}

      {state.kind === 'blocked' && (
        <div className="settings-update-result settings-update-result-warning">
          <strong>{t('settings.updateBlocked')}</strong>
          <p>{state.blockers.length > 0 ? state.blockers.join(' · ') : t('settings.updateBlockedFallback')}</p>
        </div>
      )}

      {state.kind === 'current' && (
        <div className="settings-update-result settings-update-result-success">
          <strong>{state.availability === 'up_to_date' ? t('settings.updateCurrent') : t('settings.updateNoAction')}</strong>
          <p>{t('settings.updateCurrentVersion')}: {state.currentVersion || '—'}</p>
        </div>
      )}

      {state.kind === 'candidate' && (
        <div className="settings-update-result">
          <strong>{t('settings.updateAvailable')}: {state.result.candidateVersion}</strong>
          <p>{t('settings.updateCandidateHint')}</p>
          <button
            className="button button-secondary"
            disabled={busy}
            onClick={() => stageMutation.mutate(state.discoveryJobId)}
            type="button"
          >
            {t('settings.updateVerifyPrepare')}
          </button>
        </div>
      )}

      {state.kind === 'verified' && (
        <div className="settings-update-result settings-update-result-verified">
          <strong>{t('settings.updateVerified')}: {state.result.verifiedVersion}</strong>
          <div className="settings-update-descriptor">
            <span>{t('settings.updateCommit')}</span>
            <code>{state.result.verifiedCommit.slice(0, 12)}</code>
            <span>{t('settings.updateMigration')}</span>
            <code>{state.result.expectedMigration}</code>
          </div>
          {!confirming ? (
            <button
              className="button button-primary"
              disabled={busy}
              onClick={() => setConfirming(true)}
              type="button"
            >
              {t('settings.updateInstall')}
            </button>
          ) : (
            <div className="settings-update-confirm">
              <strong>{t('settings.updateConfirmTitle')}</strong>
              <p>{t('settings.updateConfirmWarning')}</p>
              <div className="settings-update-actions">
                <button
                  className="button button-secondary"
                  disabled={busy}
                  onClick={() => setConfirming(false)}
                  type="button"
                >
                  {t('common.cancel')}
                </button>
                <button
                  className="button button-primary"
                  disabled={busy}
                  onClick={() => applyMutation.mutate(state.stageJobId)}
                  type="button"
                >
                  {t('settings.updateConfirmInstall')}
                </button>
              </div>
            </div>
          )}
        </div>
      )}

      {state.kind === 'completed' && (
        <div className="settings-update-result settings-update-result-success">
          <strong>{t('settings.updateCompleted')}</strong>
          <p>{t('settings.updateCompletedHint')}</p>
        </div>
      )}

      {state.kind === 'unknown' && (
        <div className="settings-update-result settings-update-result-warning">
          <strong>{t('settings.updateOutcomeUnknown')}</strong>
          <p>{t('settings.updateOutcomeUnknownHint')}</p>
        </div>
      )}

      {state.kind === 'error' && (
        <div className="settings-update-result settings-update-result-error">
          <strong>{t('settings.updateFailed')}</strong>
          <p>{state.message}</p>
        </div>
      )}

      {(state.kind === 'idle' || state.kind === 'blocked' || state.kind === 'current' || state.kind === 'error') && (
        <button
          className="button button-secondary"
          disabled={busy}
          onClick={() => checkMutation.mutate()}
          type="button"
        >
          {t('settings.updateCheck')}
        </button>
      )}
    </div>
  );
}
