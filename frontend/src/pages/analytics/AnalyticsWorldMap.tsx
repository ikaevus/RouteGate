import {
  useEffect,
  useRef,
  useState,
  type MouseEvent,
  type PointerEvent,
} from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import {
  updateServerGeography,
  type AnalyticsNode,
  type HealthState,
} from '../../entities/analytics/api/analyticsApi';
import { t, type TranslationKey } from '../../shared/i18n/i18n';
import './AnalyticsActionButtons.css';
import './AnalyticsMapNodePopover.css';
import './AnalyticsWorldMapViewport.css';

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

interface MapViewport {
  x: number;
  y: number;
  width: number;
  height: number;
}

interface MapPoint {
  x: number;
  y: number;
}

interface PopoverPosition {
  x: number;
  y: number;
}

interface PanGesture {
  pointerId: number;
  startClientX: number;
  startClientY: number;
  startViewport: MapViewport;
  scaleX: number;
  scaleY: number;
  didMove: boolean;
}

const MAP_WIDTH = 1000;
const MAP_HEIGHT = 560;
const MAP_MIN_ZOOM = 1;
const MAP_MAX_ZOOM = 8;
const MAP_BUTTON_ZOOM_FACTOR = 1.45;
const MAP_WHEEL_ZOOM_SENSITIVITY = 0.0016;
const NODE_POPOVER_WIDTH = 320;
const NODE_POPOVER_HEIGHT = 220;
const NODE_POPOVER_GAP = 18;
const NODE_POPOVER_MARGIN = 8;
const INITIAL_VIEWPORT: MapViewport = {
  x: 0,
  y: 0,
  width: MAP_WIDTH,
  height: MAP_HEIGHT,
};
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

function formatLocation(node: AnalyticsNode): string {
  const structured = [node.location.city, node.location.region, node.location.country].filter(Boolean);
  return structured.length > 0 ? structured.join(', ') : node.location.label?.trim() || t('analytics.notLocated');
}

function formatIpAddress(value?: string): string {
  if (!value) {
    return '—';
  }
  return value.replace(/\/(?:32|128)$/, '');
}

function percent(value?: number): string {
  if (value === undefined || !Number.isFinite(value)) {
    return '—';
  }
  return `${Math.round(value * 100)}%`;
}

function numberValue(value?: number, digits = 2): string {
  if (value === undefined || !Number.isFinite(value)) {
    return '—';
  }
  return value.toFixed(digits).replace(/\.00$/, '');
}

function translatedRuntimeValue(prefix: string, value?: string): string {
  const normalized = value?.trim().toLowerCase();
  if (!normalized) {
    return '—';
  }
  const key = `${prefix}.${normalized}` as TranslationKey;
  const translated = t(key);
  return translated !== key ? translated : value!.trim();
}

function agentStatusLabel(value?: string): string {
  return translatedRuntimeValue('analytics.agentStatus', value);
}

function vpnCoreServiceStateLabel(value?: string): string {
  return translatedRuntimeValue('analytics.vpnCoreState', value);
}

function telemetryAgeLabel(value?: number): string {
  if (value === undefined || !Number.isFinite(value)) {
    return '—';
  }
  return t('analytics.secondsShort', { value: Math.round(value) });
}

function project(longitude: number, latitude: number): MapPoint {
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

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.max(minimum, Math.min(maximum, value));
}

function viewportZoom(viewport: MapViewport): number {
  return MAP_WIDTH / viewport.width;
}

function clampViewport(viewport: MapViewport): MapViewport {
  const width = clamp(viewport.width, MAP_WIDTH / MAP_MAX_ZOOM, MAP_WIDTH);
  const height = clamp(viewport.height, MAP_HEIGHT / MAP_MAX_ZOOM, MAP_HEIGHT);
  return {
    x: clamp(viewport.x, 0, MAP_WIDTH - width),
    y: clamp(viewport.y, 0, MAP_HEIGHT - height),
    width,
    height,
  };
}

