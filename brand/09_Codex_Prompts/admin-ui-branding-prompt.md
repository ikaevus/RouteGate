# Codex Prompt - Apply RouteGate Brand Identity to Admin UI

Use `/brand` as the source of truth.

Goal:
Apply RouteGate Brand Identity v1.0 to the Admin UI.

Requirements:
- Use the RouteGate controlled-passage logo/symbol from `/brand/02_Logo_Pack`.
- Use Inter as the UI font.
- Use RouteGate colors from `/brand/05_Design_Tokens/tokens.json` or `/brand/05_Design_Tokens/tokens.css`.
- Preserve existing functionality.
- Keep i18n support: no hardcoded user-facing strings if translation infrastructure exists.
- Dark-first enterprise look.
- Avoid shields, locks, hacker neon, matrix effects or childish visuals.
- Update login/auth shell, sidebar/header branding, favicon/app icon and relevant empty states.
- Add tests/build verification where applicable.

Expected result:
Admin UI visually reflects RouteGate Brand Identity v1.0 while preserving the current application architecture.
