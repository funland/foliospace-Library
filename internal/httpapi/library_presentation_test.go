package httpapi

import (
	"encoding/json"
	"foliospace-reader/internal/domain"
	"strings"
	"testing"
)

func TestClientPresentationKeepsRawFieldsAndOpaqueDiscScope(t *testing.T) {
	book := clientBookItem(domain.Book{ID: 7, Title: "Original title", Creator: "By Andy Weir;", DisplayCreator: "Andy Weir", CollectionTitle: "raw/path", CollectionDisplayTitle: "Collection"})
	data, _ := json.Marshal(book)
	for _, wanted := range []string{`"creator":"By Andy Weir;"`, `"displayCreator":"Andy Weir"`, `"collectionDisplayTitle":"Collection"`} {
		if !strings.Contains(string(data), wanted) {
			t.Fatalf("projection missing %s: %s", wanted, data)
		}
	}
	game := clientGameItem(domain.GameAsset{ID: 1, LibraryID: 7, Title: "Game", Platform: "3do", FilePath: "/private/library/Game (Disc 1).iso", RelPath: "Game (Disc 1).iso", Format: "iso"})
	if game.DiscGroupInfo == nil || game.DiscGroupInfo.DiscNumber != 1 {
		t.Fatal("disc projection missing")
	}
	data, _ = json.Marshal(game)
	if strings.Contains(string(data), "/private/library") {
		t.Fatal("group leaked path")
	}
}
