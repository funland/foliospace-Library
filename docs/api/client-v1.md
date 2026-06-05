# FolioSpace Library Client API v1

This document describes the stable HTTP surface intended for native clients such as a Vision Pro reader, GameEMU, and future spatial media clients. The client API is a facade over the current reading routes, so native clients do not need to depend on every web UI endpoint directly.

## Base URL

Use the NAS or test server address as the base URL:

```text
http://192.168.10.155:18080
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
8. Sync progress with `GET /api/books/{bookId}/progress` and `PUT /api/books/{bookId}/progress`.
9. Sync private state with `GET/PUT /api/client/books/{bookId}/private-state`.
10. Sync UI language and reader defaults with `GET/PUT /api/client/preferences`.
11. Open a game with `GET /api/client/games/{gameId}/manifest`, then use `fileUrl` only through the service.
12. Open a video with `GET /api/client/videos/{videoId}/manifest`, then use `fileUrl` for direct/Range playback or `hlsUrl` when `playbackMode` is `hls`.

## Client Endpoints

### `GET /api/client/info`

Returns stable client capability metadata.

Response:

```json
{
  "serviceName": "FolioSpace Library",
  "serviceVersion": "0.91",
  "apiVersion": "v1",
  "supportedFormats": ["cbz", "zip", "epub", "pdf", "mp4", "m4v", "mov", "mkv", "avi", "webm", "nes", "sfc", "smc", "gba", "gb", "gbc", "nds", "3ds", "cia", "chd", "iso", "bin", "cue", "7z"],
  "capabilities": {
    "clientHome": true,
    "unifiedManifest": true,
    "progressSync": true,
    "epubStreaming": true,
    "pdfStreaming": true,
    "pdfPageLayout": true,
    "pdfWebtoonLayout": true,
    "comicWebtoonLayout": true,
    "compactReader": true,
    "pageStreaming": true,
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
    "scanSettings": true
  }
}
```

PDF clients should read the manifest through `GET /api/client/books/{bookId}/manifest`, then fetch the PDF through the opaque page URL at `GET /api/books/{bookId}/pages/0`. The server supports HTTP Range requests for that URL, so native clients can stream PDF data without exposing the NAS path. `pdfPageLayout` means clients may offer single-page and two-page spread modes on top of the same PDF stream. `pdfWebtoonLayout` and `comicWebtoonLayout` mean clients may also offer vertical continuous scrolling when a manifest includes `webtoon` in `readerModes`. `compactReader` means the bundled web UI has a phone-oriented compact reader, but native clients can still implement their own layout.

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
      "favorite": true,
      "liked": false
    }
  ]
}
```

The client DTO intentionally omits local NAS paths such as `filePath`, `rootPath`, and `directoryPath`.

### `GET /api/client/games`

Returns a paginated client-safe ROM catalog for Vision Pro, iPad, and GameEMU native clients. Use this endpoint for full game directory browsing instead of the limited `gameShelf` on `/api/client/home`.

Query:

- `limit`: optional, default `50`, max `200`. Values above max are clamped and the response returns the actual limit.
- `offset`: optional, default `0`.
- `q`: optional search against `title`, `romSetName`, `region`, `platform`, and `format`.
- `platform`: optional exact platform filter, for example `nes`, `snes`, `gba`, `md`, `neogeo`, `arcade`, or `3ds`.
- `format`: optional exact format filter, for example `nes`, `sfc`, `gba`, `zip`, or `3ds`.
- `sort`: optional. Supported values are `recent`, `title`, and `platform`. Unknown values fall back to `recent`.

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
      "size": 262160,
      "crc32": "9bb6059e",
      "sha1": "5de393e3ad83e6e185e6d338684d7a4475b7d2ce",
      "emulatorHint": "nes",
      "compatibility": "unknown",
      "coverUrl": "/api/games/18/cover",
      "manifestUrl": "/api/client/games/18/manifest"
    }
  ],
  "total": 128,
  "limit": 50,
  "offset": 0,
  "hasMore": true
}
```

Empty results return `items: []` with `total: 0`; the endpoint does not return 404 for an empty catalog. The `items` DTO is the same client-safe game DTO used by `gameShelf`, and never includes NAS paths, local file paths, or Docker volume paths.

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
      "url": "/api/books/42/pages/0",
      "displayUrl": "/api/books/42/pages/0?maxWidth=1200"
    }
  ]
}
```

