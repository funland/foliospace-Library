package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"foliospace-reader/internal/archive"
	"foliospace-reader/internal/domain"
	_ "golang.org/x/image/webp"
)

const thumbnailAlgorithmVersion = "v1"
const thumbnailClientCacheVersion = thumbnailAlgorithmVersion + "-cover-refresh-3"
const thumbnailCacheKeyProfile = "portrait-1x-3x4.15"
const thumbnailJPEGQuality = 90
const thumbnailTargetAspectRatio = 3.0 / 4.15

type ThumbnailStream struct {
	Body            io.ReadCloser
	ContentType     string
	CacheHit        bool
	StaleFallback   bool
	SourceFallback  bool
	GenericFallback bool
	ETag            string
	CachePath       string
}

func ThumbnailCacheVersion() string {
	return thumbnailAlgorithmVersion
}

func ThumbnailClientCacheVersion() string {
	return thumbnailClientCacheVersion
}

type thumbnailWorker struct {
	service *Service
	wake    chan struct{}

	mu      sync.Mutex
	paused  bool
	stopped bool
	active  bool
}

func newThumbnailWorker(service *Service) *thumbnailWorker {
	return &thumbnailWorker{
		service: service,
		wake:    make(chan struct{}, 1),
	}
}

func (w *thumbnailWorker) start() {
	go w.loop()
	w.wakeUp()
}

func (w *thumbnailWorker) loop() {
	for {
		<-w.wake
		if w.isStopped() {
			return
		}
		for {
			if w.isStopped() || w.isPaused() {
				break
			}
			if w.service.thumbnailWorkerShouldYield() {
				time.Sleep(2 * time.Second)
				continue
			}
			processed, err := w.processOne(context.Background())
			if err != nil || !processed {
				break
			}
		}
	}
}

func (w *thumbnailWorker) processOne(ctx context.Context) (bool, error) {
	w.mu.Lock()
	if w.paused || w.stopped || w.active {
		w.mu.Unlock()
		return false, nil
	}
	w.active = true
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.active = false
		w.mu.Unlock()
	}()

	job, ok, err := w.service.store.ClaimNextThumbnailJob()
	if err != nil || !ok {
		return false, err
	}
	if err := w.service.generateThumbnailJob(ctx, job); err != nil {
		if failErr := w.service.store.FailThumbnailJob(job.ID, err.Error()); failErr != nil {
			return true, failErr
		}
		return true, nil
	}
	return true, nil
}

func (w *thumbnailWorker) wakeUp() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *thumbnailWorker) pause() {
	w.mu.Lock()
	w.paused = true
	w.mu.Unlock()
}

func (w *thumbnailWorker) resume() {
	w.mu.Lock()
	w.paused = false
	w.mu.Unlock()
	w.wakeUp()
}

func (w *thumbnailWorker) stop() {
	w.mu.Lock()
	w.stopped = true
	w.mu.Unlock()
	w.wakeUp()
}

func (w *thumbnailWorker) isPaused() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.paused
}

func (w *thumbnailWorker) isStopped() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stopped
}

func (s *Service) OpenBookThumbnail(bookID int64, size string) (ThumbnailStream, error) {
	size = normalizeBookThumbnailSize(size)
	book, err := s.store.BookByID(bookID)
	if err != nil {
		return ThumbnailStream{}, err
	}
	cacheKey, err := s.bookThumbnailCacheKey(book, size)
	if err != nil {
		return ThumbnailStream{}, err
	}
	cachePath, err := s.bookThumbnailCachePath(book.ID, size, cacheKey)
	if err != nil {
		return ThumbnailStream{}, err
	}
	if file, err := os.Open(cachePath); err == nil {
		return ThumbnailStream{
			Body:        file,
			ContentType: "image/jpeg",
			CacheHit:    true,
			ETag:        cacheKey,
			CachePath:   cachePath,
		}, nil
	}
	if _, err := s.store.EnqueueThumbnailJob(domain.ThumbnailJobInput{
		BookID:   book.ID,
		Size:     size,
		CacheKey: cacheKey,
		Priority: thumbnailPriorityForSize(size),
	}); err != nil {
		return ThumbnailStream{}, err
	}
	if s.thumbnailWorker != nil {
		s.thumbnailWorker.wakeUp()
	}
	if preferSourceThumbnailFallback(book) {
		if cover, err := s.openBookThumbnailSource(book); err == nil {
			return ThumbnailStream{
				Body:           cover.Body,
				ContentType:    cover.ContentType,
				SourceFallback: true,
				ETag:           cacheKey,
			}, nil
		}
	}
	if file, path := s.openStaleBookThumbnail(book.ID, size, cachePath); file != nil {
		return ThumbnailStream{
			Body:          file,
			ContentType:   "image/jpeg",
			CacheHit:      true,
			StaleFallback: true,
			CachePath:     path,
		}, nil
	}
	if cover, err := s.OpenCover(book.ID); err == nil {
		return ThumbnailStream{
			Body:           cover.Body,
			ContentType:    cover.ContentType,
			SourceFallback: true,
			ETag:           cacheKey,
		}, nil
	}
	if stream, err := genericBookThumbnailStream(book, size); err == nil {
		return stream, nil
	}
	return ThumbnailStream{
		Body:        io.NopCloser(strings.NewReader(thumbnailPlaceholderSVG(size))),
		ContentType: "image/svg+xml; charset=utf-8",
		CacheHit:    false,
		ETag:        cacheKey,
	}, nil
}

