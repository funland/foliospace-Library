import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { FormEvent, MouseEvent, TouchEvent } from "react";
import { api, Book, BookPrivateState, clearAuthToken, EpubManifest, FileError, getAuthToken, JobEvent, Library, Page, ScanJob, Series, setAuthToken } from "./api";

type View = "library" | "reader" | "jobs" | "errors";
type ReaderPageMode = "single" | "double";
type EpubTheme = "light" | "sepia" | "dark";
type BookSort = "title" | "recently_added" | "last_read" | "progress" | "unread";
type Locale = "zh" | "zht" | "en" | "ja" | "ko";
const bookPageSize = 60;

export function App() {
  const [view, setView] = useState<View>("library");
  const [libraries, setLibraries] = useState<Library[]>([]);
  const [series, setSeries] = useState<Series[]>([]);
  const [books, setBooks] = useState<Book[]>([]);
  const [continueBooks, setContinueBooks] = useState<Book[]>([]);
  const [recentBooks, setRecentBooks] = useState<Book[]>([]);
  const [favoriteBooks, setFavoriteBooks] = useState<Book[]>([]);
  const [wantBooks, setWantBooks] = useState<Book[]>([]);
  const [jobs, setJobs] = useState<ScanJob[]>([]);
  const [errors, setErrors] = useState<FileError[]>([]);
  const [jobEvents, setJobEvents] = useState<JobEvent[]>([]);
  const [jobErrors, setJobErrors] = useState<FileError[]>([]);
  const [selectedJob, setSelectedJob] = useState<ScanJob | null>(null);
  const [selectedSeries, setSelectedSeries] = useState<Series | null>(null);
  const [selectedBook, setSelectedBook] = useState<Book | null>(null);
  const [pages, setPages] = useState<Page[]>([]);
  const [bookTotal, setBookTotal] = useState(0);
  const [bookHasMore, setBookHasMore] = useState(false);
  const [bookListLoading, setBookListLoading] = useState(false);
  const [globalBooks, setGlobalBooks] = useState<Book[]>([]);
  const [globalSearchLoading, setGlobalSearchLoading] = useState(false);
  const [epubManifest, setEpubManifest] = useState<EpubManifest | null>(null);
  const [pageIndex, setPageIndex] = useState(0);
  const [displayedPageIndex, setDisplayedPageIndex] = useState(0);
  const [query, setQuery] = useState("");
  const [bookSort, setBookSort] = useState<BookSort>("title");
  const [status, setStatus] = useState("Ready");
  const [authChecked, setAuthChecked] = useState(false);
  const [authEnabled, setAuthEnabled] = useState(false);
  const [authRequired, setAuthRequired] = useState(false);
  const [authInput, setAuthInput] = useState("");
  const [authError, setAuthError] = useState("");
  const [activeTask, setActiveTask] = useState<string | null>(null);
  const [readerLoadState, setReaderLoadState] = useState<"idle" | "loading" | "ready" | "error">("idle");
  const [readerRetryKey, setReaderRetryKey] = useState(0);
  const [readerPageMode, setReaderPageMode] = useState<ReaderPageMode>("single");
  const [readerFullscreen, setReaderFullscreen] = useState(false);
  const [epubPageMode, setEpubPageMode] = useState<ReaderPageMode>("single");
  const [epubFontSize, setEpubFontSize] = useState(18);
  const [epubTheme, setEpubTheme] = useState<EpubTheme>("light");
  const [epubPagePosition, setEpubPagePosition] = useState(0);
  const [epubPageCount, setEpubPageCount] = useState(1);
  const [epubTocOpen, setEpubTocOpen] = useState(false);
  const [newLibraryName, setNewLibraryName] = useState("");
  const [newLibraryPath, setNewLibraryPath] = useState("");
  const [privateDraft, setPrivateDraft] = useState<BookPrivateState>(emptyPrivateState());
  const [privateSaving, setPrivateSaving] = useState(false);
  const [locale, setLocale] = useState<Locale>(readLocale());
  const t = translations[locale];
  const imageCache = useRef<Set<string>>(new Set());
  const readerRef = useRef<HTMLElement | null>(null);
  const bookLoadMoreRef = useRef<HTMLDivElement | null>(null);
  const bookListRequest = useRef(0);
  const swipeStart = useRef<{ x: number; y: number } | null>(null);
  const epubRestorePosition = useRef<number | null>(null);

  async function refreshAll(showProgress = false) {
    if (showProgress) {
      setActiveTask("Refreshing library");
    }
    const [nextLibraries, nextSeries, nextJobs, nextErrors, nextContinueBooks, nextRecentBooks, nextFavoriteBooks, nextWantBooks] = await Promise.all([
      api.libraries(),
      api.series(),
      api.jobs(),
      api.errors(),
      api.continueReading(),
      api.recentBooks(),
      api.favoriteBooks(),
      api.privateStatusBooks("want"),
    ]);
    setLibraries(nextLibraries);
    setSeries(nextSeries);
    setJobs(nextJobs);
    setErrors(nextErrors);
    setContinueBooks(nextContinueBooks);
    setRecentBooks(nextRecentBooks);
    setFavoriteBooks(nextFavoriteBooks);
    setWantBooks(nextWantBooks);
    if (showProgress) {
      setActiveTask(null);
    }
  }

  useEffect(() => {
    async function bootstrap() {
      try {
        const auth = await api.authStatus();
        setAuthEnabled(auth.enabled);
        const storedToken = getAuthToken();
        if (auth.enabled && !storedToken) {
          setAuthRequired(true);
          setStatus("Authentication required");
          return;
        }
        if (auth.enabled) {
          await api.authCheck(storedToken);
        }
        await refreshAll(true);
      } catch (error) {
        if (isUnauthorized(error)) {
          clearAuthToken();
          setAuthRequired(true);
          setStatus("Authentication required");
          return;
        }
        setStatus(error instanceof Error ? error.message : "Failed to load");
      } finally {
        setAuthChecked(true);
        setActiveTask(null);
      }
    }

    bootstrap();
  }, []);

  useEffect(() => {
    window.localStorage.setItem("foliospace_locale", locale);
  }, [locale]);

  useEffect(() => {
    const value = query.trim();
    if (value.length < 2 || view !== "library") {
      setGlobalBooks([]);
      setGlobalSearchLoading(false);
      return;
    }
    let cancelled = false;
    setGlobalSearchLoading(true);
    const timer = window.setTimeout(() => {
      api.search(value, 12)
        .then((result) => {
          if (!cancelled) {
            setGlobalBooks(result.books ?? []);
          }
        })
        .catch((error) => {
          if (!cancelled) {
            setStatus(error instanceof Error ? error.message : "Search failed");
            setGlobalBooks([]);
          }
        })
        .finally(() => {
          if (!cancelled) {
            setGlobalSearchLoading(false);
          }
        });
    }, 220);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [query, view]);

  useEffect(() => {
    if (!selectedBook) {
      setPrivateDraft(emptyPrivateState());
      return;
    }
    setPrivateDraft(privateStateFromBook(selectedBook));
  }, [selectedBook]);

  async function submitAuth(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const token = authInput.trim();
    if (!token) return;
    setAuthError("");
    setActiveTask("Unlocking library");
    try {
      await api.authCheck(token);
      setAuthToken(token);
      setAuthRequired(false);
      setAuthInput("");
      setStatus("Ready");
      await refreshAll(true);
    } catch (error) {
      clearAuthToken();
      setAuthError(error instanceof Error ? error.message : "Invalid access token");
    } finally {
      setActiveTask(null);
      setAuthChecked(true);
    }
  }

  function lockApp() {
    api.authLogout().catch(() => undefined);
    clearAuthToken();
    setAuthRequired(true);
    setAuthInput("");
    setStatus("Authentication required");
  }

  function handleAPIError(error: unknown) {
    if (isUnauthorized(error)) {
      lockApp();
      return;
    }
    setStatus(error instanceof Error ? error.message : "Request failed");
  }

  const activeScan = jobs.find((job) => job.status === "running") ?? null;

  useEffect(() => {
    if (!activeScan) return;

    const timer = window.setInterval(() => {
      refreshAll().catch(handleAPIError);
    }, 1200);

    return () => window.clearInterval(timer);
  }, [activeScan?.id]);

  useEffect(() => {
    if (!selectedBook) return;
    if (selectedBook.format === "epub") return;

    const timer = window.setTimeout(() => {
      api
        .progressDetail(
          selectedBook.id,
          pageIndex,
          "",
          pages.length > 1 ? pageIndex / (pages.length - 1) : 0,
        )
        .catch(() => undefined);
    }, 450);

    return () => window.clearTimeout(timer);
  }, [selectedBook, pageIndex, pages.length]);

  useEffect(() => {
    if (!selectedBook || selectedBook.format !== "epub") return;

    const timer = window.setTimeout(() => {
      api
        .progressDetail(
          selectedBook.id,
          pageIndex,
          String(epubPagePosition),
          epubPageCount > 1 ? epubPagePosition / (epubPageCount - 1) : 0,
        )
        .catch(() => undefined);
    }, 450);

    return () => window.clearTimeout(timer);
  }, [selectedBook, pageIndex, epubPagePosition, epubPageCount]);

  async function scan(library: Library) {
    setStatus(`Scanning ${library.rootPath}`);
    setActiveTask("Scanning library");
    try {
      const job = await api.scan(library.id);
      setStatus(`Scan queued: job #${job.id}`);
      await refreshAll();
    } finally {
      setActiveTask(null);
    }
  }

  async function deleteLibrary(library: Library) {
    const confirmed = window.confirm(`Remove "${library.name}" from FolioSpace Library? Files on disk will not be deleted.`);
    if (!confirmed) return;

    setActiveTask(`Removing ${library.name}`);
    try {
      await api.deleteLibrary(library.id);
      setStatus(`Library removed: ${library.rootPath}`);
      setSelectedSeries(null);
      setBooks([]);
      setBookTotal(0);
      setBookHasMore(false);
      await refreshAll();
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Failed to remove library");
    } finally {
      setActiveTask(null);
    }
  }

  async function addLibrary(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setActiveTask("Adding library");
    try {
      const library = await api.createLibrary(newLibraryName, newLibraryPath);
      setStatus(`Library added: ${library.rootPath}`);
      setNewLibraryName("");
      setNewLibraryPath("");
      await refreshAll();
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Failed to add library");
    } finally {
      setActiveTask(null);
    }
  }

  async function openJob(job: ScanJob) {
    setActiveTask(`Loading job #${job.id}`);
    setSelectedJob(job);
    try {
      const [events, scopedErrors] = await Promise.all([api.jobEvents(job.id), api.jobErrors(job.id)]);
      setJobEvents(events);
      setJobErrors(scopedErrors);
    } finally {
      setActiveTask(null);
    }
  }

  function openSeries(item: Series) {
    setStatus(`Loading ${item.title}`);
    setSelectedSeries(item);
    setQuery("");
    setBooks([]);
    setBookTotal(0);
    setBookHasMore(false);
  }

  const loadBooksPage = useCallback(
    async (seriesItem: Series, offset: number, reset: boolean) => {
      const requestID = ++bookListRequest.current;
      setBookListLoading(true);
      try {
        const page = await api.booksPage(seriesItem.id, {
          limit: bookPageSize,
          offset,
          q: query.trim(),
          sort: bookSort,
        });
        if (requestID !== bookListRequest.current) return;
        const pageItems = page.items ?? [];
        setBooks((currentBooks) => {
          const nextBooks = reset ? pageItems : [...currentBooks, ...pageItems];
          const seen = new Set<number>();
          return nextBooks.filter((book) => {
            if (seen.has(book.id)) return false;
            seen.add(book.id);
            return true;
          });
        });
        setBookTotal(page.total);
        setBookHasMore(page.hasMore);
        setStatus("Ready");
      } catch (error) {
        if (requestID !== bookListRequest.current) return;
        setStatus(error instanceof Error ? error.message : "Failed to load volumes");
      } finally {
        if (requestID === bookListRequest.current) {
          setBookListLoading(false);
        }
      }
    },
    [bookSort, query],
  );

  useEffect(() => {
    if (!selectedSeries) return;
    setBooks([]);
    setBookTotal(0);
    setBookHasMore(false);
    void loadBooksPage(selectedSeries, 0, true);
  }, [loadBooksPage, selectedSeries]);

  useEffect(() => {
    const node = bookLoadMoreRef.current;
    if (!node || !selectedSeries || !bookHasMore || bookListLoading) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          void loadBooksPage(selectedSeries, books.length, false);
        }
      },
      { rootMargin: "600px 0px" },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [bookHasMore, bookListLoading, books.length, loadBooksPage, selectedSeries]);

  async function openBook(book: Book) {
    setActiveTask(`Opening ${book.title}`);
    setEpubManifest(null);
    setPageIndex(0);
    setDisplayedPageIndex(0);
    setEpubPagePosition(0);
    setEpubPageCount(1);
    setEpubTocOpen(false);
    setReaderLoadState("loading");
    try {
      const nextPages = await api.pages(book.id);
      setPages(nextPages);
      if (book.format === "epub") {
        const [manifest, progress] = await Promise.all([api.epubManifest(book.id), api.readProgress(book.id)]);
        const restoredPosition = readEpubLocator(progress.locator);
        epubRestorePosition.current = restoredPosition;
        setEpubManifest(manifest);
        setPageIndex(Math.max(0, Math.min(progress.pageIndex, Math.max(0, nextPages.length - 1))));
        setEpubPagePosition(restoredPosition);
        setReaderLoadState("ready");
      } else {
        const progress = await api.readProgress(book.id);
        const restoredPage = Math.max(0, Math.min(progress.pageIndex, Math.max(0, nextPages.length - 1)));
        setPageIndex(restoredPage);
        setDisplayedPageIndex(restoredPage);
      }
      setSelectedBook(book);
      setView("reader");
    } finally {
      setActiveTask(null);
    }
  }

  function mergeBookState(updatedBook: Book) {
    setSelectedBook((currentBook) => (currentBook?.id === updatedBook.id ? updatedBook : currentBook));
    setBooks((items) => replaceBook(items, updatedBook));
    setContinueBooks((items) => replaceBook(items, updatedBook));
    setRecentBooks((items) => replaceBook(items, updatedBook));
    setFavoriteBooks((items) => mergeShelfBook(items, updatedBook, (book) => book.favorite));
    setWantBooks((items) => mergeShelfBook(items, updatedBook, (book) => book.privateStatus === "want"));
    setGlobalBooks((items) => replaceBook(items, updatedBook));
  }

  async function savePrivateState() {
    if (!selectedBook) return;
    setPrivateSaving(true);
    try {
      const updatedBook = await api.privateState(selectedBook.id, {
        ...privateDraft,
        tags: normalizeDraftTags(privateDraft.tags),
      });
      mergeBookState(updatedBook);
      setStatus("Private state saved");
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Failed to save private state");
    } finally {
      setPrivateSaving(false);
    }
  }

  async function setReaderPage(book: Book, nextIndex: number) {
    const clamped = Math.max(0, Math.min(nextIndex, Math.max(0, pages.length - 1)));
    if (book.format === "epub") {
      setEpubPagePosition(0);
      setEpubPageCount(1);
    }
    if (clamped !== pageIndex) {
      setReaderLoadState("loading");
    }
    setPageIndex(clamped);
  }

  useEffect(() => {
    if (!selectedBook || pages.length === 0 || selectedBook.format === "epub") return;

    let cancelled = false;
    const targetIndex = pageIndex;
    setReaderLoadState("loading");

    preloadVisiblePages(selectedBook.id, targetIndex, pages.length, readerPageMode)
      .then(() => {
        if (cancelled) return;
        setDisplayedPageIndex(targetIndex);
        setReaderLoadState("ready");
        prefetchNeighborPages(selectedBook.id, targetIndex, pages.length, readerPageMode);
      })
      .catch(() => {
        if (cancelled) return;
        setReaderLoadState("error");
        setStatus(`Failed to load page ${targetIndex + 1}`);
      });

    return () => {
      cancelled = true;
    };
  }, [selectedBook?.id, pageIndex, pages.length, readerRetryKey, readerPageMode]);

  useEffect(() => {
    function onFullscreenChange() {
      setReaderFullscreen(document.fullscreenElement === readerRef.current);
    }

    document.addEventListener("fullscreenchange", onFullscreenChange);
    return () => document.removeEventListener("fullscreenchange", onFullscreenChange);
  }, []);

  useEffect(() => {
    if (view !== "reader" || !selectedBook) return;

    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "ArrowLeft") {
        event.preventDefault();
        goReaderPrevious();
      }
      if (event.key === "ArrowRight") {
        event.preventDefault();
        goReaderNext();
      }
      if (event.key.toLowerCase() === "f") {
        event.preventDefault();
        toggleReaderFullscreen();
      }
    }

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [view, selectedBook, pageIndex, pages.length, readerPageMode, epubPagePosition, epubPageCount]);

  useEffect(() => {
    if (view !== "reader" || !selectedBook) return;

    function onMouseUp(event: globalThis.MouseEvent) {
      finishReaderSwipe(event.clientX, event.clientY);
    }

    function onTouchEnd(event: globalThis.TouchEvent) {
      const touch = event.changedTouches[0];
      if (touch) {
        finishReaderSwipe(touch.clientX, touch.clientY);
      }
    }

    window.addEventListener("mouseup", onMouseUp);
    window.addEventListener("touchend", onTouchEnd);
    return () => {
      window.removeEventListener("mouseup", onMouseUp);
      window.removeEventListener("touchend", onTouchEnd);
    };
  }, [view, selectedBook, pageIndex, pages.length, readerPageMode, epubPagePosition, epubPageCount]);

  function preloadPage(bookID: number, index: number) {
    const src = `/api/books/${bookID}/pages/${index}`;
    if (imageCache.current.has(src)) {
      return Promise.resolve();
    }

    return new Promise<void>((resolve, reject) => {
      const image = new Image();
      image.onload = () => {
        const decode = "decode" in image ? image.decode() : Promise.resolve();
        decode
          .catch(() => undefined)
          .then(() => {
            imageCache.current.add(src);
            resolve();
          });
      };
      image.onerror = () => reject(new Error(`Failed to load ${src}`));
      image.src = src;
    });
  }

  function preloadVisiblePages(bookID: number, index: number, total: number, mode: ReaderPageMode) {
    const visible = visiblePageIndexes(index, total, mode);
    return Promise.all(visible.map((next) => preloadPage(bookID, next)));
  }

  function prefetchNeighborPages(bookID: number, index: number, total: number, mode: ReaderPageMode) {
    const step = mode === "double" ? 2 : 1;
    for (const next of [index + step, index - step]) {
      if (next >= 0 && next < total) {
        preloadVisiblePages(bookID, next, total, mode).catch(() => undefined);
      }
    }
  }

  function visiblePageIndexes(index: number, total: number, mode: ReaderPageMode) {
    if (total <= 0) return [];
    if (mode === "single") return [index];
    return [index, index + 1].filter((next) => next >= 0 && next < total);
  }

  function readerStep() {
    if (selectedBook?.format === "epub") return 1;
    return readerPageMode === "double" ? 2 : 1;
  }

  function goReaderPrevious() {
    if (!selectedBook) return;
    if (selectedBook.format === "epub") {
      if (epubPagePosition > 0) {
        setEpubPagePosition((value) => Math.max(0, value - 1));
        return;
      }
      setReaderPage(selectedBook, pageIndex - 1);
      return;
    }
    setReaderPage(selectedBook, pageIndex - readerStep());
  }

  function goReaderNext() {
    if (!selectedBook) return;
    if (selectedBook.format === "epub") {
      if (epubPagePosition < epubPageCount - 1) {
        setEpubPagePosition((value) => Math.min(epubPageCount - 1, value + 1));
        return;
      }
      setReaderPage(selectedBook, pageIndex + 1);
      return;
    }
    setReaderPage(selectedBook, pageIndex + readerStep());
  }

  function jumpToEpubChapter(index: number) {
    if (!selectedBook) return;
    setEpubTocOpen(false);
    setReaderPage(selectedBook, index);
  }

  async function toggleReaderFullscreen() {
    if (!readerRef.current) return;
    try {
      if (document.fullscreenElement === readerRef.current) {
        await document.exitFullscreen();
        return;
      }
      await readerRef.current.requestFullscreen();
    } catch (error) {
      setStatus(error instanceof Error ? `Fullscreen unavailable: ${error.message}` : "Fullscreen unavailable");
    }
  }

  function startReaderSwipe(x: number, y: number) {
    swipeStart.current = { x, y };
  }

  function finishReaderSwipe(x: number, y: number) {
    const start = swipeStart.current;
    swipeStart.current = null;
    if (!start) return;

    const deltaX = x - start.x;
    const deltaY = y - start.y;
    if (Math.abs(deltaX) < 48 || Math.abs(deltaX) < Math.abs(deltaY) * 1.2) return;

    if (deltaX < 0) {
      goReaderNext();
    } else {
      goReaderPrevious();
    }
  }

  function handleReaderMouseDown(event: MouseEvent<HTMLDivElement>) {
    startReaderSwipe(event.clientX, event.clientY);
  }

  function handleReaderTouchStart(event: TouchEvent<HTMLDivElement>) {
    const touch = event.changedTouches[0];
    if (touch) {
      startReaderSwipe(touch.clientX, touch.clientY);
    }
  }

  const filteredSeries = useMemo(() => {
    const value = query.trim().toLowerCase();
    if (!value) return series;
    return series.filter((item) => item.title.toLowerCase().includes(value));
  }, [query, series]);

  const scanProgressLabel = activeScan
    ? `${activeScan.indexedFiles} indexed · ${activeScan.skippedFiles} skipped · ${activeScan.errorCount} errors`
    : null;
  const selectedJobLatest = selectedJob ? jobs.find((job) => job.id === selectedJob.id) ?? selectedJob : null;

  return (
    <main className="app">
      <aside className="sidebar">
        <div className="brand">FolioSpace Library</div>
        <button className={view === "library" ? "active" : ""} onClick={() => setView("library")}>
          {t.library}
        </button>
        <button className={view === "reader" ? "active" : ""} onClick={() => setView("reader")}>
          {t.reader}
        </button>
        <button className={view === "jobs" ? "active" : ""} onClick={() => setView("jobs")}>
          {t.jobs}
        </button>
        <button className={view === "errors" ? "active" : ""} onClick={() => setView("errors")}>
          {t.errors}
        </button>
        {authEnabled && !authRequired && (
          <button className="lockButton" onClick={lockApp}>
            {t.lock}
          </button>
        )}
      </aside>

      <section className="workspace">
        {activeTask && (
          <div className="globalProgress" role="status" aria-live="polite">
            <div className="progressBar" />
            <span>{activeTask}</span>
          </div>
        )}

        <header className="topbar">
          <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t.searchLibrary} />
          <select className="localeSelect" value={locale} onChange={(event) => setLocale(event.target.value as Locale)} aria-label={t.language}>
            <option value="zh">中文</option>
            <option value="zht">繁體中文</option>
            <option value="en">English</option>
            <option value="ja">日本語</option>
            <option value="ko">한국어</option>
          </select>
          <span>{activeScan ? `Scanning: ${scanProgressLabel}` : status}</span>
        </header>

        {activeScan && (
          <section className="scanProgress" role="status" aria-live="polite">
            <div>
              <strong>Scan job #{activeScan.id}</strong>
              <small>{scanProgressLabel}</small>
            </div>
            <div className="scanMeter">
              <div />
            </div>
          </section>
        )}

        {view === "library" && (
          <div className="grid">
            {query.trim().length >= 2 && (
              <section className="globalSearch panel wide" aria-label="Global search results">
                <div className="globalSearchHeader">
                  <div>
                    <h1>{t.searchResults}</h1>
                    <small>{globalSearchLoading ? t.searching : t.matchingVolumes(globalBooks.length)}</small>
                  </div>
                  <button onClick={() => setQuery("")}>{t.clear}</button>
                </div>
                {globalBooks.length > 0 ? (
                  <div className="searchResults">
                    {globalBooks.map((book) => (
                      <button className="searchResult" key={`search-${book.id}`} onClick={() => openBook(book)} title={book.title}>
                        <span className="searchCover">
                          <img src={`/api/books/${book.id}/cover`} alt="" loading="lazy" />
                          <span className="coverBadge">{book.format.toUpperCase()}</span>
                        </span>
                        <span>
                          <strong>{book.title}</strong>
                          <small>{book.collectionTitle || t.library} · {privateMeta(book, t) || t.noPrivateState}</small>
                        </span>
                      </button>
                    ))}
                  </div>
                ) : (
                  <div className="coverEmpty compact">
                    <strong>{globalSearchLoading ? t.searching : t.noMatchingVolumes}</strong>
                    <small>{t.searchHelp}</small>
                  </div>
                )}
              </section>
            )}

            {(continueBooks.length > 0 || favoriteBooks.length > 0 || wantBooks.length > 0 || recentBooks.length > 0) && (
              <section className="homeRows panel wide" aria-label="Reading shortcuts">
                {continueBooks.length > 0 && (
                  <BookShelf
                    title={t.continueReading}
                    subtitle={t.continueSubtitle}
                    books={continueBooks}
                    onOpen={openBook}
                    meta={(book) => continueMeta(book, t)}
                    progress
                  />
                )}
                {favoriteBooks.length > 0 && (
                  <BookShelf
                    title={t.favorites}
                    subtitle={t.favoriteSubtitle}
                    books={favoriteBooks}
                    onOpen={openBook}
                    meta={(book) => privateShelfMeta(book, t)}
                  />
                )}
                {wantBooks.length > 0 && (
                  <BookShelf
                    title={t.wantToRead}
                    subtitle={t.wantSubtitle}
                    books={wantBooks}
                    onOpen={openBook}
                    meta={(book) => privateShelfMeta(book, t)}
                  />
                )}
                {recentBooks.length > 0 && (
                  <BookShelf
                    title={t.recentlyAddedTitle}
                    subtitle={t.recentSubtitle}
                    books={recentBooks}
                    onOpen={openBook}
                    meta={(book) => recentMeta(book, t)}
                  />
                )}
              </section>
            )}

            <section className="panel">
              <h1>{t.libraries}</h1>
              <form className="libraryForm" onSubmit={addLibrary}>
                <input
                  value={newLibraryName}
                  onChange={(event) => setNewLibraryName(event.target.value)}
                  placeholder={t.name}
                />
                <input
                  value={newLibraryPath}
                  onChange={(event) => setNewLibraryPath(event.target.value)}
                  placeholder="/volume2/ComicCenter"
                />
                <button disabled={!newLibraryPath.trim()}>{t.add}</button>
              </form>
              {libraries.map((library) => (
                <div className="row" key={library.id}>
                  <div>
                    <strong>{library.name}</strong>
                    <small>{library.rootPath}</small>
                  </div>
                  <div className="rowActions">
                    <button onClick={() => scan(library)}>{t.scan}</button>
                    <button className="danger" onClick={() => deleteLibrary(library)}>{t.delete}</button>
                  </div>
                </div>
              ))}
            </section>

            <section className="panel">
              <h1>{t.collections}</h1>
              <div className="list">
                {filteredSeries.map((item) => (
                  <button className="listItem" key={item.id} onClick={() => openSeries(item)}>
                    <span>{item.title}</span>
                    <small>
                      {item.directoryPath || "."} · {item.bookCount} volumes
                    </small>
                  </button>
                ))}
              </div>
            </section>

            <section className="coverWall panel wide">
              <div className="coverWallHeader">
                <div>
                  <h1>{selectedSeries ? selectedSeries.title : t.volumeWall}</h1>
                  <small>
                    {selectedSeries
                      ? `${books.length} of ${bookTotal || selectedSeries.bookCount} volumes`
                      : t.selectCollection}
                  </small>
                </div>
                <div className="coverWallTools">
                  {selectedSeries && <span>{selectedSeries.bookCount} indexed</span>}
                  {selectedSeries && (
                    <label>
                      <span>{t.sort}</span>
                      <select value={bookSort} onChange={(event) => setBookSort(event.target.value as BookSort)}>
                        <option value="title">{t.sortTitle}</option>
                        <option value="recently_added">{t.sortRecentlyAdded}</option>
                        <option value="last_read">{t.sortLastRead}</option>
                        <option value="progress">{t.sortProgress}</option>
                        <option value="unread">{t.sortUnread}</option>
                      </select>
                    </label>
                  )}
                </div>
              </div>
              {selectedSeries && books.length > 0 ? (
                <div className="books">
                  {books.map((book) => (
                    <button className="book" key={book.id} onClick={() => openBook(book)} title={book.title}>
                      <span className="coverFrame">
                        <img src={`/api/books/${book.id}/cover`} alt="" loading="lazy" />
                        <span className="coverBadge">{book.format.toUpperCase()}</span>
                      </span>
                      <strong>{book.title}</strong>
                      <small>
                        {t.singleVolume} · {book.pageCount ? t.pageCount(book.pageCount) : t.notAnalyzed}
                      </small>
                      {privateMeta(book, t) && <small className="privateMeta">{privateMeta(book, t)}</small>}
                    </button>
                  ))}
                  <div className="bookLoadMore" ref={bookLoadMoreRef} aria-live="polite">
                    {bookListLoading
                      ? t.loadingMoreVolumes
                      : bookHasMore
                        ? t.scrollToLoadMore
                        : t.volumesLoaded(books.length)}
                  </div>
                </div>
              ) : (
                <div className="coverEmpty">
                  <strong>{selectedSeries ? (bookListLoading ? t.loadingVolumes : t.noMatchingVolumes) : t.noCollectionSelected}</strong>
                  <small>
                    {selectedSeries ? t.clearSearchHint : t.chooseCollectionHint}
                  </small>
                </div>
              )}
            </section>
          </div>
        )}

        {view === "reader" && (
          <section className="reader" ref={readerRef}>
            {selectedBook ? (
              <>
                <div className="readerHeader">
                  <div className="readerTitle">
                    <strong>{selectedBook.title}</strong>
                    <span>
                      {selectedBook.format === "epub" ? "Chapter " : ""}
                      {pageIndex + 1}
                      {selectedBook.format !== "epub" && readerPageMode === "double" && pageIndex + 1 < pages.length
                        ? `-${pageIndex + 2}`
                        : ""} /{" "}
                      {Math.max(pages.length, 1)}
                    </span>
                  </div>
                  <div className="readerToolbar" aria-label="Reader options">
                    {selectedBook.format === "epub" ? (
                      <>
                        <button onClick={() => setEpubTocOpen((value) => !value)}>{t.contents}</button>
                        <div className="segmentedControl" role="group" aria-label="EPUB page mode">
                          <button
                            className={epubPageMode === "single" ? "selected" : ""}
                            onClick={() => {
                              setEpubPageMode("single");
                              setEpubPagePosition(0);
                            }}
                          >
                            {t.single}
                          </button>
                          <button
                            className={epubPageMode === "double" ? "selected" : ""}
                            onClick={() => {
                              setEpubPageMode("double");
                              setEpubPagePosition(0);
                            }}
                          >
                            {t.double}
                          </button>
                        </div>
                        <select value={epubTheme} onChange={(event) => setEpubTheme(event.target.value as EpubTheme)}>
                          <option value="light">{t.light}</option>
                          <option value="sepia">{t.sepia}</option>
                          <option value="dark">{t.dark}</option>
                        </select>
                        <label className="fontControl">
                          <span>{t.text}</span>
                          <input
                            type="range"
                            min="14"
                            max="26"
                            value={epubFontSize}
                            onChange={(event) => setEpubFontSize(Number(event.target.value))}
                          />
                        </label>
                      </>
                    ) : (
                      <div className="segmentedControl" role="group" aria-label="Page mode">
                        <button
                          className={readerPageMode === "single" ? "selected" : ""}
                          onClick={() => setReaderPageMode("single")}
                        >
                          {t.single}
                        </button>
                        <button
                          className={readerPageMode === "double" ? "selected" : ""}
                          onClick={() => setReaderPageMode("double")}
                        >
                          {t.double}
                        </button>
                      </div>
                    )}
                    <button onClick={toggleReaderFullscreen}>{readerFullscreen ? t.exitFullscreen : t.fullscreen}</button>
                  </div>
                </div>
                <div className="readerStateBar">
                  <label>
                    <span>{t.privateStatus}</span>
                    <select
                      value={privateDraft.status}
                      onChange={(event) => setPrivateDraft((draft) => ({ ...draft, status: event.target.value }))}
                    >
                      <option value="">{t.none}</option>
                      <option value="want">{t.want}</option>
                      <option value="reading">{t.reading}</option>
                      <option value="finished">{t.finished}</option>
                      <option value="dropped">{t.dropped}</option>
                    </select>
                  </label>
                  <label className="inlineCheck">
                    <input
                      type="checkbox"
                      checked={privateDraft.favorite}
                      onChange={(event) => setPrivateDraft((draft) => ({ ...draft, favorite: event.target.checked }))}
                    />
                    {t.favorite}
                  </label>
                  <label>
                    <span>{t.rating}</span>
                    <input
                      type="number"
                      min="0"
                      max="5"
                      value={privateDraft.rating}
                      onChange={(event) => setPrivateDraft((draft) => ({ ...draft, rating: Number(event.target.value) }))}
                    />
                  </label>
                  <label className="wideStateField">
                    <span>{t.tags}</span>
                    <input
                      value={privateDraft.tags.join(", ")}
                      onChange={(event) => setPrivateDraft((draft) => ({ ...draft, tags: event.target.value.split(",") }))}
                      placeholder={t.tagsPlaceholder}
                    />
                  </label>
                  <label className="wideStateField">
                    <span>{t.note}</span>
                    <input
                      value={privateDraft.summary}
                      onChange={(event) => setPrivateDraft((draft) => ({ ...draft, summary: event.target.value }))}
                      placeholder={t.privateNote}
                    />
                  </label>
                  <button onClick={savePrivateState} disabled={privateSaving}>
                    {privateSaving ? t.saving : t.save}
                  </button>
                </div>
                <div
                  className={`pageStage ${selectedBook.format === "epub" ? "epub" : readerPageMode}`}
                  onMouseDownCapture={handleReaderMouseDown}
                  onTouchStartCapture={handleReaderTouchStart}
                >
                  <button className="pageEdge previous" aria-label="Previous page" onClick={goReaderPrevious} />
                  <button className="pageEdge next" aria-label="Next page" onClick={goReaderNext} />
                  {readerLoadState === "loading" && selectedBook.format !== "epub" && pageIndex !== displayedPageIndex && (
                    <div className="pageLoading floating" role="status" aria-live="polite">
                      <div className="pageProgress"><div /></div>
                      <span>{t.loadingPage(pageIndex + 1)}</span>
                    </div>
                  )}
                  {readerLoadState === "error" && (
                    <div className="pageLoading errorState" role="alert">
                      <strong>{t.pageFailed(pageIndex + 1)}</strong>
                      <button onClick={() => setReaderRetryKey((value) => value + 1)}>{t.retry}</button>
                    </div>
                  )}
                  {selectedBook.format === "epub" ? (
                    <>
                      {epubTocOpen && epubManifest && (
                        <div className="epubToc">
                          {((epubManifest.toc?.length ?? 0) > 0
                            ? epubManifest.toc
                            : epubManifest.spine.map((item) => ({
                                label: `Chapter ${item.index + 1}`,
                                href: item.href,
                                index: item.index,
                              }))
                          ).map((item) => (
                            <button
                              className={item.index === pageIndex ? "active" : ""}
                              key={`${item.index}-${item.href}`}
                              onClick={() => jumpToEpubChapter(item.index)}
                            >
                              {item.label}
                            </button>
                          ))}
                        </div>
                      )}
                      <EpubFrame
                        book={selectedBook}
                        manifest={epubManifest}
                        pageIndex={pageIndex}
                        pageMode={epubPageMode}
                        fontSize={epubFontSize}
                        theme={epubTheme}
                        pagePosition={epubPagePosition}
                        onMetrics={(count, position) => {
                          const restoredPosition = epubRestorePosition.current;
                          if (restoredPosition !== null) {
                            if (count > restoredPosition) {
                              epubRestorePosition.current = null;
                            }
                            setEpubPageCount(Math.max(count, restoredPosition + 1));
                            setEpubPagePosition(Math.max(0, restoredPosition));
                            return;
                          }
                          setEpubPageCount(count);
                          setEpubPagePosition(position);
                        }}
                      />
                    </>
                  ) : (
                    <div className="pageSpread" aria-live="polite">
                      {visiblePageIndexes(displayedPageIndex, pages.length, readerPageMode).map((visibleIndex) => (
                        <img
                          key={`${selectedBook.id}-${visibleIndex}`}
                          src={`/api/books/${selectedBook.id}/pages/${visibleIndex}`}
                          alt={pages[visibleIndex]?.name ?? ""}
                          draggable={false}
                        />
                      ))}
                    </div>
                  )}
                </div>
                <div className="readerControls">
                  <button onClick={goReaderPrevious}>{t.previous}</button>
                  {selectedBook.format === "epub" && (
                    <span className="epubProgress">
                      {t.pageLabel(Math.min(epubPagePosition + 1, epubPageCount), epubPageCount)}
                    </span>
                  )}
                  <input
                    type="range"
                    min="0"
                    max={Math.max(0, pages.length - 1)}
                    value={pageIndex}
                    onChange={(event) => setReaderPage(selectedBook, Number(event.target.value))}
                  />
                  <button onClick={goReaderNext}>{t.next}</button>
                </div>
              </>
            ) : (
              <div className="empty">{t.selectBook}</div>
            )}
          </section>
        )}

        {view === "jobs" && (
          <div className="jobLayout">
            <section className="panel">
              <h1>Jobs</h1>
              {jobs.map((job) => (
                <button className="jobRow" key={job.id} onClick={() => openJob(job)}>
                  <strong>Job #{job.id}</strong>
                  <small>
                    {job.status} · {job.discoveredFiles} discovered · {job.indexedFiles} indexed · {job.skippedFiles} skipped ·{" "}
                    {job.errorCount} errors
                  </small>
                  {job.currentPath && <span>{job.currentPath}</span>}
                </button>
              ))}
            </section>

            <section className="panel">
              <h1>{selectedJobLatest ? `Job #${selectedJobLatest.id}` : "Job Detail"}</h1>
              {selectedJobLatest ? (
                <div className="jobDetail">
                  <div className="statGrid">
                    <span>Status<strong>{selectedJobLatest.status}</strong></span>
                    <span>Discovered<strong>{selectedJobLatest.discoveredFiles}</strong></span>
                    <span>Indexed<strong>{selectedJobLatest.indexedFiles}</strong></span>
                    <span>Skipped<strong>{selectedJobLatest.skippedFiles}</strong></span>
                    <span>Errors<strong>{selectedJobLatest.errorCount}</strong></span>
                    <span>Elapsed<strong>{formatElapsed(selectedJobLatest)}</strong></span>
                  </div>
                  {selectedJobLatest.currentPath && <code className="currentPath">{selectedJobLatest.currentPath}</code>}
                  <h2>Events</h2>
                  <div className="eventList">
                    {jobEvents.map((event) => (
                      <div className={`event ${event.level}`} key={event.id}>
                        <code>{event.level}</code>
                        <span>{event.message}</span>
                      </div>
                    ))}
                  </div>
                  <h2>Errors</h2>
                  <div className="table compact">
                    {jobErrors.map((item) => (
                      <div className="errorRow" key={item.id}>
                        <code>{item.code}</code>
                        <span>{item.path}</span>
                        <small>{item.message}</small>
                      </div>
                    ))}
                  </div>
                </div>
              ) : (
                <div className="empty">Select a job to inspect events and errors.</div>
              )}
            </section>
          </div>
        )}

        {view === "errors" && (
          <section className="panel">
            <h1>{t.errors}</h1>
            <div className="table">
              {errors.map((item) => (
                <div className="errorRow" key={item.id}>
                  <code>{item.code}</code>
                  <span>{item.path}</span>
                  <small>{item.message}</small>
                </div>
              ))}
            </div>
          </section>
        )}
      </section>
      {(!authChecked || authRequired) && (
        <div className="authOverlay" role="dialog" aria-modal="true" aria-labelledby="auth-title">
          <form className="authPanel" onSubmit={submitAuth}>
            <div>
              <h1 id="auth-title">FolioSpace Library</h1>
              <small>{authChecked ? "Enter the NAS access token." : "Checking access settings."}</small>
            </div>
            {authRequired && (
              <>
                <input
                  autoFocus
                  type="password"
                  value={authInput}
                  onChange={(event) => setAuthInput(event.target.value)}
                  placeholder="Access token"
                />
                {authError && <span className="authError">{authError}</span>}
                <button disabled={!authInput.trim()}>Unlock</button>
              </>
            )}
          </form>
        </div>
      )}
    </main>
  );
}

