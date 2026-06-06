# FolioSpace Library MCP Reference

This MCP server gives agents a safe control surface over FolioSpace Library. It is for lookup, diagnostics, manifests, preferences, private state, progress, and scan operations. It is not the normal transport for page images, EPUB resources, or ROM bytes; agents should use the opaque HTTP URLs returned by the Client API when they need to point a native client at media.

The server accepts both standard MCP stdio `Content-Length` framed messages and newline-delimited JSON-RPC messages for clients that use a simpler stdio transport.

## Quick Install

For end users, the recommended path is to install a release binary on the machine where the MCP client runs:

```bash
curl -fsSL https://foliospace.app/install-mcp.sh | sh
```

This installs `foliospace-mcp` to:

```text
~/.local/bin/foliospace-mcp
```

Release packages are expected at:

```text
https://foliospace.app/releases/foliospace-mcp_0.91_darwin_arm64.tar.gz
https://foliospace.app/releases/foliospace-mcp_0.91_darwin_amd64.tar.gz
https://foliospace.app/releases/foliospace-mcp_0.91_linux_arm64.tar.gz
https://foliospace.app/releases/foliospace-mcp_0.91_linux_amd64.tar.gz
https://foliospace.app/releases/checksums.txt
```

Override the release URL when testing another host:

```bash
curl -fsSL http://localhost:8081/install-mcp.sh \
  | FOLIOSPACE_MCP_RELEASE_BASE_URL=http://localhost:8081/releases sh
```

## Build From Source

```bash
go build -o ./bin/foliospace-mcp ./cmd/foliospace-mcp
```

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
List my configured FolioSpace libraries, then start a scan for the Books library.
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

- `foliospace.client_info`: service name, version, supported formats, and capability flags.
- `foliospace.home`: continue reading, recent books, and collections.
- `foliospace.search_books`: search indexed books and comics.
- `foliospace.open_book_manifest`: open a CBZ/ZIP/EPUB/PDF client manifest by `bookId`. Manifests include `readerModes` and `defaultReaderMode` so clients can expose single-page, double-page, or webtoon/vertical-scroll controls without guessing from the extension. CBZ/ZIP page entries include `pageKey`, `url` for the original image, and `displayUrl` for a server-downsampled mobile/tablet-safe image. PDF manifests expose the opaque PDF stream URL; clients should use HTTP Range capable reads against that URL.
- `foliospace.list_games`: list paginated client-safe ROM assets with `limit`, `offset`, `q`, `platform`, `format`, and `sort`.
- `foliospace.open_game_manifest`: open a ROM client manifest by `gameId`.
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
- `foliospace.list_collections`: list collections with profile-scoped favorite and liked flags.
- `foliospace.save_collection_state`: save collection `favorite` and `liked` flags.
- `foliospace.list_collection_volumes`: list books/comics in a collection with optional `limit`, `offset`, `q`, and `sort`.
- `foliospace.list_collection_assets`: list mixed collection assets by `collectionId`.
- `foliospace.scan_library`: start a library scan by `libraryId`; optional `path` scans one container-visible subdirectory or file inside the library root.
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
