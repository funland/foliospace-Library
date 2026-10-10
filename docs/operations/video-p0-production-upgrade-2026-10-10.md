# Video P0 production upgrade — 2026-10-10

The user authorized upgrading the existing production service to the reviewed video-library P0, including TMDB configuration. The service was upgraded after a copied-database rehearsal, with a stopped-state rollback database and previous container retained. Preview remains independent.

## Result

- 83 works, 96 video files, 51 cached posters. All 87 existing production video IDs and rows preserved; nine additional files mapped by host file identity and size, never by preview numeric IDs.
- Nine libraries, 26,789 books, 21,312 games and one profile retained.
- 32 legacy tables identical; file records identical except background hash-worker timestamps. SQLite integrity check passed. Seven pre-existing foreign-key issues were unchanged.
- Original production authentication retained. TMDB provider/token/language migrated privately; real TMDB configuration request returned 200. No preview authentication was copied into production. Preview and production tokens are not interchangeable.
- Works, detail, versions, old file lists, artwork and video Range passed. Book, PC-98 and NDS manifest/Range passed. No claim of a new Triforce device playtest.

## Startup preservation

The copied-database rehearsal exposed an unrelated catalog reconciliation that changed eight existing game records. `FOLIOSPACE_PRESERVE_GAME_CATALOG_ON_START=1` skips that reconciliation while retaining schema migration and explicit scanning/curation. The deployed environment enables this switch. Its regression test verifies repeated startup preserves curated fields and default behavior still runs when the switch is absent.

The deployed Linux binary SHA-256 is `9ec6e10d0e195ec50ce5a37e8192bba7671b3820faa1abca3bdafaf8bd2b10dc`, built with Go 1.24.7 using `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath`. The backend uses the 1.02 source baseline plus this P0 change. Runtime libraries and game resources remain from the prior production image; the new server and web assets have independent read-only mounts.

`go test ./...` and `go vet ./...` passed. The private deployment directory retains exact source, scripts, hashes and database rollback evidence; credentials are excluded from Git. When recreating the container, retain the server/web mounts, production authentication, TMDB environment and preservation switch. Do not overwrite post-upgrade writes with an old database backup without reconciling them.

P0 does not promise cross-device progress, complete season/episode enumeration or next-episode playback. Native Mac/iPad P0 was accepted by the user against preview; production API acceptance is separate from device playback.
