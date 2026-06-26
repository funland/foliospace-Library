package scanner

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
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
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
)

type Scanner struct {
	store       *store.Store
	workerCount func() int
}

type scanScope struct {
	rootPath       string
	fullScan       bool
	deferPageIndex bool
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
			expectedPlatform := inferGamePlatform(ext, relPath)
			if s.store.CanSkipGame(path, info.Size(), info.ModTime(), expectedPlatform) {
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
			result, err := s.indexFileMetadata(library, path, info, ext)
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
	return job, nil
}

func (s *Scanner) runRecentScanJob(library domain.Library, job domain.ScanJob, scope scanScope, limit int) (domain.ScanJob, error) {
	limit = NormalizeRecentScanLimit(limit)
	scope.fullScan = false
	scope.deferPageIndex = true
	_ = s.store.AddJobEvent(job.ID, "info", "recent scan started")
	_ = s.store.AddJobEvent(job.ID, "info", fmt.Sprintf("finding latest %d under %s", limit, scope.rootPath))

	dirStates := map[string]*scanDirState{}
	fileIndexes, _ := s.store.ListFileIndexesByLibrary(library.ID)
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
		expectedPlatform := inferGamePlatform(task.ext, relPath)
		if s.store.CanSkipGame(task.path, task.info.Size(), task.info.ModTime(), expectedPlatform) {
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
		result, err := s.indexFileMetadata(library, task.path, task.info, task.ext)
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
		return !s.store.CanSkipGame(task.path, task.info.Size(), task.info.ModTime(), inferGamePlatform(task.ext, relPath))
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

func (s *Scanner) cleanupSkippedEntries(library domain.Library, job *domain.ScanJob) error {
	deleted, err := s.store.DeleteSkippedDirectoryEntries(library.ID, skippedScanDirNames())
	if err != nil {
		return err
	}
	if deleted > 0 {
		_ = s.store.AddJobEvent(job.ID, "info", fmt.Sprintf("removed %d skipped-directory entries", deleted))
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
	if library.AssetType == "game" {
		if isGameExt(ext) || isGamePackageExt(ext) {
			return "game"
		}
		return ""
	}
	if library.AssetType == "video" {
		if isVideoExt(ext) {
			return "video"
		}
		return ""
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

func isBookExt(ext string) bool {
	return ext == ".cbz" || ext == ".zip" || ext == ".epub" || ext == ".7z" || ext == ".pdf"
}

func isGamePackageExt(ext string) bool {
	return ext == ".zip" || ext == ".7z"
}

func isGameExt(ext string) bool {
	switch ext {
	case ".nes", ".sfc", ".smc", ".gba", ".gb", ".gbc", ".nds", ".3ds", ".cia", ".chd", ".iso", ".bin", ".cue", ".img", ".pbp":
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

func (s *Scanner) indexFile(library domain.Library, jobID int64, path string, info fs.FileInfo, ext string) (indexedBookResult, error) {
	result, err := s.indexFileMetadata(library, path, info, ext)
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
	if scope.deferPageIndex {
		if ext != ".epub" {
			return s.indexBasicFileMetadata(library, path, info, ext)
		}
		return s.indexFileMetadata(library, path, info, ext)
	}
	return s.indexFile(library, jobID, path, info, ext)
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
	checksums, err := fileChecksums(path)
	if err != nil {
		return err
	}
	title := gameTitle(path)
	platform := inferGamePlatform(ext, relPath)
	_, err = s.store.UpsertGame(domain.GameAsset{
		LibraryID:     library.ID,
		Title:         title,
		Platform:      platform,
		ROMSetName:    inferROMSetName(relPath),
		Region:        inferRegion(path),
		Format:        strings.TrimPrefix(ext, "."),
		FilePath:      path,
		RelPath:       filepath.ToSlash(relPath),
		Size:          info.Size(),
		MTime:         info.ModTime(),
		CRC32:         checksums.crc32,
		SHA1:          checksums.sha1,
		EmulatorHint:  platform,
		Compatibility: "unknown",
	})
	return err
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
	return err
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

func (s *Scanner) indexFileMetadata(library domain.Library, path string, info fs.FileInfo, ext string) (indexedBookResult, error) {
	relPath, err := filepath.Rel(library.RootPath, path)
	if err != nil {
		return indexedBookResult{}, fmt.Errorf("relative path: %w", err)
	}

	seriesTitle, seriesDirectoryPath := seriesIdentityForRelPath(library.RootPath, relPath)
	metadata, err := bookMetadataForPath(path, ext)
	if err != nil {
		return indexedBookResult{}, err
	}
	if metadata.Creator != "" {
		seriesTitle = metadata.Creator
		seriesDirectoryPath = filepath.ToSlash(filepath.Dir(relPath))
		if seriesDirectoryPath == "." || seriesDirectoryPath == "/" {
			seriesDirectoryPath = "."
		}
	}
	format := strings.TrimPrefix(ext, ".")

	series, err := s.store.UpsertSeries(library.ID, seriesTitle, seriesDirectoryPath)
	if err != nil {
		return indexedBookResult{}, err
	}

	var book domain.Book
	existing, err := s.store.FileIndexByPath(path)
	if err == nil {
		previous, previousErr := s.store.BookByID(existing.File.BookID)
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
			result.MetadataUpdated = previous.Title != metadata.Title || previous.Creator != metadata.Creator || previous.Description != metadata.Description || !sameStringList(previous.Tags, metadata.Tags)
			result.Reclassified = previous.SeriesID != series.ID
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
		return result, nil
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
	return indexedBookResult{Book: book}, nil
}

func (s *Scanner) indexBasicFileMetadata(library domain.Library, path string, info fs.FileInfo, ext string) (indexedBookResult, error) {
	relPath, err := filepath.Rel(library.RootPath, path)
	if err != nil {
		return indexedBookResult{}, fmt.Errorf("relative path: %w", err)
	}
	seriesTitle, seriesDirectoryPath := seriesIdentityForRelPath(library.RootPath, relPath)
	title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	format := strings.TrimPrefix(ext, ".")
	book, err := s.store.UpsertBasicBookFile(library.ID, seriesTitle, seriesDirectoryPath, title, format, path, relPath, info.Size(), info.ModTime(), ext)
	if err != nil {
		return indexedBookResult{}, err
	}
	return indexedBookResult{Book: book}, nil
}

func (s *Scanner) disambiguateBookTitle(library domain.Library, seriesID int64, title string, format string, relPath string, currentBookID int64) (string, error) {
	existing, err := s.store.BookBySeriesTitle(seriesID, title, format)
	if err != nil {
		return title, nil
	}
	if existing.ID == currentBookID {
		return title, nil
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
	}
	currentTitle := disambiguatedTitle(title, relPath)
	if currentTitle == existingTitle {
		currentTitle = title + " (" + strings.TrimSpace(filepath.ToSlash(filepath.Dir(relPath))) + ")"
	}
	return currentTitle, nil
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
	Title       string
	Creator     string
	Description string
	Tags        []string
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
	info := pdfInfoDictionary(data)
	if len(info) == 0 {
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

func pdfInfoDictionary(data []byte) []byte {
	pattern := regexp.MustCompile(`/Info\s+(\d+)\s+(\d+)\s+R`)
	matches := pattern.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return nil
	}
	match := matches[len(matches)-1]
	objectPattern := regexp.MustCompile(`(^|[\x00\t\n\f\r ])` + regexp.QuoteMeta(string(match[1])) + `\s+` + regexp.QuoteMeta(string(match[2])) + `\s+obj\b`)
	objectMatch := objectPattern.FindIndex(data)
	if len(objectMatch) == 0 {
		return nil
	}
	return extractPDFDictionary(data[objectMatch[1]:])
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

func skippedScanDirNames() []string {
	return []string{"#recycle", "@eaDir", ".calnotes", "__MACOSX", "media", "covers", "cover", "thumbnails", ".thumbnails", "thumbs", ".thumbs"}
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
	crc32 string
	sha1  string
}

func fileChecksums(path string) (checksumPair, error) {
	file, err := os.Open(path)
	if err != nil {
		return checksumPair{}, err
	}
	defer file.Close()

	crc := crc32.NewIEEE()
	sha := sha1.New()
	if _, err := io.Copy(io.MultiWriter(crc, sha), file); err != nil {
		return checksumPair{}, err
	}
	return checksumPair{
		crc32: fmt.Sprintf("%08x", crc.Sum32()),
		sha1:  hex.EncodeToString(sha.Sum(nil)),
	}, nil
}

func gameTitle(path string) string {
	title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	title = regexp.MustCompile(`\s*\([^)]*\)`).ReplaceAllString(title, "")
	title = regexp.MustCompile(`\s*\[[^]]*]`).ReplaceAllString(title, "")
	return strings.TrimSpace(title)
}

func mediaTitle(path string) string {
	title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	title = strings.ReplaceAll(title, ".", " ")
	title = strings.ReplaceAll(title, "_", " ")
	title = regexp.MustCompile(`\s+`).ReplaceAllString(title, " ")
	return strings.TrimSpace(title)
}

func inferGamePlatform(ext string, relPath string) string {
	path := strings.ToLower(filepath.ToSlash(relPath))
	if platform := inferFBNeoPlatform(path); platform != "" {
		return platform
	}
	for _, part := range strings.Split(path, "/") {
		switch part {
		case "snes", "super nintendo":
			return "snes"
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
		case "nds", "nintendo ds":
			return "nds"
		case "3ds", "nintendo 3ds":
			return "3ds"
		case "arcade", "mame":
			return "arcade"
		case "neogeo", "neo geo", "neo-geo":
			return "neogeo"
		case "naomi":
			return "naomi"
		case "model3", "model3roms", "model 3", "sega model 3":
			return "model3"
		case "32x", "sega 32x":
			return "32x"
		case "ss", "saturn", "sega saturn":
			return "saturn"
		case "ps", "ps1", "psx", "playstation", "playstation 1", "playstation one", "psone":
			return "ps1"
		}
	}
	switch ext {
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
	case ".3ds", ".cia":
		return "3ds"
	case ".chd", ".iso", ".bin", ".cue", ".img":
		return "disc"
	case ".pbp":
		return "ps1"
	case ".zip", ".7z":
		return "arcade"
	default:
		return "unknown"
	}
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
		case "model3", "model 3", "sega model 3":
			return "model3"
		case "32x", "sega 32x":
			return "32x"
		case "arcade":
			if index+2 < len(parts) {
				shortName := strings.TrimSuffix(parts[index+2], filepath.Ext(parts[index+2]))
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
		return parts[0]
	}
	return ""
}

func inferRegion(path string) string {
	lower := strings.ToLower(filepath.Base(path))
	switch {
	case strings.Contains(lower, "(usa)") || strings.Contains(lower, "[usa]"):
		return "USA"
	case strings.Contains(lower, "(japan)") || strings.Contains(lower, "[japan]"):
		return "Japan"
	case strings.Contains(lower, "(europe)") || strings.Contains(lower, "[europe]"):
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
