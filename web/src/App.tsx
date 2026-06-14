import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, FormEvent, MouseEvent, ReactNode, SyntheticEvent, TouchEvent } from "react";
import { GlobalWorkerOptions, getDocument } from "pdfjs-dist";
import type { PDFDocumentProxy } from "pdfjs-dist";
import pdfWorkerURL from "pdfjs-dist/build/pdf.worker.mjs?url";
import { api, Book, BookPrivateState, clearAuthToken, ClientInfo, ClientPreferences, CollectionPrivateState, DirectoryEntry, DirectoryListing, EpubManifest, EpubTocItem, FileError, GameAsset, getActiveProfileId, getAuthToken, JobEvent, Library, Page, Profile, ScanJob, Series, setActiveProfileId as persistActiveProfileId, setAuthToken, SetupStatus, ScanSettings, ThumbnailWorkerStatus, VideoAsset, VideoTranscodeQueueStatus, VideoTranscodeStatus } from "./api";
import {
  DEFAULT_WEBTOON_ANCHOR_RATIO,
  WEBTOON_POSITION_SCHEMA,
  buildWebtoonPosition,
  resolveWebtoonRestoreTarget,
  stabilizeWebtoonDocumentProgress,
  type WebtoonPageMetric,
  type WebtoonPosition,
} from "./webtoon-position";
import { fullscreenImageFit, type ReaderImageFitMode } from "./reader-fit";

GlobalWorkerOptions.workerSrc = pdfWorkerURL;

type View = "library" | "favorites" | "reader" | "games" | "videos" | "jobs" | "errors" | "about";
type ReaderPageMode = "single" | "double" | "webtoon";
type EpubPageMode = "single" | "double";
type EpubTheme = "light" | "sepia" | "dark";
const WEBTOON_RENDER_RADIUS = 2;
const WEBTOON_PLACEHOLDER_HEIGHT = 2200;
const PDF_WEBTOON_RENDER_RADIUS = 2;
const PDF_WEBTOON_PLACEHOLDER_HEIGHT = 1600;
const PDF_WEBTOON_MAX_CANVAS_PIXELS = 6_000_000;
const BOOTSTRAP_TIMEOUT_MS = 12_000;
type BookSort = "title" | "recently_added" | "last_read" | "progress" | "unread";
type Locale = "zh" | "zht" | "en" | "ja" | "ko";
type LibraryAssetType = "mixed" | "book" | "comic" | "game" | "video";
type ReaderImageSize = { width: number; height: number };
const bookPageSize = 30;
const catalogPageSize = 200;

