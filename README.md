# Go MCP Template

A minimal, production-ready **Go MCP server template** for Vercel. It uses the official Model Context Protocol Go SDK, stateless Streamable HTTP, two working sample tools, and an optional ZueDocs site.

**Choose how clients connect:** keep the existing private `?key=` URL, or enable a first-party **OAuth 2.1 authorization flow** compatible with ChatGPT's MCP client metadata document. OAuth uses owner sign-in, PKCE, explicit consent, rotating refresh tokens, and durable revocation.

This is infrastructure, not a published CLI or npm package. Both modes use the same tools in `pkg/server/tools.go`.

## Start a new project

1. [Create a repository from this template](https://github.com/amxv/go-mcp-template/generate) and clone it.
2. Replace `github.com/amxv/go-mcp-template` in `go.mod` and Go imports (`api/mcp.go`, `main.go`, and `pkg/server/server.go`) with your new module path.
3. Rename the MCP implementation in `pkg/server/tools.go` and replace `echo_text` and `add_numbers` with your actual tools.
4. Select **key** or **OAuth** authentication below, then deploy the repository root to Vercel. Deploy `docs/` as a separate project if you want a documentation site.

### Option A: URL key (default, no database)

Leave `vercel.json` unchanged. Create a Vercel project with Root Directory `/` and Framework Preset **Other**. Set `MCP_API_KEY` to a long random value in Production (you may explicitly set `MCP_AUTH_MODE=key`). Connect your MCP client to:

```text
https://your-api-domain.example/mcp?key=<your-secret>
```

Missing key configuration fails closed. URL query parameters can leak through browser history, logs, or copied URLs; **treat the entire URL as a credential**.

### Option B: OAuth (ChatGPT, interactive sign-in)

OAuth needs public well-known discovery routes that Vercel cannot rewrite to a Go Function. It therefore runs through Vercel's native **Go server** framework, like Angelos, instead of the default Go Function profile.

```sh
cp vercel.oauth.json vercel.json
```

Deploy the repository root with the **Go** framework and configure these Production environment variables:

| Variable | Value |
| --- | --- |
| `MCP_AUTH_MODE` | `oauth` |
| `MCP_OAUTH_ISSUER` | Canonical public HTTPS origin, e.g. `https://api.your-domain.example` (no path or trailing slash) |
| `MCP_OAUTH_OWNER_SECRET` | A unique, random secret of at least 32 characters; **used only on the owner's sign-in page** |
| `MCP_OAUTH_REDIS_REST_URL` | HTTPS Redis REST endpoint (e.g. Upstash) |
| `MCP_OAUTH_REDIS_REST_TOKEN` | Redis REST authentication token |
| `MCP_OAUTH_CLIENTS_JSON` | Optional JSON allowlist of additional clients; ChatGPT is already built in |

Generate a suitable owner secret locally with `openssl rand -base64 48`. Use a **permanent custom domain** matching `MCP_OAUTH_ISSUER` before connecting clients; a change of issuer invalidates existing OAuth grants and requires reconnection. Redis must be durable and available to all instances; do not enable an eviction policy that removes OAuth state.

In ChatGPT, add an MCP app using this URL, **without** `?key=`:

```text
https://api.your-domain.example/mcp
```

ChatGPT discovers OAuth and opens the owner sign-in page. Enter `MCP_OAUTH_OWNER_SECRET` **there, not into ChatGPT or the MCP URL**; review the client and approve the connection. Your MCP server issues short-lived bearer tokens with rotating refresh tokens and a 30-day maximum grant lifetime. Manage and revoke connections at `https://api.your-domain.example/oauth/grants`.

For another public OAuth client, allowlist its exact client ID and HTTPS callback URLs:

```json
[
  {
    "client_id": "my-client",
    "client_name": "My MCP Client",
    "redirect_uris": ["https://client.example.com/oauth/callback"]
  }
]
```

Set the JSON above as `MCP_OAUTH_CLIENTS_JSON`. ChatGPT's fixed `https://chatgpt.com/oauth/client.json` ID and callback are supported without custom registration. OAuth discovery is read-only public; the MCP endpoint and grants are private. **Do not use the key URL when OAuth is enabled.** An invalid or partially configured OAuth deployment fails closed, even when `MCP_API_KEY` is set.

**Scope of this starter:** single-owner access to the entire tool surface (`mcp:access`); no multi-user identity, tenant isolation, passkeys, or fine-grained tool scopes. Unlike Angelos, the starter uses a high-entropy owner secret for browser sign-in and opaque Redis-backed access tokens to keep the example small. For production tools that act on other users' data or make consequential changes, implement per-user authorization and appropriate approval boundaries before deploying.

See [OAuth architecture](docs/src/content/docs/architecture.md) and [deployment guide](docs/src/content/docs/deployment.md) for protocol details and operational checks.

## Included

- `api/mcp.go`: default single Vercel Go Function entrypoint for key mode.
- `main.go` and `vercel.oauth.json`: native Go server profile with public OAuth discovery routes.
- `pkg/server/`: stateless MCP transport, auth selector, and sample tools.
- `pkg/oauth/`: OAuth endpoints, explicit client validation, PKCE, owner sign-in/consent, and Redis-backed tokens/grants.
- `pkg/server/server_test.go` and `pkg/oauth/server_test.go`: HTTP JSON-RPC and OAuth flow/security tests.
- `docs/`: optional independently deployed Astro + ZueDocs site.

## Development

Requirements: Go 1.26+ and Bun (only for the docs).

```sh
make check
make docs-install
make docs-check
make docs-build
```

Test key mode locally with `vercel dev` and `MCP_API_KEY` configured. For OAuth, set the OAuth variables in a **local ignored environment file** and run `go run .`; this serves OAuth and MCP on port 8080 by default. Production OAuth requires a valid external HTTPS origin because callback URLs and client metadata must match it exactly. Unit and integration tests use an in-memory test store and don't require credentials or Redis.

Sample handlers use `mcp.AddTool` and demonstrate text (`echo_text`) and structured JSON (`add_numbers`) results. Each tool has `ReadOnlyHint: true`. Remove these examples when building a real service.

## Docs site

The bundled ZueDocs application lives under `docs/`. Connect it to a **separate** Vercel project with Root Directory `docs`, Framework Preset Astro, and its own domain. It must not receive API or OAuth credentials.

Licensed under Apache-2.0.
