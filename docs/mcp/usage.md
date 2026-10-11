# FolioSpace Library MCP Reference

This MCP server gives agents a safe control surface over FolioSpace Library. It is for lookup, diagnostics, manifests, preferences, private state, progress, and scan operations. It is not the normal transport for page images, EPUB resources, or ROM bytes; agents should use the opaque HTTP URLs returned by the Client API when they need to point a native client at media.

The server accepts both standard MCP stdio `Content-Length` framed messages and newline-delimited JSON-RPC messages for clients that use a simpler stdio transport.

## Quick Install

For end users, the recommended path is to install a release binary on the machine where the MCP client runs:

```bash
curl --proto '=https' --proto-redir '=https' -fsSL \
  https://github.com/funland/foliospace-Library/releases/download/mcp-v1.03.1/install-mcp.sh \
  -o install-mcp.sh
sh install-mcp.sh
```

This installs `foliospace-mcp` to:

```text
~/.local/bin/foliospace-mcp
```

The independent [MCP 1.03.1 release](https://github.com/funland/foliospace-Library/releases/tag/mcp-v1.03.1) contains four packages and checksums:

```text
foliospace-mcp_1.03.1_darwin_arm64.tar.gz
foliospace-mcp_1.03.1_darwin_amd64.tar.gz
foliospace-mcp_1.03.1_linux_arm64.tar.gz
foliospace-mcp_1.03.1_linux_amd64.tar.gz
checksums.txt
```

The installer refuses missing/incorrect checksums, verifies initialization, then atomically replaces the executable. URL/token configuration is untouched. One previous binary is kept at `~/.local/bin/.foliospace-mcp.previous`. Reconnect the MCP host to load the update. The older website installer may lag; use the GitHub release above.

For an HTTPS mirror, override the release URL:

```bash
FOLIOSPACE_MCP_RELEASE_BASE_URL=https://your-mirror.example/mcp-v1.03.1 sh install-mcp.sh
```

## Build From Source

```bash
go build -o ./bin/foliospace-mcp ./cmd/foliospace-mcp
```

Release packaging uses Go 1.24+ and `sh scripts/build-mcp-release.sh`. Tags `mcp-v*` publish MCP packages through GitHub Actions without triggering Docker's `v*` workflow. MCP 1.03.1 and server 1.03 are intentionally different versions.

## Runtime Environment

```bash
export FOLIOSPACE_BASE_URL=http://your-nas-ip:8080
export FOLIOSPACE_API_TOKEN=your-token-if-enabled
```

`FOLIOSPACE_BASE_URL` defaults to `http://127.0.0.1:8080` when omitted. `FOLIOSPACE_API_TOKEN` is optional and is forwarded as `Authorization: Bearer <token>`.

## MCP Client Config

Use an absolute path for `command`.

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

If you build from source instead of using the installer, set `command` to the absolute path of your local binary, for example:

```text
./bin/foliospace-mcp
```

## Agent Prompt Samples

After the MCP server is configured, users can ask an agent:

```text
Use FolioSpace Library MCP to show service version, supported formats, and current health.
```

```text
Search my FolioSpace Library for "metal slug" and open the game manifest for the best match.
```

```text
Resolve game 12 for SpatialEMU.Windows 1.302 using the runtime inventory reported by the client.
```

```text
List my configured FolioSpace libraries, then start a scan for the Books library.
```

```text
Scan the latest 20 files added under my Hanman library without walking the whole ComicCenter library.
```

```text
Show recent scan jobs and summarize the latest errors.
```

```text
Find books marked want-to-read and show the first 10.
```

```text
List my FolioSpace profiles, create a "Guest" profile with a game avatar, then show its continue-reading shelf.
```

```text
Open the manifest for book 12 and tell me whether it is EPUB or CBZ.
```

```text
List my local videos, open one movie manifest, and tell me whether it will direct-play or use HLS transcoding.
```

```text
Check whether FolioSpace is currently transcoding a video and which item is occupying the queue.
```

## Tools

- `foliospace.client_info`: service name, version, supported formats, and capability flags such as `webtoonPositionSync` and `pageImageDownsample`.
- `foliospace.home`: continue reading, recent books, and collections.
- `foliospace.search_books`: search indexed books and comics.
- `foliospace.open_book_manifest`: open a CBZ/ZIP/EPUB/PDF client manifest by `bookId`. Manifests include `readerModes` and `defaultReaderMode` so clients can expose single-page, double-page, or webtoon/vertical-scroll controls without guessing from the extension. CBZ/ZIP page entries include `pageKey`, `url` for the original image, and `displayUrl` for a server-downsampled mobile/tablet-safe image. PDF manifests expose the opaque PDF stream URL; clients should use HTTP Range capable reads against that URL.
- `foliospace.list_games`: list paginated client-visible ROM assets with `limit`, `offset`, `q`, `platform`, `romSetName`, `format`, and `sort`. Use `platform: "cps1"`, `"cps2"`, or `"cps3"` for canonical Capcom CPS families, `platform: "mame"` for audited MAME sets, `platform: "model2"` for Sega Model 2, `platform: "virtualboy"` for Nintendo Virtual Boy, `platform: "n64"` for Nintendo 64, `platform: "psp"` for PSP, `platform: "ngc"` for Nintendo GameCube, `platform: "ps2"` for PlayStation 2, `platform: "pc98"` for NEC PC-98, and `platform: "dos"` for IBM-compatible DOS archives. `needs-curation` records remain discoverable and preserve their role; dependency-only records stay hidden. Catalog visibility does not guarantee a strict FBNeo/MAME launch profile. Validated metadata includes `fileName`, `format`, hashes, and an opaque authenticated `downloadUrl`.
- `foliospace.list_game_platforms`: list normalized platforms currently represented by client-visible indexed ROMs, with full-catalog counts. Optional `q`, `platform`, `romSetName`, and `format` filters mirror `GET /api/client/games/facets`. Use it before `list_games` to choose valid library filters; it intentionally omits platform types with no client-visible games. Launch readiness still comes from `foliospace.resolve_game_launch_profile`.
- `foliospace.get_game_platform_catalog`: return every platform recognized by the server, including stable platform ids, display titles, aliases, indexed counts, and availability. Use it to generate dynamic platform menus without a client-side platform allow-list.
- `foliospace.open_game_manifest`: open a ROM client manifest by `gameId`. Sega Model 2 manifests retain the original MAME ZIP filename and bytes and use `inputProfile: "operatorArcade"`. Nintendo 64 manifests expose one validated raw ROM entry. PC-98 manifests expose byte-exact raw disk images even when the server stores each image in a single-media ZIP; numbered multi-disk sets include one `entryFile` plus ordered `entry`/`disk` files with `diskIndex`, and translated titles may include a validated `role: "font"` sidecar without disk metadata. DOS ZIP/DOSZ manifests add `dosLaunch`: `entryFile` is a safe inner-archive executable only when curated metadata or `dosbox.conf` resolves it unambiguously, otherwise it is null and `candidates` provides the safe chooser inventory. PC-98 HDIs missing an active MS-DOS partition or required DOS system files are retained with `compatibility: "broken"`; standalone YU-NO Special Disk media is blocked until it can be modeled as a complete add-on package. Multi-file Dreamcast GDI, Saturn CUE, and PC-FX CUE/M3U manifests likewise include `entryFile` plus an ordered `files[]` list containing every descriptor and required track. Transfer the returned HTTP URLs outside MCP and preserve each returned filename.
- `foliospace.resolve_game_launch_profile`: post an exact client/runtime inventory for a `gameId` and return a stable profile with `launchProfileId`, `profileRevision`, the selected runtime tuple, logical file names, a required SHA-1 for every file, and the complete canonical dependency closure. Canonical Apple device identities are `SpatialEMU.iOS`/`ios-arm64`, `SpatialEMU.iPadOS`/`ipados-arm64`, `SpatialEMU.visionOS`/`visionos-arm64`, and `SpatialEMU.tvOS`/`tvos-arm64`, all with `architecture: arm64`; simulator and generic Apple placeholders are not accepted. Ordinary console/computer Libretro cores do not require an application-build SHA. FBNeo remains strict and may match an approved optional `coreBuildId` or the exact legacy `coreSha256`; MAME remains strict by audited runtime version and content set. PSP normalizes to `ppsspp` and DOS to `dosboxpure`. A PC client may report MAME and FBNeo together in `runtimes`; the server selects a matching audited profile by stable server-side priority, independent of request order, and returns the selected tuple in `runtime`. Curated DOS responses include `dosLaunch`, while ambiguous entries return `launch-profile-missing`. Other unsafe resolutions return stable codes including `runtime-unsupported`, `core-fingerprint-unknown`, `dependency-missing`, `manifest-checksum-unavailable`, and `content-set-mismatch`. Pass nested `client` and `runtimes` values exactly as the native client reports them. Logical aliases such as `ym2413.zip` exist only in strict resolved manifests and never rename the stored source asset. Returned file URLs remain bearer-authenticated and must be transferred outside MCP.
- `foliospace.get_game_metadata_providers`: list game metadata provider status and local artwork import capabilities.
- `foliospace.export_game_gamelist`: export indexed games as `gamelist.xml` with optional catalog filters.
- `foliospace.save_game_private_state`: save profile-scoped game `favorite` and `liked` flags.
- `foliospace.list_played_games`: list profile-scoped games with recorded launches or play time. Supports `limit`, `offset`, `q`, `platform`, `sort=recent|playtime|launches|title`, and `direction=desc|asc`; returns stable game ids, hashes, cover URLs, cumulative seconds, launch counts, and first/last played timestamps.
- `foliospace.get_game_play_stats`: read profile-scoped first/last played timestamps, cumulative play seconds, and launch count.
- `foliospace.report_game_play_session`: report cumulative elapsed seconds for a stable client-generated session id; repeated heartbeat payloads do not double-count time.
- `foliospace.list_videos`: list paginated client-safe video assets with `limit`, `offset`, `q`, `format`, and `sort`.
- `foliospace.open_video_manifest`: open a video client manifest by `videoId`; the returned `fileUrl` is an opaque Range-capable service URL, while `hlsUrl` is used when `playbackMode` is `hls`.
- `foliospace.get_video_transcode_status`: read HLS transcode/cache status for a video; returns `idle`, `starting`, `running`, `queued`, `ready`, or `failed`.
- `foliospace.get_video_transcode_queue`: read the current active global video transcode task, if any.
- `foliospace.list_profiles`: list in-app profiles with avatar and color metadata.
- `foliospace.create_profile`: create an in-app profile with optional `avatar` and `color`.
- `foliospace.update_profile`: update a profile `name`, `avatar`, and `color`.
- `foliospace.delete_profile`: delete a non-default profile and its scoped reading state.
- `foliospace.get_preferences`: read client preferences such as interface language.
- `foliospace.save_preferences`: save client preferences.
- `foliospace.get_scan_settings`: read scan runtime settings such as worker count.
- `foliospace.save_scan_settings`: save scan runtime settings such as `scanWorkers`.
- `foliospace.get_private_state`: read per-book private state.
- `foliospace.save_private_state`: save per-book private state.
- `foliospace.list_favorites`: list favorite books as client-safe DTOs.
- `foliospace.list_private_status`: list books by private status, for example `want`, `reading`, `finished`, or `dropped`.
- `foliospace.get_progress`: read legacy reading progress. Structured webtoon-aware clients should use the HTTP `reading-position` API directly for exact page-key plus Y-offset anchors.
- `foliospace.save_progress`: save legacy reading progress. For webtoon fallback compatibility, a `locator` shaped like `webtoon:<fraction>` is accepted by the HTTP API.
- `foliospace.list_libraries`: list configured libraries for diagnostics and scan selection. This admin tool can expose configured mount paths.
- `foliospace.update_library_excludes`: update scan exclude directory names or relative paths for a configured library.
- `foliospace.list_collections`: list collections with profile-scoped favorite and liked flags.
- `foliospace.save_collection_state`: save collection `favorite` and `liked` flags.
- `foliospace.list_collection_volumes`: list books/comics in a collection with optional `limit`, `offset`, `q`, and `sort`.
- `foliospace.list_collection_assets`: list mixed collection assets by `collectionId`.
- `foliospace.list_manual_collections`, `foliospace.create_manual_collection`, `foliospace.get_manual_collection`, `foliospace.add_manual_collection_item`, and `foliospace.remove_manual_collection_item`: manage user-defined shelves that can mix books, games, and videos.
- `foliospace.scan_library`: start a library scan by `libraryId`; optional `path` scans one container-visible subdirectory or file inside the library root.
- `foliospace.scan_recent`: scan only the latest new or changed files under a library or optional target path. Use this after adding several files to a large directory; `recentLimit` defaults to 20.
- `foliospace.list_jobs`: list scan/import jobs.
- `foliospace.job_events`: list job events by `jobId`.
- `foliospace.pause_job`: request pause for a running scan job.
- `foliospace.cancel_job`: request cancellation for a running, pause-requested, or paused scan job.
- `foliospace.resume_job`: resume a paused scan job by starting a new scan for the same library.
- `foliospace.list_errors`: list scan/import errors, optionally filtered by `jobId`.
- `foliospace.library_health`: service info plus job and error counts.

## Resources

- `foliospace://client/info`
- `foliospace://client/home`
- `foliospace://client/videos`
- `foliospace://client/preferences`
- `foliospace://profiles`
- `foliospace://settings/scan`
- `foliospace://libraries`
- `foliospace://jobs`
- `foliospace://errors`
- `foliospace://health`

## JSON-RPC Examples

Initialize:

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"example","version":"0.1.0"}}}
```

List tools:

```json
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
```

Open a game manifest:

```json
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"foliospace.open_game_manifest","arguments":{"gameId":12}}}
```

List local videos:

```json
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"foliospace.list_videos","arguments":{"q":"movie","format":"mp4","limit":20}}}
```

Open a video manifest and choose playback:

```json
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"foliospace.open_video_manifest","arguments":{"videoId":21}}}
```

Use `fileUrl` when `directPlayable` is `true`; use `hlsUrl` when `playbackMode` is `hls`.

The MCP response only returns service URLs and metadata. The agent should hand the returned `fileUrl`, `hlsUrl`, or `thumbnailUrl` to a web/native client instead of trying to transfer large media bytes over MCP.

Check HLS transcode status:

```json
{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"foliospace.get_video_transcode_status","arguments":{"videoId":21}}}
```

Check which video is occupying the transcode slot:

```json
{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"foliospace.get_video_transcode_queue","arguments":{}}}
```

List profiles:

```json
{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"foliospace.list_profiles","arguments":{}}}
```

Create a profile:

```json
{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"foliospace.create_profile","arguments":{"name":"Guest","avatar":"game","color":"violet"}}}
```

Read another profile's home shelf:

```json
{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"foliospace.home","arguments":{"profileId":2,"limit":12}}}
```

List want-to-read books:

```json
{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"foliospace.list_private_status","arguments":{"profileId":2,"status":"want","limit":12}}}
```

Mark a collection as favorite and liked:

```json
{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"foliospace.save_collection_state","arguments":{"profileId":2,"collectionId":42,"favorite":true,"liked":true}}}
```

Scan one newly added chapter without walking the full library:

```json
{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"foliospace.scan_library","arguments":{"libraryId":1,"path":"/library/韩漫/某作品/Chap.263.zip"}}}
```

Scan the latest 20 new or changed files under a large directory:

```json
{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"foliospace.scan_recent","arguments":{"libraryId":21,"path":"/library/韩漫","recentLimit":20}}}
```

Exclude generated thumbnail/media directories from future scans:

```json
{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"foliospace.update_library_excludes","arguments":{"libraryId":21,"excludePatterns":["media","thumbnails","covers"]}}}
```

Pause a running scan job:

```json
{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"foliospace.pause_job","arguments":{"jobId":42}}}
```

Save interface language preference:

```json
{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"foliospace.save_preferences","arguments":{"interfaceLanguage":"zh-Hans"}}}
```

Read current health:

```json
{"jsonrpc":"2.0","id":7,"method":"resources/read","params":{"uri":"foliospace://health"}}
```

End-to-end local smoke sample using standard `Content-Length` framed messages:

```bash
python3 - <<'PY'
import json
import os
import subprocess

