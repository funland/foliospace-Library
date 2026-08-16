package db

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrateBackfillsLegacyArcadeSingleFileManifests(t *testing.T) {
	configDir := t.TempDir()
	conn, err := Open(configDir)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	gamePath := filepath.Join(t.TempDir(), "legacy-model3.zip")
	if err := os.WriteFile(gamePath, []byte("rom"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(gamePath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := conn.Exec(`INSERT INTO libraries(name, root_path, asset_type) VALUES('Games', ?, 'game')`, filepath.Dir(gamePath))
	if err != nil {
		t.Fatal(err)
	}
	libraryID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	result, err = conn.Exec(`INSERT INTO games(library_id, title, platform, rom_set_name, format, file_path, rel_path, size, mtime, crc32, sha1, emulator_hint)
		VALUES(?, 'Legacy Model 3', 'model3', 'Model3ROMs', 'zip', ?, 'Model3ROMs/legacy-model3.zip', ?, ?, 'crc', 'sha', 'model3')`,
		libraryID, gamePath, info.Size(), info.ModTime().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	gameID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	if err := Migrate(conn); err != nil {
		t.Fatal(err)
	}
	var name, filePath, role string
	var size int64
	var position int
	if err := conn.QueryRow(`SELECT name, file_path, size, role, position FROM game_files WHERE game_id = ?`, gameID).
		Scan(&name, &filePath, &size, &role, &position); err != nil {
		t.Fatal(err)
	}
	if name != filepath.Base(gamePath) || filePath != gamePath || size != info.Size() || role != "entry" || position != 0 {
		t.Fatalf("backfilled manifest = %q %q %d %q %d", name, filePath, size, role, position)
	}

	if err := Migrate(conn); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM game_files WHERE game_id = ?`, gameID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("manifest rows after repeated migration = %d, want 1", count)
	}
}

func TestMigrateReconcilesLaunchCatalogRoles(t *testing.T) {
	configDir := t.TempDir()
	conn, err := Open(configDir)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	result, err := conn.Exec(`INSERT INTO libraries(name, root_path, asset_type) VALUES('Games', '/library', 'game')`)
	if err != nil {
		t.Fatal(err)
	}
	libraryID, _ := result.LastInsertId()
	insertGame := func(title, platform, path string, size int64, sha1, role string) int64 {
		t.Helper()
		result, err := conn.Exec(`INSERT INTO games(library_id, title, platform, format, file_path, rel_path, size, mtime, sha1, catalog_role, updated_at)
			VALUES(?, ?, ?, 'zip', ?, ?, ?, '2026-01-01T00:00:00Z', ?, ?, '2026-01-01T00:00:00Z')`,
			libraryID, title, platform, path, filepath.Base(path), size, sha1, role)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		return id
	}

	readyID := insertGame("Virtua Striker", "model2", "/library/vstriker.zip", 10313686, "8e3518318eeb157ab299b2f284faef176d3f49dd", "needs-curation")
	legacySF2ID := insertGame("Street Fighter II", "arcade", "/library/sf2.zip", 3551819, "bd59872a57f14dc492e2fb387727a9402f3d4f97", "game")
	if _, err := conn.Exec(`UPDATE games SET rom_set_name = 'FBNeo', emulator_hint = 'arcade' WHERE id = ?`, legacySF2ID); err != nil {
		t.Fatal(err)
	}
	unverifiedID := insertGame("Unknown Arcade", "arcade", "/library/unknown.zip", 100, "1111111111111111111111111111111111111111", "game")
	biosID := insertGame("Neo Geo BIOS", "neogeo", "/library/neogeo.zip", 100, "2222222222222222222222222222222222222222", "game")
	pointBlankID := insertGame("Point Blank", "arcade", "/library/ptblank.zip", 5033400, "15f9dd6ccf009bffcb156b234bdeadbe26344314", "needs-curation")
	namcoC75ID := insertGame("Namco C75", "arcade", "/library/namcoc75.zip", 8709, "0649e27b7d605add7fc4215ee628b71e3c835328", "game")
	if _, err := conn.Exec(`INSERT INTO game_launch_profiles(
		game_id, profile_id, profile_revision, priority, policy,
		client_name, min_client_version, client_platform, architecture,
		runtime_id, runtime_version, content_set, entry_file, canonical_set, status)
		VALUES(?, 'legacy-namcoc75-profile', 1, 100, 'legacy-mame',
		'SpatialEMU.Windows', '1.302', 'windows-x64', 'x64',
		'mame', '0.288', 'mame-0.288', 'namcoc75.zip', 'namcoc75', 'ready')`, namcoC75ID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE games SET emulator_hint = 'mame' WHERE id = ?`, pointBlankID); err != nil {
		t.Fatal(err)
	}
	dosReadyID := insertGame("DOS Ready", "dos", "/library/ready.zip", 100, "3333333333333333333333333333333333333333", "needs-curation")
	dosUnknownID := insertGame("DOS Unknown", "dos", "/library/unknown-dos.zip", 100, "4444444444444444444444444444444444444444", "game")
	if _, err := conn.Exec(`INSERT INTO game_dos_launch(game_id, entry_file, entry_source) VALUES(?, 'PLAY.BAT', 'curated')`, dosReadyID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO game_dos_launch(game_id, entry_file, entry_source) VALUES(?, '', 'unknown')`, dosUnknownID); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(conn); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[int64]string{
		readyID: "game", legacySF2ID: "game", unverifiedID: "needs-curation", biosID: "dependency", pointBlankID: "game", namcoC75ID: "dependency", dosReadyID: "game", dosUnknownID: "needs-curation",
	} {
		var got string
		if err := conn.QueryRow(`SELECT catalog_role FROM games WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("game %d role = %q, want %q", id, got, want)
		}
	}
	var platform, romSetName, emulatorHint string
	if err := conn.QueryRow(`SELECT platform, rom_set_name, emulator_hint FROM games WHERE id = ?`, legacySF2ID).
		Scan(&platform, &romSetName, &emulatorHint); err != nil {
		t.Fatal(err)
	}
	if platform != "cps1" || romSetName != "sf2" || emulatorHint != "fbneo" {
		t.Fatalf("legacy sf2 metadata = %q %q %q, want cps1 sf2 fbneo", platform, romSetName, emulatorHint)
	}
	if err := conn.QueryRow(`SELECT platform, rom_set_name, emulator_hint FROM games WHERE id = ?`, pointBlankID).
		Scan(&platform, &romSetName, &emulatorHint); err != nil {
		t.Fatal(err)
	}
	if platform != "arcade" || romSetName != "ptblank" || emulatorHint != "fbneo" {
		t.Fatalf("legacy ptblank metadata = %q %q %q, want arcade ptblank fbneo", platform, romSetName, emulatorHint)
	}
	var updatedAt string
	if err := conn.QueryRow(`SELECT updated_at FROM games WHERE id = ?`, unverifiedID).Scan(&updatedAt); err != nil {
		t.Fatal(err)
	}
	if updatedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("catalog role migration changed updated_at to %q", updatedAt)
	}
	if err := Migrate(conn); err != nil {
		t.Fatal(err)
	}
}
