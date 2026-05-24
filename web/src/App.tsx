import { useEffect, useMemo, useState } from "react";
import { api, Book, FileError, Library, Page, ScanJob, Series } from "./api";

type View = "library" | "reader" | "jobs" | "errors";

export function App() {
  const [view, setView] = useState<View>("library");
  const [libraries, setLibraries] = useState<Library[]>([]);
  const [series, setSeries] = useState<Series[]>([]);
  const [books, setBooks] = useState<Book[]>([]);
  const [jobs, setJobs] = useState<ScanJob[]>([]);
  const [errors, setErrors] = useState<FileError[]>([]);
  const [selectedSeries, setSelectedSeries] = useState<Series | null>(null);
  const [selectedBook, setSelectedBook] = useState<Book | null>(null);
  const [pages, setPages] = useState<Page[]>([]);
  const [pageIndex, setPageIndex] = useState(0);
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("Ready");
  const [activeTask, setActiveTask] = useState<string | null>(null);
  const [readerImageLoaded, setReaderImageLoaded] = useState(false);

  async function refreshAll() {
    setActiveTask("Refreshing library");
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
    setActiveTask(null);
  }

  useEffect(() => {
    refreshAll()
      .catch((error) => setStatus(error.message))
      .finally(() => setActiveTask(null));
  }, []);

  async function scan(library: Library) {
    setStatus(`Scanning ${library.rootPath}`);
    setActiveTask("Scanning library");
    try {
      const job = await api.scan(library.id);
      setStatus(`Scan ${job.status}: ${job.indexedFiles} indexed, ${job.errorCount} errors`);
      await refreshAll();
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
    setReaderImageLoaded(false);
    setSelectedBook(book);
    try {
      setPages(await api.pages(book.id));
      setPageIndex(0);
      setView("reader");
    } finally {
      setActiveTask(null);
    }
  }

  async function setReaderPage(book: Book, nextIndex: number) {
    const clamped = Math.max(0, Math.min(nextIndex, Math.max(0, pages.length - 1)));
    if (clamped !== pageIndex) {
      setReaderImageLoaded(false);
    }
    setPageIndex(clamped);
    await api.progress(book.id, clamped).catch(() => undefined);
  }

  const filteredSeries = useMemo(() => {
    const value = query.trim().toLowerCase();
    if (!value) return series;
    return series.filter((item) => item.title.toLowerCase().includes(value));
  }, [query, series]);

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
          <span>{status}</span>
        </header>

        {view === "library" && (
          <div className="grid">
            <section className="panel">
              <h1>Libraries</h1>
              {libraries.map((library) => (
                <div className="row" key={library.id}>
                  <div>
                    <strong>{library.name}</strong>
                    <small>{library.rootPath}</small>
                  </div>
                  <button onClick={() => scan(library)}>Scan</button>
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

            <section className="panel wide">
              <h1>{selectedSeries ? selectedSeries.title : "Books"}</h1>
              <div className="books">
                {books.map((book) => (
                  <button className="book" key={book.id} onClick={() => openBook(book)}>
                    <img src={`/api/books/${book.id}/cover`} alt="" />
                    <strong>{book.title}</strong>
                    <small>
                      {book.format.toUpperCase()} · {book.pageCount || "?"} pages
                    </small>
                  </button>
                ))}
              </div>
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
                  {!readerImageLoaded && (
                    <div className="pageLoading" role="status" aria-live="polite">
                      <div className="pageProgress">
                        <div />
                      </div>
                      <span>Loading page {pageIndex + 1}</span>
                    </div>
                  )}
                  <img
                    key={`${selectedBook.id}-${pageIndex}`}
                    className={readerImageLoaded ? "loaded" : ""}
                    src={`/api/books/${selectedBook.id}/pages/${pageIndex}`}
                    alt={pages[pageIndex]?.name ?? ""}
                    onLoad={() => setReaderImageLoaded(true)}
                    onError={() => {
                      setReaderImageLoaded(true);
                      setStatus(`Failed to load page ${pageIndex + 1}`);
                    }}
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
          <section className="panel">
            <h1>Jobs</h1>
            {jobs.map((job) => (
              <div className="row" key={job.id}>
                <div>
                  <strong>Job #{job.id}</strong>
                  <small>
                    {job.status} · {job.indexedFiles}/{job.discoveredFiles} indexed · {job.errorCount} errors
                  </small>
                </div>
              </div>
            ))}
          </section>
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
