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
    ROUTEGATE_PUBLIC_URL: 'https://workspace.example.invalid',
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
  const otherAccount = await api('/api/v1/vpn-accounts', { method: 'POST', token,
    body: { displayName: 'Other draft account', serverId: server.id } });

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

  const serverWorkspace = `${origin}/servers/${server.id}`;
  await page.goto(serverWorkspace);
  await page.waitForURL(`${serverWorkspace}/overview`);
  await page.locator('.server-workspace-overview').waitFor();
  assert.equal(await page.locator('.server-workspace-overview .primary-button').getAttribute('href'), `/servers/${server.id}/connection`);

  // Agent onboarding owns a single registration-token surface. Reopening the
  // workflow reuses the raw token already held by this browser session; only
  // the explicit rotation action is allowed to replace it.
  await page.locator('.workspace-nav-link[href$="/connection"]').click();
  const connectionPanel = page.locator('.server-connection-panel');
  const connectServerButton = connectionPanel.getByRole('button', { name: 'Connect server', exact: true });
  await connectServerButton.click();
  const onboardingDialog = page.locator('.agent-onboarding-dialog');
  await onboardingDialog.waitFor();
  const registrationTokenField = onboardingDialog.locator('.registration-token-field code');
  const firstRegistrationToken = (await registrationTokenField.innerText()).trim();
  assert.match(firstRegistrationToken, /^rg_reg_/);
  assert.equal(await page.locator('.token-panel').count(), 0);

  await onboardingDialog.locator('.registration-token-dialog-close').click();
  await connectServerButton.click();
  await onboardingDialog.waitFor();
  assert.equal((await registrationTokenField.innerText()).trim(), firstRegistrationToken);

  await onboardingDialog.getByRole('button', { name: 'Generate new token', exact: true }).click();
  await page.waitForFunction(
    previous => document.querySelector('.agent-onboarding-dialog .registration-token-field code')?.textContent?.trim() !== previous,
    firstRegistrationToken,
  );
  const rotatedRegistrationToken = (await registrationTokenField.innerText()).trim();
  assert.match(rotatedRegistrationToken, /^rg_reg_/);
  assert.notEqual(rotatedRegistrationToken, firstRegistrationToken);
  await onboardingDialog.locator('.registration-token-dialog-close').click();

  for (const section of ['connection', 'services', 'routing', 'deployments', 'settings']) {
    await page.locator(`.workspace-nav-link[href$="/${section}"]`).click();
    await page.locator(`.workspace-nav-link[aria-current="page"][href$="/${section}"]`).waitFor();
    assert.equal(await page.locator('.server-details-header h1').innerText(), server.name);
  }
  const profile = await api('/api/v1/routing-profiles', { method: 'POST', token,
    body: { name: 'Workspace routing profile', description: '', isDefault: false } });
  const routingWorkspace = `${origin}/routing-profiles/${profile.id}`;
  await page.goto(routingWorkspace);
  await page.waitForURL(`${routingWorkspace}/overview`);
  await page.locator('.routing-workspace-overview').waitFor();
  await page.locator('.workspace-nav-link[href$="/settings"]').click();
  await page.locator('.routing-profile-details-panel input').first().fill('Persisted routing profile');
  await page.locator('.workspace-nav-link[href$="/rules"]').click();
  await page.locator('.routing-rules-panel').getByRole('button', { name: 'Add routing rule', exact: true }).click();
  await page.locator('.routing-rule-form input').first().fill('Direct example');
  await page.locator('.routing-rule-form textarea').first().fill('example.com');
  await page.locator('.routing-rule-form textarea').nth(1).fill('example.org');
  await page.locator('.routing-rule-form textarea').nth(2).fill('192.0.2.0/24');
  await page.locator('.routing-rule-form summary').click();
  await page.locator('.routing-rule-form textarea').nth(3).fill('example');
  await page.locator('.routing-rule-form textarea').nth(4).fill('category-ads-all');
  await page.locator('.routing-rule-form textarea').nth(5).fill('private');
  // Deliberate rejected write: feedback is an alert, and the editor/draft stays
  // available for retry. Only this request is mocked; the retry hits Manager.
  const ruleEndpoint = `**/api/v1/routing-profiles/${profile.id}/rules`;
  await page.route(ruleEndpoint, route => route.fulfill({ status: 409,
    contentType: 'application/json', body: JSON.stringify({ message: 'Rule conflict: retry safely' }) }), { times: 1 });
  await page.locator('.routing-rule-form').getByRole('button', { name: 'Save rule', exact: true }).click();
  const ruleAlert = page.locator('.routing-rule-form').getByRole('alert');
  await ruleAlert.waitFor();
  assert.ok((await ruleAlert.textContent()).includes('Rule conflict: retry safely'));
  assert.equal(await ruleAlert.getAttribute('aria-atomic'), 'true');
  assert.equal(await page.locator('.routing-rule-form input').first().inputValue(), 'Direct example');
  assert.equal(await page.locator('.routing-rule-form textarea').nth(5).inputValue(), 'private');
  await page.locator('.routing-rule-form').getByRole('button', { name: 'Save rule', exact: true }).click();
  await page.locator('.routing-rule-list').getByText('Direct example', { exact: true }).waitFor();
  assert.equal(await page.locator('.routing-rule-matchers li').count(), 6);
  await page.locator('.routing-rule-detail').getByRole('button', { name: 'Edit', exact: true }).click();
  assert.ok(await page.locator('.routing-rule-form details').evaluate(element => element.open));
  assert.equal(await page.locator('.routing-rule-form textarea').nth(5).inputValue(), 'private');
  await page.locator('.routing-rule-form input').first().fill('Updated example');
  await page.locator('.routing-rule-form').getByRole('button', { name: 'Save rule', exact: true }).click();
  await page.locator('.routing-rule-detail h3').getByText('Updated example', { exact: true }).waitFor();
  const savedProfile = await api(`/api/v1/routing-profiles/${profile.id}`, { token });
  const savedRule = savedProfile.rules[0];
  for (const [field, values] of Object.entries({ domains: ['example.com'], domainSuffixes: ['example.org'], ipCidrs: ['192.0.2.0/24'], domainKeywords: ['example'], geoSites: ['category-ads-all'], geoIps: ['private'] })) {
    assert.deepEqual(savedRule[field], values, `preserve ${field} through editing`);
  }
  await page.locator('.workspace-nav-link[href$="/settings"]').click();
  assert.equal(await page.locator('.routing-profile-details-panel input').first().inputValue(), 'Persisted routing profile');
  await page.locator('.routing-profile-details-panel').getByRole('button', { name: 'Save profile', exact: true }).click();
  await page.waitForFunction(() => document.querySelector('.routing-workspace-header h2')?.textContent === 'Persisted routing profile');
  await page.reload();
  await page.getByRole('heading', { name: 'Persisted routing profile', exact: true }).waitFor();
  for (const theme of ['dark', 'light']) {
    await page.evaluate(value => { document.documentElement.dataset.theme = value; }, theme);
    await page.setViewportSize({ width: 390, height: 844 });
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${theme} routing mobile overflow`);
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
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
  const notes = page.locator('.vpn-account-edit-form textarea');
  await notes.fill('Unsaved notes retained across tabs');
  for (const section of ['overview', 'access', 'routing', 'protocols', 'traffic']) {
    await page.locator(`.workspace-nav-link[href$="/${section}"]`).click();
    await page.waitForURL(`${workspace}/${section}`);
    await name.waitFor({ state: 'hidden' });
    assert.equal(await name.isVisible(), false, 'Settings inputs stay hidden outside settings');
  }
  await page.locator('.workspace-nav-link[href$="/settings"]').click();
  assert.equal(await name.inputValue(), 'Workspace persisted name');
  assert.equal(await notes.inputValue(), 'Unsaved notes retained across tabs');
  // A lifecycle operation refetches both account data and notes. Changed remote
  // values must not overwrite the local dirty fields.
  await api(`/api/v1/vpn-accounts/${account.id}`, { method: 'PATCH', token,
    body: { displayName: 'Remote account name' } });
  await api(`/api/v1/vpn-accounts/${account.id}/notes`, { method: 'PATCH', token,
    body: { notes: 'Remote notes' } });
  await page.locator('.vpn-account-lifecycle-actions').getByRole('button', { name: 'Suspend', exact: true }).click();
  await page.waitForFunction(() => document.querySelector('#vpn-account-workspace-title')?.textContent === 'Remote account name');
  await page.waitForFunction(() => !document.querySelector('.vpn-account-edit-form button[type="submit"]')?.disabled);
  assert.equal(await name.inputValue(), 'Workspace persisted name', 'Refetch preserves identity draft');
  assert.equal(await notes.inputValue(), 'Unsaved notes retained across tabs', 'Refetch preserves notes draft');
  const saved = page.waitForResponse(response => response.request().method() === 'PATCH'
    && new URL(response.url()).pathname === `/api/v1/vpn-accounts/${account.id}`);
  await page.locator('.vpn-account-edit-form button[type="submit"]').click();
  assert.ok((await saved).ok());
  await page.waitForFunction(() => !document.querySelector('.vpn-account-edit-form button[type="submit"]')?.disabled);
  const accountSuccess = page.locator('.vpn-account-settings .form-message-success[role="status"]');
  await accountSuccess.waitFor();
  assert.equal(await accountSuccess.getAttribute('aria-atomic'), 'true');
  await page.reload();
  await page.locator('.vpn-account-edit-form').waitFor();
  await page.waitForFunction(() => document.querySelector('.vpn-account-edit-form input')?.value === 'Workspace persisted name');
  assert.equal((await api(`/api/v1/vpn-accounts/${account.id}`, { token })).displayName, 'Workspace persisted name');
  assert.equal((await api(`/api/v1/vpn-accounts/${account.id}/notes`, { token })).notes, 'Unsaved notes retained across tabs');
  await name.fill('Draft must not follow another account');
  await notes.fill('Private draft must not follow another account');
  await page.locator(`a.vpn-account-management-row-link[href*="/vpn-accounts/${otherAccount.id}/"]`).click();
  await page.locator('.workspace-nav-link[href$="/settings"]').click();
  await page.waitForFunction(() => document.querySelector('.vpn-account-edit-form input')?.value === 'Other draft account');
  assert.equal(await notes.inputValue(), '');
  await page.locator(`a.vpn-account-management-row-link[href*="/vpn-accounts/${account.id}/"]`).click();
  await page.locator('.workspace-nav-link[href$="/settings"]').click();
  await page.waitForFunction(() => document.querySelector('.vpn-account-edit-form input')?.value === 'Workspace persisted name');
  await page.waitForFunction(() => document.querySelector('.vpn-account-edit-form textarea')?.value === 'Unsaved notes retained across tabs');
  await page.locator('.vpn-account-lifecycle-actions').getByRole('button', { name: 'Activate', exact: true }).click();
  await page.waitForFunction(() => !document.querySelector('.vpn-account-edit-form button[type="submit"]')?.disabled);

  await page.goto(`${workspace}/access?addDevice=1`);
  const form = page.locator('.vpn-access-device-add-form');
  await form.locator('input').fill('Integration laptop');
  const created = page.waitForResponse(response => response.request().method() === 'POST'
    && new URL(response.url()).pathname === `/api/v1/vpn-accounts/${account.id}/devices`);
  await form.locator('button[type="submit"]').click();
  const createdResponse = await created;
  assert.ok(createdResponse.ok(), `Device creation: HTTP ${createdResponse.status()}`);
  await page.locator('.vpn-access-device-detail').waitFor();
  await page.reload();
  await page.locator('.vpn-access-device-detail').waitFor();
  assert.ok((await page.locator('.vpn-access-device-detail').innerText()).includes('Integration laptop'));

  // Traffic settings retain a local draft, but never carry it to another account.
  await page.locator('.workspace-nav-link[href$="/traffic"]').click();
  const trafficForm = page.locator('.traffic-limit-form');
  const trafficNumbers = trafficForm.locator('input[type="number"]');
  const trafficHardLimit = trafficForm.locator('input[type="checkbox"]');
  await trafficForm.waitFor();
  await trafficNumbers.nth(0).fill('125');
  await trafficNumbers.nth(1).fill('35');
  await trafficNumbers.nth(2).fill('12');
  await trafficHardLimit.check();
  async function checkTrafficDraft(monthly = '125', speed = '35', day = '12', hard = true) {
    assert.equal(await trafficNumbers.nth(0).inputValue(), monthly);
    assert.equal(await trafficNumbers.nth(1).inputValue(), speed);
    assert.equal(await trafficNumbers.nth(2).inputValue(), day);
    assert.equal(await trafficHardLimit.isChecked(), hard);
  }
  for (const section of ['overview', 'access', 'routing', 'protocols', 'settings']) {
    await page.locator(`.workspace-nav-link[href$="/${section}"]`).click();
    await page.waitForURL(`${workspace}/${section}`);
    await trafficForm.waitFor({ state: 'hidden' });
  }
  await page.locator('.workspace-nav-link[href$="/traffic"]').click();
  await trafficForm.waitFor();
  await checkTrafficDraft();
  const trafficPath = `/api/v1/vpn-accounts/${account.id}/traffic`;
  const limitPath = `/api/v1/vpn-accounts/${account.id}/traffic-limit`;
  await api(limitPath, { method: 'PATCH', token, body: {
    monthlyLimitBytes: 88 * 1024 ** 3, speedLimitBps: 9_000_000, resetDay: 3, hardLimitEnabled: false,
  } });
  const refreshedTraffic = page.waitForResponse(response => response.request().method() === 'GET'
    && new URL(response.url()).pathname === trafficPath);
  await page.locator('.workspace-nav-link[href$="/overview"]').click();
  assert.equal((await (await refreshedTraffic).json()).limit.resetDay, 3);
  await page.waitForLoadState('networkidle');
  await page.locator('.workspace-nav-link[href$="/traffic"]').click();
  await trafficForm.waitFor();
  await checkTrafficDraft();

  // Hold one rejected write to verify pending controls, then retry for real.
  let releaseTrafficWrite;
  const trafficWriteArrived = new Promise(resolve => { releaseTrafficWrite = resolve; });
  await page.route(`**${limitPath}`, route => { releaseTrafficWrite(route); }, { times: 1 });
  await trafficForm.locator('button[type="submit"]').click();
  const heldTrafficWrite = await trafficWriteArrived;
  await page.waitForFunction(() => {
    const controls = [...document.querySelectorAll('.traffic-limit-form input, .traffic-limit-form button[type="submit"]')];
    return controls.length === 5 && controls.every(control => control.disabled);
  });
  for (const input of await trafficForm.locator('input').all()) assert.ok(await input.isDisabled());
  assert.ok(await trafficForm.locator('button[type="submit"]').isDisabled());
  await heldTrafficWrite.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ message: 'Traffic conflict' }) });
  await trafficForm.getByRole('alert').waitFor();
  await checkTrafficDraft();
  const trafficSaved = page.waitForResponse(response => response.request().method() === 'PATCH'
    && new URL(response.url()).pathname === limitPath);
  await trafficForm.locator('button[type="submit"]').click();
  assert.ok((await trafficSaved).ok());
  await trafficForm.getByRole('status').waitFor();
  await checkTrafficDraft();
  const persistedTraffic = (await api(trafficPath, { token })).limit;
  assert.equal(persistedTraffic.monthlyLimitBytes, 125 * 1024 ** 3);
  assert.equal(persistedTraffic.speedLimitBps, 35_000_000);
  assert.equal(persistedTraffic.resetDay, 12);
  assert.equal(persistedTraffic.hardLimitEnabled, true);
  await trafficNumbers.nth(0).fill('999');
  assert.equal(await trafficForm.getByRole('status').count(), 0, 'Editing clears stale save confirmation');
  await api(`/api/v1/vpn-accounts/${otherAccount.id}/traffic-limit`, { method: 'PATCH', token, body: {
    monthlyLimitBytes: 77 * 1024 ** 3, speedLimitBps: null, resetDay: 2, hardLimitEnabled: false,
  } });
  await page.locator(`a.vpn-account-management-row-link[href*="/vpn-accounts/${otherAccount.id}/"]`).click();
  await page.locator('.workspace-nav-link[href$="/traffic"]').click();
  await page.waitForFunction(() => document.querySelector('.traffic-limit-form input')?.value === '77');
  await checkTrafficDraft('77', '', '2', false);
  await page.locator(`a.vpn-account-management-row-link[href*="/vpn-accounts/${account.id}/"]`).click();
  await page.locator('.workspace-nav-link[href$="/traffic"]').click();
  await page.waitForFunction(() => document.querySelector('.traffic-limit-form input')?.value === '125');
  await checkTrafficDraft();

  // Four independent routing drafts survive tabs/refetches and sibling saves.
  const draftServer = await api('/api/v1/servers', { method: 'POST', token,
    body: { name: 'Routing draft node', publicIp: '192.0.2.11', deploymentRole: 'vpn' } });
  const groupA = await api('/api/v1/node-groups', { method: 'POST', token,
    body: { name: 'Routing group A', description: '', selectionStrategy: 'priority' } });
  const groupB = await api('/api/v1/node-groups', { method: 'POST', token,
    body: { name: 'Routing group B', description: '', selectionStrategy: 'priority' } });
  const remoteProfile = await api('/api/v1/routing-profiles', { method: 'POST', token,
    body: { name: 'Remote assigned profile', description: '', isDefault: false } });
  const policyPath = `/api/v1/vpn-accounts/${account.id}/routing-policy`;
  const profilePath = `/api/v1/vpn-accounts/${account.id}/routing-profile`;
  const groupPath = `/api/v1/vpn-accounts/${account.id}/node-group`;
  const selectionPath = `/api/v1/vpn-accounts/${account.id}/automatic-selection`;
  await api(groupPath, { method: 'PUT', token, body: { nodeGroupId: groupA.id } });
  await page.goto(`${workspace}/routing`);
  const routingForms = page.locator('.vpn-account-routing-form');
  const nodeSelect = routingForms.nth(0).locator('select');
  const profileSelect = routingForms.nth(1).locator('select');
  const groupSelect = routingForms.nth(2).locator('select');
  const selectionForm = routingForms.nth(3);
  await groupSelect.waitFor();
  await page.waitForFunction(id => document.querySelectorAll('.vpn-account-routing-form select')[2]?.value === id, groupA.id);
  await nodeSelect.selectOption(draftServer.id);
  await profileSelect.selectOption(profile.id);
  await selectionForm.locator('input[type="checkbox"]').nth(0).check();
  await selectionForm.locator('input[type="checkbox"]').nth(1).check();
  await selectionForm.locator('input[type="number"]').fill('17');
  await groupSelect.selectOption(groupB.id);
  async function checkRoutingDraft() {
    assert.equal(await nodeSelect.inputValue(), draftServer.id);
    assert.equal(await profileSelect.inputValue(), profile.id);
    assert.equal(await groupSelect.inputValue(), groupB.id);
    assert.equal(await selectionForm.locator('input[type="number"]').inputValue(), '17');
    for (const checkbox of await selectionForm.locator('input[type="checkbox"]').all()) assert.ok(await checkbox.isChecked());
  }
  for (const section of ['overview', 'access', 'protocols', 'traffic', 'settings']) {
    await page.locator(`.workspace-nav-link[href$="/${section}"]`).click();
    await page.waitForURL(`${workspace}/${section}`);
    await routingForms.nth(0).waitFor({ state: 'hidden' });
  }
  await api(profilePath, { method: 'PUT', token, body: { routingProfileId: remoteProfile.id } });
  await api(selectionPath, { method: 'PUT', token, body: { enabled: false, allowDegraded: false, cooldownSeconds: 600 } });
  const policyRefetch = page.waitForResponse(response => response.request().method() === 'GET'
    && new URL(response.url()).pathname === policyPath);
  await page.locator('.workspace-nav-link[href$="/overview"]').click();
  assert.equal((await (await policyRefetch).json()).explicitRoutingProfile.id, remoteProfile.id);
  await page.waitForLoadState('networkidle');
  await page.locator('.workspace-nav-link[href$="/routing"]').click();
  await routingForms.nth(0).waitFor();
  await checkRoutingDraft();

  let releaseRoutingWrite;
  const routingWriteArrived = new Promise(resolve => { releaseRoutingWrite = resolve; });
  await page.route(`**${profilePath}`, route => { releaseRoutingWrite(route); }, { times: 1 });
  await routingForms.nth(1).locator('button[type="submit"]').click();
  const heldRoutingWrite = await routingWriteArrived;
  await page.waitForFunction(() => {
    const controls = [...document.querySelectorAll('.vpn-account-routing-form input, .vpn-account-routing-form select, .vpn-account-routing-form button')];
    return controls.length > 0 && controls.every(control => control.disabled);
  });
  await heldRoutingWrite.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ message: 'Routing conflict' }) });
  await page.locator('.vpn-account-routing-workspace > .form-message-error').waitFor();
  await checkRoutingDraft();
  async function saveRoutingForm(index, path, method = 'PUT') {
    const savedResponse = page.waitForResponse(response => response.request().method() === method
      && new URL(response.url()).pathname === path);
    await routingForms.nth(index).locator('button[type="submit"]').click();
    assert.ok((await savedResponse).ok());
    await page.waitForFunction(() => !document.querySelector('.vpn-account-routing-form select')?.disabled);
    await checkRoutingDraft();
  }
  await saveRoutingForm(1, profilePath);
  await saveRoutingForm(2, groupPath);
  await saveRoutingForm(3, selectionPath);
  await saveRoutingForm(0, `/api/v1/vpn-accounts/${account.id}`, 'PATCH');
  const storedPolicy = await api(policyPath, { token });
  assert.equal(storedPolicy.explicitRoutingProfile.id, profile.id);
  assert.equal(storedPolicy.nodeGroup.id, groupB.id);
  assert.equal(storedPolicy.automaticSelectionPolicy.cooldownSeconds, 1020);
  assert.equal(storedPolicy.automaticSelectionPolicy.enabled, true);
  assert.equal(storedPolicy.automaticSelectionPolicy.allowDegraded, true);
  assert.equal((await api(`/api/v1/vpn-accounts/${account.id}`, { token })).serverId, draftServer.id);
  await profileSelect.selectOption(remoteProfile.id);
  await page.locator(`a.vpn-account-management-row-link[href*="/vpn-accounts/${otherAccount.id}/"]`).click();
  // Wait for the new account's navigation; a generic suffix selector can click
  // the previous account's still-rendered tab and navigate back during routing.
  await page.locator(`.workspace-nav-link[href="/vpn-accounts/${otherAccount.id}/routing"]`).click();
  await page.waitForURL(`${origin}/vpn-accounts/${otherAccount.id}/routing`);
  await nodeSelect.waitFor();
  await page.waitForLoadState('networkidle');
  assert.equal(await nodeSelect.inputValue(), server.id);
  assert.equal(await profileSelect.inputValue(), '');
  assert.equal(await groupSelect.inputValue(), '');
  await page.locator(`a.vpn-account-management-row-link[href*="/vpn-accounts/${account.id}/"]`).click();
  await page.locator(`.workspace-nav-link[href="/vpn-accounts/${account.id}/routing"]`).click();
  await page.waitForURL(`${workspace}/routing`);
  await nodeSelect.waitFor();
  await page.waitForLoadState('networkidle');
  await checkRoutingDraft();

  // Summary navigation must open the indicated domain without writing data.
  // Check every card, including both account cards that lead to Routing.
  const navigationWrites = [];
  const recordNavigationWrite = request => {
    if (request.url().includes('/api/') && !['GET', 'HEAD', 'OPTIONS'].includes(request.method())) {
      navigationWrites.push(`${request.method()} ${new URL(request.url()).pathname}`);
    }
  };
  page.on('request', recordNavigationWrite);
  for (const target of [
    { base: serverWorkspace, selector: '.server-workspace-summaries a', sections: ['connection', 'services', 'deployments'] },
    { base: workspace, selector: '.vpn-account-summary', sections: ['routing', 'access', 'routing', 'traffic'] },
    { base: routingWorkspace, selector: '.routing-workspace-overview .primary-button', sections: ['rules'] },
  ]) {
    for (const [index, section] of target.sections.entries()) {
      await page.goto(`${target.base}/overview`);
      const link = page.locator(target.selector).nth(index);
      await link.waitFor();
      assert.equal(await page.locator(target.selector).count(), target.sections.length);
      assert.equal(await link.getAttribute('href'), `${new URL(target.base).pathname}/${section}`);
      await link.click();
      await page.waitForURL(`${target.base}/${section}`);
      await page.locator(`.workspace-nav-link[aria-current="page"][href$="/${section}"]`).waitFor();
      await page.waitForLoadState('networkidle');
    }
  }
  page.off('request', recordNavigationWrite);
  assert.deepEqual(navigationWrites, [], 'Summary navigation must remain read-only');

  // Exercise every domain, not just whichever tab happens to be selected after
  // CRUD tests. Data and mutations remain confined to the disposable database.
  const workspaces = [
    { path: `/servers/${server.id}`, header: '.server-details-header h1', name: server.name,
      sections: ['overview', 'connection', 'services', 'routing', 'deployments', 'settings'] },
    { path: `/vpn-accounts/${account.id}`, header: '#vpn-account-workspace-title', name: 'Workspace persisted name',
      sections: ['overview', 'access', 'routing', 'protocols', 'traffic', 'settings'] },
    { path: `/routing-profiles/${profile.id}`, header: '.routing-workspace-header h2', name: 'Persisted routing profile',
      sections: ['overview', 'rules', 'settings'] },
  ];
  let layoutChecks = 0;
  async function checkLayout(label) {
    await page.waitForLoadState('networkidle');
    await page.evaluate(async () => {
      await document.fonts.ready;
      await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    });
    const dimensions = await page.evaluate(() => ({
      viewport: innerWidth, document: document.documentElement.scrollWidth,
      body: document.body.scrollWidth,
    }));
    assert.ok(dimensions.document <= dimensions.viewport + 1 && dimensions.body <= dimensions.viewport + 1,
      `${label}: horizontal page overflow ${JSON.stringify(dimensions)}`);
    const active = page.locator('.workspace-nav-link[aria-current="page"]');
    assert.equal(await active.count(), 1, `${label}: one active domain`);
    assert.ok(await active.evaluate(element => {
      const link = element.getBoundingClientRect();
      const nav = element.closest('nav').getBoundingClientRect();
      return link.left >= Math.max(nav.left, 0) - 1 && link.right <= Math.min(nav.right, innerWidth) + 1;
    }), `${label}: active domain is visible in navigation strip`);
    layoutChecks++;
  }
  for (const width of [390, 1440]) {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 1000 });
    for (const theme of ['dark', 'light']) {
      for (const target of workspaces) {
        await page.goto(`${origin}${target.path}/overview`);
        await page.locator(target.header).getByText(target.name, { exact: true }).waitFor();
        await page.evaluate(value => { document.documentElement.dataset.theme = value; }, theme);
        for (const section of target.sections) {
          await page.locator(`.workspace-nav-link[href="${target.path}/${section}"]`).click();
          await page.waitForURL(`${origin}${target.path}/${section}`);
          await page.locator(`.workspace-nav-link[aria-current="page"][href="${target.path}/${section}"]`).waitFor();
          assert.equal(await page.locator(target.header).innerText(), target.name);
          await checkLayout(`${width}/${theme}${target.path}/${section}`);
        }
        if (target.path.startsWith('/routing-profiles/')) {
          await page.locator('.workspace-nav-link[href$="/rules"]').click();
          await page.locator('.routing-rules-panel').getByRole('button', { name: 'Add routing rule', exact: true }).click();
          await page.locator('.routing-rule-form input').first().waitFor();
          await checkLayout(`${width}/${theme}/routing-editor`);
          await page.locator('.routing-rule-form summary').click();
          await page.locator('.routing-rule-form textarea').last().waitFor();
          await checkLayout(`${width}/${theme}/routing-editor-advanced`);
          await page.locator('.routing-rule-form').getByRole('button', { name: 'Cancel', exact: true }).click();
          await page.locator('.routing-rule-form').waitFor({ state: 'hidden' });
        }
      }
    }
  }
  assert.equal(layoutChecks, 68, '60 domain layouts plus 8 rule editor layouts');
  assert.deepEqual(errors, []);
  console.log(`PASS: Manager CRUD, account/traffic draft isolation and retry, routing matchers, 8 read-only summary links, and ${layoutChecks} workspace/editor layouts (390/1440px, dark/light)`);
} finally {
  await browser?.close();
  await vite?.close();
  manager.kill('SIGTERM');
}
