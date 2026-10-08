---
title: Getting started
description: Connect to Origo's Go MCP endpoint and understand its current scaffold-only status.
order: 1
category: Start
summary: MCP endpoint, API-key configuration, and development commands.
---

Origo is not yet a scraper. The deployed server handles MCP transport and authentication, but advertises **zero tools** until the `read_link` and `map_site` design has been approved and implemented.

## MCP endpoint

```text
https://api.origo.ashray.xyz/mcp?key=<your-private-key>
```

The server is stateless, uses MCP Streamable HTTP, and requires exactly one `key` query parameter matching `ORIGO_API_KEY` in the API deployment.

Keep the full URL private. Query-string keys can be captured by access logs and copied URL histories. Never commit real keys or paste them into public documentation.

## Developer setup

```bash
git clone git@github.com:amxv/origo.git
cd origo
make check
make docs-install
make docs-check
make docs-build
```

For local API testing, link the API Vercel project and run `vercel dev`. For docs, run `make docs-dev` from the repository root.

No npm package, executable shim, CLI binary, release tag, or GitHub Release is needed.
