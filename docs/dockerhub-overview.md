# FolioSpace Library

FolioSpace Library is a self-hosted personal digital asset library for NAS, Docker, and local servers. It provides a unified indexing layer and client API for books, comics, PDFs, game ROM libraries, videos, and future spatial media clients.

It is not a cloud media service and does not distribute books, comics, ROMs, movies, or other media content. It indexes user-owned local files and exposes stable service URLs to web and native clients without leaking real NAS paths.

## 0.996 Release: Audited Point Blank Launch Support

Release `0.996` adds strict FBNeo launch profiles for Point Blank on SpatialEMU Apple clients.

- Exact `ptblank` and `ptblanka` fingerprints route to FBNeo on iOS, iPadOS, and visionOS.
- Stable packaged-core `coreBuildId` identities replace whole-application fingerprints for these profiles; unknown builds remain rejected and approved legacy SHA-256 profiles continue to work.
- `ptblank` resolves with `namcoc75.zip`; `ptblanka` resolves with its parent `ptblank.zip` and `namcoc75.zip`.
- `namcoc75.zip` is hidden from client game directories as a dependency while remaining available to audited manifests.
- Existing Client API routes remain backward compatible. Service, Client API, Web, and source MCP metadata report version `0.996`.

## 0.995 Release: Safe Native Delivery and Targeted Curation

Release `0.995` expands safe native game delivery, compatibility curation, and offline access.

- Nintendo 3DS libraries validate direct `.3ds`/`.cci` NCSD images, `.cxi` NCCH images, and safe single-image ZIP packages before indexing. Launchable images stream as their original inner bytes, while `.cia` packages are explicitly marked for client-side installation.
- ZIP-backed game downloads support single-range requests even when the inner archive stream is not seekable, enabling resumable Nintendo DS, Nintendo 3DS, and other validated single-ROM downloads.
- Game Curation can rebuild FBNeo or MAME compatibility for one game without deleting unrelated profiles. The MAME audit includes a fingerprint-pinned exception for the verified Time Crisis package that embeds its exact `namcoc71` device ROM, without rewriting the ZIP.
- Android ARM64 launch resolution accepts the pinned Flycast v4 runtime identity for Dreamcast, NAOMI, Atomiswave, and audited NAOMI 2 packages. Split NAOMI 2 sets require their checksummed parent ZIP, while user-managed firmware is not injected into Android manifests.
- Book manifests expose a byte-exact authenticated download URL with HTTP Range and HEAD support for offline reading.
- The Client Home API can omit collection expansion for faster first-screen loading, and scans skip `_maintenance` directories by default.
- Existing Client API routes remain backward compatible. Service, Client API, Web, and source MCP metadata report version `0.995`.

## 0.994 Release: Offline Identity and Expanded Game Delivery

Release `0.994` expands native game delivery and adds stable offline identity for books and comics.

- Nintendo DS `.nds` files and supported single-ROM ZIP packages are indexed as canonical `nds` games, matched against Nintendo DS artwork, and negotiated only with the exact `melonds-ds` core on supported physical Apple clients.
- 3DO `.cue`, `.iso`, and `.chd` images are indexed as canonical `3do` games. CUE manifests preserve every referenced track, exclude BIOS files from the public catalog, and require an Opera-compatible client runtime.
- Konami Python 1 `.py1` descriptors are indexed as one game with their seven validated relative dependencies, complete file checksums, and an explicit `pcsx2-reliquary` launch contract.
- PC-98 mixed packages containing `USER.FDI` plus CUE/BIN CD media are published as one launchable game with a complete ordered manifest instead of separate or missing entries.
- Audited NAOMI Project Justice revisions preserve canonical clone and parent identities, while Atomiswave manifests include the shared `awbios.zip` dependency when required.
- Game file downloads support HTTP Range requests, enabling resumable downloads and large-image streaming without restarting from byte zero.
- Book, EPUB, PDF, CBZ, and ZIP DTOs add nullable `contentHash`, `contentHashAlgorithm`, `fileSize`, and `contentRevision` fields. A serialized background worker computes full-file SHA-256 values without blocking list or manifest requests and invalidates them when source bytes or page manifests change.
- Existing Client API routes remain backward compatible. Service, Client API, Web, and MCP metadata report version `0.994`.

## 0.993 Release: Virtual Boy and Dynamic Platform Catalog

Release `0.993` adds Nintendo Virtual Boy support and removes the need for clients to hard-code game-platform filters.

