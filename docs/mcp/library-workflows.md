# Library lookup, stored diagnostics, and single-file metadata workflows

MCP 1.04 adds these nine tools, bringing the total to 71 while preserving the
previous 62 tools and their response envelopes. The matching official server
release is Library 1.11. Official Library 1.10 does not include these new routes
or capabilities; MCP 1.03.1 does not include these nine tools.

Compatibility is determined by the boolean capabilities returned by
`GET /api/client/info`, not by comparing version strings. A server must advertise
the corresponding capability below. New tools return `capability-unsupported`
when it is absent or false, without a potentially mutating fallback. Successful
MCP initialization or discovery of all 71 tools does not establish server
support. Upgrade both the MCP binary and Library server to use the new workflows.

## Lookup

Capability: `catalogSearchV1`. All three tools are read-only, non-destructive,
idempotent, and closed-world. They return shared catalog metadata only, excluding
private notes, favorites, tags merged from profiles, and reading/playback state.
Optional `profileId` selects a validated context; omission resolves the default.
Malformed or nonexistent explicit profiles fail rather than selecting a different
profile. Profiles are state namespaces, not separate authentication identities or
independent access-control boundaries.

| Tool | Arguments | HTTP route |
| --- | --- | --- |
| `foliospace.list_books` | `q`, `format`, `sort` (`title` or `recent`), `limit`, `offset`, `profileId` | `GET /api/client/catalog/books` |
| `foliospace.browse_comics` | `q`, `libraryId`, relative `path`, `limit`, `offset`, `profileId` | `GET /api/client/catalog/comics` |
| `foliospace.search_library` | `q`, `format`, `limit`, `cursor`, `profileId` | `GET /api/client/catalog/search` |

`limit` defaults to 20 and must be 1–100. Offset must be 0–10,000,000. Format is
an exact, case-insensitive extension; `.EPUB` and `epub` are equivalent. Empty or
`all` selects all formats. Filtering happens before counts and pagination.

Book listing includes comic books. It sorts by `(title, id)` ascending or
`(created_at, id)` descending, and returns `items`, `total`, `limit`, `offset`
(omitted at zero), and `hasMore`. Offset ordering is deterministic while the
catalog is unchanged; restart after changes. Comic browsing returns folders and
collections with validated relative navigation paths. A query searches collection
titles, display titles, and relative paths; folder totals are not book totals.

Cross-media search operates on SQL catalog rows, not the first page from each
media API. Its order is **book, game, video**, then primary key ascending. It is
not a relevance ranking. A comic appears once in the book namespace. Games include
all indexed catalog roles, including dependencies; inspect `catalogRole` before
offering a game for launch. Videos are physical file/version records, not unique
movies or series. `(assetType, id)` is the unique identity: equal IDs across media
are different items. Book query coverage is title, creator, shared tags and
collection title; game coverage is title, platform and ROM set; video coverage is
resolved display title and original indexed title. SQLite's built-in `LOWER`
defines case matching; no language-specific stemming or fuzzy ranking is added.

Follow `nextCursor` until `hasMore` is false, resending identical `q`, `format`,
and profile context. Page size may change. The cursor uses keyset pagination and
captures each table's maximum ID at the first request. Later inserts are excluded,
and deleted rows are skipped without shifting subsequent positions. This is not
a metadata snapshot: edits can change query membership or result fields. Restart
without a cursor to refresh after edits. `total` is the current matching count
within those captured ID bounds, not an immutable initial count. Cursor content is
opaque pagination state, not authorization; API authentication and profile checks
run on every request. Format filters apply across all three media types.

Example continuation:

```json
{"name":"foliospace.search_library","arguments":{"q":"Dune","limit":20,"profileId":1}}
```

Use the returned cursor in the next call with the same query/profile. Do not
represent a single page as the full matching library.

## Stored diagnostics

Capability: `storedDiagnosticsV1`.

| Tool | Arguments | HTTP route |
| --- | --- | --- |
| `foliospace.diagnose_book` | required `bookId`; optional `profileId` | `GET /api/client/diagnostics/books/{id}` |
| `foliospace.diagnose_game` | required `gameId`; optional `profileId` | `GET /api/client/diagnostics/games/{id}` |

