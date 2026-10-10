# Video library phase 1 — implementation checkpoint

## Follow-up planning — 2026-10-10

Phase 1 is now implemented and previewed on 18081; follow-up client compatibility,
TV season/episode management, viewing progress and next-up work is tracked in
[Docker video development plan](../product/foliospace-video-docker-development-plan.md),
aligned by VID/TV task IDs with the
[FolioLume client brief](../product/foliolume-video-library-client-brief-2026-10-10.md).
Those follow-ups are planned, not implemented or authorized for deployment.
Latest preview UI evidence: [home works and cover wall](video-home-works-2026-10-10.md).

## Historical checkpoint

Historical foundation checkpoint, superseded by the completed
[2026-10-10 local acceptance](video-library-phase1-acceptance.md).
The remaining-work section below records the earlier state, not today's status.

Base: public 1.02, `2333ee57c6291449fe3921453d07652c7913968e`.
Worktree: `/private/tmp/foliospace-video-library-phase1`, branch
`codex/video-library-phase1`. Development only: no commit, push, provider enablement,
production migration, restart, deployment or release is authorized by this checkpoint.

## Implemented foundation

- Transactional additive work/season/episode/version/file-link schema and local
  observations / field overrides. No backfill, replacement or renumbering of videos.
- Separate manual title/overview/year overrides. Empty strings remain intentional;
  explicit deletion of an override restores the latest local value.
- Filename/year and basic SxxExx hints; same-stem NFO and unambiguous single-video
  folder movie.nfo. XML movie/episodedetails, title/year/plot and TMDB ID hints.
- NFO size cap, directive/entity rejection, symlink/nonregular rejection, safe
  malformed-NFO fallback. Hints do not constitute confirmed provider matches.
- Existing serial/concurrent scans refresh local observations on unchanged videos,
  hashing only bounded NFO inputs rather than entire media files.
- Authenticated GET `/api/client/videos/{id}/metadata` and GET/PUT/DELETE
  `/api/videos/{id}/metadata`. PUT body: `{"field":"title","value":"My title"}`;
  DELETE body: `{"field":"title"}`. Allowed fields: title, overview, year.
- Old manifests/catalog/file/thumbnail/HLS handlers remain unchanged.

## Verification

Tests were first run red (missing types/functions), then implemented and passed.
Go 1.24.7: `go test ./...`, `go vet ./...`; focused store/scanner/HTTP tests cover
repeat migration, stable video IDs, manual empty values, unchanged-file NFO updates
in serial and concurrent scans, malformed NFO/symlink fallback, auth and path privacy.
Tests use disposable databases/fixtures, never production data.

## Remaining before phase 1 acceptance

This is NOT the completed movie-library feature. The relational tables are only a
foundation: no work grouping/confirmation APIs or poster wall are available yet.

1. Add work association service with cross-work relationship validation, persistent
   confirmed/unlinked state, revision concurrency checks, and paginated queries.
2. Add bounded persistent metadata jobs using Library's own task pattern (not the
   external subtitle queue), cancellation/recovery/backoff and independent artwork tasks.
3. Provider-neutral matching + TMDB adapter, candidates/manual adoption, attribution,
   complete terms verification before enabling any online provider.
4. Artwork roles, local discovery/revision, bounded remote fetch/cache and fallback;
   no synchronous frame extraction from new poster-wall browsing.
5. Web poster wall/details/seasons/versions/manual actions and UI tests. No FolioLume changes.
6. Migration backup/restore exercise, legacy Range regression expansion, pagination
   and larger-library performance tests, restart/obsolete-result tests and acceptance.

Known limited parser scope: no complex anime numbering, tvshow.nfo inheritance,
multi-episode filename expansion, ISO/BDMV indexing or arbitrary remote NFO URLs.
No new version number/capability is advertised while these workflows are incomplete.
