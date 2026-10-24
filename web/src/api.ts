export type Library = {
  id: number;
  name: string;
  rootPath: string;
  assetType: "mixed" | "book" | "comic" | "game" | "video";
  excludePatterns: string[];
};

export type DirectoryEntry = {
  name: string;
  path: string;
};

export type DirectoryListing = {
  path: string;
  parent?: string;
  entries: DirectoryEntry[];
};

export type Profile = {
  id: number;
  name: string;
  avatar: string;
  color: string;
  isDefault: boolean;
  createdAt: string;
  updatedAt: string;
};

export type Series = {
  displayTitle?: string;
  id: number;
  libraryId: number;
  title: string;
  directoryPath: string;
  collectionType: "directory" | "game_platform";
  primaryType: "book" | "comic" | "game" | "video";
  bookCount: number;
  coverBookId?: number;
  addedAt: string;
  thumbnailStatus?: string;
  thumbnailUrl?: string;
  favorite: boolean;
  liked: boolean;
};

export type Book = {
  fileErrorCode?: string;
  fileErrorMessage?: string;
  id: number;
  seriesId: number;
  collectionTitle?: string;
  collectionDisplayTitle?: string;
  title: string;
  creator?: string;
  displayCreator?: string;
  description?: string;
  bookType: "single_volume";
  format: string;
  pageCount: number;
  coverStatus: string;
  coverUrl?: string;
  thumbnailStatus: string;
  thumbnailUrl: string;
  analyzed: boolean;
  filePath?: string;
  contentHash?: string | null;
  contentHashAlgorithm?: string | null;
  fileSize?: number | null;
  contentRevision?: string | null;
  addedAt: string;
  updatedAt: string;
  currentPage: number;
  progressFraction: number;
  progressLocator?: string;
  lastReadAt: string;
  privateStatus: string;
  favorite: boolean;
  rating: number;
  tags: string[];
  summary: string;
};

export type BookPrivateState = {
  status: string;
  favorite: boolean;
  rating: number;
  tags: string[];
  summary: string;
};

export type CollectionPrivateState = {
  favorite: boolean;
  liked: boolean;
};

export type ManualCollection = {
  id: number;
  name: string;
  description?: string;
  itemCount: number;
  createdAt: string;
  updatedAt: string;
};

export type ManualCollectionItem = {
  collectionId?: number;
  assetType: "book" | "game" | "video";
  assetId: number;
  title?: string;
  subtitle?: string;
  coverUrl?: string;
  manifestUrl?: string;
  createdAt?: string;
};

export type ManualCollectionDetails = {
  collection: ManualCollection;
  items: ManualCollectionItem[];
};

export type GamePrivateState = {
  favorite: boolean;
  liked: boolean;
};

export type GameAsset = {
  discGroupInfo?: { groupKey: string; title: string; discNumber: number };
  id: number;
  assetType?: "game";
  title: string;
  platform: string;
  romSetName?: string;
  region?: string;
  format: string;
  size: number;
  crc32: string;
  sha1: string;
  emulatorHint: string;
  inputProfile?: string;
  compatibility: string;
  catalogRole?: string;
  coverUrl?: string;
  manifestUrl?: string;
  favorite: boolean;
  liked: boolean;
};

export type VideoAsset = {
  id: number;
  assetType?: "video";
  title: string;
  displayTitle?: string;
  format: string;
  size: number;
  durationSeconds: number;
  width: number;
  height: number;
  videoCodec?: string;
  audioCodec?: string;
  thumbnailStatus: string;
  thumbnailUrl: string;
  manifestUrl: string;
  directPlayable: boolean;
  playbackMode: "direct" | "hls";
  playbackReason?: string;
  fileUrl?: string;
  hlsUrl?: string;
  transcodeStatusUrl?: string;
};

export type VideoTranscodeStatus = {
  videoId: number;
  status: "idle" | "starting" | "running" | "queued" | "ready" | "failed";
  message?: string;
  segmentCount: number;
};

export type VideoTranscodeQueueStatus = {
  status: "idle" | "running";
  activeVideoId?: number;
  activeTitle?: string;
  segmentCount: number;
  message?: string;
};

