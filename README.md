# zip-forger

`zip-forger` streams ZIP downloads from a Forgejo repository based on presets and
filters defined in `.zip-forger.yaml`.

Current stage:

- Local test mode (`ZIP_FORGER_SOURCE=local`)
- Forgejo API mode (`ZIP_FORGER_SOURCE=forgejo`)
- Web UI at `/`
- UI theme switch (`system`, `light`, `dark`)
- Searchable owner/repository/branch inputs
- Preset editor (create/edit/delete/save)
- Preview tree view with selected files
- Shareable direct download URL copy action
- Preview and config APIs
- Streaming ZIP download endpoint
- Forgejo OAuth login routes (optional)

## Quick Start (Local Mode)

1. Run the server:

```bash
go run ./cmd/zip-forger
```

2. In another shell, preview a preset:

```bash
curl -sS \
  -X POST \
  http://localhost:8080/api/repos/acme/rules/preview \
  -H 'Content-Type: application/json' \
  -d '{"ref":"main","preset":"core-pdfs"}' | jq
```

3. Download a ZIP:

```bash
curl -fL \
  'http://localhost:8080/api/repos/acme/rules/download.zip?ref=main&preset=core-pdfs' \
  -o /tmp/rules.zip
```

4. Inspect config resolution:

```bash
curl -sS 'http://localhost:8080/api/repos/acme/rules/config?ref=main' | jq
```

The repository root defaults to `./mock-repos`, and this project already ships
with a sample repo under that path.

Open `http://localhost:8080/` to use the built-in UI.

## Forgejo Mode (PAT/Bearer Header)

This is the simplest way to test against a real Forgejo instance without OAuth UI setup.

1. Start server:

```bash
ZIP_FORGER_SOURCE=forgejo \
ZIP_FORGER_FORGEJO_BASE_URL='https://forgejo.example.org' \
ZIP_FORGER_AUTH_MODE=none \
ZIP_FORGER_AUTH_REQUIRED=true \
go run ./cmd/zip-forger
```

2. Call preview with a Forgejo token:

```bash
curl -sS \
  -X POST \
  "http://localhost:8080/api/repos/<owner>/<repo>/preview" \
  -H "Authorization: Bearer <forgejo_token>" \
  -H "Content-Type: application/json" \
  -d '{"ref":"main","preset":"core-pdfs"}' | jq
```

3. Download with the same token:

```bash
curl -fL \
  "http://localhost:8080/api/repos/<owner>/<repo>/download.zip?ref=main&preset=core-pdfs" \
  -H "Authorization: Bearer <forgejo_token>" \
  -o /tmp/repo.zip
```

## Forgejo OAuth Mode

Set these environment variables:

- `ZIP_FORGER_SOURCE=forgejo`
- `ZIP_FORGER_FORGEJO_BASE_URL`
- `ZIP_FORGER_AUTH_MODE=forgejo-oauth`
- `ZIP_FORGER_OAUTH_CLIENT_ID`
- `ZIP_FORGER_OAUTH_CLIENT_SECRET`
- `ZIP_FORGER_OAUTH_REDIRECT_URL` (must match Forgejo OAuth app config)
- `ZIP_FORGER_SESSION_SECRET`

Optional:

- `ZIP_FORGER_AUTH_REQUIRED` (defaults to `true` for Forgejo source)
- `ZIP_FORGER_OAUTH_SCOPES` (comma-separated)
- `ZIP_FORGER_SESSION_COOKIE_NAME`
- `ZIP_FORGER_SESSION_COOKIE_SECURE`

OAuth endpoints:

- `GET /auth/login`
- `GET /auth/callback`
- `POST /auth/logout`
- `GET /auth/me`

## API Surface (Current)

- `GET /healthz`
- `GET /api/owners`
- `GET /api/owners/{owner}/repos`
- `GET /api/repos/{owner}/{repo}/branches`
- `GET /api/repos/{owner}/{repo}/config?ref=...`
- `PUT /api/repos/{owner}/{repo}/config`
- `POST /api/repos/{owner}/{repo}/preview`
- `GET /api/repos/{owner}/{repo}/download.zip?ref=...&preset=...`

Query filters for `download.zip`:

- `include` (repeatable or comma-separated)
- `exclude` (repeatable or comma-separated)
- `ext` (repeatable or comma-separated)
- `prefix` (repeatable or comma-separated)

## Notes

- ZIP output is streamed directly and not persisted.
- `Range` resume is not fully implemented yet.
- Recursive tree listing falls back to Forgejo contents walk if tree responses are truncated.
