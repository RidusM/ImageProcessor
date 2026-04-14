package entity

import "time"

type Image struct {
	ID            string     `json:"id"`
	OriginalName  string     `json:"original_name"`
	Extension     string     `json:"extension"`
	Size          int64      `json:"size"`
	MimeType      string     `json:"mime_type"`
	OriginalPath  string     `json:"-"`
	ProcessedPath string     `json:"-"`
	ThumbnailPath string     `json:"-"`
	Width         int        `json:"width,omitempty"`
	Height        int        `json:"height,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	ProcessedAt   *time.Time `json:"processed_at,omitempty"`
}

type ImagePaths struct {
	Original  string
	Processed string
	Thumbnail string
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
