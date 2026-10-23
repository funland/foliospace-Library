package main

import (
	"crypto/sha1"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/launchcatalog"
	"foliospace-reader/internal/store"
)

func TestMAMERebuildClassifiesVerifiedDependenciesAndKeepsRunnableParents(t *testing.T) {
	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	root := t.TempDir()
	library, err := st.CreateLibrary("Games", root)
	if err != nil {
		t.Fatal(err)
	}
	var xml strings.Builder
	xml.WriteString(`<mame build="0.288">`)
	var candidates []domain.GameAsset
	before := make(map[int64][]byte)
	for _, fixture := range []struct{ name, attributes string }{
		{"bios", `isbios="yes"`},
		{"sound", `isdevice="yes" runnable="no"`},
		{"parent", ``},
		{"clone", `romof="parent" cloneof="parent"`},
		{"badbios", `isbios="yes"`},
	} {
		rom := []byte(fixture.name + " rom contents")
		fmt.Fprintf(&xml, `<machine name="%s" %s><rom name="%s.rom" size="%d" crc="%08x"/></machine>`, fixture.name, fixture.attributes, fixture.name, len(rom), crc32.ChecksumIEEE(rom))
		filePath := filepath.Join(root, fixture.name+".zip")
		if fixture.name == "badbios" {
			rom = []byte("unrelated content")
		}
		writeMAMETestArchive(t, filePath, map[string][]byte{fixture.name + ".rom": rom})
		data, readErr := os.ReadFile(filePath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		entry, upsertErr := st.UpsertGame(domain.GameAsset{LibraryID: library.ID, Title: fixture.name, Platform: "mame", FilePath: filePath, RelPath: fixture.name + ".zip", Format: "zip", Size: int64(len(data)), SHA1: fmt.Sprintf("%x", sha1.Sum(data)), CatalogRole: "needs-curation"})
		if upsertErr != nil {
			t.Fatal(upsertErr)
		}
		if err := st.ReplaceGameFiles(entry.ID, []domain.GameFile{{GameID: entry.ID, Name: fixture.name + ".zip", FilePath: filePath, Size: entry.Size, SHA1: entry.SHA1, Role: "entry"}}); err != nil {
			t.Fatal(err)
		}
		if err := st.UpdateGamePrivateStateForProfile(entry.ID, 1, domain.GamePrivateState{Favorite: true, Liked: true}); err != nil {
			t.Fatal(err)
		}
		candidates = append(candidates, entry)
		before[entry.ID] = data
	}
	xml.WriteString(`</mame>`)
	xmlPath := filepath.Join(root, "list.xml")
	if err := os.WriteFile(xmlPath, []byte(xml.String()), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := rebuildMAMEProfiles(st, xmlPath, map[string]bool{"mame": true}, candidates, defaultLaunchProfileTargets("mame"), 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.DependenciesClassified != 2 || result.Result.GamesReady != 2 || result.Result.GamesRejected != 1 || len(result.Failures) != 1 {
		t.Fatalf("unexpected rebuild result: %+v", result)
	}
	for _, original := range candidates {
		updated, readErr := st.GameByID(original.ID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		wantRole := launchcatalog.RoleGame
		if original.Title == "bios" || original.Title == "sound" {
			wantRole = launchcatalog.RoleDependency
		} else if original.Title == "badbios" {
			wantRole = launchcatalog.RoleNeedsCuration
		}
		if updated.CatalogRole != wantRole || updated.FilePath != original.FilePath || !updated.Favorite || !updated.Liked || updated.SHA1 != original.SHA1 {
			t.Fatalf("wrong classification or changed identity/state: %+v", updated)
		}
		files, filesErr := st.GameFiles(original.ID)
		if filesErr != nil || len(files) != 1 || files[0].FilePath != original.FilePath {
			t.Fatalf("source manifest changed: %+v %v", files, filesErr)
		}
		data, fileErr := os.ReadFile(original.FilePath)
		if fileErr != nil || string(data) != string(before[original.ID]) {
			t.Fatalf("source bytes changed: %v", fileErr)
		}
		profiles, profileErr := st.GameLaunchProfiles(original.ID)
		if profileErr != nil || (wantRole == launchcatalog.RoleDependency && len(profiles) != 0) {
			t.Fatalf("dependency incorrectly promoted to a launch profile: %+v %v", profiles, profileErr)
		}
	}
}
