// Browser regression test of the Dashboard "Getting started" wizard. The
// Manager API is mocked in the page (no database, no Manager), so the real
// widget, router and React Query run against fixed node states:
//   * working US + empty configured RU + unconnected FI (the production case),
//     in different API orders and after a reload: setup is complete on US;
//   * no working node: the wizard names the node it describes and its create
//     link keeps that node selected in the account form;
//   * an account added after the last apply, and a failed apply: the next
//     step is applying the configuration on that node.
// Run: node --test scripts/getting-started.browser.test.mjs
import assert from 'node:assert/strict';
import { after, before, test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { createServer } from 'vite';

const frontendRoot = fileURLToPath(new URL('../', import.meta.url));
const port = 15174;
const origin = `http://127.0.0.1:${port}`;

const US = '11111111-1111-4111-8111-111111111111';
const RU = '22222222-2222-4222-8222-222222222222';
const FI = '33333333-3333-4333-8333-333333333333';

const coreCapabilities = {
  vpnCores: [{ type: 'sing-box', installed: true, state: 'running' }],
  vpnCoreInstallationOperations: ['install_sing_box'],
};

function server(id, name, createdAt, agentStatus) {
  return {
    id, name, deploymentRole: 'hybrid', status: 'active', createdAt, updatedAt: createdAt,
    agent: agentStatus ? { id: `agent-${id}`, agentVersion: '1.4.2', status: agentStatus, lastSeenAt: createdAt, capabilities: coreCapabilities } : null,
    inventory: { connectionState: agentStatus === 'online' ? 'online' : 'not_installed', capabilityStatus: 'compatible', nextAction: 'none', managedAdapterCount: 1 },
  };
}

const servers = {
  us: server(US, 'us.routegate.org', '2026-06-01T00:00:00Z', 'online'),
  ru: server(RU, 'ru.routegate.org', '2026-09-01T00:00:00Z', 'online'),
  fi: server(FI, 'fi.routegate.org', '2026-07-01T00:00:00Z', null),
};

function account(id, serverId, createdAt) {
  return { id, displayName: id, status: 'active', serverId, createdAt, updatedAt: createdAt };
}

const vlessSettings = (serverId) => ({
  serverId, protocol: 'vless',
  vless: { port: 443 }, reality: { enabled: true, publicKey: 'public-key', shortId: 'ab12', serverName: 'www.example.com' },
  wireGuard: { port: 0, address: '', dns: '', publicKey: '', ready: false },
  hysteria2: { port: 0, ready: false }, shadowsocks: { port: 0, ready: false }, mtproto: { port: 0, ready: false },
});

/**
 * fixture: { servers: [...in API order], accounts: { [serverId]: [...newest first] },
 *   applied: { [serverId]: bool }, access: { [accountId]: status }, applyJobs: { [serverId]: [...] },
 *   unknown: [serverId...] }
 * The access summary is what the Manager reports after evaluating every
 * active account of a node; per-account endpoints stay mocked so that a
 * widget sampling accounts would see the same states and fail.
 */
function accessSummary(fixture) {
  return Object.entries(fixture.accounts).filter(([, items]) => items.length > 0).map(([serverId, items]) => {
    const summary = { serverId, activeAccounts: items.length, state: 'not_served' };
    if (fixture.unknown?.includes(serverId)) return { ...summary, state: 'unknown' };
    const oldestFirst = [...items].reverse();
    const served = oldestFirst.find((item) => fixture.applied[serverId] && fixture.access[item.id] === 'ready');
    if (served) return { ...summary, state: 'served', servedAccountId: served.id };
    const first = oldestFirst[0];
    return { ...summary, pendingAccountId: first.id, pendingStatus: fixture.applied[serverId] ? (fixture.access[first.id] ?? 'awaiting_apply') : 'awaiting_first_apply' };
  });
}

async function mockApi(page, fixture, requests = []) {
  await page.route((url) => url.pathname.startsWith('/api/'), async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    requests.push(path);
    const json = (body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
    if (path === '/api/admin/health') return json({ status: 'ok', service: 'routegate-manager', timestamp: new Date().toISOString() });
    if (path === '/api/admin/me') return json({ user: { id: 'admin', email: 'admin@example.invalid', displayName: 'Admin', roles: ['super_admin'] } });
    if (path === '/api/v1/servers') return json({ items: fixture.servers });
    if (path === '/api/v1/vpn-accounts/access-summary') return json({ items: accessSummary(fixture) });
    if (path === '/api/v1/vpn-accounts' && url.searchParams.get('status') === 'active') {
      const items = fixture.accounts[url.searchParams.get('serverId')] ?? [];
      const pageSize = Number(url.searchParams.get('pageSize') ?? 50);
      return json({ items: items.slice(0, pageSize), total: items.length, page: 1, pageSize, totalPages: 1 });
    }
    let match = path.match(/^\/api\/v1\/servers\/([^/]+)\/protocol-settings$/);
    if (match) return json(vlessSettings(match[1]));
    match = path.match(/^\/api\/v1\/servers\/([^/]+)\/config\/versions$/);
    if (match) return json({ items: [], currentConfigVersionId: fixture.applied[match[1]] ? `version-${match[1]}` : null });
    match = path.match(/^\/api\/v1\/servers\/([^/]+)\/config\/apply-jobs$/);
    if (match) return json({ items: fixture.applyJobs?.[match[1]] ?? [] });
    match = path.match(/^\/api\/v1\/vpn-accounts\/([^/]+)\/client-profile$/);
    if (match) {
      return json({
        vpnAccountId: match[1], activeProtocol: 'vless', profile: { enabledProtocols: ['vless'], activeProtocols: [] },
        connectionStatus: fixture.access[match[1]] ?? 'awaiting_first_apply',
      });
    }
    return json({ error: { code: 'not_mocked', message: 'Not mocked in this test.' } }, 404);
  });
}

let vite;
let browser;

before(async () => {
  vite = await createServer({ root: frontendRoot, logLevel: 'error', server: { host: '127.0.0.1', port, strictPort: true } });
  await vite.listen();
  browser = await chromium.launch({
    headless: true,
    ...(process.env.ROUTEGATE_CHROMIUM ? { executablePath: process.env.ROUTEGATE_CHROMIUM } : {}),
  });
});

after(async () => {
  await browser?.close();
  await vite?.close();
});

async function openDashboard(fixture) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [];
  const requests = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.addInitScript(() => {
    localStorage.setItem('routegate.locale', 'ru');
    localStorage.setItem('routegate.auth.token', 'test-token');
  });
  await mockApi(page, fixture, requests);
  await page.goto(`${origin}/`);
  const widget = page.locator('.getting-started-widget');
  await widget.waitFor();
  await page.waitForFunction(() => {
    const element = document.querySelector('.getting-started-widget');
    return element && !element.textContent.includes('Проверяем состояние RouteGate');
  });
  return { page, widget, errors, requests };
}

