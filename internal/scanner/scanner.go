package scanner

import (
	stdzip "archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"foliospace-reader/internal/archive"
	"foliospace-reader/internal/bookmeta"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/launchcatalog"
	"foliospace-reader/internal/naomi2catalog"
	"foliospace-reader/internal/store"
	"golang.org/x/text/encoding/japanese"
)

type Scanner struct {
	bookIndexMu     sync.Mutex
	store           *store.Store
	workerCount     func() int
	completionHook  func(domain.Library, domain.ScanJob)
	gamelistCache   sync.Map
	pegasusCache    sync.Map
	dosCatalogCache sync.Map
}

type scanScope struct {
	rootPath         string
	fullScan         bool
	deferPageIndex   bool
	bookMetadata     *bookmeta.Resolver
	metadataWarnings *sync.Map
}

type scanDirState struct {
	mtime      time.Time
	hasSubdirs bool
}

var (
	errScanPaused    = errors.New("scan paused")
	errScanCancelled = errors.New("scan cancelled")
)

func New(store *store.Store) *Scanner {
	return NewWithWorkerCount(store, scanWorkerCount)
}

func NewWithWorkerCount(store *store.Store, workerCount func() int) *Scanner {
	if workerCount == nil {
		workerCount = scanWorkerCount
	}
	return &Scanner{store: store, workerCount: workerCount}
}

func (s *Scanner) SetCompletionHook(hook func(domain.Library, domain.ScanJob)) {
	s.completionHook = hook
}

func (s *Scanner) notifyCompleted(library domain.Library, job domain.ScanJob) {
	if s.completionHook != nil {
		s.completionHook(library, job)
	}
}

func (s *Scanner) ScanLibrary(library domain.Library) (domain.ScanJob, error) {
	job, err := s.store.StartScanJobWithTarget(library.ID, filepath.Clean(library.RootPath))
	if err != nil {
		return job, err
	}
	return s.RunScanJob(library, job)
}

func (s *Scanner) ScanLibraryPath(library domain.Library, targetPath string) (domain.ScanJob, error) {
	scope, err := scanScopeForPath(library, targetPath)
	if err != nil {
		return domain.ScanJob{}, err
	}
	job, err := s.store.StartScanJobWithTarget(library.ID, scope.rootPath)
	if err != nil {
		return job, err
	}
	return s.runScanJob(library, job, scope)
}

func (s *Scanner) StartScanJob(library domain.Library) (domain.ScanJob, error) {
	job, err := s.store.StartScanJobWithTarget(library.ID, filepath.Clean(library.RootPath))
	if err != nil {
		return job, err
	}
	go func() {
		_, _ = s.RunScanJob(library, job)
	}()
	return job, nil
}

func (s *Scanner) StartScanJobPath(library domain.Library, targetPath string) (domain.ScanJob, error) {
	scope, err := scanScopeForPath(library, targetPath)
	if err != nil {
		return domain.ScanJob{}, err
	}
	job, err := s.store.StartScanJobWithTarget(library.ID, scope.rootPath)
	if err != nil {
		return job, err
	}
	go func() {
		_, _ = s.runScanJob(library, job, scope)
	}()
	return job, nil
}

func (s *Scanner) StartRecentScanJobPath(library domain.Library, targetPath string, limit int) (domain.ScanJob, error) {
	scope, err := scanScopeForPath(library, targetPath)
	if err != nil {
		return domain.ScanJob{}, err
	}
	limit = NormalizeRecentScanLimit(limit)
	job, err := s.store.StartScanJobWithTarget(library.ID, recentScanTargetLabel(scope.rootPath, limit))
	if err != nil {
		return job, err
	}
	go func() {
		_, _ = s.runRecentScanJob(library, job, scope, limit)
	}()
	return job, nil
}

func (s *Scanner) RunScanJob(library domain.Library, job domain.ScanJob) (domain.ScanJob, error) {
	return s.runScanJob(library, job, scanScope{rootPath: library.RootPath, fullScan: true})
}

func (s *Scanner) RunScanJobPath(library domain.Library, job domain.ScanJob, targetPath string) (domain.ScanJob, error) {
	scope, err := scanScopeForPath(library, targetPath)
	if err != nil {
		job.Status = "failed"
		job.CurrentPath = ""
		job.ErrorCount++
		job.FinishedAt = time.Now()
		_ = s.store.UpdateScanJob(job)
		_ = s.store.AddJobEvent(job.ID, "error", "scan target failed: "+err.Error())
		return job, err
	}
	return s.runScanJob(library, job, scope)
}

func (s *Scanner) RunRecentScanJobPath(library domain.Library, job domain.ScanJob, targetPath string, limit int) (domain.ScanJob, error) {
	scope, err := scanScopeForPath(library, targetPath)
	if err != nil {
		job.Status = "failed"
		job.CurrentPath = ""
		job.ErrorCount++
		job.FinishedAt = time.Now()
		_ = s.store.UpdateScanJob(job)
		_ = s.store.AddJobEvent(job.ID, "error", "recent scan target failed: "+err.Error())
		return job, err
	}
	return s.runRecentScanJob(library, job, scope, NormalizeRecentScanLimit(limit))
}

func (s *Scanner) runScanJob(library domain.Library, job domain.ScanJob, scope scanScope) (domain.ScanJob, error) {
	scope.bookMetadata = bookmeta.NewResolver(library.RootPath)
	scope.metadataWarnings = &sync.Map{}
	_ = s.store.AddJobEvent(job.ID, "info", "scan started")
	_ = s.store.AddJobEvent(job.ID, "info", "walking "+scope.rootPath)

	workers := s.workerCount()
	if workers > 1 {
		return s.runScanJobConcurrent(library, job, workers, scope)
	}

	dirStates := map[string]*scanDirState{}
	fileIndexes, _ := s.store.ListFileIndexesByLibrary(library.ID)
	walkErr := s.walkScanScope(library, scope, dirStates, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := s.applyScanControl(&job); err != nil {
			return err
		}
		if walkErr != nil {
			if shouldSkipScanDir(library, path) {
				return filepath.SkipDir
			}
			job.CurrentPath = path
			job.ErrorCount++
			_ = s.recordPathError(library.ID, job.ID, path, classifyWalkError(walkErr), walkErr.Error())
			_ = s.store.AddJobEvent(job.ID, "error", "walk failed: "+path)
			_ = s.store.UpdateScanJob(job)
			return nil
		}
		if entry.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		kind := classifyFileKind(library, path, ext)
		if kind == "" {
			return nil
		}
		job.CurrentPath = path
		job.DiscoveredFiles++

		info, err := entry.Info()
		if err != nil {
			job.ErrorCount++
			_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorUnknownIO, err.Error())
			_ = s.store.AddJobEvent(job.ID, "error", "stat failed: "+path)
			_ = s.store.UpdateScanJob(job)
			return nil
		}
		if info.Size() == 0 {
			job.ErrorCount++
			_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorEmptyFile, "empty file")
			_ = s.store.AddJobEvent(job.ID, "error", "empty file: "+path)
			_ = s.store.UpdateScanJob(job)
			return nil
		}
		if kind == "game" {
			relPath, err := filepath.Rel(library.RootPath, path)
			if err != nil {
				job.ErrorCount++
				_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorUnknownIO, err.Error())
				_ = s.store.AddJobEvent(job.ID, "error", "relative path failed: "+path)
				_ = s.store.UpdateScanJob(job)
				return nil
			}
			expectedPlatform := inferLibraryGamePlatform(library, ext, relPath)
			if s.canSkipGame(library, path, info, ext, expectedPlatform) {
				job.SkippedFiles++
				_ = s.store.UpdateScanJob(job)
				return nil
			}
			if err := s.indexGameFile(library, path, info, ext); err != nil {
				job.ErrorCount++
				_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorUnknownIO, err.Error())
				_ = s.store.AddJobEvent(job.ID, "error", "game metadata failed: "+path)
				_ = s.store.UpdateScanJob(job)
				return nil
			}
			job.IndexedFiles++
			_ = s.store.AddJobEvent(job.ID, "info", "indexed game: "+path)
			_ = s.store.UpdateScanJob(job)
			return nil
		}
		if kind == "video" {
			if s.store.CanSkipVideo(path, info.Size(), info.ModTime()) {
				if err := s.refreshVideoLocalMetadata(path); err != nil {
					job.ErrorCount++
					_ = s.store.AddJobEvent(job.ID, "error", "video local metadata refresh failed: "+path)
				}
				job.SkippedFiles++
				_ = s.store.UpdateScanJob(job)
				return nil
			}
			if err := s.indexVideoFile(library, path, info, ext); err != nil {
				job.ErrorCount++
				_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorUnknownIO, err.Error())
				_ = s.store.AddJobEvent(job.ID, "error", "video metadata failed: "+path)
				_ = s.store.UpdateScanJob(job)
				return nil
			}
			job.IndexedFiles++
			_ = s.store.AddJobEvent(job.ID, "info", "indexed video: "+path)
			_ = s.store.UpdateScanJob(job)
			return nil
		}
		if _, unchanged := s.unchangedMetadataOnlyFile(fileIndexes, path, info, ext); unchanged {
			if metadata, found, metadataErr := scope.bookMetadata.Lookup(path); found || metadataErr != nil {
				if found {
					result, err := s.indexResolvedFileMetadata(library, path, info, ext, importedBookMetadata(path, metadata))
					if err != nil {
						metadataErr = err
						found = false
					} else {
						job.SkippedFiles++
						if result.MetadataUpdated {
							job.MetadataUpdatedFiles++
						}
						if result.Reclassified {
							job.ReclassifiedFiles++
						}
					}
				}
				if metadataErr != nil {
					s.recordBookMetadataWarning(job.ID, scope, metadataErr)
					if !found {
						job.ErrorCount++
					}
				}
				_ = s.store.UpdateScanJob(job)
				return nil
			}
		}
		if ext != ".epub" {
			if index, ok := s.unchangedMetadataOnlyFile(fileIndexes, path, info, ext); ok && canSkipUnchangedBook(library, path, index, ext) {
				if err := s.cleanupStaleNonBookAssets(path); err != nil {
					job.ErrorCount++
					_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorUnknownIO, err.Error())
					_ = s.store.AddJobEvent(job.ID, "error", "stale asset cleanup failed: "+path)
					_ = s.store.UpdateScanJob(job)
					return nil
				}
				job.SkippedFiles++
				_ = s.store.UpdateScanJob(job)
				return nil
			}
		}

		if index, ok := s.unchangedFileIndex(fileIndexes, path, info, ext); ok {
			if canSkipUnchangedBook(library, path, index, ext) {
				if err := s.cleanupStaleNonBookAssets(path); err != nil {
					job.ErrorCount++
					_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorUnknownIO, err.Error())
					_ = s.store.AddJobEvent(job.ID, "error", "stale asset cleanup failed: "+path)
					_ = s.store.UpdateScanJob(job)
					return nil
				}
				job.SkippedFiles++
				_ = s.store.UpdateScanJob(job)
				return nil
			}
			result, err := s.indexFileMetadata(library, path, info, ext, scope.bookMetadata)
			if err != nil {
				job.ErrorCount++
				_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorUnknownIO, err.Error())
				_ = s.store.AddJobEvent(job.ID, "error", "metadata update failed: "+path)
				_ = s.store.UpdateScanJob(job)
				return nil
			}
			if result.MetadataUpdated {
				job.MetadataUpdatedFiles++
			}
			if result.Reclassified {
				job.ReclassifiedFiles++
			}
			job.SkippedFiles++
			_ = s.store.UpdateScanJob(job)
			return nil
		}

		if err := s.store.DeleteGameByPath(path); err != nil {
			job.ErrorCount++
			_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorUnknownIO, err.Error())
			_ = s.store.AddJobEvent(job.ID, "error", "game cleanup failed: "+path)
			_ = s.store.UpdateScanJob(job)
			return nil
		}
		if err := s.store.DeleteVideoByPath(path); err != nil {
			job.ErrorCount++
			_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorUnknownIO, err.Error())
			_ = s.store.AddJobEvent(job.ID, "error", "video cleanup failed: "+path)
			_ = s.store.UpdateScanJob(job)
			return nil
		}

		result, err := s.indexScanFile(library, job.ID, path, info, ext, scope)
		if err != nil {
			job.ErrorCount++
			_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorArchiveOpenFailed, err.Error())
			_ = s.store.AddJobEvent(job.ID, "error", "archive failed: "+path)
			_ = s.store.UpdateScanJob(job)
			return nil
		}
		job.IndexedFiles++
		if result.MetadataUpdated {
			job.MetadataUpdatedFiles++
		}
		if result.Reclassified {
			job.ReclassifiedFiles++
		}
		_ = s.store.AddJobEvent(job.ID, "info", "indexed: "+path)
		_ = s.store.UpdateScanJob(job)
		return nil
	})
	if errors.Is(walkErr, errScanPaused) || errors.Is(walkErr, errScanCancelled) {
		return job, nil
	}
	if walkErr != nil {
		job.Status = "failed"
		job.CurrentPath = ""
		job.FinishedAt = time.Now()
		_ = s.store.UpdateScanJob(job)
		_ = s.store.AddJobEvent(job.ID, "error", "scan failed: "+walkErr.Error())
		return job, walkErr
	}
	if err := s.persistScanDirectories(library.ID, dirStates); err != nil {
		job.Status = "failed"
		job.CurrentPath = ""
		job.FinishedAt = time.Now()
		_ = s.store.UpdateScanJob(job)
		_ = s.store.AddJobEvent(job.ID, "error", "directory cache failed: "+err.Error())
		return job, err
	}
	if scope.fullScan {
		if err := s.cleanupSkippedEntries(library, &job); err != nil {
			job.Status = "failed"
			job.CurrentPath = ""
			job.FinishedAt = time.Now()
			_ = s.store.UpdateScanJob(job)
			_ = s.store.AddJobEvent(job.ID, "error", "cleanup failed: "+err.Error())
			return job, err
		}
	} else if err := s.store.DeleteEmptySeries(library.ID); err != nil {
		job.Status = "failed"
		job.CurrentPath = ""
		job.FinishedAt = time.Now()
		_ = s.store.UpdateScanJob(job)
		_ = s.store.AddJobEvent(job.ID, "error", "cleanup failed: "+err.Error())
		return job, err
	}

	job.Status = "completed"
	job.CurrentPath = ""
	job.FinishedAt = time.Now()
	if err := s.store.UpdateScanJob(job); err != nil {
		return job, err
	}
	_ = s.store.AddJobEvent(job.ID, "info", "scan completed")
	s.notifyCompleted(library, job)
	return job, nil
}

func (s *Scanner) runRecentScanJob(library domain.Library, job domain.ScanJob, scope scanScope, limit int) (domain.ScanJob, error) {
	scope.bookMetadata = bookmeta.NewResolver(library.RootPath)
	scope.metadataWarnings = &sync.Map{}
	limit = NormalizeRecentScanLimit(limit)
	scope.fullScan = false
	scope.deferPageIndex = true
	_ = s.store.AddJobEvent(job.ID, "info", "recent scan started")
	_ = s.store.AddJobEvent(job.ID, "info", fmt.Sprintf("finding latest %d under %s", limit, scope.rootPath))

	dirStates := map[string]*scanDirState{}
	fileIndexes, _ := s.store.ListFileIndexesByLibrary(library.ID)
	// Existing sidecars can change without a directory or book mtime change.
	// Refresh them independently of directory pruning and the newest-file limit.
	if err := s.refreshRecentBookMetadata(library, &job, scope, fileIndexes); err != nil {
		if errors.Is(err, errScanPaused) || errors.Is(err, errScanCancelled) {
			return job, nil
		}
		return job, err
	}
	directoryIndexes, _ := s.store.ListScanDirectoriesByLibrary(library.ID)
	candidates := make([]recentScanCandidate, 0, limit)
	visitedDirs := 0
	visitedFiles := 0
	prunedDirs := 0
	lastProgressAt := time.Now()
	lastProgressCount := 0
	reportProgress := func(force bool, path string) {
		totalVisited := visitedDirs + visitedFiles
		if !force && totalVisited-lastProgressCount < 500 && time.Since(lastProgressAt) < 2*time.Second {
			return
		}
		lastProgressAt = time.Now()
		lastProgressCount = totalVisited
		job.CurrentPath = path
		_ = s.store.UpdateScanJob(job)
		_ = s.store.AddJobEvent(job.ID, "info", fmt.Sprintf("recent scan progress: visited %d dirs, %d files, candidates %d", visitedDirs, visitedFiles, len(candidates)))
	}
	walkErr := s.walkScanScope(library, scope, dirStates, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := s.applyScanControl(&job); err != nil {
			return err
		}
		if walkErr != nil {
			if shouldSkipScanDir(library, path) {
				return filepath.SkipDir
			}
			job.CurrentPath = path
			job.ErrorCount++
			_ = s.recordPathError(library.ID, job.ID, path, classifyWalkError(walkErr), walkErr.Error())
			_ = s.store.AddJobEvent(job.ID, "error", "walk failed: "+path)
			_ = s.store.UpdateScanJob(job)
			return nil
		}
		if entry.IsDir() {
			visitedDirs++
			if path != scope.rootPath {
				if state := dirStates[path]; state != nil && !state.mtime.IsZero() {
					if cached, ok := directoryIndexes[path]; ok && !cached.MTime.IsZero() && cached.MTime.Equal(state.mtime) {
						prunedDirs++
						reportProgress(false, path)
						return filepath.SkipDir
					}
				}
			}
			reportProgress(false, path)
			return nil
		}
		visitedFiles++

		ext := strings.ToLower(filepath.Ext(path))
		kind := classifyFileKind(library, path, ext)
		if kind == "" {
			reportProgress(false, path)
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			job.CurrentPath = path
			job.ErrorCount++
			_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorUnknownIO, err.Error())
			_ = s.store.AddJobEvent(job.ID, "error", "stat failed: "+path)
			_ = s.store.UpdateScanJob(job)
			return nil
		}
		task := scanFileTask{path: path, info: info, ext: ext, kind: kind}
		if !s.scanTaskNeedsIndex(library, task, fileIndexes) {
			reportProgress(false, path)
			return nil
		}
		candidates = append(candidates, recentScanCandidate{task: task, modTime: info.ModTime()})
		job.CurrentPath = path
		_ = s.store.UpdateScanJob(job)
		reportProgress(false, path)
		return nil
	})
	if errors.Is(walkErr, errScanPaused) || errors.Is(walkErr, errScanCancelled) {
		return job, nil
	}
	if walkErr != nil {
		job.Status = "failed"
		job.CurrentPath = ""
		job.FinishedAt = time.Now()
		_ = s.store.UpdateScanJob(job)
		_ = s.store.AddJobEvent(job.ID, "error", "recent scan failed: "+walkErr.Error())
		return job, walkErr
	}
	reportProgress(true, "")
	if prunedDirs > 0 {
		_ = s.store.AddJobEvent(job.ID, "info", fmt.Sprintf("pruned unchanged directories: %d", prunedDirs))
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].modTime.Equal(candidates[j].modTime) {
			return candidates[i].task.path > candidates[j].task.path
		}
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	job.DiscoveredFiles = len(candidates)
	_ = s.store.UpdateScanJob(job)
	_ = s.store.AddJobEvent(job.ID, "info", fmt.Sprintf("recent candidates: %d", len(candidates)))

	updateJob := func(change func(*domain.ScanJob)) {
		change(&job)
		_ = s.store.UpdateScanJob(job)
	}
	for _, candidate := range candidates {
		if err := s.applyScanControl(&job); err != nil {
			return job, nil
		}
		s.processScanTask(library, job.ID, candidate.task, scope, fileIndexes, updateJob)
	}

	if err := s.persistScanDirectories(library.ID, dirStates); err != nil {
		job.Status = "failed"
		job.CurrentPath = ""
		job.FinishedAt = time.Now()
		_ = s.store.UpdateScanJob(job)
		_ = s.store.AddJobEvent(job.ID, "error", "directory cache failed: "+err.Error())
		return job, err
	}
	if err := s.store.DeleteEmptySeries(library.ID); err != nil {
		job.Status = "failed"
		job.CurrentPath = ""
		job.FinishedAt = time.Now()
		_ = s.store.UpdateScanJob(job)
		_ = s.store.AddJobEvent(job.ID, "error", "cleanup failed: "+err.Error())
		return job, err
	}

	job.Status = "completed"
	job.CurrentPath = ""
	job.FinishedAt = time.Now()
	if err := s.store.UpdateScanJob(job); err != nil {
		return job, err
	}
	_ = s.store.AddJobEvent(job.ID, "info", "recent scan completed")
	s.notifyCompleted(library, job)
	return job, nil
}

type scanFileTask struct {
	path string
	info fs.FileInfo
	ext  string
	kind string
}

type recentScanCandidate struct {
	task    scanFileTask
	modTime time.Time
}

func scanScopeForPath(library domain.Library, targetPath string) (scanScope, error) {
	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" {
		return scanScope{rootPath: library.RootPath, fullScan: true}, nil
	}
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(library.RootPath, targetPath)
	}
	targetPath = filepath.Clean(targetPath)
	rootPath := filepath.Clean(library.RootPath)
	relPath, err := filepath.Rel(rootPath, targetPath)
	if err != nil {
		return scanScope{}, fmt.Errorf("resolve target path: %w", err)
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) || filepath.IsAbs(relPath) {
		return scanScope{}, fmt.Errorf("target path is outside library root: %s", targetPath)
	}
	if _, err := os.Stat(targetPath); err != nil {
		return scanScope{}, err
	}
	fullScan := targetPath == rootPath
	return scanScope{rootPath: targetPath, fullScan: fullScan, deferPageIndex: !fullScan}, nil
}

func (s *Scanner) walkScanScope(library domain.Library, scope scanScope, dirStates map[string]*scanDirState, walkFn fs.WalkDirFunc) error {
	return filepath.WalkDir(scope.rootPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr == nil && entry.IsDir() {
			if shouldSkipScanDir(library, path) {
				return filepath.SkipDir
			}
			info, err := entry.Info()
			if err == nil {
				if path != library.RootPath {
					parentPath := filepath.Dir(path)
					parentState := dirStates[parentPath]
					if parentState == nil {
						parentState = &scanDirState{}
						dirStates[parentPath] = parentState
					}
					parentState.hasSubdirs = true
				}
				state := dirStates[path]
				if state == nil {
					state = &scanDirState{}
					dirStates[path] = state
				}
				state.mtime = info.ModTime()
			}
		}
		return walkFn(path, entry, walkErr)
	})
}

func (s *Scanner) persistScanDirectories(libraryID int64, dirStates map[string]*scanDirState) error {
	for path, state := range dirStates {
		if state == nil || state.mtime.IsZero() {
			continue
		}
		if err := s.store.UpsertScanDirectory(libraryID, path, state.mtime, state.hasSubdirs); err != nil {
			return err
		}
	}
	return nil
}

func recentScanTargetLabel(rootPath string, limit int) string {
	return fmt.Sprintf("%s [recent:%d]", filepath.Clean(rootPath), NormalizeRecentScanLimit(limit))
}

