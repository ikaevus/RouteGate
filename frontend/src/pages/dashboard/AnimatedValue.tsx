import { useEffect, useRef, useState } from 'react';

const DURATION_MS = 700;
const NUMBER_PATTERN = /\d+/g;

function prefersReducedMotion(): boolean {
  return typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches === true;
}

function isCountable(value: string): boolean {
  return /\d/.test(value) && /^[\d\s/]+$/.test(value);
}

function interpolate(from: string, to: string, progress: number): string {
  const fromNumbers = from.match(NUMBER_PATTERN) ?? [];
  let index = 0;
  return to.replace(NUMBER_PATTERN, (token) => {
    const target = Number(token);
    const start = Number(fromNumbers[index++] ?? 0);
    return String(Math.round(start + (target - start) * progress));
  });
}

// Counts the integers in a KPI value ("3 / 3", "12") up to their new values.
// Values whose text shape changes (e.g. "—" → "3 / 3") count up from zero;
// values without integers, such as "N/A" or "1.5 GiB", are shown as is.
export function AnimatedValue({ value }: { value: string }) {
  const [display, setDisplay] = useState(() => (isCountable(value) && !prefersReducedMotion() ? interpolate('', value, 0) : value));
  const [changed, setChanged] = useState(false);
  const shown = useRef(display);
  const lastValue = useRef<string | null>(null);

  useEffect(() => {
    const from = shown.current;
    const updated = lastValue.current !== null && lastValue.current !== value;
    lastValue.current = value;
    if (!isCountable(value) || prefersReducedMotion() || from === value) {
      shown.current = value;
      setDisplay(value);
      return undefined;
    }

    const sameShape = from.replace(NUMBER_PATTERN, '#') === value.replace(NUMBER_PATTERN, '#');
    const start = sameShape ? from : '';
    let frame = 0;
    const startedAt = performance.now();
    const tick = (now: number) => {
      const progress = Math.min((now - startedAt) / DURATION_MS, 1);
      shown.current = interpolate(start, value, 1 - (1 - progress) ** 3);
      setDisplay(shown.current);
      if (progress < 1) frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);

    if (!updated || !sameShape) {
      return () => cancelAnimationFrame(frame);
    }
    setChanged(true);
    const timer = window.setTimeout(() => setChanged(false), 900);
    return () => {
      cancelAnimationFrame(frame);
      window.clearTimeout(timer);
    };
  }, [value]);

  return <span className={changed ? 'kpi-value-text kpi-value-changed' : 'kpi-value-text'}>{display}</span>;
}
