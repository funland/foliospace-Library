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

## Client Endpoints

### `GET /api/client/info`

Returns stable client capability metadata.

Response:

```json
{
  "serviceName": "FolioSpace Library",
  "apiVersion": "v1",
  "supportedFormats": ["cbz", "zip", "epub"],
  "capabilities": {
    "clientHome": true,
    "unifiedManifest": true,
    "progressSync": true,
    "epubStreaming": true,
    "pageStreaming": true,
    "privateState": true,
    "search": true,
    "bearerTokenAuth": true,
    "scannerJobEvents": true
  }
}
```

### `GET /api/client/home`

Returns the data needed for a native home screen in one request.

Query:

- `limit`: optional, default `12`, max `50`. Applies to `continueReading`, `recentBooks`, `favoriteBooks`, and `wantToRead`.

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
  "collections": [
    {
      "id": 7,
      "title": "Series A",
      "collectionType": "directory",
      "bookCount": 12
    }
  ]
}
```

The client DTO intentionally omits local NAS paths such as `filePath`, `rootPath`, and `directoryPath`.

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
      "url": "/api/books/42/pages/0"
    }
  ]
}
```

Use `pages[index].url` to stream the image bytes. The returned page URL is relative to the same base URL and still requires bearer auth when auth is enabled.

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

For CBZ/ZIP, `pageIndex` is the page array index and `locator` can be empty.

For EPUB, use `pageIndex` as the spine index and use `locator` for the current EPUB resource href or a future CFI-like locator. `progressFraction` is clamped by the server to `0...1`.

## Optional Collection Browsing

The native home screen can start from `/api/client/home`, but collection browsing can use the existing collection route.

### `GET /api/collections`

Lists collections.

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

Good MCP tools:

- `foliospace.client_info`: return server info and capability flags.
- `foliospace.home`: return continue-reading, recent books, and collections.
- `foliospace.search_books`: search/filter books by title, collection, format, progress, or unread state.
- `foliospace.open_manifest`: return the client manifest for a book.
- `foliospace.get_private_state` and `foliospace.save_private_state`: inspect or update status, favorite, rating, tags, and notes.
- `foliospace.list_favorites` and `foliospace.list_private_status`: browse private shelves such as favorites and want-to-read.
- `foliospace.get_progress` and `foliospace.save_progress`: inspect or update reading progress.
- `foliospace.list_collections` and `foliospace.list_volumes`: browse the indexed library.
- `foliospace.scan_library`: start a scan for a configured library.
- `foliospace.list_jobs` and `foliospace.job_events`: inspect scan progress and history.
- `foliospace.list_errors`: surface broken archives, unsupported files, permission errors, and missing mounts.
- `foliospace.library_health`: summarize scan status, error counts, stale books, empty collections, and missing covers.

Good MCP resources:

- `foliospace://client/info`
- `foliospace://home`
- `foliospace://collections/{collectionId}`
- `foliospace://books/{bookId}/manifest`
- `foliospace://books/{bookId}/private-state`
- `foliospace://books/favorites`
- `foliospace://books/private-status/{status}`
- `foliospace://books/{bookId}/progress`
- `foliospace://jobs/{jobId}/events`
- `foliospace://errors`

Useful assistant workflows:

- "Find unread EPUBs in this collection."
- "Show books tagged Vision Pro that are marked want-to-read."
- "Mark this book as favorite and add the spatial tag."
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

1. Read-only discovery: `client_info`, `home`, `search_books`, `open_manifest`.
2. Diagnostics: `list_jobs`, `job_events`, `list_errors`, `library_health`.
3. Controlled progress and private state sync: `get_progress`, `save_progress`, `get_private_state`, `save_private_state`.
4. Admin actions later: `scan_library`, library root management, reindex/repair operations.
