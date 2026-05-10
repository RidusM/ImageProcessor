package service

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
