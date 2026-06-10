export type Library = {
  id: number;
  name: string;
  rootPath: string;
  assetType: "mixed" | "book" | "comic" | "game" | "video";
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
  id: number;
  libraryId: number;
  title: string;
  directoryPath: string;
  collectionType: "directory" | "game_platform";
  primaryType: "book" | "comic" | "game" | "video";
  bookCount: number;
  coverBookId?: number;
  thumbnailStatus?: string;
  thumbnailUrl?: string;
  favorite: boolean;
  liked: boolean;
};

export type Book = {
  id: number;
  seriesId: number;
  collectionTitle?: string;
  title: string;
  creator?: string;
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
  addedAt: string;
  updatedAt: string;
  currentPage: number;
  progressFraction: number;
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

export type GameAsset = {
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
  compatibility: string;
  coverUrl?: string;
  manifestUrl?: string;
};

export type VideoAsset = {
  id: number;
  assetType?: "video";
  title: string;
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

export type BookListPage = {
  items: Book[];
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
  limit?: number;
  offset?: number;
  q?: string;
  sort?: string;
};

export type GameListOptions = BookListOptions & {
  platform?: string;
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
  tocTree?: EpubTocItem[];
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
  children?: EpubTocItem[];
};

export type ReadProgress = {
  bookId: number;
  pageIndex: number;
  locator: string;
  progressFraction: number;
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
  scanWorkers?: number;
};

export type ScanSettings = {
  scanWorkers: number;
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

async function request<T>(path: string, init?: RequestOptions): Promise<T> {
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
  profiles: () => request<Profile[]>("/api/profiles"),
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
  scanSettings: () => request<ScanSettings>("/api/settings/scan"),
  saveScanSettings: (settings: ScanSettings) =>
    request<ScanSettings>("/api/settings/scan", {
      method: "PUT",
      body: JSON.stringify(settings),
    }),
  clientPreferences: () => request<ClientPreferences>("/api/client/preferences"),
  saveClientPreferences: (preferences: ClientPreferences) =>
    request<ClientPreferences>("/api/client/preferences", {
      method: "PUT",
      body: JSON.stringify(preferences),
    }),
  clientInfo: () => request<ClientInfo>("/api/client/info"),
  libraries: () => request<Library[]>("/api/libraries"),
  createLibrary: (name: string, rootPath: string, assetType = "mixed") =>
    request<Library>("/api/libraries", {
      method: "POST",
      body: JSON.stringify({ name, rootPath, assetType }),
    }),
  deleteLibrary: (libraryId: number) => request<{ ok: boolean }>(`/api/libraries/${libraryId}`, { method: "DELETE" }),
  directories: (path = "/") => request<DirectoryListing>(`/api/fs/directories?path=${encodeURIComponent(path)}`),
  scan: (libraryId: number, path?: string) =>
    request<ScanJob>(`/api/libraries/${libraryId}/scan`, {
      method: "POST",
      body: path ? JSON.stringify({ path }) : undefined,
    }),
  thumbnailWorkerStatus: (options?: { detail?: "summary" | "full"; timeoutMs?: number }) =>
    request<ThumbnailWorkerStatus>(
      `/api/thumbnail-worker/status${options?.detail === "full" ? "?detail=full" : ""}`,
      { timeoutMs: options?.timeoutMs },
    ),
  pauseThumbnailWorker: () => request<ThumbnailWorkerStatus>("/api/thumbnail-worker/pause", { method: "POST" }),
  resumeThumbnailWorker: () => request<ThumbnailWorkerStatus>("/api/thumbnail-worker/resume", { method: "POST" }),
  cancelThumbnailJobs: () => request<ThumbnailWorkerStatus>("/api/thumbnail-worker/cancel", { method: "POST" }),
  cleanupThumbnailOrphans: () => request<ThumbnailWorkerStatus>("/api/thumbnail-worker/cleanup-orphans", { method: "POST" }),
  series: () => request<Series[]>("/api/collections"),
  books: (seriesId: number) => request<Book[]>(`/api/collections/${seriesId}/volumes`),
  collectionAssets: (seriesId: number) => request<CollectionAssets>(`/api/collections/${seriesId}/assets`),
  collectionPrivateState: (seriesId: number, state: CollectionPrivateState) =>
    request<Series>(`/api/collections/${seriesId}/private-state`, {
      method: "PUT",
      body: JSON.stringify(state),
    }),
  booksPage: (seriesId: number, options: BookListOptions) => {
    const params = new URLSearchParams();
    if (options.limit) params.set("limit", String(options.limit));
    if (options.offset) params.set("offset", String(options.offset));
    if (options.q) params.set("q", options.q);
    if (options.sort) params.set("sort", options.sort);
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
    if (options.format) params.set("format", options.format);
    if (options.sort) params.set("sort", options.sort);
    return request<GameListPage>(`/api/client/games?${params.toString()}`);
  },
  clientVideos: (options: VideoListOptions = {}) => {
    const params = new URLSearchParams();
    if (options.limit) params.set("limit", String(options.limit));
    if (options.offset) params.set("offset", String(options.offset));
    if (options.q) params.set("q", options.q);
    if (options.format) params.set("format", options.format);
    if (options.sort) params.set("sort", options.sort);
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
  jobs: () => request<ScanJob[]>("/api/jobs"),
  jobEvents: (jobId: number) => request<JobEvent[]>(`/api/jobs/${jobId}/events`),
  pauseJob: (jobId: number) => request<ScanJob>(`/api/jobs/${jobId}/pause`, { method: "POST" }),
  cancelJob: (jobId: number) => request<ScanJob>(`/api/jobs/${jobId}/cancel`, { method: "POST" }),
  resumeJob: (jobId: number) => request<ScanJob>(`/api/jobs/${jobId}/resume`, { method: "POST" }),
  errors: () => request<FileError[]>("/api/errors"),
  jobErrors: (jobId: number) => request<FileError[]>(`/api/errors?jobId=${jobId}`),
  readProgress: (bookId: number) => request<ReadProgress>(`/api/books/${bookId}/progress`),
  readingPositions: (bookId: number) => request<ReadingPositions>(`/api/books/${bookId}/reading-position`),
  saveWebtoonReadingPosition: (bookId: number, position: WebtoonReadingPosition) =>
    request<WebtoonReadingPosition>(`/api/books/${bookId}/reading-position/webtoon`, {
      method: "PUT",
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
      body: JSON.stringify({ pageIndex, locator, progressFraction }),
    }),
  privateState: (bookId: number, state: BookPrivateState) =>
    request<Book>(`/api/books/${bookId}/private-state`, {
      method: "PUT",
      body: JSON.stringify(state),
    }),
};
