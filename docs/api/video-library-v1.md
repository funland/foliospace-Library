# Movie / TV library (development phase 1)

This additive API is implemented on top of public 1.02. It is not yet a published
Docker release. Existing `/api/client/videos`, manifests, IDs, file URLs and HLS
routes remain valid. Original file delivery additionally supports HEAD alongside
GET and byte Range. The metadata provider is disabled by default.

The accepted P0 is retained in the local production service at port 18080;
the current mainline baseline also includes P1/P2. See the
[complete baseline](../operations/production-baseline-video-p1-p2-2026-10-10.md)
and the historical [P0 production record](../operations/video-p0-production-upgrade-2026-10-10.md).
This does not imply that a new public Docker image has been released.
Subsequent local P1/P2 deployment adds [season, progress and next-up APIs](video-library-p1-p2.md).
The phase-1 exclusions below describe that original phase, not the later additive APIs.

## Responsibilities and quick use

FolioSpace Library catalogs user-owned files and supplies metadata, posters and
authenticated original-file URLs. A dedicated client such as FolioLume owns
playback/decoding. This phase does not change FolioLume or the existing web HLS
policy. It does not download movies or supply copyrighted media.

1. Add a **Video** library pointing to an existing read-only media mount and scan it.
2. Open **Videos → Poster wall**. Files recognized from local hints become
   provisional movie/series works. **All files / unmatched** shows *all* files,
   including unknown ones; it is not an unmatched-only filter.
3. Open a work for its files, versions and numbered seasons/episodes. Use
   **Manage metadata** to protect a file title/year/overview, confirm its work,
   supply a version label, unlink, or upload/reset a poster/backdrop.
4. Use an existing work ID (displayed in its detail) to merge another file/version
   into that work within the same library. Confirmed/unlinked associations survive
   rescans. Unmatched files remain downloadable/playable without online matching.
5. Original file links are for native clients. The explicit web Play action retains
   the old web-player behavior, including existing HLS where necessary. The new
   poster wall never starts frame extraction; the legacy browser is opt-in.

File overrides and work associations are separate: protecting a **file title**
does not rename the whole series. File overrides are exposed by the metadata API
and work-file list; legacy catalog/manifest titles are preserved for compatibility.
Change/confirm the **work title** to change the poster-wall association.

## Local input rules

- Movie hint: `Title (2001).mkv` or `Title [2001].mp4`.
- TV hint: `Show S01E02.mkv`; `Show/Season 01/S01E02.mkv` can inherit the folder
  title. There is no fuzzy/AI match. Ambiguous files stay in the file catalog.
- NFO: same-stem `.nfo` first; `movie.nfo` only in a single-video directory.
  Supported XML roots: `movie`, `episodedetails`; fields: title, plot, year,
  showtitle, season, episode, `uniqueid type="tmdb"`. IDs are hints, not automatic
  provider confirmations. No remote URLs inside NFO are fetched.
- NFO reads cap at 1 MiB; symlinks/nonregular files, declarations/entities,
  invalid XML/numbers fall back to filename metadata with a warning.
- Posters: `stem-poster`, `stem`, `poster`, `folder` plus jpg/jpeg/png/webp.
  Backdrops: `stem-backdrop` or `fanart`. Uploaded/provider art takes precedence;
  reset returns to local discovery. Missing art uses a static placeholder.
- Art: 10 MiB / 20 million pixels maximum, validated JPEG/PNG/WebP; no SVG uploads.
  No ROM/media file or user sidecar is rewritten.

## Optional TMDB

Before enabling, the operator must independently register a valid TMDB credential
and verify applicable use/terms. Do not assume that open-source licensing determines
API commercial-use status. The Library/noncommercial scenario and any paid
FolioLume integration require separate assessment; this phase grants neither a
commercial license nor permission to send a private library to a provider.

Server-only environment variables (do not put credentials in the browser, Git,
URLs, screenshots or logs):

| Variable | Behavior |
| --- | --- |
| `FOLIOSPACE_VIDEO_METADATA_PROVIDER=tmdb` | Explicitly enable the adapter |
| `FOLIOSPACE_TMDB_TOKEN` | Operator's API read-access bearer token |
| `FOLIOSPACE_TMDB_LANGUAGE` | Optional; defaults to `en-US` |

Both provider and nonempty token are required. Existing Compose operators must
explicitly pass these variables to the container; no default Compose secret was
added. Restarting/redeploying production is a separate authorized operation.

Search only sends the user-entered title query, kind and configured language to
the fixed TMDB API. It does not scan/upload your library, files or paths. Results
are candidates; a human must select one. Poster/backdrop download is another
explicit action from a stored candidate, never an arbitrary supplied URL.

One persistent worker, at most one active job per file, 1,000 queued/running jobs
globally, at most three attempts, global provider cooldown, HTTP deadlines and
bounded responses. Search honors Retry-After for 429/5xx; image failures retry
with bounded backoff. Running jobs recover on process restart when enabled.
Cancellation and changed-input generations reject late results; retry creates a
new explicit search after a terminal failure. Recent history is bounded per file.

Provider data is cached for 150 days (below six months), swept at startup, hourly
while the provider worker runs, and before relevant reads. Expiry removes
provider text/art/candidates, falls back to the original catalog title, and keeps
files/IDs, manual fields and local sidecars. The worker does not run while disabled;
on restart the expiry sweep precedes serving cached provider data. Content-addressed
unreferenced artwork is garbage-collected; manual referenced artwork remains.
**Clear TMDB cache** clears provider records/art/candidates and cancels queued work.
For termination, disable provider configuration first, clear cache, and remove
provider data from any operator-managed historical backups as appropriate. The
server never automatically deletes your backups or credentials.

