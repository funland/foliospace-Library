import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";

const source = await readFile(new URL("./library-presentation.ts", import.meta.url), "utf8");
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ES2020, target: ts.ScriptTarget.ES2020 } }).outputText;
const presentation = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`);

test("collection cards reuse the server cleaning policy without rewriting curated titles", () => {
  const library = { name: "  漫画库  ", rootPath: "/media/comics" };
  assert.equal(presentation.collectionDisplayTitle({ title: "目录/001 [作者] 作品-115", directoryPath: "目录/001 [作者] 作品-115", displayTitle: " [作者] 作品 " }, library), "[作者] 作品");
  assert.equal(presentation.collectionDisplayTitle({ title: "Manual / Title -115", directoryPath: "folder" }, library), "Manual / Title -115");
  assert.equal(presentation.collectionDisplayTitle({ title: ".", directoryPath: "." }, library), "漫画库");
  assert.equal(presentation.originalCollectionPath(".", library), "/media/comics");
  assert.equal(presentation.originalCollectionPath("nested/folder", library), "nested/folder");
});

test("metadata trims whitespace and removes repeated title and author fields", () => {
  assert.equal(presentation.distinctMetadata([" 收藏 ", " 作者 A ", " 作品 ", "作者  A"], ["作品"]), "收藏 · 作者 A");
  assert.equal(presentation.distinctMetadata(["收藏", "作者 A", " 作品 "], ["作品", "作者 A"]), "收藏");
  assert.equal(presentation.bookDisplayCreator({ creator: "By 作者 A /", displayCreator: "作者 A" }), "作者 A");
  assert.equal(presentation.bookCollectionTitle({ collectionTitle: "001 [作者] 作品-115", collectionDisplayTitle: " [作者] 作品 " }), "[作者] 作品");
  assert.equal(presentation.bookCollectionTitle({ collectionTitle: "." }), "");
});

test("comic direct and subtree totals have distinct scope labels", () => {
  assert.equal(presentation.collectionCountScope("collection", "zh"), "本目录");
  assert.equal(presentation.collectionCountScope("folder", "zh"), "含子目录");
  assert.equal(presentation.loadedDiscCount(2, "zh"), "已加载 2 张光盘");
});

test("curation statuses translate in every supported locale and preserve unknown diagnostics", () => {
  for (const locale of ["zh", "zht", "en", "ja", "ko"]) {
    for (const code of ["needs-curation", "missing-cover", "unmatched-metadata", "launch-profile-missing", "untested", "playable", "broken"]) {
      assert.notEqual(presentation.gameStateLabel(code, locale), code);
    }
  }
  assert.equal(presentation.gameStateLabel("future-status", "zh"), "future-status");
  assert.equal(presentation.gameStateLabel("untested", "unsupported-locale"), "Untested");
});

test("cover matching distinguishes checked items from newly matched covers", () => {
  const summary = presentation.gameTaskSummary({ action: "covers-local", processed: 50, total: 0, matched: 0, skipped: 0, failed: 0 }, "zh");
  assert.equal(summary.text, "已检查 50 · 新增封面 0 · 未新增封面 50 · 失败 0");
  assert.match(summary.hint, /未新增不代表失败/);
  assert.equal(presentation.gameTaskSummary({ action: "analyze", processed: 50, total: 0, matched: 3, failed: 0 }, "zh").text, "已检查 50 · 分类已调整 3 · 分类未变 47 · 失败 0");
  assert.equal(presentation.gameTaskSummary({ action: "checksums", processed: 5, total: 8, matched: 3, skipped: 1, failed: 1 }, "en").text, "Files processed 5 / 8 · Checksums added 3 · Skipped 1 · Failed 1");
});

test("queue-only thumbnail polling cannot turn unchecked cache fields into a zero disk count", () => {
  const summary = { ready: 40, cache: { files: 0, algorithmVersion: "" } };
  const inspected = { ready: 39, cache: { files: 12, algorithmVersion: "thumb-v1" } };
  assert.equal(presentation.hasThumbnailCacheDetails(summary), false);
  assert.equal(presentation.hasThumbnailCacheDetails(null), false);
  assert.deepEqual(presentation.mergeThumbnailStatus(null, summary), summary);
  assert.deepEqual(presentation.mergeThumbnailStatus(inspected, summary), { ...summary, cache: inspected.cache });
  const emptyDisk = { ready: 40, cache: { files: 0, algorithmVersion: "thumb-v1" } };
  assert.equal(presentation.hasThumbnailCacheDetails(emptyDisk), true);
  assert.deepEqual(presentation.mergeThumbnailStatus(inspected, emptyDisk), emptyDisk);
});

test("video scan records describe their library and do not infer retry loops", () => {
  assert.equal(presentation.scanJobDescription({ id: 1536 }, { name: " 视频 ", assetType: "video" }, "zh"), "视频库扫描 · 视频");
});
