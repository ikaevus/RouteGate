from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    if old not in text:
        raise SystemExit(f"expected block not found in {path}: {old[:120]!r}")
    p.write_text(text.replace(old, new, 1))


# RBAC: deletion of unused snapshots is explicit and separate from apply/rollback.
replace_once(
    "backend/internal/auth/repository.go",
    '\t"configs:apply",\n\t"configs:rollback",',
    '\t"configs:apply",\n\t"configs:delete",\n\t"configs:rollback",',
)

# Canonical API routes for safe snapshot deletion and deliberate redeploy/rollback.
replace_once(
    "backend/internal/http/router.go",
    '\tmux.Handle("POST /api/v1/servers/{server_id}/config/versions/{version_id}/apply", authn(auth.RequirePermission("configs:apply")(stdhttp.HandlerFunc(configsHandler.Apply))))\n',
    '\tmux.Handle("POST /api/v1/servers/{server_id}/config/versions/{version_id}/apply", authn(auth.RequirePermission("configs:apply")(stdhttp.HandlerFunc(configsHandler.Apply))))\n'
    '\tmux.Handle("POST /api/v1/servers/{server_id}/config/versions/{version_id}/reapply", authn(auth.RequirePermission("configs:rollback")(stdhttp.HandlerFunc(configsHandler.Reapply))))\n'
    '\tmux.Handle("DELETE /api/v1/servers/{server_id}/config/versions/{version_id}", authn(auth.RequirePermission("configs:delete")(stdhttp.HandlerFunc(configsHandler.DeleteVersion))))\n',
)

# Frontend API helpers.
replace_once(
    "frontend/src/entities/server/api/serverApi.ts",
    "export function getConfigApplyJobs(serverId: string): Promise<ListConfigApplyJobsResponse> {\n",
    "export function reapplyConfigVersion(\n"
    "  serverId: string,\n"
    "  versionId: string,\n"
    "): Promise<ApplyConfigResponse> {\n"
    "  return apiPost<undefined, ApplyConfigResponse>(\n"
    "    `/api/v1/servers/${encodeURIComponent(serverId)}/config/versions/${encodeURIComponent(versionId)}/reapply`,\n"
    "  );\n"
    "}\n\n"
    "export function deleteConfigVersion(serverId: string, versionId: string): Promise<void> {\n"
    "  return apiDelete(\n"
    "    `/api/v1/servers/${encodeURIComponent(serverId)}/config/versions/${encodeURIComponent(versionId)}`,\n"
    "  );\n"
    "}\n\n"
    "export function getConfigApplyJobs(serverId: string): Promise<ListConfigApplyJobsResponse> {\n",
)

# Server Details imports.
replace_once(
    "frontend/src/pages/servers/ServerDetailsLegacyPage.tsx",
    "  createServerRegistrationToken,\n  deleteServer,\n",
    "  createServerRegistrationToken,\n  deleteConfigVersion,\n  deleteServer,\n",
)
replace_once(
    "frontend/src/pages/servers/ServerDetailsLegacyPage.tsx",
    "  renderConfig,\n  updateServer,\n",
    "  reapplyConfigVersion,\n  renderConfig,\n  updateServer,\n",
)

# Mutations for explicit redeploy and safe deletion.
replace_once(
    "frontend/src/pages/servers/ServerDetailsLegacyPage.tsx",
    "  const applyConfigMutation = useMutation({\n"
    "    mutationFn: (versionId: string) => applyConfigVersion(serverId ?? '', versionId),\n"
    "    onSuccess: refreshConfigQueries,\n"
    "  });\n\n",
    "  const applyConfigMutation = useMutation({\n"
    "    mutationFn: (versionId: string) => applyConfigVersion(serverId ?? '', versionId),\n"
    "    onSuccess: refreshConfigQueries,\n"
    "  });\n\n"
    "  const reapplyConfigMutation = useMutation({\n"
    "    mutationFn: (versionId: string) => reapplyConfigVersion(serverId ?? '', versionId),\n"
    "    onSuccess: refreshConfigQueries,\n"
    "  });\n\n"
    "  const deleteConfigVersionMutation = useMutation({\n"
    "    mutationFn: (versionId: string) => deleteConfigVersion(serverId ?? '', versionId),\n"
    "    onSuccess: refreshConfigQueries,\n"
    "  });\n\n",
)

