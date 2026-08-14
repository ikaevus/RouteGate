import { useEffect, useState, type MouseEvent } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import {
  updateServerGeography,
  type AnalyticsNode,
  type HealthState,
} from '../../entities/analytics/api/analyticsApi';
import { t } from '../../shared/i18n/i18n';
import './AnalyticsActionButtons.css';

interface AnalyticsWorldMapProps {
  nodes: AnalyticsNode[];
  selectedNodeId?: string;
  onSelectNode: (nodeId: string) => void;
}

interface ProjectedNode {
  node: AnalyticsNode;
  x: number;
  y: number;
}

interface NodeCluster {
  key: string;
  nodes: ProjectedNode[];
  x: number;
  y: number;
  state: HealthState;
}

interface PlacementPoint {
  x: number;
  y: number;
  latitude: number;
  longitude: number;
}

const MAP_WIDTH = 1000;
const MAP_HEIGHT = 430;
const worldMapUrl = new URL('../../shared/assets/world-map.svg', import.meta.url).href;

const HEALTH_RANK: Record<HealthState, number> = {
  healthy: 1,
  unknown: 2,
  degraded: 3,
  unhealthy: 4,
};

function healthLabel(state: HealthState): string {
  switch (state) {
    case 'healthy':
      return t('analytics.healthy');
    case 'degraded':
      return t('analytics.degraded');
    case 'unhealthy':
      return t('analytics.unhealthy');
    default:
      return t('analytics.unknown');
  }
}

function project(longitude: number, latitude: number): { x: number; y: number } {
  return {
    x: ((longitude + 180) / 360) * MAP_WIDTH,
    y: ((90 - latitude) / 180) * MAP_HEIGHT,
  };
}

function unproject(x: number, y: number): { latitude: number; longitude: number } {
  return {
    longitude: (x / MAP_WIDTH) * 360 - 180,
    latitude: 90 - (y / MAP_HEIGHT) * 180,
  };
}

function roundCoordinate(value: number): number {
  return Math.round(value * 10_000) / 10_000;
}

function hasCoordinates(node: AnalyticsNode): boolean {
  return Number.isFinite(node.location.latitude) && Number.isFinite(node.location.longitude);
}

function clusterNodes(nodes: AnalyticsNode[]): NodeCluster[] {
  const projected: ProjectedNode[] = nodes
    .filter(hasCoordinates)
    .map((node) => {
      const point = project(node.location.longitude as number, node.location.latitude as number);
      return { node, ...point };
    });

  const groups = new Map<string, ProjectedNode[]>();
  projected.forEach((item) => {
    const key = `${Math.round(item.x / 56)}:${Math.round(item.y / 38)}`;
    groups.set(key, [...(groups.get(key) ?? []), item]);
  });

  return Array.from(groups.entries()).map(([key, items]) => {
    const state = items.reduce<HealthState>((worst, item) => (
      HEALTH_RANK[item.node.health.state] > HEALTH_RANK[worst] ? item.node.health.state : worst
    ), 'healthy');
    return {
      key,
      nodes: items,
      x: items.reduce((sum, item) => sum + item.x, 0) / items.length,
      y: items.reduce((sum, item) => sum + item.y, 0) / items.length,
      state,
    };
  });
}

function clusterLabel(cluster: NodeCluster): string {
  if (cluster.nodes.length === 1) {
    const node = cluster.nodes[0].node;
    const location = [node.location.city, node.location.country].filter(Boolean).join(', ');
    return `${node.name}${location ? ` · ${location}` : ''} · ${healthLabel(node.health.state)}`;
  }
  return `${cluster.nodes.length} · ${healthLabel(cluster.state)}`;
}

function preferredClusterNode(cluster: NodeCluster): AnalyticsNode {
  return [...cluster.nodes]
    .sort((left, right) => HEALTH_RANK[right.node.health.state] - HEALTH_RANK[left.node.health.state])[0]
    .node;
}

