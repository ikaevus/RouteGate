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
npm run build
```

The static build is written to `website/dist/`.

## Content and brand boundaries

- Public user-facing copy belongs in locale content files.
- Official RouteGate brand assets originate from `/brand`.
- Do not add secrets, production tokens, private analytics credentials, or real infrastructure data.
- The website presents RouteGate but does not define core architecture, commercial boundaries, or roadmap sequencing.
