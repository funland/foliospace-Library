# FolioSpace Library Client API v1

This document describes the stable HTTP surface intended for native clients such as a Vision Pro reader, GameEMU, and future spatial media clients. The client API is a facade over the current reading routes, so native clients do not need to depend on every web UI endpoint directly.

## Base URL

Use the NAS or test server address as the base URL:

```text
http://your-nas-ip:8080
```

All examples below use relative paths.

## Authentication

Authentication is disabled when `FOLIOSPACE_API_TOKEN` is empty.

When `FOLIOSPACE_API_TOKEN` is set, every `/api/*` route requires one of:

- Native clients: `Authorization: Bearer <token>`
- Web UI: the HttpOnly cookie created by `POST /api/auth/check`

Native clients should use the bearer token. The cookie flow exists mainly so browser-loaded covers, pages, and EPUB iframe resources can work without manually attaching headers to every subresource.

## Profiles And Scoped State

FolioSpace supports multiple in-app profiles inside the same authenticated service. Switching profiles does not require another token or password.

Profile-scoped endpoints accept either:

- `X-FolioSpace-Profile-Id: <profileId>`
- `?profileId=<profileId>`

If the profile id is missing, invalid, deleted, or unknown, the server falls back to the default profile. Older native clients can keep using the v1 API without sending profile information and will continue to read and write the default profile.

Profile-scoped data includes reading progress, continue/recent/favorite/want shelves, private status, rating, tags, notes, summaries, and client preferences. Instance-level data remains shared: libraries, scan jobs, indexed files, metadata, covers, setup, and service authentication.

### `GET /api/profiles`

Returns available profiles.

```json
[
  {
    "id": 1,
    "name": "Default",
    "avatar": "reader",
    "color": "teal",
    "isDefault": true,
    "createdAt": "2026-05-31T12:00:00Z",
    "updatedAt": "2026-05-31T12:00:00Z"
  }
]
```

### `POST /api/profiles`

Creates a profile.

```json
{
  "name": "Guest",
  "avatar": "game",
  "color": "violet"
}
```

### `PUT /api/profiles/{profileId}`

Updates a profile.

```json
{
  "name": "Kids",
  "avatar": "comic",
  "color": "amber"
}
```

`avatar` and `color` are UI metadata for the web profile switcher and native clients. Current built-in avatar ids are `reader`, `comic`, `game`, `movie`, `star`, `archive`, `coffee`, and `rocket`. Current built-in color ids are `teal`, `amber`, `violet`, `rose`, `blue`, `green`, `slate`, and `copper`.

### `DELETE /api/profiles/{profileId}`

Deletes a non-default profile and its scoped state. The default profile cannot be deleted.

### Auth Helpers

#### `GET /api/auth/status`

Public. Returns whether token auth is enabled.

```json
{
  "enabled": true
}
```

#### `POST /api/auth/check`

Public. Checks a token and sets the web auth cookie when valid.

Request:

```json
{
  "token": "secret"
}
```

Response:

```json
{
  "ok": true
}
```

Native clients can skip this endpoint and send `Authorization: Bearer <token>` directly.

#### `POST /api/auth/logout`

Public. Clears the web auth cookie.

```json
{
  "ok": true
}
```

## First-Run Setup

Release `0.91` supports a web-first setup flow for Docker deployments. A fresh `/config` starts uninitialized until it has an access token and at least one configured library.

Environment variable token auth still has priority. If `FOLIOSPACE_API_TOKEN` is set, `POST /api/setup/initialize` must include that token as a bearer token and the setup page treats the token field as the existing deployment token. If `FOLIOSPACE_API_TOKEN` is empty, setup stores the first user-provided token as a SHA-256 hash in SQLite.

### `GET /api/setup/status`

Returns setup state and container-visible directory roots.

```json
{
  "initialized": false,
  "authEnabled": false,
  "hasLibraries": false,
  "tokenConfigured": false,
  "directoryRoots": [
    { "name": "library", "path": "/library" },
    { "name": "books", "path": "/books" },
    { "name": "games", "path": "/games" }
  ]
}
```

`initialized` is true only when an access token is configured and at least one library exists.

### `POST /api/setup/initialize`

Creates the first library and, when no environment token is configured, saves the first access token.

Request:

```json
{
  "token": "change-me-long-token",
  "name": "Books",
  "rootPath": "/books",
  "assetType": "book"
}
```

`assetType` can be `mixed`, `book`, `comic`, `game`, or `video`.

Response is the created library:

```json
{
  "id": 1,
  "name": "Books",
  "rootPath": "/books",
  "assetType": "book"
}
```

### `GET /api/config/directory-roots`

Returns the container-visible roots used by the setup page and directory picker:

```json
{
  "roots": [
    { "name": "library", "path": "/library" }
  ]
}
```

This endpoint reports container paths, not host/NAS paths. Docker volume mappings decide which host paths are visible.

## Recommended Native Client Flow

1. Call `GET /api/auth/status`.
2. If `enabled` is true, store the token in the platform keychain and send `Authorization: Bearer <token>` on every `/api/*` request.
3. Call `GET /api/client/info` to check server capabilities.
4. Call `GET /api/client/home` for the first screen.
5. Open a book with `GET /api/client/books/{bookId}/manifest`.
6. For CBZ/ZIP, load page image URLs from `pages`.
7. For EPUB, load chapters/resources from `epub.resourceBaseUrl`.
8. Sync paged/legacy progress with `GET /api/books/{bookId}/progress` and `PUT /api/books/{bookId}/progress`. For comic webtoon mode, prefer the structured `GET /api/books/{bookId}/reading-position` and `PUT /api/books/{bookId}/reading-position/webtoon` endpoints described below.
9. Sync private state with `GET/PUT /api/client/books/{bookId}/private-state`.
10. Sync UI language and reader defaults with `GET/PUT /api/client/preferences`.
11. Open a game with `GET /api/client/games/{gameId}/manifest`, then use `fileUrl` only through the service.
12. Open a video with `GET /api/client/videos/{videoId}/manifest`, then use `fileUrl` for direct/Range playback or `hlsUrl` when `playbackMode` is `hls`.

## Covers, Thumbnails, And Cache Compatibility

Clients should treat every returned `coverUrl`, `thumbnailUrl`, page URL, EPUB resource URL, game `fileUrl`, and video URL as an opaque service URL. Do not strip query parameters or rebuild the URL from the book id. When auth is enabled, native clients should send the bearer token on the request that loads the image or media bytes. Browser surfaces that must append token auth to an existing URL should append with `&` when the URL already contains `?`.

Book cover and thumbnail URLs may include a client cache-busting query value such as:

```text
/api/books/42/cover?v=v1-cover-refresh-4
/api/books/42/thumbnail?size=small&v=v1-cover-refresh-4
```

That query value is for browser and client cache invalidation only. It is separate from the thumbnail cache algorithm, which remains `v1`. Older clients and integrations can still use the pre-existing routes:

```text
/api/books/42/cover
/api/books/42/thumbnail?size=small
/api/books/42/thumbnail?size=small&v=v1
```

The thumbnail endpoint is a read-through cache. When a JPEG thumbnail is ready, it returns the cached image with private browser caching and an ETag. On a cache miss, the request queues thumbnail generation and returns the best backward-compatible image immediately: the original cover/page image when available, a stale compatible thumbnail when useful, or the built-in generic cover. Fallback responses are marked `no-store` and include `X-FolioSpace-Thumbnail-Fallback` so clients can retry later without caching an intermediate state forever.

PDF covers and PDF thumbnail sources are generated from the first rendered page with `pdftoppm`. The official container image includes `poppler-utils` for that renderer. If rendering is temporarily unavailable or fails, the HTTP response can still fall back to the built-in PDF/generic cover, but the failed PDF thumbnail job is not stored as a ready cache entry; later requests can retry and replace the fallback once rendering works. This is backward-compatible at the API level because the resource remains an image URL, but clients should not hard-code one content type for PDF covers; a PDF cover can now be `image/jpeg` instead of the older SVG placeholder.

Legacy REST endpoints keep their existing fields and response shapes. Some book responses now include an additive `thumbnailUrl` and `thumbnailStatus` to help the web UI and older integrations pick up the refreshed cache version without changing the endpoint they call. The client-safe `/api/client/*` facade still omits local NAS paths such as `filePath`, `rootPath`, and `directoryPath`.

## Client Endpoints

### `GET /api/client/info`

Returns stable client capability metadata.

Response:

```json
{
  "serviceName": "FolioSpace Library",
  "serviceVersion": "0.996",
  "apiVersion": "v1",
  "supportedFormats": ["cbz", "zip", "epub", "pdf", "mp4", "m4v", "mov", "mkv", "avi", "webm", "nes", "sfc", "smc", "vb", "vboy", "gba", "gb", "gbc", "nds", "3ds", "cci", "cxi", "cia", "z64", "v64", "n64", "gdi", "cdi", "chd", "iso", "bin", "cue", "ccd", "toc", "m3u", "cso", "gcm", "rvz", "7z", "dosz", "exe", "com", "bat", "d88", "fdi", "thd", "nhd", "hdi", "vhd", "py1"],
  "capabilities": {
    "clientHome": true,
    "unifiedManifest": true,
    "progressSync": true,
    "epubStreaming": true,
    "pdfStreaming": true,
    "pdfPageLayout": true,
    "pdfWebtoonLayout": true,
    "comicWebtoonLayout": true,
    "webtoonPositionSync": true,
    "compactReader": true,
    "pageStreaming": true,
    "pageImageDownsample": true,
    "bookCatalog": true,
    "collectionCatalog": true,
    "gameShelf": true,
    "gameCatalog": true,
    "videoCatalog": true,
    "videoHls": true,
    "privateState": true,
    "search": true,
    "preferences": true,
    "profiles": true,
    "bearerTokenAuth": true,
    "setupWizard": true,
    "scannerJobEvents": true,
    "scannerJobControl": true,
    "scanSettings": true,
    "recentScan": true,
    "gameSaveSync": true,
    "gamePlayStats": true,
	"gamePlayedCatalog": true,
	"gameMetadataProviders": true,
	"gameLaunchResolver": false,
	"stableRuntimeIdentityV1": false,
	"dosArchiveLaunchV1": true
  }
}
```

PDF clients should read the manifest through `GET /api/client/books/{bookId}/manifest`, then fetch the PDF through the opaque page URL at `GET /api/books/{bookId}/pages/0`. The server supports HTTP Range requests for that URL, so native clients can stream PDF data without exposing the NAS path. `pdfPageLayout` means clients may offer single-page and two-page spread modes on top of the same PDF stream. `pdfWebtoonLayout` and `comicWebtoonLayout` mean clients may also offer vertical continuous scrolling when a manifest includes `webtoon` in `readerModes`. `webtoonPositionSync` means the structured `reading-position/webtoon` endpoints are available. `pageImageDownsample` means archive image pages can be requested with `maxWidth` and client manifests include `displayUrl` for memory-safe mobile/tablet rendering. `compactReader` means the bundled web UI has a phone-oriented compact reader, but native clients can still implement their own layout.

### `GET /api/client/preferences`

Returns server-side client preferences. Web currently uses local storage only as a first-paint fallback, then reconciles from this API.

Response:

```json
{
  "locale": "zh",
  "readerPageMode": "single",
  "epubPageMode": "single",
  "epubTheme": "light",
  "epubFontSize": 18
}
```

Fields:

- `locale`: `zh`, `zht`, `en`, `ja`, or `ko`.
- `readerPageMode`: `single`, `double`, or `webtoon` for image/PDF readers. `webtoon` means vertical continuous scrolling for long-strip comics or PDF/image documents.
- `epubPageMode`: `single` or `double`.
- `epubTheme`: `light`, `sepia`, or `dark`.
- `epubFontSize`: integer, normalized to `14...26`.

### `PUT /api/client/preferences`

Saves client preferences and returns the normalized value.

Request:

```json
{
  "locale": "zht",
  "readerPageMode": "webtoon",
  "epubPageMode": "double",
  "epubTheme": "dark",
  "epubFontSize": 24
}
```

Response is the same shape as `GET /api/client/preferences`.

### `GET /api/client/home`

Returns the data needed for a native home screen in one request.

Query:

- `limit`: optional, default `12`, max `50`. Applies to `continueReading`, `recentBooks`, `favoriteBooks`, and `wantToRead`.
- `gameShelf` uses the same limit and returns recent local ROM assets.
- `videoShelf` uses the same limit and returns recent local video assets.

Response:

```json
{
  "continueReading": [
    {
      "id": 42,
      "collectionId": 7,
      "collectionTitle": "Series A",
      "title": "Volume 01",
      "bookType": "single_volume",
      "format": "cbz",
      "pageCount": 180,
      "coverStatus": "ready",
      "coverUrl": "/api/books/42/cover",
      "currentPage": 16,
      "progressFraction": 0.09,
      "privateStatus": "reading",
      "favorite": true,
      "rating": 4,
      "tags": ["vision", "spatial"],
      "summary": "Vision Pro candidate"
    }
  ],
  "recentBooks": [],
  "favoriteBooks": [],
  "wantToRead": [],
  "gameShelf": [
    {
      "id": 12,
      "assetType": "game",
      "title": "Super Mario World",
      "platform": "snes",
      "romSetName": "SNES",
      "region": "USA",
      "format": "sfc",
      "size": 524288,
      "crc32": "b19ed489",
      "sha1": "0123456789abcdef0123456789abcdef01234567",
      "emulatorHint": "snes",
      "compatibility": "unknown",
      "coverUrl": "/api/games/12/cover",
      "manifestUrl": "/api/client/games/12/manifest"
    }
  ],
  "videoShelf": [
    {
      "id": 21,
      "assetType": "video",
      "title": "Demo Movie",
      "format": "mp4",
      "size": 104857600,
      "durationSeconds": 0,
      "width": 0,
      "height": 0,
      "thumbnailStatus": "placeholder",
      "thumbnailUrl": "/api/videos/21/thumbnail",
      "manifestUrl": "/api/client/videos/21/manifest",
      "directPlayable": true,
      "playbackMode": "direct",
      "fileUrl": "/api/client/videos/21/file",
      "hlsUrl": "/api/client/videos/21/hls/index.m3u8",
      "transcodeStatusUrl": "/api/client/videos/21/transcode/status"
    }
  ],
  "collections": [
    {
      "id": 7,
      "title": "Series A",
      "collectionType": "directory",
      "primaryType": "book",
      "bookCount": 12,
      "coverBookId": 42,
      "thumbnailStatus": "pending",
      "thumbnailUrl": "/api/books/42/thumbnail?size=small&v=v1-cover-refresh-4",
      "favorite": true,
      "liked": false
    }
  ]
}
```

Collection `coverBookId`, `thumbnailStatus`, and `thumbnailUrl` are additive optional fields. Clients can use them to render collection covers from the first response without calling the collection volumes endpoint first. Older servers may omit them, so clients should keep a local fallback for missing values.

The client DTO intentionally omits local NAS paths such as `filePath`, `rootPath`, and `directoryPath`.

### `GET /api/client/books`

Returns a paginated client-safe catalog of all readable book, comic, EPUB, and PDF entries. Use this endpoint for native `All` or full library overview screens instead of trying to merge the limited shelves from `/api/client/home`.

Query:

- `limit`: optional, default `60`, max `200`.
- `offset`: optional, default `0`.
- `q`: optional text filter against book title.
- `sort`: optional. Supported values are `title`, `recently_added`, `recent`, `last_read`, `progress`, and `unread`. Unknown values fall back to `title`.
- `direction`: optional, `asc` or `desc`. Unknown values fall back to `asc`; recency/progress sorts keep their existing default descending behavior when omitted.
- `format`: optional exact format filter such as `cbz`, `zip`, `epub`, or `pdf`. Use `all` or omit it to include every readable format.

Example:

```http
GET /api/client/books?limit=60&offset=0&sort=title&direction=asc&format=all
```

Response:

```json
{
  "items": [
    {
      "id": 42,
      "collectionId": 7,
      "seriesId": 7,
      "collectionTitle": "Demo Series",
      "title": "Volume 01",
      "creator": "Author",
      "bookType": "comic",
      "format": "cbz",
      "pageCount": 180,
      "coverStatus": "ready",
      "coverUrl": "/api/books/42/cover?v=v1-cover-refresh-4",
      "thumbnailStatus": "ready",
      "thumbnailUrl": "/api/books/42/thumbnail?size=small&v=v1-cover-refresh-4",
      "manifestUrl": "/api/client/books/42/manifest",
      "downloadUrl": "/api/client/books/42/file",
      "analyzed": true,
      "addedAt": "2026-06-17T09:00:00Z",
      "updatedAt": "2026-06-17T09:00:00Z",
      "currentPage": 16,
      "progressFraction": 0.42,
      "lastReadAt": "2026-06-17T10:00:00Z",
      "privateStatus": "reading",
      "favorite": true,
      "rating": 0,
      "tags": [],
      "summary": ""
    }
  ],
  "total": 12345,
  "limit": 60,
  "offset": 0,
  "hasMore": true
}
```

Like other `/api/client/*` book DTOs, this response does not expose local NAS file paths. Use `manifestUrl` to open the item and preserve all query parameters on returned cover and thumbnail URLs.

### `GET /api/client/books/{bookId}/file`

Downloads the byte-exact original EPUB, PDF, CBZ, or ZIP file for offline
reading. The route requires the same Bearer token as the other client APIs,
supports HTTP Range and HEAD requests, and returns an attachment filename based
on the source file. Treat `downloadUrl` as opaque and do not reconstruct it from
NAS paths. A missing source file returns `404` instead of blocking or failing a
catalog request.

The book manifest also returns the same URL as top-level `fileUrl`. Clients may
verify downloaded bytes against `contentHash` when its algorithm is `sha256`.

#### Offline identity fields

All book DTOs returned by `/api/client/books`, `/api/client/home`,
`/api/client/search`, `/api/collections/{collectionId}/volumes`, and the
nested `book` object in `/api/client/books/{bookId}/manifest` may include these
additive fields:

```json
{
  "contentHash": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "contentHashAlgorithm": "sha256",
  "fileSize": 12345678,
  "contentRevision": "sha256:..."
}
```

`contentHash` is the lowercase SHA-256 of the complete original EPUB, PDF,
CBZ, or ZIP file. It is not a path hash, sampled hash, or extracted-content
hash. `contentHashAlgorithm` is currently `sha256`. `fileSize` is the original
file size in bytes. `contentRevision` is an opaque server revision for the
offline-readable content; it changes when the source bytes or indexed page
collection changes.

These fields are nullable. The server computes the complete hash in a
serialized background worker, so list and manifest requests never read a whole
large book synchronously. While a hash is pending or has failed, clients must
accept `null` and continue to use the existing book id and manifest URLs.
Scanning a file whose size or modification time changed clears the previous
identity and queues it again. A later scan can retry failed work. Once both
`contentHashAlgorithm == "sha256"` and `contentHash` match, a client may
associate a downloaded copy or manually imported copy with the same local
file. A hash match does not replace the stable server identity
`serverProfileID + remoteBookID`.

Clients should use `contentRevision` to mark an offline copy as stale while
preserving its progress, bookmarks, and annotations. Similar titles, creators,
or ISBNs are only duplicate hints and must not be auto-merged.

### `GET /api/client/games`

Returns a paginated client-safe ROM catalog for Vision Pro, iPad, and GameEMU native clients. Use this endpoint for full game directory browsing instead of the limited `gameShelf` on `/api/client/home`.

Query:

- `limit`: optional, default `50`, max `200`. Values above max are clamped and the response returns the actual limit.
- `offset`: optional, default `0`.
- `q`: optional search against `title`, archive filename/path, `romSetName`, `region`, `platform`, and `format`. A matching dependency-only package may be returned so a native client can resolve runtime files by shortname.
- `platform`: optional exact platform filter, for example `nes`, `snes`, `virtualboy`, `n64`, `gba`, `md`, `neogeo`, `model2`, `arcade`, or `3ds`.
- `romSetName`: optional exact ROM set filter.
- `format`: optional exact format filter, for example `nes`, `sfc`, `gba`, `zip`, or `3ds`.
- `sort`: optional. Supported values are `recent`, `oldest`, `title`, and `platform`. Unknown values fall back to `recent`.

FBNeo console ROM sets are normalized by source system instead of being merged into `arcade`: `FBNeo/megadrive` returns `md`, `FBNeo/snes` returns `snes`, `FBNeo/nes` returns `nes`, and known Neo Geo shortnames in FBNeo return `neogeo`.

Response:

```json
{
  "items": [
    {
      "id": 18,
      "assetType": "game",
      "title": "Super Contra",
      "platform": "nes",
      "romSetName": "NES",
      "region": "Japan",
      "format": "nes",
      "fileName": "Super Contra.nes",
      "size": 262160,
      "crc32": "9bb6059e",
      "sha1": "5de393e3ad83e6e185e6d338684d7a4475b7d2ce",
      "emulatorHint": "nes",
      "compatibility": "unknown",
      "favorite": false,
      "liked": false,
      "coverUrl": "/api/games/18/cover",
      "manifestUrl": "/api/client/games/18/manifest",
      "downloadUrl": "/api/client/games/18/file"
    }
  ],
  "total": 128,
  "limit": 50,
  "offset": 0,
  "hasMore": true
}
```

Empty results return `items: []` with `total: 0`; the endpoint does not return 404 for an empty catalog. The `items` DTO is the same client-safe game DTO used by `gameShelf`, and never includes NAS paths, local file paths, or Docker volume paths.

The client catalog (`/api/client/games`, `/api/client/games/facets`, client search, and played-game lists) separates discovery from runtime certification. Records marked `needs-curation` remain visible and retain that `catalogRole`, so a missing launch profile does not make a user's game disappear. Records marked `dependency` remain hidden because they are not independently launchable. Clients must not interpret catalog visibility as proof that a strict runtime profile exists.

### `GET /api/client/games/facets`

Returns the canonical full-catalog list of **currently indexed, discoverable game platforms** for native ROM browsers. Use this endpoint to build system filters before or alongside paged `/api/client/games` loading; do not derive filter options from a single page of results. Facet counts can include `needs-curation` records and therefore describe discovery, not resolver certification.

Query:

- `q`: optional search term. When present, facets are computed for matching games.
- `platform`: optional exact platform filter. Comma-separated values are accepted for native clients that group multiple raw platforms under one console.
- `romSetName`: optional exact ROM set filter.
- `format`: optional exact format filter.

Response:

```json
{
  "total": 1800,
  "platforms": [
    {
      "platform": "snes",
      "romSetName": "SNES",
      "format": "sfc",
      "emulatorHint": "snes",
      "count": 1400
    },
    {
      "platform": "cps2",
      "romSetName": "",
      "format": "zip",
      "emulatorHint": "fbneo",
      "count": 37
    }
  ]
}
```

