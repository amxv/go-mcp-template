---
title: Documentation site
description: Maintain the Origo Astro/ZueDocs documentation app.
order: 4
category: Operations
summary: Docs content, ZueDocs integration, and local checks.
---

Origo uses the ZueDocs-powered Astro site inherited from the Go bootstrap template. It lives entirely in `docs/` and deploys independently of the Go MCP API.

```bash
make docs-install
make docs-dev
```

Edit articles in `docs/src/content/docs/`, the navigation and site identity in `docs/src/data/docs.ts`, and the landing page in `docs/src/pages/index.astro`.

Before a docs deployment, run `make docs-check` and `make docs-build` **sequentially**. Pushing `main` triggers the Vercel docs project automatically.