These tools use stored records only, with `source: "stored"` and
`filesystemVerified: false`. They do not open media, validate archives, scan,
generate checksums or thumbnails, rebuild launch profiles, or contact providers.
Absent evidence remains `unknown`; absence of a recorded error does not prove a
file is healthy. A ready game launch profile is a stored audit result, not proof
of current device/runtime compatibility. Responses contain safe status/issue
codes and counts, never raw filesystem paths or diagnostic messages. All four
annotations are read-only/non-destructive/idempotent/closed-world. No repair or
recheck tool is added in this wave.

## Single-video metadata

Capability: `videoMetadataWorkflowV1`. First-wave coverage is TMDB candidates for
one movie file or one explicitly selected series episode. No book/game metadata,
bulk matching, artwork download, or provider configuration is exposed.

All workflow tools require positive `videoId` and `profileId`. The HTTP workflow
requires a configured API/admin token and an explicit valid Bearer header even
when other Library reads are allowed without authentication. Metadata has
`scope: "shared-library"`; profile selection binds the receipt context and does
not make metadata private to that profile. Search sends the explicitly supplied
query to the already-configured TMDB provider. Do not search externally unless
the user has requested it. No media bytes or whole-library inventory are sent.

| Tool | Additional arguments | Behavior |
| --- | --- | --- |
| `foliospace.search_video_metadata` | required `query`, `kind` (`movie` or `series`) | Queue candidate search; never apply a match automatically. |
| `foliospace.get_video_metadata_job` | required `jobId` | Read stored job/candidates without cache purging. |
| `foliospace.preview_video_metadata` | required `jobId`, zero-based `candidateIndex`; optional `version`; series require `season` and `episode` | Return current/proposed values, field changes, locked overrides and a receipt. |
| `foliospace.apply_video_metadata` | required `receipt`, `confirm: true` | Apply the exact reviewed selection to one file after revalidation. |

The HTTP prefix is `/api/client/metadata/videos/{videoId}/`: POST `search`, POST
`preview`, POST `apply`, or GET `jobs/{jobId}?profileId=...`. POST profile context
belongs in the JSON body. Unknown fields are rejected.

Show the complete preview and obtain explicit user confirmation before calling
apply. Never infer confirmation merely from the existence of a candidate or from
the tool schema's `confirm` field. The server enforces the explicit confirmation
flag and the receipt, but cannot independently establish a human's intent.

Receipts expire after ten minutes, are bound to the video/profile/candidate and
current metadata, are held in memory, and do not survive server restart. A matched
confirmation attempt consumes the receipt even if stale or unsuccessful; obtain
a fresh preview to retry. Candidate generation, status, expiration and metadata
fingerprints are checked again within the applying transaction. Changed/replayed
receipts fail without applying stale data. Search with a conflicting active query
fails instead of silently substituting another search.

Manual overrides, including intentional empty values, are preserved. Existing
shared work metadata and sibling displayed metadata must remain unchanged;
unsupported shared-display changes are rejected for separate review. Files with
shared episodes also reject any change to the episode's representative file,
even when both files currently have the same displayed title. Expired TMDB work
records, including legacy records with a zero expiry, cannot be reused. Files with
multiple work associations are outside this first-wave workflow. Private
playback/reader records are not transferred or overwritten. Reassociation can
change the computed playback identity; review `playbackIdentityMayChange`.

Search is a write and open-world operation because it creates a provider job.
Job reading is read-only/idempotent/closed-world. Preview is read-only with respect
to persisted media data, but creates an ephemeral receipt and is not idempotent.
Apply is a non-idempotent, destructive write (it can replace an association),
closed-world and limited to the confirmed target. Existing server background
provider-cache maintenance remains independent of these new tools.

## Verification scope

Regression fixtures use temporary SQLite databases, synthetic catalog records,
and local HTTP servers. They cover more than 200 records per media stream,
filtering before pagination, cursor continuity, strict profile/auth behavior,
read-only database checks, confirmation/replay/staleness, shared/manual metadata,
and both MCP stdio formats through the real HTTP/service/store stack. Provider
network behavior uses fixtures; this is not validation against a user's live
library, TMDB account, or installed MCP client.