# Determine the actually current version from the latest successful apply job.
replace_once(
    "frontend/src/pages/servers/ServerDetailsLegacyPage.tsx",
    "  const versionsById = new Map(configVersions.map((version) => [version.id, version]));\n  const managerBaseUrl = getManagerBaseUrl();\n",
    "  const versionsById = new Map(configVersions.map((version) => [version.id, version]));\n"
    "  const latestSuccessfulApplyJob = applyJobs\n"
    "    .filter((job) => job.action === 'apply' && job.status === 'succeeded')\n"
    "    .sort((left, right) => {\n"
    "      const leftTime = new Date(left.completedAt ?? left.updatedAt).getTime();\n"
    "      const rightTime = new Date(right.completedAt ?? right.updatedAt).getTime();\n"
    "      return rightTime - leftTime;\n"
    "    })[0];\n"
    "  const currentConfigVersionId = latestSuccessfulApplyJob?.configVersionId\n"
    "    ?? configVersions\n"
    "      .filter((version) => Boolean(version.appliedAt))\n"
    "      .sort((left, right) => new Date(right.appliedAt ?? 0).getTime() - new Date(left.appliedAt ?? 0).getTime())[0]?.id\n"
    "    ?? null;\n"
    "  const managerBaseUrl = getManagerBaseUrl();\n",
)

# Explain immutable snapshots directly where the operator sees them.
replace_once(
    "frontend/src/pages/servers/ServerDetailsLegacyPage.tsx",
    "        {configVersionsQuery.isError && (\n",
    "        <p className=\"muted-text\">{t('serverDetails.configVersionsImmutableHint')}</p>\n\n"
    "        {configVersionsQuery.isError && (\n",
)

# Surface errors from the new lifecycle actions.
replace_once(
    "frontend/src/pages/servers/ServerDetailsLegacyPage.tsx",
    "        {(validateConfigMutation.isError || applyConfigMutation.isError) && (\n",
    "        {(validateConfigMutation.isError || applyConfigMutation.isError || reapplyConfigMutation.isError || deleteConfigVersionMutation.isError) && (\n",
)

# Per-version derived state.
replace_once(
    "frontend/src/pages/servers/ServerDetailsLegacyPage.tsx",
    "              const isApplying =\n                applyConfigMutation.isPending && applyConfigMutation.variables === version.id;\n\n              return (\n",
    "              const isApplying =\n"
    "                applyConfigMutation.isPending && applyConfigMutation.variables === version.id;\n"
    "              const isReapplying =\n"
    "                reapplyConfigMutation.isPending && reapplyConfigMutation.variables === version.id;\n"
    "              const isDeleting =\n"
    "                deleteConfigVersionMutation.isPending && deleteConfigVersionMutation.variables === version.id;\n"
    "              const hasApplyHistory = applyJobs.some((job) => job.configVersionId === version.id);\n"
    "              const canDeleteConfigVersion = !version.appliedAt && !hasApplyHistory;\n"
    "              const isCurrentConfig = currentConfigVersionId === version.id;\n\n"
    "              return (\n",
)

# Current marker.
replace_once(
    "frontend/src/pages/servers/ServerDetailsLegacyPage.tsx",
    "                  <StatusBadge status={version.status} />\n                  <code>{shortHash(version.configHash)}</code>\n",
    "                  <div className=\"timestamp-stack\">\n"
    "                    <StatusBadge status={version.status} />\n"
    "                    {isCurrentConfig && <span className=\"badge badge-online\">{t('serverDetails.currentConfig')}</span>}\n"
    "                  </div>\n"
    "                  <code>{shortHash(version.configHash)}</code>\n",
)

