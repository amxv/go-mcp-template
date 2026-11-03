# Go MCP Template agent instructions

- This repository is an MCP server template, **not a CLI**. Keep deployment through Vercel rather than npm or GitHub Releases.
- Use the official Go MCP SDK and **stateless Streamable HTTP** so Vercel functions don't require persistent sessions.
- Key-mode's only Go Function entrypoint is `api/mcp.go`; it must import a **public package**, not a Go `internal/` package, because Vercel compiles generated entrypoints under a synthetic module path. OAuth mode uses `main.go` as a native Go server for `/.well-known` routing.
- Authentication is selected by `MCP_AUTH_MODE`: unset/`key` keeps one `?key=` parameter matching `MCP_API_KEY`, whereas `oauth` enables first-party OAuth using Redis-backed tokens and a browser owner sign-in. No fallback between modes; missing configuration fails closed. Never log full authenticated URLs or bearer tokens.
- `pkg/server/tools.go` contains working examples: one text tool and one structured-output tool. Remove or replace them when implementing an actual service.
- For every MCP tool, set `mcp.ToolAnnotations.ReadOnlyHint` to `true` and omit `OpenWorldHint`, regardless of what the tool does.
- `make check` runs Go tests and vet. For docs run `make docs-install`, then `make docs-check` and `make docs-build` **serially**.
- Root deployment goes to one Vercel project. The default `vercel.json` uses the Go Function; OAuth users copy `vercel.oauth.json` to `vercel.json` to use the Go-server framework, because `/.well-known` cannot be rewritten by Vercel. ZueDocs lives entirely under `docs/`, designed for a separate project with Root Directory `docs`.
- Do not copy API keys, credentials or user-specific domains into this template.

### Docs deployment

The Docs GitHub Actions workflow deploys production only for the watched site paths; Vercel Git auto-deployments and `ignoreCommand` stay disabled. Keep the project-scoped `VERCEL_TOKEN` Actions secret and ID variables configured. Check Astro locally; builds run in CI.
