# FolioSpace Library Direction

FolioSpace Library is the product direction that evolves the current FolioSpace Reader codebase into a personal digital asset library.

The server runs on a NAS, Docker host, or local machine. Its job is to index personal assets and expose a stable client service layer. Apple-device clients, including reading apps, GameEMU, and Vision Pro experiences, own the final consumption UI.

## Positioning

FolioSpace Library is not a general media-server clone and is not a full replacement for Plex, Jellyfin, or Immich.

It focuses on:

- Unified indexing of personal digital assets.
- Stable client APIs.
- Covers and thumbnails.
- Classification, collections, tags, favorites, private state, search, recent access, and progress.
- Client-safe responses that do not expose real NAS file paths.

## Asset Types

Initial and planned asset types:

- `book`: EPUB and book-like long-form reading.
- `comic`: CBZ and ZIP image archives.
- `game`: local ROM files and ROM sets.
- `document`: PDF, manuals, art books, guides, setting collections, and reference files.
- `photo`: ordinary photos.
- `spatialPhoto`: Vision Pro spatial photos.
- `video`: ordinary videos.
- `spatialVideo`: Vision Pro spatial videos.
- `audio`: OSTs and audio material connected to games, books, and collections.

ROM support is strictly for indexing and launching user-owned local content. FolioSpace Library must not distribute ROMs, provide download sources, or bundle pirated assets.

## Product Modules

- Reader: EPUB and comic reading.
- Game Shelf: ROM library, ROM-set metadata, and GameEMU launch handoff.
- Spatial Gallery: Vision Pro spatial photo/video browsing.
- Archive: PDFs, manuals, art books, guides, and reference collections.

The existing web reader is an operational admin and reading surface, not the long-term only client.

## Transitional Naming

External product name:

```text
FolioSpace Library
```

Recommended deployment names:

```text
Docker image: foliospace-library
CLI command: foliospace-library
Default NAS config root: /volume1/docker/foliospace-library
```

Current transitional constraints:

- Keep the repository folder `FolioSpaceReader` for now.
- Keep the Go module and source package names for now.
- Keep the existing `/api/client` prefix to avoid client migration cost.
- Rename the broader codebase after the data model moves from Book/Series to Asset/LibraryItem.

## Asset / LibraryItem Model

`Asset` or `LibraryItem` should become the unified base model after the first non-reading asset type is implemented.

Suggested base fields:

```text
id
type: book | comic | game | photo | spatialPhoto | video | spatialVideo | audio | document
title
coverUrl
thumbnailUrl
metadata
fileRefs
progress
state
collections
tags
createdAt
updatedAt
lastAccessedAt
```

`metadata` should be typed by asset kind at the service boundary even if stored as JSON internally.

`fileRefs` should point to internal file records or opaque resource IDs, not real client-visible NAS paths.

## Game Extension

Game assets should be the first expansion beyond books/comics.

Suggested fields:

```text
platform
romSetName
region
crc
sha1
emulatorHint
compatibility
saveStateRefs
lastPlayedAt
```

Initial game indexing should prioritize:

- Detecting ROM files and common archive formats.
- Platform hints from extension, path, and optional metadata sidecars.
- Checksums for local identification.
- ROM-set grouping without download-source behavior.
- A launch handoff payload for GameEMU rather than embedded emulation in the server.

## Spatial Media Extension

Spatial media should follow game indexing.

Initial indexing should prioritize:

- Detect ordinary photos/videos separately from spatial photos/videos.
- Extract basic dimensions, duration, creation time, and thumbnail.
- Preserve original files as read-only source assets.
- Provide Vision Pro clients with thumbnail and stream URLs, not NAS paths.

## API Direction

Keep `/api/client` as the stable client-facing prefix for now.

Near-term additions should stay client-safe:

- `/api/client/home`: mixed shelves across books, comics, games, and spatial media.
- `/api/client/assets/{id}/manifest`: eventual generalized open metadata.
- `/api/client/assets/search`: eventual typed search across asset types.
- Existing book endpoints remain valid until clients migrate.

Do not expose:

- Real absolute paths.
- Library mount topology.
- ROM download sources.
- Mutating library-root operations without an admin permission model.

## Implementation Sequence

1. Rename product copy and deployment names to FolioSpace Library.
2. Keep current EPUB/comic reading functionality stable.
3. Add an internal model design for Asset/LibraryItem.
4. Add game asset indexing first.
5. Add spatial photo/video indexing second.
6. Generalize `/api/client` once at least one non-reading asset type exists.
7. Rename repository, binary entrypoint, and Go module only after the data model transition is real.
