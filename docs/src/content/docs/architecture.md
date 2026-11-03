---
title: Architecture
description: Stateless MCP, optional OAuth, Redis state, Vercel routing, and tool boundaries.
order: 2
category: Design
summary: How the template separates tool logic, authentication, and serverless transport.
---

## Shared MCP transport

The official `github.com/modelcontextprotocol/go-sdk/mcp` implementation uses **stateless Streamable HTTP** with JSON responses. Every request creates its own SDK server; Vercel instances do not share MCP sessions. Tools are registered in `pkg/server/tools.go`.

With **key mode**, Vercel routes `/mcp?key=...` to `api/mcp.go`, which calls `pkg/server/server.go`. It uses a constant-time comparison against `MCP_API_KEY` and fails closed when unset. No persistent store is needed.

With **OAuth mode**, Vercel's native Go-server framework runs `main.go`, exposing `/mcp`, OAuth routes, and the well-known discovery paths. It uses the same MCP tools but enforces a verified bearer credential for every MCP request. Key-mode secrets never authorize OAuth requests. There is no fallback if an OAuth variable or Redis is unavailable.

## OAuth flow

```text
ChatGPT / OAuth MCP client
  → POST /mcp without credentials
  ← 401 + WWW-Authenticate: Bearer resource_metadata=...
  → GET /.well-known/oauth-protected-resource/mcp
  → GET /.well-known/oauth-authorization-server
  → GET /oauth/authorize (code_challenge + resource + state)
  ↔ owner sign-in + explicit client consent (browser)
  ← callback with authorization code + state + iss
  → POST /oauth/token (code_verifier + resource)
  ← access and rotating refresh tokens
  → POST /mcp (Authorization: Bearer ...)
```

OAuth implements the authorization-code flow with **PKCE S256** and refresh tokens. It requires an exact `resource` audience and an allowlisted client ID/redirect URI. ChatGPT uses its predefined [client metadata document](https://chatgpt.com/oauth/client.json); the server fetches **only that URL**, with bounded requests, public DNS answers and no redirects, before accepting a new ChatGPT authorization. Arbitrary client URLs and dynamic registration are not supported.

The authorization page renders sign-in **on the original `/oauth/authorize` URL** rather than redirecting preflight requests to a bare login URL; this preserves the OAuth request when ChatGPT opens the user's browser separately. Signed-in owners see the client and requested scope before consenting.

Access tokens are 256-bit opaque bearer values, stored as hashed Redis keys, with a **five-minute lifetime**. Authorization codes are one-time, PKCE-bound, and expire after **five minutes**. Refresh tokens rotate atomically; replay revokes the grant. Each grant has a **30-day maximum lifetime**. Browser sessions have `HttpOnly`, `Secure`, `SameSite=Lax` cookies, CSRF checks, and a 30-minute expiry. Revocation is checked on every request, not just at token renewal.

Redis REST is required in OAuth mode to coordinate codes, sessions, grants, refresh tokens, and revocations across instances. The only long-term owner credential is `MCP_OAUTH_OWNER_SECRET`, used to sign in on the server's own page, never transmitted to the MCP client. A shared login attempt limiter applies across instances. Back up and protect Redis state; grant records and replay detection rely on it.

## Security and limits

- This is a **single-owner** starter: OAuth proves authorization to the whole tool surface with `mcp:access`. It is not user delegation or tenant isolation. Add application-level authorization and per-tool approvals for sensitive applications.
- Owner sign-in uses a high-entropy secret rather than Angelos's passkeys. Access tokens are opaque and Redis-backed rather than Angelos's ES256 JWTs. The discovery, PKCE, consent, client metadata, short-lived credentials, and rotation patterns are adapted from Angelos.
- The issuer is a fixed lowercase public HTTPS origin from deployment configuration. Host and forwarded headers cannot set the issuer, change redirect targets, or turn HTTP into a trusted public origin.
- All token storage and network failures are fail-closed; request credentials are never included in logs or response bodies. `?key=` is accepted **only in key mode**.

## Adding a tool

Use `mcp.AddTool` with a typed Go input struct. The SDK generates JSON Schema and validates inputs. `echo_text` demonstrates returning `mcp.TextContent`; `add_numbers` demonstrates structured output.

Keep application code in a public package such as `pkg/server` so Vercel's synthetic Go Function package can import it in key mode. For every tool, retain `mcp.ToolAnnotations.ReadOnlyHint: true` and omit `OpenWorldHint`. Remove example handlers when implementing a real application.
