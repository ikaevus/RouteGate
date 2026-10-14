import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const stylesheet = readFileSync(new URL('../src/shared/rg-dashboard-live.css', import.meta.url), 'utf8');

function rule(selector) {
  const prefix = selector + ' {';
  const from = stylesheet.indexOf(prefix);
  assert.ok(from >= 0, 'Missing rule: ' + selector);
  return stylesheet.slice(from + prefix.length, stylesheet.indexOf('}', from));
}

function seconds(selector, animation) {
  const block = rule(selector);
  const match = block.match(new RegExp('animation:\\s*' + animation + '\\s+([\\d.]+)s'));
  assert.ok(match, 'Missing duration for ' + animation + ' on ' + selector);
  return Number(match[1]);
}

test('heartbeat has breathing room for rounded strokes and glow', () => {
  const padding = rule('.dashboard-page .health-pulse').match(/padding-inline:\s*(\d+)px/);
  assert.ok(padding && Number(padding[1]) >= 8, 'Heartbeat must not touch the card edges');
  assert.match(rule('.dashboard-page .health-pulse svg'), /overflow:\s*visible/);
});

test('continuous dashboard signals use calmer loops', () => {
  assert.ok(seconds('.dashboard-page .health-pulse-line', 'rg-heartbeat') >= 5.5);
  assert.ok(seconds('.dashboard-page .node-distribution-map::after', 'rg-map-sweep') >= 13);
  assert.ok(seconds('.dashboard-page .node-country-marker-ping', 'rg-marker-ping') >= 4.8);
  assert.ok(seconds('.dashboard-page .traffic-empty-wave span', 'rg-idle-wave') >= 5.5);
  assert.ok(seconds('.dashboard-live-bar--live .dashboard-live-indicator i', 'rg-live-bar-dot') >= 3.4);
});

test('online dots animate their own opacity and size rather than shadow alone', () => {
  const dot = rule('.dashboard-page .online-users-table .online-user-state--online i');
  assert.match(dot, /animation-name:\s*rg-online-dot-breathe/);
  assert.match(dot, /animation-duration:\s*5\.2s/);
  const frames = stylesheet.slice(stylesheet.indexOf('@keyframes rg-online-dot-breathe'));
  assert.match(frames, /50%\s*\{[^}]*opacity:\s*0\.62;[^}]*transform:\s*scale\(0\.82\)/s);
  assert.doesNotMatch(dot, /opacity:\s*0\s*;/);
  assert.doesNotMatch(stylesheet, /\.online-user-state--recent i\s*\{\s*animation-name:/);
});

test('reduced-motion override still disables decorative animation', () => {
  assert.match(stylesheet, /@media\s*\(prefers-reduced-motion:\s*reduce\)/);
  assert.match(stylesheet, /\.dashboard-page \*,\s*\.dashboard-page \*::before,\s*\.dashboard-page \*::after\s*\{\s*animation:\s*none\s*!important;\s*transition:\s*none\s*!important;/);
});