func (s *Scanner) runScanJobConcurrent(library domain.Library, job domain.ScanJob, workers int, scope scanScope) (domain.ScanJob, error) {
	_ = s.store.AddJobEvent(job.ID, "info", fmt.Sprintf("scan workers: %d", workers))

	taskCh := make(chan scanFileTask, workers*2)
	var wg sync.WaitGroup
	var mu sync.Mutex
	lastPersist := time.Now()
	stopped := false
	var stopErr error
	dirStates := map[string]*scanDirState{}
	fileIndexes, _ := s.store.ListFileIndexesByLibrary(library.ID)

	updateJob := func(change func(*domain.ScanJob)) {
		mu.Lock()
		defer mu.Unlock()
		change(&job)
		if time.Since(lastPersist) >= 500*time.Millisecond || job.Status != "running" {
			_ = s.store.UpdateScanJob(job)
			lastPersist = time.Now()
		}
	}
	requestStop := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		stopped = true
		if stopErr == nil {
			stopErr = err
		}
	}
	currentStopErr := func() error {
		mu.Lock()
		defer mu.Unlock()
		return stopErr
	}
	checkControl := func() bool {
		mu.Lock()
		defer mu.Unlock()
		if stopped {
			return false
		}
		if err := s.applyScanControl(&job); err != nil {
			stopped = true
			if stopErr == nil {
				stopErr = err
			}
			return false
		}
		return true
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskCh {
				if !checkControl() {
					return
				}
				s.processScanTask(library, job.ID, task, scope, fileIndexes, updateJob)
			}
		}()
	}

	walkErr := s.walkScanScope(library, scope, dirStates, func(path string, entry fs.DirEntry, walkErr error) error {
		if !checkControl() {
			if err := currentStopErr(); err != nil {
				return err
			}
			return errScanCancelled
		}
		if walkErr != nil {
			if shouldSkipScanDir(library, path) {
				return filepath.SkipDir
			}
			_ = s.recordPathError(library.ID, job.ID, path, classifyWalkError(walkErr), walkErr.Error())
			updateJob(func(job *domain.ScanJob) {
				job.CurrentPath = path
				job.ErrorCount++
				_ = s.store.AddJobEvent(job.ID, "error", "walk failed: "+path)
			})
			return nil
		}
		if entry.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		kind := classifyFileKind(library, path, ext)
		if kind == "" {
			return nil
		}
		updateJob(func(job *domain.ScanJob) {
			job.CurrentPath = path
			job.DiscoveredFiles++
		})

		info, err := entry.Info()
		if err != nil {
			_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorUnknownIO, err.Error())
			updateJob(func(job *domain.ScanJob) {
				job.ErrorCount++
				_ = s.store.AddJobEvent(job.ID, "error", "stat failed: "+path)
			})
			return nil
		}
		if info.Size() == 0 {
			_ = s.recordPathError(library.ID, job.ID, path, domain.ErrorEmptyFile, "empty file")
			updateJob(func(job *domain.ScanJob) {
				job.ErrorCount++
				_ = s.store.AddJobEvent(job.ID, "error", "empty file: "+path)
			})
			return nil
		}

		task := scanFileTask{path: path, info: info, ext: ext, kind: kind}
		for {
			if !checkControl() {
				if err := currentStopErr(); err != nil {
					return err
				}
				return errScanCancelled
			}
			select {
			case taskCh <- task:
				return nil
			case <-time.After(100 * time.Millisecond):
			}
		}
	})
	if errors.Is(walkErr, errScanPaused) || errors.Is(walkErr, errScanCancelled) {
		requestStop(walkErr)
	}
	close(taskCh)
	wg.Wait()

	if errors.Is(walkErr, errScanPaused) || errors.Is(walkErr, errScanCancelled) {
		return job, nil
	}
	if walkErr != nil {
		job.Status = "failed"
		job.CurrentPath = ""
		job.FinishedAt = time.Now()
		_ = s.store.UpdateScanJob(job)
		_ = s.store.AddJobEvent(job.ID, "error", "scan failed: "+walkErr.Error())
		return job, walkErr
	}
	if job.Status == "paused" || job.Status == "cancelled" {
		return job, nil
	}
	if err := s.persistScanDirectories(library.ID, dirStates); err != nil {
		job.Status = "failed"
		job.CurrentPath = ""
		job.FinishedAt = time.Now()
		_ = s.store.UpdateScanJob(job)
		_ = s.store.AddJobEvent(job.ID, "error", "directory cache failed: "+err.Error())
		return job, err
	}
	if scope.fullScan {
		if err := s.cleanupSkippedEntries(library, &job); err != nil {
			job.Status = "failed"
			job.CurrentPath = ""
			job.FinishedAt = time.Now()
			_ = s.store.UpdateScanJob(job)
			_ = s.store.AddJobEvent(job.ID, "error", "cleanup failed: "+err.Error())
			return job, err
		}
	} else if err := s.store.DeleteEmptySeries(library.ID); err != nil {
		job.Status = "failed"
		job.CurrentPath = ""
		job.FinishedAt = time.Now()
		_ = s.store.UpdateScanJob(job)
		_ = s.store.AddJobEvent(job.ID, "error", "cleanup failed: "+err.Error())
		return job, err
	}

	job.Status = "completed"
	job.CurrentPath = ""
	job.FinishedAt = time.Now()
	if err := s.store.UpdateScanJob(job); err != nil {
		return job, err
	}
	_ = s.store.AddJobEvent(job.ID, "info", "scan completed")
	s.notifyCompleted(library, job)
	return job, nil
}

func (s *Scanner) processScanTask(library domain.Library, jobID int64, task scanFileTask, scope scanScope, fileIndexes map[string]store.FileIndex, updateJob func(func(*domain.ScanJob))) {
	setCurrent := func(job *domain.ScanJob) {
		job.CurrentPath = task.path
	}
	if task.kind == "game" {
		relPath, err := filepath.Rel(library.RootPath, task.path)
		if err != nil {
			s.recordTaskError(library.ID, jobID, task.path, domain.ErrorUnknownIO, "relative path failed: ", err, updateJob)
			return
		}
		expectedPlatform := inferLibraryGamePlatform(library, task.ext, relPath)
		if s.canSkipGame(library, task.path, task.info, task.ext, expectedPlatform) {
			updateJob(func(job *domain.ScanJob) {
				setCurrent(job)
				job.SkippedFiles++
			})
			return
		}
		if err := s.indexGameFile(library, task.path, task.info, task.ext); err != nil {
			s.recordTaskError(library.ID, jobID, task.path, domain.ErrorUnknownIO, "game metadata failed: ", err, updateJob)
			return
		}
		updateJob(func(job *domain.ScanJob) {
			setCurrent(job)
			job.IndexedFiles++
			_ = s.store.AddJobEvent(jobID, "info", "indexed game: "+task.path)
		})
		return
	}
	if task.kind == "video" {
		if s.store.CanSkipVideo(task.path, task.info.Size(), task.info.ModTime()) {
			if err := s.refreshVideoLocalMetadata(task.path); err != nil {
				s.recordTaskError(library.ID, jobID, task.path, domain.ErrorUnknownIO, "video local metadata refresh failed: ", err, updateJob)
				return
			}
			updateJob(func(job *domain.ScanJob) {
				setCurrent(job)
				job.SkippedFiles++
			})
			return
		}
		if err := s.indexVideoFile(library, task.path, task.info, task.ext); err != nil {
			s.recordTaskError(library.ID, jobID, task.path, domain.ErrorUnknownIO, "video metadata failed: ", err, updateJob)
			return
		}
		updateJob(func(job *domain.ScanJob) {
			setCurrent(job)
			job.IndexedFiles++
			_ = s.store.AddJobEvent(jobID, "info", "indexed video: "+task.path)
		})
		return
	}
	if _, unchanged := s.unchangedMetadataOnlyFile(fileIndexes, task.path, task.info, task.ext); unchanged && scope.bookMetadata != nil {
		if metadata, found, metadataErr := scope.bookMetadata.Lookup(task.path); found || metadataErr != nil {
			if found {
				result, err := s.indexResolvedFileMetadata(library, task.path, task.info, task.ext, importedBookMetadata(task.path, metadata))
				if err != nil {
					metadataErr = err
					found = false
				} else {
					updateJob(func(job *domain.ScanJob) {
						setCurrent(job)
						job.SkippedFiles++
						if result.MetadataUpdated {
							job.MetadataUpdatedFiles++
						}
						if result.Reclassified {
							job.ReclassifiedFiles++
						}
					})
				}
			}
			if metadataErr != nil {
				s.recordBookMetadataWarning(jobID, scope, metadataErr)
				if !found {
					updateJob(func(job *domain.ScanJob) { setCurrent(job); job.ErrorCount++ })
				}
			}
			return
		}
	}
	if task.ext != ".epub" {
		if index, ok := s.unchangedMetadataOnlyFile(fileIndexes, task.path, task.info, task.ext); ok && canSkipUnchangedBook(library, task.path, index, task.ext) {
			if err := s.cleanupStaleNonBookAssets(task.path); err != nil {
				s.recordTaskError(library.ID, jobID, task.path, domain.ErrorUnknownIO, "stale asset cleanup failed: ", err, updateJob)
				return
			}
			updateJob(func(job *domain.ScanJob) {
				setCurrent(job)
				job.SkippedFiles++
			})
			return
		}
	}

	if index, ok := s.unchangedFileIndex(fileIndexes, task.path, task.info, task.ext); ok {
		if canSkipUnchangedBook(library, task.path, index, task.ext) {
			if err := s.cleanupStaleNonBookAssets(task.path); err != nil {
				s.recordTaskError(library.ID, jobID, task.path, domain.ErrorUnknownIO, "stale asset cleanup failed: ", err, updateJob)
				return
			}
			updateJob(func(job *domain.ScanJob) {
				setCurrent(job)
				job.SkippedFiles++
			})
			return
		}
		result, err := s.indexFileMetadata(library, task.path, task.info, task.ext, scope.bookMetadata)
		if err != nil {
			s.recordTaskError(library.ID, jobID, task.path, domain.ErrorUnknownIO, "metadata update failed: ", err, updateJob)
			return
		}
		updateJob(func(job *domain.ScanJob) {
			setCurrent(job)
			if result.MetadataUpdated {
				job.MetadataUpdatedFiles++
			}
			if result.Reclassified {
				job.ReclassifiedFiles++
			}
			job.SkippedFiles++
		})
		return
	}

	if err := s.store.DeleteGameByPath(task.path); err != nil {
		s.recordTaskError(library.ID, jobID, task.path, domain.ErrorUnknownIO, "game cleanup failed: ", err, updateJob)
		return
	}
	if err := s.store.DeleteVideoByPath(task.path); err != nil {
		s.recordTaskError(library.ID, jobID, task.path, domain.ErrorUnknownIO, "video cleanup failed: ", err, updateJob)
		return
	}

	result, err := s.indexScanFile(library, jobID, task.path, task.info, task.ext, scope)
	if err != nil {
		s.recordTaskError(library.ID, jobID, task.path, domain.ErrorArchiveOpenFailed, "archive failed: ", err, updateJob)
		return
	}
	updateJob(func(job *domain.ScanJob) {
		setCurrent(job)
		job.IndexedFiles++
		if result.MetadataUpdated {
			job.MetadataUpdatedFiles++
		}
		if result.Reclassified {
			job.ReclassifiedFiles++
		}
		if !scope.deferPageIndex {
			_ = s.store.AddJobEvent(jobID, "info", "indexed: "+task.path)
		}
	})
}

func (s *Scanner) scanTaskNeedsIndex(library domain.Library, task scanFileTask, fileIndexes map[string]store.FileIndex) bool {
	if task.info.Size() == 0 {
		return true
	}
	switch task.kind {
	case "game":
		relPath, err := filepath.Rel(library.RootPath, task.path)
		if err != nil {
			return true
		}
		return !s.canSkipGame(library, task.path, task.info, task.ext, inferLibraryGamePlatform(library, task.ext, relPath))
	case "video":
		return !s.store.CanSkipVideo(task.path, task.info.Size(), task.info.ModTime())
	default:
		if task.ext != ".epub" {
			if index, ok := s.unchangedMetadataOnlyFile(fileIndexes, task.path, task.info, task.ext); ok && canSkipUnchangedBook(library, task.path, index, task.ext) {
				return false
			}
		}
		if index, ok := s.unchangedFileIndex(fileIndexes, task.path, task.info, task.ext); ok && canSkipUnchangedBook(library, task.path, index, task.ext) {
			return false
		}
		return true
	}
}

func (s *Scanner) canSkipGame(library domain.Library, path string, info fs.FileInfo, ext string, platform string) bool {
	if ext == ".zip" {
		archiveROM, detected, err := inspectConsoleROMZIP(path, false)
		if err != nil {
			return false
		}
		if detected {
			if !archiveROM.definitive {
				return false
			}
			game, err := s.store.GameByPath(path)
			if err != nil {
				return false
			}
			romSetName, emulatorHint := consolePlatformMetadata(archiveROM.platform)
			files, err := s.store.GameFiles(game.ID)
			return err == nil &&
				game.Size == archiveROM.size &&
				game.MTime.Equal(info.ModTime()) &&
				game.Platform == archiveROM.platform &&
				game.ROMSetName == romSetName &&
				game.Format == archiveROM.format &&
				game.EmulatorHint == emulatorHint &&
				strings.EqualFold(game.CatalogRole, launchcatalog.RoleGame) &&
				game.CRC32 != "" &&
				game.SHA1 != "" &&
				len(files) == 1 &&
				files[0].Role == "entry" &&
				files[0].Name == archiveROM.name &&
				files[0].Size == archiveROM.size &&
				files[0].SHA1 != ""
		}
	}
	if platform == "3ds" {
		game, err := s.store.GameByPath(path)
		if err != nil || !game.MTime.Equal(info.ModTime()) || game.Platform != "3ds" ||
			game.ROMSetName != "Nintendo 3DS" || game.EmulatorHint != "spatialemu-3ds-companion" ||
			game.CRC32 == "" || game.SHA1 == "" {
			return false
		}
		files, err := s.store.GameFiles(game.ID)
		if err != nil || len(files) != 1 || files[0].Role != "entry" || files[0].Position != 0 ||
			files[0].FilePath != path || files[0].Size != game.Size || files[0].SHA1 == "" {
			return false
		}
		if ext == ".zip" {
			return game.Format == "zip" && strings.EqualFold(game.CatalogRole, launchcatalog.RoleGame)
		}
		expectedFormat := strings.TrimPrefix(ext, ".")
		expectedRole := launchcatalog.RoleGame
		if ext == ".cia" {
			expectedRole = launchcatalog.RoleNeedsCuration
			if err := validateThreeDSCIAStructure(path, uint64(info.Size())); err != nil {
				return false
			}
		}
		return game.Size == info.Size() && game.Format == expectedFormat &&
			strings.EqualFold(game.CatalogRole, expectedRole) && files[0].Name == filepath.Base(path)
	}
	if platform == "dos" {
		catalog, _ := s.dosCatalogForPath(library.RootPath, path)
		return s.store.CanSkipDOSGame(path, info.Size(), info.ModTime(), catalog.revision)
	}
	if platform == "naomi2" {
		return s.canSkipNaomi2Game(path, info)
	}
	if ext == ".zip" && (platform == "naomi" || isProjectJusticeArchiveName(path)) {
		identity, detected, err := inspectNaomiROMZIP(path)
		if err != nil {
			return false
		}
		if detected {
			game, err := s.store.GameByPath(path)
			if err != nil {
				return false
			}
			files, err := s.store.GameFiles(game.ID)
			canonicalName := identity.romSetName + ".zip"
			return err == nil && game.Size == info.Size() && game.MTime.Equal(info.ModTime()) &&
				game.Title == identity.title && game.Platform == "naomi" && game.ROMSetName == identity.romSetName &&
				game.Format == "zip" && game.EmulatorHint == "flycast" && strings.EqualFold(game.CatalogRole, launchcatalog.RoleGame) &&
				game.CRC32 != "" && game.SHA1 != "" && len(files) == 1 && files[0].Role == "entry" &&
				files[0].Name == canonicalName && files[0].FilePath == path && files[0].Size == info.Size() && files[0].SHA1 != ""
		}
	}
	if platform == "n64" && ext == ".zip" {
		game, err := s.store.GameByPath(path)
		if err != nil || !game.MTime.Equal(info.ModTime()) || game.Platform != "n64" || game.EmulatorHint != "mupen64plus" || game.CRC32 == "" || game.SHA1 == "" || !isN64RawExt("."+game.Format) {
			return false
		}
		files, err := s.store.GameFiles(game.ID)
		return err == nil && len(files) == 1 && files[0].Role == "entry" && files[0].Size == game.Size
	}
	if platform == "pc98" {
		if pkg, matched := pc98MixedCDPackageForPath(path); matched && sameFilePath(path, pkg.entryPath) {
			game, err := s.store.GameByPath(path)
			if err != nil {
				return false
			}
			storedFiles, err := s.store.GameFiles(game.ID)
			if err != nil || !samePC98MixedCDPackageFiles(pkg.files, storedFiles) {
				return false
			}
		}
		fonts, err := pc98PackageFontFiles(path)
		if err != nil {
			return false
		}
		storedFonts, err := s.store.PC98SourceSupportFiles(path)
		if err != nil {
			return false
		}
		return s.store.CanSkipPC98Source(path, info.Size(), info.ModTime()) && samePC98SupportFiles(path, fonts, storedFonts)
	}
	if romSetName, emulatorHint, _, ok := canonicalArcadeCatalogMetadata(platform, path, ext); ok {
		game, err := s.store.GameByPath(path)
		if err != nil {
			return false
		}
		expected := launchcatalog.CanonicalizeAuditedGame(domain.GameAsset{
			FilePath: path, Size: info.Size(), SHA1: game.SHA1, Platform: platform,
			ROMSetName: romSetName, EmulatorHint: emulatorHint, CatalogRole: game.CatalogRole,
		})
		expectedRole := launchcatalog.CatalogRole(expected, nil)
		return game.Size == info.Size() &&
			game.MTime.Equal(info.ModTime()) &&
			game.Platform == expected.Platform &&
			game.ROMSetName == expected.ROMSetName &&
			game.EmulatorHint == expected.EmulatorHint &&
			strings.EqualFold(game.CatalogRole, expectedRole) &&
			game.CRC32 != "" &&
			game.SHA1 != ""
	}
	if platform == "model3" {
		game, err := s.store.GameByPath(path)
		if err != nil || game.ROMSetName != gameROMSetStem(path, ext) {
			return false
		}
	}
	if !isMultiFileGameDescriptor(ext) {
		return s.store.CanSkipGame(path, info.Size(), info.ModTime(), platform)
	}
	files, totalSize, err := indexedGameFiles(path, info, ext)
	if err != nil {
		return false
	}
	if platform == "pc-fx" && ext == ".cue" {
		if discPaths := pcfxDiscSetPaths(library.RootPath, path); len(discPaths) > 1 {
			files, totalSize, err = indexedVirtualM3UFiles(path, discPaths)
			if err != nil {
				return false
			}
		}
	}
	return s.store.CanSkipGameSet(path, totalSize, info.ModTime(), platform, files)
}

func (s *Scanner) cleanupSkippedEntries(library domain.Library, job *domain.ScanJob) error {
	deleted, err := s.store.DeleteSkippedDirectoryEntries(library.ID, skippedScanDirNames())
	if err != nil {
		return err
	}
	if deleted > 0 {
		_ = s.store.AddJobEvent(job.ID, "info", fmt.Sprintf("removed %d skipped-directory entries", deleted))
	}
	ignored, err := s.store.DeleteIgnoredAppleDoubleEntries(library.ID)
	if err != nil {
		return err
	}
	if ignored > 0 {
		_ = s.store.AddJobEvent(job.ID, "info", fmt.Sprintf("removed %d AppleDouble entries", ignored))
	}
	return s.store.DeleteEmptySeries(library.ID)
}

func (s *Scanner) cleanupStaleNonBookAssets(path string) error {
	if err := s.store.DeleteGameByPath(path); err != nil {
		return err
	}
	return s.store.DeleteVideoByPath(path)
}

func (s *Scanner) recordTaskError(libraryID int64, jobID int64, path string, code domain.ErrorCode, eventPrefix string, err error, updateJob func(func(*domain.ScanJob))) {
	_ = s.recordPathError(libraryID, jobID, path, code, err.Error())
	updateJob(func(job *domain.ScanJob) {
		job.CurrentPath = path
		job.ErrorCount++
		_ = s.store.AddJobEvent(jobID, "error", eventPrefix+path)
	})
}

func scanWorkerCount() int {
	value := strings.TrimSpace(os.Getenv("FOLIOSPACE_SCAN_WORKERS"))
	return NormalizeWorkerCount(value)
}

func NormalizeWorkerCount(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 1
	}
	workers, err := strconv.Atoi(value)
	if err != nil || workers < 1 {
		return 1
	}
	if workers > 8 {
		return 8
	}
	return workers
}

func NormalizeRecentScanLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func (s *Scanner) applyScanControl(job *domain.ScanJob) error {
	latest, err := s.store.ScanJobByID(job.ID)
	if err != nil {
		return nil
	}
	switch latest.Status {
	case "pause_requested":
		job.Status = "paused"
		job.CurrentPath = ""
		job.FinishedAt = time.Now()
		_ = s.store.UpdateScanJob(*job)
		_ = s.store.AddJobEvent(job.ID, "info", "scan paused")
		return errScanPaused
	case "cancel_requested":
		job.Status = "cancelled"
		job.CurrentPath = ""
		job.FinishedAt = time.Now()
		_ = s.store.UpdateScanJob(*job)
		_ = s.store.AddJobEvent(job.ID, "info", "scan cancelled")
		return errScanCancelled
	default:
		return nil
	}
}

func classifyFileKind(library domain.Library, path string, ext string) string {
	if shouldSkipScanFile(path) || isPegasusIgnoredFile(path) {
		return ""
	}
	if library.AssetType == "game" {
		if relPath, err := filepath.Rel(library.RootPath, path); err == nil && inferLibraryGamePlatform(library, ext, relPath) == "dos" {
			if ext == ".zip" || ext == ".dosz" || ext == ".exe" || ext == ".com" || ext == ".bat" {
				return "game"
			}
			return ""
		}
		if isNaomi2Path(library, path) {
			if ext != ".zip" {
				return ""
			}
			if isNaomi2BIOS(library, path) || isNaomi2GameZIP(library, path) {
				return "game"
			}
			return ""
		}
		relPath, relErr := filepath.Rel(library.RootPath, path)
		if relErr == nil && hasPC98PathContext(library, relPath) {
			if hasPC98NegativePathSignal(relPath) || isPC98ExcludedFile(path) {
				return ""
			}
			if pkg, matched := pc98MixedCDPackageForPath(path); matched {
				if sameFilePath(path, pkg.entryPath) {
					return "game"
				}
				return ""
			}
			if isDiscTrackDependency(path) {
				return ""
			}
			if isPC98MediaExt(ext) || ext == ".zip" {
				return "game"
			}
			return ""
		}
		if isDiscTrackDependency(path) {
			return ""
		}
		relPath, err := filepath.Rel(library.RootPath, path)
		if err == nil && inferLibraryGamePlatform(library, ext, relPath) == "pc-fx" {
			name := strings.ToLower(filepath.Base(path))
			if name == "pcfx.rom" || ext == ".bin" || ext == ".img" || ext == ".iso" || ext == ".sub" || isPCFXSecondaryDiscPath(path) {
				return ""
			}
		}
		if err == nil {
			platform := inferLibraryGamePlatform(library, ext, relPath)
			if isDedicatedDiscPlatform(platform) && !isDedicatedDiscPlatformFormat(platform, ext) {
				return ""
			}
		}
		if isGameExt(ext) || isGamePackageExt(ext) {
			return "game"
		}
		return ""
	}
	if isDiscTrackDependency(path) {
		return ""
	}
	if library.AssetType == "video" {
		if isVideoExt(ext) {
			return "video"
		}
		return ""
	}
	if ext == ".zip" {
		if relPath, err := filepath.Rel(library.RootPath, path); err == nil && inferLibraryGamePlatform(library, ext, relPath) == "n64" {
			return "game"
		}
	}
	if isBookExt(ext) {
		return "book"
	}
	if isGameExt(ext) {
		return "game"
	}
	if isVideoExt(ext) {
		return "video"
	}
	return ""
}

func isDedicatedDiscPlatform(platform string) bool {
	switch platform {
	case "nds", "3ds", "3do", "psp", "ngc", "ps2", "konami-python1":
		return true
	default:
		return false
	}
}

func isDedicatedDiscPlatformFormat(platform, ext string) bool {
	switch platform {
	case "nds":
		return ext == ".nds" || ext == ".zip"
	case "3ds":
		return ext == ".3ds" || ext == ".cci" || ext == ".cxi" || ext == ".cia" || ext == ".zip"
	case "3do":
		return ext == ".cue" || ext == ".iso" || ext == ".chd"
	case "psp":
		return ext == ".iso" || ext == ".cso"
	case "ngc":
		return ext == ".iso" || ext == ".gcm" || ext == ".rvz"
	case "ps2":
		return ext == ".iso" || ext == ".chd"
	case "konami-python1":
		return ext == ".py1"
	default:
		return false
	}
}

func isBookExt(ext string) bool {
	return ext == ".cbz" || ext == ".zip" || ext == ".epub" || ext == ".7z" || ext == ".pdf"
}

func isGamePackageExt(ext string) bool {
	return ext == ".zip" || ext == ".7z"
}

func isGameExt(ext string) bool {
	switch ext {
	case ".nes", ".sfc", ".smc", ".vb", ".vboy", ".gba", ".gb", ".gbc", ".nds", ".3ds", ".cci", ".cxi", ".cia", ".z64", ".v64", ".n64", ".gdi", ".cdi", ".chd", ".iso", ".bin", ".cue", ".ccd", ".toc", ".m3u", ".img", ".pbp", ".cso", ".gcm", ".rvz", ".py1":
		return true
	default:
		return false
	}
}

func isVideoExt(ext string) bool {
	switch ext {
	case ".mp4", ".m4v", ".mov", ".mkv", ".avi", ".webm":
		return true
	default:
		return false
	}
}

