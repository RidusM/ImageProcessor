package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"img-processor/internal/entity"
	"img-processor/internal/repository"

	"github.com/google/uuid"
	"github.com/wb-go/wbf/logger"
)

const (
	_slowOperationThreshold = 500 * time.Millisecond
	_defaultMaxFileSizeMB   = 32
	_defaultWorkerCount     = 4
	_defaultQueueSize       = 100
	_defaultMaxWidth        = 1920
	_defaultThumbSize       = 300
	_defaultJPEGQuality     = 90
	_defaultTimeout         = 30 * time.Second
	_processingTimeout      = 5 * time.Minute

	_maxRetryAttempts = 3
	_retryDelay       = 2 * time.Second
)

type (
	ImageRepository = repository.ImageRepository

	ImageProcessor interface {
		Resize(ctx context.Context, src io.Reader, ext string, maxWidth int) (io.Reader, error)
		CreateThumbnail(ctx context.Context, src io.Reader, ext string, size int) (io.Reader, error)
		AddWatermark(ctx context.Context, src io.Reader, ext string, watermarkPath string) (io.Reader, error)
		ConvertFormat(ctx context.Context, src io.Reader, srcExt, dstExt string, quality int) (io.Reader, error)
	}

	WorkerPool interface {
		Submit(ctx context.Context, task *entity.Task) error
		Stop(ctx context.Context)
		Status(id string) (*entity.Task, error)
	}

	ProcessorService struct {
		repo      ImageRepository
		processor ImageProcessor
		workers   WorkerPool
		log       logger.Logger

		maxFileSizeMB   int64
		workerCount     int
		queueSize       int
		maxWidth        int
		thumbSize       int
		jpegQuality     int
		enableWatermark bool
		watermarkPath   string
		allowedExts     map[string]struct{}
	}

	UploadRequest struct {
		File     *multipart.FileHeader
		Options  *entity.ProcessingOptions
		ClientID string
	}

	UploadResponse struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Filename string `json:"filename"`
	}

	ProcessingStats struct {
		Total     int           `json:"total"`
		Pending   int           `json:"pending"`
		Processing int          `json:"processing"`
		Done      int           `json:"done"`
		Failed    int           `json:"failed"`
		AvgDuration time.Duration `json:"avg_duration"`
	}
)

func NewProcessorService(
	repo ImageRepository,
	processor ImageProcessor,
	workers WorkerPool,
	log logger.Logger,
	opts ...Option,
) (*ProcessorService, error) {
	const op = "service.processor.NewProcessorService"

	s := &ProcessorService{
		repo:            repo,
		processor:       processor,
		workers:         workers,
		log:             log,
		maxFileSizeMB:   _defaultMaxFileSizeMB,
		workerCount:     _defaultWorkerCount,
		queueSize:       _defaultQueueSize,
		maxWidth:        _defaultMaxWidth,
		thumbSize:       _defaultThumbSize,
		jpegQuality:     _defaultJPEGQuality,
		enableWatermark: true,
		allowedExts:     make(map[string]struct{}),
	}

	for _, ext := range entity.SupportedExtensions() {
		s.allowedExts[strings.ToLower(ext)] = struct{}{}
	}

	for _, opt := range opts {
		opt(s)
	}

	if err := s.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return s, nil
}

