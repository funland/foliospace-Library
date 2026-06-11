import assert from "node:assert/strict";
import { access, readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const srcDir = path.dirname(fileURLToPath(import.meta.url));
const webDir = path.resolve(srcDir, "..");

test("thumbnail fallback uses the legacy bookshelf artwork", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");
  const fallbackLine = appSource.split("\n").find((line) => line.includes("thumbnailFallbackImage")) || "";

  assert.equal(fallbackLine.trim(), 'const thumbnailFallbackImage = "/bookshelf-bg-v2.jpg";');
  assert.ok(!appSource.includes("data:image/svg+xml"), "inline SVG fallback should not be used");
  await access(path.join(webDir, "public", "bookshelf-bg-v2.jpg"));
});

test("continue reading uses source covers for enlarged shelves", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.ok(appSource.includes("const visibleContinueBooks = continueBooks.slice(0, 4);"), "continue reading should base large cover mode on displayed books");
  assert.ok(appSource.includes("largeCovers={visibleContinueBooks.length < 4}"), "continue reading should enable large cover mode only when fewer than four displayed books are shown");
  assert.ok(appSource.includes("largeCover={largeCovers}"), "BookShelf should pass large cover mode to each book cover");
  assert.ok(appSource.includes("sourceCoverUrl={largeCover ? book.coverUrl || `/api/books/${book.id}/cover` : undefined}"), "BookCover should fall back to the legacy source cover URL when older API payloads omit coverUrl");
  assert.ok(appSource.includes('className={`sourceCoverImage${sourceCoverLoaded ? " loaded" : ""}`}'), "large covers should render a real cover image overlay instead of relying on a hidden preload");
});

test("book cover frames keep a stable portrait ratio on narrow shelves", async () => {
  const styleSource = await readFile(path.join(srcDir, "styles.css"), "utf8");

  for (const selector of [".searchCover", ".shelfCover", ".coverFrame"]) {
    const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    assert.match(
      styleSource,
      new RegExp(`${escapedSelector}\\s*\\{[^}]*aspect-ratio:\\s*3\\s*/\\s*4\\.15;`, "s"),
      `${selector} should own the book-cover aspect ratio so tall source images cannot stretch the card`,
    );
    assert.match(
      styleSource,
      new RegExp(`${escapedSelector}\\s*\\{[^}]*width:\\s*100%;`, "s"),
      `${selector} should fill its shelf column instead of using image intrinsic width`,
    );
    assert.match(
      styleSource,
      new RegExp(`${escapedSelector}\\s*\\{[^}]*min-width:\\s*0;`, "s"),
      `${selector} should be allowed to shrink inside grid columns on narrow screens`,
    );
  }

  for (const selector of [".searchCover", ".shelfCover", ".coverFrame"]) {
    const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    assert.match(
      styleSource,
      new RegExp(`${escapedSelector} img,\\s*\\n${escapedSelector} \\.pdfCoverPreview\\s*\\{[^}]*height:\\s*100%;`, "s"),
      `${selector} images should fill the fixed cover frame instead of sizing the frame from their natural ratio`,
    );
  }

  for (const selector of [".shelfBook", ".book"]) {
    const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    assert.match(
      styleSource,
      new RegExp(`${escapedSelector}\\s*\\{[^}]*grid-template-columns:\\s*minmax\\(0,\\s*1fr\\);`, "s"),
      `${selector} should prevent an intrinsic cover width from expanding its internal grid column`,
    );
  }
});

test("collection covers use server-provided thumbnail fields before lazy fallback", async () => {
  const apiSource = await readFile(path.join(srcDir, "api.ts"), "utf8");
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.ok(apiSource.includes("coverBookId?: number;"), "Series should expose the representative cover book id from the API");
  assert.ok(apiSource.includes("thumbnailStatus?: string;"), "Series should expose optional collection thumbnail status");
  assert.ok(apiSource.includes("thumbnailUrl?: string;"), "Series should expose optional collection thumbnail URL");
  assert.ok(appSource.includes("<CollectionCover series={item} />"), "collection cards should pass the full series payload into CollectionCover");
  assert.ok(appSource.includes("className={collectionThumbClass(item)}"), "collection cards should use a thumbnail-aware thumb class");
  assert.ok(appSource.includes("if (series.thumbnailUrl)"), "CollectionCover should use server-provided collection thumbnails before loading a fallback");
  assert.ok(appSource.includes("api.booksPage(series.id"), "CollectionCover should keep the old booksPage fallback for older API payloads");

  const styleSource = await readFile(path.join(srcDir, "styles.css"), "utf8");
  assert.ok(styleSource.includes(".collectionThumb.withCover"), "known collection thumbnails should use a non-bookshelf loading placeholder");
});

