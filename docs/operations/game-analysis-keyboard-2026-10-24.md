# Safe game analysis and EPUB keyboard repair — 2026-10-24

Base: eab810ad14307a98095355a426e6c2ac0c75aebd, external reader-mainline-20261021 checkout. Existing hotfixes retained.

The old launch-profile helper called the server's startup migrations even for `-dry-run`. The replacement opens only an initialized database, never migrates, and uses SQLite read-only/query-only mode for previews. Audit writes update derived readiness and launch profiles, preserving platform, ROM-set label and emulator preference. The service requires capability protocol 1 before invoking a helper, preventing accidental use of the old executable.

Manual game metadata and its source marker now commit together. Scanner sidecars and online imports cannot overwrite a marked manual record, including deliberately empty values. Reindexing preserves marked titles, platform, region, ROM-set labels and emulator preferences while continuing to refresh file facts. There is no schema migration or historical metadata rewrite.

EPUB text lives in a separate document. Clicking it moved keyboard focus away from the outer-window shortcut listener. Each loaded EPUB document now binds the same reader shortcuts, removes the previous binding, and resolves current callbacks after page/chapter changes. Editable controls, modified shortcuts and composition input retain their native behavior.

Validation:

- Full Go suite, TypeScript typecheck and all 88 frontend tests passed.
- 18081 dedicated synthetic MAME sample: game 9, library 13. Normal reindex automatically ran the new MAME helper; manual catalog identity, metadata, intentionally blank summary, favorite/like and 37-second play record stayed unchanged. Existing records across 45 non-operational tables remained present; helper dry-run changed no table. Automatic source scan timestamps are allowed to refresh. Temporary automatic-analysis settings were removed with an exact-value guard.
- 18081 real EPUB: book 27296, dedicated profile 3. With focus inside the text, Right moved 3 → 4 / 59 and Left 4 → 3 / 59. Previous/next chapter boundaries worked; the font slider consumed its own arrow keys and was restored to size 18. Only the dedicated QA profile's reading position changed.
- Release gate: runtime-v3, main SHA-256 9e5d6f138c63eac09bbf22e6283acc98c9e7494ee6360a037c4c22598a08866f; helper dd87e6063ea709407e91818e3d1b1e50b85f761471900a1c3f147d2cd17c03a4. Same payload promoted after test acceptance; runtime-only rollback and full before/after data verification retained in each NAS release directory.

Screenshot investigation (read-only): production books 27297 and 27298 are existing Calibre files, newly indexed at 2026-10-23 23:12:27 UTC. Calibre lists Unknown and no cover for both. Book 27297 (`A Geek in China`) is 7,873,635 zero bytes; book 27298 (`Your Place in the Universe Understanding Our Big Messy Existence`) starts as EPUB ZIP but has no readable central directory/end record. Both fail ZIP parsing and their SHA-256 hashes still match ingestion. Neither source file nor book record was repaired, deleted or replaced. Re-scanning cannot recover missing source bytes.

Evidence: sibling outputs/current/qa/game-analysis-safe-20261024; NAS releases/game-analysis-safe-20261024-runtime-v3. Build: Go 1.24.7, CGO_ENABLED=0 GOOS=linux GOARCH=amd64; ./cmd/foliospace-reader and ./cmd/foliospace-rebuild-launch-profiles. Frontend: node --test src/*.test.mjs; tsc; vite build.
