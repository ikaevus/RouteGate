import type { AnalyticsNode, HealthState } from '../../entities/analytics/api/analyticsApi';
import { t } from '../../shared/i18n/i18n';

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
    x: ((longitude + 180) / 360) * 1000,
    y: ((90 - latitude) / 180) * 500,
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
    const key = `${Math.round(item.x / 56)}:${Math.round(item.y / 44)}`;
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
      <svg viewBox="0 0 1000 500" role="img" aria-label={t('analytics.worldMapSubtitle')}>
        <defs>
          <linearGradient id="analytics-map-ocean" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="currentColor" stopOpacity="0.025" />
            <stop offset="100%" stopColor="currentColor" stopOpacity="0.075" />
          </linearGradient>
          <filter id="analytics-map-glow" x="-80%" y="-80%" width="260%" height="260%">
            <feGaussianBlur stdDeviation="5" result="blur" />
            <feMerge>
              <feMergeNode in="blur" />
              <feMergeNode in="SourceGraphic" />
            </feMerge>
          </filter>
        </defs>

        <rect className="analytics-map-ocean" x="0" y="0" width="1000" height="500" rx="24" />
        <g className="analytics-map-grid" aria-hidden="true">
          {[100, 200, 300, 400, 500, 600, 700, 800, 900].map((x) => <line key={`x-${x}`} x1={x} y1="0" x2={x} y2="500" />)}
          {[100, 200, 300, 400].map((y) => <line key={`y-${y}`} x1="0" y1={y} x2="1000" y2={y} />)}
        </g>

        <g className="analytics-map-land" aria-hidden="true">
          <path d="M70 95 L115 67 172 62 216 82 245 118 226 148 193 154 171 185 143 196 115 174 84 145 58 123 Z" />
          <path d="M206 38 L248 24 282 43 267 69 224 70 Z" />
          <path d="M210 206 L245 215 274 254 286 302 272 358 245 412 224 378 230 334 211 296 198 249 Z" />
          <path d="M430 92 L462 70 507 76 528 99 558 100 584 80 642 74 694 88 734 111 778 114 823 142 855 176 836 204 787 206 750 190 713 204 684 185 650 190 621 174 590 184 557 157 523 164 487 143 449 149 419 126 Z" />
          <path d="M475 171 L520 170 551 198 566 242 550 292 523 346 488 328 471 286 452 240 455 199 Z" />
          <path d="M765 326 L805 303 858 313 891 341 883 378 844 397 793 386 759 354 Z" />
          <path d="M902 284 L922 278 932 294 918 310 902 304 Z" />
          <path d="M594 340 L606 351 600 372 589 360 Z" />
        </g>

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