func (s *Scanner) unchangedFileIndex(fileIndexes map[string]store.FileIndex, path string, info fs.FileInfo, ext string) (store.FileIndex, bool) {
	index, ok := fileIndexes[path]
	if !ok {
		var err error
		index, err = s.store.FileIndexByPath(path)
		if err != nil {
			return store.FileIndex{}, false
		}
	}
	ok = index.File.Size == info.Size() &&
		index.File.Ext == ext &&
		index.File.MTime.Equal(info.ModTime()) &&
		index.Analyzed &&
		index.PageCount > 0
	return index, ok
}

func canSkipUnchangedBook(library domain.Library, path string, index store.FileIndex, ext string) bool {
	if ext == ".7z" {
		return false
	}
	if ext != ".epub" {
		relPath, err := filepath.Rel(library.RootPath, path)
		if err != nil {
			return false
		}
		if dir := filepath.Dir(relPath); dir == "." || dir == "/" {
			seriesTitle, _ := seriesIdentityForRelPath(library.RootPath, relPath)
			return index.Book.CollectionTitle == seriesTitle
		}
		return true
	}
	return strings.TrimSpace(index.Book.Creator) != "" || strings.TrimSpace(index.Book.Description) != ""
}

func (s *Scanner) indexFile(library domain.Library, jobID int64, path string, info fs.FileInfo, ext string, resolver *bookmeta.Resolver) (indexedBookResult, error) {
	result, err := s.indexFileMetadata(library, path, info, ext, resolver)
	if err != nil {
		return result, err
	}

	pages, err := listBookPages(path, ext)
	if err != nil {
		return result, err
	}
	if err := s.store.ReplacePages(result.Book.ID, pages); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Scanner) indexScanFile(library domain.Library, jobID int64, path string, info fs.FileInfo, ext string, scope scanScope) (indexedBookResult, error) {
	if scope.bookMetadata != nil {
		if _, _, err := scope.bookMetadata.Lookup(path); err != nil {
			s.recordBookMetadataWarning(jobID, scope, err)
		}
	}
	if scope.deferPageIndex {
		if ext != ".epub" {
			return s.indexBasicFileMetadata(library, path, info, ext, scope.bookMetadata)
		}
		return s.indexFileMetadata(library, path, info, ext, scope.bookMetadata)
	}
	return s.indexFile(library, jobID, path, info, ext, scope.bookMetadata)
}

func (s *Scanner) unchangedMetadataOnlyFile(fileIndexes map[string]store.FileIndex, path string, info fs.FileInfo, ext string) (store.FileIndex, bool) {
	index, ok := fileIndexes[path]
	if !ok {
		var err error
		index, err = s.store.FileIndexByPath(path)
		if err != nil {
			return store.FileIndex{}, false
		}
	}
	ok = index.File.Size == info.Size() &&
		index.File.Ext == ext &&
		index.File.MTime.Equal(info.ModTime())
	return index, ok
}

func (s *Scanner) indexGameFile(library domain.Library, path string, info fs.FileInfo, ext string) error {
	relPath, err := filepath.Rel(library.RootPath, path)
	if err != nil {
		return fmt.Errorf("relative path: %w", err)
	}
	checksums := checksumPair{}
	title := gameTitle(path)
	platform := inferLibraryGamePlatform(library, ext, relPath)
	if platform == "dos" {
		return s.indexDOSGameFile(library, path, info, ext, relPath)
	}
	if platform == "naomi2" {
		return s.indexNaomi2GameFile(library, path, info, relPath)
	}
	naomiIdentity := naomiROMSetIdentity{}
	naomiROMDetected := false
	if ext == ".zip" && (platform == "naomi" || isProjectJusticeArchiveName(path)) {
		naomiIdentity, naomiROMDetected, err = inspectNaomiROMZIP(path)
		if err != nil {
			_ = s.store.UpdateGameCatalogRoleByPath(path, launchcatalog.RoleNeedsCuration)
			return err
		}
	}
	consoleROM := consoleROMInfo{}
	consoleROMDetected := false
	if ext == ".zip" {
		consoleROM, consoleROMDetected, err = inspectConsoleROMZIP(path, true)
		if err != nil {
			return err
		}
		if consoleROMDetected {
			platform = consoleROM.platform
		}
	}
	romSetName := inferROMSetName(relPath)
	emulatorHint := platform
	format := strings.TrimPrefix(ext, ".")
	gameFiles := []domain.GameFile{{Name: filepath.Base(path), FilePath: path, Size: info.Size(), MTime: info.ModTime(), Role: "entry", Position: 0}}
	totalSize := info.Size()
	compatibility := "unknown"
	catalogRole := "game"
	bootabilityChecked := false
	var pc98Package *pc98MixedCDPackage
	if consoleROMDetected {
		romSetName, emulatorHint = consolePlatformMetadata(platform)
		if consoleROM.definitive {
			title = gameTitle(consoleROM.name)
			format = consoleROM.format
			totalSize = consoleROM.size
			checksums = consoleROM.checksums
			compatibility = "untested"
			gameFiles = []domain.GameFile{{Name: consoleROM.name, FilePath: path, Size: consoleROM.size, MTime: info.ModTime(), Role: "entry", Position: 0}}
			relPath = filepath.Join(filepath.Dir(relPath), consoleROM.name)
		} else {
			catalogRole = launchcatalog.RoleNeedsCuration
		}
	}
	if naomiROMDetected {
		title = naomiIdentity.title
		platform = "naomi"
		romSetName = naomiIdentity.romSetName
		emulatorHint = "flycast"
		format = "zip"
		compatibility = "playable"
		catalogRole = launchcatalog.RoleGame
		canonicalName := naomiIdentity.romSetName + ".zip"
		gameFiles = []domain.GameFile{{Name: canonicalName, FilePath: path, Size: info.Size(), MTime: info.ModTime(), Role: "entry", Position: 0}}
		relPath = filepath.Join(filepath.Dir(relPath), canonicalName)
	}
	if canonicalROMSetName, canonicalEmulatorHint, canonicalCatalogRole, ok := canonicalArcadeCatalogMetadata(platform, path, ext); ok {
		romSetName = canonicalROMSetName
		emulatorHint = canonicalEmulatorHint
		catalogRole = canonicalCatalogRole
	}
	if consoleROMDetected && consoleROM.definitive {
		// The archive inspection already validated and checksummed the inner ROM.
	} else if platform == "3ds" {
		image, inspectErr := inspectThreeDSImage(path, info, ext)
		if inspectErr != nil {
			_ = s.store.DeleteGameByPath(path)
			return inspectErr
		}
		title = gameTitle(image.name)
		romSetName = "Nintendo 3DS"
		emulatorHint = "spatialemu-3ds-companion"
		format = image.format
		totalSize = image.size
		checksums = image.checksums
		compatibility = "untested"
		gameFiles = []domain.GameFile{{
			Name: image.name, FilePath: path, Size: image.size, MTime: info.ModTime(), Role: "entry", Position: 0,
		}}
		if image.install {
			catalogRole = launchcatalog.RoleNeedsCuration
		}
		if ext == ".zip" {
			relPath = filepath.Join(filepath.Dir(relPath), image.name)
		}
	} else if platform == "n64" {
		rom, inspectErr := inspectN64ROM(path, info, ext)
		if inspectErr != nil {
			_ = s.store.DeleteGameByPath(path)
			return inspectErr
		}
		title = gameTitle(rom.name)
		romSetName = "Nintendo 64"
		emulatorHint = "mupen64plus"
		format = rom.format
		totalSize = rom.size
		checksums = rom.checksums
		compatibility = "untested"
		gameFiles = []domain.GameFile{{Name: rom.name, FilePath: path, Size: rom.size, MTime: info.ModTime(), Role: "entry", Position: 0}}
		if ext == ".zip" {
			relPath = filepath.Join(filepath.Dir(relPath), rom.name)
		}
	} else if platform == "pc98" {
		media, inspectErr := inspectPC98Media(path, info, ext)
		if inspectErr != nil {
			_ = s.store.DeleteGameByPath(path)
			return inspectErr
		}
		title = gameTitle(media.name)
		romSetName = "PC-98"
		emulatorHint = "np2kai"
		format = media.format
		totalSize = media.size
		checksums = media.checksums
		compatibility = media.compatibility
		bootabilityChecked = media.bootabilityChecked
		gameFiles = []domain.GameFile{{Name: media.name, FilePath: path, Size: media.size, MTime: info.ModTime(), Role: "entry", Position: 0}}
		if pkg, matched := pc98MixedCDPackageForPath(path); matched && sameFilePath(path, pkg.entryPath) {
			pc98Package = &pkg
			gameFiles = pkg.files
			totalSize = gameFilesSize(gameFiles)
		}
		if ext == ".zip" {
			relPath = filepath.Join(filepath.Dir(relPath), media.name)
		}
	} else {
		checksums, err = fileChecksums(path)
		if err != nil {
			return err
		}
	}
	pegasus, hasPegasus := pegasusEntryForGame(library.RootPath, path)
	if hasPegasus && strings.TrimSpace(pegasus.Name) != "" && !naomiROMDetected {
		title = strings.TrimSpace(pegasus.Name)
	}
	if platform == "model2" {
		shortName := strings.ToLower(strings.TrimSuffix(filepath.Base(path), ext))
		romSetName = shortName
		emulatorHint = "model2"
		compatibility = model2Compatibility(shortName)
		if friendlyTitle := model2FriendlyTitle(shortName); friendlyTitle != "" {
			title = friendlyTitle
		}
		if shortName == "segabill" {
			catalogRole = "dependency"
			compatibility = "unknown"
		}
	} else if platform == "virtualboy" {
		romSetName = "Virtual Boy"
		emulatorHint = "virtualfriend"
		compatibility = "untested"
	} else if platform == "nds" {
		if consoleROMDetected && consoleROM.definitive {
			title = ndsGameTitle(consoleROM.name)
		} else {
			title = ndsGameTitle(path)
		}
		romSetName = "Nintendo DS"
		emulatorHint = "melonds-ds"
		compatibility = "untested"
	} else if platform == "3ds" {
		romSetName = "Nintendo 3DS"
		emulatorHint = "spatialemu-3ds-companion"
		compatibility = "untested"
	} else if platform == "3do" {
		if !hasPegasus {
			title = gameTitlePreservingDiscLabel(path)
		}
		romSetName = "3DO"
		emulatorHint = "opera"
		compatibility = "untested"
	} else if platform == "psp" {
		romSetName = "PSP"
		emulatorHint = "ppsspp"
	} else if platform == "ngc" {
		romSetName = "NGC"
		if hasTriforcePathContext(library, relPath) {
			romSetName = "Triforce"
		}
		emulatorHint = "dolphin"
	} else if platform == "ps2" {
		romSetName = "PS2"
		emulatorHint = "pcsx2"
	} else if platform == "konami-python1" {
		descriptor, descriptorErr := readKonamiPython1Descriptor(path)
		if descriptorErr != nil {
			return descriptorErr
		}
		title = descriptor.Name
		romSetName = "KONAMI-PYTHON1"
		emulatorHint = "pcsx2-reliquary"
		compatibility = "untested"
	} else if platform == "dreamcast" {
		romSetName = "DC"
	} else if platform == "saturn" {
		romSetName = "SS"
	} else if platform == "pc-fx" {
		romSetName = "PC-FX"
		emulatorHint = "pcfx"
		if !hasPegasus {
			title = pcfxDirectoryTitle(library.RootPath, path)
		}
	}
	if platform != "3ds" && platform != "n64" && platform != "pc98" && !(consoleROMDetected && consoleROM.definitive) && !naomiROMDetected {
		gameFiles, totalSize, err = indexedGameFiles(path, info, ext)
		if err != nil {
			return err
		}
	}
	if platform == "pc-fx" && ext == ".cue" {
		if discPaths := pcfxDiscSetPaths(library.RootPath, path); len(discPaths) > 1 {
			gameFiles, totalSize, err = indexedVirtualM3UFiles(path, discPaths)
			if err != nil {
				return err
			}
			format = "m3u"
		}
	}
	if platform != "pc98" || pc98Package != nil {
		if err := populateGameFileSHA1(gameFiles, path, checksums.sha1, format); err != nil {
			return err
		}
	}
	regionSource := path
	if consoleROMDetected && consoleROM.definitive {
		regionSource = consoleROM.name
	}
	gameAsset := domain.GameAsset{
		LibraryID:     library.ID,
		Title:         title,
		Platform:      platform,
		ROMSetName:    romSetName,
		Region:        inferRegion(regionSource),
		Format:        format,
		FilePath:      path,
		RelPath:       filepath.ToSlash(relPath),
		Size:          totalSize,
		MTime:         info.ModTime(),
		CRC32:         checksums.crc32,
		SHA1:          checksums.sha1,
		EmulatorHint:  emulatorHint,
		Compatibility: compatibility,
		CatalogRole:   catalogRole,
	}
	gameAsset = launchcatalog.CanonicalizeAuditedGame(gameAsset)
	gameAsset.CatalogRole = launchcatalog.CatalogRole(gameAsset, nil)
	if platform == "model2" {
		gameAsset.Region = model2Region(strings.ToLower(strings.TrimSuffix(filepath.Base(path), ext)))
	}
	var game domain.GameAsset
	if platform == "pc98" {
		if pc98Package != nil {
			for _, stalePath := range pc98Package.staleSourcePaths {
				if err := s.store.DeleteGameByPath(stalePath); err != nil {
					return err
				}
			}
		}
		sourceTitle, groupKey, diskOrder := pc98SourceIdentity(library.RootPath, path, mediaName(gameFiles, path))
		if isBlockedPC98SpecialDiskTitle(sourceTitle) {
			_ = s.store.DeleteGameByPath(path)
			return fmt.Errorf("PC-98 add-on %q requires a bootable main-game installation and cannot be published independently", sourceTitle)
		}
		gameAsset.Title = sourceTitle
		game, err = s.store.UpsertPC98GameSource(gameAsset, domain.GameSource{
			LibraryID: library.ID, Title: sourceTitle, FilePath: path, RelPath: gameAsset.RelPath,
			EntryName: mediaName(gameFiles, path), Format: gameAsset.Format, Size: gameFiles[0].Size, ContainerSize: info.Size(),
			MTime: info.ModTime(), CRC32: checksums.crc32, SHA1: checksums.sha1, GroupKey: groupKey, DiskOrder: diskOrder,
			Compatibility: compatibility, BootabilityChecked: bootabilityChecked,
		})
	} else {
		game, err = s.store.UpsertGame(gameAsset)
	}
	if err != nil {
		return err
	}
	if platform == "pc98" {
		if pc98Package != nil {
			if err := s.store.ReplaceGameFiles(game.ID, gameFiles); err != nil {
				return err
			}
		}
		if err := s.syncPC98SupportFiles(game.ID); err != nil {
			return err
		}
	}
	if platform != "pc98" {
		if err := s.store.ReplaceGameFiles(game.ID, gameFiles); err != nil {
			return err
		}
	}
	if len(gameFiles) > 1 {
		dependencyPaths := make([]string, 0, len(gameFiles)-1)
		for _, file := range gameFiles {
			if file.Role == "dependency" {
				dependencyPaths = append(dependencyPaths, file.FilePath)
			}
		}
		if err := s.store.DeleteGamesByPaths(dependencyPaths, game.ID); err != nil {
			return err
		}
	}
	if err := s.applyPegasusMetadata(game.ID, pegasus, hasPegasus); err != nil {
		return err
	}
	return s.applyGamelistMetadata(library, path, filepath.ToSlash(relPath), game.ID)
}

func isNaomi2Library(library domain.Library) bool {
	return isNaomi2RootName(library.Name) || isNaomi2RootName(filepath.Base(filepath.Clean(library.RootPath)))
}

func isNaomi2RootName(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "naomi2", "naomi 2", "sega naomi 2":
		return true
	default:
		return false
	}
}

// naomi2RootForPath makes the catalog rules work both for a dedicated NAOMI 2
// library and for a NAOMI 2 directory nested below a shared GameROMS root.
func naomi2RootForPath(library domain.Library, path string) (string, bool) {
	libraryRoot := filepath.Clean(library.RootPath)
	for dir := filepath.Clean(filepath.Dir(path)); ; {
		if isNaomi2RootName(filepath.Base(dir)) {
			return dir, true
		}
		if dir == libraryRoot || filepath.Dir(dir) == dir {
			break
		}
		dir = filepath.Dir(dir)
	}
	if isNaomi2Library(library) {
		return libraryRoot, true
	}
	return "", false
}

func isNaomi2Path(library domain.Library, path string) bool {
	_, ok := naomi2RootForPath(library, path)
	return ok
}

func isNaomi2BIOS(library domain.Library, path string) bool {
	root, ok := naomi2RootForPath(library, path)
	return ok && strings.EqualFold(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), "naomi2") &&
		filepath.Clean(filepath.Dir(path)) == root
}

func isNaomi2GameZIP(library domain.Library, path string) bool {
	root, ok := naomi2RootForPath(library, path)
	if !ok {
		return false
	}
	shortName := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if _, ok := naomi2catalog.Lookup(shortName); !ok {
		return false
	}
	dir := filepath.Clean(filepath.Dir(path))
	return strings.EqualFold(filepath.Base(dir), shortName) || dir == root
}

func (s *Scanner) indexNaomi2GameFile(library domain.Library, path string, info fs.FileInfo, relPath string) error {
	if isNaomi2BIOS(library, path) {
		checksums, err := fileChecksums(path)
		if err != nil {
			return err
		}
		game, err := s.store.UpsertGame(domain.GameAsset{
			LibraryID: library.ID, Title: "NAOMI 2 BIOS", Platform: "naomi2", ROMSetName: "naomi2", Format: "zip",
			FilePath: path, RelPath: filepath.ToSlash(relPath), Size: info.Size(), MTime: info.ModTime(), CRC32: checksums.crc32, SHA1: checksums.sha1,
			EmulatorHint: "flycast", Compatibility: "unknown", CatalogRole: "dependency",
		})
		if err != nil {
			return err
		}
		return s.store.ReplaceGameFiles(game.ID, []domain.GameFile{{Name: filepath.Base(path), FilePath: path, Size: info.Size(), MTime: info.ModTime(), SHA1: checksums.sha1, Role: "entry", Position: 0}})
	}

	shortName := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	entry, ok := naomi2catalog.Lookup(shortName)
	if !ok || !isNaomi2GameZIP(library, path) {
		return fmt.Errorf("unrecognized NAOMI 2 package %s", filepath.Base(path))
	}
	if err := validateNaomi2DescriptorZIP(path, entry); err != nil {
		_ = s.store.DeleteGameByPath(path)
		return err
	}
	checksums, err := fileChecksums(path)
	if err != nil {
		return err
	}
	files, totalSize, companionPath, err := naomi2IndexedGameFiles(path, info, shortName, entry)
	if err != nil {
		_ = s.store.DeleteGameByPath(path)
		if companionPath != "" && entry.GDROM != "" {
			_ = s.store.DeleteGameByPath(companionPath)
		}
		return err
	}
	if err := populateGameFileSHA1(files, path, checksums.sha1, "zip"); err != nil {
		return err
	}
	game, err := s.store.UpsertGame(domain.GameAsset{
		LibraryID: library.ID, Title: entry.Title, Platform: "naomi2", ROMSetName: shortName, Region: entry.Region, Format: "zip",
		FilePath: path, RelPath: filepath.ToSlash(relPath), Size: totalSize, MTime: info.ModTime(), CRC32: checksums.crc32, SHA1: checksums.sha1,
		EmulatorHint: "flycast", Compatibility: naomi2Compatibility(entry), CatalogRole: "game",
	})
	if err != nil {
		return err
	}
	if err := s.store.ReplaceGameFiles(game.ID, files); err != nil {
		return err
	}
	if companionPath == "" || entry.GDROM == "" {
		return nil
	}
	companionInfo, err := os.Stat(companionPath)
	if err != nil {
		return err
	}
	companionSHA1 := ""
	for _, file := range files {
		if filepath.Clean(file.FilePath) == filepath.Clean(companionPath) {
			companionSHA1 = file.SHA1
			break
		}
	}
	companionRelPath, err := filepath.Rel(library.RootPath, companionPath)
	if err != nil {
		return err
	}
	dependency, err := s.store.UpsertGame(domain.GameAsset{
		LibraryID: library.ID, Title: entry.GDROM, Platform: "naomi2", ROMSetName: entry.GDROM, Region: entry.Region, Format: "chd",
		FilePath: companionPath, RelPath: filepath.ToSlash(companionRelPath), Size: companionInfo.Size(), MTime: companionInfo.ModTime(), SHA1: companionSHA1,
		EmulatorHint: "flycast", Compatibility: "unknown", CatalogRole: "dependency",
	})
	if err != nil {
		return err
	}
	return s.store.ReplaceGameFiles(dependency.ID, []domain.GameFile{{Name: filepath.Base(companionPath), FilePath: companionPath, Size: companionInfo.Size(), MTime: companionInfo.ModTime(), SHA1: companionSHA1, Role: "entry", Position: 0}})
}

func (s *Scanner) canSkipNaomi2Game(path string, info fs.FileInfo) bool {
	shortName := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if shortName == "naomi2" {
		return s.store.CanSkipGame(path, info.Size(), info.ModTime(), "naomi2")
	}
	entry, ok := naomi2catalog.Lookup(shortName)
	if !ok {
		return false
	}
	files, totalSize, _, err := naomi2IndexedGameFiles(path, info, shortName, entry)
	if err != nil {
		return false
	}
	return s.store.CanSkipGameSet(path, totalSize, info.ModTime(), "naomi2", files)
}

func naomi2IndexedGameFiles(path string, info fs.FileInfo, shortName string, entry naomi2catalog.Entry) ([]domain.GameFile, int64, string, error) {
	files := []domain.GameFile{{Name: filepath.Base(path), FilePath: path, Size: info.Size(), MTime: info.ModTime(), Role: "entry", Position: 0}}
	if entry.Parent != "" {
		parentName := entry.Parent + ".zip"
		parentPath, err := resolveNaomi2ParentROMSet(path, shortName, entry.Parent, parentName)
		if err != nil {
			return nil, 0, filepath.Join(filepath.Dir(path), parentName), fmt.Errorf("NAOMI 2 parent ROM set %s: %w", parentName, err)
		}
		parentInfo, err := os.Stat(parentPath)
		if err != nil {
			return nil, 0, parentPath, err
		}
		if parentInfo.IsDir() {
			return nil, 0, parentPath, fmt.Errorf("NAOMI 2 parent ROM set is a directory: %s", parentName)
		}
		files = append(files, domain.GameFile{
			Name: parentName, FilePath: parentPath, Size: parentInfo.Size(), MTime: parentInfo.ModTime(), Role: "dependency", Position: 1,
		})
		return files, info.Size() + parentInfo.Size(), parentPath, nil
	}
	if entry.GDROM == "" {
		return files, info.Size(), "", nil
	}
	companionName := entry.GDROM + ".chd"
	companionPath, err := resolveDiscDependencyPath(filepath.Dir(path), filepath.Join(shortName, companionName))
	if err != nil {
		return nil, 0, filepath.Join(filepath.Dir(path), shortName, companionName), fmt.Errorf("NAOMI 2 GD-ROM companion %s: %w", companionName, err)
	}
	companionInfo, err := os.Stat(companionPath)
	if err != nil {
		return nil, 0, companionPath, err
	}
	if companionInfo.IsDir() {
		return nil, 0, companionPath, fmt.Errorf("NAOMI 2 GD-ROM companion is a directory: %s", companionName)
	}
	files = append(files, domain.GameFile{
		Name: filepath.ToSlash(filepath.Join(shortName, companionName)), FilePath: companionPath, Size: companionInfo.Size(), MTime: companionInfo.ModTime(), Role: "dependency", Position: 1,
	})
	return files, info.Size() + companionInfo.Size(), companionPath, nil
}

func resolveNaomi2ParentROMSet(path string, shortName string, parentShortName string, parentName string) (string, error) {
	gameDir := filepath.Dir(path)
	candidates := []struct {
		dir  string
		name string
	}{{dir: gameDir, name: parentName}}
	if strings.EqualFold(filepath.Base(gameDir), shortName) {
		root := filepath.Dir(gameDir)
		candidates = append(candidates,
			struct {
				dir  string
				name string
			}{dir: root, name: filepath.Join(parentShortName, parentName)},
			struct {
				dir  string
				name string
			}{dir: root, name: parentName},
		)
	}
	var lastErr error
	for _, candidate := range candidates {
		resolved, err := resolveDiscDependencyPath(candidate.dir, candidate.name)
		if err == nil {
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = os.ErrNotExist
	}
	return "", lastErr
}

func validateNaomi2DescriptorZIP(path string, entry naomi2catalog.Entry) error {
	reader, err := stdzip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("open NAOMI 2 ZIP: %w", err)
	}
	defer reader.Close()
	if len(reader.File) == 0 {
		return fmt.Errorf("NAOMI 2 ZIP has no entries")
	}
	foundExpectedPIC := entry.ExpectedPIC == ""
	for _, file := range reader.File {
		if err := validateArchiveEntryName(file.Name); err != nil {
			return err
		}
		if file.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("NAOMI 2 ZIP contains symlink entry: %s", file.Name)
		}
		if strings.EqualFold(filepath.Base(file.Name), entry.ExpectedPIC) {
			foundExpectedPIC = true
		}
	}
	if !foundExpectedPIC {
		return fmt.Errorf("NAOMI 2 ZIP is missing expected PIC member %s", entry.ExpectedPIC)
	}
	return nil
}

