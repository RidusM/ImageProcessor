package processor

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"strings"

	"img-processor/internal/entity"

	"github.com/wb-go/wbf/logger"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	_defaultJPEGQuality = 90
	_defaultMaxWidth    = 1920
	_defaultThumbSize   = 300
	_watermarkPadding   = 20
	_watermarkRatio     = 1.0

	_gifMaxColors  = 256
	_textCharWidth = 7
	_textPadding   = 5
	_centerDivisor = 2

	_watermarkMinPx = 10

	_extJPG  = "jpg"
	_extJPEG = "jpeg"
	_extPNG  = "png"
	_extGIF  = "gif"
	_extWEBP = "webp"
)

type ImageProcessor struct {
	log         logger.Logger
	maxWidth    int
	thumbSize   int
	jpegQuality int
	useBiLinear bool
}

func NewImageProcessor(log logger.Logger, opts ...Option) (*ImageProcessor, error) {
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
	return p, nil
}

func (p *ImageProcessor) Resize(ctx context.Context, src io.Reader, ext string, maxWidth int) (io.Reader, error) {
	const op = "processor.Resize"

	if strings.ToLower(ext) == _extGIF {
		return p.resizeGIF(ctx, src, maxWidth)
	}

	img, err := p.Decode(ctx, src, ext)
	if err != nil {
		return nil, fmt.Errorf("%s: decode: %w", op, err)
	}

	bounds := img.Bounds()
	originalWidth := bounds.Dx()
	if originalWidth <= maxWidth {
		return p.Encode(ctx, img, ext, p.jpegQuality)
	}

	resized := p.scaleImage(img, maxWidth, -1)
	return p.Encode(ctx, resized, ext, p.jpegQuality)
}

func (p *ImageProcessor) resizeGIF(ctx context.Context, src io.Reader, maxWidth int) (io.Reader, error) {
	const op = "processor.resizeGIF"

	g, err := gif.DecodeAll(src)
	if err != nil {
		return nil, fmt.Errorf("%s: decode gif: %w", op, err)
	}

	if len(g.Image) == 0 {
		return nil, fmt.Errorf("%s: empty gif", op)
	}

	origW := g.Config.Width
	origH := g.Config.Height

	if origW <= maxWidth {
		return p.encodeGIF(ctx, g)
	}

	scale := float64(maxWidth) / float64(origW)
	newW := maxWidth
	newH := int(float64(origH) * scale)

	for i, frame := range g.Image {
		resized := image.NewPaletted(image.Rect(0, 0, newW, newH), frame.Palette)
		xdraw.BiLinear.Scale(resized, resized.Bounds(), frame, frame.Bounds(), xdraw.Over, nil)
		g.Image[i] = resized
	}
	g.Config.Width = newW
	g.Config.Height = newH

	return p.encodeGIF(ctx, g)
}

func (p *ImageProcessor) CreateThumbnail(ctx context.Context, src io.Reader, ext string, size int) (io.Reader, error) {
	const op = "processor.CreateThumbnail"

	if strings.ToLower(ext) == _extGIF {
		return p.thumbnailGIF(ctx, src, size)
	}

	img, err := p.Decode(ctx, src, ext)
	if err != nil {
		return nil, fmt.Errorf("%s: decode: %w", op, err)
	}

	thumb := p.cropToSquare(img, size)
	return p.Encode(ctx, thumb, ext, p.jpegQuality)
}

func (p *ImageProcessor) thumbnailGIF(ctx context.Context, src io.Reader, size int) (io.Reader, error) {
	const op = "processor.thumbnailGIF"

	g, err := gif.DecodeAll(src)
	if err != nil {
		return nil, fmt.Errorf("%s: decode: %w", op, err)
	}
	if len(g.Image) == 0 {
		return nil, fmt.Errorf("%s: empty gif", op)
	}

	origW := g.Config.Width
	origH := g.Config.Height

	var scale float64
	if origW < origH {
		scale = float64(size) / float64(origW)
	} else {
		scale = float64(size) / float64(origH)
	}
	scaledW := int(float64(origW) * scale)
	scaledH := int(float64(origH) * scale)
	dx := (scaledW - size) / _centerDivisor
	dy := (scaledH - size) / _centerDivisor

	for i, frame := range g.Image {
		scaled := image.NewPaletted(image.Rect(0, 0, scaledW, scaledH), frame.Palette)
		xdraw.BiLinear.Scale(scaled, scaled.Bounds(), frame, frame.Bounds(), xdraw.Over, nil)

		thumb := image.NewPaletted(image.Rect(0, 0, size, size), frame.Palette)
		draw.Draw(thumb, thumb.Bounds(), scaled, image.Point{X: dx, Y: dy}, draw.Src)
		g.Image[i] = thumb
	}
	g.Config.Width = size
	g.Config.Height = size

	return p.encodeGIF(ctx, g)
}

