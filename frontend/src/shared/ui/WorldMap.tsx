const worldMapUrl = new URL('../assets/world-map.svg', import.meta.url).href;

export function WorldMap() {
  return <img className="world-map-svg" src={worldMapUrl} alt="" aria-hidden="true" />;
}