# Applied snapshots must not be re-validated in place.
replace_once(
    "frontend/src/pages/servers/ServerDetailsLegacyPage.tsx",
    "                      disabled={isValidating}\n                      onClick={() => validateConfigMutation.mutate(version.id)}\n",
    "                      disabled={Boolean(version.appliedAt) || isValidating}\n                      onClick={() => validateConfigMutation.mutate(version.id)}\n",
)

# Replace the single Apply action with explicit apply/redeploy/delete lifecycle controls.
replace_once(
    "frontend/src/pages/servers/ServerDetailsLegacyPage.tsx",
    "                    <button\n"
    "                      className=\"small-button\"\n"
    "                      type=\"button\"\n"
    "                      disabled={version.status !== 'validated' || isApplying}\n"
    "                      onClick={() => applyConfigMutation.mutate(version.id)}\n"
    "                    >\n"
    "                      {isApplying ? t('serverDetails.applying') : t('serverDetails.apply')}\n"
    "                    </button>\n",
    "                    {!version.appliedAt && (\n"
    "                      <button\n"
    "                        className=\"small-button\"\n"
    "                        type=\"button\"\n"
    "                        disabled={version.status !== 'validated' || isApplying}\n"
    "                        onClick={() => applyConfigMutation.mutate(version.id)}\n"
    "                      >\n"
    "                        {isApplying ? t('serverDetails.applying') : t('serverDetails.apply')}\n"
    "                      </button>\n"
    "                    )}\n"
    "                    {version.appliedAt && (\n"
    "                      <button\n"
    "                        className=\"small-button\"\n"
    "                        type=\"button\"\n"
    "                        disabled={isReapplying}\n"
    "                        onClick={() => reapplyConfigMutation.mutate(version.id)}\n"
    "                      >\n"
    "                        {isReapplying\n"
    "                          ? t('serverDetails.reapplying')\n"
    "                          : isCurrentConfig\n"
    "                            ? t('serverDetails.applyAgain')\n"
    "                            : t('serverDetails.restoreVersion', { version: version.version })}\n"
    "                      </button>\n"
    "                    )}\n"
    "                    {canDeleteConfigVersion && (\n"
    "                      <button\n"
    "                        className=\"small-button\"\n"
    "                        type=\"button\"\n"
    "                        disabled={isDeleting}\n"
    "                        onClick={() => {\n"
    "                          if (window.confirm(t('serverDetails.deleteConfigConfirm', { version: version.version }))) {\n"
    "                            deleteConfigVersionMutation.mutate(version.id);\n"
    "                          }\n"
    "                        }}\n"
    "                      >\n"
    "                        {isDeleting ? t('serverDetails.deletingConfig') : t('serverDetails.deleteConfig')}\n"
    "                      </button>\n"
    "                    )}\n",
)

# Reframe jobs as immutable deployment history.
replace_once(
    "frontend/src/pages/servers/ServerDetailsLegacyPage.tsx",
    "        {applyJobsQuery.isError && (\n",
    "        <p className=\"muted-text\">{t('serverDetails.deploymentHistoryImmutableHint')}</p>\n\n"
    "        {applyJobsQuery.isError && (\n",
)

