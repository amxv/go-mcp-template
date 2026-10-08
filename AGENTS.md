# Origo agent instructions

- Origo is a serverless Go MCP service, **not a CLI**. Do not restore npm wrappers, release workflows, CLI commands, or search APIs.
- The only planned public MCP tools are `read_link` and `map_site`; neither is implemented. Obtain design approval before implementing retrieval features.
- The API deployment uses the repository root; ZueDocs is independently deployed from `docs/`.
- Keep endpoint authentication through `ORIGO_API_KEY` using exactly one `?key=` query parameter. Never log credentials or full authenticated request URLs.
- Use the official Go MCP SDK with stateless Streamable HTTP for serverless deployments.
- `make check` runs Go tests/vet; `make docs-check` and `make docs-build` must run **serially** after `make docs-install`.
- Pushes to `main` trigger Vercel deployments. There is no manual release or npm publication.
- Keep credentials out of the repository. Update README/docs when the protocol or deployment changes.
