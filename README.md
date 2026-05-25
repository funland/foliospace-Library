# FolioSpace Library

FolioSpace Library is a personal digital asset library that runs on a NAS, Docker host, or local server. It provides a unified indexing layer and stable client service layer for Apple-device experiences across reading, games, spatial media, documents, photos, videos, and related audio collections.

It is not trying to become a complete Plex, Jellyfin, or Immich replacement. The first priority is personal asset indexing: scanning, identifying, covers/thumbnails, classification, search, favorites, recent access, progress, and private state. Dedicated clients such as a reader app, GameEMU, and Vision Pro experiences own the actual consumption UI.

The current implementation still starts from the FolioSpace Reader codebase and keeps the existing reading MVP operational while the model evolves toward `Asset` / `LibraryItem`.

## Runtime Layout

- `/config`: SQLite database, generated covers/thumbnails, runtime cache.
- `/library`: read-only mounted asset library root.
- `8080`: web UI and HTTP API.

Recommended NAS config root:

```text
/volume1/docker/foliospace-library
```

## Local Development

The backend requires Go 1.22 or newer. The frontend requires Node.js 20 or newer.

```bash
npm --prefix web install
npm --prefix web run build
go test ./...
go run ./cmd/foliospace-reader
```

## Environment

```bash
FOLIOSPACE_CONFIG_DIR=/config
FOLIOSPACE_LIBRARY_DIR=/library
FOLIOSPACE_ADDR=:8080
FOLIOSPACE_API_TOKEN=
```

Set `FOLIOSPACE_API_TOKEN` to require API authentication. Native clients can send `Authorization: Bearer <token>`. The web UI stays publicly loadable, then prompts for the access token and receives an HttpOnly cookie so covers, pages, and EPUB iframe resources can load through normal browser requests.

Authentication helpers:

- `GET /api/auth/status`: returns whether token auth is enabled.
- `POST /api/auth/check`: accepts `{"token":"..."}` and returns `{"ok":true}` for a valid token.
- `POST /api/auth/logout`: clears the web auth cookie.

## Client API v1

Detailed client integration docs are in [`docs/api/client-v1.md`](docs/api/client-v1.md).

- `GET /api/client/info`: service metadata, supported formats, and capability flags.
- `GET /api/client/home`: `continueReading`, `recentBooks`, and `collections` in one response.
- `GET /api/client/books/:id/manifest`: a client-safe open manifest. CBZ/ZIP books include page URLs; EPUB books include spine, TOC, `resourceBaseUrl`, `coverUrl`, and progress.
- `GET/PUT /api/client/books/:id/private-state`: client-safe private status, favorite, rating, tags, and note sync.
- `GET /api/client/search`, `/api/client/books/favorites`, and `/api/client/books/private-status/:status`: private-state-aware discovery shelves.

Client API book and collection responses omit local NAS file paths.

## Product Direction

Detailed product direction and the proposed `Asset` / `LibraryItem` model are in [`docs/product/foliospace-library-direction.md`](docs/product/foliospace-library-direction.md).

Core asset types:

- Books and EPUBs.
- Comics and CBZ/ZIP archives.
- Game ROMs and ROM sets.
- PDFs, manuals, art books, guides, and reference documents.
- Photos, videos, Vision Pro spatial photos, and spatial videos.
- OSTs and audio material connected to games, books, and collections.

ROM support is for indexing and launching user-owned local content. FolioSpace Library does not distribute ROMs, provide download sources, or bundle pirated assets.

## Docker

For local verification:

```bash
mkdir -p data/config data/library
docker compose up --build
```

For a NAS deployment, mount your real library as read-only:

```bash
docker run -p 8080:8080 \
  -v /volume1/docker/foliospace-library/config:/config \
  -v /volume2/ComicCenter:/library:ro \
  foliospace-library:dev
```

Open `http://localhost:8080`, scan the configured library, then browse collections and books.

## Current MVP Support

- P0 reading formats: `.cbz`, `.zip`, `.epub`.
- Series derivation: immediate parent directory, with root-level files grouped under `Unsorted`.
- Reading: backend streams one ZIP image entry or EPUB resource at a time.
- Errors: empty files, archive open failures, walk errors, and unsupported future categories are recorded as structured rows.

Near-term expansion priority:

1. Keep existing EPUB/comic reader APIs stable.
2. Add game asset indexing for local ROMs and ROM sets.
3. Add spatial photo / spatial video indexing.
4. Move data model language from Book/Series toward Asset/LibraryItem after the first non-reading asset type is real.

## Git Remote

The project remote is:

```bash
git remote add origin http://192.168.10.158:8418/funland/FolioSpaceReader.git
```
