#!/usr/bin/env python3
"""Reviewed, bounded SQLite ownership repair; Python 3.8+, standard library only.

plan is read-only. apply changes only the ownership columns explicitly recorded
in a plan. No filesystem content is inspected, moved, deleted or deduplicated.
JSON plans/receipts are operator artifacts and can contain private paths/state.
"""
import argparse
import base64
import collections
import datetime
import hashlib
import json
import os
import posixpath
import sqlite3
import sys
import tempfile
import urllib.parse

VERSION = 1
MAX_BATCH = 100
SKIPPED_DIRS = {x.lower() for x in (
    "#recycle", "@eaDir", ".calnotes", "__MACOSX", "_maintenance", "media",
    "covers", "cover", "thumbnails", ".thumbnails", "thumbs", ".thumbs",
    "出版物附属盘、非卖品", "游戏镜像")}
OWNERSHIP = {"series": {"library_id", "directory_path"},
             "files": {"library_id", "rel_path"}}


class Refused(Exception):
    pass


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def pack(value):
    if isinstance(value, bytes):
        return {"$sqlite_blob": base64.b64encode(value).decode("ascii")}
    if isinstance(value, dict):
        return {k: pack(v) for k, v in value.items()}
    if isinstance(value, (tuple, list)):
        return [pack(v) for v in value]
    return value


def encoded(value):
    return json.dumps(pack(value), sort_keys=True, ensure_ascii=True,
                      separators=(",", ":"), allow_nan=False).encode("ascii")


def digest(value):
    return hashlib.sha256(encoded(value)).hexdigest()


def sealed(plan):
    return digest({k: v for k, v in plan.items() if k != "plan_id"})


def q(name):
    return '"' + name.replace('"', '""') + '"'


def connect(path, readonly=True):
    path = os.path.realpath(path)
    uri = "file:" + urllib.parse.quote(path, safe="/") + ("?mode=ro" if readonly else "?mode=rw")
    db = sqlite3.connect(uri, uri=True, isolation_level=None, timeout=10)
    db.row_factory = sqlite3.Row
    # Some unrelated legacy game titles contain invalid UTF-8. Preserve them.
    db.text_factory = lambda b: b.decode("utf-8", "surrogateescape")
    db.execute("PRAGMA foreign_keys=ON")
    db.execute("PRAGMA busy_timeout=10000")
    if readonly:
        db.execute("PRAGMA query_only=ON")
    return db


def rows(db, sql, args=()):
    return [dict(row) for row in db.execute(sql, args)]


def schema(db):
    return rows(db, "SELECT type,name,tbl_name,sql FROM sqlite_master "
                "WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name")


def table_columns(db, table):
    return {r["name"] for r in rows(db, "PRAGMA table_info(" + q(table) + ")")}


def check_schema(db):
    required = {
        "libraries": {"id", "root_path", "asset_type", "exclude_patterns"},
        "series": {"id", "library_id", "title", "directory_path", "collection_type"},
        "books": {"id", "series_id", "title", "format"},
        "files": {"id", "book_id", "library_id", "abs_path", "rel_path"},
    }
    for table, cols in required.items():
        if not cols <= table_columns(db, table):
            raise Refused("unsupported schema: " + table)
    triggers = rows(db, "SELECT name FROM sqlite_master WHERE type='trigger' "
                    "AND tbl_name IN ('series','files')")
    if triggers:
        raise Refused("ownership table triggers require separate review")


def normalize(path):
    if not isinstance(path, str) or not path.startswith("/") or "\x00" in path:
        raise Refused("ownership paths must be absolute POSIX paths")
    return posixpath.normpath(path)


def within(root, path):
    return path == root or path.startswith(root.rstrip("/") + "/")


