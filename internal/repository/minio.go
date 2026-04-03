package repository

import (
	"context"
	"fmt"
	"io"

	"img-processor/internal/config"
	"img-processor/internal/entity"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/wb-go/wbf/logger"
)

type minIORepository struct {
	client *minio.Client
	cfg    config.Storage
	log    logger.Logger
	bucket string
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
		return nil, fmt.Errorf("%s: check bucket: %w", op, err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.MinIOBucket, minio.MakeBucketOptions{
			Region: cfg.MinIORegion,
		}); err != nil {
			return nil, fmt.Errorf("%s: create bucket: %w", op, err)
		}
	}

	return &minIORepository{
		client: client,
		cfg:    cfg,
		log:    log,
		bucket: cfg.MinIOBucket,
	}, nil
}

func (r *minIORepository) SaveOriginal(ctx context.Context, imageID, ext string, src io.Reader) (string, error) {
	const op = "repository.minio.SaveOriginal"

	if err := validateExtension(ext); err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}

	key := r.key("originals", imageID, ext)
	info, err := r.client.PutObject(ctx, r.bucket, key, src, -1, minio.PutObjectOptions{
		ContentType: "image/" + ext,
	})
	if err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}

	r.log.Debug("uploaded original", "image_id", imageID, "key", key, "size", info.Size)

	return key, nil
}

func (r *minIORepository) SaveProcessed(ctx context.Context, imageID, ext string, src io.Reader) (string, error) {
	const op = "repository.minio.SaveProcessed"

	if err := validateExtension(ext); err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}

	key := r.key("processed", imageID, ext)
	_, err := r.client.PutObject(ctx, r.bucket, key, src, -1, minio.PutObjectOptions{
		ContentType: "image/" + ext,
	})
	if err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
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
		ContentType: "image/" + ext,
	})
	if err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
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

	key := r.key("originals", imageID, ext)

	return r.getObject(ctx, op, key)
}

func (r *minIORepository) GetProcessed(ctx context.Context, imageID, ext string) (io.ReadCloser, int64, error) {
	const op = "repository.minio.GetProcessed"

	key := r.key("processed", imageID, ext)

	return r.getObject(ctx, op, key)
}

func (r *minIORepository) GetThumbnail(ctx context.Context, imageID, ext string) (io.ReadCloser, int64, error) {
	const op = "repository.minio.GetThumbnail"

	key := r.key("thumbnails", imageID, ext)

	return r.getObject(ctx, op, key)
}

func (r *minIORepository) getObject(ctx context.Context, op, key string) (io.ReadCloser, int64, error) {
	obj, err := r.client.GetObject(ctx, r.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, 0, entity.ErrFileNotFound
		}
		return nil, 0, fmt.Errorf("%s: get object: %w", op, err)
	}

	info, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		return nil, 0, fmt.Errorf("%s: stat object: %w", op, err)
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
			if minio.ToErrorResponse(err).Code != "NoSuchKey" {
				return fmt.Errorf("%s: remove %s: %w", op, key, err)
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
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return false, nil
		}
		return false, fmt.Errorf("%s: stat: %w", op, err)
	}
	return true, nil
}


func (r *minIORepository) key(subdir, imageID, ext string) string {
	return buildPath(subdir, imageID, ext)
}