export type ReaderImageDisplayMode = "contain" | "width" | "height";
export type ReaderControlLayout = "balanced" | "left" | "right";

export type ReaderDevicePreferences = {
  imageDisplayMode: ReaderImageDisplayMode;
  controlLayout: ReaderControlLayout;
};

export function defaultReaderDevicePreferences(): ReaderDevicePreferences {
  return {
    imageDisplayMode: "contain",
    controlLayout: "balanced",
  };
}

export function normalizeReaderDevicePreferences(value: Partial<ReaderDevicePreferences>): ReaderDevicePreferences {
  const defaults = defaultReaderDevicePreferences();
  return {
    imageDisplayMode:
      value.imageDisplayMode === "width" || value.imageDisplayMode === "height"
        ? value.imageDisplayMode
        : defaults.imageDisplayMode,
    controlLayout:
      value.controlLayout === "left" || value.controlLayout === "right"
        ? value.controlLayout
        : defaults.controlLayout,
  };
}

export function readerDisplayMaxWidth(
  mode: ReaderImageDisplayMode,
  viewportWidth: number,
  viewportHeight: number,
  devicePixelRatio: number,
): number {
  if (mode === "contain") return 1200;
  const width = safePositive(viewportWidth, 1200);
  const height = safePositive(viewportHeight, width);
  const dpr = Math.min(3, safePositive(devicePixelRatio, 1));
  const target = Math.ceil(Math.max(width, height) * dpr);
  return Math.max(1200, Math.min(2000, target));
}

export function pagePathWithMaxWidth(path: string, maxWidth: number): string {
  const hashIndex = path.indexOf("#");
  const hash = hashIndex >= 0 ? path.slice(hashIndex) : "";
  const withoutHash = hashIndex >= 0 ? path.slice(0, hashIndex) : path;
  const queryIndex = withoutHash.indexOf("?");
  const base = queryIndex >= 0 ? withoutHash.slice(0, queryIndex) : withoutHash;
  const query = queryIndex >= 0 ? withoutHash.slice(queryIndex + 1) : "";
  const params = new URLSearchParams(query);
  params.set("maxWidth", String(Math.max(320, Math.min(2400, Math.round(maxWidth)))));
  return `${base}?${params.toString()}${hash}`;
}

function safePositive(value: number, fallback: number): number {
  return Number.isFinite(value) && value > 0 ? value : fallback;
}