function BookShelf({
  title,
  subtitle,
  books,
  onOpen,
  meta,
  progress = false,
}: {
  title: string;
  subtitle: string;
  books: Book[];
  onOpen: (book: Book) => void;
  meta: (book: Book) => string;
  progress?: boolean;
}) {
  return (
    <div className="bookShelf">
      <div className="bookShelfHeader">
        <div>
          <h1>{title}</h1>
          <small>{subtitle}</small>
        </div>
      </div>
      <div className="shelfScroller">
        {books.map((book) => (
          <button className="shelfBook" key={`${title}-${book.id}`} onClick={() => onOpen(book)} title={book.title}>
            <span className="shelfCover">
              <img src={`/api/books/${book.id}/cover`} alt="" loading="lazy" />
              <span className="coverBadge">{book.format.toUpperCase()}</span>
            </span>
            <span>
              <strong>{book.title}</strong>
              <small>{meta(book)}</small>
              {progress && (
                <span className="shelfProgress" aria-label={`${readingProgress(book)} percent read`}>
                  <span style={{ width: `${readingProgress(book)}%` }} />
                </span>
              )}
            </span>
          </button>
        ))}
      </div>
    </div>
  );
}

function EpubFrame({
  book,
  manifest,
  pageIndex,
  pageMode,
  fontSize,
  theme,
  pagePosition,
  onMetrics,
}: {
  book: Book;
  manifest: EpubManifest | null;
  pageIndex: number;
  pageMode: ReaderPageMode;
  fontSize: number;
  theme: EpubTheme;
  pagePosition: number;
  onMetrics: (pageCount: number, pagePosition: number) => void;
}) {
  const iframeRef = useRef<HTMLIFrameElement | null>(null);
  const spineItem = manifest?.spine[pageIndex] ?? null;

  useEffect(() => {
    applyEpubLayout();
    const timer = window.setTimeout(applyEpubLayout, 80);
    return () => window.clearTimeout(timer);
  }, [pageMode, fontSize, theme, pagePosition, spineItem?.href]);

  function applyEpubLayout() {
    const frame = iframeRef.current;
    const doc = frame?.contentDocument;
    const win = frame?.contentWindow;
    if (!frame || !doc || !win || !spineItem) return;

    const viewportWidth = Math.max(320, frame.clientWidth);
    const viewportHeight = Math.max(320, frame.clientHeight);
    const isDoublePage = pageMode === "double";
    const horizontalPadding = isDoublePage ? 34 : 52;
    const verticalPadding = isDoublePage ? 34 : 42;
    const gap = isDoublePage
      ? Math.min(34, Math.max(22, Math.round(viewportWidth * 0.022)))
      : horizontalPadding * 2;
    const dividerWidth = isDoublePage ? 2 : 0;
    const readableWidth = Math.max(260, viewportWidth - horizontalPadding * 2);
    const columnWidth = isDoublePage
      ? Math.max(220, Math.floor((readableWidth - gap) / 2))
      : readableWidth;
    const pageWidth = isDoublePage ? (columnWidth + gap) * 2 : columnWidth + gap;
    const palette = epubThemePalette(theme);
    const bodyScrollWidth = Math.max(doc.body.scrollWidth, doc.documentElement.scrollWidth, viewportWidth);
    const estimatedPageCount = Math.max(1, Math.ceil(bodyScrollWidth / pageWidth));
    const estimatedPosition = Math.max(0, Math.min(pagePosition, estimatedPageCount - 1));
    const style = doc.getElementById("foliospace-epub-style") ?? doc.createElement("style");
    style.id = "foliospace-epub-style";
    style.textContent = `
      html {
        width: ${viewportWidth}px !important;
        min-width: ${viewportWidth}px !important;
        height: ${viewportHeight}px !important;
        margin: 0 !important;
        overflow: hidden !important;
        background: ${palette.background} !important;
        color: ${palette.text} !important;
      }
      body {
        width: ${viewportWidth}px !important;
        min-width: ${viewportWidth}px !important;
        height: ${viewportHeight}px !important;
        margin: 0 !important;
        overflow: visible !important;
        background: ${palette.background} !important;
        color: ${palette.text} !important;
      }
      body {
        box-sizing: border-box !important;
        padding: ${verticalPadding}px ${horizontalPadding}px !important;
        font-size: ${fontSize}px !important;
        line-height: 1.72 !important;
        column-width: ${columnWidth}px !important;
        column-gap: ${gap}px !important;
        column-fill: auto !important;
        position: relative !important;
        transform-origin: top left !important;
        transform: translateX(-${estimatedPosition * pageWidth}px) !important;
        transition: transform 140ms ease !important;
      }
      html::before {
        content: "" !important;
        display: block !important;
        position: fixed !important;
        top: 0 !important;
        right: 0 !important;
        bottom: 0 !important;
        width: ${horizontalPadding}px !important;
        background: ${palette.background} !important;
        box-shadow: -${viewportWidth - horizontalPadding}px 0 0 ${palette.background} !important;
        pointer-events: none !important;
        z-index: 2147483646 !important;
      }
      html::after {
        content: "" !important;
        display: ${isDoublePage ? "block" : "none"} !important;
        position: fixed !important;
        top: ${verticalPadding}px !important;
        bottom: ${verticalPadding}px !important;
        left: 50% !important;
        width: ${dividerWidth}px !important;
        margin-left: -${Math.floor(dividerWidth / 2)}px !important;
        background: ${palette.divider} !important;
        box-shadow: 0 0 10px ${palette.gutter} !important;
        pointer-events: none !important;
        z-index: 2147483647 !important;
      }
      body, p, li {
        color: ${palette.text} !important;
      }
      a {
        color: ${palette.link} !important;
      }
      img, svg {
        max-width: 100% !important;
        height: auto !important;
      }
    `;
    if (!style.parentElement) {
      doc.head.appendChild(style);
    }

    window.requestAnimationFrame(() => {
      const pageCount = Math.max(1, Math.ceil(Math.max(doc.body.scrollWidth, doc.documentElement.scrollWidth) / pageWidth));
      const nextPosition = Math.max(0, Math.min(pagePosition, pageCount - 1));
      doc.body.style.transform = `translateX(-${nextPosition * pageWidth}px)`;
      win.scrollTo({ left: 0, top: 0, behavior: "auto" });
      onMetrics(pageCount, nextPosition);
    });
  }

  if (!manifest || !spineItem) {
    return (
      <div className="epubEmpty" role="status">
        Loading EPUB
      </div>
    );
  }

  return (
    <iframe
      ref={iframeRef}
      key={`${book.id}-${spineItem.href}`}
      className="epubFrame"
      title={`${book.title} chapter ${pageIndex + 1}`}
      sandbox="allow-same-origin"
      src={`/api/books/${book.id}/epub/resources/${encodeResourcePath(spineItem.href)}`}
      onLoad={applyEpubLayout}
    />
  );
}