The response is aggregate-only and never includes NAS paths, local file paths, or Docker volume paths.

Facets contain exactly one entry per normalized `platform`. Its `count` is the number of launchable game records and therefore matches `GET /api/client/games?platform={platform}`; dependency files such as Dreamcast GDI tracks and Saturn CUE tracks never contribute to the count. When one platform contains multiple ROM-set names, formats, or emulator hints, the corresponding aggregate field is an empty string instead of producing duplicate platform rows.

This is an inventory endpoint, not a declaration of every platform the server could recognize. A platform with no indexed launchable ROMs is omitted. Use the platform catalog below when a client needs the complete server-owned list.

### `GET /api/client/games/platforms`

Returns the server-owned game platform catalog. Clients should use this endpoint to build platform menus instead of maintaining a hard-coded platform allow-list. Unlike facets, it includes supported platforms that currently have no indexed games. Newly observed canonical platforms that are not yet in the declared catalog are appended automatically, so they remain discoverable by generic clients.

Response:

```json
{
  "items": [
    {
      "platform": "virtualboy",
      "title": "Virtual Boy",
      "aliases": ["virtual-boy", "virtual boy"],
      "count": 71,
      "available": true
    },
    {
      "platform": "ps2",
      "title": "PlayStation 2",
      "aliases": ["playstation-2"],
      "count": 0,
      "available": false
    }
  ],
  "total": 30
}
```

Use `platform` as the value sent to `GET /api/client/games?platform=...`; `title` is display text, and aliases are normalization hints only. `count` describes client-visible indexed games and `available` is equivalent to `count > 0`. The `gamePlatformCatalog` capability in `/api/client/info` indicates that this endpoint is available. Older servers without that capability can continue to use `/api/client/games/facets`.

Sega Model 2 scans use `platform: "model2"`, the ZIP filename stem as `romSetName`, `format: "zip"`, `emulatorHint: "model2"`, and `inputProfile: "operatorArcade"`. Known sets receive their MAME display title. Archive size, CRC32, SHA-1, and download bytes describe the original ZIP container. The `segabill.zip` firmware package remains stored as `catalogRole: "dependency"` for audited profile dependency closure, but it is not returned by client search, catalog pages, or facets. An exact MAME 0.288 listxml audit promotes compatible entries to `game`; failed parent/device/BIOS closures remain discoverable as `needs-curation` without being deleted or falsely certified.

CPS and Neo Geo sets verified against the configured official FBNeo Arcade DAT use canonical `platform: "cps1"`, `"cps2"`, `"cps3"`, or `"neogeo"`, the DAT set name as `romSetName`, and `emulatorHint: "fbneo"`. The audit checks every non-merged ROM entry by logical name, uncompressed size, and CRC, then verifies the complete `romof` parent/BIOS closure. Ready Windows 1.302 profiles require Libretro core `fbneo` with SHA-256 `6ebc2675c272c8d654935647ac336d45bbd97452c4d5943290d5ffc75678d9f1`. Rejected archives remain `needs-curation` and discoverable, but they are not promoted to an audited launch profile until a matching profile is rebuilt.

Canonical MAME ZIP/7Z records use the physical file stem as `romSetName`, never a collection label such as `MAME`. Windows MAME 0.288 profiles are audited for `hypreact`, `hypreac2`, `srmp4`, `fromancr`, `fromanc4`, and `mcnpshnt`. The physical `ym2413_instruments.zip` asset is stored with `catalogRole: "dependency"`, omitted from ordinary pages and facets, and exposed to `mcnpshnt` only as the logical profile filename `ym2413.zip`; the NAS file is never renamed.

New Dreamcast scans use `platform: "dreamcast"`, `romSetName: "DC"`, and `emulatorHint: "dreamcast"`. Existing records with `platform: "disc"` remain queryable for backward compatibility, but are not emitted for newly recognized Dreamcast games.

New Saturn CUE scans use `platform: "saturn"`, `romSetName: "SS"`, `emulatorHint: "saturn"`, and `format: "cue"`. A single-file Saturn ISO remains one standalone game.

PC-FX scans use `platform: "pc-fx"`, `romSetName: "PC-FX"`, and `emulatorHint: "pcfx"`. CUE, CCD, TOC, CHD, and M3U files are launch entries; raw BIN/IMG/ISO tracks and `pcfx.rom` are never standalone catalog items. Adjacent `CD1`/`CD2` directories are exposed as one virtual M3U game, so facets count launchable titles rather than physical discs. Pegasus `metadata.pegasus.txt` files may provide title, description, developer, and `ignore-file` data.

Nintendo 64 scans use `platform: "n64"`, `romSetName: "Nintendo 64"`, `emulatorHint: "mupen64plus"`, and `inputProfile: "standard"`. Raw `.z64`, `.v64`, and `.n64` files are validated by their byte-order header. If a legacy ROM has the wrong extension, the header remains authoritative and the catalog/download filename is normalized to the matching extension without changing the source file. A ZIP is treated as N64 only when its library/path identifies N64 and it contains exactly one valid raw ROM candidate; the catalog and download endpoint expose that raw entry's normalized filename, format, size, CRC32, SHA-1, and bytes rather than the ZIP container. ZIPs with zero or multiple candidates, unsafe paths, invalid ROM headers, or size-limit violations are reported as scan errors. Historical `nintendo64` and `nintendo 64` records are normalized to the canonical `n64` facet.

Virtual Boy scans use `platform: "virtualboy"`, `romSetName: "Virtual Boy"`, `emulatorHint: "virtualfriend"`, and `inputProfile: "standard"`. Raw `.vb` and `.vboy` files are supported. A ZIP containing exactly one such ROM is indexed as one game and exposes the inner ROM name, size, checksums, and bytes through its manifest; the ZIP filename alone never overrides the inner cartridge format. Covers are matched lazily against Libretro's `Nintendo - Virtual Boy` boxart catalog and cached by FolioSpace.

Nintendo DS scans use `platform: "nds"`, `romSetName: "Nintendo DS"`, `format: "nds"`, `emulatorHint: "melonds-ds"`, and `inputProfile: "standard"`. Each `.nds` file is one single-entry game manifest. BIOS, firmware, DSi NAND, and unrelated sibling files are never indexed or packaged. Resolver matching requires the exact Libretro `melonds-ds` core and is limited to physical iOS, iPadOS, and visionOS clients that explicitly report that runtime; desktop melonDS, tvOS, simulators, and generic archive runtimes are not substituted.

Nintendo 3DS scans use `platform: "3ds"`, `romSetName: "Nintendo 3DS"`, `emulatorHint: "spatialemu-3ds-companion"`, and `inputProfile: "standard"`. Direct `.3ds`/`.cci` NCSD images and `.cxi` NCCH images are validated at header offset `0x100`; the maximum image size is 8 GiB. A ZIP is accepted only when a 3DS library/path identifies the platform and the safe archive contains exactly one validated direct image. Its manifest and authenticated file endpoints expose the inner image name, uncompressed size, checksums, full bytes, and byte ranges rather than the outer ZIP. `.cia` is classified as `contentMode: "install"` and remains `needs-curation`; direct images and accepted ZIPs use `contentMode: "launch"`. The server does not advertise CIA installation capability, upload keys, firmware, NAND, saves, or other device material.

3DO scans use `platform: "3do"`, `romSetName: "3DO"`, `emulatorHint: "opera"`, and `inputProfile: "standard"`. Platform assignment comes from a 3DO library root or explicit platform metadata rather than the shared `.cue`, `.iso`, or `.chd` extensions alone. A CUE is published as one game with all referenced tracks in its ordered manifest; dependency lookup is case-insensitive on disk while logical filenames preserve the CUE declarations. Referenced tracks and known 3DO BIOS files are never published as standalone games or included as game dependencies. Resolver matching requires a client-reported Libretro `opera` core; catalog browsing remains available when the client does not bundle Opera.

PSP scans use `platform: "psp"`, `romSetName: "PSP"`, `emulatorHint: "ppsspp"`, and `inputProfile: "standard"`. PSP `.iso` and `.cso` images are exposed as single-file manifests.

Nintendo GameCube scans use `platform: "ngc"`, `romSetName: "NGC"`, `emulatorHint: "dolphin"`, and `inputProfile: "standard"`. GameCube `.iso`, `.gcm`, and `.rvz` images are exposed as single-file manifests.

PlayStation 2 scans use `platform: "ps2"`, `romSetName: "PS2"`, `emulatorHint: "pcsx2"`, and `inputProfile: "standard"`. PS2 `.iso` and `.chd` images are exposed as single-file manifests.

Konami Python 1 scans use `platform: "konami-python1"`, `romSetName: "KONAMI-PYTHON1"`, `format: "py1"`, `emulatorHint: "pcsx2-reliquary"`, and `inputProfile: "standard"`. A `.py1` descriptor is the only catalog entry. Its manifest preserves the seven relative dependency paths declared by `CfImagePath`, `BbsRamPath`, `IoBootRomPath`, `IoConfigRomPath`, `InternalDonglePath`, `MemoryCardDonglePath`, and `MemoryCardIdPath`; dependency CHDs, ROMs, dongles, global keys, and COH-H BIOS files are never published as standalone games. All eight files include size and SHA-1. Launch resolution requires a Windows or macOS client reporting `pcsx2-reliquary` version `1.5.1.0` or newer with `contentSet: "konami-python1"`; it never falls back to ordinary PCSX2.

IBM-compatible DOS archive scans use `platform: "dos"`, `romSetName: "DOS"`, and `emulatorHint: "dosbox-staging"`. ZIP and DOSZ archives remain one catalog item and are never extracted or executed by the server. When a nearby controlled `games.json` matches the exact archive by SHA-256 plus byte size, FolioSpace imports its localized title, release year, key help, cover, curated launch command, and optional virtual DOS `installDirectory`. An install directory is one validated DOS directory name, never a host path. Archive candidates and paths are bounded, normalized, path-traversal-safe, and unique under case-insensitive comparison. PC-98 remains a separate `pc98` platform.

PC-98 scans use `platform: "pc98"`, `romSetName: "PC-98"`, `emulatorHint: "np2kai"`, and `inputProfile: "standard"`. Supported floppy and hard-disk images are validated by known geometry/header rules before indexing. ZIP entry names marked as non-UTF-8 are decoded as CP932/Shift-JIS, and generic internal names such as `1.D88` do not replace a meaningful container or parent-directory title. A ZIP is accepted only when it contains exactly one validated PC-98 media candidate; ZIPs with multiple media candidates still require manual review.

The scanner computes CRC32 and SHA-1 over the raw image, not the ZIP container. Byte-identical mirrors are represented by one catalog record while the backend retains every physical source for incremental scans and recovery. Floppy files in the same parent directory with explicit disk suffixes such as `Disk 1`, `Disk 2`, `Data 1`, `FD 2`, or their Japanese equivalents are grouped into one launchable game. Catalog `size` is the sum of every delivered package file. A multi-floppy manifest exposes the ordered media as one `entry` followed by `disk` files with zero-based contiguous `diskIndex` values; the first floppy also includes `driveHint: "FDD1"`.

A same-directory `FONT.bmp` or `PC98_CN.bmp` is packaged only when it is a valid uncompressed 2048 x 2048 1-bit BMP. It is returned as `role: "font"`, contributes to package `size`, remains beside the downloaded entry image, and never receives `diskIndex` or `driveHint`. Invalid bitmaps and fonts under firmware, emulator, tools, cache, or DOS roots remain excluded. Firmware, emulator binaries, DOS support roots, unsafe archive paths, symlinks, encrypted entries, and archive-bomb patterns are rejected or excluded. RAR, `.7z`, and TAR containers remain outside the PC-98 contract.

