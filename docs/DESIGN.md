# zip-forger Design

## 1. Purpose

`zip-forger` is a service that lets authenticated Forgejo users download selected
subsets of repositories as ZIP archives. It is designed for large repositories
with Git LFS content and must not persist generated artifacts.

Core goals:

- Authenticate users against Forgejo.
- Enforce repository access using the user's Forgejo permissions.
- Allow selection by presets and optional ad-hoc filters.
- Stream ZIP output directly to clients without artifact storage.
- Degrade gracefully when ephemeral caches are lost.

## 2. Scope

### In scope (v1)

- Forgejo OAuth login.
- Repo/ref selection and config read from `.zip-forger.yaml`.
- Preset-driven filtering and optional ad-hoc filtering.
- Live ZIP streaming from Forgejo source content.
- Ephemeral caching using memory and local SQLite.
- Best-effort resume support only.

### Out of scope (v1)

- Persistent artifact storage.
- Guaranteed resumable downloads across restarts.
- Multi-node shared resume state.
- Full-featured admin UI for presets (initial API groundwork only).

## 3. Constraints and Assumptions

- Service state may be wiped at any time without data loss concerns.
- Local SQLite is cache-only and disposable.
- No generated ZIP file is written to durable storage.
- Output size may be very large (hundreds of GB in source repo), so stream paths
  must avoid full in-memory buffering.

## 4. High-Level Architecture

Components:

- `HTTP API`: authentication, repo introspection, preview, download.
- `Forgejo Client`: talks to repo/tree/blob/LFS endpoints with user token.
- `Config Loader`: fetches and parses `.zip-forger.yaml` at a commit.
- `Filter Engine`: resolves presets and ad-hoc criteria into selected entries.
- `Manifest Builder`: creates deterministic file list for a commit snapshot.
- `ZIP Streamer`: serially writes ZIP entries to response body.
- `Ephemeral Cache`: in-memory + SQLite for manifest and resume hints.

Data flow (`download.zip`):

1. User request arrives with repo/ref and filter input.
2. Resolve ref to immutable commit SHA.
3. Load `.zip-forger.yaml` from commit.
4. Validate filters against policy (`allowAdhocFilters`).
5. Build or reuse manifest (deterministic order).
6. Stream each selected file into ZIP writer.
7. Return stream with best-effort resume metadata.

## 5. Repository Config: `.zip-forger.yaml`

Root config file in the repository:

```yaml
version: 1

options:
  allowAdhocFilters: true
  maxFilesPerDownload: 50000
  maxBytesPerDownload: 214748364800

presets:
  - id: core-pdfs
    description: Core rules PDFs
    includeGlobs:
      - "rules/core/**/*.pdf"
    excludeGlobs:
      - "**/*draft*"
    extensions:
      - ".pdf"
    pathPrefixes:
      - "rules/core"
```

Rules:

- `version` must be `1`.
- `presets[].id` must be unique.
- `options.allowAdhocFilters` defaults to `true` if omitted.
- Empty filter fields are treated as no restriction.

## 6. Filtering Model

A final selection is produced from:

- one optional preset, plus
- optional ad-hoc filters (if allowed by config).

Supported filter primitives:

- `includeGlobs`: allow list patterns.
- `excludeGlobs`: deny list patterns.
- `extensions`: suffix matching (normalized lowercase).
- `pathPrefixes`: path root constraints.

Evaluation order for each file:

1. Must match include criteria (if present).
2. Must not match exclude criteria.
3. Must satisfy extension and prefix constraints.

## 7. API Draft

Authentication:

- `GET /auth/login`
- `GET /auth/callback`
- `POST /auth/logout`

Repository and config:

- `GET /api/repos`
- `GET /api/repos/{owner}/{repo}/config?ref=...`
- `POST /api/repos/{owner}/{repo}/preview`

Download:

- `GET /api/repos/{owner}/{repo}/download.zip?ref=...&preset=...`

Preview request body:

```json
{
  "preset": "core-pdfs",
  "adhoc": {
    "includeGlobs": ["rules/**/*.pdf"],
    "excludeGlobs": ["**/*draft*"],
    "extensions": [".pdf"],
    "pathPrefixes": ["rules"]
  }
}
```

## 8. Resume Strategy (Best-Effort Only)

Without persisted ZIP bytes, strict HTTP range resume cannot be guaranteed.
`zip-forger` provides best-effort behavior:

- Use deterministic archive construction:
  - stable sorted entry order,
  - fixed timestamps in ZIP headers,
  - no compression (`zip.Store`) by default.
- Track ephemeral session metadata in memory/SQLite:
  - request key (`repo`, `commit`, filter hash),
  - file offsets/checkpoints,
  - CRC and size hints where available.
- On resume attempt:
  - if session metadata exists, regenerate deterministically and skip bytes to
    requested offset;
  - if metadata is missing/expired, return restart requirement.

Recommended response contract when resume fails:

- `409 Conflict`
- machine-readable error code: `resume_not_available`

## 9. Caching Strategy

In-memory cache:

- commit tree listings
- parsed config files
- compiled filter presets

SQLite cache (ephemeral):

- manifest snapshots by `(repo, commit, filterHash)`
- optional resume checkpoints
- optional CRC/size memoization

Eviction:

- TTL-based expiration and size limits.
- Full cache loss is acceptable and only affects performance/features.

## 10. Security Model

- Use user-scoped Forgejo access tokens (OAuth).
- Authorize all repository operations with user token.
- Never bypass permissions via privileged global token.
- Validate and sanitize all input patterns and repo identifiers.
- Apply per-user rate limiting and basic abuse controls.

## 11. Performance Guidance

- Stream with constant memory usage per request.
- Avoid parallel file fetch inside a single ZIP stream unless profiling shows a
  measurable gain without memory spikes.
- Reuse HTTP transport and tune keep-alive/timeouts.
- Prefer `zip.Store` for PDF/LFS-heavy repositories.

## 12. Observability

Metrics:

- request count, latency, error rates
- manifest build duration
- stream throughput and cancellation reasons
- cache hit/miss and eviction counts

Logging:

- request identifiers
- user/repo/ref context
- structured error fields

## 13. Implementation Phases

Phase 1:

- Go module and HTTP skeleton
- config schema parser
- basic filter engine
- streamed ZIP endpoint with deterministic ordering

Phase 2:

- Forgejo integration for trees/blobs/LFS
- preview endpoint
- SQLite cache for manifest and resume hints

Phase 3:

- best-effort resume API behavior
- preset management UI and commit-back workflow
- performance tuning and load validation

## 14. Open Decisions

- Exact Forgejo token/session storage approach (encrypted cookie vs in-memory
  session store).
- Final resume API shape (`Range` only vs explicit resume token endpoint).
- Limits defaults (`maxFilesPerDownload`, `maxBytesPerDownload`) per deployment.