function epubThemePalette(theme: EpubTheme) {
  if (theme === "dark") {
    return {
      background: "#161b1d",
      text: "#edf4f6",
      link: "#85d5e3",
      gutter: "#0f1315",
      divider: "rgba(255, 255, 255, 0.16)",
    };
  }
  if (theme === "sepia") {
    return {
      background: "#f4ecd9",
      text: "#33291c",
      link: "#7b5a24",
      gutter: "#e5dac4",
      divider: "rgba(76, 55, 31, 0.18)",
    };
  }
  return {
    background: "#ffffff",
    text: "#20282c",
    link: "#337f92",
    gutter: "#edf2f4",
    divider: "rgba(31, 42, 46, 0.16)",
  };
}

function readEpubLocator(locator: string) {
  const value = Number.parseInt(locator, 10);
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, value);
}

function encodeResourcePath(value: string) {
  return value
    .split("/")
    .map((part) => encodeURIComponent(part))
    .join("/");
}

function compareBooks(left: Book, right: Book, sort: BookSort) {
  if (sort === "recently_added") {
    return compareDatesDesc(left.addedAt, right.addedAt) || left.title.localeCompare(right.title);
  }
  if (sort === "last_read") {
    return compareDatesDesc(left.lastReadAt, right.lastReadAt) || left.title.localeCompare(right.title);
  }
  if (sort === "progress") {
    return right.progressFraction - left.progressFraction || left.title.localeCompare(right.title);
  }
  if (sort === "unread") {
    return readRank(left) - readRank(right) || left.title.localeCompare(right.title);
  }
  return left.title.localeCompare(right.title);
}

