package scanner

import (
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
	"strconv"
	"strings"
	"sync"
	"time"

	"foliospace-reader/internal/archive"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/store"
)

type Scanner struct {
	store       *store.Store
	workerCount func() int
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
	job, err := s.store.StartScanJob(library.ID)
	if err != nil {
		return job, err
	}
	return s.RunScanJob(library, job)
}

func (s *Scanner) StartScanJob(library domain.Library) (domain.ScanJob, error) {
	job, err := s.store.StartScanJob(library.ID)
	if err != nil {
		return job, err
	}
	go func() {
		_, _ = s.RunScanJob(library, job)
	}()
	return job, nil
}

func (s *Scanner) RunScanJob(library domain.Library, job domain.ScanJob) (domain.ScanJob, error) {
	_ = s.store.AddJobEvent(job.ID, "info", "scan started")
	_ = s.store.AddJobEvent(job.ID, "info", "walking "+library.RootPath)

	workers := s.workerCount()
	if workers > 1 {
		return s.runScanJobConcurrent(library, job, workers)
	}

	walkErr := filepath.WalkDir(library.RootPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := s.applyScanControl(&job); err != nil {
			return err
		}
		if walkErr != nil {
			if shouldSkipScanDir(library.RootPath, path) {
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
			if shouldSkipScanDir(library.RootPath, path) {
				return filepath.SkipDir
			}
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

		if index, ok := s.unchangedFileIndex(path, info, ext); ok {
			if canSkipUnchangedBook(library, path, index, ext) {
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

		result, err := s.indexFile(library, job.ID, path, info, ext)
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
	if err := s.cleanupSkippedEntries(library, &job); err != nil {
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

type scanFileTask struct {
	path string
	info fs.FileInfo
	ext  string
	kind string
}

func (s *Scanner) runScanJobConcurrent(library domain.Library, job domain.ScanJob, workers int) (domain.ScanJob, error) {
	_ = s.store.AddJobEvent(job.ID, "info", fmt.Sprintf("scan workers: %d", workers))

	var tasks []scanFileTask
	walkErr := filepath.WalkDir(library.RootPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := s.applyScanControl(&job); err != nil {
			return err
		}
		if walkErr != nil {
			if shouldSkipScanDir(library.RootPath, path) {
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
			if shouldSkipScanDir(library.RootPath, path) {
				return filepath.SkipDir
			}
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
		tasks = append(tasks, scanFileTask{path: path, info: info, ext: ext, kind: kind})
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

	taskCh := make(chan scanFileTask, len(tasks))
	var wg sync.WaitGroup
	var mu sync.Mutex
	lastPersist := time.Now()
	stopped := false

	updateJob := func(change func(*domain.ScanJob)) {
		mu.Lock()
		defer mu.Unlock()
		change(&job)
		if time.Since(lastPersist) >= 500*time.Millisecond || job.Status != "running" {
			_ = s.store.UpdateScanJob(job)
			lastPersist = time.Now()
		}
	}
	checkControl := func() bool {
		mu.Lock()
		defer mu.Unlock()
		if stopped {
			return false
		}
		if err := s.applyScanControl(&job); err != nil {
			stopped = true
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
				s.processScanTask(library, job.ID, task, updateJob)
			}
		}()
	}
	for _, task := range tasks {
		mu.Lock()
		shouldStop := stopped
		mu.Unlock()
		if shouldStop {
			break
		}
		taskCh <- task
	}
	close(taskCh)
	wg.Wait()

	if job.Status == "paused" || job.Status == "cancelled" {
		return job, nil
	}
	if err := s.cleanupSkippedEntries(library, &job); err != nil {
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

func (s *Scanner) processScanTask(library domain.Library, jobID int64, task scanFileTask, updateJob func(func(*domain.ScanJob))) {
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
	if err := s.store.DeleteGameByPath(task.path); err != nil {
		s.recordTaskError(library.ID, jobID, task.path, domain.ErrorUnknownIO, "game cleanup failed: ", err, updateJob)
		return
	}
	if err := s.store.DeleteVideoByPath(task.path); err != nil {
		s.recordTaskError(library.ID, jobID, task.path, domain.ErrorUnknownIO, "video cleanup failed: ", err, updateJob)
		return
	}

	if index, ok := s.unchangedFileIndex(task.path, task.info, task.ext); ok {
		if canSkipUnchangedBook(library, task.path, index, task.ext) {
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

	result, err := s.indexFile(library, jobID, task.path, task.info, task.ext)
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
		_ = s.store.AddJobEvent(jobID, "info", "indexed: "+task.path)
	})
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
	case ".nes", ".sfc", ".smc", ".gba", ".gb", ".gbc", ".nds", ".3ds", ".cia", ".chd", ".iso", ".bin", ".cue":
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

func (s *Scanner) unchangedFileIndex(path string, info fs.FileInfo, ext string) (store.FileIndex, bool) {
	index, err := s.store.FileIndexByPath(path)
	if err != nil {
		return store.FileIndex{}, false
	}
	ok := index.File.Size == info.Size() &&
		index.File.Ext == ext &&
		index.File.MTime.Equal(info.ModTime()) &&
		index.Analyzed &&
		index.PageCount > 0
	return index, ok
}

func canSkipUnchangedBook(library domain.Library, path string, index store.FileIndex, ext string) bool {
	if ext != ".epub" {
		relPath, err := filepath.Rel(library.RootPath, path)
		if err != nil {
			return false
		}
		seriesTitle, _ := seriesIdentityForRelPath(library.RootPath, relPath)
		return index.Book.CollectionTitle == seriesTitle
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
			result.MetadataUpdated = previous.Title != metadata.Title || previous.Creator != metadata.Creator || previous.Description != metadata.Description
			result.Reclassified = previous.SeriesID != series.ID
		}
		book, err = s.store.UpdateBookMetadata(book.ID, metadata.Creator, metadata.Description)
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
	book, err = s.store.UpdateBookMetadata(book.ID, metadata.Creator, metadata.Description)
	if err != nil {
		return indexedBookResult{}, err
	}
	_, err = s.store.UpsertFile(book.ID, library.ID, path, relPath, info.Size(), info.ModTime(), ext)
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
}

func bookMetadataForPath(path string, ext string) (bookMetadata, error) {
	fallback := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if ext != ".epub" {
		return bookMetadata{Title: fallback}, nil
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

func (s *Scanner) recordPathError(libraryID int64, jobID int64, path string, code domain.ErrorCode, message string) error {
	return s.store.RecordFileError(domain.FileErrorInput{
		LibraryID: libraryID,
		JobID:     jobID,
		Path:      path,
		Code:      code,
		Message:   message,
	})
}

func shouldSkipScanDir(rootPath string, path string) bool {
	if filepath.Clean(path) == filepath.Clean(rootPath) {
		return false
	}
	switch filepath.Base(path) {
	case skippedScanDirNames()[0], skippedScanDirNames()[1], skippedScanDirNames()[2]:
		return true
	default:
		return false
	}
}

func skippedScanDirNames() []string {
	return []string{"#recycle", "@eaDir", ".calnotes"}
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
		case "ps1", "psx", "playstation":
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
	case ".chd", ".iso", ".bin", ".cue":
		return "disc"
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