def config(db):
    libs = rows(db, "SELECT id,root_path,asset_type,exclude_patterns FROM libraries ORDER BY id")
    normalized = set()
    for lib in libs:
        root = normalize(lib["root_path"])
        if root in normalized:
            raise Refused("ambiguous normalized library roots")
        normalized.add(root)
        try:
            patterns = json.loads(lib["exclude_patterns"] or "[]")
        except (ValueError, TypeError):
            raise Refused("invalid library exclusion JSON")
        if patterns is not None and (not isinstance(patterns, list) or
                                    any(not isinstance(x, str) for x in patterns)):
            raise Refused("invalid library exclusion list")
    settings = []
    if table_columns(db, "app_settings"):
        settings = rows(db, "SELECT key,value FROM app_settings "
                        "WHERE key LIKE 'comic_title_rules:%' ORDER BY key")
    return {"libraries": libs, "comic_title_rules": settings}


def owner(libs, path):
    path = normalize(path)
    for lib in sorted(libs, key=lambda x: (-len(normalize(x["root_path"])), x["id"])):
        if within(normalize(lib["root_path"]), path):
            return lib
    return None


def excluded(lib, path):
    rel = posixpath.relpath(normalize(path), normalize(lib["root_path"]))
    dirs = rel.split("/")[:-1]
    patterns = json.loads(lib["exclude_patterns"] or "[]") or []
    for i, directory in enumerate(dirs):
        current = "/".join(dirs[:i + 1]).lower()
        if directory.lower() in SKIPPED_DIRS:
            return True
        for pattern in patterns:
            pattern = pattern.replace("\\", "/").strip("/").lower()
            if pattern and (("/" not in pattern and directory.lower() == pattern) or
                            current == pattern or current.startswith(pattern + "/")):
                return True
    return False


def select_ids(db, table, column, ids):
    out = []
    # Keep below the older SQLite 999 variable limit.
    ids = sorted(set(ids))
    for i in range(0, len(ids), 500):
        part = ids[i:i + 500]
        out.extend(rows(db, "SELECT * FROM " + q(table) + " WHERE " + q(column) +
                        " IN (" + ",".join("?" for _ in part) + ")", part))
    return sorted(out, key=encoded)


def unit_rows(db, series_id):
    ss = rows(db, "SELECT * FROM series WHERE id=?", (series_id,))
    bs = rows(db, "SELECT * FROM books WHERE series_id=? ORDER BY id", (series_id,))
    fs = select_ids(db, "files", "book_id", [b["id"] for b in bs])
    return {"series": ss, "books": bs, "files": fs}


def identity(snapshot):
    # Personal progress and metadata may change between preview and apply.
    # Grouping, membership, file/content identity and ownership must not drift.
    return {"series": [{k: r[k] for k in ("id", "library_id", "title", "directory_path", "collection_type")}
                       for r in snapshot["series"]],
            "books": [{k: r[k] for k in ("id", "series_id", "title", "format")}
                      for r in snapshot["books"]],
            "files": snapshot["files"]}


def related(db, snapshot):
    ids = {"series": [r["id"] for r in snapshot["series"]],
           "books": [r["id"] for r in snapshot["books"]],
           "files": [r["id"] for r in snapshot["files"]]}
    out = {}
    tables = [r["name"] for r in rows(db, "SELECT name FROM sqlite_master WHERE type='table' "
                                    "AND name NOT LIKE 'sqlite_%' ORDER BY name")]
    for table in tables:
        if table in ids:
            continue
        found = {}
        # Direct FK references plus legacy book_id/series_id/file_id columns.
        refs = {(fk["table"], fk["from"]) for fk in rows(db, "PRAGMA foreign_key_list(" + q(table) + ")")
                if fk["table"] in ids and fk["to"] == "id"}
        cols = table_columns(db, table)
        for target, column in (("books", "book_id"), ("series", "series_id"), ("files", "file_id")):
            if column in cols:
                refs.add((target, column))
        for target, column in refs:
            for row in select_ids(db, table, column, ids[target]):
                found[encoded(row)] = row
        if table == "manual_collection_items" and {"asset_id", "asset_type"} <= cols:
            for row in select_ids(db, table, "asset_id", ids["books"]):
                if row["asset_type"] in ("book", "comic"):
                    found[encoded(row)] = row
        if table == "app_settings" and {"key", "value"} <= cols:
            for book_id in ids["books"]:
                for row in rows(db, "SELECT * FROM app_settings WHERE key=?",
                                ("book-local-metadata:" + str(book_id),)):
                    found[encoded(row)] = row
        if refs or table == "manual_collection_items":
            out[table] = sorted(found.values(), key=encoded)
        elif table == "app_settings":
            out[table] = sorted(found.values(), key=encoded)
    if "manual_collection_items" in out and "manual_collections" in tables:
        out["manual_collections"] = select_ids(db, "manual_collections", "id",
                                             [r["collection_id"] for r in out["manual_collection_items"]])
    return out