function readRank(book: Book) {
  if (book.progressFraction <= 0 && book.currentPage <= 0) return 0;
  if (book.progressFraction >= 0.98) return 2;
  return 1;
}

function compareDatesDesc(left: string, right: string) {
  return dateValue(right) - dateValue(left);
}

function dateValue(value: string) {
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

type Translation = typeof translations.en;

const translations = {
  zh: {
    language: "语言",
    library: "书库",
    reader: "阅读器",
    jobs: "任务",
    errors: "错误",
    lock: "锁定",
    searchLibrary: "搜索书库",
    searchResults: "搜索结果",
    searching: "搜索中",
    matchingVolumes: (count: number) => `${count} 本匹配`,
    clear: "清除",
    noPrivateState: "未标记",
    searchHelp: "会搜索标题、作品集、格式、标签和备注。",
    continueReading: "继续阅读",
    favorites: "收藏",
    wantToRead: "想读",
    recentlyAddedTitle: "最近添加",
    continueSubtitle: "点击后直接回到上次阅读位置",
    favoriteSubtitle: "你的私人收藏",
    wantSubtitle: "稍后再读",
    recentSubtitle: "最近入库",
    libraries: "书库目录",
    name: "名称",
    add: "添加",
    scan: "扫描",
    delete: "删除",
    collections: "作品集",
    volumeWall: "封面墙",
    selectCollection: "选择一个作品集浏览单行本",
    sort: "排序",
    sortTitle: "标题",
    sortRecentlyAdded: "最近添加",
    sortLastRead: "最近阅读",
    sortProgress: "进度",
    sortUnread: "未读优先",
    singleVolume: "单行本",
    pageCount: (count: number) => `${count} 页`,
    notAnalyzed: "未分析",
    loadingMoreVolumes: "正在加载更多",
    scrollToLoadMore: "滚动加载更多",
    volumesLoaded: (count: number) => `已加载 ${count} 本`,
    loadingVolumes: "正在加载",
    noMatchingVolumes: "没有匹配条目",
    noCollectionSelected: "未选择作品集",
    clearSearchHint: "清空搜索框显示全部条目。",
    chooseCollectionHint: "从上方列表选择一个作品集。",
    contents: "目录",
    single: "单页",
    double: "双页",
    light: "浅色",
    sepia: "米色",
    dark: "深色",
    text: "字号",
    fullscreen: "全屏",
    exitFullscreen: "退出全屏",
    privateStatus: "状态",
    none: "无",
    want: "想读",
    reading: "在读",
    finished: "已读",
    dropped: "搁置",
    favorite: "收藏",
    rating: "评分",
    tags: "标签",
    tagsPlaceholder: "标签, 标签",
    note: "备注",
    privateNote: "私人备注",
    saving: "保存中",
    save: "保存",
    loadingPage: (page: number) => `正在加载第 ${page} 页`,
    pageFailed: (page: number) => `第 ${page} 页加载失败`,
    retry: "重试",
    previous: "上一页",
    next: "下一页",
    pageLabel: (current: number, total: number) => `第 ${current} / ${total} 页`,
    selectBook: "选择一本书开始阅读。",
    statusFavorite: "收藏",
    statusWant: "想读",
    statusReading: "在读",
    statusFinished: "已读",
    statusDropped: "搁置",
    lastRead: (value: string) => `上次阅读：${value}`,
    today: "今天",
    yesterday: "昨天",
    daysAgo: (days: number) => `${days} 天前`,
    recentlyAdded: "最近添加",
    epubChapter: (chapter: number) => `EPUB 第 ${chapter} 章`,
    comicPage: (page: number) => `漫画第 ${page} 页`,
    percentRead: (percent: number) => `${percent}%`,
  },
  zht: {
    language: "語言",
    library: "書庫",
    reader: "閱讀器",
    jobs: "任務",
    errors: "錯誤",
    lock: "鎖定",
    searchLibrary: "搜尋書庫",
    searchResults: "搜尋結果",
    searching: "搜尋中",
    matchingVolumes: (count: number) => `${count} 本符合`,
    clear: "清除",
    noPrivateState: "未標記",
    searchHelp: "會搜尋標題、作品集、格式、標籤和備註。",
    continueReading: "繼續閱讀",
    favorites: "收藏",
    wantToRead: "想讀",
    recentlyAddedTitle: "最近新增",
    continueSubtitle: "點擊後直接回到上次閱讀位置",
    favoriteSubtitle: "你的私人收藏",
    wantSubtitle: "稍後再讀",
    recentSubtitle: "最近入庫",
    libraries: "書庫目錄",
    name: "名稱",
    add: "新增",
    scan: "掃描",
    delete: "刪除",
    collections: "作品集",
    volumeWall: "封面牆",
    selectCollection: "選擇一個作品集瀏覽單行本",
    sort: "排序",
    sortTitle: "標題",
    sortRecentlyAdded: "最近新增",
    sortLastRead: "最近閱讀",
    sortProgress: "進度",
    sortUnread: "未讀優先",
    singleVolume: "單行本",
    pageCount: (count: number) => `${count} 頁`,
    notAnalyzed: "未分析",
    loadingMoreVolumes: "正在載入更多",
    scrollToLoadMore: "捲動載入更多",
    volumesLoaded: (count: number) => `已載入 ${count} 本`,
    loadingVolumes: "正在載入",
    noMatchingVolumes: "沒有符合項目",
    noCollectionSelected: "未選擇作品集",
    clearSearchHint: "清空搜尋框顯示全部項目。",
    chooseCollectionHint: "從上方列表選擇一個作品集。",
    contents: "目錄",
    single: "單頁",
    double: "雙頁",
    light: "淺色",
    sepia: "米色",
    dark: "深色",
    text: "字號",
    fullscreen: "全螢幕",
    exitFullscreen: "退出全螢幕",
    privateStatus: "狀態",
    none: "無",
    want: "想讀",
    reading: "在讀",
    finished: "已讀",
    dropped: "擱置",
    favorite: "收藏",
    rating: "評分",
    tags: "標籤",
    tagsPlaceholder: "標籤, 標籤",
    note: "備註",
    privateNote: "私人備註",
    saving: "儲存中",
    save: "儲存",
    loadingPage: (page: number) => `正在載入第 ${page} 頁`,
    pageFailed: (page: number) => `第 ${page} 頁載入失敗`,
    retry: "重試",
    previous: "上一頁",
    next: "下一頁",
    pageLabel: (current: number, total: number) => `第 ${current} / ${total} 頁`,
    selectBook: "選擇一本書開始閱讀。",
    statusFavorite: "收藏",
    statusWant: "想讀",
    statusReading: "在讀",
    statusFinished: "已讀",
    statusDropped: "擱置",
    lastRead: (value: string) => `上次閱讀：${value}`,
    today: "今天",
    yesterday: "昨天",
    daysAgo: (days: number) => `${days} 天前`,
    recentlyAdded: "最近新增",
    epubChapter: (chapter: number) => `EPUB 第 ${chapter} 章`,
    comicPage: (page: number) => `漫畫第 ${page} 頁`,
    percentRead: (percent: number) => `${percent}%`,
  },
  en: {
    language: "Language",
    library: "Library",
    reader: "Reader",
    jobs: "Jobs",
    errors: "Errors",
    lock: "Lock",
    searchLibrary: "Search library",
    searchResults: "Search Results",
    searching: "Searching",
    matchingVolumes: (count: number) => `${count} matching volumes`,
    clear: "Clear",
    noPrivateState: "No private state",
    searchHelp: "Search checks titles, collections, formats, tags, and notes.",
    continueReading: "Continue Reading",
    favorites: "Favorites",
    wantToRead: "Want to Read",
    recentlyAddedTitle: "Recently Added",
    continueSubtitle: "One click resumes at your saved page",
    favoriteSubtitle: "Private picks",
    wantSubtitle: "Queued for later",
    recentSubtitle: "Newest indexed volumes",
    libraries: "Libraries",
    name: "Name",
    add: "Add",
    scan: "Scan",
    delete: "Delete",
    collections: "Collections",
    volumeWall: "Volume Wall",
    selectCollection: "Select a collection to browse its single volumes",
    sort: "Sort",
    sortTitle: "Title",
    sortRecentlyAdded: "Recently added",
    sortLastRead: "Last read",
    sortProgress: "Progress",
    sortUnread: "Unread first",
    singleVolume: "Single volume",
    pageCount: (count: number) => `${count} pages`,
    notAnalyzed: "Not analyzed",
    loadingMoreVolumes: "Loading more volumes...",
    scrollToLoadMore: "Scroll to load more",
    volumesLoaded: (count: number) => `${count} volumes loaded`,
    loadingVolumes: "Loading volumes",
    noMatchingVolumes: "No matching volumes",
    noCollectionSelected: "No collection selected",
    clearSearchHint: "Clear the search field to show all volumes.",
    chooseCollectionHint: "Choose a collection from the list above.",
    contents: "Contents",
    single: "Single",
    double: "Double",
    light: "Light",
    sepia: "Sepia",
    dark: "Dark",
    text: "Text",
    fullscreen: "Fullscreen",
    exitFullscreen: "Exit Fullscreen",
    privateStatus: "Status",
    none: "None",
    want: "Want",
    reading: "Reading",
    finished: "Finished",
    dropped: "Dropped",
    favorite: "Favorite",
    rating: "Rating",
    tags: "Tags",
    tagsPlaceholder: "tag, tag",
    note: "Note",
    privateNote: "Private note",
    saving: "Saving",
    save: "Save",
    loadingPage: (page: number) => `Loading page ${page}`,
    pageFailed: (page: number) => `Page ${page} failed to load`,
    retry: "Retry",
    previous: "Previous",
    next: "Next",
    pageLabel: (current: number, total: number) => `Page ${current} / ${total}`,
    selectBook: "Select a book to start reading.",
    statusFavorite: "Favorite",
    statusWant: "Want",
    statusReading: "Reading",
    statusFinished: "Finished",
    statusDropped: "Dropped",
    lastRead: (value: string) => `Last read ${value.toLowerCase()}`,
    today: "Today",
    yesterday: "Yesterday",
    daysAgo: (days: number) => `${days} days ago`,
    recentlyAdded: "Recently added",
    epubChapter: (chapter: number) => `EPUB chapter ${chapter}`,
    comicPage: (page: number) => `Comic page ${page}`,
    percentRead: (percent: number) => `${percent}%`,
  },
  ja: {
    language: "言語",
    library: "ライブラリ",
    reader: "リーダー",
    jobs: "ジョブ",
    errors: "エラー",
    lock: "ロック",
    searchLibrary: "ライブラリを検索",
    searchResults: "検索結果",
    searching: "検索中",
    matchingVolumes: (count: number) => `${count} 件`,
    clear: "クリア",
    noPrivateState: "未設定",
    searchHelp: "タイトル、コレクション、形式、タグ、メモを検索します。",
    continueReading: "続きを読む",
    favorites: "お気に入り",
    wantToRead: "読みたい",
    recentlyAddedTitle: "最近追加",
    continueSubtitle: "保存した位置からすぐ再開",
    favoriteSubtitle: "お気に入り",
    wantSubtitle: "あとで読む",
    recentSubtitle: "最近追加",
    libraries: "ライブラリ",
    name: "名前",
    add: "追加",
    scan: "スキャン",
    delete: "削除",
    collections: "コレクション",
    volumeWall: "カバー一覧",
    selectCollection: "コレクションを選んで単巻を表示",
    sort: "並び替え",
    sortTitle: "タイトル",
    sortRecentlyAdded: "最近追加",
    sortLastRead: "最近読んだ",
    sortProgress: "進捗",
    sortUnread: "未読優先",
    singleVolume: "単巻",
    pageCount: (count: number) => `${count} ページ`,
    notAnalyzed: "未解析",
    loadingMoreVolumes: "さらに読み込み中",
    scrollToLoadMore: "スクロールで追加読み込み",
    volumesLoaded: (count: number) => `${count} 件読み込み済み`,
    loadingVolumes: "読み込み中",
    noMatchingVolumes: "一致する項目なし",
    noCollectionSelected: "コレクション未選択",
    clearSearchHint: "検索欄をクリアすると全件表示します。",
    chooseCollectionHint: "上のリストからコレクションを選んでください。",
    contents: "目次",
    single: "単ページ",
    double: "見開き",
    light: "ライト",
    sepia: "セピア",
    dark: "ダーク",
    text: "文字",
    fullscreen: "全画面",
    exitFullscreen: "全画面終了",
    privateStatus: "状態",
    none: "なし",
    want: "読みたい",
    reading: "読書中",
    finished: "読了",
    dropped: "保留",
    favorite: "お気に入り",
    rating: "評価",
    tags: "タグ",
    tagsPlaceholder: "タグ, タグ",
    note: "メモ",
    privateNote: "個人メモ",
    saving: "保存中",
    save: "保存",
    loadingPage: (page: number) => `${page} ページを読み込み中`,
    pageFailed: (page: number) => `${page} ページの読み込み失敗`,
    retry: "再試行",
    previous: "前へ",
    next: "次へ",
    pageLabel: (current: number, total: number) => `${current} / ${total} ページ`,
    selectBook: "本を選んで読み始めます。",
    statusFavorite: "お気に入り",
    statusWant: "読みたい",
    statusReading: "読書中",
    statusFinished: "読了",
    statusDropped: "保留",
    lastRead: (value: string) => `前回：${value}`,
    today: "今日",
    yesterday: "昨日",
    daysAgo: (days: number) => `${days}日前`,
    recentlyAdded: "最近追加",
    epubChapter: (chapter: number) => `EPUB ${chapter}章`,
    comicPage: (page: number) => `漫画 ${page}ページ`,
    percentRead: (percent: number) => `${percent}%`,
  },
  ko: {
    language: "언어",
    library: "라이브러리",
    reader: "리더",
    jobs: "작업",
    errors: "오류",
    lock: "잠금",
    searchLibrary: "라이브러리 검색",
    searchResults: "검색 결과",
    searching: "검색 중",
    matchingVolumes: (count: number) => `${count}권 일치`,
    clear: "지우기",
    noPrivateState: "표시 없음",
    searchHelp: "제목, 컬렉션, 형식, 태그, 메모를 검색합니다.",
    continueReading: "이어 읽기",
    favorites: "즐겨찾기",
    wantToRead: "읽고 싶음",
    recentlyAddedTitle: "최근 추가",
    continueSubtitle: "저장된 위치에서 바로 이어서 읽기",
    favoriteSubtitle: "개인 즐겨찾기",
    wantSubtitle: "나중에 읽기",
    recentSubtitle: "최근 인덱싱된 항목",
    libraries: "라이브러리",
    name: "이름",
    add: "추가",
    scan: "스캔",
    delete: "삭제",
    collections: "컬렉션",
    volumeWall: "커버 월",
    selectCollection: "컬렉션을 선택해 단행본을 봅니다",
    sort: "정렬",
    sortTitle: "제목",
    sortRecentlyAdded: "최근 추가",
    sortLastRead: "최근 읽음",
    sortProgress: "진행률",
    sortUnread: "미독 우선",
    singleVolume: "단행본",
    pageCount: (count: number) => `${count}페이지`,
    notAnalyzed: "분석 안 됨",
    loadingMoreVolumes: "더 불러오는 중",
    scrollToLoadMore: "스크롤해서 더 불러오기",
    volumesLoaded: (count: number) => `${count}권 불러옴`,
    loadingVolumes: "불러오는 중",
    noMatchingVolumes: "일치하는 항목 없음",
    noCollectionSelected: "컬렉션이 선택되지 않음",
    clearSearchHint: "검색어를 지우면 모든 항목을 표시합니다.",
    chooseCollectionHint: "위 목록에서 컬렉션을 선택하세요.",
    contents: "목차",
    single: "한 페이지",
    double: "두 페이지",
    light: "라이트",
    sepia: "세피아",
    dark: "다크",
    text: "글자",
    fullscreen: "전체 화면",
    exitFullscreen: "전체 화면 종료",
    privateStatus: "상태",
    none: "없음",
    want: "읽고 싶음",
    reading: "읽는 중",
    finished: "완독",
    dropped: "보류",
    favorite: "즐겨찾기",
    rating: "평점",
    tags: "태그",
    tagsPlaceholder: "태그, 태그",
    note: "메모",
    privateNote: "개인 메모",
    saving: "저장 중",
    save: "저장",
    loadingPage: (page: number) => `${page}페이지 불러오는 중`,
    pageFailed: (page: number) => `${page}페이지 불러오기 실패`,
    retry: "다시 시도",
    previous: "이전",
    next: "다음",
    pageLabel: (current: number, total: number) => `${current} / ${total}페이지`,
    selectBook: "읽을 책을 선택하세요.",
    statusFavorite: "즐겨찾기",
    statusWant: "읽고 싶음",
    statusReading: "읽는 중",
    statusFinished: "완독",
    statusDropped: "보류",
    lastRead: (value: string) => `마지막 읽음: ${value}`,
    today: "오늘",
    yesterday: "어제",
    daysAgo: (days: number) => `${days}일 전`,
    recentlyAdded: "최근 추가",
    epubChapter: (chapter: number) => `EPUB ${chapter}장`,
    comicPage: (page: number) => `만화 ${page}페이지`,
    percentRead: (percent: number) => `${percent}%`,
  },
};

function readLocale(): Locale {
  const value = window.localStorage.getItem("foliospace_locale");
  return value === "en" || value === "ja" || value === "ko" || value === "zh" || value === "zht" ? value : "zh";
}

function emptyPrivateState(): BookPrivateState {
  return { status: "", favorite: false, rating: 0, tags: [], summary: "" };
}

function privateStateFromBook(book: Book): BookPrivateState {
  return {
    status: book.privateStatus ?? "",
    favorite: Boolean(book.favorite),
    rating: book.rating ?? 0,
    tags: book.tags ?? [],
    summary: book.summary ?? "",
  };
}

function normalizeDraftTags(tags: string[]) {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const raw of tags) {
    const tag = raw.trim();
    if (!tag || seen.has(tag)) continue;
    seen.add(tag);
    out.push(tag);
  }
  return out;
}

