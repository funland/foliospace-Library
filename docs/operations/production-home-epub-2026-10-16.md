# Production homepage + EPUB hotfix — 2026-10-16

## Outcome / source boundary

175:18080 now runs the narrow 1.03 homepage/EPUB hotfix. Final successful start:
2026-10-16 18:00:26 Asia/Shanghai (10:00:26.327945118Z). No version bump,
Git commit/push/tag, Docker Hub publication, client change, or 18081 change.

Worktree: `/Users/deadseafu/.codex/worktrees/foliospace-release-1-03/FolioSpaceReader`,
branch `codex/release-1.03`, HEAD `46770f51fe4d1cef1e69437faade8e9a70922964`.
Actual production source baseline `35a48eb13457b900ea571f2117fede4bd480142f`;
the intervening release HEAD changes only Docker Hub docs/workflow. New
Ludoteca/MCP work is excluded; both original dirty trees remain untouched.

Allowlisted source changes (13 files):

- `internal/store/store.go`, `home_performance_test.go`: indexed favorite/status
  predicates, cheap collection total and title-page selection before aggregates;
  preserve aggregate sorts, explicit type filters and empty-status semantics.
- `internal/httpapi/server.go`, `request_timings.go`, `request_timings_test.go`:
  first 16 completed requests per selected category, then at most one slow/error/
  cancelled completion per minute/category. Logs contain category/method/status/
  milliseconds/bytes/cancelled only; no path, query, IDs, headers or content.
  Streams/downloads are excluded. Range/ETag/304/HEAD behavior is tested.
- `web/src/App.tsx`, `epub-viewport.ts`, `epub-viewport.test.mjs`: observe actual
  iframe dimensions, coalesce layout and clean up. Historical nested-column
  reset remains; no unrelated App changes.
- `VideoPoster.tsx`, `video-poster-cache.ts`, `VideoMetadataEditor.tsx`,
  `VideoLibrary.tsx`, `video-library.test.mjs`: stable poster URLs with explicit
  edit/job/cache-clear invalidation. Existing request timeouts/features retained.

## Runtime identity (do not confuse image and overlay)

Same original container:
`a106d8c61b70d5132d72d97ffb55dc5d807e7eb3c6f0a7e02943f823f1bc6bae`.
Its Docker image field still points to official 1.03
`sha256:e1c52376df49963c3dcd0c744dfa1944a823d3971329252a9a105929ac31094e`.
Program/Web files were replaced in that container; **recreating it from the
unchanged public tag alone would lose this hotfix**.

Verified live and over HTTP:

- program SHA256 `3f7528e66f1ddc7f241baf98a6d7fd0e64d282ba5717707955393300b51d5669`
- `/assets/index-BL-qvmGu.js` SHA256 `65323eba39ee091397dd39fc4a37348658db7c77e46378a0abb64d79304287b4`
- unchanged `/assets/index-Vm2rEPS2.css` SHA256 `f10dc7a939dbb2b69d37421487a417640b79f338fde841ba47e2fcfd53893ed0`
- reproducible local NAS image `foliospace-library:home-epub-20261016`, ID
  `sha256:40a3f9e9ff96e83b8fdaf2f6ec5f6575d5fea49ab39eec7d74da272c2f232063`
- server/dist source context SHA256
  `ba76b5f8fe52831d53171c041af6ae00bac479a5619c7e722aa629be1b6c306e`
- complete frontend source archive SHA256
  `013f4807033be6b7b6db4fbb3f4b688f2a6f337fbc9b52c76c6d08e70fa72bef`

