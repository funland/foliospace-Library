# Video library P1/P2 contract

Implementation target: production P0 `7394401`. Additive; old file IDs, manifests,
Range, authentication and playback remain unchanged. Feature flags only become
available after the implementing server is deployed. No public release implied.

## Capability negotiation

`GET /api/client/info` adds booleans `videoLibraryV1`, `videoSeasonsV1`,
`videoPlaybackStateV1`, `videoNextUpV1`, `videoEpisodeAvailabilityV1`.
Absent/false: retain P0/file browsing and local progress. Authentication failures
are not an old-server signal. All calls use existing Bearer/cookie auth.

## Catalog

- Works supports `q`, `kind=movie|series`, `libraryId` and `sort=title|recent`;
  omitted filters mean all accessible libraries. `total` counts works, not files.
  Each work adds `fileCount`, `versionCount`, `availableEpisodeCount`, `addedAt`.
  `addedAt` is the newest associated file's catalog creation time (UTC SQL timestamp),
  and recent sorting uses that value followed by descending work ID.
- `GET /api/client/video-works/{id}/seasons?limit=&offset=` returns a page of
  `{id,workId,number,availableEpisodeCount,fileCount,expectedCount:null}`.
- `GET /api/client/video-works/{id}/episodes?season=1&limit=&offset=` returns a page
  of `{id,seasonId,workId,season,number,title,fileCount,videoId,availability}`.
  `availability=available`; only local episode identities are in this endpoint.
  Season 0 is explicit specials, never implicitly inserted into next-up.
- Existing `/files` additionally accepts `episodeId`; files add nullable
  `seasonId`,`episodeId` and `versionId`. All pages use existing
  `items,total,limit,offset,hasMore`, cap 100, deterministic tie-breaking.
- Existing `/api/videos/{videoId}/metadata/match` with generation, workId,
  season/episode/version remains the manual regrouping route. It preserves file
  IDs and protects confirmed associations from scans. No client-side SQL.

## Progress: per file/version and profile, not shared playhead across cuts

`GET /api/client/videos/{videoId}/playback-state` returns:

```json
{"videoId":1,"profileId":1,"mediaKey":"opaque","revision":0,
 "positionSeconds":0,"durationSeconds":0,"completed":false,
 "serverUpdatedAt":0,"identityChanged":false}
```

Profile: use existing `X-FolioSpace-Profile-Id` (or `profileId` query). Omitted
means default. Invalid/unknown explicit profile is rejected, never silently
written into default. Shared server token grants household profile selection;
these are not separate user accounts. Never accept a profile from event JSON.

`POST /api/client/videos/{videoId}/playback-events`:

```json
{"eventId":"uuid","sessionId":"uuid","clientSequence":1,"baseRevision":0,
 "mediaKey":"opaque from GET","eventKind":"progress",
 "positionSeconds":123.5,"durationSeconds":7200}
```

Kinds: `progress`, `completed`, `unwatched`. Position/duration are finite
nonnegative seconds. No wall-clock ordering from clients. `completed` is explicit;
client may offer mark-watched or send it after natural playback completion.
No server-inferred 95-percent rule. `unwatched` resets position/completed.
Ordinary progress does not undo explicit watched; use unwatched for that action.
Watched is stored per file; an episode is watched when any valid version is watched.
An episode-wide unwatched action must explicitly reset its watched versions.
Success 200 returns state above with incremented revision and Unix-second server
timestamp. Same event/payload replay returns original acknowledgement and never
writes again. Reusing event ID with different payload: 409 `playback-event-conflict`.
Lower/repeated sequence in the same session: 409 `playback-sequence-conflict`.
Stale base revision: 409 `playback-revision-conflict`. Changed content or association:
409 `playback-identity-conflict`. Error body includes `code` and `state` when available.

Client queues are keyed by server+profile+video+mediaKey. On timeout retry the SAME
event. On 409 fetch current state; do not silently rebase old queued events onto a
new revision or merge with max(position). Offer resume/latest/local choice; a new
explicit user action may use a fresh event and current revision. Never let a
background offline heartbeat undo another device's watched/unwatched action.
Capture profile at session start, stop/reconcile on logout/profile switch. Pause,
stop, background and bounded heartbeats (e.g. 30 seconds), not every frame.
Playback remains local-first and never waits for synchronization.

Identity includes file identity and work/version/episode association. Metadata
title edits alone do not reset progress. Changed association/cut exposes zero
progress with `identityChanged=true`, old revision retained for CAS. The next
accepted event starts state for the new identity; old events cannot overwrite it.

## Continue / next-up

`GET /api/client/video-playback?kind=continue|next-up&limit=&offset=&libraryId=`
returns a page. Items: `{videoId,workId,title,posterUrl,season,episode,reason,
positionSeconds,durationSeconds,completed,revision,mediaKey}`.
Continue is unfinished exact file/version state. Next-up uses the most recently
watched regular episode per series, picks the next locally available regular
episode, excludes episodes already watched through any valid version. Reason
`next-available-episode` does not mean intervening episodes are missing. Do not
auto-play across gaps/seasons without user choice; auto-play defaults OFF.
File playback still uses the existing video manifest. No transcoding/download job.

## Optional expected episodes

`GET /api/client/video-works/{id}/availability?season=1&limit=&offset=` returns
`{items,total,limit,offset,hasMore,source,ordering,dateBasis,expectedCount}`.
Items `{season,number,title,airDate,availability,episodeId,videoId}`;
availability `available|missing|unreleased|unknown`. Unknown dates stay unknown;
dateBasis is `UTC-date`, ordering is `tmdb-aired`. No invented release timezone.
Without enabled/fresh external data: source `local`, expectedCount null, only
available local episodes. No inferred missing ranges. Provider disable/expiry
falls back locally; progress survives provider purge.

Optional explicit refresh: POST `/api/videos/works/{id}/availability/refresh`
with `{"season":1}`; uses confirmed TMDB series identity, existing server token,
bounded network access/cache. No automatic per-card provider calls or client key.
Provider unavailable never blocks local browsing/playing.