function replaceBook(items: Book[], updatedBook: Book) {
  return items.map((book) => (book.id === updatedBook.id ? updatedBook : book));
}

function mergeShelfBook(items: Book[], updatedBook: Book, include: (book: Book) => boolean) {
  const withoutBook = items.filter((book) => book.id !== updatedBook.id);
  if (!include(updatedBook)) return withoutBook;
  return [updatedBook, ...withoutBook].slice(0, 12);
}

function privateMeta(book: Book, t: Translation) {
  const parts: string[] = [];
  if (book.favorite) parts.push(t.statusFavorite);
  if (book.privateStatus) parts.push(statusLabel(book.privateStatus, t));
  if (book.rating > 0) parts.push(`${book.rating}/5`);
  if (book.tags?.length) parts.push(book.tags.slice(0, 2).join(", "));
  return parts.join(" · ");
}

function statusLabel(value: string, t: Translation) {
  if (value === "want") return t.statusWant;
  if (value === "reading") return t.statusReading;
  if (value === "finished") return t.statusFinished;
  if (value === "dropped") return t.statusDropped;
  return value;
}

function continueMeta(book: Book, t: Translation) {
  const location = book.format === "epub" ? t.epubChapter(book.currentPage + 1) : t.comicPage(book.currentPage + 1);
  const lastRead = t.lastRead(book.lastReadAt ? formatRelativeDate(book.lastReadAt, t) : t.recentlyAdded);
  return `${t.percentRead(readingProgress(book))} · ${location} · ${lastRead}${book.collectionTitle ? ` · ${book.collectionTitle}` : ""}`;
}

