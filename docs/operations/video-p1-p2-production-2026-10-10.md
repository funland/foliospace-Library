# Video P1/P2 production acceptance — 2026-10-10

User explicitly authorized P1/P2 implementation, production 175:18080 replacement
and the existing native test clients' in-place upgrade. Public publication, version
bump, Git push and App Store submission are not part of this task.

## Source and runtime

- Isolated worktree: `/private/tmp/foliospace-video-library-p1-p2`, branch
  `codex/video-library-p1-p2`, from local main `73944012a26bb880d71144fe13ab5d50edaba340`.
  P1/P2 source was subsequently committed as
  `7c4b67a438ceefaf122ebb17079fe059ef4c65de` under explicit user authorization.
  See the [complete baseline](production-baseline-video-p1-p2-2026-10-10.md);
  do not use pristine public 1.02 as production.
- Running container: `0fa590d6bb1c0fc9681935117753534743dc27a49f3ebd0a9b6d51dc6dcb83f8`.
- Server SHA-256: `0b803ae92af67949d62b054ee546ea8e7056e0c613a0fd87cf900d7f8d4e2d73`.
- Build: Go 1.24.7, `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath`.
- NAS release directory: `/volume2/docker/foliospace-library/releases/video-p1-p2-20261010`.
  Its `source.tar.gz` is the exact build-source snapshot; later acceptance-only
  documentation changes do not change that binary. Private inspect/environment
  receipts stay there and must never be committed or copied into a client brief.
  Source archive SHA-256: `fb1c2c2d144b51b0e4a56e93ddc8f5066a2f20c24b2cbc72053466cc6aac3e84`.
- Original image, token, complete environment, all media mounts and port bindings
  retained. Only the read-only server executable mount changed. The P0 web bundle
  `index-BwL_jPEO.js` and its mount are unchanged. Native clients own new browsing
  and viewing interactions; no new web decoding or transcoding work was added.

## Delivered contract

See [video-library-p1-p2](../api/video-library-p1-p2.md): capability discovery,
work counts/library filter/latest-file sorting, stable season/episode IDs and
version file filtering, bounded tvshow.nfo/season inheritance with conflict
rejection and manual grouping protection; profile/file-specific playback events,
idempotency and optimistic revision conflict, continue/next-up; optional explicit
TMDB single-season expected episodes with unavailable/expired/unknown fallback.

Existing file/manifest IDs and routes remain compatible. Progress conflicts never
block file streaming. No playback position is silently shared between cuts or
transferred after regrouping/content identity changes. Profile selection uses the
existing shared-token household model, not separate authentication accounts.
TV-06 complex multi-episode files and SRC-01 extra providers remain deferred.
The old-thumbnail poster bridge is still separate from this P1/P2 scope.

## Verification

- Full `go test ./...`, `go vet ./...`, focused `go test -race` across store,
  service, HTTP and scanner passed; final store/scanner rerun also passed.
- New coverage: 1,001-work deterministic pagination and library isolation;
  specials/two seasons/empty season/three versions; NFO updates, ambiguous numbers,
  symlinks/entities/root boundaries; duplicate event/payload mismatch/out-of-order
  session/stale revision/concurrent devices/profile isolation; explicit watched
  and unwatched; content/regroup identity invalidation; provider purge and repeated
  migrations preserve progress; local/missing/unreleased/unknown and cache expiry.
- TMDB season adapter verified against official API documentation with bounded
  mock responses. No production expected-season fetch or whole-library scan was
  triggered during rollout. Existing TMDB credentials/config were preserved.
- Rehearsal: reflink config clone plus SQLite online backup, loopback-only 18082.
  All 45 old tables unchanged (file-index worker timestamp excluded), integrity
  `ok`, pre-existing foreign-key issues unchanged. Five additive tables only.
  Synthetic playback writes/replay/409 checks occurred only on that copy.
- Production: five new capabilities true; 89 works; works/detail/files/seasons/
  episodes/availability/playback-state/continue/next-up return 200. Video Range
  206; book manifest/Range, PC-98 and NDS manifest/Range pass. Old web entry present.
  No eligible Triforce sample was returned by the platform query; no new Triforce
  device/gameplay validation is claimed. No synthetic progress writes on production.
- Web source unchanged: no new frontend build or visual behavior is claimed here.
  Native client compilation, installation and real playback are separate evidence.

## Recovery and retention

Previous production container `foliospace-library-rollback-video-p0-20261010`
and stopped-state `rollback-config` retained. Do not restore that database over
new user progress without reconciliation. The old binary can read additive tables;
deployment failure handling would restart it without discarding new data.
Production still requires `FOLIOSPACE_PRESERVE_GAME_CATALOG_ON_START=1`.

The temporary 18082 rehearsal container and its disposable config have been
removed after mount/process/open-file checks. The NAS `docker` path resolves to
`docker_1`; cleanup verified that physical path before deletion. Summed size was
17,307,697,152 bytes, but measured immediate disk-free change was only 20,480
bytes because the copy used shared reflink extents (and filesystem accounting is
not a promise of immediate reclamation). Do not report 17 GB as reclaimed space.
Rollback assets, original P0/P1 sources and current runtime remain.
Original dirty Documents code is unchanged; only its existing session handoff
received a new production-baseline notice. No Git staging/push/public image
release is implied by production deployment.

## Client handoff

Exact protocol brief delivered to `Folio Lume 主线开发 · Build 95 接续` and live
18080 availability confirmed to that session. It owns native implementation,
four-platform build checks and authorized existing Mac/iPad in-place test upgrades.
Native implementation passed 323 tests in 34 suites and final macOS, iOS,
visionOS and tvOS builds. A retry-body JSON key-order nondeterminism found in
the last regression run was corrected before rebuilding the final packages.
The existing Mac and iPad (2) test clients were upgraded in place and launched:
`com.funland.foliolume.multiplatform.dev`, version `0.85`, build `2026101003`.
No uninstall or user-data reset was performed. These are unlocked personal test
packages, not App Store distribution builds. Vision Pro and Apple TV have build
evidence only, not new device installation or playback acceptance.

Client evidence is under `/Users/deadseafu/Documents/XRPlayer/build/foliospace-p1p2`;
the authoritative client worktree's
`docs/testing/FOLIOSPACE-P1P2-CLIENT-HANDOFF-2026-10-10.md` owns the final cleanup,
connection and UI acceptance record. Mac was already playing a movie after
installation and was not interrupted for further UI checks. Installation does
not establish that every new P1/P2 feature was exercised against production:
the selected connection is NOT CONFIRMED in either installed client. The OS
denied direct app-container reads and the active Mac playback was left alone;
no privacy permissions were changed. `18081` remains the older preview with a
different token; do not silently overwrite saved connections.

Client closeout is complete with these explicit acceptance limits: cross-device
resume/seek/conflict and natural-EOF countdown still need user device testing;
production provider refresh/manual regroup were not exercised. Disc ISO and
multi-title timelines retain local resume rather than file-seconds server sync.
Existing unrelated client diagnostic dirty source was preserved and included in
these development builds; exact source hashes/patch are retained, and this is
not a clean commercial-release-source assertion. The original deployment task
did not commit or publish P1/P2; the later user-authorized local-commit closeout
is recorded in the complete baseline document. No public publication is implied.

Client cleanup retained current successful platform products and removed only
verified superseded P0 outputs/unused intermediates. Allocated sizes removed:
2,304,331,776 bytes; measured filesystem free-space increase: 2,143,260,672
bytes (2.00 GiB). Sources, checkouts, release archives and app data remain.
