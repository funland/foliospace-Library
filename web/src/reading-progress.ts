import type { Book } from "./api";

type ProgressBook = Pick<Book, "progressFraction"> & Partial<Pick<Book, "format" | "currentPage" | "pageCount" | "privateStatus">>;

export function isContinueReading(book: ProgressBook): boolean {
  if (book.privateStatus === "finished") return false;
  if ((book.progressFraction || 0) < 1) return true;
  // EPUB fractions are chapter-relative, including values saved by older clients.
  return book.format === "epub" && (!(book.pageCount && book.pageCount > 0) || (book.currentPage || 0) < book.pageCount - 1);
}

export function readingProgress(book: ProgressBook): number {
  let fraction = book.progressFraction || 0;
  if (book.format === "epub" && book.pageCount && book.pageCount > 0) {
    fraction = ((book.currentPage || 0) + Math.min(1, fraction)) / book.pageCount;
  }
  return fraction >= 1 ? 100 : Math.max(0, Math.min(99, Math.round(fraction * 100)));
}
