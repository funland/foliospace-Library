package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPC98PerGameFontSelection(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("font.bmp", syntheticPC98FontBitmap(0))
	write("Font_CN_VA.BMP", syntheticPC98FontBitmap(255))
	write("VirginAngelCN.HDI.FONT.JSON", []byte(`{"file":"font_cn_va.bmp"}`))
	selected := func(media, want string) {
		t.Helper()
		files, err := pc98PackageFontFiles(filepath.Join(dir, media))
		if err != nil || len(files) != 1 || filepath.Base(files[0].FilePath) != want || files[0].Name != "FONT.bmp" || files[0].Role != "font" || files[0].SHA1 == "" {
			t.Fatalf("%s: %#v %v", media, files, err)
		}
	}
	selected("VirginAngelCN.hdi", "Font_CN_VA.BMP")
	selected("VirginAngelJP.hdi", "font.bmp")
	write("VirginAngelJP.font.BMP", syntheticPC98FontBitmap(1))
	selected("VirginAngelJP.hdi", "VirginAngelJP.font.BMP")
	for _, config := range []string{`{"file":"../outside.bmp"}`, `{"file":"/outside.bmp"}`, `{"file":"missing.bmp"}`, `{"file":"invalid.bmp"}`, `not-json`} {
		write("invalid.bmp", []byte("not a font"))
		write("VirginAngelCN.HDI.FONT.JSON", []byte(config))
		selected("VirginAngelCN.hdi", "font.bmp")
	}
	outside := filepath.Join(t.TempDir(), "outside.bmp")
	if err := os.WriteFile(outside, syntheticPC98FontBitmap(5), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linked.bmp")); err != nil {
		t.Fatal(err)
	}
	write("VirginAngelCN.HDI.FONT.JSON", []byte(`{"file":"linked.bmp"}`))
	selected("VirginAngelCN.hdi", "font.bmp")
	// Same-sized font changes with preserved mtime must refresh cached checksum.
	old, _ := pc98PackageFontFiles(filepath.Join(dir, "VirginAngelCN.hdi"))
	write("font.bmp", syntheticPC98FontBitmap(42))
	if err := os.Chtimes(filepath.Join(dir, "font.bmp"), old[0].MTime, old[0].MTime); err != nil {
		t.Fatal(err)
	}
	current, _ := pc98PackageFontFiles(filepath.Join(dir, "VirginAngelCN.hdi"))
	if samePC98SupportFiles(filepath.Join(dir, "VirginAngelCN.hdi"), current, old) {
		t.Fatal("same-size content change was skipped")
	}
	if err := os.Remove(filepath.Join(dir, "font.bmp")); err != nil {
		t.Fatal(err)
	}
	files, err := pc98PackageFontFiles(filepath.Join(dir, "VirginAngelCN.hdi"))
	if err != nil || len(files) != 0 {
		t.Fatalf("arbitrary dedicated BMP must not be guessed: %#v %v", files, err)
	}
}