func naomi2Compatibility(entry naomi2catalog.Entry) string {
	if entry.GDROM != "" {
		return "playable"
	}
	return "untested"
}

func (s *Scanner) syncPC98SupportFiles(gameID int64) error {
	sources, err := s.store.GameSources(gameID)
	if err != nil {
		return err
	}
	fonts := make([]domain.GameFile, 0, 1)
	seen := make(map[string]bool)
	for _, source := range sources {
		files, err := pc98PackageFontFiles(source.FilePath)
		if err != nil {
			return err
		}
		for _, file := range files {
			key := strings.ToLower(filepath.Clean(file.FilePath))
			if seen[key] {
				continue
			}
			seen[key] = true
			fonts = append(fonts, file)
		}
		// One active font per package, selected from sources in launch order.
		if len(fonts) != 0 {
			break
		}
	}
	sort.Slice(fonts, func(i, j int) bool {
		return strings.ToLower(fonts[i].FilePath) < strings.ToLower(fonts[j].FilePath)
	})
	for index := range fonts {
		checksums, err := fileChecksums(fonts[index].FilePath)
		if err != nil {
			return err
		}
		fonts[index].SHA1 = checksums.sha1
	}
	return s.store.ReplacePC98SupportFiles(gameID, fonts)
}

func samePC98SupportFiles(sourcePath string, local []domain.GameFile, stored []domain.GameFile) bool {
	dir := filepath.Clean(filepath.Dir(sourcePath))
	filtered := make([]domain.GameFile, 0, len(stored))
	for _, file := range stored {
		if filepath.Clean(filepath.Dir(file.FilePath)) == dir {
			filtered = append(filtered, file)
		}
	}
	if len(local) != len(filtered) {
		return false
	}
	byPath := make(map[string]domain.GameFile, len(filtered))
	for _, file := range filtered {
		byPath[strings.ToLower(filepath.Clean(file.FilePath))] = file
	}
	for _, file := range local {
		storedFile, ok := byPath[strings.ToLower(filepath.Clean(file.FilePath))]
		if !ok || storedFile.Name != file.Name || storedFile.SHA1 != file.SHA1 || storedFile.Size != file.Size || !storedFile.MTime.Equal(file.MTime) {
			return false
		}
	}
	return true
}

func pc98PackageFontFiles(mediaPath string) ([]domain.GameFile, error) {
	dir := filepath.Dir(mediaPath)
	if hasPC98NegativePathSignal(filepath.ToSlash(dir)) {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	// Only exact, case-insensitive sibling names are eligible; never guess from
	// arbitrary BMP files or follow symlinks outside the source package.
	lookup := func(name string) (string, fs.FileInfo) {
		if name == "" || strings.ContainsAny(name, `/\\`) || name == "." || name == ".." {
			return "", nil
		}
		var found fs.DirEntry
		for _, entry := range entries {
			if strings.EqualFold(entry.Name(), name) {
				if found != nil {
					log.Printf("PC-98 font selection: ambiguous sibling %q for %q; ignoring candidate", name, mediaPath)
					return "", nil
				} // ambiguous on case-sensitive hosts
				found = entry
			}
		}
		if found == nil {
			return "", nil
		}
		info, err := found.Info()
		if err != nil || !info.Mode().IsRegular() {
			return "", nil
		}
		return filepath.Join(dir, found.Name()), info
	}
	names := []string{}
	if path, info := lookup(filepath.Base(mediaPath) + ".font.json"); info != nil && info.Size() <= 4096 {
		data, err := os.ReadFile(path)
		var association struct {
			File string `json:"file"`
		}
		if err == nil && json.Unmarshal(data, &association) == nil && strings.EqualFold(filepath.Ext(association.File), ".bmp") {
			names = append(names, association.File)
		}
	}
	names = append(names, strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))+".font.bmp", "FONT.bmp", "PC98_CN.bmp")
	for _, name := range names {
		path, info := lookup(name)
		if info == nil || !validPC98FontBitmap(path, info.Size()) {
			continue
		}
		checksums, err := fileChecksums(path)
		if err != nil {
			return nil, err
		}
		return []domain.GameFile{{Name: "FONT.bmp", FilePath: path, Size: info.Size(), MTime: info.ModTime(), SHA1: checksums.sha1, Role: "font"}}, nil
	}
	return nil, nil
}

func validPC98FontBitmap(path string, size int64) bool {
	if size < 62 || size > 1<<20 {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	header := make([]byte, 54)
	if _, err := io.ReadFull(file, header); err != nil || !bytes.Equal(header[:2], []byte("BM")) {
		return false
	}
	dibSize := binary.LittleEndian.Uint32(header[14:18])
	width := int32(binary.LittleEndian.Uint32(header[18:22]))
	height := int32(binary.LittleEndian.Uint32(header[22:26]))
	planes := binary.LittleEndian.Uint16(header[26:28])
	bitsPerPixel := binary.LittleEndian.Uint16(header[28:30])
	compression := binary.LittleEndian.Uint32(header[30:34])
	pixelOffset := int64(binary.LittleEndian.Uint32(header[10:14]))
	if height < 0 {
		height = -height
	}
	rowBytes := int64((2048 + 31) / 32 * 4)
	return dibSize >= 40 && width == 2048 && height == 2048 && planes == 1 && bitsPerPixel == 1 && compression == 0 &&
		pixelOffset >= 54 && size >= pixelOffset+rowBytes*2048
}

func mediaName(files []domain.GameFile, path string) string {
	if len(files) > 0 && strings.TrimSpace(files[0].Name) != "" {
		return files[0].Name
	}
	return filepath.Base(path)
}

const (
	pc98ArchiveMaxEntries           = 4096
	pc98ArchiveMaxUncompressedBytes = uint64(16 << 30)
	pc98MediaMaxBytes               = uint64(8 << 30)
	pc98ArchiveMaxCompressionRatio  = uint64(1000)
)

type pc98MediaInfo struct {
	name               string
	format             string
	size               int64
	checksums          checksumPair
	compatibility      string
	bootabilityChecked bool
}

// Some PC-98 CD releases boot from a prepared USER.FDI while loading game data
// from a sibling CUE/BIN image. Treat that directory as one launchable package.
type pc98MixedCDPackage struct {
	entryPath        string
	files            []domain.GameFile
	staleSourcePaths []string
}

func pc98MixedCDPackageForPath(path string) (pc98MixedCDPackage, bool) {
	dir := filepath.Dir(path)
	entryPath, ok := caseInsensitiveSiblingPath(dir, "USER.FDI")
	if !ok {
		return pc98MixedCDPackage{}, false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return pc98MixedCDPackage{}, false
	}
	var cuePaths []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".cue") {
			cuePaths = append(cuePaths, filepath.Join(dir, entry.Name()))
		}
	}
	if len(cuePaths) != 1 {
		return pc98MixedCDPackage{}, false
	}
	entryInfo, err := os.Stat(entryPath)
	if err != nil || entryInfo.IsDir() {
		return pc98MixedCDPackage{}, false
	}
	cueInfo, err := os.Stat(cuePaths[0])
	if err != nil || cueInfo.IsDir() {
		return pc98MixedCDPackage{}, false
	}
	cueFiles, _, err := indexedGameFiles(cuePaths[0], cueInfo, ".cue")
	if err != nil || len(cueFiles) < 2 {
		return pc98MixedCDPackage{}, false
	}
	files := []domain.GameFile{{
		Name: filepath.Base(entryPath), FilePath: entryPath, Size: entryInfo.Size(), MTime: entryInfo.ModTime(), Role: "entry", Position: 0,
	}}
	files = append(files, domain.GameFile{
		Name: filepath.Base(cuePaths[0]), FilePath: cuePaths[0], Size: cueInfo.Size(), MTime: cueInfo.ModTime(), Role: "dependency", Position: 1,
	})
	for _, file := range cueFiles[1:] {
		file.Role = "dependency"
		file.Position = len(files)
		files = append(files, file)
	}
	if artworkPath, found := caseInsensitiveSiblingPath(dir, "KAMON.GIF"); found {
		if artworkInfo, statErr := os.Stat(artworkPath); statErr == nil && !artworkInfo.IsDir() {
			files = append(files, domain.GameFile{
				Name: filepath.Base(artworkPath), FilePath: artworkPath, Size: artworkInfo.Size(), MTime: artworkInfo.ModTime(), Role: "dependency", Position: len(files),
			})
		}
	}
	stalePaths := make([]string, 0, 2)
	for _, name := range []string{"SYSTEM.FDI", "USER.NFD"} {
		if stalePath, found := caseInsensitiveSiblingPath(dir, name); found && !sameFilePath(stalePath, entryPath) {
			stalePaths = append(stalePaths, stalePath)
		}
	}
	managed := append([]string{entryPath}, cuePaths...)
	for _, file := range files[2:] {
		managed = append(managed, file.FilePath)
	}
	managed = append(managed, stalePaths...)
	for _, managedPath := range managed {
		if sameFilePath(path, managedPath) {
			return pc98MixedCDPackage{entryPath: entryPath, files: files, staleSourcePaths: stalePaths}, true
		}
	}
	return pc98MixedCDPackage{}, false
}

func caseInsensitiveSiblingPath(dir string, name string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(entry.Name(), name) {
			return filepath.Join(dir, entry.Name()), true
		}
	}
	return "", false
}

func sameFilePath(left string, right string) bool {
	return filepath.Clean(left) == filepath.Clean(right)
}

func gameFilesSize(files []domain.GameFile) int64 {
	var size int64
	for _, file := range files {
		size += file.Size
	}
	return size
}

func samePC98MixedCDPackageFiles(expected []domain.GameFile, stored []domain.GameFile) bool {
	actual := make([]domain.GameFile, 0, len(stored))
	for _, file := range stored {
		if file.Role != "font" {
			actual = append(actual, file)
		}
	}
	if len(expected) != len(actual) {
		return false
	}
	for index := range expected {
		if !sameFilePath(expected[index].FilePath, actual[index].FilePath) ||
			expected[index].Name != actual[index].Name || expected[index].Role != actual[index].Role ||
			expected[index].Size != actual[index].Size || !expected[index].MTime.Equal(actual[index].MTime) || actual[index].SHA1 == "" {
			return false
		}
	}
	return true
}

