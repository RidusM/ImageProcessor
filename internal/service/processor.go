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
	_progressComplete       = 100
)

type ImageRepository = repository.ImageRepository

type ImageProcessor interface {
	Resize(ctx context.Context, src io.Reader, ext string, maxWidth int) (io.Reader, error)
	CreateThumbnail(ctx context.Context, src io.Reader, ext string, size int) (io.Reader, error)
	AddWatermark(ctx context.Context, src io.Reader, ext string, watermarkPath string) (io.Reader, error)
}

type WorkerPool interface {
	Submit(ctx context.Context, task *entity.Task) error
	Stop(ctx context.Context)
	Status(id string) (*entity.Task, error)
}

type ProcessorService struct {
	repo      ImageRepository
	processor ImageProcessor
	workers   WorkerPool
	log       logger.Logger

	maxFileSizeMB   int64
	enableWatermark bool
	watermarkPath   string
	allowedExts     map[string]struct{}
}

type UploadRequest struct {
	File     *multipart.FileHeader
	Options  *entity.ProcessingOptions
	ClientID string
}

type UploadResponse struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Message  string `json:"message"`
}

func NewProcessorService(
	repo ImageRepository,
	processor ImageProcessor,
	workers WorkerPool,
	log logger.Logger,
	opts ...Option,
) (*ProcessorService, error) {
	const op = "service.NewProcessorService"

	s := &ProcessorService{
		repo:            repo,
		processor:       processor,
		workers:         workers,
		log:             log,
		maxFileSizeMB:   _defaultMaxFileSizeMB,
		enableWatermark: true,
		allowedExts:     make(map[string]struct{}, len(entity.SupportedExtensions())),
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
	const op = "service.Upload"
	log := s.log.Ctx(ctx).With("op", op)
	startTime := time.Now()

	defer s.logSlowOperation(ctx, op, startTime, logger.String("client_id", req.ClientID))

	if err := s.validateUploadRequest(req); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	imageID := uuid.NewString()
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(req.File.Filename), "."))

	file, err := req.File.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: open file: %w", op, err)
	}
	defer file.Close()

	if _, err = s.repo.SaveOriginal(ctx, imageID, ext, file); err != nil {
		return nil, fmt.Errorf("%s: save original: %w", op, err)
	}

	procOpts := req.Options
	if procOpts == nil {
		procOpts = entity.DefaultProcessingOptions()
	}

	task := entity.NewTask(imageID, procOpts)
	if err = s.workers.Submit(ctx, task); err != nil {
		_ = s.repo.Delete(ctx, imageID, ext)
		return nil, fmt.Errorf("%s: submit task: %w", op, err)
	}

	log.LogAttrs(ctx, logger.InfoLevel, "upload completed",
		logger.String("image_id", imageID),
		logger.Duration("duration", time.Since(startTime)),
	)

	return &UploadResponse{
		ID:       imageID,
		Status:   string(entity.TaskStatusPending),
		Filename: req.File.Filename,
		Size:     req.File.Size,
		Message:  "Image uploaded successfully",
	}, nil
}

