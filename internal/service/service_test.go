package service

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
)

func TestListDirectoriesRestrictsToConfiguredRoots(t *testing.T) {
	allowed := t.TempDir()
	libraryRoot := t.TempDir()
	blocked := t.TempDir()
	if err := mkdir(filepath.Join(allowed, "Books")); err != nil {
		t.Fatal(err)
	}
	if err := mkdir(filepath.Join(libraryRoot, "Comics")); err != nil {
		t.Fatal(err)
	}
	if err := mkdir(filepath.Join(blocked, "Private")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FOLIOSPACE_DIRECTORY_ROOTS", allowed)
	t.Setenv("FOLIOSPACE_LIBRARY_DIR", "")

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	if _, err := st.CreateLibrary("Existing", libraryRoot); err != nil {
		t.Fatal(err)
	}

	svc := New(st)
	rootListing, err := svc.ListDirectories("/")
	if err != nil {
		t.Fatal(err)
	}
	if !hasDirectory(rootListing.Entries, allowed) || !hasDirectory(rootListing.Entries, libraryRoot) {
		t.Fatalf("root entries = %#v, want configured and existing library roots", rootListing.Entries)
	}
	if hasDirectory(rootListing.Entries, blocked) {
		t.Fatalf("root entries = %#v, blocked directory should not be exposed", rootListing.Entries)
	}

	allowedListing, err := svc.ListDirectories(allowed)
	if err != nil {
		t.Fatal(err)
	}
	if allowedListing.Parent != "/" {
		t.Fatalf("allowed root parent = %q, want virtual root", allowedListing.Parent)
	}
	if !hasDirectory(allowedListing.Entries, filepath.Join(allowed, "Books")) {
		t.Fatalf("allowed entries = %#v, want Books child", allowedListing.Entries)
	}

	if _, err := svc.ListDirectories(blocked); err == nil {
		t.Fatal("blocked directory listing succeeded, want error")
	}
}

func TestOpenVideoThumbnailUsesLocalSidecarImage(t *testing.T) {
	root := t.TempDir()
	videoPath := filepath.Join(root, "Demo Movie.mp4")
	coverPath := filepath.Join(root, "Demo Movie.jpg")
	if err := os.WriteFile(videoPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(coverPath, []byte{0xff, 0xd8, 0xff, 0xdb, 0x00, 0x43, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st := store.New(conn)
	lib, err := st.CreateLibraryWithType("Movies", root, "video")
	if err != nil {
		t.Fatal(err)
	}
	video, err := st.UpsertVideo(domain.VideoAsset{
		LibraryID:       lib.ID,
		Title:           "Demo Movie",
		Format:          "mp4",
		FilePath:        videoPath,
		RelPath:         "Demo Movie.mp4",
		Size:            5,
		MTime:           time.Unix(1, 0),
		ThumbnailStatus: "placeholder",
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := NewWithConfig(st, t.TempDir()).OpenVideoThumbnail(video.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	data, err := io.ReadAll(stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	if stream.ContentType != "image/jpeg" || len(data) == 0 {
		t.Fatalf("stream contentType=%q len=%d, want local jpeg", stream.ContentType, len(data))
	}
}

func mkdir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func hasDirectory(entries []domain.DirectoryEntry, path string) bool {
	for _, entry := range entries {
		if entry.Path == path {
			return true
		}
	}
	return false
}
