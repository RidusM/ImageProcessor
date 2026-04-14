package processor

import "fmt"

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

func (p *ImageProcessor) validate() error {
	if p.maxWidth <= 0 || p.maxWidth > 4096 {
		return fmt.Errorf("invalid max width: %d", p.maxWidth)
	}
	if p.thumbSize <= 0 || p.thumbSize > 1000 {
		return fmt.Errorf("invalid thumb size: %d", p.thumbSize)
	}
	if p.jpegQuality < 1 || p.jpegQuality > 100 {
		return fmt.Errorf("invalid jpeg quality: %d", p.jpegQuality)
	}
	return nil
}
