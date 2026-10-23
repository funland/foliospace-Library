# Production homepage / EPUB continuation — 2026-10-16

## Active task (read this first after compaction)

User authorization, relayed with direct transcript by coordination on 2026-10-16 09:33:54 UTC: diagnose FolioSpace loading slowness, optimize where justified, update 175:18080. This is the active task, NOT the older Zelda/Ludoteca question.

Reuse this initially clean release worktree, codex/release-1.03 at 46770f51fe4d1cef1e69437faade8e9a70922964. Actual production 1.03 source: 35a48eb13457b900ea571f2117fede4bd480142f; differences at release HEAD are documentation/workflow only.

## Scope and boundaries

- Only validated EPUB iframe ResizeObserver repair, homepage SQL / poster cache optimizations, and necessary privacy-safe bounded request timings. No Ludoteca or MCP additions.
- Preserve original checkout and codex/ludoteca-provider dirty work. No commit, push, tag or public release authorized.
- Preserve 18080 data, token and mounts. Never read/reset credentials, change permissions, or force-stop active transcodes. Recheck tasks immediately before a normal authorized restart; defer if active.
- No authenticated production HTTP timings available without credentials. Never claim synthetic tests, public shell requests or SQL timings are authenticated end-to-end acceptance.
- Roll back runtime files on failed verification; never restore an older DB over newer user progress.

## Completed evidence / sources

- Production container foliospace-library, official funland/foliospace-library:1.03, image e1c52376df49963c3dcd0c744dfa1944a823d3971329252a9a105929ac31094e. Existing JS index-BRAh3YFk.js.
- Earlier ffmpeg load naturally ended; latest snapshot CPU about 0.2%, server only. This does NOT establish historical timeout root cause.
- Production has no per-request timing logs. Read-only SQL showed expensive collection aggregation; existing store uses one DB connection. Poster mount timestamp defeats image cache.
- Narrow patches already validated on isolated 18081: full Go tests, store/httpapi race and vet, frontend tests/build; 576 SQL differential cases; real authorized EPUB resize reproduced then fixed in headless Chrome. Synthetic before/after results and limitations: ../foliospace-mcp-upgrade/FolioSpaceReader/docs/operations/reader-home-dev-2026-10-16.md (absolute worktree sibling under ~/.codex/worktrees).
- Production receipt: /Users/deadseafu/Documents/FolioSpaceReader/docs/operations/production-1.03-2026-10-11.md. Do not read private inspect/token backup files.
- Private prior test evidence: /private/tmp/foliospace-reader-qa/. Prior local EPUB/DB/server fixtures were cleaned; do not assume they remain.

## Remaining

1. Verify rootfs/Compose labels, production runtime hashes, low-impact public HTTP/SQL/activity samples.
2. Port only allowlisted patches to release worktree; add bounded category-only timing tests. Audit against production source.
3. Run regression/build and narrow runtime image build. No credentials in context/logs.
4. Back up required data and original runtime; choose deployment preserving existing opaque credentials (prefer existing container runtime replacement + normal restart if no safe existing Compose workflow). Document image-vs-runtime identity explicitly.
5. Recheck tasks, perform normal update only when safe; verify public endpoints, hashes, available real request timing logs; rollback if abnormal.
6. Clean only this task's disposable build outputs. Final actual link: http://192.168.10.175:18080/; report timing x/n and evidence boundaries honestly.

## Latest state — read before continuing

**FINAL OVERRIDE:** first attempt below auto-rolled back due to unordered mount
array comparison. Verified all mount fields equal across five inspections;
normalized the comparison and retried successfully. Final start is
2026-10-16T10:00:26.327945118Z. Session 86045 exited 0; `deployed.json` exists,
public root/asset hashes match the candidate, 50 table names/schema and 14 key
row counts are unchanged. Full receipt:
[production-home-epub-2026-10-16.md](production-home-epub-2026-10-16.md).
No active deployment remains. Local synthetic test service/fixtures and borrowed
node_modules symlink are cleaned; NAS smoke container removed. Cleanup session
87062 completed successfully, removing own build staging/upload duplicates after
open-file/mount checks; observed NAS free-space delta 0 bytes. Cleanup and records
are complete. Final response should report completion and the authenticated-
production timing limitation. Do not redeploy. Older paragraphs below are the
superseded first-attempt history, not the current task status.

Production runtime switched at 2026-10-16T09:57:21.750315993Z (17:57:21 Shanghai).
Same container a106d8c..., same official 1.03 image configuration. Runtime files
are now the narrow hotfix, verified directly via docker exec sha256sum:

- binary: 3f7528e66f1ddc7f241baf98a6d7fd0e64d282ba5717707955393300b51d5669
- JS index-BL-qvmGu.js: 65323eba39ee091397dd39fc4a37348658db7c77e46378a0abb64d79304287b4
- CSS index-Vm2rEPS2.css unchanged: f10dc7a939dbb2b69d37421487a417640b79f338fde841ba47e2fcfd53893ed0
- Built hotfix image (not production container Image field): sha256:40a3f9e9ff96e83b8fdaf2f6ec5f6575d5fea49ab39eec7d74da272c2f232063

Active deployment command is exec session 64741. It is completing post-update
table-count verification; do NOT start another deploy. Receipt/scripts/backups:
/volume2/docker/foliospace-library/releases/home-epub-20261016/.
The script backs up the DB opaquely, replaces program/Web only in the stopped
original container, starts it and verifies hashes/auth/mounts/table counts. It
rolls back runtime (never DB) on failure. No token/env contents were inspected.

Before and immediately before stopping: only server process, zero established
8080 connections, zero queued/running scan/thumbnail/metadata jobs, zero video
progress updates in five minutes. No active transcode was stopped.

Full table counts took minutes of disk reads; own process observed 68 MB read
and wait_on_page_bit_common. An attempt to terminate only the still-preparing
diagnostic was safely rejected by phase assertions because the backup and switch
had already completed. No signal was sent. Do not claim the diagnostic was stopped.

Full Go tests, race/vet, 47 frontend tests, TS/Vite build pass. Same source branch
excludes new MCP/Ludoteca. Linux image starts in no-network/no-mount smoke
container foliospace-home-smoke (remove this task's container after acceptance).
15 final synthetic EPUB browser cases pass; existing real-book DEV proof remains
separate. Production-narrow API/browser samples preceded fixing the generated
EPUB mtime and are sanity-only. Final corrected fixture rerun saved as
production-final-api.json / production-final-browser.json under private tmp:
40 API requests, 9 browser scenarios, zero timeouts; three slow-network thumbnail
requests aborted on navigation. Do not equate different fixture response bytes
or timings with the earlier real-book before/after measurements.

Remaining: finish deployment command; public after samples and bounded production
timing logs; data verification; retain rollback/runtime/source evidence; clean
own smoke container, build staging and disposable local QA outputs; final receipt
and actual 18080 link. No commit/push/public release.
