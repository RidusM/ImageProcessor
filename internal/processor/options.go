package processor

import "fmt"

// Option функциональная опция для ImageProcessor
type Option func(*ImageProcessor)

// MaxWidth устанавливает максимальную ширину для ресайза
func MaxWidth(width int) Option {
	return func(p *ImageProcessor) {
		if width > 0 && width <= 4096 {
			p.maxWidth = width
		}
	}
}

// ThumbSize устанавливает размер миниатюры
func ThumbSize(size int) Option {
	return func(p *ImageProcessor) {
		if size > 0 && size <= 1000 {
			p.thumbSize = size
		}
	}
}

// JPEGQuality устанавливает качество JPEG (1-100)
func JPEGQuality(quality int) Option {
	return func(p *ImageProcessor) {
		if quality >= 1 && quality <= 100 {
			p.jpegQuality = quality
		}
	}
}

// UseBiLinear включает/отключает билинейную интерполяцию при ресайзе
// true = качественнее, но медленнее; false = быстрее, но менее качественно
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