func (s *ProcessorService) Upload(ctx context.Context, req UploadRequest) (*UploadResponse, error) {
	const op = "service.processor.Upload"

	log := s.log.Ctx(ctx).With("op", op)
	startTime := time.Now()

	defer s.logSlowOperation(ctx, op, startTime,
		logger.String("client_id", req.ClientID),
		logger.String("filename", req.File.Filename),
	)

	log.LogAttrs(ctx, logger.InfoLevel, "upload started",
		logger.String("filename", req.File.Filename),
		logger.Int64("size", req.File.Size),
		logger.String("client_id", req.ClientID),
	)

	if err := s.validateUploadRequest(req); err != nil {
		log.LogAttrs(ctx, logger.ErrorLevel, "validation failed",
			logger.Any("error", err),
		)
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	imageID := uuid.NewString()
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(req.File.Filename)), ".")

	file, err := req.File.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: open file: %w", op, err)
	}
	defer file.Close()

	originalPath, err := s.repo.SaveOriginal(ctx, imageID, ext, file)
	if err != nil {
		log.LogAttrs(ctx, logger.ErrorLevel, "save original failed",
			logger.Any("error", err),
		)
		return nil, fmt.Errorf("%s: save original: %w", op, err)
	}

	procOpts := req.Options
	if procOpts == nil {
		procOpts = entity.DefaultProcessingOptions()
	}

	task := entity.NewTask(imageID, procOpts)
	task.ID = imageID

	if err := s.workers.Submit(ctx, task); err != nil {
		log.LogAttrs(ctx, logger.ErrorLevel, "submit to worker pool failed",
			logger.Any("error", err),
		)
		_ = s.repo.Delete(ctx, imageID, ext)
		return nil, fmt.Errorf("%s: submit task: %w", op, err)
	}

	log.LogAttrs(ctx, logger.InfoLevel, "upload completed",
		logger.String("image_id", imageID),
		logger.String("original_path", originalPath),
		logger.Duration("duration", time.Since(startTime)),
	)

	return &UploadResponse{
		ID:       imageID,
		Status:   string(entity.TaskStatusPending),
		Filename: req.File.Filename,
	}, nil
}

