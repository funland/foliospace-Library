import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import ts from "typescript";

const srcDir = path.dirname(fileURLToPath(import.meta.url));

async function loadReaderLayoutModule() {
  const source = await readFile(path.join(srcDir, "reader-layout.ts"), "utf8");
  const transpiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.ES2020,
      target: ts.ScriptTarget.ES2020,
    },
  }).outputText;
  return import(`data:text/javascript;base64,${Buffer.from(transpiled).toString("base64")}`);
}

test("reader device preferences normalize unknown values to safe defaults", async () => {
  const { normalizeReaderDevicePreferences } = await loadReaderLayoutModule();

  assert.deepEqual(normalizeReaderDevicePreferences({ imageDisplayMode: "width", controlLayout: "left" }), {
    imageDisplayMode: "width",
    controlLayout: "left",
  });
  assert.deepEqual(normalizeReaderDevicePreferences({ imageDisplayMode: "invalid", controlLayout: "invalid" }), {
    imageDisplayMode: "contain",
    controlLayout: "balanced",
  });
});

test("fill modes request a sharper but bounded display image", async () => {
  const { readerDisplayMaxWidth } = await loadReaderLayoutModule();

  assert.equal(readerDisplayMaxWidth("contain", 1024, 768, 2), 1200);
  assert.equal(readerDisplayMaxWidth("width", 1024, 768, 2), 2000);
  assert.equal(readerDisplayMaxWidth("height", 390, 844, 3), 2000);
  assert.equal(readerDisplayMaxWidth("width", 600, 500, 1), 1200);
});

test("display width replaces an existing page query without losing other parameters", async () => {
  const { pagePathWithMaxWidth } = await loadReaderLayoutModule();

  assert.equal(
    pagePathWithMaxWidth("/api/books/7/pages/3?maxWidth=1200&v=2", 1800),
    "/api/books/7/pages/3?maxWidth=1800&v=2",
  );
  assert.equal(pagePathWithMaxWidth("/api/books/7/pages/3", 4000), "/api/books/7/pages/3?maxWidth=2400");
});

test("reader styles expose width, height, and handed control layouts", async () => {
  const styleSource = await readFile(path.join(srcDir, "styles.css"), "utf8");
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.match(styleSource, /\.pageStage\.single\.fit-width\s+\.pageSpread/s);
  assert.match(styleSource, /\.pageStage\.single\.fit-height\s+\.pageSpread/s);
  assert.match(styleSource, /\.readerThumbControls\.left/s);
  assert.match(styleSource, /\.readerThumbControls\.right/s);
  assert.ok(appSource.includes("readerImageDisplayMode"));
  assert.ok(appSource.includes("readerControlLayout"));
});
