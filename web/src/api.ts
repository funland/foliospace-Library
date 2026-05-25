export type Library = {
  id: number;
  name: string;
  rootPath: string;
};

export type Series = {
  id: number;
  libraryId: number;
  title: string;
  directoryPath: string;
  collectionType: "directory";
  bookCount: number;
};

export type Book = {
  id: number;
  seriesId: number;
  collectionTitle?: string;
  title: string;
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
};

export type BookListPage = {
  items: Book[];
  total: number;
  limit: number;
  offset: number;
  hasMore: boolean;
};

export type BookListOptions = {
  limit?: number;
  offset?: number;
  q?: string;
  sort?: string;
};

export type Page = {
  index: number;
  name: string;
};

export type EpubManifest = {
  title: string;
  creator: string;
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

const authTokenKey = "foliospace_api_token";

export function getAuthToken() {
  return window.localStorage.getItem(authTokenKey) ?? "";
}

export function setAuthToken(token: string) {
  window.localStorage.setItem(authTokenKey, token);
}

export function clearAuthToken() {
  window.localStorage.removeItem(authTokenKey);
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
  libraries: () => request<Library[]>("/api/libraries"),
  createLibrary: (name: string, rootPath: string) =>
    request<Library>("/api/libraries", {
      method: "POST",
      body: JSON.stringify({ name, rootPath }),
    }),
  deleteLibrary: (libraryId: number) => request<{ ok: boolean }>(`/api/libraries/${libraryId}`, { method: "DELETE" }),
  scan: (libraryId: number) => request<ScanJob>(`/api/libraries/${libraryId}/scan`, { method: "POST" }),
  series: () => request<Series[]>("/api/collections"),
  books: (seriesId: number) => request<Book[]>(`/api/collections/${seriesId}/volumes`),
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
  pages: (bookId: number) => request<Page[]>(`/api/books/${bookId}/pages`),
  epubManifest: (bookId: number) => request<EpubManifest>(`/api/books/${bookId}/epub/manifest`),
  jobs: () => request<ScanJob[]>("/api/jobs"),
  jobEvents: (jobId: number) => request<JobEvent[]>(`/api/jobs/${jobId}/events`),
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
};