func (s *ProcessorService) GetStatus(ctx context.Context, imageID string) (*entity.Task, error) {
	const op = "service.processor.GetStatus"

	log := s.log.Ctx(ctx).With("op", op, "image_id", imageID)
	startTime := time.Now()

	defer s.logSlowOperation(ctx, op, startTime,
		logger.String("image_id", imageID),
	)

	log.LogAttrs(ctx, logger.DebugLevel, "get status requested")

	task, err := s.workers.Status(imageID)
	if err != nil && !errors.Is(err, entity.ErrTaskNotFound) {
		log.LogAttrs(ctx, logger.ErrorLevel, "failed to get status from workers",
			logger.Any("error", err),
		)
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if task != nil {
		return task, nil
	}


	for _, ext := range entity.SupportedExtensions() {
		exists, err := s.repo.Exists(ctx, imageID, ext)
		if err != nil {
			log.LogAttrs(ctx, logger.WarnLevel, "exists check failed",
				logger.String("ext", ext),
				logger.Any("error", err),
			)
			continue
		}
		if exists {
			doneTime := time.Now()
			return &entity.Task{
				ID:          imageID,
				ImageID:     imageID,
				Status:      entity.TaskStatusDone,
				Progress:    100,
				CompletedAt: &doneTime,
			}, nil
		}
	}

	return nil, entity.ErrImageNotFound
}

func (s *ProcessorService) GetImage(ctx context.Context, imageID, version string) (io.ReadCloser, int64, string, error) {
	const op = "service.processor.GetImage"

	log := s.log.Ctx(ctx).With("op", op, "image_id", imageID, "version", version)
	startTime := time.Now()

	defer s.logSlowOperation(ctx, op, startTime,
		logger.String("image_id", imageID),
		logger.String("version", version),
	)

	log.LogAttrs(ctx, logger.DebugLevel, "get image requested")

	var ext string
	for _, e := range entity.SupportedExtensions() {
		exists, _ := s.repo.Exists(ctx, imageID, e)
		if exists {
			ext = e
			break
		}
	}
	if ext == "" {
		return nil, 0, "", entity.ErrImageNotFound
	}

	var reader io.ReadCloser
	var size int64
	var err error

	switch version {
	case "thumb", "thumbnail":
		reader, size, err = s.repo.GetThumbnail(ctx, imageID, ext)
	case "original":
		reader, size, err = s.repo.GetOriginal(ctx, imageID, ext)
	case "processed", "":
		reader, size, err = s.repo.GetProcessed(ctx, imageID, ext)
	default:
		return nil, 0, "", fmt.Errorf("%s: unknown version: %s", op, version)
	}

	if err != nil {
		if errors.Is(err, entity.ErrFileNotFound) {
			return nil, 0, "", entity.ErrImageNotFound
		}
		log.LogAttrs(ctx, logger.ErrorLevel, "failed to get image",
			logger.Any("error", err),
		)
		return nil, 0, "", fmt.Errorf("%s: %w", op, err)
	}

	log.LogAttrs(ctx, logger.DebugLevel, "image retrieved",
		logger.Int64("size", size),
		logger.Duration("duration", time.Since(startTime)),
	)

	return reader, size, ext, nil
}

func (s *ProcessorService) Delete(ctx context.Context, imageID string) error {
	const op = "service.processor.Delete"

	log := s.log.Ctx(ctx).With("op", op, "image_id", imageID)
	startTime := time.Now()

	defer s.logSlowOperation(ctx, op, startTime,
		logger.String("image_id", imageID),
	)

	log.LogAttrs(ctx, logger.InfoLevel, "delete requested")

	var ext string
	for _, e := range entity.SupportedExtensions() {
		exists, _ := s.repo.Exists(ctx, imageID, e)
		if exists {
			ext = e
			break
		}
	}
	if ext == "" {
		log.LogAttrs(ctx, logger.DebugLevel, "image not found, considered deleted")
		return nil
	}

	if err := s.repo.Delete(ctx, imageID, ext); err != nil {
		log.LogAttrs(ctx, logger.ErrorLevel, "delete failed",
			logger.Any("error", err),
		)
		return fmt.Errorf("%s: %w", op, err)
	}

	log.LogAttrs(ctx, logger.InfoLevel, "image deleted",
		logger.Duration("duration", time.Since(startTime)),
	)

	return nil
}

func (s *ProcessorService) GetStats(ctx context.Context) (*ProcessingStats, error) {
	const op = "service.processor.GetStats"

	log := s.log.Ctx(ctx).With("op", op)
	startTime := time.Now()

	defer s.logSlowOperation(ctx, op, startTime)

	log.LogAttrs(ctx, logger.DebugLevel, "get stats requested")

	return &ProcessingStats{
		Total:     0,
		Pending:   0,
		Processing: 0,
		Done:      0,
		Failed:    0,
		AvgDuration: 0,
	}, nil
}

func (s *ProcessorService) processTask(ctx context.Context, task *entity.Task) error {
	const op = "service.processor.processTask"

	log := s.log.Ctx(ctx).With("op", op, "task_id", task.ID)
	startTime := time.Now()

	task.MarkProcessing()
	log.LogAttrs(ctx, logger.InfoLevel, "processing started")

	var ext string
	for _, e := range entity.SupportedExtensions() {
		exists, _ := s.repo.Exists(ctx, task.ImageID, e)
		if exists {
			ext = e
			break
		}
	}
	if ext == "" {
		task.MarkError("image extension not found")
		return fmt.Errorf("%s: %w", op, entity.ErrInvalidImageFormat)
	}

	original, size, err := s.repo.GetOriginal(ctx, task.ImageID, ext)
	if err != nil {
		task.MarkError(fmt.Sprintf("get original: %v", err))
		return fmt.Errorf("%s: get original: %w", op, err)
	}
	defer original.Close()

	log.LogAttrs(ctx, logger.DebugLevel, "original opened",
		logger.Int64("size", size),
	)

	var current io.Reader = original
	if task.Options.ResizeWidth > 0 {
		resized, err := s.processor.Resize(ctx, current, ext, task.Options.ResizeWidth)
		if err != nil {
			task.MarkError(fmt.Sprintf("resize: %v", err))
			return fmt.Errorf("%s: resize: %w", op, err)
		}
		current = resized
		task.Progress = 40
	}

	if task.Options.AddWatermark && s.enableWatermark && s.watermarkPath != "" {
		watermarked, err := s.processor.AddWatermark(ctx, current, ext, s.watermarkPath)
		if err != nil {
			task.MarkError(fmt.Sprintf("watermark: %v", err))
			return fmt.Errorf("%s: watermark: %w", op, err)
		}
		current = watermarked
		task.Progress = 70
	}

	processedPath, err := s.repo.SaveProcessed(ctx, task.ImageID, ext, current)
	if err != nil {
		task.MarkError(fmt.Sprintf("save processed: %v", err))
		return fmt.Errorf("%s: save processed: %w", op, err)
	}
	task.Progress = 90
	log.LogAttrs(ctx, logger.DebugLevel, "processed saved",
		logger.String("path", processedPath),
	)

	originalForThumb, _, err := s.repo.GetOriginal(ctx, task.ImageID, ext)
	if err != nil {
		task.MarkError(fmt.Sprintf("get original for thumb: %v", err))
		return fmt.Errorf("%s: get original for thumb: %w", op, err)
	}
	defer originalForThumb.Close()

	thumb, err := s.processor.CreateThumbnail(ctx, originalForThumb, ext, task.Options.ThumbnailSize)
	if err != nil {
		task.MarkError(fmt.Sprintf("create thumbnail: %v", err))
		return fmt.Errorf("%s: create thumbnail: %w", op, err)
	}

	thumbPath, err := s.repo.SaveThumbnail(ctx, task.ImageID, ext, thumb)
	if err != nil {
		task.MarkError(fmt.Sprintf("save thumbnail: %v", err))
		return fmt.Errorf("%s: save thumbnail: %w", op, err)
	}
	task.Progress = 100

	task.MarkDone()
	log.LogAttrs(ctx, logger.InfoLevel, "processing completed",
		logger.String("processed_path", processedPath),
		logger.String("thumbnail_path", thumbPath),
		logger.Duration("duration", time.Since(startTime)),
	)

	return nil
}

func (s *ProcessorService) validateUploadRequest(req UploadRequest) error {
	if req.File == nil {
		return fmt.Errorf("file is required: %w", entity.ErrFileNotFound)
	}
	if req.File.Size == 0 {
		return fmt.Errorf("file is empty: %w", entity.ErrInvalidFileSize)
	}
	if req.File.Size > s.maxFileSizeMB*1024*1024 {
		return fmt.Errorf("file too large (max %dMB): %w", s.maxFileSizeMB, entity.ErrInvalidFileSize)
	}

	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(req.File.Filename)), ".")
	if ext == "" {
		return fmt.Errorf("file extension is required: %w", entity.ErrInvalidImageFormat)
	}
	if _, ok := s.allowedExts[ext]; !ok {
		return fmt.Errorf("%w: supported: %v", entity.ErrUnsupportedFormat, entity.SupportedExtensions())
	}

	if req.Options != nil {
		if err := req.Options.Validate(); err != nil {
			return fmt.Errorf("invalid processing options: %w", err)
		}
	}

	return nil
}

func (s *ProcessorService) logSlowOperation(
	ctx context.Context,
	op string,
	startTime time.Time,
	attrs ...logger.Attr,
) {
	duration := time.Since(startTime)
	if duration > _slowOperationThreshold {
		allAttrs := append([]logger.Attr{
			logger.String("op", op),
			logger.Duration("duration", duration),
		}, attrs...)
		s.log.Ctx(ctx).LogAttrs(ctx, logger.WarnLevel, "slow operation detected", allAttrs...)
	}
}

func (s *ProcessorService) Shutdown(ctx context.Context) error {
	const op = "service.processor.Shutdown"

	log := s.log.Ctx(ctx).With("op", op)
	log.LogAttrs(ctx, logger.InfoLevel, "shutdown started")

	if s.workers != nil {
		s.workers.Stop(ctx)
	}

	log.LogAttrs(ctx, logger.InfoLevel, "shutdown completed")
	return nil
}