// Browser regression: subscription retrieval evidence is read-only and
// must never be presented as proof of a working VPN tunnel.
import assert from 'node:assert/strict';
import { before, after, test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { createServer } from 'vite';

const origin = 'http://127.0.0.1:15179';
const stamp = '2026-10-08T12:00:00Z';
const account = { id: 'evidence-account', displayName: 'Evidence account',
  serverId: 'evidence-node', status: 'active', createdAt: stamp, updatedAt: stamp };
const node = { id: 'evidence-node', name: 'Evidence node', status: 'active', deploymentRole: 'vpn' };
let vite, browser;

before(async () => {
  vite = await createServer({
    root: fileURLToPath(new URL('../', import.meta.url)),
    logLevel: 'error', server: { host: '127.0.0.1', port: 15179, strictPort: true },
  });
  await vite.listen();
  browser = await chromium.launch({ headless: true,
    ...(process.env.ROUTEGATE_CHROMIUM ? { executablePath: process.env.ROUTEGATE_CHROMIUM } : {}) });
});
after(async () => { await browser?.close(); await vite?.close(); });

test('current link request status can be rechecked without changing or rotating the link', async () => {
  const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
  const errors = [], calls = [];
  let active = true, lastRequest = null;
  page.on('pageerror', error => errors.push(error.message));
  await page.addInitScript(() => {
    localStorage.setItem('routegate.locale', 'en');
    localStorage.setItem('routegate.auth.token', 'fixture');
  });
  await page.route(url => url.pathname.startsWith('/api/'), async route => {
    const path = new URL(route.request().url()).pathname;
    const method = route.request().method();
    calls.push({ path, method });
    const json = (body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
    if (path === '/api/admin/me') return json({ user: { id: 'operator', roles: ['super_admin'] } });
    if (path === '/api/admin/health') return json({ status: 'ok' });
    if (path === '/api/v1/servers') return json({ items: [node] });
    if (path === '/api/v1/vpn-accounts') return json({ items: [account], total: 1, page: 1, pageSize: 50, totalPages: 1 });
    if (path === '/api/v1/vpn-accounts/evidence-account') return json(account);
    if (path === '/api/v1/vpn-accounts/evidence-account/devices') return json({ items: [{
      device: { id: 'phone', vpnAccountId: account.id, name: 'Test iPhone',
        clientType: 'hiddify', deviceType: 'ios', status: 'active', createdAt: stamp, updatedAt: stamp },
      hasActiveToken: active, tokenLastUsedAt: lastRequest,
      compatibility: { status: 'basic_connection', guidanceCodes: [], limitationCodes: [] },
    }] });
    if (path === '/api/v1/vpn-accounts/evidence-account/subscription-token')
      return json({ hasActiveToken: false });
    if (path === '/api/v1/vpn-accounts/evidence-account/client-profile')
      return json({ vpnAccountId: account.id, connectionStatus: 'ready',
        activeProtocol: 'vless', profile: { protocol: 'vless', enabledProtocols: ['vless'], activeProtocols: ['vless'] } });
    return json({ message: `Unmocked API: ${method} ${path}` }, 404);
  });

  try {
    await page.goto(`${origin}/vpn-accounts/evidence-account/access`);
    const detail = page.locator('.vpn-access-device-detail');
    await detail.getByText('No request observed for the current subscription link.', { exact: true }).waitFor();
    await detail.getByText('This timestamp records a request to the current subscription URL', { exact: false }).waitFor();
    const before = calls.filter(call => call.path.endsWith('/devices')).length;
    lastRequest = stamp;
    await detail.getByRole('button', { name: 'Check for new requests' }).click();
    await detail.getByText('Last subscription request:', { exact: false }).waitFor();
    assert.ok(calls.filter(call => call.path.endsWith('/devices')).length > before);
    assert.equal(calls.filter(call => call.method !== 'GET').length, 0,
      'evidence recheck must not rotate any token or apply node config');

    // A revoked token or rotated-away old token must not inherit historical
    // request evidence merely because the device row still exists.
    active = false;
    await page.reload();
    await detail.getByText('No access link yet', { exact: true }).first().waitFor();
    assert.equal(await detail.getByText('Last subscription request:', { exact: false }).count(), 0);
    assert.deepEqual(errors, []);
  } finally { await page.close(); }
});
