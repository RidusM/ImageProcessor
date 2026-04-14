package service

import (
	"errors"
	"fmt"
)

type Option func(*ProcessorService)

func MaxFileSizeMB(sizeMB int64) Option {
	return func(s *ProcessorService) {
		if sizeMB > 0 {
			s.maxFileSizeMB = sizeMB
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

func (s *ProcessorService) validate() error {
	if s.maxFileSizeMB <= 0 {
		return fmt.Errorf("maxFileSizeMB must be > 0, got %d", s.maxFileSizeMB)
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
