# Frontend i18n checker

RouteGate is an English-first product with full Russian localization. User-facing frontend text must go through the shared i18n dictionaries instead of being hardcoded in React components.

RG-85 adds a lightweight static QA check for likely hardcoded UI strings in `frontend/src`.

## Run

From the frontend directory:

```bash
npm run i18n:check
```

From the repository root:

```bash
make frontend-i18n-check
make check
```

## What it scans

The checker scans TypeScript and TSX files under `frontend/src`.

It reports suspicious static text in:

- JSX text nodes.
- Static values of common user-facing attributes:
  - `placeholder`
  - `title`
  - `aria-label`
  - `alt`
  - `label`

Example that should be reported:

```tsx
<button>Create server</button>
<input placeholder="Search servers" />
```

Preferred pattern:

```tsx
<button>{t('servers.createServer')}</button>
<input placeholder={t('servers.searchPlaceholder')} />
```

## Intentional exceptions

The checker intentionally allows technical and product terms that are normal in RouteGate UI, such as `RouteGate`, `VPN`, `VLESS`, `Reality`, `API`, `UUID`, `URL`, `Admin UI`, `Manager API`, protocol names, paths, status-like tokens, icons, and email addresses.

If a new technical term is legitimate and repeatedly appears in UI, extend the allowlist in:

```text
frontend/scripts/i18n-check.mjs
```

For rare one-off cases, place `i18n-check-ignore` on the same line or the previous line. Prefer extending the allowlist only for stable product or protocol terms.

## Scope

This checker is intentionally small. It is a QA guardrail, not a full JSX parser. It should catch likely hardcoded user-facing strings while avoiding noisy reports for code identifiers, routes, CSS classes, test IDs, icons, and technical RouteGate vocabulary.
