package processor

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"strings"

	"img-processor/internal/entity"

	"github.com/wb-go/wbf/logger"
	"golang.org/x/image/draw"
)

const (
	_defaultJPEGQuality = 90
	_defaultMaxWidth    = 1920
	_defaultThumbSize   = 300
	_watermarkPadding   = 20
	_watermarkOpacity   = 128

	_gifMaxColors  = 256
	_watermarkW    = 200
	_watermarkH    = 50
	_textCharWidth = 7
	_textPadding   = 5
	_centerDivisor = 2
)

type ImageProcessor struct {
	log         logger.Logger
	maxWidth    int
	thumbSize   int
	jpegQuality int
	useBiLinear bool
}

func NewImageProcessor(log logger.Logger, opts ...Option) (*ImageProcessor, error) {
	const op = "processor.NewImageProcessor"
	p := &ImageProcessor{
		log:         log,
		maxWidth:    _defaultMaxWidth,
		thumbSize:   _defaultThumbSize,
		jpegQuality: _defaultJPEGQuality,
		useBiLinear: true,
	}

	for _, opt := range opts {
		opt(p)
	}
	if err := p.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return p, nil
}

func (p *ImageProcessor) Resize(ctx context.Context, src io.Reader, ext string, maxWidth int) (io.Reader, error) {
	const op = "processor.Resize"
	img, err := p.Decode(ctx, src, ext)
	if err != nil {
		return nil, fmt.Errorf("%s: decode: %w", op, err)
	}

	bounds := img.Bounds()
	originalWidth := bounds.Dx()
	if originalWidth <= maxWidth {
		return p.Encode(ctx, img, ext, p.jpegQuality)
	}

	scale := float64(maxWidth) / float64(originalWidth)
	newWidth := maxWidth
	newHeight := int(float64(bounds.Dy()) * scale)

	resized := image.NewNRGBA(image.Rect(0, 0, newWidth, newHeight))
	var scaler draw.Scaler = draw.BiLinear
	if !p.useBiLinear {
		scaler = draw.NearestNeighbor
	}
	scaler.Scale(resized, resized.Bounds(), img, bounds, draw.Over, nil)
	return p.Encode(ctx, resized, ext, p.jpegQuality)
}

func (p *ImageProcessor) CreateThumbnail(ctx context.Context, src io.Reader, ext string, size int) (io.Reader, error) {
	const op = "processor.CreateThumbnail"
	img, err := p.Decode(ctx, src, ext)
	if err != nil {
		return nil, fmt.Errorf("%s: decode: %w", op, err)
	}

	bounds := img.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()

	var scale float64
	if srcW < srcH {
		scale = float64(size) / float64(srcW)
	} else {
		scale = float64(size) / float64(srcH)
	}

	scaledW := int(float64(srcW) * scale)
	scaledH := int(float64(srcH) * scale)

	scaled := image.NewRGBA(image.Rect(0, 0, scaledW, scaledH))
	draw.BiLinear.Scale(scaled, scaled.Bounds(), img, bounds, draw.Over, nil)

	thumb := image.NewRGBA(image.Rect(0, 0, size, size))
	dx := (scaledW - size) / _centerDivisor
	dy := (scaledH - size) / _centerDivisor

	draw.Draw(thumb, thumb.Bounds(), scaled, image.Point{X: dx, Y: dy}, draw.Src)
	return p.Encode(ctx, thumb, ext, p.jpegQuality)
}