Go build: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o
/out/foliospace-library ./cmd/foliospace-reader`, existing `golang:1.23-alpine`,
runtime FROM official 1.03. Web built with existing lockfile-identical dependencies,
TypeScript and Vite 8.0.14; no new package installation. React skill review focused
on cleanup/cache invalidation; Playwright used isolated headless Chrome, not the
user's browser/session or FolioLume GUI.

## Deployment safety / rollback incident

Original container retained, so its opaque environment/token, five media/config
mounts, port/restart/network/memory settings remain. No token was read/reset or
new production access permission granted. No original media or catalog migration.

Before each switch, including after backup: only the server process; no established
8080 socket; no queued/running scan, thumbnail or metadata job; no playback-state
update within five minutes. Earlier ffmpeg had ended naturally. No user transcode
or session was force-stopped. Opaque SQLite backup uses the SQLite backup API;
credential values were not queried. Normal stop/copy/start only.

First attempt started new runtime at 17:57:21 but auto-rolled back when a naive
raw mount-array string comparison failed. Five subsequent read-only inspections
proved array order varied while every mount field was equal after sorting by
destination. The script was fixed to compare sorted complete mount objects,
not to ignore fields. A proposed diagnostic termination was prevented by phase
guards because the first switch had already advanced; no signal was sent.
Second attempt completed successfully at 18:00:26, with `deployed.json` and all
checks passing. It took 10.83s from stop through verification (not a measured
HTTP outage duration). No DB rollback occurred.

Post-update schema hash and all 50 table names match; 14 critical row counts match:
libraries 9, profiles 1, series 1005, books 26803, files 26802, games 21312,
videos 98, works 99, seasons 0, episodes 0, links 91, video playback states 7,
book private states 33, profile reading progress 289. This is not a whole-DB
content hash or full integrity scan. Initial all-table count was too costly;
final check uses bounded critical counts and schema identity instead.

Root and new asset HTTP200, auth enabled, setup initialized. Five protected
routes (info/home/games/video works/EPUB manifest) still reject anonymous access
with 401. No authenticated production API acceptance was fabricated. New-image
isolated Linux smoke test reports 1.03 and movie P0/P1/P2/CIA/reader capabilities;
resolver flags there reflect its empty DB, not production policy.

## Performance evidence and limits

Actual production, read-only diagnostic projections (not full HTTP responses):

- Favorite old: 3s cap on first read, then 270.23/24.76ms; new:
  595.30/0.28/0.28ms. Completed pairs equal (26 rows).
- Want old: 26.38/26.20/24.73ms; new: 24.66/0.10/0.09ms; all pairs equal (4 rows).
- Collections old: three 3s-cap interruptions; new: first capped, then 8.34/8.99ms
  (60 rows). No completed real-data old/new equality claim; 576 differential
  combinations against the old query passed in tests.
- Diagnostic process was observed with ~68MB read over about two minutes,
  `wait_on_page_bit_common`, while server CPU was ~0.2%. This is evidence of
  cold-read cost, not proof of disk failure or the sole historical timeout cause.

Public startup samples from NAS→LAN address, same 12-second timeout, three per
route before/after; first samples have uncontrolled OS caches:

| Route | Before ms | After ms |
| --- | --- | --- |
| `/` | 93.53, 2.11, 1.78 | 12.80, 1.56, 1.50 |
| `/api/setup/status` | 840.19, 2.75, 2.94 | 2.36, 2.37, 2.32 |
| `/api/auth/status` | 1.55, 1.49, 1.45 | 1.37, 1.30, 1.32 |
| `/api/client/info` (expected 401) | 1.49, 1.42, 1.43 | 1.34, 1.32, 1.29 |

Timeouts 0/12 before and 0/12 after. These are **not logged-in homepage timings**.
Production's new timing log has so far captured only the deliberate 401 probes,
not an authenticated home render. Real logged-in latency improvement remains
unmeasured; the intermittent 12s user timeout was not reproduced.

Prior DEV controlled before/after evidence remains in that worktree's
`docs/operations/reader-home-dev-2026-10-16.md`. Current release-candidate sanity
samples use a newly generated EPUB, not the earlier real-book fixture: final
40 API requests (5 sequential + 5 parallel rounds of four routes), 0/40 failures
or timeouts; 9 browser scenarios, 0/9 timeout messages. Three slow-network book
thumbnail requests were aborted on navigation, not timeouts. Earlier
`production-narrow` samples preceded the generated EPUB mtime correction and
must not be treated as a strict same-data before comparison.

Final corrected fixture: home sequential median 1.56ms, collections 3.29ms;
re-entry poster transfers 0 bytes, refresh revalidation 8400 bytes versus
3045336 bytes for first uncached load. Editing a synthetic blue poster shows
the new pixels after edit/re-entry/refresh. No stale-image regression observed.

## Tests, evidence and cleanup

- Full `go test ./...`; store/httpapi race + vet: pass.
- 47 frontend tests, TypeScript, Vite: pass (existing >500KB chunk warning).
- 15 generated-EPUB viewport/font/chapter/fullscreen browser cases: pass;
  actual frame/body dimensions agree. Prior approved real-book DEV evidence is
  separate. No Safari/physical-device production-reading acceptance claim.
- `git diff --check`: pass; nothing staged/committed. Original dirty trees intact.
- Private scripts/results/screenshots: `/private/tmp/foliospace-reader-qa/`.
- NAS protected recovery/source/receipt directory:
  `/volume2/docker/foliospace-library/releases/home-epub-20261016/`.
  Keep previous runtime and both opaque database snapshots; never restore them
  over newer user progress as a routine rollback. Old hashed Web assets remain
  alongside the new entry for already-open tabs. Existing prior rollback
  containers and DEV18081 are untouched.
- Removed this task's no-network/no-mount smoke container and extraction container.
  Removed local disposable server, synthetic DB/cache, EPUB and generated posters
  only after process/open-file checks. Removed borrowed node_modules symlink,
  not its dependency target. Removed NAS build staging/upload duplicates after
  checking process FDs/cwd and container mounts; retained latest usable hotfix
  image, build/source archives and regression evidence.
- Local removed allocated total 58084 KiB; observed `df` available delta
  +534096 KiB. That larger concurrent filesystem delta is not attributed solely
  to this cleanup. NAS build staging allocated size 7456 KiB; exact free-space
  delta in NAS `cleanup.json` is 0 bytes, not 7456 KiB reclaimed. Shared layers,
  filesystem accounting and background activity preclude a reclaimed-size claim.

Independent coordinator public verification at 10:02:25Z also matched final
JS/CSS hashes: root 200 x3 (18.78/17.34/14.11ms), setup/auth 200 and client-info
401 x3. This is independent public-shell evidence, not authenticated latency.

User acceptance: refresh <http://192.168.10.175:18080/>, browse home/movie posters,
then resize/fullscreen an EPUB. Do not point users to a local Codex thread URL.
