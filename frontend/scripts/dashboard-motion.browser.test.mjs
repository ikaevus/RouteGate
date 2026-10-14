// Chromium contract for the screenshot-reported Dashboard online indicator mismatch.
// Use the same ordered CSS rules as the app, without touching the production API.
import assert from 'node:assert/strict';
import { after, before, test } from 'node:test';
import { readFileSync } from 'node:fs';
import { chromium } from 'playwright';

const read = relative => readFileSync(new URL(relative, import.meta.url), 'utf8');
const css = [
  read('../src/shared/rg80-dashboard-tables.css'),
  read('../src/pages/dashboard/DashboardPage.css'),
  read('../src/shared/rg-dashboard-live.css'),
].join('\n');
let browser;

before(async () => {
  browser = await chromium.launch({
    headless: true,
    ...(process.env.ROUTEGATE_CHROMIUM ? { executablePath: process.env.ROUTEGATE_CHROMIUM } : {}),
  });
});
after(async () => { await browser?.close(); });

for (const width of [390, 1440]) {
  test('server and VPN online dots render and pulse identically at ' + width + 'px', async () => {
    const page = await browser.newPage({ viewport: { width, height: 900 } });
    try {
      await page.emulateMedia({ reducedMotion: 'no-preference' });
      const html = [
        '<div class="dashboard-page">',
        '<section class="health-pulse health-pulse--healthy">',
        '<svg viewBox="0 0 240 48"><path class="health-pulse-line" d="M0 24 H240"></path></svg>',
        '</section><div class="node-distribution-map"></div>',
        '<section class="traffic-empty-state"><div class="traffic-empty-wave"><span></span></div></section>',
        '<div class="dashboard-table servers-summary-table">',
        '<div class="dashboard-table-row"><span class="server-online-dot"></span></div>',
        '<div class="dashboard-table-row"><span class="server-offline-dot"></span></div>',
        '</div>',
        '<div class="online-users-table dashboard-table">',
        '<div class="dashboard-table-row"><span class="online-user-state online-user-state--online"><i></i>Online</span></div>',
        '<div class="dashboard-table-row"><span class="online-user-state online-user-state--recent"><i></i>Recently active</span></div>',
        '</div></div>',
      ].join('');
      await page.setContent('<style>:root { --rg-success: #22c55e; }</style><style>' + css + '</style>' + html);
      const result = await page.evaluate(() => {
        const select = s => document.querySelector(s);
        const server = select('.server-online-dot');
        const online = select('.online-user-state--online i');
        const recent = select('.online-user-state--recent i');
        const offline = select('.server-offline-dot');
        const serverAnimation = server.getAnimations().find(a => a.animationName === 'rg-status-dot-breathe');
        const userAnimation = online.getAnimations().find(a => a.animationName === 'rg-status-dot-breathe');
        const style = element => getComputedStyle(element);
        const base = {
          server: {
            width: style(server).width,
            height: style(server).height,
            background: style(server).backgroundColor,
            duration: style(server).animationDuration,
            timing: style(server).animationTimingFunction,
            name: style(server).animationName,
          },
          user: {
            width: style(online).width,
            height: style(online).height,
            background: style(online).backgroundColor,
            duration: style(online).animationDuration,
            timing: style(online).animationTimingFunction,
            name: style(online).animationName,
          },
          parentOverflow: style(select('.online-user-state--online')).overflow,
          recentAnimation: style(recent).animationName,
          offlineAnimation: style(offline).animationName,
          heartbeatDuration: style(select('.health-pulse-line')).animationDuration,
          radarDuration: getComputedStyle(select('.node-distribution-map'), '::after').animationDuration,
          waveDuration: style(select('.traffic-empty-wave span')).animationDuration,
        };
        if (!serverAnimation || !userAnimation) return { base, missingAnimation: true };
        serverAnimation.pause();
        userAnimation.pause();
        function state(time) {
          serverAnimation.currentTime = time;
          userAnimation.currentTime = time;
          const get = dot => ({
            opacity: Number(style(dot).opacity),
            transform: style(dot).transform,
            glow: style(dot).boxShadow,
          });
          return { server: get(server), user: get(online) };
        }
        return { base, full: state(0), dim: state(2600) };
      });
      assert.equal(result.missingAnimation, undefined, 'Both statuses need the same running animation');
      assert.deepEqual(result.base.server, result.base.user, 'Online dot visual design must match across tables');
      assert.equal(result.base.server.width, '9px');
      assert.equal(result.base.server.height, '9px');
      assert.equal(result.base.server.duration, '5.2s');
      assert.equal(result.base.parentOverflow, 'visible', 'Online state must not clip dot glow');
      assert.equal(result.base.recentAnimation, 'none');
      assert.equal(result.base.offlineAnimation, 'none');
      assert.equal(result.base.heartbeatDuration, '6s');
      assert.equal(result.base.radarDuration, '14s');
      assert.equal(result.base.waveDuration, '6s');
      assert.deepEqual(result.full.server, result.full.user, 'At full brightness the dots are identical');
      assert.deepEqual(result.dim.server, result.dim.user, 'At half cycle the dots are identical');
      assert.ok(result.full.server.opacity >= 0.99);
      assert.ok(result.dim.server.opacity >= 0.65 && result.dim.server.opacity < result.full.server.opacity - 0.2);

      await page.emulateMedia({ reducedMotion: 'reduce' });
      const reduced = await page.evaluate(() => {
        const style = s => getComputedStyle(document.querySelector(s)).animationName;
        return {
          server: style('.server-online-dot'),
          online: style('.online-user-state--online i'),
          heartbeat: style('.health-pulse-line'),
          radar: getComputedStyle(document.querySelector('.node-distribution-map'), '::after').animationName,
        };
      });
      assert.deepEqual(reduced, { server: 'none', online: 'none', heartbeat: 'none', radar: 'none' });
    } finally {
      await page.close();
    }
  });
}
