# Origo

Origo is a Go MCP server for source-first web retrieval. It is intended as a complementary tool for agents when their built-in web access cannot retrieve a page. It does **not** provide web search.

**Status: infrastructure scaffold only.** The MCP endpoint is live-ready, but no scraping tools are registered yet. The planned model-facing surface is exactly two tools, `read_link` and `map_site`, pending design review. Firecrawl, direct Markdown, and GitHub-native retrieval are future implementations, not active features.

## Endpoints

| Surface | URL | Vercel project root |
| --- | --- | --- |
| MCP API | `https://api.origo.ashray.xyz/mcp?key=<key>` | Repository root |
| ZueDocs | `https://origo.ashray.xyz` | `docs/` |

The two surfaces are deployed as independent Vercel projects connected to the same GitHub repository. Only pushes to `main` deploy automatically; there is no CLI, npm package, release workflow, or tagged release process.

## Authentication

Set `ORIGO_API_KEY` as a secret in the API Vercel project's environment. Access to `/mcp` requires exactly one matching `key` query parameter; the server fails closed if its key is unset. Use a long random key, and do not commit it.

Query-string credentials can appear in platform access logs and copied URLs. Treat MCP URLs as secrets, avoid redirects, and never put an authenticated URL into analytics, browser history, documentation, or support logs.

The endpoint implements stateless MCP Streamable HTTP using the official Go SDK and currently returns an empty tool list. A successful handshake only means transport and auth work, **not** that website access has been implemented.

## Development

Requirements: Go 1.26+, Bun (for ZueDocs), Vercel CLI (for deploying).

```sh
make check
make docs-install
make docs-check
make docs-build
```

Run the API locally with `vercel dev` after linking the API Vercel project. Run `make docs-dev` for the docs preview. The server entrypoint is `api/mcp.go`; auth and MCP wiring live in `internal/server`.

## Project layout

```text
api/mcp.go                  Vercel Go Function entrypoint
internal/server/            MCP transport, authentication, tests
docs/                       Astro/ZueDocs project, independently deployed
vercel.json                 MCP route rewrite and API deployment settings
```

## Next phase (discussion first)

Research and agree on WebCTX's optimized Markdown/GitHub retrieval, Firecrawl Scrape and Map, and fallback approaches before implementing the two tools. See the [architecture notes](https://origo.ashray.xyz/docs/architecture).

Licensed under Apache-2.0.