export type ThumbnailJob = {
  id: number;
  bookId: number;
  bookTitle?: string;
  size: string;
  status: string;
  priority: number;
  cacheKey: string;
  cachePath?: string;
  contentType?: string;
  width?: number;
  height?: number;
  byteSize?: number;
  errorMessage?: string;
  createdAt: string;
  updatedAt: string;
  startedAt?: string;
  finishedAt?: string;
};

export type ThumbnailCacheStatus = {
  files: number;
  bytes: number;
  readyFiles: number;
  readyBytes: number;
  missingFiles: number;
  staleFiles: number;
  staleBytes: number;
  orphanFiles: number;
  orphanBytes: number;
  algorithmVersion: string;
  smallWidth: number;
  mediumWidth: number;
  targetAspect: number;
};

export type ThumbnailWorkerStatus = {
  status: "idle" | "queued" | "running" | "paused" | "stopped";
  queued: number;
  running: number;
  ready: number;
  failed: number;
  cancelled: number;
  processed: number;
  activeJob?: ThumbnailJob;
  lastError?: string;
  paused: boolean;
  workerEnabled: boolean;
  cache: ThumbnailCacheStatus;
};

export type SearchResponse = {
  query: string;
  books: Book[];
};

export type ClientPreferences = {
  locale: "zh" | "zht" | "en" | "ja" | "ko";
  readerPageMode: "single" | "double" | "webtoon";
  epubPageMode: "single" | "double";
  epubTheme: "light" | "sepia" | "dark";
  epubFontSize: number;
};

export type ClientHome = {
  continueReading: Book[];
  recentBooks: Book[];
  favoriteBooks: Book[];
  wantToRead: Book[];
  gameShelf: GameAsset[];
  videoShelf: VideoAsset[];
  collections: Series[];
};

export type BookListPage = {
  items: Book[];
  total: number;
  limit: number;
  offset: number;
  hasMore: boolean;
};

export type CollectionListPage = {
  items: Series[];
  total: number;
  limit: number;
  offset: number;
  hasMore: boolean;
};

export type GameListPage = {
  items: GameAsset[];
  total: number;
  limit: number;
  offset: number;
  hasMore: boolean;
};

export type GamePlatformFacet = {
  platform: string;
  title: string;
  romSetName?: string;
  format?: string;
  emulatorHint?: string;
  count: number;
};

export type GameListFacets = {
  total: number;
  platforms: GamePlatformFacet[];
};

export type GameCatalogSettings = {
  autoAnalyzeAfterScan: boolean;
  enableLibretroCovers: boolean;
  fbneoDatPath: string;
  mameListXmlPath: string;
  fbneoTargetsPath: string;
  mameTargetsPath: string;
  launchTargetsPath: string;
  mamePlatforms: string;
  metadataProvider: "local" | "hasheous" | "disabled";
};

export type GameCatalogTaskStatus = {
  id?: string;
  action?: string;
  scope?: "game" | "platform" | "all";
  gameId?: number;
  platform?: string;
  force?: boolean;
  status?: "running" | "completed" | "failed" | "interrupted";
  message?: string;
  processed: number;
  total: number;
  matched: number;
  skipped?: number;
  failed: number;
  errors?: string[];
  startedAt?: string;
  endedAt?: string;
  details?: Record<string, unknown>;
};

export type GameCatalogPolicyStatus = {
  id: string;
  path: string;
  available: boolean;
  message?: string;
};

export type GameCurationSummary = {
  total: number;
  ready: number;
  needsCuration: number;
  dependencies: number;
  metadataReady: number;
  artworkReady: number;
  fileCount: number;
  checksummed: number;
  checksumPending: number;
  policies: GameCatalogPolicyStatus[];
  lastTask: GameCatalogTaskStatus;
};

export type GameCurationItem = {
  game: GameAsset;
  metadataStatus: string;
  artworkStatus: string;
  readyProfiles: number;
  fileCount: number;
  checksummed: number;
  mobileReady: boolean;
  issueCode?: string;
  issueMessage?: string;
};

export type GameCurationPage = {
  items: GameCurationItem[];
  total: number;
  limit: number;
  offset: number;
  hasMore: boolean;
};

export type GameMetadata = {
  gameId: number;
  displayTitle: string;
  summary: string;
  releaseDate: string;
  genres: string[];
  developers: string[];
  publishers: string[];
  players: string;
  rating: number;
  externalLinks: string[];
};