func (s *ProcessorService) GetStatus(ctx context.Context, imageID string) (*entity.Task, error) {
	const op = "service.GetStatus"
	log := s.log.Ctx(ctx).With("op", op, "image_id", imageID)

	task, err := s.workers.Status(imageID)
	if err != nil && !errors.Is(err, entity.ErrTaskNotFound) {
		log.LogAttrs(ctx, logger.WarnLevel, "failed to get status from workers",
			logger.Any("error", err),
		)
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if task != nil {
		return task, nil
	}

	for _, ext := range entity.SupportedExtensions() {
		exists, _ := s.repo.Exists(ctx, imageID, ext)
		if exists {
			doneTime := time.Now()
			return &entity.Task{
				ID:          imageID,
				ImageID:     imageID,
				Status:      entity.TaskStatusDone,
				Progress:    _progressComplete,
				CompletedAt: &doneTime,
			}, nil
		}
	}

	return nil, entity.ErrImageNotFound
}

func (s *ProcessorService) GetImage(
	ctx context.Context,
	imageID, version string,
) (io.ReadCloser, int64, string, error) {
	const op = "service.GetImage"

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

func (s *ProcessorService) Delete(ctx context.Context, imageID string) error {
	const op = "service.Delete"

	var ext string
	for _, e := range entity.SupportedExtensions() {
		exists, _ := s.repo.Exists(ctx, imageID, e)
		if exists {
			ext = e
			break
		}
	}
	if ext == "" {
		return nil
	}

	return fmt.Errorf("%s: %w", op, s.repo.Delete(ctx, imageID, ext))
}

func (s *ProcessorService) ProcessTask(ctx context.Context, task *entity.Task) error {
	const op = "service.ProcessTask"
	log := s.log.Ctx(ctx).With("op", op, "task_id", task.ID)
	startTime := time.Now()

	task.MarkProcessing()
	log.LogAttrs(ctx, logger.InfoLevel, "processing started")

	ext, err := s.detectImageExtension(ctx, task.ImageID)
	if err != nil {
		task.MarkError("image extension not found")
		return fmt.Errorf("%s: detect extension: %w", op, entity.ErrInvalidImageFormat)
	}

	if err = s.processResize(ctx, task, ext); err != nil {
		return fmt.Errorf("%s: resize: %w", op, err)
	}

	if err = s.processWatermark(ctx, task, ext); err != nil {
		return fmt.Errorf("%s: watermark: %w", op, err)
	}

	if err = s.saveProcessed(ctx, task, ext); err != nil {
		return fmt.Errorf("%s: save processed: %w", op, err)
	}

	if err = s.createAndSaveThumbnail(ctx, task, ext); err != nil {
		return fmt.Errorf("%s: thumbnail: %w", op, err)
	}

	task.MarkDone()
	log.LogAttrs(ctx, logger.InfoLevel, "processing completed",
		logger.Duration("duration", time.Since(startTime)),
	)
	return nil
}

func (s *ProcessorService) detectImageExtension(ctx context.Context, imageID string) (string, error) {
	for _, ext := range entity.SupportedExtensions() {
		exists, _ := s.repo.Exists(ctx, imageID, ext)
		if exists {
			return ext, nil
		}
	}
	return "", entity.ErrFileNotFound
}

func (s *ProcessorService) processResize(ctx context.Context, task *entity.Task, ext string) error {
	if task.Options == nil || task.Options.ResizeWidth <= 0 {
		return nil
	}

	original, _, err := s.repo.GetOriginal(ctx, task.ImageID, ext)
	if err != nil {
		return fmt.Errorf("get original: %w", err)
	}
	defer original.Close()

	_, err = s.processor.Resize(ctx, original, ext, task.Options.ResizeWidth)
	if err != nil {
		return fmt.Errorf("resize: %w", err)
	}

	task.Progress = 40
	return nil
}

func (s *ProcessorService) processWatermark(ctx context.Context, task *entity.Task, ext string) error {
	if task.Options == nil || !task.Options.AddWatermark || !s.enableWatermark || s.watermarkPath == "" {
		return nil
	}

	original, _, err := s.repo.GetOriginal(ctx, task.ImageID, ext)
	if err != nil {
		return fmt.Errorf("get original: %w", err)
	}
	defer original.Close()

	watermarked, err := s.processor.AddWatermark(ctx, original, ext, s.watermarkPath)
	if err != nil {
		return fmt.Errorf("add watermark: %w", err)
	}

	task.Progress = 70
	_ = watermarked
	return nil
}

func (s *ProcessorService) saveProcessed(ctx context.Context, task *entity.Task, ext string) error {
	original, _, err := s.repo.GetOriginal(ctx, task.ImageID, ext)
	if err != nil {
		return fmt.Errorf("get original: %w", err)
	}
	defer original.Close()

	var current io.Reader = original

	if task.Options != nil && task.Options.ResizeWidth > 0 {
		current, err = s.processor.Resize(ctx, current, ext, task.Options.ResizeWidth)
		if err != nil {
			return fmt.Errorf("resize: %w", err)
		}
	}

	if task.Options != nil && task.Options.AddWatermark && s.enableWatermark && s.watermarkPath != "" {
		current, err = s.processor.AddWatermark(ctx, current, ext, s.watermarkPath)
		if err != nil {
			return fmt.Errorf("watermark: %w", err)
		}
	}

	if _, err = s.repo.SaveProcessed(ctx, task.ImageID, ext, current); err != nil {
		return fmt.Errorf("save processed: %w", err)
	}

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
	const op = "service.validateUploadRequest"

	if req.File == nil {
		return fmt.Errorf("%s: file is required: %w", op, entity.ErrFileNotFound)
	}
	if req.File.Size == 0 {
		return fmt.Errorf("%s: file is empty: %w", op, entity.ErrInvalidFileSize)
	}
	if req.File.Size > s.maxFileSizeMB*1024*1024 {
		return fmt.Errorf("%s: file too large (max %dMB): %w", op, s.maxFileSizeMB, entity.ErrInvalidFileSize)
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(req.File.Filename), "."))
	if ext == "" {
		return fmt.Errorf("%s: file extension is required: %w", op, entity.ErrInvalidImageFormat)
	}
	if _, ok := s.allowedExts[ext]; !ok {
		return fmt.Errorf("%s: %w: supported=%v", op, entity.ErrUnsupportedFormat, entity.SupportedExtensions())
	}

	if req.Options != nil {
		if err := req.Options.Validate(); err != nil {
			return fmt.Errorf("%s: invalid processing options: %w", op, err)
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
	if duration := time.Since(startTime); duration > _slowOperationThreshold {
		allAttrs := append([]logger.Attr{
			logger.String("op", op),
			logger.Duration("duration", duration),
		}, attrs...)
		s.log.Ctx(ctx).LogAttrs(ctx, logger.WarnLevel, "slow operation detected", allAttrs...)
	}
}
