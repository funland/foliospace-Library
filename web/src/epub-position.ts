export const EPUB_POSITION_SCHEMA = "epub-position-v1";
export const DEFAULT_EPUB_ANCHOR_RATIO = 0.28;
const EPUB_LOCATOR_MARKER = "|epub-v1:";

export type EpubAnchorType = "text" | "media" | "spine";

export type EpubTextAnchor = {
  blockKey: string;
  textOffset: number;
  textHash: string;
  blockOffsetRatio?: number;
  targetFragmentID?: string;
  textSample?: string;
};

export type EpubMediaAnchor = {
  resourceKey: string;
  mediaIndex: number;
  xRatio: number;
  yRatio: number;
};

export type EpubPosition = {
  schema: typeof EPUB_POSITION_SCHEMA;
  anchorType: EpubAnchorType;
  spineIndex: number;
  spineHref: string;
  viewportAnchorRatio: number;
  documentProgress: number;
  legacyPagePosition: number;
  layoutPageCount: number;
  text?: EpubTextAnchor;
  media?: EpubMediaAnchor;
  updatedAt?: string;
};

export type EpubLocator = {
  legacyPagePosition: number;
  position: EpubPosition | null;
};

export type EpubAnchorTypeInput = {
  hasAnchorMedia: boolean;
  hasAnchorText: boolean;
  documentTextLength: number;
  mediaCoverageRatio: number;
};

export function preferEpubNavigationCapture({
  spineHref,
  targetHref,
  userInitiated,
}: {
  spineHref: string;
  targetHref: string | null | undefined;
  userInitiated: boolean;
}): string {
  if (!userInitiated || !targetHref) return "";
  const spinePath = stripEPUBLocatorFragment(spineHref);
  const targetPath = stripEPUBLocatorFragment(targetHref);
  if (!spinePath || spinePath !== targetPath) return "";
  const marker = targetHref.indexOf("#");
  if (marker < 0) return "";
  return decodeEPUBLocatorFragment(targetHref.slice(marker + 1));
}

export function encodeEpubLocator(position: EpubPosition): string {
  const normalized = normalizeEpubPosition(position);
  return `${normalized.legacyPagePosition}${EPUB_LOCATOR_MARKER}${base64URLFromString(JSON.stringify(normalized))}`;
}

export function readEpubLocator(locator: string | null | undefined): EpubLocator {
  const value = typeof locator === "string" ? locator : "";
  const legacyPagePosition = readLegacyPagePosition(value);
  const markerIndex = value.indexOf(EPUB_LOCATOR_MARKER);
  if (markerIndex < 0) {
    return { legacyPagePosition, position: null };
  }
  try {
    const payload = value.slice(markerIndex + EPUB_LOCATOR_MARKER.length);
    const parsed = JSON.parse(stringFromBase64URL(payload)) as Partial<EpubPosition>;
    if (parsed.schema !== EPUB_POSITION_SCHEMA) {
      return { legacyPagePosition, position: null };
    }
    return {
      legacyPagePosition,
      position: normalizeEpubPosition({ ...parsed, legacyPagePosition: parsed.legacyPagePosition ?? legacyPagePosition } as EpubPosition),
    };
  } catch {
    return { legacyPagePosition, position: null };
  }
}

export function chooseEpubAnchorType(input: EpubAnchorTypeInput): EpubAnchorType {
  const mediaCoverage = clampUnit(input.mediaCoverageRatio);
  const textLength = safeNonNegative(input.documentTextLength);
  if (input.hasAnchorMedia && (!input.hasAnchorText || mediaCoverage >= 0.55 || textLength < 280)) {
    return "media";
  }
  if (input.hasAnchorText) return "text";
  if (input.hasAnchorMedia) return "media";
  return "spine";
}

function normalizeEpubPosition(position: EpubPosition): EpubPosition {
  const anchorType = position.anchorType === "text" || position.anchorType === "media" || position.anchorType === "spine"
    ? position.anchorType
    : "spine";
  const normalized: EpubPosition = {
    schema: EPUB_POSITION_SCHEMA,
    anchorType,
    spineIndex: safeInteger(position.spineIndex, 0),
    spineHref: typeof position.spineHref === "string" ? position.spineHref : "",
    viewportAnchorRatio: position.viewportAnchorRatio > 0 ? clampUnit(position.viewportAnchorRatio) : DEFAULT_EPUB_ANCHOR_RATIO,
    documentProgress: clampUnit(position.documentProgress),
    legacyPagePosition: safeInteger(position.legacyPagePosition, 0),
    layoutPageCount: Math.max(1, safeInteger(position.layoutPageCount, 1)),
  };
  if (position.updatedAt) normalized.updatedAt = position.updatedAt;
  if (anchorType === "text" && position.text) {
    normalized.text = {
      blockKey: String(position.text.blockKey ?? ""),
      textOffset: safeInteger(position.text.textOffset, 0),
      textHash: String(position.text.textHash ?? ""),
    };
    if (typeof position.text.blockOffsetRatio === "number") {
      normalized.text.blockOffsetRatio = clampUnit(position.text.blockOffsetRatio);
    }
    if (position.text.targetFragmentID) {
      normalized.text.targetFragmentID = String(position.text.targetFragmentID);
    }
    if (position.text.textSample) {
      normalized.text.textSample = String(position.text.textSample).slice(0, 160);
    }
  }
  if (anchorType === "media" && position.media) {
    normalized.media = {
      resourceKey: String(position.media.resourceKey ?? ""),
      mediaIndex: safeInteger(position.media.mediaIndex, 0),
      xRatio: clampUnit(position.media.xRatio),
      yRatio: clampUnit(position.media.yRatio),
    };
  }
  return normalized;
}

function readLegacyPagePosition(locator: string): number {
  const value = Number.parseInt(locator, 10);
  return Number.isFinite(value) && value > 0 ? value : 0;
}

function stripEPUBLocatorFragment(value: string): string {
  return value.split("#", 1)[0];
}

function decodeEPUBLocatorFragment(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

function base64URLFromString(value: string): string {
  const bytes = new TextEncoder().encode(value);
  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

function stringFromBase64URL(value: string): string {
  const base64 = value.replace(/-/g, "+").replace(/_/g, "/");
  const padded = base64.padEnd(base64.length + ((4 - (base64.length % 4)) % 4), "=");
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return new TextDecoder().decode(bytes);
}

function safeInteger(value: number | undefined, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) ? Math.max(0, Math.floor(value)) : fallback;
}

function safeNonNegative(value: number | undefined): number {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : 0;
}

function clampUnit(value: number | undefined): number {
  if (typeof value !== "number" || !Number.isFinite(value) || value < 0) return 0;
  if (value > 1) return 1;
  return value;
}
