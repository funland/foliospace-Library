# Complete local production baseline — P0 + P1/P2

User-authorized baseline closeout, 2026-10-10 (Asia/Shanghai). The authoritative
server baseline is the full service running on `192.168.10.175:18080`, not the
older public Docker image and not the independent P0 preview on `18081`.

## Preserved history

| Layer | Commit | Retained scope |
| --- | --- | --- |
| Public 1.02 | `2333ee5` | Previous reader/game APIs, CIA install, FBNeo compatibility, archive Range, PC-98 fonts and Triforce fixes |
| Movie-library P0 | `0e20af95c56a053f976e4ec1ac1f5f2bec76d880` | Local metadata/artwork, TMDB, works/posters, home/recent/collections artwork and safe production startup |
| P0 mainline record | `73944012a26bb880d71144fe13ab5d50edaba340` | Accepted P0 baseline and client/server responsibilities |
| P1/P2 implementation | `7c4b67a438ceefaf122ebb17079fe059ef4c65de` | Seasons/episodes/versions, library filters, profile playback events, continue/next-up, optional expected episodes, tests and protocol |

These form a linear ancestry, not a selection of cherry-picked features. The
local `main` is advanced to the documentation closeout descending from all of
them. Continue from that `main` or a verified descendant. The original Documents
checkout remains on its old branch with unrelated dirty work intact; do not use
it as a production source merely because it is the primary checkout path.

## Production/source identity

- Container `0fa590d6bb1c0fc9681935117753534743dc27a49f3ebd0a9b6d51dc6dcb83f8`
  was rechecked running with zero restarts during commit closeout.
- Executable SHA-256:
  `0b803ae92af67949d62b054ee546ea8e7056e0c613a0fd87cf900d7f8d4e2d73`.
- Deployed source archive SHA-256:
  `fb1c2c2d144b51b0e4a56e93ddc8f5066a2f20c24b2cbc72053466cc6aac3e84`.
- All 107 committed backend/module files match that archive byte-for-byte.
  Documentation closeout changes no runtime source. The web tree is unchanged
  from accepted P0, including its movie library and poster surfaces.
- Five P1/P2 capabilities are true on the authenticated production info route;
  the existing reader/game/CIA/DOS capabilities remain advertised.
- Server still reports `1.02`; this string does not mean the public 1.02 image
  contains P0/P1/P2. No version bump or image publication occurred here.

For executable/web mounts, source snapshots, rollback assets and verification
details see [production acceptance](video-p1-p2-production-2026-10-10.md).
The token, data, media mounts and `FOLIOSPACE_PRESERVE_GAME_CATALOG_ON_START=1`
remain unchanged. This commit operation did not deploy, restart, scan or migrate
production. Credentials, database copies, ROMs and `tmp/` are not committed.

## Verification and remaining boundaries

Commit closeout reran full `go test ./...`, `go vet ./...`, staged whitespace and
file-allowlist checks. Backend source parity is stronger evidence than a version
label, but does not replace device/gameplay validation. Earlier rehearsal,
race/HTTP/Range checks are in the production acceptance record.

Client P1/P2 local-main commit:
`e4daaf62d845fbb3488158180c6d83a10fa51b4b` (30 scoped files), descending from
P0 `b2ebc97fdf67943e0cc004f1a49b46e8e1cc06b8` and earlier client features.
Authoritative checkout:
`/Users/deadseafu/.codex/worktrees/xrplayer-priority-mainline/XRPlayer`.
See its `docs/testing/FOLIOSPACE-P1P2-CLIENT-HANDOFF-2026-10-10.md` for test,
build and in-place Mac/iPad installation evidence. Shared playback/project files
were staged by feature-only content, preserving their original working copies.
Pre-existing audio/renderer/resource-loader diagnostics and other dirty files
remain uncommitted. The installed development packages include those additional
diagnostics and are not byte-identical to a clean checkout of the client commit.

Cross-device progress, natural-EOF auto-next and visionOS/tvOS device acceptance
remain explicitly separate from compilation/installation. A clean commit's
independent build checks are recorded by the client closeout, not inferred from
the earlier dirty-source development builds.

Deferred work is not silently declared delivered: old-thumbnail poster bridging,
complex multi-episode files, and MetaTube/DMM provider integration remain outside
this baseline's completed P1/P2 scope.

No push, tag, Docker Hub publication or App Store submission is authorized by
this local baseline commit. A later public release must build from this complete
lineage and validate the resulting image before changing production.
