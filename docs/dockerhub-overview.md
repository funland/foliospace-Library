# FolioSpace Library

FolioSpace Library is a self-hosted personal digital asset library for NAS, Docker, and local servers. It provides a unified indexing layer and client API for books, comics, PDFs, game ROM libraries, videos, and future spatial media clients.

It is not a cloud media service and does not distribute books, comics, ROMs, movies, or other media content. It indexes user-owned local files and exposes stable service URLs to web and native clients without leaking real NAS paths.

## 0.932 Hotfix

Release `0.932` is a large-library scan performance hotfix:

- Full-library scans preload existing file index rows once per job instead of querying SQLite for every unchanged book.
- Unchanged CBZ/ZIP/PDF/7z entries can fast-skip from file metadata without reopening archives or forcing page analysis.
- Existing nested comic collections are not reclassified during normal unchanged scans, avoiding expensive churn on very large libraries.
- Root-level legacy collection migration is still preserved for older imports.
- This release keeps the on-demand analysis model: unchanged comics do not need page metadata populated before they can be skipped.

## Quick Start

```bash
docker pull funland/foliospace-library:0.932
```

```bash
docker run -p 8080:8080 \
  -v /volume1/docker/foliospace-library/config:/config \
  -v /volume2/ComicCenter:/library:ro \
  -v /volume2/Books:/books:ro \
  -v /volume2/GameROMS:/games:ro \
  -e FOLIOSPACE_DIRECTORY_ROOTS=/library,/books,/games \
  funland/foliospace-library:0.932
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