function zoomViewport(current: MapViewport, focus: MapPoint, requestedZoom: number): MapViewport {
  const targetZoom = clamp(requestedZoom, MAP_MIN_ZOOM, MAP_MAX_ZOOM);
  const width = MAP_WIDTH / targetZoom;
  const height = MAP_HEIGHT / targetZoom;
  const boundedFocus = {
    x: clamp(focus.x, current.x, current.x + current.width),
    y: clamp(focus.y, current.y, current.y + current.height),
  };
  const xRatio = width / current.width;
  const yRatio = height / current.height;

  return clampViewport({
    x: boundedFocus.x - (boundedFocus.x - current.x) * xRatio,
    y: boundedFocus.y - (boundedFocus.y - current.y) * yRatio,
    width,
    height,
  });
}

function viewportCenter(viewport: MapViewport): MapPoint {
  return {
    x: viewport.x + viewport.width / 2,
    y: viewport.y + viewport.height / 2,
  };
}

function screenToMapPoint(svg: SVGSVGElement, clientX: number, clientY: number): MapPoint | undefined {
  const matrix = svg.getScreenCTM();
  if (!matrix) {
    return undefined;
  }

  const screenPoint = svg.createSVGPoint();
  screenPoint.x = clientX;
  screenPoint.y = clientY;
  const point = screenPoint.matrixTransform(matrix.inverse());
  return { x: point.x, y: point.y };
}

function nodePopoverPosition(node: AnalyticsNode, viewport: MapViewport, zoom: number): PopoverPosition {
  const point = project(node.location.longitude as number, node.location.latitude as number);
  const popoverWidth = NODE_POPOVER_WIDTH / zoom;
  const popoverHeight = NODE_POPOVER_HEIGHT / zoom;
  const gap = NODE_POPOVER_GAP / zoom;
  const margin = NODE_POPOVER_MARGIN / zoom;
  const rightX = point.x + gap;
  const preferredX = rightX + popoverWidth <= viewport.x + viewport.width - margin
    ? rightX
    : point.x - popoverWidth - gap;

  return {
    x: clamp(
      preferredX,
      viewport.x + margin,
      viewport.x + viewport.width - popoverWidth - margin,
    ),
    y: clamp(
      point.y - popoverHeight / 2,
      viewport.y + margin,
      viewport.y + viewport.height - popoverHeight - margin,
    ),
  };
}

function clusterNodes(nodes: AnalyticsNode[], zoom: number): NodeCluster[] {
  const projected: ProjectedNode[] = nodes
    .filter(hasCoordinates)
    .map((node) => {
      const point = project(node.location.longitude as number, node.location.latitude as number);
      return { node, ...point };
    });

  const groups = new Map<string, ProjectedNode[]>();
  const clusterWidth = 56 / zoom;
  const clusterHeight = 38 / zoom;
  projected.forEach((item) => {
    const key = `${Math.round(item.x / clusterWidth)}:${Math.round(item.y / clusterHeight)}`;
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

function PanIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24">
      <path d="M7.5 12V7.2a1.45 1.45 0 0 1 2.9 0V11" />
      <path d="M10.4 11V5.5a1.45 1.45 0 0 1 2.9 0V11" />
      <path d="M13.3 11V6.7a1.45 1.45 0 0 1 2.9 0V12" />
      <path d="M16.2 12V9a1.45 1.45 0 0 1 2.9 0v4.2c0 4.4-2.7 7.3-6.9 7.3h-.8c-2.3 0-3.8-.9-5-2.5l-2.3-3.2a1.55 1.55 0 0 1 2.3-2.1l1.1 1V12" />
    </svg>
  );
}

function ZoomInIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24">
      <line x1="12" y1="5" x2="12" y2="19" />
      <line x1="5" y1="12" x2="19" y2="12" />
    </svg>
  );
}

function ZoomOutIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24">
      <line x1="5" y1="12" x2="19" y2="12" />
    </svg>
  );
}

function ResetViewIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24">
      <path d="M4.5 9A8 8 0 1 1 5 16" />
      <path d="M4.5 4.5V9H9" />
    </svg>
  );
}