function readingProgress(book: Book) {
  return Math.max(0, Math.min(100, Math.round((book.progressFraction || 0) * 100)));
}

function privateShelfMeta(book: Book, t: Translation) {
  const meta = privateMeta(book, t);
  const location = book.collectionTitle ? book.collectionTitle : t.library;
  return meta ? `${meta} · ${location}` : location;
}

function recentMeta(book: Book, t: Translation) {
  const added = formatRelativeDate(book.addedAt, t);
  return `${added}${book.collectionTitle ? ` · ${book.collectionTitle}` : ""}`;
}

function formatRelativeDate(value: string, t: Translation) {
  const parsed = dateValue(value);
  if (!parsed) return t.recentlyAdded;
  const days = Math.floor((Date.now() - parsed) / 86_400_000);
  if (days <= 0) return t.today;
  if (days === 1) return t.yesterday;
  if (days < 30) return t.daysAgo(days);
  return new Date(parsed).toLocaleDateString();
}

function isUnauthorized(error: unknown) {
  return error instanceof Error && error.message === "Unauthorized";
}

function formatElapsed(job: ScanJob) {
  const started = new Date(job.startedAt).getTime();
  const finished = job.finishedAt ? new Date(job.finishedAt).getTime() : Date.now();
  if (!Number.isFinite(started) || !Number.isFinite(finished)) return "-";
  return `${Math.max(0, Math.round((finished - started) / 1000))}s`;
}
