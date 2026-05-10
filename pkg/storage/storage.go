package storage

import (
	"context"
	"io"
)

type Provider interface {
	Put(ctx context.Context, key string, src io.Reader, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, int64, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
}