messages = [
    {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "initialize",
        "params": {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {"name": "smoke", "version": "0.1.0"},
        },
    },
    {
        "jsonrpc": "2.0",
        "id": 2,
        "method": "tools/call",
        "params": {"name": "foliospace.client_info", "arguments": {}},
    },
]

payload = b""
for message in messages:
    body = json.dumps(message).encode()
    payload += f"Content-Length: {len(body)}\r\n\r\n".encode() + body

env = os.environ.copy()
env["FOLIOSPACE_BASE_URL"] = "http://your-nas-ip:8080"
env["FOLIOSPACE_API_TOKEN"] = "your-access-key"

result = subprocess.run(
    [os.path.expanduser("~/.local/bin/foliospace-mcp")],
    input=payload,
    stdout=subprocess.PIPE,
    stderr=subprocess.PIPE,
    env=env,
    check=True,
)
print(result.stdout.decode())
PY
```

The smoke test should return JSON-RPC responses for initialization and service info. It is safe because it does not start scans or access media bytes.

For simple diagnostics, newline JSON-RPC is also accepted:

```bash
printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke","version":"0.1.0"}}}' \
  | FOLIOSPACE_BASE_URL=http://your-nas-ip:8080 FOLIOSPACE_API_TOKEN=your-access-key ~/.local/bin/foliospace-mcp
