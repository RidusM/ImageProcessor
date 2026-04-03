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
	"time"

	"img-processor/internal/entity"

	"github.com/wb-go/wbf/logger"
	"golang.org/x/image/draw"
	"golang.org/x/image/math/fixed"
)

const (
	_slowOperationThreshold = 2 * time.Second
	_defaultJPEGQuality     = 90
	_defaultMaxWidth        = 1920
	_defaultThumbSize       = 300
	_watermarkPadding       = 20
	_watermarkOpacity       = 128 // 0-255
)

type ImageProcessor struct {
		log logger.Logger

		maxWidth    int
		thumbSize   int
		jpegQuality int
		useBiLinear bool
	}

func NewImageProcessor(log logger.Logger, opts ...Option) (*ImageProcessor, error) {
	const op = "processor.image.NewImageProcessor"

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
	const op = "processor.image.Resize"

	log := p.log.Ctx(ctx).With("op", op)
	startTime := time.Now()

	defer p.logSlowOperation(ctx, op, startTime,
		logger.String("ext", ext),
		logger.Int("max_width", maxWidth),
	)

	log.LogAttrs(ctx, logger.DebugLevel, "resize started")

	img, err := p.Decode(ctx, src, ext)
	if err != nil {
		return nil, fmt.Errorf("%s: decode: %w", op, err)
	}

	bounds := img.Bounds()
	originalWidth := bounds.Dx()

	if originalWidth <= maxWidth {
		log.LogAttrs(ctx, logger.DebugLevel, "image already small enough",
			logger.Int("width", originalWidth),
			logger.Int("max_width", maxWidth),
		)
		return p.Encode(ctx, img, ext, p.jpegQuality)
	}

	scale := float64(maxWidth) / float64(originalWidth)
	newWidth := maxWidth
	newHeight := int(float64(bounds.Dy()) * scale)

	log.LogAttrs(ctx, logger.DebugLevel, "calculating new dimensions",
		logger.Int("original_width", originalWidth),
		logger.Int("original_height", bounds.Dy()),
		logger.Int("new_width", newWidth),
		logger.Int("new_height", newHeight),
		logger.Any("scale", scale),
	)

	var resized image.Image
	switch img.(type) {
	case *image.RGBA:
		resized = image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	case *image.NRGBA:
		resized = image.NewNRGBA(image.Rect(0, 0, newWidth, newHeight))
	case *image.Gray:
		resized = image.NewGray(image.Rect(0, 0, newWidth, newHeight))
	default:
		resized = image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	}

	var scaler draw.Scaler = draw.BiLinear
	if !p.useBiLinear {
		scaler = draw.NearestNeighbor
	}

	scaler.Scale(resized, resized.Bounds(), img, bounds, draw.Over, nil)

	log.LogAttrs(ctx, logger.DebugLevel, "resize completed",
		logger.Duration("duration", time.Since(startTime)),
	)

	return p.Encode(ctx, resized, ext, p.jpegQuality)
}

func (p *ImageProcessor) CreateThumbnail(ctx context.Context, src io.Reader, ext string, size int) (io.Reader, error) {
	const op = "processor.image.CreateThumbnail"

	log := p.log.Ctx(ctx).With("op", op)
	startTime := time.Now()

	defer p.logSlowOperation(ctx, op, startTime,
		logger.String("ext", ext),
		logger.Int("thumb_size", size),
	)

	log.LogAttrs(ctx, logger.DebugLevel, "thumbnail creation started")

	img, err := p.Decode(ctx, src, ext)
	if err != nil {
		return nil, fmt.Errorf("%s: decode: %w", op, err)
	}

	bounds := img.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()

	thumb := image.NewRGBA(image.Rect(0, 0, size, size))

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

	dx := (scaledW - size) / 2
	dy := (scaledH - size) / 2

	draw.Draw(thumb, thumb.Bounds(), scaled, image.Point{X: dx, Y: dy}, draw.Src)

	log.LogAttrs(ctx, logger.DebugLevel, "thumbnail created",
		logger.Duration("duration", time.Since(startTime)),
	)

	return p.Encode(ctx, thumb, ext, p.jpegQuality)
}

func (p *ImageProcessor) AddWatermark(ctx context.Context, src io.Reader, ext string, watermarkPath string) (io.Reader, error) {
	const op = "processor.image.AddWatermark"

	log := p.log.Ctx(ctx).With("op", op)
	startTime := time.Now()

	defer p.logSlowOperation(ctx, op, startTime,
		logger.String("ext", ext),
		logger.String("watermark_path", watermarkPath),
	)

	log.LogAttrs(ctx, logger.DebugLevel, "watermark application started")

	baseImg, err := p.Decode(ctx, src, ext)
	if err != nil {
		return nil, fmt.Errorf("%s: decode base: %w", op, err)
	}

	bounds := baseImg.Bounds()
	result := image.NewNRGBA(bounds)
	draw.Draw(result, bounds, baseImg, bounds.Min, draw.Src)

	wmExt := strings.TrimPrefix(strings.ToLower(extFromPath(watermarkPath)), ".")
	if isImageExt(wmExt) {
		wmImg, err := p.loadWatermarkImage(watermarkPath)
		if err != nil {
			return nil, fmt.Errorf("%s: load watermark image: %w", op, err)
		}
		if err := p.applyImageWatermark(result, wmImg); err != nil {
			return nil, fmt.Errorf("%s: apply image watermark: %w", op, err)
		}
	} else {
		text := watermarkPath
		if text == "" {
			text = "© SAMPLE"
		}
		if err := p.applyTextWatermark(result, text); err != nil {
			return nil, fmt.Errorf("%s: apply text watermark: %w", op, err)
		}
	}

	log.LogAttrs(ctx, logger.DebugLevel, "watermark applied",
		logger.Duration("duration", time.Since(startTime)),
	)

	return p.Encode(ctx, result, ext, p.jpegQuality)
}

