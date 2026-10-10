# Video library phase 1 — local acceptance, 2026-10-10

Development and local acceptance complete; not a production rollout.
Base: public 1.02 `2333ee57c6291449fe3921453d07652c7913968e`.
Worktree: `/private/tmp/foliospace-video-library-phase1`, branch
`codex/video-library-phase1`. All phase source changes remain uncommitted.
User/protocol guide: [video-library-v1](../api/video-library-v1.md).

## Delivered

- Additive movie/series, season/episode, version/file links, local inputs, protected
  overrides, persistent metadata jobs and artwork. Original video IDs remain intact.
- Filename/directory and bounded NFO input; unchanged-file rescan refresh without
  hashing entire movies. Confirmed/unlinked associations and manual fields survive.
- Paged work/detail/files API, legacy original-file URLs and additional HEAD support.
- Optional TMDB adapter, explicit candidate confirmation (never automatic), separate
  image selection, bounded/recoverable jobs, retries and stale-result protection.
- Local/manual/provider art, static fallback, 150-day provider expiry/purge,
  preserving originals and manual data. Official attribution and unchanged logo.
- English/Chinese web poster wall, details, seasons/episodes/versions, manual
  fields/matching/unlink/artwork; unknown files remain available. Compact mobile
  controls and two-column posters; old browser is an explicit toggle.

No FolioLume/client changes, AI, progress sync, complex anime or deep ISO/BDMV.
No HLS expansion/removal/default change. New poster browsing never invokes ffmpeg;
explicitly opening the old browser/player retains existing behavior.

## Validation

Final source: Go 1.24.7 and bundled Node runtime; all commands passed:

```sh
go test ./...
go vet ./...
go test -race ./internal/store ./internal/service ./internal/httpapi ./internal/scanner -run 'TestVideo|TestProvider|TestTMDB'
go build -o tmp/foliospace-video-server ./cmd/foliospace-reader
cd web
npm test
npm run build
```

Frontend 38/38 tests, TypeScript/Vite passed. Focused backend coverage: old-schema
upgrade/repeat migration, WAL-safe VACUUM backup/restore, stable IDs, rescans,
empty/manual fields, malformed/symlink NFO, 125-work paging/cap, library isolation,
candidate confirmation, three-attempt retry/backoff budget, interrupted job
recovery, cancellation/stale results, worker shutdown, mocked provider
200/401/404/429/500, provider expiry/manual retention, art validation, auth,
legacy manifest, exact Range 206, HEAD length/no body and 416.

Browser tests with synthetic media only:

- Desktop English/Chinese wall/local art; work details and S1/E1 file.
- Manual title save appeared in work-file list; version confirmation showed
  `confirmed` and the new version label. Unknown file stayed available.
- Disabled provider explained setup and disabled online search.
- 390×844: two 175px poster columns, document width exactly 390; editor also
  had no horizontal overflow. No legacy tile grid rendered by default.
- No console errors observed. Viewport override reset after verification.

No real TMDB credentials/requests, real-movie decoding or native-client acceptance
are claimed. Screenshots are local only: `tmp/acceptance/desktop.png` and
`tmp/acceptance/mobile.png`. Preview: `http://127.0.0.1:18089`, Video Shelf.
It uses only `tmp/preview` synthetic data; provider disabled. Test-only access
token `video-preview-test` has no production authority.

Latest build SHA-256:

- Backend: `4adb09f15293049bfcc9951243643b5a7abddfbb1f1c0ac0ebc0563c28ffcb4a`.
- `index-ADpvy_r_.js`: `6fc2d588d998b04ea551f95fea87fa616da6e25d1423d5a31444060851a1a6ab`.
- Official TMDB logo: `9665a76b1ae401a510ec1e0ca40ddcb3b0cfe45f1d51b77a308fea0845885648`.

Source provenance is the base plus this isolated dirty diff, not a release SHA.

## Cleanup / production boundaries

Retained the latest usable server binary and web/dist, active synthetic preview
and screenshots. Earlier binary replaced in place after stopping its verified
loopback process; Vite removed superseded assets. After checking processes/open
files, removed reinstallable web/node_modules and temporary npm bootstrap:
166,752 KiB summed size. Measured free space grew from 182,933,448 to 183,102,096
KiB (+168,648 KiB; concurrent filesystem activity may affect this measurement).
No source, worktree, download, release archive or production asset was deleted.

Nothing staged. `tmp/`, screenshots, generated DB/binary/dist/dependencies must
not enter a source commit. Original checkout remains at a23439f on
`codex/3ds-platform-catalog`, with existing website/infra/docs/tmp dirty preserved.
No production connection, 175 migration, 18080 replacement, commit/push, tag,
image publication or version increment performed.

Pre-existing frontend dependency advisories and >500 kB bundle warning remain;
no unrelated dependency migration performed.

## Separate next-stage gates

- Source review/narrow commit, version/notes and CI/image verification before release.
- Explicit deployment authorization, production delta review, consistent config/art
  backup and isolated restore before touching 175.
- Operator-authorized TMDB setup/terms assessment and real credential acceptance
  before enabling online matching. Library/noncommercial and paid client usage
  must not automatically be treated as the same licensing scenario.
- FolioLume implementation and real-device validation before claiming integration.