function FullscreenIcon({ active }: { active: boolean }) {
  if (active) {
    return (
      <svg aria-hidden="true" viewBox="0 0 24 24">
        <path d="M9 4v5H4" />
        <path d="M15 4v5h5" />
        <path d="M9 20v-5H4" />
        <path d="M15 20v-5h5" />
      </svg>
    );
  }
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24">
      <path d="M9 4H4v5" />
      <path d="M15 4h5v5" />
      <path d="M9 20H4v-5" />
      <path d="M15 20h5v-5" />
    </svg>
  );
}

function NodeDetailsPopover({
  node,
  viewport,
  zoom,
}: {
  node: AnalyticsNode;
  viewport: MapViewport;
  zoom: number;
}) {
  const position = nodePopoverPosition(node, viewport, zoom);
  const telemetryAge = telemetryAgeLabel(node.agent.observationAgeSeconds);

  return (
    <g transform={`translate(${position.x} ${position.y}) scale(${1 / zoom})`}>
      <foreignObject
        className="analytics-map-node-popover"
        height={NODE_POPOVER_HEIGHT}
        width={NODE_POPOVER_WIDTH}
        x="0"
        y="0"
        onClick={(event) => event.stopPropagation()}
        onPointerDown={(event) => event.stopPropagation()}
      >
        <div className="analytics-map-node-popover-card" role="group" aria-label={node.name}>
          <div className="analytics-map-node-popover-header">
            <div className="analytics-map-node-popover-title">
              <strong>{node.name}</strong>
              <span className="analytics-map-node-popover-location">{formatLocation(node)}</span>
            </div>
            <span className={`analytics-map-node-popover-status analytics-map-node-popover-status--${node.health.state}`}>
              {healthLabel(node.health.state)}
            </span>
          </div>

          <div className="analytics-map-node-popover-facts">
            <div className="analytics-map-node-popover-fact">
              <span>{t('servers.publicIp')}</span>
              <strong>{formatIpAddress(node.publicIp)}</strong>
            </div>
            <div className="analytics-map-node-popover-fact">
              <span>{t('servers.provider')}</span>
              <strong>{node.provider || '—'}</strong>
            </div>
            <div className="analytics-map-node-popover-fact">
              <span>{t('analytics.agent')}</span>
              <strong>{agentStatusLabel(node.agent.status)}</strong>
            </div>
            <div className="analytics-map-node-popover-fact">
              <span>{t('analytics.vpnCore')}</span>
              <strong>{node.vpnCore.type || '—'} · {vpnCoreServiceStateLabel(node.vpnCore.serviceState)}</strong>
            </div>
            <div className="analytics-map-node-popover-fact">
              <span>{t('analytics.memory')}</span>
              <strong>{percent(node.resources.memoryUsageRatio)}</strong>
            </div>
            <div className="analytics-map-node-popover-fact">
              <span>{t('analytics.disk')}</span>
              <strong>{percent(node.resources.rootFsUsageRatio)}</strong>
            </div>
            <div className="analytics-map-node-popover-fact">
              <span>{t('analytics.load')}</span>
              <strong>{numberValue(node.resources.load1)}</strong>
            </div>
            <div className="analytics-map-node-popover-fact">
              <span>{node.agent.observationFresh ? t('analytics.observationFresh') : t('analytics.observationStale')}</span>
              <strong>{telemetryAge}</strong>
            </div>
          </div>
        </div>
      </foreignObject>
    </g>
  );
}

