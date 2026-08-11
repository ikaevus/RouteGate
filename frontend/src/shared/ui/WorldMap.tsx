const worldMapUrl = new URL('../assets/world-map.svg', import.meta.url).href;

export function WorldMap() {
  return (
    <img
      className="world-map-svg"
      src={worldMapUrl}
      alt=""
      aria-hidden="true"
      style={{
        inset: '6px',
        height: 'calc(100% - 12px)',
        width: 'calc(100% - 12px)',
        transform: 'none',
      }}
    />
  );
}
