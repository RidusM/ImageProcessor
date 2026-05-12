package repository

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"img-processor/internal/entity"
	"img-processor/pkg/storage"

	"github.com/google/uuid"
	"github.com/wb-go/wbf/logger"
)

const (
	_subdirOriginal  = "originals"
	_subdirProcessed = "processed"
	_subdirThumbnail = "thumbnails"
)

type ImageRepository struct {
	store storage.Provider
	log   logger.Logger
}

func NewImageRepository(store storage.Provider, log logger.Logger) *ImageRepository {
	return &ImageRepository{
		store: store,
		log:   log,
	}
}

func (r *ImageRepository) SaveOriginal(
	ctx context.Context,
	id uuid.UUID,
	ext string,
	src io.Reader,
	originalName string,
) (string, error) {
	key, err := r.upload(ctx, _subdirOriginal, id, ext, src, "SaveOriginal")
	if err != nil {
		return "", err
	}

	nameKey := _subdirOriginal + "/" + id.String() + ".name"
	_ = r.store.Put(ctx, nameKey, strings.NewReader(originalName), "text/plain")
	return key, nil
}

func (r *ImageRepository) SaveProcessed(
	ctx context.Context,
	id uuid.UUID,
	ext string,
	src io.Reader,
) (string, error) {
	return r.upload(ctx, _subdirProcessed, id, ext, src, "SaveProcessed")
}

func (r *ImageRepository) SaveThumbnail(
	ctx context.Context,
	id uuid.UUID,
	ext string,
	src io.Reader,
) (string, error) {
	return r.upload(ctx, _subdirThumbnail, id, ext, src, "SaveThumbnail")
}

func (r *ImageRepository) GetOriginal(
	ctx context.Context,
	id uuid.UUID,
	ext string,
) (io.ReadCloser, int64, error) {
	return r.download(ctx, _subdirOriginal, id, ext, "GetOriginal")
}

func (r *ImageRepository) GetProcessed(
	ctx context.Context,
	id uuid.UUID,
	ext string,
) (io.ReadCloser, int64, error) {
	return r.download(ctx, _subdirProcessed, id, ext, "GetProcessed")
}

func (r *ImageRepository) GetThumbnail(
	ctx context.Context,
	id uuid.UUID,
	ext string,
) (io.ReadCloser, int64, error) {
	return r.download(ctx, _subdirThumbnail, id, ext, "GetThumbnail")
}

func (r *ImageRepository) GetPaths(id uuid.UUID, ext string) *entity.ImagePaths {
	return &entity.ImagePaths{
		Original:  r.key(_subdirOriginal, id, ext),
		Processed: r.key(_subdirProcessed, id, ext),
		Thumbnail: r.key(_subdirThumbnail, id, ext),
	}
}

func (r *ImageRepository) Delete(
	ctx context.Context,
	id uuid.UUID,
	ext string,
) error {
	const op = "repository.Delete"

	subdirs := []string{_subdirOriginal, _subdirProcessed, _subdirThumbnail}
	for _, sd := range subdirs {
		key := r.key(sd, id, ext)
		if err := r.store.Delete(ctx, key); err != nil && !r.isErrNotFound(err) {
			return fmt.Errorf("%s: remove %q: %w", op, key, err)
		}
	}
	return nil
}

func (r *ImageRepository) Exists(
	ctx context.Context,
	id uuid.UUID,
	ext string,
) (bool, error) {
	const op = "repository.Exists"
	ok, err := r.store.Exists(ctx, r.key(_subdirOriginal, id, ext))
	if err != nil {
		return false, fmt.Errorf("%s: %w", op, err)
	}
	return ok, nil
}

func (r *ImageRepository) List(ctx context.Context) ([]entity.ImageMeta, error) {
	const op = "repository.List"

	if localStore, ok := r.store.(interface{ Root() string }); ok {
		return r.listLocal(ctx, localStore.Root())
	}
	if minioStore, ok := r.store.(interface {
		ListObjects(ctx context.Context, prefix string) ([]string, error)
	}); ok {
		return r.listMinio(ctx, minioStore)
	}
	return nil, fmt.Errorf("%s: listing not supported for this storage type", op)
}

func (r *ImageRepository) listLocal(ctx context.Context, root string) ([]entity.ImageMeta, error) {
	const op = "repository.listLocal"

	originalsDir := filepath.Join(root, _subdirOriginal)
	entries, err := os.ReadDir(originalsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []entity.ImageMeta{}, nil
		}
		return nil, fmt.Errorf("%s: read dir: %w", op, err)
	}

	var result []entity.ImageMeta
	for _, entry := range entries {
		meta, ok := r.parseLocalEntry(ctx, entry)
		if !ok {
			continue
		}
		result = append(result, meta)
	}
	return result, nil
}

func (r *ImageRepository) parseLocalEntry(ctx context.Context, entry os.DirEntry) (entity.ImageMeta, bool) {
	if entry.IsDir() {
		return entity.ImageMeta{}, false
	}

	idStr, ext, err := parseFilename(entry.Name())
	if err != nil {
		return entity.ImageMeta{}, false
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		return entity.ImageMeta{}, false
	}

	name := r.readOriginalName(ctx, idStr)
	return entity.ImageMeta{
		ID:           id,
		Ext:          strings.TrimPrefix(ext, "."),
		OriginalName: name,
	}, true
}

func (r *ImageRepository) readOriginalName(ctx context.Context, idStr string) string {
	nameKey := _subdirOriginal + "/" + idStr + ".name"
	rc, _, err := r.store.Get(ctx, nameKey)
	if err != nil {
		return ""
	}
	defer func() {
		if closeErr := rc.Close(); closeErr != nil {
			r.log.LogAttrs(ctx, logger.WarnLevel, "failed to close name reader",
				logger.Any("error", closeErr),
			)
		}
	}()

	b, err := io.ReadAll(rc)
	if err != nil {
		return ""
	}
	return string(b)
}

