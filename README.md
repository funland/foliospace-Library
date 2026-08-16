# FolioSpace Library

[Website](https://foliospace.app/) · [Docker Hub](https://hub.docker.com/r/funland/foliospace-library) · [FolioSpace on the App Store](https://apps.apple.com/app/foliospace/id6765784590) · [Reader](https://reader.foliospace.app/) · [SpatialEMU downloads](https://spatialemu.com/downloads.html) · [Client API](docs/api/client-v1.md) · [MCP](docs/mcp/usage.md)

FolioSpace Library is a self-hosted personal digital asset library that runs on a NAS, Docker host, or local server. One service keeps your collection, metadata, progress, and private state available to the web, Vision Pro, iPad, iPhone, SpatialEMU, and MCP agents.

It is not trying to become a complete Plex, Jellyfin, or Immich replacement. The first priority is personal asset indexing: scanning, identifying, covers/thumbnails, classification, search, favorites, recent access, progress, and private state. Dedicated clients such as a reader app, GameEMU, and Vision Pro experiences own the actual consumption UI.

The current implementation still starts from the FolioSpace Reader codebase and keeps the existing reading MVP operational while the model evolves toward `Asset` / `LibraryItem`.

Current release: [`0.996`](https://github.com/funland/foliospace-Library/releases/tag/v0.996).

## Quick Answers

### What is FolioSpace Library?

FolioSpace Library is a self-hosted NAS and Docker service for indexing and accessing user-owned books, comics, games, videos, documents, photos, and other personal digital assets across multiple clients.

### Is FolioSpace a cloud storage service?

No. FolioSpace runs on your NAS, Docker host, or local server. Your files remain under your control, and clients receive authenticated service URLs instead of exposed NAS paths.

### Which clients can connect to FolioSpace?

The same FolioSpace Library service can connect to the web UI, FolioSpace Reader on Vision Pro, iPad, and iPhone, SpatialEMU for compatible game catalogs, and MCP-enabled AI agents.

### How do I install FolioSpace Library?

The fastest deployment path is the published multi-architecture Docker image. Follow the [Docker setup](#docker), then open `http://<docker-host>:8080` and complete first-run setup.

### Does FolioSpace distribute books, ROMs, or other media?

No. FolioSpace indexes and serves metadata for files that you already own and store locally. It does not host, share, or provide download sources for copyrighted media.

![FolioSpace Library web reader with English controls](https://raw.githubusercontent.com/funland/foliospace-Library/main/docs/screenshots/web-reader-en.png)

## Clients and Downloads

FolioSpace Library is the server layer. Your files stay on your own NAS or local server; clients connect to that service instead of turning FolioSpace into a hosted cloud library.

- **FolioSpace Reader**: Read EPUB, PDF, and ZIP/CBZ comics on [Vision Pro, iPad, and iPhone through the App Store](https://apps.apple.com/app/foliospace/id6765784590), or open the [Reader web experience](https://reader.foliospace.app/).
- **SpatialEMU**: Browse the game catalog exposed by FolioSpace and import compatible titles into the native emulator. Download [SpatialEMU](https://spatialemu.com/downloads.html) and see the [FolioSpace connection guide](https://spatialemu.com/foliospace.html#connect).
- **Web UI and API**: Use the built-in web interface or integrate another native client through the [Client API](docs/api/client-v1.md).
- **AI agents**: Install and configure the local MCP client with the [MCP guide](docs/mcp/usage.md).

FolioSpace Library indexes and serves metadata for user-owned local files. It does not host or distribute books, comics, ROMs, games, or other copyrighted media.

## Screenshots

### Transparent scan jobs

![FolioSpace Library scan worker settings and completed job details](https://raw.githubusercontent.com/funland/foliospace-Library/main/docs/screenshots/scan-jobs-en.png)

### Game catalog curation

![FolioSpace Library game compatibility and metadata curation](https://raw.githubusercontent.com/funland/foliospace-Library/main/docs/screenshots/game-curation-en.png)

## License

Copyright (C) 2026 funland co.,Ltd.

The server and web application source in this repository is released under the GNU Affero General Public License v3.0. See [`LICENSE`](LICENSE).

FolioSpace Library indexes user-owned local files only. It does not distribute books, comics, ROMs, movies, or other media content.

## Runtime Layout

- `/config`: SQLite database, generated covers/thumbnails, runtime cache.
- `/library`: read-only mounted asset library root.
- `/books`, `/games`: optional read-only roots used by the default Docker compose example.
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
FOLIOSPACE_DIRECTORY_ROOTS=/library,/books,/games
FOLIOSPACE_ADDR=:8080
FOLIOSPACE_API_TOKEN=
FOLIOSPACE_SCAN_WORKERS=2
FOLIOSPACE_DISABLE_GAME_LAUNCH_RESOLVER=true
```

Set `FOLIOSPACE_API_TOKEN` to require API authentication from environment variables. If it is empty, release `0.966` can create the first access token from the web setup page and stores only a SHA-256 token hash in SQLite. Native clients can send `Authorization: Bearer <token>`. The web UI stays publicly loadable, then prompts for the access token and receives an HttpOnly cookie so covers, pages, and EPUB iframe resources can load through normal browser requests.

The launch resolver is disabled by default while runtime profiles are being stabilized. With `FOLIOSPACE_DISABLE_GAME_LAUNCH_RESOLVER=true`, the server advertises `gameLaunchResolver: false` and returns `404` from the resolve endpoint, allowing compatible clients to use the established game manifest flow. Set the variable to `false` only after the runtime target catalog and profile migration have passed client acceptance tests.

Authentication helpers:

- `GET /api/auth/status`: returns whether token auth is enabled.
- `POST /api/auth/check`: accepts `{"token":"..."}` and returns `{"ok":true}` for a valid token.
- `POST /api/auth/logout`: clears the web auth cookie.

First-run setup helpers:

- `GET /api/setup/status`: returns whether the service has an access token and at least one library.
- `POST /api/setup/initialize`: creates the first access token and first library.
- `GET /api/config/directory-roots`: returns container-visible root directories for the setup picker.

Fresh installations can also configure the game catalog pipeline during setup. FolioSpace classifies new game files after scanning, keeps unverified archives in `needs-curation`, and exposes them for discovery without claiming that a compatible launch profile exists. Dependency-only files remain hidden from client catalogs. The setup page can enable automatic post-scan analysis, local/Libretro cover matching, optional Hasheous hash metadata, and explicit FBNeo/MAME compatibility policy paths under `/config/policies`.

Game catalog discovery is manifest-first while the resolver is stabilized. A missing runtime profile never removes a game from lists, search, or facets. The resolver remains disabled by default, allowing existing clients to use canonical manifests; strict FBNeo and MAME negotiation will be re-enabled only after policy migration and physical-device acceptance. Runtime descriptors accept an optional `coreBuildId`; when the resolver is explicitly enabled, the server also advertises `stableRuntimeIdentityV1` so supported clients can negotiate exact runtime builds while retaining legacy manifest fallback.

After setup, open **Game Curation** in the web sidebar to inspect ready games, dependencies, and records that need attention. The page reports missing identity, launch-file checksum coverage, missing policy packs, and failed launch-profile audits; it can run bounded checksum backfill, re-run catalog analysis, batch-match local covers, optionally try Libretro artwork, and edit metadata without moving the source ROM. See [`docs/operations/game-catalog-curation.md`](docs/operations/game-catalog-curation.md) for the complete workflow.

## Client API v1

Detailed client integration docs are in [`docs/api/client-v1.md`](docs/api/client-v1.md).

- `GET /api/client/info`: service metadata, supported formats, and capability flags.
- `GET /api/client/home`: `continueReading`, `recentBooks`, and `collections` in one response.
- `GET /api/client/books`: paginated all-books catalog for native All / Library overview screens.
- `GET /api/collections?primaryType=comic&limit=60&offset=0`: paginated collection catalog for native Home / collection overview screens.
- `GET /api/client/books/:id/manifest`: a client-safe open manifest with `readerModes` and `defaultReaderMode`. CBZ/ZIP books include page URLs; EPUB books include spine, TOC, `resourceBaseUrl`, `coverUrl`, and progress; PDF books expose an opaque Range-capable stream URL for single-page, double-page, or webtoon/vertical-scroll client layouts.
- `GET /api/books/:id/reading-position` and `PUT /api/books/:id/reading-position/webtoon`: structured webtoon progress using a stable page key plus normalized page Y offset, with automatic legacy `/progress` fallback compatibility.
- `GET /api/client/games/:id/manifest`: a client-safe game launch manifest with platform, checksums, emulator hint, and opaque file URLs. Sega Model 2 keeps original MAME ZIP names and bytes, exposes operator-arcade input metadata, and resolves hidden runtime dependencies without inflating facets. Nintendo 64 `.z64`, `.v64`, and `.n64` games, Virtual Boy `.vb`/`.vboy` games, and PC-98 disk images are validated before indexing and downloaded as raw media bytes even when stored in a single-media ZIP. PC-98 multi-disk sets expose one ordered entry/dependency list, while byte-identical mirrors are collapsed into one catalog record. Dreamcast GDI, Saturn CUE, and PC-FX CUE/M3U manifests include the descriptor plus every required track in `files[]`. DOS ZIP/DOSZ archives keep their exact download bytes and add safe inner-archive launch candidates, hash-matched localized metadata, covers, and curated launch commands from a controlled `games.json` source.
- `GET/PUT /api/client/games/:id/play-stats`: profile-scoped first/last played time, cumulative play seconds, and launch count. Clients report cumulative seconds per stable session id, so heartbeat retries are idempotent.
- `GET/PUT /api/client/books/:id/private-state`: client-safe private status, favorite, rating, tags, and note sync.
- `GET/PUT /api/client/preferences`: client UI language and reader preference sync.
- `GET/PUT /api/settings/scan`: scan worker settings for NAS devices with different CPU and memory budgets.
- `GET /api/client/search`, `/api/client/books/favorites`, and `/api/client/books/private-status/:status`: private-state-aware discovery shelves.
- `POST /api/libraries/:id/scan` supports full scans, targeted `path` scans, and `mode: "recent"` scans for only the latest new or changed files.

Client API book and collection responses omit local NAS file paths. Returned cover, thumbnail, page, EPUB, game, and video URLs are opaque service URLs; clients should preserve query parameters because FolioSpace uses them for cache-compatible media refreshes while keeping older routes valid.

## Compact Mobile Reader

Release `0.91` added a compact mobile reading mode tuned for Safari and small screens:

- Bottom navigation is hidden while reading so books, comics, and PDFs get the full viewport.
- CBZ/ZIP comics support single-page, double-page, and vertical webtoon scrolling.
- Webtoon mode removes page buttons and keeps the long-strip comic body as the only scrollable area.
- PDF and image readers use a fixed three-region mobile layout to avoid Safari viewport reflow issues.
- EPUB gets an in-app fullscreen mode with a softened floating exit control.
- Direct page, cover, PDF, and EPUB resource URLs can carry token auth for browser surfaces that cannot attach headers.

This mode is meant for phone and tablet browsing. Native clients can still use the stable Client API manifests and implement their own reading UI.

## Release 0.93 Highlights

Release `0.93` adds anchored webtoon reading positions for long-strip comics and PDF/image webtoon layouts:

- Webtoon progress is saved as a stable page key plus normalized vertical offset, not just a global scroll percentage.
- Restoring webtoon mode prefers `pageKey` anchors and falls back to page index or document progress when needed.
- Switching between single, double, and webtoon modes no longer immediately overwrites the previous reading position.
- The service exposes `GET /api/books/:id/reading-position` and `PUT /api/books/:id/reading-position/webtoon` for native clients.
- Legacy `/progress` remains compatible through `locator: "webtoon:<fraction>"`, so older clients and MCP progress tools keep working.
- Webtoon image joins are hidden more cleanly to reduce visible seams between adjacent long-strip pages.

## Release 0.931 Hotfix

Release `0.931` is a PDF webtoon stability hotfix:

- PDF webtoon mode no longer renders every PDF page into canvas at once.
- Only the current PDF page and its near neighbors are rendered; distant pages use lightweight placeholders.
- PDF webtoon canvas DPR is capped to reduce memory pressure on Safari, iPadOS, and other mobile browsers.
- The release also includes the latest fullscreen comic reader display fixes merged from GitHub.

## Release 0.932 Hotfix

Release `0.932` is a large-library scan performance hotfix:

- Full-library scans now preload existing file index rows once per job instead of querying SQLite for every unchanged book.
- Unchanged CBZ/ZIP/PDF/7z entries can fast-skip from file metadata without reopening archives or forcing page analysis.
- Existing nested comic collections are not reclassified during normal unchanged scans, avoiding expensive churn on very large libraries.
- Root-level legacy collection migration is still preserved for older `Unsorted`-style imports.
- The change keeps the on-demand analysis model: unchanged comics do not need page metadata populated before they can be skipped.

## Release 0.95

Release `0.95` collects the post-`0.932` reader and library-state fixes:

- Narrow-screen cover cards now keep stable portrait frames, preventing tall or intrinsic cover images from stretching shelves and search results.
- Collection favorite and liked state is preserved during book reclassification, so private collection state follows the active collection instead of being left on old series IDs.
- The favorites page count now matches the visible favorite sections and no longer counts hidden empty collections.
- Image webtoon mode no longer leaves large black gaps in compact or fullscreen layouts after viewport width changes.
- Loaded webtoon images now size from the real image dimensions while unloaded placeholders keep scroll height stable.

## Release 0.96

Release `0.96` adds a fast import path for large libraries:

- Library scans can now run in `mode: "recent"` to index only the newest new or changed files under a library or subdirectory.
- The Tasks page exposes a "scan latest added" action with selectable limits for common manga import batches.
- Duplicate running scans for the same library and target path are reused instead of creating overlapping jobs.
- MCP now exposes `foliospace.scan_recent`, matching the HTTP API and allowing agents to trigger recent-file scans safely.
- Client capability discovery advertises `recentScan: true` from `/api/client/info`.

## Release 0.961 Hotfix

Release `0.961` is a cleanup and cover-refresh hotfix:

- ZIP/CBZ archive listing ignores macOS resource fork files such as `__MACOSX/` and `._*`, fixing doubled page counts and broken pseudo-pages in affected archives.
- Continue Reading, Favorites, Want to Read, and recent shelves hide stale entries when the indexed file has been deleted or changed on disk.
- Thumbnail cache keys were refreshed so re-analyzed books do not keep old generic placeholder covers.
- Service, Client API, and MCP metadata report version `0.961`.

## Release 0.965

Release `0.965` adds paginated catalog APIs for native clients:

- `GET /api/client/books` returns a client-safe paginated All Books catalog with `limit`, `offset`, `q`, `sort`, `direction`, and `format`.
- Book catalog responses include `manifestUrl` so clients can open an item directly through the stable manifest route.
- `GET /api/collections` supports an optional paginated shape for Home / collection overview screens, including `primaryType=book` or `primaryType=comic`.
- Legacy `GET /api/collections` without query parameters remains an array response for existing web UI compatibility.
- `/api/client/info` advertises `bookCatalog: true` and `collectionCatalog: true`.

## Release 0.966

Release `0.966` adds embedded JSON metadata support for comic archives:

- ZIP/CBZ scans now read small embedded metadata JSON files such as `metadata.json`, `info.json`, `comicinfo.json`, and `元数据.json`.
- Archive metadata fields `name`, `author`, `description`, and `tags` map onto the existing book title, creator, description, and tag fields without a database migration.
- Search now matches public archive tags and creators, so tags like `C106`, `中文`, or custom pack tags can find the indexed book.
- Public archive tags are merged with profile-private tags in book API responses while keeping user private state separate.

## Release 0.969

Release `0.969` improves metadata and scan hygiene for mixed book/comic libraries:

- PDF scans now read lightweight embedded Info metadata when available: title, author, and subject are mapped to existing title, creator, and description fields.
- Libraries can define scan exclude directories from the web UI, API, or MCP.
- The scanner skips common generated folders such as `media`, `thumbnails`, `covers`, `__MACOSX`, and `@eaDir`, preventing artwork and sidecar folders from being indexed as books.
- Service, Client API, and MCP metadata report version `0.969`.

## Release 0.970

Release `0.970` improves game library organization and user-curated shelves:

- User-defined manual collections can now group books, games, and videos without moving files on disk.
- Game assets support profile-scoped favorite and liked state in the web UI, Client API, and MCP.
- Game catalog browsing can filter by platform groups derived from indexed game collections.
- Game metadata helpers expose provider information and `gamelist.xml` export for launcher-style integrations.
- Service, Client API, and MCP metadata report version `0.970`.

## Release 0.975

Release `0.975` is a stability and performance hotfix for large game libraries:

- The web game catalog now loads a smaller first page, reducing initial cover-wall pressure on NAS deployments with large ROM collections.
- The Client Home API no longer fans out concurrent SQLite reads for every home section, avoiding single-connection queue pressure during startup.
- Game and collection private-state lookups now only read state for the current page of items.
- Game list sorting and filtering add SQLite expression indexes for title and platform-heavy browsing.
- Service, Client API, and MCP metadata report version `0.975`.

## Release 0.996

Release `0.996` adds audited Point Blank launch support for SpatialEMU Apple clients:

- Exact `ptblank` and `ptblanka` fingerprints now route to FBNeo instead of MAME metadata.
- iOS, iPadOS, and visionOS profiles use stable packaged-core `coreBuildId` identities while retaining strict rejection of unknown builds and legacy SHA-256 compatibility for older approved profiles.
- `ptblank` resolves with `namcoc75.zip`; `ptblanka` resolves with its parent `ptblank.zip` plus `namcoc75.zip`.
- `namcoc75.zip` is reconciled to catalog role `dependency`, keeping it out of client game directories while preserving resolver access.
- Service, Client API, Web, and source MCP metadata report version `0.996`.

## Release 0.995

Release `0.995` expands safe native game delivery, compatibility curation, and offline access:

- Nintendo 3DS libraries now validate direct `.3ds`/`.cci` NCSD images, `.cxi` NCCH images, and safe single-image ZIP packages before indexing. Launchable images stream as their original inner bytes, while `.cia` packages are explicitly marked for client-side installation instead of being advertised as directly launchable.
- ZIP-backed game downloads now support single-range requests even when the inner archive stream is not seekable, enabling resumable Nintendo DS, Nintendo 3DS, and other validated single-ROM downloads.
- Game Curation can rebuild FBNeo or MAME compatibility for one game without deleting unrelated profiles. The MAME audit includes a fingerprint-pinned exception for the verified Time Crisis package that embeds its exact `namcoc71` device ROM, without renaming or rewriting the source ZIP.
- Android ARM64 launch resolution accepts the pinned Flycast v4 runtime identity for Dreamcast, NAOMI, Atomiswave, and audited NAOMI 2 packages. Split NAOMI 2 sets require their checksummed parent ZIP, while user-managed firmware is not injected into Android manifests.
- Book manifests expose a byte-exact authenticated download URL with HTTP Range and HEAD support for offline reading.
- The Client Home API can omit collection expansion for faster first-screen loading, and the web client uses this lightweight mode. Scans also skip `_maintenance` directories by default.
- Existing Client API routes remain backward compatible. Service, Client API, Web, and source MCP metadata report version `0.995`.

## Release 0.994

Release `0.994` expands native game delivery and adds stable offline identity for books and comics:

- Nintendo DS `.nds` files and supported single-ROM ZIP packages are indexed as canonical `nds` games, matched against Nintendo DS artwork, and negotiated only with the exact `melonds-ds` core on supported physical Apple clients.
- 3DO `.cue`, `.iso`, and `.chd` images are indexed as canonical `3do` games. CUE manifests preserve every referenced track, exclude BIOS files from the public catalog, and require an Opera-compatible client runtime.
- Konami Python 1 `.py1` descriptors are indexed as one game with their seven validated relative dependencies, complete file checksums, and an explicit `pcsx2-reliquary` launch contract.
- PC-98 mixed packages containing `USER.FDI` plus CUE/BIN CD media are published as one launchable game with a complete ordered manifest instead of separate or missing entries.
- Audited NAOMI Project Justice revisions preserve canonical clone and parent identities, while Atomiswave manifests include the shared `awbios.zip` dependency when required.
- Game file downloads support HTTP Range requests, enabling resumable downloads and large-image streaming without restarting from byte zero.
- Book, EPUB, PDF, CBZ, and ZIP DTOs add nullable `contentHash`, `contentHashAlgorithm`, `fileSize`, and `contentRevision` fields. A serialized background worker computes full-file SHA-256 values without blocking list or manifest requests and invalidates them when source bytes or page manifests change.
- Existing Client API routes remain backward compatible. Service, Client API, Web, and MCP metadata report version `0.994`.

## Release 0.993

Release `0.993` adds Nintendo Virtual Boy support and a server-owned game-platform catalog:

- Virtual Boy `.vb` and `.vboy` ROMs are classified as `platform: "virtualboy"` and exposed through existing game list, facet, manifest, and cover APIs.
- Local `boxart` directories are matched using normalized ROM and artwork names and take priority over previously cached network artwork.
- `GET /api/client/games/platforms` returns stable platform IDs, display titles, aliases, indexed counts, and availability so clients can build filters without a hard-coded platform allow-list.
- `/api/client/info` advertises the additive `gamePlatformCatalog` capability; older clients and servers can continue using `/api/client/games/facets`.
- MCP adds `foliospace.get_game_platform_catalog` while retaining the existing inventory-only platform facets tool.
- Existing Client API response shapes remain backward compatible, and Service, Client API, Web, and MCP metadata report version `0.993`.

## Release 0.992

Release `0.992` stabilizes game delivery and improves responsiveness on large NAS libraries:

- Game clients use the established manifest-first flow by default; the optional launch resolver remains capability-gated so older and mobile clients keep a safe fallback path.
- Legacy manifests include required parent, BIOS, and device dependencies for audited arcade games, including Capcom QSound and ZN packages.
- ZIP classification inspects archive contents before filename short-name guesses, preventing Virtual Boy, SNES, NES, Game Boy, GBA, and Mega Drive ROMs from being misclassified as CPS/MAME games.
- Self-contained MAME clone sets can validate merged parent ROM content without requiring a separate parent archive.
- Search results prefer launchable catalog games over same-name `needs-curation` records.
- The Game Curation Center batches metadata, artwork, profile, file, and checksum status queries instead of issuing per-game database reads.
- The web home screen loads core sections progressively, pages collections on the server, fetches maintenance data on demand, and refreshes only scan state while a job is running.
- Existing Client API response shapes remain backward compatible, and Service, Client API, Web, and MCP metadata report version `0.992`.

## Release 0.991

Release `0.991` is a focused web catalog hotfix for CPS libraries:

- CPS-1, CPS-2, and CPS-3 collection labels now map to the canonical `cps1`, `cps2`, and `cps3` platform values used by the server.
- The game shelf builds platform filters from the full client facets endpoint, so displayed counts match the launchable catalog returned by `/api/client/games`.
- Dependency archives and `needs-curation` records no longer inflate platform counts that lead to empty filtered pages.
- Existing API response shapes and native-client behavior remain backward compatible.
- Service, Client API, Web, and MCP metadata report version `0.991`.

## Release 0.990

Release `0.990` introduces the Game Curation Center, a complete post-scan workflow for turning raw ROM imports into a clean, client-safe game catalog:

- A dedicated **Game Curation** page separates published games, dependencies, and `needs-curation` records with actionable issue messages.
- Fresh installations enable automatic post-scan analysis by default, while administrators can change the behavior at any time under compatibility and metadata settings.
- Compatibility analysis classifies scan results and rebuilds audited FBNeo/MAME launch profiles from deployment-supplied policy files without moving or rewriting ROMs.
- Client catalogs publish only launchable game records, preventing incomplete archives and unresolved dependencies from surfacing as broken entries.
- Bounded background tasks provide visible progress and prevent duplicate compatibility, cover, or metadata jobs from running concurrently.
- Local artwork matching supports common sidecars, cover folders, and `media/<ROM name>/boxFront.*`; optional Libretro matching can fill remaining artwork.
- Administrators can edit titles, descriptions, genres, developers, publishers, dates, regions, and external metadata choices directly from the web interface.
- Optional Hasheous metadata lookup is hash-based and opt-in. Local-only mode remains the default and network failures never block scanning or client manifests.
- First-run setup now includes game-catalog automation and advanced policy paths, so new NAS installations can establish the workflow before their first game scan.
- Service, Client API, Web, and MCP metadata report version `0.990`.

See [`docs/operations/game-catalog-curation.md`](docs/operations/game-catalog-curation.md) for setup, policy, and recovery details.

## Release 0.982

Release `0.982` makes audited arcade launch profiles durable and rebuildable for existing libraries:

- Audited launch profiles and their complete entry/dependency closure are persisted in SQLite instead of being limited to a small hard-coded list.
- The explicit `foliospace-rebuild-launch-profiles` maintenance command validates existing FBNeo ZIP sets against a deployment-supplied official Arcade DAT without rescanning or rewriting ROM files.
- The same command can audit selected MAME platforms against an official MAME 0.288 `listxml`; Model 2 uses ZIP stems as canonical set names and publishes only archives whose complete parent/device/BIOS closure is available.
- The production Model 2 audit retains all 32 indexed game archives: 20 exact MAME 0.288 matches are published with launch profiles, while 12 incomplete sets remain visible to maintenance tooling as `needs-curation`; `segabill.zip` remains a dependency only.
- Each archive is checked by logical ROM name, uncompressed size, CRC, and its complete parent/BIOS dependency chain before it is published as playable.
- Windows FBNeo profiles require the exact approved core SHA-256; an incorrect or unknown core continues to return `409 runtime-profile-not-available`.
- SFC/SNES launch negotiation now recognizes the Windows Libretro bsnes family in addition to the existing Snes9x and Mesen-S routes.
- Pragmatic launch negotiation accepts canonical physical-device identities for iPhone, iPad, Apple Vision Pro, and Apple TV, so supported ordinary runtimes no longer fail solely because the client is not Windows or macOS. Simulator and generic placeholder identities remain rejected.
- MAME and FBNeo remain target-specific and audited. The rebuild command now accepts deployment-supplied client targets, allowing Apple physical-device profiles to coexist with Windows profiles while preserving exact FBNeo core hashes and separate MAME 0.287/0.288 content-set audits.
- Rebuilding one MAME version no longer hides a game that still has a ready profile under another audited MAME policy.
- Client game lists, facets, search, and played shelves no longer advertise dependency or `needs-curation` records as playable games.
- Existing users do not need to rescan their game libraries. Only records missing stable hashes or rejected by the DAT audit need a targeted rescan or ROM-set repair.
- Service, Client API, and MCP source metadata report version `0.982`.

The FBNeo DAT is intentionally not bundled in the image. Mount or copy it to `/config/policies/fbneo-arcade.dat`, then run the rebuild once after upgrading:

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

## Release 0.981

Release `0.981` adds tiered launch-profile negotiation for native game clients while preserving the existing manifest contract:

- `POST /api/client/games/:id/resolve` selects a compatible profile from the authoritative client and runtime inventory in the request body.
- Ordinary console platforms reuse their validated canonical manifests. Libretro routes match a known platform/core family without requiring a per-build core hash; Dolphin and Supermodel also accept runtimes without a reliable product version.
- Dreamcast, Saturn, PlayStation, PC-FX, PC-98, PSP, PlayStation 2, GameCube, Nintendo 64, and other ordinary platforms no longer require one manually authored profile per game.
- Curated DOS entries reuse the existing `dosLaunch` contract and accept Windows DOSBox Staging `0.82.2.0` while preserving the exact selected runtime tuple in the response. Ambiguous DOS entries remain unavailable until curated.
- The initial 0.981 seed included Windows MAME 0.288 profiles for Virtua Striker and Tekken Tag Tournament; 0.982 replaces that seed-only behavior with the audited rebuild flow above.
- CPS1/CPS2/CPS3 sets use pinned MAME 0.288 family classification; audited `sf2`, `sfa`, and `sfiii` profiles require the exact bundled FBNeo core hash.
- Audited MAME 0.288 profiles cover `hypreact`, `hypreac2`, `srmp4`, `fromancr`, `fromanc4`, and `mcnpshnt`; the latter receives `ym2413.zip` as a logical dependency alias while the physical source remains unchanged.
- Resolver manifests include stable profile revisions, logical filenames, available checksums, exact selected runtime identity, and the canonical dependency closure.
- Unsupported runtime combinations return `409 runtime-profile-not-available` instead of silently choosing a nearby ROM set.
- Existing clients continue using `GET /api/client/games/:id/manifest` unchanged.
- MCP adds `foliospace.resolve_game_launch_profile` for the same exact negotiation flow.
- Service, Client API, and MCP metadata report version `0.981`.

## Release 0.98

Release `0.98` expands game-platform indexing and adds a curated DOS launch contract:

- PSP, Nintendo GameCube, and PlayStation 2 files receive canonical platform metadata, web filters, and client-safe catalog entries.
- NAOMI 2 catalog filtering now uses its own stable platform identity instead of being folded into adjacent arcade groups.
- MCP adds `foliospace.list_game_platforms`, backed by full-library facets rather than a partial game page.
- Curated DOS archives can be indexed from `games.json`, including title, metadata, cover mapping, executable launch command, working directory, and package manifest data.
- DOS launch manifests preserve archive-relative paths and expose the launch command clients need to start the correct executable after extraction.
- Service, Client API, Web, and MCP metadata report version `0.98`.

## Release 0.978

Release `0.978` adds profile-scoped game play-time synchronization for native emulators and agents:

- Clients report cumulative active play seconds for a stable launch session through idempotent heartbeats, so retries and out-of-order reports do not double-count time.
- `GET` and `PUT /api/client/games/{gameId}/play-stats` expose first/last played time, total play seconds, and launch count without exposing NAS paths.
- MCP exposes `foliospace.get_game_play_stats` and `foliospace.report_game_play_session` for agents using the same profile-scoped data.
- `/api/client/info` advertises `gamePlayStats: true` for capability detection.
- Service, Client API, and MCP metadata report version `0.978`.

## Release 0.977

Release `0.977` expands FolioSpace Library's ROM ingestion and launch-manifest support across six additional console and arcade platforms:

- Dreamcast GDI/CDI/CHD games are classified as `dreamcast`; GDI descriptors and every referenced track are delivered as one launchable package.
- Sega Saturn CUE/BIN and single-file ISO games are classified as `saturn`; referenced audio/data tracks no longer appear as separate catalog entries.
- NEC PC-FX CUE/CCD/TOC/CHD/M3U libraries support multi-disc grouping, Pegasus metadata, case-insensitive dependency lookup, and local cover directories.
- Nintendo 64 `.z64`, `.v64`, and `.n64` ROMs are validated by byte-order headers; supported single-ROM ZIPs expose the original ROM payload to clients.
- NEC PC-98 floppy and hard-disk images gain format validation, CP932 title decoding, multi-disk grouping, duplicate-image merging, artwork sidecars, and ordered launch manifests.
- Sega Model 2 MAME ZIP sets are classified as `model2`, receive friendly titles and compatibility states, use the operator-arcade input profile, and keep runtime BIOS packages searchable without inflating platform counts.
- Client API facets, game manifests, MCP metadata, web platform filters, and authenticated downloads expose the canonical platform identifiers and complete package files without leaking NAS paths.
- Service, Client API, and MCP metadata report version `0.977`.

## Release 0.976

Release `0.976` is a T0 memory-safety and reader ergonomics release:

- Image decode and thumbnail transforms now enforce source-size, decoded-pixel, output-pixel, and concurrency limits.
- PDF thumbnail rendering runs only in the bounded background worker instead of spawning synchronous render work on cache misses.
- Thumbnail cache misses use a bounded in-memory queue and batched SQLite writes, preventing cover walls from blocking the single database connection.
- The server applies a 768 MiB Go memory budget by default, while the Docker Compose example adds a 1.5 GiB container hard limit.
- Comic single-page reading adds contain, fit-width, and fit-height display modes plus left- and right-handed control layouts.
- Service, Client API, and MCP metadata report version `0.976`.

## MCP

Agent integration docs are in [`docs/mcp/usage.md`](docs/mcp/usage.md). The MCP server wraps the stable Client API for diagnostics, library lookup, manifests, favorites/private-status shelves, preferences, private reader state, game private state, progress, scan jobs, recent-file scans, scan worker settings, job control, manual collections, and collection access. Heavy media streams still use the HTTP URLs returned by the API.

End users can install the MCP binary on the machine where their agent client runs:

```bash
curl -fsSL https://foliospace.app/install-mcp.sh | sh
```

Release maintainers can build macOS/Linux MCP packages with:

```bash
VERSION=0.996 ./scripts/build-mcp-release.sh
```

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

The included Compose file is ready to use without cloning or building the
application source. It pulls the published multi-architecture image for Linux
AMD64 and ARM64 hosts.

Download the two deployment files into an empty directory:

```bash
curl -O https://raw.githubusercontent.com/funland/foliospace-Library/main/docker-compose.yml
curl -o .env https://raw.githubusercontent.com/funland/foliospace-Library/main/.env.example
```

Edit `.env` and set the host directories that contain your media. Then start
the service:

```bash
mkdir -p data/config data/library data/books data/games data/videos
docker compose up -d
docker compose ps
```

Open `http://<docker-host>:8080`. On a fresh `/config`, the setup page asks for
an access key and lets you choose a container path such as `/library`, `/books`,
`/games`, or `/videos`.

The default deployment currently pins this image:

```bash
docker pull funland/foliospace-library:0.996
```

To upgrade later, change `FOLIOSPACE_IMAGE` in `.env`, then run:

```bash
docker compose pull
docker compose up -d
```

The `/config` host directory is writable and persistent. Media directories are
mounted read-only. Back up `/config` before an upgrade because it contains the
SQLite database, generated covers, preferences, and runtime cache.

Example Synology-style `.env` paths:

```dotenv
FOLIOSPACE_CONFIG_PATH=/volume1/docker/foliospace-library/config
FOLIOSPACE_LIBRARY_PATH=/volume2/Media
FOLIOSPACE_BOOKS_PATH=/volume2/ComicCenter
FOLIOSPACE_GAMES_PATH=/volume2/GameROMS
FOLIOSPACE_VIDEOS_PATH=/volume2/MovieCollection/Movies
```

If port `8080` is already occupied, change only the host port:

```dotenv
FOLIOSPACE_PORT=18080
```

For a one-off NAS deployment without Compose:

```bash
docker run -p 8080:8080 \
  -v /volume1/docker/foliospace-library/config:/config \
  -v /volume2/ComicCenter:/library:ro \
  -v /volume2/Books:/books:ro \
  -v /volume2/GameROMS:/games:ro \
  -v /volume2/MovieCollection/Movies:/videos:ro \
  -e FOLIOSPACE_DIRECTORY_ROOTS=/library,/books,/games,/videos \
  funland/foliospace-library:0.996
```

If a directory is missing from the setup page, add its Docker volume mapping
first. FolioSpace Library can only browse paths visible inside the container.

For source development, overlay the build configuration explicitly:

```bash
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
```

### Publishing

Docker Hub releases are built by GitHub Actions from Git tags. Configure these repository secrets before publishing:

- `DOCKERHUB_USERNAME`
- `DOCKERHUB_TOKEN`

Then create and push a version tag:

```bash
git tag v0.996
git push github v0.996
```

The workflow builds `linux/amd64` and `linux/arm64` images, then pushes `funland/foliospace-library:0.996` and `funland/foliospace-library:latest`.

## Current MVP Support

- P0 reading formats: `.cbz`, `.zip`, `.epub`.
- P0 game formats: `.nes`, `.sfc`, `.smc`, `.gba`, `.gb`, `.gbc`, `.nds`, `.3ds`, `.cia`, `.gdi`, `.cdi`, `.chd`, `.iso`, `.bin`, `.cue`, plus validated PC-98 floppy and hard-disk image formats. Nintendo DS `.nds` files are exposed as single-entry games for the exact `melonds-ds` mobile runtime. 3DO `.cue`, `.iso`, and `.chd` images use canonical `3do` metadata; CUE tracks remain dependencies and require a client-reported Opera core. `.zip` and `.7z` are treated as ROM sets only when the library type is `game`; PC-98 ZIP ingestion is limited to one validated media image and does not accept `.7z`, RAR, or TAR containers.
- Series derivation: immediate parent directory, with root-level files grouped under `Unsorted`.
- Reading: backend streams one ZIP image entry or EPUB resource at a time.
- Games: backend indexes local ROM metadata and checksums, exposes client-safe launch manifests without NAS paths, and lazily caches supported Libretro boxart under `/config/cache/game-covers`. Dreamcast `.gdi`, Saturn `.cue`, and PC-FX `.cue`/`.m3u` sets are indexed as one launchable game; referenced track files remain dependencies instead of separate catalog records. PC-FX multi-disc folders are merged into one virtual M3U package, and Pegasus metadata plus local `media/.../boxFront.*` or `PC-FX Covers/* [正面].jpg` artwork is used when available. PC-98 media uses canonical `pc98` / `PC-98` / `np2kai` metadata, decodes CP932 archive names, rejects firmware/tool/DOS support files, merges byte-identical mirrors by raw-image SHA-1, and groups explicitly numbered disks into one ordered launch manifest.
- Errors: empty files, archive open failures, walk errors, and unsupported future categories are recorded as structured rows.

Near-term expansion priority:

1. Keep existing EPUB/comic reader APIs stable.
2. Add game asset indexing for local ROMs and ROM sets.
3. Add spatial photo / spatial video indexing.
4. Move data model language from Book/Series toward Asset/LibraryItem after the first non-reading asset type is real.
