package entity

import "errors"

var (
	ErrImageNotFound        = errors.New("image not found")
	ErrInvalidImageFormat   = errors.New("invalid image format")
	ErrTaskNotFound         = errors.New("task not found")
	ErrFileNotFound         = errors.New("file not found")
	ErrFileTooLarge         = errors.New("file too large")
	ErrProcessingFailed     = errors.New("image processing failed")
	ErrUnsupportedFormat    = errors.New("unsupported image format")
	ErrEncodeFailed         = errors.New("image encoding failed")
	ErrDecodeFailed         = errors.New("image decoding failed")
	ErrInvalidFileSize      = errors.New("invalid file size")
	ErrInvalidResizeWidth   = errors.New("invalid resize width")
	ErrInvalidThumbnailSize = errors.New("invalid thumbnail size")
	ErrInvalidQuality       = errors.New("invalid quality value")
)
