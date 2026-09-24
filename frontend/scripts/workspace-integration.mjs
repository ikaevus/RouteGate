import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { createServer } from 'vite';

// This test creates records. It must only run against the disposable CI database.
assert.equal(process.env.ROUTEGATE_E2E_ISOLATED, '1', 'Use the isolated workspace integration job');
const database = new URL(process.env.ROUTEGATE_DATABASE_URL);
assert.equal(database.pathname, '/routegate_workspace_e2e');
assert.ok(['127.0.0.1', 'localhost'].includes(database.hostname));
const managerUrl = 'http://127.0.0.1:18080';
const frontendRoot = fileURLToPath(new URL('../', import.meta.url));
const email = 'workspace@example.invalid';
const password = 'Workspace-CI-only-2026!';
const manager = spawn(process.env.ROUTEGATE_E2E_MANAGER, [], {
  cwd: fileURLToPath(new URL('../../backend/', import.meta.url)),
  env: { ...process.env, ROUTEGATE_ENV: 'dev', ROUTEGATE_HTTP_ADDR: '127.0.0.1:18080',
    ROUTEGATE_GEOIP_ENABLED: 'false', ROUTEGATE_BOOTSTRAP_ADMIN_EMAIL: email,
    ROUTEGATE_BOOTSTRAP_ADMIN_USERNAME: 'workspace-ci', ROUTEGATE_BOOTSTRAP_ADMIN_PASSWORD: password },
  stdio: ['ignore', 'ignore', 'inherit'],
});
let launchError;
manager.on('error', error => { launchError = error; });
let vite;
let browser;
try {
  let ready = false;
  for (let attempt = 0; attempt < 120; attempt++) {
    if (launchError) throw launchError;
    assert.equal(manager.exitCode, null, 'Manager exited before becoming ready');
    try { ready = (await fetch(`${managerUrl}/api/admin/health`)).ok; } catch { /* startup */ }
    if (ready) break;
    await new Promise(resolve => setTimeout(resolve, 500));
  }
  assert.ok(ready, 'Manager health did not become ready');
  async function api(path, { method = 'GET', body, token } = {}) {
    const response = await fetch(`${managerUrl}${path}`, {
      method, headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) },
      ...(body ? { body: JSON.stringify(body) } : {}),
    });
    assert.ok(response.ok, `${method} ${path}: HTTP ${response.status}`);
    return response.json();
  }
  const { token } = await api('/api/admin/auth/login', { method: 'POST', body: { email, password } });
  const server = await api('/api/v1/servers', { method: 'POST', token,
    body: { name: 'Workspace integration server', publicIp: '192.0.2.10', deploymentRole: 'vpn' } });
  const account = await api('/api/v1/vpn-accounts', { method: 'POST', token,
    body: { displayName: 'Workspace integration account', serverId: server.id } });
  await api(`/api/v1/vpn-accounts/${account.id}/activate`, { method: 'POST', token });

  vite = await createServer({ root: frontendRoot, server: { host: '127.0.0.1', port: 15173, strictPort: true,
    proxy: { '/api': { target: managerUrl, changeOrigin: true } } } });
  await vite.listen();
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('response', response => {
    if (response.url().includes('/api/') && response.status() >= 500) {
      errors.push(`${new URL(response.url()).pathname}: HTTP ${response.status()}`);
    }
  });
  await page.addInitScript(() => localStorage.setItem('routegate.locale', 'en'));
  const origin = 'http://127.0.0.1:15173';
  await page.goto(origin);
  await page.getByLabel('Email', { exact: true }).fill(email);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.locator('a.kpi-widget').first().waitFor();
  await page.locator('.servers-summary-widget .dashboard-entity-link').waitFor();
  assert.equal(await page.locator('.servers-summary-widget .dashboard-entity-link').getAttribute('href'), `/servers/${server.id}`);
  await page.locator('a.kpi-widget').first().click();
  await page.waitForURL(`${origin}/servers`);

  const workspace = `${origin}/vpn-accounts/${account.id}`;
  await page.goto(workspace);
  await page.waitForURL(`${workspace}/overview`);
  for (const section of ['access', 'routing', 'protocols', 'traffic', 'settings']) {
    await page.locator(`.workspace-nav-link[href$="/${section}"]`).click();
    await page.waitForURL(`${workspace}/${section}`);
    await page.locator(`.workspace-nav-link[aria-current="page"][href="/vpn-accounts/${account.id}/${section}"]`).waitFor();
    assert.equal(await page.locator('#vpn-account-workspace-title').innerText(), account.displayName);
  }
  const name = page.locator('.vpn-account-edit-form input').first();
  await name.fill('Workspace persisted name');
  const saved = page.waitForResponse(response => response.request().method() === 'PATCH'
    && new URL(response.url()).pathname === `/api/v1/vpn-accounts/${account.id}`);
  await page.locator('.vpn-account-edit-form button[type="submit"]').click();
  assert.ok((await saved).ok());
  await page.reload();
  await page.locator('.vpn-account-edit-form').waitFor();
  await page.waitForFunction(() => document.querySelector('.vpn-account-edit-form input')?.value === 'Workspace persisted name');
  assert.equal((await api(`/api/v1/vpn-accounts/${account.id}`, { token })).displayName, 'Workspace persisted name');

  await page.goto(`${workspace}/access?addDevice=1`);
  const form = page.locator('.vpn-access-device-add-form');
  await form.locator('input').fill('Integration laptop');
  const created = page.waitForResponse(response => response.request().method() === 'POST'
    && new URL(response.url()).pathname === `/api/v1/vpn-accounts/${account.id}/devices`);
  await form.locator('button[type="submit"]').click();
  assert.ok((await created).ok());
  await page.locator('.vpn-access-device-detail').waitFor();
  await page.reload();
  await page.locator('.vpn-access-device-detail').waitFor();
  assert.ok((await page.locator('.vpn-access-device-detail').innerText()).includes('Integration laptop'));

  for (const theme of ['dark', 'light']) {
    await page.evaluate(value => { document.documentElement.dataset.theme = value; }, theme);
    await page.setViewportSize({ width: 390, height: 844 });
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${theme} mobile overflow`);
  }
  assert.deepEqual(errors, []);
  console.log('PASS: real Manager login, Dashboard link, six workspace routes, persisted account edit and device creation, mobile themes');
} finally {
  await browser?.close();
  await vite?.close();
  manager.kill('SIGTERM');
}
