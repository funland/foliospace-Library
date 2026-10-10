# Preview poster batch and home synchronization — 2026-10-10

Scope: user-authorized missing-poster batch and home/recent-video display fix on
175:18081 only. No production replacement, commit, push, or public release.

## Batch result

- 96 videos inventoried; 27 already had cached or recognized local posters.
- 50 additional posters fetched successfully through the existing authenticated
  UI, stored candidate selection, and background artwork worker.
- Current coverage: 77/96; 19 left for review. Existing 27 were not targeted.
- 61 titles queried, with 7 alternate-title retries. Eight files were excluded
  before querying because of sidecar conflicts, samples, or segmented shows.
- New associations were created only for unmatched files. Existing local work
  metadata was preserved when only its poster was missing.
- The test database contains 51 TMDB posters including the previous single-film
  trial. All cached posters have work links; no active/failed metadata jobs were
  observed at final audit. Other user-created searches were not modified.
- One UI timing check stopped before acting on file #37; refreshed DOM showed
  the correct file, then it was processed successfully with explicit load waits.

## Remaining review list

| IDs | Title / reason |
| --- | --- |
| 13, 14 | DOUDOU spring/summer/autumn/winter live parts: segmented concert, not assumed to be a movie |
| 16 | Eternity (2025): multiple same-year candidates |
| 17 | Fly (2024): multiple same-year candidates |
| 19 | Good Luck Have Fun Don't Die: file 2025, candidate 2026 |
| 27 | Othello sample: not associated as a separate full movie |
| 29 | Japanese Noise file versus White Noise local title: identity conflict |
| 44 | The Red Tree: file 2021, candidate 2022 |
| 52 | Gundam Seed Freedom file versus Pop-God NFO: identity conflict |
| 54 | The Furious / 火遮眼: file 2025, candidate 2026 |
| 62 | Obsession: multiple titles, likely candidate 2026 versus file 2025 |
| 64 | Tokyo Taxi: 2009 and 2025 candidates, filename lacks year |
| 71 | 小武: file 1998, candidate 1997 |
| 73 | 心迷宫: file/local 2014, candidate 2015 |
| 74 | 悲情城市: ambiguous `1988+1` filename/local 1988 |
| 78 | Human Resource / 最后的KPI: file 2025, candidate 2026 |
| 89 | 燃比娃: file 2025, candidate 2026 |
| 91 | 站台: filename 2000, local 2001 |
| 93 | Love and Bruises / 花 file versus unrelated Japanese NFO; existing user search preserved |

These are matching uncertainties, not playback restrictions. No source movie,
NFO, local image, or filename was changed. Metadata correctness is based on
filename/title/year review, not watching all 50 films.

## Home / recently indexed video display

- `VideoPoster.tsx` is shared by home video cards, the legacy video tile list,
  new poster wall, and new file list. It uses `/api/videos/{id}/artwork/poster`
  exclusively, with a static failure placeholder rather than frame generation.
- Source changes reset the failed-image state; per-view cache keys and metadata
  revision keys refresh changed posters when returning to a view.
- Home's existing newest-first `videoShelf` now explicitly says 最近入库 and
  shows 2:3 posters. The separate book-only recent section is hidden when all
  configured libraries are video-only, avoiding stale subtitle-ZIP book entries.
  Such historical records were not deleted from the database.
- React guidance applied: shared component, lazy/async images, stable per-mount
  state, explicit source reset, no extra metadata fetch waterfall, decorative
  fallback accessibility. No routing/framework/deployment-target changes.

Validation: 40 frontend tests passed; TypeScript and Vite build passed. Vite's
pre-existing large-chunk warning remains. No backend source change this turn.
Initial test run before dependencies were restored failed for missing TypeScript;
the final lockfile-based install and full rerun passed.

Only the bound test frontend was updated (assets first, then atomic index swap).
JS `index-CVv9b9zZ.js` SHA-256:
`a530ae4ab1e490aa055d425d160ea871ffb93912efe5234d2d9493d34240ff8a`.
Browser verified home posters #96/#94 from TMDB, #95 from local art, and #93's
static placeholder; no `thumbnail` image URL on video cards.
Screenshot: `tmp/acceptance/nas-home-poster-sync.png` (not staged).

Production container remains `ee416282ee8d72000ed73a82d42644597e539a629881fc48ef66f287eeb17694`,
started `2026-10-03T03:59:25.895305435Z`. Preview container remains
`b8670eb9cc56c36076a886b287bd90b3acfc8a9fc14cd1f78c02f822c856666d`,
started `2026-10-10T06:27:31.41758189Z`; neither backend restarted for this UI fix.
User authentication, read-only media mounts, and test-only provider config kept.
Source worktree remains uncommitted; original dirty checkout was not modified.

## Generated-output cleanup

After successful build/browser checks, `lsof` reported no open files in the local
dependency/bootstrap targets. Removed reinstallable `web/node_modules` and the
one-off npm bootstrap (169,648 KiB summed allocation). Local `df -k` reported no
measurable free-space delta in this sample; do not equate summed sizes with
observed free space. Preserved current dist, source, screenshots and test data.

On NAS, `/proc/*/fd` inspection found no open files in the explicit cleanup
allowlist. Removed the upload staging copy, temporary previous index, and two
superseded hashed JS/CSS files only (5,351,407 summed file bytes); measured NAS
free-space delta was also zero. These outputs are reproducible from source.
Current assets, credential files, DB, media and rollback containers remain.
