---
title: Deployment
description: How the Go API and Astro/ZueDocs site are deployed separately through Vercel.
order: 3
category: Operations
summary: Go Function and docs deployments, DNS, and API-key handling.
---

## API project

Root Directory: repository root. Framework: Other / no frontend framework. Vercel's native Go Functions runtime picks up `api/mcp.go`. The root `vercel.json` rewrites `/mcp` to `/api/mcp`.

Set the server-only secret `ORIGO_API_KEY` in the Vercel API project's Production environment before exposing the authenticated URL. Never store it in GitHub, docs, or source control.

## Docs project

Root Directory: `docs/`. Framework: Astro. `docs/vercel.json` runs `bun install --frozen-lockfile` followed by `bun run build`, publishing `dist/`. No API secrets belong here.

Both projects are linked to `amxv/origo`. Pushes to `main` redeploy automatically. There is no npm release or GitHub Actions release workflow.

## Domains

The docs use `origo.ashray.xyz`. The API uses `api.origo.ashray.xyz`. DNS is managed under the `ashray.xyz` Cloudflare zone; the domain records should remain DNS-only for Vercel ownership verification.

## Local validation

```bash
make check
make docs-install
make docs-check
make docs-build
```

Run docs check and build sequentially.