func (p *ImageProcessor) AddWatermark(
	ctx context.Context,
	src io.Reader,
	ext string,
	watermarkPath string,
) (io.Reader, error) {
	const op = "processor.AddWatermark"
	baseImg, err := p.Decode(ctx, src, ext)
	if err != nil {
		return nil, fmt.Errorf("%s: decode base: %w", op, err)
	}

	bounds := baseImg.Bounds()
	result := image.NewNRGBA(bounds)
	draw.Draw(result, bounds, baseImg, bounds.Min, draw.Src)

	wmExt := extFromPath(watermarkPath)
	if isImageExt(wmExt) {
		wmImg := p.loadWatermarkImage(watermarkPath)
		p.applyImageWatermark(result, wmImg)
	} else {
		text := watermarkPath
		if text == "" {
			text = "© SAMPLE"
		}
		p.applyTextWatermark(result, text)
	}

	return p.Encode(ctx, result, ext, p.jpegQuality)
}

func (p *ImageProcessor) Decode(_ context.Context, src io.Reader, ext string) (image.Image, error) {
	const op = "processor.Decode"
	switch strings.ToLower(ext) {
	case "jpg", "jpeg":
		img, err := jpeg.Decode(src)
		if err != nil {
			return nil, fmt.Errorf("%s: jpeg decode: %w", op, err)
		}
		return img, nil
	case "png":
		img, err := png.Decode(src)
		if err != nil {
			return nil, fmt.Errorf("%s: png decode: %w", op, err)
		}
		return img, nil
	case "gif":
		g, err := gif.DecodeAll(src)
		if err != nil || len(g.Image) == 0 {
			return nil, fmt.Errorf("%s: %w", op, entity.ErrDecodeFailed)
		}
		return g.Image[0], nil
	default:
		return nil, fmt.Errorf("%s: %w: %s", op, entity.ErrUnsupportedFormat, ext)
	}
}

func (p *ImageProcessor) Encode(_ context.Context, img image.Image, ext string, quality int) (io.Reader, error) {
	const op = "processor.Encode"
	var buf bytes.Buffer

	switch strings.ToLower(ext) {
	case "jpg", "jpeg":
		if quality <= 0 {
			quality = p.jpegQuality
		}
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
	case "png":
		if err := png.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
	case "gif":
		if err := gif.Encode(&buf, img, &gif.Options{NumColors: _gifMaxColors}); err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
	default:
		return nil, fmt.Errorf("%s: %w: %s", op, entity.ErrUnsupportedFormat, ext)
	}
	return bytes.NewReader(buf.Bytes()), nil
}

func (p *ImageProcessor) loadWatermarkImage(_ string) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, _watermarkW, _watermarkH))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{255, 255, 255, _watermarkOpacity}}, image.Point{}, draw.Src)
	return img
}

func (p *ImageProcessor) applyImageWatermark(base *image.NRGBA, watermark image.Image) {
	bBounds := base.Bounds()
	wBounds := watermark.Bounds()
	x := bBounds.Dx() - wBounds.Dx() - _watermarkPadding
	y := bBounds.Dy() - wBounds.Dy() - _watermarkPadding
	if x < 0 {
		x = _watermarkPadding
	}
	if y < 0 {
		y = _watermarkPadding
	}
	draw.Draw(base, image.Rect(x, y, x+wBounds.Dx(), y+wBounds.Dy()), watermark, wBounds.Min, draw.Over)
}

func (p *ImageProcessor) applyTextWatermark(base *image.NRGBA, text string) {
	bBounds := base.Bounds()
	textW := len(text) * _textCharWidth
	textH := 13

	x := bBounds.Dx() - textW - _watermarkPadding
	y := bBounds.Dy() - _watermarkPadding

	bgRect := image.Rect(x-_textPadding, y-textH-_textPadding, x+textW+_textPadding, y+_textPadding)
	draw.Draw(base, bgRect, &image.Uniform{color.RGBA{0, 0, 0, 100}}, image.Point{}, draw.Src)
}

func extFromPath(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return path[i+1:]
		}
		if path[i] == '/' || path[i] == '\\' {
			break
		}
	}
	return ""
}

func isImageExt(ext string) bool {
	for _, e := range []string{"png", "jpg", "jpeg", "gif", "webp"} {
		if e == ext {
			return true
		}
	}
	return false
}
