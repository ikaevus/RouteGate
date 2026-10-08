// Actual router/UI with isolated Manager fixtures; never writes to a live node.
import assert from 'node:assert/strict';
import { before, after, test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { createServer } from 'vite';

const origin = 'http://127.0.0.1:15176';
const stamp = '2026-10-03T12:00:00Z';
const accounts = ['account-one', 'account-two'].map(id => ({ id, displayName: id, status: 'active', serverId: 'node', createdAt: stamp, updatedAt: stamp }));
const node = { id: 'node', name: 'Fixture node', status: 'active', deploymentRole: 'vpn',
  agent: { status: 'online', capabilities: { vpnCores: [{ type: 'sing-box', installed: true }] } } };
let vite, browser;
before(async () => {
  vite = await createServer({ root: fileURLToPath(new URL('../', import.meta.url)), logLevel: 'error',
    server: { host: '127.0.0.1', port: 15176, strictPort: true } });
  await vite.listen();
  browser = await chromium.launch({ headless: true,
    ...(process.env.ROUTEGATE_CHROMIUM ? { executablePath: process.env.ROUTEGATE_CHROMIUM } : {}) });
});
after(async () => { await browser?.close(); await vite?.close(); });

async function open(width = 1440, pendingAccess = false) {
  const page = await browser.newPage({ viewport: { width, height: 1000 } });
  const errors = [], writes = [];
  let reject = false, finish = true, profileReads = 0;
  const profile = { id: 'profile', vpnAccountId: accounts[0].id, name: '', clientType: 'generic', deviceType: 'other',
    fingerprintMode: 'auto', fingerprint: '', resolvedFingerprint: 'chrome', spiderX: '/',
    protocol: 'auto', enabledProtocols: ['vless'], activeProtocols: pendingAccess ? [] : ['vless'], createdAt: stamp, updatedAt: stamp };
  page.on('pageerror', error => errors.push(error.stack ?? error.message));
  await page.addInitScript(() => {
    localStorage.setItem('routegate.locale', 'en');
    localStorage.setItem('routegate.auth.token', 'fixture');
  });
  await page.route(url => url.pathname.startsWith('/api/'), async route => {
    const path = new URL(route.request().url()).pathname, method = route.request().method();
    const json = (body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
    if (method !== 'GET') writes.push({ path, method, body: route.request().postDataJSON() });
    if (path === '/api/admin/health') return json({ status: 'ok' });
    if (path === '/api/admin/me') return json({ user: { id: 'admin', roles: ['super_admin'] } });
    if (path === '/api/v1/servers') return json({ items: [node] });
    if (path === '/api/v1/vpn-accounts') return json({ items: accounts, total: 2, page: 1, pageSize: 50, totalPages: 1 });
    if (path.endsWith('/traffic')) return json({ period: { from: stamp, to: stamp },
      usage: { totalBytes: 0, txBytes: 0, rxBytes: 0 }, limit: null });
    if (path.endsWith('/routing-policy')) return json({ routingProfileSource: 'none', currentServerInGroup: false,
      automaticSelection: false, automaticSelectionPolicy: { enabled: false, allowDegraded: false, cooldownSeconds: 300 },
      clientRoutingSupported: true });
    if (path === '/api/v1/routing-profiles' || path === '/api/v1/node-groups' || path.endsWith('/devices')) return json({ items: [] });
    if (path.endsWith('/notes')) return json({ notes: '', updatedAt: stamp });
    if (path.endsWith('/subscription-token')) return json({ hasActiveToken: false });
    if (path === '/api/v1/servers/node/protocol-settings') return json({ serverId: 'node', protocol: 'vless',
      vless: { port: 8443 }, reality: { enabled: true }, shadowsocks: { ready: true },
      wireGuard: { ready: false }, hysteria2: { ready: false }, mtproto: { ready: false } });
    if (method === 'POST' && path.endsWith('/client-connection/pre-import')) {
      const request = route.request().postDataJSON();
      if (request.acknowledgeUnapplied !== true || !pendingAccess) return json({ message: 'Pre-import not permitted' }, 409);
      return json({ status: 'unapplied_preview', protocol: 'vless', format: 'vless-reality-uri',
        vlessUri: 'vless://example-credential@203.0.113.10:443?security=reality#Unapplied', warning: 'PRELIMINARY ONLY' });
    }
    if (path.endsWith('/client-profile')) {
      if (method === 'PATCH') {
        if (reject) return json({ message: 'Fixture rejected save' }, 409);
        Object.assign(profile, route.request().postDataJSON());
      } else profileReads++;
      return json({ vpnAccountId: accounts[0].id, activeProtocol: 'vless',
        connectionStatus: pendingAccess ? 'awaiting_apply' : 'ready', profile: { ...profile } });
    }
    if (path.endsWith('/config/render') || path.endsWith('/validate')) return json({ configVersion: { id: 'version' }, validationResult: { valid: true, errors: [], warnings: [] } });
    if (path.endsWith('/apply')) return json({ job: { id: 'job' } }, 202);
    if (path.endsWith('/config/apply-jobs/job')) {
      if (finish) profile.activeProtocols = [...profile.enabledProtocols];
      return json({ status: finish ? 'succeeded' : 'pending' });
    }
    const account = accounts.find(account => path === `/api/v1/vpn-accounts/${account.id}`);
    if (account) return json(account);
    return json({ message: `Unmocked endpoint: ${method} ${path}` }, 404);
  });
  await page.goto(`${origin}/vpn-accounts/${accounts[0].id}/protocols`);
  const panel = page.locator('.vpn-account-protocol-workspace');
  const select = panel.locator('select');
  try { await select.waitFor({ timeout: 10000 }); }
  catch (error) {
    await page.close();
    throw new Error(`Protocol panel did not open. Page errors: ${JSON.stringify(errors)}`, { cause: error });
  }
  return { page, panel, select, errors, writes, profile,
    reject: value => { reject = value; }, finish: value => { finish = value; }, reads: () => profileReads };
}
async function section(page, name, account = accounts[0].id) {
  await page.locator(`a[href="/vpn-accounts/${account}/${name}"]`).first().click();
}
async function checked(panel, label) {
  return panel.locator('.vpn-protocol-row').filter({ hasText: label }).locator('input[type=checkbox]').isChecked();
}

for (const width of [390, 1440]) test(`draft survives navigation/refetch but not account switch or reload (${width}px)`, async () => {
  const f = await open(width);
  try {
    await f.select.selectOption('vless');
    await f.panel.locator('.vpn-protocol-row').filter({ hasText: 'Shadowsocks' }).locator('input').check();
    const reads = f.reads();
    await section(f.page, 'settings');
    f.profile.protocol = 'shadowsocks';
    f.profile.enabledProtocols = ['shadowsocks'];
    const refresh = f.page.waitForResponse(response => response.url().endsWith('/client-profile'));
    await section(f.page, 'protocols'); await refresh;
    await f.page.waitForFunction(() => document.querySelector('.vpn-account-protocol-workspace select')?.value === 'vless');
    assert.ok(f.reads() > reads);
    assert.equal(await f.select.inputValue(), 'vless');
    assert.equal(await checked(f.panel, 'Shadowsocks'), true);
    assert.equal(await checked(f.panel, 'VLESS'), true);
    assert.equal(f.writes.length, 0);
    await section(f.page, 'overview', accounts[1].id);
    await section(f.page, 'protocols', accounts[1].id);
    await f.select.waitFor();
    assert.equal(await f.select.inputValue(), 'shadowsocks');
    await f.select.selectOption('auto');
    await f.page.reload();
    await f.select.waitFor();
    assert.equal(await f.select.inputValue(), 'shadowsocks');
    assert.deepEqual(f.errors, []);
  } finally { await f.page.close(); }
});

test('failed save retains draft; retry locks controls across navigation and clears draft only after apply', async () => {
  const f = await open();
  try {
    await f.select.selectOption('vless');
    f.reject(true);
    await f.panel.getByRole('button', { name: 'Apply protocol set', exact: true }).click();
    await f.panel.locator('.form-message-error').waitFor();
    assert.equal(await f.select.inputValue(), 'vless');
    await section(f.page, 'settings');
    await section(f.page, 'protocols');
    assert.equal(await f.select.inputValue(), 'vless');
    f.reject(false); f.finish(false);
    const pending = f.page.waitForResponse(response => response.url().endsWith('/config/apply-jobs/job'));
    await f.panel.locator('.form-actions button').click(); await pending;
    await section(f.page, 'settings');
    await section(f.page, 'protocols');
    assert.equal(await f.select.isDisabled(), true);
    assert.equal(await f.panel.locator('.form-actions button').isDisabled(), true);
    f.finish(true);
    await f.panel.getByText('Protocol set applied successfully.', { exact: true }).waitFor({ timeout: 10000 });
    assert.equal(f.writes.filter(request => request.path.endsWith('/apply')).length, 1);
    assert.equal(await f.select.isDisabled(), false);
    // Successful activation clears the explicit draft: a newer server preference now wins.
    f.profile.protocol = 'auto';
    await section(f.page, 'settings');
    const refresh = f.page.waitForResponse(response => response.url().endsWith('/client-profile'));
    await section(f.page, 'protocols'); await refresh;
    await f.page.waitForFunction(() => document.querySelector('.vpn-account-protocol-workspace select')?.value === 'auto');
    assert.deepEqual(f.errors, []);
  } finally { await f.page.close(); }
});

test('pre-import is opt-in, never auto-fetches, and clears the credential on navigation', async () => {
  const f = await open(1440, true);
  try {
    const pending = f.panel.getByRole('group', { name: 'Preliminary VLESS import' });
    await pending.waitFor();
    const reveal = pending.getByRole('button', { name: 'Show preliminary VLESS link' });
    assert.equal(await reveal.isDisabled(), true, 'operator must acknowledge first');
    assert.equal(f.writes.filter(write => write.path.endsWith('/pre-import')).length, 0);
    await pending.getByRole('checkbox').check();
    const delivered = f.page.waitForResponse(response => response.url().endsWith('/client-connection/pre-import'));
    await reveal.click();
    assert.ok((await delivered).ok());
    const credential = pending.getByRole('textbox', { name: 'Preliminary VLESS link' });
    await credential.waitFor();
    assert.match(await credential.inputValue(), /^vless:\/\//);
    assert.equal(f.writes.filter(write => write.path.endsWith('/pre-import')).length, 1);
    assert.deepEqual(f.writes.find(write => write.path.endsWith('/pre-import')).body, { acknowledgeUnapplied: true });
    await section(f.page, 'settings');
    await section(f.page, 'protocols');
    await f.panel.getByRole('group', { name: 'Preliminary VLESS import' }).waitFor();
    assert.equal(await f.panel.getByRole('textbox', { name: 'Preliminary VLESS link' }).count(), 0,
      'secret may not survive a tab switch');
    assert.equal(await f.panel.getByRole('button', { name: 'Show preliminary VLESS link' }).isDisabled(), true);
    assert.equal(f.writes.filter(write => write.path.endsWith('/apply')).length, 0,
      'preview must not apply or change node configuration');
    assert.deepEqual(f.errors, []);
  } finally { await f.page.close(); }
});