func inspectPC98Media(path string, info fs.FileInfo, ext string) (pc98MediaInfo, error) {
	if ext != ".zip" {
		if !isPC98MediaExt(ext) {
			return pc98MediaInfo{}, fmt.Errorf("unsupported PC-98 format %s", ext)
		}
		file, err := os.Open(path)
		if err != nil {
			return pc98MediaInfo{}, err
		}
		defer file.Close()
		checksums, err := validateAndChecksumPC98Media(file, uint64(info.Size()), ext)
		if err != nil {
			return pc98MediaInfo{}, err
		}
		compatibility := "untested"
		if ext == ".hdi" {
			bootable, err := inspectPC98HDIBootability(func() (io.ReadCloser, error) {
				return os.Open(path)
			}, uint64(info.Size()))
			if err != nil {
				return pc98MediaInfo{}, fmt.Errorf("inspect PC-98 HDI bootability: %w", err)
			}
			if !bootable {
				compatibility = "broken"
			}
		}
		return pc98MediaInfo{
			name: filepath.Base(path), format: strings.TrimPrefix(ext, "."), size: info.Size(), checksums: checksums,
			compatibility: compatibility, bootabilityChecked: ext == ".hdi",
		}, nil
	}

	reader, err := stdzip.OpenReader(path)
	if err != nil {
		return pc98MediaInfo{}, fmt.Errorf("open PC-98 zip: %w", err)
	}
	defer reader.Close()
	if len(reader.File) > pc98ArchiveMaxEntries {
		return pc98MediaInfo{}, fmt.Errorf("PC-98 zip has %d entries, limit is %d", len(reader.File), pc98ArchiveMaxEntries)
	}
	type archiveCandidate struct {
		file *stdzip.File
		name string
	}
	var total uint64
	candidates := make([]archiveCandidate, 0, 1)
	for _, file := range reader.File {
		decodedName := DecodePC98ZIPEntryName(file.Name, file.NonUTF8)
		if err := validateArchiveEntryName(decodedName); err != nil {
			return pc98MediaInfo{}, err
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return pc98MediaInfo{}, fmt.Errorf("PC-98 zip contains symlink entry: %s", file.Name)
		}
		if file.Flags&0x1 != 0 {
			return pc98MediaInfo{}, fmt.Errorf("PC-98 zip contains encrypted entry: %s", file.Name)
		}
		total += file.UncompressedSize64
		if total > pc98ArchiveMaxUncompressedBytes {
			return pc98MediaInfo{}, fmt.Errorf("PC-98 zip uncompressed size exceeds %d bytes", pc98ArchiveMaxUncompressedBytes)
		}
		if file.CompressedSize64 > 0 && file.UncompressedSize64/file.CompressedSize64 > pc98ArchiveMaxCompressionRatio {
			return pc98MediaInfo{}, fmt.Errorf("PC-98 zip entry compression ratio is too high: %s", file.Name)
		}
		if !file.FileInfo().IsDir() && !isIgnoredArchiveEntry(decodedName) && isPC98MediaExt(strings.ToLower(filepath.Ext(decodedName))) {
			candidates = append(candidates, archiveCandidate{file: file, name: decodedName})
		}
	}
	if len(candidates) != 1 {
		return pc98MediaInfo{}, fmt.Errorf("PC-98 zip requires exactly one media candidate, found %d; manual review required", len(candidates))
	}
	candidate := candidates[0]
	if candidate.file.UncompressedSize64 == 0 || candidate.file.UncompressedSize64 > pc98MediaMaxBytes {
		return pc98MediaInfo{}, fmt.Errorf("invalid PC-98 media size %d", candidate.file.UncompressedSize64)
	}
	body, err := candidate.file.Open()
	if err != nil {
		return pc98MediaInfo{}, fmt.Errorf("open PC-98 media entry: %w", err)
	}
	defer body.Close()
	mediaExt := strings.ToLower(filepath.Ext(candidate.name))
	checksums, err := validateAndChecksumPC98Media(body, candidate.file.UncompressedSize64, mediaExt)
	if err != nil {
		return pc98MediaInfo{}, fmt.Errorf("PC-98 media entry %s: %w", candidate.name, err)
	}
	compatibility := "untested"
	if mediaExt == ".hdi" {
		bootable, err := inspectPC98HDIBootability(candidate.file.Open, candidate.file.UncompressedSize64)
		if err != nil {
			return pc98MediaInfo{}, fmt.Errorf("inspect PC-98 HDI entry %s bootability: %w", candidate.name, err)
		}
		if !bootable {
			compatibility = "broken"
		}
	}
	name := filepath.Base(filepath.Clean(strings.ReplaceAll(candidate.name, `\`, "/")))
	return pc98MediaInfo{
		name: name, format: strings.TrimPrefix(mediaExt, "."), size: int64(candidate.file.UncompressedSize64), checksums: checksums,
		compatibility: compatibility, bootabilityChecked: mediaExt == ".hdi",
	}, nil
}

func inspectPC98HDIBootability(open func() (io.ReadCloser, error), declaredSize uint64) (bool, error) {
	header, err := readPC98MediaRange(open, 0, 32, declaredSize)
	if err != nil {
		return false, err
	}
	headerSize := uint64(binary.LittleEndian.Uint32(header[8:12]))
	sectorSize := uint64(binary.LittleEndian.Uint32(header[16:20]))
	sectorsPerTrack := uint64(binary.LittleEndian.Uint32(header[20:24]))
	surfaces := uint64(binary.LittleEndian.Uint32(header[24:28]))
	if headerSize < 512 || sectorSize < 128 || sectorSize > 4096 || sectorsPerTrack == 0 || surfaces == 0 {
		return false, nil
	}

	tableOffset := headerSize + sectorSize
	if tableOffset > declaredSize || sectorSize > declaredSize-tableOffset {
		return false, nil
	}
	table, err := readPC98MediaRange(open, tableOffset, sectorSize, declaredSize)
	if err != nil {
		return false, err
	}
	startCylinder := uint64(0)
	active := false
	foundPartition := false
	for offset := 0; offset+32 <= len(table); offset += 32 {
		if !bytes.Contains(table[offset+16:offset+32], []byte("MS-DOS")) {
			continue
		}
		startCylinder = uint64(binary.LittleEndian.Uint16(table[offset+6 : offset+8]))
		active = table[offset]&0x80 != 0
		foundPartition = true
		break
	}
	if !foundPartition || !active {
		return false, nil
	}

	bytesPerCylinder := sectorsPerTrack * surfaces * sectorSize
	if bytesPerCylinder == 0 || startCylinder > (^uint64(0)-headerSize)/bytesPerCylinder {
		return false, nil
	}
	bootOffset := headerSize + startCylinder*bytesPerCylinder
	if bootOffset > declaredSize || 32 > declaredSize-bootOffset {
		return false, nil
	}
	boot, err := readPC98MediaRange(open, bootOffset, 32, declaredSize)
	if err != nil {
		return false, err
	}
	logicalSectorSize := uint64(binary.LittleEndian.Uint16(boot[11:13]))
	reservedSectors := uint64(binary.LittleEndian.Uint16(boot[14:16]))
	fatCount := uint64(boot[16])
	rootEntries := uint64(binary.LittleEndian.Uint16(boot[17:19]))
	fatSectors := uint64(binary.LittleEndian.Uint16(boot[22:24]))
	if logicalSectorSize < 128 || logicalSectorSize > 4096 || logicalSectorSize%128 != 0 || reservedSectors == 0 ||
		fatCount < 1 || fatCount > 4 || rootEntries == 0 || rootEntries > 8192 || fatSectors == 0 {
		return false, nil
	}
	fatRegionSectors := reservedSectors + fatCount*fatSectors
	if fatRegionSectors > (^uint64(0)-bootOffset)/logicalSectorSize {
		return false, nil
	}
	rootOffset := bootOffset + fatRegionSectors*logicalSectorSize
	rootSize := rootEntries * 32
	if rootOffset > declaredSize || rootSize > declaredSize-rootOffset {
		return false, nil
	}
	root, err := readPC98MediaRange(open, rootOffset, rootSize, declaredSize)
	if err != nil {
		return false, err
	}
	required := [][]byte{[]byte("IO      SYS"), []byte("MSDOS   SYS"), []byte("COMMAND COM")}
	for _, name := range required {
		if !pc98FATRootContains(root, name) {
			return false, nil
		}
	}
	return true, nil
}

func readPC98MediaRange(open func() (io.ReadCloser, error), offset, size, declaredSize uint64) ([]byte, error) {
	if size == 0 || offset > declaredSize || size > declaredSize-offset || size > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("PC-98 media range %d+%d exceeds %d bytes", offset, size, declaredSize)
	}
	body, err := open()
	if err != nil {
		return nil, err
	}
	defer body.Close()
	if _, err := io.CopyN(io.Discard, body, int64(offset)); err != nil {
		return nil, err
	}
	data := make([]byte, int(size))
	if _, err := io.ReadFull(body, data); err != nil {
		return nil, err
	}
	return data, nil
}

func pc98FATRootContains(root []byte, expected []byte) bool {
	if len(expected) != 11 {
		return false
	}
	for offset := 0; offset+11 <= len(root); offset += 32 {
		if root[offset] == 0 || root[offset] == 0xe5 {
			continue
		}
		if bytes.Equal(root[offset:offset+11], expected) {
			return true
		}
	}
	return false
}

func DecodePC98ZIPEntryName(name string, nonUTF8 bool) string {
	if !nonUTF8 && utf8.ValidString(name) && !strings.ContainsRune(name, '\uFFFD') {
		return name
	}
	decoded, err := japanese.ShiftJIS.NewDecoder().Bytes([]byte(name))
	if err == nil && utf8.Valid(decoded) && !bytes.ContainsRune(decoded, '\uFFFD') {
		return string(decoded)
	}
	return strings.ToValidUTF8(name, "")
}

func validateAndChecksumPC98Media(reader io.Reader, declaredSize uint64, ext string) (checksumPair, error) {
	if declaredSize == 0 || declaredSize > pc98MediaMaxBytes {
		return checksumPair{}, fmt.Errorf("invalid PC-98 media size %d", declaredSize)
	}
	crc := crc32.NewIEEE()
	sha := sha1.New()
	stream := io.TeeReader(io.LimitReader(reader, int64(declaredSize)+1), io.MultiWriter(crc, sha))
	prefixSize := declaredSize
	if prefixSize > 4096 {
		prefixSize = 4096
	}
	prefix := make([]byte, int(prefixSize))
	if _, err := io.ReadFull(stream, prefix); err != nil {
		return checksumPair{}, fmt.Errorf("read PC-98 media header: %w", err)
	}
	if err := validatePC98MediaHeader(prefix, declaredSize, ext); err != nil {
		return checksumPair{}, err
	}
	written, err := io.Copy(io.Discard, stream)
	if err != nil {
		return checksumPair{}, err
	}
	if uint64(written)+prefixSize != declaredSize {
		return checksumPair{}, fmt.Errorf("PC-98 media size mismatch: read %d, expected %d", uint64(written)+prefixSize, declaredSize)
	}
	return checksumPair{crc32: fmt.Sprintf("%08x", crc.Sum32()), sha1: hex.EncodeToString(sha.Sum(nil))}, nil
}

func validatePC98MediaHeader(prefix []byte, declaredSize uint64, ext string) error {
	ext = strings.ToLower(strings.TrimSpace(ext))
	switch ext {
	case ".hdi":
		return validatePC98AnexImage(prefix, declaredSize)
	case ".fdi":
		if err := validatePC98AnexImage(prefix, declaredSize); err == nil {
			return nil
		}
		if isKnownPC98RawFloppySize(declaredSize) {
			return nil
		}
		return fmt.Errorf("PC-98 FDI image is neither Anex86 nor a known raw floppy geometry")
	case ".nhd":
		return validatePC98NHD(prefix, declaredSize)
	case ".vhd":
		return validatePC98Virtual98VHD(prefix, declaredSize)
	case ".slh":
		return validatePC98SLH(prefix, declaredSize)
	case ".thd":
		return validatePC98THD(prefix, declaredSize)
	case ".hdn":
		if declaredSize < 512 || (declaredSize%(512*8*25) != 0 && declaredSize%(512*8*32) != 0) {
			return fmt.Errorf("invalid PC-98 HDN geometry for %d bytes", declaredSize)
		}
		return nil
	case ".d88", ".88d", ".d98", ".98d":
		return validatePC98D88(prefix, declaredSize)
	default:
		if isKnownPC98RawFloppySize(declaredSize) {
			return nil
		}
		return fmt.Errorf("PC-98 %s image does not match a known raw floppy geometry", strings.TrimPrefix(ext, "."))
	}
}

func validatePC98AnexImage(prefix []byte, declaredSize uint64) error {
	if len(prefix) < 32 {
		return fmt.Errorf("truncated Anex86 image header")
	}
	headerSize := uint64(binary.LittleEndian.Uint32(prefix[8:12]))
	dataSize := uint64(binary.LittleEndian.Uint32(prefix[12:16]))
	sectorSize := uint64(binary.LittleEndian.Uint32(prefix[16:20]))
	sectors := uint64(binary.LittleEndian.Uint32(prefix[20:24]))
	surfaces := uint64(binary.LittleEndian.Uint32(prefix[24:28]))
	cylinders := uint64(binary.LittleEndian.Uint32(prefix[28:32]))
	if !validPC98Geometry(cylinders, surfaces, sectors, sectorSize) || headerSize < 32 || headerSize > declaredSize {
		return fmt.Errorf("invalid Anex86 image geometry")
	}
	geometrySize := cylinders * surfaces * sectors * sectorSize
	if geometrySize != dataSize || headerSize+dataSize != declaredSize {
		return fmt.Errorf("Anex86 image header size does not match file size")
	}
	return nil
}

func validatePC98NHD(prefix []byte, declaredSize uint64) error {
	if len(prefix) < 288 || !bytes.HasPrefix(prefix, []byte("T98HDDIMAGE.R0")) {
		return fmt.Errorf("invalid NHD signature or truncated header")
	}
	headerSize := uint64(binary.LittleEndian.Uint32(prefix[272:276]))
	cylinders := uint64(binary.LittleEndian.Uint32(prefix[276:280]))
	surfaces := uint64(binary.LittleEndian.Uint16(prefix[280:282]))
	sectors := uint64(binary.LittleEndian.Uint16(prefix[282:284]))
	sectorSize := uint64(binary.LittleEndian.Uint16(prefix[284:286]))
	if !validPC98Geometry(cylinders, surfaces, sectors, sectorSize) || headerSize < 288 || headerSize > declaredSize {
		return fmt.Errorf("invalid NHD geometry")
	}
	if headerSize+cylinders*surfaces*sectors*sectorSize != declaredSize {
		return fmt.Errorf("NHD geometry does not match file size")
	}
	return nil
}

func validatePC98Virtual98VHD(prefix []byte, declaredSize uint64) error {
	const headerSize = uint64(216)
	if len(prefix) < int(headerSize) || !bytes.HasPrefix(prefix, []byte("VHD1.00")) {
		return fmt.Errorf("invalid Virtual98 VHD signature or truncated header")
	}
	sectorSize := uint64(binary.LittleEndian.Uint16(prefix[142:144]))
	sectors := uint64(prefix[144])
	surfaces := uint64(prefix[145])
	cylinders := uint64(binary.LittleEndian.Uint16(prefix[146:148]))
	totals := uint64(binary.LittleEndian.Uint32(prefix[148:152]))
	if !validPC98Geometry(cylinders, surfaces, sectors, sectorSize) || totals == 0 {
		return fmt.Errorf("invalid Virtual98 VHD geometry")
	}
	if headerSize+totals*sectorSize != declaredSize {
		return fmt.Errorf("Virtual98 VHD geometry does not match file size")
	}
	return nil
}

func validatePC98SLH(prefix []byte, declaredSize uint64) error {
	if len(prefix) < 28 || !bytes.HasPrefix(prefix, []byte("HDIM")) {
		return fmt.Errorf("invalid SLH signature or truncated header")
	}
	driveSize := binary.LittleEndian.Uint64(prefix[4:12])
	sectorSize := uint64(binary.LittleEndian.Uint32(prefix[12:16]))
	cylinders := uint64(binary.LittleEndian.Uint32(prefix[16:20]))
	surfaces := uint64(binary.LittleEndian.Uint32(prefix[20:24]))
	sectors := uint64(binary.LittleEndian.Uint32(prefix[24:28]))
	if !validPC98Geometry(cylinders, surfaces, sectors, sectorSize) || driveSize != cylinders*surfaces*sectors*sectorSize {
		return fmt.Errorf("invalid SLH geometry")
	}
	if 512+driveSize != declaredSize {
		return fmt.Errorf("SLH geometry does not match file size")
	}
	return nil
}

func validatePC98THD(prefix []byte, declaredSize uint64) error {
	if len(prefix) < 2 {
		return fmt.Errorf("truncated THD header")
	}
	cylinders := uint64(binary.LittleEndian.Uint16(prefix[:2]))
	if cylinders == 0 || cylinders >= 65536 || 256+cylinders*33*8*256 != declaredSize {
		return fmt.Errorf("invalid THD geometry")
	}
	return nil
}

func validatePC98D88(prefix []byte, declaredSize uint64) error {
	const headerSize = 0x2b0
	if len(prefix) < headerSize || declaredSize < headerSize {
		return fmt.Errorf("truncated D88 image header")
	}
	diskSize := uint64(binary.LittleEndian.Uint32(prefix[0x1c:0x20]))
	if diskSize != declaredSize {
		return fmt.Errorf("D88 declared size %d does not match file size %d", diskSize, declaredSize)
	}
	previous := uint64(0)
	hasTrack := false
	for offset := 0x20; offset+4 <= headerSize; offset += 4 {
		track := uint64(binary.LittleEndian.Uint32(prefix[offset : offset+4]))
		if track == 0 {
			continue
		}
		if track < headerSize || track >= declaredSize || (previous != 0 && track <= previous) {
			return fmt.Errorf("invalid D88 track table")
		}
		previous = track
		hasTrack = true
	}
	if !hasTrack {
		return fmt.Errorf("D88 image contains no tracks")
	}
	return nil
}

func validPC98Geometry(cylinders, surfaces, sectors, sectorSize uint64) bool {
	return cylinders > 0 && cylinders < 65536 && surfaces > 0 && surfaces < 256 && sectors > 0 && sectors < 256 && sectorSize >= 128 && sectorSize <= 4096 && sectorSize&(sectorSize-1) == 0
}

func isKnownPC98RawFloppySize(size uint64) bool {
	known := map[uint64]struct{}{
		327680: {}, 655360: {}, 737280: {}, 1228800: {}, 1261568: {}, 1269760: {}, 1474560: {},
	}
	_, ok := known[size]
	return ok
}

func isPC98MediaExt(ext string) bool {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".d88", ".88d", ".d98", ".98d", ".fdi", ".xdf", ".hdm", ".dup", ".2hd", ".tfd", ".nfd", ".hd4", ".hd5", ".hd9", ".fdd", ".h01", ".hdb", ".ddb", ".dd6", ".dcp", ".dcu", ".flp", ".img", ".ima", ".bin", ".fim", ".thd", ".nhd", ".hdi", ".vhd", ".slh", ".hdn", ".cmd":
		return true
	default:
		return false
	}
}

const (
	consoleArchiveMaxEntries           = 4096
	consoleArchiveMaxUncompressedBytes = uint64(1 << 30)
	consoleROMMaxBytes                 = uint64(512 << 20)
)

type consoleROMInfo struct {
	name       string
	platform   string
	format     string
	size       int64
	checksums  checksumPair
	definitive bool
}

type naomiArchiveMember struct {
	size  uint64
	crc32 uint32
}

type naomiROMSetIdentity struct {
	romSetName string
	parent     string
	title      string
	bootMember string
	members    map[string]naomiArchiveMember
}

var projectJusticeNaomiROMSetCatalog = []naomiROMSetIdentity{
	projectJusticeNaomiROMSetIdentity("pjustica", "pjustic", "Project Justice / Moero! Justice Gakuen (Rev A)", "epr-23548a.ic22", 0xf4ccf1ec),
	projectJusticeNaomiROMSetIdentity("pjustic", "", "Project Justice / Moero! Justice Gakuen", "epr-23548b.ic22", 0xd0dbdf40),
}

func isProjectJusticeArchiveName(path string) bool {
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	return name == "pjustic" || name == "pjustica"
}

func projectJusticeNaomiROMSetIdentity(romSetName string, parent string, title string, bootMember string, bootCRC uint32) naomiROMSetIdentity {
	members := map[string]naomiArchiveMember{
		bootMember:       {size: 0x0400000, crc32: bootCRC},
		"mpr-23537.ic1":  {size: 0x1000000, crc32: 0xa2462770},
		"mpr-23538.ic2":  {size: 0x1000000, crc32: 0xe4480832},
		"mpr-23539.ic3":  {size: 0x1000000, crc32: 0x97e3f7f5},
		"mpr-23540.ic4":  {size: 0x1000000, crc32: 0xb9e92d21},
		"mpr-23541.ic5":  {size: 0x1000000, crc32: 0x95b8a9c6},
		"mpr-23542.ic6":  {size: 0x1000000, crc32: 0xdfd490f5},
		"mpr-23543.ic7":  {size: 0x1000000, crc32: 0x66847ebd},
		"mpr-23544.ic8":  {size: 0x1000000, crc32: 0xd1f5b460},
		"mpr-23545.ic9":  {size: 0x1000000, crc32: 0x60bd692f},
		"mpr-23546.ic10": {size: 0x1000000, crc32: 0x85db2248},
		"mpr-23547.ic11": {size: 0x1000000, crc32: 0x18b369c7},
	}
	return naomiROMSetIdentity{romSetName: romSetName, parent: parent, title: title, bootMember: bootMember, members: members}
}

func inspectNaomiROMZIP(path string) (naomiROMSetIdentity, bool, error) {
	reader, err := stdzip.OpenReader(path)
	if err != nil {
		return naomiROMSetIdentity{}, false, fmt.Errorf("open NAOMI ROM zip: %w", err)
	}
	defer reader.Close()

	members := make(map[string]naomiArchiveMember, len(reader.File))
	for _, file := range reader.File {
		if err := validateArchiveEntryName(file.Name); err != nil {
			return naomiROMSetIdentity{}, false, err
		}
		if file.FileInfo().IsDir() || isIgnoredArchiveEntry(file.Name) {
			continue
		}
		if file.Mode()&os.ModeSymlink != 0 || file.Flags&0x1 != 0 {
			return naomiROMSetIdentity{}, false, fmt.Errorf("unsupported NAOMI ROM zip entry: %s", file.Name)
		}
		name := strings.ToLower(filepath.Base(filepath.Clean(strings.ReplaceAll(file.Name, `\`, "/"))))
		if _, exists := members[name]; exists {
			return naomiROMSetIdentity{}, false, fmt.Errorf("NAOMI ROM zip contains duplicate member %s", name)
		}
		members[name] = naomiArchiveMember{size: file.UncompressedSize64, crc32: file.CRC32}
	}
	return identifyProjectJusticeNaomiROMSet(members)
}

func identifyProjectJusticeNaomiROMSet(members map[string]naomiArchiveMember) (naomiROMSetIdentity, bool, error) {
	matched := make([]naomiROMSetIdentity, 0, 1)
	for _, identity := range projectJusticeNaomiROMSetCatalog {
		if _, exists := members[identity.bootMember]; exists {
			matched = append(matched, identity)
		}
	}
	if len(matched) == 0 {
		return naomiROMSetIdentity{}, false, nil
	}
	if len(matched) != 1 {
		return naomiROMSetIdentity{}, true, errors.New("Project Justice archive contains boot ROMs from multiple revisions")
	}
	identity := matched[0]
	for name, expected := range identity.members {
		actual, exists := members[name]
		if !exists {
			return naomiROMSetIdentity{}, true, fmt.Errorf("Project Justice %s archive is missing %s", identity.romSetName, name)
		}
		if actual.size != expected.size || actual.crc32 != expected.crc32 {
			return naomiROMSetIdentity{}, true, fmt.Errorf("Project Justice %s member %s has size/CRC %d/%08x, expected %d/%08x", identity.romSetName, name, actual.size, actual.crc32, expected.size, expected.crc32)
		}
	}
	return identity, true, nil
}

// inspectConsoleROMZIP treats explicit cartridge ROM content as stronger
// evidence than an archive short name. The short name remains useful only
// when the archive does not contain a recognizable console ROM.
func inspectConsoleROMZIP(path string, withChecksums bool) (consoleROMInfo, bool, error) {
	reader, err := stdzip.OpenReader(path)
	if err != nil {
		return consoleROMInfo{}, false, fmt.Errorf("open console ROM zip: %w", err)
	}
	defer reader.Close()
	if len(reader.File) > consoleArchiveMaxEntries {
		return consoleROMInfo{}, false, fmt.Errorf("console ROM zip has %d entries, limit is %d", len(reader.File), consoleArchiveMaxEntries)
	}

	var total uint64
	candidates := make([]consoleROMInfo, 0, 2)
	files := make([]*stdzip.File, 0, 2)
	binFiles := make([]*stdzip.File, 0, 1)
	for _, file := range reader.File {
		if err := validateArchiveEntryName(file.Name); err != nil {
			return consoleROMInfo{}, false, err
		}
		total += file.UncompressedSize64
		if total > consoleArchiveMaxUncompressedBytes {
			return consoleROMInfo{}, false, fmt.Errorf("console ROM zip uncompressed size exceeds %d bytes", consoleArchiveMaxUncompressedBytes)
		}
		if file.FileInfo().IsDir() || isIgnoredArchiveEntry(file.Name) {
			continue
		}
		if file.Mode()&os.ModeSymlink != 0 || file.Flags&0x1 != 0 {
			return consoleROMInfo{}, false, fmt.Errorf("unsupported console ROM zip entry: %s", file.Name)
		}
		ext := strings.ToLower(filepath.Ext(file.Name))
		platform, format, ok := consolePlatformForROMExtension(ext)
		if ok {
			candidates = append(candidates, consoleROMInfo{
				name: filepath.Base(filepath.Clean(strings.ReplaceAll(file.Name, `\`, "/"))), platform: platform,
				format: format, size: int64(file.UncompressedSize64),
			})
			files = append(files, file)
			continue
		}
		if ext == ".bin" {
			binFiles = append(binFiles, file)
		}
	}

	if len(candidates) == 0 && len(binFiles) == 1 {
		isMegaDrive, err := hasMegaDriveROMHeader(binFiles[0])
		if err != nil {
			return consoleROMInfo{}, false, err
		}
		if isMegaDrive {
			file := binFiles[0]
			candidates = append(candidates, consoleROMInfo{
				name: filepath.Base(filepath.Clean(strings.ReplaceAll(file.Name, `\`, "/"))), platform: "md",
				format: "bin", size: int64(file.UncompressedSize64),
			})
			files = append(files, file)
		}
	}
	if len(candidates) == 0 {
		return consoleROMInfo{}, false, nil
	}

	platform := candidates[0].platform
	for _, candidate := range candidates[1:] {
		if candidate.platform != platform {
			return consoleROMInfo{platform: "unknown", format: "zip"}, true, nil
		}
	}
	if len(candidates) != 1 {
		return consoleROMInfo{platform: platform, format: "zip"}, true, nil
	}
	if uint64(candidates[0].size) == 0 || uint64(candidates[0].size) > consoleROMMaxBytes {
		return consoleROMInfo{}, false, fmt.Errorf("console ROM entry %s has invalid size %d", candidates[0].name, candidates[0].size)
	}
	candidates[0].definitive = true
	if !withChecksums {
		return candidates[0], true, nil
	}
	body, err := files[0].Open()
	if err != nil {
		return consoleROMInfo{}, false, fmt.Errorf("open console ROM entry %s: %w", candidates[0].name, err)
	}
	defer body.Close()
	checksums, err := checksumsForDeclaredSize(body, uint64(candidates[0].size))
	if err != nil {
		return consoleROMInfo{}, false, fmt.Errorf("checksum console ROM entry %s: %w", candidates[0].name, err)
	}
	candidates[0].checksums = checksums
	return candidates[0], true, nil
}

func consolePlatformForROMExtension(ext string) (platform string, format string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".vb", ".vboy":
		return "virtualboy", strings.TrimPrefix(strings.ToLower(ext), "."), true
	case ".sfc", ".smc":
		return "snes", strings.TrimPrefix(strings.ToLower(ext), "."), true
	case ".nes":
		return "nes", "nes", true
	case ".gb":
		return "gb", "gb", true
	case ".gbc":
		return "gbc", "gbc", true
	case ".gba":
		return "gba", "gba", true
	case ".nds":
		return "nds", "nds", true
	case ".md", ".gen":
		return "md", strings.TrimPrefix(strings.ToLower(ext), "."), true
	default:
		return "", "", false
	}
}

func consolePlatformMetadata(platform string) (romSetName string, emulatorHint string) {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "virtualboy":
		return "Virtual Boy", "virtualfriend"
	case "snes":
		return "SNES", "snes9x"
	case "nes":
		return "NES", "nestopia"
	case "gb":
		return "GB", "gambatte"
	case "gbc":
		return "GBC", "gambatte"
	case "gba":
		return "GBA", "mgba"
	case "nds":
		return "Nintendo DS", "melonds-ds"
	case "md":
		return "Mega Drive", "genesisplusgx"
	default:
		return "", ""
	}
}

func hasMegaDriveROMHeader(file *stdzip.File) (bool, error) {
	if file.UncompressedSize64 < 0x104 || file.UncompressedSize64 > consoleROMMaxBytes {
		return false, nil
	}
	body, err := file.Open()
	if err != nil {
		return false, fmt.Errorf("open Mega Drive ROM candidate %s: %w", file.Name, err)
	}
	defer body.Close()
	header := make([]byte, 0x104)
	if _, err := io.ReadFull(body, header); err != nil {
		return false, fmt.Errorf("read Mega Drive ROM candidate %s: %w", file.Name, err)
	}
	return bytes.Equal(header[0x100:0x104], []byte("SEGA")), nil
}

func checksumsForDeclaredSize(reader io.Reader, declaredSize uint64) (checksumPair, error) {
	crc := crc32.NewIEEE()
	sha := sha1.New()
	sha256Hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(crc, sha, sha256Hash), io.LimitReader(reader, int64(declaredSize)+1))
	if err != nil {
		return checksumPair{}, err
	}
	if uint64(written) != declaredSize {
		return checksumPair{}, fmt.Errorf("ROM size mismatch: read %d, expected %d", written, declaredSize)
	}
	return checksumPair{
		crc32:  fmt.Sprintf("%08x", crc.Sum32()),
		sha1:   hex.EncodeToString(sha.Sum(nil)),
		sha256: hex.EncodeToString(sha256Hash.Sum(nil)),
	}, nil
}

const (
	n64ArchiveMaxEntries           = 4096
	n64ArchiveMaxUncompressedBytes = uint64(1 << 30)
	n64ROMMaxBytes                 = uint64(512 << 20)
)

type n64ROMInfo struct {
	name      string
	format    string
	size      int64
	checksums checksumPair
}

func inspectN64ROM(path string, info fs.FileInfo, ext string) (n64ROMInfo, error) {
	if ext != ".zip" {
		if !isN64RawExt(ext) {
			return n64ROMInfo{}, fmt.Errorf("unsupported N64 format %s", ext)
		}
		file, err := os.Open(path)
		if err != nil {
			return n64ROMInfo{}, err
		}
		defer file.Close()
		checksums, format, err := validateAndChecksumN64ROM(file, uint64(info.Size()))
		if err != nil {
			return n64ROMInfo{}, err
		}
		return n64ROMInfo{name: canonicalN64ROMName(filepath.Base(path), format), format: format, size: info.Size(), checksums: checksums}, nil
	}

	reader, err := stdzip.OpenReader(path)
	if err != nil {
		return n64ROMInfo{}, fmt.Errorf("open N64 zip: %w", err)
	}
	defer reader.Close()
	if len(reader.File) > n64ArchiveMaxEntries {
		return n64ROMInfo{}, fmt.Errorf("N64 zip has %d entries, limit is %d", len(reader.File), n64ArchiveMaxEntries)
	}
	var total uint64
	candidates := make([]*stdzip.File, 0, 1)
	for _, file := range reader.File {
		if err := validateArchiveEntryName(file.Name); err != nil {
			return n64ROMInfo{}, err
		}
		total += file.UncompressedSize64
		if total > n64ArchiveMaxUncompressedBytes {
			return n64ROMInfo{}, fmt.Errorf("N64 zip uncompressed size exceeds %d bytes", n64ArchiveMaxUncompressedBytes)
		}
		if !file.FileInfo().IsDir() && !isIgnoredArchiveEntry(file.Name) && isN64RawExt(strings.ToLower(filepath.Ext(file.Name))) {
			candidates = append(candidates, file)
		}
	}
	if len(candidates) != 1 {
		return n64ROMInfo{}, fmt.Errorf("N64 zip requires exactly one ROM candidate, found %d; manual review required", len(candidates))
	}
	candidate := candidates[0]
	if candidate.UncompressedSize64 > n64ROMMaxBytes {
		return n64ROMInfo{}, fmt.Errorf("N64 ROM is too large: %d bytes", candidate.UncompressedSize64)
	}
	body, err := candidate.Open()
	if err != nil {
		return n64ROMInfo{}, fmt.Errorf("open N64 ROM entry: %w", err)
	}
	defer body.Close()
	checksums, format, err := validateAndChecksumN64ROM(body, candidate.UncompressedSize64)
	if err != nil {
		return n64ROMInfo{}, fmt.Errorf("N64 ROM entry %s: %w", candidate.Name, err)
	}
	return n64ROMInfo{
		name: canonicalN64ROMName(filepath.Base(filepath.Clean(strings.ReplaceAll(candidate.Name, `\`, "/"))), format), format: format,
		size: int64(candidate.UncompressedSize64), checksums: checksums,
	}, nil
}

func validateAndChecksumN64ROM(reader io.Reader, declaredSize uint64) (checksumPair, string, error) {
	if declaredSize < 4 || declaredSize > n64ROMMaxBytes {
		return checksumPair{}, "", fmt.Errorf("invalid N64 ROM size %d", declaredSize)
	}
	crc := crc32.NewIEEE()
	sha := sha1.New()
	var header [4]byte
	stream := io.TeeReader(io.LimitReader(reader, int64(declaredSize)+1), io.MultiWriter(crc, sha))
	if _, err := io.ReadFull(stream, header[:]); err != nil {
		return checksumPair{}, "", fmt.Errorf("read N64 header: %w", err)
	}
	format, ok := n64FormatForHeader(header)
	if !ok {
		return checksumPair{}, "", fmt.Errorf("invalid N64 ROM header % x", header)
	}
	written, err := io.Copy(io.Discard, stream)
	if err != nil {
		return checksumPair{}, "", err
	}
	if uint64(written+4) != declaredSize {
		return checksumPair{}, "", fmt.Errorf("N64 ROM size mismatch: read %d, expected %d", written+4, declaredSize)
	}
	return checksumPair{crc32: fmt.Sprintf("%08x", crc.Sum32()), sha1: hex.EncodeToString(sha.Sum(nil))}, format, nil
}

func isN64RawExt(ext string) bool {
	return ext == ".z64" || ext == ".v64" || ext == ".n64"
}

func n64FormatForHeader(header [4]byte) (string, bool) {
	formats := map[[4]byte]string{
		{0x80, 0x37, 0x12, 0x40}: "z64",
		{0x37, 0x80, 0x40, 0x12}: "v64",
		{0x40, 0x12, 0x37, 0x80}: "n64",
	}
	format, ok := formats[header]
	return format, ok
}

func canonicalN64ROMName(name, format string) string {
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext) + "." + format
}

func validateArchiveEntryName(name string) error {
	normalized := strings.ReplaceAll(strings.TrimSpace(name), `\`, "/")
	clean := filepath.Clean(filepath.FromSlash(normalized))
	hasWindowsDrive := len(normalized) >= 3 && normalized[1] == ':' && normalized[2] == '/'
	if normalized == "" || hasWindowsDrive || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("archive entry escapes root: %s", name)
	}
	return nil
}

func isIgnoredArchiveEntry(name string) bool {
	normalized := strings.Trim(strings.ReplaceAll(name, `\`, "/"), "/")
	for _, part := range strings.Split(normalized, "/") {
		if part == "__MACOSX" || part == ".DS_Store" || strings.HasPrefix(part, "._") {
			return true
		}
	}
	return false
}

var gdiTrackLinePattern = regexp.MustCompile(`^\s*\d+\s+\d+\s+\d+\s+\d+\s+(?:"([^"]+)"|(\S+))`)

func indexedGameFiles(path string, info fs.FileInfo, ext string) ([]domain.GameFile, int64, error) {
	entry := domain.GameFile{
		Name: filepath.Base(path), FilePath: path, Size: info.Size(), MTime: info.ModTime(), Role: "entry", Position: 0,
	}
	files := []domain.GameFile{entry}
	if !isMultiFileGameDescriptor(ext) {
		return files, info.Size(), nil
	}
	if ext == ".ccd" {
		return indexedCCDGameFiles(path, info)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	if ext == ".m3u" {
		return indexedM3UFiles(path, info, data)
	}
	names, err := discDescriptorDependencyNames(ext, data)
	if err != nil {
		return nil, 0, fmt.Errorf("parse %s descriptor %s: %w", strings.TrimPrefix(ext, "."), path, err)
	}
	totalSize := info.Size()
	dir := filepath.Dir(path)
	for index, name := range names {
		cleanName, err := cleanDiscDependencyName(name)
		if err != nil {
			return nil, 0, err
		}
		dependencyPath, err := resolveDiscDependencyPath(dir, cleanName)
		if err != nil {
			return nil, 0, fmt.Errorf("disc dependency %s: %w", name, err)
		}
		dependencyInfo, err := os.Stat(dependencyPath)
		if err != nil {
			return nil, 0, fmt.Errorf("disc dependency %s: %w", name, err)
		}
		if dependencyInfo.IsDir() {
			return nil, 0, fmt.Errorf("disc dependency is a directory: %s", name)
		}
		files = append(files, domain.GameFile{
			Name: filepath.ToSlash(cleanName), FilePath: dependencyPath, Size: dependencyInfo.Size(), MTime: dependencyInfo.ModTime(), Role: "dependency", Position: index + 1,
		})
		totalSize += dependencyInfo.Size()
	}
	return files, totalSize, nil
}

func indexedCCDGameFiles(path string, info fs.FileInfo) ([]domain.GameFile, int64, error) {
	files := []domain.GameFile{{Name: filepath.Base(path), FilePath: path, Size: info.Size(), MTime: info.ModTime(), Role: "entry", Position: 0}}
	totalSize := info.Size()
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	for _, suffix := range []string{".img", ".sub"} {
		dependencyPath, err := resolveDiscDependencyPath(filepath.Dir(path), base+suffix)
		if errors.Is(err, os.ErrNotExist) {
			if suffix == ".sub" {
				continue
			}
			return nil, 0, fmt.Errorf("ccd dependency %s: %w", base+suffix, err)
		}
		if err != nil {
			return nil, 0, err
		}
		dependencyInfo, err := os.Stat(dependencyPath)
		if err != nil {
			return nil, 0, err
		}
		files = append(files, domain.GameFile{Name: filepath.Base(dependencyPath), FilePath: dependencyPath, Size: dependencyInfo.Size(), MTime: dependencyInfo.ModTime(), Role: "dependency", Position: len(files)})
		totalSize += dependencyInfo.Size()
	}
	return files, totalSize, nil
}

func cleanDiscDependencyName(name string) (string, error) {
	normalized := strings.ReplaceAll(strings.TrimSpace(name), `\`, "/")
	if len(normalized) >= 3 && normalized[1] == ':' && normalized[2] == '/' {
		normalized = normalized[strings.LastIndex(normalized, "/")+1:]
	}
	cleanName := filepath.Clean(filepath.FromSlash(normalized))
	if filepath.IsAbs(cleanName) || cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("disc dependency escapes game directory: %s", name)
	}
	return cleanName, nil
}

func resolveDiscDependencyPath(dir string, cleanName string) (string, error) {
	current := dir
	for _, part := range strings.Split(filepath.ToSlash(cleanName), "/") {
		entries, err := os.ReadDir(current)
		if err != nil {
			return "", err
		}
		match := ""
		for _, entry := range entries {
			if strings.EqualFold(entry.Name(), part) {
				if match != "" && match != entry.Name() {
					return "", fmt.Errorf("ambiguous case-insensitive match for %s", cleanName)
				}
				match = entry.Name()
			}
		}
		if match == "" {
			return "", os.ErrNotExist
		}
		current = filepath.Join(current, match)
	}
	return current, nil
}

func isMultiFileGameDescriptor(ext string) bool {
	return ext == ".gdi" || ext == ".cue" || ext == ".ccd" || ext == ".toc" || ext == ".m3u" || ext == ".py1"
}

func discDescriptorDependencyNames(ext string, data []byte) ([]string, error) {
	switch ext {
	case ".gdi":
		return gdiDependencyNames(data)
	case ".cue":
		return cueDependencyNames(data)
	case ".ccd":
		return nil, nil
	case ".toc":
		return tocDependencyNames(data)
	case ".m3u":
		return m3uDependencyNames(data)
	case ".py1":
		descriptor, err := parseKonamiPython1Descriptor(data)
		if err != nil {
			return nil, err
		}
		return descriptor.Dependencies, nil
	default:
		return nil, fmt.Errorf("unsupported descriptor format %s", ext)
	}
}

type konamiPython1Descriptor struct {
	Name         string
	Dependencies []string
}

var konamiPython1DependencyKeys = []string{
	"cfimagepath",
	"bbsrampath",
	"iobootrompath",
	"ioconfigrompath",
	"internaldonglepath",
	"memorycarddonglepath",
	"memorycardidpath",
}

func readKonamiPython1Descriptor(path string) (konamiPython1Descriptor, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return konamiPython1Descriptor{}, err
	}
	descriptor, err := parseKonamiPython1Descriptor(data)
	if err != nil {
		return konamiPython1Descriptor{}, fmt.Errorf("parse py1 descriptor %s: %w", path, err)
	}
	return descriptor, nil
}

func parseKonamiPython1Descriptor(data []byte) (konamiPython1Descriptor, error) {
	values := make(map[string]string)
	inGameSection := false
	lines := strings.Split(strings.ReplaceAll(strings.TrimPrefix(string(data), "\ufeff"), "\r\n", "\n"), "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inGameSection = strings.EqualFold(strings.TrimSpace(line[1:len(line)-1]), "Game")
			continue
		}
		if !inGameSection {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if _, exists := values[key]; exists {
			return konamiPython1Descriptor{}, fmt.Errorf("contains duplicate %s", key)
		}
		values[key] = value
	}
	name := strings.TrimSpace(values["name"])
	if name == "" {
		return konamiPython1Descriptor{}, errors.New("contains no Game.Name")
	}
	dependencies := make([]string, 0, len(konamiPython1DependencyKeys))
	seen := make(map[string]struct{}, len(konamiPython1DependencyKeys))
	for _, key := range konamiPython1DependencyKeys {
		value := strings.TrimSpace(values[key])
		if value == "" {
			return konamiPython1Descriptor{}, fmt.Errorf("contains no Game.%s", key)
		}
		cleanName, err := cleanDiscDependencyName(value)
		if err != nil {
			return konamiPython1Descriptor{}, err
		}
		normalized := filepath.ToSlash(cleanName)
		lookupKey := strings.ToLower(normalized)
		if _, exists := seen[lookupKey]; exists {
			return konamiPython1Descriptor{}, fmt.Errorf("contains duplicate dependency %s", value)
		}
		seen[lookupKey] = struct{}{}
		dependencies = append(dependencies, normalized)
	}
	return konamiPython1Descriptor{Name: name, Dependencies: dependencies}, nil
}

func tocDependencyNames(data []byte) ([]string, error) {
	return cueDependencyNames(data)
}

func m3uDependencyNames(data []byte) ([]string, error) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimPrefix(string(data), "\ufeff"), "\r\n", "\n"), "\n")
	names := make([]string, 0)
	seen := map[string]struct{}{}
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" || strings.HasPrefix(name, "#") {
			continue
		}
		key := strings.ToLower(filepath.ToSlash(filepath.Clean(strings.ReplaceAll(name, `\`, "/"))))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("contains no disc entries")
	}
	return names, nil
}

func indexedM3UFiles(path string, info fs.FileInfo, data []byte) ([]domain.GameFile, int64, error) {
	names, err := m3uDependencyNames(data)
	if err != nil {
		return nil, 0, err
	}
	files := []domain.GameFile{{Name: filepath.Base(path), FilePath: path, Size: info.Size(), MTime: info.ModTime(), Role: "entry", Position: 0}}
	totalSize := info.Size()
	for _, name := range names {
		cleanName, err := cleanDiscDependencyName(name)
		if err != nil {
			return nil, 0, err
		}
		descriptorPath, err := resolveDiscDependencyPath(filepath.Dir(path), cleanName)
		if err != nil {
			return nil, 0, fmt.Errorf("m3u dependency %s: %w", name, err)
		}
		descriptorInfo, err := os.Stat(descriptorPath)
		if err != nil {
			return nil, 0, err
		}
		descriptorExt := strings.ToLower(filepath.Ext(descriptorPath))
		nested, _, err := indexedGameFiles(descriptorPath, descriptorInfo, descriptorExt)
		if err != nil {
			return nil, 0, err
		}
		for index, file := range nested {
			file.Role = "dependency"
			if index == 0 {
				file.Name = filepath.ToSlash(cleanName)
			}
			file.Position = len(files)
			files = append(files, file)
			totalSize += file.Size
		}
	}
	return files, totalSize, nil
}

func indexedVirtualM3UFiles(primaryPath string, discPaths []string) ([]domain.GameFile, int64, error) {
	entryName := multiDiscBaseName(filepath.Base(primaryPath)) + ".m3u"
	entryData := virtualM3UDataForPaths(discPaths)
	primaryInfo, err := os.Stat(primaryPath)
	if err != nil {
		return nil, 0, err
	}
	files := []domain.GameFile{{Name: entryName, FilePath: primaryPath, Size: int64(len(entryData)), MTime: primaryInfo.ModTime(), Role: "entry", Position: 0}}
	totalSize := int64(len(entryData))
	for _, descriptorPath := range discPaths {
		info, err := os.Stat(descriptorPath)
		if err != nil {
			return nil, 0, err
		}
		nested, _, err := indexedGameFiles(descriptorPath, info, strings.ToLower(filepath.Ext(descriptorPath)))
		if err != nil {
			return nil, 0, err
		}
		for _, file := range nested {
			file.Role = "dependency"
			file.Position = len(files)
			files = append(files, file)
			totalSize += file.Size
		}
	}
	return files, totalSize, nil
}

func virtualM3UDataForPaths(paths []string) []byte {
	lines := make([]string, 0, len(paths))
	for _, path := range paths {
		lines = append(lines, filepath.Base(path))
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

var multiDiscSuffixPattern = regexp.MustCompile(`(?i)\s*-?\s*disc\s*[a-z0-9]+$`)

func multiDiscBaseName(name string) string {
	name = strings.TrimSuffix(name, filepath.Ext(name))
	return strings.TrimSpace(multiDiscSuffixPattern.ReplaceAllString(name, ""))
}

func gdiDependencyNames(data []byte) ([]string, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty descriptor")
	}
	expected, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || expected <= 0 {
		return nil, fmt.Errorf("invalid track count")
	}
	names := make([]string, 0, expected)
	seen := map[string]struct{}{}
	for _, line := range lines[1:] {
		match := gdiTrackLinePattern.FindStringSubmatch(line)
		if len(match) == 0 {
			continue
		}
		name := match[1]
		if name == "" {
			name = match[2]
		}
		key := strings.ToLower(filepath.ToSlash(filepath.Clean(name)))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, name)
	}
	if len(names) != expected {
		return nil, fmt.Errorf("declares %d tracks but lists %d", expected, len(names))
	}
	return names, nil
}

var cueFileLinePattern = regexp.MustCompile(`(?i)^\s*FILE\s+(?:"([^"]+)"|(\S+))\s+\S+`)
var cueFileRewritePattern = regexp.MustCompile(`(?i)^(\s*FILE\s+)(?:"([^"]+)"|(\S+))(\s+\S+.*)$`)

func cueDependencyNames(data []byte) ([]string, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	names := make([]string, 0)
	seen := map[string]struct{}{}
	for _, line := range lines {
		match := cueFileLinePattern.FindStringSubmatch(line)
		if len(match) == 0 {
			continue
		}
		name := match[1]
		if name == "" {
			name = match[2]
		}
		key := strings.ToLower(filepath.ToSlash(filepath.Clean(strings.ReplaceAll(name, `\`, "/"))))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("contains no FILE directives")
	}
	return names, nil
}

func NormalizeCUEFileReferences(data []byte) ([]byte, error) {
	lineBreak := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		lineBreak = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	matched := 0
	for index, line := range lines {
		match := cueFileRewritePattern.FindStringSubmatch(line)
		if len(match) == 0 {
			continue
		}
		name := match[2]
		if name == "" {
			name = match[3]
		}
		cleanName, err := cleanDiscDependencyName(name)
		if err != nil {
			return nil, err
		}
		lines[index] = match[1] + `"` + filepath.ToSlash(cleanName) + `"` + match[4]
		matched++
	}
	if matched == 0 {
		return nil, fmt.Errorf("contains no FILE directives")
	}
	return []byte(strings.Join(lines, lineBreak)), nil
}

type gamelistXML struct {
	Games []gamelistGame `xml:"game"`
}

type pegasusGame struct {
	Name        string
	Description string
	Developer   string
	Files       []string
}

type pegasusMetadata struct {
	GamesByFile map[string]pegasusGame
	Ignored     map[string]struct{}
}

func readPegasusMetadata(rootPath string) (pegasusMetadata, bool) {
	path := filepath.Join(rootPath, "metadata.pegasus.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		return pegasusMetadata{}, false
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	metadata := pegasusMetadata{GamesByFile: map[string]pegasusGame{}, Ignored: map[string]struct{}{}}
	var current *pegasusGame
	flush := func() {
		if current == nil {
			return
		}
		for _, file := range current.Files {
			metadata.GamesByFile[strings.ToLower(filepath.ToSlash(filepath.Clean(strings.TrimSpace(file))))] = *current
		}
	}
	lastField := ""
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, ":") && current != nil && lastField == "description" {
			current.Description = strings.TrimSpace(current.Description + " " + line)
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		lastField = key
		switch key {
		case "game":
			flush()
			current = &pegasusGame{Name: value}
		case "file":
			if current != nil && value != "" {
				current.Files = append(current.Files, value)
			}
		case "description":
			if current != nil {
				current.Description = value
			}
		case "developer":
			if current != nil {
				current.Developer = value
			}
		case "ignore-file":
			if value != "" {
				metadata.Ignored[strings.ToLower(filepath.ToSlash(filepath.Clean(value)))] = struct{}{}
			}
		}
	}
	flush()
	return metadata, true
}

func pegasusEntryForGame(rootPath string, gamePath string) (pegasusGame, bool) {
	for dir := filepath.Dir(gamePath); ; dir = filepath.Dir(dir) {
		metadata, ok := readPegasusMetadata(dir)
		if ok {
			relPath, err := filepath.Rel(dir, gamePath)
			if err == nil {
				entry, found := metadata.GamesByFile[strings.ToLower(filepath.ToSlash(filepath.Clean(relPath)))]
				if found {
					return entry, true
				}
			}
		}
		if filepath.Clean(dir) == filepath.Clean(rootPath) || filepath.Dir(dir) == dir {
			break
		}
	}
	return pegasusGame{}, false
}

func isPegasusIgnoredFile(path string) bool {
	metadata, ok := readPegasusMetadata(filepath.Dir(path))
	if !ok {
		return false
	}
	_, ignored := metadata.Ignored[strings.ToLower(filepath.Base(path))]
	return ignored
}

func pegasusMultiDiscPaths(rootPath string, primaryPath string) []string {
	metadataRoot := filepath.Dir(primaryPath)
	metadata, ok := readPegasusMetadata(metadataRoot)
	if !ok {
		return nil
	}
	base := strings.ToLower(multiDiscBaseName(filepath.Base(primaryPath)))
	if base == strings.ToLower(strings.TrimSuffix(filepath.Base(primaryPath), filepath.Ext(primaryPath))) {
		return nil
	}
	paths := []string{primaryPath}
	for ignored := range metadata.Ignored {
		if strings.ToLower(filepath.Ext(ignored)) != ".cue" || strings.ToLower(multiDiscBaseName(filepath.Base(ignored))) != base {
			continue
		}
		resolved, err := resolveDiscDependencyPath(metadataRoot, filepath.FromSlash(ignored))
		if err == nil && filepath.Clean(resolved) != filepath.Clean(primaryPath) {
			paths = append(paths, resolved)
		}
	}
	sort.Slice(paths[1:], func(i, j int) bool { return strings.ToLower(paths[i+1]) < strings.ToLower(paths[j+1]) })
	return paths
}

var pcfxDiscDirectorySuffixPattern = regexp.MustCompile(`(?i)\s+CD\s*([0-9]+)$`)

func isPCFXSecondaryDiscPath(path string) bool {
	match := pcfxDiscDirectorySuffixPattern.FindStringSubmatch(filepath.Base(filepath.Dir(path)))
	return len(match) == 2 && match[1] != "1"
}

func pcfxDiscSetPaths(rootPath string, primaryPath string) []string {
	if paths := pegasusMultiDiscPaths(rootPath, primaryPath); len(paths) > 1 {
		return paths
	}
	dir := filepath.Dir(primaryPath)
	match := pcfxDiscDirectorySuffixPattern.FindStringSubmatch(filepath.Base(dir))
	if len(match) != 2 || match[1] != "1" {
		return nil
	}
	dirBase := pcfxDiscDirectorySuffixPattern.ReplaceAllString(filepath.Base(dir), "")
	parent := filepath.Dir(dir)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	type discPath struct {
		number int
		path   string
	}
	discs := []discPath{{number: 1, path: primaryPath}}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidateMatch := pcfxDiscDirectorySuffixPattern.FindStringSubmatch(entry.Name())
		if len(candidateMatch) != 2 || !strings.EqualFold(pcfxDiscDirectorySuffixPattern.ReplaceAllString(entry.Name(), ""), dirBase) {
			continue
		}
		number, _ := strconv.Atoi(candidateMatch[1])
		if number <= 1 {
			continue
		}
		cueEntries, _ := filepath.Glob(filepath.Join(parent, entry.Name(), "*.[cC][uU][eE]"))
		if len(cueEntries) == 1 {
			discs = append(discs, discPath{number: number, path: cueEntries[0]})
		}
	}
	if len(discs) <= 1 {
		return nil
	}
	sort.Slice(discs, func(i, j int) bool { return discs[i].number < discs[j].number })
	paths := make([]string, len(discs))
	for index, disc := range discs {
		paths[index] = disc.path
	}
	return paths
}

func pcfxDirectoryTitle(rootPath string, gamePath string) string {
	dir := filepath.Base(filepath.Dir(gamePath))
	if dir == filepath.Base(filepath.Clean(rootPath)) {
		return gameTitle(gamePath)
	}
	dir = pcfxDiscDirectorySuffixPattern.ReplaceAllString(dir, "")
	dir = regexp.MustCompile(`^\d{8}\s+`).ReplaceAllString(dir, "")
	dir = regexp.MustCompile(`\([^)]*\)\s*$`).ReplaceAllString(dir, "")
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return gameTitle(gamePath)
	}
	return dir
}

func (s *Scanner) applyPegasusMetadata(gameID int64, entry pegasusGame, ok bool) error {
	if !ok {
		return nil
	}
	developers := []string{}
	if strings.TrimSpace(entry.Developer) != "" {
		developers = []string{strings.TrimSpace(entry.Developer)}
	}
	return s.store.UpsertGameMetadata(domain.GameMetadata{
		GameID: gameID, DisplayTitle: strings.TrimSpace(entry.Name), Summary: strings.TrimSpace(entry.Description), Developers: developers,
	})
}

type gamelistGame struct {
	Path        string `xml:"path" json:"path,omitempty"`
	Name        string `xml:"name" json:"name,omitempty"`
	Desc        string `xml:"desc" json:"desc,omitempty"`
	ReleaseDate string `xml:"releasedate" json:"releasedate,omitempty"`
	Developer   string `xml:"developer" json:"developer,omitempty"`
	Publisher   string `xml:"publisher" json:"publisher,omitempty"`
	Genre       string `xml:"genre" json:"genre,omitempty"`
	Players     string `xml:"players" json:"players,omitempty"`
	Image       string `xml:"image" json:"image,omitempty"`
	Thumbnail   string `xml:"thumbnail" json:"thumbnail,omitempty"`
	Marquee     string `xml:"marquee" json:"marquee,omitempty"`
	Screenshot  string `xml:"screenshot" json:"screenshot,omitempty"`
	TitleScreen string `xml:"title_screen" json:"title_screen,omitempty"`
	Manual      string `xml:"manual" json:"manual,omitempty"`
}

type gamelistIndex struct {
	rootPath     string
	gamelistPath string
	mtime        time.Time
	size         int64
	games        map[string]gamelistGame
}

func (s *Scanner) applyGamelistMetadata(library domain.Library, gamePath string, relPath string, gameID int64) error {
	index, entry, ok := s.gamelistEntryForGame(library.RootPath, gamePath, relPath)
	if !ok {
		return nil
	}
	if hasGamelistMetadata(entry) {
		if err := s.store.UpsertGameMetadata(domain.GameMetadata{
			GameID:       gameID,
			DisplayTitle: strings.TrimSpace(entry.Name),
			Summary:      strings.TrimSpace(entry.Desc),
			ReleaseDate:  normalizeGamelistReleaseDate(entry.ReleaseDate),
			Genres:       splitGamelistValues(entry.Genre),
			Developers:   splitGamelistValues(entry.Developer),
			Publishers:   splitGamelistValues(entry.Publisher),
			Players:      strings.TrimSpace(entry.Players),
		}); err != nil {
			return err
		}
	}
	rawJSON, _ := json.Marshal(entry)
	if _, err := s.store.UpsertGameMetadataSource(domain.GameMetadataSource{
		GameID:     gameID,
		Source:     "gamelist",
		SourceID:   filepath.ToSlash(relPath),
		MatchedBy:  "path",
		Confidence: 1,
		RawJSON:    string(rawJSON),
	}); err != nil {
		return err
	}

	for _, item := range []struct {
		kind     string
		rawPath  string
		selected bool
	}{
		{kind: "cover", rawPath: entry.Image, selected: true},
		{kind: "thumbnail", rawPath: entry.Thumbnail},
		{kind: "marquee", rawPath: entry.Marquee},
		{kind: "screenshot", rawPath: entry.Screenshot},
		{kind: "title_screen", rawPath: entry.TitleScreen},
		{kind: "manual", rawPath: entry.Manual},
	} {
		cachePath, ok := resolveGamelistPath(library.RootPath, filepath.Dir(index.gamelistPath), item.rawPath)
		if !ok {
			continue
		}
		info, err := os.Stat(cachePath)
		if err != nil || info.IsDir() {
			continue
		}
		if _, err := s.store.UpsertGameArtwork(domain.GameArtwork{
			GameID:     gameID,
			Source:     "gamelist",
			Kind:       item.kind,
			CachePath:  cachePath,
			Selected:   item.selected,
			Confidence: 1,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scanner) gamelistEntryForGame(libraryRoot string, gamePath string, relPath string) (gamelistIndex, gamelistGame, bool) {
	rootPath := filepath.Clean(libraryRoot)
	gameDir := filepath.Dir(gamePath)
	searchDirs := []string{gameDir}
	if filepath.Clean(gameDir) != rootPath {
		searchDirs = append(searchDirs, rootPath)
	}
	relPath = filepath.ToSlash(relPath)
	for _, dir := range searchDirs {
		index, ok := s.gamelistIndexForDirectory(rootPath, dir)
		if !ok {
			continue
		}
		entry, ok := index.games[relPath]
		if ok {
			return index, entry, true
		}
	}
	return gamelistIndex{}, gamelistGame{}, false
}

func (s *Scanner) gamelistIndexForDirectory(libraryRoot string, dir string) (gamelistIndex, bool) {
	gamelistPath := filepath.Join(dir, "gamelist.xml")
	info, err := os.Stat(gamelistPath)
	if err != nil || info.IsDir() {
		return gamelistIndex{}, false
	}
	if cached, ok := s.gamelistCache.Load(gamelistPath); ok {
		index := cached.(gamelistIndex)
		if index.mtime.Equal(info.ModTime()) && index.size == info.Size() && index.rootPath == filepath.Clean(libraryRoot) {
			return index, len(index.games) > 0
		}
	}
	index, err := parseGamelistIndex(libraryRoot, gamelistPath, info)
	if err != nil {
		return gamelistIndex{}, false
	}
	s.gamelistCache.Store(gamelistPath, index)
	return index, len(index.games) > 0
}

func parseGamelistIndex(libraryRoot string, gamelistPath string, info os.FileInfo) (gamelistIndex, error) {
	data, err := os.ReadFile(gamelistPath)
	if err != nil {
		return gamelistIndex{}, err
	}
	var parsed gamelistXML
	if err := xml.Unmarshal(data, &parsed); err != nil {
		return gamelistIndex{}, err
	}
	baseDir := filepath.Dir(gamelistPath)
	index := gamelistIndex{
		rootPath:     filepath.Clean(libraryRoot),
		gamelistPath: gamelistPath,
		mtime:        info.ModTime(),
		size:         info.Size(),
		games:        map[string]gamelistGame{},
	}
	for _, game := range parsed.Games {
		_, relPath, ok := resolveGamelistPathWithRel(libraryRoot, baseDir, game.Path)
		if !ok || relPath == "" {
			continue
		}
		index.games[relPath] = game
	}
	return index, nil
}

func hasGamelistMetadata(game gamelistGame) bool {
	return strings.TrimSpace(game.Name) != "" ||
		strings.TrimSpace(game.Desc) != "" ||
		strings.TrimSpace(game.ReleaseDate) != "" ||
		strings.TrimSpace(game.Developer) != "" ||
		strings.TrimSpace(game.Publisher) != "" ||
		strings.TrimSpace(game.Genre) != "" ||
		strings.TrimSpace(game.Players) != ""
}

func resolveGamelistPath(libraryRoot string, baseDir string, rawPath string) (string, bool) {
	path, _, ok := resolveGamelistPathWithRel(libraryRoot, baseDir, rawPath)
	return path, ok
}

func resolveGamelistPathWithRel(libraryRoot string, baseDir string, rawPath string) (string, string, bool) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "", "", false
	}
	rawPath = strings.ReplaceAll(rawPath, "\\", "/")
	var absPath string
	if filepath.IsAbs(rawPath) {
		absPath = filepath.Clean(rawPath)
	} else {
		absPath = filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(rawPath)))
	}
	rootPath := filepath.Clean(libraryRoot)
	relPath, err := filepath.Rel(rootPath, absPath)
	if err != nil || relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) || filepath.IsAbs(relPath) {
		return "", "", false
	}
	return absPath, filepath.ToSlash(relPath), true
}

func normalizeGamelistReleaseDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 8 && allDigits(value[:8]) {
		return value[:4] + "-" + value[4:6] + "-" + value[6:8]
	}
	return value
}

func allDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

func splitGamelistValues(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';'
	})
	values := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		values = append(values, item)
	}
	return values
}

func (s *Scanner) indexVideoFile(library domain.Library, path string, info fs.FileInfo, ext string) error {
	relPath, err := filepath.Rel(library.RootPath, path)
	if err != nil {
		return fmt.Errorf("relative path: %w", err)
	}
	metadata := probeVideoMetadata(path)
	_, err = s.store.UpsertVideo(domain.VideoAsset{
		LibraryID:       library.ID,
		Title:           mediaTitle(path),
		Format:          strings.TrimPrefix(ext, "."),
		FilePath:        path,
		RelPath:         filepath.ToSlash(relPath),
		Size:            info.Size(),
		MTime:           info.ModTime(),
		DurationSeconds: metadata.durationSeconds,
		Width:           metadata.width,
		Height:          metadata.height,
		VideoCodec:      metadata.videoCodec,
		AudioCodec:      metadata.audioCodec,
		ThumbnailStatus: "placeholder",
	})
	if err != nil {
		return err
	}
	return s.refreshVideoLocalMetadata(path)
}

type videoProbeMetadata struct {
	durationSeconds float64
	width           int
	height          int
	videoCodec      string
	audioCodec      string
}

func probeVideoMetadata(path string) videoProbeMetadata {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return videoProbeMetadata{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-print_format", "json", "-show_format", "-show_streams", path).Output()
	if err != nil {
		return videoProbeMetadata{}
	}
	var payload struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			Duration  string `json:"duration"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return videoProbeMetadata{}
	}
	metadata := videoProbeMetadata{durationSeconds: parseProbeDuration(payload.Format.Duration)}
	for _, stream := range payload.Streams {
		switch stream.CodecType {
		case "video":
			if metadata.videoCodec == "" {
				metadata.videoCodec = strings.ToLower(strings.TrimSpace(stream.CodecName))
				metadata.width = stream.Width
				metadata.height = stream.Height
				if metadata.durationSeconds == 0 {
					metadata.durationSeconds = parseProbeDuration(stream.Duration)
				}
			}
		case "audio":
			if metadata.audioCodec == "" {
				metadata.audioCodec = strings.ToLower(strings.TrimSpace(stream.CodecName))
			}
		}
	}
	return metadata
}

func parseProbeDuration(value string) float64 {
	duration, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || duration < 0 {
		return 0
	}
	return duration
}

func listBookPages(path string, ext string) ([]domain.Page, error) {
	if ext == ".epub" {
		return archive.ListEPUBSpine(path)
	}
	if ext == ".pdf" {
		return []domain.Page{{Index: 0, Name: filepath.Base(path)}}, nil
	}
	return archive.ListPages(path)
}

type indexedBookResult struct {
	Book            domain.Book
	MetadataUpdated bool
	Reclassified    bool
}

func (s *Scanner) indexFileMetadata(library domain.Library, path string, info fs.FileInfo, ext string, resolver *bookmeta.Resolver) (indexedBookResult, error) {
	if resolver != nil {
		metadata, found, err := resolver.Lookup(path)
		if found {
			return s.indexResolvedFileMetadata(library, path, info, ext, importedBookMetadata(path, metadata))
		}
		if err != nil {
			if _, lookupErr := s.store.FileIndexByPath(path); !errors.Is(lookupErr, sql.ErrNoRows) {
				return indexedBookResult{}, err
			}
		}
	}
	metadata, err := bookMetadataForPath(path, ext)
	if err != nil {
		return indexedBookResult{}, err
	}
	return s.indexResolvedFileMetadata(library, path, info, ext, metadata)
}

func (s *Scanner) indexResolvedFileMetadata(library domain.Library, path string, info fs.FileInfo, ext string, metadata bookMetadata) (indexedBookResult, error) {
	// Title disambiguation can rename another book. Serialize that decision with
	// the identity update so concurrent formats cannot collapse distinct books.
	s.bookIndexMu.Lock()
	defer s.bookIndexMu.Unlock()
	relPath, err := filepath.Rel(library.RootPath, path)
	if err != nil {
		return indexedBookResult{}, fmt.Errorf("relative path: %w", err)
	}

	format := strings.TrimPrefix(ext, ".")
	existing, existingErr := s.store.FileIndexByPath(path)
	if existingErr == nil && metadata.SourceRevision == "" {
		// A removed sidecar is not permission to replace imported/user metadata
		// with embedded fallback. Keep the last source baseline for its return.
		_, stateErr := s.store.Setting(importedBookStateKey(existing.Book.ID))
		if stateErr == nil {
			if existing.File.Size != info.Size() || existing.File.Ext != ext || !existing.File.MTime.Equal(info.ModTime()) {
				_, err = s.store.UpsertFile(existing.Book.ID, library.ID, path, relPath, info.Size(), info.ModTime(), ext)
			}
			return indexedBookResult{Book: existing.Book}, err
		}
		if !errors.Is(stateErr, sql.ErrNoRows) {
			return indexedBookResult{}, stateErr
		}
	}
	expectedSeriesTitle, _ := bookSeriesIdentity(library, relPath, metadata)
	if existingErr == nil && existing.File.Size == info.Size() && existing.File.Ext == ext && existing.File.MTime.Equal(info.ModTime()) && s.importedBookUnchanged(existing, metadata, expectedSeriesTitle) {
		return indexedBookResult{Book: existing.Book}, nil
	}
	var importedState *importedBookState
	if existingErr == nil && metadata.SourceRevision != "" {
		metadata, importedState, err = s.mergeImportedBookMetadata(library, path, relPath, ext, existing, metadata)
		if err != nil {
			return indexedBookResult{}, err
		}
	}

	seriesTitle, seriesDirectoryPath := bookSeriesIdentity(library, relPath, metadata)
	var series domain.Series
	if importedState != nil && importedState.preserves("series") {
		series, err = s.store.SeriesByID(existing.Book.SeriesID)
	} else {
		series, err = s.store.UpsertSeries(library.ID, seriesTitle, seriesDirectoryPath)
	}
	if err != nil {
		return indexedBookResult{}, err
	}

	var book domain.Book
	if existingErr == nil {
		previous, previousErr := s.store.BookSharedMetadata(existing.File.BookID)
		title, err := s.disambiguateBookTitle(library, series.ID, metadata.Title, format, relPath, existing.File.BookID)
		if err != nil {
			return indexedBookResult{}, err
		}
		book, err = s.store.UpdateBookIdentity(existing.File.BookID, series.ID, title, format)
		if err != nil {
			return indexedBookResult{}, err
		}
		result := indexedBookResult{Book: book}
		if previousErr == nil {
			result.MetadataUpdated = previous.Title != title || previous.Creator != metadata.Creator || previous.Description != metadata.Description || !sameStringList(previous.Tags, metadata.Tags)
			result.Reclassified = previous.SeriesID != series.ID
		}
		if metadata.SourceRevision != "" {
			result.MetadataUpdated = true
		}
		book, err = s.store.UpdateBookMetadata(book.ID, metadata.Creator, metadata.Description, metadata.Tags)
		if err != nil {
			return indexedBookResult{}, err
		}
		result.Book = book
		_, err = s.store.UpsertFile(book.ID, library.ID, path, relPath, info.Size(), info.ModTime(), ext)
		if err != nil {
			return indexedBookResult{}, err
		}
		if err := s.rememberImportedBook(book, metadata, importedState); err != nil {
			return indexedBookResult{}, err
		}
		return result, nil
	} else if !errors.Is(existingErr, sql.ErrNoRows) {
		return indexedBookResult{}, existingErr
	} else {
		title, titleErr := s.disambiguateBookTitle(library, series.ID, metadata.Title, format, relPath, 0)
		if titleErr != nil {
			return indexedBookResult{}, titleErr
		}
		book, err = s.store.UpsertBook(series.ID, title, format)
		if err != nil {
			return indexedBookResult{}, err
		}
	}
	book, err = s.store.UpdateBookMetadata(book.ID, metadata.Creator, metadata.Description, metadata.Tags)
	if err != nil {
		return indexedBookResult{}, err
	}
	_, err = s.store.UpsertFile(book.ID, library.ID, path, relPath, info.Size(), info.ModTime(), ext)
	if err != nil {
		return indexedBookResult{}, err
	}
	if err := s.rememberImportedBook(book, metadata, nil); err != nil {
		return indexedBookResult{}, err
	}
	return indexedBookResult{Book: book}, nil
}

func bookSeriesIdentity(library domain.Library, relPath string, metadata bookMetadata) (string, string) {
	seriesTitle, directoryPath := seriesIdentityForRelPath(library.RootPath, relPath)
	if metadata.Creator != "" && !metadata.DirectoryCollection && !strings.EqualFold(strings.TrimSpace(library.AssetType), "comic") {
		seriesTitle = metadata.Creator
		directoryPath = filepath.ToSlash(filepath.Dir(relPath))
		if directoryPath == "." || directoryPath == "/" {
			directoryPath = "."
		}
	}
	return seriesTitle, directoryPath
}

func (s *Scanner) indexBasicFileMetadata(library domain.Library, path string, info fs.FileInfo, ext string, resolver *bookmeta.Resolver) (indexedBookResult, error) {
	metadata := bookMetadata{Title: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))}
	if resolver != nil {
		external, found, err := resolver.Lookup(path)
		if found {
			metadata = importedBookMetadata(path, external)
		} else if err != nil {
			if _, lookupErr := s.store.FileIndexByPath(path); !errors.Is(lookupErr, sql.ErrNoRows) {
				return indexedBookResult{}, err
			}
		}
	}
	return s.indexResolvedFileMetadata(library, path, info, ext, metadata)
}

func (s *Scanner) disambiguateBookTitle(library domain.Library, seriesID int64, title string, format string, relPath string, currentBookID int64) (string, error) {
	existing, err := s.store.BookBySeriesTitle(seriesID, title, format)
	if err != nil {
		return title, nil
	}
	if existing.ID == currentBookID {
		return title, nil
	}
	// A collision must not rename another book's manually preserved title.
	var importedState importedBookState
	if raw, err := s.store.Setting(importedBookStateKey(existing.ID)); err == nil {
		_ = json.Unmarshal([]byte(raw), &importedState)
	}
	preserveExistingTitle := importedState.preserves("title") || (importedState.Title != "" && importedState.Title != existing.Title)
	if importedState.Revision == "" {
		legacy, legacyErr := bookMetadataForPath(existing.FilePath, "."+format)
		legacyRel, relErr := filepath.Rel(library.RootPath, existing.FilePath)
		preserveExistingTitle = legacyErr != nil || relErr != nil || (existing.Title != legacy.Title && existing.Title != disambiguatedTitle(legacy.Title, legacyRel))
	}
	if preserveExistingTitle {
		return s.availableDisambiguatedBookTitle(seriesID, title, format, relPath, currentBookID)
	}

	existingRelPath := existing.FilePath
	if rel, relErr := filepath.Rel(library.RootPath, existing.FilePath); relErr == nil {
		existingRelPath = rel
	}
	existingTitle := disambiguatedTitle(title, existingRelPath)
	if existingTitle != existing.Title {
		if _, err := s.store.UpdateBookIdentity(existing.ID, seriesID, existingTitle, format); err != nil {
			return "", err
		}
		if err := s.rememberImportedBookTitle(existing.ID, existingTitle); err != nil {
			return "", err
		}
	}
	currentTitle := disambiguatedTitle(title, relPath)
	if currentTitle == existingTitle {
		currentTitle = title + " (" + strings.TrimSpace(filepath.ToSlash(filepath.Dir(relPath))) + ")"
	}
	return currentTitle, nil
}

func (s *Scanner) availableDisambiguatedBookTitle(seriesID int64, title, format, relPath string, currentBookID int64) (string, error) {
	base := disambiguatedTitle(title, relPath)
	if base == title {
		base = title + " (" + filepath.ToSlash(relPath) + ")"
	}
	for suffix := 1; ; suffix++ {
		candidate := base
		if suffix > 1 {
			candidate = fmt.Sprintf("%s (%d)", base, suffix)
		}
		other, err := s.store.BookBySeriesTitle(seriesID, candidate, format)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && other.ID == currentBookID) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
	}
}

func disambiguatedTitle(title string, relPath string) string {
	dir := filepath.Base(filepath.Dir(filepath.ToSlash(relPath)))
	if dir == "." || dir == "/" || dir == "" {
		return title
	}
	if matches := regexp.MustCompile(`\((\d+)\)\s*$`).FindStringSubmatch(dir); len(matches) == 2 {
		return title + " (" + matches[1] + ")"
	}
	return title + " (" + dir + ")"
}

type bookMetadata struct {
	Title               string
	Creator             string
	Description         string
	Tags                []string
	DirectoryCollection bool
	SourceRevision      string
}

func bookMetadataForPath(path string, ext string) (bookMetadata, error) {
	fallback := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if ext != ".epub" {
		metadata := bookMetadata{Title: fallback}
		if ext == ".pdf" {
			return readPDFMetadata(path, metadata)
		}
		if ext == ".zip" || ext == ".cbz" {
			embedded, ok, err := archive.ReadEmbeddedComicMetadata(path)
			if err != nil {
				return bookMetadata{}, err
			}
			if ok {
				if embedded.Title != "" {
					metadata.Title = embedded.Title
				}
				metadata.Creator = embedded.Creator
				metadata.Description = embedded.Description
				metadata.Tags = embedded.Tags
			}
		}
		return metadata, nil
	}
	manifest, err := archive.ReadEPUBManifest(path)
	if err != nil {
		return bookMetadata{}, err
	}
	metadata := bookMetadata{
		Title:       fallback,
		Creator:     strings.TrimSpace(manifest.Creator),
		Description: strings.TrimSpace(manifest.Description),
	}
	if title := strings.TrimSpace(manifest.Title); title != "" {
		metadata.Title = title
	}
	return metadata, nil
}

func readPDFMetadata(path string, fallback bookMetadata) (bookMetadata, error) {
	data, err := readPDFMetadataWindow(path)
	if err != nil {
		return bookMetadata{}, err
	}
	metadata := fallback
	info, hasInfoRef := pdfInfoDictionary(data)
	if len(info) == 0 {
		if hasInfoRef {
			return metadata, nil
		}
		info = data
	}
	if title := pdfInfoText(info, "Title"); title != "" {
		metadata.Title = title
	}
	if author := pdfInfoText(info, "Author"); author != "" {
		metadata.Creator = author
	}
	if subject := pdfInfoText(info, "Subject"); subject != "" {
		metadata.Description = subject
	}
	return metadata, nil
}

func readPDFMetadataWindow(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	const windowSize int64 = 2 << 20
	if info.Size() <= windowSize*2 {
		return io.ReadAll(file)
	}
	head := make([]byte, windowSize)
	if _, err := io.ReadFull(file, head); err != nil {
		return nil, err
	}
	tail := make([]byte, windowSize)
	if _, err := file.ReadAt(tail, info.Size()-windowSize); err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(head)+len(tail)+1)
	out = append(out, head...)
	out = append(out, '\n')
	out = append(out, tail...)
	return out, nil
}

func pdfInfoDictionary(data []byte) ([]byte, bool) {
	pattern := regexp.MustCompile(`/Info\s+(\d+)\s+(\d+)\s+R`)
	matches := pattern.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return nil, false
	}
	match := matches[len(matches)-1]
	objectPattern := regexp.MustCompile(`(^|[\x00\t\n\f\r ])` + regexp.QuoteMeta(string(match[1])) + `\s+` + regexp.QuoteMeta(string(match[2])) + `\s+obj\b`)
	objectMatch := objectPattern.FindIndex(data)
	if len(objectMatch) == 0 {
		return nil, true
	}
	return extractPDFDictionary(data[objectMatch[1]:]), true
}

func extractPDFDictionary(data []byte) []byte {
	start := bytes.Index(data, []byte("<<"))
	if start < 0 {
		return nil
	}
	depth := 0
	for i := start; i < len(data)-1; i++ {
		switch data[i] {
		case '(':
			i = skipPDFLiteralString(data, i)
		case '<':
			if data[i+1] == '<' {
				depth++
				i++
				continue
			}
			i = skipPDFHexString(data, i)
		case '>':
			if data[i+1] == '>' {
				depth--
				i++
				if depth == 0 {
					return data[start : i+1]
				}
			}
		}
	}
	return nil
}

func skipPDFLiteralString(data []byte, start int) int {
	depth := 1
	for i := start + 1; i < len(data); i++ {
		switch data[i] {
		case '\\':
			if i+1 < len(data) {
				i++
			}
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(data) - 1
}

func skipPDFHexString(data []byte, start int) int {
	for i := start + 1; i < len(data); i++ {
		if data[i] == '>' {
			return i
		}
	}
	return len(data) - 1
}

func pdfInfoText(data []byte, key string) string {
	name := []byte("/" + key)
	for offset := 0; offset < len(data); {
		index := bytes.Index(data[offset:], name)
		if index < 0 {
			return ""
		}
		index += offset
		valueStart := index + len(name)
		if valueStart < len(data) && !isPDFDelimiter(data[valueStart]) {
			offset = valueStart
			continue
		}
		valueStart = skipPDFWhitespace(data, valueStart)
		var raw []byte
		var ok bool
		switch {
		case valueStart < len(data) && data[valueStart] == '(':
			raw, _, ok = parsePDFLiteralString(data, valueStart)
		case valueStart+1 < len(data) && data[valueStart] == '<' && data[valueStart+1] != '<':
			raw, _, ok = parsePDFHexString(data, valueStart)
		}
		if !ok {
			offset = valueStart + 1
			continue
		}
		return strings.TrimSpace(decodePDFTextString(raw))
	}
	return ""
}

func isPDFDelimiter(b byte) bool {
	switch b {
	case 0, '\t', '\n', '\f', '\r', ' ', '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	default:
		return false
	}
}

func skipPDFWhitespace(data []byte, start int) int {
	for start < len(data) {
		switch data[start] {
		case 0, '\t', '\n', '\f', '\r', ' ':
			start++
		default:
			return start
		}
	}
	return start
}

func parsePDFLiteralString(data []byte, start int) ([]byte, int, bool) {
	if start >= len(data) || data[start] != '(' {
		return nil, start, false
	}
	raw := make([]byte, 0)
	depth := 1
	for i := start + 1; i < len(data); i++ {
		if data[i] != '\\' {
			switch data[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					return raw, i + 1, true
				}
			}
			raw = append(raw, data[i])
			continue
		}
		i++
		if i >= len(data) {
			break
		}
		switch data[i] {
		case 'n':
			raw = append(raw, '\n')
		case 'r':
			raw = append(raw, '\r')
		case 't':
			raw = append(raw, '\t')
		case 'b':
			raw = append(raw, '\b')
		case 'f':
			raw = append(raw, '\f')
		case '(', ')', '\\':
			raw = append(raw, data[i])
		case '\n':
		case '\r':
			if i+1 < len(data) && data[i+1] == '\n' {
				i++
			}
		default:
			if data[i] >= '0' && data[i] <= '7' {
				value := int(data[i] - '0')
				for count := 0; count < 2 && i+1 < len(data) && data[i+1] >= '0' && data[i+1] <= '7'; count++ {
					i++
					value = value*8 + int(data[i]-'0')
				}
				raw = append(raw, byte(value))
			} else {
				raw = append(raw, data[i])
			}
		}
	}
	return raw, len(data), false
}

func parsePDFHexString(data []byte, start int) ([]byte, int, bool) {
	if start >= len(data) || data[start] != '<' {
		return nil, start, false
	}
	digits := make([]byte, 0)
	for i := start + 1; i < len(data); i++ {
		if data[i] == '>' {
			if len(digits)%2 == 1 {
				digits = append(digits, '0')
			}
			out := make([]byte, hex.DecodedLen(len(digits)))
			if _, err := hex.Decode(out, digits); err != nil {
				return nil, i + 1, false
			}
			return out, i + 1, true
		}
		if isPDFWhitespace(data[i]) {
			continue
		}
		digits = append(digits, data[i])
	}
	return nil, len(data), false
}

func isPDFWhitespace(b byte) bool {
	switch b {
	case 0, '\t', '\n', '\f', '\r', ' ':
		return true
	default:
		return false
	}
}

func decodePDFTextString(raw []byte) string {
	switch {
	case len(raw) >= 2 && raw[0] == 0xfe && raw[1] == 0xff:
		return decodeUTF16(raw[2:], true)
	case len(raw) >= 2 && raw[0] == 0xff && raw[1] == 0xfe:
		return decodeUTF16(raw[2:], false)
	case len(raw) >= 3 && raw[0] == 0xef && raw[1] == 0xbb && raw[2] == 0xbf:
		return string(raw[3:])
	case utf8.Valid(raw):
		return string(raw)
	default:
		return decodePDFDocEncoding(raw)
	}
}

func decodeUTF16(raw []byte, bigEndian bool) string {
	if len(raw)%2 == 1 {
		raw = raw[:len(raw)-1]
	}
	words := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		if bigEndian {
			words = append(words, uint16(raw[i])<<8|uint16(raw[i+1]))
		} else {
			words = append(words, uint16(raw[i+1])<<8|uint16(raw[i]))
		}
	}
	return string(utf16.Decode(words))
}

func decodePDFDocEncoding(raw []byte) string {
	runes := make([]rune, 0, len(raw))
	for _, b := range raw {
		if b >= 0x20 && b <= 0x7e {
			runes = append(runes, rune(b))
			continue
		}
		if r, ok := pdfDocEncodingSpecials[b]; ok {
			runes = append(runes, r)
			continue
		}
		runes = append(runes, rune(b))
	}
	return string(runes)
}

var pdfDocEncodingSpecials = map[byte]rune{
	0x18: '\u02d8', 0x19: '\u02c7', 0x1a: '\u02c6', 0x1b: '\u02d9',
	0x1c: '\u02dd', 0x1d: '\u02db', 0x1e: '\u02da', 0x1f: '\u02dc',
	0x80: '\u2022', 0x81: '\u2020', 0x82: '\u2021', 0x83: '\u2026',
	0x84: '\u2014', 0x85: '\u2013', 0x86: '\u0192', 0x87: '\u2044',
	0x88: '\u2039', 0x89: '\u203a', 0x8a: '\u2212', 0x8b: '\u2030',
	0x8c: '\u201e', 0x8d: '\u201c', 0x8e: '\u201d', 0x8f: '\u2018',
	0x90: '\u2019', 0x91: '\u201a', 0x92: '\u2122', 0x93: '\ufb01',
	0x94: '\ufb02', 0x95: '\u0141', 0x96: '\u0152', 0x97: '\u0160',
	0x98: '\u0178', 0x99: '\u017d', 0x9a: '\u0131', 0x9b: '\u0142',
	0x9c: '\u0153', 0x9d: '\u0161', 0x9e: '\u017e', 0xa0: '\u20ac',
}

func sameStringList(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (s *Scanner) recordPathError(libraryID int64, jobID int64, path string, code domain.ErrorCode, message string) error {
	return s.store.RecordFileError(domain.FileErrorInput{
		LibraryID: libraryID,
		JobID:     jobID,
		Path:      path,
		Code:      code,
		Message:   message,
	})
}

func shouldSkipScanDir(library domain.Library, path string) bool {
	if filepath.Clean(path) == filepath.Clean(library.RootPath) {
		return false
	}
	name := strings.ToLower(filepath.Base(path))
	for _, skipped := range skippedScanDirNames() {
		if name == strings.ToLower(skipped) {
			return true
		}
	}
	rel, err := filepath.Rel(library.RootPath, path)
	if err != nil {
		return false
	}
	if library.AssetType == "game" && hasPC98PathContext(library, rel) && hasPC98NegativePathSignal(rel) {
		return true
	}
	rel = strings.Trim(strings.ToLower(filepath.ToSlash(rel)), "/")
	for _, pattern := range library.ExcludePatterns {
		pattern = strings.Trim(strings.ToLower(strings.ReplaceAll(pattern, "\\", "/")), "/")
		if pattern == "" {
			continue
		}
		if !strings.Contains(pattern, "/") && name == pattern {
			return true
		}
		if rel == pattern || strings.HasPrefix(rel, pattern+"/") {
			return true
		}
	}
	return false
}

func shouldSkipScanFile(path string) bool {
	name := filepath.Base(path)
	return name == ".DS_Store" || strings.HasPrefix(name, "._")
}

func isDiscTrackDependency(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".bin" && ext != ".raw" && ext != ".wav" && ext != ".mp3" && ext != ".img" && ext != ".sub" && ext != ".cue" && ext != ".ccd" && ext != ".toc" && ext != ".chd" {
		return false
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return false
	}
	cleanPath := filepath.Clean(path)
	for _, entry := range entries {
		descriptorExt := strings.ToLower(filepath.Ext(entry.Name()))
		if entry.IsDir() || !isMultiFileGameDescriptor(descriptorExt) || shouldSkipScanFile(entry.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(filepath.Dir(path), entry.Name()))
		if err != nil {
			continue
		}
		names, err := discDescriptorDependencyNames(descriptorExt, data)
		if err != nil {
			continue
		}
		if descriptorExt == ".ccd" {
			base := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			names = []string{base + ".img", base + ".sub"}
		}
		for _, name := range names {
			cleanName, err := cleanDiscDependencyName(name)
			if err != nil {
				continue
			}
			candidate, err := resolveDiscDependencyPath(filepath.Dir(path), cleanName)
			if err != nil {
				continue
			}
			candidateInfo, candidateErr := os.Stat(candidate)
			trackInfo, trackErr := os.Stat(cleanPath)
			if candidateErr == nil && trackErr == nil && os.SameFile(candidateInfo, trackInfo) {
				return true
			}
		}
	}
	return false
}

func skippedScanDirNames() []string {
	return []string{"#recycle", "@eaDir", ".calnotes", "__MACOSX", "_maintenance", "media", "covers", "cover", "thumbnails", ".thumbnails", "thumbs", ".thumbs", "出版物附属盘、非卖品", "游戏镜像"}
}

func seriesIdentityForRelPath(rootPath string, relPath string) (string, string) {
	dir := filepath.Dir(relPath)
	if dir == "." || dir == "/" {
		rootName := filepath.Base(filepath.Clean(rootPath))
		if rootName != "." && rootName != string(filepath.Separator) && rootName != "" {
			return rootName, "."
		}
		return "Unsorted", "."
	}
	directoryPath := filepath.ToSlash(dir)
	return directoryPath, directoryPath
}

type checksumPair struct {
	crc32  string
	sha1   string
	sha256 string
}

func fileChecksums(path string) (checksumPair, error) {
	file, err := os.Open(path)
	if err != nil {
		return checksumPair{}, err
	}
	defer file.Close()

	crc := crc32.NewIEEE()
	sha := sha1.New()
	sha256Hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(crc, sha, sha256Hash), file); err != nil {
		return checksumPair{}, err
	}
	return checksumPair{
		crc32:  fmt.Sprintf("%08x", crc.Sum32()),
		sha1:   hex.EncodeToString(sha.Sum(nil)),
		sha256: hex.EncodeToString(sha256Hash.Sum(nil)),
	}, nil
}

func populateGameFileSHA1(files []domain.GameFile, primaryPath string, primarySHA1 string, format string) error {
	for index := range files {
		file := &files[index]
		if file.Position == 0 && filepath.Clean(file.FilePath) == filepath.Clean(primaryPath) {
			switch strings.ToLower(strings.TrimSpace(format)) {
			case "cue":
				data, err := os.ReadFile(file.FilePath)
				if err != nil {
					return err
				}
				normalized, err := NormalizeCUEFileReferences(data)
				if err != nil {
					return err
				}
				file.SHA1 = sha1Hex(normalized)
				continue
			case "m3u":
				file.SHA1 = sha1Hex(canonicalM3UData(files))
				continue
			default:
				if gameFileSHA1Pattern.MatchString(strings.ToLower(strings.TrimSpace(primarySHA1))) {
					file.SHA1 = strings.ToLower(strings.TrimSpace(primarySHA1))
					continue
				}
			}
		}
		checksums, err := fileChecksums(file.FilePath)
		if err != nil {
			return fmt.Errorf("checksum game file %s: %w", file.Name, err)
		}
		file.SHA1 = checksums.sha1
	}
	return nil
}

func canonicalM3UData(files []domain.GameFile) []byte {
	lines := make([]string, 0)
	for _, file := range files {
		if file.Role != "dependency" {
			continue
		}
		switch strings.ToLower(filepath.Ext(file.Name)) {
		case ".cue", ".ccd", ".toc", ".chd":
			lines = append(lines, file.Name)
		}
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func sha1Hex(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}

var gameFileSHA1Pattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func gameTitle(path string) string {
	title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	title = regexp.MustCompile(`\s*\([^)]*\)`).ReplaceAllString(title, "")
	title = regexp.MustCompile(`\s*\[[^]]*]`).ReplaceAllString(title, "")
	return strings.TrimSpace(title)
}

var ndsReleaseNumberPattern = regexp.MustCompile(`(?i)^\s*(?:\(nds\)\s*)?\d{1,5}\s*-\s*`)

func ndsGameTitle(path string) string {
	title := gameTitle(path)
	title = ndsReleaseNumberPattern.ReplaceAllString(title, "")
	return strings.TrimSpace(title)
}

func gameTitlePreservingDiscLabel(path string) string {
	title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	title = regexp.MustCompile(`\s*\(([^)]*)\)`).ReplaceAllStringFunc(title, func(value string) string {
		label := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(value), "("), ")"))
		if strings.HasPrefix(strings.ToLower(label), "disc ") {
			return " (" + label + ")"
		}
		return ""
	})
	title = regexp.MustCompile(`\s*\[[^]]*]`).ReplaceAllString(title, "")
	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(title, " "))
}

var (
	pc98DiskSuffixPattern          = regexp.MustCompile(`(?i)[\s._-]*(?:disk|disc|data|fd|floppy|ディスク|データ|枚目|面)[\s._-]*([0-9０-９]+|[a-z])$`)
	pc98BareAlphaDiskSuffixPattern = regexp.MustCompile(`(?i)[._-]([a-z])$`)
)

func pc98SourceIdentity(rootPath string, sourcePath string, entryName string) (string, string, int) {
	mediaTitle := gameTitle(entryName)
	_, mediaDiskOrder := splitPC98DiskSuffix(mediaTitle)
	title := mediaTitle
	if strings.EqualFold(filepath.Ext(sourcePath), ".zip") {
		title = gameTitle(sourcePath)
	} else {
		parentTitle := gameTitle(filepath.Base(filepath.Dir(sourcePath)))
		if (mediaDiskOrder > 0 || isGenericPC98MediaTitle(mediaTitle)) && !isGenericPC98DirectoryTitle(parentTitle) {
			title = parentTitle
		}
	}
	logicalTitle, diskOrder := splitPC98DiskSuffix(title)
	if diskOrder == 0 {
		diskOrder = mediaDiskOrder
	}
	if strings.TrimSpace(logicalTitle) == "" {
		logicalTitle = title
	}
	relDir, err := filepath.Rel(rootPath, filepath.Dir(sourcePath))
	if err != nil {
		relDir = filepath.Dir(sourcePath)
	}
	groupKey := strings.ToLower(filepath.ToSlash(filepath.Clean(relDir))) + "\x1f" + normalizePC98GroupTitle(logicalTitle)
	return strings.TrimSpace(logicalTitle), groupKey, diskOrder
}

func splitPC98DiskSuffix(title string) (string, int) {
	match := pc98DiskSuffixPattern.FindStringSubmatchIndex(title)
	if match == nil {
		match = pc98BareAlphaDiskSuffixPattern.FindStringSubmatchIndex(title)
		if match == nil {
			return strings.TrimSpace(title), 0
		}
	}
	order := 0
	if len(match) >= 4 {
		value := normalizeFullWidthDigits(title[match[2]:match[3]])
		if parsed, err := strconv.Atoi(value); err == nil {
			order = parsed
		} else if len(value) == 1 {
			order = int(strings.ToLower(value)[0]-'a') + 1
		}
	}
	return strings.TrimSpace(title[:match[0]]), order
}

func normalizeFullWidthDigits(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= '０' && r <= '９' {
			return '0' + (r - '０')
		}
		return r
	}, value)
}