def protected_collision_rows(db, unit):
    """Existing target collections are never merged or renamed by the tool."""
    result = {}
    for old in unit.get("target_collision_rows", []):
        snap = unit_rows(db, old["id"])
        if len(snap["series"]) != 1 or any(snap["series"][0][key] != old[key]
                                              for key in ("id", "library_id", "title", "directory_path", "collection_type")):
            raise Refused("retained target series identity drift: " + unit["unit_id"])
        result[str(old["id"])] = {"rows": snap, "related": related(db, snap)}
    return result


def fingerprints(snapshot):
    return {t: {"rows": len(rr), "sha256": digest(rr)} for t, rr in sorted(snapshot.items())}


def preserved(snapshot, changes=()):
    ignored = {t: set(cols) for t, cols in OWNERSHIP.items()}
    for change in changes:
        ignored.setdefault(change["table"], set()).update(change["after"])
    return {t: [{k: v for k, v in row.items() if k not in ignored.get(t, set())}
                for row in rr] for t, rr in snapshot.items()}


def settings_value(cfg, library_id):
    key = "comic_title_rules:" + str(library_id)
    for setting in cfg["comic_title_rules"]:
        if setting["key"] == key:
            try:
                return json.loads(setting["value"])
            except ValueError:
                return {"invalid": setting["value"]}
    return {"uses_application_defaults": True}