export type GameDetails = {
  game: GameAsset;
  metadataStatus: string;
  metadata: GameMetadata;
  sources: Array<{ source: string; sourceId: string; matchedBy: string; confidence: number }>;
  artwork: Array<{ source: string; kind: string; selected: boolean; url?: string; cachePath?: string }>;
};

export type GameMetadataActionResult = {
  gameId: number;
  action: string;
  status: string;
  message: string;
  metadataStatus: string;
  sources: Array<{ source: string; sourceId: string; matchedBy: string; confidence: number }>;
};

export type VideoListPage = {
  items: VideoAsset[];
  total: number;
  limit: number;
  offset: number;
  hasMore: boolean;
};

export type CollectionAssets = {
  books: Book[];
  games: GameAsset[];
  videos: VideoAsset[];
};

export type BookListOptions = {
  format?: string;
  limit?: number;
  offset?: number;
  q?: string;
  sort?: string;
  direction?: string;
};

export type CollectionListOptions = BookListOptions & {
  primaryType?: string;
  libraryId?: number;
};

export type GameListOptions = BookListOptions & {
  platform?: string;
  romSetName?: string;
  format?: string;
};

export type VideoListOptions = BookListOptions & {
  format?: string;
};

export type Page = {
  index: number;
  name: string;
  pageKey?: string;
  url?: string;
  displayUrl?: string;
};

export type EpubManifest = {
  title: string;
  creator: string;
  description: string;
  coverHref: string;
  spine: EpubSpineItem[];
  toc: EpubTocItem[];
};

export type EpubSpineItem = {
  index: number;
  id: string;
  href: string;
  mediaType: string;
};

export type EpubTocItem = {
  label: string;
  href: string;
  index: number;
};

export type ReadProgress = {
  bookId: number;
  pageIndex: number;
  locator: string;
  progressFraction: number;
  updatedAt?: string;
};

export type WebtoonReadingPosition = {
  schema: "webtoon-position-v1";
  pageIndex: number;
  pageKey: string;
  pageYOffsetRatio: number;
  viewportAnchorRatio: number;
  documentProgress: number;
  pageCount: number;
  contentSignature?: string;
  updatedAt?: string;
};

export type ReadingPositions = {
  bookId: number;
  positions: {
    webtoon?: WebtoonReadingPosition;
  };
};

export type ScanJob = {
  id: number;
  libraryId: number;
  status: string;
  targetPath: string;
  currentPath: string;
  discoveredFiles: number;
  indexedFiles: number;
  skippedFiles: number;
  errorCount: number;
  metadataUpdatedFiles: number;
  reclassifiedFiles: number;
  startedAt: string;
  finishedAt?: string;
};

export type FileError = {
  resolved?: boolean;
  bookId?: number;
  id: number;
  path: string;
  code: string;
  message: string;
  lastSeen: string;
};

export type JobEvent = {
  id: number;
  jobId: number;
  level: string;
  message: string;
  createdAt: string;
};

export type AuthStatus = {
  enabled: boolean;
};

export type SetupStatus = {
  initialized: boolean;
  authEnabled: boolean;
  hasLibraries: boolean;
  tokenConfigured: boolean;
  directoryRoots: DirectoryEntry[];
  scanWorkers: number;
  gameCatalog: GameCatalogSettings;
};

export type ClientInfo = {
  serviceName: string;
  serviceVersion: string;
  apiVersion: string;
  supportedFormats: string[];
  capabilities: Record<string, boolean>;
};

export type SetupInput = {
  token: string;
  name: string;
  rootPath: string;
  assetType: Library["assetType"];
  excludePatterns?: string[];
  scanWorkers?: number;
  gameCatalog?: GameCatalogSettings;
};

export type ScanSettings = {
  scanWorkers: number;
};

export type ScanOptions = {
  path?: string;
  mode?: "recent";
  recentLimit?: number;
};

const authTokenKey = "foliospace_api_token";
const activeProfileKey = "foliospace_active_profile_id";

export function getAuthToken() {
  try {
    return window.localStorage.getItem(authTokenKey) ?? "";
  } catch {
    return "";
  }
}

export function setAuthToken(token: string) {
  try {
    window.localStorage.setItem(authTokenKey, token);
  } catch {
    // Ignore storage failures in restricted browser contexts.
  }
}

