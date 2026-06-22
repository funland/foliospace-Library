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
