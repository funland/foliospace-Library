import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import ts from "typescript";

const srcDir = path.dirname(fileURLToPath(import.meta.url));

async function loadReaderFitModule() {
  const source = await readFile(path.join(srcDir, "reader-fit.ts"), "utf8");
  const transpiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.ES2020,
      target: ts.ScriptTarget.ES2020,
    },
  }).outputText;
  return import(`data:text/javascript;base64,${Buffer.from(transpiled).toString("base64")}`);
}

test("fullscreen webtoon cap stays source-aware for high resolution art", async () => {
  const { fullscreenImageFit } = await loadReaderFitModule();

  const fit = fullscreenImageFit({
    naturalWidth: 2400,
    naturalHeight: 3600,
    devicePixelRatio: 2,
    mode: "webtoon",
  });

  assert.equal(fit.maxCssWidth, 1620);
  assert.equal(fit.maxCssHeight, 2430);
});

test("fullscreen image fit keeps lower resolution art from being stretched across wide screens", async () => {
  const { fullscreenImageFit } = await loadReaderFitModule();

  const fit = fullscreenImageFit({
    naturalWidth: 800,
    naturalHeight: 1200,
    devicePixelRatio: 2,
    mode: "webtoon",
  });

  assert.equal(fit.maxCssWidth, 540);
  assert.equal(fit.maxCssHeight, 810);
});

test("fullscreen paged images contain within viewport slots and only webtoon keeps the source cap", async () => {
  const styleSource = await readFile(path.join(srcDir, "styles.css"), "utf8");
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.match(styleSource, /\.pageSpread img\s*\{[^}]*object-fit:\s*contain/);
  assert.match(
    styleSource,
    /\.reader\.immersiveMode \.pageStage\.double \.pageSpread\s*\{[^}]*grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\)/,
  );
  assert.match(
    styleSource,
    /\.reader\.immersiveMode \.pageStage\.single\.fit-contain \.pageSpread img,\s*\.reader\.immersiveMode \.pageStage\.double \.pageSpread img\s*\{[^}]*width:\s*100%;[^}]*height:\s*100%;[^}]*max-width:\s*none;[^}]*max-height:\s*none/,
  );
  assert.match(styleSource, /\.reader\.immersiveMode \.webtoonPage img\s*\{[^}]*var\(--reader-fit-width/);
  assert.match(appSource, /function webtoonImageFitStyle\(page: Page\)[\s\S]*?fullscreenImageFit\(\{[\s\S]*?mode: "webtoon"/);
  assert.equal(appSource.match(/webtoonImageFitStyle\(/g)?.length, 2, "only the webtoon image wrapper uses the source cap");
});

test("fullscreen webtoon reader uses a subtle scrollbar", async () => {
  const styleSource = await readFile(path.join(srcDir, "styles.css"), "utf8");

  assert.match(
    styleSource,
    /\.reader\.immersiveMode\s+\.webtoonReader\s*\{[\s\S]*scrollbar-width:\s*thin/s,
    "fullscreen webtoon should use a thin native scrollbar",
  );
  assert.match(
    styleSource,
    /\.reader\.immersiveMode\s+\.webtoonReader::\-webkit\-scrollbar-thumb\s*\{[\s\S]*background-clip:\s*padding-box/s,
    "fullscreen webtoon should render a padded translucent scrollbar thumb",
  );
});
