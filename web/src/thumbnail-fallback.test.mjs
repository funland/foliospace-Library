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
