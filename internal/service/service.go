package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"img-processor/internal/entity"

	"github.com/google/uuid"
	"github.com/wb-go/wbf/logger"
)

const (
	_defaultMaxFileSizeMB   = 32
	_slowOperationThreshold = 500 * time.Millisecond
	_progressComplete       = 100
)

type (
	ImageRepository interface {
		SaveOriginal(ctx context.Context, id uuid.UUID, ext string, src io.Reader, originalName string) (string, error)
		SaveProcessed(ctx context.Context, id uuid.UUID, ext string, src io.Reader) (string, error)
		SaveThumbnail(ctx context.Context, id uuid.UUID, ext string, src io.Reader) (string, error)
		GetOriginal(ctx context.Context, id uuid.UUID, ext string) (io.ReadCloser, int64, error)
		GetProcessed(ctx context.Context, id uuid.UUID, ext string) (io.ReadCloser, int64, error)
		GetThumbnail(ctx context.Context, id uuid.UUID, ext string) (io.ReadCloser, int64, error)
		Delete(ctx context.Context, id uuid.UUID, ext string) error
		Exists(ctx context.Context, id uuid.UUID, ext string) (bool, error)
		List(ctx context.Context) ([]entity.ImageMeta, error)
	}

	ImageProcessor interface {
		Resize(ctx context.Context, src io.Reader, ext string, maxWidth int) (io.Reader, error)
		CreateThumbnail(ctx context.Context, src io.Reader, ext string, size int) (io.Reader, error)
		AddWatermark(ctx context.Context, src io.Reader, ext string, watermarkPath string) (io.Reader, error)
	}

	TaskPublisher interface {
		Publish(ctx context.Context, task *entity.Task) error
	}

	UploadRequest struct {
		File     *multipart.FileHeader
		Options  *entity.ProcessingOptions
		ClientID string
	}

	UploadResponse struct {
		ID       uuid.UUID
		Status   string
		Filename string
		Size     int64
		Message  string
	}

	ImageListItem struct {
		ID           uuid.UUID
		Ext          string
		OriginalName string
		Status       string
		Progress     int
	}

	ProcessorService struct {
		repo      ImageRepository
		processor ImageProcessor
		publisher TaskPublisher
		log       logger.Logger

		maxFileSizeMB   int64
		enableWatermark bool
		watermarkPath   string
		allowedExts     map[string]struct{}
		statuses        sync.Map
	}
)

func NewProcessorService(
	repo ImageRepository,
	processor ImageProcessor,
	publisher TaskPublisher,
	log logger.Logger,
	opts ...Option,
) *ProcessorService {
	s := &ProcessorService{
		repo:            repo,
		processor:       processor,
		publisher:       publisher,
		log:             log,
		maxFileSizeMB:   _defaultMaxFileSizeMB,
		enableWatermark: true,
		allowedExts:     make(map[string]struct{}),
	}

	for _, ext := range entity.SupportedExtensions() {
		s.allowedExts[strings.ToLower(ext)] = struct{}{}
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

func (s *ProcessorService) SetKafka(publisher TaskPublisher) {
	s.publisher = publisher
}

func (s *ProcessorService) Upload(ctx context.Context, req UploadRequest) (*UploadResponse, error) {
	const op = "service.Upload"
	log := s.log.With("op", op)
	startTime := time.Now()
	defer s.logSlowOperation(ctx, op, startTime,
		logger.String("filename", req.File.Filename),
		logger.String("client_id", req.ClientID),
	)

	log.LogAttrs(ctx, logger.InfoLevel, "upload started",
		logger.String("filename", req.File.Filename),
		logger.Int64("size", req.File.Size),
	)

	if err := s.validateUploadRequest(req); err != nil {
		log.LogAttrs(ctx, logger.ErrorLevel, "validation failed", logger.Any("error", err))
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	imageID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("%s: generate id: %w", op, err)
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(req.File.Filename), "."))

	file, err := req.File.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: open file: %w", op, err)
	}
	defer file.Close()

	if _, err = s.repo.SaveOriginal(ctx, imageID, ext, file, req.File.Filename); err != nil {
		return nil, fmt.Errorf("%s: save original: %w", op, err)
	}

	procOpts := req.Options
	if procOpts == nil {
		procOpts = entity.DefaultProcessingOptions()
	}

	task := entity.NewTask(imageID, procOpts)
	s.statuses.Store(imageID.String(), task)

	if s.publisher == nil {
		return nil, fmt.Errorf("%s: publisher is not configured", op)
	}
	if err = s.publisher.Publish(ctx, task); err != nil {
		if delErr := s.repo.Delete(ctx, imageID, ext); delErr != nil {
			log.LogAttrs(ctx, logger.WarnLevel, "rollback delete failed",
				logger.Any("image_id", imageID),
				logger.Any("error", delErr),
			)
		}
		return nil, fmt.Errorf("%s: publish task: %w", op, err)
	}

	log.LogAttrs(ctx, logger.InfoLevel, "upload completed",
		logger.Any("image_id", imageID),
		logger.Duration("duration", time.Since(startTime)),
	)

	return &UploadResponse{
		ID:       imageID,
		Status:   string(entity.StatusPending),
		Filename: req.File.Filename,
		Size:     req.File.Size,
		Message:  "Image uploaded successfully",
	}, nil
}

