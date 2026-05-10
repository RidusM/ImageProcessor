package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"img-processor/internal/config"
)

type LocalStorage struct {
	root string
}

func NewLocal(cfg config.Storage) (*LocalStorage, error) {
	if err := os.MkdirAll(cfg.Path, 0o750); err != nil {
		return nil, fmt.Errorf("storage.local: init root: %w", err)
	}
	return &LocalStorage{root: cfg.Path}, nil
}

func (s *LocalStorage) Put(_ context.Context, key string, src io.Reader, _ string) error {
	fullPath := filepath.Join(s.root, key)

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		return fmt.Errorf("local put mkdir: %w", err)
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("local put create: %w", err)
	}
	defer f.Close()

	_, err = io.Copy(f, src)
	if err != nil {
		return fmt.Errorf("local put copy: %w", err)
	}
	return nil
}

func (s *LocalStorage) Get(_ context.Context, key string) (io.ReadCloser, int64, error) {
	fullPath := filepath.Join(s.root, key)
	f, err := os.Open(fullPath)
	if err != nil {
		return nil, 0, fmt.Errorf("local get open: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, fmt.Errorf("local get stat: %w", err)
	}

	return f, info.Size(), nil
}

func (s *LocalStorage) Delete(_ context.Context, key string) error {
	err := os.Remove(filepath.Join(s.root, key))
	if err != nil {
		return fmt.Errorf("local delete: %w", err)
	}
	return nil
}

func (s *LocalStorage) Exists(_ context.Context, key string) (bool, error) {
	_, err := os.Stat(filepath.Join(s.root, key))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("local exists: %w", err)
}