```

## Design Notes

Most MCP responses intentionally avoid NAS file paths. Book pages, EPUB resources, covers, and game files are exposed as service URLs from the HTTP API. The exception is `foliospace.list_libraries` / `foliospace://libraries`, which is an admin/diagnostic surface for choosing scan targets and can expose configured mount roots. Keep performance-sensitive reader and emulator paths on HTTP; use MCP for agent decisions, setup, troubleshooting, and orchestration.

## Movie-library read-only tools

All existing tools remain available. New tools use GET only and carry MCP read-only annotations:

| Tool (prefix `foliospace.`) | Required / optional arguments |
| --- | --- |
| `list_video_works` | Optional q, kind (movie/series), libraryId, sort (title/recent), limit, offset |
| `get_video_work` | workId |
| `list_video_seasons` | workId; optional limit, offset |
| `list_video_episodes` | workId; optional season, limit, offset |
| `list_video_versions` | workId; optional season, episodeId, limit, offset |
| `get_video_availability` | workId, season; optional limit, offset |
| `get_video_playback_state` | videoId; optional profileId |
| `list_video_continue` | Optional profileId, libraryId, limit, offset |
| `list_video_next_up` | Optional profileId, libraryId, limit, offset |

Pagination defaults to 20 and caps at 100. Season 0 means specials. A version's videoId identifies its own playhead; do not merge versions' progress. Unknown expected episode counts remain null, not zero.

