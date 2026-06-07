import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import ts from "typescript";

const srcDir = path.dirname(fileURLToPath(import.meta.url));

async function loadEpubPositionModule() {
  const source = await readFile(path.join(srcDir, "epub-position.ts"), "utf8");
  const transpiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.ES2020,
      target: ts.ScriptTarget.ES2020,
    },
  }).outputText;
  return import(`data:text/javascript;base64,${Buffer.from(transpiled).toString("base64")}`);
}

test("epub v1 locator remains parseInt-compatible with legacy clients", async () => {
  const { encodeEpubLocator, readEpubLocator } = await loadEpubPositionModule();
  const locator = encodeEpubLocator({
    schema: "epub-position-v1",
    anchorType: "text",
    spineIndex: 4,
    spineHref: "OEBPS/Text/genesis.xhtml",
    viewportAnchorRatio: 0.28,
    documentProgress: 0.12,
    legacyPagePosition: 18,
    layoutPageCount: 90,
    text: {
      blockKey: "id:gen.1.1",
      textOffset: 42,
      textHash: "abc123",
    },
  });

  assert.equal(Number.parseInt(locator, 10), 18);
  assert.equal(locator.startsWith("18|epub-v1:"), true);
  assert.deepEqual(readEpubLocator(locator).position?.text, {
    blockKey: "id:gen.1.1",
    textOffset: 42,
    textHash: "abc123",
  });
});

test("epub locator parser preserves old integer locators", async () => {
  const { readEpubLocator } = await loadEpubPositionModule();

  assert.deepEqual(readEpubLocator("27"), {
    legacyPagePosition: 27,
    position: null,
  });
});

test("epub text anchors preserve explicit navigation target metadata", async () => {
  const { encodeEpubLocator, readEpubLocator } = await loadEpubPositionModule();
  const locator = encodeEpubLocator({
    schema: "epub-position-v1",
    anchorType: "text",
    spineIndex: 5,
    spineHref: "OEBPS/ch004.xhtml",
    viewportAnchorRatio: 0.28,
    documentProgress: 0.07,
    legacyPagePosition: 34,
    layoutPageCount: 666,
    text: {
      blockKey: "id:v01003001",
      targetFragmentID: "v01003001",
      textOffset: 0,
      textHash: "4a5f039b",
    },
  });

  assert.equal(readEpubLocator(locator).position?.text?.targetFragmentID, "v01003001");
});

test("epub anchor selection protects image-dominant comic pages from text anchors", async () => {
  const { chooseEpubAnchorType } = await loadEpubPositionModule();

  assert.equal(
    chooseEpubAnchorType({
      hasAnchorMedia: true,
      hasAnchorText: true,
      documentTextLength: 80,
      mediaCoverageRatio: 0.92,
    }),
    "media",
  );
  assert.equal(
    chooseEpubAnchorType({
      hasAnchorMedia: true,
      hasAnchorText: true,
      documentTextLength: 18000,
      mediaCoverageRatio: 0.18,
    }),
    "text",
  );
});

test("epub navigation captures prefer the clicked fragment target over viewport text", async () => {
  const { preferEpubNavigationCapture } = await loadEpubPositionModule();

  assert.equal(
    preferEpubNavigationCapture({
      spineHref: "OEBPS/ch004.xhtml",
      targetHref: "OEBPS/ch004.xhtml#v01003001",
      userInitiated: true,
    }),
    "v01003001",
  );
  assert.equal(
    preferEpubNavigationCapture({
      spineHref: "OEBPS/ch004.xhtml",
      targetHref: "OEBPS/ch004.xhtml#v01003001",
      userInitiated: false,
    }),
    "",
  );
  assert.equal(
    preferEpubNavigationCapture({
      spineHref: "OEBPS/ch004.xhtml",
      targetHref: "OEBPS/ch005.xhtml#v02001001",
      userInitiated: true,
    }),
    "",
  );
});
