# Contributing to Origo

Origo is currently a private, infrastructure-only project. Work is organized around a small Go MCP API and its independently deployed ZueDocs website.

## Development

Run `make check` before pushing Go changes. Run `make docs-install`, then `make docs-check` followed by `make docs-build` for documentation changes. Do not run the docs check and build concurrently.

## Deployment

Both Vercel projects are connected to `amxv/origo` and deploy from `main` automatically. The API project root is `/`; the docs project root is `/docs`. Keep API secrets in Vercel environment variables. No GitHub Releases, npm packages, or version tags are needed.

## Product scope

The only planned MCP tools are `read_link` and `map_site`; implementation requires prior design discussion. Search is explicitly out of scope.