func (p *ImageProcessor) AddWatermark(
	ctx context.Context,
	src io.Reader,
	ext string,
	watermarkPath string,
) (io.Reader, error) {
	const op = "processor.AddWatermark"

	if strings.ToLower(ext) == _extGIF {
		return p.addWatermarkGIF(ctx, src, watermarkPath)
	}

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
		if wmImg != nil {
			wmScaled := p.scaleWatermark(wmImg, bounds.Dx())
			p.applyImageWatermark(result, wmScaled)
		}
	} else {
		text := watermarkPath
		if text == "" {
			text = "© SAMPLE"
		}
		p.applyTextWatermark(result, text)
	}

	return p.Encode(ctx, result, ext, p.jpegQuality)
}

func (p *ImageProcessor) addWatermarkGIF(ctx context.Context, src io.Reader, watermarkPath string) (io.Reader, error) {
	const op = "processor.addWatermarkGIF"

	g, err := gif.DecodeAll(src)
	if err != nil {
		return nil, fmt.Errorf("%s: decode: %w", op, err)
	}
	if len(g.Image) == 0 {
		return nil, fmt.Errorf("%s: empty gif", op)
	}

	var wmScaled image.Image
	wmExt := extFromPath(watermarkPath)
	if isImageExt(wmExt) {
		wmImg := p.loadWatermarkImage(watermarkPath)
		if wmImg != nil {
			wmScaled = p.scaleWatermark(wmImg, g.Config.Width)
		}
	}

	for i, frame := range g.Image {
		nrgba := image.NewNRGBA(frame.Bounds())
		draw.Draw(nrgba, frame.Bounds(), frame, frame.Bounds().Min, draw.Src)

		if wmScaled != nil {
			p.applyImageWatermark(nrgba, wmScaled)
		} else {
			p.applyTextWatermark(nrgba, "© SAMPLE")
		}

		palettedBack := image.NewPaletted(frame.Bounds(), palette.Plan9)
		draw.Draw(palettedBack, palettedBack.Bounds(), image.White, image.Point{}, draw.Src)
		draw.FloydSteinberg.Draw(palettedBack, palettedBack.Bounds(), nrgba, image.Point{})
		g.Image[i] = palettedBack
	}

	return p.encodeGIF(ctx, g)
}

func (p *ImageProcessor) scaleWatermark(wm image.Image, baseWidth int) image.Image {
	targetW := max(int(float64(baseWidth)*_watermarkRatio), _watermarkMinPx)
	wmBounds := wm.Bounds()
	wmW := wmBounds.Dx()
	wmH := wmBounds.Dy()

	scale := float64(targetW) / float64(wmW)
	targetH := int(float64(wmH) * scale)
	if targetH < 1 {
		targetH = 1
	}

	scaled := image.NewNRGBA(image.Rect(0, 0, targetW, targetH))
	xdraw.BiLinear.Scale(scaled, scaled.Bounds(), wm, wmBounds, xdraw.Over, nil)
	return scaled
}

func (p *ImageProcessor) scaleImage(img image.Image, maxWidth, maxHeight int) image.Image {
	bounds := img.Bounds()
	origW := bounds.Dx()
	origH := bounds.Dy()

	newW, newH := origW, origH
	if maxWidth > 0 && origW > maxWidth {
		scale := float64(maxWidth) / float64(origW)
		newW = maxWidth
		newH = int(float64(origH) * scale)
	}
	if maxHeight > 0 && newH > maxHeight {
		scale := float64(maxHeight) / float64(newH)
		newH = maxHeight
		newW = int(float64(newW) * scale)
	}

	resized := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	var scaler xdraw.Scaler = xdraw.BiLinear
	if !p.useBiLinear {
		scaler = xdraw.NearestNeighbor
	}
	scaler.Scale(resized, resized.Bounds(), img, bounds, xdraw.Over, nil)
	return resized
}