export function clearAuthToken() {
  try {
    window.localStorage.removeItem(authTokenKey);
  } catch {
    // Ignore storage failures in restricted browser contexts.
  }
}

export function getActiveProfileId() {
  try {
    const value = window.localStorage.getItem(activeProfileKey) ?? "";
    const parsed = Number(value);
    return Number.isFinite(parsed) && parsed > 0 ? String(Math.trunc(parsed)) : "";
  } catch {
    return "";
  }
}

export function setActiveProfileId(profileId: number | string) {
  try {
    const value = String(profileId).trim();
    if (value) {
      window.localStorage.setItem(activeProfileKey, value);
    } else {
      window.localStorage.removeItem(activeProfileKey);
    }
  } catch {
    // Ignore storage failures in restricted browser contexts.
  }
}

type RequestOptions = RequestInit & {
  timeoutMs?: number;
};

export async function request<T>(path: string, init?: RequestOptions): Promise<T> {
  const token = getAuthToken();
  const profileId = getActiveProfileId();
  const { timeoutMs, headers, signal, ...fetchInit } = init ?? {};
  const requestHeaders = new Headers(headers);
  if (!requestHeaders.has("Content-Type")) {
    requestHeaders.set("Content-Type", "application/json");
  }
  if (token) {
    requestHeaders.set("Authorization", `Bearer ${token}`);
  }
  if (profileId) {
    requestHeaders.set("X-FolioSpace-Profile-Id", profileId);
  }
  const controller = timeoutMs ? new AbortController() : null;
  let timeout: number | undefined;
  let removeAbortListener: (() => void) | undefined;
  if (controller) {
    if (signal?.aborted) {
      controller.abort();
    } else if (signal) {
      const abort = () => controller.abort();
      signal.addEventListener("abort", abort, { once: true });
      removeAbortListener = () => signal.removeEventListener("abort", abort);
    }
    timeout = window.setTimeout(() => controller.abort(), timeoutMs);
  }
  let response: Response;
  try {
    response = await fetch(path, {
      ...fetchInit,
      headers: requestHeaders,
      signal: controller?.signal ?? signal,
    });
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError") {
      throw new Error(`Request timed out: ${path}`);
    }
    throw error;
  } finally {
    if (timeout !== undefined) {
      window.clearTimeout(timeout);
    }
    removeAbortListener?.();
  }
  if (!response.ok) {
    if (response.status === 401) {
      throw new Error("Unauthorized");
    }
    const body = await response.text();
    throw new Error(body || response.statusText);
  }
  return response.json() as Promise<T>;
}

