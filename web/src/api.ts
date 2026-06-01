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

export type Series = {
  id: number;
  libraryId: number;
  title: string;
  directoryPath: string;
  collectionType: "directory" | "game_platform";
  primaryType: "book" | "comic" | "game" | "video";
  bookCount: number;
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
};

export type ScanJob = {
  id: number;
  libraryId: number;
  status: string;
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

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const token = getAuthToken();
  const response = await fetch(path, {
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(init?.headers ?? {}),
    },
    ...init,
  });
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
  authStatus: () => request<AuthStatus>("/api/auth/status"),
  authCheck: (token: string) =>
    request<{ ok: boolean }>("/api/auth/check", {
      method: "POST",
      body: JSON.stringify({ token }),
    }),
  authLogout: () => request<{ ok: boolean }>("/api/auth/logout", { method: "POST" }),
  setupStatus: () => request<SetupStatus>("/api/setup/status"),
  setupInitialize: (input: SetupInput) =>
    request<Library>("/api/setup/initialize", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  directoryRoots: () => request<{ roots: DirectoryEntry[] }>("/api/config/directory-roots"),
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
  scan: (libraryId: number) => request<ScanJob>(`/api/libraries/${libraryId}/scan`, { method: "POST" }),
  series: () => request<Series[]>("/api/collections"),
  books: (seriesId: number) => request<Book[]>(`/api/collections/${seriesId}/volumes`),
  collectionAssets: (seriesId: number) => request<CollectionAssets>(`/api/collections/${seriesId}/assets`),
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
