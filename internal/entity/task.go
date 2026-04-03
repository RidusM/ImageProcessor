package entity

import (
	"time"
)

type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusProcessing TaskStatus = "processing"
	TaskStatusDone       TaskStatus = "done"
	TaskStatusError      TaskStatus = "error"
)

func (s TaskStatus) String() string {
	return string(s)
}

func (s TaskStatus) IsTerminal() bool {
	return s == TaskStatusDone || s == TaskStatusError
}

type Task struct {
	ID           string                 `json:"id"`
	ImageID      string                 `json:"image_id"`
	Status       TaskStatus             `json:"status"`
	ErrorMessage string                 `json:"error_message,omitempty"`
	Progress     int                    `json:"progress,omitempty"`
	Options      *ProcessingOptions     `json:"options,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	StartedAt    *time.Time             `json:"started_at,omitempty"`
	CompletedAt  *time.Time             `json:"completed_at,omitempty"`
	RetryCount   int                    `json:"-"`
}

type ProcessingOptions struct {
	ResizeWidth     int  `json:"resize_width,omitempty"`
	ResizeHeight    int  `json:"resize_height,omitempty"`
	ThumbnailSize   int  `json:"thumbnail_size,omitempty"`
	AddWatermark    bool `json:"add_watermark"`
	ConvertTo       string `json:"convert_to,omitempty"`
	Quality         int  `json:"quality,omitempty"`
	PreserveMetadata bool `json:"preserve_metadata"`
}

func DefaultProcessingOptions() *ProcessingOptions {
	return &ProcessingOptions{
		ResizeWidth:      1920,
		ThumbnailSize:    300,
		AddWatermark:     true,
		Quality:          90,
		PreserveMetadata: false,
	}
}

func (o *ProcessingOptions) Validate() error {
	if o == nil {
		return nil
	}

	if o.ResizeWidth < 0 || o.ResizeWidth > 4096 {
		return ErrInvalidResizeWidth
	}

	if o.ThumbnailSize < 50 || o.ThumbnailSize > 1000 {
		return ErrInvalidThumbnailSize
	}

	if o.Quality < 1 || o.Quality > 100 {
		return ErrInvalidQuality
	}

	if o.ConvertTo != "" && !IsValidExtension(o.ConvertTo) {
		return ErrUnsupportedFormat
	}

	return nil
}

func NewTask(imageID string, opts *ProcessingOptions) *Task {
	return &Task{
		ID:        "",
		ImageID:   imageID,
		Status:    TaskStatusPending,
		Progress:  0,
		Options:   opts,
		CreatedAt: time.Now(),
	}
}

func (t *Task) MarkProcessing() {
	now := time.Now()
	t.Status = TaskStatusProcessing
	t.StartedAt = &now
	t.Progress = 10
}

func (t *Task) MarkDone() {
	now := time.Now()
	t.Status = TaskStatusDone
	t.CompletedAt = &now
	t.Progress = 100
}

func (t *Task) MarkError(errMsg string) {
	now := time.Now()
	t.Status = TaskStatusError
	t.ErrorMessage = errMsg
	t.CompletedAt = &now
	t.Progress = 0
}