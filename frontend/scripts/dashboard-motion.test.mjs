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
  assert.ok(seconds('.dashboard-page .health-pulse-line', 'rg-heartbeat') >= 3.5);
  assert.ok(seconds('.dashboard-page .node-distribution-map::after', 'rg-map-sweep') >= 8.5);
  assert.ok(seconds('.dashboard-page .node-country-marker-ping', 'rg-marker-ping') >= 3.2);
  assert.ok(seconds('.dashboard-page .traffic-empty-wave span', 'rg-idle-wave') >= 3.3);
  assert.ok(seconds('.dashboard-live-bar--live .dashboard-live-indicator i', 'rg-live-bar-dot') >= 2.2);
});

test('reduced-motion override still disables decorative animation', () => {
  assert.match(stylesheet, /@media\s*\(prefers-reduced-motion:\s*reduce\)/);
  assert.match(stylesheet, /\.dashboard-page \*,\s*\.dashboard-page \*::before,\s*\.dashboard-page \*::after\s*\{\s*animation:\s*none\s*!important;\s*transition:\s*none\s*!important;/);
});