def build_unit(db, snap, cfg, resolve_auto_title_collisions=False):
    s = snap["series"][0]
    fs, bs = snap["files"], snap["books"]
    libs = {lib["id"]: lib for lib in cfg["libraries"]}
    destinations = []
    invalid = False
    for f in fs:
        try:
            destinations.append(owner(cfg["libraries"], f["abs_path"]))
        except Refused:
            destinations.append(None)
            invalid = True
    desired = {lib["id"] if lib else None for lib in destinations}
    mismatch = any(lib is None or f["library_id"] != lib["id"] or
                   s["library_id"] != lib["id"] or
                   f["rel_path"] != posixpath.relpath(normalize(f["abs_path"]), normalize(lib["root_path"]))
                   for f, lib in zip(fs, destinations))
    if not mismatch:
        return None
    unit = {"unit_id": "series:" + str(s["id"]), "series_id": s["id"],
            "source_library_id": s["library_id"], "target_library_id": None,
            "status": "blocked", "reasons": [], "book_ids": [b["id"] for b in bs],
            "file_ids": sorted(f["id"] for f in fs),
            "row_fingerprints": fingerprints(snap), "identity": identity(snap),
            "related_fingerprints": fingerprints(related(db, snap)),
            "changes": [], "title_preserved": True}
    reasons = unit["reasons"]
    if invalid:
        reasons.append("invalid_absolute_path")
    if None in desired:
        reasons.append("no_configured_owner")
    if len(desired) != 1:
        reasons.append("members_span_roots_no_split_or_clone")
    unit["retained_fileless_book_ids"] = sorted({b["id"] for b in bs} - {f["book_id"] for f in fs})
    if s["collection_type"] != "directory":
        reasons.append("non_directory_series_requires_review")
    if reasons:
        return unit
    target = destinations[0]
    unit["target_library_id"] = target["id"]
    if target["asset_type"] not in ("mixed", "book", "comic"):
        reasons.append("target_library_type_does_not_accept_books")
    if any(excluded(target, f["abs_path"]) for f in fs):
        reasons.append("target_path_excluded")
    old_lib = libs.get(s["library_id"])
    if old_lib is None:
        reasons.append("source_library_missing")
        return unit
    if (old_lib["asset_type"] in ("comic", "mixed") or target["asset_type"] in ("comic", "mixed")) and \
            settings_value(cfg, old_lib["id"]) != settings_value(cfg, target["id"]):
        reasons.append("comic_title_rules_differ_requires_review")
    collisions = rows(db, "SELECT * FROM series WHERE library_id=? AND title=? AND id<>?",
                      (target["id"], s["title"], s["id"]))
    unit["target_collision_rows"] = collisions
    if collisions:
        unit["target_collision_fingerprints"] = {
            str(row["id"]): {"rows": fingerprints(unit_rows(db, row["id"])),
                             "related": fingerprints(related(db, unit_rows(db, row["id"])))}
            for row in collisions}
    new_title = None
    if collisions:
        proposed = s["title"] + "（迁入目录）"
        proven_auto = (s["title"] == s["directory_path"] ==
                       posixpath.basename(normalize(target["root_path"])))
        if resolve_auto_title_collisions and proven_auto and not rows(
                db, "SELECT id FROM series WHERE library_id=? AND title=? AND id<>?",
                (target["id"], proposed, s["id"])):
            new_title = proposed
            unit["title_preserved"] = False
            unit["auto_title_collision_resolution"] = {"old_title": s["title"], "new_title": proposed,
                                                        "retained_target_series_ids": [r["id"] for r in collisions]}
        else:
            reasons.append("target_series_title_collision_no_merge")
    directory = s["directory_path"]
    if posixpath.isabs(directory) or ".." in directory.split("/"):
        reasons.append("series_directory_not_relative")
    else:
        absolute = posixpath.normpath(posixpath.join(normalize(old_lib["root_path"]), directory))
        if any(posixpath.dirname(normalize(f["abs_path"])) != absolute for f in fs):
            reasons.append("multi_directory_series_requires_review")
        if not within(normalize(target["root_path"]), absolute):
            reasons.append("series_directory_outside_target_root")
        else:
            new_directory = posixpath.relpath(absolute, normalize(target["root_path"]))
            unit["changes"].append({"table": "series", "id": s["id"],
                                    "before": {"library_id": s["library_id"], "directory_path": directory},
                                    "after": {"library_id": target["id"], "directory_path": new_directory}})
            if new_title is not None:
                unit["changes"][-1]["before"]["title"] = s["title"]
                unit["changes"][-1]["after"]["title"] = new_title
                unit["changes"][-1]["auto_title_collision_resolution"] = True
            unit["scanner_title_hint"] = (posixpath.basename(normalize(target["root_path"]))
                                           if new_directory == "." else new_directory)
            unit["scanner_title_differs"] = s["title"] != unit["scanner_title_hint"]
    for f in fs:
        unit["changes"].append({"table": "files", "id": f["id"],
                                "before": {"library_id": f["library_id"], "rel_path": f["rel_path"]},
                                "after": {"library_id": target["id"], "rel_path": posixpath.relpath(
                                    normalize(f["abs_path"]), normalize(target["root_path"]))}})
    if not reasons:
        unit["status"] = "ready"
    return unit


def summary(plan, example_limit=8):
    by_target = collections.Counter()
    reasons = collections.Counter()
    ready = 0
    for unit in plan["units"]:
        ready += unit["status"] == "ready"
        by_target[str(unit.get("target_library_id"))] += len(unit.get("file_ids", [])) or 1
        reasons.update(unit.get("reasons", []))
    return {"plan_id": plan["plan_id"], "ready_units": ready,
            "blocked_units": len(plan["units"]) - ready,
            "files_by_target_library_id": dict(sorted(by_target.items())),
            "blocked_reasons": dict(sorted(reasons.items())),
            "examples": [{k: u.get(k) for k in ("unit_id", "status", "source_library_id",
                         "target_library_id", "reasons")} for u in plan["units"][:example_limit]]}