export const api = {
  authStatus: (options?: RequestOptions) => request<AuthStatus>("/api/auth/status", options),
  authCheck: (token: string, options?: RequestOptions) =>
    request<{ ok: boolean }>("/api/auth/check", {
      ...options,
      method: "POST",
      body: JSON.stringify({ token }),
    }),
  authLogout: () => request<{ ok: boolean }>("/api/auth/logout", { method: "POST" }),
  setupStatus: (options?: RequestOptions) => request<SetupStatus>("/api/setup/status", options),
  setupInitialize: (input: SetupInput) =>
    request<Library>("/api/setup/initialize", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  directoryRoots: () => request<{ roots: DirectoryEntry[] }>("/api/config/directory-roots"),
  profiles: (options?: RequestOptions) => request<Profile[]>("/api/profiles", options),
  createProfile: (name: string, avatar?: string, color?: string) =>
    request<Profile>("/api/profiles", {
      method: "POST",
      body: JSON.stringify({ name, avatar, color }),
    }),
  updateProfile: (profileId: number, input: { name: string; avatar?: string; color?: string }) =>
    request<Profile>(`/api/profiles/${profileId}`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  renameProfile: (profileId: number, name: string) =>
    request<Profile>(`/api/profiles/${profileId}`, {
      method: "PUT",
      body: JSON.stringify({ name }),
    }),
  deleteProfile: (profileId: number) => request<{ ok: boolean }>(`/api/profiles/${profileId}`, { method: "DELETE" }),
  scanSettings: (options?: RequestOptions) => request<ScanSettings>("/api/settings/scan", options),
  saveScanSettings: (settings: ScanSettings) =>
    request<ScanSettings>("/api/settings/scan", {
      method: "PUT",
      body: JSON.stringify(settings),
    }),
  gameCatalogSettings: () => request<GameCatalogSettings>("/api/settings/game-catalog"),
  saveGameCatalogSettings: (settings: GameCatalogSettings) =>
    request<GameCatalogSettings>("/api/settings/game-catalog", {
      method: "PUT",
      body: JSON.stringify(settings),
    }),
  gameCurationSummary: () => request<GameCurationSummary>("/api/games/curation?summary=1"),
  gameCurationPage: (options: { limit?: number; offset?: number; q?: string; state?: string; platform?: string } = {}) => {
    const params = new URLSearchParams();
    if (options.limit) params.set("limit", String(options.limit));
    if (options.offset) params.set("offset", String(options.offset));
    if (options.q) params.set("q", options.q);
    if (options.state) params.set("state", options.state);
    if (options.platform) params.set("platform", options.platform);
    return request<GameCurationPage>(`/api/games/curation?${params.toString()}`);
  },
  gameCurationTask: () => request<GameCatalogTaskStatus>("/api/games/curation/task"),
  analyzeGameCatalog: () => request<GameCatalogTaskStatus>("/api/games/curation/analyze", { method: "POST" }),
  rebuildGameCompatibility: (options: { scope?: "game" | "platform" | "all"; gameId?: number; platform?: string; force?: boolean } = {}) =>
    request<GameCatalogTaskStatus>("/api/games/curation/rebuild", {
      method: "POST",
      body: JSON.stringify({ scope: options.scope || "all", ...options }),
    }),
  backfillGameChecksums: (limit = 100, gameId?: number) =>
    request<GameCatalogTaskStatus>("/api/games/curation/checksums", {
      method: "POST",
      body: JSON.stringify({ limit, ...(gameId ? { gameId } : {}) }),
    }),
  matchGameCovers: (includeNetwork: boolean) =>
    request<GameCatalogTaskStatus>("/api/games/curation/covers", {
      method: "POST",
      body: JSON.stringify({ includeNetwork }),
    }),
  gameDetails: (gameId: number) => request<GameDetails>(`/api/games/${gameId}`),
  refreshGameMetadata: (gameId: number) =>
    request<GameMetadataActionResult>(`/api/games/${gameId}/metadata/refresh`, { method: "POST" }),
  selectGameMetadataMatch: (gameId: number, source: string, sourceId: string) =>
    request<GameMetadataActionResult>(`/api/games/${gameId}/metadata/select-match`, {
      method: "POST",
      body: JSON.stringify({ source, sourceId }),
    }),
  saveGameMetadata: (gameId: number, metadata: GameMetadata) =>
    request<GameDetails>(`/api/games/${gameId}/metadata`, {
      method: "PUT",
      body: JSON.stringify(metadata),
    }),
  clientPreferences: (options?: RequestOptions) => request<ClientPreferences>("/api/client/preferences", options),
  saveClientPreferences: (preferences: ClientPreferences) =>
    request<ClientPreferences>("/api/client/preferences", {
      method: "PUT",
      body: JSON.stringify(preferences),
    }),
  clientInfo: (options?: RequestOptions) => request<ClientInfo>("/api/client/info", options),
  clientHome: (limit = 12, options?: RequestOptions) =>
    request<ClientHome>(`/api/client/home?limit=${limit}&includeCollections=false`, options),
  libraries: (options?: RequestOptions) => request<Library[]>("/api/libraries", options),
  createLibrary: (name: string, rootPath: string, assetType = "mixed", excludePatterns: string[] = []) =>
    request<Library>("/api/libraries", {
      method: "POST",
      body: JSON.stringify({ name, rootPath, assetType, excludePatterns }),
    }),
  updateLibraryExcludes: (libraryId: number, excludePatterns: string[]) =>
    request<Library>(`/api/libraries/${libraryId}`, {
      method: "PUT",
      body: JSON.stringify({ excludePatterns }),
    }),
  deleteLibrary: (libraryId: number) => request<{ ok: boolean }>(`/api/libraries/${libraryId}`, { method: "DELETE" }),
  directories: (path = "/") => request<DirectoryListing>(`/api/fs/directories?path=${encodeURIComponent(path)}`),
  scan: (libraryId: number, pathOrOptions?: string | ScanOptions) => {
    const body =
      typeof pathOrOptions === "string"
        ? pathOrOptions
          ? { path: pathOrOptions }
          : undefined
        : pathOrOptions;
    return request<ScanJob>(`/api/libraries/${libraryId}/scan`, {
      method: "POST",
      body: body ? JSON.stringify(body) : undefined,
    });
  },
  thumbnailWorkerStatus: (options?: { detail?: "summary" | "full"; timeoutMs?: number }) =>
    request<ThumbnailWorkerStatus>(
      `/api/thumbnail-worker/status${options?.detail === "full" ? "?detail=full" : ""}`,
      { timeoutMs: options?.timeoutMs },
    ),
  pauseThumbnailWorker: () => request<ThumbnailWorkerStatus>("/api/thumbnail-worker/pause", { method: "POST" }),
  resumeThumbnailWorker: () => request<ThumbnailWorkerStatus>("/api/thumbnail-worker/resume", { method: "POST" }),
  cancelThumbnailJobs: () => request<ThumbnailWorkerStatus>("/api/thumbnail-worker/cancel", { method: "POST" }),
  cleanupThumbnailOrphans: () => request<ThumbnailWorkerStatus>("/api/thumbnail-worker/cleanup-orphans", { method: "POST" }),
  series: (options?: RequestOptions) => request<Series[]>("/api/collections", options),
  collectionsPage: (options: CollectionListOptions = {}, requestOptions?: RequestOptions) => {
    const params = new URLSearchParams();
    if (options.limit) params.set("limit", String(options.limit));
    if (options.offset) params.set("offset", String(options.offset));
    if (options.q) params.set("q", options.q);
    if (options.primaryType) params.set("primaryType", options.primaryType);
    if (options.libraryId) params.set("libraryId", String(options.libraryId));
    if (options.format) params.set("format", options.format);
    if (options.sort) params.set("sort", options.sort);
    if (options.direction) params.set("direction", options.direction);
    return request<CollectionListPage>(`/api/collections?${params.toString()}`, requestOptions);
  },
  books: (seriesId: number) => request<Book[]>(`/api/collections/${seriesId}/volumes`),
  collectionAssets: (seriesId: number) => request<CollectionAssets>(`/api/collections/${seriesId}/assets`),
  collectionPrivateState: (seriesId: number, state: CollectionPrivateState) =>
    request<Series>(`/api/collections/${seriesId}/private-state`, {
      method: "PUT",
      body: JSON.stringify(state),
    }),
  booksPage: (seriesId: number, options: BookListOptions) => {
    const params = new URLSearchParams();
    if (options.format) params.set("format", options.format);
    if (options.limit) params.set("limit", String(options.limit));
    if (options.offset) params.set("offset", String(options.offset));
    if (options.q) params.set("q", options.q);
    if (options.sort) params.set("sort", options.sort);
    if (options.direction) params.set("direction", options.direction);
    return request<BookListPage>(`/api/collections/${seriesId}/volumes?${params.toString()}`);
  },
  continueReading: () => request<Book[]>("/api/books/continue-reading?limit=12"),
  recentBooks: () => request<Book[]>("/api/books/recent?limit=12"),
  recentGames: () => request<GameAsset[]>("/api/games/recent?limit=12"),
  recentVideos: () => request<VideoAsset[]>("/api/videos/recent?limit=12"),
  clientGames: (options: GameListOptions = {}) => {
    const params = new URLSearchParams();
    if (options.limit) params.set("limit", String(options.limit));
    if (options.offset) params.set("offset", String(options.offset));
    if (options.q) params.set("q", options.q);
    if (options.platform) params.set("platform", options.platform);
    if (options.romSetName) params.set("romSetName", options.romSetName);
    if (options.format) params.set("format", options.format);
    if (options.sort) params.set("sort", options.sort);
    if (options.direction) params.set("direction", options.direction);
    return request<GameListPage>(`/api/client/games?${params.toString()}`);
  },
  clientGameFacets: () => request<GameListFacets>("/api/client/games/facets"),
  gamePrivateState: (gameId: number, state: GamePrivateState) =>
    request<GameAsset>(`/api/client/games/${gameId}/private-state`, {
      method: "PUT",
      body: JSON.stringify(state),
    }),
  manualCollections: (options?: RequestOptions) => request<ManualCollection[]>("/api/client/manual-collections", options),
  createManualCollection: (payload: Pick<ManualCollection, "name"> & Partial<Pick<ManualCollection, "description">>) =>
    request<ManualCollection>("/api/client/manual-collections", {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  manualCollectionDetails: (collectionId: number) => request<ManualCollectionDetails>(`/api/client/manual-collections/${collectionId}`),
  addManualCollectionItem: (collectionId: number, item: Pick<ManualCollectionItem, "assetType" | "assetId">) =>
    request<ManualCollection>(`/api/client/manual-collections/${collectionId}/items`, {
      method: "POST",
      body: JSON.stringify(item),
    }),
  removeManualCollectionItem: (collectionId: number, item: Pick<ManualCollectionItem, "assetType" | "assetId">) =>
    request<ManualCollection>(`/api/client/manual-collections/${collectionId}/items/${item.assetType}/${item.assetId}`, {
      method: "DELETE",
    }),
  clientVideos: (options: VideoListOptions = {}) => {
    const params = new URLSearchParams();
    if (options.limit) params.set("limit", String(options.limit));
    if (options.offset) params.set("offset", String(options.offset));
    if (options.q) params.set("q", options.q);
    if (options.format) params.set("format", options.format);
    if (options.sort) params.set("sort", options.sort);
    if (options.direction) params.set("direction", options.direction);
    return request<VideoListPage>(`/api/client/videos?${params.toString()}`);
  },
  videoTranscodeStatus: (videoId: number) => request<VideoTranscodeStatus>(`/api/client/videos/${videoId}/transcode/status`),
  videoTranscodeQueueStatus: () => request<VideoTranscodeQueueStatus>("/api/client/videos/transcode/status"),
  favoriteBooks: () => request<Book[]>("/api/books/favorites?limit=12"),
  privateStatusBooks: (status: string) => request<Book[]>(`/api/books/private-status/${encodeURIComponent(status)}?limit=12`),
  search: (q: string, limit = 12) =>
    request<SearchResponse>(`/api/search?q=${encodeURIComponent(q)}&limit=${limit}`),
  pages: (bookId: number) => request<Page[]>(`/api/books/${bookId}/pages`),
  epubManifest: (bookId: number) => request<EpubManifest>(`/api/books/${bookId}/epub/manifest`),
  jobs: (options?: RequestOptions) => request<ScanJob[]>("/api/jobs", options),
  jobEvents: (jobId: number) => request<JobEvent[]>(`/api/jobs/${jobId}/events`),
  pauseJob: (jobId: number) => request<ScanJob>(`/api/jobs/${jobId}/pause`, { method: "POST" }),
  cancelJob: (jobId: number) => request<ScanJob>(`/api/jobs/${jobId}/cancel`, { method: "POST" }),
  resumeJob: (jobId: number) => request<ScanJob>(`/api/jobs/${jobId}/resume`, { method: "POST" }),
  errors: (options?: RequestOptions) => request<FileError[]>("/api/errors", options),
  jobErrors: (jobId: number) => request<FileError[]>(`/api/errors?jobId=${jobId}`),
  readProgress: (bookId: number) => request<ReadProgress>(`/api/books/${bookId}/progress`),
  readingPositions: (bookId: number) => request<ReadingPositions>(`/api/books/${bookId}/reading-position`),
  saveWebtoonReadingPosition: (bookId: number, position: WebtoonReadingPosition) =>
    request<WebtoonReadingPosition>(`/api/books/${bookId}/reading-position/webtoon`, {
      method: "PUT",
      timeoutMs: 10_000,
      body: JSON.stringify(position),
    }),
  progress: (bookId: number, pageIndex: number) =>
    request<{ ok: boolean }>(`/api/books/${bookId}/progress`, {
      method: "PUT",
      body: JSON.stringify({ pageIndex }),
    }),
  progressDetail: (bookId: number, pageIndex: number, locator: string, progressFraction: number) =>
    request<{ ok: boolean }>(`/api/books/${bookId}/progress`, {
      method: "PUT",
      timeoutMs: 10_000,
      body: JSON.stringify({ pageIndex, locator, progressFraction }),
    }),
  privateState: (bookId: number, state: BookPrivateState) =>
    request<Book>(`/api/books/${bookId}/private-state`, {
      method: "PUT",
      body: JSON.stringify(state),
    }),
};
