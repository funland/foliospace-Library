# NAS 175 isolated video preview — 2026-10-10

> Historical preview checkpoint. For the subsequent authorized production upgrade, see [production upgrade](video-p0-production-upgrade-2026-10-10.md).

User authorized a test service on 175, not replacement of production or public
publication. Local implementation acceptance remains recorded separately.

> Current state: the synthetic preview below has been superseded by the
> independently authenticated, read-only real-library preview described at the
> end of this document. The old demo token is not valid for the current service.

- URL: `http://192.168.10.175:18081`, open Video Shelf / 视频库.
- Container: `foliospace-video-preview`, ID
  `e91cf6e83c73223304604b767b411db28dc58b6a1c7bf79d23093b8f6de7740d`.
- Dedicated directory: `/volume2/docker/foliospace-video-preview` (~28 MiB).
- Synthetic data only: five works, seven video files, five generated gradient
  posters, basic local NFOs. Files play the same five-second 640×360 H.264 test
  pattern, not copyrighted movie content. No production media/config mounted.
- Test-only token: `video-preview-test`. No production authority; do not reuse it
  for a real library. TMDB provider and token explicitly empty/disabled.

## Build / runtime

Source: `codex/video-library-phase1`, base
`2333ee57c6291449fe3921453d07652c7913968e` plus the local phase diff.
Go 1.24.7 command: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath
-o tmp/foliospace-video-server-linux-amd64 ./cmd/foliospace-reader`.
Linux binary SHA-256 (matched locally and remotely):
`21b20d20d77694058935df3f863f0c0f607d63dc240ebc3d6ae9f37480b48679`.
Frontend is the previously accepted `web/dist` (index-ADpvy_r_.js).

Reuses the *image*, not the production container, for runtime libraries:
`sha256:d292fdec407a85eecadc8473522d3cf507b1e23ae6d8f5abff9bf9169c2b2ed1`.
New binary bind-mounted at /app/foliospace-library, new dist at /app/web/dist,
both read-only. Dedicated config read/write; dedicated /library read-only.
Entrypoint is the new server binary. Image tag alone does not identify this
preview's source: the binary hash and mounts are part of its provenance.

Bound only to 192.168.10.175:18081. Restart unless-stopped, 384 MiB memory cap,
256 MiB Go memory target, GOMAXPROCS=2, low CPU shares (256), read-only container
filesystem, /tmp tmpfs capped at 64 MiB, dropped capabilities, no-new-privileges,
bounded Docker logs. NAS lacks CPU CFS quota support; no hard CPU cap is claimed.
An initial copied-DB owner mismatch was corrected only within the test config
directory. The current container runs without restarts after that correction.

## Verification / preservation

- Synthetic scan jobs 1 and 2 completed, zero errors; seven indexed files then
  unchanged-file NFO refresh.
- Work list 200 / five works; poster 200 image/png; provider enabled=false.
- File Range bytes=0-31 returned 206, Content-Range bytes 0-31/446863.
- Unauthenticated work request returned 401.
- Browser login, Chinese poster wall and work detail passed. Test video readyState
  4, duration 5 seconds, playback unpaused with advancing currentTime, no media error.
- Idle sample: CPU 0.19%, memory 49.42 MiB / 384 MiB.
- Screenshot: local `tmp/acceptance/nas-175-preview.png` (not staged).

Production before and after is unchanged:
container `ee416282ee8d72000ed73a82d42644597e539a629881fc48ef66f287eeb17694`,
image d292fdec407a..., started `2026-10-03T03:59:25.895305435Z`, port 18080.
No production DB/token/config/media writes, restarts or migrations. No commits,
pushes, version changes, GitHub builds or registry publication.

One-off inspection/encoding containers used --rm. After checking local processes
and open files, removed the Linux transfer binary and temporary copied seed
directory locally (19,872 KiB summed); the latest usable Linux build/data remain
on 175 for user review. Measured local free space rose by 19,768 KiB (filesystem
accounting/concurrent activity differs from summed sizes). Local Mac build and source worktree preserved. This test
container is intentionally retained, not an abandoned verification container.
To stop it after review: `docker stop foliospace-video-preview` on 175. Removal
of its container/data should be scoped explicitly to this name/directory; do not
use any production cleanup or rollback asset path.

## Real-library preview and independent credential — 2026-10-10

The user explicitly authorized using the real library and chose to set an
independent credential. A proposed production-token reuse operation was rejected
before execution and was abandoned. Production credentials were not accessed.

First-run setup was exposed with an empty media mount and a fresh `config-real`
directory. The user completed credential entry in the browser. Only after
`initialized`, `authEnabled`, `tokenConfigured`, and `hasLibraries` were all true
was the real media root mounted read-only. The credential was neither read nor
copied into commands, reports, or environment variables.

- URL remains `http://192.168.10.175:18081/`; open 视频库.
- Current container: `foliospace-video-preview`, ID
  `b513fd1e8c4b3f02c40ebf3eaff93aabf42b7e94b10c143572aea66e13cedb6e`.
