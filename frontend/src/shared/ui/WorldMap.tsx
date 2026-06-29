import worldMapUrl from '../assets/world-map.svg';

export function WorldMap() {
  return <img className="world-map-svg" src={worldMapUrl} alt="" aria-hidden="true" />;
}