export function AnalyticsWorldMap({ nodes, selectedNodeId, onSelectNode }: AnalyticsWorldMapProps) {
  const queryClient = useQueryClient();
  const mapContainerRef = useRef<HTMLDivElement>(null);
  const mapSvgRef = useRef<SVGSVGElement>(null);
  const panGestureRef = useRef<PanGesture | null>(null);
  const suppressNextClickRef = useRef(false);
  const [placementTargetId, setPlacementTargetId] = useState<string>();
  const [pendingPoint, setPendingPoint] = useState<PlacementPoint>();
  const [popoverNodeId, setPopoverNodeId] = useState<string>();
  const [viewport, setViewport] = useState<MapViewport>(INITIAL_VIEWPORT);
  const [panEnabled, setPanEnabled] = useState(true);
  const [isPanning, setIsPanning] = useState(false);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const zoom = viewportZoom(viewport);
  const clusters = clusterNodes(nodes, zoom);
  const selectedNode = nodes.find((node) => node.id === selectedNodeId);
  const popoverNode = popoverNodeId ? nodes.find((node) => node.id === popoverNodeId) : undefined;
  const unlocatedNodes = nodes.filter((node) => !hasCoordinates(node));
  const placementCandidate = selectedNode && !hasCoordinates(selectedNode)
    ? selectedNode
    : unlocatedNodes.length === 1
      ? unlocatedNodes[0]
      : undefined;
  const placementNode = placementTargetId ? nodes.find((node) => node.id === placementTargetId) : undefined;
  const placementMode = placementNode !== undefined;
  const fullscreenSupported = typeof document !== 'undefined' && document.fullscreenEnabled;

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
      setPlacementTargetId(undefined);
    },
  });

  useEffect(() => {
    if (placementTargetId && selectedNodeId && selectedNodeId !== placementTargetId) {
      setPendingPoint(undefined);
      setPlacementTargetId(undefined);
      saveLocationMutation.reset();
    }
  // The mutation object is stable for the lifetime of this component; only selection changes should cancel placement.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [placementTargetId, selectedNodeId]);

  useEffect(() => {
    if (placementMode || (popoverNodeId && (!popoverNode || !hasCoordinates(popoverNode)))) {
      setPopoverNodeId(undefined);
      return;
    }
    if (popoverNodeId && selectedNodeId && popoverNodeId !== selectedNodeId) {
      setPopoverNodeId(undefined);
    }
  }, [placementMode, popoverNode, popoverNodeId, selectedNodeId]);

  useEffect(() => {
    const handleFullscreenChange = () => {
      setIsFullscreen(document.fullscreenElement === mapContainerRef.current);
    };
    document.addEventListener('fullscreenchange', handleFullscreenChange);
    return () => document.removeEventListener('fullscreenchange', handleFullscreenChange);
  }, []);

  useEffect(() => {
    const svg = mapSvgRef.current;
    if (!svg) {
      return;
    }

    const handleWheelZoom = (event: globalThis.WheelEvent) => {
      const focus = screenToMapPoint(svg, event.clientX, event.clientY);
      if (!focus) {
        return;
      }

      event.preventDefault();
      event.stopPropagation();
      setViewport((current) => {
        const currentZoom = viewportZoom(current);
        const zoomFactor = Math.exp(-event.deltaY * MAP_WHEEL_ZOOM_SENSITIVITY);
        return zoomViewport(current, focus, currentZoom * zoomFactor);
      });
    };

    svg.addEventListener('wheel', handleWheelZoom, { passive: false });
    return () => svg.removeEventListener('wheel', handleWheelZoom);
  }, []);

  const beginPlacement = () => {
    if (!placementCandidate) {
      return;
    }
    onSelectNode(placementCandidate.id);
    setPopoverNodeId(undefined);
    setPendingPoint(undefined);
    setPlacementTargetId(placementCandidate.id);
    saveLocationMutation.reset();
  };

  const cancelPlacement = () => {
    setPendingPoint(undefined);
    setPlacementTargetId(undefined);
    saveLocationMutation.reset();
  };

  const handleMapClick = (event: MouseEvent<SVGSVGElement>) => {
    if (suppressNextClickRef.current) {
      suppressNextClickRef.current = false;
      return;
    }
    if (!placementNode) {
      setPopoverNodeId(undefined);
      return;
    }
    if (saveLocationMutation.isPending) {
      return;
    }

    const point = screenToMapPoint(event.currentTarget, event.clientX, event.clientY);
    if (!point) {
      return;
    }
    const x = clamp(point.x, 0, MAP_WIDTH);
    const y = clamp(point.y, 0, MAP_HEIGHT);
    const coordinates = unproject(x, y);

    setPendingPoint({
      x,
      y,
      latitude: roundCoordinate(coordinates.latitude),
      longitude: roundCoordinate(coordinates.longitude),
    });
    saveLocationMutation.reset();
  };

  const handlePointerDown = (event: PointerEvent<SVGSVGElement>) => {
    if (!panEnabled || placementMode || zoom <= MAP_MIN_ZOOM || event.button !== 0) {
      return;
    }
    if (event.target instanceof Element && event.target.closest('.analytics-map-marker, .analytics-map-node-popover')) {
      return;
    }

    const matrix = event.currentTarget.getScreenCTM();
    if (!matrix) {
      return;
    }
    const scaleX = Math.hypot(matrix.a, matrix.b);
    const scaleY = Math.hypot(matrix.c, matrix.d);
    if (scaleX <= 0 || scaleY <= 0) {
      return;
    }

    event.currentTarget.setPointerCapture(event.pointerId);
    panGestureRef.current = {
      pointerId: event.pointerId,
      startClientX: event.clientX,
      startClientY: event.clientY,
      startViewport: viewport,
      scaleX,
      scaleY,
      didMove: false,
    };
    setIsPanning(true);
  };

  const handlePointerMove = (event: PointerEvent<SVGSVGElement>) => {
    const gesture = panGestureRef.current;
    if (!gesture || gesture.pointerId !== event.pointerId) {
      return;
    }

    const deltaX = event.clientX - gesture.startClientX;
    const deltaY = event.clientY - gesture.startClientY;
    if (Math.abs(deltaX) > 3 || Math.abs(deltaY) > 3) {
      gesture.didMove = true;
    }

    setViewport(clampViewport({
      ...gesture.startViewport,
      x: gesture.startViewport.x - deltaX / gesture.scaleX,
      y: gesture.startViewport.y - deltaY / gesture.scaleY,
    }));
  };

  const finishPan = (event: PointerEvent<SVGSVGElement>, cancelled = false) => {
    const gesture = panGestureRef.current;
    if (!gesture || gesture.pointerId !== event.pointerId) {
      return;
    }
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    suppressNextClickRef.current = !cancelled && gesture.didMove;
    panGestureRef.current = null;
    setIsPanning(false);
  };

  const changeZoom = (factor: number) => {
    setViewport((current) => zoomViewport(
      current,
      viewportCenter(current),
      viewportZoom(current) * factor,
    ));
  };

  const resetViewport = () => {
    setViewport({ ...INITIAL_VIEWPORT });
  };

  const toggleFullscreen = async () => {
    const container = mapContainerRef.current;
    if (!container || !fullscreenSupported) {
      return;
    }
    try {
      if (document.fullscreenElement === container) {
        await document.exitFullscreen();
      } else {
        await container.requestFullscreen();
      }
    } catch {
      // Fullscreen can be rejected by the browser or host policy. The map remains fully usable inline.
    }
  };

  const savePlacement = () => {
    if (!placementNode || !pendingPoint) {
      return;
    }
    saveLocationMutation.mutate({ node: placementNode, point: pendingPoint });
  };

  const canvasClasses = [
    'analytics-world-map-canvas',
    placementMode ? 'is-placement-mode' : '',
    panEnabled && zoom > MAP_MIN_ZOOM && !placementMode ? 'is-pannable' : '',
    isPanning ? 'is-panning' : '',
  ].filter(Boolean).join(' ');

  return (
    <div
      ref={mapContainerRef}
      className={`analytics-world-map${placementMode ? ' is-placement-mode' : ''}`}
      aria-label={t('analytics.worldMap')}
    >
      <div className="analytics-map-navigation-controls" role="group" aria-label={t('analytics.mapNavigation')}>
        <button
          className="analytics-map-navigation-button"
          type="button"
          aria-label={t('analytics.mapPan')}
          aria-pressed={panEnabled}
          title={t('analytics.mapPan')}
          disabled={placementMode}
          onClick={() => setPanEnabled((value) => !value)}
        >
          <PanIcon />
        </button>
        <span className="analytics-map-navigation-separator" aria-hidden="true" />
        <button
          className="analytics-map-navigation-button"
          type="button"
          aria-label={t('analytics.mapZoomOut')}
          title={t('analytics.mapZoomOut')}
          disabled={zoom <= MAP_MIN_ZOOM + 0.001}
          onClick={() => changeZoom(1 / MAP_BUTTON_ZOOM_FACTOR)}
        >
          <ZoomOutIcon />
        </button>
        <span className="analytics-map-zoom-readout" aria-hidden="true">{Math.round(zoom * 100)}%</span>
        <button
          className="analytics-map-navigation-button"
          type="button"
          aria-label={t('analytics.mapZoomIn')}
          title={t('analytics.mapZoomIn')}
          disabled={zoom >= MAP_MAX_ZOOM - 0.001}
          onClick={() => changeZoom(MAP_BUTTON_ZOOM_FACTOR)}
        >
          <ZoomInIcon />
        </button>
        <button
          className="analytics-map-navigation-button"
          type="button"
          aria-label={t('analytics.mapResetView')}
          title={t('analytics.mapResetView')}
          disabled={zoom <= MAP_MIN_ZOOM + 0.001}
          onClick={resetViewport}
        >
          <ResetViewIcon />
        </button>
        {fullscreenSupported && (
          <>
            <span className="analytics-map-navigation-separator" aria-hidden="true" />
            <button
              className="analytics-map-navigation-button"
              type="button"
              aria-label={isFullscreen ? t('analytics.mapExitFullscreen') : t('analytics.mapEnterFullscreen')}
              title={isFullscreen ? t('analytics.mapExitFullscreen') : t('analytics.mapEnterFullscreen')}
              onClick={() => void toggleFullscreen()}
            >
              <FullscreenIcon active={isFullscreen} />
            </button>
          </>
        )}
      </div>

      {placementCandidate && !placementMode && (
        <div className="analytics-map-placement-entry">
          <button className="button button-secondary" onClick={beginPlacement} type="button">
            {t('analytics.placeOnMap')}
          </button>
        </div>
      )}

      {placementNode && (
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
        ref={mapSvgRef}
        className={canvasClasses}
        viewBox={`${viewport.x} ${viewport.y} ${viewport.width} ${viewport.height}`}
        preserveAspectRatio="xMidYMid meet"
        role="img"
        aria-label={t('analytics.worldMapSubtitle')}
        onClick={handleMapClick}
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={(event) => finishPan(event)}
        onPointerCancel={(event) => finishPan(event, true)}
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
          preserveAspectRatio="none"
          width={MAP_WIDTH}
          x="0"
          y="0"
        />

        <g className="analytics-map-markers">
          {clusters.map((cluster) => {
            const preferred = preferredClusterNode(cluster);
            const selected = cluster.nodes.some((item) => item.node.id === selectedNodeId);
            const radius = cluster.nodes.length === 1 ? 8 : Math.min(18, 10 + Math.log2(cluster.nodes.length) * 3);
            const handleSelect = () => {
              onSelectNode(preferred.id);
              setPopoverNodeId(preferred.id);
            };
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
                transform={`translate(${cluster.x} ${cluster.y}) scale(${1 / zoom})`}
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

        {popoverNode && hasCoordinates(popoverNode) && !placementMode && (
          <NodeDetailsPopover node={popoverNode} viewport={viewport} zoom={zoom} />
        )}

        {pendingPoint && (
          <g
            className="analytics-map-placement-preview"
            aria-hidden="true"
            transform={`translate(${pendingPoint.x} ${pendingPoint.y}) scale(${1 / zoom})`}
          >
            <circle className="analytics-map-placement-preview-ring" r="18" />
            <circle className="analytics-map-placement-preview-core" r="7" />
            <path d="M-26 0 H-12 M12 0 H26 M0 -26 V-12 M0 12 V26" />
          </g>
        )}
      </svg>
    </div>
  );
}
