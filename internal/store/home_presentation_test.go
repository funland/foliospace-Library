package store

import (
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"testing"
	"time"
)

func TestContinueReadingExcludesCompletionWithoutRounding(t *testing.T) {
	c, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := New(c)
	lib, _ := s.CreateLibrary("Books", t.TempDir())
	series, _ := s.UpsertSeries(lib.ID, "Books", ".")
	for _, tc := range []struct {
		title    string
		fraction float64
		status   string
	}{{"unfinished", .996, ""}, {"complete", 1, ""}, {"marked", .2, "finished"}} {
		b, e := s.UpsertBook(series.ID, tc.title, "cbz")
		if e != nil {
			t.Fatal(e)
		}
		if e = s.SaveProgressDetail(b.ID, 1, "", tc.fraction); e != nil {
			t.Fatal(e)
		}
		if _, e = c.Exec(`INSERT INTO book_private_states(profile_id,book_id,private_status) VALUES(1,?,?)`, b.ID, tc.status); e != nil {
			t.Fatal(e)
		}
	}
	got, e := s.ListContinueReading(10)
	if e != nil {
		t.Fatal(e)
	}
	if len(got) != 1 || got[0].Title != "unfinished" {
		t.Fatalf("continue = %+v", got)
	}
	history, e := s.ListRecentBooks(10)
	if e != nil || len(history) != 3 {
		t.Fatalf("history lost: %+v %v", history, e)
	}
}

func TestVideoDisplayTitlePreservesFileIdentityAndResolvesSearch(t *testing.T) {
	c, e := db.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	s := New(c)
	lib, e := s.CreateLibraryWithType("Movies", t.TempDir(), "video")
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.UpsertVideo(domain.VideoAsset{LibraryID: lib.ID, Title: "release.x265.mkv", FilePath: lib.RootPath + "/release.mkv", Format: "mkv", MTime: time.Now()})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SaveVideoLocalMetadata(v.ID, "rev", domain.VideoLocalMetadata{Kind: "movie", Title: "Local title", Year: 1991}); e != nil {
		t.Fatal(e)
	}
	m, _ := s.VideoMetadata(v.ID)
	if e = s.MatchVideo(v.ID, domain.VideoMatchInput{Generation: m.Generation, Title: "匹配片名", Kind: "movie", Year: 1991}); e != nil {
		t.Fatal(e)
	}
	got, e := s.VideoByID(v.ID)
	if e != nil || got.Title != v.Title || got.DisplayTitle != "匹配片名" {
		t.Fatalf("resolved = %+v %v", got, e)
	}
	page, e := s.ListVideosPage(domain.VideoListOptions{Query: "匹配片名"})
	if e != nil || page.Total != 1 {
		t.Fatalf("search = %+v %v", page, e)
	}
	if e = s.SetVideoMetadataOverride(v.ID, "title", `"人工片名"`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.UpsertVideo(v); e != nil {
		t.Fatal(e)
	}
	got, e = s.VideoByID(v.ID)
	if e != nil || got.Title != v.Title || got.DisplayTitle != "人工片名" {
		t.Fatalf("override lost: %+v %v", got, e)
	}
	works, e := s.VideoWorks("人工片名", "movie", 30, 0)
	if e != nil || len(works.Items) != 1 || works.Items[0].Title != got.DisplayTitle {
		t.Fatalf("movie wall: %+v %v", works, e)
	}
	files, e := s.VideoWorkFiles(works.Items[0].ID, nil, 30, 0)
	if e != nil || len(files.Items) != 1 || files.Items[0].Title != got.DisplayTitle {
		t.Fatalf("movie files: %+v %v", files, e)
	}

}

func TestContinueReadingKeepsEarlierEPUBChapter(t *testing.T) {
	c, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := New(c)
	lib, _ := s.CreateLibrary("Books", t.TempDir())
	series, _ := s.UpsertSeries(lib.ID, "Books", ".")
	b, _ := s.UpsertBook(series.ID, "EPUB", "epub")
	c.Exec(`UPDATE books SET page_count=10 WHERE id=?`, b.ID)
	s.SaveProgressDetail(b.ID, 0, "5", 1)
	got, err := s.ListContinueReading(10)
	if err != nil || len(got) != 1 {
		t.Fatalf("chapter hidden: %+v %v", got, err)
	}
	s.SaveProgressDetail(b.ID, 9, "5", 1)
	got, err = s.ListContinueReading(10)
	if err != nil || len(got) != 0 {
		t.Fatalf("finished retained: %+v %v", got, err)
	}
}

func TestPrivateTagsDoNotReplaceImportedMetadata(t *testing.T) {
	c, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := New(c)
	lib, _ := s.CreateLibrary("Books", t.TempDir())
	series, _ := s.UpsertSeries(lib.ID, "Books", ".")
	b, _ := s.UpsertBook(series.ID, "Imported", "epub")
	if _, err = s.UpdateBookMetadata(b.ID, "Author", "Description", []string{"Calibre tag"}); err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateBookPrivateState(b.ID, domain.BookPrivateState{Tags: []string{"Personal tag"}, Favorite: true}); err != nil {
		t.Fatal(err)
	}
	var shared string
	if err = c.QueryRow(`SELECT tags FROM books WHERE id=?`, b.ID).Scan(&shared); err != nil || shared != "Calibre tag" {
		t.Fatalf("shared tag overwritten: %q %v", shared, err)
	}
	b, err = s.BookByID(b.ID)
	if err != nil || len(b.Tags) != 2 || !b.Favorite {
		t.Fatalf("merged tags/state: %+v %v", b, err)
	}
}