## Game Catalog Administration

The following authenticated routes back the web game-curation workflow. They are administration APIs, not native-client catalog routes. Native clients should continue to use `/api/client/games`, `/api/client/games/facets`, manifests, and launch resolution; they never need NAS policy paths or curation records.

### `GET` / `PUT /api/settings/game-catalog`

Reads or replaces the instance-level catalog pipeline configuration:

```json
{
  "autoAnalyzeAfterScan": true,
  "enableLibretroCovers": true,
  "fbneoDatPath": "/config/policies/fbneo-arcade.dat",
  "mameListXmlPath": "/config/policies/mame0288lx.zip",
  "fbneoTargetsPath": "/config/policies/fbneo-mobile-targets.json",
  "mameTargetsPath": "/config/policies/mame-mobile-targets.json",
  "launchTargetsPath": "/config/policies/targets.json",
  "mamePlatforms": "arcade,mame,model2,cps,cps1,cps2,cps3,neogeo",
  "metadataProvider": "local"
}
```

`metadataProvider` is `local`, `hasheous`, or `disabled`. `local` is the default and performs no Internet requests. Hasheous is opt-in and uses stable ROM hashes; it is never required for scanning or launch-profile resolution. FBNeo and MAME use separate target files because their runtime fingerprint policies differ. `launchTargetsPath` remains a backward-compatible fallback for installations that still use one legacy target file. A missing policy file is reported in curation status rather than causing the service to fail startup.

### `GET /api/games/curation`

Returns all administrative game records, including dependencies and `needs-curation` entries. Query parameters are `limit`, `offset`, `q`, `platform`, `state`, and `sort`. `state` accepts catalog roles such as `game`, `needs-curation`, or `dependency`. Each item includes metadata/artwork state, ready profile count, `fileCount`, `checksummed`, `mobileReady`, and an actionable issue code such as `identity-missing`, `policy-pack-missing`, `dependency-missing`, or `manifest-checksum-unavailable`.

Use `GET /api/games/curation?summary=1` for aggregate counts, including `fileCount`, `checksummed`, and `checksumPending`, installed-policy status, and the latest background task. `GET /api/games/curation/task` returns the persisted task status directly.

### `POST /api/games/curation/checksums`

Starts one exclusive, bounded checksum-backfill task for canonical launch files. The optional request body is `{"limit": 100}`; the default is 100 and the maximum is 500. Pass `{"gameId": 18726, "limit": 32}` to repair one blocked game's files without hashing unrelated games. Files are processed sequentially, source identity is checked before and after reading, and a checksum is committed only if path, size, and modification time still match the indexed record. Repeat the global task while `checksumPending` is greater than zero. The resolver does not hash files synchronously.

### `POST /api/games/curation/analyze`

Starts one exclusive background classification and compatibility-analysis task. It normalizes dependency and curation roles, then runs the installed FBNeo DAT and MAME listxml policies through the packaged launch-profile rebuild tool. It returns `202 Accepted`; a concurrent request returns `409 Conflict` instead of creating duplicate workers.

### `POST /api/games/curation/covers`

Starts bounded batch artwork matching:

```json
{"includeNetwork": false}
```

Local matching checks same-name sidecars, common `covers`/`boxarts`/`images` directories, and `media/<ROM name>/boxFront.*`. Set `includeNetwork` only when the administrator has enabled Libretro fallback and accepts network requests. The task does not modify ROM files.

### `GET /api/games/{gameId}` and `PUT /api/games/{gameId}/metadata`

The first route returns the administrative game record, normalized metadata, metadata sources, and artwork candidates. The second stores a manual correction using the existing metadata model. `POST /api/games/{gameId}/metadata/refresh` invokes the configured provider; `POST /api/games/{gameId}/metadata/select-match` selects one ambiguous source candidate. Manual data remains usable when all network providers are disabled.

### `GET /api/client/videos`

Returns a paginated client-safe video catalog. FolioSpace keeps NAS paths hidden, probes codecs with `ffprobe` when available, and marks each video as direct-playable or HLS-transcode playback.

Query:

- `limit`: optional, default `50`, max `200`.
- `offset`: optional, default `0`.
- `q`: optional search against `title`, `relPath`, and `format`.
- `format`: optional exact format filter, for example `mp4`, `mov`, or `mkv`.
- `sort`: optional. Supported values are `recent` and `title`. Unknown values fall back to `recent`.

Response:

```json
{
  "items": [
    {
      "id": 21,
      "assetType": "video",
      "title": "Demo Movie",
      "format": "mp4",
      "size": 104857600,
      "durationSeconds": 0,
      "width": 0,
      "height": 0,
      "thumbnailStatus": "placeholder",
      "thumbnailUrl": "/api/videos/21/thumbnail",
      "manifestUrl": "/api/client/videos/21/manifest",
      "directPlayable": true,
      "playbackMode": "direct",
      "fileUrl": "/api/client/videos/21/file",
      "hlsUrl": "/api/client/videos/21/hls/index.m3u8",
      "transcodeStatusUrl": "/api/client/videos/21/transcode/status"
    }
  ],
  "total": 1,
  "limit": 50,
  "offset": 0,
  "hasMore": false
}
```

### `GET /api/client/videos/{videoId}/manifest`

Returns client-safe video playback metadata. It does not expose the real NAS path.

```json
{
  "video": {
    "id": 21,
    "assetType": "video",
    "title": "Demo Movie",
    "format": "mp4",
    "size": 104857600,
    "durationSeconds": 0,
    "width": 0,
    "height": 0,
    "thumbnailStatus": "placeholder",
    "thumbnailUrl": "/api/videos/21/thumbnail",
    "manifestUrl": "/api/client/videos/21/manifest",
    "directPlayable": false,
    "playbackMode": "hls",
    "playbackReason": "container or codecs need browser transcode",
    "fileUrl": "/api/client/videos/21/file",
    "hlsUrl": "/api/client/videos/21/hls/index.m3u8",
    "transcodeStatusUrl": "/api/client/videos/21/transcode/status"
  },
  "fileUrl": "/api/client/videos/21/file",
  "hlsUrl": "/api/client/videos/21/hls/index.m3u8",
  "transcodeStatusUrl": "/api/client/videos/21/transcode/status"
}
```

`fileUrl` streams the local file through FolioSpace Library using `http.ServeFile`, so clients can use HTTP Range requests while keeping NAS paths hidden.

If `playbackMode` is `hls`, clients should open `hlsUrl`. The first request to `hlsUrl` starts an on-demand `ffmpeg` transcode into `/config/cache/video-transcodes`; subsequent playback reuses the cached HLS playlist and segments until the source file changes. The built-in transcoder keeps one active video transcode at a time and downscales wide 4K sources to 1080p H.264/AAC HLS for NAS-friendly playback.

While HLS is still being generated, timeline seeking is limited to segments that already exist. Once the playlist is fully cached, clients can seek normally within the generated HLS output. If another video is already occupying the single transcode slot, the HLS playlist request returns `409` and clients should poll the per-video and global status endpoints below.

### `GET /api/client/videos/{videoId}/transcode/status`

Returns the current HLS cache/transcode state for a video.

```json
{
  "videoId": 21,
  "status": "running",
  "message": "Transcoding to browser-compatible HLS",
  "segmentCount": 8
}
```

`status` is one of `idle`, `starting`, `running`, `queued`, `ready`, or `failed`. Clients can poll this endpoint while opening HLS playback to show `转码中`, `已缓存`, or a failure state. If another video is already being transcoded, the HLS playlist request can return `409` and this endpoint reports `queued`.

### `GET /api/client/videos/transcode/status`

Returns the active global HLS transcode task. Use this when a selected video reports `queued` and the client wants to show which video is currently occupying the single NAS-friendly transcode slot.

```json
{
  "status": "running",
  "activeVideoId": 88,
  "activeTitle": "Demo 4K HEVC Movie",
  "segmentCount": 12,
  "message": "Transcoding to browser-compatible HLS"
}
```

If nothing is currently transcoding, `status` is `idle`.

### `GET /api/videos/{videoId}/thumbnail`

Returns the best available video thumbnail without exposing the NAS path. FolioSpace first looks for local sidecar images next to the video, including `Movie.jpg`, `Movie.poster.jpg`, `Movie.cover.jpg`, `poster.jpg`, and `cover.jpg`. If no local image exists, it extracts a cached JPEG frame with `ffmpeg` into `/config/cache/video-thumbnails`. If extraction is busy or unavailable, it falls back to the built-in SVG placeholder.

### `GET /api/client/books/{bookId}/manifest`

Returns all stable metadata needed to open one book.

#### CBZ/ZIP Response

```json
{
  "book": {
    "id": 42,
    "collectionId": 7,
    "collectionTitle": "Series A",
    "title": "Volume 01",
    "bookType": "single_volume",
    "format": "cbz",
    "pageCount": 180,
    "coverStatus": "ready",
    "coverUrl": "/api/books/42/cover",
    "currentPage": 16,
    "progressFraction": 0.09,
    "privateStatus": "reading",
    "favorite": true,
    "rating": 4,
    "tags": ["vision", "spatial"],
    "summary": "Vision Pro candidate"
  },
  "format": "cbz",
  "coverUrl": "/api/books/42/cover",
  "readerModes": ["single", "double", "webtoon"],
  "defaultReaderMode": "single",
  "progress": {
    "bookId": 42,
    "pageIndex": 16,
    "locator": "",
    "progressFraction": 0.09
  },
  "pages": [
    {
      "index": 0,
      "name": "001.jpg",
      "pageKey": "archive:001.jpg",
      "url": "/api/books/42/pages/0",
      "displayUrl": "/api/books/42/pages/0?maxWidth=1200"
    }
  ]
}
```

Use `pages[index].displayUrl` for normal comic reading surfaces, especially phones, tablets, and webtoon/vertical-scroll mode. It points at a server-downsampled image that limits decoded client memory. Use `pages[index].url` when the client explicitly needs the original page bytes. Returned page URLs are relative to the same base URL and still require bearer auth when auth is enabled.

`pageKey` is the stable page identity used by structured webtoon progress. For archive-backed comics it is `archive:{entry-name}` and should be preferred over numeric indexes when restoring scroll position after rescans, client changes, or device changes.

#### EPUB Response

```json
{
  "book": {
    "id": 84,
    "collectionId": 9,
    "collectionTitle": "Books",
    "title": "Sample EPUB",
    "bookType": "single_volume",
    "format": "epub",
    "pageCount": 12,
    "coverStatus": "ready",
    "coverUrl": "/api/books/84/cover",
    "currentPage": 3,
    "progressFraction": 0.25,
    "privateStatus": "want",
    "favorite": false,
    "rating": 0,
    "tags": [],
    "summary": ""
  },
  "format": "epub",
  "coverUrl": "/api/books/84/cover",
  "readerModes": ["single"],
  "defaultReaderMode": "single",
  "progress": {
    "bookId": 84,
    "pageIndex": 3,
    "locator": "OPS/text/chapter1.xhtml",
    "progressFraction": 0.25
  },
  "epub": {
    "title": "Sample EPUB",
    "creator": "Author",
    "coverHref": "OPS/images/cover.jpg",
    "spine": [
      {
        "index": 0,
        "id": "chapter1",
        "href": "OPS/text/chapter1.xhtml",
        "mediaType": "application/xhtml+xml"
      }
    ],
    "toc": [
      {
        "label": "Chapter 1",
        "href": "OPS/text/chapter1.xhtml",
        "index": 0
      }
    ],
    "resourceBaseUrl": "/api/books/84/epub/resources/",
    "coverUrl": "/api/books/84/cover"
  }
}
```

