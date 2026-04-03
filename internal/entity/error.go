package entity

import (
	"errors"
	"fmt"
)

var (
	ErrImageNotFound      = errors.New("image not found")
	ErrImageAlreadyExists = errors.New("image already exists")
	ErrInvalidImageFormat = errors.New("invalid image format")
	ErrCorruptedImage     = errors.New("corrupted image file")

	ErrTaskNotFound     = errors.New("task not found")
	ErrTaskNotPending   = errors.New("task is not in pending state")
	ErrTaskAlreadyDone  = errors.New("task already completed")

	ErrStorageUnavailable = errors.New("storage unavailable")
	ErrFileNotFound       = errors.New("file not found")
	ErrFileTooLarge       = errors.New("file too large")
	ErrUploadFailed       = errors.New("upload failed")
	ErrDeleteFailed       = errors.New("delete failed")

	ErrProcessingFailed   = errors.New("image processing failed")
	ErrResizeFailed       = errors.New("resize operation failed")
	ErrWatermarkFailed    = errors.New("watermark application failed")
	ErrEncodeFailed       = errors.New("image encoding failed")
	ErrDecodeFailed       = errors.New("image decoding failed")

	ErrUnsupportedFormat    = errors.New("unsupported image format")
	ErrInvalidFileSize      = errors.New("invalid file size")
	ErrInvalidResizeWidth   = errors.New("invalid resize width")
	ErrInvalidThumbnailSize = errors.New("invalid thumbnail size")
	ErrInvalidQuality       = errors.New("invalid quality value")
	ErrInvalidDimensions    = errors.New("invalid image dimensions")

	ErrInternalError = errors.New("internal error")
)

type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Err     error  `json:"-"`
	Details map[string]interface{} `json:"details,omitempty"`
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func NewAppError(code, message string, err error) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

func (e *AppError) WithDetails(key string, value interface{}) *AppError {
	if e.Details == nil {
		e.Details = make(map[string]interface{})
	}
	e.Details[key] = value
	return e
}

func IsAppError(err error) bool {
	_, ok := err.(*AppError)
	return ok
}

func ErrorCode(err error) string {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Code
	}
	return "UNKNOWN"
}

func ErrorMessage(err error) string {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Message
	}
	return err.Error()
}


func NewValidationError(field, message string) *AppError {
	return NewAppError("VALIDATION_ERROR", message, nil).
		WithDetails("field", field)
}

func NewNotFoundError(resource, id string) *AppError {
	return NewAppError("NOT_FOUND", fmt.Sprintf("%s not found", resource), nil).
		WithDetails("resource", resource).
		WithDetails("id", id)
}

func NewProcessingError(operation string, err error) *AppError {
	return NewAppError("PROCESSING_ERROR", fmt.Sprintf("failed to %s", operation), err)
}

func NewStorageError(operation string, err error) *AppError {
	return NewAppError("STORAGE_ERROR", fmt.Sprintf("storage %s failed", operation), err)
}