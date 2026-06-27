import worldMapUrl from '../assets/world-map.svg';

export function WorldMap() {
  return <img className="world-map-asset" src={worldMapUrl} alt="" aria-hidden="true" draggable={false} />;
}