func normalizePC98GroupTitle(title string) string {
	title = strings.ToLower(normalizeFullWidthDigits(strings.TrimSpace(title)))
	title = regexp.MustCompile(`[\s._-]+`).ReplaceAllString(title, " ")
	return strings.TrimSpace(title)
}

func isBlockedPC98SpecialDiskTitle(title string) bool {
	normalized := normalizePC98GroupTitle(title)
	return normalized == "yu no special disk" || normalized == "yuno special disk"
}

func isGenericPC98MediaTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" {
		return true
	}
	if _, err := strconv.Atoi(normalizeFullWidthDigits(title)); err == nil {
		return true
	}
	return title == strings.ToUpper(title) && len([]rune(title)) <= 12
}

func isGenericPC98DirectoryTitle(title string) bool {
	switch strings.ToLower(strings.TrimSpace(title)) {
	case "", ".", "pc98", "pc-98", "game.pc98", "games", "original", "原始数据":
		return true
	default:
		return false
	}
}

func mediaTitle(path string) string {
	title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	title = strings.ReplaceAll(title, ".", " ")
	title = strings.ReplaceAll(title, "_", " ")
	title = regexp.MustCompile(`\s+`).ReplaceAllString(title, " ")
	return strings.TrimSpace(title)
}

func hasPC98PathContext(library domain.Library, relPath string) bool {
	for _, value := range []string{relPath, library.Name, filepath.Base(filepath.Clean(library.RootPath))} {
		for _, part := range strings.Split(strings.ToLower(filepath.ToSlash(strings.TrimSpace(value))), "/") {
			switch strings.TrimSpace(part) {
			case "pc98", "pc-98", "pc 98", "pc9801", "pc-9801", "pc9821", "pc-9821", "nec pc-98", "game.pc98":
				return true
			}
		}
	}
	return false
}