Each call checks `/api/client/info` capabilities. Without `videoLibraryV1`, only unfiltered (or q/sort-filtered) work listing falls back to `/api/client/videos`, wrapped as `mode=legacy-files`; it does not pretend files are works. Other unavailable features return `capability-unsupported`. Authentication, transport and invalid profile failures never trigger fallback. An omitted profile uses the server default; an explicit invalid/unknown profile is an error.

Tool failures return `isError=true` with JSON containing `code`, optional HTTP `status`, and conflict `state`. New tools cap HTTP responses at 2 MiB, reject redirects and remove diagnostic/path/credential fields. Content titles and metadata are library data, not instructions. No new scraping, metadata editing or playback-writing tools are enabled by this release.

### Hermes

Configure the existing executable using Hermes' `mcp_servers` configuration, with `command` and `env` matching the client-config example above. Keep tokens in the protected local configuration or environment; never include them in prompts or source control. Existing connections need only the binary replacement and reconnect. Use `hermes mcp test foliospace-library` to check discovery; use `/reload-mcp` in an existing Hermes session to refresh discovery.

For a **new, read-only** connection, Hermes supports `tools.include` as an explicit list of the tool names above plus `foliospace.client_info`. Upgrading an existing connection does not automatically remove or expand its configured tool permissions.

简体中文：MCP 1.03.1 独立于服务端 1.03 发布。请使用上面的 GitHub 安装入口；校验失败不会替换旧程序，原 URL/口令不变，升级后需重新连接 Hermes/MCP 宿主（现有 Hermes 会话可用 `/reload-mcp`）。新增九项电影库只读工具，保留旧工具；分页默认 20、上限 100，显式无效档案不会回退到默认用户。旧服务器仅在支持的列表场景回退为文件列表，不伪装成作品，也不新增刮削、修改元数据或写进度操作。
