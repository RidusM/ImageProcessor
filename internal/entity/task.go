package entity

import (
	"time"

	"github.com/google/uuid"
)

const (
	_defaultResizeWidth   = 1920
	_defaultThumbnailSize = 300
	_defaultQuality       = 90
)

type Task struct {
	ID           uuid.UUID
	ImageID      uuid.UUID
	Status       Status
	ErrorMessage string
	Progress     int
	Options      *ProcessingOptions
	CreatedAt    time.Time
	StartedAt    *time.Time
	CompletedAt  *time.Time
}

type ProcessingOptions struct {
	ResizeWidth      int
	ResizeHeight     int
	ThumbnailSize    int
	AddWatermark     bool
	ConvertTo        string
	Quality          int
	PreserveMetadata bool
}

func DefaultProcessingOptions() *ProcessingOptions {
	return &ProcessingOptions{
		ResizeWidth:   _defaultResizeWidth,
		ThumbnailSize: _defaultThumbnailSize,
		AddWatermark:  true,
		Quality:       _defaultQuality,
	}
}

func NewTask(imageID uuid.UUID, opts *ProcessingOptions) *Task {
	return &Task{
		ID:        imageID,
		ImageID:   imageID,
		Status:    StatusPending,
		Progress:  0,
		Options:   opts,
		CreatedAt: time.Now(),
	}
}

func (t *Task) MarkProcessing() {
	now := time.Now()
	t.Status = StatusProcessing
	t.StartedAt = &now
	t.Progress = 10
}

func (t *Task) MarkDone() {
	now := time.Now()
	t.Status = StatusDone
	t.CompletedAt = &now
	t.Progress = 100
}

func (t *Task) MarkError(errMsg string) {
	now := time.Now()
	t.Status = StatusError
	t.ErrorMessage = errMsg
	t.CompletedAt = &now
	t.Progress = 0
}
