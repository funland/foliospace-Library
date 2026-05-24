import { useEffect, useMemo, useRef, useState } from "react";
import type { FormEvent } from "react";
import { api, Book, FileError, JobEvent, Library, Page, ScanJob, Series } from "./api";

type View = "library" | "reader" | "jobs" | "errors";

export function App() {
  const [view, setView] = useState<View>("library");
  const [libraries, setLibraries] = useState<Library[]>([]);
  const [series, setSeries] = useState<Series[]>([]);
  const [books, setBooks] = useState<Book[]>([]);
  const [jobs, setJobs] = useState<ScanJob[]>([]);
  const [errors, setErrors] = useState<FileError[]>([]);
  const [jobEvents, setJobEvents] = useState<JobEvent[]>([]);
  const [jobErrors, setJobErrors] = useState<FileError[]>([]);
  const [selectedJob, setSelectedJob] = useState<ScanJob | null>(null);
  const [selectedSeries, setSelectedSeries] = useState<Series | null>(null);
  const [selectedBook, setSelectedBook] = useState<Book | null>(null);
  const [pages, setPages] = useState<Page[]>([]);
  const [pageIndex, setPageIndex] = useState(0);
  const [displayedPageIndex, setDisplayedPageIndex] = useState(0);
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("Ready");
  const [activeTask, setActiveTask] = useState<string | null>(null);
  const [readerLoadState, setReaderLoadState] = useState<"idle" | "loading" | "ready" | "error">("idle");
  const [readerRetryKey, setReaderRetryKey] = useState(0);
  const [newLibraryName, setNewLibraryName] = useState("");
  const [newLibraryPath, setNewLibraryPath] = useState("");
  const imageCache = useRef<Set<string>>(new Set());

  async function refreshAll(showProgress = false) {
    if (showProgress) {
      setActiveTask("Refreshing library");
    }
    const [nextLibraries, nextSeries, nextJobs, nextErrors] = await Promise.all([
      api.libraries(),
      api.series(),
      api.jobs(),
      api.errors(),
    ]);
    setLibraries(nextLibraries);
    setSeries(nextSeries);
    setJobs(nextJobs);
    setErrors(nextErrors);
    if (showProgress) {
      setActiveTask(null);
    }
  }

  useEffect(() => {
    refreshAll(true)
      .catch((error) => setStatus(error.message))
      .finally(() => setActiveTask(null));
  }, []);

  const activeScan = jobs.find((job) => job.status === "running") ?? null;

  useEffect(() => {
    if (!activeScan) return;

    const timer = window.setInterval(() => {
      refreshAll().catch((error) => setStatus(error.message));
    }, 1200);

    return () => window.clearInterval(timer);
  }, [activeScan?.id]);

  useEffect(() => {
    if (!selectedBook) return;

    const timer = window.setTimeout(() => {
      api.progress(selectedBook.id, pageIndex).catch(() => undefined);
    }, 450);

    return () => window.clearTimeout(timer);
  }, [selectedBook, pageIndex]);

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

  async function openSeries(item: Series) {
    setActiveTask(`Loading ${item.title}`);
    setSelectedSeries(item);
    try {
      setBooks(await api.books(item.id));
    } finally {
      setActiveTask(null);
    }
  }

  async function openBook(book: Book) {
    setActiveTask(`Opening ${book.title}`);
    setSelectedBook(book);
    setPageIndex(0);
    setDisplayedPageIndex(0);
    setReaderLoadState("loading");
    try {
      setPages(await api.pages(book.id));
      setView("reader");
    } finally {
      setActiveTask(null);
    }
  }

  async function setReaderPage(book: Book, nextIndex: number) {
    const clamped = Math.max(0, Math.min(nextIndex, Math.max(0, pages.length - 1)));
    if (clamped !== pageIndex) {
      setReaderLoadState("loading");
    }
    setPageIndex(clamped);
  }

  useEffect(() => {
    if (!selectedBook || pages.length === 0) return;

    let cancelled = false;
    const targetIndex = pageIndex;
    setReaderLoadState("loading");

    preloadPage(selectedBook.id, targetIndex)
      .then(() => {
        if (cancelled) return;
        setDisplayedPageIndex(targetIndex);
        setReaderLoadState("ready");
        prefetchNeighborPages(selectedBook.id, targetIndex, pages.length);
      })
      .catch(() => {
        if (cancelled) return;
        setReaderLoadState("error");
        setStatus(`Failed to load page ${targetIndex + 1}`);
      });

    return () => {
      cancelled = true;
    };
  }, [selectedBook?.id, pageIndex, pages.length, readerRetryKey]);

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

  function prefetchNeighborPages(bookID: number, index: number, total: number) {
    for (const next of [index + 1, index - 1]) {
      if (next >= 0 && next < total) {
        preloadPage(bookID, next).catch(() => undefined);
      }
    }
  }

  const filteredSeries = useMemo(() => {
    const value = query.trim().toLowerCase();
    if (!value) return series;
    return series.filter((item) => item.title.toLowerCase().includes(value));
  }, [query, series]);

  const filteredBooks = useMemo(() => {
    const value = query.trim().toLowerCase();
    if (!value || !selectedSeries) return books;
    return books.filter((book) => book.title.toLowerCase().includes(value));
  }, [books, query, selectedSeries]);

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
      </aside>

      <section className="workspace">
        {activeTask && (
          <div className="globalProgress" role="status" aria-live="polite">
            <div className="progressBar" />
            <span>{activeTask}</span>
          </div>
        )}

        <header className="topbar">
          <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search series" />
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
              <h1>Series</h1>
              <div className="list">
                {filteredSeries.map((item) => (
                  <button className="listItem" key={item.id} onClick={() => openSeries(item)}>
                    <span>{item.title}</span>
                    <small>{item.bookCount} books</small>
                  </button>
                ))}
              </div>
            </section>

            <section className="coverWall panel wide">
              <div className="coverWallHeader">
                <div>
                  <h1>{selectedSeries ? selectedSeries.title : "Cover Wall"}</h1>
                  <small>
                    {selectedSeries
                      ? `${filteredBooks.length} of ${books.length} books`
                      : "Select a series to browse its books"}
                  </small>
                </div>
                {selectedSeries && <span>{selectedSeries.bookCount} indexed</span>}
              </div>
              {selectedSeries && filteredBooks.length > 0 ? (
                <div className="books">
                  {filteredBooks.map((book) => (
                    <button className="book" key={book.id} onClick={() => openBook(book)} title={book.title}>
                      <span className="coverFrame">
                        <img src={`/api/books/${book.id}/cover`} alt="" loading="lazy" />
                        <span className="coverBadge">{book.format.toUpperCase()}</span>
                      </span>
                      <strong>{book.title}</strong>
                      <small>{book.pageCount ? `${book.pageCount} pages` : "Not analyzed"}</small>
                    </button>
                  ))}
                </div>
              ) : (
                <div className="coverEmpty">
                  <strong>{selectedSeries ? "No matching books" : "No series selected"}</strong>
                  <small>{selectedSeries ? "Clear the search field to show all books." : "Choose a series from the list above."}</small>
                </div>
              )}
            </section>
          </div>
        )}

        {view === "reader" && (
          <section className="reader">
            {selectedBook ? (
              <>
                <div className="readerHeader">
                  <strong>{selectedBook.title}</strong>
                  <span>
                    {pageIndex + 1} / {Math.max(pages.length, 1)}
                  </span>
                </div>
                <div className="pageStage">
                  {readerLoadState === "loading" && pageIndex !== displayedPageIndex && (
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
                  <img
                    key={`${selectedBook.id}-${displayedPageIndex}`}
                    src={`/api/books/${selectedBook.id}/pages/${displayedPageIndex}`}
                    alt={pages[displayedPageIndex]?.name ?? ""}
                  />
                </div>
                <div className="readerControls">
                  <button onClick={() => setReaderPage(selectedBook, pageIndex - 1)}>Previous</button>
                  <input
                    type="range"
                    min="0"
                    max={Math.max(0, pages.length - 1)}
                    value={pageIndex}
                    onChange={(event) => setReaderPage(selectedBook, Number(event.target.value))}
                  />
                  <button onClick={() => setReaderPage(selectedBook, pageIndex + 1)}>Next</button>
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
    </main>
  );
}

function formatElapsed(job: ScanJob) {
  const started = new Date(job.startedAt).getTime();
  const finished = job.finishedAt ? new Date(job.finishedAt).getTime() : Date.now();
  if (!Number.isFinite(started) || !Number.isFinite(finished)) return "-";
  return `${Math.max(0, Math.round((finished - started) / 1000))}s`;
}
