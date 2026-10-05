// Controlled API fixtures: network/server failures must not destroy a valid
// session; only an actual HTTP 401 may require signing in again.
import assert from 'node:assert/strict';
import { before, after, test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { createServer } from 'vite';

let vite, browser;
const origin = 'http://127.0.0.1:15180';
before(async () => {
  vite = await createServer({ root: fileURLToPath(new URL('../', import.meta.url)), logLevel: 'error',
    server: { host: '127.0.0.1', port: 15180, strictPort: true } });
  await vite.listen();
  browser = await chromium.launch({ headless: true,
    ...(process.env.ROUTEGATE_CHROMIUM ? { executablePath: process.env.ROUTEGATE_CHROMIUM } : {}) });
});
after(async () => { await browser?.close(); await vite?.close(); });

async function fixture(initialResponse) {
  const page = await browser.newPage();
  const state = { response: initialResponse, requests: 0, headers: [], writes: [] };
  await page.addInitScript(() => {
    localStorage.setItem('routegate.locale', 'en');
    localStorage.setItem('routegate.auth.token', 'fixture-session');
  });
  await page.route(url => url.pathname.startsWith('/api/'), async route => {
    const request = route.request(), path = new URL(request.url()).pathname;
    const json = (body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
    if (request.method() !== 'GET') state.writes.push(path);
    if (path === '/api/admin/me') {
      state.requests++;
      state.headers.push(request.headers().authorization);
      if (state.response === 'network') return route.abort('internetdisconnected');
      if (state.response !== 200) return json({ status: 'fixture_failure', message: 'Fixture failure' }, state.response);
      return json({ user: { id: 'admin', displayName: 'Fixture admin', roles: ['super_admin'] } });
    }
    if (path === '/api/admin/health') return json({ status: 'ok' });
    return json({ items: [], total: 0 });
  });
  return { page, state };
}
const token = page => page.evaluate(() => localStorage.getItem('routegate.auth.token'));
async function refocus(page) {
  // Drive the real Query focus listener through a hidden -> visible transition.
  for (const visibility of ['hidden', 'visible']) {
    await page.evaluate(value => {
      Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => value });
      document.dispatchEvent(new Event('visibilitychange', { bubbles: true }));
    }, visibility);
  }
}

for (const response of ['network', 500, 503, 403]) {
  test(`initial session ${response} preserves sign-in and retries the current route`, async () => {
    const { page, state } = await fixture(response);
    try {
      await page.goto(`${origin}/servers`);
      await page.getByRole('button', { name: 'Retry session check', exact: true }).waitFor();
      assert.equal(await token(page), 'fixture-session');
      assert.equal(new URL(page.url()).pathname, '/servers');
      assert.ok(await page.getByRole('alert').isVisible());
      assert.equal(await page.locator('input[type="password"]').count(), 0);
      if (response === 'network' && process.env.ROUTEGATE_SESSION_SCREENSHOT_DIR) {
        for (const width of [390, 1440]) {
          await page.setViewportSize({ width, height: 900 });
          await page.screenshot({ path: `${process.env.ROUTEGATE_SESSION_SCREENSHOT_DIR}/session-${width}.png`, fullPage: true });
          assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2));
        }
      }
      state.response = 200;
      await page.getByRole('button', { name: 'Retry session check', exact: true }).click();
      await page.locator('.admin-statusbar').waitFor();
      assert.equal(new URL(page.url()).pathname, '/servers');
      assert.equal(await token(page), 'fixture-session');
      assert.ok(state.requests >= 2);
      assert.ok(state.headers.every(header => header === 'Bearer fixture-session'));
      assert.deepEqual(state.writes, []);
    } finally { await page.close(); }
  });
}

test('a background session refetch failure preserves sign-in and recovers', async () => {
  const { page, state } = await fixture(200);
  try {
    await page.goto(`${origin}/servers`);
    await page.locator('.admin-statusbar').waitFor();
    const shell = await page.locator('.admin-statusbar').elementHandle();
    state.response = 'network';
    await refocus(page);
    await page.getByRole('button', { name: 'Retry session check', exact: true }).waitFor();
    assert.equal(await token(page), 'fixture-session');
    assert.equal(new URL(page.url()).pathname, '/servers');
    assert.ok(await shell.evaluate(element => element.isConnected));
    state.response = 200;
    await page.getByRole('button', { name: 'Retry session check', exact: true }).click();
    await page.locator('.admin-statusbar').waitFor();
    await page.getByRole('button', { name: 'Retry session check', exact: true }).waitFor({ state: 'detached' });
    assert.equal(await token(page), 'fixture-session');
    assert.deepEqual(state.writes, []);
  } finally { await page.close(); }
});

for (const background of [false, true]) {
  test(`HTTP 401 ${background ? 'on refetch' : 'on initial check'} clears the session`, async () => {
    const { page, state } = await fixture(background ? 200 : 401);
    try {
      await page.goto(`${origin}/servers`);
      if (background) {
        await page.locator('.admin-statusbar').waitFor();
        state.response = 401;
        await refocus(page);
      }
      await page.waitForURL(`${origin}/login`);
      await page.locator('input[type="password"]').waitFor();
      assert.equal(await token(page), null);
      assert.equal(await page.getByRole('button', { name: 'Retry session check', exact: true }).count(), 0);
    } finally { await page.close(); }
  });
}