export function AnalyticsWorldMap({ nodes, selectedNodeId, onSelectNode }: AnalyticsWorldMapProps) {
  const queryClient = useQueryClient();
  const [placementMode, setPlacementMode] = useState(false);
  const [pendingPoint, setPendingPoint] = useState<PlacementPoint>();
  const clusters = clusterNodes(nodes);
  const selectedNode = nodes.find((node) => node.id === selectedNodeId);
  const unlocatedNodes = nodes.filter((node) => !hasCoordinates(node));
  const placementNode = selectedNode && !hasCoordinates(selectedNode)
    ? selectedNode
    : unlocatedNodes.length === 1
      ? unlocatedNodes[0]
      : undefined;

  const saveLocationMutation = useMutation({
    mutationFn: ({ node, point }: { node: AnalyticsNode; point: PlacementPoint }) => updateServerGeography(node.id, {
      country: node.location.country ?? '',
      region: node.location.region ?? '',
      city: node.location.city ?? '',
      latitude: point.latitude,
      longitude: point.longitude,
      source: 'manual',
    }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['analytics-overview'] });
      setPendingPoint(undefined);
      setPlacementMode(false);
    },
  });

  useEffect(() => {
    setPendingPoint(undefined);
    setPlacementMode(false);
  }, [selectedNodeId]);

  const beginPlacement = () => {
    if (!placementNode) {
      return;
    }
    onSelectNode(placementNode.id);
    setPendingPoint(undefined);
    setPlacementMode(true);
    saveLocationMutation.reset();
  };

  const cancelPlacement = () => {
    setPendingPoint(undefined);
    setPlacementMode(false);
    saveLocationMutation.reset();
  };

  const handleMapClick = (event: MouseEvent<SVGSVGElement>) => {
    if (!placementMode || !placementNode || saveLocationMutation.isPending) {
      return;
    }

    const matrix = event.currentTarget.getScreenCTM();
    if (!matrix) {
      return;
    }

    const screenPoint = event.currentTarget.createSVGPoint();
    screenPoint.x = event.clientX;
    screenPoint.y = event.clientY;
    const svgPoint = screenPoint.matrixTransform(matrix.inverse());
    const x = Math.max(0, Math.min(MAP_WIDTH, svgPoint.x));
    const y = Math.max(0, Math.min(MAP_HEIGHT, svgPoint.y));
    const coordinates = unproject(x, y);

    setPendingPoint({
      x,
      y,
      latitude: roundCoordinate(coordinates.latitude),
      longitude: roundCoordinate(coordinates.longitude),
    });
    saveLocationMutation.reset();
  };

  const savePlacement = () => {
    if (!placementNode || !pendingPoint) {
      return;
    }
    saveLocationMutation.mutate({ node: placementNode, point: pendingPoint });
  };

  return (
    <div className={`analytics-world-map${placementMode ? ' is-placement-mode' : ''}`} aria-label={t('analytics.worldMap')}>
      {placementNode && !placementMode && (
        <div className="analytics-map-placement-entry">
          <button className="button button-secondary" onClick={beginPlacement} type="button">
            {t('analytics.placeOnMap')}
          </button>
        </div>
      )}

      {placementMode && placementNode && (
        <div className="analytics-map-placement-panel" role="status">
          <div>
            <strong>{t('analytics.placeOnMapTitle', { node: placementNode.name })}</strong>
            <span>
              {pendingPoint
                ? t('analytics.placeOnMapReady', { latitude: pendingPoint.latitude, longitude: pendingPoint.longitude })
                : t('analytics.placeOnMapHint')}
            </span>
          </div>
          {saveLocationMutation.isError && <small>{t('analytics.locationError')}</small>}
          <div className="analytics-map-placement-actions">
            <button className="button button-secondary" disabled={saveLocationMutation.isPending} onClick={cancelPlacement} type="button">
              {t('analytics.cancel')}
            </button>
            <button className="button button-primary" disabled={!pendingPoint || saveLocationMutation.isPending} onClick={savePlacement} type="button">
              {saveLocationMutation.isPending ? t('analytics.savingLocation') : t('analytics.saveLocation')}
            </button>
          </div>
        </div>
      )}

      <svg
        className={placementMode ? 'is-placement-mode' : undefined}
        viewBox={`0 0 ${MAP_WIDTH} ${MAP_HEIGHT}`}
        role="img"
        aria-label={t('analytics.worldMapSubtitle')}
        onClick={handleMapClick}
      >
        <defs>
          <filter id="analytics-map-glow" x="-80%" y="-80%" width="260%" height="260%">
            <feGaussianBlur stdDeviation="5" result="blur" />
            <feMerge>
              <feMergeNode in="blur" />
              <feMergeNode in="SourceGraphic" />
            </feMerge>
          </filter>
        </defs>

        <image
          aria-hidden="true"
          href={worldMapUrl}
          height={MAP_HEIGHT}
          preserveAspectRatio="xMidYMid meet"
          width={MAP_WIDTH}
          x="0"
          y="0"
        />

        <g className="analytics-map-markers">
          {clusters.map((cluster) => {
            const preferred = preferredClusterNode(cluster);
            const selected = cluster.nodes.some((item) => item.node.id === selectedNodeId);
            const radius = cluster.nodes.length === 1 ? 8 : Math.min(18, 10 + Math.log2(cluster.nodes.length) * 3);
            const handleSelect = () => onSelectNode(preferred.id);
            return (
              <g
                key={cluster.key}
                className={`analytics-map-marker analytics-map-marker--${cluster.state}${selected ? ' is-selected' : ''}`}
                role="button"
                tabIndex={0}
                aria-label={clusterLabel(cluster)}
                onClick={(event) => {
                  event.stopPropagation();
                  if (!placementMode) {
                    handleSelect();
                  }
                }}
                onKeyDown={(event) => {
                  if (!placementMode && (event.key === 'Enter' || event.key === ' ')) {
                    event.preventDefault();
                    handleSelect();
                  }
                }}
                transform={`translate(${cluster.x} ${cluster.y})`}
              >
                <circle className="analytics-map-marker-pulse" r={radius + 8} />
                <circle className="analytics-map-marker-core" r={radius} filter="url(#analytics-map-glow)" />
                {cluster.nodes.length > 1 && (
                  <text className="analytics-map-cluster-count" textAnchor="middle" dominantBaseline="central">
                    {cluster.nodes.length}
                  </text>
                )}
                <title>{clusterLabel(cluster)}</title>
              </g>
            );
          })}
        </g>

        {pendingPoint && (
          <g className="analytics-map-placement-preview" aria-hidden="true" transform={`translate(${pendingPoint.x} ${pendingPoint.y})`}>
            <circle className="analytics-map-placement-preview-ring" r="18" />
            <circle className="analytics-map-placement-preview-core" r="7" />
            <path d="M-26 0 H-12 M12 0 H26 M0 -26 V-12 M0 12 V26" />
          </g>
        )}
      </svg>
    </div>
  );
}
