# zip-forger

`zip-forger` is a web UI and API for downloading filtered subsets of a
repository as a ZIP archive based on presets and filters defined in
`.zip-forger.yaml`.

The ZIP is virtual. `zip-forger` does not prebuild a local archive file before
serving it. Instead it computes the ZIP structure on demand and streams entry
bytes directly from the source repository. In Forgejo mode that means download
traffic is proxied from the Forgejo server, including resumed byte-range
requests.

The service is otherwise stateless. It only keeps a local cache database for
tree metadata, manifests, and CRC reuse so repeated previews and downloads are
faster.

Current stage:

- Forgejo API mode
- Web UI at `/`
- UI theme switch (`system`, `light`, `dark`)
- Searchable owner/repository/branch inputs
- Preset and options editor (create/edit/delete/save, ad-hoc + max file/byte limits)
- Preview tree view with selected files
- Shareable direct download URL copy action
- Preview and config APIs
- Virtual ZIP download endpoint with resumable byte ranges
- Private direct download URLs backed by encrypted access tokens
- Forgejo OAuth login routes (optional)

## Self-Hosting

`zip-forger` only needs three runtime concerns:

- a place to listen for HTTP traffic
- a writable cache directory for the tree database and CRC cache
- access to a Forgejo instance

The included [Containerfile](/home/eric/zip-forger/Containerfile) defaults to:

- `ZIP_FORGER_ADDR=:8080`
- `ZIP_FORGER_CACHE_DIR=/var/lib/zip-forger/cache`

Persist `/var/lib/zip-forger/cache`.

That cache is only for performance. It is safe to delete, and the service does
not depend on any durable application database beyond it.

### Container Image

Replace `podman` with `docker` if that is what you run in production.

Build locally:

```bash
podman build -t zip-forger -f Containerfile .
```

Use the published image from this repository:

```bash
podman pull ghcr.io/tionis/zip-forger:latest
```

Run Forgejo OAuth mode:

```bash
podman run --rm \
  -p 8080:8080 \
  -e ZIP_FORGER_FORGEJO_BASE_URL='https://forgejo.example.org' \
  -e ZIP_FORGER_OAUTH_CLIENT_ID='replace-me' \
  -e ZIP_FORGER_OAUTH_CLIENT_SECRET='replace-me' \
  -e ZIP_FORGER_SESSION_SECRET='replace-with-32-random-bytes-or-more' \
  -v zip-forger-cache:/var/lib/zip-forger/cache \
  zip-forger
```

The GitHub container workflow publishes `ghcr.io/tionis/zip-forger` for this
repository. Forks will publish to `ghcr.io/<owner>/zip-forger`.

### Binary Deployment

You can also run the service directly:

```bash
go build -o ./bin/zip-forger ./cmd/zip-forger
./bin/zip-forger
```

For a systemd-style deployment, set the same environment variables described
below and ensure the service user can write `ZIP_FORGER_CACHE_DIR`.

### Required Configuration

Common variables:

- `ZIP_FORGER_ADDR` HTTP bind address, default `:8080`
- `ZIP_FORGER_CACHE_DIR` writable cache path, default `./.cache/zip-forger`

- `ZIP_FORGER_FORGEJO_BASE_URL` base URL of the Forgejo instance

Forgejo OAuth mode additionally requires:

- `ZIP_FORGER_OAUTH_CLIENT_ID`
- `ZIP_FORGER_OAUTH_CLIENT_SECRET`
- `ZIP_FORGER_SESSION_SECRET`

Recommended optional settings:

- `ZIP_FORGER_OAUTH_REDIRECT_URL` explicit OAuth callback URL override
- `ZIP_FORGER_DOWNLOAD_URL_TTL` private URL lifetime, default `24h`
- `ZIP_FORGER_OAUTH_SCOPES` comma-separated scope list, default `write:repository`
- `ZIP_FORGER_SESSION_COOKIE_NAME` cookie name override
- `ZIP_FORGER_SESSION_COOKIE_SECURE` set to `false` only for plain HTTP development

### Reverse Proxy Notes

- Run the public service behind HTTPS if you use OAuth or private download URLs.
- If `ZIP_FORGER_OAUTH_REDIRECT_URL` is unset, `zip-forger` derives the callback URL from `Forwarded`, `X-Forwarded-Proto`, `X-Forwarded-Host`, and `Host` headers in that order. Make sure your reverse proxy sets those headers correctly.
- Set `ZIP_FORGER_OAUTH_REDIRECT_URL` when you want a fixed callback URL independent of proxy headers, for example `https://zip-forger.example.org/auth/callback`.
- Keep `ZIP_FORGER_SESSION_COOKIE_SECURE=true` in production.
- Private download URLs and session cookies both derive from `ZIP_FORGER_SESSION_SECRET`. Use a stable production secret so both sessions and private links survive restarts.
- `zip-forger` does not need privileged Forgejo access. It uses the signed-in user's own OAuth access token for repository API, media, and LFS requests.
- `WriteTimeout` is intentionally disabled by the server because downloads are
  long-running virtual ZIP streams.

