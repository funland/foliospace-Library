# FolioSpace Library

FolioSpace Library is a self-hosted personal digital asset library for NAS, Docker, and local servers. It provides a unified indexing layer and client API for books, comics, PDFs, game ROM libraries, videos, and future spatial media clients.

It is not a cloud media service and does not distribute books, comics, ROMs, movies, or other media content. It indexes user-owned local files and exposes stable service URLs to web and native clients without leaking real NAS paths.

## 0.965 Release: Client Catalog APIs

Release `0.965` adds paginated catalog APIs for native iPad, iPhone, and Vision Pro clients.

- `GET /api/client/books` returns a client-safe paginated All Books catalog with `limit`, `offset`, `q`, `sort`, `direction`, and `format`.
- Book catalog responses include `manifestUrl`, cover URLs, thumbnail URLs, profile-scoped progress, favorite state, private status, tags, and ratings without exposing NAS file paths.
- `GET /api/collections` now has an optional paginated mode with `primaryType`, `limit`, `offset`, `sort`, `direction`, and `q`.
- Legacy `GET /api/collections` without query parameters still returns the original array shape for existing web UI compatibility.
- `/api/client/info` advertises `bookCatalog: true` and `collectionCatalog: true` for client capability detection.

## 0.961 Hotfix: Cleaner Shelves and Covers

Release `0.961` is a library cleanup and cover-refresh hotfix on top of `0.96`.

- ZIP/CBZ page listing now ignores macOS resource fork entries such as `__MACOSX/` and `._*`, preventing doubled page counts and broken placeholder pages in affected archives.
- Continue Reading, Favorites, Want to Read, and recent shelves now hide stale entries when the indexed file has been deleted or changed on disk.
- Book thumbnail cache keys were refreshed so corrected books no longer keep old generic placeholder covers after re-analysis.
- The service, Client API, and MCP metadata report version `0.961`.

## 0.96 Release: Fast Recent Scans

Release `0.96` focuses on faster day-to-day imports for very large libraries. When you add several new comics or books to a directory with thousands of existing files, you no longer need to kick off a heavy full-library scan.

- New "scan latest added" action in the Tasks page.
- Selectable recent limits for common import batches, such as 10, 20, 50, 100, or 200 files.
- Recent scans index only new or changed files under a selected library or subdirectory.
- Duplicate running scans for the same library and target path are reused instead of creating overlapping jobs.
- HTTP API supports `POST /api/libraries/:id/scan` with `mode: "recent"`.
- MCP exposes `foliospace.scan_recent`, so local agents can trigger the same fast scan path.
- `/api/client/info` advertises `recentScan: true` for client capability discovery.

Example API request after adding new files under a large manga folder:

```json
{
  "mode": "recent",
  "path": "/library/韩漫",
  "recentLimit": 20
}
```

## Quick Start

```bash
docker pull funland/foliospace-library:0.965
```

```bash
docker run -p 8080:8080 \
  -v /volume1/docker/foliospace-library/config:/config \
  -v /volume2/ComicCenter:/library:ro \
  -v /volume2/Books:/books:ro \
  -v /volume2/GameROMS:/games:ro \
  -e FOLIOSPACE_DIRECTORY_ROOTS=/library,/books,/games \
  funland/foliospace-library:0.965
```

Open `http://localhost:8080`. On a fresh `/config`, FolioSpace Library starts with a setup page for the first access key and first library path.

## Runtime Paths

- `/config`: SQLite database, generated covers/thumbnails, runtime cache.
- `/library`: default read-only mounted asset library root.
- `/books`, `/games`, `/movies`: optional read-only roots.
- `8080`: web UI and HTTP API.

## Key Environment Variables

```bash
FOLIOSPACE_CONFIG_DIR=/config
FOLIOSPACE_LIBRARY_DIR=/library
FOLIOSPACE_DIRECTORY_ROOTS=/library,/books,/games
FOLIOSPACE_ADDR=:8080
FOLIOSPACE_API_TOKEN=
FOLIOSPACE_SCAN_WORKERS=2
FOLIOSPACE_WEB_TTS_ENABLED=false
```

If `FOLIOSPACE_API_TOKEN` is empty, the web setup page can create the first access token and stores only a SHA-256 token hash in SQLite.

EPUB text-to-speech in the bundled web UI is experimental and disabled by default. Set `FOLIOSPACE_WEB_TTS_ENABLED=true` to expose the TTS controls.

## Supported Areas

- EPUB, CBZ, ZIP, and PDF reading.
- Single-page, double-page, compact mobile, fullscreen, and webtoon-style comic/PDF modes.
- Structured reading progress and private state.
- Game ROM library indexing and client-safe launch manifests.
- Video library indexing and lightweight playback/transcode support.
- Scan jobs with progress, worker settings, errors, pause/cancel/resume, and targeted scan entry points.
- MCP server packages for local agent integration.

## Links

- Website: https://foliospace.app/
- GitHub: https://github.com/funland/foliospace-Library
- Client API docs: https://github.com/funland/foliospace-Library/blob/main/docs/api/client-v1.md
- MCP docs: https://github.com/funland/foliospace-Library/blob/main/docs/mcp/usage.md