func (s *ProcessorService) GetStatus(ctx context.Context, imageID uuid.UUID) (*entity.Task, error) {
	const op = "service.GetStatus"
	log := s.log.With("op", op, "image_id", imageID)
	startTime := time.Now()
	defer s.logSlowOperation(ctx, op, startTime, logger.Any("image_id", imageID))

	log.LogAttrs(ctx, logger.DebugLevel, "get status requested", logger.Any("image_id", imageID))

	val, ok := s.statuses.Load(imageID.String())
	if !ok {
		ext := s.detectImageExtension(ctx, imageID)
		if ext != "" {
			now := time.Now()
			return &entity.Task{
				ID:          imageID,
				ImageID:     imageID,
				Status:      entity.StatusDone,
				Progress:    _progressComplete,
				CompletedAt: &now,
			}, nil
		}
		return nil, entity.ErrTaskNotFound
	}

	task, ok := val.(*entity.Task)
	if !ok {
		return nil, fmt.Errorf("%s: invalid task type in status map", op)
	}
	return task, nil
}

func (s *ProcessorService) GetImage(
	ctx context.Context,
	imageID uuid.UUID,
	version string,
) (io.ReadCloser, int64, string, error) {
	const op = "service.GetImage"
	ext := s.detectImageExtension(ctx, imageID)
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
		return nil, 0, "", fmt.Errorf("%s: unknown version %q", op, version)
	}

	if err != nil {
		if errors.Is(err, entity.ErrFileNotFound) {
			return nil, 0, "", entity.ErrImageNotFound
		}
		return nil, 0, "", fmt.Errorf("%s: %w", op, err)
	}

	return reader, size, ext, nil
}

