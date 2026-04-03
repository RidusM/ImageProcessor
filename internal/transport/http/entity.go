// nolint: revive,staticcheck
package handlers

import (
	"time"
)

// swagger:model UploadRequest
type UploadRequest struct {
	Image string `json:"image" form:"image" swaggerignore:"true"`
	Options string `json:"options,omitempty" form:"options"`
}

// swagger:model UploadResponse
type UploadResponse struct {
	ID       string `json:"id" example:"a1b2c3d4e5f6"`
	Status   string `json:"status" example:"pending"`
	Filename string `json:"filename" example:"photo.jpg"`
	Size     int64  `json:"size" example:"2048576"`
	Message  string `json:"message" example:"Image uploaded successfully"`
}

// swagger:model ImageStatusResponse
type ImageStatusResponse struct {
	ID        string     `json:"id" example:"a1b2c3d4e5f6"`
	Filename  string     `json:"filename" example:"photo.jpg"`
	Status    string     `json:"status" example:"processing"`
	Progress  int        `json:"progress" example:"45"`
	Error     *string    `json:"error,omitempty" example:"resize failed"`
	CreatedAt time.Time  `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt *time.Time `json:"updated_at,omitempty" example:"2024-01-15T10:30:05Z"`
}

// swagger:model ImageMetadataResponse
type ImageMetadataResponse struct {
	ID        string    `json:"id" example:"a1b2c3d4e5f6"`
	Filename  string    `json:"filename" example:"photo.jpg"`
	Extension string    `json:"extension" example:"jpg"`
	Size      int64     `json:"size" example:"2048576"`
	Width     int       `json:"width,omitempty" example:"1920"`
	Height    int       `json:"height,omitempty" example:"1080"`
	Status    string    `json:"status" example:"done"`
	Links     ImageLinks `json:"links"`
	CreatedAt time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
}

// swagger:model ImageLinks
type ImageLinks struct {
	Self      string `json:"self" example:"/image/a1b2c3d4e5f6"`
	Original  string `json:"original" example:"/image/a1b2c3d4e5f6?version=original"`
	Processed string `json:"processed" example:"/image/a1b2c3d4e5f6?version=processed"`
	Thumbnail string `json:"thumbnail" example:"/image/a1b2c3d4e5f6?version=thumb"`
}

// swagger:model ErrorResponse
type ErrorResponse struct {
	Error   string `json:"error" example:"image not found"`
	Code    string `json:"code,omitempty" example:"not_found"`
	Details string `json:"details,omitempty" example:"image with id abc123 does not exist"`
}

// swagger:model SuccessResponse
type SuccessResponse struct {
	Message string `json:"message" example:"Image deleted successfully"`
}

// swagger:model HealthResponse
type HealthResponse struct {
	Status string `json:"status" example:"ok"`
	Time   string `json:"time" example:"2024-01-15T10:30:00Z"`
}