- Virtual Boy `.vb` and `.vboy` ROMs are indexed with canonical `virtualboy` metadata and existing client-safe manifests.
- Local `boxart` artwork uses normalized filename matching and takes priority over cached network artwork.
- The authenticated `/api/client/games/platforms` endpoint publishes stable platform IDs, display titles, aliases, counts, and availability.
- `/api/client/info` advertises `gamePlatformCatalog`; older integrations can continue using `/api/client/games/facets`.
- MCP adds `foliospace.get_game_platform_catalog` for agents that need the complete server-owned platform catalog.
- Existing Client API response shapes remain backward compatible. Service, Client API, Web, and MCP metadata report version `0.993`.

## 0.992 Release: Manifest Stability and Progressive Loading

Release `0.992` stabilizes game delivery and reduces startup work on large self-hosted libraries.

- Manifest-first game delivery remains the compatible default for existing and mobile clients; launch-profile resolution is explicitly capability-gated.
- Arcade manifests include audited parent, BIOS, device, QSound, and Capcom ZN dependencies when required.
- ZIP contents take precedence over filename guesses, preventing cartridge ROMs inside ZIP archives from being published as similarly named CPS/MAME sets.
- Self-contained MAME clones can satisfy merged parent ROM requirements from their own archive.
- Game searches prioritize launchable entries over same-name records that still require curation.
- The Game Curation Center batches status queries, eliminating per-item SQLite reads on large pages.
- The web home screen progressively loads first-screen sections, pages collections, defers maintenance data, and polls only active scan state.
- Existing Client API response shapes remain backward compatible. Service, Client API, Web, and MCP metadata report version `0.992`.

## 0.991 Release: CPS Catalog Filter Hotfix

Release `0.991` fixes CPS platform browsing in the web game catalog while preserving existing Client API behavior.

- CPS-1, CPS-2, and CPS-3 labels map to the canonical `cps1`, `cps2`, and `cps3` platform identifiers.
- Platform filters and counts come from the full client facets endpoint instead of legacy collection totals.
- Counts now match the launchable records returned by `/api/client/games`; dependency and `needs-curation` entries do not lead users to empty shelves.
- Existing native clients remain compatible because API response shapes are unchanged.
- Service, Client API, Web, and MCP metadata report version `0.991`.

## 0.990 Release: Game Curation Center

Release `0.990` adds a complete game-library preparation workflow for new and existing self-hosted installations.

- The new **Game Curation Center** separates published games, dependencies, and records that still need attention.
- Automatic post-scan analysis is enabled by default for fresh installations and can be configured from the web UI.
- Compatibility analysis rebuilds audited FBNeo/MAME launch profiles from administrator-supplied policy files without modifying source ROMs.
- Only launchable records are published to native clients; incomplete archives and unresolved dependencies remain visible to administrators as `needs-curation`.
- Background analysis, artwork, and metadata tasks are bounded, observable, and protected against duplicate concurrent runs.
- Batch artwork matching supports local sidecars, cover folders, `media/<ROM name>/boxFront.*`, and optional Libretro fallback.
- Web metadata editing covers titles, descriptions, genres, developers, publishers, dates, regions, and explicit source selection.
- Optional Hasheous lookup is hash-based and opt-in. Local-only operation remains the default, and metadata outages never block scanning.
- First-run setup includes game-catalog automation and advanced FBNeo/MAME/runtime policy paths.
- Service, Client API, Web, and MCP metadata report version `0.990`.

