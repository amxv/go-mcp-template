# go-cli-template

Minimal template for shipping a Go CLI with:

- one-command identity bootstrap after GitHub template cloning
- a local command runner (`Makefile`)
- npm global install wrapper (`bin/mycli.js`)
- automatic GitHub Release + npm publish on tag
- bundled ZueDocs-powered docs site

## Install (template example)

```bash
npm i -g @amxv/go-cli-template
mycli --help
```

## Commands in this starter

```bash
mycli --help
mycli hello
mycli hello <name>
mycli --version
```

## Docs site

This template includes an Astro docs site powered by ZueDocs.

```bash
make docs-install
make docs-dev
make docs-check
make docs-build
```

Customize the docs alongside the CLI:

- `docs/src/data/docs.ts`: site name, repo URL, footer sections, nav, categories
- `docs/src/pages/index.astro`: landing page
- `docs/src/content/docs/*.md`: guides and command reference

The docs app is self-contained under `docs/`, including its Bun dependencies,
Astro config, public assets, and Vercel config. Configure Vercel with `docs` as
the project Root Directory.

## Start a new CLI

Create the repository from the directory where the new clone should be placed:

```bash
gh repo create acme/pluck \
  --public \
  --template amxv/go-cli-template \
  --clone
```

Then run the identity bootstrap from the new repository root:

```bash
cd pluck
make bootstrap BOOTSTRAP_ARGS='--cli-name pluck \
  --binary-name pluck \
  --go-module github.com/acme/pluck \
  --github-owner acme --github-repo pluck \
  --npm-package @acme/pluck \
  --description "A fast file picker" \
  --license Apache-2.0'
```

The bootstrap command runs from the cloned repository root and never creates or
clones another repository. Omit `--public` in the GitHub command for the safe
private default, while public repositories are the normal path for anonymous npm
installs. If a private repo is intentionally paired with anonymous npm installs,
pass `--visibility private --anonymous-npm` to the bootstrap command for the warning.

For any existing clone, `make bootstrap` accepts the same identity options:

```bash
make bootstrap BOOTSTRAP_ARGS='--cli-name pluck \
  --binary-name pluck \
  --go-module github.com/acme/pluck \
  --github-owner acme --github-repo pluck \
  --npm-package @acme/pluck \
  --description "A fast file picker" \
  --homepage https://pluck.dev \
  --canonical-url https://pluck.dev \
  --license Apache-2.0'
```

The bootstrap script updates the command path, Go module, npm metadata, GitHub URLs,
Makefile, release workflow, postinstall metadata, docs configuration, and license
references together. Apache-2.0 is built in and does not require an extra CLI.
Other license-generator names listed by `license-generator --list` are supported
when that optional CLI is installed. Without it, bootstrap keeps the project on
Apache-2.0 and prints a warning.

## Customize the CLI

After bootstrap, replace starter logic:

- `internal/app/app.go`
- `internal/app/app_test.go`

Update bundled docs as the command surface changes:

- `docs/src/data/docs.ts`
- `docs/src/pages/index.astro`
- `docs/src/content/docs/*.md`

## Release flow

Future releases are tag-driven. Edit `docs/src/content/docs/changelog.md` manually,
run the checks, commit and push the changelog, then push a `v*` tag:

```bash
make check
make docs-check
make docs-build
# edit docs/src/content/docs/changelog.md
git add docs/src/content/docs/changelog.md
git commit -m "docs: update changelog for v0.2.0"
git push origin HEAD
make release-tag VERSION=0.2.0
```

GitHub Actions builds the release binaries, creates the GitHub Release, and
publishes npm. Configure the `NPM_TOKEN` GitHub secret once before the first
release. No local token synchronization or release orchestration is required.

## Project layout

- `cmd/mycli/main.go`: CLI entrypoint
- `internal/app/`: command logic
- `internal/buildinfo/`: build-time version plumbing for `--version`
- `scripts/postinstall.js`: installs binary from GitHub release (falls back to local `go build`)
- `scripts/setup.js`: initializes the repeated project identity fields
- `.agents/skills/release/SKILL.md`: manual changelog and tag release checklist
- `.github/workflows/release.yml`: automated release pipeline
- `project.config.json`: canonical project identity
- `docs/`: self-contained Astro/ZueDocs documentation workspace
- `docs/src/`: documentation pages, content, styles, and site config
- `docs/vercel.json`: docs deployment config; Vercel Root Directory should be `docs`
- `AGENTS.md`: instructions for coding agents
- `CONTRIBUTORS.md`: maintainer/release operations

See `AGENTS.md` and `CONTRIBUTORS.md` for complete dev/release instructions.

## License

Apache-2.0
