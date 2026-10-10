# Home collections and cover wall — 2026-10-10

> Historical preview checkpoint. For the subsequent authorized production upgrade, see [production upgrade](video-p0-production-upgrade-2026-10-10.md).

User authorized updating 175:18081 only. No production deployment, commit,
push, database mutation, rescan or metadata rematching performed.

## Fix

The previous poster fix left both legacy book collection panels on the home
page. Home now embeds the same `VideoLibrary` used by the video catalog:
`/api/client/video-works` supplies real works, with bounded 24-item paging,
search, movie/series filtering and per-work versions/episodes. Shared
`VideoPoster` supplies cached/local artwork, not frame extraction.

Home labels explicitly identify 作品集 · 电影与剧集 and 封面墙. All files remains
available, including unmatched videos. The cache-purge control stays in the
dedicated catalog, not the homepage.

Both old collection and book cover-wall panels are excluded for video-only
libraries. Mixed/reading/game libraries retain their existing panels; video
records and stale book records inside video-only directories are excluded from
that collection list. No historical record or source file was deleted.

Files changed this iteration: `web/src/App.tsx`, `VideoLibrary.tsx`,
`home-library.ts`, `video-library.css`, `video-library.test.mjs` and this report.
Existing phase-one changes remain uncommitted and unstaged. The original dirty
checkout was not edited.

## Validation

- 43 frontend tests passed, including video-only, empty, mixed, reading and game
  section gates, stale-record exclusion, and guarding BOTH legacy panels.
- TypeScript and Vite passed; existing >500 kB chunk warning remains.
- Browser against authenticated real 18081: 83 works; 24 cards on first page;
  page two 25–48 / 83; search 百米 returns one work; details expose its one file
  and `/api/client/videos/1/file`. 人工智能 details also verified.
- Empty search/type intersection displayed its explicit empty state. All files
  still showed 96 files. No downloads, playback or transcoding started.
- 386px viewport: two poster columns, no horizontal overflow; 1280px: five
  columns, no overflow. Zero legacy collection/volume-wall panels on video home.
- English and Chinese headings verified; restored Chinese and cleared temporary
  viewport override. Browser error log empty.
- Screenshots: `tmp/acceptance/nas-home-works-mobile.png` and
  `tmp/acceptance/nas-home-works-desktop.png` (not staged).
- Existing unmatched/conflicting metadata and poster choices are unchanged;
  UI verification is not an assertion that every pre-existing match is correct.

## Deployment evidence

Base source: `2333ee57c6291449fe3921453d07652c7913968e`, branch
`codex/video-library-phase1`, with preserved phase-one uncommitted changes.
Build: lockfile `npm ci --ignore-scripts --no-audit --no-fund`, `npm test`,
`npm run build`, bundled Node 24.19.0 and npm 11.6.2.

New JS `index-BwL_jPEO.js`, SHA-256
`002c4d332f4f7928e45492f1ef0977b659356ffd39d585ea8bbd9090e202543f`;
CSS `index-Vm2rEPS2.css`. Assets copied first, index switched atomically.
Initial unprivileged staging creation failed with permission denied before any
write; retried using sudo for the already-authorized test-only directory.

Production ID `ee416282ee8d72000ed73a82d42644597e539a629881fc48ef66f287eeb17694`,
started `2026-10-03T03:59:25.895305435Z`, unchanged.
Preview ID `b8670eb9cc56c36076a886b287bd90b3acfc8a9fc14cd1f78c02f822c856666d`,
started `2026-10-10T06:27:31.41758189Z`, unchanged.
Authentication, media mounts, provider credentials and databases untouched.

After validation, removed NAS staging copy, temporary previous index and two
superseded JS/CSS files after `/proc/*/fd` checks found no active users.
Summed file bytes: 5,350,730; measured free-space delta: 0 bytes. Current usable
frontend and all containers retained. Local build cleanup recorded below.

Local `lsof` and process checks found no active use of this build's dependencies
or npm bootstrap. Removed only `web/node_modules` and
`/private/tmp/foliospace-npm.MR8nvI` (169,632 KiB summed allocation), both
reinstallable. `df -k` showed 0 measured free-space delta. Kept current dist,
source, screenshots and reports. Final `git diff --check` passed; nothing staged.
