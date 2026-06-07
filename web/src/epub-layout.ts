export function epubFragmentID(targetHref: string | null | undefined, currentHref: string | null | undefined): string {
  if (!targetHref || !currentHref) return "";
  const targetPath = stripEPUBFragment(targetHref);
  const currentPath = stripEPUBFragment(currentHref);
  if (targetPath !== currentPath) return "";
  const marker = targetHref.indexOf("#");
  if (marker < 0) return "";
  return decodeEPUBFragment(targetHref.slice(marker + 1));
}

export function epubPositionForAnchorOffset(offsetLeft: number, pageWidth: number, pageCount: number): number {
  if (!Number.isFinite(offsetLeft) || !Number.isFinite(pageWidth) || !Number.isFinite(pageCount)) return 0;
  if (offsetLeft <= 0 || pageWidth <= 0 || pageCount <= 1) return 0;
  return Math.max(0, Math.min(Math.floor(offsetLeft / pageWidth), Math.max(0, Math.floor(pageCount) - 1)));
}

function stripEPUBFragment(value: string): string {
  return value.split("#", 1)[0];
}

function decodeEPUBFragment(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}
