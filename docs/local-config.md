# Local Configuration Boundary

This repository is safe to publish only when real deployment files stay local.

## Tracked Examples

These files are committed and should contain placeholders only:

- `.env.example`
- `config/status-board.example.yaml`
- `deploy/gatus/config.example.yaml`
- `deploy/nginx/status-board.example.conf`

Use documentation-safe values such as `status.example.com`, `203.0.113.10`,
and `100.64.10.x` in examples.
The Nginx example may show an HTTPS server block, but it must reference
placeholder certificate paths only. Real certificates and htpasswd files live
on the deployment host and are never committed.

## Ignored Real Files

These files are intentionally ignored by git and may contain private topology,
domains, IPs, tokens, hostnames, and deployment-specific paths:

- `.env`
- `.env.production`
- `config/status-board.yaml`
- `deploy/gatus/config.yaml`
- `deploy/nginx/status.local.conf`

Create missing local files with:

```bash
make init-env
make init-prod-env
make init-config
```

Then edit the generated real files for the target deployment.

For sh-core HTTPS, keep the real domain and IP in `.env.production`, then run:

```bash
make install-status-https-sh-core
```

That command uses the ignored real environment file, installs the certificate
on sh-core, backs up the live Nginx status-board config, and reloads Nginx only
after `nginx -t` succeeds.

`make deploy-sh-core` may update only these non-sensitive public/deployment
metadata keys in the remote `.env.production`: `STATUS_BOARD_PUBLIC_DOMAIN`,
`STATUS_BOARD_PUBLIC_IP`, `STATUS_BOARD_TAILNET_URL`,
`STATUS_BOARD_BUILD_COMMIT`, and `STATUS_BOARD_BUILD_TIME`. Do not broaden that
sync path to copy tokens or full env files.

## Publishing Rule

Before pushing a public repo, run a sensitive-value scan that excludes generated
or ignored runtime directories:

```bash
rg -n --hidden \
  --glob '!frontend/node_modules/**' \
  --glob '!frontend/dist/**' \
  --glob '!dist/**' \
  --glob '!data/**' \
  --glob '!work/**' \
  'ghp_|github_pat_|sk-|BEGIN .*PRIVATE KEY|PASSWORD|TOKEN|API[_-]?KEY|YOUR_PUBLIC_IP|YOUR_TAILNET_SUBNET|YOUR_DOMAIN|/Users/you'
```

If this scan reports a real secret or private topology value in a tracked file,
move that value into an ignored real file and replace the tracked value with an
example placeholder.