# English copy.
replace_once(
    "frontend/src/shared/i18n/locales/en.ts",
    "  'serverDetails.configVersionsSubtitle': 'Rendered configs for this server and their deploy state.',\n",
    "  'serverDetails.configVersionsSubtitle': 'Immutable rendered snapshots for this server and their deployment state.',\n"
    "  'serverDetails.configVersionsImmutableHint': 'To change the configuration, update RouteGate server, protocol, VPN account, or routing settings and render a new version. Existing snapshots are not edited in place.',\n",
)
replace_once(
    "frontend/src/shared/i18n/locales/en.ts",
    "  'serverDetails.apply': 'Apply',\n  'serverDetails.applyJobs': 'Apply jobs',\n  'serverDetails.applyJobsSubtitle': 'Agent-side config deployment progress and results.',\n  'serverDetails.applyJobsLoadError': 'Failed to load apply jobs.',\n  'serverDetails.loadingApplyJobs': 'Loading apply jobs...',\n  'serverDetails.noApplyJobs': 'No apply jobs have been queued yet.',\n",
    "  'serverDetails.apply': 'Apply',\n"
    "  'serverDetails.currentConfig': 'Current',\n"
    "  'serverDetails.applyAgain': 'Apply again',\n"
    "  'serverDetails.restoreVersion': 'Restore v{version}',\n"
    "  'serverDetails.reapplying': 'Deploying...',\n"
    "  'serverDetails.deleteConfig': 'Delete',\n"
    "  'serverDetails.deletingConfig': 'Deleting...',\n"
    "  'serverDetails.deleteConfigConfirm': 'Delete unused v{version}? Only a snapshot that has never had a deployment attempt can be deleted.',\n"
    "  'serverDetails.applyJobs': 'Deployment history',\n"
    "  'serverDetails.applyJobsSubtitle': 'Agent-side configuration deployment history and results.',\n"
    "  'serverDetails.deploymentHistoryImmutableHint': 'Deployment history records describe actions that actually happened. They are intentionally not editable or manually deletable.',\n"
    "  'serverDetails.applyJobsLoadError': 'Failed to load deployment history.',\n"
    "  'serverDetails.loadingApplyJobs': 'Loading deployment history...',\n"
    "  'serverDetails.noApplyJobs': 'No configuration deployments have been queued yet.',\n",
)

# Russian copy.
replace_once(
    "frontend/src/shared/i18n/locales/ru.ts",
    "  'serverDetails.configVersionsSubtitle': 'Отрендеренные конфиги для этого сервера и состояние их развертывания.',\n",
    "  'serverDetails.configVersionsSubtitle': 'Неизменяемые снимки конфигурации этого сервера и состояние их развертывания.',\n"
    "  'serverDetails.configVersionsImmutableHint': 'Чтобы изменить конфигурацию, измените настройки сервера, протокола, VPN-аккаунтов или маршрутизации в RouteGate и отрендерьте новую версию. Уже созданные версии не редактируются задним числом.',\n",
)
replace_once(
    "frontend/src/shared/i18n/locales/ru.ts",
    "  'serverDetails.apply': 'Применить',\n  'serverDetails.applyJobs': 'Задачи применения',\n  'serverDetails.applyJobsSubtitle': 'Прогресс и результаты развертывания конфигов на стороне агента.',\n  'serverDetails.applyJobsLoadError': 'Не удалось загрузить задачи применения.',\n  'serverDetails.loadingApplyJobs': 'Загрузка задач применения...',\n  'serverDetails.noApplyJobs': 'Задачи применения пока не ставились в очередь.',\n",
    "  'serverDetails.apply': 'Применить',\n"
    "  'serverDetails.currentConfig': 'Текущая',\n"
    "  'serverDetails.applyAgain': 'Применить снова',\n"
    "  'serverDetails.restoreVersion': 'Вернуться к v{version}',\n"
    "  'serverDetails.reapplying': 'Развертывание...',\n"
    "  'serverDetails.deleteConfig': 'Удалить',\n"
    "  'serverDetails.deletingConfig': 'Удаление...',\n"
    "  'serverDetails.deleteConfigConfirm': 'Удалить неиспользуемую v{version}? Можно удалить только версию, для которой ни разу не запускалось развертывание.',\n"
    "  'serverDetails.applyJobs': 'История развертываний',\n"
    "  'serverDetails.applyJobsSubtitle': 'История и результаты развертывания конфигураций на стороне Agent.',\n"
    "  'serverDetails.deploymentHistoryImmutableHint': 'Эти записи описывают реально выполненные операции. Их нельзя редактировать или удалять вручную задним числом.',\n"
    "  'serverDetails.applyJobsLoadError': 'Не удалось загрузить историю развертываний.',\n"
    "  'serverDetails.loadingApplyJobs': 'Загрузка истории развертываний...',\n"
    "  'serverDetails.noApplyJobs': 'Развертывания конфигураций пока не запускались.',\n",
)