test("docker runtime includes PDF thumbnail renderer dependency", async () => {
  const dockerfile = await readFile(path.resolve(webDir, "..", "Dockerfile"), "utf8");

  assert.ok(dockerfile.includes("poppler-utils"), "runtime image should install poppler-utils so pdftoppm can render PDF covers and thumbnails");
});

test("webtoon pages hide image join seams without drawing reading dividers", async () => {
  const styleSource = await readFile(path.join(srcDir, "styles.css"), "utf8");

  assert.match(styleSource, /\.webtoonPage\s*\+\s*\.webtoonPage\s*\{[^}]*margin-top:\s*-1px;/s, "webtoon pages should slightly overlap to hide subpixel seams");
  assert.doesNotMatch(styleSource, /\.webtoonPage[^{]*\{[^}]*border-(top|bottom):/s, "webtoon pages should not draw visible page divider borders");
});

test("webtoon structured progress falls back to the legacy progress API", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.match(
    appSource,
    /api\.saveWebtoonReadingPosition\(selectedBook\.id,\s*webtoonPosition\)[\s\S]*api\s*\.\s*progressDetail\([\s\S]*`webtoon:\$\{webtoonPosition\.documentProgress\}/,
    "webtoon saves should fall back to legacy progress when the structured endpoint is unavailable",
  );
});

test("webtoon restore is not overwritten by passive image-load position updates", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.match(
    appSource,
    /if\s*\(\s*webtoonRestorePosition\s*!==\s*null\s*&&\s*!userInitiated\s*\)\s*return;/,
    "image loads during restore should not reset the saved webtoon anchor back to the current scrollTop",
  );
});

test("webtoon programmatic restore scrolls do not update the current page window", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.ok(appSource.includes("const webtoonUserScrollUntil = useRef(0);"), "webtoon scroll saves should be gated by recent user input");
  assert.match(
    appSource,
    /function\s+handleWebtoonScroll\(\)\s*\{[\s\S]*if\s*\(\s*webtoonRestoring\.current\s*\|\|\s*webtoonRestorePosition\s*!==\s*null\s*\)\s*return;[\s\S]*if\s*\(\s*Date\.now\(\)\s*>\s*webtoonUserScrollUntil\.current\s*\)\s*return;[\s\S]*updateWebtoonPosition\(true\);/s,
    "programmatic restore scroll events should not move the virtual render window away from the saved page",
  );
  assert.match(
    appSource,
    /className="webtoonReader"[\s\S]*onWheel=\{markWebtoonUserScroll\}[\s\S]*onTouchStart=\{markWebtoonUserScroll\}[\s\S]*onPointerDown=\{markWebtoonUserScroll\}/,
    "webtoon reader should mark real user input before accepting scroll progress updates",
  );
  assert.doesNotMatch(
    appSource,
    /requestAnimationFrame\(\(\)\s*=>\s*\{[\s\S]*updateWebtoonPosition\(false\);[\s\S]*setWebtoonRestorePosition\(null\);/,
    "restore completion should not recalculate pageIndex from an estimated scrollTop",
  );
});

test("webtoon restore accepts cached target images without waiting for onLoad state", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.ok(appSource.includes("function isWebtoonRestoreTargetReady"), "webtoon restore should inspect the DOM target image readiness");
  assert.match(
    appSource,
    /if\s*\(\s*!isWebtoonRestoreTargetReady\(node,\s*targetPageIndex\)\s*\)\s*return;/,
    "restore should continue when a cached target image is already complete even if onLoad did not update state",
  );
  assert.doesNotMatch(
    appSource,
    /webtoonPageHeights\[targetPageIndex\]\s*===\s*undefined/,
    "restore should not depend only on the webtoonPageHeights state, because cached images may miss onLoad",
  );
});

test("switching back to webtoon restores the current page before accepting scroll progress", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.ok(appSource.includes("function changeComicReaderPageMode"), "comic page mode changes should go through a shared handler");
  assert.match(
    appSource,
    /function\s+changeComicReaderPageMode\(nextMode:\s*ReaderPageMode\)[\s\S]*if\s*\(\s*nextMode\s*===\s*"webtoon"[\s\S]*setWebtoonRestorePosition\(nextPosition\)[\s\S]*webtoonRestoring\.current\s*=\s*true;/s,
    "entering webtoon mode should restore to the current page anchor before scroll events can update progress",
  );
  assert.match(
    appSource,
    /onClick=\{\(\)\s*=>\s*changeComicReaderPageMode\("webtoon"\)\}/,
    "the webtoon mode button should use the guarded mode switch handler",
  );
  assert.doesNotMatch(
    appSource,
    /onClick=\{\(\)\s*=>\s*\{\s*setReaderPageMode\("webtoon"\);[\s\S]*setReaderLoadState\("ready"\);[\s\S]*\}\s*\}/,
    "the webtoon mode button should not switch modes without preparing a restore anchor",
  );
});

test("switching comic page modes does not immediately overwrite legacy progress", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.ok(appSource.includes("const suppressPagedProgressSave = useRef(false);"), "paged progress saves should be suppressible during mode-only switches");
  assert.match(
    appSource,
    /if\s*\(\s*suppressPagedProgressSave\.current\s*\)\s*\{\s*suppressPagedProgressSave\.current\s*=\s*false;\s*return;\s*\}/s,
    "the next non-webtoon progress save after a mode-only switch should be skipped",
  );
  assert.match(
    appSource,
    /if\s*\(\s*nextMode\s*!==\s*"webtoon"\s*\)\s*\{[\s\S]*suppressPagedProgressSave\.current\s*=\s*true;/s,
    "switching between paged comic modes should not rewrite the saved progress until the user changes pages",
  );
});

test("PDF webtoon mode does not render every page at once", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");
  const start = appSource.indexOf("function pdfRenderablePages");
  const end = appSource.indexOf("function isPDFRenderCancelled", start);
  const renderableSource = start >= 0 && end > start ? appSource.slice(start, end) : "";

  assert.doesNotMatch(
    renderableSource,
    /if\s*\(\s*mode\s*===\s*"webtoon"\s*\)\s*return\s+Array\.from\(\{\s*length:\s*total\s*\}/,
    "PDF webtoon mode should window rendered canvases instead of allocating one canvas per PDF page",
  );
  assert.ok(
    renderableSource.includes("PDF_WEBTOON_RENDER_RADIUS"),
    "PDF webtoon mode should use an explicit render radius like image webtoon mode",
  );
});

test("PDF webtoon rendering caps canvas memory and releases offscreen canvases", async () => {
  const appSource = await readFile(path.join(srcDir, "App.tsx"), "utf8");

  assert.ok(appSource.includes("const PDF_WEBTOON_RENDER_RADIUS = 2;"), "PDF webtoon should render the current page plus two neighbors on each side");
  assert.ok(appSource.includes("const PDF_WEBTOON_MAX_CANVAS_PIXELS = 6_000_000;"), "PDF webtoon canvases should have a hard pixel budget");
  assert.match(
    appSource,
    /const dpr = isWebtoonMode \? 1 : rawDpr;/,
    "PDF webtoon should not multiply huge long-strip canvases by high devicePixelRatio",
  );
  assert.match(
    appSource,
    /Math\.sqrt\(PDF_WEBTOON_MAX_CANVAS_PIXELS \/ Math\.max\(1, baseViewport\.width \* baseViewport\.height\)\)/,
    "PDF webtoon render scale should be capped from page area",
  );
  assert.ok(appSource.includes("? slotWidth / baseViewport.width"), "PDF webtoon pages should keep a normal full-width reading scale");
  assert.match(
    appSource,
    /Object\.entries\(canvasRefs\.current\)[\s\S]*releasePDFCanvas\(canvas\);[\s\S]*delete canvasRefs\.current\[pageNumber\];/,
    "PDF webtoon should actively release canvases that leave the render window",
  );
  assert.match(
    appSource,
    /function releasePDFCanvas\(canvas: HTMLCanvasElement \| null \| undefined\)[\s\S]*canvas\.width = 0;[\s\S]*canvas\.height = 0;/,
    "releasePDFCanvas should drop the canvas backing store",
  );
});
