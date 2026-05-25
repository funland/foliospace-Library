import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { FormEvent, MouseEvent, TouchEvent } from "react";
import { api, Book, clearAuthToken, EpubManifest, FileError, getAuthToken, JobEvent, Library, Page, ScanJob, Series, setAuthToken } from "./api";

type View = "library" | "reader" | "jobs" | "errors";
type ReaderPageMode = "single" | "double";
type EpubTheme = "light" | "sepia" | "dark";
type BookSort = "title" | "recently_added" | "last_read" | "progress" | "unread";
const bookPageSize = 60;

export function App() {
  const [view, setView] = useState<View>("library");
  const [libraries, setLibraries] = useState<Library[]>([]);
  const [series, setSeries] = useState<Series[]>([]);
  const [books, setBooks] = useState<Book[]>([]);
  const [continueBooks, setContinueBooks] = useState<Book[]>([]);
  const [recentBooks, setRecentBooks] = useState<Book[]>([]);
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
    const [nextLibraries, nextSeries, nextJobs, nextErrors, nextContinueBooks, nextRecentBooks] = await Promise.all([
      api.libraries(),
      api.series(),
      api.jobs(),
      api.errors(),
      api.continueReading(),
      api.recentBooks(),
    ]);
    setLibraries(nextLibraries);
    setSeries(nextSeries);
    setJobs(nextJobs);
    setErrors(nextErrors);
    setContinueBooks(nextContinueBooks);
    setRecentBooks(nextRecentBooks);
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
    const confirmed = window.confirm(`Remove "${library.name}" from FolioSpace Reader? Files on disk will not be deleted.`);
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
      }
      setSelectedBook(book);
      setView("reader");
    } finally {
      setActiveTask(null);
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
        <div className="brand">FolioSpace Reader</div>
        <button className={view === "library" ? "active" : ""} onClick={() => setView("library")}>
          Library
        </button>
        <button className={view === "reader" ? "active" : ""} onClick={() => setView("reader")}>
          Reader
        </button>
        <button className={view === "jobs" ? "active" : ""} onClick={() => setView("jobs")}>
          Jobs
        </button>
        <button className={view === "errors" ? "active" : ""} onClick={() => setView("errors")}>
          Errors
        </button>
        {authEnabled && !authRequired && (
          <button className="lockButton" onClick={lockApp}>
            Lock
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
          <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search collections" />
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
            {(continueBooks.length > 0 || recentBooks.length > 0) && (
              <section className="homeRows panel wide" aria-label="Reading shortcuts">
                {continueBooks.length > 0 && (
                  <BookShelf
                    title="Continue Reading"
                    subtitle="Pick up from your latest position"
                    books={continueBooks}
                    onOpen={openBook}
                    meta={continueMeta}
                  />
                )}
                {recentBooks.length > 0 && (
                  <BookShelf
                    title="Recently Added"
                    subtitle="Newest indexed volumes"
                    books={recentBooks}
                    onOpen={openBook}
                    meta={recentMeta}
                  />
                )}
              </section>
            )}

            <section className="panel">
              <h1>Libraries</h1>
              <form className="libraryForm" onSubmit={addLibrary}>
                <input
                  value={newLibraryName}
                  onChange={(event) => setNewLibraryName(event.target.value)}
                  placeholder="Name"
                />
                <input
                  value={newLibraryPath}
                  onChange={(event) => setNewLibraryPath(event.target.value)}
                  placeholder="/volume2/ComicCenter"
                />
                <button disabled={!newLibraryPath.trim()}>Add</button>
              </form>
              {libraries.map((library) => (
                <div className="row" key={library.id}>
                  <div>
                    <strong>{library.name}</strong>
                    <small>{library.rootPath}</small>
                  </div>
                  <div className="rowActions">
                    <button onClick={() => scan(library)}>Scan</button>
                    <button className="danger" onClick={() => deleteLibrary(library)}>Delete</button>
                  </div>
                </div>
              ))}
            </section>

            <section className="panel">
              <h1>Collections</h1>
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
                  <h1>{selectedSeries ? selectedSeries.title : "Volume Wall"}</h1>
                  <small>
                    {selectedSeries
                      ? `${books.length} of ${bookTotal || selectedSeries.bookCount} volumes`
                      : "Select a collection to browse its single volumes"}
                  </small>
                </div>
                <div className="coverWallTools">
                  {selectedSeries && <span>{selectedSeries.bookCount} indexed</span>}
                  {selectedSeries && (
                    <label>
                      <span>Sort</span>
                      <select value={bookSort} onChange={(event) => setBookSort(event.target.value as BookSort)}>
                        <option value="title">Title</option>
                        <option value="recently_added">Recently added</option>
                        <option value="last_read">Last read</option>
                        <option value="progress">Progress</option>
                        <option value="unread">Unread first</option>
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
                        Single volume · {book.pageCount ? `${book.pageCount} pages` : "Not analyzed"}
                      </small>
                    </button>
                  ))}
                  <div className="bookLoadMore" ref={bookLoadMoreRef} aria-live="polite">
                    {bookListLoading
                      ? "Loading more volumes..."
                      : bookHasMore
                        ? "Scroll to load more"
                        : `${books.length} volumes loaded`}
                  </div>
                </div>
              ) : (
                <div className="coverEmpty">
                  <strong>{selectedSeries ? (bookListLoading ? "Loading volumes" : "No matching volumes") : "No collection selected"}</strong>
                  <small>
                    {selectedSeries ? "Clear the search field to show all volumes." : "Choose a collection from the list above."}
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
                        <button onClick={() => setEpubTocOpen((value) => !value)}>Contents</button>
                        <div className="segmentedControl" role="group" aria-label="EPUB page mode">
                          <button
                            className={epubPageMode === "single" ? "selected" : ""}
                            onClick={() => {
                              setEpubPageMode("single");
                              setEpubPagePosition(0);
                            }}
                          >
                            Single
                          </button>
                          <button
                            className={epubPageMode === "double" ? "selected" : ""}
                            onClick={() => {
                              setEpubPageMode("double");
                              setEpubPagePosition(0);
                            }}
                          >
                            Double
                          </button>
                        </div>
                        <select value={epubTheme} onChange={(event) => setEpubTheme(event.target.value as EpubTheme)}>
                          <option value="light">Light</option>
                          <option value="sepia">Sepia</option>
                          <option value="dark">Dark</option>
                        </select>
                        <label className="fontControl">
                          <span>Text</span>
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
                          Single
                        </button>
                        <button
                          className={readerPageMode === "double" ? "selected" : ""}
                          onClick={() => setReaderPageMode("double")}
                        >
                          Double
                        </button>
                      </div>
                    )}
                    <button onClick={toggleReaderFullscreen}>{readerFullscreen ? "Exit Fullscreen" : "Fullscreen"}</button>
                  </div>
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
                      <span>Loading page {pageIndex + 1}</span>
                    </div>
                  )}
                  {readerLoadState === "error" && (
                    <div className="pageLoading errorState" role="alert">
                      <strong>Page {pageIndex + 1} failed to load</strong>
                      <button onClick={() => setReaderRetryKey((value) => value + 1)}>Retry</button>
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
                  <button onClick={goReaderPrevious}>Previous</button>
                  {selectedBook.format === "epub" && (
                    <span className="epubProgress">
                      Page {Math.min(epubPagePosition + 1, epubPageCount)} / {epubPageCount}
                    </span>
                  )}
                  <input
                    type="range"
                    min="0"
                    max={Math.max(0, pages.length - 1)}
                    value={pageIndex}
                    onChange={(event) => setReaderPage(selectedBook, Number(event.target.value))}
                  />
                  <button onClick={goReaderNext}>Next</button>
                </div>
              </>
            ) : (
              <div className="empty">Select a book to start reading.</div>
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
            <h1>Errors</h1>
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
              <h1 id="auth-title">FolioSpace Reader</h1>
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
}: {
  title: string;
  subtitle: string;
  books: Book[];
  onOpen: (book: Book) => void;
  meta: (book: Book) => string;
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

function continueMeta(book: Book) {
  const progress = Math.max(0, Math.min(100, Math.round((book.progressFraction || 0) * 100)));
  const location = book.format === "epub" ? `chapter ${book.currentPage + 1}` : `page ${book.currentPage + 1}`;
  return `${progress}% · ${location}${book.collectionTitle ? ` · ${book.collectionTitle}` : ""}`;
}

function recentMeta(book: Book) {
  const added = formatRelativeDate(book.addedAt);
  return `${added}${book.collectionTitle ? ` · ${book.collectionTitle}` : ""}`;
}

function formatRelativeDate(value: string) {
  const parsed = dateValue(value);
  if (!parsed) return "Recently added";
  const days = Math.floor((Date.now() - parsed) / 86_400_000);
  if (days <= 0) return "Today";
  if (days === 1) return "Yesterday";
  if (days < 30) return `${days} days ago`;
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