# New backend lifecycle files avoid changing the canonical apply executor.
Path("backend/internal/configs/lifecycle_repository.go").write_text('''package configs

import "context"

func (r *Repository) DeleteUnusedConfigVersion(ctx context.Context, serverID, versionID string) (bool, error) {
\tresult, err := r.pool.Exec(ctx, `
\t\tDELETE FROM config_versions cv
\t\tWHERE cv.server_id = $1::uuid
\t\t  AND cv.id = $2::uuid
\t\t  AND cv.applied_at IS NULL
\t\t  AND NOT EXISTS (
\t\t\tSELECT 1
\t\t\tFROM config_apply_jobs j
\t\t\tWHERE j.config_version_id = cv.id
\t\t  )
\t`, serverID, versionID)
\tif err != nil {
\t\treturn false, err
\t}
\treturn result.RowsAffected() == 1, nil
}
''')

Path("backend/internal/configs/lifecycle.go").write_text('''package configs

import (
\t"context"
\t"errors"
\t"strings"
)

var ErrConfigVersionInUse = errors.New("config version is immutable because it has deployment history")
var ErrConfigVersionNeverApplied = errors.New("config version has never been applied")

type configVersionDeletionRepository interface {
\tDeleteUnusedConfigVersion(context.Context, string, string) (bool, error)
}

func (s *Service) DeleteUnused(ctx context.Context, serverID, versionID string) error {
\trepository, ok := s.repository.(configVersionDeletionRepository)
\tif !ok {
\t\treturn errors.New("config repository does not support version deletion")
\t}

\tdeleted, err := repository.DeleteUnusedConfigVersion(ctx, serverID, versionID)
\tif err != nil {
\t\treturn err
\t}
\tif deleted {
\t\treturn nil
\t}

\tif _, err := s.repository.GetConfigVersion(ctx, serverID, versionID); err != nil {
\t\treturn err
\t}
\treturn ErrConfigVersionInUse
}

func (s *Service) Reapply(ctx context.Context, serverID, versionID string, request ApplyConfigRequest) (ApplyConfigResponse, error) {
\tversion, err := s.repository.GetConfigVersion(ctx, serverID, versionID)
\tif err != nil {
\t\treturn ApplyConfigResponse{}, err
\t}
\tif version.AppliedAt == nil {
\t\treturn ApplyConfigResponse{}, ErrConfigVersionNeverApplied
\t}
\tif err := ensureConfigVersionSafeForApply(version); err != nil {
\t\treturn ApplyConfigResponse{}, err
\t}

\tinfo, err := s.repository.GetServerConfigInfo(ctx, serverID)
\tif err != nil {
\t\treturn ApplyConfigResponse{}, err
\t}
\tif info.Agent == nil {
\t\treturn ApplyConfigResponse{}, ErrConfigApplyAgentMissing
\t}

\tjob, err := s.repository.CreateConfigApplyJob(ctx, CreateConfigApplyJobInput{
\t\tServerID:        serverID,
\t\tAgentID:         info.Agent.ID,
\t\tConfigVersionID: version.ID,
\t\tAction:          ApplyJobActionApply,
\t\tRequestPayload: map[string]any{
\t\t\t"comment":     strings.TrimSpace(request.Comment),
\t\t\t"config_hash": version.ConfigHash,
\t\t\t"redeploy":    true,
\t\t},
\t})
\tif err != nil {
\t\treturn ApplyConfigResponse{}, err
\t}
\treturn ApplyConfigResponse{Job: job}, nil
}
''')