func (p *ImageProcessor) cropToSquare(img image.Image, size int) image.Image {
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
	xdraw.BiLinear.Scale(scaled, scaled.Bounds(), img, bounds, xdraw.Over, nil)

	thumb := image.NewRGBA(image.Rect(0, 0, size, size))
	dx := (scaledW - size) / _centerDivisor
	dy := (scaledH - size) / _centerDivisor
	draw.Draw(thumb, thumb.Bounds(), scaled, image.Point{X: dx, Y: dy}, draw.Src)
	return thumb
}

func (p *ImageProcessor) applyImageWatermark(base *image.NRGBA, wm image.Image) {
	bBounds := base.Bounds()
	wBounds := wm.Bounds()
	x := bBounds.Dx() - wBounds.Dx() - _watermarkPadding
	y := bBounds.Dy() - wBounds.Dy() - _watermarkPadding
	if x < 0 {
		x = _watermarkPadding
	}
	if y < 0 {
		y = _watermarkPadding
	}
	draw.Draw(base, image.Rect(x, y, x+wBounds.Dx(), y+wBounds.Dy()), wm, wBounds.Min, draw.Over)
}

func (p *ImageProcessor) applyTextWatermark(base *image.NRGBA, text string) {
	bBounds := base.Bounds()
	textW := len(text) * _textCharWidth
	textH := 13

	x := bBounds.Dx() - textW - _watermarkPadding
	y := bBounds.Dy() - _watermarkPadding

	bgRect := image.Rect(x-_textPadding, y-textH-_textPadding, x+textW+_textPadding, y+_textPadding)
	draw.Draw(base, bgRect, &image.Uniform{color.RGBA{0, 0, 0, 100}}, image.Point{}, draw.Src)

	drawer := &font.Drawer{
		Dst:  base,
		Src:  &image.Uniform{color.RGBA{255, 255, 255, 255}},
		Face: basicfont.Face7x13,
		Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)},
	}
	drawer.DrawString(text)
}

func (p *ImageProcessor) loadWatermarkImage(path string) image.Image {
	file, err := os.Open(path)
	if err != nil {
		p.log.LogAttrs(context.Background(), logger.ErrorLevel, "failed to open watermark",
			logger.String("path", path),
			logger.Any("error", err),
		)
		return nil
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		p.log.LogAttrs(context.Background(), logger.ErrorLevel, "failed to decode watermark",
			logger.String("path", path),
			logger.Any("error", err),
		)
		return nil
	}
	return img
}

func (p *ImageProcessor) Decode(_ context.Context, src io.Reader, ext string) (image.Image, error) {
	const op = "processor.Decode"

	switch strings.ToLower(ext) {
	case _extJPG, _extJPEG:
		img, err := jpeg.Decode(src)
		if err != nil {
			return nil, fmt.Errorf("%s: jpeg decode: %w", op, err)
		}
		return img, nil
	case _extPNG:
		img, err := png.Decode(src)
		if err != nil {
			return nil, fmt.Errorf("%s: png decode: %w", op, err)
		}
		return img, nil
	case _extGIF:
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
	case _extJPG, _extJPEG:
		if quality <= 0 {
			quality = p.jpegQuality
		}
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
	case _extPNG:
		if err := png.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
	case _extGIF:
		opts := &gif.Options{NumColors: _gifMaxColors}
		if err := gif.Encode(&buf, img, opts); err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
	default:
		return nil, fmt.Errorf("%s: %w: %s", op, entity.ErrUnsupportedFormat, ext)
	}
	return bytes.NewReader(buf.Bytes()), nil
}

func (p *ImageProcessor) encodeGIF(_ context.Context, g *gif.GIF) (io.Reader, error) {
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		return nil, fmt.Errorf("processor.encodeGIF: %w", err)
	}
	return bytes.NewReader(buf.Bytes()), nil
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