def make_plan(db, source_ids=None, target_ids=None, resolve_auto_title_collisions=False):
    check_schema(db)
    cfg = config(db)
    units = []
    for s in rows(db, "SELECT id,library_id FROM series ORDER BY id"):
        if source_ids and s["library_id"] not in source_ids:
            continue
        snap = unit_rows(db, s["id"])
        unit = build_unit(db, snap, cfg, resolve_auto_title_collisions)
        if unit and (not target_ids or unit["target_library_id"] in target_ids or
                     unit["target_library_id"] is None):
            units.append(unit)
    # Report unsupported legacy records without loading unrelated titles/blobs.
    for table, path_col in (("games", "file_path"), ("videos", "file_path"), ("game_sources", "file_path")):
        if not {"id", "library_id", path_col} <= table_columns(db, table):
            continue
        for row in rows(db, "SELECT id,library_id," + q(path_col) + " FROM " + q(table) + " ORDER BY id"):
            if source_ids and row["library_id"] not in source_ids:
                continue
            try:
                destination = owner(cfg["libraries"], row[path_col])
            except Refused:
                destination = None
            target_id = destination["id"] if destination else None
            if row["library_id"] != target_id and (not target_ids or target_id in target_ids):
                units.append({"unit_id": table + ":" + str(row["id"]),
                              "source_library_id": row["library_id"], "target_library_id": target_id,
                              "status": "blocked", "reasons": ["game_video_identity_requires_separate_review"],
                              "row_fingerprints": {table: {"rows": 1, "sha256": digest(row)}}})
    incoming = collections.defaultdict(list)
    for unit in units:
        if unit["status"] == "ready":
            change = next(c for c in unit["changes"] if c["table"] == "series")
            title = change["after"].get("title", unit["identity"]["series"][0]["title"])
            incoming[(unit["target_library_id"], title)].append(unit)
    for group in incoming.values():
        if len(group) > 1:
            for unit in group:
                unit["status"] = "blocked"
                unit["reasons"].append("incoming_series_identity_collision_no_merge")
    plan = {"version": VERSION, "created_at": now(), "schema_sha256": digest(schema(db)),
            "configuration": cfg, "filters": {"source_library_ids": source_ids or [],
                                                "target_library_ids": target_ids or []},
            "policy": {"preserve_series_titles": True, "whole_series_only": True,
                       "no_file_moves": True, "no_split_merge_clone": True,
                       "resolve_auto_title_collisions": resolve_auto_title_collisions}, "units": units}
    plan["plan_id"] = sealed(plan)
    return plan


def write_json(path, value):
    directory = os.path.dirname(os.path.abspath(path))
    os.makedirs(directory, mode=0o700, exist_ok=True)
    fd, temp = tempfile.mkstemp(prefix=".ownership-", suffix=".tmp", dir=directory)
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(encoded(value) + b"\n")
            f.flush()
            os.fsync(f.fileno())
        # Never replace an existing reviewed plan or receipt.
        os.link(temp, path)
        os.unlink(temp)
    finally:
        if os.path.exists(temp):
            os.unlink(temp)


def read_json(path):
    with open(path, "r", encoding="ascii") as f:
        return json.load(f)


def transformed_identity(unit):
    result = json.loads(encoded(unit["identity"]).decode("ascii"))
    for change in unit["changes"]:
        for row in result[change["table"]]:
            if row["id"] == change["id"]:
                row.update(change["after"])
    return result