Load EPUB resources by appending the percent-encoded resource path to `resourceBaseUrl`.

Example:

```text
/api/books/84/epub/resources/OPS/text/chapter1.xhtml
```

#### Reader Modes

Every book manifest includes `readerModes` and `defaultReaderMode` so clients do not need to infer supported layouts from file extensions alone.

- `single`: one page at a time.
- `double`: two-page spread where the client has enough screen space.
- `webtoon`: vertical continuous scrolling for long-strip comics or PDF/image documents.

For CBZ/ZIP comics, clients should keep mode-specific progress separate. `single` and `double` can continue to use the legacy page-based progress endpoint. `webtoon` should use `webtoon-position-v1`, whose true position is a content anchor inside one page image rather than a global `scrollTop` value.

Current defaults are conservative:

- EPUB: `readerModes: ["single"]`.
- CBZ/ZIP/PDF: `readerModes: ["single", "double", "webtoon"]`.
- `defaultReaderMode` is currently `single` for all formats. Future metadata or user preferences may choose `webtoon` automatically for known long-strip works.

### `POST /api/client/games/{gameId}/resolve`

Resolves a stable launch profile for the exact client and emulator runtime inventory reported in the request body. This endpoint is additive: older clients continue using `GET /api/client/games/{gameId}/manifest` unchanged.

Resolution is risk-tiered:

- Ordinary console and computer platforms reuse the existing validated canonical manifest. Every returned launch file has a persisted SHA-1; the resolver never computes large-file hashes synchronously.
- Ordinary console and computer runtimes do not require a hash of the containing application. PSP uses normalized core id `ppsspp`; DOS uses `dosboxpure` (the input alias `dosbox-pure` is accepted). Desktop standalone PPSSPP 1.20.4+ and DOSBox Staging 0.82.2+ remain supported.
- Curated DOS packages reuse their existing `dosLaunch` object. An ambiguous or unknown inner executable is not promoted to a launchable profile.
- MAME and FBNeo remain strict: runtime/content-set or core/hash mismatches return `409`, and every audited dependency must be present.
- Ordinary SFC/SNES entries accept the known Libretro `bsnes`, `bsnes-mercury`, `snes9x`, `snes9x-current`, and Mesen-S core identifiers. They do not require the per-ROM arcade audit table.

Pragmatic profiles accept these canonical native client identities:

| Client | `client.name` | `client.platform` | `client.architecture` |
| --- | --- | --- | --- |
| Windows | `SpatialEMU.Windows` | `windows-x64` | `x64` |
| macOS Apple silicon | `SpatialEMU.macOS` | `macos-arm64` | `arm64` |
| macOS Intel | `SpatialEMU.macOS` | `macos-x64` | `x64` |
| iPhone | `SpatialEMU.iOS` | `ios-arm64` | `arm64` |
| iPad | `SpatialEMU.iPadOS` | `ipados-arm64` | `arm64` |
| Apple Vision Pro | `SpatialEMU.visionOS` | `visionos-arm64` | `arm64` |
| Apple TV | `SpatialEMU.tvOS` | `tvos-arm64` | `arm64` |
| Android ARM64 | `GameEMU.Android` | `android-arm64` | `arm64` |

Apple mobile identities describe physical-device builds. Simulator identities and generic placeholders such as `SpatialEMU.Apple` are intentionally rejected, because they do not identify a deployable runtime ABI. The client must report only runtimes actually bundled in that target. Ordinary console, computer, and disc platforms can then resolve through the same deterministic manifest-backed route used by desktop clients.

Runtime descriptors accept the optional additive `coreBuildId` field. When both the request and an approved profile contain it, the server prefers that stable identity; otherwise it preserves legacy `coreSha256` matching. Ordinary console and computer platforms do not require an application-build SHA. FBNeo remains strict and requires either an approved stable build ID or the exact legacy core hash. MAME remains strict through its audited runtime version and `contentSet`.

`coreBuildId` must identify the core source revision, compatibility-affecting patch/configuration digest, ABI, and build configuration. It must not include the application version, signature, or unrelated UI object code. When the resolver is enabled, the server advertises `stableRuntimeIdentityV1: true`; deployed clients must require both that flag and `gameLaunchResolver: true` before sending stable build identities, and must retain the legacy manifest fallback for `404`, `405`, or `501` responses.

The Android ARM64 Dreamcast runtime reports Flycast 2.6 with `coreBuildId: "flycast-392a429-android-v4-arm64-gles3-hle-vmu-arcade-save-bundle"`. Successful resolution preserves that exact runtime tuple and returns the canonical manifest nested under `manifest`.

GameEMU Android uses the same exact tuple for resolver-certified NAOMI 2 catalog entries. The service accepts only canonical `platform: "naomi2"` games with a complete checksummed manifest; scanner-recognized cartridge packages contain one entry ZIP, GD-ROM packages contain that ZIP plus one same-driver CHD dependency, and pinned split cartridge clones contain the clone ZIP plus their checksummed parent ZIP. `parentRomSetName` identifies the latter relationship. The resolver never includes `naomi2.zip` for Android because firmware remains user-managed. A stale or unknown Android Flycast `coreBuildId` is rejected rather than echoed into an automatic profile.

MAME and FBNeo profiles remain target-specific and audited. Adding an Apple client identity does not make a Windows arcade profile portable: each mobile target needs a profile matching its exact client identity and runtime tuple. Current Apple builds report `mame/0.287/mame-0.287`. New Apple FBNeo profiles use a stable `coreBuildId`; legacy profiles may continue using an exact `coreSha256`. The server can persist these alongside Windows MAME 0.288 and FBNeo profiles without replacing or weakening either policy.

Clients may report MAME and FBNeo together in `runtimes`. The server evaluates every reported capability against the game's immutable fingerprint and audited profiles, then returns the selected request tuple in `runtime`. Selection is controlled by a stable server-side profile priority and does not depend on the order of `runtimes`. Existing clients that report only one runtime keep the same behavior. If no reported runtime has an audited profile, the endpoint returns `409`.

Legacy statically linked Apple FBNeo builds may still report an application-derived `coreSha256`. New profiles may additionally store `coreBuildId` so UI-only application rebuilds do not invalidate the core identity. Clients must never omit or fabricate either field to bypass strict arcade verification.

Point Blank uses the stable identity rule `fbneo:archive-{archiveSHA256}:{targetABI}:{buildFlavor}`, encoded as lowercase ASCII. `archiveSHA256` identifies the packaged FBNeo archive used by the target, not the SpatialEMU application executable. The audited lightgun profiles require these exact values:

- SpatialEMU iOS/iPadOS ARM64: `fbneo:archive-f1d54ccd94b661434a38930591e3697b89165a5946c45eff98f60d3981fd7b6c:ios-arm64:full-v1`
- SpatialEMU visionOS ARM64: `fbneo:archive-a161e273b161dc77fad5acc449798987f89741f0f75da1f05bec4ff7b6b75181:xros-arm64:full-localized-v1`

The archive digest, target ABI, and build flavor are part of the compatibility contract. A change to the packaged FBNeo core, ABI, or compatibility-affecting build flavor requires a new ID and a separately audited server profile. Unrelated application code, version numbers, signing, and UI changes do not. Clients send `coreBuildId` only when server capabilities advertise both `gameLaunchResolver: true` and `stableRuntimeIdentityV1: true`; otherwise they preserve the legacy `coreSha256` tuple.

For the exact audited ROM fingerprints, `ptblank` resolves `ptblank.zip` plus `namcoc75.zip`; `ptblanka` resolves `ptblanka.zip`, parent `ptblank.zip`, and `namcoc75.zip`. The device archive remains catalog role `dependency`: it is hidden from client game directories but remains addressable by the resolver.

```json
{
  "client": {
    "name": "SpatialEMU.Windows",
    "version": "1.302",
    "platform": "windows-x64",
    "architecture": "x64"
  },
  "runtimes": [
    {
      "id": "mame",
      "version": "0.288",
      "contentSet": "mame-0.288"
    },
    {
      "id": "libretro",
      "coreId": "fbneo",
      "coreSha256": "6ebc2675c272c8d654935647ac336d45bbd97452c4d5943290d5ffc75678d9f1"
    }
  ]
}
```

The request body is authoritative. Optional `X-FolioSpace-Client`, `X-FolioSpace-Client-Version`, and `X-FolioSpace-Runtime` headers are observability hints only and never affect authentication, authorization, or profile selection.

A successful response nests the canonical game manifest and adds cache identity:

```json
{
  "launchProfileId": "vstriker-windows-mame0288-v1",
  "profileRevision": 1,
  "runtime": {
    "id": "mame",
    "version": "0.288",
    "contentSet": "mame-0.288"
  },
  "manifest": {
    "game": {
      "id": 12,
      "title": "Virtua Striker",
      "platform": "model2",
      "romSetName": "vstriker",
      "format": "zip",
      "fileName": "vstriker.zip",
      "size": 10316803,
      "sha1": "8e3518318eeb157ab299b2f284faef176d3f49dd"
    },
    "fileUrl": "/api/client/games/12/file",
    "entryFile": "vstriker.zip",
    "files": [
      {
        "name": "vstriker.zip",
        "size": 10313686,
        "role": "entry",
        "url": "/api/client/games/12/file",
        "checksum": "sha1:8e3518318eeb157ab299b2f284faef176d3f49dd"
      },
      {
        "name": "segabill.zip",
        "size": 3117,
        "role": "dependency",
        "url": "/api/client/games/13/file",
        "checksum": "sha1:4631db7f7f5160a3a6591d3102722be869710f66"
      }
    ]
  }
}
```

The profile may publish a client-visible logical filename that differs from the immutable physical asset name. Clients must save each downloaded file using `files[].name`; URLs remain opaque, bearer-authenticated service URLs and never contain access tokens or NAS paths. Exactly one file is `entry`; BIOS, parent sets, device ROMs, CHDs, tracks, and other required files are `dependency` entries. File positions are unique and contiguous, every file has a positive size, and every returned file includes a lowercase `sha1:<40 hex>` checksum. `manifest.game.size` is the complete profile download size.

For a successful Windows 1.302 request, `runtime` is the exact selected request tuple, including trailing version components such as PCSX2 `2.6.3.0` or DOSBox Staging `0.82.2.0`. The server may normalize these versions internally for compatibility policy but must not rewrite them in the response.

Ordinary profiles are deterministic wrappers over the current canonical manifest. Their IDs and revisions are stable until the indexed game changes. Multi-file games use `/api/client/games/{gameId}/files/{position}` URLs and preserve descriptor-relative filenames. DOS responses keep the downloadable archive as `manifest.game.fileName`, use the inner `.bat`/`.com`/`.exe` as `entryFile`, and include the validated `dosLaunch` object.

The server never chooses an unreported runtime or substitutes the newest or closest MAME/FBNeo profile. An installed resolver returns `409` with a stable structured error when it cannot safely produce a launch package:

```json
{
  "code": "manifest-checksum-unavailable",
  "message": "A required launch file has not been checksummed.",
  "details": {
    "gameId": 12,
    "file": "track01.bin"
  }
}
```

Current error codes are:

