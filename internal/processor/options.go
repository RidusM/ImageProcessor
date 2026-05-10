package processor

type Option func(*ImageProcessor)

func MaxWidth(width int) Option {
	return func(p *ImageProcessor) {
		if width > 0 && width <= 4096 {
			p.maxWidth = width
		}
	}
}

func ThumbSize(size int) Option {
	return func(p *ImageProcessor) {
		if size > 0 && size <= 1000 {
			p.thumbSize = size
		}
	}
}

func JPEGQuality(quality int) Option {
	return func(p *ImageProcessor) {
		if quality >= 1 && quality <= 100 {
			p.jpegQuality = quality
		}
	}
}

func UseBiLinear(enabled bool) Option {
	return func(p *ImageProcessor) {
		p.useBiLinear = enabled
	}
}
