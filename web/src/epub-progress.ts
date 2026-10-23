type SavedEpubProgress = {
  pageIndex: number;
  locator: string;
  progressFraction: number;
};

type ProgressManifest = { spine: { index: number; href: string }[] };

export type RestoredEpubProgress = {
  chapterIndex: number;
  pagePosition: number;
  fragment?: string;
  progressFraction: number;
  progressScope: "book" | "chapter";
  locatorKind: "page" | "path" | "unknown";
  matched: boolean;
};

function decodePart(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

function normalizePath(value: string): string {
  const raw = decodePart(value.split("#", 1)[0].split("?", 1)[0]).replace(/\\/g, "/");
  if (/^[a-z][a-z0-9+.-]*:/i.test(raw)) return "";
  const segments: string[] = [];
  for (const segment of raw.split("/")) {
    if (!segment || segment === ".") continue;
    if (segment === "..") segments.pop();
    else segments.push(segment);
  }
  return segments.join("/");
}

export function resolveEpubProgress(progress: SavedEpubProgress, manifest: ProgressManifest): RestoredEpubProgress {
  const maxChapter = Math.max(0, manifest.spine.length - 1);
  const fallbackChapter = Number.isFinite(progress.pageIndex)
    ? Math.max(0, Math.min(Math.trunc(progress.pageIndex), maxChapter))
    : 0;
  const locator = progress.locator.trim();
  const pagePosition = /^\d+$/.test(progress.locator) ? Number(progress.locator) : NaN;
  if (Number.isSafeInteger(pagePosition)) {
    return {
      chapterIndex: fallbackChapter,
      pagePosition,
      progressFraction: progress.progressFraction,
      progressScope: "chapter",
      locatorKind: "page",
      matched: manifest.spine.length > 0,
    };
  }

  const path = normalizePath(locator);
  const paths = manifest.spine.map((item) => normalizePath(item.href));
  let chapterIndex = path ? paths.indexOf(path) : -1;
  if (chapterIndex < 0 && path) {
    // Some native clients omit the package directory. Match only an unambiguous
    // suffix, so identical chapter filenames in different volumes stay distinct.
    const candidates = paths.flatMap((candidate, index) =>
      candidate && (candidate.endsWith(`/${path}`) || path.endsWith(`/${candidate}`)) ? [index] : [],
    );
    if (candidates.length === 1) chapterIndex = candidates[0];
  }
  const hash = locator.indexOf("#");
  const fragment = hash >= 0 ? decodePart(locator.slice(hash + 1)) : "";
  return {
    chapterIndex: chapterIndex >= 0 ? chapterIndex : fallbackChapter,
    pagePosition: 0,
    ...(chapterIndex >= 0 && fragment ? { fragment } : {}),
    // A native client's whole-book fraction is not a chapter page fraction. Its
    // pagination/weighting is unknown, so preserve it without inventing an offset.
    progressFraction: progress.progressFraction,
    progressScope: "book",
    locatorKind: chapterIndex >= 0 ? "path" : "unknown",
    matched: chapterIndex >= 0,
  };
}
