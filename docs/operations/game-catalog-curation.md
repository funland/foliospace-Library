# Game Catalog Curation

FolioSpace separates file discovery from runtime certification. Scanning proves that a file exists and records stable identity where possible. Catalog curation then decides whether it is a certified game, a dependency, or an item that still needs review. Native clients can discover `game` and `needs-curation` records, while dependency-only files remain hidden.

## 1. Automatic Post-Scan Analysis

Enable **Analyze after game scans** during first-run setup or under **Game Curation**. After a game or mixed library scan completes, FolioSpace starts one background catalog task. A second task is not created while the first one is running.

The base pass identifies ordinary launch entries and known dependency files. Strict arcade families remain `needs-curation` until an installed compatibility policy verifies the ROM set and creates at least one target-specific Launch Profile. These records remain discoverable but must not be presented as certified launchable.

Automatic analysis is saved only after setup explicitly writes game settings. Upgraded installations therefore do not unexpectedly start a full arcade audit on their first scan.

## 2. Curation Center

Open **Game Curation** in the web sidebar. The summary shows:

- total indexed records;
- ready games published to clients;
- records needing curation;
- dependencies hidden from clients;
- metadata and cover coverage;
- canonical launch-file checksum coverage and remaining files;
- compatibility policy availability;
- the latest background task and its progress.

The record list can filter by state, platform, or search text. Typical issue codes are:

- `identity-missing`: rescan the single file so a stable hash can be recorded;
- `policy-pack-missing`: install the matching FBNeo DAT or MAME listxml package;
- `launch-profile-missing`: the archive failed the installed audit, usually because the set version is wrong or a parent, BIOS, or device archive is absent;
- `dependency-missing`: the canonical manifest is empty or a required launch file is unavailable;
- `manifest-checksum-unavailable`: at least one canonical launch file has no valid SHA-1 or changed after its checksum was recorded;
- `dependency`: the file is intentionally retained for another game's manifest and is not a standalone game.

Use **Analyze Catalog** after adding policies or repairing ROM dependencies. The operation changes database classification and profiles only; it does not rename, move, rewrite, or delete source ROM files.

MAME audits classify BIOS and device sets from the installed listxml attributes. A non-runnable support set also needs an incoming `romof` or `device_ref` relationship. The indexed archive must pass the matching ROM audit before receiving the dependency role. A runnable parent remains independently discoverable; `cloneof` alone never makes a game a dependency. Failed archive matches remain available for curation. The rebuild report counts successfully classified dependencies separately from rejected games.

Use **Backfill checksums** after upgrading an existing library. Each run processes at most 100 pending canonical files in sequence and persists progress, so the NAS is not forced to hash the entire library in one task. Run it again until the pending count reaches zero. A blocked row also provides **Checksum** to repair only that game's canonical files. New scans write per-file SHA-1 values immediately; unchanged files reuse a checksum only when path, size, and modification time still match. A replaced or edited source invalidates that reuse and remains blocked from mobile resolution until it is scanned or checksummed again.

## 3. Compatibility Policies

The default container paths are:

```text
/config/policies/fbneo-arcade.dat
/config/policies/mame0288lx.zip
/config/policies/fbneo-mobile-targets.json
/config/policies/mame-mobile-targets.json
```

The FBNeo DAT and MAME listxml are not bundled with FolioSpace. Administrators supply policy files for ROM audits. The two target JSON files declare client/platform/runtime targets for each policy family and remain separate. For supported official clients, FBNeo build identifiers and hashes are diagnostic, not launch gates. MAME uses versioned content contracts; release 1.01 also bundles reviewed [content-equivalence evidence](mame-content-equivalence.md) for reusing existing audited profiles. Paths are configurable in setup and Game Curation for installations with a different `/config` layout. The older `/config/policies/targets.json` setting is retained only as a compatibility fallback.

When no FBNeo target JSON is installed, the rebuild tool uses the release defaults for SpatialEMU iOS, iPadOS, and visionOS plus the Windows target. An installed target JSON is authoritative and replaces that built-in list, so it must contain every client/platform/architecture the deployment intends to keep. A compatibility rebuild writes only the declared targets; using a temporary one-client document as the permanent policy can therefore leave other clients without a matching profile. A supported official client's changed `coreBuildId` or App hash does not require a new FBNeo profile, but a missing client/platform target or incorrect runtime route still needs correction.

If policy files are absent, ordinary console and disc games can still be indexed. Strict arcade records remain `needs-curation` and are included in native-client discovery and facets without being promoted to a ready launch profile.

## 4. Cover Matching

Use **Match Local Covers** first. FolioSpace checks, in order, controlled same-name sidecars and common artwork layouts such as:

```text
Game.iso
Game.jpg
covers/Game.png
boxarts/Game.webp
media/Game/boxFront.jpg
```

The source library may be read-only; selected artwork can point at a readable local sidecar without changing the ROM. If **Libretro cover matching** is enabled, the separate network-assisted action can fall back to the existing Libretro thumbnail matcher for still-unmatched games. The job is bounded and sequential to avoid a cover-wall task exhausting NAS memory.

## 5. Metadata And Manual Corrections

Local metadata is the default. It preserves scanner-derived title, platform, format, region, hashes, and existing sidecar metadata without contacting an external service.

Hasheous can be enabled explicitly as an optional hash provider. FolioSpace sends CRC/SHA identity, stores returned candidates as metadata sources, and fills only missing display title, year, and publisher fields when the result is unambiguous. Ambiguous results require an administrator to select a candidate. Hasheous downtime or no match never blocks scanning, covers, manifests, or client launch.

The metadata editor remains the final authority. Administrators can correct display title, description, genres, developers, publishers, release date, player count, rating, and external links. Manual corrections are stored separately from source files and continue to work with all online providers disabled.

## 6. Multi-disc presentation

The client game list can include `discGroupInfo` with an opaque `groupKey`, a shared `title`, and a one-based `discNumber`. This is a display hint derived from an explicit Disc, Disk, or CD number in a supported disc-media filename. It retains edition, region, revision, language, pilot and demo labels, and separates libraries, source folders, platforms and formats. File checksums do not determine these groups.

The web catalog groups distinct disc numbers already loaded into the current result, and labels that count as loaded discs. Expanding a group exposes each original game card and ID, so favorites, likes, collections and launch manifests keep their existing ownership. A lone disc or duplicate disc number stays separate. Existing PC-98 source aggregation and PC-FX virtual playlists are unchanged. No file or database row is merged or removed.

## API And Safety

All curation routes are protected by the normal FolioSpace API token. They intentionally expose policy status and administrative records, so they are not part of `/api/client`. Native clients continue using the stable client catalog and never receive NAS file paths.

Background tasks persist their last status. If the service restarts during a task, the previous task is reported as `interrupted`; it is not silently resumed or duplicated. Checksum backfill reads one file at a time, verifies its source identity before and after hashing, and never exposes the NAS path through client APIs. Run the action again after confirming the policy files and free storage.
