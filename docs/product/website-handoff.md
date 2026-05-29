# FolioSpace Library Website Handoff

This document is the handoff package for building the public website at:

```text
https://foliospace.app
```

The website should present FolioSpace Library as a lightweight personal digital asset library for NAS, Docker, and local servers. It should not over-position the project as a Plex/Jellyfin/Immich replacement. The correct framing is a unified indexing and client service layer for personal Apple-device workflows: reading, local game libraries, and future spatial media.

## Product Name

Primary name:

```text
FolioSpace Library
```

Current repository name:

```text
FolioSpaceReader
```

Docker image:

```text
funland/foliospace-library:0.82
```

Suggested CLI / binary name for future release work:

```text
foliospace-library
```

Current release:

```text
0.82
```

## One-Line Description

FolioSpace Library is a lightweight personal digital asset library that runs on your NAS or Docker host and provides fast indexing, reading, game-shelf, search, progress, and client-safe APIs for your own books, comics, ROMs, and future spatial media.

## Short Product Copy

FolioSpace Library runs on a NAS, Docker host, or local server. It indexes your personal EPUBs, comics, local ROM collections, documents, and future spatial media, then exposes a stable web UI, HTTP API, and MCP surface for clients such as a web reader, Vision Pro reader, GameEMU, and other Apple-device experiences.

It is intentionally smaller than a full media server. The priority is fast startup, low overhead, transparent scanning, explainable file errors, private reading state, and client-safe access that does not expose real NAS paths.

## Positioning

Use this positioning:

- Personal digital asset library for NAS and Docker.
- Lightweight alternative to heavy book/comic servers when the user mainly needs indexing, reading, progress, search, and clean error reporting.
- Unified service layer for Apple-device experiences.
- Web UI included, but native clients are first-class through `/api/client`.
- Local-first and self-hosted.

Avoid this positioning:

- Do not call it a full Plex, Jellyfin, Immich, Komga, or Calibre replacement.
- Do not imply it distributes books, comics, ROMs, or media.
- Do not advertise ROM download features.
- Do not expose or suggest direct NAS file-path access from clients.

## Target Users

- Users with a NAS-hosted book, comic, or ROM collection.
- Users who want a lighter alternative to heavyweight comic/book services.
- Apple-device users who want a server layer for Vision Pro, iPad, iPhone, Mac, or Apple TV clients.
- Developers who want a stable API for EPUB/comic reading, game launch handoff, and private progress/state sync.

## Primary Value Props

- Fast Docker/NAS startup and low memory footprint.
- Incremental, observable scans instead of opaque full-library analysis.
- Scan jobs can be timed, paused, cancelled, resumed, and inspected.
- File errors are explicit and connected to the real failing path.
- EPUB and CBZ/ZIP are streamed on demand instead of fully extracted.
- Private state for personal library workflows: want, reading, finished, dropped, favorite, rating, tags, notes.
- Client-safe HTTP API keeps NAS paths private.
- MCP support lets agents inspect libraries, jobs, manifests, preferences, progress, and health.
- Game ROM indexing is local-only and designed for launch handoff to native clients such as GameEMU.

## Current Feature Set

Reading:

- EPUB indexing and reading.
- CBZ/ZIP comic indexing and reading.
- Single-page and double-page reader modes.
- Fullscreen reading.
- EPUB themes: light, sepia, dark.
- EPUB font-size preference.
- Reading progress sync.
- Continue Reading shelf with progress.

Library:

- Multiple resource directories.
- Directory picker for container-visible roots.
- Collections / series view.
- Cover wall.
- Search.
- Recently added.
- Favorites and Want to Read shelves.
- Private status: want, reading, finished, dropped.
- Private metadata: favorite, rating, tags, note.

Scanning:

- Async scan jobs.
- Incremental scans.
- Concurrent worker pool controlled by `FOLIOSPACE_SCAN_WORKERS`.
- Job elapsed time.
- Pause, cancel, and resume controls.
- Structured scan events.
- Structured file errors.
- Skips `#recycle` directories.
- Avoids rereading unchanged EPUB metadata when cached metadata is still valid.