func (s *Service) openStaleBookThumbnail(bookID int64, size string, currentPath string) (io.ReadCloser, string) {
	dir := filepath.Join(s.thumbnailCacheRoot(), normalizeBookThumbnailSize(size))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, ""
	}
	currentPath = filepath.Clean(currentPath)
	prefix := fmt.Sprintf("%d-", bookID)
	var bestPath string
	var bestMod time.Time
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(strings.ToLower(name), ".jpg") {
			continue
		}
		path := filepath.Clean(filepath.Join(dir, name))
		if path == currentPath {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
			continue
		}
		if bestPath == "" || info.ModTime().After(bestMod) {
			bestPath = path
			bestMod = info.ModTime()
		}
	}
	if bestPath == "" {
		return nil, ""
	}
	file, err := os.Open(bestPath)
	if err != nil {
		return nil, ""
	}
	return file, bestPath
}

func genericBookThumbnailStream(book domain.Book, size string) (ThumbnailStream, error) {
	var body bytes.Buffer
	if err := jpeg.Encode(&body, genericBookThumbnail(book, size), &jpeg.Options{Quality: thumbnailJPEGQuality}); err != nil {
		return ThumbnailStream{}, err
	}
	return ThumbnailStream{
		Body:            io.NopCloser(bytes.NewReader(body.Bytes())),
		ContentType:     "image/jpeg",
		GenericFallback: true,
	}, nil
}

func (s *Service) ThumbnailWorkerStatus() (domain.ThumbnailQueueStatus, error) {
	status, err := s.store.ThumbnailQueueStatus()
	if err != nil {
		return status, err
	}
	status.WorkerEnabled = s.thumbnailWorker != nil
	if s.thumbnailWorker != nil {
		status.Paused = s.thumbnailWorker.isPaused()
		if status.Paused {
			status.Status = "paused"
		}
	}
	cache, err := s.thumbnailCacheStatus()
	if err != nil {
		return status, err
	}
	status.Cache = cache
	return status, nil
}

func (s *Service) PauseThumbnailWorker() {
	if s.thumbnailWorker != nil {
		s.thumbnailWorker.pause()
	}
}

func (s *Service) ResumeThumbnailWorker() {
	if s.thumbnailWorker != nil {
		s.thumbnailWorker.resume()
	}
}

func (s *Service) CancelThumbnailJobs() (int64, error) {
	return s.store.CancelQueuedThumbnailJobs()
}

func (s *Service) CleanupThumbnailOrphanCache() (domain.ThumbnailCacheCleanupResult, error) {
	var result domain.ThumbnailCacheCleanupResult
	referenced, err := s.readyThumbnailCachePaths()
	if err != nil {
		return result, err
	}
	root := s.thumbnailCacheRoot()
	if _, err := os.Stat(root); os.IsNotExist(err) {
		s.invalidateThumbnailCacheStatus()
		return result, nil
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		cachePath := filepath.Clean(path)
		if referenced[cachePath] {
			return nil
		}
		info, statErr := entry.Info()
		if statErr != nil {
			result.FailedFiles++
			return nil
		}
		if err := os.Remove(cachePath); err != nil {
			result.FailedFiles++
			return nil
		}
		result.DeletedFiles++
		result.DeletedBytes += info.Size()
		return nil
	})
	s.invalidateThumbnailCacheStatus()
	return result, err
}

