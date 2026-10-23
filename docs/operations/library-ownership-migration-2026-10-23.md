# Legacy ownership reconciliation (2026-10-23)

External checkout only; baseline 0ffbfcb. User authorized preview, 18081 sample verification, then bounded production batches preserving all item IDs, reading progress, favorites and manual metadata.

## Reviewed scope

Production read-only audit found 8611 conflicting file rows in 165 whole directory collections: library 1 to 25 (871), and 1 to 21 (7740). The collections contain 8613 files; files 26825/26826 already had correct ownership and remain unchanged. Every unit resolves to one target root. Two nonempty target title collisions preserve both collection IDs: only proven generated source labels receive the explicit suffix “迁入目录”. Source series 2998 becomes `gifcollection（迁入目录）`, and 21539 becomes `韩漫（迁入目录）`; target series 51012/21550 retain their original labels and state. Existing fileless book 3389 follows its original collection ID 2998. No file moves, deletes, content deduplication or book/series merging.

## Tool

`scripts/library_ownership_migration.py` is Python 3.8 stdlib only. `plan` opens SQLite read-only; `apply` requires exact reviewed unit IDs, takes a WAL-aware backup before first batch, and uses BEGIN IMMEDIATE with configuration/schema/identity and target collision checks. Every batch hashes fresh related progress/private/manual states before and after; only library/path ownership columns and the two reviewed generated-label resolutions may change. Personal writes since preview are retained. Backup is the first-batch point, not an invitation to restore over later user writes.

Post-migration scans reuse a matching same-library directory collection anchor, retaining collection IDs, curated labels, and manual shared fields when no authoritative metadata source is present. Existing legacy Unsorted canonicalization remains. Full, targeted and recent scans are covered with two worker counts and three book formats.

## Verification

- Python migration suite: 24 tests pass.
- Full Go suite passes on final source.
- 18081 nondefault profile 5, dedicated libraries 10/11: three real comic files (about 14 MB) retain original book IDs and cloned reading progress; two compact collision fixtures preserve both series IDs and manual shared/private fields.
- Sample migration changes exactly 6 ownership rows across 50 tables. API compares five IDs, series IDs, progress, favorites, rating, tags, summary, creator and description. Parent/child rescans jobs 10 and 11 complete without errors and preserve these fields.
- Test startup compatibility added two default-profile progress copies for newly imported test books from their copied legacy progress. Deployment guard rolled back runtime; snapshots proved only these derived sample rows were inserted, with every prior row unchanged. A second deployment used current data as baseline and passed. No database was restored.

## Production result

After the 18081 sample and rescan checks passed, production migrated an initial 47-file sample (series 45953/42746), verified the complete database, then continued in bounded batches. All 35 committed batches cover exactly 165 series and 8611 changed file ownerships. Plan ID: `c0fa821b17130bb229580012fc50656b7263ebcb37b4c0ccc1bf499f271810d5`.

Final consistent snapshots compare all 50 tables: schema, row counts, original IDs, all user/private/manual fields and every unrelated column are identical. Exactly 8776 rows change only the planned fields: 8611 files and 165 series. Production libraries 1/21/25 report zero remaining ownership conflicts through authenticated APIs. The test and production container IDs, images, ports, full mounts and configuration fingerprints remain unchanged; both serve binary SHA-256 `26e0191552c3fee413daf37c1a4483c9fbecc68caf6a8dd94ca19129a5a32452` and the same 13 verified Web files, with anonymous API access still returning 401. This task's acceptance uses database, API and automated scanner checks; it does not claim a new browser UI inspection.

Operational receipts live beside the checkout in `../outputs/current/qa/ownership-migration/` and on each NAS application's `releases/ownership-migration-20261023/`. Production keeps the reviewed plan, all batch receipts, first-batch backup, final snapshot, `final-database-proof.json` and `final-migration-receipt.json`. The matching runtime release is `ownership-migration-20261023-runtime-verified`.

## Recovery and generated artifacts

The saved old runtime predates collection-ID protection and must not resume scans after migration. Both verified-release `rollback.sh` scripts now perform a read-only ownership guard and refuse while migrated ownership remains; this refusal was tested without restarting either container. Old runtime files remain available for analysis. Recovery requires a compatible program or an explicitly reviewed ownership reversal that compares current ownership with receipts and preserves newer private/user data. Never restore the entire old database over subsequent user writes.

Keep only the verified current Linux build, existing latest macOS build, current Web files and required dependency runtimes locally. Retain database snapshots, source and receipt evidence on NAS; remove only separately inspected superseded generated runtime copies and temporary packaging. File counts, frozen cleanup manifests and measured filesystem deltas are recorded in the QA directory.