def backup_once(db, db_path, receipt_dir, plan):
    backup = os.path.join(receipt_dir, "before-" + plan["plan_id"] + ".sqlite")
    metadata = backup + ".json"
    if os.path.exists(backup) or os.path.exists(metadata):
        if not os.path.isfile(backup) or not os.path.isfile(metadata):
            raise Refused("incomplete before backup; operator review required")
        note = read_json(metadata)
        if note.get("plan_id") != plan["plan_id"] or note.get("database") != os.path.realpath(db_path):
            raise Refused("before backup identity mismatch")
        if os.stat(backup).st_size != note.get("backup_size"):
            raise Refused("before backup size changed")
        return backup
    fd, temp = tempfile.mkstemp(prefix=".before-", suffix=".sqlite", dir=receipt_dir)
    os.close(fd)
    try:
        destination = sqlite3.connect(temp)
        try:
            db.backup(destination)
            if destination.execute("PRAGMA quick_check").fetchone()[0] != "ok":
                raise Refused("before backup quick_check failed")
        finally:
            destination.close()
        os.link(temp, backup)
        write_json(metadata, {"plan_id": plan["plan_id"], "database": os.path.realpath(db_path),
                              "created_at": now(), "backup_size": os.stat(backup).st_size})
    finally:
        os.unlink(temp)
    return backup


def cas_update(db, change):
    table = change["table"]
    permitted = set(OWNERSHIP.get(table, set()))
    if table == "series" and change.get("auto_title_collision_resolution") is True:
        permitted.add("title")
    if table not in OWNERSHIP or set(change["before"]) != permitted or \
            set(change["after"]) != permitted:
        raise Refused("plan contains non-ownership mutation")
    cols = sorted(change["after"])
    before = sorted(change["before"])
    sql = "UPDATE " + q(table) + " SET " + ",".join(q(c) + "=?" for c in cols)
    sql += " WHERE id=? AND " + " AND ".join(q(c) + " IS ?" for c in before)
    args = [change["after"][c] for c in cols] + [change["id"]] + [change["before"][c] for c in before]
    if db.execute(sql, args).rowcount != 1:
        raise Refused("ownership compare-and-swap failed")


