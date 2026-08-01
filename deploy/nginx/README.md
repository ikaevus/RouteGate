# RouteGate nginx deployment

`routegate.conf.example` is the current production-like reverse-proxy and static-frontend template used by the RG-89C clean-host validation runbook.

It provides:

- static Admin UI files from `/var/www/routegate`;
- SPA fallback to `index.html`;
- `/api/*` proxying to RouteGate Manager on `127.0.0.1:8080`;
- an HTTP entry point suitable for subsequent Certbot nginx integration.

Before installing the template:

1. replace `routegate.example.com` with the real hostname;
2. verify the DNS record points to the target VPS;
3. verify ports 80 and 443 are reachable;
4. run `nginx -t` before reloading nginx.

The template intentionally does not embed certificate paths. HTTPS is added by the deployment operator or, later, by the planned Clean VPS Installer after successful HTTP and DNS preflight checks.