func (s *ProcessorService) ListImages(ctx context.Context) ([]ImageListItem, error) {
	const op = "service.ListImages"

	metas, err := s.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	result := make([]ImageListItem, 0, len(metas))
	for _, m := range metas {
		item := ImageListItem{
			ID:  m.ID,
			Ext: m.Ext,
		}
		if val, ok := s.statuses.Load(m.ID.String()); ok {
			var task *entity.Task
			if task, ok = val.(*entity.Task); ok {
				item.Status = string(task.Status)
				item.Progress = task.Progress
			}
		} else {
			item.Status = string(entity.StatusDone)
			item.Progress = 100
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *ProcessorService) Delete(ctx context.Context, imageID uuid.UUID) error {
	const op = "service.Delete"
	log := s.log.With("op", op, "image_id", imageID)
	startTime := time.Now()
	defer s.logSlowOperation(ctx, op, startTime, logger.Any("image_id", imageID))

	log.LogAttrs(ctx, logger.InfoLevel, "delete requested", logger.Any("image_id", imageID))

	ext := s.detectImageExtension(ctx, imageID)
	if ext == "" {
		log.LogAttrs(ctx, logger.DebugLevel, "image not found, nothing to delete", logger.Any("image_id", imageID))
		return nil
	}

	if err := s.repo.Delete(ctx, imageID, ext); err != nil {
		log.LogAttrs(ctx, logger.ErrorLevel, "delete failed",
			logger.Any("image_id", imageID),
			logger.Any("error", err),
		)
		return fmt.Errorf("%s: %w", op, err)
	}

	log.LogAttrs(ctx, logger.InfoLevel, "image deleted",
		logger.Any("image_id", imageID),
		logger.Duration("duration", time.Since(startTime)),
	)
	return nil
}

func (s *ProcessorService) ProcessTask(ctx context.Context, task *entity.Task) error {
	const op = "service.ProcessTask"
	log := s.log.With("op", op, "task_id", task.ID)
	startTime := time.Now()
	defer s.logSlowOperation(ctx, op, startTime, logger.Any("task_id", task.ID))

	task.MarkProcessing()
	s.statuses.Store(task.ImageID.String(), task)

	log.LogAttrs(ctx, logger.InfoLevel, "processing started", logger.Any("task_id", task.ID))

	ext := s.detectImageExtension(ctx, task.ImageID)
	if ext == "" {
		errMsg := "image extension not found"
		task.MarkError(errMsg)
		s.statuses.Store(task.ImageID.String(), task)
		return fmt.Errorf("%s: detect extension: %w", op, entity.ErrInvalidImageFormat)
	}

	if err := s.saveProcessed(ctx, task, ext); err != nil {
		task.MarkError("processing failed")
		s.statuses.Store(task.ImageID.String(), task)
		return fmt.Errorf("%s: %w", op, err)
	}

	if err := s.createAndSaveThumbnail(ctx, task, ext); err != nil {
		task.MarkError("thumbnail creation failed")
		s.statuses.Store(task.ImageID.String(), task)
		return fmt.Errorf("%s: %w", op, err)
	}

	task.MarkDone()
	s.statuses.Store(task.ImageID.String(), task)

	log.LogAttrs(ctx, logger.InfoLevel, "processing completed",
		logger.Any("task_id", task.ID),
		logger.Duration("duration", time.Since(startTime)),
	)
	return nil
}

func (s *ProcessorService) CleanupMemoryTasks(ctx context.Context, cutoffTime time.Time) int {
	var deletedCount int

	s.statuses.Range(func(_ any, value any) bool {
		task, ok := value.(*entity.Task)
		if !ok || task == nil {
			return true
		}

		if task.CompletedAt != nil && task.CompletedAt.Before(cutoffTime) {
			s.statuses.Delete(task.ImageID.String())
			deletedCount++
		}

		return true
	})

	if deletedCount > 0 {
		s.log.LogAttrs(ctx, logger.InfoLevel, "cleanup in-memory tasks",
			logger.Int("count", deletedCount),
		)
	}

	return deletedCount
}

func (s *ProcessorService) detectImageExtension(ctx context.Context, imageID uuid.UUID) string {
	for _, ext := range entity.SupportedExtensions() {
		exists, _ := s.repo.Exists(ctx, imageID, ext)
		if exists {
			return ext
		}
	}
	return ""
}

func (s *ProcessorService) saveProcessed(ctx context.Context, task *entity.Task, ext string) error {
	original, _, err := s.repo.GetOriginal(ctx, task.ImageID, ext)
	if err != nil {
		return fmt.Errorf("get original: %w", err)
	}
	defer original.Close()

	var current io.Reader = original

	if task.Options != nil && task.Options.ResizeWidth > 0 {
		var resized io.Reader
		resized, err = s.processor.Resize(ctx, current, ext, task.Options.ResizeWidth)
		if err != nil {
			return fmt.Errorf("resize: %w", err)
		}
		current = resized
	}

	addWatermark := s.enableWatermark && s.watermarkPath != "" &&
		(task.Options == nil || task.Options.AddWatermark)
	if addWatermark {
		var watermarked io.Reader
		watermarked, err = s.processor.AddWatermark(ctx, current, ext, s.watermarkPath)
		if err != nil {
			return fmt.Errorf("watermark: %w", err)
		}
		current = watermarked
	}

	if _, err = s.repo.SaveProcessed(ctx, task.ImageID, ext, current); err != nil {
		return fmt.Errorf("save processed: %w", err)
	}

	s.log.LogAttrs(ctx, logger.InfoLevel, "task options",
		logger.Any("options", task.Options),
		logger.String("ext", ext),
	)

	task.Progress = 90
	return nil
}

func (s *ProcessorService) createAndSaveThumbnail(ctx context.Context, task *entity.Task, ext string) error {
	original, _, err := s.repo.GetOriginal(ctx, task.ImageID, ext)
	if err != nil {
		return fmt.Errorf("get original: %w", err)
	}
	defer original.Close()

	thumbSize := entity.DefaultProcessingOptions().ThumbnailSize
	if task.Options != nil && task.Options.ThumbnailSize > 0 {
		thumbSize = task.Options.ThumbnailSize
	}

	thumb, err := s.processor.CreateThumbnail(ctx, original, ext, thumbSize)
	if err != nil {
		return fmt.Errorf("create thumbnail: %w", err)
	}

	if _, err = s.repo.SaveThumbnail(ctx, task.ImageID, ext, thumb); err != nil {
		return fmt.Errorf("save thumbnail: %w", err)
	}

	return nil
}

func (s *ProcessorService) validateUploadRequest(req UploadRequest) error {
	if req.File.Size > s.maxFileSizeMB*1024*1024 {
		return fmt.Errorf("file too large (max %dMB): %w", s.maxFileSizeMB, entity.ErrInvalidFileSize)
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(req.File.Filename), "."))
	if ext == "" {
		return fmt.Errorf("file extension is required: %w", entity.ErrInvalidImageFormat)
	}
	if _, ok := s.allowedExts[ext]; !ok {
		return fmt.Errorf("unsupported format %q: %w", ext, entity.ErrUnsupportedFormat)
	}

	return nil
}

func (s *ProcessorService) logSlowOperation(ctx context.Context, op string, startTime time.Time, attrs ...logger.Attr) {
	duration := time.Since(startTime)
	if duration > _slowOperationThreshold {
		allAttrs := append([]logger.Attr{
			logger.String("op", op),
			logger.Duration("duration", duration),
		}, attrs...)
		s.log.LogAttrs(ctx, logger.WarnLevel, "slow operation detected", allAttrs...)
	}
}