func hasPC98NegativePathSignal(relPath string) bool {
	for _, part := range strings.Split(strings.ToLower(filepath.ToSlash(relPath)), "/") {
		part = strings.TrimSpace(part)
		switch part {
		case "game.dos", "dos", "dosbox", "dosbox-x", "emulator", "emulators", "firmware", "bios", "cache", "caches", "tool", "tools", "utility", "utilities", "shader", "shaders", "lang", "language", "languages", "retroarch", "np2fmgen", "rom.mt32":
			return true
		}
		if strings.HasPrefix(part, "dosbox") || strings.HasPrefix(part, "---emulator") {
			return true
		}
	}
	return false
}

func isPC98ExcludedFile(path string) bool {
	name := strings.ToLower(strings.TrimSpace(filepath.Base(path)))
	switch name {
	case "bios.rom", "itf.rom", "sound.rom", "font.rom", "font.bmp", "pc98_cn.bmp", "font.tmp", "mt32_control.rom", "mt32_pcm.rom":
		return true
	}
	if strings.HasPrefix(name, "np2") || strings.HasPrefix(name, "np21") || strings.HasPrefix(name, "dosbox") {
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".exe", ".dll", ".drv", ".so", ".dylib", ".cfg", ".conf", ".ini", ".log", ".txt", ".nfo", ".md", ".csv", ".bat", ".bak", ".tmp", ".partial", ".download", ".rom", ".bmp":
		return true
	default:
		return false
	}
}

