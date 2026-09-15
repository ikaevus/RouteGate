import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createMaintenancePlan,
  executeMaintenancePlan,
  getMaintenanceInventory,
  type MaintenanceCategory,
  type MaintenancePlan,
  type MaintenancePlanResponse,
} from '../../entities/system/api/systemApi';
import { getCurrentLocale, t } from '../../shared/i18n/i18n';
import './MaintenancePanel.css';

function categoryTitle(id: string): string {
  switch (id) {
    case 'expired_ephemeral_data': return t('maintenance.category.ephemeral');
    case 'agent_heartbeat_history': return t('maintenance.category.heartbeats');
    case 'diagnostic_history': return t('maintenance.category.diagnostics');
    case 'observability_history': return t('maintenance.category.observability');
    case 'audit_history': return t('maintenance.category.audit');
    case 'manager_update_partials': return t('maintenance.category.updatePartials');
    case 'raw_traffic_history': return t('maintenance.category.rawTraffic');
    case 'platform_rollback_backups': return t('maintenance.category.rollbackBackups');
    case 'agent_runtime_artifacts': return t('maintenance.category.agentArtifacts');
    case 'prometheus_tsdb_retention': return t('maintenance.category.prometheus');
    default: return id;
  }
}

function categoryDescription(id: string): string {
  switch (id) {
    case 'expired_ephemeral_data': return t('maintenance.description.ephemeral');
    case 'agent_heartbeat_history': return t('maintenance.description.heartbeats');
    case 'diagnostic_history': return t('maintenance.description.diagnostics');
    case 'observability_history': return t('maintenance.description.observability');
    case 'audit_history': return t('maintenance.description.audit');
    case 'manager_update_partials': return t('maintenance.description.updatePartials');
    case 'raw_traffic_history': return t('maintenance.description.rawTraffic');
    case 'platform_rollback_backups': return t('maintenance.description.rollbackBackups');
    case 'agent_runtime_artifacts': return t('maintenance.description.agentArtifacts');
    case 'prometheus_tsdb_retention': return t('maintenance.description.prometheus');
    default: return '';
  }
}

function blockedReason(reason?: string): string {
  switch (reason) {
    case 'rollup_archive_boundary_required': return t('maintenance.blocked.rawTraffic');
    case 'privileged_cleanup_contract_required': return t('maintenance.blocked.privileged');
    case 'agent_cleanup_contract_required': return t('maintenance.blocked.agent');
    case 'retention_configuration_only': return t('maintenance.blocked.prometheus');
    case 'unsafe_ownership': return t('maintenance.blocked.ownership');
    default: return t('maintenance.blocked.unavailable');
  }
}

function formatBytes(value?: number): string {
  if (!value) return '';
  return new Intl.NumberFormat(getCurrentLocale() === 'ru' ? 'ru-RU' : 'en-US', {
    style: 'unit', unit: value >= 1024 * 1024 ? 'megabyte' : 'kilobyte', unitDisplay: 'short',
    maximumFractionDigits: 1,
  }).format(value / (value >= 1024 * 1024 ? 1024 * 1024 : 1024));
}

