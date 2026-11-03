---
title: Deployment
description: Deploy Go MCP to Vercel with a private URL key or first-party OAuth.
order: 3
category: Operations
summary: Select the Vercel runtime, configure secrets, verify OAuth routes, and deploy docs separately.
---

## Key mode: Go Function

Deploy the repository root to Vercel with Framework Preset **Other**. Leave `vercel.json` unchanged; it rewrites `/mcp` to `api/mcp.go` and deploys from `main`. Set a long random `MCP_API_KEY` secret in the Production environment. `MCP_AUTH_MODE` may be unset or `key`.

```text
https://api.your-domain.example/mcp?key=<your-secret>
```

An unauthenticated `/mcp` call must return **401**. Without `MCP_API_KEY`, it returns **503**. Query strings can leak through logs and browser history, so never publish the authenticated URL.

## OAuth mode: native Go server

OAuth requires dynamic discovery routes at `/.well-known/...`. Vercel's conventional rewrites reserve this path. Instead, use the **same Go-server framework pattern as Angelos**, which serves routes natively rather than rewriting them into `api/mcp.go`.

```bash
cp vercel.oauth.json vercel.json
```

Choose the **Go** framework in Vercel, with Root Directory `/`. `main.go` listens on Vercel's `PORT` and handles `/mcp`, `/.well-known/`, and `/oauth/`. Its MCP transport is still stateless; Redis stores **only OAuth state**.

| Variable | Purpose |
| --- | --- |
| `MCP_AUTH_MODE=oauth` | Enable OAuth exclusively |
| `MCP_OAUTH_ISSUER` | Exact public HTTPS origin: lowercase hostname, no path, port, or trailing slash |
| `MCP_OAUTH_OWNER_SECRET` | High-entropy browser sign-in secret, at least 32 characters |
| `MCP_OAUTH_REDIS_REST_URL` | HTTPS Redis REST endpoint, such as Upstash |
| `MCP_OAUTH_REDIS_REST_TOKEN` | Private Redis REST token |
| `MCP_OAUTH_CLIENTS_JSON` | Optional JSON array of predefined `{client_id,client_name,redirect_uris}` entries for clients other than ChatGPT |

Generate a unique owner secret with `openssl rand -base64 48`. Do not reuse `MCP_API_KEY`, include it in an MCP URL, or give it to an assistant. The browser uses it only at `/oauth/login`, on the canonical API domain, to establish an owner session. Set all variables on the **API** project and keep them out of `docs/` and the repository. Set `MCP_OAUTH_ISSUER` to the stable production domain **before** issuing grants; a new hostname makes old clients reconnect. No defaults are inferred from request headers.

### Verify the OAuth deployment

After deployment, verify these endpoints on the **canonical public domain**:

| HTTP request | Expected |
| --- | --- |
| `GET /.well-known/oauth-protected-resource/mcp` | HTTP 200 JSON containing `resource` equal to `<issuer>/mcp` |
| `GET /.well-known/oauth-authorization-server` | HTTP 200 JSON advertising `/oauth/authorize`, `/oauth/token`, `S256`, and `client_id_metadata_document_supported` |
| `POST /mcp` without a bearer token | HTTP 401 with `WWW-Authenticate: Bearer resource_metadata="..."` |
| `GET /oauth/login` | Owner sign-in page |

Then connect ChatGPT to `<issuer>/mcp`, allow the browser sign-in and consent, and test `echo_text` and `add_numbers`. Test reconnection/refresh and check that revoking a grant at `<issuer>/oauth/grants` causes the client to be denied. This repository tests OAuth with an in-memory test store; deployment and live ChatGPT compatibility still require testing on your own custom domain.

OAuth state must persist across serverless instances. Use a Redis REST provider with durable storage, protect its token, and disable policies that silently evict authentication records. The starter supports one owner with broad `mcp:access` scope and a shared login rate limit; it does not implement multi-tenant authorization, passkeys, or external OAuth identity providers.

## Documentation project

Optionally create a second Vercel project from the same repository, Root Directory **`docs`**, Framework Preset **Astro**. `docs/vercel.json` configures the isolated Bun build. The docs project must not receive MCP key, owner secret, or Redis token.

## Custom domains and local checks

Add the API hostname to the API project and use Vercel's DNS instructions for verification. Do not assume docs and API hosts point to the same project. Verify the HTTPS certificate before OAuth login.

```bash
make check
make docs-install
make docs-check
make docs-build
```

Run the last two commands **serially**. OAuth can be exercised locally with `MCP_AUTH_MODE=oauth go run .` and an appropriate test store; the actual browser flow uses a public HTTPS origin and a durable Redis REST service. Key mode works with `vercel dev` and the Go Function directly.