Detailed setup and recovery guidance is available in the [Game Curation documentation](https://github.com/funland/foliospace-Library/blob/main/docs/operations/game-catalog-curation.md).

## 0.982 Release: Durable Audited Arcade Profiles

Release `0.982` makes audited arcade launch profiles durable and rebuildable for existing libraries.

- Audited profiles and their complete entry/dependency closure are persisted in SQLite.
- The explicit `foliospace-rebuild-launch-profiles` command validates existing FBNeo ZIP sets against a deployment-supplied official Arcade DAT without rescanning or rewriting ROM files.
- It also supports an official MAME 0.288 `listxml` audit for selected platforms. Model 2 archives are matched by ZIP stem and promoted only when their complete parent, device, and BIOS closure validates.
- Every published FBNeo game passes logical ROM name, uncompressed size, CRC, and parent/BIOS dependency checks.
- Windows FBNeo profiles require the exact approved core SHA-256; unknown or mismatched cores return `409 runtime-profile-not-available`.
- Deployment-supplied target files can publish the same audited FBNeo closure to Apple physical-device builds using their exact reported core SHA-256, while mobile MAME 0.287 and Windows MAME 0.288 profiles remain separate.
- SFC/SNES profile negotiation recognizes Libretro bsnes alongside Snes9x and Mesen-S.
- Client lists, facets, search, and played shelves hide dependency and `needs-curation` records instead of advertising games that cannot launch.
- Existing users do not need to rescan their game libraries. Only missing hashes or rejected ROM sets need a targeted rescan or repair.
- Service, Client API, and MCP source metadata report version `0.982`.

The official FBNeo Arcade DAT is intentionally not bundled. Place it at `/config/policies/fbneo-arcade.dat`, then run once after upgrading:

```bash
docker exec foliospace-library /app/foliospace-rebuild-launch-profiles \
  --dat=/config/policies/fbneo-arcade.dat
```

For Model 2, place the official MAME 0.288 listxml ZIP at `/config/policies/mame0288lx.zip`, inspect a dry-run, and then repeat without `--dry-run`:

```bash
docker exec foliospace-library /app/foliospace-rebuild-launch-profiles \
  --policy=mame \
  --mame-listxml=/config/policies/mame0288lx.zip \
  --platforms=model2 \
  --dry-run
```

## 0.981 Release: Tiered Game Launch Profiles

Release `0.981` adds launch-profile negotiation without forcing every ordinary console game into a per-runtime audit table.

- `POST /api/client/games/{gameId}/resolve` matches the authoritative client/runtime inventory against a validated canonical manifest.
- Ordinary console platforms reuse existing single-file and multi-file manifests. Known Libretro platform/core combinations do not require a per-build core hash.
- Curated DOS packages preserve the archive download plus their safe inner executable, arguments, working directory, and launch candidates. Unknown DOS entries still return a profile conflict.
- Windows runtime versions such as PCSX2 `2.6.3.0` and DOSBox Staging `0.82.2.0` are normalized only for policy matching; the successful response echoes the exact selected request tuple.
- Virtua Striker resolves its required `segabill.zip` dependency for Windows MAME 0.288.
- Tekken Tag Tournament can receive the compatible logical entry name without renaming or rewriting the physical ROM archive.
- CPS1, CPS2, and CPS3 are exposed as separate canonical platforms. Audited `sf2`, `sfa`, and `sfiii` profiles match the exact packaged FBNeo core SHA-256.
- MAME 0.288 profiles now cover six audited Mahjong sets. `mcnpshnt` receives the required logical `ym2413.zip` dependency without renaming `ym2413_instruments.zip` on disk.
- Responses include profile revision, exact selected runtime identity, logical filenames, available checksums, and complete dependency closure.
- Unsupported runtime combinations return an explicit `409 runtime-profile-not-available`; the server never substitutes the closest or newest MAME set.
- The legacy game manifest remains unchanged for existing clients.
- MCP adds `foliospace.resolve_game_launch_profile` for trusted agent integrations.
- Service, Client API, and MCP metadata report version `0.981`.

## 0.98 Release: Expanded Platforms and Curated DOS Launches

Release `0.98` expands the game catalog while keeping launch details stable for native clients.

- PSP, Nintendo GameCube, and PlayStation 2 files receive canonical platform metadata and web/API filters.
- NAOMI 2 filtering now remains distinct from adjacent arcade platforms.
- MCP adds `foliospace.list_game_platforms`, using full-library facets and launchable-item counts.
- Curated DOS collections can read `games.json` metadata, match covers, index archive packages, and expose executable launch commands and working directories.
- DOS manifests preserve archive-relative paths so clients can extract a package and start the intended executable reliably.
- Service, Client API, Web, and MCP metadata report version `0.98`.

## 0.978 Release: Game Play-Time Sync

Release `0.978` adds profile-scoped game play-time synchronization for GameEMU and other native clients.

- Clients report cumulative active emulation time through idempotent launch-session heartbeats, so retries and out-of-order reports never double-count time.
- `GET` and `PUT /api/client/games/{gameId}/play-stats` provide total play seconds, launch count, and first/last played timestamps.
- `GET /api/client/games/played` provides a profile-scoped, paginated played-game catalog for recent activity and play-time dashboards.
- MCP adds `foliospace.get_game_play_stats` and `foliospace.report_game_play_session` for trusted local agents.
- MCP also provides `foliospace.list_played_games` for aggregate game-history queries without one request per game.
- `/api/client/info` advertises `gamePlayStats: true` for capability detection.
- Service and MCP metadata report version `0.978`.

## 0.977 Release: Expanded Console and Arcade ROM Support

Release `0.977` adds canonical scanning, filtering, artwork, and complete launch manifests for six additional platforms.

- Dreamcast GDI/CDI/CHD packages retain every required track under one launchable game.
- Sega Saturn CUE/BIN and ISO games are counted by disc rather than by physical track file.
- NEC PC-FX supports CUE, CCD, TOC, CHD, M3U, multi-disc grouping, Pegasus metadata, and local cover folders.
- Nintendo 64 validates `.z64`, `.v64`, and `.n64` byte order and can stream the raw ROM from a supported single-ROM ZIP.
- NEC PC-98 adds validated floppy/hard-disk formats, CP932 title decoding, duplicate merging, multi-disk manifests, and artwork sidecars.
- Sega Model 2 preserves MAME ZIP shortnames and bytes, adds friendly titles and compatibility states, and keeps BIOS dependencies outside ordinary platform counts.
- Client API facets, manifests, MCP tools, web filters, and authenticated downloads use stable canonical platform metadata.
- Service and MCP metadata now report version `0.977`.

## 0.976 Release: Bounded Memory and Reader Layouts

Release `0.976` addresses a T0 memory-exhaustion risk on large NAS libraries and expands comic reader display controls.

- Image decoding and thumbnail transforms enforce source-size, pixel-count, output-size, and concurrency limits.
- PDF thumbnail rendering is handled only by the bounded background worker.
- Cover-wall cache misses use a bounded queue and batched SQLite writes, keeping the API responsive under thumbnail bursts.
- Docker deployments receive a 768 MiB Go memory budget and a 1.5 GiB Compose container limit by default.
- Comic single-page reading supports contain, fit-width, and fit-height modes with left- and right-handed controls.
- Service and MCP metadata now report version `0.976`.

## 0.975 Release: Large Game Library Stability

Release `0.975` is a stability and performance hotfix for large game libraries and NAS deployments.

- The web game catalog now loads a smaller first page to reduce initial cover-wall pressure.
- The Client Home API avoids concurrent SQLite section reads that could queue up on single-connection deployments.
- Game and collection private-state lookups now only read state for the current page of items.
- Game list sorting and filtering add SQLite expression indexes for title and platform-heavy browsing.
- Service and MCP metadata now report version `0.975`.

## 0.970 Release: Manual Collections and Game Library Controls

Release `0.970` adds a more flexible personal-library layer on top of indexed assets.

- User-defined manual collections can group books, games, and videos without moving files on disk.
- Game assets can now be marked favorite or liked from the web UI, Client API, and MCP.
- Game catalog browsing can filter by platform groups derived from indexed game collections.
- Game metadata helpers include provider discovery and `gamelist.xml` export for launcher-style integrations.
- Service and MCP metadata now report version `0.970`.

## 0.969 Release: PDF Metadata and Scan Excludes

Release `0.969` improves scan results for PDF-heavy libraries and mixed folders that contain generated artwork.

- PDF scans now read lightweight embedded Info metadata when available, mapping title, author, and subject to FolioSpace title, creator, and description fields.
- Libraries can define scan exclude directories from the web UI, API, or MCP.
- The scanner skips common generated folders such as `media`, `thumbnails`, `covers`, `__MACOSX`, and `@eaDir`, preventing artwork and sidecar folders from being indexed as books.
- Service and MCP metadata now report version `0.969`.

## 0.968 Release: Sortable Library Views

Release `0.968` improves large-library browsing in the web UI and keeps the Client API version aligned.

- Collection pages can now sort by title, recently added time, item count, or primary type, with ascending/descending direction controls.
- Game and video catalog pages now expose simple sort controls for title, recently added time, and platform where applicable.
- Collection API responses include `addedAt`, and paginated `/api/collections` supports type-based sorting for client integrations.
- Game cover lookup continues to support local `media/<rom-name>/boxFront.*` artwork beside ROM files, so curated arcade/console covers can be displayed without remote scraping.
- Service and MCP metadata now report version `0.968`.

## 0.966 Release: Embedded Comic Metadata

Release `0.966` adds embedded JSON metadata support for comic ZIP/CBZ archives.

- ZIP/CBZ scans now read small embedded metadata JSON files such as `metadata.json`, `info.json`, `comicinfo.json`, and `元数据.json`.
- Metadata fields `name`, `author`, `description`, and `tags` map onto FolioSpace's existing book title, creator, description, and public tag fields without a database migration.
- Search now matches public archive tags and creators, so tagged packs can be found through the web UI, Client API, and MCP-backed search flows.
- Book API responses merge public archive tags with profile-private tags while keeping user private state separate.

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
docker pull funland/foliospace-library:0.996
```

```bash
docker run -p 8080:8080 \
  -v /volume1/docker/foliospace-library/config:/config \
  -v /volume2/ComicCenter:/library:ro \
  -v /volume2/Books:/books:ro \
  -v /volume2/GameROMS:/games:ro \
  -e FOLIOSPACE_DIRECTORY_ROOTS=/library,/books,/games \
  funland/foliospace-library:0.996
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
```

If `FOLIOSPACE_API_TOKEN` is empty, the web setup page can create the first access token and stores only a SHA-256 token hash in SQLite.

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
