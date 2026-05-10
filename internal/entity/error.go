package entity

import "errors"

var (
	ErrDataNotFound         = errors.New("data not found")
	ErrConflictingData      = errors.New("conflicting data")
	ErrInvalidData          = errors.New("invalid data")
	ErrImageNotFound        = errors.New("image not found")
	ErrInvalidImageFormat   = errors.New("invalid image format")
	ErrTaskNotFound         = errors.New("task not found")
	ErrFileNotFound         = errors.New("file not found")
	ErrProcessingFailed     = errors.New("image processing failed")
	ErrUnsupportedFormat    = errors.New("unsupported image format")
	ErrDecodeFailed         = errors.New("image decoding failed")
	ErrInvalidFileSize      = errors.New("invalid file size")
	ErrInvalidResizeWidth   = errors.New("invalid resize width")
	ErrInvalidThumbnailSize = errors.New("invalid thumbnail size")
	ErrInvalidQuality       = errors.New("invalid quality value")
)