def apply_plan(db_path, plan, unit_ids, receipt_dir, max_units=10):
    if plan.get("version") != VERSION or plan.get("plan_id") != sealed(plan):
        raise Refused("plan version or digest mismatch")
    if not unit_ids or len(unit_ids) != len(set(unit_ids)) or len(unit_ids) > max_units or \
            not 1 <= max_units <= MAX_BATCH:
        raise Refused("select a nonempty unique bounded batch (maximum 100 units)")
    index = {u["unit_id"]: u for u in plan["units"]}
    selected = []
    for key in unit_ids:
        if key not in index or index[key]["status"] != "ready":
            raise Refused("selected unit is missing or blocked: " + key)
        selected.append(index[key])
    os.makedirs(receipt_dir, mode=0o700, exist_ok=True)
    db = connect(db_path, readonly=False)
    pending = None
    receipt = {"version": VERSION, "plan_id": plan["plan_id"], "started_at": now(),
               "database": os.path.realpath(db_path), "unit_ids": unit_ids, "units": []}
    try:
        # SQLite backup API includes committed WAL content. It runs before the
        # write transaction; never call backup on the same held write handle.
        receipt["before_backup"] = backup_once(db, db_path, receipt_dir, plan)
        db.execute("BEGIN IMMEDIATE")
        check_schema(db)
        if digest(schema(db)) != plan["schema_sha256"]:
            raise Refused("database schema drift")
        if config(db) != plan["configuration"]:
            raise Refused("library root, exclusion, type or title-rule configuration drift")
        work = []
        for unit in selected:
            snap = unit_rows(db, unit["series_id"])
            current = identity(snap)
            # Pack for comparison so SQLite BLOBs and invalid UTF-8 survive.
            original = encoded(current) == encoded(unit["identity"])
            already = encoded(current) == encoded(transformed_identity(unit))
            if not original and not already:
                raise Refused("selected identity/membership/content drift: " + unit["unit_id"])
            if not already:
                fresh = build_unit(db, snap, plan["configuration"],
                                   plan["policy"].get("resolve_auto_title_collisions", False))
                if not fresh or fresh["status"] != "ready" or fresh["changes"] != unit["changes"]:
                    raise Refused("selected ownership or target collision drift: " + unit["unit_id"])
            state = related(db, snap)
            protected = protected_collision_rows(db, unit)
            work.append((unit, snap, state, protected, already))
        for unit, before, state, protected, already in work:
            if not already:
                for change in unit["changes"]:
                    cas_update(db, change)
            after = unit_rows(db, unit["series_id"])
            state_after = related(db, after)
            protected_after = protected_collision_rows(db, unit)
            if encoded(preserved(before, unit["changes"])) != encoded(preserved(after, unit["changes"])) or encoded(state) != encoded(state_after):
                raise Refused("non-ownership values changed; rolling back batch")
            if encoded(protected) != encoded(protected_after):
                raise Refused("retained target collection changed; rolling back batch")
            if encoded(identity(after)) != encoded(transformed_identity(unit)):
                raise Refused("ownership verification failed; rolling back batch")
            receipt["units"].append({"unit_id": unit["unit_id"], "status": "already_applied" if already else "applied",
                                      "before": fingerprints(before), "after": fingerprints(after),
                                      "preserved_sha256": digest(preserved(after, unit["changes"])),
                                      "changes": unit["changes"],
                                      "retained_target_before_sha256": digest(protected),
                                      "retained_target_after_sha256": digest(protected_after),
                                      "related_before": fingerprints(state), "related_after": fingerprints(state_after)})
        receipt["validated_at"] = now()
        receipt["status"] = "validated_pending_commit"
        batch_key = digest({"units": unit_ids, "started_at": receipt["started_at"]})[:20]
        pending = os.path.join(receipt_dir, "batch-" + batch_key + ".pending.json")
        write_json(pending, receipt)
        db.commit()
        receipt["status"] = "committed"
        receipt["committed_at"] = now()
        final = os.path.join(receipt_dir, "batch-" + batch_key + ".json")
        write_json(final, receipt)
        os.unlink(pending)
        return receipt
    except Exception:
        if db.in_transaction:
            db.rollback()
        # Pending receipts are retained if final durable receipt fails after
        # commit; exact reapplication safely establishes current state.
        raise
    finally:
        db.close()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    pp = sub.add_parser("plan", help="read-only snapshot and private operator JSON")
    pp.add_argument("--database", required=True)
    pp.add_argument("--output", required=True)
    pp.add_argument("--source-library", "--library-id", dest="source_library", type=int, action="append")
    pp.add_argument("--target-library", type=int, action="append")
    pp.add_argument("--resolve-auto-title-collisions", action="store_true",
                    help="explicitly propose suffix only when old title==old directory==target root basename; retains both IDs")
    ap = sub.add_parser("apply", help="apply explicitly selected ready units atomically")
    ap.add_argument("--database", required=True)
    ap.add_argument("--plan", required=True)
    ap.add_argument("--unit", action="append", required=True, help="repeat exact unit IDs from reviewed JSON")
    ap.add_argument("--receipt-dir", required=True)
    ap.add_argument("--max-units", type=int, default=10)
    args = parser.parse_args(argv)
    try:
        if args.command == "plan":
            db = connect(args.database)
            try:
                db.execute("BEGIN")
                plan = make_plan(db, args.source_library, args.target_library, args.resolve_auto_title_collisions)
                db.rollback()
            finally:
                db.close()
            write_json(args.output, plan)
            print(json.dumps(summary(plan), sort_keys=True, ensure_ascii=True))
        else:
            receipt = apply_plan(args.database, read_json(args.plan), args.unit, args.receipt_dir, args.max_units)
            print(json.dumps({"plan_id": receipt["plan_id"], "status": receipt["status"],
                              "units": [{"unit_id": u["unit_id"], "status": u["status"]} for u in receipt["units"]]},
                             sort_keys=True))
    except (Refused, sqlite3.Error, OSError, ValueError) as exc:
        # Refusal messages contain IDs/reasons only; SQL operational errors may
        # include table names but never private paths or decoded catalog rows.
        message = str(exc) if isinstance(exc, (Refused, sqlite3.Error)) else type(exc).__name__
        print("ownership migration refused: " + message, file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