func (r *ImageRepository) listMinio(ctx context.Context, store interface {
	ListObjects(ctx context.Context, prefix string) ([]string, error)
},
) ([]entity.ImageMeta, error) {
	const op = "repository.listMinio"

	keys, err := store.ListObjects(ctx, _subdirOriginal+"/")
	if err != nil {
		return nil, fmt.Errorf("%s: list objects: %w", op, err)
	}

	var result []entity.ImageMeta
	for _, key := range keys {
		meta, ok := r.parseMinioKey(ctx, key)
		if !ok {
			continue
		}
		result = append(result, meta)
	}
	return result, nil
}

func (r *ImageRepository) parseMinioKey(ctx context.Context, key string) (entity.ImageMeta, bool) {
	name := filepath.Base(key)
	idStr, ext, err := parseFilename(name)
	if err != nil {
		return entity.ImageMeta{}, false
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		return entity.ImageMeta{}, false
	}

	originalName := r.readOriginalName(ctx, idStr)
	return entity.ImageMeta{
		ID:           id,
		Ext:          strings.TrimPrefix(ext, "."),
		OriginalName: originalName,
	}, true
}

func (r *ImageRepository) Cleanup(ctx context.Context, maxAge time.Duration) (int, error) {
	const op = "repository.Cleanup"
	start := time.Now()
	deleted := 0

	localStore, ok := r.store.(interface{ Root() string })
	if !ok {
		r.log.LogAttrs(ctx, logger.DebugLevel, "cleanup not supported for this storage type")
		return 0, nil
	}

	originalsDir := filepath.Join(localStore.Root(), _subdirOriginal)
	entries, err := os.ReadDir(originalsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("%s: read originals dir: %w", op, err)
	}

	for _, entry := range entries {
		var n int
		n, err = r.cleanupSingleEntry(ctx, entry, maxAge)
		if err != nil {
			r.log.LogAttrs(ctx, logger.WarnLevel, "cleanup entry failed",
				logger.String("filename", entry.Name()),
				logger.Any("error", err),
			)
			continue
		}
		deleted += n
	}

	duration := time.Since(start)
	if deleted > 0 {
		r.log.LogAttrs(ctx, logger.InfoLevel, "cleanup completed",
			logger.Int("deleted", deleted),
			logger.Duration("duration", duration),
		)
	}
	return deleted, nil
}

func (r *ImageRepository) cleanupSingleEntry(
	ctx context.Context,
	entry os.DirEntry,
	maxAge time.Duration,
) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("single entry ctx err: %w", err)
	}
	if entry.IsDir() {
		return 0, nil
	}

	idStr, ext, err := parseFilename(entry.Name())
	if err != nil {
		return 0, fmt.Errorf("parse filename: %w", err)
	}

	imageID, err := uuid.Parse(idStr)
	if err != nil {
		return 0, fmt.Errorf("parse uuid: %w", err)
	}

	info, err := entry.Info()
	if err != nil {
		return 0, fmt.Errorf("entry info: %w", err)
	}
	if time.Since(info.ModTime()) < maxAge {
		return 0, nil
	}

	processedKey := filepath.Join(_subdirProcessed, idStr+ext)
	processedExists, err := r.store.Exists(ctx, processedKey)
	if err != nil {
		return 0, fmt.Errorf("check processed: %w", err)
	}
	if !processedExists {
		return 0, nil
	}

	if err = r.Delete(ctx, imageID, ext); err != nil {
		return 0, fmt.Errorf("delete: %w", err)
	}

	return 1, nil
}

func parseFilename(name string) (string, string, error) {
	extVal := filepath.Ext(name)
	idStr := strings.TrimSuffix(name, extVal)

	if _, err := uuid.Parse(idStr); err != nil {
		return "", "", fmt.Errorf("parse uuid from filename %q: %w", name, err)
	}
	return idStr, extVal, nil
}

func (r *ImageRepository) upload(
	ctx context.Context,
	subdir string,
	id uuid.UUID,
	ext string,
	src io.Reader,
	method string,
) (string, error) {
	op := "repository." + method
	if err := validateExtension(ext); err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}

	key := r.key(subdir, id, ext)
	if err := r.store.Put(ctx, key, src, contentTypeForExt(ext)); err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}
	return key, nil
}

func (r *ImageRepository) download(
	ctx context.Context,
	subdir string,
	id uuid.UUID,
	ext string,
	method string,
) (io.ReadCloser, int64, error) {
	op := "repository." + method
	key := r.key(subdir, id, ext)

	body, size, err := r.store.Get(ctx, key)
	if err != nil {
		if r.isErrNotFound(err) {
			return nil, 0, entity.ErrFileNotFound
		}
		return nil, 0, fmt.Errorf("%s: %w", op, err)
	}
	return body, size, nil
}

func (r *ImageRepository) key(subdir string, id uuid.UUID, ext string) string {
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return subdir + "/" + id.String() + ext
}

func (r *ImageRepository) isErrNotFound(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	return strings.Contains(err.Error(), "NoSuchKey") || strings.Contains(err.Error(), "404")
}

func validateExtension(ext string) error {
	if ext == "" {
		return errors.New("extension is required")
	}
	if !entity.IsValidExtension(ext) {
		return fmt.Errorf("%w: %q", entity.ErrUnsupportedFormat, ext)
	}
	return nil
}

func contentTypeForExt(ext string) string {
	ext = strings.TrimPrefix(strings.ToLower(ext), ".")
	switch ext {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}
