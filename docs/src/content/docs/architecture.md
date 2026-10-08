---
title: Architecture
description: How Origo separates the MCP boundary from future retrieval adapters.
order: 2
category: Design
summary: Two planned tools, no search, with source-first retrieval principles.
---

## Infrastructure now

```text
ChatGPT MCP client
      |
      | POST /mcp?key=...
      v
Vercel Go Function  (api/mcp.go)
      |
      v
pkg/server        (auth + stateless MCP Streamable HTTP)
      |
      v
tools/list         (currently empty)
```

The code uses the official Go MCP SDK with stateless Streamable HTTP. It does not require a Redis-backed session store.

## Future tool boundary

Origo is deliberately limited to exactly two model-facing tools:

- `read_link`: return useful, source-grounded Markdown from a URL.
- `map_site`: return discoverable URLs and metadata from a website.

Website search is excluded. Retrieval implementations will be chosen after reviewing WebCTX's GitHub-native paths, `.md` paths, and current Firecrawl APIs.

Internal adapters may use direct Markdown, GitHub APIs, Firecrawl scrape and map, or a stronger browser-based fallback. These adapters are **not** additional model-facing MCP tools.

## Deployment boundaries

The same private GitHub repository deploys two Vercel projects:

| Project | Root | Host |
| --- | --- | --- |
| Origo API | `/` | `api.origo.ashray.xyz` |
| Origo docs | `/docs` | `origo.ashray.xyz` |

Each project auto-deploys commits to `main`. There is no CLI distribution pipeline.
