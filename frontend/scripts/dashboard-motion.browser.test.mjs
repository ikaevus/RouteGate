// Real Chromium check for screenshot-reported Dashboard motion discrepancies.
// Uses local CSS and a minimal DOM fixture; never contacts the production API.
import assert from 'node:assert/strict';
import { after, before, test } from 'node:test';
import { readFileSync } from 'node:fs';
import { chromium } from 'playwright';

const css = readFileSync(new URL('../src/shared/rg-dashboard-live.css', import.meta.url), 'utf8');
let browser;

before(async () => {
  browser = await chromium.launch({
    headless: true,
    ...(process.env.ROUTEGATE_CHROMIUM ? { executablePath: process.env.ROUTEGATE_CHROMIUM } : {}),
  });
});
after(async () => { await browser?.close(); });

for (const width of [390, 1440]) {
  test('online indicator breathing in Chromium at ' + width + 'px', async () => {
    const page = await browser.newPage({ viewport: { width, height: 900 } });
    try {
      await page.emulateMedia({ reducedMotion: 'no-preference' });
      const markup = [
        '<div class="dashboard-page">',
        '<section class="health-pulse health-pulse--healthy">',
        '<svg viewBox="0 0 240 48"><path class="health-pulse-line" d="M0 24 H240"></path></svg>',
        '</section><div class="node-distribution-map"></div>',
        '<section class="traffic-empty-state"><div class="traffic-empty-wave"><span></span></div></section>',
        '<div class="online-users-table dashboard-table">',
        '<div class="dashboard-table-row"><span class="online-user-state online-user-state--online"><i></i>Online</span></div>',
        '<div class="dashboard-table-row"><span class="online-user-state online-user-state--recent"><i></i>Recently active</span></div>',
        '</div></div>',
      ].join('');
      await page.setContent('<style>' + css + '</style>' + markup);
      const result = await page.evaluate(() => {
        const dot = document.querySelector('.online-user-state--online i');
        const recent = document.querySelector('.online-user-state--recent i');
        const animation = dot.getAnimations().find(a => a.animationName === 'rg-online-dot-breathe');
        const styles = {
          onlineDuration: getComputedStyle(dot).animationDuration,
          recentAnimation: getComputedStyle(recent).animationName,
          heartbeatDuration: getComputedStyle(document.querySelector('.health-pulse-line')).animationDuration,
          radarDuration: getComputedStyle(document.querySelector('.node-distribution-map'), '::after').animationDuration,
          waveDuration: getComputedStyle(document.querySelector('.traffic-empty-wave span')).animationDuration,
        };
        if (!animation) return { styles, missingAnimation: true };
        animation.pause();
        animation.currentTime = 0;
        const full = Number(getComputedStyle(dot).opacity);
        animation.currentTime = 2600;
        const dimmed = Number(getComputedStyle(dot).opacity);
        return { styles, full, dimmed };
      });
      assert.equal(result.missingAnimation, undefined, 'Online dot requires a real CSS animation');
      assert.equal(result.styles.onlineDuration, '5.2s');
      assert.equal(result.styles.recentAnimation, 'none', 'Recent activity must not look live');
      assert.equal(result.styles.heartbeatDuration, '6s');
      assert.equal(result.styles.radarDuration, '14s');
      assert.equal(result.styles.waveDuration, '6s');
      assert.ok(result.full >= 0.99, 'Online dot stays visible at full brightness');
      assert.ok(result.dimmed >= 0.6 && result.dimmed < result.full - 0.25,
        'Online dot itself breathes without disappearing');

      await page.emulateMedia({ reducedMotion: 'reduce' });
      const reduced = await page.evaluate(() => ({
        online: getComputedStyle(document.querySelector('.online-user-state--online i')).animationName,
        heartbeat: getComputedStyle(document.querySelector('.health-pulse-line')).animationName,
        radar: getComputedStyle(document.querySelector('.node-distribution-map'), '::after').animationName,
      }));
      assert.deepEqual(reduced, { online: 'none', heartbeat: 'none', radar: 'none' });
    } finally {
      await page.close();
    }
  });
}
