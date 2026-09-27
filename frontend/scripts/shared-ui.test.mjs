import assert from 'node:assert/strict';
import { after, test } from 'node:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { createServer } from 'vite';

const vite = await createServer({ server: { middlewareMode: true }, appType: 'custom',
  optimizeDeps: { noDiscovery: true, include: [] } });
after(() => vite.close());
const { Notice } = await vite.ssrLoadModule('/src/shared/ui/Notice.tsx');
const { StatusBadge } = await vite.ssrLoadModule('/src/shared/ui/StatusBadge.tsx');
const { translateStatus } = await vite.ssrLoadModule('/src/shared/i18n/i18n.ts');
const render = (props = {}, children = 'Feedback') => renderToStaticMarkup(createElement(Notice, props, children));

for (const tone of ['info', 'success', 'warning', 'error']) {
  test(`${tone} retains existing styling without announcing static content`, () => {
    const html = render({ tone });
    const expectedClass = tone === 'info' ? 'form-message' : `form-message form-message-${tone}`;
    assert.equal(html, `<div class="${expectedClass}">Feedback</div>`);
  });
}

for (const [announcement, role] of [['polite', 'status'], ['assertive', 'alert']]) {
  test(`${announcement} feedback uses ${role} semantics`, () => {
    const html = render({ announcement, tone: 'error' });
    assert.ok(html.includes(`role="${role}"`));
    assert.ok(html.includes('aria-atomic="true"'));
    assert.ok(!html.includes('aria-live='), 'Use the implicit live role, without redundant announcements');
  });
}

test('defaults to informational styling and retains identifiers, classes and child actions', () => {
  const html = render({ id: 'next-step', className: 'context-hint' }, [
    'Next step ', createElement('a', { key: 'link', href: '/servers' }, 'Select server'),
  ]);
  assert.equal(html, '<div id="next-step" class="form-message context-hint">Next step <a href="/servers">Select server</a></div>');
});

test('renders text safely', () => {
  assert.ok(render({}, '<script>alert(1)</script>').includes('&lt;script&gt;'));
});

const renderStatus = (props = {}) => renderToStaticMarkup(createElement(StatusBadge, props));

for (const status of [undefined, null, '', '   ']) {
  test(`missing status (${JSON.stringify(status)}) is explicitly unknown`, () => {
    const html = renderStatus({ status });
    assert.ok(html.includes('class="badge badge-unknown"'));
    assert.ok(html.includes(translateStatus('unknown')));
    assert.ok(!html.includes('badge-online') && !html.includes('badge-active'));
  });
}

for (const status of ['default', 'custom', 'enabled', 'disabled', 'pending', 'failed']) {
  test(`${status} retains its distinct style and translated text`, () => {
    const html = renderStatus({ status });
    assert.ok(html.includes(`class="badge badge-${status}"`));
    assert.ok(html.includes(translateStatus(status)));
    assert.ok(!html.includes('role=') && !html.includes('aria-live='), 'A badge is not a live notification');
  });
}

test('normalizes case/whitespace and safely maps backend status tokens to classes', () => {
  assert.equal(renderStatus({ status: ' ONLINE ' }), renderStatus({ status: 'online' }));
  assert.ok(renderStatus({ status: 'commit_confirmed' }).includes('badge-commit-confirmed'));
  const future = renderStatus({ status: 'future_state' });
  assert.ok(future.includes('badge-future-state') && future.includes('future_state'));
});

test('preserves domain-specific labels and does not infer connectivity', () => {
  const html = renderStatus({ status: 'offline', label: 'Agent unavailable' });
  assert.ok(html.includes('badge-offline') && html.includes('Agent unavailable'));
  assert.ok(!html.includes('badge-online'));
});

test('positive labels stay calm; arbitrary text is escaped', () => {
  assert.ok(renderStatus({ status: 'active', label: 'ACTIVE' }).includes('>Active</span>'));
  assert.ok(renderStatus({ status: 'unknown', label: '<script>unsafe</script>' }).includes('&lt;script&gt;'));
});