func (s *Service) ProcessNextThumbnailJobForTest() error {
	if s.thumbnailWorker == nil {
		return fmt.Errorf("thumbnail worker is not enabled")
	}
	processed, err := s.thumbnailWorker.processOne(context.Background())
	if err != nil {
		return err
	}
	if !processed {
		return nil
	}
	return nil
}

func (s *Service) thumbnailWorkerShouldYield() bool {
	jobs, err := s.store.ListScanJobs()
	if err != nil {
		return false
	}
	for _, job := range jobs {
		if job.Status == "running" || job.Status == "pause_requested" || job.Status == "cancel_requested" {
			return true
		}
	}
	return false
}

func (s *Service) generateThumbnailJob(ctx context.Context, job domain.ThumbnailJob) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	book, err := s.store.BookByID(job.BookID)
	if err != nil {
		return err
	}
	cachePath, err := s.bookThumbnailCachePath(book.ID, job.Size, job.CacheKey)
	if err != nil {
		return err
	}
	if _, err := os.Stat(cachePath); err == nil {
		info, statErr := os.Stat(cachePath)
		if statErr != nil {
			return statErr
		}
		return s.store.CompleteThumbnailJob(job.ID, cachePath, "image/jpeg", 0, 0, info.Size())
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return err
	}
	imageStream, err := s.openBookThumbnailSource(book)
	if err != nil {
		if book.Format == "pdf" {
			return fmt.Errorf("render pdf thumbnail source: %w", err)
		}
		return s.writeGenericThumbnailJob(job, book, cachePath, err)
	}
	defer imageStream.Body.Close()

	img, _, err := image.Decode(io.LimitReader(imageStream.Body, 64<<20))
	if err != nil {
		if book.Format == "pdf" {
			return fmt.Errorf("decode pdf thumbnail source: %w", err)
		}
		return s.writeGenericThumbnailJob(job, book, cachePath, fmt.Errorf("decode thumbnail source: %w", err))
	}
	resized := resizeImageToMaxWidth(cropImageToAspect(img, thumbnailTargetAspectRatio), thumbnailMaxWidth(job.Size))
	return s.writeThumbnailImage(job.ID, cachePath, resized)
}

func (s *Service) writeGenericThumbnailJob(job domain.ThumbnailJob, book domain.Book, cachePath string, sourceErr error) error {
	_ = sourceErr
	return s.writeThumbnailImage(job.ID, cachePath, genericBookThumbnail(book, job.Size))
}

func (s *Service) writeThumbnailImage(jobID int64, cachePath string, img image.Image) error {
	tmpPath := cachePath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	encodeErr := jpeg.Encode(file, img, &jpeg.Options{Quality: thumbnailJPEGQuality})
	closeErr := file.Close()
	if encodeErr != nil {
		_ = os.Remove(tmpPath)
		return encodeErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return closeErr
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	info, err := os.Stat(cachePath)
	if err != nil {
		return err
	}
	bounds := img.Bounds()
	if err := s.store.CompleteThumbnailJob(jobID, cachePath, "image/jpeg", bounds.Dx(), bounds.Dy(), info.Size()); err != nil {
		return err
	}
	s.invalidateThumbnailCacheStatus()
	return nil
}

func (s *Service) openBookThumbnailSource(book domain.Book) (PageStream, error) {
	if book.Format == "epub" {
		body, contentType, err := archive.OpenEPUBCover(book.FilePath)
		if err != nil {
			return PageStream{}, err
		}
		return PageStream{Body: body, ContentType: contentType}, nil
	}
	if book.Format == "pdf" {
		return renderPDFCover(book.FilePath, pdfThumbnailRenderDPI)
	}
	body, contentType, err := archive.OpenCover(book.FilePath)
	if err != nil {
		return PageStream{}, err
	}
	return PageStream{Body: body, ContentType: contentType}, nil
}

func preferSourceThumbnailFallback(book domain.Book) bool {
	if book.Format == "pdf" {
		return true
	}
	return bookThumbnailSourceCacheMarker(book) != ""
}

func (s *Service) bookThumbnailCacheKey(book domain.Book, size string) (string, error) {
	info, err := os.Stat(book.FilePath)
	if err != nil {
		return "", err
	}
	source := fmt.Sprintf("%d|%s|%s|%d|%s|%s|%s|%s", book.ID, book.FilePath, book.Format, info.Size(), info.ModTime().UTC().Format(time.RFC3339Nano), normalizeBookThumbnailSize(size), thumbnailAlgorithmVersion, thumbnailCacheKeyProfile)
	if marker := bookThumbnailSourceCacheMarker(book); marker != "" {
		source += "|" + marker
	}
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])[:20], nil
}