- `runtime-unsupported`: the client identity, architecture, runtime, or core family is unsupported.
- `core-fingerprint-unknown`: a physical Apple client omitted or supplied an unrecognized Libretro core SHA-256.
- `launch-profile-missing`: a launchable profile has not been curated for this game/runtime.
- `dependency-missing`: an entry, track, BIOS, parent, device, or other required file is missing or invalid.
- `manifest-checksum-unavailable`: a required file has no persisted SHA-1 or changed after it was checksummed.
- `content-set-mismatch`: a MAME content set does not match an installed audited profile.

Clients must not fall back after `409`, `422`, authentication failures, or server errors. Only `404`, `405`, or `501` indicate that the resolver itself is unavailable and permit legacy manifest fallback. Cache identity is `gameId + launchProfileId + profileRevision + runtime.id + runtime.version + runtime.contentSet + runtime.coreId + runtime.coreBuildId + runtime.coreSha256`. Clients talking to older servers preserve the legacy identity without `coreBuildId`.

### `GET /api/client/games/{gameId}/manifest`

Returns client-safe game launch metadata. It does not expose the real NAS path.

```json
{
  "game": {
    "id": 12,
    "assetType": "game",
    "title": "Super Mario World",
    "platform": "snes",
    "romSetName": "SNES",
    "region": "USA",
    "format": "sfc",
    "fileName": "Super Mario World.sfc",
    "size": 524288,
    "crc32": "b19ed489",
    "sha1": "0123456789abcdef0123456789abcdef01234567",
    "emulatorHint": "snes",
    "compatibility": "unknown",
    "favorite": false,
    "liked": false,
    "coverUrl": "/api/games/12/cover",
    "manifestUrl": "/api/client/games/12/manifest",
    "downloadUrl": "/api/client/games/12/file"
  },
  "fileUrl": "/api/client/games/12/file",
  "entryFile": "Super Mario World.sfc",
  "files": [
    {
      "name": "Super Mario World.sfc",
      "size": 524288,
      "role": "entry",
      "url": "/api/client/games/12/files/0"
    }
  ]
}
```

`fileUrl` streams the entry file through FolioSpace Library and remains available for older single-file clients. It still requires bearer auth when auth is enabled. Native clients should treat it as an opaque service URL, not as a file path.

For Nintendo 64, `fileUrl` and `downloadUrl` return the byte-exact raw `.z64`, `.v64`, or `.n64` payload. If storage uses a single-ROM ZIP, FolioSpace opens the validated raw entry without exposing or downloading the ZIP container. `Content-Length` matches the catalog `size`, and the response filename preserves the raw ROM byte-order extension.

For PC-98, each `files[].url` returns a byte-exact validated package file. If storage uses a single-media ZIP, FolioSpace streams the raw entry and preserves its decoded media filename and extension. `Content-Length` matches that raw image size. A multi-disk set uses `entryFile` and an ordered `files[]` list with `role: "entry"` or `role: "disk"`, `label`, and `diskIndex`; clients should download every file while preserving the returned names. A translated title may additionally include one validated `role: "font"` file without disk metadata. The legacy `fileUrl` and `downloadUrl` continue to point to the first entry image for backward compatibility. Clients route canonical `platform: "pc98"` records to NP2kai and must not attempt to launch the ZIP container.

PC-98 HDI scans inspect the Anex86 partition table and FAT root directory without booting or fully loading the image. An HDI is left as `compatibility: "untested"` only when it has an active `MS-DOS` partition containing `IO.SYS`, `MSDOS.SYS`, and `COMMAND.COM`; otherwise it is retained as `compatibility: "broken"` so clients do not claim it is directly bootable. FolioSpace never packages or distributes a private DOS system disk. HDI/NHD/THD/VHD/SLH/HDN entries do not receive floppy-only `diskIndex`, `label`, or `driveHint` fields; those fields are emitted only for switchable floppy media.

Expansion or special-disk manifests include any cross-title media required at runtime. In the current curated library, `Dragon Knight 4 Special Disk` contains its own A/B disks plus the main game's C-L disks so the client does not need to infer a relationship from title text.

`Yu-No - Special Disk` is blocked as an independent game because it requires an existing bootable YU-NO installation. It may be exposed later only as a modeled add-on dependency of a complete main-game package.

`files[]` is the complete ordered launch set. Single-file games contain one `entry` item. Dreamcast GDI games contain the `.gdi` descriptor as `entry` followed by every referenced track as `dependency`. Saturn and PC-FX CUE games contain the `.cue` descriptor followed by every file named by a CUE `FILE` directive, including `.bin`, `.wav`, and `.mp3` tracks. A PC-FX M3U package contains the M3U entry, every referenced disc descriptor, and all descriptor tracks. Dependency lookup is case-insensitive, while each returned `name` preserves the spelling expected by its descriptor. Clients must preserve each `name` when downloading so the emulator can resolve the package. Each `url` uses `GET /api/client/games/{gameId}/files/{position}` and requires the same authentication as `fileUrl`.

For DOS ZIP/DOSZ archives, `entryFile` is the normalized path of the executable *inside* the archive, not the archive filename. It is explicitly `null` when no authoritative entry can be resolved. The additive `dosLaunch` object reports `entrySource` (`unknown`, `dosboxConfig`, or `curated`), nullable `installDirectory`, `workingDirectory`, and `dosboxConfig`, literal `arguments`, safe `.bat`/`.com`/`.exe` `candidates`, and optional advisory `keymapHints`. When `installDirectory` is present, clients expose the extracted archive as `C:\<installDirectory>` while interpreting `workingDirectory` relative to that installed root. Clients must download the archive through `fileUrl`, verify its published SHA-1, extract it with their own archive safety limits, and show an entry chooser when `entryFile` is null. The server never passes a catalog command to a host shell. The manifest response also includes `updatedAt` and uses `Cache-Control: private, no-store`; clients should refresh launch metadata on every launch while caching archive bytes independently by SHA-1.
`coverUrl` is optional. For supported retro platforms it streams a cached Libretro boxart image through FolioSpace Library; clients should fall back to their own placeholder when it is absent or returns 404.

### `GET /api/client/games/{gameId}/details`

Returns the same client-safe game DTO plus optional metadata fields and artwork references for detail screens. Local file paths are not exposed.

### `GET /api/client/games/{gameId}/metadata`

Returns game metadata suitable for native detail panels and external launcher synchronization.

### `PUT /api/client/games/{gameId}/private-state`

Saves profile-scoped private state for a game.

```json
{
  "favorite": true,
  "liked": false
}
```

The response is the updated client-safe game DTO.

### `GET /api/client/games/{gameId}/play-stats`

Returns profile-scoped play history for one game. A game that has never been launched returns zero counters and `null` timestamps.

```json
{
  "gameId": 42,
  "profileId": 1,
  "firstPlayedAt": "2026-07-22T10:00:00Z",
  "lastPlayedAt": "2026-07-22T10:45:00Z",
  "totalPlaySeconds": 2700,
  "launchCount": 3
}
```

### `GET /api/client/games/played`

Returns the profile-scoped, paginated list of games that have at least one recorded launch or non-zero play time. This is the preferred endpoint for recent-games and play-time statistics screens because clients do not need to fetch every game individually.

Supported query parameters:

- `limit` (default `50`, maximum `200`)
- `offset`
- `q`
- `platform` (one value or comma-separated values)
- `sort=recent|playtime|launches|title` (default `recent`)
- `direction=desc|asc` (default `desc`)
- `profileId` (or `X-FolioSpace-Profile-Id`)

```json
{
  "items": [
    {
      "gameId": 42,
      "title": "Metal Slug",
      "platform": "neogeo",
      "romSetName": "FBNeo",
      "format": "zip",
      "sha1": "...",
      "emulatorHint": "neogeo",
      "coverUrl": "/api/games/42/cover",
      "manifestUrl": "/api/client/games/42/manifest",
      "firstPlayedAt": "2026-07-22T10:00:00Z",
      "lastPlayedAt": "2026-07-22T10:45:00Z",
      "totalPlaySeconds": 2700,
      "launchCount": 3
    }
  ],
  "total": 1,
  "limit": 50,
  "offset": 0,
  "hasMore": false
}
```

Use `gameId` as the primary match key within one FolioSpace instance. `sha1` and `crc32` are included as fallback identity keys after a library rebuild or re-index.

### `PUT /api/client/games/{gameId}/play-stats`

Reports one launch session. `sessionId` is generated by the client and must remain stable from launch through all heartbeats and the final exit report. `elapsedSeconds` is the cumulative duration for that session, not the duration since the previous heartbeat.

```json
{
  "sessionId": "7c4558e4-9f79-41c6-bd87-7fbd98bedce7",
  "startedAt": "2026-07-22T10:00:00Z",
  "elapsedSeconds": 900,
  "endedAt": "2026-07-22T10:15:00Z"
}
```

`startedAt` and `endedAt` are optional RFC3339 timestamps. The recommended client flow is:

1. Send `elapsedSeconds: 0` immediately after a successful game launch.
2. Send the current cumulative elapsed value every 30 to 60 seconds.
3. Send one final cumulative value with `endedAt` when the game exits or switches.

Repeated or out-of-order reports for the same `sessionId` are idempotent: the server stores the greatest elapsed value and only adds its increase to `totalPlaySeconds`. A new `sessionId` increments `launchCount` once. The response contains the updated aggregate under `stats`, plus the accepted session duration.

```json
{
  "stats": {
    "gameId": 42,
    "profileId": 1,
    "firstPlayedAt": "2026-07-22T10:00:00Z",
    "lastPlayedAt": "2026-07-22T10:15:00Z",
    "totalPlaySeconds": 900,
    "launchCount": 1
  },
  "sessionId": "7c4558e4-9f79-41c6-bd87-7fbd98bedce7",
  "sessionPlaySeconds": 900,
  "ended": true
}
```

Both endpoints accept `X-FolioSpace-Profile-Id` or `?profileId=` and otherwise use the default profile. Older clients can ignore this additive capability.

### `PUT /api/client/games/{gameId}/save-sync/archive`

Uploads the GameEMU save-sync archive for a game. The request body is the raw `.gameemusaves` archive payload generated by the native client, with content type:

```text
application/vnd.gameemu.save-sync+json
```

The archive is profile-scoped. Clients may send `X-FolioSpace-Profile-Id` or `?profileId=` when they support FolioSpace profiles; otherwise the default profile is used.

Response:

```json
{
  "ok": true
}
```

### `GET /api/client/games/{gameId}/save-sync/archive`

Downloads the previously uploaded GameEMU save-sync archive for a game and profile. The response body is the raw `.gameemusaves` archive payload and the content type is `application/vnd.gameemu.save-sync+json`.

Returns `404` when the game does not exist or no save-sync archive has been uploaded for that game/profile yet.

### `GET /api/games/metadata/providers`

Returns supported game metadata provider information for admin or launcher-style integrations.

### `GET /api/games/gamelist.xml`

Exports indexed games as a `gamelist.xml` document. Optional query parameters include `q`, `platform`, `romSetName`, `format`, and `basePath`.

### Manual Collections

Manual collections are user-defined logical shelves. They can contain books, games, and videos without moving files or changing scanner classification.

- `GET /api/client/manual-collections`: list manual collections.
- `POST /api/client/manual-collections`: create a collection with `name` and optional `description`.
- `GET /api/client/manual-collections/{collectionId}`: return collection details and resolved item summaries.
- `PUT /api/client/manual-collections/{collectionId}`: update name or description.
- `DELETE /api/client/manual-collections/{collectionId}`: delete a manual collection.
- `POST /api/client/manual-collections/{collectionId}/items`: add `{ "assetType": "book" | "game" | "video", "assetId": 123 }`.
- `DELETE /api/client/manual-collections/{collectionId}/items/{assetType}/{assetId}`: remove one item.

