---
title: Quickstart
description: Clone the Go MCP template and choose URL-key or OAuth authentication.
order: 1
category: Start
summary: Choose an authentication mode, run sample tools, and deploy to Vercel.
---

## Create and clone

Create a repository using [Go MCP Template](https://github.com/amxv/go-mcp-template/generate) and clone it. Replace `github.com/amxv/go-mcp-template` in `go.mod` and Go imports with your new module name.

```bash
make check
```

The official Go MCP SDK exposes two runnable example tools in `pkg/server/tools.go`:

- `echo_text`: `{ "text": "hello" }` returns text `hello`.
- `add_numbers`: `{ "a": 2, "b": 3 }` returns `{ "sum": 5 }` as structured JSON.

## Choose authentication

### URL key — default

Use `vercel.json` unchanged, deploy the root project with Framework Preset **Other**, and configure a strong Production `MCP_API_KEY`. Optional: set `MCP_AUTH_MODE=key` explicitly.

```text
https://your-domain.example/mcp?key=<your-private-key>
```

This mode needs no database. An absent secret denies requests; keep the complete URL private because query strings can appear in logs or copied links.

### OAuth — ChatGPT and other OAuth clients

Choose the native Go-server deployment profile **before deploying**:

```bash
cp vercel.oauth.json vercel.json
```

In Vercel, choose the **Go** framework (not Other) and set:

| Production variable | Description |
| --- | --- |
| `MCP_AUTH_MODE` | `oauth` |
| `MCP_OAUTH_ISSUER` | Your stable public HTTPS origin (no trailing slash), e.g. `https://api.example.com` |
| `MCP_OAUTH_OWNER_SECRET` | A strong random secret (generate with `openssl rand -base64 48`) |
| `MCP_OAUTH_REDIS_REST_URL` | Upstash-compatible HTTPS Redis REST URL |
| `MCP_OAUTH_REDIS_REST_TOKEN` | Redis REST token |

An optional `MCP_OAUTH_CLIENTS_JSON` allowlists additional clients. ChatGPT is supported by default. Connect your ChatGPT MCP app to `https://api.example.com/mcp`, **without a query key**. A browser opens the owner sign-in and consent screens; enter the owner secret **only on that page**. After approving, ChatGPT uses OAuth access and refresh tokens.

Manage connected grants at `https://api.example.com/oauth/grants`. This starter supports a **single owner** and one broad `mcp:access` scope; it does not implement multi-user sign-in or passkeys. See the [architecture](/docs/architecture) and [deployment](/docs/deployment) guides before deploying sensitive tools.

## Local documentation

```bash
make docs-install
make docs-dev
```

The optional ZueDocs site lives entirely under `docs/` and deploys as a separate Vercel project. Do not provide it with API/OAuth secrets.
