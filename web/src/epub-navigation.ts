export type EpubChapterOpenPosition = "start" | "end";

export function resolveEpubOpenPosition(target: EpubChapterOpenPosition, pageCount: number) {
  if (target === "end") {
    return Math.max(0, pageCount - 1);
  }
  return 0;
}

type EpubNavigationManifest = {
  spine: { index: number; href: string }[];
  toc: { label: string; href: string; index: number }[];
};

export type EpubNavigationRequest = {
  id: number;
  pageIndex: number;
  fragment?: string;
  pagePosition?: number;
};

function decodeEpubPath(value: string) {
  return value.split("/").map((part) => {
    try { return decodeURIComponent(part); } catch { return part; }
  }).join("/");
}

function epubDocumentHref(value: string) {
  return decodeEpubPath(value.split(/[?#]/, 1)[0]).replace(/^\/+/, "");
}

export function epubFragment(value: string) {
  const hash = value.indexOf("#");
  if (hash < 0) return "";
  const fragment = value.slice(hash + 1);
  try { return decodeURIComponent(fragment); } catch { return fragment; }
}

// Resource URLs may contain an authentication query before the fragment. Keep
// only the archive path and fragment; credentials never enter reader state.
export function normalizeEPUBLink(currentHref: string, href: string, origin = "https://foliospace.local"): string | null {
  try {
    const base = new URL(`/${currentHref.replace(/^\/+/, "")}`, origin);
    const url = new URL(href, base);
    if (url.origin !== base.origin || !["https:", "http:"].includes(url.protocol)) return null;
    const marker = "/epub/resources/";
    const markerIndex = url.pathname.indexOf(marker);
    const path = markerIndex >= 0 ? url.pathname.slice(markerIndex + marker.length) : url.pathname.replace(/^\/+/, "");
    return decodeEpubPath(path) + url.hash;
  } catch {
    return null;
  }
}

export function resolveEpubHrefIndex(manifest: EpubNavigationManifest, href: string, fallbackIndex?: number): number | null {
  const documentHref = epubDocumentHref(href);
  const exact = manifest.spine.find((item) => epubDocumentHref(item.href) === documentHref);
  if (exact) return exact.index;
  if (fallbackIndex !== undefined && manifest.spine.some((item) => item.index === fallbackIndex)) return fallbackIndex;
  return null;
}

export function epubChapterLabel(manifest: EpubNavigationManifest | null, pageIndex: number) {
  if (!manifest) return "";
  const entries = manifest.toc.map((item) => ({ ...item, spineIndex: resolveEpubHrefIndex(manifest, item.href) }))
    .filter((item) => item.spineIndex !== null && item.label.trim());
  const exact = entries.filter((item) => item.spineIndex === pageIndex);
  const selected = exact.find((item) => !epubFragment(item.href)) ?? exact[0]
    ?? entries.filter((item) => item.spineIndex! < pageIndex).sort((a, b) => b.spineIndex! - a.spineIndex!)[0];
  return selected?.label.trim() ?? "";
}

// ID lookup avoids treating EPUB-controlled fragments as CSS selectors.
export function findEpubFragment(doc: Document, fragment: string): Element | null {
  return doc.getElementById(fragment) ?? Array.from(doc.getElementsByName(fragment)).find((item) => item.tagName.toLowerCase() === "a") ?? null;
}

export function isEpubInlineGlyph(width: number, height: number, hasTextContext: boolean) {
  return hasTextContext && width > 0 && height > 0 && width <= 40 && height <= 40 && width / height >= 0.5 && width / height <= 2;
}
