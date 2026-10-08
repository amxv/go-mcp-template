---
title: Docs site maintenance
description: Run, edit, validate, and deploy the ZueDocs-powered documentation site embedded in this Go CLI template.
order: 5
category: Reference
summary: Developer notes for maintaining the Astro docs app alongside the Go CLI.
---

## Local development

Install dependencies and start Astro:

```bash
make docs-install
make docs-dev
```

Astro serves the docs locally, usually at `http://localhost:4321`.

## Files to edit

The docs site is intentionally small:

```bash
docs/package.json              # isolated docs dependencies and scripts
docs/astro.config.mjs          # Astro config
docs/src/data/docs.ts          # site name, repo URL, nav, categories
docs/src/pages/index.astro     # landing page
docs/src/pages/docs/index.astro # docs index
docs/src/pages/docs/[...slug].astro # article route
docs/src/content/docs/*.md     # documentation pages
docs/src/styles/global.css     # shared ZueDocs import
docs/vercel.json               # deployment config
```

For most updates, edit markdown in `docs/src/content/docs` first.

## ZueDocs package usage

This site imports the shared docs shell from `zuedocs`:

```astro
import BaseLayout from "zuedocs/layouts/BaseLayout.astro";
import DocsPageLayout from "zuedocs/layouts/DocsPageLayout.astro";
```

Local repos should keep their own docs content and `docs/src/data/docs.ts`, while shared shell behavior belongs in the `zuedocs` package.

## Validate changes

Run:

```bash
make docs-check
make docs-build
```

Run these commands serially. Do not run Astro check and build concurrently in the same repo.

## Deployment

The docs site builds to static output in `docs/dist`:

```bash
make docs-build
```

For Vercel, use `docs` as the Root Directory. The committed `docs/vercel.json`
installs and builds the isolated docs workspace and serves `docs/dist` without
sharing the Go CLI's root `dist/` directory.