const production = {
  accounts: {
    [US]: [
      account('us-3', US, '2026-09-20T00:00:00Z'),
      account('us-2', US, '2026-09-10T00:00:00Z'),
      account('us-1', US, '2026-08-01T00:00:00Z'),
    ],
  },
  applied: { [US]: true, [RU]: true },
  access: { 'us-1': 'ready', 'us-2': 'ready', 'us-3': 'ready' },
};

test('working US + empty RU + unconnected FI: setup complete, independent of API order and reloads', async () => {
  for (const order of [[servers.ru, servers.fi, servers.us], [servers.us, servers.fi, servers.ru], [servers.fi, servers.us, servers.ru]]) {
    const { page, widget, errors } = await openDashboard({ ...production, servers: order });
    for (let load = 0; load < 2; load++) {
      if (load === 1) {
        await page.reload();
        await page.locator('#getting-started-complete-title').waitFor();
      }
      const text = await widget.innerText();
      assert.ok(await page.locator('#getting-started-complete-title').isVisible(), `complete card for ${order.map((item) => item.name)}`);
      assert.match(text, /Рабочий узел: us\.routegate\.org/);
      assert.match(text, /Пока без выданного доступа: fi\.routegate\.org, ru\.routegate\.org/);
      assert.doesNotMatch(text, /Создать первый VPN-аккаунт|4 из 5/);
      const href = await widget.locator('a.getting-started-action').getAttribute('href');
      assert.match(href, /^\/vpn-accounts\/us-[123]\/access$/);
    }
    assert.deepEqual(errors, []);
    await page.close();
  }
});

