// Real frontend/router tests against fixed Manager responses. No installer,
// Manager, SSH, firewall or live node is used.
import assert from 'node:assert/strict';
import { after, before, test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { createServer } from 'vite';

const root = fileURLToPath(new URL('../', import.meta.url));
const origin = 'http://127.0.0.1:15175';
const id = '11111111-1111-4111-8111-111111111111';
const base = `/api/v1/servers/${id}`;
const stamp = '2026-10-03T12:00:00Z';
const core = { type: 'sing-box', installed: true, state: 'stopped', serviceName: 'sing-box', serviceState: 'inactive' };
const node = {
  id, name: 'Test VPN node', deploymentRole: 'vpn', status: 'active', publicIp: '192.0.2.10',
  agent: { id: 'agent-test', agentVersion: 'test', status: 'online', lastSeenAt: stamp,
    capabilities: { vpnCore: core, vpnCores: [core], vpnCoreServiceOperations: ['start', 'stop', 'restart'], vpnCoreInstallationOperations: ['install_sing_box'] } },
  inventory: { connectionState: 'online', capabilityStatus: 'compatible', nextAction: 'none', managedAdapterCount: 1 },
};
const version = (number, status = 'validated') => ({
  id: `version-${number}`, serverId: id, version: number, status, configHash: 'test-hash',
  createdAt: stamp, pinned: false, appliedAt: number === 1 ? stamp : null,
});
const settings = {
  serverId: id, protocol: 'vless', vless: { port: 8443, flow: 'xtls-rprx-vision', network: 'tcp' },
  reality: { enabled: true, publicKey: 'test-key', shortId: 'ab12', serverName: 'www.example.com' },
  wireGuard: { port: 51820, address: '', dns: '', publicKey: '', ready: false },
  hysteria2: { port: 443, domain: '', acmeEmail: '', ready: false },
  shadowsocks: { port: 8388, method: '', ready: false }, mtproto: { port: 443, ready: false },
};
let vite, browser;
before(async () => {
  vite = await createServer({ root, logLevel: 'error', server: { host: '127.0.0.1', port: 15175, strictPort: true } });
  await vite.listen();
  browser = await chromium.launch({ headless: true,
    ...(process.env.ROUTEGATE_CHROMIUM ? { executablePath: process.env.ROUTEGATE_CHROMIUM } : {}) });
});
after(async () => { await browser?.close(); await vite?.close(); });

async function open(path, fixture = {}, locale = 'en') {
  const page = await browser.newPage({ viewport: { width: locale === 'ru' ? 390 : 1440, height: 1000 } });
  const errors = [];
  const requests = [];
  let versions = fixture.versions ?? [];
  let current = fixture.current ?? null;
  let job;
  let jobReads = 0;
  let tokenCount = 0;
  page.on('pageerror', error => errors.push(error.message));
  await page.addInitScript(value => {
    localStorage.setItem('routegate.locale', value);
    localStorage.setItem('routegate.auth.token', 'test-token');
  }, locale);
  await page.route(url => url.pathname.startsWith('/api/'), async route => {
    const path = new URL(route.request().url()).pathname;
    const method = route.request().method();
    requests.push({ path, method });
    const json = (body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
    const server = fixture.unconnected ? { ...node, agent: null,
      inventory: { ...node.inventory, connectionState: 'awaiting_agent', nextAction: 'install_agent' } } : node;
    if (path === '/api/admin/health') return json({ status: 'ok' });
    if (path === '/api/admin/me') return json({ user: { id: 'admin', email: 'test@example.invalid', roles: ['super_admin'] } });
    if (path === '/api/v1/servers') return json({ items: [server] });
    if (path === base) return json(server);
    if (path === '/api/v1/routing-profiles') return json({ items: [] });
    if (path === `${base}/routing-profile`) return json({ serverId: id, routingProfile: null });
    if (path === `${base}/protocol-settings`) return json(settings);
    if (path === `${base}/registration-token`) {
      tokenCount += 1;
      return json({ serverId: id, registrationToken: `test-token-${tokenCount}`, expiresAt: '2099-01-01T00:00:00Z',
        managerUrl: 'https://manager.example.invalid', bootstrapCommand: `echo fixture-command-${tokenCount}` }, 201);
    }
    if (path === `${base}/config/versions`) return json({ items: versions, currentConfigVersionId: current });
    if (path === `${base}/config/render`) {
      if (fixture.renderError) return json({ error: { code: 'protocol_not_ready', message: 'Configure Reality before rendering.' } }, 409);
      const item = version(2, 'validation_failed');
      versions = [item, ...versions];
      return json({ configVersion: item, validationResult: { valid: false, errors: ['Fixture: missing inbound account credentials'], warnings: null } }, 201);
    }
    if (path === `${base}/config/versions/version-2/apply`) {
      job = { id: 'job-test', serverId: id, configVersionId: 'version-2', action: 'apply', status: 'pending',
        requestPayload: {}, resultPayload: {}, createdAt: stamp, updatedAt: stamp };
      return json({ job }, 202);
    }
    if (path === `${base}/config/apply-jobs`) {
      if (job && ++jobReads >= 3) {
        job = { ...job, status: fixture.applySuccess ? 'succeeded' : 'failed', updatedAt: '2026-10-03T12:01:00Z',
          resultPayload: fixture.applySuccess ? { healthcheck: 'passed' } : { failedStage: 'validate', validate: 'failed' },
          errorMessage: fixture.applySuccess ? '' : 'Reality DNS: no such host' };
        if (fixture.applySuccess) {
          current = 'version-2';
          versions = versions.map(item => item.id === current ? { ...item, status: 'applied', appliedAt: job.updatedAt } : item);
        }
      }
      return json({ items: job ? [job] : [], total: job ? 1 : 0, limit: 8, offset: 0 });
    }
    return json({ error: { code: 'not_mocked', message: 'Not mocked in isolated UI test.' } }, 404);
  });
  await page.goto(origin + path);
  return { page, errors, requests };
}

test('a stopped runtime before first apply leads to protocol setup, without Start service', async () => {
  const { page, errors, requests } = await open(`/servers/${id}/services`);
  try {
    const panel = page.locator('.vpn-core-status-panel');
    await panel.getByText('VPN service is stopped', { exact: true }).first().waitFor();
    const link = panel.getByRole('link', { name: 'Configure VPN protocol →' });
    assert.equal(await link.getAttribute('href'), `/protocol-settings/${id}`);
    assert.equal(await panel.getByRole('button', { name: 'Start service', exact: true }).count(), 0);
    await link.click();
    await page.getByText('This page shows saved settings.', { exact: false }).waitFor();
    assert.equal(await page.getByRole('link', { name: 'Create VPN account →' }).getAttribute('href'), `/vpn-accounts?create=1&server=${id}`);
    assert.ok((await page.locator('.protocol-settings-hint').allTextContents()).join(' ').includes('Agent does not open firewall rules'));
    assert.equal(requests.filter(item => item.method === 'POST').length, 0);
    assert.deepEqual(errors, []);
  } finally { await page.close(); }
});

test('an applied stopped node keeps its explicit Start service control', async () => {
  const { page, errors } = await open(`/servers/${id}/services`, { versions: [version(1)], current: 'version-1' });
  try {
    await page.getByRole('button', { name: 'Start service', exact: true }).waitFor();
    assert.deepEqual(errors, []);
  } finally { await page.close(); }
});

test('bootstrap shows retry diagnostics and rotates the generated command', async () => {
  const { page, errors, requests } = await open(`/servers/${id}/connection`, { unconnected: true });
  try {
    await page.getByRole('button', { name: 'Connect server', exact: true }).click();
    const dialog = page.locator('.agent-onboarding-dialog');
    await dialog.getByText('fixture-command-1', { exact: false }).waitFor();
    await dialog.getByText('/var/log/routegate-agent-installer.log', { exact: false }).waitFor();
    await dialog.getByRole('button', { name: 'Generate new token', exact: true }).click();
    await dialog.getByText('fixture-command-2', { exact: false }).waitFor();
    assert.equal(requests.filter(item => item.path.endsWith('/registration-token')).length, 2);
    assert.deepEqual(errors, []);
  } finally { await page.close(); }
});

test('rendering displays static validation reasons and the account prerequisite', async () => {
  const { page, errors } = await open(`/servers/${id}/deployments`);
  try {
    await page.getByRole('button', { name: 'Render config', exact: true }).click();
    await page.getByText('Fixture: missing inbound account credentials', { exact: true }).waitFor();
    await page.getByText('Validation failed. Correct the settings and render a new version.', { exact: true }).waitFor();
    assert.equal(await page.getByRole('button', { name: 'Apply', exact: true }).isDisabled(), true);
    await page.getByText('An active VPN account must be assigned', { exact: false }).waitFor();
    assert.deepEqual(errors, []);
  } finally { await page.close(); }
});

test('a rejected render shows the API reason', async () => {
  const { page, errors } = await open(`/servers/${id}/deployments`, { renderError: true });
  try {
    await page.getByRole('button', { name: 'Render config', exact: true }).click();
    await page.getByText('Configure Reality before rendering.', { exact: true }).waitFor();
    assert.deepEqual(errors, []);
  } finally { await page.close(); }
});

for (const success of [false, true]) {
  test(`queued apply refreshes to ${success ? 'success/current version' : 'failure/recovery with previous current version'}`, async () => {
    const { page, errors, requests } = await open(`/servers/${id}/deployments`, {
      versions: [version(2), version(1)], current: 'version-1', applySuccess: success,
    });
    try {
      await page.getByRole('button', { name: 'v2', exact: false }).click();
      await page.getByRole('button', { name: 'Apply', exact: true }).click();
      await page.getByText('Application is queued or running.', { exact: false }).waitFor();
      assert.equal(await page.getByRole('button', { name: 'Apply', exact: true }).isDisabled(), true);
      if (success) {
        await page.locator('.server-config-detail .badge-online').filter({ hasText: 'Current' }).waitFor({ timeout: 15000 });
      } else {
        await page.getByText('Reality DNS: no such host', { exact: true }).waitFor({ timeout: 15000 });
        await page.locator('.server-deployment-job summary').click();
        await page.getByText('Agent rejected this version during validation', { exact: false }).waitFor();
        const previous = page.locator('.server-config-choice').filter({ hasText: 'v1' });
        await previous.getByText('Current', { exact: true }).waitFor();
        assert.equal(await page.locator('.server-deployment-job-detail').getByRole('link', { name: 'Protocol Settings →' }).getAttribute('href'), `/protocol-settings/${id}`);
      }
      const reads = requests.filter(item => item.path.endsWith('/config/apply-jobs')).length;
      await new Promise(resolve => setTimeout(resolve, 2500));
      assert.equal(requests.filter(item => item.path.endsWith('/config/apply-jobs')).length, reads, 'polling stops after the terminal result');
      assert.equal(requests.filter(item => item.method === 'POST' && item.path.endsWith('/apply')).length, 1);
      assert.deepEqual(errors, []);
    } finally { await page.close(); }
  });
}

test('Russian mobile UI keeps the saved/applied distinction and next links', async () => {
  const { page, errors } = await open(`/protocol-settings/${id}`, {}, 'ru');
  try {
    await page.getByText('Здесь показаны сохранённые настройки.', { exact: false }).waitFor();
    await page.getByRole('link', { name: 'Развёртывания →' }).waitFor();
    assert.deepEqual(errors, []);
  } finally { await page.close(); }
});