function CategoryRow({
  category,
  advanced,
  checked,
  onChange,
}: {
  category: MaintenanceCategory;
  advanced: boolean;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  const selectable = category.selectable && (advanced || category.recommended);
  return (
    <label className={`maintenance-category ${!category.selectable ? 'maintenance-category-disabled' : ''}`}>
      {advanced && (
        <input
          type="checkbox"
          checked={checked}
          disabled={!selectable}
          onChange={(event) => onChange(event.target.checked)}
        />
      )}
      <span className="maintenance-category-copy">
        <span className="maintenance-category-title">
          <strong>{categoryTitle(category.id)}</strong>
          {category.recommended && <span className="maintenance-recommended">{t('maintenance.recommended')}</span>}
        </span>
        <span>{categoryDescription(category.id)}</span>
        {!category.selectable && <small>{blockedReason(category.blockedReason)}</small>}
      </span>
      <span className="maintenance-category-metric">
        <strong>{category.candidateCount}</strong>
        <span>{t('maintenance.items')}</span>
        {category.retentionDays > 0 && <small>{t('maintenance.retention', { days: category.retentionDays })}</small>}
        {category.estimatedBytes ? <small>{formatBytes(category.estimatedBytes)}</small> : null}
      </span>
    </label>
  );
}

export function MaintenancePanel() {
  const queryClient = useQueryClient();
  const [advanced, setAdvanced] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [selectionInitialized, setSelectionInitialized] = useState(false);
  const [preview, setPreview] = useState<MaintenancePlanResponse | null>(null);
  const [lastResult, setLastResult] = useState<MaintenancePlan | null>(null);
  const inventoryQuery = useQuery({
    queryKey: ['maintenance-inventory'],
    queryFn: getMaintenanceInventory,
    staleTime: 30_000,
  });

  useEffect(() => {
    if (!inventoryQuery.data || selectionInitialized) return;
    setSelected(new Set(inventoryQuery.data.categories.filter((item) => item.selectable && item.recommended).map((item) => item.id)));
    setSelectionInitialized(true);
  }, [inventoryQuery.data, selectionInitialized]);

  const allCategories = inventoryQuery.data?.categories ?? [];
  const visibleCategories = advanced
    ? allCategories
    : allCategories.filter((item) => item.selectable && item.recommended);
  const selectedCount = useMemo(
    () => visibleCategories.filter((item) => selected.has(item.id) && item.selectable).length,
    [selected, visibleCategories],
  );
  const planMutation = useMutation({
    mutationFn: () => createMaintenancePlan(advanced ? 'advanced' : 'recommended', advanced ? [...selected] : []),
    onSuccess: (response) => { setLastResult(null); setPreview(response); },
  });
  const executeMutation = useMutation({
    mutationFn: () => {
      if (!preview) throw new Error('missing maintenance preview');
      return executeMaintenancePlan(preview.plan.id, preview.confirmationToken);
    },
    onSuccess: async (plan) => {
      setLastResult(plan);
      setPreview(null);
      await queryClient.invalidateQueries({ queryKey: ['maintenance-inventory'] });
      await queryClient.invalidateQueries({ queryKey: ['dashboard-activity'] });
    },
  });

  const setCategory = (id: string, checked: boolean) => {
    setPreview(null);
    setLastResult(null);
    setSelected((current) => {
      const next = new Set(current);
      if (checked) next.add(id); else next.delete(id);
      return next;
    });
  };

  return (
    <section className="panel settings-panel maintenance-panel">
      <div className="settings-panel-heading">
        <div>
          <div className="panel-title">{t('maintenance.title')}</div>
          <p className="panel-subtitle">{t('maintenance.subtitle')}</p>
        </div>
        <button
          type="button"
          className="small-button"
          onClick={() => { setAdvanced((value) => !value); setPreview(null); setLastResult(null); }}
        >
          {advanced ? t('maintenance.useRecommended') : t('maintenance.advanced')}
        </button>
      </div>

      <div className="maintenance-safety-note">{t('maintenance.safetyNote')}</div>

      {inventoryQuery.isLoading && <p className="empty-state">{t('maintenance.analyzing')}</p>}
      {inventoryQuery.isError && <div className="form-message form-message-error">{t('maintenance.loadError')}</div>}
      {inventoryQuery.data && (
        <div className="maintenance-category-list">
          {visibleCategories.map((category) => (
            <CategoryRow
              key={category.id}
              category={category}
              advanced={advanced}
              checked={selected.has(category.id)}
              onChange={(checked) => setCategory(category.id, checked)}
            />
          ))}
        </div>
      )}

      {!preview && inventoryQuery.isSuccess && (
        <div className="maintenance-actions">
          <span>{advanced ? t('maintenance.selected', { count: selectedCount }) : t('maintenance.recommendedReady')}</span>
          <button
            type="button"
            className="primary-button"
            disabled={planMutation.isPending || (advanced && selectedCount === 0)}
            onClick={() => planMutation.mutate()}
          >
            {planMutation.isPending ? t('maintenance.preparing') : t('maintenance.preview')}
          </button>
        </div>
      )}

      {planMutation.isError && <div className="form-message form-message-error">{t('maintenance.planError')}</div>}
      {preview && (
        <div className="maintenance-confirm">
          <div>
            <strong>{t('maintenance.confirmTitle')}</strong>
            <p>{t('maintenance.confirmWarning')}</p>
          </div>
          <div className="maintenance-preview-list">
            {preview.plan.payload.items.map((item) => (
              <div key={item.categoryId}>
                <span>{categoryTitle(item.categoryId)}</span>
                <strong>{t('maintenance.deleteItems', { count: item.candidateCount })}</strong>
              </div>
            ))}
          </div>
          <div className="maintenance-confirm-actions">
            <button type="button" className="small-button" disabled={executeMutation.isPending} onClick={() => setPreview(null)}>
              {t('maintenance.cancel')}
            </button>
            <button type="button" className="danger-button" disabled={executeMutation.isPending} onClick={() => executeMutation.mutate()}>
              {executeMutation.isPending ? t('maintenance.cleaning') : t('maintenance.confirm')}
            </button>
          </div>
        </div>
      )}

      {lastResult?.status === 'succeeded' && (
        <div className="form-message form-message-success">{t('maintenance.completed')}</div>
      )}
      {(executeMutation.isError || lastResult?.status === 'failed') && (
        <div className="form-message form-message-error">{t('maintenance.executeError')}</div>
      )}
    </section>
  );
}