Games:

- Local ROM and ROM-set indexing.
- Game shelf in the home API.
- Platform-aware game DTOs.
- Game manifest for native launch handoff.
- Opaque game file URL through the service.
- Libretro-style boxart cache where available.
- `Now Printing` placeholder when cover art is missing.
- ROM support is for user-owned local files only.

Setup / Release:

- First-run setup page.
- Access key setup when no environment token is configured.
- SHA-256 token hash stored in SQLite.
- HttpOnly cookie flow for web subresources.
- Bearer token flow for native clients.
- Multi-architecture Docker image for `linux/amd64` and `linux/arm64`.

## Supported Formats

Current priority support:

- EPUB: `.epub`
- Comics: `.cbz`, `.zip`
- Games: `.nes`, `.sfc`, `.smc`, `.gba`, `.gb`, `.gbc`, `.nds`, `.3ds`, `.cia`, `.chd`, `.iso`, `.bin`, `.cue`
- Archive ROM sets: `.zip`, `.7z` only when the configured library type is `game`

Important note for website copy:

`.7z` should not be described as a default comic format. In mixed or book/comic libraries it is treated conservatively and should not be promoted as primary reading support.

## Planned Expansion

Near-term:

- More polished release packaging and update guide.
- Broader asset model: `Asset` / `LibraryItem`.
- PDF/manual/archive collection support.
- Game metadata improvements.
- Spatial photo and spatial video indexing.
- Better diagnostic export for early adopters.

Future modules:

- Reader: EPUB and comic reading.
- Game Shelf: ROM library and GameEMU launch handoff.
- Spatial Gallery: Vision Pro spatial photo/video browsing.
- Archive: PDFs, manuals, art books, guides, setting collections, and reference documents.

## Recommended Website Structure

Suggested pages:

1. Home
2. Install
3. Features
4. Client API
5. MCP
6. Roadmap
7. Feedback / Issues

Suggested home-page sections:

1. Hero
   - Product name: FolioSpace Library
   - Tagline: Personal digital asset library for NAS, Docker, and Apple-device clients.
   - CTA: Install with Docker
   - Secondary CTA: View API Docs
2. What it manages
   - Books / EPUB
   - Comics / CBZ / ZIP
   - Game ROM libraries
   - PDFs and manuals
   - Future spatial photos and videos
3. Why it exists
   - Lightweight alternative to heavyweight book/comic servers.
   - Transparent scans and explainable errors.
   - Stable API for native clients.
4. Screenshots
   - Library home
   - Cover wall
   - EPUB reader
   - Comic reader
   - Scan jobs
   - Game shelf
   - Setup page
5. Install
   - Docker pull and run commands.
6. API and MCP
   - Native client API.
   - MCP agent tools.
7. Roadmap / current boundaries
   - Local-only, no ROM distribution.
   - Not a full Plex/Jellyfin/Immich clone.

## Docker Install

Primary Docker command:

```bash
docker pull funland/foliospace-library:0.82
```

Simple NAS run example:

```bash
docker run -d \
  --name foliospace-library \
  --restart unless-stopped \
  -p 8080:8080 \
  -v /volume1/docker/foliospace-library/config:/config \
  -v /volume2/ComicCenter:/library:ro \
  -v /volume2/Books:/books:ro \
  -v /volume2/GameROMS:/games:ro \
  -e FOLIOSPACE_DIRECTORY_ROOTS=/library,/books,/games \
  funland/foliospace-library:0.82
```

Open:

```text
http://localhost:8080
```

Fresh installs show a setup page. The user creates an access key and selects a container-visible directory such as `/library`, `/books`, or `/games`.

Important installation explanation:

The path selected in the web UI is the path inside the container. If a NAS folder is not visible in the setup picker, the user must add a Docker volume mapping first.

## Docker Compose

Reference compose:

```yaml
services:
  foliospace-library:
    image: funland/foliospace-library:0.82
    ports:
      - "8080:8080"
    volumes:
      - ./data/config:/config
      - ./data/library:/library:ro
      - ./data/books:/books:ro
      - ./data/games:/games:ro
    environment:
      FOLIOSPACE_CONFIG_DIR: /config
      FOLIOSPACE_LIBRARY_DIR: /library
      FOLIOSPACE_DIRECTORY_ROOTS: /library,/books,/games
      FOLIOSPACE_ADDR: :8080
      FOLIOSPACE_API_TOKEN: ""
      FOLIOSPACE_SCAN_WORKERS: "2"
```

## Environment Variables

| Variable | Default / Example | Purpose |
| --- | --- | --- |
| `FOLIOSPACE_CONFIG_DIR` | `/config` | SQLite database, generated covers, thumbnails, cache. |
| `FOLIOSPACE_LIBRARY_DIR` | `/library` | Legacy/default library root. |
| `FOLIOSPACE_DIRECTORY_ROOTS` | `/library,/books,/games` | Container-visible roots shown in setup/directory picker. |
| `FOLIOSPACE_ADDR` | `:8080` | HTTP listen address inside the container. |
| `FOLIOSPACE_API_TOKEN` | empty | Optional environment-managed API token. If empty, first-run setup creates a DB-backed token. |
| `FOLIOSPACE_SCAN_WORKERS` | `2` | Concurrent scan workers. Keep low on NAS devices. |

## Auth Summary

Native clients:

```http
Authorization: Bearer <token>
```

Web UI:

- User enters the access key.
- Server sets an HttpOnly cookie.
- Covers, pages, and EPUB iframe resources load through normal browser requests.

First-run setup:

- If `FOLIOSPACE_API_TOKEN` is empty, setup creates the first access key.
- If `FOLIOSPACE_API_TOKEN` is set, setup requires that token.

## Client API Summary

Base API docs:

```text
docs/api/client-v1.md
```

Stable client prefix:

```text
/api/client
```

Important endpoints:

```http
GET  /api/auth/status
POST /api/auth/check
POST /api/auth/logout

GET  /api/setup/status
POST /api/setup/initialize
GET  /api/config/directory-roots

GET  /api/client/info
GET  /api/client/home
GET  /api/client/search?q=...
GET  /api/client/preferences
PUT  /api/client/preferences
GET  /api/settings/scan
PUT  /api/settings/scan

GET  /api/client/books/:id/manifest
GET  /api/client/books/:id/private-state
PUT  /api/client/books/:id/private-state
GET  /api/client/books/favorites
GET  /api/client/books/private-status/:status

GET  /api/client/games
GET  /api/client/games/:id/manifest
GET  /api/client/games/:id/file
```

Client-safe rule:

Client API responses should not expose real absolute NAS paths. They return opaque service URLs for covers, pages, EPUB resources, and game files.

## Example Client Flow

```text
1. GET /api/auth/status
2. Store the access key in the native keychain if auth is enabled.
3. GET /api/client/info
4. GET /api/client/home
5. Open a book with GET /api/client/books/{bookId}/manifest
6. Stream CBZ/ZIP pages from returned page URLs, EPUB spine/resources from the EPUB manifest, or PDF data from the Range-capable PDF page URL.
7. Sync reading progress and private state.
8. Open a game with GET /api/client/games/{gameId}/manifest and pass the service file URL to the native emulator layer.
```

## MCP Summary

MCP docs:

```text
docs/mcp/usage.md
```

Website should expose this as a user-facing flow:

```text
1. Run FolioSpace Library with Docker on your NAS or local server.
2. Install the FolioSpace MCP binary on the computer where your agent client runs.
3. Configure Codex, Claude Desktop, or another MCP client with your FolioSpace base URL and access key.
4. Ask the agent to inspect your library, start scans, summarize errors, or open manifests.
```

Website copy should make the deployment model explicit:

```text
FolioSpace Library MCP is not a hosted cloud service. It connects your MCP client to your own FolioSpace Library server. First run FolioSpace Library on your NAS, Docker host, or local server, then configure the `foliospace-mcp` binary on the machine where your agent client runs.
```

Recommended architecture diagram copy:

```text
NAS / Docker host:
  FolioSpace Library web service
  http://nas-ip:8080

User computer / agent runtime:
  foliospace-mcp binary
  Codex, Claude Desktop, or another MCP client

The MCP server calls FolioSpace Library through HTTP API. Large media content such as comic pages, EPUB resources, PDF streams, and ROM files still streams through the HTTP URLs returned by the API.
```

End-user install:

```bash
curl -fsSL https://foliospace.app/install-mcp.sh | sh
```

Default install path:

```text
~/.local/bin/foliospace-mcp
```

Release package placeholders to publish on the website:

```text
/install-mcp.sh
/releases/foliospace-mcp_0.82_darwin_arm64.tar.gz
/releases/foliospace-mcp_0.82_darwin_amd64.tar.gz
/releases/foliospace-mcp_0.82_linux_arm64.tar.gz
/releases/foliospace-mcp_0.82_linux_amd64.tar.gz
/releases/checksums.txt
```

Current local release artifact source for the website build:

```text
/Users/deadseafu/Documents/FolioSpaceReader/dist/install-mcp.sh
/Users/deadseafu/Documents/FolioSpaceReader/dist/releases/checksums.txt
/Users/deadseafu/Documents/FolioSpaceReader/dist/releases/foliospace-mcp_0.82_darwin_arm64.tar.gz
/Users/deadseafu/Documents/FolioSpaceReader/dist/releases/foliospace-mcp_0.82_darwin_amd64.tar.gz
/Users/deadseafu/Documents/FolioSpaceReader/dist/releases/foliospace-mcp_0.82_linux_arm64.tar.gz
/Users/deadseafu/Documents/FolioSpaceReader/dist/releases/foliospace-mcp_0.82_linux_amd64.tar.gz
```

Maintainer build command:

```bash
VERSION=0.82 ./scripts/build-mcp-release.sh
```

Environment:

```bash
export FOLIOSPACE_BASE_URL=http://localhost:8080
export FOLIOSPACE_API_TOKEN=your-token-if-enabled
```

MCP client config example:

```json
{
  "mcpServers": {
    "foliospace-library": {
      "command": "/Users/you/.local/bin/foliospace-mcp",
      "env": {
        "FOLIOSPACE_BASE_URL": "http://your-nas-ip:8080",
        "FOLIOSPACE_API_TOKEN": "your-token-if-enabled"
      }
    }
  }
}
```

User prompt samples for the website:

```text
Use FolioSpace Library MCP to show service version, supported formats, and current health.
```

```text
Search my FolioSpace Library for "metal slug" and open the game manifest for the best match.
```

```text
List my configured FolioSpace libraries, then start a scan for the Books library.
```

```text
Show recent scan jobs and summarize the latest errors.
```

```text
Find books marked want-to-read and show the first 10.
```

MCP smoke-test note for docs:

```text
FolioSpace MCP accepts standard Content-Length framed MCP stdio and newline-delimited JSON-RPC. Prefer MCP client configuration examples and natural-language prompt samples for normal users. Newline JSON-RPC can be shown as a lightweight diagnostic path for Hermes-style clients.
```

Highlighted tools:

- `foliospace.client_info`
- `foliospace.home`
- `foliospace.search_books`
- `foliospace.open_book_manifest`
- `foliospace.list_games`
- `foliospace.open_game_manifest`
- `foliospace.get_preferences`
- `foliospace.save_preferences`
- `foliospace.get_private_state`
- `foliospace.save_private_state`
- `foliospace.scan_library`
- `foliospace.list_jobs`
- `foliospace.job_events`
- `foliospace.pause_job`
- `foliospace.cancel_job`
- `foliospace.resume_job`
- `foliospace.list_errors`
- `foliospace.library_health`

Example MCP JSON-RPC:

```json
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"foliospace.client_info","arguments":{}}}
```

```json
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"foliospace.open_book_manifest","arguments":{"bookId":12}}}
```

```json
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"foliospace.pause_job","arguments":{"jobId":42}}}
```

## Screenshot Placeholder Plan

Use placeholder blocks until production screenshots are captured. Recommended file names:

| Placeholder | Suggested alt text | Notes |
| --- | --- | --- |
| `screenshots/home-library.png` | FolioSpace Library home with continue reading, favorites, collections, and recent assets. | Main hero screenshot. |
| `screenshots/setup.png` | First-run setup page with access key and directory picker. | Use on install page. |
| `screenshots/cover-wall.png` | Cover wall showing EPUB, comic, and game collections. | Show personal library browsing. |
| `screenshots/epub-reader.png` | EPUB reader with single/double page controls, theme selector, and progress. | Show reading experience. |
| `screenshots/comic-reader.png` | Comic reader streaming archive pages with fullscreen mode. | Show CBZ/ZIP reading. |
| `screenshots/scan-jobs.png` | Scan job list with elapsed time, progress, pause, cancel, and resume controls. | Show transparent scanning. |
| `screenshots/errors.png` | Structured file error list with reason and path context. | Show diagnostics. |
| `screenshots/game-shelf.png` | Game shelf grouped by platform with covers and Now Printing placeholders. | Show ROM library direction. |
| `screenshots/api-mcp.png` | Developer page showing API and MCP examples. | Optional docs visual. |

For placeholder design, keep it product-like:

- Use neutral NAS/admin UI framing, not a marketing-only mock.
- Show actual UI areas where possible.
- Avoid fake copyrighted book/game covers in public marketing assets.
- For game covers without assets, use the existing `Now Printing` placeholder visual language.

## README Content to Reuse

The website can reuse these sections from `README.md`:

- Runtime Layout
- Environment
- Client API v1
- MCP
- Product Direction
- Docker
- Current MVP Support

The README should remain the source of truth for command-level installation details until the website has a docs build pipeline.

## API Docs to Link

Repository docs:

```text
docs/api/client-v1.md
docs/mcp/usage.md
docs/product/foliospace-library-direction.md
```

Suggested website docs URLs:

```text
https://foliospace.app/docs/install
https://foliospace.app/docs/client-api
https://foliospace.app/docs/mcp
https://foliospace.app/docs/roadmap
```

## Feedback Channel Placeholder

Docker Hub does not provide meaningful product feedback. The website should include a feedback destination.

Placeholder options:

- `https://foliospace.app/feedback`
- Gitea/GitHub Issues link when public issue hosting is ready.
- Email placeholder: `feedback@foliospace.app`

Suggested copy:

```text
Early release feedback is welcome. Please include your FolioSpace Library version, Docker platform, mount layout, scan job summary, and any visible error messages. Do not send private access tokens or full private library listings.
```

## Legal / Safety Copy

Use clear local-only wording for game support:

```text
FolioSpace Library indexes and serves metadata for user-owned local ROM files. It does not distribute ROMs, provide download sources, or include copyrighted game content.
```

Use privacy wording:

```text
FolioSpace Library is designed for self-hosted local libraries. Client APIs return service URLs and metadata instead of exposing real NAS file paths.
```

## Current Public Release Facts

Docker Hub image:

```text
funland/foliospace-library:0.82
funland/foliospace-library:latest should be promoted after the 0.82 Docker Hub upload succeeds.
```

Current Docker Hub digest:

```text
0.82 manifest: sha256:4ed3da899eaa795674f2775148eb7359d712a0bee3558f404c787a59a77bc173
amd64:         sha256:833e08b8cba7d1e1a791fc099033854bc67cdfcb932e59756c28b515b99b1c26
arm64:         sha256:17e7779db5bdf4cd1898b59838d376be59bc9d56d89b05d9f7b4f2c18ace5883
```

Architectures:

```text
linux/amd64
linux/arm64
```

Service version returned by API:

```json
{
  "serviceName": "FolioSpace Library",
  "serviceVersion": "0.82",
  "apiVersion": "v1"
}
```

## Open Website Decisions

These should be confirmed before publishing:

- Final logo / icon.
- Whether the website is purely static or backed by a docs framework.
- Final feedback channel.
- Whether API docs are rendered from Markdown directly or rewritten as website pages.
- Whether screenshots are real captures or designed placeholders for the first launch.
- Whether Docker Hub is the only public distribution channel for 0.82.
