import type { AnalyticsNode, HealthState } from '../../entities/analytics/api/analyticsApi';
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

function clusterNodes(nodes: AnalyticsNode[]): NodeCluster[] {
  const projected: ProjectedNode[] = nodes
    .filter((node) => Number.isFinite(node.location.latitude) && Number.isFinite(node.location.longitude))
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
  const clusters = clusterNodes(nodes);

  return (
    <div className="analytics-world-map" aria-label={t('analytics.worldMap')}>
      <svg viewBox={`0 0 ${MAP_WIDTH} ${MAP_HEIGHT}`} role="img" aria-label={t('analytics.worldMapSubtitle')}>
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
                onClick={handleSelect}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' || event.key === ' ') {
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
      </svg>
    </div>
  );
}