func bookThumbnailSourceCacheMarker(book domain.Book) string {
	if book.Format == "pdf" {
		return pdfThumbnailSourceCacheMarker()
	}
	if strings.TrimSpace(book.FilePath) == "" {
		return ""
	}
	if book.Format == "epub" {
		manifest, err := archive.ReadEPUBManifest(book.FilePath)
		if err != nil || strings.TrimSpace(manifest.CoverHref) == "" {
			return ""
		}
		return "epub-cover:" + manifest.CoverHref
	}
	if book.Format != "zip" && book.Format != "cbz" {
		return ""
	}
	info, err := archive.CoverInfo(book.FilePath)
	if err != nil || strings.TrimSpace(info.Name) == "" {
		return ""
	}
	ext := strings.ToLower(filepath.Ext(info.Name))
	if info.Name == info.FirstName && ext != ".webp" {
		return ""
	}
	marker := "archive-cover:" + info.Name
	if ext == ".webp" {
		marker += ":webp-decode:v1"
	}
	return marker
}

func (s *Service) bookThumbnailCachePath(bookID int64, size string, cacheKey string) (string, error) {
	size = normalizeBookThumbnailSize(size)
	cacheKey = strings.TrimSpace(cacheKey)
	if cacheKey == "" {
		return "", fmt.Errorf("thumbnail cache key is required")
	}
	return filepath.Join(s.thumbnailCacheRoot(), size, fmt.Sprintf("%d-%s.jpg", bookID, cacheKey)), nil
}

func (s *Service) thumbnailCacheRoot() string {
	base := s.configDir
	if base == "" {
		base = filepath.Join(os.TempDir(), "foliospace-reader")
	}
	return filepath.Join(base, "cache", "book-thumbnails")
}

func (s *Service) thumbnailCacheStatus() (domain.ThumbnailCacheStatus, error) {
	s.thumbnailCacheStatusMu.Lock()
	if time.Since(s.thumbnailCacheStatusTime) < 5*time.Second {
		cached := s.thumbnailCacheStatusSnap
		s.thumbnailCacheStatusMu.Unlock()
		return cached, nil
	}
	s.thumbnailCacheStatusMu.Unlock()

	status := domain.ThumbnailCacheStatus{
		AlgorithmVersion: thumbnailAlgorithmVersion,
		SmallWidth:       thumbnailMaxWidth("small"),
		MediumWidth:      thumbnailMaxWidth("medium"),
		TargetAspect:     thumbnailTargetAspectRatio,
	}
	entries, err := s.store.ListReadyThumbnailCacheEntries()
	if err != nil {
		return status, err
	}
	referenced := make(map[string]bool, len(entries))
	for _, entry := range entries {
		cachePath := filepath.Clean(strings.TrimSpace(entry.CachePath))
		if cachePath == "" {
			continue
		}
		referenced[cachePath] = true
		info, err := os.Stat(cachePath)
		if err == nil && !info.IsDir() {
			status.ReadyFiles++
			status.ReadyBytes += info.Size()
			if s.isStaleThumbnailCacheEntry(entry) {
				status.StaleFiles++
				status.StaleBytes += info.Size()
			}
			continue
		}
		if os.IsNotExist(err) {
			status.MissingFiles++
		}
	}

	root := s.thumbnailCacheRoot()
	if _, err := os.Stat(root); os.IsNotExist(err) {
		s.setThumbnailCacheStatus(status)
		return status, nil
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		cachePath := filepath.Clean(path)
		status.Files++
		status.Bytes += info.Size()
		if !referenced[cachePath] {
			status.OrphanFiles++
			status.OrphanBytes += info.Size()
		}
		return nil
	})
	if err == nil {
		s.setThumbnailCacheStatus(status)
	}
	return status, err
}

func (s *Service) readyThumbnailCachePaths() (map[string]bool, error) {
	entries, err := s.store.ListReadyThumbnailCacheEntries()
	if err != nil {
		return nil, err
	}
	referenced := make(map[string]bool, len(entries))
	for _, entry := range entries {
		cachePath := filepath.Clean(strings.TrimSpace(entry.CachePath))
		if cachePath != "" {
			referenced[cachePath] = true
		}
	}
	return referenced, nil
}