Path("backend/internal/configs/lifecycle_handler.go").write_text('''package configs

import (
\t"errors"
\t"net/http"

\t"github.com/jackc/pgx/v5"

\t"github.com/ikaevus/routegate/backend/internal/audit"
\t"github.com/ikaevus/routegate/backend/internal/httpx"
)

func (h *Handler) Reapply(w http.ResponseWriter, r *http.Request) {
\tserverID := r.PathValue("server_id")
\tversionID := r.PathValue("version_id")

\tresponse, err := h.service.Reapply(r.Context(), serverID, versionID, ApplyConfigRequest{})
\tif errors.Is(err, pgx.ErrNoRows) {
\t\th.recordApplyRejected(r, serverID, versionID, "config_version_not_found")
\t\twriteConfigVersionNotFound(w)
\t\treturn
\t}
\tif errors.Is(err, ErrConfigVersionNeverApplied) {
\t\th.recordApplyRejected(r, serverID, versionID, "config_never_applied")
\t\thttpx.WriteJSON(w, http.StatusConflict, httpx.Error("config_never_applied", "Only a previously applied config version can be redeployed through this action."))
\t\treturn
\t}
\tif errors.Is(err, ErrConfigApplyAgentMissing) {
\t\th.recordApplyRejected(r, serverID, versionID, "agent_missing")
\t\thttpx.WriteJSON(w, http.StatusConflict, httpx.Error("agent_missing", "Server must have a registered agent before config apply."))
\t\treturn
\t}
\tif errors.Is(err, ErrConfigApplyUnsafe) {
\t\th.recordApplyRejected(r, serverID, versionID, "unsafe_config")
\t\thttpx.WriteJSON(w, http.StatusConflict, httpx.Error("unsafe_config", "Config version is not safe to apply."))
\t\treturn
\t}
\tif errors.Is(err, ErrConfigHashMismatch) {
\t\th.recordApplyRejected(r, serverID, versionID, "config_hash_mismatch")
\t\thttpx.WriteJSON(w, http.StatusConflict, httpx.Error("config_hash_mismatch", "Config version hash does not match rendered config."))
\t\treturn
\t}
\tif err != nil {
\t\th.databaseError(w, "redeploy config version", err)
\t\treturn
\t}

\th.recordAudit(r, audit.EventInput{
\t\tAction:       "config.reapply.requested",
\t\tResourceType: "config_apply_job",
\t\tResourceID:   response.Job.ID,
\t\tResult:       audit.ResultSuccess,
\t\tMetadata: map[string]any{
\t\t\t"server_id":         response.Job.ServerID,
\t\t\t"agent_id":          response.Job.AgentID,
\t\t\t"config_version_id": response.Job.ConfigVersionID,
\t\t\t"job_id":            response.Job.ID,
\t\t\t"job_status":        response.Job.Status,
\t\t},
\t})
\thttpx.WriteJSON(w, http.StatusAccepted, response)
}

func (h *Handler) DeleteVersion(w http.ResponseWriter, r *http.Request) {
\tserverID := r.PathValue("server_id")
\tversionID := r.PathValue("version_id")

\terr := h.service.DeleteUnused(r.Context(), serverID, versionID)
\tif errors.Is(err, pgx.ErrNoRows) {
\t\twriteConfigVersionNotFound(w)
\t\treturn
\t}
\tif errors.Is(err, ErrConfigVersionInUse) {
\t\thttpx.WriteJSON(w, http.StatusConflict, httpx.Error("config_version_in_use", "Applied config versions and versions with deployment history are immutable."))
\t\treturn
\t}
\tif err != nil {
\t\th.databaseError(w, "delete unused config version", err)
\t\treturn
\t}

\th.recordAudit(r, audit.EventInput{
\t\tAction:       "config.version.deleted",
\t\tResourceType: "config_version",
\t\tResourceID:   versionID,
\t\tResult:       audit.ResultSuccess,
\t\tMetadata: map[string]any{
\t\t\t"server_id": serverID,
\t\t},
\t})
\tw.WriteHeader(http.StatusNoContent)
}
''')

