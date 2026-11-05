# FolioSpace MCP 1.04

MCP 1.04 is an independent agent-side release with **71 tools**: the original
62 tools remain available, and nine tools add library lookup, stored diagnostics,
and a preview-and-confirm workflow for one video's metadata. The companion
official server release is **FolioSpace Library 1.11**.

## Server compatibility

Use official Library 1.11 for the nine additions. Official Library 1.10 does not
provide their new API routes or capability flags. Compatibility is checked on
each call through `GET /api/client/info`: the corresponding capability below
must be the boolean `true`. A version string alone does not establish support,
including for custom or backported server builds.

Tool discovery lists all 71 tools even when the connected server lacks a new
capability. In that case, the affected new call returns `isError: true` with
`code: "capability-unsupported"` before invoking its target endpoint. These nine
tools have no fallback to older endpoints. Authentication and connection errors
remain errors. Existing tools retain their existing capability and fallback
behavior.

## Nine new tools

| Tool | Required capability | Behavior |
| --- | --- | --- |
| `foliospace.list_books` | `catalogSearchV1` | List indexed books and comics with SQL pagination and exact format filtering. |
| `foliospace.browse_comics` | `catalogSearchV1` | Browse comic folders and collections with relative-path navigation and pagination. |
| `foliospace.search_library` | `catalogSearchV1` | Search book, game and video-file records with a continuation cursor. |
| `foliospace.diagnose_book` | `storedDiagnosticsV1` | Read stored book-health and page-index diagnostics. |
| `foliospace.diagnose_game` | `storedDiagnosticsV1` | Read stored file, checksum, catalog and launch-profile diagnostics. |
| `foliospace.search_video_metadata` | `videoMetadataWorkflowV1` | Create a candidate-search job and contact the configured TMDB provider. |
| `foliospace.get_video_metadata_job` | `videoMetadataWorkflowV1` | Read a stored job and its candidates without purging provider caches. |
| `foliospace.preview_video_metadata` | `videoMetadataWorkflowV1` | Preview one candidate's changes and issue an expiring confirmation receipt. |
| `foliospace.apply_video_metadata` | `videoMetadataWorkflowV1` | Apply the reviewed receipt to one file after explicit confirmation and revalidation. |

Lookup returns shared catalog metadata. Follow `nextCursor` until `hasMore` is
false for complete cross-media results; a single page is not the whole library.
Search ordering is by media type and ID, and concurrent metadata edits can change
matches. Stored diagnostics report the evidence already indexed; they do not
open or revalidate media, scan the library, or prove that a file is healthy.

See [Library workflows](https://github.com/funland/foliospace-Library/blob/mcp-v1.04/docs/mcp/library-workflows.md) for parameters, pagination, HTTP
routes, response boundaries and examples.

## Metadata changes and authorization

This release includes writing tools. Metadata search writes a job and sends the
user-requested query to the operator's configured TMDB provider. It never applies
a candidate automatically. All four metadata workflow tools require positive
`videoId` and `profileId`, a configured API/admin token, and an explicit valid
Bearer header, even when other server reads permit anonymous access.

Profiles are state namespaces, **not independent access-control identities or
ACLs**. Metadata changes affect the shared library. This workflow covers one
movie file or an explicitly selected series episode; it does not add book/game
metadata editing, bulk matching, artwork downloading, or provider configuration.

Show the complete preview and obtain the user's explicit confirmation before
calling apply with its receipt and `confirm: true`. Preview creates an in-memory
receipt without changing persisted media data. Receipts expire after **10
minutes**, disappear on server restart, and are bound to the target, profile,
candidate and current metadata. A matched confirmation attempt consumes the
receipt; obtain a fresh preview to retry. Stale or replayed receipts cannot apply
outdated selections.

Apply can replace a shared association. It preserves manual overrides, including
intentional empty values, and rejects unsupported changes to shared work or
sibling display metadata. Private reading/playback records are not transferred
or overwritten. Reassociation can change computed playback identity; review the
preview's `playbackIdentityMayChange` field before confirming.

## Packages and upgrading

The independent `mcp-v1.04` release packages target macOS Intel, macOS Apple
Silicon, Linux AMD64 and Linux ARM64. Archives and the installer are accompanied
by `checksums.txt` with SHA-256 hashes. Use the release assets and the
[installation instructions](https://github.com/funland/foliospace-Library/blob/mcp-v1.04/docs/mcp/usage.md).

The installer verifies the selected archive, checks MCP initialization before
atomic replacement, preserves existing URL/token configuration, and retains one
previous executable for rollback. Installing MCP does not upgrade the Library
server. Reconnect or restart the MCP host after installation so it loads the new
binary; existing running processes keep their old executable. Existing host tool
allowlists also remain in effect. MCP remains a stdio bridge, not a new network
service.

## Verification scope

Regression validation uses temporary SQLite databases, synthetic catalog records,
local HTTP servers and provider fixtures. Coverage includes pagination beyond
default batches, cursor continuity, strict profile/authentication checks,
read-only database behavior, confirmation, expiry/replay/staleness, preservation
of manual/shared metadata, and both MCP stdio formats through the actual
HTTP/service/store stack. Compatibility checks compare the original 62 tool
definitions with the 71-tool result. Installer tests cover upgrade checks and
preservation of an existing installation on failure.

These tests do not constitute live TMDB account validation, verification of every
file in a user's library, or acceptance of every installed MCP host. Stored
diagnostics remain distinct from a fresh filesystem check.
