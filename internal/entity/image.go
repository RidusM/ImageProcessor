package entity

import (
	"time"

	"github.com/google/uuid"
)

type Image struct {
	ID            uuid.UUID
	OriginalName  string
	Extension     string
	Size          int64
	MimeType      string
	OriginalPath  string
	ProcessedPath string
	ThumbnailPath string
	Width         int
	Height        int
	CreatedAt     time.Time
	ProcessedAt   *time.Time
}

type ImagePaths struct {
	Original  string
	Processed string
	Thumbnail string
}

type ImageMeta struct {
	ID           uuid.UUID
	Ext          string
	OriginalName string
}

func SupportedExtensions() []string {
	return []string{"jpg", "jpeg", "png", "gif", "webp"}
}

func IsValidExtension(ext string) bool {
	for _, e := range SupportedExtensions() {
		if e == ext {
			return true
		}
	}
	return false
}