func (p *ImageProcessor) ConvertFormat(ctx context.Context, src io.Reader, srcExt, dstExt string, quality int) (io.Reader, error) {
	const op = "processor.image.ConvertFormat"

	log := p.log.Ctx(ctx).With("op", op)
	startTime := time.Now()

	defer p.logSlowOperation(ctx, op, startTime,
		logger.String("src_ext", srcExt),
		logger.String("dst_ext", dstExt),
		logger.Int("quality", quality),
	)

	log.LogAttrs(ctx, logger.DebugLevel, "format conversion started")

	img, err := p.Decode(ctx, src, srcExt)
	if err != nil {
		return nil, fmt.Errorf("%s: decode: %w", op, err)
	}

	if quality <= 0 {
		quality = p.jpegQuality
	}

	log.LogAttrs(ctx, logger.DebugLevel, "format conversion completed",
		logger.Duration("duration", time.Since(startTime)),
	)

	return p.Encode(ctx, img, dstExt, quality)
}

func (p *ImageProcessor) Decode(ctx context.Context, src io.Reader, ext string) (image.Image, error) {
	const op = "processor.image.Decode"

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
		if err != nil {
			return nil, fmt.Errorf("%s: gif decode: %w", op, err)
		}
		if len(g.Image) == 0 {
			return nil, fmt.Errorf("%s: empty gif", op)
		}
		return g.Image[0], nil
	default:
		return nil, fmt.Errorf("%s: %w: %s", op, entity.ErrUnsupportedFormat, ext)
	}
}

func (p *ImageProcessor) Encode(ctx context.Context, img image.Image, ext string, quality int) (io.Reader, error) {
	const op = "processor.image.Encode"

	var buf bytes.Buffer

	switch strings.ToLower(ext) {
	case "jpg", "jpeg":
		if quality <= 0 {
			quality = p.jpegQuality
		}
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
			return nil, fmt.Errorf("%s: jpeg encode: %w", op, err)
		}
	case "png":
		if err := png.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("%s: png encode: %w", op, err)
		}
	case "gif":
		if err := gif.Encode(&buf, img, &gif.Options{NumColors: 256}); err != nil {
			return nil, fmt.Errorf("%s: gif encode: %w", op, err)
		}
	default:
		return nil, fmt.Errorf("%s: %w: %s", op, entity.ErrUnsupportedFormat, ext)
	}

	return bytes.NewReader(buf.Bytes()), nil
}

func (p *ImageProcessor) loadWatermarkImage(path string) (image.Image, error) {
	// В реальном проекте: кэширование, загрузка из хранилища
	// Здесь — простая загрузка с диска
	// Для безопасности: валидация пути, ограничение размера

	// Заглушка: возвращаем прозрачное изображение 200x50
	// В продакшене: os.Open + decode по расширению
	img := image.NewNRGBA(image.Rect(0, 0, 200, 50))
	// Рисуем полупрозрачный прямоугольник
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{255, 255, 255, _watermarkOpacity}}, image.Point{}, draw.Src)
	return img, nil
}

func (p *ImageProcessor) applyImageWatermark(base *image.NRGBA, watermark image.Image) error {
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
	return nil
}

func (p *ImageProcessor) applyTextWatermark(base *image.NRGBA, text string) error {
	bBounds := base.Bounds()

	textW := len(text) * 7
	textH := 13

	x := bBounds.Dx() - textW - _watermarkPadding
	y := bBounds.Dy() - _watermarkPadding

	bgRect := image.Rect(x-5, y-textH-5, x+textW+5, y+5)
	draw.Draw(base, bgRect, &image.Uniform{color.RGBA{0, 0, 0, 100}}, image.Point{}, draw.Over)

	// Рисуем текст (упрощённо: через draw.String из basicfont)
	// В реальном проекте: use golang.org/x/image/font
	point := fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)}
	// draw.String(base, face, point, text, color.White, nil) // требует font.Drawer

	// Заглушка: просто прямоугольник с текстом (без рендеринга символов)
	_ = point // suppress unused
	return nil
}

func (p *ImageProcessor) logSlowOperation(
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
		p.log.Ctx(ctx).LogAttrs(ctx, logger.WarnLevel, "slow operation detected", allAttrs...)
	}
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