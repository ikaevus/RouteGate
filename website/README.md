# RouteGate Website

This directory contains the source code for the public RouteGate website at `routegate.org`.

The website is part of the project repository for maintainability and coordinated releases. It is **not** required to install, deploy, or operate RouteGate Manager, Agent, or VPN Core.

## Stack

- Vite
- React
- TypeScript
- Static Git-managed content

## Development

```bash
cd website
npm install
npm run dev
```

## Production build

```bash
npm ci
npm run build
```

The static build is written to `website/dist/`.

## GitHub Pages deployment

The repository includes `.github/workflows/pages.yml`. It builds this directory
and deploys `website/dist/` through GitHub Pages when website-related changes
reach `main`. The workflow can also be started manually.

The production URL is `https://routegate.org/`. Vite intentionally uses the root
base path (`/`) for this custom domain. `public/CNAME` is copied into every
production build.

Repository owner steps:

1. Open **Settings → Pages** in `ikaevus/RouteGate`.
2. Under **Build and deployment**, select **GitHub Actions** as the source.
3. Merge the website deployment PR or manually run the `RouteGate Website`
   workflow.
4. Confirm that the Pages environment reports a successful deployment.
5. Only after the deployment is available, configure DNS for `routegate.org`
   using the current records documented by GitHub Pages.
6. Add the custom domain in **Settings → Pages**, wait for DNS verification, and
   enable **Enforce HTTPS** when GitHub makes the option available.

DNS is deliberately not managed by this repository. Record values can change,
so use GitHub's current custom-domain documentation when making the manual DNS
change. For an apex domain, GitHub currently documents `A`/`AAAA` records; an
optional `www` host normally uses a `CNAME` to the GitHub Pages host. Do not
remove existing mail or verification records while changing website DNS.

Before switching DNS, verify:

```bash
curl -I https://ikaevus.github.io/RouteGate/
dig routegate.org
dig www.routegate.org
```

After switching DNS, verify:

```bash
curl -I https://routegate.org/
curl -I https://routegate.org/robots.txt
curl -I https://routegate.org/sitemap.xml
```

## Map data

The dashboard preview uses a static, vendored SVG derived from Natural Earth
Admin 0 Countries at 1:110m scale. Natural Earth data is public domain. See
`public/map-data-license.txt` for source details. The site makes no runtime map
requests.

## Search and social metadata

The static entry point includes canonical, Open Graph, Twitter Card, and
structured-data metadata. `public/og-image.svg` is the editable source for
`public/og-image.png`; update both when changing the social preview.

## Content and brand boundaries

- Public user-facing copy belongs in locale content files.
- Official RouteGate brand assets originate from `/brand`.
- Do not add secrets, production tokens, private analytics credentials, or real infrastructure data.
- The website presents RouteGate but does not define core architecture, commercial boundaries, or roadmap sequencing.
- Do not add production-readiness claims unless the product status explicitly supports them.