func inferGamePlatform(ext string, relPath string) string {
	path := strings.ToLower(filepath.ToSlash(relPath))
	if platform := inferFBNeoPlatform(path); platform != "" {
		return platform
	}
	for _, part := range strings.Split(path, "/") {
		switch part {
		case "snes", "sfc", "super nintendo":
			return "snes"
		case "virtualboy", "virtual boy", "virtual-boy", "nintendo virtual boy":
			return "virtualboy"
		case "n64", "nintendo64", "nintendo 64":
			return "n64"
		case "psp", "playstation portable":
			return "psp"
		case "ngc", "gamecube", "game cube", "nintendo gamecube", "nintendo game cube":
			return "ngc"
		case "ps2", "playstation 2", "playstation2":
			return "ps2"
		case "konami-python1", "konami python 1", "konami_python_1_collection", "kpython1":
			return "konami-python1"
		case "nes", "famicom":
			return "nes"
		case "md", "megadrive", "mega drive", "mega-drive":
			return "md"
		case "gba", "game boy advance":
			return "gba"
		case "gbc", "game boy color":
			return "gbc"
		case "gb", "game boy":
			return "gb"
		case "nds", "ds", "nintendo ds", "nintendo-ds", "nintendods":
			return "nds"
		case "3do", "panasonic 3do", "the 3do company - 3do", "3do interactive multiplayer":
			return "3do"
		case "3ds", "nintendo 3ds", "nintendo-3ds", "nintendo3ds", "ctr":
			return "3ds"
		case "mame", "mahjong":
			return "mame"
		case "arcade":
			return "arcade"
		case "neogeo", "neo geo", "neo-geo":
			return "neogeo"
		case "naomi 2", "sega naomi 2":
			return "naomi2"
		case "naomi":
			return "naomi"
		case "model2", "model2roms", "model 2", "sega model 2":
			return "model2"
		case "model3", "model3roms", "model 3", "sega model 3":
			return "model3"
		case "32x", "sega 32x":
			return "32x"
		case "ss", "saturn", "sega saturn":
			return "saturn"
		case "dc", "dreamcast", "sega dreamcast":
			return "dreamcast"
		case "pc-fx", "pcfx", "nec pc-fx", "nec pcfx":
			return "pc-fx"
		case "pc98", "pc-98", "pc 98", "pc9801", "pc-9801", "pc9821", "pc-9821", "nec pc-98", "game.pc98":
			return "pc98"
		case "dos", "ms-dos", "msdos", "ibm dos", "ibm pc dos":
			return "dos"
		case "ps", "ps1", "psx", "playstation", "playstation 1", "playstation one", "psone":
			return "ps1"
		}
	}
	switch ext {
	case ".vb", ".vboy":
		return "virtualboy"
	case ".gdi", ".cdi":
		return "dreamcast"
	case ".z64", ".v64", ".n64":
		return "n64"
	case ".sfc", ".smc":
		return "snes"
	case ".nes":
		return "nes"
	case ".gba":
		return "gba"
	case ".gbc":
		return "gbc"
	case ".gb":
		return "gb"
	case ".nds":
		return "nds"
	case ".3ds", ".cci", ".cxi", ".cia":
		return "3ds"
	case ".chd", ".iso", ".bin", ".cue", ".img":
		return "disc"
	case ".pbp":
		return "ps1"
	case ".py1":
		return "konami-python1"
	case ".zip", ".7z":
		return "mame"
	default:
		return "unknown"
	}
}

func hasTriforcePathContext(library domain.Library, relPath string) bool {
	for _, value := range []string{relPath, library.Name, filepath.Base(filepath.Clean(library.RootPath))} {
		for _, part := range strings.Split(filepath.ToSlash(value), "/") {
			if strings.EqualFold(strings.TrimSpace(part), "triforce") {
				return true
			}
		}
	}
	return false
}

func inferLibraryGamePlatform(library domain.Library, ext string, relPath string) string {
	// Keep the established client routing enum while preserving arcade hardware
	// identity in ROMSetName. This does not claim Dolphin gameplay compatibility.
	if (ext == ".iso" || ext == ".gcm" || ext == ".rvz") && hasTriforcePathContext(library, relPath) {
		return "ngc"
	}
	if hasPC98PathContext(library, relPath) && !hasPC98NegativePathSignal(relPath) {
		return "pc98"
	}
	if isNaomi2Path(library, filepath.Join(library.RootPath, filepath.FromSlash(relPath))) && ext == ".zip" {
		return "naomi2"
	}
	for _, value := range []string{relPath, library.Name, filepath.Base(filepath.Clean(library.RootPath))} {
		lower := strings.ToLower(filepath.ToSlash(strings.TrimSpace(value)))
		for _, part := range strings.Split(lower, "/") {
			if part == "dos" || part == "ms-dos" || part == "msdos" {
				return "dos"
			}
		}
		if strings.Contains(lower, "pc-fx") || strings.Contains(lower, "pcfx") {
			return "pc-fx"
		}
	}
	for _, value := range []string{library.Name, filepath.Base(filepath.Clean(library.RootPath))} {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "3do", "panasonic 3do", "the 3do company - 3do", "3do interactive multiplayer":
			if ext == ".cue" || ext == ".iso" || ext == ".chd" {
				return "3do"
			}
		case "virtualboy", "virtual boy", "virtual-boy", "nintendo virtual boy":
			if ext == ".zip" || ext == ".vb" || ext == ".vboy" || ext == ".bin" {
				return "virtualboy"
			}
		case "psp", "playstation portable":
			return "psp"
		case "ngc", "gamecube", "game cube", "nintendo gamecube", "nintendo game cube":
			return "ngc"
		case "ps2", "playstation 2", "playstation2":
			return "ps2"
		case "konami-python1", "konami python 1", "konami_python_1_collection", "kpython1":
			return "konami-python1"
		case "model2", "model2roms", "model 2", "sega model 2":
			if ext == ".zip" {
				return "model2"
			}
		case "n64", "nintendo64", "nintendo 64":
			if ext == ".zip" || isN64RawExt(ext) {
				return "n64"
			}
		case "3ds", "nintendo 3ds", "nintendo-3ds", "nintendo3ds", "ctr":
			if ext == ".zip" || ext == ".3ds" || ext == ".cci" || ext == ".cxi" || ext == ".cia" {
				return "3ds"
			}
		}
	}
	platform := inferGamePlatform(ext, relPath)
	if platform != "disc" {
		return platform
	}
	for _, value := range []string{library.Name, filepath.Base(filepath.Clean(library.RootPath))} {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "dc", "dreamcast", "sega dreamcast":
			if ext == ".chd" {
				return "dreamcast"
			}
		case "ss", "saturn", "sega saturn":
			if ext == ".cue" || ext == ".chd" || ext == ".iso" {
				return "saturn"
			}
		}
	}
	return platform
}

func inferFBNeoPlatform(path string) string {
	parts := strings.Split(path, "/")
	for index, part := range parts {
		if part != "fbneo" || index+1 >= len(parts) {
			continue
		}
		system := parts[index+1]
		switch system {
		case "megadrive", "mega drive", "mega-drive", "md":
			return "md"
		case "snes", "super nintendo":
			return "snes"
		case "nes", "famicom":
			return "nes"
		case "neogeo", "neo geo", "neo-geo":
			return "neogeo"
		case "naomi":
			return "naomi"
		case "model2", "model 2", "sega model 2":
			return "model2"
		case "model3", "model 3", "sega model 3":
			return "model3"
		case "32x", "sega 32x":
			return "32x"
		case "arcade":
			if index+2 < len(parts) {
				shortName := strings.TrimSuffix(parts[index+2], filepath.Ext(parts[index+2]))
				if isMAMEMahjongShortName(shortName) {
					return "mame"
				}
				if isFBNeoMegaDriveShortName(shortName) {
					return "md"
				}
				if isNeoGeoShortName(shortName) {
					return "neogeo"
				}
			}
			return "arcade"
		}
	}
	return ""
}

func isFBNeoMegaDriveShortName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	prefixes := []string{
		"shinobi3",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func isMAMEMahjongShortName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "fromanc4", "fromancr", "hypreact", "hypreac2", "mcnpshnt", "srmp4", "srmp7", "ym2413_instruments":
		return true
	default:
		return false
	}
}

func isNeoGeoShortName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "neogeo" || name == "mslug" {
		return true
	}
	prefixes := []string{
		"mslug", "kof", "samsho", "samsh", "aof", "fatfur", "fatfury", "rbff", "garou",
		"lastblad", "svc", "sengoku", "bstars", "blazstar", "pulstar", "shocktro",
		"magdrop", "wjammers", "breakers", "matrim", "preisle2", "kizuna", "kabukikl",
		"ninjamas", "neobombe", "neodrift", "neomrdo", "tws96", "goalx3", "lresort",
		"viewpoin", "nam1975", "cyberlip", "superspy", "roboarmy", "eightman",
		"burningf", "crsword", "socbrawl", "mutnat", "mutation", "kotm", "alpham2",
		"androdun", "zedblade", "strhoop", "turfmast", "puzzledp", "joyjoy", "2020bb",
		"3countb", "tophuntr", "spinmast", "pbobblen", "popbounc", "panicbom", "nitd",
		"zupapa", "ganryu", "bangbead", "flipshot", "ssideki", "overtop", "ghostlop",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func inferROMSetName(relPath string) string {
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	if len(parts) > 1 {
		for _, part := range parts[:len(parts)-1] {
			switch strings.ToLower(strings.TrimSpace(part)) {
			case "konami-python1", "konami python 1", "konami_python_1_collection", "kpython1":
				return "KONAMI-PYTHON1"
			}
		}
		switch strings.ToLower(strings.TrimSpace(parts[0])) {
		case "mahjong", "mame":
			return gameROMSetStem(relPath, filepath.Ext(relPath))
		case "cps1 cps2 cps3", "cps1", "cps2", "cps3":
			return gameROMSetStem(relPath, filepath.Ext(relPath))
		case "model2", "model2roms", "model 2", "sega model 2",
			"model3", "model3roms", "model 3", "sega model 3":
			return gameROMSetStem(relPath, filepath.Ext(relPath))
		case "fbneo":
			if len(parts) > 2 && strings.EqualFold(strings.TrimSpace(parts[1]), "arcade") {
				shortName := strings.TrimSuffix(parts[2], filepath.Ext(parts[2]))
				if isMAMEMahjongShortName(shortName) {
					return strings.ToLower(shortName)
				}
			}
		}
		return parts[0]
	}
	return ""
}

func model2FriendlyTitle(shortName string) string {
	titles := map[string]string{
		"bel":      "Behind Enemy Lines",
		"daytona":  "Daytona USA",
		"desert":   "Desert Tank",
		"doa":      "Dead or Alive",
		"dynabb97": "Dynamite Baseball 97",
		"dynamcop": "Dynamite Cop",
		"fvipers":  "Fighting Vipers",
		"gunblade": "Gunblade NY",
		"hotd":     "The House of the Dead",
		"indy500":  "INDY 500",
		"lastbrnx": "Last Bronx",
		"manxtt":   "Manx TT Superbike",
		"manxttc":  "Manx TT Superbike (Revision C)",
		"motoraid": "Motor Raid",
		"overrev":  "Over Rev",
		"pltkids":  "Pilot Kids",
		"rchase2":  "Rail Chase 2",
		"schamp":   "Sonic Championship",
		"segawski": "Sega Water Ski",
		"sgt24h":   "Super GT 24h",
		"skisuprg": "Sega Ski Super G",
		"skytargt": "Sky Target",
		"srallyc":  "Sega Rally Championship",
		"stcc":     "Sega Touring Car Championship",
		"topskatr": "Top Skater",
		"vcop":     "Virtua Cop",
		"vcop2":    "Virtua Cop 2",
		"vf2":      "Virtua Fighter 2",
		"von":      "Cyber Troopers Virtual-On",
		"vstriker": "Virtua Striker",
		"waverunr": "Wave Runner",
		"zerogun":  "Zero Gunner",
	}
	return titles[strings.ToLower(strings.TrimSpace(shortName))]
}

func model2Compatibility(shortName string) string {
	broken := map[string]struct{}{
		"daytona": {}, "desert": {}, "doa": {}, "hotd": {}, "manxtt": {}, "manxttc": {},
		"overrev": {}, "rchase2": {}, "srallyc": {}, "stcc": {}, "vcop": {}, "von": {},
	}
	if _, ok := broken[strings.ToLower(strings.TrimSpace(shortName))]; ok {
		return "broken"
	}
	return "untested"
}

func model2Region(shortName string) string {
	switch strings.ToLower(strings.TrimSpace(shortName)) {
	case "segawski", "waverunr":
		return "Japan"
	case "schamp":
		return "USA"
	case "segabill":
		return ""
	default:
		return "World"
	}
}

func inferRegion(path string) string {
	lower := strings.ToLower(filepath.Base(path))
	switch {
	case strings.Contains(lower, "(usa)") || strings.Contains(lower, "[usa]") || strings.Contains(lower, "(u)"):
		return "USA"
	case strings.Contains(lower, "(japan)") || strings.Contains(lower, "[japan]") || strings.Contains(lower, "(j)"):
		return "Japan"
	case strings.Contains(lower, "(europe)") || strings.Contains(lower, "[europe]") || strings.Contains(lower, "(e)"):
		return "Europe"
	default:
		return ""
	}
}

func classifyWalkError(err error) domain.ErrorCode {
	if strings.Contains(strings.ToLower(err.Error()), "permission") {
		return domain.ErrorPermissionDenied
	}
	return domain.ErrorUnknownIO
}
