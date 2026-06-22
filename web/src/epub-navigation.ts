export type EpubChapterOpenPosition = "start" | "end";

export function resolveEpubOpenPosition(target: EpubChapterOpenPosition, pageCount: number) {
  if (target === "end") {
    return Math.max(0, pageCount - 1);
  }
  return 0;
}
