#!/usr/bin/env python3
"""Run: PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts -p 'test_library_ownership_migration.py'."""
import contextlib
import io
import json
import os
import sqlite3
import tempfile
import unittest
from unittest import mock

import library_ownership_migration as m


SCHEMA = """
PRAGMA foreign_keys=ON;
CREATE TABLE libraries(id INTEGER PRIMARY KEY,root_path TEXT UNIQUE,asset_type TEXT,exclude_patterns TEXT DEFAULT '[]',name TEXT);
CREATE TABLE series(id INTEGER PRIMARY KEY,library_id INTEGER REFERENCES libraries(id),title TEXT,directory_path TEXT,collection_type TEXT DEFAULT 'directory',created_at TEXT DEFAULT 'original',updated_at TEXT DEFAULT 'original',UNIQUE(library_id,title));
CREATE TABLE books(id INTEGER PRIMARY KEY,series_id INTEGER REFERENCES series(id),title TEXT,format TEXT,creator TEXT DEFAULT 'Manual author',summary TEXT DEFAULT 'Personal note',favorite INTEGER DEFAULT 1,updated_at TEXT DEFAULT 'original');
CREATE TABLE files(id INTEGER PRIMARY KEY,book_id INTEGER REFERENCES books(id),library_id INTEGER REFERENCES libraries(id),abs_path TEXT UNIQUE,rel_path TEXT,size INTEGER DEFAULT 10,mtime TEXT DEFAULT 'stable',hash TEXT DEFAULT 'hash',updated_at TEXT DEFAULT 'original');
CREATE TABLE read_progress(book_id INTEGER PRIMARY KEY REFERENCES books(id),page_index INTEGER,locator TEXT,updated_at TEXT DEFAULT 'original');
CREATE TABLE profile_read_progress(profile_id INTEGER,book_id INTEGER REFERENCES books(id),page_index INTEGER,locator TEXT,updated_at TEXT DEFAULT 'original',PRIMARY KEY(profile_id,book_id));
CREATE TABLE profile_read_positions(profile_id INTEGER,book_id INTEGER REFERENCES books(id),reader_mode TEXT,payload_json TEXT,PRIMARY KEY(profile_id,book_id,reader_mode));
CREATE TABLE book_private_states(profile_id INTEGER,book_id INTEGER REFERENCES books(id),favorite INTEGER,rating INTEGER,tags TEXT,summary TEXT,PRIMARY KEY(profile_id,book_id));
CREATE TABLE collection_private_states(profile_id INTEGER,series_id INTEGER REFERENCES series(id),favorite INTEGER,liked INTEGER,PRIMARY KEY(profile_id,series_id));
CREATE TABLE pages(id INTEGER PRIMARY KEY,book_id INTEGER REFERENCES books(id),path TEXT);
CREATE TABLE manual_collections(id INTEGER PRIMARY KEY,name TEXT,description TEXT);
CREATE TABLE manual_collection_items(collection_id INTEGER REFERENCES manual_collections(id),asset_type TEXT,asset_id INTEGER,PRIMARY KEY(collection_id,asset_type,asset_id));
CREATE TABLE app_settings(key TEXT PRIMARY KEY,value TEXT,updated_at TEXT DEFAULT 'original');
CREATE TABLE games(id INTEGER PRIMARY KEY,library_id INTEGER,file_path TEXT,title TEXT);
CREATE TABLE game_sources(id INTEGER PRIMARY KEY,library_id INTEGER,file_path TEXT,title TEXT);
CREATE TABLE videos(id INTEGER PRIMARY KEY,library_id INTEGER,file_path TEXT,title TEXT);
INSERT INTO libraries VALUES(1,'/library','mixed','[]','Parent');
INSERT INTO libraries VALUES(2,'/library/Child','comic','[]','Child');
INSERT INTO libraries VALUES(3,'/library/Other','book','[]','Other');
"""


class MigrationTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = os.path.join(self.tmp.name, "catalog.sqlite")
        self.receipts = os.path.join(self.tmp.name, "receipts")
        self.db = sqlite3.connect(self.path)
        self.addCleanup(self.db.close)
        self.db.executescript(SCHEMA)

    def seed(self, series=10, title="Child/A", directory="Child/A", book=20, file=30,
             path="/library/Child/A/one.epub", library=1, fmt="epub"):
        self.db.execute("INSERT OR IGNORE INTO series(id,library_id,title,directory_path) VALUES(?,?,?,?)",
                        (series, library, title, directory))
        self.db.execute("INSERT OR IGNORE INTO books(id,series_id,title,format) VALUES(?,?,?,?)",
                        (book, series, "Curated title " + str(book), fmt))
        self.db.execute("INSERT INTO files(id,book_id,library_id,abs_path,rel_path) VALUES(?,?,?,?,?)",
                        (file, book, library, path, path[len('/library/'):]))
        self.db.commit()

    def plan(self, **kwargs):
        self.db.commit()
        db = m.connect(self.path)
        try:
            db.execute("BEGIN")
            return m.make_plan(db, **kwargs)
        finally:
            db.close()

    def apply(self, plan, units=None, **kwargs):
        return m.apply_plan(self.path, plan, units or [u["unit_id"] for u in plan["units"] if u["status"] == "ready"],
                            self.receipts, **kwargs)

    def value(self, sql, args=()):
        return self.db.execute(sql, args).fetchone()[0]

    def seed_state(self, book=20, series=10):
        self.db.execute("INSERT INTO read_progress VALUES(?,12,'epubcfi(/6/20)','original')", (book,))
        self.db.execute("INSERT INTO profile_read_progress VALUES(7,?,30,'profile-seven','original')", (book,))
        self.db.execute("INSERT INTO profile_read_positions VALUES(7,?,'epub','{\"chapter\":42}')", (book,))
        self.db.execute("INSERT INTO book_private_states VALUES(7,?,1,5,'private-tag','Private summary')", (book,))
        self.db.execute("INSERT INTO collection_private_states VALUES(7,?,1,1)", (series,))
        self.db.execute("INSERT INTO pages VALUES(900,?,'cached-page')", (book,))
        self.db.execute("INSERT INTO manual_collections VALUES(400,'Favourites','Custom description')")
        self.db.execute("INSERT INTO manual_collection_items VALUES(400,'book',?)", (book,))
        self.db.execute("INSERT INTO app_settings(key,value) VALUES(?,?)",
                        ("book-local-metadata:" + str(book), '{"title":"Original imported title"}'))
        self.db.commit()

    def test_plan_readonly_apply_preserves_all_metadata_and_private_state(self):
        self.seed()
        self.seed_state()
        with open(self.path, 'rb') as f:
            before_bytes = f.read()
        plan = self.plan()
        with open(self.path, 'rb') as f:
            self.assertEqual(before_bytes, f.read())
        self.assertEqual("ready", plan["units"][0]["status"])
        self.assertEqual({"id": 10, "library_id": 1, "title": "Child/A", "directory_path": "Child/A", "collection_type": "directory"},
                         plan["units"][0]["identity"]["series"][0])
        receipt = self.apply(plan)
        self.assertEqual(2, self.value("SELECT library_id FROM series WHERE id=10"))
        self.assertEqual("A", self.value("SELECT directory_path FROM series WHERE id=10"))
        self.assertEqual("Child/A", self.value("SELECT title FROM series WHERE id=10"))
        self.assertEqual("A/one.epub", self.value("SELECT rel_path FROM files WHERE id=30"))
        self.assertEqual("original", self.value("SELECT updated_at FROM series WHERE id=10"))
        self.assertEqual("original", self.value("SELECT updated_at FROM files WHERE id=30"))
        self.assertEqual("Manual author", self.value("SELECT creator FROM books WHERE id=20"))
        self.assertEqual("Private summary", self.value("SELECT summary FROM book_private_states WHERE book_id=20"))
        self.assertEqual(receipt["units"][0]["related_before"], receipt["units"][0]["related_after"])
        self.assertIn("app_settings", receipt["units"][0]["related_after"])
        backup = sqlite3.connect(receipt["before_backup"])
        self.addCleanup(backup.close)
        self.assertEqual(1, backup.execute("SELECT library_id FROM files WHERE id=30").fetchone()[0])
        self.assertEqual("ok", backup.execute("PRAGMA quick_check").fetchone()[0])

    def test_multiformat_shared_book_relocates_one_identity(self):
        self.seed()
        self.seed(book=20, file=31, path="/library/Child/A/two.pdf", fmt="pdf")
        plan = self.plan()
        self.assertEqual([20], plan["units"][0]["book_ids"])
        self.assertEqual([30, 31], plan["units"][0]["file_ids"])
        self.apply(plan)
        self.assertEqual(1, self.value("SELECT COUNT(*) FROM books"))
        self.assertEqual(2, self.value("SELECT COUNT(*) FROM files WHERE book_id=20 AND library_id=2"))

    def test_fileless_member_is_retained_with_state(self):
        self.seed()
        self.db.execute("INSERT INTO books(id,series_id,title,format) VALUES(21,10,'Orphan notes','pdf')")
        self.db.execute("INSERT INTO book_private_states VALUES(5,21,1,4,'manual','keep')")
        plan = self.plan()
        self.assertEqual([21], plan["units"][0]["retained_fileless_book_ids"])
        self.apply(plan)
        self.assertEqual(10, self.value("SELECT series_id FROM books WHERE id=21"))
        self.assertEqual('keep', self.value("SELECT summary FROM book_private_states WHERE book_id=21"))

    def test_split_series_or_shared_book_is_blocked(self):
        for shared in (False, True):
            with self.subTest(shared=shared):
                self.db.execute("DELETE FROM files")
                self.db.execute("DELETE FROM books")
                self.db.execute("DELETE FROM series")
                self.seed()
                self.seed(book=20 if shared else 21, file=31, path="/library/Other/B/two.epub")
                unit = self.plan()["units"][0]
                self.assertEqual("blocked", unit["status"])
                self.assertIn("members_span_roots_no_split_or_clone", unit["reasons"])

    def test_same_target_multi_directory_series_is_blocked_for_rescan_safety(self):
        self.seed()
        self.seed(book=21, file=31, path="/library/Child/A/Sub/two.epub")
        unit = self.plan()["units"][0]
        self.assertEqual("blocked", unit["status"])
        self.assertIn("multi_directory_series_requires_review", unit["reasons"])

    def test_path_boundary_and_deepest_root(self):
        self.db.execute("INSERT INTO libraries VALUES(4,'/library/Child/A','book','[]','Deepest')")
        self.seed()
        self.seed(series=11, title="Childish", directory="Childish", book=21, file=31, path="/library/Childish/two.epub")
        plan = self.plan()
        self.assertEqual(["series:10"], [u["unit_id"] for u in plan["units"]])
        self.assertEqual(4, plan["units"][0]["target_library_id"])

    def test_excluded_destination_blocks_and_does_not_fall_back_to_parent(self):
        self.seed()
        self.db.execute("UPDATE libraries SET exclude_patterns='[\"A\"]' WHERE id=2")
        unit = self.plan()["units"][0]
        self.assertEqual(2, unit["target_library_id"])
        self.assertIn("target_path_excluded", unit["reasons"])

    def test_title_collision_blocked_by_default(self):
        self.seed(title="Child", directory="Child", path="/library/Child/one.epub")
        self.seed(series=11, title="Child", directory=".", book=21, file=31,
                  path="/library/Child/two.epub", library=2)
        unit = self.plan()["units"][0]
        self.assertIn("target_series_title_collision_no_merge", unit["reasons"])

    def test_explicit_auto_collision_keeps_both_series_and_state(self):
        self.seed(title="Child", directory="Child", path="/library/Child/one.epub")
        self.seed(series=11, title="Child", directory=".", book=21, file=31,
                  path="/library/Child/two.epub", library=2)
        self.seed_state()
        self.db.execute("INSERT INTO collection_private_states VALUES(7,11,1,0)")
        plan = self.plan(resolve_auto_title_collisions=True)
        self.assertEqual('ready', plan['units'][0]['status'])
        receipt = self.apply(plan)
        self.assertEqual("Child（迁入目录）", self.value("SELECT title FROM series WHERE id=10"))
        self.assertEqual("Child", self.value("SELECT title FROM series WHERE id=11"))
        self.assertEqual(2, self.value("SELECT COUNT(*) FROM collection_private_states"))
        self.assertEqual(2, self.value("SELECT COUNT(*) FROM series"))
        self.assertEqual(receipt['units'][0]['related_before'], receipt['units'][0]['related_after'])
        self.assertEqual("already_applied", self.apply(plan)['units'][0]['status'])

    def test_curated_collision_never_auto_renames(self):
        self.seed(title="Manual title", directory="Child", path="/library/Child/one.epub")
        self.seed(series=11, title="Manual title", directory=".", book=21, file=31,
                  path="/library/Child/two.epub", library=2)
        unit = self.plan(resolve_auto_title_collisions=True)["units"][0]
        self.assertEqual("blocked", unit["status"])
        self.assertTrue(unit["title_preserved"])

    def test_different_library_title_rules_block_instead_of_copying_settings(self):
        self.seed()
        self.db.execute("INSERT INTO app_settings(key,value) VALUES('comic_title_rules:1','[]')")
        unit = self.plan()["units"][0]
        self.assertEqual("blocked", unit["status"])
        self.assertIn("comic_title_rules_differ_requires_review", unit["reasons"])
        self.assertEqual(1, self.value("SELECT COUNT(*) FROM app_settings"))

    def test_two_incoming_series_same_target_identity_blocked(self):
        self.seed(title="Shared manual title")
        self.db.execute("INSERT INTO libraries VALUES(4,'/','mixed','[]','Broad')")
        self.seed(series=11, title="Shared manual title", directory="library/Child/B", book=21, file=31,
                  path="/library/Child/B/two.epub", library=4)
        plan = self.plan()
        self.assertEqual(2, len(plan["units"]))
        self.assertTrue(all("incoming_series_identity_collision_no_merge" in u["reasons"] for u in plan["units"]))

    def test_root_or_exclusion_or_schema_drift_rejected(self):
        self.seed()
        plan = self.plan()
        for sql in ("UPDATE libraries SET root_path='/different' WHERE id=2",
                    "UPDATE libraries SET exclude_patterns='[\"A\"]' WHERE id=2"):
            with self.subTest(sql=sql):
                self.db.execute(sql)
                self.db.commit()
                with self.assertRaisesRegex(m.Refused, "configuration drift"):
                    self.apply(plan)
                self.db.execute("UPDATE libraries SET root_path='/library/Child',exclude_patterns='[]' WHERE id=2")
                self.db.commit()
        self.db.execute("CREATE TABLE new_metadata(id INTEGER)")
        self.db.commit()
        with self.assertRaisesRegex(m.Refused, "schema drift"):
            self.apply(plan)

    def test_personal_and_manual_metadata_updates_since_preview_are_preserved(self):
        self.seed()
        self.seed_state()
        plan = self.plan()
        self.db.execute("UPDATE books SET creator='New manual author',summary='Newest note',updated_at='now' WHERE id=20")
        self.db.execute("UPDATE profile_read_progress SET page_index=79,locator='latest',updated_at='now'")
        self.db.execute("UPDATE collection_private_states SET liked=0")
        self.db.execute("UPDATE manual_collections SET description='Edited between preview and apply'")
        self.db.execute("UPDATE app_settings SET value='new provenance'")
        self.db.commit()
        receipt = self.apply(plan)
        self.assertEqual("New manual author", self.value("SELECT creator FROM books WHERE id=20"))
        self.assertEqual(79, self.value("SELECT page_index FROM profile_read_progress"))
        self.assertEqual("new provenance", self.value("SELECT value FROM app_settings"))
        self.assertEqual(receipt['units'][0]['related_before'], receipt['units'][0]['related_after'])

    def test_unrelated_updates_do_not_reject_selected_batch(self):
        self.seed()
        self.seed(series=11, title="Other/A", directory="Other/A", book=21, file=31, path="/library/Other/A/two.epub")
        plan = self.plan()
        self.db.execute("UPDATE files SET size=900 WHERE id=31")
        self.db.commit()
        self.apply(plan, ["series:10"])
        self.assertEqual(1, self.value("SELECT library_id FROM files WHERE id=31"))

    def test_membership_content_title_or_ownership_drift_rejected(self):
        self.seed()
        plan = self.plan()
        for sql in ("UPDATE files SET size=11 WHERE id=30",
                    "UPDATE books SET title='New identity' WHERE id=20",
                    "UPDATE files SET rel_path='changed' WHERE id=30"):
            self.db.execute(sql)
            self.db.commit()
            with self.assertRaisesRegex(m.Refused, "identity/membership/content drift"):
                self.apply(plan)
            self.db.execute("UPDATE files SET size=10,rel_path='Child/A/one.epub' WHERE id=30")
            self.db.execute("UPDATE books SET title='Curated title 20' WHERE id=20")
            self.db.commit()

    def test_new_target_collision_rejected_before_mutation(self):
        self.seed()
        plan = self.plan()
        self.db.execute("INSERT INTO series(id,library_id,title,directory_path) VALUES(99,2,'Child/A','A')")
        self.db.commit()
        with self.assertRaisesRegex(m.Refused, "target collision drift"):
            self.apply(plan)
        self.assertEqual(1, self.value("SELECT library_id FROM series WHERE id=10"))

    def test_transaction_rollback_when_later_cas_fails(self):
        self.seed()
        self.seed(series=11, title="Other/A", directory="Other/A", book=21, file=31, path="/library/Other/A/two.epub")
        plan = self.plan()
        real = m.cas_update
        calls = []
        def fail_late(db, change):
            calls.append(change)
            if len(calls) == 4:
                raise m.Refused("forced CAS failure")
            real(db, change)
        with mock.patch.object(m, "cas_update", side_effect=fail_late):
            with self.assertRaisesRegex(m.Refused, "forced CAS failure"):
                self.apply(plan)
        self.assertEqual([1, 1], [r[0] for r in self.db.execute("SELECT library_id FROM files ORDER BY id")])
        self.assertFalse(any(x.startswith('batch-') for x in os.listdir(self.receipts)))

    def test_nonownership_side_effect_detected_and_rolled_back(self):
        self.seed()
        self.seed_state()
        plan = self.plan()
        real = m.cas_update
        def corrupt_state(db, change):
            real(db, change)
            db.execute("UPDATE profile_read_progress SET locator='unexpected side effect'")
        with mock.patch.object(m, "cas_update", side_effect=corrupt_state):
            with self.assertRaisesRegex(m.Refused, "non-ownership values changed"):
                self.apply(plan)
        self.assertEqual("profile-seven", self.value("SELECT locator FROM profile_read_progress"))
        self.assertEqual(1, self.value("SELECT library_id FROM files WHERE id=30"))

    def test_exact_reapply_idempotent_one_backup_and_no_new_plan_conflicts(self):
        self.seed()
        plan = self.plan()
        self.apply(plan)
        first_backups = [x for x in os.listdir(self.receipts) if x.endswith('.sqlite')]
        receipt = self.apply(plan)
        self.assertEqual("already_applied", receipt['units'][0]['status'])
        self.assertEqual(first_backups, [x for x in os.listdir(self.receipts) if x.endswith('.sqlite')])
        self.assertEqual([], self.plan()['units'])

    def test_wal_backup_contains_latest_committed_state(self):
        self.seed()
        self.db.execute("PRAGMA journal_mode=WAL")
        self.db.execute("UPDATE books SET creator='Committed in WAL' WHERE id=20")
        self.db.commit()
        receipt = self.apply(self.plan())
        backup = sqlite3.connect(receipt['before_backup'])
        self.addCleanup(backup.close)
        self.assertEqual('Committed in WAL', backup.execute('SELECT creator FROM books').fetchone()[0])

    def test_game_video_conflicts_block_without_decoding_invalid_title(self):
        self.db.execute("INSERT INTO games VALUES(1,1,'/library/Child/game.zip',CAST(x'ff' AS TEXT))")
        self.db.execute("INSERT INTO videos VALUES(2,1,'/library/Child/movie.mkv','Video')")
        plan = self.plan()
        self.assertEqual(2, len(plan['units']))
        self.assertTrue(all(u['status'] == 'blocked' for u in plan['units']))
        m.encoded(plan).decode('ascii')

    def test_bounded_selection_digest_and_blocked_units(self):
        self.seed()
        plan = self.plan()
        with self.assertRaises(m.Refused):
            self.apply(plan, ['series:10', 'series:10'])
        with self.assertRaises(m.Refused):
            self.apply(plan, ['series:missing'])
        plan['units'][0]['changes'][0]['after']['library_id'] = 3
        with self.assertRaisesRegex(m.Refused, 'digest mismatch'):
            self.apply(plan)

    def test_cli_output_omits_paths_and_refuses_output_overwrite(self):
        self.seed()
        output = os.path.join(self.tmp.name, 'private-plan.json')
        stdout = io.StringIO()
        with contextlib.redirect_stdout(stdout):
            self.assertEqual(0, m.main(['plan', '--database', self.path, '--output', output, '--library-id', '1']))
        self.assertNotIn('/library', stdout.getvalue())
        self.assertEqual(1, json.loads(stdout.getvalue())['ready_units'])
        stderr = io.StringIO()
        with contextlib.redirect_stderr(stderr):
            self.assertEqual(2, m.main(['plan', '--database', self.path, '--output', output]))
        self.assertNotIn(self.tmp.name, stderr.getvalue())


if __name__ == '__main__':
    unittest.main()
