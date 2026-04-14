package repository

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"img-processor/internal/config"
	"img-processor/internal/entity"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/wb-go/wbf/logger"
)

type minIORepository struct {
	client *minio.Client
	cfg    config.Storage
	bucket string
	log    logger.Logger
}

func newMinIORepository(cfg config.Storage, log logger.Logger) (*minIORepository, error) {
	const op = "repository.newMinIORepository"

	client, err := minio.New(cfg.MinIOEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIOAccessKey, cfg.MinIOSecretKey, ""),
		Secure: cfg.MinIOUseSSL,
		Region: cfg.MinIORegion,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: init client: %w", op, err)
	}

	ctx := context.Background()
	exists, err := client.BucketExists(ctx, cfg.MinIOBucket)
	if err != nil {
		return nil, fmt.Errorf("%s: check bucket %q: %w", op, cfg.MinIOBucket, err)
	}
	if !exists {
		if err = client.MakeBucket(ctx, cfg.MinIOBucket, minio.MakeBucketOptions{
			Region: cfg.MinIORegion,
		}); err != nil {
			return nil, fmt.Errorf("%s: create bucket %q: %w", op, cfg.MinIOBucket, err)
		}
		log.LogAttrs(ctx, logger.InfoLevel, "minio bucket created",
			logger.String("bucket", cfg.MinIOBucket),
		)
	}

	log.LogAttrs(ctx, logger.DebugLevel, "minio repository initialized",
		logger.String("endpoint", cfg.MinIOEndpoint),
		logger.String("bucket", cfg.MinIOBucket),
	)
	return &minIORepository{
		client: client,
		cfg:    cfg,
		bucket: cfg.MinIOBucket,
		log:    log,
	}, nil
}

func (r *minIORepository) SaveOriginal(ctx context.Context, imageID, ext string, src io.Reader) (string, error) {
	const op = "repository.minio.SaveOriginal"
	if err := validateExtension(ext); err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}
	key := r.key("originals", imageID, ext)
	_, err := r.client.PutObject(ctx, r.bucket, key, src, -1, minio.PutObjectOptions{
		ContentType: contentTypeForExt(ext),
	})
	if err != nil {
		return "", fmt.Errorf("%s: put %q: %w", op, key, err)
	}
	return key, nil
}

func (r *minIORepository) SaveProcessed(ctx context.Context, imageID, ext string, src io.Reader) (string, error) {
	const op = "repository.minio.SaveProcessed"
	if err := validateExtension(ext); err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}
	key := r.key("processed", imageID, ext)
	_, err := r.client.PutObject(ctx, r.bucket, key, src, -1, minio.PutObjectOptions{
		ContentType: contentTypeForExt(ext),
	})
	if err != nil {
		return "", fmt.Errorf("%s: put %q: %w", op, key, err)
	}
	return key, nil
}

func (r *minIORepository) SaveThumbnail(ctx context.Context, imageID, ext string, src io.Reader) (string, error) {
	const op = "repository.minio.SaveThumbnail"
	if err := validateExtension(ext); err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}
	key := r.key("thumbnails", imageID, ext)
	_, err := r.client.PutObject(ctx, r.bucket, key, src, -1, minio.PutObjectOptions{
		ContentType: contentTypeForExt(ext),
	})
	if err != nil {
		return "", fmt.Errorf("%s: put %q: %w", op, key, err)
	}
	return key, nil
}

func (r *minIORepository) GetPaths(imageID, ext string) *entity.ImagePaths {
	return &entity.ImagePaths{
		Original:  r.key("originals", imageID, ext),
		Processed: r.key("processed", imageID, ext),
		Thumbnail: r.key("thumbnails", imageID, ext),
	}
}

func (r *minIORepository) GetOriginal(ctx context.Context, imageID, ext string) (io.ReadCloser, int64, error) {
	const op = "repository.minio.GetOriginal"
	return r.getObject(ctx, op, r.key("originals", imageID, ext))
}

func (r *minIORepository) GetProcessed(ctx context.Context, imageID, ext string) (io.ReadCloser, int64, error) {
	const op = "repository.minio.GetProcessed"
	return r.getObject(ctx, op, r.key("processed", imageID, ext))
}

func (r *minIORepository) GetThumbnail(ctx context.Context, imageID, ext string) (io.ReadCloser, int64, error) {
	const op = "repository.minio.GetThumbnail"
	return r.getObject(ctx, op, r.key("thumbnails", imageID, ext))
}

func (r *minIORepository) getObject(ctx context.Context, op, key string) (io.ReadCloser, int64, error) {
	obj, err := r.client.GetObject(ctx, r.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		if isNotFound(err) {
			return nil, 0, entity.ErrFileNotFound
		}
		return nil, 0, fmt.Errorf("%s: get %q: %w", op, key, err)
	}

	info, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		return nil, 0, fmt.Errorf("%s: stat %q: %w", op, key, err)
	}
	return obj, info.Size, nil
}

func (r *minIORepository) Delete(ctx context.Context, imageID, ext string) error {
	const op = "repository.minio.Delete"
	keys := []string{
		r.key("originals", imageID, ext),
		r.key("processed", imageID, ext),
		r.key("thumbnails", imageID, ext),
	}

	for _, key := range keys {
		if err := r.client.RemoveObject(ctx, r.bucket, key, minio.RemoveObjectOptions{}); err != nil {
			if !isNotFound(err) {
				return fmt.Errorf("%s: remove %q: %w", op, key, err)
			}
		}
	}
	return nil
}

func (r *minIORepository) Exists(ctx context.Context, imageID, ext string) (bool, error) {
	const op = "repository.minio.Exists"
	key := r.key("processed", imageID, ext)
	_, err := r.client.StatObject(ctx, r.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("%s: stat %q: %w", op, key, err)
	}
	return true, nil
}

func (r *minIORepository) key(subdir, imageID, ext string) string {
	return buildPath(subdir, imageID, ext)
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	resp := minio.ToErrorResponse(err)
	return resp.Code == "NoSuchKey" || resp.StatusCode == http.StatusNotFound
}

func contentTypeForExt(ext string) string {
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
