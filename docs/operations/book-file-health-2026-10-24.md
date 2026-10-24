# Book file health diagnostics — 2026-10-24

External checkout base: 1c3fb6d. Existing game-analysis safety, EPUB keyboard, Calibre and ownership hotfixes retained. No schema migration.

## Behavior

The scanner checks readable book containers before its unchanged-file and Calibre fast paths. Empty files, zero-filled files, invalid ZIP directories and unreadable EPUB package/spine structures receive distinct diagnostics. Access failures remain operational errors. Container checks do not decode every comic image or fully parse every PDF; PDF validation checks its header. Unsupported nonempty formats such as 7z do not receive a false healthy/recovered result.

Current diagnostic state is stored in `app_settings` under `book-file-health:<book ID>`. A book's identity, page list, private state and file bytes are not changed to attach a diagnostic. Calibre sidecars can still update imported metadata through the existing manual-preserving merge, even when source book bytes are damaged. The authenticated `POST /api/books/{id}/validate` route changes diagnostics only. EPUB reading and analysis also check file health.

Recent-book SQL filters active diagnostics before LIMIT for both the web shelf and client home endpoint. Collections and favorites retain the book and display a localized error badge. Clicking a marked book opens the errors view. Errors show the original path and explanation; validation after a repaired file restores home eligibility and keeps errors as recovered history. An unindexed empty file remains an error/path record; an existing empty book retains its ID.

## Validation

- Full Go suite passed, followed by final archive/scanner/service tests for the unsupported-format boundary. TypeScript passed; all 89 frontend tests passed.
- Old regressions that deliberately corrupted same-size/same-mtime files now assert a diagnostic instead of silent skip. Their Calibre identity, manual fields, page lists and reading-state preservation assertions remain.
- 18081 used copies of production EPUBs 27297/27298, a healthy real EPUB and one synthetic recoverable EPUB. Test IDs 600015–600018. Damaged copies retained their bytes and were excluded from both recent endpoints. Empty-file validation changed only diagnostic settings/errors. After restoring and rescanning the synthetic file, its ID, manual title/description, reading locator, favorites and notes remained; the historical empty-file error became recovered.
- Browser acceptance: home excludes the two damaged copies; collection cards retain both with red localized labels; clicking one opens errors with the full path and reason; recovered history remains visible. Screenshots: errors-18081.png and book-labels-18081.png.
- Release: `book-health-20261024-runtime-v2`, main SHA-256 `161c0fb74b32c96f8de5c124ae76ab16620f71a2b4194d952dde816c15640f8d`; previously verified helper remains `dd87e6063ea709407e91818e3d1b1e50b85f761471900a1c3f147d2cd17c03a4` from 1c3fb6d.

Production promotion requires the exact test payload acceptance. Deployment uses SQLite snapshots, 50-table comparison, runtime-only rollback and container/mount/authentication checks. Diagnostic application is restricted to existing EPUB/PDF/ZIP/CBZ error candidates, with snapshots proving no changes outside their health settings and errors, and before/after source hashes. No full-library scan, source repair, deletion or replacement is performed.

Evidence: sibling `outputs/current/qa/book-health-20261024`, and each NAS root's `releases/book-health-20261024-runtime-v2`. Keep the latest usable Linux/web build and rollback; clean only reviewed generated/intermediate files after replacement validation.

Production result: four existing supported-format candidates were checked. IDs 22787, 27297 and 27298 remain structurally invalid and are excluded from recent shelves; ID 24489 now has a readable ZIP container and retains its old error as recovered history. Four 7z error candidates were deliberately left unchanged. Source hashes were unchanged. Full-table comparison excluding only the candidate diagnostic settings/error rows verified that all other data, including book/file rows, pages, reading progress, favorites and manual metadata, remained byte-for-byte unchanged; historical error IDs/paths/first-seen values were also retained.
