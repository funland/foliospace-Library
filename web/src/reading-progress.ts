import type { Book } from "./api";

type ProgressBook = Pick<Book, "progressFraction"> & Partial<Pick<Book, "format" | "currentPage" | "pageCount" | "privateStatus">> & {
  progressLocator?: string;
};

function hasChapterFraction(book: ProgressBook): boolean {
  // Only the web reader's integer locator denotes a chapter-relative fraction.
  // Native path locators, and older list APIs without a locator, use book progress.
  return book.format === "epub" && /^\d+$/.test(book.progressLocator ?? "");
}

export function isContinueReading(book: ProgressBook): boolean {
  if (book.privateStatus === "finished") return false;
  if ((book.progressFraction || 0) < 1) return true;
  return hasChapterFraction(book) && (!(book.pageCount && book.pageCount > 0) || (book.currentPage || 0) < book.pageCount - 1);
}

export function readingProgress(book: ProgressBook): number {
  let fraction = book.progressFraction || 0;
  if (hasChapterFraction(book) && book.pageCount && book.pageCount > 0) {
    fraction = ((book.currentPage || 0) + Math.min(1, fraction)) / book.pageCount;
  }
  return fraction >= 1 ? 100 : Math.max(0, Math.min(99, Math.round(fraction * 100)));
}

type ProgressForRestore = { locator: string; updatedAt?: string };
type WebtoonForRestore = { updatedAt?: string };

function progressTimestamp(value?: string): number | null {
  if (!value) return null;
  const trimmed = value.trim();
  // SQLite CURRENT_TIMESTAMP is UTC; never let the browser interpret it locally.
  const normalized = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?$/.test(trimmed)
    ? `${trimmed.replace(" ", "T")}Z`
    : trimmed;
  if (!/(?:Z|[+-]\d{2}:\d{2})$/i.test(normalized)) return null;
  const timestamp = Date.parse(normalized);
  return Number.isFinite(timestamp) ? timestamp : null;
}

export function shouldRestoreWebtoonPosition(
  progress: ProgressForRestore,
  position: WebtoonForRestore | null | undefined,
  readerPageMode: string,
): boolean {
  if (readerPageMode !== "webtoon" || !position) return false;
  const generalTime = progressTimestamp(progress.updatedAt);
  const webtoonTime = progressTimestamp(position.updatedAt);
  if (generalTime !== null && webtoonTime !== null) return webtoonTime >= generalTime;
  // A missing timestamp cannot prove an old mode-specific record is current.
  // Legacy webtoon locators explicitly identify the same reading mode.
  return progress.locator.trim().startsWith("webtoon:");
}
