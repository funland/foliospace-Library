# Collection filtering and mixed-library scan completion

External checkout continuation from `5f24358`. Changes are additive query/UI behavior and a completion-hook guard; no schema migration, metadata refresh, item reassignment or user-state rewrite is required.

## Behavior

The collection panel defaults to Ebooks and offers Comics and All types. Ebooks support library and EPUB/PDF filtering before server pagination. Collection totals, selected cover and displayed volume count match the requested format; opening a result retains its format filter. The detail view can clear or change it. Opening an unrelated favorite or comic collection starts with all formats. The Comics / All formats view preserves the existing directory hierarchy and its own pagination. Switching types clears incompatible library/format selections. Five existing UI languages are covered.

The collection endpoint accepts `primaryType`, `libraryId` and `format`; the existing volumes endpoint already accepts format. Content type remains the existing explicit library / mixed-collection classification, so selecting PDF does not reclassify comic PDFs as ordinary books. Existing sort semantics remain collection-based.

Mixed-library automatic game analysis now requires a game observed during that actual scan. The observation is an in-memory completion value, including unchanged games in full/targeted scans, and uses the existing synchronized job updates with concurrent workers. Recent scans count only processed candidates. A comic-only root or subdirectory no longer triggers the global helper because unrelated games exist elsewhere. Explicit game libraries and deliberate game analysis retain their existing behavior. This patch does not replace the old launch-profile helper itself.

## Verification

- Complete Go suite passes, including scanner, EPUB, HTTP/API and ownership-preservation regressions.
- New store tests cover cross-type PDF classification, format-specific covers/counts, library selection, pagination and existing sort options.
- New scan tests cover mixed libraries with games elsewhere, full/targeted/recent scans, repeat scans and 1/3 workers.
- 86 frontend tests and TypeScript checking pass; production Vite build succeeds (existing bundle-size advisory remains).
- 18081 receives the new runtime first. Three isolated real EPUB/PDF/ZIP copies exercise filtered APIs, authenticated browser type/format selection and detail navigation, and comic-only scans while automatic game analysis is enabled. Original test records are retained. The temporary saved game setting is removed only after comparing the exact value/timestamp, returning the test environment to its original setting state.
- Runtime deployment compares database snapshots, container ID/image/mounts/configuration fingerprints, anonymous auth and every served web file. Production promotion requires matching test acceptance and runtime hashes. Runtime recovery never restores the database.

Evidence, source archive, build binding, operational scripts and test logs are in `../outputs/current/qa/collection-filters-20261024/`. NAS evidence is under each application's `releases/collection-filters-20261024-runtime/`. Final production acceptance is recorded in the QA README and receipts.

## Production result

Both environments serve binary SHA-256 `8dc330dadfda2fb1e8b2e8f87dcf99737753646026d381fd87586379c4c112fd` and the same 14 Web files. Production BOOKS filtering returns 550 EPUB books across 430 collections and 163 PDF books across 78 collections, with no duplicate pages and matching detail counts/formats/covers. Production jobs 2955 (library 21) and 2956 (library 25) each rescan one existing file, complete without errors, and leave the game task/settings unchanged while automatic analysis remains enabled. All stable tables compare exactly across these rescans (only operational jobs and book/file/series update timestamps are outside equality). Deployment compares all original rows, with only pre-existing failed-hash retry timestamps permitted. Test fixture creation separately proves all 44 original stable tables retain their pre-existing rows. No database restore was used.