test('no working node: the wizard names its node and the create link keeps it', async () => {
  const { page, widget, errors } = await openDashboard({ servers: [servers.ru, servers.fi], accounts: {}, applied: { [RU]: true }, access: {} });
  const text = await widget.innerText();
  assert.match(text, /4 из 5/);
  assert.equal(await widget.locator('.getting-started-node').getAttribute('data-node-id'), RU);
  assert.match(await widget.locator('.getting-started-node').innerText(), /Узел:\s*ru\.routegate\.org/);
  const action = widget.locator('aside a.getting-started-action');
  assert.equal(await action.getAttribute('href'), `/vpn-accounts?create=1&server=${RU}`);
  await action.click();
  await page.waitForURL(`${origin}/vpn-accounts?create=1&server=${RU}`);
  const select = page.locator('.vpn-account-create-form select').first();
  await select.waitFor();
  await page.waitForFunction((id) => document.querySelector('.vpn-account-create-form select')?.value === id, RU);
  assert.deepEqual(errors, []);
  await page.close();
});

test('account created after the last apply: next step applies the configuration on that node', async () => {
  const { page, widget, errors } = await openDashboard({
    servers: [servers.us],
    accounts: { [US]: [account('fresh', US, '2026-09-30T00:00:00Z')] },
    applied: { [US]: true },
    access: { fresh: 'awaiting_apply' },
  });
  const text = await widget.innerText();
  assert.match(text, /4 из 5/);
  assert.match(text, /Узел:\s*us\.routegate\.org/);
  assert.ok(await widget.locator('aside button.getting-started-action', { hasText: 'Применить конфигурацию' }).isVisible());
  assert.equal(await page.locator('#getting-started-complete-title').count(), 0);
  assert.deepEqual(errors, []);
  await page.close();
});

test('failed apply of saved settings is not shown as applied', async () => {
  const { page, widget, errors } = await openDashboard({
    servers: [servers.us],
    accounts: { [US]: [account('first', US, '2026-09-30T00:00:00Z')] },
    applied: {},
    access: { first: 'awaiting_first_apply' },
    applyJobs: { [US]: [{ id: 'job-1', serverId: US, configVersionId: 'v1', action: 'apply', status: 'failed', errorMessage: 'healthcheck failed', requestPayload: {}, resultPayload: {}, createdAt: '2026-09-30T00:00:00Z', updatedAt: '2026-09-30T00:00:00Z' }] },
  });
  const text = await widget.innerText();
  assert.match(text, /4 из 5/);
  assert.match(text, /Последнее применение на этом узле завершилось ошибкой: healthcheck failed/);
  assert.equal(await page.locator('#getting-started-complete-title').count(), 0);
  assert.deepEqual(errors, []);
  await page.close();
});

test('old served account behind three newer accounts awaiting apply: the node stays ready', async () => {
  const { page, widget, errors, requests } = await openDashboard({
    servers: [servers.ru, servers.fi, servers.us],
    accounts: {
      [US]: [
        account('us-new-3', US, '2026-09-30T00:00:00Z'),
        account('us-new-2', US, '2026-09-29T00:00:00Z'),
        account('us-new-1', US, '2026-09-28T00:00:00Z'),
        account('us-old', US, '2026-06-01T00:00:00Z'),
      ],
    },
    applied: { [US]: true, [RU]: true },
    access: { 'us-new-3': 'awaiting_apply', 'us-new-2': 'awaiting_apply', 'us-new-1': 'awaiting_apply', 'us-old': 'ready' },
  });
  const text = await widget.innerText();
  assert.ok(await page.locator('#getting-started-complete-title').isVisible(), 'RouteGate ready');
  assert.match(text, /Рабочий узел: us\.routegate\.org/);
  assert.doesNotMatch(text, /4 из 5|Применить конфигурацию/);
  assert.equal(await widget.locator('a.getting-started-action').getAttribute('href'), '/vpn-accounts/us-old/access');
  // Readiness comes from the Manager's summary, not from polling accounts.
  assert.equal(requests.filter((path) => path.endsWith('/client-profile')).length, 0);
  assert.deepEqual(errors, []);
  await page.close();
});

test('access that could not be evaluated is not reported as a missing VPN', async () => {
  const { page, widget, errors } = await openDashboard({
    servers: [servers.ru, servers.us],
    accounts: { [US]: [account('us-1', US, '2026-08-01T00:00:00Z')] },
    applied: { [US]: true, [RU]: true },
    access: { 'us-1': 'ready' },
    unknown: [US],
  });
  const text = await widget.innerText();
  assert.match(text, /Не удалось проверить, выдаёт ли доступ узел us\.routegate\.org\. Это не означает, что VPN не работает/);
  assert.doesNotMatch(text, /Создать первый VPN-аккаунт|Создать VPN-аккаунт|RouteGate готов|из 5/);
  assert.ok(await widget.locator('button', { hasText: 'Проверить снова' }).isVisible());
  assert.deepEqual(errors, []);
  await page.close();
});
