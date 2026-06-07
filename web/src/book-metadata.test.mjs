import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import ts from "typescript";

const srcDir = path.dirname(fileURLToPath(import.meta.url));

async function loadBookMetadataModule() {
  const source = await readFile(path.join(srcDir, "book-metadata.ts"), "utf8");
  const transpiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.ES2020,
      target: ts.ScriptTarget.ES2020,
    },
  }).outputText;
  return import(`data:text/javascript;base64,${Buffer.from(transpiled).toString("base64")}`);
}

test("book metadata description strips EPUB HTML markup for card display", async () => {
  const { displayMetadataText } = await loadBookMetadataModule();

  const raw = `<p><b>Winner of the Hugo and Nebula Awards</b></p><p><br>In order to develop a secure defense&#8212;young Ender is drafted.</p>`;

  assert.equal(
    displayMetadataText(raw),
    "Winner of the Hugo and Nebula Awards In order to develop a secure defense\u2014young Ender is drafted.",
  );
});

test("book metadata description preserves plain text without synthetic spacing", async () => {
  const { displayMetadataText } = await loadBookMetadataModule();

  assert.equal(displayMetadataText("Metadata description."), "Metadata description.");
  assert.equal(displayMetadataText(""), "");
});

test("book descriptions are normalized before rendering in the reader UI", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.match(
    appSource,
    /import \{ displayMetadataText \} from "\.\/book-metadata";/,
    "App should use the shared metadata text display helper",
  );
  assert.match(
    appSource,
    /<BookMetadataDescription className="bookDescription" value=\{book\.description\} \/>/,
    "collection volume cards should not render raw EPUB HTML descriptions",
  );
  assert.match(
    appSource,
    /<BookMetadataDescription element="p" value=\{selectedBook\.description\} \/>/,
    "reader details should not render raw EPUB HTML descriptions",
  );
});
