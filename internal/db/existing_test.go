package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaintenanceOpenDoesNotMigrateOrCreate(t *testing.T) {
	root := t.TempDir()
	if c, err := OpenExisting(root, false); err == nil {
		c.Close()
		t.Fatal("created absent database")
	}
	if _, err := os.Stat(filepath.Join(root, "foliospace-reader.db")); !os.IsNotExist(err) {
		t.Fatal("created database file")
	}
	c, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Exec(`INSERT INTO libraries(id,name,root_path) VALUES(1,'Games','/library');
 INSERT INTO games(id,library_id,title,platform,rom_set_name,emulator_hint,format,file_path,rel_path,size,mtime) VALUES(1,1,'Manual title','n64','Manual set','Manual emulator','z64','/library/x.z64','x.z64',1,'2026-01-01');
 INSERT INTO series(id,library_id,title) VALUES(1,1,'Books');
 INSERT INTO books(id,series_id,title,format,private_status,tags) VALUES(1,1,'Book','epub','reading','public tag');`)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	for _, ro := range []bool{true, false} {
		c, err = OpenExisting(root, ro)
		if err != nil {
			t.Fatal(err)
		}
		var set, hint string
		var n int
		if err = c.QueryRow("SELECT rom_set_name,emulator_hint FROM games WHERE id=1").Scan(&set, &hint); err != nil || set != "Manual set" || hint != "Manual emulator" {
			t.Fatalf("migrated: %q %q %v", set, hint, err)
		}
		if err = c.QueryRow("SELECT count(*) FROM book_private_states").Scan(&n); err != nil || n != 0 {
			t.Fatalf("private state backfill: %d %v", n, err)
		}
		if ro {
			if _, err = c.Exec("UPDATE games SET title='bad'"); err == nil {
				t.Fatal("dry run connection writable")
			}
		}
		c.Close()
	}
}
