// Package repository provides storage abstractions for images.
package repository

import (
	"context"
	"fmt"
	"io"
	"strings"

	"img-processor/internal/config"
	"img-processor/internal/entity"

	"github.com/wb-go/wbf/logger"
)

type ImageRepository interface {
	SaveOriginal(ctx context.Context, imageID, ext string, src io.Reader) (string, error)
	SaveProcessed(ctx context.Context, imageID, ext string, src io.Reader) (string, error)
	SaveThumbnail(ctx context.Context, imageID, ext string, src io.Reader) (string, error)

	GetPaths(imageID, ext string) *entity.ImagePaths

	GetOriginal(ctx context.Context, imageID, ext string) (io.ReadCloser, int64, error)
	GetProcessed(ctx context.Context, imageID, ext string) (io.ReadCloser, int64, error)
	GetThumbnail(ctx context.Context, imageID, ext string) (io.ReadCloser, int64, error)

	Delete(ctx context.Context, imageID, ext string) error
	Exists(ctx context.Context, imageID, ext string) (bool, error)
}

func NewImageRepository(cfg config.Storage, log logger.Logger) (ImageRepository, error) {
	const op = "repository.NewImageRepository"

	switch cfg.Type {
	case "minio", "s3":
		return newMinIORepository(cfg, log)
	case "local", "":
		return newLocalRepository(cfg, log)
	default:
		return nil, fmt.Errorf("%s: unsupported storage type %q", op, cfg.Type)
	}
}

func buildPath(subdir, imageID, ext string) string {
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return subdir + "/" + imageID + ext
}

func validateExtension(ext string) error {
	const op = "repository.validateExtension"
	if ext == "" {
		return fmt.Errorf("%s: extension is required", op)
	}
	if !entity.IsValidExtension(ext) {
		return fmt.Errorf("%s: %w: %q", op, entity.ErrUnsupportedFormat, ext)
	}
	return nil
}