Official sources verified 2026-10-10:
[terms](https://www.themoviedb.org/api-terms-of-use),
[FAQ](https://developer.themoviedb.org/docs/faq),
[attribution assets](https://www.themoviedb.org/about/logos-attribution),
[authentication](https://developer.themoviedb.org/docs/authentication-application),
[rate limiting](https://developer.themoviedb.org/docs/rate-limiting).
The footer includes the official unmodified logo (`web/public/tmdb.svg`) and
required attribution. Terms were reviewed, not accepted or registered on a user's
behalf. Synthetic mocks, not real TMDB results, were used for development tests.

## HTTP contract

All routes inherit existing token/cookie authentication. No filesystem paths or
provider credentials are returned. `limit` defaults to 30, caps at 100; `offset`
is nonnegative. Pages contain `items,total,limit,offset,hasMore`.

| Method / route | Purpose |
| --- | --- |
| GET `/api/client/video-works?q=&kind=movie&limit=24&offset=0` | Stable title/ID sorted works; optional movie/series filter |
| GET `/api/client/video-works/{workId}` | Detail, representative videoId and posterUrl |
| GET `/api/client/video-works/{workId}/files?season=1&limit=24&offset=0` | Versions, videoId, title, season, episode, format, original fileUrl |
| GET `/api/client/videos/{id}/metadata` | Effective local + protected fields, workId, status, revision, generation |
| GET/PUT/DELETE `/api/videos/{id}/metadata` | Read/set/reset a file field |
| POST `/api/videos/{id}/metadata/match` | Confirm manual work/version association |
| POST `/api/videos/{id}/metadata/unlink` | Explicit unlink with generation |
| POST `/api/videos/{id}/metadata/refresh` | Queue candidate search (202) |
| GET `/api/videos/{id}/metadata/candidates` | Latest persistent job; 404 if none |
| GET/DELETE `/api/video-metadata-jobs/{jobId}` | Status/cancel |
| POST `/api/videos/{id}/metadata/select-match` | Confirm one stored candidate |
| POST `/api/videos/{id}/artwork/select` | Queue candidate poster/backdrop (202) |
| GET/HEAD/PUT/DELETE `/api/videos/{id}/artwork/{poster\|backdrop}` | Read/upload/reset art; read supports ETag/Range |
| GET/DELETE `/api/videos/metadata/provider` | Enabled status / clear provider cache |

Field PUT: `{"field":"title","value":"My title"}`. DELETE: `{"field":"title"}`.
Allowed fields are `title`, `overview`, `year`; empty strings intentionally lock
the field, while DELETE unlocks and exposes the latest local value.

Manual movie match:

```json
{"generation":4,"kind":"movie","title":"My Movie","year":2001,"overview":"Notes","version":"Director cut · 4K"}
```

Series matches additionally require `season` and `episode` (zero is allowed for
specials). Existing `workId` must belong to the same library. Unlink body:
`{"generation":5}`. Fetch metadata again after a successful change.

Search body: `{"query":"My Movie","kind":"movie"}`. Selection: match fields plus
`jobId` and zero-based `candidate`; server uses the stored provider result, not
client-supplied provider text. Search generation must still match. Artwork
selection: `{"jobId":12,"candidate":0,"role":"poster"}`. Manual artwork PUT sends
raw image bytes with `?generation=N`, not multipart or a remote URL.

Job states: queued, running, ready, failed, cancelled, stale, expired. Jobs expose
attempts, nextAt (Unix seconds), errorCode and candidates. New routes return
stable `code`: `metadata-revision-conflict` (409), `provider-disabled` (503),
`metadata-queue-full` (429), `not-found` (404), `invalid-request` (400). A stale
metadata edit is unrelated to game launch/core identity policy.

## Migration / rollback boundary

Migration is additive and repeatable; original video tables/IDs are not rebuilt.
Before an eventual deployment, make a consistent SQLite backup with `VACUUM INTO`
or stop the service and back up the entire config directory. Do not copy only the
main DB file while WAL writes are active. Also back up the generated-art cache to
preserve manual uploads. Restore an isolated config copy and verify old file IDs,
Range and protected metadata before changing production. Rollback uses the saved
config/image together; never try to drop the new tables in a live production DB.

## 中文摘要

本期是服务端影视资料库，不是新转码系统。视频库新增海报墙、电影/剧集、季集、
版本与原文件入口；全部文件仍包含未匹配项目。文件名/NFO/本地图片可以独立使用，
人工字段和已确认/已解除的关联不被重扫覆盖。在线 TMDB 默认关闭，需要运营者自行
确认用途与条款并配置服务端 token；只发送主动搜索词，不上传影片或整库。
候选需人工确认，图片单独选择。署名、缓存到期和清除入口已实现。
网页播放沿用已有逻辑，客户端负责主要播放解码。本阶段不修改客户端、不发布镜像、
不迁移生产、不启用真实提供方。复杂动漫命名、tvshow.nfo 继承、ISO/BDMV 深层解析、
多集单文件、全部花絮类型、观看进度同步、AI 与 Jellyfin 同步不在本期范围内。
