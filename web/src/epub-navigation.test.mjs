import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import ts from "typescript";

const srcDir = path.dirname(fileURLToPath(import.meta.url));

async function loadEpubNavigationModule() {
  const source = await readFile(path.join(srcDir, "epub-navigation.ts"), "utf8");
  const transpiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.ES2020,
      target: ts.ScriptTarget.ES2020,
    },
  }).outputText;
  return import(`data:text/javascript;base64,${Buffer.from(transpiled).toString("base64")}`);
}

test("EPUB previous chapter opens at the last measured page", async () => {
  const { resolveEpubOpenPosition } = await loadEpubNavigationModule();
  assert.equal(resolveEpubOpenPosition("end", 7), 6);
});

test("EPUB chapter start opens at the first page", async () => {
  const { resolveEpubOpenPosition } = await loadEpubNavigationModule();
  assert.equal(resolveEpubOpenPosition("start", 7), 0);
});

test("EPUB resource links retain encoded fragments but discard authentication queries", async () => {
  const { normalizeEPUBLink, epubFragment } = await loadEpubNavigationModule();
  const href = normalizeEPUBLink("text/part0004.html", "/api/books/27183/epub/resources/text/part0006.html?token=private#%E6%B3%A8%E4%B8%80");
  assert.equal(href, "text/part0006.html#%E6%B3%A8%E4%B8%80");
  assert.equal(epubFragment(href), "注一");
  assert.equal(normalizeEPUBLink("text/part0004.html", "#note:1"), "text/part0004.html#note:1");
  assert.equal(normalizeEPUBLink("OEBPS/text/chapter.xhtml", "../notes.xhtml#note-2"), "OEBPS/notes.xhtml#note-2");
});

test("external and executable links cannot become an internal chapter jump", async () => {
  const { normalizeEPUBLink, resolveEpubHrefIndex } = await loadEpubNavigationModule();
  assert.equal(normalizeEPUBLink("text/chapter.xhtml", "javascript:alert(1)"), null);
  assert.equal(normalizeEPUBLink("text/chapter.xhtml", "https://other.invalid/epub/resources/chapter.xhtml"), null);
  assert.equal(resolveEpubHrefIndex({ spine: [{ index: 0, href: "chapter.xhtml" }], toc: [] }, "missing.xhtml#note"), null);
});

test("chapter label uses matching TOC paths and preserves split-chapter names", async () => {
  const { epubChapterLabel } = await loadEpubNavigationModule();
  const manifest = {
    spine: [
      { index: 0, href: "text/title.xhtml" },
      { index: 1, href: "text/chapter.xhtml" },
      { index: 2, href: "text/chapter-continued.xhtml" },
      { index: 3, href: "text/notes.xhtml" },
    ],
    toc: [
      { index: 99, href: "text/chapter.xhtml#start", label: "第一章　金瓯缺" },
      { index: 0, href: "text/notes.xhtml", label: "注释" },
    ],
  };
  assert.equal(epubChapterLabel(manifest, 0), "");
  assert.equal(epubChapterLabel(manifest, 1), "第一章　金瓯缺");
  assert.equal(epubChapterLabel(manifest, 2), "第一章　金瓯缺");
  assert.equal(epubChapterLabel(manifest, 3), "注释");
});

test("fragment lookup treats punctuation as a literal ID and supports old named anchors", async () => {
  const { findEpubFragment } = await loadEpubNavigationModule();
  const note = { tagName: "p" };
  const legacy = { tagName: "A" };
  const document = {
    getElementById: (id) => id === "note:1[2]" ? note : null,
    getElementsByName: (name) => name === "old-note" ? [{ tagName: "input" }, legacy] : [],
  };
  assert.equal(findEpubFragment(document, "note:1[2]"), note);
  assert.equal(findEpubFragment(document, "old-note"), legacy);
  assert.equal(findEpubFragment(document, "missing"), null);
});

test("small text glyphs retain inline sizing without changing full-size illustrations", async () => {
  const { isEpubInlineGlyph } = await loadEpubNavigationModule();
  assert.equal(isEpubInlineGlyph(20, 19, true), true);
  assert.equal(isEpubInlineGlyph(20, 20, true), true);
  assert.equal(isEpubInlineGlyph(20, 20, false), false);
  assert.equal(isEpubInlineGlyph(600, 800, true), false);
  assert.equal(isEpubInlineGlyph(0, 0, true), false);
  assert.equal(isEpubInlineGlyph(40, 2, true), false);
});
