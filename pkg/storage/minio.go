package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"img-processor/internal/config"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinIOStorage struct {
	client *minio.Client
	bucket string
}

func NewMinIO(cfg config.Storage) (*MinIOStorage, error) {
	client, err := minio.New(cfg.MinIOEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIOAccessKey, cfg.MinIOSecretKey, ""),
		Secure: cfg.MinIOUseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio init: %w", err)
	}

	ctx := context.Background()
	exists, err := client.BucketExists(ctx, cfg.MinIOBucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		if err = client.MakeBucket(ctx, cfg.MinIOBucket, minio.MakeBucketOptions{
			Region: cfg.MinIORegion,
		}); err != nil {
			return nil, fmt.Errorf("create bucket %q: %w", cfg.MinIOBucket, err)
		}
	}

	return &MinIOStorage{client: client, bucket: cfg.MinIOBucket}, nil
}

func (s *MinIOStorage) Put(
	ctx context.Context,
	key string,
	src io.Reader,
	contentType string,
) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, src, -1, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("minio put: %w", err)
	}
	return nil
}

func (s *MinIOStorage) Get(
	ctx context.Context,
	key string,
) (io.ReadCloser, int64, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, fmt.Errorf("minio get object: %w", err)
	}

	stat, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		return nil, 0, fmt.Errorf("minio get stat: %w", err)
	}

	return obj, stat.Size, nil
}

func (s *MinIOStorage) ListObjects(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if obj.Err != nil {
			return nil, fmt.Errorf("minio list: %w", obj.Err)
		}
		keys = append(keys, obj.Key)
	}
	return keys, nil
}

func (s *MinIOStorage) Delete(
	ctx context.Context,
	key string,
) error {
	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("minio delete: %w", err)
	}
	return nil
}

func (s *MinIOStorage) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if s.isNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("minio exists: %w", err)
	}
	return true, nil
}

func (s *MinIOStorage) isNotFound(err error) bool {
	if err == nil {
		return false
	}
	errResp := minio.ToErrorResponse(err)
	return errResp.Code == "NoSuchKey" ||
		errResp.Code == "NoSuchBucket" ||
		errResp.StatusCode == http.StatusNotFound
}
