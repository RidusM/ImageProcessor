package service

import (
	"errors"
)

type Option func(*ProcessorService)

func MaxFileSizeMB(sizeMB int64) Option {
	return func(s *ProcessorService) {
		if sizeMB > 0 {
			s.maxFileSizeMB = sizeMB
		}
	}
}

func WorkerCount(count int) Option {
	return func(s *ProcessorService) {
		if count > 0 && count <= 32 {
			s.workerCount = count
		}
	}
}

func QueueSize(size int) Option {
	return func(s *ProcessorService) {
		if size > 0 {
			s.queueSize = size
		}
	}
}

func MaxWidth(width int) Option {
	return func(s *ProcessorService) {
		if width > 0 && width <= 4096 {
			s.maxWidth = width
		}
	}
}

func ThumbSize(size int) Option {
	return func(s *ProcessorService) {
		if size > 0 && size <= 1000 {
			s.thumbSize = size
		}
	}
}

func JPEGQuality(quality int) Option {
	return func(s *ProcessorService) {
		if quality >= 1 && quality <= 100 {
			s.jpegQuality = quality
		}
	}
}

func EnableWatermark(enabled bool) Option {
	return func(s *ProcessorService) {
		s.enableWatermark = enabled
	}
}

func WatermarkPath(path string) Option {
	return func(s *ProcessorService) {
		if path != "" {
			s.watermarkPath = path
		}
	}
}

func AllowedExtensions(exts []string) Option {
	return func(s *ProcessorService) {
		if len(exts) > 0 {
			s.allowedExts = make(map[string]struct{}, len(exts))
			for _, ext := range exts {
				s.allowedExts[ext] = struct{}{}
			}
		}
	}
}

func (s *ProcessorService) validate() error {
	if s.maxFileSizeMB <= 0 {
		return errors.New("invalid max file size: must be > 0")
	}
	if s.workerCount <= 0 || s.workerCount > 32 {
		return errors.New("invalid worker count: must be in [1, 32]")
	}
	if s.queueSize <= 0 {
		return errors.New("invalid queue size: must be > 0")
	}
	if s.maxWidth <= 0 || s.maxWidth > 4096 {
		return errors.New("invalid max width: must be in [1, 4096]")
	}
	if s.thumbSize <= 0 || s.thumbSize > 1000 {
		return errors.New("invalid thumbnail size: must be in [1, 1000]")
	}
	if s.jpegQuality < 1 || s.jpegQuality > 100 {
		return errors.New("invalid JPEG quality: must be in [1, 100]")
	}
	if s.repo == nil {
		return errors.New("image repository is required")
	}
	if s.processor == nil {
		return errors.New("image processor is required")
	}
	if s.workers == nil {
		return errors.New("worker pool is required")
	}
	return nil
}