func (s *Service) isStaleThumbnailCacheEntry(entry domain.ThumbnailCacheEntry) bool {
	book, err := s.store.BookByID(entry.BookID)
	if err != nil {
		return false
	}
	cacheKey, err := s.bookThumbnailCacheKey(book, entry.Size)
	if err != nil {
		return false
	}
	return cacheKey != strings.TrimSpace(entry.CacheKey)
}

func (s *Service) setThumbnailCacheStatus(status domain.ThumbnailCacheStatus) {
	s.thumbnailCacheStatusMu.Lock()
	defer s.thumbnailCacheStatusMu.Unlock()
	s.thumbnailCacheStatusSnap = status
	s.thumbnailCacheStatusTime = time.Now()
}

func (s *Service) invalidateThumbnailCacheStatus() {
	s.thumbnailCacheStatusMu.Lock()
	defer s.thumbnailCacheStatusMu.Unlock()
	s.thumbnailCacheStatusTime = time.Time{}
}

func normalizeBookThumbnailSize(size string) string {
	switch strings.ToLower(strings.TrimSpace(size)) {
	case "medium":
		return "medium"
	default:
		return "small"
	}
}

func thumbnailPriorityForSize(size string) int {
	if normalizeBookThumbnailSize(size) == "medium" {
		return 80
	}
	return 100
}

func thumbnailMaxWidth(size string) int {
	if normalizeBookThumbnailSize(size) == "medium" {
		return 640
	}
	return 320
}