Use `pages[index].displayUrl` for normal comic reading surfaces, especially phones, tablets, and webtoon/vertical-scroll mode. It points at a server-downsampled image that limits decoded client memory. Use `pages[index].url` when the client explicitly needs the original page bytes. Returned page URLs are relative to the same base URL and still require bearer auth when auth is enabled.

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

Current defaults are conservative:

- EPUB: `readerModes: ["single"]`.
- CBZ/ZIP/PDF: `readerModes: ["single", "double", "webtoon"]`.
- `defaultReaderMode` is currently `single` for all formats. Future metadata or user preferences may choose `webtoon` automatically for known long-strip works.

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
    "size": 524288,
    "crc32": "b19ed489",
    "sha1": "0123456789abcdef0123456789abcdef01234567",
    "emulatorHint": "snes",
    "compatibility": "unknown",
    "coverUrl": "/api/games/12/cover",
    "manifestUrl": "/api/client/games/12/manifest"
  },
  "fileUrl": "/api/client/games/12/file"
}
```

`fileUrl` streams the local file through FolioSpace Library and still requires bearer auth when auth is enabled. Native clients should treat it as an opaque service URL, not as a file path.
`coverUrl` is optional. For supported retro platforms it streams a cached Libretro boxart image through FolioSpace Library; clients should fall back to their own placeholder when it is absent or returns 404.

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

Returns current progress. If no progress exists, the server returns page `0` with progress `0`.

```json
{
  "bookId": 42,
  "pageIndex": 16,
  "locator": "",
  "progressFraction": 0.09
}
```

### `PUT /api/books/{bookId}/progress`

Saves progress.

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

For CBZ/ZIP/PDF, `pageIndex` is the page array index and `locator` can be empty. In Webtoon/vertical-scroll mode, clients may store `locator` as `webtoon:<fraction>` where `<fraction>` is the scroll progress from `0` to `1`.

For EPUB, use `pageIndex` as the spine index and use `locator` for the current EPUB resource href or a future CFI-like locator. `progressFraction` is clamped by the server to `0...1`.

## Optional Collection Browsing

The native home screen can start from `/api/client/home`, but collection browsing can use the existing collection route.

### `GET /api/collections`

Lists collections. Responses include profile-scoped `favorite` and `liked` flags when a profile is selected with `X-FolioSpace-Profile-Id` or `profileId`.

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

### `POST /api/libraries/{libraryId}/scan`

Starts a scan job for a library and returns the job.

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
- `foliospace.list_games` and `foliospace.open_game_manifest`: browse and open local ROM assets through client-safe DTOs.
- `foliospace.list_videos` and `foliospace.open_video_manifest`: browse and open local video assets through client-safe DTOs.
- `foliospace.get_private_state` and `foliospace.save_private_state`: inspect or update status, favorite, rating, tags, and notes.
- `foliospace.list_favorites` and `foliospace.list_private_status`: browse private shelves such as favorites and want-to-read.
- `foliospace.get_preferences` and `foliospace.save_preferences`: inspect or update UI language and reader defaults.
- `foliospace.get_progress` and `foliospace.save_progress`: inspect or update reading progress.
- `foliospace.list_libraries`: list configured libraries for diagnostics and scan selection.
- `foliospace.list_collections`, `foliospace.save_collection_state`, `foliospace.list_collection_volumes`, and `foliospace.list_collection_assets`: browse the indexed library and save profile-scoped collection favorite/liked flags.
- `foliospace.scan_library`: start a scan for a configured library.
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
