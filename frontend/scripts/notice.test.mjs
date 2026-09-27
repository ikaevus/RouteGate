import assert from 'node:assert/strict';
import { after, test } from 'node:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { createServer } from 'vite';

const vite = await createServer({ server: { middlewareMode: true }, appType: 'custom',
  optimizeDeps: { noDiscovery: true, include: [] } });
after(() => vite.close());
const { Notice } = await vite.ssrLoadModule('/src/shared/ui/Notice.tsx');
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
