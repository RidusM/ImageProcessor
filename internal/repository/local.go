package repository

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"img-processor/internal/config"
	"img-processor/internal/entity"

	"github.com/wb-go/wbf/logger"
)

type localRepository struct {
	cfg   config.Storage
	paths map[string]string
	log   logger.Logger
}

func newLocalRepository(cfg config.Storage, log logger.Logger) (*localRepository, error) {
	const op = "repository.newLocalRepository"

	basePath := cfg.Path
	if basePath == "" {
		basePath = "./storage"
	}

	repo := &localRepository{
		cfg: cfg,
		log: log,
		paths: map[string]string{
			"originals":  filepath.Join(basePath, "originals"),
			"processed":  filepath.Join(basePath, "processed"),
			"thumbnails": filepath.Join(basePath, "thumbnails"),
		},
	}

	for name, dir := range repo.paths {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("%s: create dir %q: %w", op, name, err)
		}
	}

	log.LogAttrs(context.Background(), logger.DebugLevel, "local repository initialized",
		logger.String("base_path", basePath),
	)
	return repo, nil
}

func (r *localRepository) SaveOriginal(ctx context.Context, imageID, ext string, src io.Reader) (string, error) {
	const op = "repository.local.SaveOriginal"
	if err := validateExtension(ext); err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}
	path := r.fullPath("originals", imageID, ext)
	if err := r.saveAtomic(ctx, path, src); err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}
	return path, nil
}

func (r *localRepository) SaveProcessed(ctx context.Context, imageID, ext string, src io.Reader) (string, error) {
	const op = "repository.local.SaveProcessed"
	if err := validateExtension(ext); err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}
	path := r.fullPath("processed", imageID, ext)
	if err := r.saveAtomic(ctx, path, src); err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}
	return path, nil
}

func (r *localRepository) SaveThumbnail(ctx context.Context, imageID, ext string, src io.Reader) (string, error) {
	const op = "repository.local.SaveThumbnail"
	if err := validateExtension(ext); err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}
	path := r.fullPath("thumbnails", imageID, ext)
	if err := r.saveAtomic(ctx, path, src); err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}
	return path, nil
}

func (r *localRepository) GetPaths(imageID, ext string) *entity.ImagePaths {
	return &entity.ImagePaths{
		Original:  r.fullPath("originals", imageID, ext),
		Processed: r.fullPath("processed", imageID, ext),
		Thumbnail: r.fullPath("thumbnails", imageID, ext),
	}
}

func (r *localRepository) GetOriginal(_ context.Context, imageID, ext string) (io.ReadCloser, int64, error) {
	const op = "repository.local.GetOriginal"
	return r.getFile(op, r.fullPath("originals", imageID, ext))
}

func (r *localRepository) GetProcessed(_ context.Context, imageID, ext string) (io.ReadCloser, int64, error) {
	const op = "repository.local.GetProcessed"
	return r.getFile(op, r.fullPath("processed", imageID, ext))
}

func (r *localRepository) GetThumbnail(_ context.Context, imageID, ext string) (io.ReadCloser, int64, error) {
	const op = "repository.local.GetThumbnail"
	return r.getFile(op, r.fullPath("thumbnails", imageID, ext))
}

func (r *localRepository) getFile(op, path string) (io.ReadCloser, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, entity.ErrFileNotFound
		}
		return nil, 0, fmt.Errorf("%s: open %q: %w", op, path, err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, 0, fmt.Errorf("%s: stat %q: %w", op, path, err)
	}
	return file, info.Size(), nil
}

func (r *localRepository) Delete(ctx context.Context, imageID, ext string) error {
	const op = "repository.local.Delete"
	paths := []string{
		r.fullPath("originals", imageID, ext),
		r.fullPath("processed", imageID, ext),
		r.fullPath("thumbnails", imageID, ext),
	}

	var lastErr error
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			r.log.LogAttrs(ctx, logger.WarnLevel, "failed to remove file",
				logger.String("path", path),
				logger.Any("error", err),
			)
			lastErr = fmt.Errorf("%s: remove %q: %w", op, path, err)
		}
	}
	return lastErr
}

func (r *localRepository) Exists(_ context.Context, imageID, ext string) (bool, error) {
	const op = "repository.local.Exists"
	path := r.fullPath("processed", imageID, ext)
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("%s: stat %q: %w", op, path, err)
	}
	return true, nil
}

func (r *localRepository) fullPath(subdir, imageID, ext string) string {
	return filepath.Join(r.paths[subdir], imageID+"."+ext)
}

func (r *localRepository) tempPath(targetPath string) string {
	dir := filepath.Dir(targetPath)
	base := filepath.Base(targetPath)
	return filepath.Join(dir, fmt.Sprintf(".tmp.%s.%d", base, time.Now().UnixNano()))
}

func (r *localRepository) saveAtomic(_ context.Context, targetPath string, src io.Reader) error {
	const op = "repository.local.saveAtomic"
	tmpPath := r.tempPath(targetPath)

	tmpFile, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("%s: create temp %q: %w", op, tmpPath, err)
	}

	if _, err = io.Copy(tmpFile, src); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("%s: copy to temp: %w", op, err)
	}

	if err = tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("%s: close temp: %w", op, err)
	}

	if err = os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("%s: rename %q -> %q: %w", op, tmpPath, targetPath, err)
	}

	return nil
}
