import type { Library, Series } from "./api";

export function homeLibrarySections(libraries: Pick<Library, "assetType">[], hasVideos: boolean) {
  return {
    video: hasVideos || libraries.some(library => library.assetType === "video"),
    legacy: libraries.some(library => library.assetType !== "video"),
  };
}

// Former mixed scans can leave book records in a now video-only directory.
// Filter presentation only; never delete those records or alter the files.
export function isReadingOrGameCollection(item: Pick<Series, "libraryId" | "primaryType">, libraries: Pick<Library, "id" | "assetType">[]) {
  return item.primaryType !== "video" && !libraries.some(library => library.id === item.libraryId && library.assetType === "video");
}
