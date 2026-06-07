import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import ts from "typescript";

const srcDir = path.dirname(fileURLToPath(import.meta.url));

async function loadEpubLayoutModule() {
  const source = await readFile(path.join(srcDir, "epub-layout.ts"), "utf8");
  const transpiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.ES2020,
      target: ts.ScriptTarget.ES2020,
    },
  }).outputText;
  return import(`data:text/javascript;base64,${Buffer.from(transpiled).toString("base64")}`);
}

test("epub toc fragment targets resolve to an in-chapter page position", async () => {
  const { epubFragmentID, epubPositionForAnchorOffset } = await loadEpubLayoutModule();

  assert.equal(epubFragmentID("OEBPS/ch004.xhtml#v01020001", "OEBPS/ch004.xhtml"), "v01020001");
  assert.equal(epubPositionForAnchorOffset(60688, 978, 197), 62);
});

test("epub toc fragment ignores targets from other spine items", async () => {
  const { epubFragmentID } = await loadEpubLayoutModule();

  assert.equal(epubFragmentID("OEBPS/ch005.xhtml#v02001001", "OEBPS/ch004.xhtml"), "");
});

test("epub reader relayouts when the frame size changes", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.match(
    appSource,
    /new ResizeObserver\(\(\) => applyEpubLayout\(\)\)/,
    "EPUB iframe should relayout when its rendered size changes",
  );
  assert.match(
    appSource,
    /if \(!doc\.body \|\| !doc\.documentElement\) return;/,
    "EPUB relayout should guard iframe body availability inside async layout callbacks",
  );
  assert.doesNotMatch(
    appSource,
    /instanceof HTMLElement/,
    "EPUB anchor detection should not use parent-window HTMLElement checks for iframe elements",
  );
});

test("epub reader uses cached layout metrics for ordinary page turns", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.match(
    appSource,
    /function applyEpubPagePosition\(position: number\)/,
    "EPUB page turns should have a fast transform-only path",
  );
  assert.match(
    appSource,
    /epubFastPageTurnRef/,
    "EPUB reader controls should be able to move within a chapter before the React state update",
  );
  assert.match(
    appSource,
    /scheduleEpubPagePositionState/,
    "EPUB page position state should be synchronized after the immediate visual page turn",
  );
  assert.match(
    appSource,
    /useLayoutEffect\(\(\) => \{\s*pagePositionRef\.current = pagePosition;\s*applyEpubPagePosition\(pagePosition\);[\s\S]*?\}, \[pagePosition, spineItem\?\.href\]\);/,
    "pagePosition updates should use a pre-paint fast path instead of forcing full chapter layout",
  );
  assert.match(
    appSource,
    /doc\.body\.style\.setProperty\("transform", `translateX\(-\$\{nextPosition \* metrics\.pageWidth\}px\)`, "important"\);/,
    "the fast page-turn transform should override the iframe stylesheet transform",
  );
  assert.match(
    appSource,
    /transition:\s+none !important;/,
    "EPUB page turns should avoid animating huge column layouts",
  );
  assert.doesNotMatch(
    appSource,
    /useEffect\(\(\) => \{\s*applyEpubLayout\(\);[\s\S]*?\}, \[[^\]]*pagePosition[^\]]*\]\);/,
    "full EPUB layout should not depend on pagePosition",
  );
});

test("epub iframe body forwards keyboard and wheel page turns", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.match(
    appSource,
    /const EPUB_WHEEL_PAGE_TURN_INTERVAL_MS = \d+;/,
    "EPUB wheel page turns should be throttled",
  );
  assert.match(
    appSource,
    /doc\.addEventListener\("keydown", onFrameKeyDown\);/,
    "EPUB iframe documents should listen for left and right arrow keys",
  );
  assert.match(
    appSource,
    /doc\.addEventListener\("wheel", onFrameWheel, \{ passive: false \}\);/,
    "EPUB iframe wheel events must be cancelable so one wheel gesture does not scroll the embedded document",
  );
  assert.match(
    appSource,
    /if \(event\.key === "ArrowLeft"\)[\s\S]*onPageTurnRef\.current\(-1\);[\s\S]*if \(event\.key === "ArrowRight"\)[\s\S]*onPageTurnRef\.current\(1\);/,
    "EPUB iframe arrow keys should use the same page-turn path as the outer reader",
  );
  assert.match(
    appSource,
    /if \(now - lastWheelPageTurnAtRef\.current < EPUB_WHEEL_PAGE_TURN_INTERVAL_MS\) return;/,
    "EPUB iframe wheel handling should ignore repeated wheel events within the throttle window",
  );
});
