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
	cfg  config.Storage
	log  logger.Logger
	paths map[string]string
}

func newLocalRepository(cfg config.Storage, log logger.Logger) (*localRepository, error) {
	const op = "repository.newLocalRepository"

	if cfg.Path == "" {
		cfg.Path = "./storage"
	}

	repo := &localRepository{
		cfg: cfg,
		log: log, // будет заменено в app.Run
		paths: map[string]string{
			"originals":  filepath.Join(cfg.Path, "originals"),
			"processed":  filepath.Join(cfg.Path, "processed"),
			"thumbnails": filepath.Join(cfg.Path, "thumbnails"),
		},
	}

	// Создаём директории
	for _, dir := range repo.paths {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("%s: mkdir %s: %w", op, dir, err)
		}
	}

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

func (r *localRepository) GetOriginal(ctx context.Context, imageID, ext string) (io.ReadCloser, int64, error) {
	const op = "repository.local.GetOriginal"
	path := r.fullPath("originals", imageID, ext)
	return r.getFile(ctx, op, path)
}

func (r *localRepository) GetProcessed(ctx context.Context, imageID, ext string) (io.ReadCloser, int64, error) {
	const op = "repository.local.GetProcessed"
	path := r.fullPath("processed", imageID, ext)
	return r.getFile(ctx, op, path)
}

func (r *localRepository) GetThumbnail(ctx context.Context, imageID, ext string) (io.ReadCloser, int64, error) {
	const op = "repository.local.GetThumbnail"
	path := r.fullPath("thumbnails", imageID, ext)
	return r.getFile(ctx, op, path)
}

func (r *localRepository) getFile(ctx context.Context, op, path string) (io.ReadCloser, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, entity.ErrFileNotFound
		}
		return nil, 0, fmt.Errorf("%s: open: %w", op, err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, 0, fmt.Errorf("%s: stat: %w", op, err)
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
			lastErr = fmt.Errorf("%s: remove %s: %w", op, path, err)
		}
	}
	return lastErr
}

func (r *localRepository) Exists(ctx context.Context, imageID, ext string) (bool, error) {
	const op = "repository.local.Exists"
	path := r.fullPath("processed", imageID, ext)
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("%s: stat: %w", op, err)
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

func (r *localRepository) saveAtomic(ctx context.Context, targetPath string, src io.Reader) error {
	const op = "repository.local.saveAtomic"

	tmpPath := r.tempPath(targetPath)
	
	tmpFile, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("%s: create temp: %w", op, err)
	}

	if _, err = io.Copy(tmpFile, src); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("%s: copy: %w", op, err)
	}
	if err = tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("%s: close temp: %w", op, err)
	}

	if err = os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("%s: rename: %w", op, err)
	}

	return nil
}