export function App() {
  const initialPreferences = useRef(readLocalPreferences()).current;
  const [view, setView] = useState<View>("library");
  const [libraries, setLibraries] = useState<Library[]>([]);
  const [series, setSeries] = useState<Series[]>([]);
  const [books, setBooks] = useState<Book[]>([]);
  const [continueBooks, setContinueBooks] = useState<Book[]>([]);
  const [recentBooks, setRecentBooks] = useState<Book[]>([]);
  const [favoriteBooks, setFavoriteBooks] = useState<Book[]>([]);
  const [wantBooks, setWantBooks] = useState<Book[]>([]);
  const [gameShelf, setGameShelf] = useState<GameAsset[]>([]);
  const [videoShelf, setVideoShelf] = useState<VideoAsset[]>([]);
  const [gameCatalog, setGameCatalog] = useState<GameAsset[]>([]);
  const [videoCatalog, setVideoCatalog] = useState<VideoAsset[]>([]);
  const [gameCatalogTotal, setGameCatalogTotal] = useState(0);
  const [videoCatalogTotal, setVideoCatalogTotal] = useState(0);
  const [gameCatalogHasMore, setGameCatalogHasMore] = useState(false);
  const [videoCatalogHasMore, setVideoCatalogHasMore] = useState(false);
  const [gameCatalogLoading, setGameCatalogLoading] = useState(false);
  const [videoCatalogLoading, setVideoCatalogLoading] = useState(false);
  const [clientInfo, setClientInfo] = useState<ClientInfo | null>(null);
  const [selectedVideo, setSelectedVideo] = useState<VideoAsset | null>(null);
  const [videoTranscodeStatus, setVideoTranscodeStatus] = useState<VideoTranscodeStatus | null>(null);
  const [videoTranscodeQueueStatus, setVideoTranscodeQueueStatus] = useState<VideoTranscodeQueueStatus | null>(null);
  const [videoPlaybackReloadKey, setVideoPlaybackReloadKey] = useState(0);
  const [jobs, setJobs] = useState<ScanJob[]>([]);
  const [thumbnailWorkerStatus, setThumbnailWorkerStatus] = useState<ThumbnailWorkerStatus | null>(null);
  const [thumbnailWorkerBusy, setThumbnailWorkerBusy] = useState(false);
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
  const [nowTick, setNowTick] = useState(Date.now());
  const [authChecked, setAuthChecked] = useState(false);
  const [authEnabled, setAuthEnabled] = useState(false);
  const [authRequired, setAuthRequired] = useState(false);
  const [authInput, setAuthInput] = useState("");
  const [authError, setAuthError] = useState("");
  const [authMessage, setAuthMessage] = useState("Checking access settings.");
  const [setupStatus, setSetupStatus] = useState<SetupStatus | null>(null);
  const [setupRequired, setSetupRequired] = useState(false);
  const [setupToken, setSetupToken] = useState("");
  const [setupName, setSetupName] = useState("");
  const [setupPath, setSetupPath] = useState("");
  const [setupAssetType, setSetupAssetType] = useState<LibraryAssetType>("mixed");
  const [setupScanWorkers, setSetupScanWorkers] = useState(2);
  const [setupError, setSetupError] = useState("");
  const [scanSettings, setScanSettings] = useState<ScanSettings>({ scanWorkers: 1 });
  const [scanWorkerDraft, setScanWorkerDraft] = useState(1);
  const [scanSettingsSaving, setScanSettingsSaving] = useState(false);
  const [quickScanLibraryId, setQuickScanLibraryId] = useState(0);
  const [quickScanPath, setQuickScanPath] = useState("");
  const [quickScanRunning, setQuickScanRunning] = useState(false);
  const [recentScanLimit, setRecentScanLimit] = useState(20);
  const [recentScanRunning, setRecentScanRunning] = useState(false);
  const [activeTask, setActiveTask] = useState<string | null>(null);
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [activeProfileId, setActiveProfileId] = useState(0);
  const [profileSaving, setProfileSaving] = useState(false);
  const [profileMenuOpen, setProfileMenuOpen] = useState(false);
  const [readerLoadState, setReaderLoadState] = useState<"idle" | "loading" | "ready" | "error">("idle");
  const [readerRetryKey, setReaderRetryKey] = useState(0);
  const [readerPageMode, setReaderPageMode] = useState<ReaderPageMode>(initialPreferences.readerPageMode);
  const [readerFullscreen, setReaderFullscreen] = useState(false);
  const [epubPageMode, setEpubPageMode] = useState<EpubPageMode>(initialPreferences.epubPageMode);
  const [epubFontSize, setEpubFontSize] = useState(initialPreferences.epubFontSize);
  const [epubTheme, setEpubTheme] = useState<EpubTheme>(initialPreferences.epubTheme);
  const [epubPagePosition, setEpubPagePosition] = useState(0);
  const [epubPageCount, setEpubPageCount] = useState(1);
  const [epubTocOpen, setEpubTocOpen] = useState(false);
  const [pdfPageCount, setPdfPageCount] = useState(1);
  const [webtoonProgress, setWebtoonProgress] = useState(0);
  const [webtoonPosition, setWebtoonPosition] = useState<WebtoonPosition | null>(null);
  const [webtoonRestorePosition, setWebtoonRestorePosition] = useState<WebtoonPosition | null>(null);
  const [webtoonPageHeights, setWebtoonPageHeights] = useState<Record<number, number>>({});
  const [readerImageSizes, setReaderImageSizes] = useState<Record<number, ReaderImageSize>>({});
  const [newLibraryName, setNewLibraryName] = useState("");
  const [newLibraryPath, setNewLibraryPath] = useState("");
  const [newLibraryAssetType, setNewLibraryAssetType] = useState<LibraryAssetType>("mixed");
  const [directoryPickerOpen, setDirectoryPickerOpen] = useState(false);
  const [directoryListing, setDirectoryListing] = useState<DirectoryListing | null>(null);
  const [directoryPickerLoading, setDirectoryPickerLoading] = useState(false);
  const [directoryPickerError, setDirectoryPickerError] = useState("");
  const [privateDraft, setPrivateDraft] = useState<BookPrivateState>(emptyPrivateState());
  const [privateSaving, setPrivateSaving] = useState(false);
  const [bookDetailsOpen, setBookDetailsOpen] = useState(false);
  const [locale, setLocale] = useState<Locale>(initialPreferences.locale);
  const t = translations[locale];
  const activeProfile = useMemo(
    () => profiles.find((profile) => profile.id === activeProfileId) ?? profiles.find((profile) => profile.isDefault) ?? profiles[0] ?? null,
    [activeProfileId, profiles],
  );
  const activeProfileLabel = activeProfile ? profileDisplayName(activeProfile, t) : t.defaultProfile;
  const imageCache = useRef<Set<string>>(new Set());
  const readerRef = useRef<HTMLElement | null>(null);
  const webtoonRef = useRef<HTMLDivElement | null>(null);
  const bookLoadMoreRef = useRef<HTMLDivElement | null>(null);
  const collectionSectionsRef = useRef<HTMLDivElement | null>(null);
  const collectionContentRef = useRef<HTMLElement | null>(null);
  const videoPlayerRef = useRef<HTMLVideoElement | null>(null);
  const previousVideoTranscodeStatus = useRef<string>("");
  const collectionScrollTop = useRef(0);
  const bookListRequest = useRef(0);
  const swipeStart = useRef<{ x: number; y: number } | null>(null);
  const epubRestorePosition = useRef<number | null>(null);
  const webtoonRestoring = useRef(false);
  const webtoonUserActivated = useRef(false);
  const webtoonUserScrollUntil = useRef(0);
  const suppressPagedProgressSave = useRef(false);
  const preferencesLoaded = useRef(false);

  function applyClientPreferences(preferences: ClientPreferences) {
    const normalized = normalizeClientPreferences(preferences);
    setLocale(normalized.locale);
    setReaderPageMode(normalized.readerPageMode);
    setEpubPageMode(normalized.epubPageMode);
    setEpubTheme(normalized.epubTheme);
    setEpubFontSize(normalized.epubFontSize);
    writeLocalPreferences(normalized);
  }

  async function refreshAll(showProgress = false) {
    if (showProgress) {
      setActiveTask("Refreshing library");
    }
    try {
      const nextProfiles = await api.profiles();
      const profileList = arrayOrEmpty(nextProfiles);
      const resolvedProfile = resolveActiveProfile(profileList, getActiveProfileId());
      setProfiles(profileList);
      setActiveProfileId(resolvedProfile?.id ?? 0);
      if (resolvedProfile) {
        persistActiveProfileId(resolvedProfile.id);
      } else {
        persistActiveProfileId("");
      }
      const [preferences, info, nextScanSettings, nextLibraries, nextSeries, nextJobs, nextThumbnailWorkerStatus, nextErrors, nextContinueBooks, nextRecentBooks, nextFavoriteBooks, nextWantBooks, nextGameShelf, nextVideoShelf] = await Promise.all([
        api.clientPreferences(),
        api.clientInfo(),
        api.scanSettings(),
        api.libraries(),
        api.series(),
        api.jobs(),
        api.thumbnailWorkerStatus({ timeoutMs: 4_000 }),
        api.errors(),
        api.continueReading(),
        api.recentBooks(),
        api.favoriteBooks(),
        api.privateStatusBooks("want"),
        api.recentGames(),
        api.recentVideos(),
      ]);
      applyClientPreferences(preferences);
      preferencesLoaded.current = true;
      setClientInfo(info);
      setScanSettings(nextScanSettings);
      setScanWorkerDraft(nextScanSettings.scanWorkers);
      setLibraries(arrayOrEmpty(nextLibraries));
      setSeries(arrayOrEmpty(nextSeries));
      setJobs(arrayOrEmpty(nextJobs));
      setThumbnailWorkerStatus(nextThumbnailWorkerStatus);
      setErrors(arrayOrEmpty(nextErrors));
      setContinueBooks(arrayOrEmpty(nextContinueBooks));
      setRecentBooks(arrayOrEmpty(nextRecentBooks));
      setFavoriteBooks(arrayOrEmpty(nextFavoriteBooks));
      setWantBooks(arrayOrEmpty(nextWantBooks));
      setGameShelf(arrayOrEmpty(nextGameShelf));
      setVideoShelf(arrayOrEmpty(nextVideoShelf));
    } finally {
      if (showProgress) {
        setActiveTask(null);
      }
    }
  }

  async function openGameCatalog() {
    setView("games");
    if (gameCatalog.length > 0 || gameCatalogLoading) return;
    await loadGameCatalogPage(0, true);
  }

  async function loadGameCatalogPage(offset: number, reset = false) {
    if (gameCatalogLoading) return;
    setGameCatalogLoading(true);
    try {
      const page = await api.clientGames({ limit: catalogPageSize, offset, sort: "platform" });
      const items = arrayOrEmpty(page.items);
      setGameCatalog((current) => reset ? items : mergeByID(current, items));
      setGameCatalogTotal(page.total);
      setGameCatalogHasMore(page.hasMore);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Failed to load games");
    } finally {
      setGameCatalogLoading(false);
    }
  }

  async function openVideoCatalog() {
    setView("videos");
    if (videoCatalog.length > 0 || videoCatalogLoading) return;
    await loadVideoCatalogPage(0, true);
  }

  async function loadVideoCatalogPage(offset: number, reset = false) {
    if (videoCatalogLoading) return;
    setVideoCatalogLoading(true);
    try {
      const page = await api.clientVideos({ limit: catalogPageSize, offset, sort: "title" });
      const items = arrayOrEmpty(page.items);
      setVideoCatalog((current) => reset ? items : mergeByID(current, items));
      setVideoCatalogTotal(page.total);
      setVideoCatalogHasMore(page.hasMore);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Failed to load videos");
    } finally {
      setVideoCatalogLoading(false);
    }
  }

  useEffect(() => {
    async function bootstrap() {
      try {
        setAuthMessage("Checking setup status.");
        const setup = await api.setupStatus({ timeoutMs: BOOTSTRAP_TIMEOUT_MS });
        setSetupStatus(setup);
        if (!setup.initialized) {
          setSetupRequired(true);
          setAuthEnabled(setup.authEnabled);
          setSetupScanWorkers(setup.scanWorkers || 2);
          const firstRoot = setup.directoryRoots[0];
          if (firstRoot) {
            setSetupPath(firstRoot.path);
            setSetupName(firstRoot.name);
          }
          setStatus("Setup required");
          return;
        }
        setAuthMessage("Checking authentication mode.");
        const auth = await api.authStatus({ timeoutMs: BOOTSTRAP_TIMEOUT_MS });
        setAuthEnabled(auth.enabled);
        const storedToken = getAuthToken();
        if (auth.enabled && !storedToken) {
          setAuthRequired(true);
          setStatus("Authentication required");
          return;
        }
        if (auth.enabled) {
          setAuthMessage("Checking saved access token.");
          await api.authCheck(storedToken, { timeoutMs: BOOTSTRAP_TIMEOUT_MS });
        }
        setAuthMessage("Loading library overview.");
        await refreshAll(true);
      } catch (error) {
        if (isUnauthorized(error)) {
          clearAuthToken();
          setAuthRequired(true);
          setStatus("Authentication required");
          return;
        }
        const message = error instanceof Error ? error.message : "Failed to load";
        setAuthError(message);
        setStatus(message);
      } finally {
        setAuthChecked(true);
        setActiveTask(null);
      }
    }

    bootstrap();
  }, []);

  useEffect(() => {
    writeLocalPreferences({
      locale,
      readerPageMode,
      epubPageMode,
      epubTheme,
      epubFontSize,
    });
    if (!preferencesLoaded.current || authRequired) {
      return;
    }
    const timer = window.setTimeout(() => {
      api.saveClientPreferences({
        locale,
        readerPageMode,
        epubPageMode,
        epubTheme,
        epubFontSize,
      }).catch((error) => {
        setStatus(error instanceof Error ? error.message : "Failed to save preferences");
      });
    }, 300);
    return () => window.clearTimeout(timer);
  }, [locale, readerPageMode, epubPageMode, epubTheme, epubFontSize, authRequired]);

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
    setBookDetailsOpen(false);
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

  async function submitSetup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const token = setupToken.trim();
    if (!setupPath.trim() && !setupStatus?.hasLibraries) return;
    if (!setupStatus?.authEnabled && !token) {
      setSetupError("Access token is required");
      return;
    }
    setSetupError("");
    setActiveTask("Initializing FolioSpace Library");
    try {
      if (setupStatus?.authEnabled && token) {
        setAuthToken(token);
      }
      await api.setupInitialize({
        token: setupStatus?.authEnabled ? "" : token,
        name: setupName,
        rootPath: setupPath,
        assetType: setupAssetType,
        scanWorkers: setupScanWorkers,
      });
      if (!setupStatus?.authEnabled) {
        setAuthToken(token);
      }
      setSetupRequired(false);
      setAuthRequired(false);
      setAuthEnabled(true);
      setSetupToken("");
      setStatus("Ready");
      await refreshAll(true);
    } catch (error) {
      if (setupStatus?.authEnabled) {
        clearAuthToken();
      }
      setSetupError(error instanceof Error ? error.message : "Setup failed");
    } finally {
      setAuthChecked(true);
      setActiveTask(null);
    }
  }

  async function saveScanSettings() {
    const nextWorkers = clampScanWorkers(scanWorkerDraft);
    setScanSettingsSaving(true);
    try {
      const saved = await api.saveScanSettings({ scanWorkers: nextWorkers });
      setScanSettings(saved);
      setScanWorkerDraft(saved.scanWorkers);
      setStatus(t.scanWorkersSaved(saved.scanWorkers));
    } catch (error) {
      handleAPIError(error);
    } finally {
      setScanSettingsSaving(false);
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

  const activeScan = jobs.find((job) => isActiveScanStatus(job.status)) ?? null;
  const quickScanLibrary = libraries.find((item) => item.id === quickScanLibraryId) ?? libraries[0] ?? null;
  const quickScanTargetPath = quickScanLibrary ? normalizeQuickScanTarget(quickScanLibrary, quickScanPath) : "";
  const recentScanTargetPath = quickScanLibrary ? recentScanTargetLabel(normalizeQuickScanTarget(quickScanLibrary, quickScanPath) || quickScanLibrary.rootPath, recentScanLimit) : "";
  const activeQuickScanJob = quickScanTargetPath
    ? jobs.find((job) => job.libraryId === quickScanLibrary?.id && job.targetPath === quickScanTargetPath && isActiveScanStatus(job.status)) ?? null
    : null;
  const activeRecentScanJob = recentScanTargetPath
    ? jobs.find((job) => job.libraryId === quickScanLibrary?.id && job.targetPath === recentScanTargetPath && isActiveScanStatus(job.status)) ?? null
    : null;

  useEffect(() => {
    if (quickScanLibraryId === 0 && libraries.length > 0) {
      setQuickScanLibraryId(libraries[0].id);
    }
  }, [quickScanLibraryId, libraries]);

  useEffect(() => {
    if (!activeScan) return;

    setNowTick(Date.now());
    const timer = window.setInterval(() => {
      setNowTick(Date.now());
    }, 1000);

    return () => window.clearInterval(timer);
  }, [activeScan?.id]);

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

    const totalPages = selectedBook.format === "pdf" ? pdfPageCount : pages.length;
    const useWebtoonMode = readerPageMode === "webtoon" && selectedBook.format !== "pdf";
    if (useWebtoonMode) {
      if (!webtoonPosition || !webtoonUserActivated.current) return;
      const timer = window.setTimeout(() => {
        api.saveWebtoonReadingPosition(selectedBook.id, webtoonPosition).catch(() =>
          api
            .progressDetail(
              selectedBook.id,
              webtoonPosition.pageIndex,
              `webtoon:${webtoonPosition.documentProgress}`,
              webtoonPosition.documentProgress,
            )
            .catch(() => undefined),
        );
      }, 650);
      return () => window.clearTimeout(timer);
    }
    if (suppressPagedProgressSave.current) {
      suppressPagedProgressSave.current = false;
      return;
    }
    const progressFraction = useWebtoonMode
      ? webtoonProgress
      : totalPages > 1
        ? pageIndex / (totalPages - 1)
        : 0;
    const timer = window.setTimeout(() => {
      api
        .progressDetail(
          selectedBook.id,
          pageIndex,
          "",
          progressFraction,
        )
        .catch(() => undefined);
    }, 450);

    return () => window.clearTimeout(timer);
  }, [selectedBook, pageIndex, pages.length, pdfPageCount, readerPageMode, webtoonPosition, webtoonProgress]);

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
    const existingJob = jobs.find((job) => job.libraryId === library.id && job.targetPath === normalizeQuickScanTarget(library, library.rootPath) && isActiveScanStatus(job.status));
    if (existingJob) {
      setSelectedJob(existingJob);
      setStatus(t.quickScanAlreadyRunning(existingJob.id));
      return;
    }
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

  async function quickScan() {
    const library = quickScanLibrary;
    const path = quickScanPath.trim();
    if (!library || !path) return;
    if (activeQuickScanJob) {
      setSelectedJob(activeQuickScanJob);
      setStatus(t.quickScanAlreadyRunning(activeQuickScanJob.id));
      return;
    }
    setStatus(t.quickScanStarting(path));
    setQuickScanRunning(true);
    setActiveTask(t.quickScan);
    try {
      const job = await api.scan(library.id, path);
      setStatus(t.quickScanQueued(job.id));
      await refreshAll();
    } catch (error) {
      handleAPIError(error);
    } finally {
      setQuickScanRunning(false);
      setActiveTask(null);
    }
  }

  async function recentScan() {
    const library = quickScanLibrary;
    if (!library) return;
    if (activeRecentScanJob) {
      setSelectedJob(activeRecentScanJob);
      setStatus(t.quickScanAlreadyRunning(activeRecentScanJob.id));
      return;
    }
    const path = quickScanPath.trim();
    setStatus(t.recentScanStarting(path || library.rootPath, recentScanLimit));
    setRecentScanRunning(true);
    setActiveTask(t.recentScan);
    try {
      const job = await api.scan(library.id, { path, mode: "recent", recentLimit: recentScanLimit });
      setStatus(t.recentScanQueued(job.id));
      await refreshAll();
    } catch (error) {
      handleAPIError(error);
    } finally {
      setRecentScanRunning(false);
      setActiveTask(null);
    }
  }

  async function controlJob(job: ScanJob, action: "pause" | "cancel" | "resume") {
    setActiveTask(`${action} job #${job.id}`);
    try {
      const updated =
        action === "pause"
          ? await api.pauseJob(job.id)
          : action === "cancel"
            ? await api.cancelJob(job.id)
            : await api.resumeJob(job.id);
      setSelectedJob(updated);
      setStatus(`Job #${job.id}: ${action}`);
      await refreshAll();
      await openJob(updated);
    } catch (error) {
      handleAPIError(error);
    } finally {
      setActiveTask(null);
    }
  }

  async function refreshThumbnailWorkerStatus() {
    try {
      setThumbnailWorkerStatus(await api.thumbnailWorkerStatus({ detail: "full", timeoutMs: 15_000 }));
    } catch (error) {
      handleAPIError(error);
    }
  }

  async function controlThumbnailWorker(action: "pause" | "resume" | "cancel" | "cleanupOrphans") {
    setThumbnailWorkerBusy(true);
    setActiveTask(`${action} thumbnail worker`);
    try {
      const nextStatus =
        action === "pause"
          ? await api.pauseThumbnailWorker()
          : action === "resume"
            ? await api.resumeThumbnailWorker()
            : action === "cancel"
              ? await api.cancelThumbnailJobs()
              : await api.cleanupThumbnailOrphans();
      setThumbnailWorkerStatus(nextStatus);
      setStatus(`Thumbnail worker: ${nextStatus.status}`);
    } catch (error) {
      handleAPIError(error);
    } finally {
      setThumbnailWorkerBusy(false);
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
      const library = await api.createLibrary(newLibraryName, newLibraryPath, newLibraryAssetType);
      setStatus(`Library added: ${library.rootPath}`);
      setNewLibraryName("");
      setNewLibraryPath("");
      setNewLibraryAssetType("mixed");
      await refreshAll();
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Failed to add library");
    } finally {
      setActiveTask(null);
    }
  }

  async function openDirectoryPicker(path = newLibraryPath || "/") {
    setDirectoryPickerOpen(true);
    await loadDirectory(path);
  }

  async function loadDirectory(path: string) {
    setDirectoryPickerLoading(true);
    setDirectoryPickerError("");
    try {
      setDirectoryListing(await api.directories(path));
    } catch (error) {
      setDirectoryPickerError(error instanceof Error ? error.message : "Failed to load directory");
    } finally {
      setDirectoryPickerLoading(false);
    }
  }

  function selectDirectory(path: string) {
    setNewLibraryPath(path);
    if (!newLibraryName.trim()) {
      const name = path.split("/").filter(Boolean).pop();
      if (name) setNewLibraryName(name);
    }
    setDirectoryPickerOpen(false);
  }

  function selectSetupRoot(root: DirectoryEntry) {
    setSetupPath(root.path);
    if (!setupName.trim()) {
      setSetupName(root.name);
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
    const isSameSeries = selectedSeries?.id === item.id;
    setView("library");
    setSelectedSeries(item);
    setQuery("");
    setBooks([]);
    setBookTotal(0);
    setBookHasMore(false);
    if (isSameSeries) {
      void loadBooksPage(item, 0, true, "");
    }
  }

  function scrollCollectionContentIntoView() {
    const scroll = (behavior: ScrollBehavior) => {
      const node = collectionContentRef.current;
      if (!node) return;
      const targetTop = Math.max(0, node.getBoundingClientRect().top + window.scrollY - 12);
      window.scrollTo({ top: targetTop, behavior });
    };
    window.requestAnimationFrame(() => {
      window.requestAnimationFrame(() => scroll("smooth"));
    });
    window.setTimeout(() => scroll("auto"), 300);
  }

  const loadBooksPage = useCallback(
    async (seriesItem: Series, offset: number, reset: boolean, queryOverride?: string) => {
      const requestID = ++bookListRequest.current;
      setBookListLoading(true);
      try {
        const page = await api.booksPage(seriesItem.id, {
          limit: bookPageSize,
          offset,
          q: queryOverride ?? query.trim(),
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
        if (reset && offset === 0) {
          scrollCollectionContentIntoView();
        }
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

  function resetProfileScopedViewState() {
    setView("library");
    setSelectedBook(null);
    setSelectedVideo(null);
    setVideoTranscodeStatus(null);
    setVideoTranscodeQueueStatus(null);
    setPages([]);
    setEpubManifest(null);
    setBookDetailsOpen(false);
    setPrivateDraft(emptyPrivateState());
    setPageIndex(0);
    setDisplayedPageIndex(0);
    setEpubPagePosition(0);
    setEpubPageCount(1);
    setPdfPageCount(1);
    setWebtoonProgress(0);
    setWebtoonPosition(null);
    setWebtoonRestorePosition(null);
    webtoonRestoring.current = false;
    webtoonUserActivated.current = false;
    webtoonUserScrollUntil.current = 0;
    suppressPagedProgressSave.current = false;
    setReaderImageSizes({});
    setEpubTocOpen(false);
    setReaderLoadState("idle");
    setBooks([]);
    setBookTotal(0);
    setBookHasMore(false);
    setContinueBooks([]);
    setRecentBooks([]);
    setFavoriteBooks([]);
    setWantBooks([]);
    setGlobalBooks([]);
  }

  async function leaveProfileScopedSurface() {
    try {
      if (document.fullscreenElement) {
        await document.exitFullscreen();
      }
    } catch {
      // The profile switch still needs to continue and return to Home.
    }
    setReaderFullscreen(false);
    resetProfileScopedViewState();
  }

  async function reloadSelectedSeries() {
    if (!selectedSeries) return;
    await loadBooksPage(selectedSeries, 0, true);
  }

  async function switchProfile(profileID: number) {
    if (!profileID || profileID === activeProfileId || profileSaving) return;
    const targetProfile = profiles.find((profile) => profile.id === profileID) ?? null;
    setProfileMenuOpen(false);
    setProfileSaving(true);
    persistActiveProfileId(profileID);
    setActiveProfileId(profileID);
    await leaveProfileScopedSurface();
    setStatus(t.profileSwitching);
    try {
      await refreshAll(true);
      await reloadSelectedSeries();
      setStatus(t.profileSwitched(targetProfile?.name ?? t.defaultProfile));
    } catch (error) {
      handleAPIError(error);
    } finally {
      setProfileSaving(false);
    }
  }

  async function createProfile() {
    const name = window.prompt(t.createProfilePrompt, "");
    const trimmedName = name?.trim();
    if (!trimmedName) return;
    setProfileSaving(true);
    try {
      const preset = profilePresetForIndex(profiles.length);
      const profile = await api.createProfile(trimmedName, preset.avatar, preset.color);
      persistActiveProfileId(profile.id);
      setActiveProfileId(profile.id);
      await leaveProfileScopedSurface();
      await refreshAll(true);
      await reloadSelectedSeries();
      setStatus(t.profileCreated(profile.name));
    } catch (error) {
      handleAPIError(error);
    } finally {
      setProfileSaving(false);
    }
  }

  async function renameActiveProfile() {
    if (!activeProfile || profileSaving) return;
    const name = window.prompt(t.renameProfilePrompt(activeProfile.name), activeProfile.name);
    const trimmedName = name?.trim();
    if (!trimmedName || trimmedName === activeProfile.name) return;
    setProfileSaving(true);
    try {
      const profile = await api.renameProfile(activeProfile.id, trimmedName);
      setProfiles((items) => items.map((item) => item.id === profile.id ? profile : item));
      setStatus(t.profileRenamed(profile.name));
    } catch (error) {
      handleAPIError(error);
    } finally {
      setProfileSaving(false);
    }
  }

  async function updateActiveProfileStyle(avatar: string, color: string) {
    if (!activeProfile || profileSaving) return;
    setProfileSaving(true);
    try {
      const profile = await api.updateProfile(activeProfile.id, { name: activeProfile.name, avatar, color });
      setProfiles((items) => items.map((item) => item.id === profile.id ? profile : item));
      setStatus(t.profileStyled(profile.name));
    } catch (error) {
      handleAPIError(error);
    } finally {
      setProfileSaving(false);
    }
  }

  useEffect(() => {
    if (!selectedVideo) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setSelectedVideo(null);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [selectedVideo]);

  useEffect(() => {
    previousVideoTranscodeStatus.current = "";
    setVideoTranscodeStatus(null);
    setVideoTranscodeQueueStatus(null);
    if (view !== "videos" || !selectedVideo || selectedVideo.playbackMode !== "hls") {
      return;
    }
    let cancelled = false;
    let timer: number | undefined;

    const poll = async () => {
      try {
        const [nextStatus, nextQueueStatus] = await Promise.all([
          api.videoTranscodeStatus(selectedVideo.id),
          api.videoTranscodeQueueStatus(),
        ]);
        if (cancelled) return;
        const previousStatus = previousVideoTranscodeStatus.current;
        previousVideoTranscodeStatus.current = nextStatus.status;
        setVideoTranscodeStatus(nextStatus);
        setVideoTranscodeQueueStatus(nextQueueStatus);
        if (nextStatus.status === "ready" && previousStatus && previousStatus !== "ready") {
          setVideoPlaybackReloadKey((key) => key + 1);
        }
        if (nextStatus.status !== "ready" && nextStatus.status !== "failed") {
          timer = window.setTimeout(poll, 2000);
        }
      } catch (error) {
        if (!cancelled) {
          setVideoTranscodeStatus({
            videoId: selectedVideo.id,
            status: "failed",
            message: error instanceof Error ? error.message : t.videoTranscodeStatusFailed,
            segmentCount: 0,
          });
        }
      }
    };

    void poll();
    return () => {
      cancelled = true;
      if (timer) window.clearTimeout(timer);
    };
  }, [selectedVideo, t.videoTranscodeStatusFailed, view]);

  useEffect(() => {
    if (view !== "videos" || !selectedVideo || !videoPlayerRef.current) return;
    const player = videoPlayerRef.current;
    const source = videoPlaybackSource(selectedVideo);
    if (!source) return;
    let disposed = false;
    let hls: { destroy: () => void; loadSource: (source: string) => void; attachMedia: (media: HTMLMediaElement) => void } | null = null;
    player.removeAttribute("src");
    if (selectedVideo.playbackMode === "hls") {
      if (player.canPlayType("application/vnd.apple.mpegurl")) {
        player.src = source;
        player.load();
      } else {
        void import("hls.js").then(({ default: Hls }) => {
          if (disposed) return;
          if (Hls.isSupported()) {
            hls = new Hls({ enableWorker: true });
            hls.loadSource(source);
            hls.attachMedia(player);
          } else {
            player.src = source;
            player.load();
          }
        });
      }
    } else {
      player.src = source;
      player.load();
    }
    return () => {
      disposed = true;
      hls?.destroy();
    };
  }, [selectedVideo, videoPlaybackReloadKey, view]);

  useEffect(() => {
    const node = bookLoadMoreRef.current;
    if (!node || !selectedSeries || !bookHasMore || bookListLoading) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          void loadBooksPage(selectedSeries, books.length, false);
        }
      },
      { rootMargin: "220px 0px" },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [bookHasMore, bookListLoading, books.length, loadBooksPage, selectedSeries]);

  useEffect(() => {
    if (view !== "jobs" || authRequired) return;
    let cancelled = false;
    const refresh = async () => {
      try {
        const nextStatus = await api.thumbnailWorkerStatus({ timeoutMs: 4_000 });
        if (!cancelled) setThumbnailWorkerStatus(nextStatus);
      } catch (error) {
        if (!cancelled) setStatus(error instanceof Error ? error.message : "Failed to load thumbnail worker");
      }
    };
    void refresh();
    const timer = window.setInterval(refresh, 2500);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [authRequired, view]);

  async function openBook(book: Book) {
    setActiveTask(`Opening ${book.title}`);
    setSelectedBook(book);
    setView("reader");
    setEpubManifest(null);
    setPageIndex(0);
    setDisplayedPageIndex(0);
    setEpubPagePosition(0);
    setEpubPageCount(1);
    setPdfPageCount(1);
    setWebtoonProgress(0);
    setWebtoonPosition(null);
    setWebtoonRestorePosition(null);
    webtoonRestoring.current = false;
    webtoonUserActivated.current = false;
    webtoonUserScrollUntil.current = 0;
    suppressPagedProgressSave.current = false;
    setReaderImageSizes({});
    setEpubTocOpen(false);
    setReaderLoadState("loading");
    try {
      await nextFrame();
      const nextPages = await api.pages(book.id);
      setPages(nextPages);
      if (book.format === "epub") {
        const [manifestResult, progressResult] = await Promise.allSettled([api.epubManifest(book.id), api.readProgress(book.id)]);
        const manifest = manifestResult.status === "fulfilled" ? manifestResult.value : fallbackEpubManifest(book, nextPages);
        const progress = progressResult.status === "fulfilled" ? progressResult.value : null;
        const restoredPosition = readEpubLocator(progress?.locator ?? "");
        epubRestorePosition.current = restoredPosition;
        setEpubManifest(manifest);
        setPageIndex(Math.max(0, Math.min(progress?.pageIndex ?? 0, Math.max(0, nextPages.length - 1))));
        setEpubPagePosition(restoredPosition);
        if (manifestResult.status === "rejected") {
          setStatus(`EPUB metadata fallback: ${manifestResult.reason instanceof Error ? manifestResult.reason.message : "manifest unavailable"}`);
        }
        setReaderLoadState("ready");
      } else {
        const [progressResult, positionsResult] = await Promise.allSettled([api.readProgress(book.id), api.readingPositions(book.id)]);
        const progress = progressResult.status === "fulfilled" ? progressResult.value : { pageIndex: 0, locator: "", progressFraction: 0 };
        const restoredWebtoonPosition =
          book.format !== "pdf" && positionsResult.status === "fulfilled" ? positionsResult.value.positions.webtoon ?? null : null;
        const restoredPage = book.format === "pdf" ? Math.max(0, progress.pageIndex) : Math.max(0, Math.min(progress.pageIndex, Math.max(0, nextPages.length - 1)));
        if (restoredWebtoonPosition) {
          const restoredWebtoonPage = webtoonInitialPageIndex(restoredWebtoonPosition, nextPages);
          setWebtoonPosition(restoredWebtoonPosition);
          setWebtoonRestorePosition(restoredWebtoonPosition);
          setWebtoonProgress(restoredWebtoonPosition.documentProgress);
          setPageIndex(restoredWebtoonPage);
          setDisplayedPageIndex(restoredWebtoonPage);
        } else {
          const restoredWebtoonProgress = readWebtoonLocator(progress.locator);
          if (restoredWebtoonProgress !== null) {
            const legacyWebtoonPosition = legacyWebtoonProgressPosition(restoredWebtoonProgress, nextPages.length);
            const restoredWebtoonPage = webtoonInitialPageIndex(legacyWebtoonPosition, nextPages);
            setWebtoonProgress(restoredWebtoonProgress);
            setWebtoonPosition(legacyWebtoonPosition);
            setWebtoonRestorePosition(legacyWebtoonPosition);
            setPageIndex(restoredWebtoonPage);
            setDisplayedPageIndex(restoredWebtoonPage);
          } else {
            setPageIndex(restoredPage);
            setDisplayedPageIndex(restoredPage);
          }
        }
        setReaderLoadState("ready");
      }
    } catch (error) {
      setReaderLoadState("error");
      setStatus(error instanceof Error ? error.message : `Failed to open ${book.title}`);
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

  function mergeCollectionState(updatedCollection: Series) {
    setSeries((items) => replaceSeries(items, updatedCollection));
    setSelectedSeries((current) => (current?.id === updatedCollection.id ? updatedCollection : current));
  }

  async function updateCollectionState(collection: Series, patch: Partial<CollectionPrivateState>) {
    const nextState = {
      favorite: patch.favorite ?? collection.favorite,
      liked: patch.liked ?? collection.liked,
    };
    try {
      const updatedCollection = await api.collectionPrivateState(collection.id, nextState);
      mergeCollectionState(updatedCollection);
      setStatus(t.collectionStateSaved);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : t.collectionStateFailed);
    }
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
    const totalPages = book.format === "pdf" ? pdfPageCount : pages.length;
    const clamped = Math.max(0, Math.min(nextIndex, Math.max(0, totalPages - 1)));
    if (book.format === "epub") {
      setEpubPagePosition(0);
      setEpubPageCount(1);
    }
    if (readerPageMode === "webtoon" && book.format !== "epub" && book.format !== "pdf") {
      requestAnimationFrame(() => scrollWebtoonToPage(clamped));
    }
    suppressPagedProgressSave.current = false;
    if (clamped !== pageIndex) {
      setReaderLoadState("loading");
    }
    setPageIndex(clamped);
  }

  function changeComicReaderPageMode(nextMode: ReaderPageMode) {
    if (readerPageMode === nextMode) return;
    if (nextMode === "webtoon" && selectedBook && selectedBook.format !== "epub" && selectedBook.format !== "pdf") {
      const currentPageKey = webtoonPageKey(pages[pageIndex]);
      const canReuseWebtoonPosition =
        webtoonPosition !== null &&
        ((currentPageKey !== "" && webtoonPosition.pageKey === currentPageKey) || webtoonPosition.pageIndex === pageIndex);
      const nextPosition = canReuseWebtoonPosition ? webtoonPosition : webtoonPositionForPage(pageIndex, pages);
      const nextPageIndex = webtoonInitialPageIndex(nextPosition, pages);
      setWebtoonPosition(nextPosition);
      setWebtoonProgress(nextPosition.documentProgress);
      setWebtoonRestorePosition(nextPosition);
      setPageIndex(nextPageIndex);
      setDisplayedPageIndex(nextPageIndex);
      webtoonRestoring.current = true;
      webtoonUserActivated.current = false;
      webtoonUserScrollUntil.current = 0;
      setReaderLoadState("ready");
    } else {
      setWebtoonRestorePosition(null);
      webtoonRestoring.current = false;
      webtoonUserScrollUntil.current = 0;
      if (nextMode !== "webtoon") {
        suppressPagedProgressSave.current = true;
      }
    }
    setReaderPageMode(nextMode);
  }

  useEffect(() => {
    if (!selectedBook || pages.length === 0 || selectedBook.format === "epub" || selectedBook.format === "pdf") return;
    if (readerPageMode === "webtoon") {
      setDisplayedPageIndex(pageIndex);
      setReaderLoadState("ready");
      return;
    }

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
    if (!selectedBook || readerPageMode !== "webtoon" || selectedBook.format === "epub" || selectedBook.format === "pdf") return;
    const target = webtoonRestorePosition;
    if (target === null) return;
    const timer = window.setTimeout(() => {
      const node = webtoonRef.current;
      if (!node) return;
      const targetPageIndex = webtoonInitialPageIndex(target, pages);
      if (!isWebtoonRestoreTargetReady(node, targetPageIndex)) return;
      const restoreTarget = resolveWebtoonRestoreTarget({
        position: target,
        pages: collectWebtoonPageMetrics(node, pages),
        viewportHeight: node.clientHeight,
      });
      if (!restoreTarget) return;
      webtoonRestoring.current = true;
      node.scrollTop = restoreTarget.scrollTop;
      setPageIndex((value) => (value === restoreTarget.pageIndex ? value : restoreTarget.pageIndex));
      setDisplayedPageIndex((value) => (value === restoreTarget.pageIndex ? value : restoreTarget.pageIndex));
      window.requestAnimationFrame(() => {
        setWebtoonRestorePosition(null);
        webtoonRestoring.current = false;
      });
    }, 120);
    return () => window.clearTimeout(timer);
  }, [selectedBook?.id, readerPageMode, webtoonRestorePosition, pages, webtoonPageHeights]);

  useEffect(() => {
    setWebtoonPageHeights({});
  }, [selectedBook?.id, readerPageMode]);

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
  }, [view, selectedBook, pageIndex, pages.length, pdfPageCount, readerPageMode, readerFullscreen, epubPagePosition, epubPageCount]);

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
  }, [view, selectedBook, pageIndex, pages.length, pdfPageCount, readerPageMode, epubPagePosition, epubPageCount]);

  function comicPageDisplayPath(bookID: number, page: Page | undefined, index: number) {
    return page?.displayUrl || page?.url || `/api/books/${bookID}/pages/${index}?maxWidth=1200`;
  }

  function preloadPage(bookID: number, index: number) {
    const src = authenticatedResourcePath(comicPageDisplayPath(bookID, pages[index], index));
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
    if (mode === "webtoon") return [];
    if (mode === "single") return [index];
    return [index, index + 1].filter((next) => next >= 0 && next < total);
  }

  function readerStep() {
    if (selectedBook?.format === "epub") return 1;
    if (readerPageMode === "webtoon") return 1;
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
    if (readerPageMode === "webtoon" && selectedBook.format !== "pdf") {
      scrollWebtoonByPage(-1);
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
    if (readerPageMode === "webtoon" && selectedBook.format !== "pdf") {
      scrollWebtoonByPage(1);
      return;
    }
    setReaderPage(selectedBook, pageIndex + readerStep());
  }

  function scrollWebtoonByPage(direction: 1 | -1) {
    const node = webtoonRef.current;
    if (!node) return;
    markWebtoonUserScroll();
    node.scrollBy({ top: direction * Math.max(320, node.clientHeight * 0.88), behavior: "smooth" });
  }

  function scrollWebtoonToPage(index: number) {
    const node = webtoonRef.current;
    const target = node?.querySelector<HTMLElement>(`[data-page-index="${index}"]`);
    if (!node || !target) return;
    markWebtoonUserScroll();
    node.scrollTo({ top: Math.max(0, target.offsetTop - 10), behavior: "smooth" });
  }

  function markWebtoonUserScroll() {
    webtoonUserScrollUntil.current = Date.now() + 1200;
  }

  function handleWebtoonScroll() {
    if (webtoonRestoring.current || webtoonRestorePosition !== null) return;
    if (Date.now() > webtoonUserScrollUntil.current) return;
    updateWebtoonPosition(true);
  }

  function updateWebtoonPosition(userInitiated = false) {
    const node = webtoonRef.current;
    if (!node) return;
    if (webtoonRestorePosition !== null && !userInitiated) return;
    const metrics = collectWebtoonPageMetrics(node, pages);
    if (metrics.length === 0) return;
    const position = stabilizeWebtoonDocumentProgress(
      buildWebtoonPosition({
        pages: metrics,
        scrollTop: node.scrollTop,
        viewportHeight: node.clientHeight,
      }),
      webtoonPosition,
      metrics,
    );
    if (userInitiated) {
      webtoonUserActivated.current = true;
      setWebtoonPosition(position);
      setPageIndex((value) => (value === position.pageIndex ? value : position.pageIndex));
      setDisplayedPageIndex((value) => (value === position.pageIndex ? value : position.pageIndex));
    }
    setWebtoonProgress(position.documentProgress);
  }

  function handleWebtoonImageLoad(event: SyntheticEvent<HTMLImageElement>, page: Page) {
    recordReaderImageSize(page, event.currentTarget);
    const height = Math.ceil(event.currentTarget.getBoundingClientRect().height);
    if (height > 0) {
      setWebtoonPageHeights((items) => (items[page.index] === height ? items : { ...items, [page.index]: height }));
    }
    updateWebtoonPosition();
  }

  function handleComicImageLoad(event: SyntheticEvent<HTMLImageElement>, page: Page | undefined) {
    recordReaderImageSize(page, event.currentTarget);
  }

  function recordReaderImageSize(page: Page | undefined, image: HTMLImageElement) {
    if (!page || image.naturalWidth <= 0 || image.naturalHeight <= 0) return;
    const next = { width: image.naturalWidth, height: image.naturalHeight };
    setReaderImageSizes((items) => {
      const current = items[page.index];
      if (current?.width === next.width && current.height === next.height) return items;
      return { ...items, [page.index]: next };
    });
  }

  function comicImageFitStyle(page: Page | undefined, mode: ReaderImageFitMode): CSSProperties {
    if (!readerFullscreen || !page) return {};
    const size = readerImageSizes[page.index];
    if (!size) return {};
    const fit = fullscreenImageFit({
      naturalWidth: size.width,
      naturalHeight: size.height,
      devicePixelRatio: typeof window === "undefined" ? 1 : window.devicePixelRatio || 1,
      mode,
    });
    return {
      "--reader-fit-width": `${fit.maxCssWidth}px`,
      "--reader-fit-height": `${fit.maxCssHeight}px`,
    } as CSSProperties;
  }

  function jumpToEpubChapter(item: EpubTocItem) {
    if (!selectedBook || !epubManifest) return;
    jumpToEpubHref(item.href, item.label, item.index);
  }

  function jumpToEpubHref(href: string, label?: string, fallbackIndex?: number) {
    if (!epubManifest) return;
    const index = resolveEpubHrefIndex(epubManifest, href, fallbackIndex);
    epubRestorePosition.current = null;
    setEpubTocOpen(false);
    setEpubPagePosition(0);
    setEpubPageCount(1);
    setDisplayedPageIndex(index);
    setReaderLoadState("loading");
    setPageIndex(index);
    setStatus(`Opening ${label || href}`);
  }

  async function returnToLibrary() {
    try {
      if (document.fullscreenElement) {
        await document.exitFullscreen();
      }
    } catch (error) {
      setStatus(error instanceof Error ? `Fullscreen unavailable: ${error.message}` : "Fullscreen unavailable");
    } finally {
      setReaderFullscreen(false);
      setView("library");
    }
  }

  async function toggleReaderFullscreen() {
    if (!readerRef.current) return;
    try {
      if (document.fullscreenElement === readerRef.current) {
        await document.exitFullscreen();
        setReaderFullscreen(false);
        return;
      }
      if (readerFullscreen) {
        setReaderFullscreen(false);
        return;
      }
      if (readerRef.current.requestFullscreen) {
        await readerRef.current.requestFullscreen();
        setReaderFullscreen(true);
      } else {
        setReaderFullscreen(true);
      }
    } catch (error) {
      setReaderFullscreen((value) => !value);
      setStatus(error instanceof Error ? `Using in-app fullscreen: ${error.message}` : "Using in-app fullscreen");
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
  const collectionSections = useMemo(() => {
    const sections = [
      { key: "comic", title: t.comicCollections, items: [] as Series[] },
      { key: "book", title: t.bookCollections, items: [] as Series[] },
    ];
    for (const item of filteredSeries) {
      const kind = collectionKind(item, libraries);
      const section = sections.find((candidate) => candidate.key === kind);
      section?.items.push(item);
    }
    return sections.filter((section) => section.items.length > 0);
  }, [filteredSeries, libraries, t]);
  const favoriteCollections = useMemo(() => series.filter((item) => item.favorite), [series]);
  const likedCollections = useMemo(() => series.filter((item) => item.liked), [series]);
  const favoritePageItemCount = favoriteCollections.length + likedCollections.length + favoriteBooks.length;

  useEffect(() => {
    const node = collectionSectionsRef.current;
    if (!node) return;
    if (node.scrollTop !== collectionScrollTop.current) {
      node.scrollTop = collectionScrollTop.current;
    }
  }, [collectionSections]);

  const scanProgressLabel = activeScan
    ? `${activeScan.indexedFiles} indexed · ${activeScan.skippedFiles} skipped · ${activeScan.errorCount} errors`
    : null;
  const activeScanElapsed = activeScan ? formatElapsed(activeScan, nowTick) : null;
  const selectedJobLatest = selectedJob ? jobs.find((job) => job.id === selectedJob.id) ?? selectedJob : null;
  const useWebtoonReader = selectedBook ? readerPageMode === "webtoon" && selectedBook.format !== "epub" : false;
  const selectedBookReaderClass = selectedBook
    ? selectedBook.format === "pdf"
      ? "pdfBookReader"
      : `${selectedBook.format}Reader`
    : "";
  const readerClassName = selectedBook
    ? `reader ${selectedBookReaderClass}${useWebtoonReader ? " webtoonMode" : ""}${readerFullscreen ? " immersiveMode" : ""}`
    : "reader";
  const visibleContinueBooks = continueBooks.slice(0, 4);

  return (
    <main className={view === "reader" ? "app readerMode" : "app"}>
      <aside className="sidebar">
        <div className="brand">FolioSpace Library</div>
        <button className={view === "library" ? "active" : ""} onClick={returnToLibrary}>
          {t.library}
        </button>
        <button className={view === "favorites" ? "active" : ""} onClick={() => setView("favorites")}>
          {t.favorites}
        </button>
        <button className={view === "reader" ? "active" : ""} onClick={() => setView("reader")}>
          {t.reader}
        </button>
        <button className={view === "games" ? "active" : ""} onClick={openGameCatalog}>
          {t.gameShelf}
        </button>
        <button className={view === "videos" ? "active" : ""} onClick={openVideoCatalog}>
          {t.videoShelf}
        </button>
        <button className={view === "jobs" ? "active" : ""} onClick={() => setView("jobs")}>
          {t.jobs}
        </button>
        <button className={view === "errors" ? "active" : ""} onClick={() => setView("errors")}>
          {t.errors}
        </button>
        <button className={view === "about" ? "active" : ""} onClick={() => setView("about")}>
          {t.about}
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

        {directoryPickerOpen && (
          <section className="modalOverlay" role="dialog" aria-modal="true" aria-label={t.directoryPickerTitle}>
            <div className="directoryPicker">
              <div className="directoryPickerHeader">
                <div>
                  <h1>{t.directoryPickerTitle}</h1>
                  <small>{t.directoryPickerHint}</small>
                </div>
                <button type="button" onClick={() => setDirectoryPickerOpen(false)}>{t.close}</button>
              </div>
              <code>{directoryListing?.path || "/"}</code>
              <div className="directoryPickerActions">
                <button
                  type="button"
                  disabled={!directoryListing?.parent || directoryPickerLoading}
                  onClick={() => directoryListing?.parent && loadDirectory(directoryListing.parent)}
                >
                  {t.parentDirectory}
                </button>
                <button
                  type="button"
                  disabled={!directoryListing || directoryPickerLoading}
                  onClick={() => directoryListing && selectDirectory(directoryListing.path)}
                >
                  {t.selectThisDirectory}
                </button>
              </div>
              {directoryPickerError && <p className="directoryPickerError">{directoryPickerError}</p>}
              <div className="directoryList">
                {directoryPickerLoading ? (
                  <span>{t.loadingDirectories}</span>
                ) : directoryListing && directoryListing.entries.length > 0 ? (
                  directoryListing.entries.map((entry) => (
                    <button type="button" key={entry.path} onClick={() => loadDirectory(entry.path)}>
                      <span>{entry.name}</span>
                      <small>{entry.path}</small>
                    </button>
                  ))
                ) : (
                  <span>{t.noDirectories}</span>
                )}
              </div>
            </div>
          </section>
        )}

        <header className="topbar">
          <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t.searchLibrary} />
          <div className="profileControls" aria-label={t.profile}>
            <button
              type="button"
              className="profileMenuButton"
              onClick={() => setProfileMenuOpen((value) => !value)}
              disabled={profileSaving || profiles.length === 0}
              aria-label={t.profile}
              aria-expanded={profileMenuOpen}
            >
              {activeProfile ? <ProfileAvatar profile={activeProfile} /> : <ProfileAvatar profile={fallbackProfile(t.defaultProfile)} />}
              <span>{activeProfileLabel}</span>
              <svg viewBox="0 0 24 24" focusable="false" aria-hidden="true">
                <path d="m6 9 6 6 6-6" />
              </svg>
            </button>
            {profileMenuOpen && (
              <div className="profilePanel" role="menu">
                <div className="profilePanelHeader">
                  <strong>{t.profile}</strong>
                  <small>{t.profileSharedLibrary}</small>
                </div>
                <div className="profileGrid">
                  {profiles.map((profile) => (
                    <button
                      type="button"
                      key={profile.id}
                      className={profile.id === activeProfile?.id ? "profileCard selected" : "profileCard"}
                      onClick={() => switchProfile(profile.id)}
                      disabled={profileSaving}
                    >
                      <ProfileAvatar profile={profile} />
                      <span>{profileDisplayName(profile, t)}</span>
                    </button>
                  ))}
                </div>
                {activeProfile && (
                  <div className="profileStylePanel">
                    <small>{t.profileStyle}</small>
                    <div className="profilePresetGrid">
                      {profilePresets.map((preset) => (
                        <button
                          type="button"
                          key={`${preset.avatar}-${preset.color}`}
                          className={activeProfile.avatar === preset.avatar && activeProfile.color === preset.color ? "selected" : ""}
                          onClick={() => updateActiveProfileStyle(preset.avatar, preset.color)}
                          disabled={profileSaving}
                          title={preset.label}
                        >
                          <ProfileAvatar profile={{ ...activeProfile, avatar: preset.avatar, color: preset.color }} />
                        </button>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            )}
            <button type="button" className="profileIconButton" onClick={createProfile} disabled={profileSaving} title={t.newProfile} aria-label={t.newProfile}>
              +
            </button>
            <button type="button" className="profileRenameButton" onClick={renameActiveProfile} disabled={profileSaving || !activeProfile} title={t.renameProfile} aria-label={t.renameProfile}>
              <svg viewBox="0 0 24 24" focusable="false" aria-hidden="true">
                <path d="M12 20h9" />
                <path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z" />
              </svg>
            </button>
          </div>
          <select className="localeSelect" value={locale} onChange={(event) => setLocale(event.target.value as Locale)} aria-label={t.language}>
            <option value="zh">中文</option>
            <option value="zht">繁體中文</option>
            <option value="en">English</option>
            <option value="ja">日本語</option>
            <option value="ko">한국어</option>
          </select>
          <span className="statusText">{activeScan ? `Scanning: ${scanProgressLabel} · ${t.elapsed} ${activeScanElapsed}` : status}</span>
        </header>

        {activeScan && (
          <section className="scanProgress" role="status" aria-live="polite">
            <div>
              <strong>Scan job #{activeScan.id}</strong>
              <small>
                {scanProgressLabel} · {t.elapsed} {activeScanElapsed}
              </small>
            </div>
            <div className="scanMeter">
              <div />
            </div>
          </section>
        )}

        {view === "library" && (
          <div className="libraryDashboard">
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
                          <BookCover book={book} />
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

            <section className="libraryDirectory panel">
              <h1>{t.libraries}</h1>
              <form className="libraryForm" onSubmit={addLibrary}>
                <input
                  value={newLibraryName}
                  onChange={(event) => setNewLibraryName(event.target.value)}
                  placeholder={t.name}
                />
                <input
                  className="libraryPathInput"
                  value={newLibraryPath}
                  readOnly
                  placeholder="/volume2/ComicCenter"
                  onClick={() => openDirectoryPicker()}
                />
                <button type="button" onClick={() => openDirectoryPicker()}>{t.chooseDirectory}</button>
                <select value={newLibraryAssetType} onChange={(event) => setNewLibraryAssetType(event.target.value as LibraryAssetType)} aria-label={t.libraryAssetType}>
                  <option value="mixed">{t.assetTypeMixed}</option>
                  <option value="comic">{t.assetTypeComic}</option>
                  <option value="book">{t.assetTypeBook}</option>
                  <option value="game">{t.assetTypeGame}</option>
                  <option value="video">{t.assetTypeVideo}</option>
                </select>
                <button disabled={!newLibraryPath.trim()}>{t.add}</button>
              </form>
              <div className="libraryRows">
                {libraries.map((library) => (
                  <div className="row" key={library.id}>
                    <div>
                      <strong>{library.name}</strong>
                      <small>{library.rootPath} · {libraryAssetTypeLabel(library.assetType, t)}</small>
                    </div>
                    <div className="rowActions">
                      <button onClick={() => scan(library)}>{t.scan}</button>
                      <button className="danger" onClick={() => deleteLibrary(library)}>{t.delete}</button>
                    </div>
                  </div>
                ))}
              </div>
            </section>

            {(visibleContinueBooks.length > 0 || favoriteBooks.length > 0 || wantBooks.length > 0 || gameShelf.length > 0 || videoShelf.length > 0 || recentBooks.length > 0) && (
              <section className="homeRows quickShelfPanel panel wide" aria-label="Reading shortcuts">
                <div className="quickShelfColumn">
                  {visibleContinueBooks.length > 0 && (
                    <BookShelf
                      title={t.continueReading}
                      subtitle={t.continueSubtitle}
                      books={visibleContinueBooks}
                      onOpen={openBook}
                      meta={(book) => continueMeta(book, t)}
                      largeCovers={visibleContinueBooks.length < 4}
                      progress
                    />
                  )}
                  {favoriteBooks.length > 0 && (
                    <BookShelf
                      title={t.favorites}
                      subtitle={t.favoriteSubtitle}
                      books={favoriteBooks.slice(0, 4)}
                      onOpen={openBook}
                      meta={(book) => privateShelfMeta(book, t)}
                    />
                  )}
                  {wantBooks.length > 0 && (
                    <BookShelf
                      title={t.wantToRead}
                      subtitle={t.wantSubtitle}
                      books={wantBooks.slice(0, 4)}
                      onOpen={openBook}
                      meta={(book) => privateShelfMeta(book, t)}
                    />
                  )}
                </div>
                <span className="quickShelfDivider" aria-hidden="true" />
                <div className="quickShelfColumn">
                  {gameShelf.length > 0 && (
                    <GameShelf
                      title={t.gameShelf}
                      subtitle={t.gameShelfSubtitle}
                      games={gameShelf.slice(0, 4)}
                      meta={(game) => gameMeta(game, t)}
                      moreLabel={t.more}
                      onMore={openGameCatalog}
                    />
                  )}
                  {videoShelf.length > 0 && (
                    <VideoShelf
                      title={t.videoShelf}
                      subtitle={t.videoShelfSubtitle}
                      videos={videoShelf.slice(0, 4)}
                      meta={(video) => videoMeta(video, t)}
                      onOpen={(video) => {
                        setSelectedVideo(video);
                        void openVideoCatalog();
                      }}
                      moreLabel={t.more}
                      onMore={openVideoCatalog}
                    />
                  )}
                  {recentBooks.length > 0 && (
                    <BookShelf
                      title={t.recentlyAddedTitle}
                      subtitle={t.recentSubtitle}
                      books={recentBooks.slice(0, 4)}
                      onOpen={openBook}
                      meta={(book) => recentMeta(book, t)}
                    />
                  )}
                </div>
              </section>
            )}

            <section className="collectionPanel panel">
              <h1>{t.collections}</h1>
              <div
                className="collectionSections"
                ref={collectionSectionsRef}
                onScroll={(event) => {
                  collectionScrollTop.current = event.currentTarget.scrollTop;
                }}
              >
                {collectionSections.map((section) => (
                  <section className="collectionSection" key={section.key} aria-label={section.title}>
                    <h2>{section.title}</h2>
                    <div className="collectionGrid">
                      {section.items.map((item) => (
                        <CollectionCard
                          key={item.id}
                          item={item}
                          selected={selectedSeries?.id === item.id}
                          labels={t}
                          onOpen={openSeries}
                          onStateChange={updateCollectionState}
                        />
                      ))}
                    </div>
                  </section>
                ))}
              </div>
            </section>

            <section className="coverWall panel collectionContent" ref={collectionContentRef}>
              <div className="coverWallHeader">
                <div>
                  <h1>{selectedSeries ? selectedSeries.title : t.volumeWall}</h1>
                  <small>
                    {selectedSeries ? loadedCollectionCountLabel(selectedSeries, books.length, 0, 0) : t.selectCollection}
                  </small>
                </div>
                <div className="coverWallTools">
                  {selectedSeries && <span>{collectionCountLabel(selectedSeries)}</span>}
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
                        <BookCover book={book} />
                        <span className="coverBadge">{book.format.toUpperCase()}</span>
                      </span>
                      <strong>{book.title}</strong>
                      {book.creator && <small className="bookCreator">{book.creator}</small>}
                      <small>
                        {t.singleVolume} · {book.pageCount ? t.pageCount(book.pageCount) : t.notAnalyzed}
                      </small>
                      {book.description && <small className="bookDescription">{book.description}</small>}
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

        {view === "favorites" && (
          <section className="favoritesPage">
            <div className="catalogHeader">
              <div>
                <h1>{t.favorites}</h1>
                <small>{t.favoriteSubtitle}</small>
              </div>
              <span>{t.catalogLoadedCount(favoritePageItemCount, favoritePageItemCount)}</span>
            </div>

            <div className="favoritesSections">
              <section className="panel favoriteSection">
                <h2>{t.favoriteCollections}</h2>
                {favoriteCollections.length > 0 ? (
                  <div className="collectionGrid">
                    {favoriteCollections.map((item) => (
                      <CollectionCard
                        key={`favorite-collection-${item.id}`}
                        item={item}
                        selected={selectedSeries?.id === item.id}
                        labels={t}
                        onOpen={openSeries}
                        onStateChange={updateCollectionState}
                      />
                    ))}
                  </div>
                ) : (
                  <div className="coverEmpty compact">
                    <strong>{t.noFavorites}</strong>
                    <small>{t.chooseCollectionHint}</small>
                  </div>
                )}
              </section>

              <section className="panel favoriteSection">
                <h2>{t.likedCollections}</h2>
                {likedCollections.length > 0 ? (
                  <div className="collectionGrid">
                    {likedCollections.map((item) => (
                      <CollectionCard
                        key={`liked-collection-${item.id}`}
                        item={item}
                        selected={selectedSeries?.id === item.id}
                        labels={t}
                        onOpen={openSeries}
                        onStateChange={updateCollectionState}
                      />
                    ))}
                  </div>
                ) : (
                  <div className="coverEmpty compact">
                    <strong>{t.noFavorites}</strong>
                    <small>{t.chooseCollectionHint}</small>
                  </div>
                )}
              </section>

              <section className="panel favoriteSection wide">
                <h2>{t.favoriteBooks}</h2>
                {favoriteBooks.length > 0 ? (
                  <div className="books">
                    {favoriteBooks.map((book) => (
                      <button className="book" key={`favorite-book-${book.id}`} onClick={() => openBook(book)} title={book.title}>
                        <span className="coverFrame">
                          <BookCover book={book} />
                          <span className="coverBadge">{book.format.toUpperCase()}</span>
                        </span>
                        <strong>{book.title}</strong>
                        {book.creator && <small className="bookCreator">{book.creator}</small>}
                        <small>{privateShelfMeta(book, t)}</small>
                      </button>
                    ))}
                  </div>
                ) : (
                  <div className="coverEmpty compact">
                    <strong>{t.noFavorites}</strong>
                    <small>{t.favoriteSubtitle}</small>
                  </div>
                )}
              </section>
            </div>
          </section>
        )}

        {view === "games" && (
          <CatalogPage
            title={t.gameShelf}
            subtitle={t.gameCatalogSubtitle}
            countLabel={gameCatalogLoading && gameCatalog.length === 0 ? t.loadingGames : t.catalogLoadedCount(gameCatalog.length, gameCatalogTotal)}
          >
            <div className="catalogGrid games">
              {[...gameCatalog].sort(compareGamesByPlatform).map((game) => (
                <GameTile key={`catalog-game-${game.id}`} game={game} meta={gameMeta(game, t)} />
              ))}
              {gameCatalogHasMore && (
                <button className="catalogLoadMore" type="button" disabled={gameCatalogLoading} onClick={() => loadGameCatalogPage(gameCatalog.length)}>
                  {gameCatalogLoading ? t.loadingGames : t.loadMore}
                </button>
              )}
            </div>
          </CatalogPage>
        )}

        {view === "videos" && (
          <section className="catalogPage videoCatalogPage">
            <div className="catalogHeader">
              <div>
                <h1>{t.videoShelf}</h1>
                <small>{videoCatalogLoading && videoCatalog.length === 0 ? t.loadingVideos : t.catalogLoadedCount(videoCatalog.length, videoCatalogTotal)}</small>
                <span>{t.videoCoverHint}</span>
              </div>
            </div>
            {selectedVideo && (
              <div className="inlineVideoPlayer">
                <div>
                  <strong>{selectedVideo.title}</strong>
                  <small>{videoMeta(selectedVideo, t)}</small>
                </div>
                <video
                  ref={videoPlayerRef}
                  key={`${selectedVideo.id}-${videoPlaybackReloadKey}`}
                  className="videoPlayer"
                  controls
                  preload="metadata"
                  poster={selectedVideo.thumbnailUrl}
                />
                {!selectedVideo.directPlayable && (
                  <div className="videoTranscodePanel">
                    <span className={`videoTranscodeStatus ${videoTranscodeStatus?.status || "idle"}`}>
                      {videoTranscodeLabel(videoTranscodeStatus, t)}
                    </span>
                    <small className="videoTranscodeHint">
                      {selectedVideo.playbackReason || t.videoTranscodeHint}
                    </small>
                    {videoTranscodeQueueStatus?.activeVideoId && videoTranscodeQueueStatus.activeVideoId !== selectedVideo.id && (
                      <small className="videoTranscodeHint">
                        {t.videoCurrentTranscode(videoTranscodeQueueStatus.activeTitle || `#${videoTranscodeQueueStatus.activeVideoId}`)}
                      </small>
                    )}
                    <button type="button" onClick={() => setVideoPlaybackReloadKey((key) => key + 1)}>
                      {t.videoReloadPlayback}
                    </button>
                  </div>
                )}
              </div>
            )}
            <div className="catalogGrid videos">
              {[...videoCatalog].sort(compareVideosByTitle).map((video) => (
                <VideoTile
                  className={selectedVideo?.id === video.id ? "book selected" : "book"}
                  key={`catalog-video-${video.id}`}
                  video={video}
                  meta={videoMeta(video, t)}
                  onOpen={setSelectedVideo}
                />
              ))}
              {videoCatalogHasMore && (
                <button className="catalogLoadMore" type="button" disabled={videoCatalogLoading} onClick={() => loadVideoCatalogPage(videoCatalog.length)}>
                  {videoCatalogLoading ? t.loadingVideos : t.loadMore}
                </button>
              )}
            </div>
          </section>
        )}

        {view === "about" && (
          <section className="aboutPage panel">
            <div className="aboutHero">
              <span className="aboutMark">FS</span>
              <div>
                <h1>{clientInfo?.serviceName || "FolioSpace Library"}</h1>
                <p>{t.aboutSubtitle}</p>
              </div>
            </div>
            <div className="aboutGrid">
              <div>
                <span>{t.version}</span>
                <strong>{clientInfo?.serviceVersion || "0.90"}</strong>
              </div>
              <div>
                <span>{t.apiVersion}</span>
                <strong>{clientInfo?.apiVersion || "v1"}</strong>
              </div>
              <div>
                <span>{t.copyright}</span>
                <strong>Copyright © 2026 funland co.,Ltd.</strong>
              </div>
            </div>
            <section className="aboutSection">
              <h2>{t.supportedFormats}</h2>
              <div className="formatChips">
                {(clientInfo?.supportedFormats || ["cbz", "zip", "epub", "pdf", "mp4", "mkv"]).map((format) => (
                  <span key={format}>{format.toUpperCase()}</span>
                ))}
              </div>
            </section>
            <section className="aboutSection">
              <h2>{t.capabilities}</h2>
              <div className="capabilityList">
                {Object.entries(clientInfo?.capabilities || {})
                  .filter(([, enabled]) => enabled)
                  .slice(0, 18)
                  .map(([name]) => (
                    <span key={name}>{formatCapabilityName(name)}</span>
                  ))}
              </div>
            </section>
          </section>
        )}

        {view === "reader" && (
          <section className={readerClassName} ref={readerRef}>
            {selectedBook ? (
              <>
                <div className="readerHeader">
                  <div className="readerTitle">
                    <strong>{selectedBook.title}</strong>
                    {selectedBook.creator && <em>{selectedBook.creator}</em>}
                    <span>
                      {selectedBook.format === "epub" ? "Chapter " : ""}
                      {pageIndex + 1}
                      {selectedBook.format !== "epub" && readerPageMode === "double" && pageIndex + 1 < readerTotalPages(selectedBook, pages.length, pdfPageCount)
                        ? `-${pageIndex + 2}`
                        : ""} /{" "}
                      {Math.max(readerTotalPages(selectedBook, pages.length, pdfPageCount), 1)}
                    </span>
                  </div>
                  <div className="readerToolbar" aria-label="Reader options">
                    <button onClick={returnToLibrary}>{t.backToShelf}</button>
                    {selectedBook.format === "epub" ? (
                      <>
                        <button className="epubContentsButton" onClick={() => setEpubTocOpen((value) => !value)}>{t.contents}</button>
                        <div className="segmentedControl epubPageModeControl" role="group" aria-label="EPUB page mode">
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
                          onClick={() => changeComicReaderPageMode("single")}
                        >
                          {t.single}
                        </button>
                        <button
                          className={readerPageMode === "double" ? "selected" : ""}
                          onClick={() => changeComicReaderPageMode("double")}
                        >
                          {t.double}
                        </button>
                        <button
                          className={readerPageMode === "webtoon" ? "selected" : ""}
                          onClick={() => changeComicReaderPageMode("webtoon")}
                        >
                          {t.webtoon}
                        </button>
                      </div>
                    )}
                    <button className="readerFullscreenButton" onClick={toggleReaderFullscreen}>{readerFullscreen ? t.exitFullscreen : t.fullscreen}</button>
                  </div>
                </div>
                {readerFullscreen && (
                  <button className="readerFullscreenExit" onClick={toggleReaderFullscreen}>
                    {t.exitFullscreen}
                  </button>
                )}
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
                {(selectedBook.creator || selectedBook.description) && (
                  <section className={bookDetailsOpen ? "bookDetails open" : "bookDetails"}>
                    <button onClick={() => setBookDetailsOpen((value) => !value)}>
                      {bookDetailsOpen ? t.hideBookDetails : t.showBookDetails}
                    </button>
                    <div>
                      {selectedBook.creator && <strong>{selectedBook.creator}</strong>}
                      {selectedBook.description && <p>{selectedBook.description}</p>}
                    </div>
                  </section>
                )}
                <div
                  className={`pageStage ${selectedBook.format === "epub" ? "epub" : selectedBook.format === "pdf" ? `pdf ${readerPageMode === "webtoon" ? "webtoon" : ""}` : readerPageMode}`}
                  onMouseDownCapture={handleReaderMouseDown}
                  onTouchStartCapture={handleReaderTouchStart}
                >
                  <button className="pageEdge previous" aria-label="Previous page" onClick={goReaderPrevious} />
                  <button className="pageEdge next" aria-label="Next page" onClick={goReaderNext} />
                  {readerLoadState === "loading" && selectedBook.format !== "epub" && selectedBook.format !== "pdf" && pageIndex !== displayedPageIndex && (
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
                              onClick={() => jumpToEpubChapter(item)}
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
                        onNavigate={jumpToEpubHref}
                        onMetrics={(count, position) => {
                          const restoredPosition = epubRestorePosition.current;
                          if (restoredPosition !== null) {
                            if (count > restoredPosition) {
                              epubRestorePosition.current = null;
                            }
                            setEpubPageCount(Math.max(count, restoredPosition + 1));
                            setEpubPagePosition(Math.max(0, restoredPosition));
                            setReaderLoadState("ready");
                            return;
                          }
                          setEpubPageCount(count);
                          setEpubPagePosition(position);
                          setReaderLoadState("ready");
                        }}
                      />
                    </>
                  ) : selectedBook.format === "pdf" ? (
                    <PdfReader
                      book={selectedBook}
                      pageIndex={pageIndex}
                      pageMode={readerPageMode}
                      onPageCount={(count) => setPdfPageCount(count)}
                      onPageChange={(nextIndex) => {
                        setPageIndex((value) => (value === nextIndex ? value : nextIndex));
                        setDisplayedPageIndex((value) => (value === nextIndex ? value : nextIndex));
                      }}
                    />
                  ) : useWebtoonReader ? (
                    <div
                      ref={webtoonRef}
                      className="webtoonReader"
                      onScroll={handleWebtoonScroll}
                      onWheel={markWebtoonUserScroll}
                      onTouchStart={markWebtoonUserScroll}
                      onPointerDown={markWebtoonUserScroll}
                      aria-live="polite"
                    >
                      {pages.map((page) => {
                        const shouldRenderImage = Math.abs(page.index - pageIndex) <= WEBTOON_RENDER_RADIUS;
                        const measuredHeight = webtoonPageHeights[page.index] || WEBTOON_PLACEHOLDER_HEIGHT;
                        return (
                          <div
                            className={shouldRenderImage ? "webtoonPage" : "webtoonPage unloaded"}
                            data-page-index={page.index}
                            data-page-key={webtoonPageKey(page)}
                            key={`${selectedBook.id}-${page.index}`}
                            style={{ ...(shouldRenderImage ? {} : { minHeight: measuredHeight }), ...comicImageFitStyle(page, "webtoon") }}
                          >
                            {shouldRenderImage ? (
                              <img
                                src={authenticatedResourcePath(comicPageDisplayPath(selectedBook.id, page, page.index))}
                                alt={page.name}
                                draggable={false}
                                loading="eager"
                                onLoad={(event) => handleWebtoonImageLoad(event, page)}
                              />
                            ) : (
                              <span className="webtoonPlaceholder" aria-hidden="true" />
                            )}
                          </div>
                        );
                      })}
                    </div>
                  ) : (
                    <div className="pageSpread" aria-live="polite">
                      {visiblePageIndexes(displayedPageIndex, pages.length, readerPageMode).map((visibleIndex) => (
                        <img
                          key={`${selectedBook.id}-${visibleIndex}`}
                          src={authenticatedResourcePath(comicPageDisplayPath(selectedBook.id, pages[visibleIndex], visibleIndex))}
                          alt={pages[visibleIndex]?.name ?? ""}
                          draggable={false}
                          style={comicImageFitStyle(pages[visibleIndex], readerPageMode)}
                          onLoad={(event) => handleComicImageLoad(event, pages[visibleIndex])}
                        />
                      ))}
                    </div>
                  )}
                </div>
                {!useWebtoonReader && (
                  <div className="readerControls">
                    <button onClick={goReaderPrevious}>{t.previous}</button>
                    {selectedBook.format === "epub" && (
                      <span className="epubProgress">
                        {t.epubChapterPageLabel(Math.min(epubPagePosition + 1, epubPageCount), epubPageCount)}
                      </span>
                    )}
                    <input
                      type="range"
                      aria-label={selectedBook.format === "epub" ? t.epubChapterSlider : t.pageSlider}
                      min="0"
                      max={Math.max(0, readerTotalPages(selectedBook, pages.length, pdfPageCount) - 1)}
                      value={pageIndex}
                      onChange={(event) => setReaderPage(selectedBook, Number(event.target.value))}
                    />
                    <button onClick={goReaderNext}>{t.next}</button>
                  </div>
                )}
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
              <div className="scanSettings">
                <div>
                  <strong>{t.scanWorkers}</strong>
                  <small>{t.scanWorkersHint}</small>
                </div>
                <input
                  type="range"
                  min="1"
                  max="8"
                  value={scanWorkerDraft}
                  onChange={(event) => setScanWorkerDraft(Number(event.target.value))}
                />
                <span>{scanWorkerDraft}</span>
                <button onClick={saveScanSettings} disabled={scanSettingsSaving || scanWorkerDraft === scanSettings.scanWorkers}>
                  {scanSettingsSaving ? t.saving : t.save}
                </button>
              </div>
              <div className="thumbnailWorkerPanel">
                <div className="thumbnailWorkerHeader">
                  <div>
                    <strong>{t.thumbnailWorker}</strong>
                    <small>{t.thumbnailWorkerHint}</small>
                  </div>
                  <span className={`workerBadge ${thumbnailWorkerStatus?.status || "idle"}`}>{thumbnailWorkerStatus?.status || "idle"}</span>
                </div>
                <div className="thumbnailStats">
                  <span>{t.thumbnailQueued}<strong>{thumbnailWorkerStatus?.queued ?? 0}</strong></span>
                  <span>{t.thumbnailRunning}<strong>{thumbnailWorkerStatus?.running ?? 0}</strong></span>
                  <span>{t.thumbnailReady}<strong>{thumbnailWorkerStatus?.ready ?? 0}</strong></span>
                  <span>{t.thumbnailFailed}<strong>{thumbnailWorkerStatus?.failed ?? 0}</strong></span>
                  <span>{t.thumbnailCancelled}<strong>{thumbnailWorkerStatus?.cancelled ?? 0}</strong></span>
                </div>
                {thumbnailWorkerStatus?.cache && (
                  <div className="thumbnailCachePanel">
                    <div className="thumbnailCacheHeader">
                      <strong>{t.thumbnailCache}</strong>
                      <small>
                        {t.thumbnailCacheHint(
                          thumbnailWorkerStatus.cache.algorithmVersion,
                          thumbnailWorkerStatus.cache.smallWidth,
                          thumbnailWorkerStatus.cache.mediumWidth,
                        )}
                      </small>
                    </div>
                    <div className="thumbnailStats">
                      <span>{t.thumbnailCacheFiles}<strong>{thumbnailWorkerStatus.cache.files}</strong></span>
                      <span>{t.thumbnailCacheSize}<strong>{formatBytes(thumbnailWorkerStatus.cache.bytes)}</strong></span>
                      <span>{t.thumbnailCacheReady}<strong>{thumbnailWorkerStatus.cache.readyFiles}</strong></span>
                      <span>{t.thumbnailCacheMissing}<strong>{thumbnailWorkerStatus.cache.missingFiles}</strong></span>
                      <span>{t.thumbnailCacheStale}<strong>{thumbnailWorkerStatus.cache.staleFiles}</strong></span>
                      <span>{t.thumbnailCacheOrphans}<strong>{thumbnailWorkerStatus.cache.orphanFiles}</strong></span>
                    </div>
                  </div>
                )}
                {thumbnailWorkerStatus?.activeJob?.bookTitle && (
                  <small className="thumbnailActive">
                    {t.thumbnailActive}: {thumbnailWorkerStatus.activeJob.bookTitle} · {thumbnailWorkerStatus.activeJob.size}
                  </small>
                )}
                {thumbnailWorkerStatus?.lastError && <small className="thumbnailError">{thumbnailWorkerStatus.lastError}</small>}
                <div className="jobActions">
                  {thumbnailWorkerStatus?.paused ? (
                    <button onClick={() => controlThumbnailWorker("resume")} disabled={thumbnailWorkerBusy}>{t.resume}</button>
                  ) : (
                    <button onClick={() => controlThumbnailWorker("pause")} disabled={thumbnailWorkerBusy}>{t.pause}</button>
                  )}
                  <button className="danger" onClick={() => controlThumbnailWorker("cancel")} disabled={thumbnailWorkerBusy || !thumbnailWorkerStatus?.queued}>{t.cancel}</button>
                  <button onClick={() => controlThumbnailWorker("cleanupOrphans")} disabled={thumbnailWorkerBusy || !thumbnailWorkerStatus?.cache?.orphanFiles}>{t.cleanupThumbnailOrphans}</button>
                  <button onClick={refreshThumbnailWorkerStatus} disabled={thumbnailWorkerBusy}>{t.refreshThumbnailWorker}</button>
                </div>
              </div>
              <div className="quickScanBox">
                <div>
                  <strong>{t.quickScan}</strong>
                  <small>{t.quickScanHint}</small>
                </div>
                <select
                  value={quickScanLibraryId || libraries[0]?.id || 0}
                  onChange={(event) => setQuickScanLibraryId(Number(event.target.value))}
                  aria-label={t.libraries}
                >
                  {libraries.map((library) => (
                    <option value={library.id} key={library.id}>
                      {library.name}
                    </option>
                  ))}
                </select>
                <input
                  value={quickScanPath}
                  onChange={(event) => setQuickScanPath(event.target.value)}
                  placeholder="/library/韩漫/某作品/Chap.263.zip"
                />
                <button onClick={quickScan} disabled={quickScanRunning || !!activeQuickScanJob || !quickScanPath.trim() || libraries.length === 0}>
                  {activeQuickScanJob ? t.quickScanAlreadyRunning(activeQuickScanJob.id) : quickScanRunning ? t.quickScanRunning : t.quickScanAction}
                </button>
                <select
                  value={recentScanLimit}
                  onChange={(event) => setRecentScanLimit(Number(event.target.value))}
                  aria-label={t.recentScanLimit}
                >
                  {[10, 20, 50, 100, 200].map((limit) => (
                    <option value={limit} key={limit}>
                      {t.recentScanLimitOption(limit)}
                    </option>
                  ))}
                </select>
                <button onClick={recentScan} disabled={recentScanRunning || !!activeRecentScanJob || libraries.length === 0}>
                  {activeRecentScanJob ? t.quickScanAlreadyRunning(activeRecentScanJob.id) : recentScanRunning ? t.quickScanRunning : t.recentScanAction}
                </button>
                {activeQuickScanJob && <small>{t.quickScanAlreadyRunningHint(activeQuickScanJob.id)}</small>}
                {activeRecentScanJob && <small>{t.recentScanAlreadyRunningHint(activeRecentScanJob.id)}</small>}
              </div>
              {jobs.map((job) => (
                <button className="jobRow" key={job.id} onClick={() => openJob(job)}>
                  <strong>Job #{job.id}</strong>
                  <small>
                    {job.status} · {job.discoveredFiles} discovered · {job.indexedFiles} indexed · {job.skippedFiles} skipped ·{" "}
                    {job.errorCount} errors · {job.metadataUpdatedFiles} metadata · {job.reclassifiedFiles} moved · {t.elapsed} {formatElapsed(job, nowTick)}
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
                    <span>Metadata<strong>{selectedJobLatest.metadataUpdatedFiles}</strong></span>
                    <span>Moved<strong>{selectedJobLatest.reclassifiedFiles}</strong></span>
                    <span>{t.elapsed}<strong>{formatElapsed(selectedJobLatest, nowTick)}</strong></span>
                  </div>
                  <div className="jobActions">
                    {canPauseJob(selectedJobLatest) && <button onClick={() => controlJob(selectedJobLatest, "pause")}>{t.pause}</button>}
                    {canResumeJob(selectedJobLatest) && <button onClick={() => controlJob(selectedJobLatest, "resume")}>{t.resume}</button>}
                    {canCancelJob(selectedJobLatest) && <button className="danger" onClick={() => controlJob(selectedJobLatest, "cancel")}>{t.cancel}</button>}
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
      {setupRequired && (
        <div className="authOverlay setupOverlay" role="dialog" aria-modal="true" aria-labelledby="setup-title">
          <form className="authPanel setupPanel" onSubmit={submitSetup}>
            <div>
              <h1 id="setup-title">FolioSpace Library 0.82 Setup</h1>
              <small>
                {setupStatus?.hasLibraries
                  ? "设置访问密钥，沿用当前数据库里的资源目录。"
                  : "设置访问密钥，并选择 Docker 容器内可访问的第一个资源目录。"}
              </small>
            </div>
            <label>
              <span>{setupStatus?.authEnabled ? "当前访问密钥" : "新访问密钥"}</span>
              <input
                autoFocus
                type="password"
                value={setupToken}
                onChange={(event) => setSetupToken(event.target.value)}
                placeholder={setupStatus?.authEnabled ? "Existing access token" : "At least 8 characters"}
              />
            </label>
            <label>
              <span>资源目录名称</span>
              <input value={setupName} onChange={(event) => setSetupName(event.target.value)} placeholder="Books / Comics / GameROMS" />
            </label>
            <label>
              <span>容器内路径</span>
              <input value={setupPath} onChange={(event) => setSetupPath(event.target.value)} placeholder="/books" />
            </label>
            {setupStatus?.directoryRoots && setupStatus.directoryRoots.length > 0 && (
              <div className="setupRootGrid">
                {setupStatus.directoryRoots.map((root) => (
                  <button
                    type="button"
                    key={root.path}
                    className={setupPath === root.path ? "selected" : ""}
                    onClick={() => selectSetupRoot(root)}
                  >
                    <strong>{root.name}</strong>
                    <small>{root.path}</small>
                  </button>
                ))}
              </div>
            )}
            <label>
              <span>资源类型</span>
              <select value={setupAssetType} onChange={(event) => setSetupAssetType(event.target.value as LibraryAssetType)}>
                <option value="mixed">{t.assetTypeMixed}</option>
                <option value="comic">{t.assetTypeComic}</option>
                <option value="book">{t.assetTypeBook}</option>
                <option value="game">{t.assetTypeGame}</option>
                <option value="video">{t.assetTypeVideo}</option>
              </select>
            </label>
            <label>
              <span>{t.scanWorkers}</span>
              <input
                type="number"
                min="1"
                max="8"
                value={setupScanWorkers}
                onChange={(event) => setSetupScanWorkers(clampScanWorkers(Number(event.target.value)))}
              />
            </label>
            <p className="setupHint">
              如果没有看到 NAS 目录，请先在 Docker compose 里把宿主机路径挂载到容器路径，例如 <code>/volume2/Books:/books:ro</code>。
            </p>
            {setupError && <span className="authError">{setupError}</span>}
            <button disabled={(!setupPath.trim() && !setupStatus?.hasLibraries) || !setupToken.trim()}>
              {setupStatus?.hasLibraries ? "Save setup" : "Initialize"}
            </button>
          </form>
        </div>
      )}
      {(!authChecked || authRequired || authError) && (
        <div className="authOverlay" role="dialog" aria-modal="true" aria-labelledby="auth-title">
          <form className="authPanel" onSubmit={submitAuth}>
            <div>
              <h1 id="auth-title">FolioSpace Library</h1>
              <small>{authChecked ? "Enter the NAS access token." : authMessage}</small>
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
            {!authRequired && authError && (
              <>
                <span className="authError">{authError}</span>
                <button type="button" onClick={() => window.location.reload()}>
                  Retry
                </button>
              </>
            )}
          </form>
        </div>
      )}
    </main>
  );
}

function CollectionCover({ series }: { series: Series }) {
  const rootRef = useRef<HTMLSpanElement | null>(null);
  const [thumbnail, setThumbnail] = useState<{ url: string; status: string } | null>(
    series.thumbnailUrl ? { url: series.thumbnailUrl, status: series.thumbnailStatus || "pending" } : null,
  );
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    setThumbnail(series.thumbnailUrl ? { url: series.thumbnailUrl, status: series.thumbnailStatus || "pending" } : null);
    setFailed(false);
  }, [series.thumbnailStatus, series.thumbnailUrl]);

  useEffect(() => {
    if (series.thumbnailUrl) return;
    const node = rootRef.current;
    if (!node || thumbnail || failed) return;

    let cancelled = false;
    const load = async () => {
      try {
        const page = await api.booksPage(series.id, { limit: 1, offset: 0, sort: "title" });
        if (!cancelled) {
          const book = page.items[0];
          setThumbnail(book ? {
            url: book.thumbnailUrl || `/api/books/${book.id}/thumbnail?size=small`,
            status: book.thumbnailStatus || "pending",
          } : null);
          setFailed(!book);
        }
      } catch {
        if (!cancelled) setFailed(true);
      }
    };

    if (!("IntersectionObserver" in window)) {
      void load();
      return () => {
        cancelled = true;
      };
    }

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          observer.disconnect();
          void load();
        }
      },
      { rootMargin: "240px" },
    );
    observer.observe(node);
    return () => {
      cancelled = true;
      observer.disconnect();
    };
  }, [failed, series.id, series.thumbnailUrl, thumbnail]);

  return (
    <span ref={rootRef} className="collectionCoverSlot" aria-hidden="true">
      {thumbnail && !failed && (
        <ThumbnailImage
          src={thumbnail.url}
          thumbnailStatus={thumbnail.status}
          alt=""
          loading="lazy"
          onError={() => setFailed(true)}
        />
      )}
    </span>
  );
}

function BookShelf({
  title,
  subtitle,
  books,
  onOpen,
  meta,
  largeCovers = false,
  progress = false,
}: {
  title: string;
  subtitle: string;
  books: Book[];
  onOpen: (book: Book) => void;
  meta: (book: Book) => string;
  largeCovers?: boolean;
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
              <BookCover book={book} largeCover={largeCovers} />
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

function CollectionCard({
  item,
  selected,
  labels,
  onOpen,
  onStateChange,
}: {
  item: Series;
  selected: boolean;
  labels: Translation;
  onOpen: (item: Series) => void;
  onStateChange: (item: Series, patch: Partial<CollectionPrivateState>) => void;
}) {
  return (
    <div className="collectionCardShell">
      <button
        className={selected ? "collectionCard selected" : "collectionCard"}
        onClick={() => onOpen(item)}
        onMouseDown={(event) => event.preventDefault()}
        title={item.title}
      >
        <span className={collectionThumbClass(item)}>
          {item.collectionType !== "game_platform" && (
            <CollectionCover series={item} />
          )}
          <span className="collectionInitials">{collectionInitials(item.title)}</span>
        </span>
        <strong>{item.title}</strong>
        <small>{item.directoryPath || "."}</small>
        <em>{collectionCountLabel(item)}</em>
      </button>
      <div className="collectionActions">
        <button
          type="button"
          className={item.favorite ? "collectionAction active" : "collectionAction"}
          onClick={() => onStateChange(item, { favorite: !item.favorite })}
          title={labels.collectionFavorite}
          aria-label={labels.collectionFavorite}
        >
          ★
        </button>
        <button
          type="button"
          className={item.liked ? "collectionAction active" : "collectionAction"}
          onClick={() => onStateChange(item, { liked: !item.liked })}
          title={labels.collectionLike}
          aria-label={labels.collectionLike}
        >
          ♥
        </button>
      </div>
    </div>
  );
}

function BookCover({ book, largeCover = false }: { book: Book; largeCover?: boolean }) {
  const src = book.thumbnailUrl || `/api/books/${book.id}/thumbnail?size=small`;
  return (
    <>
      <ThumbnailImage
        src={src}
        thumbnailStatus={book.thumbnailStatus || "pending"}
        alt=""
        loading="lazy"
      />
      <SourceCoverOverlay sourceCoverUrl={largeCover ? book.coverUrl || `/api/books/${book.id}/cover` : undefined} />
    </>
  );
}

const thumbnailFallbackImage = "/bookshelf-bg-v2.jpg";

function SourceCoverOverlay({ sourceCoverUrl }: { sourceCoverUrl?: string }) {
  const [sourceCoverLoaded, setSourceCoverLoaded] = useState(false);
  const [sourceCoverFailed, setSourceCoverFailed] = useState(false);

  useEffect(() => {
    setSourceCoverLoaded(false);
    setSourceCoverFailed(false);
  }, [sourceCoverUrl]);

  if (!sourceCoverUrl || sourceCoverFailed) return null;

  return (
    <img
      className={`sourceCoverImage${sourceCoverLoaded ? " loaded" : ""}`}
      src={authenticatedResourcePath(sourceCoverUrl)}
      alt=""
      loading="lazy"
      data-thumbnail-status={sourceCoverLoaded ? "source-cover" : "source-cover-loading"}
      onLoad={() => setSourceCoverLoaded(true)}
      onError={() => setSourceCoverFailed(true)}
    />
  );
}

function ThumbnailImage({
  src,
  thumbnailStatus,
  alt,
  loading,
  onError,
}: {
  src: string;
  thumbnailStatus?: string;
  alt: string;
  loading?: "eager" | "lazy";
  onError?: () => void;
}) {
  const [refreshNonce, setRefreshNonce] = useState(0);
  const [fallbackVisible, setFallbackVisible] = useState(false);

  useEffect(() => {
    setRefreshNonce(0);
    setFallbackVisible(false);
  }, [src, thumbnailStatus]);

  useEffect(() => {
    if (!src || (thumbnailStatus === "ready" && !fallbackVisible)) return;
    let cancelled = false;
    let timer: number | null = null;
    let attempts = 0;
    let consecutiveErrors = 0;

    const poll = async () => {
      try {
        const pollUrl = authenticatedResourcePath(withThumbnailRefreshParam(src, attempts + 1));
        const response = await fetch(pollUrl, { method: "HEAD" });
        const contentType = response.headers.get("Content-Type") || "";
        if (!cancelled && response.ok && contentType.toLowerCase().startsWith("image/")) {
          const fallbackKind = response.headers.get("X-FolioSpace-Thumbnail-Fallback") || "";
          consecutiveErrors = 0;
          setFallbackVisible(false);
          if (!fallbackKind || attempts === 0 || fallbackVisible) {
            setRefreshNonce((value) => value + 1);
          }
          if (!fallbackKind) return;
        } else if (!cancelled && !response.ok) {
          consecutiveErrors += 1;
          if (consecutiveErrors >= 3) setFallbackVisible(true);
        }
      } catch {
        consecutiveErrors += 1;
        if (!cancelled && consecutiveErrors >= 3) setFallbackVisible(true);
      }
      attempts += 1;
      if (!cancelled && attempts < 60) {
        timer = window.setTimeout(poll, 1800);
      } else if (!cancelled) {
        setFallbackVisible(true);
      }
    };

    timer = window.setTimeout(poll, fallbackVisible ? 200 : 1200);
    return () => {
      cancelled = true;
      if (timer !== null) window.clearTimeout(timer);
    };
  }, [fallbackVisible, src, thumbnailStatus]);

  const resolvedSrc = refreshNonce > 0 ? withThumbnailRefreshParam(src, refreshNonce) : src;

  return (
    <img
      src={fallbackVisible ? thumbnailFallbackImage : authenticatedResourcePath(resolvedSrc)}
      alt={alt}
      loading={loading}
      data-thumbnail-status={fallbackVisible ? "fallback" : thumbnailStatus || "pending"}
      onError={() => {
        if (!fallbackVisible) {
          setFallbackVisible(true);
          return;
        }
        onError?.();
      }}
    />
  );
}

function PdfReader({
  book,
  pageIndex,
  pageMode,
  onPageCount,
  onPageChange,
}: {
  book: Book;
  pageIndex: number;
  pageMode: ReaderPageMode;
  onPageCount: (count: number) => void;
  onPageChange?: (pageIndex: number) => void;
}) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const canvasRefs = useRef<Record<number, HTMLCanvasElement | null>>({});
  const renderTasksRef = useRef<Record<number, { cancel: () => void } | null>>({});
  const scrollFrameRef = useRef<number | null>(null);
  const [documentProxy, setDocumentProxy] = useState<PDFDocumentProxy | null>(null);
  const [renderError, setRenderError] = useState("");
  const [sizeTick, setSizeTick] = useState(0);
  const [pdfWebtoonPageHeights, setPDFWebtoonPageHeights] = useState<Record<number, number>>({});

  useEffect(() => {
    const node = containerRef.current;
    if (!node) return;
    const observer = new ResizeObserver(() => setSizeTick((value) => value + 1));
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    let cancelled = false;
    setDocumentProxy(null);
    setRenderError("");
    setPDFWebtoonPageHeights({});
    Object.values(canvasRefs.current).forEach((canvas) => releasePDFCanvas(canvas));
    canvasRefs.current = {};
    Object.values(renderTasksRef.current).forEach((task) => task?.cancel());
    renderTasksRef.current = {};
    const token = getAuthToken();
    const task = getDocument({
      url: authenticatedResourcePath(`/api/books/${book.id}/pages/0`),
      httpHeaders: token ? { Authorization: `Bearer ${token}` } : undefined,
      withCredentials: true,
    });

    task.promise
      .then((pdf) => {
        if (cancelled) {
          void pdf.destroy();
          return;
        }
        setDocumentProxy(pdf);
        onPageCount(pdf.numPages);
      })
      .catch((error) => {
        if (!cancelled) setRenderError(error instanceof Error ? error.message : "PDF failed to load");
      });

    return () => {
      cancelled = true;
      Object.values(canvasRefs.current).forEach((canvas) => releasePDFCanvas(canvas));
      canvasRefs.current = {};
      void task.destroy();
    };
  }, [book.id]);

  const renderPageIndex = pageIndex;

  useEffect(() => {
    if (!documentProxy || !containerRef.current) return;
    let cancelled = false;
    const pdf = documentProxy;
    const container = containerRef.current;
    Object.values(renderTasksRef.current).forEach((task) => task?.cancel());
    renderTasksRef.current = {};
    const rect = container.getBoundingClientRect();
    const isWebtoonMode = pageMode === "webtoon";
    const gap = pageMode === "double" ? 18 : 0;
    const pagesToRender = pdfRenderablePages(renderPageIndex, pdf.numPages, pageMode);
    const renderableSet = new Set(pagesToRender);
    Object.entries(canvasRefs.current).forEach(([key, canvas]) => {
      const pageNumber = Number(key);
      if (!renderableSet.has(pageNumber)) {
        releasePDFCanvas(canvas);
        delete canvasRefs.current[pageNumber];
      }
    });
    const slotWidth = isWebtoonMode
      ? Math.max(160, Math.min(rect.width - 32, 840))
      : Math.max(120, (rect.width - gap) / Math.max(1, pagesToRender.length));
    const slotHeight = Math.max(160, rect.height);

    async function render() {
      try {
        for (const pageNumber of pagesToRender) {
          const canvas = canvasRefs.current[pageNumber];
          if (!canvas) continue;
          const page = await pdf.getPage(pageNumber);
          if (cancelled) return;
          try {
            const baseViewport = page.getViewport({ scale: 1 });
            const rawDpr = Math.max(1, window.devicePixelRatio || 1);
            const dpr = isWebtoonMode ? 1 : rawDpr;
            const cssScale = isWebtoonMode
              ? slotWidth / baseViewport.width
              : Math.min(slotWidth / baseViewport.width, slotHeight / baseViewport.height);
            const desiredRenderScale = cssScale * dpr;
            const maxRenderScale = isWebtoonMode
              ? Math.sqrt(PDF_WEBTOON_MAX_CANVAS_PIXELS / Math.max(1, baseViewport.width * baseViewport.height))
              : desiredRenderScale;
            const renderScale = Math.min(desiredRenderScale, maxRenderScale);
            const viewport = page.getViewport({ scale: renderScale });
            const context = canvas.getContext("2d");
            if (!context) continue;
            renderTasksRef.current[pageNumber]?.cancel();
            releasePDFCanvas(canvas);
            canvas.width = Math.max(1, Math.floor(viewport.width));
            canvas.height = Math.max(1, Math.floor(viewport.height));
            const cssWidth = Math.max(1, Math.floor(baseViewport.width * cssScale));
            const cssHeight = Math.max(1, Math.floor(baseViewport.height * cssScale));
            canvas.style.width = `${cssWidth}px`;
            canvas.style.height = `${cssHeight}px`;
            if (isWebtoonMode) {
              setPDFWebtoonPageHeights((items) => (items[pageNumber] === cssHeight ? items : { ...items, [pageNumber]: cssHeight }));
            }
            const task = page.render({ canvasContext: context, viewport });
            renderTasksRef.current[pageNumber] = task;
            await task.promise;
            if (renderTasksRef.current[pageNumber] === task) {
              renderTasksRef.current[pageNumber] = null;
            }
          } finally {
            try {
              page.cleanup();
            } catch {
              // PDF.js can reject cleanup while a cancelled render task is still unwinding.
            }
          }
        }
        if (!cancelled) setRenderError("");
      } catch (error) {
        if (!cancelled && !isPDFRenderCancelled(error)) {
          setRenderError(error instanceof Error ? error.message : "PDF page failed to render");
        }
      }
    }

    void render();
    return () => {
      cancelled = true;
      Object.values(renderTasksRef.current).forEach((task) => task?.cancel());
      renderTasksRef.current = {};
      if (pageMode === "webtoon") {
        Object.values(canvasRefs.current).forEach((canvas) => releasePDFCanvas(canvas));
      }
    };
  }, [documentProxy, renderPageIndex, pageMode, sizeTick]);

  useEffect(() => {
    return () => {
      if (scrollFrameRef.current !== null) {
        window.cancelAnimationFrame(scrollFrameRef.current);
      }
    };
  }, []);

  const pages = documentProxy ? pdfLayoutPages(renderPageIndex, documentProxy.numPages, pageMode) : [];
  const renderablePages = new Set(documentProxy ? pdfRenderablePages(renderPageIndex, documentProxy.numPages, pageMode) : []);

  function updatePDFWebtoonPosition() {
    if (pageMode !== "webtoon" || !containerRef.current || !onPageChange) return;
    if (scrollFrameRef.current !== null) {
      window.cancelAnimationFrame(scrollFrameRef.current);
    }
    scrollFrameRef.current = window.requestAnimationFrame(() => {
      scrollFrameRef.current = null;
      const node = containerRef.current;
      if (!node) return;
      const markers = Array.from(node.querySelectorAll<HTMLElement>("[data-page-index]"));
      const viewportAnchor = node.scrollTop + node.clientHeight * 0.28;
      let current = 0;
      for (const marker of markers) {
        if (marker.offsetTop <= viewportAnchor) {
          current = Number(marker.dataset.pageIndex ?? 0);
        } else {
          break;
        }
      }
      if (Number.isFinite(current)) {
        onPageChange(current);
      }
    });
  }

  return (
    <div ref={containerRef} className={`pdfReader ${pageMode}`} onScroll={updatePDFWebtoonPosition}>
      {renderError && <div className="pdfReaderError">{renderError}</div>}
      {pages.map((pageNumber) => {
        const shouldRenderCanvas = pageMode !== "webtoon" || renderablePages.has(pageNumber);
        return shouldRenderCanvas ? (
          <canvas
            key={`${book.id}-${pageNumber}`}
            ref={(node) => {
              canvasRefs.current[pageNumber] = node;
            }}
            data-page-index={pageNumber - 1}
            aria-label={`PDF page ${pageNumber}`}
          />
        ) : (
          <div
            className="pdfPagePlaceholder"
            data-page-index={pageNumber - 1}
            key={`${book.id}-${pageNumber}-placeholder`}
            style={{ minHeight: pdfWebtoonPageHeights[pageNumber] || PDF_WEBTOON_PLACEHOLDER_HEIGHT }}
            aria-label={`PDF page ${pageNumber} placeholder`}
          />
        );
      })}
    </div>
  );
}

function pdfLayoutPages(index: number, total: number, mode: ReaderPageMode) {
  const first = Math.max(1, Math.min(total, index + 1));
  if (mode === "webtoon") return Array.from({ length: total }, (_, offset) => offset + 1);
  if (mode === "single") return [first];
  return [first, first + 1].filter((page) => page >= 1 && page <= total);
}

function pdfRenderablePages(index: number, total: number, mode: ReaderPageMode) {
  const first = Math.max(1, Math.min(total, index + 1));
  if (mode !== "webtoon") return pdfLayoutPages(index, total, mode);
  const start = Math.max(1, first - PDF_WEBTOON_RENDER_RADIUS);
  const end = Math.min(total, first + PDF_WEBTOON_RENDER_RADIUS);
  return Array.from({ length: end - start + 1 }, (_, offset) => start + offset);
}

function isPDFRenderCancelled(error: unknown) {
  return error instanceof Error && error.name === "RenderingCancelledException";
}

function releasePDFCanvas(canvas: HTMLCanvasElement | null | undefined) {
  if (!canvas) return;
  const context = canvas.getContext("2d");
  context?.clearRect(0, 0, canvas.width, canvas.height);
  canvas.width = 0;
  canvas.height = 0;
}

function CatalogPage({
  title,
  subtitle,
  countLabel,
  children,
}: {
  title: string;
  subtitle: string;
  countLabel: string;
  children: ReactNode;
}) {
  return (
    <section className="catalogPage">
      <div className="catalogHeader">
        <div>
          <h1>{title}</h1>
          <small>{subtitle}</small>
        </div>
        <span>{countLabel}</span>
      </div>
      {children}
    </section>
  );
}

function GameShelf({
  title,
  subtitle,
  games,
  meta,
  moreLabel,
  onMore,
}: {
  title: string;
  subtitle: string;
  games: GameAsset[];
  meta: (game: GameAsset) => string;
  moreLabel: string;
  onMore: () => void;
}) {
  const sortedGames = [...games].sort(compareGamesByPlatform);

  return (
    <div className="bookShelf gameShelf">
      <div className="bookShelfHeader">
        <div>
          <h1>{title}</h1>
          <small>{subtitle}</small>
        </div>
        <button type="button" className="textButton" onClick={onMore}>{moreLabel}</button>
      </div>
      <div className="shelfScroller">
        {sortedGames.map((game) => (
          <GameTile className="shelfBook" key={`game-${game.id}`} game={game} meta={meta(game)} />
        ))}
      </div>
    </div>
  );
}

function GameTile({ game, meta, className = "book" }: { game: GameAsset; meta: string; className?: string }) {
  const [coverFailed, setCoverFailed] = useState(false);
  const hasCover = Boolean(game.coverUrl && !coverFailed);

  return (
    <button className={`${className} gameCard`} title={game.title}>
      <span className={`shelfCover gameCover${hasCover ? " hasCover" : ""}`}>
        {hasCover ? <img src={authenticatedResourcePath(game.coverUrl)} alt="" loading="lazy" onError={() => setCoverFailed(true)} /> : null}
        <span className="gameCoverPlatform">{gamePlatformLabel(game)}</span>
        <span className="gameCoverTitle">Now Printing</span>
        <span className="gameCoverFormat">{game.format.toUpperCase()}</span>
      </span>
      <strong>{game.title}</strong>
      <small>{meta}</small>
    </button>
  );
}

function VideoShelf({
  title,
  subtitle,
  videos,
  meta,
  onOpen,
  moreLabel,
  onMore,
}: {
  title: string;
  subtitle: string;
  videos: VideoAsset[];
  meta: (video: VideoAsset) => string;
  onOpen: (video: VideoAsset) => void;
  moreLabel: string;
  onMore: () => void;
}) {
  return (
    <div className="bookShelf videoShelf">
      <div className="bookShelfHeader">
        <div>
          <h1>{title}</h1>
          <small>{subtitle}</small>
        </div>
        <button type="button" className="textButton" onClick={onMore}>{moreLabel}</button>
      </div>
      <div className="shelfScroller">
        {videos.map((video) => (
          <VideoTile className="shelfBook" key={`video-${video.id}`} video={video} meta={meta(video)} onOpen={onOpen} />
        ))}
      </div>
    </div>
  );
}

function VideoTile({ video, meta, onOpen, className = "book" }: { video: VideoAsset; meta: string; onOpen: (video: VideoAsset) => void; className?: string }) {
  const [thumbnailFailed, setThumbnailFailed] = useState(false);

  return (
    <button className={`${className} videoCard`} title={video.title} onClick={() => onOpen(video)}>
      <span className="shelfCover videoCover">
        {!thumbnailFailed && <img src={authenticatedResourcePath(video.thumbnailUrl)} alt="" loading="lazy" onError={() => setThumbnailFailed(true)} />}
        <span className="videoCoverFallback">
          <em>Now Showing</em>
          <strong>{video.title}</strong>
        </span>
        <span className="videoCoverFormat">{video.format.toUpperCase()}</span>
      </span>
      <strong>{video.title}</strong>
      <small>{meta}</small>
    </button>
  );
}

function compareVideosByTitle(a: VideoAsset, b: VideoAsset) {
  return a.title.localeCompare(b.title, undefined, { sensitivity: "base", numeric: true });
}

function compareGamesByPlatform(a: GameAsset, b: GameAsset) {
  const platformDelta = platformSortRank(a.platform) - platformSortRank(b.platform);
  if (platformDelta !== 0) return platformDelta;
  const platformNameDelta = (a.platform || "").localeCompare(b.platform || "");
  if (platformNameDelta !== 0) return platformNameDelta;
  return a.title.localeCompare(b.title, undefined, { sensitivity: "base", numeric: true });
}

function platformSortRank(platform: string) {
  switch ((platform || "").toLowerCase()) {
    case "nes":
      return 10;
    case "snes":
      return 20;
    case "gb":
      return 30;
    case "gbc":
      return 40;
    case "gba":
      return 50;
    case "md":
    case "genesis":
    case "mega-drive":
    case "megadrive":
      return 60;
    case "32x":
      return 65;
    case "saturn":
      return 70;
    case "neogeo":
      return 80;
    case "model3":
      return 85;
    case "naomi":
      return 86;
    case "arcade":
      return 90;
    default:
      return 999;
  }
}

function gamePlatformLabel(game: GameAsset) {
  switch ((game.platform || "").toLowerCase()) {
    case "md":
      return "MEGA DRIVE";
    case "neogeo":
      return "NEO GEO";
    case "model3":
      return "MODEL 3";
    default:
      return (game.platform || game.format || "game").toUpperCase();
  }
}

function collectionCountLabel(item: Series) {
  if (item.primaryType === "video") return `${item.bookCount} videos`;
  return item.collectionType === "game_platform" ? `${item.bookCount} games` : `${item.bookCount} volumes`;
}

function collectionThumbClass(item: Series) {
  if (item.collectionType === "game_platform") return "collectionThumb game";
  return item.thumbnailUrl ? "collectionThumb withCover" : "collectionThumb";
}

function collectionKind(item: Series, libraries: Library[]) {
  if (item.primaryType === "book" || item.primaryType === "comic" || item.primaryType === "game" || item.primaryType === "video") {
    return item.primaryType;
  }
  if (item.collectionType === "game_platform") return "game";
  const library = libraries.find((candidate) => candidate.id === item.libraryId);
  if (library?.assetType === "book") return "book";
  return "comic";
}

function loadedCollectionCountLabel(item: Series, bookCount: number, gameCount: number, videoCount: number) {
  if (item.primaryType === "video") {
    return `${videoCount} videos`;
  }
  if (item.collectionType === "game_platform") {
    return `${gameCount} games`;
  }
  if (videoCount > 0) {
    return `${bookCount} volumes · ${videoCount} videos`;
  }
  if (gameCount > 0) {
    return `${bookCount} volumes · ${gameCount} games`;
  }
  return `${bookCount} of ${item.bookCount} volumes`;
}

function collectionInitials(title: string) {
  const compact = title.trim().replace(/\s+/g, " ");
  if (!compact) return "FS";
  const parts = compact.split(" ").filter(Boolean);
  if (parts.length >= 2 && /^[\x00-\x7F]+$/.test(compact)) {
    return `${parts[0][0] ?? ""}${parts[1][0] ?? ""}`.toUpperCase();
  }
  return Array.from(compact).slice(0, 2).join("").toUpperCase();
}

function EpubFrame({
  book,
  manifest,
  pageIndex,
  pageMode,
  fontSize,
  theme,
  pagePosition,
  onNavigate,
  onMetrics,
}: {
  book: Book;
  manifest: EpubManifest | null;
  pageIndex: number;
  pageMode: ReaderPageMode;
  fontSize: number;
  theme: EpubTheme;
  pagePosition: number;
  onNavigate: (href: string, label?: string) => void;
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
    const isCompactViewport = viewportWidth <= 760;
    const isDoublePage = pageMode === "double" && !isCompactViewport;
    const horizontalPadding = isCompactViewport ? 24 : isDoublePage ? 34 : 52;
    const verticalPadding = isCompactViewport ? 24 : isDoublePage ? 34 : 42;
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

    for (const anchor of Array.from(doc.querySelectorAll<HTMLAnchorElement>("a[href]"))) {
      if (anchor.dataset.foliospaceLinkBound === "true") continue;
      anchor.dataset.foliospaceLinkBound = "true";
      anchor.addEventListener("click", (event) => {
        const href = anchor.getAttribute("href");
        if (!href) return;
        event.preventDefault();
        event.stopPropagation();
        onNavigate(normalizeEPUBLink(spineItem.href, href), anchor.textContent?.trim() || href);
      });
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
      src={authenticatedResourcePath(`/api/books/${book.id}/epub/resources/${encodeResourcePath(spineItem.href)}`)}
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

function fallbackEpubManifest(book: Book, pages: Page[]): EpubManifest {
  return {
    title: book.title,
    creator: book.creator ?? "",
    description: book.description ?? "",
    coverHref: "",
    spine: pages.map((page) => ({
      index: page.index,
      id: `page-${page.index}`,
      href: page.name,
      mediaType: "application/xhtml+xml",
    })),
    toc: pages.map((page) => ({
      label: page.name || `Chapter ${page.index + 1}`,
      href: page.name,
      index: page.index,
    })),
  };
}

function resolveEpubHrefIndex(manifest: EpubManifest, value: string, fallbackIndex = 0) {
  const href = stripEPUBFragment(value);
  const exact = manifest.spine.find((spineItem) => stripEPUBFragment(spineItem.href) === href);
  if (exact) return exact.index;
  const clamped = Math.max(0, Math.min(fallbackIndex, Math.max(0, manifest.spine.length - 1)));
  return Number.isFinite(clamped) ? clamped : 0;
}

function stripEPUBFragment(value: string) {
  return value.split("#", 1)[0];
}

function normalizeEPUBLink(currentHref: string, href: string) {
  const marker = "/epub/resources/";
  const markerIndex = href.indexOf(marker);
  if (markerIndex >= 0) {
    const resource = href.slice(markerIndex + marker.length).split(/[?#]/, 1)[0];
    return decodeEPUBPath(resource);
  }
  try {
    const url = new URL(href, `https://foliospace.local/${currentHref}`);
    return decodeEPUBPath(url.pathname.replace(/^\/+/, "")) + url.hash;
  } catch {
    if (href.startsWith("#")) return `${stripEPUBFragment(currentHref)}${href}`;
    return href;
  }
}

function decodeEPUBPath(value: string) {
  return value
    .split("/")
    .map((part) => {
      try {
        return decodeURIComponent(part);
      } catch {
        return part;
      }
    })
    .join("/");
}

function readWebtoonLocator(locator: string) {
  if (!locator.startsWith("webtoon:")) return null;
  const value = Number.parseFloat(locator.slice("webtoon:".length));
  if (!Number.isFinite(value)) return null;
  return Math.max(0, Math.min(1, value));
}

function webtoonPageKey(page: Page | undefined) {
  if (!page) return "";
  return page.pageKey || (page.name ? `archive:${page.name}` : `index:${page.index}`);
}

function webtoonInitialPageIndex(position: WebtoonPosition, pages: Page[]) {
  const byKey = position.pageKey ? pages.find((page) => webtoonPageKey(page) === position.pageKey) : null;
  if (byKey) return byKey.index;
  if (position.pageIndex >= 0 && position.pageIndex < pages.length) return position.pageIndex;
  if (pages.length === 0) return 0;
  return Math.max(0, Math.min(Math.round(position.documentProgress * Math.max(0, pages.length - 1)), pages.length - 1));
}

function legacyWebtoonProgressPosition(documentProgress: number, pageCount: number): WebtoonPosition {
  return {
    schema: WEBTOON_POSITION_SCHEMA,
    pageIndex: -1,
    pageKey: "",
    pageYOffsetRatio: 0,
    viewportAnchorRatio: DEFAULT_WEBTOON_ANCHOR_RATIO,
    documentProgress,
    pageCount,
  };
}

function webtoonPositionForPage(pageIndex: number, pages: Page[]): WebtoonPosition {
  const clampedPageIndex = pages.length > 0 ? Math.max(0, Math.min(pageIndex, pages.length - 1)) : 0;
  return {
    schema: WEBTOON_POSITION_SCHEMA,
    pageIndex: clampedPageIndex,
    pageKey: webtoonPageKey(pages[clampedPageIndex]),
    pageYOffsetRatio: 0,
    viewportAnchorRatio: DEFAULT_WEBTOON_ANCHOR_RATIO,
    documentProgress: pages.length > 1 ? clampedPageIndex / (pages.length - 1) : 0,
    pageCount: pages.length,
  };
}

function collectWebtoonPageMetrics(node: HTMLElement, pages: Page[]): WebtoonPageMetric[] {
  return Array.from(node.querySelectorAll<HTMLElement>("[data-page-index]"))
    .map((marker) => {
      const index = Number(marker.dataset.pageIndex ?? 0);
      const page = pages[index];
      const image = marker.querySelector<HTMLImageElement>("img");
      const logicalHeight = image?.naturalWidth && image.naturalWidth > 0 ? image.naturalHeight / image.naturalWidth : undefined;
      return {
        index,
        pageKey: marker.dataset.pageKey || webtoonPageKey(page),
        displayedTop: marker.offsetTop,
        displayedHeight: marker.getBoundingClientRect().height || marker.offsetHeight || 1,
        logicalHeight,
      };
    })
    .filter((page) => Number.isFinite(page.index));
}

function isWebtoonRestoreTargetReady(node: HTMLElement, targetPageIndex: number) {
  const target = node.querySelector<HTMLElement>(`[data-page-index="${targetPageIndex}"]`);
  const image = target?.querySelector<HTMLImageElement>("img");
  if (!target || !image) return false;
  if (!image.complete || image.naturalWidth <= 0 || image.naturalHeight <= 0) return false;
  return target.getBoundingClientRect().height > 0;
}

function encodeResourcePath(value: string) {
  return value
    .split("/")
    .map((part) => encodeURIComponent(part))
    .join("/");
}

function nextFrame() {
  return new Promise<void>((resolve) => {
    window.requestAnimationFrame(() => resolve());
  });
}

function authenticatedResourcePath(path: string | undefined) {
  if (!path || !path.startsWith("/api/")) return path || "";
  const token = getAuthToken();
  if (!token) return path;
  const [base, hash = ""] = path.split("#", 2);
  const separator = base.includes("?") ? "&" : "?";
  const signed = `${base}${separator}access_token=${encodeURIComponent(token)}`;
  return hash ? `${signed}#${hash}` : signed;
}

function withThumbnailRefreshParam(path: string, value: number) {
  const [base, hash = ""] = path.split("#", 2);
  const separator = base.includes("?") ? "&" : "?";
  const refreshed = `${base}${separator}thumbnail_refresh=${encodeURIComponent(String(value))}`;
  return hash ? `${refreshed}#${hash}` : refreshed;
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
    profile: "用户档案",
    defaultProfile: "默认用户",
    defaultProfileBadge: "默认",
    newProfile: "新建用户",
    renameProfile: "重命名",
    createProfilePrompt: "新建用户档案\n请输入新用户名称",
    renameProfilePrompt: (name: string) => `重命名当前用户档案：${name}\n请输入新的用户名称`,
    profileSwitching: "正在切换用户",
    profileSwitched: (name: string) => `已切换到 ${name}`,
    profileCreated: (name: string) => `已创建用户 ${name}`,
    profileRenamed: (name: string) => `已重命名为 ${name}`,
    profileStyled: (name: string) => `已更新 ${name} 的角色`,
    profileStyle: "角色样式",
    profileSharedLibrary: "书库共用，进度和收藏独立保存。",
    library: "首页",
    reader: "阅读器",
    jobs: "任务",
    errors: "错误",
    about: "关于",
    aboutSubtitle: "个人数字资产库与客户端服务层。",
    version: "版本号",
    apiVersion: "API 版本",
    copyright: "版权信息",
    supportedFormats: "支持格式",
    capabilities: "服务能力",
    elapsed: "耗时",
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
    favoriteCollections: "收藏的作品集",
    likedCollections: "点赞的作品集",
    favoriteBooks: "收藏的书",
    noFavorites: "还没有收藏",
    wantSubtitle: "稍后再读",
    gameShelf: "游戏库",
    gameShelfSubtitle: "本地 ROM 与 ROM set",
    gameCatalogSubtitle: "按机种排序的完整游戏列表",
    videoShelf: "视频库",
    videoShelfSubtitle: "本地视频文件与空间媒体入口",
    videoCoverHint: "自定义封面可放在视频同目录：同名 .jpg/.png，或 poster.jpg / cover.jpg。",
    videoTranscodeHint: "该视频会按需转码为 HLS 播放，首次打开可能需要等待几秒。",
    videoTranscodeStatusFailed: "转码状态读取失败",
    videoTranscodeSegments: (count: number) => `${count} 个片段`,
    videoCurrentTranscode: (title: string) => `当前正在转码：${title}`,
    videoReloadPlayback: "重新加载播放",
    videoTranscodeStatusLabels: {
      idle: "等待转码",
      starting: "转码启动中",
      running: "转码中",
      queued: "等待当前转码完成",
      ready: "已缓存",
      failed: "转码失败",
    },
    more: "更多",
    loadingGames: "正在加载游戏",
    loadingVideos: "正在加载视频",
    loadMore: "加载更多",
    catalogLoadedCount: (loaded: number, total: number) => total > 0 ? `${loaded} / ${total} 个条目` : `${loaded} 个条目`,
    gameCount: (count: number) => `${count} 个游戏`,
    videoCount: (count: number) => `${count} 个视频`,
    recentSubtitle: "最近入库",
    libraryAssetType: "目录类型",
    assetTypeMixed: "自动",
    assetTypeBook: "书籍",
    assetTypeComic: "漫画",
    assetTypeGame: "游戏",
    assetTypeVideo: "视频",
    comicCollections: "漫画",
    bookCollections: "书籍",
    gameCollections: "游戏",
    videoCollections: "视频",
    libraries: "资源目录",
    name: "名称",
    add: "添加",
    scan: "扫描",
    scanWorkers: "扫描 Worker",
    scanWorkersHint: "新扫描任务使用的并发数量，NAS 建议 2-4，高性能机器可调到 8。",
    scanWorkersSaved: (count: number) => `扫描 Worker 已保存为 ${count}`,
    thumbnailWorker: "封面缩略图 Worker",
    thumbnailWorkerHint: "低优先级生成列表封面，不影响资源库扫描速度。",
    thumbnailQueued: "排队",
    thumbnailRunning: "运行",
    thumbnailReady: "完成",
    thumbnailFailed: "失败",
    thumbnailCancelled: "取消",
    thumbnailActive: "当前",
    thumbnailCache: "缓存",
    thumbnailCacheHint: (version: string, small: number, medium: number) => `${version} · small ${small}px / medium ${medium}px`,
    thumbnailCacheFiles: "文件",
    thumbnailCacheSize: "体积",
    thumbnailCacheReady: "有效",
    thumbnailCacheMissing: "缺失",
    thumbnailCacheStale: "旧版",
    thumbnailCacheOrphans: "未引用",
    cleanupThumbnailOrphans: "清理未引用缓存",
    refreshThumbnailWorker: "刷新",
    quickScan: "快速扫描",
    quickScanHint: "只扫描一个容器内子目录或单个文件，例如 /library/韩漫/某作品/Chap.263.zip。",
    quickScanAction: "扫描指定路径",
    quickScanRunning: "正在扫描",
    quickScanStarting: (path: string) => `正在扫描 ${path}`,
    quickScanQueued: (jobId: number) => `快速扫描已加入队列：job #${jobId}`,
    quickScanAlreadyRunning: (jobId: number) => `已有任务 #${jobId}`,
    quickScanAlreadyRunningHint: (jobId: number) => `这个路径已有扫描任务进行中：job #${jobId}`,
    recentScan: "只扫最新添加",
    recentScanAction: "只扫最新添加",
    recentScanLimit: "最新扫描数量",
    recentScanLimitOption: (count: number) => `最新 ${count} 个`,
    recentScanStarting: (path: string, count: number) => `正在扫描 ${path} 下最新 ${count} 个新增或变更文件`,
    recentScanQueued: (jobId: number) => `最新添加扫描已加入队列：job #${jobId}`,
    recentScanAlreadyRunningHint: (jobId: number) => `最新添加扫描已在进行中：job #${jobId}`,
    pause: "暂停",
    resume: "恢复",
    cancel: "取消",
    delete: "删除",
    chooseDirectory: "选择目录",
    directoryPickerTitle: "选择资源目录",
    directoryPickerHint: "这里显示的是服务所在设备或容器能访问的路径。",
    parentDirectory: "上一级",
    selectThisDirectory: "使用当前目录",
    loadingDirectories: "正在读取目录",
    noDirectories: "没有可进入的子目录",
    close: "关闭",
    collections: "作品集",
    collectionFavorite: "收藏作品集",
    collectionLike: "点赞作品集",
    collectionStateSaved: "作品集状态已保存",
    collectionStateFailed: "作品集状态保存失败",
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
    webtoon: "条漫",
    light: "浅色",
    sepia: "米色",
    dark: "深色",
    text: "字号",
    backToShelf: "返回书架",
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
    showBookDetails: "展开详情",
    hideBookDetails: "收起详情",
    loadingPage: (page: number) => `正在加载第 ${page} 页`,
    pageFailed: (page: number) => `第 ${page} 页加载失败`,
    retry: "重试",
    previous: "上一页",
    next: "下一页",
    epubChapterPageLabel: (current: number, total: number) => `本章第 ${current} / ${total} 页`,
    epubChapterSlider: "章节进度",
    pageSlider: "页面进度",
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
    profile: "使用者檔案",
    defaultProfile: "預設使用者",
    defaultProfileBadge: "預設",
    newProfile: "新增使用者",
    renameProfile: "重新命名",
    createProfilePrompt: "新增使用者檔案\n請輸入新使用者名稱",
    renameProfilePrompt: (name: string) => `重新命名目前使用者檔案：${name}\n請輸入新的使用者名稱`,
    profileSwitching: "正在切換使用者",
    profileSwitched: (name: string) => `已切換到 ${name}`,
    profileCreated: (name: string) => `已新增使用者 ${name}`,
    profileRenamed: (name: string) => `已重新命名為 ${name}`,
    profileStyled: (name: string) => `已更新 ${name} 的角色`,
    profileStyle: "角色樣式",
    profileSharedLibrary: "書庫共用，進度和收藏獨立保存。",
    library: "首頁",
    reader: "閱讀器",
    jobs: "任務",
    errors: "錯誤",
    about: "關於",
    aboutSubtitle: "個人數位資產庫與客戶端服務層。",
    version: "版本號",
    apiVersion: "API 版本",
    copyright: "版權資訊",
    supportedFormats: "支援格式",
    capabilities: "服務能力",
    elapsed: "耗時",
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
    favoriteCollections: "收藏的作品集",
    likedCollections: "讚好的作品集",
    favoriteBooks: "收藏的書",
    noFavorites: "還沒有收藏",
    wantSubtitle: "稍後再讀",
    gameShelf: "遊戲庫",
    gameShelfSubtitle: "本地 ROM 與 ROM set",
    gameCatalogSubtitle: "依機種排序的完整遊戲列表",
    videoShelf: "影片庫",
    videoShelfSubtitle: "本地影片檔與空間媒體入口",
    videoCoverHint: "自訂封面可放在影片同目錄：同名 .jpg/.png，或 poster.jpg / cover.jpg。",
    videoTranscodeHint: "此影片會按需轉碼為 HLS 播放，首次開啟可能需要等待幾秒。",
    videoTranscodeStatusFailed: "轉碼狀態讀取失敗",
    videoTranscodeSegments: (count: number) => `${count} 個片段`,
    videoCurrentTranscode: (title: string) => `目前正在轉碼：${title}`,
    videoReloadPlayback: "重新載入播放",
    videoTranscodeStatusLabels: {
      idle: "等待轉碼",
      starting: "轉碼啟動中",
      running: "轉碼中",
      queued: "等待目前轉碼完成",
      ready: "已快取",
      failed: "轉碼失敗",
    },
    more: "更多",
    loadingGames: "正在載入遊戲",
    loadingVideos: "正在載入影片",
    loadMore: "載入更多",
    catalogLoadedCount: (loaded: number, total: number) => total > 0 ? `${loaded} / ${total} 個項目` : `${loaded} 個項目`,
    gameCount: (count: number) => `${count} 個遊戲`,
    videoCount: (count: number) => `${count} 個影片`,
    recentSubtitle: "最近入庫",
    libraryAssetType: "目錄類型",
    assetTypeMixed: "自動",
    assetTypeBook: "書籍",
    assetTypeComic: "漫畫",
    assetTypeGame: "遊戲",
    assetTypeVideo: "影片",
    comicCollections: "漫畫",
    bookCollections: "書籍",
    gameCollections: "遊戲",
    videoCollections: "影片",
    libraries: "資源目錄",
    name: "名稱",
    add: "新增",
    scan: "掃描",
    scanWorkers: "掃描 Worker",
    scanWorkersHint: "新掃描任務使用的並發數量，NAS 建議 2-4，高效能機器可調到 8。",
    scanWorkersSaved: (count: number) => `掃描 Worker 已儲存為 ${count}`,
    thumbnailWorker: "封面縮圖 Worker",
    thumbnailWorkerHint: "低優先級產生列表封面，不影響資源庫掃描速度。",
    thumbnailQueued: "排隊",
    thumbnailRunning: "執行",
    thumbnailReady: "完成",
    thumbnailFailed: "失敗",
    thumbnailCancelled: "取消",
    thumbnailActive: "目前",
    thumbnailCache: "快取",
    thumbnailCacheHint: (version: string, small: number, medium: number) => `${version} · small ${small}px / medium ${medium}px`,
    thumbnailCacheFiles: "檔案",
    thumbnailCacheSize: "體積",
    thumbnailCacheReady: "有效",
    thumbnailCacheMissing: "缺失",
    thumbnailCacheStale: "舊版",
    thumbnailCacheOrphans: "未引用",
    cleanupThumbnailOrphans: "清理未引用快取",
    refreshThumbnailWorker: "重新整理",
    quickScan: "快速掃描",
    quickScanHint: "只掃描一個容器內子目錄或單一檔案，例如 /library/韓漫/某作品/Chap.263.zip。",
    quickScanAction: "掃描指定路徑",
    quickScanRunning: "正在掃描",
    quickScanStarting: (path: string) => `正在掃描 ${path}`,
    quickScanQueued: (jobId: number) => `快速掃描已加入佇列：job #${jobId}`,
    quickScanAlreadyRunning: (jobId: number) => `已有任務 #${jobId}`,
    quickScanAlreadyRunningHint: (jobId: number) => `這個路徑已有掃描任務進行中：job #${jobId}`,
    recentScan: "只掃最新加入",
    recentScanAction: "只掃最新加入",
    recentScanLimit: "最新掃描數量",
    recentScanLimitOption: (count: number) => `最新 ${count} 個`,
    recentScanStarting: (path: string, count: number) => `正在掃描 ${path} 下最新 ${count} 個新增或變更檔案`,
    recentScanQueued: (jobId: number) => `最新加入掃描已加入佇列：job #${jobId}`,
    recentScanAlreadyRunningHint: (jobId: number) => `最新加入掃描已在進行中：job #${jobId}`,
    pause: "暫停",
    resume: "恢復",
    cancel: "取消",
    delete: "刪除",
    chooseDirectory: "選擇目錄",
    directoryPickerTitle: "選擇資源目錄",
    directoryPickerHint: "這裡顯示的是服務所在設備或容器能存取的路徑。",
    parentDirectory: "上一級",
    selectThisDirectory: "使用目前目錄",
    loadingDirectories: "正在讀取目錄",
    noDirectories: "沒有可進入的子目錄",
    close: "關閉",
    collections: "作品集",
    collectionFavorite: "收藏作品集",
    collectionLike: "讚好作品集",
    collectionStateSaved: "作品集狀態已儲存",
    collectionStateFailed: "作品集狀態儲存失敗",
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
    webtoon: "條漫",
    light: "淺色",
    sepia: "米色",
    dark: "深色",
    text: "字號",
    backToShelf: "返回書架",
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
    showBookDetails: "展開詳情",
    hideBookDetails: "收起詳情",
    loadingPage: (page: number) => `正在載入第 ${page} 頁`,
    pageFailed: (page: number) => `第 ${page} 頁載入失敗`,
    retry: "重試",
    previous: "上一頁",
    next: "下一頁",
    epubChapterPageLabel: (current: number, total: number) => `本章第 ${current} / ${total} 頁`,
    epubChapterSlider: "章節進度",
    pageSlider: "頁面進度",
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
    profile: "Profile",
    defaultProfile: "Default Profile",
    defaultProfileBadge: "Default",
    newProfile: "New Profile",
    renameProfile: "Rename",
    createProfilePrompt: "Create a new profile\nEnter the new profile name",
    renameProfilePrompt: (name: string) => `Rename current profile: ${name}\nEnter the new profile name`,
    profileSwitching: "Switching profile",
    profileSwitched: (name: string) => `Switched to ${name}`,
    profileCreated: (name: string) => `Created profile ${name}`,
    profileRenamed: (name: string) => `Renamed to ${name}`,
    profileStyled: (name: string) => `Updated ${name}'s avatar`,
    profileStyle: "Avatar style",
    profileSharedLibrary: "Libraries are shared; progress and shelves stay separate.",
    library: "Home",
    reader: "Reader",
    jobs: "Jobs",
    errors: "Errors",
    about: "About",
    aboutSubtitle: "Personal digital asset library and client service layer.",
    version: "Version",
    apiVersion: "API Version",
    copyright: "Copyright",
    supportedFormats: "Supported Formats",
    capabilities: "Capabilities",
    elapsed: "Elapsed",
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
    favoriteCollections: "Favorite collections",
    likedCollections: "Liked collections",
    favoriteBooks: "Favorite books",
    noFavorites: "No favorites yet",
    wantSubtitle: "Queued for later",
    gameShelf: "Game Shelf",
    gameShelfSubtitle: "Local ROMs and ROM sets",
    gameCatalogSubtitle: "Full game catalog grouped by platform",
    videoShelf: "Video Shelf",
    videoShelfSubtitle: "Local video files and spatial media entry points",
    videoCoverHint: "Custom covers can sit next to the video as matching .jpg/.png, poster.jpg, or cover.jpg.",
    videoTranscodeHint: "This video will be transcoded to HLS on demand. First playback may take a few seconds.",
    videoTranscodeStatusFailed: "Failed to read transcode status",
    videoTranscodeSegments: (count: number) => `${count} segments`,
    videoCurrentTranscode: (title: string) => `Currently transcoding: ${title}`,
    videoReloadPlayback: "Reload playback",
    videoTranscodeStatusLabels: {
      idle: "Waiting to transcode",
      starting: "Starting transcode",
      running: "Transcoding",
      queued: "Waiting for current transcode",
      ready: "Cached",
      failed: "Transcode failed",
    },
    more: "More",
    loadingGames: "Loading games",
    loadingVideos: "Loading videos",
    loadMore: "Load more",
    catalogLoadedCount: (loaded: number, total: number) => total > 0 ? `${loaded} / ${total} items` : `${loaded} items`,
    gameCount: (count: number) => `${count} games`,
    videoCount: (count: number) => `${count} videos`,
    recentSubtitle: "Newest indexed volumes",
    libraryAssetType: "Library type",
    assetTypeMixed: "Auto",
    assetTypeBook: "Books",
    assetTypeComic: "Comics",
    assetTypeGame: "Games",
    assetTypeVideo: "Videos",
    comicCollections: "Comics",
    bookCollections: "Books",
    gameCollections: "Games",
    videoCollections: "Videos",
    libraries: "Resource Directories",
    name: "Name",
    add: "Add",
    scan: "Scan",
    scanWorkers: "Scan workers",
    scanWorkersHint: "Concurrent workers for new scan jobs. NAS defaults work well at 2-4; faster machines can use 8.",
    scanWorkersSaved: (count: number) => `Scan workers saved as ${count}`,
    thumbnailWorker: "Cover thumbnail worker",
    thumbnailWorkerHint: "Generates list covers at low priority without slowing library scans.",
    thumbnailQueued: "Queued",
    thumbnailRunning: "Running",
    thumbnailReady: "Ready",
    thumbnailFailed: "Failed",
    thumbnailCancelled: "Cancelled",
    thumbnailActive: "Active",
    thumbnailCache: "Cache",
    thumbnailCacheHint: (version: string, small: number, medium: number) => `${version} · small ${small}px / medium ${medium}px`,
    thumbnailCacheFiles: "Files",
    thumbnailCacheSize: "Size",
    thumbnailCacheReady: "Linked",
    thumbnailCacheMissing: "Missing",
    thumbnailCacheStale: "Stale",
    thumbnailCacheOrphans: "Unlinked",
    cleanupThumbnailOrphans: "Clean unlinked cache",
    refreshThumbnailWorker: "Refresh",
    quickScan: "Quick scan",
    quickScanHint: "Scan one container-visible subdirectory or file, for example /library/webtoon/Series/Chap.263.zip.",
    quickScanAction: "Scan path",
    quickScanRunning: "Scanning",
    quickScanStarting: (path: string) => `Scanning ${path}`,
    quickScanQueued: (jobId: number) => `Quick scan queued: job #${jobId}`,
    quickScanAlreadyRunning: (jobId: number) => `Job #${jobId} is running`,
    quickScanAlreadyRunningHint: (jobId: number) => `A scan for this path is already running as job #${jobId}.`,
    recentScan: "Scan latest additions",
    recentScanAction: "Scan latest additions",
    recentScanLimit: "Latest scan count",
    recentScanLimitOption: (count: number) => `Latest ${count}`,
    recentScanStarting: (path: string, count: number) => `Scanning latest ${count} new or changed files under ${path}`,
    recentScanQueued: (jobId: number) => `Latest-additions scan queued: job #${jobId}`,
    recentScanAlreadyRunningHint: (jobId: number) => `A latest-additions scan is already running as job #${jobId}.`,
    pause: "Pause",
    resume: "Resume",
    cancel: "Cancel",
    delete: "Delete",
    chooseDirectory: "Choose",
    directoryPickerTitle: "Choose Resource Directory",
    directoryPickerHint: "These are paths visible to the server device or container.",
    parentDirectory: "Parent",
    selectThisDirectory: "Use current directory",
    loadingDirectories: "Loading directories",
    noDirectories: "No child directories",
    close: "Close",
    collections: "Collections",
    collectionFavorite: "Favorite collection",
    collectionLike: "Like collection",
    collectionStateSaved: "Collection state saved",
    collectionStateFailed: "Failed to save collection state",
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
    webtoon: "Webtoon",
    light: "Light",
    sepia: "Sepia",
    dark: "Dark",
    text: "Text",
    backToShelf: "Back to Shelf",
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
    showBookDetails: "Show details",
    hideBookDetails: "Hide details",
    loadingPage: (page: number) => `Loading page ${page}`,
    pageFailed: (page: number) => `Page ${page} failed to load`,
    retry: "Retry",
    previous: "Previous",
    next: "Next",
    epubChapterPageLabel: (current: number, total: number) => `Chapter page ${current} / ${total}`,
    epubChapterSlider: "Chapter progress",
    pageSlider: "Page progress",
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
    profile: "プロフィール",
    defaultProfile: "デフォルト",
    defaultProfileBadge: "既定",
    newProfile: "新規プロフィール",
    renameProfile: "名前変更",
    createProfilePrompt: "新規プロフィールを作成\n新しいプロフィール名を入力",
    renameProfilePrompt: (name: string) => `現在のプロフィール名を変更：${name}\n新しいプロフィール名を入力`,
    profileSwitching: "プロフィールを切り替え中",
    profileSwitched: (name: string) => `${name} に切り替えました`,
    profileCreated: (name: string) => `${name} を作成しました`,
    profileRenamed: (name: string) => `${name} に変更しました`,
    profileStyled: (name: string) => `${name} のアバターを更新しました`,
    profileStyle: "アバター",
    profileSharedLibrary: "ライブラリは共有、進捗と棚は個別に保存されます。",
    library: "ホーム",
    reader: "リーダー",
    jobs: "ジョブ",
    errors: "エラー",
    about: "情報",
    aboutSubtitle: "個人向けデジタル資産ライブラリとクライアントサービス層。",
    version: "バージョン",
    apiVersion: "API バージョン",
    copyright: "著作権",
    supportedFormats: "対応形式",
    capabilities: "機能",
    elapsed: "経過",
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
    favoriteCollections: "お気に入りのコレクション",
    likedCollections: "いいねしたコレクション",
    favoriteBooks: "お気に入りの本",
    noFavorites: "お気に入りはまだありません",
    wantSubtitle: "あとで読む",
    gameShelf: "ゲーム棚",
    gameShelfSubtitle: "ローカル ROM と ROM set",
    gameCatalogSubtitle: "プラットフォーム順のゲーム一覧",
    videoShelf: "ビデオ棚",
    videoShelfSubtitle: "ローカル動画と空間メディアの入口",
    videoCoverHint: "カスタムカバーは動画と同じフォルダに同名 .jpg/.png、poster.jpg、cover.jpg として置けます。",
    videoTranscodeHint: "このビデオは必要に応じて HLS に変換されます。初回再生には数秒かかる場合があります。",
    videoTranscodeStatusFailed: "変換状態の取得に失敗しました",
    videoTranscodeSegments: (count: number) => `${count} セグメント`,
    videoCurrentTranscode: (title: string) => `現在変換中：${title}`,
    videoReloadPlayback: "再読み込み",
    videoTranscodeStatusLabels: {
      idle: "変換待ち",
      starting: "変換を開始中",
      running: "変換中",
      queued: "現在の変換を待機中",
      ready: "キャッシュ済み",
      failed: "変換失敗",
    },
    more: "もっと見る",
    loadingGames: "ゲームを読み込み中",
    loadingVideos: "ビデオを読み込み中",
    loadMore: "さらに読み込む",
    catalogLoadedCount: (loaded: number, total: number) => total > 0 ? `${loaded} / ${total} 件` : `${loaded} 件`,
    gameCount: (count: number) => `${count} 件のゲーム`,
    videoCount: (count: number) => `${count} 件のビデオ`,
    recentSubtitle: "最近追加",
    libraryAssetType: "ライブラリ種別",
    assetTypeMixed: "自動",
    assetTypeBook: "書籍",
    assetTypeComic: "漫画",
    assetTypeGame: "ゲーム",
    assetTypeVideo: "ビデオ",
    comicCollections: "漫画",
    bookCollections: "書籍",
    gameCollections: "ゲーム",
    videoCollections: "ビデオ",
    libraries: "リソース",
    name: "名前",
    add: "追加",
    scan: "スキャン",
    scanWorkers: "スキャン Worker",
    scanWorkersHint: "新しいスキャンで使う並列数です。NAS は 2-4、高性能な環境では 8 まで使えます。",
    scanWorkersSaved: (count: number) => `スキャン Worker を ${count} に保存しました`,
    thumbnailWorker: "表紙サムネイル Worker",
    thumbnailWorkerHint: "ライブラリスキャンを遅くせず、低優先度で一覧用表紙を生成します。",
    thumbnailQueued: "待機",
    thumbnailRunning: "実行中",
    thumbnailReady: "完了",
    thumbnailFailed: "失敗",
    thumbnailCancelled: "取消",
    thumbnailActive: "処理中",
    thumbnailCache: "キャッシュ",
    thumbnailCacheHint: (version: string, small: number, medium: number) => `${version} · small ${small}px / medium ${medium}px`,
    thumbnailCacheFiles: "ファイル",
    thumbnailCacheSize: "サイズ",
    thumbnailCacheReady: "有効",
    thumbnailCacheMissing: "不足",
    thumbnailCacheStale: "旧版",
    thumbnailCacheOrphans: "未参照",
    cleanupThumbnailOrphans: "未参照キャッシュを削除",
    refreshThumbnailWorker: "更新",
    quickScan: "クイックスキャン",
    quickScanHint: "コンテナ内のサブディレクトリまたは単一ファイルだけをスキャンします。例: /library/webtoon/Series/Chap.263.zip",
    quickScanAction: "指定パスをスキャン",
    quickScanRunning: "スキャン中",
    quickScanStarting: (path: string) => `${path} をスキャン中`,
    quickScanQueued: (jobId: number) => `クイックスキャンをキューに追加しました: job #${jobId}`,
    quickScanAlreadyRunning: (jobId: number) => `job #${jobId} が実行中`,
    quickScanAlreadyRunningHint: (jobId: number) => `このパスのスキャンはすでに job #${jobId} として実行中です。`,
    recentScan: "最新追加のみスキャン",
    recentScanAction: "最新追加のみスキャン",
    recentScanLimit: "最新スキャン数",
    recentScanLimitOption: (count: number) => `最新 ${count} 件`,
    recentScanStarting: (path: string, count: number) => `${path} の最新 ${count} 件の新規または変更ファイルをスキャン中`,
    recentScanQueued: (jobId: number) => `最新追加スキャンをキューに追加しました: job #${jobId}`,
    recentScanAlreadyRunningHint: (jobId: number) => `最新追加スキャンはすでに job #${jobId} として実行中です。`,
    pause: "一時停止",
    resume: "再開",
    cancel: "キャンセル",
    delete: "削除",
    chooseDirectory: "選択",
    directoryPickerTitle: "リソースディレクトリを選択",
    directoryPickerHint: "ここにはサービス側のデバイスまたはコンテナから見えるパスが表示されます。",
    parentDirectory: "上へ",
    selectThisDirectory: "このディレクトリを使用",
    loadingDirectories: "ディレクトリを読み込み中",
    noDirectories: "子ディレクトリがありません",
    close: "閉じる",
    collections: "コレクション",
    collectionFavorite: "コレクションをお気に入りにする",
    collectionLike: "コレクションにいいね",
    collectionStateSaved: "コレクションの状態を保存しました",
    collectionStateFailed: "コレクションの状態を保存できませんでした",
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
    webtoon: "縦スクロール",
    light: "ライト",
    sepia: "セピア",
    dark: "ダーク",
    text: "文字",
    backToShelf: "棚へ戻る",
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
    showBookDetails: "詳細を表示",
    hideBookDetails: "詳細を隠す",
    loadingPage: (page: number) => `${page} ページを読み込み中`,
    pageFailed: (page: number) => `${page} ページの読み込み失敗`,
    retry: "再試行",
    previous: "前へ",
    next: "次へ",
    epubChapterPageLabel: (current: number, total: number) => `章内 ${current} / ${total} ページ`,
    epubChapterSlider: "章の進捗",
    pageSlider: "ページ進捗",
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
    profile: "프로필",
    defaultProfile: "기본 프로필",
    defaultProfileBadge: "기본",
    newProfile: "새 프로필",
    renameProfile: "이름 변경",
    createProfilePrompt: "새 프로필 만들기\n새 프로필 이름을 입력하세요",
    renameProfilePrompt: (name: string) => `현재 프로필 이름 변경: ${name}\n새 프로필 이름을 입력하세요`,
    profileSwitching: "프로필 전환 중",
    profileSwitched: (name: string) => `${name}(으)로 전환됨`,
    profileCreated: (name: string) => `${name} 프로필 생성됨`,
    profileRenamed: (name: string) => `${name}(으)로 이름 변경됨`,
    profileStyled: (name: string) => `${name} 아바타가 업데이트됨`,
    profileStyle: "아바타 스타일",
    profileSharedLibrary: "라이브러리는 공유되고 진행률과 서가는 별도로 저장됩니다.",
    library: "홈",
    reader: "리더",
    jobs: "작업",
    errors: "오류",
    about: "정보",
    aboutSubtitle: "개인 디지털 자산 라이브러리와 클라이언트 서비스 계층.",
    version: "버전",
    apiVersion: "API 버전",
    copyright: "저작권",
    supportedFormats: "지원 형식",
    capabilities: "기능",
    elapsed: "경과",
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
    favoriteCollections: "즐겨찾기 컬렉션",
    likedCollections: "좋아요 컬렉션",
    favoriteBooks: "즐겨찾기 도서",
    noFavorites: "아직 즐겨찾기가 없습니다",
    wantSubtitle: "나중에 읽기",
    gameShelf: "게임 선반",
    gameShelfSubtitle: "로컬 ROM 및 ROM set",
    gameCatalogSubtitle: "플랫폼별 전체 게임 목록",
    videoShelf: "비디오 선반",
    videoShelfSubtitle: "로컬 비디오 파일과 공간 미디어 진입점",
    videoCoverHint: "사용자 지정 표지는 비디오와 같은 폴더에 같은 이름의 .jpg/.png, poster.jpg, cover.jpg로 둘 수 있습니다.",
    videoTranscodeHint: "이 비디오는 필요할 때 HLS로 변환됩니다. 처음 재생할 때 몇 초 걸릴 수 있습니다.",
    videoTranscodeStatusFailed: "변환 상태를 읽지 못했습니다",
    videoTranscodeSegments: (count: number) => `${count}개 세그먼트`,
    videoCurrentTranscode: (title: string) => `현재 변환 중: ${title}`,
    videoReloadPlayback: "재생 다시 불러오기",
    videoTranscodeStatusLabels: {
      idle: "변환 대기",
      starting: "변환 시작 중",
      running: "변환 중",
      queued: "현재 변환 완료 대기",
      ready: "캐시됨",
      failed: "변환 실패",
    },
    more: "더 보기",
    loadingGames: "게임 불러오는 중",
    loadingVideos: "비디오 불러오는 중",
    loadMore: "더 불러오기",
    catalogLoadedCount: (loaded: number, total: number) => total > 0 ? `${loaded} / ${total}개 항목` : `${loaded}개 항목`,
    gameCount: (count: number) => `${count}개 게임`,
    videoCount: (count: number) => `${count}개 비디오`,
    recentSubtitle: "최근 인덱싱된 항목",
    libraryAssetType: "라이브러리 유형",
    assetTypeMixed: "자동",
    assetTypeBook: "도서",
    assetTypeComic: "만화",
    assetTypeGame: "게임",
    assetTypeVideo: "비디오",
    comicCollections: "만화",
    bookCollections: "도서",
    gameCollections: "게임",
    videoCollections: "비디오",
    libraries: "리소스",
    name: "이름",
    add: "추가",
    scan: "스캔",
    scanWorkers: "스캔 Worker",
    scanWorkersHint: "새 스캔 작업의 동시 실행 수입니다. NAS는 2-4, 고성능 장비는 8까지 사용할 수 있습니다.",
    scanWorkersSaved: (count: number) => `스캔 Worker가 ${count}(으)로 저장됨`,
    thumbnailWorker: "표지 썸네일 Worker",
    thumbnailWorkerHint: "라이브러리 스캔을 늦추지 않도록 낮은 우선순위로 목록 표지를 생성합니다.",
    thumbnailQueued: "대기",
    thumbnailRunning: "실행",
    thumbnailReady: "완료",
    thumbnailFailed: "실패",
    thumbnailCancelled: "취소",
    thumbnailActive: "현재",
    thumbnailCache: "캐시",
    thumbnailCacheHint: (version: string, small: number, medium: number) => `${version} · small ${small}px / medium ${medium}px`,
    thumbnailCacheFiles: "파일",
    thumbnailCacheSize: "크기",
    thumbnailCacheReady: "연결됨",
    thumbnailCacheMissing: "누락",
    thumbnailCacheStale: "이전",
    thumbnailCacheOrphans: "미참조",
    cleanupThumbnailOrphans: "미참조 캐시 정리",
    refreshThumbnailWorker: "새로고침",
    quickScan: "빠른 스캔",
    quickScanHint: "컨테이너 안의 하위 폴더나 단일 파일만 스캔합니다. 예: /library/webtoon/Series/Chap.263.zip",
    quickScanAction: "지정 경로 스캔",
    quickScanRunning: "스캔 중",
    quickScanStarting: (path: string) => `${path} 스캔 중`,
    quickScanQueued: (jobId: number) => `빠른 스캔 대기열 추가: job #${jobId}`,
    quickScanAlreadyRunning: (jobId: number) => `job #${jobId} 실행 중`,
    quickScanAlreadyRunningHint: (jobId: number) => `이 경로의 스캔이 이미 job #${jobId}로 실행 중입니다.`,
    recentScan: "최근 추가만 스캔",
    recentScanAction: "최근 추가만 스캔",
    recentScanLimit: "최근 스캔 수",
    recentScanLimitOption: (count: number) => `최근 ${count}개`,
    recentScanStarting: (path: string, count: number) => `${path} 아래 최근 ${count}개의 신규 또는 변경 파일 스캔 중`,
    recentScanQueued: (jobId: number) => `최근 추가 스캔 대기열 추가: job #${jobId}`,
    recentScanAlreadyRunningHint: (jobId: number) => `최근 추가 스캔이 이미 job #${jobId}로 실행 중입니다.`,
    pause: "일시정지",
    resume: "재개",
    cancel: "취소",
    delete: "삭제",
    chooseDirectory: "선택",
    directoryPickerTitle: "리소스 디렉터리 선택",
    directoryPickerHint: "서비스가 실행 중인 장치나 컨테이너에서 보이는 경로입니다.",
    parentDirectory: "상위",
    selectThisDirectory: "현재 디렉터리 사용",
    loadingDirectories: "디렉터리 읽는 중",
    noDirectories: "하위 디렉터리가 없습니다",
    close: "닫기",
    collections: "컬렉션",
    collectionFavorite: "컬렉션 즐겨찾기",
    collectionLike: "컬렉션 좋아요",
    collectionStateSaved: "컬렉션 상태가 저장됨",
    collectionStateFailed: "컬렉션 상태 저장 실패",
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
    webtoon: "웹툰",
    light: "라이트",
    sepia: "세피아",
    dark: "다크",
    text: "글자",
    backToShelf: "서가로 돌아가기",
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
    showBookDetails: "상세 보기",
    hideBookDetails: "상세 숨기기",
    loadingPage: (page: number) => `${page}페이지 불러오는 중`,
    pageFailed: (page: number) => `${page}페이지 불러오기 실패`,
    retry: "다시 시도",
    previous: "이전",
    next: "다음",
    epubChapterPageLabel: (current: number, total: number) => `현재 장 ${current} / ${total}페이지`,
    epubChapterSlider: "장 진행률",
    pageSlider: "페이지 진행률",
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

function readLocalPreferences(): ClientPreferences {
  const stored = readLocalStorage("foliospace_preferences");
  if (stored) {
    try {
      return normalizeClientPreferences(JSON.parse(stored));
    } catch {
      // Fall through to legacy locale migration.
    }
  }
  const legacyLocale = readLocalStorage("foliospace_locale");
  return normalizeClientPreferences({ ...defaultClientPreferences(), locale: isLocale(legacyLocale) ? legacyLocale : "zh" });
}

function writeLocalPreferences(preferences: ClientPreferences) {
  const normalized = normalizeClientPreferences(preferences);
  writeLocalStorage("foliospace_preferences", JSON.stringify(normalized));
  writeLocalStorage("foliospace_locale", normalized.locale);
}

function clampScanWorkers(value: number) {
  if (!Number.isFinite(value)) return 1;
  return Math.max(1, Math.min(8, Math.round(value)));
}

function formatCapabilityName(value: string) {
  return value
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .replace(/\b\w/g, (char) => char.toUpperCase());
}

function readerTotalPages(book: Book, archivePages: number, pdfPages: number) {
  if (book.format === "pdf") return pdfPages;
  return archivePages;
}

function readLocalStorage(key: string) {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

function writeLocalStorage(key: string, value: string) {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // Ignore storage failures in restricted browser contexts.
  }
}

function defaultClientPreferences(): ClientPreferences {
  return {
    locale: "zh",
    readerPageMode: "single",
    epubPageMode: "single",
    epubTheme: "light",
    epubFontSize: 18,
  };
}

function normalizeClientPreferences(value: Partial<ClientPreferences>): ClientPreferences {
  const defaults = defaultClientPreferences();
  const locale = value.locale;
  const readerPageMode = value.readerPageMode;
  const epubPageMode = value.epubPageMode;
  const epubTheme = value.epubTheme;
  const epubFontSize = Number(value.epubFontSize);
  return {
    locale: locale === "zh" || locale === "zht" || locale === "en" || locale === "ja" || locale === "ko" ? locale : defaults.locale,
    readerPageMode: readerPageMode === "double" || readerPageMode === "webtoon" ? readerPageMode : defaults.readerPageMode,
    epubPageMode: epubPageMode === "double" ? "double" : defaults.epubPageMode,
    epubTheme: epubTheme === "sepia" || epubTheme === "dark" || epubTheme === "light" ? epubTheme : defaults.epubTheme,
    epubFontSize: Number.isFinite(epubFontSize) ? Math.max(14, Math.min(26, Math.round(epubFontSize))) : defaults.epubFontSize,
  };
}

function isLocale(value: string | null | undefined): value is Locale {
  return value === "zh" || value === "zht" || value === "en" || value === "ja" || value === "ko";
}

function arrayOrEmpty<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

function resolveActiveProfile(profiles: Profile[], storedProfileID: string | number) {
  const parsedID = Number(storedProfileID);
  if (Number.isFinite(parsedID) && parsedID > 0) {
    const storedProfile = profiles.find((profile) => profile.id === Math.trunc(parsedID));
    if (storedProfile) return storedProfile;
  }
  return profiles.find((profile) => profile.isDefault) ?? profiles[0] ?? null;
}

function profileDisplayName(profile: Profile, t: Translation) {
  return profile.isDefault ? `${profile.name} · ${t.defaultProfileBadge}` : profile.name;
}

const profilePresets = [
  { avatar: "reader", color: "teal", label: "Reader" },
  { avatar: "comic", color: "amber", label: "Comic" },
  { avatar: "game", color: "violet", label: "Game" },
  { avatar: "movie", color: "rose", label: "Movie" },
  { avatar: "star", color: "blue", label: "Stargazer" },
  { avatar: "archive", color: "green", label: "Archive" },
  { avatar: "coffee", color: "slate", label: "Study" },
  { avatar: "rocket", color: "copper", label: "Explorer" },
] as const;

function profilePresetForIndex(index: number) {
  return profilePresets[Math.max(0, index) % profilePresets.length];
}

function fallbackProfile(name: string): Profile {
  return {
    id: 0,
    name,
    avatar: "reader",
    color: "teal",
    isDefault: true,
    createdAt: "",
    updatedAt: "",
  };
}

function ProfileAvatar({ profile }: { profile: Pick<Profile, "name" | "avatar" | "color"> }) {
  return (
    <span className={`profileAvatar profileColor-${profile.color || "teal"}`} aria-hidden="true">
      <svg viewBox="0 0 24 24" focusable="false">
        {profileAvatarPaths(profile.avatar)}
      </svg>
    </span>
  );
}

function profileAvatarPaths(avatar: string) {
  switch (avatar) {
    case "comic":
      return (
        <>
          <path d="M5 5.5h10.5a3.5 3.5 0 0 1 3.5 3.5v8.5H8.5A3.5 3.5 0 0 1 5 14Z" />
          <path d="M9 9h6" />
          <path d="M9 12h4" />
        </>
      );
    case "game":
      return (
        <>
          <path d="M7.5 10h9a4 4 0 0 1 3.9 3.1l.5 2.3a2 2 0 0 1-3.3 1.9l-1.5-1.4H7.9l-1.5 1.4a2 2 0 0 1-3.3-1.9l.5-2.3A4 4 0 0 1 7.5 10Z" />
          <path d="M8 13v3" />
          <path d="M6.5 14.5h3" />
          <path d="M15.5 14h.1" />
          <path d="M18 14.8h.1" />
        </>
      );
    case "movie":
      return (
        <>
          <path d="M5 6h14v12H5Z" />
          <path d="M8 6v12" />
          <path d="M16 6v12" />
          <path d="M5 10h14" />
          <path d="M5 14h14" />
        </>
      );
    case "star":
      return <path d="m12 4 2.2 4.7 5.1.7-3.7 3.6.9 5.1-4.5-2.4-4.5 2.4.9-5.1-3.7-3.6 5.1-.7Z" />;
    case "archive":
      return (
        <>
          <path d="M5 7h14v12H5Z" />
          <path d="M7 5h10v2H7Z" />
          <path d="M9 11h6" />
        </>
      );
    case "coffee":
      return (
        <>
          <path d="M6 9h10v5a4 4 0 0 1-4 4h-2a4 4 0 0 1-4-4Z" />
          <path d="M16 10h1a2 2 0 0 1 0 4h-1" />
          <path d="M8 5v2" />
          <path d="M12 5v2" />
        </>
      );
    case "rocket":
      return (
        <>
          <path d="M12 14 9 11c.9-3.8 3-6 7-7 .3 4-1.2 6.9-4 10Z" />
          <path d="M9 11 5.5 12.5 8 15" />
          <path d="M12 14 13 18l2-3.5" />
          <path d="M6 18l2-2" />
        </>
      );
    default:
      return (
        <>
          <path d="M5 5.5A3.5 3.5 0 0 1 8.5 2H19v16H8.5A3.5 3.5 0 0 0 5 21.5Z" />
          <path d="M5 5.5v16" />
          <path d="M9 6h6" />
        </>
      );
  }
}

function mergeByID<T extends { id: number }>(current: T[], incoming: T[]) {
  const seen = new Set<number>();
  return [...current, ...incoming].filter((item) => {
    if (seen.has(item.id)) return false;
    seen.add(item.id);
    return true;
  });
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

function replaceSeries(items: Series[], updatedSeries: Series) {
  return items.map((series) => (series.id === updatedSeries.id ? updatedSeries : series));
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

function isActiveScanStatus(status: string) {
  return status === "running" || status === "pause_requested" || status === "cancel_requested";
}

function normalizeQuickScanTarget(library: Library, inputPath: string) {
  const rawPath = inputPath.trim();
  if (!rawPath) return "";
  const joined = rawPath.startsWith("/") ? rawPath : `${library.rootPath}/${rawPath}`;
  const parts: string[] = [];
  joined.split("/").forEach((part) => {
    if (!part || part === ".") return;
    if (part === "..") {
      parts.pop();
      return;
    }
    parts.push(part);
  });
  return `/${parts.join("/")}`;
}

function recentScanTargetLabel(path: string, limit: number) {
  return `${path} [recent:${Math.max(1, Math.min(200, Math.trunc(limit) || 20))}]`;
}

function canPauseJob(job: ScanJob) {
  return job.status === "running";
}

function canCancelJob(job: ScanJob) {
  return job.status === "running" || job.status === "pause_requested" || job.status === "paused";
}

function canResumeJob(job: ScanJob) {
  return job.status === "paused";
}

function readingProgress(book: Book) {
  return Math.max(0, Math.min(100, Math.round((book.progressFraction || 0) * 100)));
}

function privateShelfMeta(book: Book, t: Translation) {
  const meta = privateMeta(book, t);
  const location = book.creator || book.collectionTitle || t.library;
  return meta ? `${meta} · ${location}` : location;
}

function gameMeta(game: GameAsset, t: Translation) {
  return [game.platform || game.format, game.region, game.romSetName, game.compatibility]
    .filter(Boolean)
    .join(" · ") || t.assetTypeGame;
}

function videoMeta(video: VideoAsset, t: Translation) {
  const parts = [video.format.toUpperCase()];
  if (video.width > 0 && video.height > 0) {
    parts.push(`${video.width}x${video.height}`);
  }
  if (video.durationSeconds > 0) {
    parts.push(formatDuration(video.durationSeconds));
  }
  return parts.join(" · ") || t.assetTypeVideo;
}

function videoTranscodeLabel(status: VideoTranscodeStatus | null, t: Translation) {
  const current = status?.status || "idle";
  const label = t.videoTranscodeStatusLabels[current];
  if (!status || status.segmentCount <= 0) {
    return label;
  }
  return `${label} · ${t.videoTranscodeSegments(status.segmentCount)}`;
}

function videoPlaybackSource(video: VideoAsset) {
  if (video.playbackMode === "hls") {
    return authenticatedResourcePath(video.hlsUrl || `/api/client/videos/${video.id}/hls/index.m3u8`);
  }
  return authenticatedResourcePath(video.fileUrl || `/api/client/videos/${video.id}/file`);
}

function formatDuration(seconds: number) {
  const total = Math.max(0, Math.round(seconds));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const rest = total % 60;
  if (hours > 0) return `${hours}:${String(minutes).padStart(2, "0")}:${String(rest).padStart(2, "0")}`;
  return `${minutes}:${String(rest).padStart(2, "0")}`;
}

function formatBytes(bytes: number) {
  const value = Math.max(0, bytes || 0);
  if (value < 1024) return `${value} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let scaled = value / 1024;
  let unit = units[0];
  for (let i = 1; i < units.length && scaled >= 1024; i += 1) {
    scaled /= 1024;
    unit = units[i];
  }
  return `${scaled >= 10 ? scaled.toFixed(1) : scaled.toFixed(2)} ${unit}`;
}

function libraryAssetTypeLabel(value: string | undefined, t: Translation) {
  switch (value) {
    case "book":
      return t.assetTypeBook;
    case "comic":
      return t.assetTypeComic;
    case "game":
      return t.assetTypeGame;
    case "video":
      return t.assetTypeVideo;
    default:
      return t.assetTypeMixed;
  }
}

function recentMeta(book: Book, t: Translation) {
  const added = formatRelativeDate(book.addedAt, t);
  return `${added}${book.creator ? ` · ${book.creator}` : book.collectionTitle ? ` · ${book.collectionTitle}` : ""}`;
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

function formatElapsed(job: ScanJob, now = Date.now()) {
  const started = new Date(job.startedAt).getTime();
  const finished = validFinishedAt(job.finishedAt) ?? now;
  if (!Number.isFinite(started) || !Number.isFinite(finished) || finished < started) return "-";

  const seconds = Math.max(0, Math.floor((finished - started) / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${minutes % 60}m`;
}

function validFinishedAt(value?: string) {
  if (!value || value.startsWith("0001-")) return null;
  const parsed = new Date(value).getTime();
  return Number.isFinite(parsed) ? parsed : null;
}