### GitHub Automation

The repository now ships two GitHub Actions workflows:

- [container.yml](/home/eric/zip-forger/.github/workflows/container.yml) runs on pull requests, `main`, and `v*` tags. It tests the project and publishes a multi-arch GHCR image on non-PR runs.
- [release.yml](/home/eric/zip-forger/.github/workflows/release.yml) runs on `v*` tags and creates a GitHub Release with Linux `amd64` and `arm64` tarballs plus checksums.

The intended release flow is:

1. Create and push a version tag such as `v1.2.3`.
2. The container workflow publishes the matching image tags to GHCR.
3. The release workflow creates the GitHub Release and uploads the binary archives.

You can also rerun the release packaging manually with `workflow_dispatch` by
providing an existing tag.

## Quick Start

1. Run the server:

```bash
ZIP_FORGER_FORGEJO_BASE_URL='https://forgejo.example.org' \
ZIP_FORGER_OAUTH_CLIENT_ID='replace-me' \
ZIP_FORGER_OAUTH_CLIENT_SECRET='replace-me' \
ZIP_FORGER_SESSION_SECRET='replace-with-32-random-bytes-or-more' \
go run ./cmd/zip-forger
```

2. In another shell, preview a preset:

```bash
curl -sS \
  -X POST \
  http://localhost:8080/api/repos/<owner>/<repo>/preview \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <forgejo_token>' \
  -d '{"ref":"main","preset":"core-pdfs"}' | jq
```

3. Download a ZIP:

```bash
curl -fL \
  'http://localhost:8080/api/repos/<owner>/<repo>/download.zip?ref=main&preset=core-pdfs' \
  -H 'Authorization: Bearer <forgejo_token>' \
  -o /tmp/rules.zip
```

4. Inspect config resolution:

```bash
curl -sS \
  'http://localhost:8080/api/repos/<owner>/<repo>/config?ref=main' \
  -H 'Authorization: Bearer <forgejo_token>' | jq
```

Open `http://localhost:8080/` to use the built-in UI.

## Forgejo OAuth Mode

Set these environment variables:

- `ZIP_FORGER_FORGEJO_BASE_URL`
- `ZIP_FORGER_OAUTH_CLIENT_ID`
- `ZIP_FORGER_OAUTH_CLIENT_SECRET`
- `ZIP_FORGER_SESSION_SECRET`

Optional:

- `ZIP_FORGER_OAUTH_REDIRECT_URL` (explicit callback URL override; otherwise derived from forwarded host/proto headers)
- `ZIP_FORGER_DOWNLOAD_URL_TTL` (defaults to `24h`)
- `ZIP_FORGER_OAUTH_SCOPES` (comma-separated)
- `ZIP_FORGER_SESSION_COOKIE_NAME`
- `ZIP_FORGER_SESSION_COOKIE_SECURE`

OAuth endpoints:

- `GET /auth/login`
- `GET /auth/callback`
- `POST /auth/logout`
- `GET /auth/me`

`zip-forger` does not use any instance-wide Forgejo admin token. The server
operates with the access token obtained for the current user via OAuth, and
programmatic clients can also supply their own Forgejo token directly.

Programmatic clients can still authenticate requests with
`Authorization: Bearer <forgejo_token>` when they already have a Forgejo token.
That is an alternate credential source, not a separate deployment mode.

## API Surface (Current)

- `GET /healthz`
- `GET /api/repos/search?q=...`
- `GET /api/repos/{owner}/{repo}/branches`
- `GET /api/repos/{owner}/{repo}/config?ref=...`
- `PUT /api/repos/{owner}/{repo}/config`
- `POST /api/repos/{owner}/{repo}/preview`
- `GET /api/repos/{owner}/{repo}/download.zip?ref=...&preset=...`
- `GET /api/downloads/private.zip?token=...`

Query filters for `download.zip`:

- `include` (repeatable or comma-separated)
- `exclude` (repeatable or comma-separated)
- `ext` (repeatable or comma-separated)
- `prefix` (repeatable or comma-separated)

## Notes

- ZIP downloads are served as a virtual archive. The server computes ZIP headers,
  central directory records, and byte offsets on demand instead of materializing
  a temporary `.zip` file first.
- The service is stateless apart from its local cache database. That cache only
  exists to avoid repeating expensive repository-tree and CRC work.
- Resume support works against that virtual archive. `Range` requests against the
  ZIP are translated into the corresponding archive sections, and file payload
  ranges are streamed from the source repository where possible.
- Preview responses include a direct download URL; when a server secret is configured, authenticated previews return a private URL with an encrypted embedded access token.
- In Forgejo mode, file bytes are streamed from the Forgejo `/media/{filepath}`
  endpoint, and resumed archive reads forward byte ranges upstream instead of
  downloading full files into a local archive cache.
- Forgejo source mode detects Git LFS pointer files and resolves them through the LFS batch download flow.
- Recursive tree listing falls back to Forgejo contents walk if tree responses are truncated.