- Independent writable index: `/volume2/docker/foliospace-video-preview/config-real`.
- Real media is mounted at `/library:ro`; no media copies or source writes.
- Runtime binary, frontend and resource limits are unchanged from above.
- Unauthenticated `/api/libraries` returns 401; authenticated browser survives
  test-container replacement/restart using the user-configured credential.
- Production ID and start time remain unchanged from above; 18080 is untouched.
- Prior synthetic container is stopped as `foliospace-video-preview-demo`.
  Empty-mount setup container is stopped as
  `foliospace-video-preview-empty-rollback`; neither is running a second service.

Initial user-created library type was automatic. Scan #1 incorrectly attempted
to parse a subtitle ZIP as a comic and two Blu-ray ancillary CCI files as 3DS
games. It was cancelled; the same library was upserted through the normal web
UI as 视频 / video, preserving its ID, index, and authentication configuration.
Only the test container was restarted to terminate outstanding scan work.
Scan #2 uses one worker and the video-only filter. Historical #1 errors are not
evidence of damaged movie files. No original files were renamed or modified.

Scan #2 completed in 10 seconds: 96 discovered video files, 43 newly indexed,
53 skipped as already indexed, zero errors. The poster wall contains 42 works
derived from local metadata; unmatched files remain available separately.
Browser search for 人工智能 returned its 2001 title and local poster. Screenshot:
`tmp/acceptance/nas-175-real-library.png` (not staged). This does not validate
every source NFO's title/poster correctness or every codec's client playback.
The original broad inventory count also included video extensions unsupported
by this phase; do not describe all source files as ingested.

Observed follow-ups, not changed during this deployment: the header can retain
"Scan queued" after Jobs reports completion; some local NFO titles/covers appear
inconsistent; the initial automatic scan left a subtitle ZIP catalog record and
historical errors. These test-index observations require separate review, not
source-media modification or claims that the release is fully accepted.

Provider direction: TMDB, MetaTube, and the user's existing DMM-cover skill.
This is a future integration requirement, not a claim of three implemented
providers: TMDB remains disabled, and no MetaTube/DMM scraping was enabled.

## TMDB single-title live trial — 2026-10-10

Supersedes the TMDB-disabled state above. After explicit user authorization,
the user obtained their own TMDB API Read Access Token and entered it through
a hidden terminal prompt. The helper saved it only on NAS in a root-owned 0600
preview configuration file. No credential was printed, committed, or sent in a
URL. The FolioSpace independent login credential remains unchanged.

An authenticated request to TMDB's `/3/configuration` returned 200 before any
container change. The preview was recreated with provider `tmdb` and language
`zh-CN`, preserving config, limits, and read-only media mounts. Current container
ID: `b8670eb9cc56c36076a886b287bd90b3acfc8a9fc14cd1f78c02f822c856666d`.
Previous preview is stopped as `foliospace-video-preview-pre-tmdb` for rollback.
Production ID/start time were checked unchanged; unauthenticated preview
provider status still returns 401.

Only video #1 was used for the live trial: a normalized `100 Meters` title query
returned a 2016 title and the year-matching 2025 `百米。` candidate. The 2025
candidate was selected, and its poster fetch was queued. No bulk refresh,
source file writes, MetaTube calls, or DMM calls were performed.

Poster job reached `ready`; the authenticated browser displayed `百米。` (2025)
with the downloaded red running-athlete poster. Screenshot evidence:
`tmp/acceptance/nas-175-tmdb-first-poster.png` (local, not staged). This validates
live provider authentication, search, candidate selection, image fetch/cache,
and poster-wall display for one title, not bulk matching accuracy.

Subsequent user-authorized batch and home/recent-video poster synchronization
are recorded in [video-posters-batch-2026-10-10.md](video-posters-batch-2026-10-10.md).
Current preview frontend is `index-CVv9b9zZ.js`; backend identity is unchanged.