## Private State

Private state is user-owned metadata on a book. It is stored server-side and returned through client-safe DTOs, without local NAS file paths.

Fields:

- `status`: free string. Current UI uses `want`, `reading`, `finished`, and `dropped`.
- `favorite`: boolean.
- `rating`: integer, clamped by the service to `0...5`.
- `tags`: string array. Empty and duplicate tags are normalized by persistence.
- `summary`: private note.

### `GET /api/client/books/{bookId}/private-state`

Returns the current private state and the current client book DTO.

```json
{
  "book": {
    "id": 42,
    "collectionId": 7,
    "collectionTitle": "Series A",
    "title": "Volume 01",
    "bookType": "single_volume",
    "format": "cbz",
    "pageCount": 180,
    "coverStatus": "ready",
    "coverUrl": "/api/books/42/cover",
    "currentPage": 16,
    "progressFraction": 0.09,
    "privateStatus": "want",
    "favorite": true,
    "rating": 4,
    "tags": ["vision", "spatial"],
    "summary": "Vision Pro candidate"
  },
  "privateState": {
    "status": "want",
    "favorite": true,
    "rating": 4,
    "tags": ["vision", "spatial"],
    "summary": "Vision Pro candidate"
  }
}
```

### `PUT /api/client/books/{bookId}/private-state`

Saves private state and returns the same shape as `GET /api/client/books/{bookId}/private-state`.

Request:

```json
{
  "status": "want",
  "favorite": true,
  "rating": 4,
  "tags": ["vision", "spatial"],
  "summary": "Vision Pro candidate"
}
```

### `GET /api/client/books/favorites`

Returns favorite books as client-safe book DTOs.

Query:

- `limit`: optional, default `12`, max `50`.

### `GET /api/client/books/private-status/{status}`

Returns books with a matching private status as client-safe book DTOs.

Query:

- `limit`: optional, default `12`, max `50`.

Example:

```text
/api/client/books/private-status/want?limit=12
```

### `GET /api/client/search`

Searches title, collection title, format, tags, and private summary.

Query:

- `q`: search text.
- `limit`: optional, default `20`, max `100`.

Response:

```json
{
  "query": "spatial",
  "books": [
    {
      "id": 42,
      "collectionId": 7,
      "collectionTitle": "Series A",
      "title": "Volume 01",
      "bookType": "single_volume",
      "format": "cbz",
      "pageCount": 180,
      "coverStatus": "ready",
      "coverUrl": "/api/books/42/cover",
      "currentPage": 16,
      "progressFraction": 0.09,
      "privateStatus": "want",
      "favorite": true,
      "rating": 4,
      "tags": ["vision", "spatial"],
      "summary": "Vision Pro candidate"
    }
  ]
}
```

## Supporting Resource Endpoints

The manifest intentionally points to existing resource routes. Native clients should treat these as implementation URLs returned by the manifest, not as the primary discovery API.

### `GET /api/books/{bookId}/cover`

Streams the book cover image.

### `GET /api/books/{bookId}/pages/{pageIndex}`

Streams one CBZ/ZIP page image.

Optional query:

- `maxWidth`: downsample image archive pages to this pixel width before streaming. Values are clamped to `320...2400`. The response is JPEG when downsampled. This is intended for memory-safe mobile, tablet, and webtoon readers; omit it to stream the original archive entry.

### `GET /api/books/{bookId}/epub/resources/{resourcePath}`

Streams one EPUB resource. This can be XHTML, CSS, image, font, or other EPUB content.

Resource paths should be URL-encoded by path segment.

## Progress Sync

### `GET /api/books/{bookId}/progress`

Returns the current legacy progress for the active profile. If no progress exists, the server returns page `0` with progress `0`.

```json
{
  "bookId": 42,
  "pageIndex": 16,
  "locator": "",
  "progressFraction": 0.09
}
```

### `PUT /api/books/{bookId}/progress`

Saves legacy progress for the active profile.

Request:

```json
{
  "pageIndex": 16,
  "locator": "",
  "progressFraction": 0.09
}
```

Response:

```json
{
  "ok": true
}
```

For CBZ/ZIP/PDF single-page and double-page modes, `pageIndex` is the page array index and `locator` can be empty. For EPUB, use `pageIndex` as the spine index and use `locator` for the current EPUB resource href or a future CFI-like locator. `progressFraction` is clamped by the server to `0...1`.

Old webtoon clients may still use this route with `locator: "webtoon:<fraction>"`. New clients should prefer the structured webtoon endpoints below. The server keeps backward compatibility by syncing each saved structured webtoon position into this legacy record as:

```json
{
  "pageIndex": 159,
  "locator": "webtoon:0.224685",
  "progressFraction": 0.224685
}
```

This means old clients, shelves, continue-reading, MCP `get_progress`, and integrations that only know `/progress` continue to see a usable page index and percent.

### `GET /api/books/{bookId}/reading-position`

Returns mode-specific structured reading positions for the active profile.

```json
{
  "bookId": 5772,
  "positions": {
    "webtoon": {
      "bookId": 5772,
      "readerMode": "webtoon",
      "schema": "webtoon-position-v1",
      "pageIndex": 159,
      "pageKey": "archive:0160_14_09.webp",
      "pageYOffsetRatio": 0.431245,
      "viewportAnchorRatio": 0.28,
      "documentProgress": 0.224685,
      "pageCount": 1235,
      "contentSignature": "optional",
      "updatedAt": "2026-06-06T04:36:53Z"
    }
  }
}
```

If no structured position exists, `positions` is empty. Unknown future reader modes may appear as additional keys, so clients should ignore modes they do not understand.

### `PUT /api/books/{bookId}/reading-position/webtoon`

Saves a structured comic webtoon position for the active profile. The only supported schema today is `webtoon-position-v1`.

Request:

```json
{
  "schema": "webtoon-position-v1",
  "pageIndex": 159,
  "pageKey": "archive:0160_14_09.webp",
  "pageYOffsetRatio": 0.431245,
  "viewportAnchorRatio": 0.28,
  "documentProgress": 0.224685,
  "pageCount": 1235,
  "contentSignature": "optional"
}
```

Response:

```json
{
  "bookId": 5772,
  "readerMode": "webtoon",
  "schema": "webtoon-position-v1",
  "pageIndex": 159,
  "pageKey": "archive:0160_14_09.webp",
  "pageYOffsetRatio": 0.431245,
  "viewportAnchorRatio": 0.28,
  "documentProgress": 0.224685,
  "pageCount": 1235,
  "contentSignature": "optional",
  "updatedAt": "2026-06-06T04:36:53Z"
}
```

Server normalization:

- Empty `schema` defaults to `webtoon-position-v1`.
- Unsupported schemas return `400`.
- Negative `pageIndex` and `pageCount` are normalized to `0`.
- `pageYOffsetRatio`, `viewportAnchorRatio`, and `documentProgress` are clamped to `0...1`.
- Missing or non-positive `viewportAnchorRatio` defaults to `0.28`.
- Saving a webtoon position also updates legacy `/progress` with `locator: "webtoon:<documentProgress>"`.

#### `webtoon-position-v1`

The semantic position is:

```text
fixed viewport anchor -> one page image -> normalized Y offset inside that image
```

The core fields are:

- `pageKey`: stable page identity. Prefer `pages[].pageKey` from the manifest. Fall back to `archive:{name}` or `index:{pageIndex}` only when necessary.
- `pageYOffsetRatio`: where the viewport anchor lands inside the target image, normalized to `0...1`.
- `viewportAnchorRatio`: where the anchor lives inside the viewport. The web client uses `0.28`.
- `documentProgress`: display/sort percentage for shelves and legacy clients. It is not the authoritative restore coordinate.
- `pageIndex`: fast lookup fallback when `pageKey` cannot be matched.
- `pageCount`: page count at save time, useful for detecting rescans or changed archives.
- `contentSignature`: optional client/server extension field for future content-change detection.

Recommended save algorithm for native clients:

1. Define `anchorY = scrollTop + viewportHeight * 0.28`.
2. Find the page image containing `anchorY`.
3. Save `pageKey` from the manifest and `pageYOffsetRatio = (anchorY - pageTop) / pageDisplayedHeight`.
4. Clamp `pageYOffsetRatio` to `0...1`.
5. Calculate `documentProgress` from logical page heights when available: `naturalHeight / naturalWidth` for each page.
6. If full logical heights are not available, do not overwrite a known good percentage with placeholder-height math. Keep the previous `documentProgress` or update it conservatively from the page/offset delta.
7. Debounce normal saves. Flush once on app exit, book close, or mode switch that changes the actual reading position.

Recommended restore algorithm:

1. Read `GET /api/books/{bookId}/reading-position`.
2. For webtoon mode, find `positions.webtoon`.
3. Locate the target page by `pageKey`; if missing, fall back to `pageIndex`; if out of range, estimate from `documentProgress`.
4. Wait until the target image has a real displayed height. Cached images may already be complete even if an image load callback does not fire.
5. Scroll to `pageTop + pageDisplayedHeight * pageYOffsetRatio - viewportHeight * viewportAnchorRatio`.
6. Ignore programmatic scroll and image-load layout events while restoring.
7. Only save after explicit user interaction, such as wheel, touch, pointer, keyboard page movement, or slider movement.

Mode switching rules:

- Switching between `single`, `double`, and `webtoon` is a view/layout change, not necessarily a new reading position.
- When entering webtoon mode from paged mode, create or reuse a webtoon anchor for the current `pageIndex`, restore to that anchor, then accept user scroll events.
- Do not immediately rewrite `/progress` during a mode-only switch. Save only after the user changes pages or scrolls.
- Keep webtoon position independent from single/double page position so changing reader modes does not corrupt the long-strip anchor.

Backward compatibility rules:

- Old clients can keep using `GET/PUT /api/books/{bookId}/progress`.
- New webtoon clients should use `GET /api/books/{bookId}/reading-position` and `PUT /api/books/{bookId}/reading-position/webtoon`.
- If `PUT /api/books/{bookId}/reading-position/webtoon` returns `404` or `405` against an older server, clients should fall back to `PUT /api/books/{bookId}/progress` with `locator: "webtoon:<documentProgress>"`.
- If `GET /api/books/{bookId}/reading-position` is unavailable, clients can fall back to legacy `GET /api/books/{bookId}/progress`; when `locator` starts with `webtoon:`, parse the fraction as display progress and estimate a starting page from it.

## Optional Collection Browsing

The native home screen can start from `/api/client/home`, but collection browsing can use the existing collection route.

### `GET /api/collections`

Lists collections. Directory collections include `libraryId` and `directoryPath` for legacy web UI flows. When a representative book is available, the response also includes optional `coverBookId`, `thumbnailStatus`, and `thumbnailUrl` fields matching the collection fields returned by `/api/client/home`. Responses also include profile-scoped `favorite` and `liked` flags when a profile is selected with `X-FolioSpace-Profile-Id` or `profileId`.

Without query parameters, the response remains the legacy collection array. When any paged query parameter is present, the response is a paginated object:

```http
GET /api/collections?primaryType=book&limit=60&offset=0&sort=recent&direction=desc
GET /api/collections?primaryType=comic&limit=60&offset=0&sort=title&direction=asc
```