func cropImageToAspect(src image.Image, targetAspect float64) image.Image {
	bounds := src.Bounds()
	srcWidth := bounds.Dx()
	srcHeight := bounds.Dy()
	if srcWidth <= 0 || srcHeight <= 0 || targetAspect <= 0 {
		return src
	}
	srcAspect := float64(srcWidth) / float64(srcHeight)
	crop := bounds
	if srcAspect > targetAspect {
		cropWidth := int(math.Round(float64(srcHeight) * targetAspect))
		if cropWidth < 1 {
			cropWidth = 1
		}
		x0 := bounds.Min.X + (srcWidth-cropWidth)/2
		crop = image.Rect(x0, bounds.Min.Y, x0+cropWidth, bounds.Max.Y)
	} else if srcAspect < targetAspect {
		cropHeight := int(math.Round(float64(srcWidth) / targetAspect))
		if cropHeight < 1 {
			cropHeight = 1
		}
		y0 := bounds.Min.Y + (srcHeight-cropHeight)/2
		crop = image.Rect(bounds.Min.X, y0, bounds.Max.X, y0+cropHeight)
	}
	if crop.Eq(bounds) {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	for y := 0; y < crop.Dy(); y++ {
		for x := 0; x < crop.Dx(); x++ {
			dst.Set(x, y, src.At(crop.Min.X+x, crop.Min.Y+y))
		}
	}
	return dst
}

func resizeImageToMaxWidth(src image.Image, maxWidth int) image.Image {
	bounds := src.Bounds()
	srcWidth := bounds.Dx()
	srcHeight := bounds.Dy()
	if srcWidth <= 0 || srcHeight <= 0 {
		return src
	}
	dstWidth := srcWidth
	dstHeight := srcHeight
	if srcWidth > maxWidth {
		scale := float64(maxWidth) / float64(srcWidth)
		dstWidth = maxWidth
		dstHeight = int(math.Round(float64(srcHeight) * scale))
		if dstHeight < 1 {
			dstHeight = 1
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstWidth, dstHeight))
	for y := 0; y < dstHeight; y++ {
		srcY := bounds.Min.Y + int(float64(y)*float64(srcHeight)/float64(dstHeight))
		if srcY >= bounds.Max.Y {
			srcY = bounds.Max.Y - 1
		}
		for x := 0; x < dstWidth; x++ {
			srcX := bounds.Min.X + int(float64(x)*float64(srcWidth)/float64(dstWidth))
			if srcX >= bounds.Max.X {
				srcX = bounds.Max.X - 1
			}
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}

func thumbnailPlaceholderSVG(size string) string {
	width := 240
	height := 332
	if normalizeBookThumbnailSize(size) == "medium" {
		width = 420
		height = 580
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
		<rect width="%d" height="%d" fill="#dfe7ea"/>
		<rect x="18" y="18" width="%d" height="%d" rx="12" fill="#eef3f5" stroke="#c4d0d5" stroke-width="2"/>
		<text x="%d" y="%d" text-anchor="middle" font-family="Arial, sans-serif" font-size="18" font-weight="700" fill="#526269">Generating</text>
	</svg>`, width, height, width, height, width, height, width-36, height-36, width/2, height/2)
}

func genericBookThumbnail(book domain.Book, size string) image.Image {
	width := thumbnailMaxWidth(size)
	height := int(math.Round(float64(width) / thumbnailTargetAspectRatio))
	if height < 1 {
		height = width
	}
	seed := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(book.Title)) + "|" + strings.ToLower(strings.TrimSpace(book.Format))))
	base := genericCoverBaseColor(book.Format)
	accent := color.RGBA{
		R: uint8(72 + int(seed[0])%92),
		G: uint8(90 + int(seed[1])%90),
		B: uint8(100 + int(seed[2])%86),
		A: 255,
	}
	light := color.RGBA{R: 244, G: 247, B: 247, A: 255}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		ratio := float64(y) / float64(maxInt(1, height-1))
		row := blendRGBA(light, base, ratio*0.82)
		for x := 0; x < width; x++ {
			img.Set(x, y, row)
		}
	}
	fillRect(img, 0, 0, maxInt(12, width/18), height, darkenRGBA(base, 0.72))
	fillRect(img, width/10, height/11, width-width/5, maxInt(18, height/36), accent)
	fillRect(img, width/10, height/11+height/18, width-width/5, maxInt(8, height/90), color.RGBA{R: 255, G: 255, B: 255, A: 180})
	panelTop := height / 4
	panelHeight := height / 3
	fillRect(img, width/5, panelTop, width*3/5, panelHeight, color.RGBA{R: 255, G: 255, B: 255, A: 205})
	fillRect(img, width/5+width/18, panelTop+height/16, width*3/5-width/9, maxInt(10, height/70), blendRGBA(accent, light, 0.28))
	fillRect(img, width/5+width/18, panelTop+height/16+height/22, width*3/5-width/9, maxInt(8, height/86), color.RGBA{R: 202, G: 216, B: 218, A: 210})
	fillRect(img, width/5+width/18, panelTop+height/16+height/11, width*3/5-width/9, maxInt(8, height/86), color.RGBA{R: 202, G: 216, B: 218, A: 190})
	fillRect(img, width/10, height-height/7, width-width/5, maxInt(14, height/48), darkenRGBA(accent, 0.78))
	drawFrame(img, color.RGBA{R: 80, G: 94, B: 98, A: 95})
	return img
}

func genericCoverBaseColor(format string) color.RGBA {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "epub":
		return color.RGBA{R: 225, G: 235, B: 229, A: 255}
	case "pdf":
		return color.RGBA{R: 235, G: 230, B: 220, A: 255}
	default:
		return color.RGBA{R: 223, G: 232, B: 235, A: 255}
	}
}

func blendRGBA(left color.RGBA, right color.RGBA, ratio float64) color.RGBA {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	return color.RGBA{
		R: uint8(float64(left.R)*(1-ratio) + float64(right.R)*ratio),
		G: uint8(float64(left.G)*(1-ratio) + float64(right.G)*ratio),
		B: uint8(float64(left.B)*(1-ratio) + float64(right.B)*ratio),
		A: 255,
	}
}

func darkenRGBA(value color.RGBA, ratio float64) color.RGBA {
	return color.RGBA{
		R: uint8(float64(value.R) * ratio),
		G: uint8(float64(value.G) * ratio),
		B: uint8(float64(value.B) * ratio),
		A: value.A,
	}
}

func fillRect(img *image.RGBA, x int, y int, width int, height int, fill color.RGBA) {
	bounds := img.Bounds()
	x0 := clampInt(x, bounds.Min.X, bounds.Max.X)
	y0 := clampInt(y, bounds.Min.Y, bounds.Max.Y)
	x1 := clampInt(x+width, bounds.Min.X, bounds.Max.X)
	y1 := clampInt(y+height, bounds.Min.Y, bounds.Max.Y)
	for yy := y0; yy < y1; yy++ {
		for xx := x0; xx < x1; xx++ {
			img.Set(xx, yy, fill)
		}
	}
}

func drawFrame(img *image.RGBA, stroke color.RGBA) {
	bounds := img.Bounds()
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		img.Set(x, bounds.Min.Y, stroke)
		img.Set(x, bounds.Max.Y-1, stroke)
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		img.Set(bounds.Min.X, y, stroke)
		img.Set(bounds.Max.X-1, y, stroke)
	}
}

func clampInt(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