Path("backend/internal/configs/lifecycle_test.go").write_text('''package configs

import (
\t"context"
\t"errors"
\t"testing"
\t"time"
)

type fakeLifecycleRepository struct {
\t*fakeApplySafetyRepository
\tdeleteResult bool
\tdeleteErr    error
}

func (f *fakeLifecycleRepository) DeleteUnusedConfigVersion(context.Context, string, string) (bool, error) {
\treturn f.deleteResult, f.deleteErr
}

func TestReapplyCreatesNormalApplyJobForPreviouslyAppliedVersion(t *testing.T) {
\trendered := validApplyRenderedConfig(t)
\thash, err := hashRenderedConfig(rendered)
\tif err != nil {
\t\tt.Fatalf("hash rendered config: %v", err)
\t}
\tappliedAt := time.Now().Add(-time.Hour)
\trepo := &fakeLifecycleRepository{fakeApplySafetyRepository: &fakeApplySafetyRepository{
\t\tversion: ConfigVersion{
\t\t\tID:             "version-id",
\t\t\tServerID:       "server-id",
\t\t\tStatus:         StatusApplied,
\t\t\tConfigHash:     hash,
\t\t\tRenderedConfig: mustMarshalRaw(t, rendered),
\t\t\tAppliedAt:      &appliedAt,
\t\t},
\t\tserverInfo: ServerConfigInfo{ID: "server-id", Name: "fi-01", Agent: &AgentConfigInfo{ID: "agent-id"}},
\t}}
\tservice := NewService(repo)

\tresponse, err := service.Reapply(context.Background(), "server-id", "version-id", ApplyConfigRequest{})
\tif err != nil {
\t\tt.Fatalf("reapply failed: %v", err)
\t}
\tif response.Job.ID != "job-id" || repo.createdInput.Action != ApplyJobActionApply {
\t\tt.Fatalf("unexpected redeploy job: response=%+v input=%+v", response, repo.createdInput)
\t}
\tif repo.createdInput.RequestPayload["redeploy"] != true {
\t\tt.Fatalf("redeploy marker missing: %+v", repo.createdInput.RequestPayload)
\t}
}

func TestReapplyRejectsNeverAppliedVersion(t *testing.T) {
\trepo := &fakeLifecycleRepository{fakeApplySafetyRepository: &fakeApplySafetyRepository{
\t\tversion: ConfigVersion{ID: "version-id", ServerID: "server-id", Status: StatusValidated},
\t}}
\tservice := NewService(repo)

\t_, err := service.Reapply(context.Background(), "server-id", "version-id", ApplyConfigRequest{})
\tif !errors.Is(err, ErrConfigVersionNeverApplied) {
\t\tt.Fatalf("expected ErrConfigVersionNeverApplied, got %v", err)
\t}
}

func TestDeleteUnusedConfigVersion(t *testing.T) {
\trepo := &fakeLifecycleRepository{
\t\tfakeApplySafetyRepository: &fakeApplySafetyRepository{version: ConfigVersion{ID: "version-id", ServerID: "server-id"}},
\t\tdeleteResult:              true,
\t}
\tservice := NewService(repo)
\tif err := service.DeleteUnused(context.Background(), "server-id", "version-id"); err != nil {
\t\tt.Fatalf("delete unused version: %v", err)
\t}
}

func TestDeleteConfigVersionRejectsHistory(t *testing.T) {
\trepo := &fakeLifecycleRepository{
\t\tfakeApplySafetyRepository: &fakeApplySafetyRepository{version: ConfigVersion{ID: "version-id", ServerID: "server-id", Status: StatusApplied}},
\t\tdeleteResult:              false,
\t}
\tservice := NewService(repo)
\tif err := service.DeleteUnused(context.Background(), "server-id", "version-id"); !errors.Is(err, ErrConfigVersionInUse) {
\t\tt.Fatalf("expected ErrConfigVersionInUse, got %v", err)
\t}
}
''')