Query:

- `primaryType`: optional exact collection type filter. Common values are `book`, `comic`, `game`, and `video`; use `all` or omit it to include every type.
- `limit`: optional, default `60`, max `200`.
- `offset`: optional, default `0`.
- `q`: optional text filter against collection title.
- `sort`: optional. Supported values are `title`, `recent`, `recently_added`, `book_count`, and `count`. Unknown values fall back to `title`.
- `direction`: optional, `asc` or `desc`. Unknown values fall back to `asc`; recent/count sorts keep default descending behavior when omitted.

Paged response:

```json
{
  "items": [
    {
      "id": 7,
      "libraryId": 1,
      "title": "Series A",
      "directoryPath": "Series A",
      "collectionType": "directory",
      "primaryType": "comic",
      "bookCount": 12,
      "coverBookId": 42,
      "thumbnailStatus": "pending",
      "thumbnailUrl": "/api/books/42/thumbnail?size=small&v=v1-cover-refresh-4",
      "favorite": false,
      "liked": false
    }
  ],
  "total": 123,
  "limit": 60,
  "offset": 0,
  "hasMore": true
}
```

Native home screens should prefer this paginated collection catalog for book/comic overview tabs, then open `/api/collections/{collectionId}/volumes` for the volumes inside one collection.

### `PUT /api/collections/{collectionId}/private-state`

Saves profile-scoped collection state.

Request:

```json
{
  "favorite": true,
  "liked": true
}
```

Response is the updated collection DTO.

### `GET /api/collections/{collectionId}/volumes`

Returns all volumes in a collection.

Optional paged query:

- `limit`: default `60`, max `200`
- `offset`: default `0`
- `q`: text filter
- `sort`: server-supported sort key

When any paged query parameter is present, the response is:

```json
{
  "items": [],
  "total": 0,
  "limit": 60,
  "offset": 0,
  "hasMore": false
}
```

Without paged query parameters, the response is the legacy book array.

### `GET /api/collections/{collectionId}/assets`

Returns mixed collection assets. Current responses can contain books/comics and games:

```json
{
  "books": [],
  "games": []
}
```

Native clients should prefer `/api/client/home`, `/api/client/games`, and book manifests for first-screen and launch flows. This endpoint is useful when a collection is used as a local shelf that can contain multiple asset types.

## Scan Diagnostics And Control

These routes are operational surfaces for web UI, trusted native tools, and MCP agents.

### `GET /api/libraries`

Returns configured library roots. This endpoint can expose configured mount paths and should be treated as an admin/diagnostic route, not a public client catalog.

Each library includes `excludePatterns`, a list of directory names or library-relative paths skipped during scans. The scanner also always skips common generated directories such as `#recycle`, `@eaDir`, `.calnotes`, `__MACOSX`, `media`, `covers`, `cover`, `thumbnails`, `.thumbnails`, `thumbs`, and `.thumbs`. PC-FX archive-only directories named `出版物附属盘、非卖品` and `游戏镜像` are also skipped. Files named `.DS_Store` or beginning with `._` are globally ignored. For Dreamcast GDI, Saturn CUE, and PC-FX descriptor sets, referenced files are dependencies and are not indexed as separate games.

### `POST /api/libraries`

Creates or updates a configured library root.

```json
{
  "name": "Comics",
  "rootPath": "/library",
  "assetType": "comic",
  "excludePatterns": ["media", "thumbnails", "covers"]
}
```

### `PUT /api/libraries/{libraryId}`

Updates scan exclude patterns for an existing library.

```json
{
  "excludePatterns": ["media", "thumbnails", "covers", "Some Series/cover-assets"]
}
```

PDF scans read lightweight embedded Info metadata when available. `/Title` maps to book title, `/Author` maps to creator/collection grouping, and `/Subject` maps to description. Missing or unreadable PDF metadata falls back to the filename and current path-based collection behavior.

### `POST /api/libraries/{libraryId}/scan`

Starts a scan job for a library and returns the job.

Request body is optional. Omit it to scan the full library. Pass `path` to scan one container-visible subdirectory or file inside the library root:

```json
{
  "path": "/library/韩漫/某作品/Chap.263.zip"
}
```

`path` can also be relative to the library root, for example `韩漫/某作品`. The server rejects paths outside the configured library root.

To scan only the newest new or changed files, pass `mode: "recent"` and an optional `recentLimit`. This still walks the selected scope to find candidates, but it indexes only the latest files that are missing from the index or whose size/mtime changed. It is intended for adding several new archives under a large library without rescanning every unchanged file:

```json
{
  "mode": "recent",
  "path": "/library/韩漫",
  "recentLimit": 20
}
```

`recentLimit` defaults to `20` and is capped by the server. If `path` is omitted, the recent scan uses the library root.

### `GET /api/jobs`

Lists recent scan jobs.

### `GET /api/jobs/{jobId}/events`

Lists job events. Events include scan start, worker count, skipped/indexed files, errors, pause/cancel state, and completion.

### `GET /api/settings/scan`

Returns scan runtime settings.

```json
{
  "scanWorkers": 4
}
```

### `PUT /api/settings/scan`

Saves scan runtime settings and returns the normalized value. `scanWorkers` is currently clamped to the supported server range.

```json
{
  "scanWorkers": 8
}
```

### `POST /api/jobs/{jobId}/pause`

Requests pause for a running scan job.

### `POST /api/jobs/{jobId}/cancel`

Requests cancellation for a running, pause-requested, or paused scan job.

### `POST /api/jobs/{jobId}/resume`

Starts a new scan for the same library when the selected job is paused.

### `GET /api/errors`

Lists scan/import errors.

Optional query:

- `jobId`: return errors for one job.

## Error Format

Errors currently use a simple JSON envelope:

```json
{
  "error": "missing or invalid bearer token"
}
```

Common statuses:

- `400`: invalid request, bad path parameter, or malformed JSON.
- `401`: token auth is enabled and the token/cookie is missing or invalid.
- `404`: unknown book, collection, library, or route.
- `405`: wrong HTTP method.
- `500`: archive, scan, database, or file streaming failure.

## Swift Sketch

```swift
struct FolioSpaceClient {
    let baseURL: URL
    let token: String?

    func request(_ path: String) throws -> URLRequest {
        var request = URLRequest(url: baseURL.appending(path: path))
        if let token, !token.isEmpty {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        return request
    }
}
```

For image or EPUB resource loading, make sure the same bearer header is applied. If the platform loader cannot attach custom headers for subresources, fetch bytes through the app networking layer and feed them to the renderer from local cache.

## MCP Opportunities

MCP is useful for assistant-driven operations, diagnostics, and library management. It should not sit in the hot path of the Vision Pro reading UI; the native app should use the HTTP API directly for reading.

The first stdio MCP server is available at `cmd/foliospace-mcp`; usage and integration reference are in [`docs/mcp/usage.md`](../mcp/usage.md).

Good MCP tools:

- `foliospace.client_info`: return server info and capability flags.
- `foliospace.home`: return continue-reading, recent books, and collections.
- `foliospace.search_books`: search/filter books by title, collection, format, progress, or unread state.
- `foliospace.open_book_manifest`: return the client manifest for a book, including `readerModes` and `defaultReaderMode`.
- `foliospace.list_games`, `foliospace.list_game_platforms`, and `foliospace.open_game_manifest`: browse available game platforms, paginated local ROM assets, and client-safe manifests. `list_game_platforms` mirrors full-catalog game facets and reports platforms with client-visible indexed games; launch readiness still comes from the resolver.
- `foliospace.get_game_metadata_providers` and `foliospace.export_game_gamelist`: inspect game metadata sources and export launcher-style `gamelist.xml`.
- `foliospace.save_game_private_state`: save profile-scoped game favorite and liked flags.
- `foliospace.list_played_games`: list profile-scoped played games with pagination, filters, cumulative play time, launch count, and first/last played timestamps.
- `foliospace.get_game_play_stats` and `foliospace.report_game_play_session`: inspect and idempotently update profile-scoped game play time and launch counts.
- `foliospace.list_videos` and `foliospace.open_video_manifest`: browse and open local video assets through client-safe DTOs.
- `foliospace.get_private_state` and `foliospace.save_private_state`: inspect or update status, favorite, rating, tags, and notes.
- `foliospace.list_favorites` and `foliospace.list_private_status`: browse private shelves such as favorites and want-to-read.
- `foliospace.get_preferences` and `foliospace.save_preferences`: inspect or update UI language and reader defaults.
- `foliospace.get_progress` and `foliospace.save_progress`: inspect or update legacy reading progress. Webtoon-aware native clients should use the HTTP `reading-position` API directly for exact page-key plus Y-offset anchors.
- `foliospace.list_libraries`: list configured libraries for diagnostics and scan selection.
- `foliospace.update_library_excludes`: update scan exclude directory names or relative paths for a configured library.
- `foliospace.list_collections`, `foliospace.save_collection_state`, `foliospace.list_collection_volumes`, and `foliospace.list_collection_assets`: browse the indexed library and save profile-scoped collection favorite/liked flags.
- `foliospace.list_manual_collections`, `foliospace.create_manual_collection`, `foliospace.get_manual_collection`, `foliospace.add_manual_collection_item`, and `foliospace.remove_manual_collection_item`: manage user-defined shelves that can mix books, games, and videos.
- `foliospace.scan_library`: start a scan for a configured library. Optional `path` scans one subdirectory or file inside the library root.
- `foliospace.scan_recent`: scan only the latest new or changed files under a library or optional target path. Use this after adding several files to a large directory.
- `foliospace.list_jobs`, `foliospace.job_events`, `foliospace.pause_job`, `foliospace.cancel_job`, and `foliospace.resume_job`: inspect and control scan progress.
- `foliospace.list_errors`: surface broken archives, unsupported files, permission errors, and missing mounts.
- `foliospace.library_health`: summarize scan status, error counts, stale books, empty collections, and missing covers.

Good MCP resources:

- `foliospace://client/info`
- `foliospace://client/home`
- `foliospace://client/preferences`
- `foliospace://libraries`
- `foliospace://jobs`
- `foliospace://errors`
- `foliospace://health`

Useful assistant workflows:

- "Find unread EPUBs in this collection."
- "Show books tagged Vision Pro that are marked want-to-read."
- "Mark this book as favorite and add the spatial tag."
- "Switch the library UI to Traditional Chinese and default EPUB to dark double-page mode."
- "Show books with scan errors."
- "Explain why this book will not open."
- "Start a scan and watch job events."
- "Prepare a Vision Pro test set: one CBZ, one ZIP, one EPUB with TOC, one EPUB without cover."
- "Generate a client fixture from the manifest for book 42."

Avoid for MCP v1:

- Streaming every page image through MCP as the normal reader transport. Use HTTP resource URLs for performance.
- Returning full EPUB chapter text by default. Prefer metadata, locators, snippets, and explicit user-directed extraction.
- Mutating library roots or deleting indexed content until there is a clear admin permission model.

Suggested first MCP scope:

1. Read-only discovery: `client_info`, `home`, `search_books`, `open_book_manifest`, `list_games`, `open_game_manifest`.
2. Diagnostics: `list_libraries`, `list_jobs`, `job_events`, `list_errors`, `library_health`.
3. Controlled progress and private state sync: `get_progress`, `save_progress`, `get_private_state`, `save_private_state`.
4. Controlled scan operations: `scan_library`, `pause_job`, `cancel_job`, `resume_job`.
5. Admin actions later: library root mutation, delete/reindex/repair operations.
